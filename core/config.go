package core

import (
	"fmt"
	"os"
	"strings"
)

// StorageType selects the file-storage backend.
type StorageType string

const (
	StorageLocal StorageType = "local"
	StorageS3    StorageType = "s3"
)

// Config is the full runtime configuration, loaded exclusively from environment
// variables. The variable names are a fixed contract with the Dublyo PaaS
// template — never rename them; new variables may only be added.
type Config struct {
	Host string // HOST (default 0.0.0.0)
	Port string // PORT (default 8080)

	DatabaseURL string // DATABASE_URL   (required)
	AppURL      string // APP_URL        (required) — drives all user-facing links
	JWTSecret   string // JWT_SECRET     (required, >=32 chars)

	AdminEmail    string // ADMIN_EMAIL    (optional; seeds first admin)
	AdminPassword string // ADMIN_PASSWORD (optional; paired with ADMIN_EMAIL)

	StorageType      StorageType // STORAGE_TYPE (local|s3)
	StorageLocalPath string      // STORAGE_LOCAL_PATH
	S3Endpoint       string      // S3_ENDPOINT
	S3Bucket         string      // S3_BUCKET
	S3AccessKey      string      // S3_ACCESS_KEY
	S3SecretKey      string      // S3_SECRET_KEY
	S3Region         string      // S3_REGION

	MigrateOnStart    bool     // MIGRATE_ON_START (default true)
	TrustProxyHeaders bool     // TRUST_PROXY_HEADERS (default true)
	CORSOrigins       []string // CORS_ORIGINS (comma-separated; default *)

	LogLevel  string // LOG_LEVEL  (debug|info|warn|error)
	LogFormat string // LOG_FORMAT (json|text)

	EnablePgvector bool // ENABLE_PGVECTOR (default false)

	SMTPHost     string // SMTP_HOST (optional; email flows skipped if unset)
	SMTPPort     string // SMTP_PORT (default 587)
	SMTPUser     string // SMTP_USER
	SMTPPassword string // SMTP_PASSWORD
	SMTPFrom     string // SMTP_FROM
}

// LoadConfig reads configuration from the environment and validates it.
// Any malformed value fails loud here — a misconfigured deploy must exit at
// boot with a clear message, not 500 mysteriously later.
func LoadConfig() (*Config, error) {
	var errs []string

	boolVar := func(key string, def bool) bool {
		v, err := envBool(key, def)
		if err != nil {
			errs = append(errs, err.Error())
		}
		return v
	}

	c := &Config{
		Host: env("HOST", "0.0.0.0"),
		Port: env("PORT", "8080"),

		DatabaseURL: os.Getenv("DATABASE_URL"),
		AppURL:      strings.TrimSpace(os.Getenv("APP_URL")),
		JWTSecret:   strings.TrimSpace(os.Getenv("JWT_SECRET")),

		AdminEmail:    strings.TrimSpace(os.Getenv("ADMIN_EMAIL")),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),

		StorageType:      StorageType(env("STORAGE_TYPE", "local")),
		StorageLocalPath: env("STORAGE_LOCAL_PATH", "/data/storage"),
		S3Endpoint:       os.Getenv("S3_ENDPOINT"),
		S3Bucket:         os.Getenv("S3_BUCKET"),
		S3AccessKey:      os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:      os.Getenv("S3_SECRET_KEY"),
		S3Region:         env("S3_REGION", "us-east-1"),

		MigrateOnStart:    boolVar("MIGRATE_ON_START", true),
		TrustProxyHeaders: boolVar("TRUST_PROXY_HEADERS", true),
		CORSOrigins:       splitCSV(env("CORS_ORIGINS", "*")),

		LogLevel:  env("LOG_LEVEL", "info"),
		LogFormat: env("LOG_FORMAT", "json"),

		EnablePgvector: boolVar("ENABLE_PGVECTOR", false),

		SMTPHost:     os.Getenv("SMTP_HOST"),
		SMTPPort:     env("SMTP_PORT", "587"),
		SMTPUser:     os.Getenv("SMTP_USER"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:     os.Getenv("SMTP_FROM"),
	}

	if err := c.Validate(); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return c, nil
}

// Validate enforces the required-variable contract.
func (c *Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.DatabaseURL) == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if c.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if c.AppURL == "" {
		missing = append(missing, "APP_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 characters (got %d)", len(c.JWTSecret))
	}
	if c.StorageType != StorageLocal && c.StorageType != StorageS3 {
		return fmt.Errorf("STORAGE_TYPE must be 'local' or 's3' (got %q)", c.StorageType)
	}
	if (c.AdminEmail == "") != (strings.TrimSpace(c.AdminPassword) == "") {
		return fmt.Errorf("ADMIN_EMAIL and ADMIN_PASSWORD must be set together")
	}
	if c.AdminPassword != "" && len(c.AdminPassword) < minAdminPasswordSize {
		return fmt.Errorf("ADMIN_PASSWORD must be at least %d characters", minAdminPasswordSize)
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error (got %q)", c.LogLevel)
	}
	switch strings.ToLower(c.LogFormat) {
	case "json", "text":
	default:
		return fmt.Errorf("LOG_FORMAT must be json or text (got %q)", c.LogFormat)
	}
	return nil
}

// Addr is the HOST:PORT bind address.
func (c *Config) Addr() string { return c.Host + ":" + c.Port }

// --- env helpers ---

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envBool parses a boolean env var strictly: unset → default, unparsable →
// error (a typo like MIGRATE_ON_START=flase must not silently become true).
func envBool(key string, def bool) (bool, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	switch strings.ToLower(v) {
	case "1", "t", "true", "yes", "on":
		return true, nil
	case "0", "f", "false", "no", "off":
		return false, nil
	}
	return def, fmt.Errorf("%s must be a boolean (true/false), got %q", key, v)
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
