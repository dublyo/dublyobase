package core

import (
	"log/slog"
	"net/url"
	"os"
	"regexp"
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

var (
	dsnQuotedPasswordRe = regexp.MustCompile(`(?i)((?:ssl)?password)\s*=\s*'([^'\\]|\\.)*'`)
	dsnPasswordRe       = regexp.MustCompile(`(?i)((?:ssl)?password)\s*=\s*\S+`)
)

// RedactURL removes credentials from a connection string before logging.
// It handles URL userinfo passwords, ?password=/&sslpassword= query params,
// and key/value DSN form ("host=db password=x"). When in doubt it redacts.
func RedactURL(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" && u.Host != "" {
		if u.User != nil {
			if _, has := u.User.Password(); has {
				u.User = url.UserPassword(u.User.Username(), "xxxxx")
			}
		}
		q := u.Query()
		changed := false
		for k := range q {
			if strings.EqualFold(k, "password") || strings.EqualFold(k, "sslpassword") {
				q.Set(k, "xxxxx")
				changed = true
			}
		}
		if changed {
			u.RawQuery = q.Encode()
		}
		s := u.String()
		// url.UserPassword escapes; make the placeholder predictable
		return strings.Replace(s, ":xxxxx@", ":***@", 1)
	}
	// key/value DSN form (or unparsable): redact quoted values before the
	// whitespace-delimited fallback, otherwise "password='a b'" leaks "b'".
	redacted := dsnQuotedPasswordRe.ReplaceAllString(dsn, "$1=***")
	return dsnPasswordRe.ReplaceAllString(redacted, "$1=***")
}
