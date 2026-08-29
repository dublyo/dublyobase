package core

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReconcileProjectSecurity recreates the database roles and row-level security
// policies that every project depends on, for projects where they are missing.
//
// This exists because of what a restored backup looks like. pg_dump writes
// database objects, but roles live in the cluster, not the database, so they are
// never in the archive. Restoring into a fresh cluster therefore fails to create
// every policy that names a role — pg_restore reports those as errors it
// "ignored" and still exits 0. The result is a database holding all its rows
// with none of its security, and nothing previously noticed: roles were created
// once when a project was created and never checked again.
//
// Row-level security stays enabled on the tables, so the failure is closed
// rather than open — queries return nothing instead of everything. That is the
// right direction, but it leaves the instance unusable, which is why this runs
// at startup and repairs it.
//
// It is idempotent: projects whose roles already exist are left untouched.
func ReconcileProjectSecurity(ctx context.Context, pool *pgxpool.Pool, log Logger) error {
	projects, err := ListProjects(ctx, pool)
	if err != nil {
		return err
	}
	// A restore run with --no-privileges strips grants as well as roles, which
	// takes PUBLIC's usage on _dbo with it. Every policy calls a function in
	// there, so without this the policies exist and then deny everything with
	// "permission denied for schema _dbo". These mirror the grants the
	// migrations make and are safe to repeat.
	if err := ensureSharedGrants(ctx, pool); err != nil {
		return err
	}
	for i := range projects {
		project := &projects[i]
		missing, err := projectRolesMissing(ctx, pool, project)
		if err != nil {
			return err
		}
		if !missing {
			continue
		}
		if log != nil {
			log.Warn("project security missing, rebuilding", "project", project.Slug)
		}
		if err := rebuildProjectSecurity(ctx, pool, project, log); err != nil {
			return fmt.Errorf("rebuild %s: %w", project.Slug, err)
		}
		if log != nil {
			log.Info("project security rebuilt", "project", project.Slug)
		}
	}
	return nil
}

func projectRolesMissing(ctx context.Context, pool *pgxpool.Pool, project *Project) (bool, error) {
	_, roles := ProjectNames(project.Slug)
	var present int
	err := pool.QueryRow(ctx,
		`select count(*) from pg_roles where rolname = any($1)`,
		[]string{roles.Anon, roles.Authenticated, roles.Service},
	).Scan(&present)
	if err != nil {
		return false, err
	}
	return present < 3, nil
}

func rebuildProjectSecurity(ctx context.Context, pool *pgxpool.Pool, project *Project, log Logger) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := ensureProjectRoles(ctx, tx, project); err != nil {
		return err
	}

	collections, err := projectCollectionsTx(ctx, tx, project.ID)
	if err != nil {
		return err
	}
	for i := range collections {
		collection := &collections[i]
		// An imported table keeps whatever security its own schema defines;
		// Dublyobase does not own it and must not write policies onto it.
		if collectionIsImported(collection) {
			continue
		}
		if err := syncCollectionPolicies(ctx, tx, project, collection); err != nil {
			if log != nil {
				log.Warn("could not rebuild policies", "project", project.Slug, "collection", collection.Name, "err", err)
			}
			continue
		}
	}
	return tx.Commit(ctx)
}

func ensureProjectRoles(ctx context.Context, tx pgx.Tx, project *Project) error {
	appRole, roles := ProjectNames(project.Slug)
	_ = appRole
	schemaIdent := quoteIdent(project.SchemaName)
	for _, role := range []string{roles.Anon, roles.Authenticated, roles.Service} {
		ident := quoteIdent(role)
		// `create role` has no if-not-exists, so guard it in plpgsql.
		stmt := fmt.Sprintf(`do $$ begin
			if not exists (select 1 from pg_roles where rolname = %s) then
				execute 'create role %s nologin';
			end if;
		end $$`, quoteLiteral(role), ident)
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	current, err := currentDatabaseUser(ctx, tx)
	if err != nil {
		return err
	}
	grants := []string{
		fmt.Sprintf(`grant %s, %s, %s to %s`, quoteIdent(roles.Anon), quoteIdent(roles.Authenticated), quoteIdent(roles.Service), quoteIdent(current)),
		fmt.Sprintf(`grant usage on schema %s to %s, %s, %s`, schemaIdent, quoteIdent(roles.Anon), quoteIdent(roles.Authenticated), quoteIdent(roles.Service)),
	}
	for _, stmt := range grants {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func currentDatabaseUser(ctx context.Context, tx pgx.Tx) (string, error) {
	var name string
	if err := tx.QueryRow(ctx, `select current_user`).Scan(&name); err != nil {
		return "", err
	}
	return name, nil
}

func projectCollectionsTx(ctx context.Context, tx pgx.Tx, projectID string) ([]Collection, error) {
	rows, err := tx.Query(ctx, `
		select id, project_id, name, type, system, fields, list_rule, view_rule, create_rule, update_rule, delete_rule, options
		from _dbo.collections
		where project_id = $1
		order by created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Collection, 0)
	for rows.Next() {
		collection, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *collection)
	}
	return out, rows.Err()
}

// ensureSharedGrants re-applies the instance-wide grants from the migrations.
// Re-running a grant that is already in place is a no-op, so this is safe on
// every boot rather than only after a restore.
func ensureSharedGrants(ctx context.Context, pool *pgxpool.Pool) error {
	statements := []string{
		`grant usage on schema _dbo to public`,
		`grant execute on all functions in schema _dbo to public`,
	}
	for _, stmt := range statements {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	return nil
}
