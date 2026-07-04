package core

import (
	"strings"
	"testing"
)

func TestBuildAuthTokenEmail(t *testing.T) {
	cfg := &Config{
		AppURL:   "https://app.example.com/base/",
		SMTPFrom: "Dublyobase <no-reply@example.com>",
	}
	msg, err := BuildAuthTokenEmail(cfg, "verify_email", "demo", "Demo App", "User@Example.COM", "dbo_verify_token")
	if err != nil {
		t.Fatal(err)
	}
	if msg.To != "user@example.com" || msg.Subject != "Verify your email for Demo App" || !strings.Contains(msg.Text, "dbo_verify_token") {
		t.Fatalf("bad verify email: %+v", msg)
	}
	if !strings.Contains(msg.Text, "Verify your email for Demo App.") ||
		!strings.Contains(msg.Text, "https://app.example.com/base/auth/verify?") ||
		!strings.Contains(msg.Text, "project=demo") ||
		!strings.Contains(msg.Text, "email=user%40example.com") {
		t.Fatalf("verify email missing APP_URL link: %s", msg.Text)
	}

	msg, err = BuildAuthTokenEmail(cfg, "password_reset", "demo", "Demo App", "user@example.com", "dbo_reset_token")
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "Reset your Demo App password" || !strings.Contains(msg.Text, "/auth/reset-password?") || !strings.Contains(msg.Text, "dbo_reset_token") {
		t.Fatalf("reset email missing link/token: %s", msg.Text)
	}
}

func TestFormatMailMessageSanitizesHeaders(t *testing.T) {
	raw := formatMailMessage("Dublyobase\r\nBcc: leak@example.com <no-reply@example.com>", "user@example.com", MailMessage{
		Subject: "Hello\r\nBcc: leak@example.com",
		Text:    "line1\rline2\nline3",
	})
	if strings.Contains(raw, "\r\nBcc: leak@example.com") {
		t.Fatalf("formatted email allowed header injection:\n%s", raw)
	}
	if !strings.Contains(raw, "line1\r\nline2\r\nline3\r\n") {
		t.Fatalf("formatted email did not normalize body line endings:\n%s", raw)
	}
}
