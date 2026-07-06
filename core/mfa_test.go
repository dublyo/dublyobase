package core

import (
	"testing"
	"time"
)

func TestVerifyTOTP(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_720_000_000, 0)
	code := totpCode(secret, now.Unix()/30)
	if code == "" {
		t.Fatal("expected generated TOTP code")
	}
	if !verifyTOTP(secret, code, now) {
		t.Fatal("expected current TOTP code to verify")
	}
	if verifyTOTP(secret, "000000", now) {
		t.Fatal("expected wrong TOTP code to fail")
	}
}

func TestRecoveryCodeNormalization(t *testing.T) {
	if normalizeRecoveryCode("ABCD EFGH-IJKL") != "abcdefgh-ijkl" {
		t.Fatal("expected recovery code normalization to trim spaces and lowercase")
	}
}
