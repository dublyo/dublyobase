package core

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const projectAdvisoryLockID int64 = 326_326_003

var projectSlugRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,30}$`)

type Project struct {
	ID         string              `json:"id"`
	Slug       string              `json:"slug"`
	Name       string              `json:"name"`
	SchemaName string              `json:"schemaName"`
	Roles      ProjectRoles        `json:"roles,omitempty"`
	CORS       ProjectCORSSettings `json:"cors"`
}

type ProjectRoles struct {
	Anon          string `json:"anon"`
	Authenticated string `json:"authenticated"`
	Service       string `json:"service"`
}

type ProjectCORSSettings struct {
	PublicOrigins []string `json:"publicOrigins"`
	Source        string   `json:"source"`
	Wildcard      bool     `json:"wildcard"`
}

type ProjectCORSSettingsInput struct {
	PublicOrigins []string `json:"publicOrigins"`
	AllowWildcard bool     `json:"allowWildcard"`
}

func NormalizeProjectSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

func ValidateProjectSlug(slug string) error {
	if !projectSlugRe.MatchString(slug) {
		return fmt.Errorf("%w: project slug must match %s", ErrValidation, projectSlugRe.String())
	}
	switch {
	case slug == "public", slug == "_dbo", slug == "information_schema":
		return fmt.Errorf("%w: project slug is reserved", ErrValidation)
	case strings.HasPrefix(slug, "pg_"):
		return fmt.Errorf("%w: project slug prefix is reserved", ErrValidation)
	}
	return nil
}

func ProjectNames(slug string) (schema string, roles ProjectRoles) {
	return "proj_" + slug, ProjectRoles{
		Anon:          slug + "_anon",
		Authenticated: slug + "_authenticated",
		Service:       slug + "_service",
	}
}

func ProvisionProject(ctx context.Context, pool *pgxpool.Pool, adminID string, slug string, name string, ip string, userAgent string) (*Project, error) {
	slug = NormalizeProjectSlug(slug)
	name = strings.TrimSpace(name)
	if err := ValidateProjectSlug(slug); err != nil {
		return nil, err
	}
	if name == "" {
		return nil, fmt.Errorf("%w: project name is required", ErrValidation)
	}
	schemaName, roles := ProjectNames(slug)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, projectAdvisoryLockID); err != nil {
		return nil, err
	}

	project := &Project{Slug: slug, Name: name, SchemaName: schemaName, Roles: roles}
	if err := tx.QueryRow(ctx, `
		insert into _dbo.projects (slug, name, schema_name)
		values ($1, $2, $3)
		returning id`,
		slug,
		name,
		schemaName,
	).Scan(&project.ID); err != nil {
		if pgErrCode(err) == "23505" {
			return nil, ErrProjectExists
		}
		return nil, err
	}

	if err := createProjectDatabaseObjects(ctx, tx, schemaName, roles); err != nil {
		if code := pgErrCode(err); code == "42P06" || code == "42710" {
			return nil, ErrProvisioningConflict
		}
		return nil, err
	}
	if _, err := ensureAuthUsersCollectionTx(ctx, tx, project); err != nil {
		return nil, err
	}

	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "project.create",
		TargetType: "project",
		TargetID:   project.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data: map[string]any{
			"slug":        slug,
			"schema_name": schemaName,
		},
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return project, nil
}

func createProjectDatabaseObjects(ctx context.Context, tx pgx.Tx, schemaName string, roles ProjectRoles) error {
	var appRole string
	if err := tx.QueryRow(ctx, `select current_user`).Scan(&appRole); err != nil {
		return err
	}

	schemaIdent := pgx.Identifier{schemaName}.Sanitize()
	anonRole := pgx.Identifier{roles.Anon}.Sanitize()
	authRole := pgx.Identifier{roles.Authenticated}.Sanitize()
	serviceRole := pgx.Identifier{roles.Service}.Sanitize()
	appRoleIdent := pgx.Identifier{appRole}.Sanitize()

	statements := []string{
		fmt.Sprintf(`create schema %s`, schemaIdent),
		fmt.Sprintf(`create role %s nologin`, anonRole),
		fmt.Sprintf(`create role %s nologin`, authRole),
		fmt.Sprintf(`create role %s nologin`, serviceRole),
		fmt.Sprintf(`grant %s, %s, %s to %s`, anonRole, authRole, serviceRole, appRoleIdent),
		fmt.Sprintf(`revoke all on schema %s from public`, schemaIdent),
		fmt.Sprintf(`grant usage on schema %s to %s, %s, %s`, schemaIdent, anonRole, authRole, serviceRole),
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func ListProjects(ctx context.Context, pool *pgxpool.Pool) ([]Project, error) {
	rows, err := pool.Query(ctx, `
		select id, slug, name, schema_name, coalesce(public_cors_origins, '{}'::text[]), public_cors_origins is not null
		from _dbo.projects
		where disabled_at is null
		order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := make([]Project, 0)
	for rows.Next() {
		var p Project
		var publicOrigins []string
		var corsConfigured bool
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.SchemaName, &publicOrigins, &corsConfigured); err != nil {
			return nil, err
		}
		_, p.Roles = ProjectNames(p.Slug)
		if corsConfigured {
			p.CORS = ProjectCORSSettings{PublicOrigins: publicOrigins, Source: corsSourceDatabase, Wildcard: corsWildcard(publicOrigins)}
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func GetProject(ctx context.Context, pool *pgxpool.Pool, slug string) (*Project, error) {
	slug = NormalizeProjectSlug(slug)
	if err := ValidateProjectSlug(slug); err != nil {
		return nil, err
	}
	var p Project
	var publicOrigins []string
	var corsConfigured bool
	if err := pool.QueryRow(ctx, `
		select id, slug, name, schema_name, coalesce(public_cors_origins, '{}'::text[]), public_cors_origins is not null
		from _dbo.projects
		where slug = $1 and disabled_at is null`,
		slug,
	).Scan(&p.ID, &p.Slug, &p.Name, &p.SchemaName, &publicOrigins, &corsConfigured); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProjectNotFound
		}
		return nil, err
	}
	_, p.Roles = ProjectNames(p.Slug)
	if corsConfigured {
		p.CORS = ProjectCORSSettings{PublicOrigins: publicOrigins, Source: corsSourceDatabase, Wildcard: corsWildcard(publicOrigins)}
	}
	return &p, nil
}

func UpdateProjectCORS(ctx context.Context, pool *pgxpool.Pool, cfg *Config, adminID string, slug string, input ProjectCORSSettingsInput, ip string, userAgent string) (*Project, error) {
	slug = NormalizeProjectSlug(slug)
	if err := ValidateProjectSlug(slug); err != nil {
		return nil, err
	}
	origins, err := normalizeCORSOrigins(input.PublicOrigins, input.AllowWildcard)
	if err != nil {
		return nil, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var p Project
	var publicOrigins []string
	var corsConfigured bool
	err = tx.QueryRow(ctx, `
		update _dbo.projects
		set public_cors_origins = nullif($2::text[], '{}'::text[]),
		    updated_at = now()
		where slug = $1 and disabled_at is null
		returning id, slug, name, schema_name, coalesce(public_cors_origins, '{}'::text[]), public_cors_origins is not null`,
		slug,
		origins,
	).Scan(&p.ID, &p.Slug, &p.Name, &p.SchemaName, &publicOrigins, &corsConfigured)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProjectNotFound
		}
		return nil, err
	}
	_, p.Roles = ProjectNames(p.Slug)
	if corsConfigured {
		p.CORS = ProjectCORSSettings{PublicOrigins: publicOrigins, Source: corsSourceDatabase, Wildcard: corsWildcard(publicOrigins)}
	}
	fillProjectCORS(&p, cfg)

	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "project.cors.update",
		TargetType: "project",
		TargetID:   p.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": p.Slug, "publicOriginCount": len(origins), "wildcard": corsWildcard(origins)},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &p, nil
}

func pgErrCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// pgErrMessage returns the bare Postgres message for errors that are safe to
// surface — rule and schema rejections the operator authored themselves. It
// never includes Detail or Hint, which can echo row values.
func pgErrMessage(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Message
	}
	return "rule was rejected by the database"
}
