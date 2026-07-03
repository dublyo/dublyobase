package core

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type APIKeyType string

const (
	APIKeyAnon    APIKeyType = "anon"
	APIKeyService APIKeyType = "service"
)

type APIKey struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"projectId"`
	Name      string     `json:"name"`
	Type      APIKeyType `json:"type"`
	Prefix    string     `json:"prefix"`
	CreatedAt time.Time  `json:"createdAt"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
}

type CreatedAPIKey struct {
	APIKey
	Key string `json:"key"`
}

func apiKeyPrefix(typ APIKeyType) string {
	return "dbo_" + string(typ) + "_"
}

func ValidateAPIKeyInput(name string, typ APIKeyType) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: API key name is required", ErrValidation)
	}
	if typ != APIKeyAnon && typ != APIKeyService {
		return fmt.Errorf("%w: API key type must be anon or service", ErrValidation)
	}
	return nil
}

func GenerateAPIKey(typ APIKeyType) (string, string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", err
	}
	key := apiKeyPrefix(typ) + base64.RawURLEncoding.EncodeToString(raw[:])
	prefixLen := len(apiKeyPrefix(typ)) + 8
	if prefixLen > len(key) {
		prefixLen = len(key)
	}
	return key, key[:prefixLen], nil
}

func CreateAPIKey(ctx context.Context, pool *pgxpool.Pool, adminID string, projectSlug string, name string, typ APIKeyType, ip string, userAgent string) (*CreatedAPIKey, error) {
	name = strings.TrimSpace(name)
	if err := ValidateAPIKeyInput(name, typ); err != nil {
		return nil, err
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	key, prefix, err := GenerateAPIKey(typ)
	if err != nil {
		return nil, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	out := &CreatedAPIKey{Key: key}
	if err := tx.QueryRow(ctx, `
		insert into _dbo.api_keys (project_id, name, type, key_hash, prefix)
		values ($1, $2, $3, $4, $5)
		returning id, project_id, name, type, prefix, created_at, revoked_at`,
		project.ID,
		name,
		typ,
		HashToken(key),
		prefix,
	).Scan(&out.ID, &out.ProjectID, &out.Name, &out.Type, &out.Prefix, &out.CreatedAt, &out.RevokedAt); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "api_key.create",
		TargetType: "api_key",
		TargetID:   out.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "name": name, "type": typ, "prefix": prefix},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func ListAPIKeys(ctx context.Context, pool *pgxpool.Pool, projectSlug string) ([]APIKey, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `
		select id, project_id, name, type, prefix, created_at, revoked_at
		from _dbo.api_keys
		where project_id = $1
		order by created_at desc`,
		project.ID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *key)
	}
	return out, rows.Err()
}

func RevokeAPIKey(ctx context.Context, pool *pgxpool.Pool, adminID string, projectSlug string, keyID string, ip string, userAgent string) error {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var target APIKey
	if err := tx.QueryRow(ctx, `
		update _dbo.api_keys
		set revoked_at = coalesce(revoked_at, now())
		where id = $1 and project_id = $2
		returning id, project_id, name, type, prefix, created_at, revoked_at`,
		keyID,
		project.ID,
	).Scan(&target.ID, &target.ProjectID, &target.Name, &target.Type, &target.Prefix, &target.CreatedAt, &target.RevokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		return err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "api_key.revoke",
		TargetType: "api_key",
		TargetID:   target.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "name": target.Name, "type": target.Type, "prefix": target.Prefix},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func FindAPIKey(ctx context.Context, pool *pgxpool.Pool, token string) (*APIKey, error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, apiKeyPrefix(APIKeyAnon)) && !strings.HasPrefix(token, apiKeyPrefix(APIKeyService)) {
		return nil, ErrUnauthorized
	}
	row := pool.QueryRow(ctx, `
		select id, project_id, name, type, prefix, created_at, revoked_at
		from _dbo.api_keys
		where key_hash = $1 and revoked_at is null`,
		HashToken(token),
	)
	return scanAPIKey(row)
}

type apiKeyScanner interface {
	Scan(dest ...any) error
}

func scanAPIKey(row apiKeyScanner) (*APIKey, error) {
	var key APIKey
	if err := row.Scan(&key.ID, &key.ProjectID, &key.Name, &key.Type, &key.Prefix, &key.CreatedAt, &key.RevokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	return &key, nil
}
