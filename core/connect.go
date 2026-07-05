package core

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pgx pool to DATABASE_URL, retrying with backoff for up to 60s.
// Postgres is frequently not ready when the app container starts (compose
// depends_on races, cold PaaS boots), so transient failures are waited out.
// Permanent configuration mistakes (an unparsable URL) fail immediately —
// retrying those for 60s would only delay the loud error the operator needs.
//
// SSL escalation: some Postgres deployments — Dublyo's ghcr.io/dublyo/postgres
// image is one — reject every non-SSL TCP connection via pg_hba.conf, even from
// containers on the same Docker bridge. Users who paste a `sslmode=disable`
// URL from a copy-paste UI (Dublyo's Internal-connection tab used to output
// exactly that) then wait 60 seconds and get a 502. We detect the pg_hba
// "no encryption" signal on the first failure and transparently retry with
// `sslmode=require`. Cheap, deterministic, only takes effect when the server
// itself asks for encryption — so it never overrides an operator who wants
// verify-ca / verify-full.
func Connect(ctx context.Context, cfg *Config, log *slog.Logger) (*pgxpool.Pool, error) {
	rawURL := cfg.DatabaseURL
	poolCfg, err := pgxpool.ParseConfig(rawURL)
	if err != nil {
		// pgconn redacts the connection string in parse errors
		return nil, fmt.Errorf("invalid DATABASE_URL: %w", err)
	}

	deadline := time.Now().Add(60 * time.Second)
	upgradedSSL := false

	var lastErr error
	for attempt := 1; ; attempt++ {
		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err = pool.Ping(pingCtx)
			cancel()
			if err == nil {
				log.Info("connected to database", "url", RedactURL(rawURL))
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err

		if !upgradedSSL && isSSLRequiredError(err) {
			if upgraded, ok := upgradeSSLMode(rawURL); ok {
				if newCfg, perr := pgxpool.ParseConfig(upgraded); perr == nil {
					log.Warn("server requires SSL — upgrading DATABASE_URL sslmode to require",
						"reason", err.Error())
					rawURL = upgraded
					poolCfg = newCfg
					upgradedSSL = true
					continue // retry immediately with the fixed URL
				}
			}
		}

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

// isSSLRequiredError detects the Postgres pg_hba "no encryption" signal that
// hostssl-only servers return when a client connects without TLS. Postgres
// wraps this as SQLSTATE 28000 with the string "no encryption" in the
// message; matching the substring keeps us decoupled from the exact pgconn
// error type layout across driver versions.
func isSSLRequiredError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no encryption")
}

// upgradeSSLMode rewrites a Postgres URL's sslmode from disable/allow/prefer
// to require. Returns (newURL, true) if a rewrite happened, (original, false)
// otherwise. Anything stricter than require (verify-ca / verify-full) is left
// alone — the operator picked something specific and we won't downgrade.
func upgradeSSLMode(raw string) (string, bool) {
	if raw == "" {
		return raw, false
	}
	// URL form (postgres://…?sslmode=…) is the shape dublyobase templates + the
	// pgx driver both use. The keyword=value DSN form is theoretically supported
	// by pgx but not by our templates; skip the ambiguity there.
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return raw, false
	}
	q := u.Query()
	current := strings.ToLower(strings.TrimSpace(q.Get("sslmode")))
	switch current {
	case "", "disable", "allow", "prefer":
		q.Set("sslmode", "require")
		u.RawQuery = q.Encode()
		return u.String(), true
	default:
		return raw, false
	}
}
