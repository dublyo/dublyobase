package core

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNormalizeAuthProvidersEncryptsAndRedactsSecrets(t *testing.T) {
	cfg := &Config{JWTSecret: "12345678901234567890123456789012"}
	private, err := normalizeAuthProviders(cfg, nil, map[string]any{
		"google": map[string]any{
			"enabled":      true,
			"clientId":     "client-id",
			"clientSecret": "client-secret",
		},
	})
	if err != nil {
		t.Fatalf("normalize providers: %v", err)
	}
	raw, ok := private["google"].(map[string]any)
	if !ok {
		t.Fatal("expected google provider map")
	}
	if raw["clientSecret"] != nil {
		t.Fatal("plain clientSecret must not be stored")
	}
	if raw["clientSecretCipher"] == "" {
		t.Fatal("expected encrypted clientSecretCipher")
	}
	public := publicAuthProviders(private)
	exposed := public["google"].(map[string]any)
	if exposed["clientSecretCipher"] != nil || exposed["clientSecret"] != nil {
		t.Fatal("public provider must not expose secret fields")
	}
	if exposed["clientSecretSet"] != true {
		t.Fatal("public provider should report saved secret")
	}
}

func TestProjectAuthSettingsInputAcceptsPublicSettingsShape(t *testing.T) {
	body := []byte(`{
		"projectId": "project-1",
		"projectSlug": "demo",
		"accessTokenMinutes": 60,
		"refreshTokenDays": 7,
		"verifyTokenHours": 24,
		"resetTokenHours": 1,
		"otpEnabled": true,
		"otpTokenMinutes": 10,
		"mfaEnabled": true,
		"mfaRequired": false,
		"emailChangeEnabled": true,
		"emailChangeRequiresPassword": true,
		"templates": {
			"verifySubject": "Verify",
			"verifyBody": "Body",
			"resetSubject": "Reset",
			"resetBody": "Body",
			"otpSubject": "OTP",
			"otpBody": "Body",
			"emailChangeSubject": "Email change",
			"emailChangeBody": "Body",
			"invitationSubject": "Invite",
			"invitationBody": "Body"
		},
		"providers": {
			"github": {
				"enabled": true,
				"clientId": "client",
				"clientSecretSet": true,
				"scopes": ["read:user", "user:email"]
			}
		},
		"createdAt": "2026-07-06T12:23:47.324909Z",
		"updatedAt": "2026-07-06T15:19:58.47681Z"
	}`)
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var input ProjectAuthSettingsInput
	if err := dec.Decode(&input); err != nil {
		t.Fatalf("decode public settings shape: %v", err)
	}
	if input.ProjectID != "project-1" || input.ProjectSlug != "demo" {
		t.Fatalf("read-only response fields should decode and be ignored by update: %#v", input)
	}
}
