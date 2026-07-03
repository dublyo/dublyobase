package core

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedAdmin creates the first admin from ADMIN_EMAIL/ADMIN_PASSWORD when the
// admins table is empty. This makes fresh deploys log-in-ready with zero manual
// steps and no interactive first-run wizard. If the vars are unset, the app
// instead exposes a self-closing POST /setup.
//
// The same core path backs POST /setup, so env seeding inherits email/password
// validation, advisory locking and audit logging.
func SeedAdmin(ctx context.Context, pool *pgxpool.Pool, cfg *Config, log *slog.Logger) error {
	if cfg.AdminEmail == "" || cfg.AdminPassword == "" {
		return nil
	}

	admin, err := CreateFirstAdmin(ctx, pool, cfg.AdminEmail, cfg.AdminPassword, "", "")
	if err != nil {
		if errors.Is(err, ErrSetupClosed) {
			return nil // already seeded / has admins; never overwrite
		}
		return err
	}
	log.Info("seeded initial admin", "email", admin.Email)
	return nil
}
