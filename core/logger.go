package core

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger builds a stdout slog logger honoring LOG_LEVEL and LOG_FORMAT.
// Containers are ephemeral, so we always log to stdout — never to files.
func NewLogger(cfg *Config) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.ToLower(cfg.LogFormat) == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

// RedactURL strips the password from a database URL for safe logging.
func RedactURL(dsn string) string {
	// postgres://user:pass@host:port/db  ->  postgres://user:***@host:port/db
	at := strings.LastIndex(dsn, "@")
	scheme := strings.Index(dsn, "://")
	if at == -1 || scheme == -1 {
		return dsn
	}
	creds := dsn[scheme+3 : at]
	if colon := strings.Index(creds, ":"); colon != -1 {
		user := creds[:colon]
		return dsn[:scheme+3] + user + ":***" + dsn[at:]
	}
	return dsn
}
