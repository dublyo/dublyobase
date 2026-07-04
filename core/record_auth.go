package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RecordRole string

const (
	RecordRoleAnon          RecordRole = "anon"
	RecordRoleAuthenticated RecordRole = "authenticated"
	RecordRoleService       RecordRole = "service"
)

type RecordAuth struct {
	Project    Project
	Role       RecordRole
	RoleName   string
	Subject    string
	Collection string
	Claims     map[string]any
}

func ResolveRecordAuth(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, token string, now time.Time) (*RecordAuth, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	_, roles := ProjectNames(project.Slug)
	token = strings.TrimSpace(token)
	if token == "" {
		return newRecordAuth(project, RecordRoleAnon, roles.Anon, "", ""), nil
	}
	if strings.HasPrefix(token, adminTokenPrefix) {
		adminAuth, err := FindAdminByToken(ctx, pool, token, now)
		if err != nil {
			return nil, err
		}
		if adminAuth.Admin.MustChangePassword {
			return nil, ErrPasswordChangeRequired
		}
		return newRecordAuth(project, RecordRoleService, roles.Service, "", ""), nil
	}
	if strings.HasPrefix(token, apiKeyPrefix(APIKeyAnon)) || strings.HasPrefix(token, apiKeyPrefix(APIKeyService)) {
		key, err := FindAPIKey(ctx, pool, token)
		if err != nil {
			return nil, err
		}
		if key.ProjectID != project.ID {
			return nil, ErrUnauthorized
		}
		if key.Type == APIKeyService {
			return newRecordAuth(project, RecordRoleService, roles.Service, "", ""), nil
		}
		return newRecordAuth(project, RecordRoleAnon, roles.Anon, "", ""), nil
	}
	claims, err := parseAppJWT(cfg.JWTSecret, token, now)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if !validAppClaimsForProject(claims, project) {
		return nil, ErrUnauthorized
	}
	matches, err := appUserMatchesTokenKey(ctx, pool, project, claims.Subject, claims.TokenKey)
	if err != nil {
		return nil, err
	}
	if !matches {
		return nil, ErrUnauthorized
	}
	return newRecordAuth(project, RecordRoleAuthenticated, roles.Authenticated, claims.Subject, claims.Collection), nil
}

func newRecordAuth(project *Project, role RecordRole, roleName string, subject string, collection string) *RecordAuth {
	claims := map[string]any{
		"role":    string(role),
		"project": project.Slug,
	}
	if subject != "" {
		claims["sub"] = subject
	}
	if collection != "" {
		claims["collection"] = collection
	}
	return &RecordAuth{
		Project:    *project,
		Role:       role,
		RoleName:   roleName,
		Subject:    subject,
		Collection: collection,
		Claims:     claims,
	}
}

// ServiceRecordAuth returns the project service role auth context for trusted
// control-plane operations such as admin UI and scoped MCP tools. Managed
// Dublyobase tables still go through SET LOCAL ROLE and RLS; imported tables
// use the app database role for service access because they may not grant the
// project service role.
func ServiceRecordAuth(project *Project) *RecordAuth {
	_, roles := ProjectNames(project.Slug)
	return newRecordAuth(project, RecordRoleService, roles.Service, "", "")
}

func withRecordTx(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, operation string, fn func(pgx.Tx) error) error {
	return withRecordTxOptions(ctx, pool, auth, operation, false, fn)
}

func withRecordTxOptions(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, operation string, bypassRole bool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	claimsJSON, err := json.Marshal(auth.Claims)
	if err != nil {
		return err
	}
	searchPath := auth.Project.SchemaName + ", pg_catalog"
	if !bypassRole {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`set local role %s`, quoteIdent(auth.RoleName))); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `set local statement_timeout = '5s'`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `select set_config('request.jwt.claims', $1, true)`, string(claimsJSON)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `select set_config('request.operation', $1, true)`, operation); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `select set_config('search_path', $1, true)`, searchPath); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
