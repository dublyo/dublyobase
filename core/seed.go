package core

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedAdmin creates the first admin when the admins table is empty. Explicit
// ADMIN_EMAIL/ADMIN_PASSWORD values still work for controlled deploys; otherwise
// fresh installs get the fixed developer bootstrap admin and must rotate it on
// first login before any admin API is available.
//
// The same core path backs POST /setup, so env seeding inherits email/password
// validation, advisory locking and audit logging.
func SeedAdmin(ctx context.Context, pool *pgxpool.Pool, cfg *Config, log *slog.Logger) error {
	var (
		admin *Admin
		err   error
	)
	if cfg.AdminEmail == "" && cfg.AdminPassword == "" {
		admin, err = CreateBootstrapAdmin(ctx, pool, cfg.BcryptCost, "", "")
	} else {
		mustChange := IsBootstrapAdminCredential(cfg.AdminEmail, cfg.AdminPassword)
		admin, err = createFirstAdminWithOptions(ctx, pool, cfg.AdminEmail, cfg.AdminPassword, cfg.BcryptCost, mustChange, mustChange, "", "")
	}

	if err != nil {
		if errors.Is(err, ErrSetupClosed) {
			return nil // already seeded / has admins; never overwrite
		}
		return err
	}
	log.Info("seeded initial admin", "email", admin.Email, "must_change_password", admin.MustChangePassword)
	return nil
}
