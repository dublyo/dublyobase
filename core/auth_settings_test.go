package core

import "testing"

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
