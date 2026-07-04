package core

import "testing"

func TestSettingsSecretEncryptionRoundTrip(t *testing.T) {
	master := "settings-secret-master-must-be-at-least-32-bytes"
	ciphertext, err := encryptSecret(master, "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if ciphertext == "secret-value" || len(ciphertext) < 10 {
		t.Fatalf("secret was not encrypted: %q", ciphertext)
	}
	plain, err := decryptSecret(master, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "secret-value" {
		t.Fatalf("decrypt = %q", plain)
	}
	if _, err := decryptSecret(master+"changed", ciphertext); err == nil {
		t.Fatal("decrypt with a different master key must fail")
	}
}

func TestNormalizeS3Endpoint(t *testing.T) {
	endpoint, useSSL, err := normalizeS3Endpoint("https://abc.r2.cloudflarestorage.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "abc.r2.cloudflarestorage.com" || !useSSL {
		t.Fatalf("unexpected normalized endpoint: %q ssl=%v", endpoint, useSSL)
	}

	endpoint, useSSL, err = normalizeS3Endpoint("s3.us-west-004.backblazeb2.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "s3.us-west-004.backblazeb2.com" || !useSSL {
		t.Fatalf("unexpected bare endpoint: %q ssl=%v", endpoint, useSSL)
	}

	if _, _, err := normalizeS3Endpoint("https://example.com/path", true); err == nil {
		t.Fatal("endpoint path must be rejected")
	}
	if _, _, err := normalizeS3Endpoint("https://127.0.0.1:9000", true); err == nil {
		t.Fatal("private/local S3 endpoint must be rejected")
	}
}

func TestNormalizeSettingsEmptySecretPreservesCurrent(t *testing.T) {
	cfg := &Config{JWTSecret: "settings-secret-master-must-be-at-least-32-bytes"}
	smtpCipher, err := encryptSecret(cfg.JWTSecret, "smtp-secret")
	if err != nil {
		t.Fatal(err)
	}
	s3Cipher, err := encryptSecret(cfg.JWTSecret, "s3-secret")
	if err != nil {
		t.Fatal(err)
	}
	empty := ""

	smtp, err := normalizeSMTPInput(cfg, storedSMTPSettings{PasswordCipher: smtpCipher}, SMTPSettingsInput{
		Enabled:  true,
		Host:     "smtp.example.com",
		Port:     "587",
		Username: "mailer",
		From:     "no-reply@example.com",
		Password: &empty,
	})
	if err != nil {
		t.Fatal(err)
	}
	if smtp.PasswordCipher != smtpCipher {
		t.Fatal("empty SMTP password must preserve current encrypted password")
	}

	storage, err := normalizeStorageInput(cfg, storedStorageSettings{S3: storedS3Settings{SecretKeyCipher: s3Cipher}}, StorageSettingsInput{
		Type: StorageS3,
		S3: S3SettingsInput{
			Endpoint:  "https://s3.example.com",
			Bucket:    "dublyobase",
			Region:    "us-east-1",
			AccessKey: "key-id",
			SecretKey: &empty,
			UseSSL:    true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if storage.S3.SecretKeyCipher != s3Cipher {
		t.Fatal("empty S3 secret key must preserve current encrypted secret")
	}
}
