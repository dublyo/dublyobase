package apis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/dublyo/dublyobase/core"
)

// The panel offers no password recovery by email, so an operator locked out of
// the only admin account previously had no way back in short of editing the
// database by hand. This covers the CLI path that replaces that: the new
// password works, the old one stops working, and every existing session for
// that admin is revoked so a stolen token does not survive the reset.
func TestAdminPasswordResetByEmail(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)

	const email = "locked-out@example.com"
	oldToken := setupAdmin(t, srv.Handler, email)

	// The old session is live before the reset.
	if rec := getJSON(srv.Handler, "/admin/api/me", oldToken); rec.Code != http.StatusOK {
		t.Fatalf("pre-reset session: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	admin, err := core.ResetAdminPasswordByEmail(context.Background(), app.Pool, email, "recovered-pass-456", app.Config.BcryptCost)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !admin.MustChangePassword {
		t.Error("reset should force a password change on next login")
	}

	// The stolen-token case: the pre-reset session must be dead.
	if rec := getJSON(srv.Handler, "/admin/api/me", oldToken); rec.Code == http.StatusOK {
		t.Error("session survived the password reset")
	}

	// The old password must no longer authenticate.
	rec := postJSON(srv.Handler, "/admin/api/auth/login", "", fmt.Sprintf(`{"email":%q,"password":"password-123"}`, email))
	if rec.Code == http.StatusOK {
		t.Error("old password still works after reset")
	}

	// The new one must.
	rec = postJSON(srv.Handler, "/admin/api/auth/login", "", fmt.Sprintf(`{"email":%q,"password":"recovered-pass-456"}`, email))
	if rec.Code != http.StatusOK {
		t.Fatalf("login with new password: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Token == "" {
		t.Fatal("no token issued after recovery login")
	}

	// An unknown address must not silently succeed, or a typo would look like a
	// reset that worked.
	if _, err := core.ResetAdminPasswordByEmail(context.Background(), app.Pool, "nobody@example.com", "whatever-123456", app.Config.BcryptCost); err == nil {
		t.Error("reset for an unknown email should fail")
	}
}
