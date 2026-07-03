package core

import (
	"strings"
	"testing"
	"time"
)

func TestAppTokenGenerationAndClaims(t *testing.T) {
	secret := strings.Repeat("s", 32)
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	token, expiresAt, err := GenerateAppAccessToken(secret, "demo", "9c10d5b9-3a23-4f25-91c3-09a40d7e9f7e", "token-key", now)
	if err != nil {
		t.Fatal(err)
	}
	if !expiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("expiresAt = %s", expiresAt)
	}
	claims, err := parseAppJWT(secret, token, now)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Project != "demo" || claims.Role != "authenticated" || claims.Collection != "users" || claims.TokenKey != "token-key" {
		t.Fatalf("claims = %+v", claims)
	}
	if _, err := parseAppJWT(secret, token, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired token must be rejected")
	}
}

func TestAppOpaqueTokens(t *testing.T) {
	refresh, err := GenerateAppRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(refresh, "dbo_refresh_") {
		t.Fatalf("refresh token prefix = %q", refresh)
	}
	if HashToken(refresh) == refresh {
		t.Fatal("token hash must not equal plaintext")
	}
	verify, err := GenerateEmailVerificationToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(verify, "dbo_verify_") {
		t.Fatalf("verify token prefix = %q", verify)
	}
	reset, err := GeneratePasswordResetToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(reset, "dbo_reset_") {
		t.Fatalf("reset token prefix = %q", reset)
	}
}

func TestAppUserValidation(t *testing.T) {
	email, err := normalizeAppEmail("User@Example.COM")
	if err != nil {
		t.Fatal(err)
	}
	if email != "user@example.com" {
		t.Fatalf("email = %q", email)
	}
	if err := ValidateAppUserPassword("short"); err == nil {
		t.Fatal("short password must be rejected")
	}
}
