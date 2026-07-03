package core

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// SeedAdmin creates the first admin from ADMIN_EMAIL/ADMIN_PASSWORD when the
// admins table is empty. This makes fresh deploys log-in-ready with zero manual
// steps and no interactive first-run wizard. If the vars are unset, the app
// instead exposes a self-closing POST /setup (added in a later milestone).
func SeedAdmin(ctx context.Context, pool *pgxpool.Pool, cfg *Config, log *slog.Logger) error {
	if cfg.AdminEmail == "" || cfg.AdminPassword == "" {
		return nil
	}

	var count int
	if err := pool.QueryRow(ctx, `select count(*) from _dbo.admins`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil // already seeded / has admins; never overwrite
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx,
		`insert into _dbo.admins (email, password_hash) values ($1, $2)`,
		cfg.AdminEmail, string(hash),
	); err != nil {
		return err
	}

	log.Info("seeded initial admin", "email", cfg.AdminEmail)
	return nil
}
