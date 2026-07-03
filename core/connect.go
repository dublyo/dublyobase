package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pgx pool to DATABASE_URL, retrying with backoff for up to 60s.
// Postgres is frequently not ready when the app container starts (compose
// depends_on races, cold PaaS boots), so we never exit on a transient failure —
// we wait it out, then fail loud if the database is truly unreachable.
func Connect(ctx context.Context, cfg *Config, log *slog.Logger) (*pgxpool.Pool, error) {
	deadline := time.Now().Add(60 * time.Second)

	var lastErr error
	for attempt := 1; ; attempt++ {
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				log.Info("connected to database", "url", RedactURL(cfg.DatabaseURL))
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("database unreachable after 60s: %w", lastErr)
		}

		wait := time.Duration(attempt) * time.Second
		if wait > 8*time.Second {
			wait = 8 * time.Second
		}
		log.Warn("waiting for database", "attempt", attempt, "retry_in", wait.String(), "err", err)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}
