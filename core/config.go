package core

import (
	"fmt"
	"os"
	"strconv"
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
// template — do not rename them without updating the template.
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
func LoadConfig() (*Config, error) {
	c := &Config{
		Host: env("HOST", "0.0.0.0"),
		Port: env("PORT", "8080"),

		DatabaseURL: os.Getenv("DATABASE_URL"),
		AppURL:      os.Getenv("APP_URL"),
		JWTSecret:   os.Getenv("JWT_SECRET"),

		AdminEmail:    os.Getenv("ADMIN_EMAIL"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),

		StorageType:      StorageType(env("STORAGE_TYPE", "local")),
		StorageLocalPath: env("STORAGE_LOCAL_PATH", "/data/storage"),
		S3Endpoint:       os.Getenv("S3_ENDPOINT"),
		S3Bucket:         os.Getenv("S3_BUCKET"),
		S3AccessKey:      os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:      os.Getenv("S3_SECRET_KEY"),
		S3Region:         env("S3_REGION", "us-east-1"),

		MigrateOnStart:    envBool("MIGRATE_ON_START", true),
		TrustProxyHeaders: envBool("TRUST_PROXY_HEADERS", true),
		CORSOrigins:       splitCSV(env("CORS_ORIGINS", "*")),

		LogLevel:  env("LOG_LEVEL", "info"),
		LogFormat: env("LOG_FORMAT", "json"),

		EnablePgvector: envBool("ENABLE_PGVECTOR", false),

		SMTPHost:     os.Getenv("SMTP_HOST"),
		SMTPPort:     env("SMTP_PORT", "587"),
		SMTPUser:     os.Getenv("SMTP_USER"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:     os.Getenv("SMTP_FROM"),
	}
	return c, c.Validate()
}

// Validate enforces the required-variable contract. It fails loud so a
// misconfigured deploy exits immediately instead of 500-ing mysteriously.
func (c *Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.DatabaseURL) == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if strings.TrimSpace(c.JWTSecret) == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if strings.TrimSpace(c.AppURL) == "" {
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

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		// accept yes/no/on/off in addition to strconv's set
		switch strings.ToLower(v) {
		case "yes", "on":
			return true
		case "no", "off":
			return false
		}
		return def
	}
	return b
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
