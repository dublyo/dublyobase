package core

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrateLockID is the pg_advisory_lock key serializing boot-time migration
// across replicas ("dbo" spelled on a phone keypad, if you must know).
const migrateLockID int64 = 326_326_001

// Migrate applies any embedded migrations not yet recorded, each in its own
// transaction, in filename order. It is idempotent and safe to run on every
// boot — which is exactly what MIGRATE_ON_START does, so a fresh deploy never
// requires an operator to exec in and run migrations by hand.
//
// The whole run is serialized with a Postgres advisory lock so that several
// replicas booting simultaneously (scale-out, rolling deploys) don't race the
// DDL: latecomers block on the lock, then see every migration already recorded
// and apply nothing.
//
// Migrations must NOT `CREATE DATABASE` (the DB must already exist) and should
// avoid `CREATE EXTENSION` unless the app role is a superuser.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	// The advisory lock is session-scoped, so hold one dedicated connection
	// for the duration of the run.
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer lockConn.Release()

	if _, err := lockConn.Exec(ctx, `select pg_advisory_lock($1)`, migrateLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = lockConn.Exec(context.WithoutCancel(ctx), `select pg_advisory_unlock($1)`, migrateLockID)
	}()

	if _, err := lockConn.Exec(ctx, `
		create schema if not exists _dbo;
		create table if not exists _dbo.schema_migrations (
			version    text primary key,
			applied_at timestamptz not null default now()
		);`); err != nil {
		return fmt.Errorf("init migration tracking: %w", err)
	}

	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)

	applied := 0
	for _, file := range files {
		version := path.Base(file)

		var exists bool
		if err := lockConn.QueryRow(ctx,
			`select exists(select 1 from _dbo.schema_migrations where version = $1)`,
			version,
		).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if exists {
			continue
		}

		body, err := migrationsFS.ReadFile(file)
		if err != nil {
			return err
		}

		tx, err := lockConn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if _, err := tx.Exec(ctx,
			`insert into _dbo.schema_migrations (version) values ($1)`, version,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}

		log.Info("applied migration", "version", version)
		applied++
	}

	log.Info("migrations up to date", "applied", applied, "total", len(files))
	return nil
}
