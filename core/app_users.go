package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const authUsersCollection = "users"

var authUsersHiddenColumns = map[string]struct{}{
	"email_normalized": {},
	"password_hash":    {},
	"token_key":        {},
	"disabled_at":      {},
	"last_login_at":    {},
}

type AppUser struct {
	ID       string    `json:"id"`
	Email    string    `json:"email"`
	Verified bool      `json:"verified"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

type appUserCredential struct {
	AppUser
	PasswordHash string
	TokenKey     string
	DisabledAt   sql.NullTime
}

func ValidateAppUserEmail(email string) error {
	if email == "" {
		return fmt.Errorf("%w: email is required", ErrValidation)
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return fmt.Errorf("%w: email is invalid", ErrValidation)
	}
	return nil
}

func ValidateAppUserPassword(password string) error {
	if len(password) < minAdminPasswordSize {
		return fmt.Errorf("%w: password must be at least %d characters", ErrValidation, minAdminPasswordSize)
	}
	return nil
}

func EnsureAuthUsersCollection(ctx context.Context, pool *pgxpool.Pool, projectSlug string) (*Collection, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	collection, err := ensureAuthUsersCollectionTx(ctx, tx, project)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return collection, nil
}

func ensureAuthUsersCollectionTx(ctx context.Context, tx pgx.Tx, project *Project) (*Collection, error) {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, collectionAdvisoryLockID); err != nil {
		return nil, err
	}

	collection, err := getCollectionTx(ctx, tx, project.ID, authUsersCollection)
	if err != nil && !errors.Is(err, ErrCollectionNotFound) {
		return nil, err
	}
	if errors.Is(err, ErrCollectionNotFound) {
		exists, err := tableExistsTx(ctx, tx, project.SchemaName, authUsersCollection)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrProvisioningConflict
		}
		collection, err = insertAuthUsersCollectionTx(ctx, tx, project.ID)
		if err != nil {
			return nil, err
		}
	} else if collection.Type != CollectionAuth || !collection.System {
		return nil, ErrProvisioningConflict
	}

	if err := createAuthUsersTable(ctx, tx, project.SchemaName); err != nil {
		return nil, err
	}
	if err := syncCollectionPolicies(ctx, tx, project, collection); err != nil {
		return nil, err
	}
	return collection, nil
}

func insertAuthUsersCollectionTx(ctx context.Context, tx pgx.Tx, projectID string) (*Collection, error) {
	viewRule := "id = @request.auth.id"
	updateRule := "id = @request.auth.id"
	fieldsJSON, err := encodeFields(nil)
	if err != nil {
		return nil, err
	}
	options := json.RawMessage(`{"auth":true}`)
	collection := &Collection{
		ProjectID:  projectID,
		Name:       authUsersCollection,
		Type:       CollectionAuth,
		System:     true,
		Fields:     []Field{},
		ViewRule:   &viewRule,
		UpdateRule: &updateRule,
		Options:    options,
		ListRule:   nil,
		CreateRule: nil,
		DeleteRule: nil,
	}
	if err := ValidateCollectionRules(collection); err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `
		insert into _dbo.collections
			(project_id, name, type, system, fields, list_rule, view_rule, create_rule, update_rule, delete_rule, options)
		values ($1, $2, $3, true, $4::jsonb, null, $5, null, $6, null, $7::jsonb)
		returning id`,
		projectID,
		collection.Name,
		collection.Type,
		fieldsJSON,
		viewRule,
		updateRule,
		options,
	).Scan(&collection.ID); err != nil {
		if pgErrCode(err) == "23505" {
			return nil, ErrProvisioningConflict
		}
		return nil, err
	}
	return collection, nil
}

func tableExistsTx(ctx context.Context, tx pgx.Tx, schemaName string, tableName string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `select to_regclass($1) is not null`, schemaName+"."+tableName).Scan(&exists)
	return exists, err
}

func createAuthUsersTable(ctx context.Context, tx pgx.Tx, schemaName string) error {
	table := quoteIdent(schemaName, authUsersCollection)
	statements := []string{
		fmt.Sprintf(`create table if not exists %s (
			id uuid primary key default gen_random_uuid(),
			created timestamptz not null default now(),
			updated timestamptz not null default now(),
			email text not null,
			email_normalized text not null,
			verified boolean not null default false,
			password_hash text not null,
			token_key text not null,
			disabled_at timestamptz null,
			last_login_at timestamptz null
		)`, table),
		fmt.Sprintf(`alter table %s add column if not exists email text not null`, table),
		fmt.Sprintf(`alter table %s add column if not exists email_normalized text not null`, table),
		fmt.Sprintf(`alter table %s add column if not exists verified boolean not null default false`, table),
		fmt.Sprintf(`alter table %s add column if not exists password_hash text not null`, table),
		fmt.Sprintf(`alter table %s add column if not exists token_key text not null`, table),
		fmt.Sprintf(`alter table %s add column if not exists disabled_at timestamptz null`, table),
		fmt.Sprintf(`alter table %s add column if not exists last_login_at timestamptz null`, table),
		fmt.Sprintf(`create unique index if not exists %s on %s (email_normalized)`, quoteIdent("users_email_normalized_idx"), table),
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return enableDefaultDenyRLS(ctx, tx, schemaName, authUsersCollection)
}

func getAppUserByEmailTx(ctx context.Context, tx pgx.Tx, project *Project, emailNormalized string) (*appUserCredential, error) {
	table := quoteIdent(project.SchemaName, authUsersCollection)
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		select id, email, verified, created, updated, password_hash, token_key, disabled_at
		from %s
		where email_normalized = $1`,
		table,
	), emailNormalized)
	return scanAppUserCredential(row)
}

func getAppUserByIDTx(ctx context.Context, tx pgx.Tx, project *Project, userID string) (*appUserCredential, error) {
	table := quoteIdent(project.SchemaName, authUsersCollection)
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		select id, email, verified, created, updated, password_hash, token_key, disabled_at
		from %s
		where id = $1`,
		table,
	), userID)
	return scanAppUserCredential(row)
}

func GetAppUserByID(ctx context.Context, pool *pgxpool.Pool, project *Project, userID string) (*AppUser, error) {
	if err := ValidateUUID(userID); err != nil {
		return nil, err
	}
	table := quoteIdent(project.SchemaName, authUsersCollection)
	row := pool.QueryRow(ctx, fmt.Sprintf(`
		select id, email, verified, created, updated, password_hash, token_key, disabled_at
		from %s
		where id = $1`,
		table,
	), userID)
	cred, err := scanAppUserCredential(row)
	if err != nil {
		return nil, err
	}
	if cred.DisabledAt.Valid {
		return nil, ErrUserDisabled
	}
	return &cred.AppUser, nil
}

func appUserMatchesTokenKey(ctx context.Context, pool *pgxpool.Pool, project *Project, userID string, tokenKey string) (bool, error) {
	table := quoteIdent(project.SchemaName, authUsersCollection)
	var exists bool
	err := pool.QueryRow(ctx, fmt.Sprintf(`
		select exists(
			select 1 from %s
			where id = $1 and token_key = $2 and disabled_at is null
		)`,
		table,
	), userID, tokenKey).Scan(&exists)
	return exists, err
}

func scanAppUserCredential(row interface{ Scan(...any) error }) (*appUserCredential, error) {
	var user appUserCredential
	if err := row.Scan(
		&user.ID,
		&user.Email,
		&user.Verified,
		&user.Created,
		&user.Updated,
		&user.PasswordHash,
		&user.TokenKey,
		&user.DisabledAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	return &user, nil
}

func normalizeAppEmail(email string) (string, error) {
	normalized := NormalizeEmail(email)
	if err := ValidateAppUserEmail(normalized); err != nil {
		return "", err
	}
	return normalized, nil
}

func isHiddenAuthColumn(name string) bool {
	name = NormalizeIdentifier(name)
	_, hidden := authUsersHiddenColumns[name]
	return hidden || strings.HasPrefix(name, "password")
}
