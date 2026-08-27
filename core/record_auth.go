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
	OrgID      string
	OrgRole    string
	Claims     map[string]any
}

// ResolveRecordAuth resolves the caller without an active organization.
func ResolveRecordAuth(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, token string, now time.Time) (*RecordAuth, error) {
	return ResolveRecordAuthForOrg(ctx, pool, cfg, projectSlug, token, "", now)
}

// ResolveRecordAuthForOrg additionally binds the request to one organization,
// supplied by the caller as X-Org-Id. Membership is verified here on every
// request — the header is a claim by the client and is never trusted on its
// own — and the resulting org id and role are published as request claims so
// policies can be written as `org = @request.auth.orgId`.
func ResolveRecordAuthForOrg(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, token string, orgID string, now time.Time) (*RecordAuth, error) {
	auth, err := resolveRecordAuthBase(ctx, pool, cfg, projectSlug, token, now)
	if err != nil {
		return nil, err
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return auth, nil
	}
	if err := ValidateUUID(orgID); err != nil {
		return nil, fmt.Errorf("%w: X-Org-Id must be a UUID", ErrValidation)
	}
	switch auth.Role {
	case RecordRoleAuthenticated:
		// A signed-in user may only act inside an organization they belong to.
		role, err := getOrganizationRoleRow(ctx, pool, &auth.Project, orgID, auth.Subject)
		if err != nil {
			return nil, err
		}
		auth.OrgID, auth.OrgRole = orgID, role
	case RecordRoleService:
		// The claim is published so rules referencing @request.auth.orgId can be
		// exercised, but it does NOT restrict what a service key may read or
		// write: service policies are unconditional (see syncCollectionPolicies).
		// A service key is a project-wide credential and passing X-Org-Id does
		// not turn it into a tenant-scoped one.
		auth.OrgID, auth.OrgRole = orgID, OrgRoleOwner
	default:
		return nil, ErrForbidden
	}
	auth.Claims["org"] = auth.OrgID
	auth.Claims["org_role"] = auth.OrgRole
	return auth, nil
}

func resolveRecordAuthBase(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, token string, now time.Time) (*RecordAuth, error) {
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
