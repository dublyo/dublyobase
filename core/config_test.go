package core

import (
	"strings"
	"testing"
)

// setRequired sets the three required vars to valid values.
func setRequired(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@db:5432/app")
	t.Setenv("APP_URL", "https://app.example.com")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
}

func TestLoadConfigRequiredVars(t *testing.T) {
	for _, missing := range []string{"DATABASE_URL", "APP_URL", "JWT_SECRET"} {
		t.Run("missing "+missing, func(t *testing.T) {
			setRequired(t)
			t.Setenv(missing, "")
			_, err := LoadConfig()
			if err == nil {
				t.Fatalf("expected error when %s missing", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Fatalf("error must name %s, got: %v", missing, err)
			}
		})
	}
}

func TestLoadConfigJWTSecretLength(t *testing.T) {
	setRequired(t)

	t.Setenv("JWT_SECRET", strings.Repeat("x", 31))
	if _, err := LoadConfig(); err == nil {
		t.Fatal("31-char JWT_SECRET must be rejected")
	}

	// 31 chars + trailing space must not pass the length check
	t.Setenv("JWT_SECRET", strings.Repeat("x", 31)+" ")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("31-char JWT_SECRET with trailing space must be rejected")
	}

	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
	if _, err := LoadConfig(); err != nil {
		t.Fatalf("32-char JWT_SECRET must pass, got: %v", err)
	}
}

func TestLoadConfigRejectsPlaceholderJWTSecret(t *testing.T) {
	setRequired(t)

	t.Setenv("JWT_SECRET", "change_me_to_a_32_plus_char_random_secret_value")
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("placeholder JWT_SECRET must be rejected, got: %v", err)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	setRequired(t)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != "8080" {
		t.Fatalf("default bind must be 0.0.0.0:8080, got %s", cfg.Addr())
	}
	if !cfg.MigrateOnStart || cfg.TrustProxyHeaders || cfg.EnablePgvector {
		t.Fatalf("bool defaults wrong: %+v", cfg)
	}
	if len(cfg.CORSOrigins) != 1 || cfg.CORSOrigins[0] != "*" {
		t.Fatalf("default CORS must be [*], got %v", cfg.CORSOrigins)
	}
	if cfg.StorageType != StorageLocal || cfg.StorageLocalPath != "/data/storage" {
		t.Fatalf("storage defaults wrong: %+v", cfg)
	}
	if cfg.BcryptCost != 10 || cfg.AuthDevTokens || cfg.MaxUploadMB != 64 {
		t.Fatalf("app auth defaults wrong: %+v", cfg)
	}
	if cfg.SMTPPort != "587" {
		t.Fatalf("smtp port default wrong: %q", cfg.SMTPPort)
	}
}

func TestLoadConfigAllowsFixedBootstrapAdmin(t *testing.T) {
	setRequired(t)
	t.Setenv("ADMIN_EMAIL", BootstrapAdminEmail)
	t.Setenv("ADMIN_PASSWORD", BootstrapAdminPassword)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("bootstrap admin config should be valid: %v", err)
	}
	if !IsBootstrapAdminCredential(cfg.AdminEmail, cfg.AdminPassword) {
		t.Fatalf("unexpected bootstrap config: %q", cfg.AdminEmail)
	}
}

func TestLoadConfigInvalidValuesFailLoud(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "boolean typo",
			env:  map[string]string{"MIGRATE_ON_START": "flase"},
			want: "MIGRATE_ON_START",
		},
		{
			name: "storage type",
			env:  map[string]string{"STORAGE_TYPE": "ftp"},
			want: "STORAGE_TYPE",
		},
		{
			name: "log level",
			env:  map[string]string{"LOG_LEVEL": "verbose"},
			want: "LOG_LEVEL",
		},
		{
			name: "log format",
			env:  map[string]string{"LOG_FORMAT": "pretty"},
			want: "LOG_FORMAT",
		},
		{
			name: "admin email without password",
			env:  map[string]string{"ADMIN_EMAIL": "admin@example.com"},
			want: "ADMIN_EMAIL",
		},
		{
			name: "admin password without email",
			env:  map[string]string{"ADMIN_PASSWORD": "secret"},
			want: "ADMIN_EMAIL",
		},
		{
			name: "admin password too short",
			env: map[string]string{
				"ADMIN_EMAIL":    "admin@example.com",
				"ADMIN_PASSWORD": "short",
			},
			want: "ADMIN_PASSWORD",
		},
		{
			name: "bcrypt cost low",
			env:  map[string]string{"BCRYPT_COST": "3"},
			want: "BCRYPT_COST",
		},
		{
			name: "bcrypt cost typo",
			env:  map[string]string{"BCRYPT_COST": "fast"},
			want: "BCRYPT_COST",
		},
		{
			name: "auth dev tokens typo",
			env:  map[string]string{"AUTH_DEV_TOKENS": "sure"},
			want: "AUTH_DEV_TOKENS",
		},
		{
			name: "max upload low",
			env:  map[string]string{"MAX_UPLOAD_MB": "0"},
			want: "MAX_UPLOAD_MB",
		},
		{
			name: "max upload high",
			env:  map[string]string{"MAX_UPLOAD_MB": "2048"},
			want: "MAX_UPLOAD_MB",
		},
		{
			name: "max upload typo",
			env:  map[string]string{"MAX_UPLOAD_MB": "large"},
			want: "MAX_UPLOAD_MB",
		},
		{
			name: "smtp host without from",
			env:  map[string]string{"SMTP_HOST": "smtp.example.com"},
			want: "SMTP_FROM",
		},
		{
			name: "smtp bad port",
			env: map[string]string{
				"SMTP_HOST": "smtp.example.com",
				"SMTP_FROM": "no-reply@example.com",
				"SMTP_PORT": "big",
			},
			want: "SMTP_PORT",
		},
		{
			name: "smtp bad from",
			env: map[string]string{
				"SMTP_HOST": "smtp.example.com",
				"SMTP_FROM": "not-email",
			},
			want: "SMTP_FROM",
		},
		{
			name: "smtp user without password",
			env: map[string]string{
				"SMTP_HOST": "smtp.example.com",
				"SMTP_FROM": "no-reply@example.com",
				"SMTP_USER": "mailer",
			},
			want: "SMTP_USER",
		},
		{
			name: "smtp local host",
			env: map[string]string{
				"SMTP_HOST": "127.0.0.1",
				"SMTP_FROM": "no-reply@example.com",
			},
			want: "SMTP_HOST",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("invalid value must fail loud naming %s, got: %v", tc.want, err)
			}
		})
	}
}

func TestEnvBoolForms(t *testing.T) {
	for _, v := range []string{"1", "t", "true", "YES", "on"} {
		t.Setenv("X_BOOL", v)
		got, err := envBool("X_BOOL", false)
		if err != nil || !got {
			t.Fatalf("%q must parse true, got %v err=%v", v, got, err)
		}
	}
	for _, v := range []string{"0", "f", "FALSE", "no", "off"} {
		t.Setenv("X_BOOL", v)
		got, err := envBool("X_BOOL", true)
		if err != nil || got {
			t.Fatalf("%q must parse false, got %v err=%v", v, got, err)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" https://a.com , ,https://b.com,")
	if len(got) != 2 || got[0] != "https://a.com" || got[1] != "https://b.com" {
		t.Fatalf("splitCSV wrong: %v", got)
	}
}
