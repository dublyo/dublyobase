package core

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// StorageType selects the file-storage backend.
type StorageType string

const (
	StorageLocal StorageType = "local"
	StorageS3    StorageType = "s3"
)

var defaultTrustedProxyCIDRs = []string{
	"127.0.0.1/32",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"100.64.0.0/10",
}

func DefaultTrustedProxyCIDRs() []string {
	out := make([]string, len(defaultTrustedProxyCIDRs))
	copy(out, defaultTrustedProxyCIDRs)
	return out
}

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

	BcryptCost    int  // BCRYPT_COST     (default 10)
	AuthDevTokens bool // AUTH_DEV_TOKENS (default false; exposes reset/verify tokens for tests/dev)
	MaxUploadMB   int  // MAX_UPLOAD_MB   (default 64)

	StorageType      StorageType // STORAGE_TYPE (local|s3)
	StorageLocalPath string      // STORAGE_LOCAL_PATH
	S3Endpoint       string      // S3_ENDPOINT
	S3Bucket         string      // S3_BUCKET
	S3AccessKey      string      // S3_ACCESS_KEY
	S3SecretKey      string      // S3_SECRET_KEY
	S3Region         string      // S3_REGION
	S3Prefix         string      // S3_PREFIX
	S3UseSSL         bool        // S3_USE_SSL (default true)
	S3ForcePathStyle bool        // S3_FORCE_PATH_STYLE (default true)

	MigrateOnStart        bool     // MIGRATE_ON_START (default true)
	TrustProxyHeaders     bool     // TRUST_PROXY_HEADERS (default false)
	TrustedProxyCIDRs     []string // TRUSTED_PROXY_CIDRS (comma-separated; default private Docker/LAN ranges)
	CORSOrigins           []string // CORS_ORIGINS (comma-separated; default APP_URL origin)
	CORSOriginsConfigured bool     // true when CORS_ORIGINS was explicitly provided

	LogLevel  string // LOG_LEVEL  (debug|info|warn|error)
	LogFormat string // LOG_FORMAT (json|text)

	EnablePgvector bool // ENABLE_PGVECTOR (default false)

	// CRON_ALLOW_PRIVATE_TARGETS (default false). Cron jobs may otherwise not
	// reach private or loopback addresses, which stops a job URL being used to
	// read cloud metadata or a service bound to localhost. Self-hosters who
	// deliberately cron an internal service turn this on at deploy time — it is
	// not settable through the API, so an MCP token cannot enable it.
	CronAllowPrivateTargets bool

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
	intVar := func(key string, def int) int {
		v, err := envInt(key, def)
		if err != nil {
			errs = append(errs, err.Error())
		}
		return v
	}

	appURL := strings.TrimSpace(os.Getenv("APP_URL"))
	corsRaw, corsConfigured := os.LookupEnv("CORS_ORIGINS")
	corsOrigins := splitCSV(corsRaw)
	if !corsConfigured || len(corsOrigins) == 0 {
		corsOrigins = DefaultCORSOrigins(appURL)
		corsConfigured = false
	}

	c := &Config{
		Host: env("HOST", "0.0.0.0"),
		Port: env("PORT", "8080"),

		DatabaseURL: os.Getenv("DATABASE_URL"),
		AppURL:      appURL,
		JWTSecret:   strings.TrimSpace(os.Getenv("JWT_SECRET")),

		AdminEmail:    strings.TrimSpace(os.Getenv("ADMIN_EMAIL")),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),

		BcryptCost:    intVar("BCRYPT_COST", bcrypt.DefaultCost),
		AuthDevTokens: boolVar("AUTH_DEV_TOKENS", false),
		MaxUploadMB:   intVar("MAX_UPLOAD_MB", 64),

		StorageType:      StorageType(env("STORAGE_TYPE", "local")),
		StorageLocalPath: env("STORAGE_LOCAL_PATH", "/data/storage"),
		S3Endpoint:       os.Getenv("S3_ENDPOINT"),
		S3Bucket:         os.Getenv("S3_BUCKET"),
		S3AccessKey:      os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:      os.Getenv("S3_SECRET_KEY"),
		S3Region:         env("S3_REGION", "us-east-1"),
		S3Prefix:         strings.Trim(strings.TrimSpace(os.Getenv("S3_PREFIX")), "/"),
		S3UseSSL:         boolVar("S3_USE_SSL", true),
		S3ForcePathStyle: boolVar("S3_FORCE_PATH_STYLE", true),

		MigrateOnStart:        boolVar("MIGRATE_ON_START", true),
		TrustProxyHeaders:     boolVar("TRUST_PROXY_HEADERS", false),
		TrustedProxyCIDRs:     splitCSV(env("TRUSTED_PROXY_CIDRS", strings.Join(defaultTrustedProxyCIDRs, ","))),
		CORSOrigins:           corsOrigins,
		CORSOriginsConfigured: corsConfigured,

		LogLevel:  env("LOG_LEVEL", "info"),
		LogFormat: env("LOG_FORMAT", "json"),

		EnablePgvector:          boolVar("ENABLE_PGVECTOR", false),
		CronAllowPrivateTargets: boolVar("CRON_ALLOW_PRIVATE_TARGETS", false),

		SMTPHost:     strings.TrimSpace(os.Getenv("SMTP_HOST")),
		SMTPPort:     env("SMTP_PORT", "587"),
		SMTPUser:     strings.TrimSpace(os.Getenv("SMTP_USER")),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:     strings.TrimSpace(os.Getenv("SMTP_FROM")),
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
	if isPlaceholderSecret(c.JWTSecret) {
		return fmt.Errorf("JWT_SECRET must be replaced with a random secret")
	}
	if c.StorageType != StorageLocal && c.StorageType != StorageS3 {
		return fmt.Errorf("STORAGE_TYPE must be 'local' or 's3' (got %q)", c.StorageType)
	}
	if c.StorageType == StorageS3 {
		if strings.TrimSpace(c.S3Endpoint) == "" {
			return fmt.Errorf("S3_ENDPOINT is required when STORAGE_TYPE=s3")
		}
		if _, _, err := normalizeS3Endpoint(c.S3Endpoint, c.S3UseSSL); err != nil {
			return err
		}
		if strings.TrimSpace(c.S3Bucket) == "" {
			return fmt.Errorf("S3_BUCKET is required when STORAGE_TYPE=s3")
		}
		if strings.TrimSpace(c.S3AccessKey) == "" || c.S3SecretKey == "" {
			return fmt.Errorf("S3_ACCESS_KEY and S3_SECRET_KEY are required when STORAGE_TYPE=s3")
		}
		for _, segment := range splitCSV(strings.ReplaceAll(c.S3Prefix, "/", ",")) {
			if !safeStorageSegment(segment) {
				return fmt.Errorf("S3_PREFIX contains an invalid path segment")
			}
		}
	}
	if (c.AdminEmail == "") != (strings.TrimSpace(c.AdminPassword) == "") {
		return fmt.Errorf("ADMIN_EMAIL and ADMIN_PASSWORD must be set together")
	}
	if c.AdminPassword != "" && len(c.AdminPassword) < minAdminPasswordSize {
		return fmt.Errorf("ADMIN_PASSWORD must be at least %d characters", minAdminPasswordSize)
	}
	if c.BcryptCost < bcrypt.MinCost || c.BcryptCost > bcrypt.MaxCost {
		return fmt.Errorf("BCRYPT_COST must be between %d and %d (got %d)", bcrypt.MinCost, bcrypt.MaxCost, c.BcryptCost)
	}
	if c.MaxUploadMB < 1 || c.MaxUploadMB > 1024 {
		return fmt.Errorf("MAX_UPLOAD_MB must be between 1 and 1024 (got %d)", c.MaxUploadMB)
	}
	for _, cidr := range c.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("TRUSTED_PROXY_CIDRS contains invalid CIDR %q", cidr)
		}
	}
	if _, err := normalizeCORSOrigins(c.CORSOrigins, true); err != nil {
		return fmt.Errorf("CORS_ORIGINS %w", err)
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
	if err := validateSMTPConfig(c); err != nil {
		return err
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

func isPlaceholderSecret(secret string) bool {
	normalized := strings.ToLower(strings.TrimSpace(secret))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	return normalized == "change_me_to_a_32_plus_char_random_secret_value" ||
		strings.HasPrefix(normalized, "change_me") ||
		strings.HasPrefix(normalized, "changeme") ||
		strings.Contains(normalized, "replace_me") ||
		strings.Contains(normalized, "placeholder")
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

func envInt(key string, def int) (int, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def, fmt.Errorf("%s must be an integer, got %q", key, v)
	}
	return n, nil
}

func originFromURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("%w: origin is invalid", ErrValidation)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return "", fmt.Errorf("%w: origin scheme must be http or https", ErrValidation)
	}
	return u.Scheme + "://" + u.Host, nil
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
