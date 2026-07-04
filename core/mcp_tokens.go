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

const (
	MCPAdminScope   = "admin"
	MCPProjectScope = "project"
)

type MCPToken struct {
	ID               string     `json:"id"`
	Scope            string     `json:"scope"`
	ProjectID        *string    `json:"projectId,omitempty"`
	ProjectSlug      string     `json:"projectSlug,omitempty"`
	Name             string     `json:"name"`
	Prefix           string     `json:"prefix"`
	AllowedTools     []string   `json:"allowedTools"`
	CreatedByAdminID *string    `json:"createdByAdminId,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
}

type CreatedMCPToken struct {
	MCPToken
	Token string `json:"token"`
}

type MCPTokenInput struct {
	Scope        string   `json:"scope"`
	ProjectSlug  string   `json:"projectSlug,omitempty"`
	Name         string   `json:"name"`
	AllowedTools []string `json:"allowedTools"`
	ExpiresAt    string   `json:"expiresAt,omitempty"`
}

func mcpTokenPrefix(scope string) string {
	return "dbo_mcp_" + scope + "_"
}

func GenerateMCPToken(scope string) (string, string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", err
	}
	token := mcpTokenPrefix(scope) + base64.RawURLEncoding.EncodeToString(raw[:])
	prefixLen := len(mcpTokenPrefix(scope)) + 8
	if prefixLen > len(token) {
		prefixLen = len(token)
	}
	return token, token[:prefixLen], nil
}

func ValidateMCPTokenInput(input *MCPTokenInput) (*time.Time, error) {
	input.Scope = strings.ToLower(strings.TrimSpace(input.Scope))
	if input.Scope == "" {
		input.Scope = MCPProjectScope
	}
	if input.Scope != MCPAdminScope && input.Scope != MCPProjectScope {
		return nil, fmt.Errorf("%w: MCP scope must be admin or project", ErrValidation)
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return nil, fmt.Errorf("%w: MCP token name is required", ErrValidation)
	}
	if input.Scope == MCPProjectScope && strings.TrimSpace(input.ProjectSlug) == "" {
		return nil, fmt.Errorf("%w: project MCP token requires projectSlug", ErrValidation)
	}
	if input.Scope == MCPAdminScope && strings.TrimSpace(input.ProjectSlug) != "" {
		return nil, fmt.Errorf("%w: admin MCP token must not set projectSlug", ErrValidation)
	}
	input.AllowedTools = normalizeToolList(input.AllowedTools)
	if len(input.AllowedTools) == 0 {
		input.AllowedTools = DefaultMCPTools(input.Scope)
	}
	var expiresAt *time.Time
	if strings.TrimSpace(input.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(input.ExpiresAt))
		if err != nil {
			return nil, fmt.Errorf("%w: expiresAt must be RFC3339", ErrValidation)
		}
		expiresAt = &parsed
	}
	return expiresAt, nil
}

func DefaultMCPTools(scope string) []string {
	if scope == MCPAdminScope {
		return []string{
			"projects.list", "projects.create",
			"collections.list", "collections.create", "collections.update",
			"schema.discover", "schema.import",
			"records.list", "records.create", "records.update", "records.delete",
			"files.upload_base64", "users.create",
			"settings.smtp.update", "settings.storage.update", "settings.storage.test",
			"cron.list", "cron.create", "cron.run",
			"backups.list", "backups.create", "backups.run",
		}
	}
	return []string{
		"collections.list", "collections.create", "collections.update",
		"schema.discover", "schema.import",
		"records.list", "records.create", "records.update", "records.delete",
		"files.upload_base64", "users.create",
		"backups.list", "backups.create", "backups.run",
	}
}

func CreateMCPToken(ctx context.Context, pool *pgxpool.Pool, adminID string, input MCPTokenInput, ip string, userAgent string) (*CreatedMCPToken, error) {
	expiresAt, err := ValidateMCPTokenInput(&input)
	if err != nil {
		return nil, err
	}
	var projectID *string
	if input.Scope == MCPProjectScope {
		project, err := GetProject(ctx, pool, input.ProjectSlug)
		if err != nil {
			return nil, err
		}
		projectID = &project.ID
	}
	token, prefix, err := GenerateMCPToken(input.Scope)
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	out := &CreatedMCPToken{Token: token}
	out.MCPToken, err = scanMCPTokenValue(tx.QueryRow(ctx, `
		insert into _dbo.mcp_tokens
			(scope, project_id, name, token_hash, prefix, allowed_tools, created_by_admin_id, expires_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		returning id, scope, project_id, coalesce((select slug from _dbo.projects where id = project_id), ''), name, prefix, allowed_tools, created_by_admin_id, created_at, expires_at, revoked_at`,
		input.Scope,
		projectID,
		input.Name,
		HashToken(token),
		prefix,
		input.AllowedTools,
		nullString(adminID),
		expiresAt,
	))
	if err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "mcp_token.create",
		TargetType: "mcp_token",
		TargetID:   out.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"scope": out.Scope, "project": out.ProjectSlug, "prefix": out.Prefix, "tools": out.AllowedTools},
	}); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func ListMCPTokens(ctx context.Context, pool *pgxpool.Pool) ([]MCPToken, error) {
	rows, err := pool.Query(ctx, `
		select m.id, m.scope, m.project_id, coalesce(p.slug, ''), m.name, m.prefix, m.allowed_tools, m.created_by_admin_id, m.created_at, m.expires_at, m.revoked_at
		from _dbo.mcp_tokens m
		left join _dbo.projects p on p.id = m.project_id
		order by m.created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MCPToken, 0)
	for rows.Next() {
		token, err := scanMCPTokenValue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, token)
	}
	return out, rows.Err()
}

func RevokeMCPToken(ctx context.Context, pool *pgxpool.Pool, adminID string, id string, ip string, userAgent string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	token, err := scanMCPTokenValue(tx.QueryRow(ctx, `
		update _dbo.mcp_tokens set revoked_at = coalesce(revoked_at, now())
		where id = $1
		returning id, scope, project_id, coalesce((select slug from _dbo.projects where id = project_id), ''), name, prefix, allowed_tools, created_by_admin_id, created_at, expires_at, revoked_at`, id))
	if err != nil {
		return err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "mcp_token.revoke",
		TargetType: "mcp_token",
		TargetID:   token.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"scope": token.Scope, "project": token.ProjectSlug, "prefix": token.Prefix},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func FindMCPToken(ctx context.Context, pool *pgxpool.Pool, token string, now time.Time) (*MCPToken, error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, mcpTokenPrefix(MCPAdminScope)) && !strings.HasPrefix(token, mcpTokenPrefix(MCPProjectScope)) {
		return nil, ErrUnauthorized
	}
	out, err := scanMCPTokenValue(pool.QueryRow(ctx, `
		select m.id, m.scope, m.project_id, coalesce(p.slug, ''), m.name, m.prefix, m.allowed_tools, m.created_by_admin_id, m.created_at, m.expires_at, m.revoked_at
		from _dbo.mcp_tokens m
		left join _dbo.projects p on p.id = m.project_id
		where m.token_hash = $1 and m.revoked_at is null`, HashToken(token)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if out.ExpiresAt != nil && !out.ExpiresAt.After(now.UTC()) {
		return nil, ErrSessionExpired
	}
	return &out, nil
}

func (t MCPToken) Allows(tool string) bool {
	for _, allowed := range t.AllowedTools {
		if allowed == "*" || allowed == tool {
			return true
		}
	}
	return false
}

type mcpTokenScanner interface{ Scan(dest ...any) error }

func scanMCPTokenValue(row mcpTokenScanner) (MCPToken, error) {
	var token MCPToken
	if err := row.Scan(&token.ID, &token.Scope, &token.ProjectID, &token.ProjectSlug, &token.Name, &token.Prefix, &token.AllowedTools, &token.CreatedByAdminID, &token.CreatedAt, &token.ExpiresAt, &token.RevokedAt); err != nil {
		return token, err
	}
	if token.AllowedTools == nil {
		token.AllowedTools = []string{}
	}
	return token, nil
}

func normalizeToolList(tools []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		tool = strings.ToLower(strings.TrimSpace(tool))
		if tool == "" {
			continue
		}
		if _, ok := seen[tool]; ok {
			continue
		}
		seen[tool] = struct{}{}
		out = append(out, tool)
	}
	return out
}
