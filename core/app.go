package core

import (
	"log/slog"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
)

// App is the central object wired through the CLI and HTTP handlers. It holds
// the external Postgres pool, the loaded config and the logger. dublyobase is a
// single stateless process: it connects to a Postgres provided via DATABASE_URL
// and never manages a database server itself.
type App struct {
	Config *Config
	Pool   *pgxpool.Pool
	Log    *slog.Logger
	Mailer Mailer // optional test override; nil uses runtime SMTP settings

	ready atomic.Bool
}

// NewApp constructs an App from an already-connected pool.
func NewApp(cfg *Config, pool *pgxpool.Pool, log *slog.Logger) *App {
	return &App{Config: cfg, Pool: pool, Log: log}
}

// SetReady marks the app ready to serve traffic (after migrations complete).
func (a *App) SetReady(v bool) { a.ready.Store(v) }

// Ready reports whether startup (incl. migrations) has finished.
func (a *App) Ready() bool { return a.ready.Load() }
