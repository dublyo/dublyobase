package core

import (
	"strings"
	"testing"
)

func TestAdminEmailAndPasswordValidation(t *testing.T) {
	if got := NormalizeEmail(" Admin@Example.COM "); got != "admin@example.com" {
		t.Fatalf("NormalizeEmail = %q", got)
	}
	if err := ValidateAdminEmail("admin@example.com"); err != nil {
		t.Fatalf("valid email rejected: %v", err)
	}
	if err := ValidateAdminEmail("not-email"); err == nil {
		t.Fatal("invalid email must be rejected")
	}
	if err := ValidateAdminPassword(strings.Repeat("x", minAdminPasswordSize-1)); err == nil {
		t.Fatal("short admin password must be rejected")
	}
	if err := ValidateAdminPassword(strings.Repeat("x", minAdminPasswordSize)); err != nil {
		t.Fatalf("minimum-length password rejected: %v", err)
	}
}

func TestAdminTokenGenerationAndHashing(t *testing.T) {
	token, err := GenerateAdminToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, adminTokenPrefix) {
		t.Fatalf("token prefix = %q", token)
	}
	hash := HashToken(token)
	if hash == token || strings.Contains(hash, token) {
		t.Fatal("token hash must not contain the plaintext token")
	}
	if len(hash) != 64 {
		t.Fatalf("sha256 hex length = %d", len(hash))
	}
}
