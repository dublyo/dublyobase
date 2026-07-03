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
// depends_on races, cold PaaS boots), so transient failures are waited out.
// Permanent configuration mistakes (an unparsable URL) fail immediately —
// retrying those for 60s would only delay the loud error the operator needs.
func Connect(ctx context.Context, cfg *Config, log *slog.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		// pgconn redacts the connection string in parse errors
		return nil, fmt.Errorf("invalid DATABASE_URL: %w", err)
	}

	deadline := time.Now().Add(60 * time.Second)

	var lastErr error
	for attempt := 1; ; attempt++ {
		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
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

		if ctx.Err() != nil {
			return nil, ctx.Err() // shutdown requested while waiting
		}
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
