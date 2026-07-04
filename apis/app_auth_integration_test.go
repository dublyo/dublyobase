package apis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dublyo/dublyobase/core"
)

type appAuthTestResult struct {
	Token            string       `json:"token"`
	RefreshToken     string       `json:"refreshToken"`
	RefreshExpiresAt string       `json:"refreshExpiresAt"`
	User             appUserShape `json:"user"`
}

type appUserShape struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Verified bool   `json:"verified"`
}

func TestAppAuthLifecycle(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)

	signup := signupAppUserForTest(t, srv.Handler, slug, "User@Example.COM")
	if signup.User.Email != "user@example.com" || signup.User.ID == "" || signup.Token == "" || !strings.HasPrefix(signup.RefreshToken, "dbo_refresh_") {
		t.Fatalf("unexpected signup result: %+v", signup)
	}

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/signup", slug), "", `{"email":"user@example.com","password":"password-123"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate signup: want 409, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/me", slug), signup.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/login", slug), "", `{"email":"user@example.com","password":"wrong-password"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login: want 401, got %d: %s", rec.Code, rec.Body.String())
	}

	login := loginAppUserForTest(t, srv.Handler, slug, "user@example.com", "password-123")
	refreshed := refreshAppSessionForTest(t, srv.Handler, slug, login.RefreshToken)
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/refresh", slug), "", fmt.Sprintf(`{"refreshToken":%q}`, login.RefreshToken))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh replay: want 401, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/refresh", slug), "", fmt.Sprintf(`{"refreshToken":%q}`, refreshed.RefreshToken))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("family revoked refresh: want 401, got %d: %s", rec.Code, rec.Body.String())
	}

	login = loginAppUserForTest(t, srv.Handler, slug, "user@example.com", "password-123")
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/logout", slug), "", fmt.Sprintf(`{"refreshToken":%q}`, login.RefreshToken))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: want 204, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/refresh", slug), "", fmt.Sprintf(`{"refreshToken":%q}`, login.RefreshToken))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout: want 401, got %d: %s", rec.Code, rec.Body.String())
	}

	verifyToken := requestDevTokenForTest(t, srv.Handler, fmt.Sprintf("/api/projects/%s/auth/request-verification", slug), "user@example.com")
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/confirm-verification", slug), "", fmt.Sprintf(`{"email":"user@example.com","token":%q}`, verifyToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm verification: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/confirm-verification", slug), "", fmt.Sprintf(`{"email":"user@example.com","token":%q}`, verifyToken))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("verification reuse: want 401, got %d: %s", rec.Code, rec.Body.String())
	}

	login = loginAppUserForTest(t, srv.Handler, slug, "user@example.com", "password-123")
	resetToken := requestDevTokenForTest(t, srv.Handler, fmt.Sprintf("/api/projects/%s/auth/request-password-reset", slug), "user@example.com")
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/confirm-password-reset", slug), "", fmt.Sprintf(`{"email":"user@example.com","token":%q,"password":"new-password-123"}`, resetToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm reset: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/me", slug), login.Token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old access after reset: want 401, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/login", slug), "", `{"email":"user@example.com","password":"password-123"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old password after reset: want 401, got %d: %s", rec.Code, rec.Body.String())
	}
	_ = loginAppUserForTest(t, srv.Handler, slug, "user@example.com", "new-password-123")

	var storedHash string
	if err := app.Pool.QueryRow(context.Background(), `select token_hash from _dbo.sessions limit 1`).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(storedHash, "dbo_refresh_") {
		t.Fatal("refresh token plaintext must not be stored")
	}
}

func TestAppAuthSendsVerificationAndResetEmails(t *testing.T) {
	app, _ := newIntegrationApp(t)
	app.Config.AuthDevTokens = false
	mailer := &recordingMailer{}
	app.Mailer = mailer
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)

	_ = signupAppUserForTest(t, srv.Handler, slug, "mail@example.com")
	if len(mailer.messages) != 1 {
		t.Fatalf("signup should send one verification email, got %d", len(mailer.messages))
	}
	verifyToken := tokenFromMail(t, mailer.messages[0], `dbo_verify_`)
	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/confirm-verification", slug), "", fmt.Sprintf(`{"email":"mail@example.com","token":%q}`, verifyToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm mailed verification: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/request-verification", slug), "", `{"email":"mail@example.com"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request verification: want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "devToken") {
		t.Fatalf("dev token must not be exposed when disabled: %s", rec.Body.String())
	}
	if len(mailer.messages) != 2 {
		t.Fatalf("manual verification should send another email, got %d", len(mailer.messages))
	}

	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/request-password-reset", slug), "", `{"email":"mail@example.com"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request reset: want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "devToken") {
		t.Fatalf("reset dev token must not be exposed when disabled: %s", rec.Body.String())
	}
	if len(mailer.messages) != 3 {
		t.Fatalf("reset should send one email, got %d", len(mailer.messages))
	}
	resetToken := tokenFromMail(t, mailer.messages[2], `dbo_reset_`)
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/confirm-password-reset", slug), "", fmt.Sprintf(`{"email":"mail@example.com","token":%q,"password":"new-password-123"}`, resetToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm mailed reset: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAppAuthEmailActionPages(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := fmt.Sprintf("auth%d", time.Now().UnixNano()%1_000_000_000)
	rec := postJSON(srv.Handler, "/admin/api/projects", adminToken, fmt.Sprintf(`{"slug":%q,"name":"Customer Portal"}`, slug))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	_ = signupAppUserForTest(t, srv.Handler, slug, "action@example.com")

	verifyToken := requestDevTokenForTest(t, srv.Handler, fmt.Sprintf("/api/projects/%s/auth/request-verification", slug), "action@example.com")
	rec = getJSON(srv.Handler, fmt.Sprintf("/auth/verify?project=%s&email=action%%40example.com&token=%s", slug, url.QueryEscape(verifyToken)), "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Customer Portal") || !strings.Contains(rec.Body.String(), "Verify email") {
		t.Fatalf("verify page: want 200 with project name, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postForm(srv.Handler, "/auth/verify", url.Values{
		"project": {slug},
		"email":   {"action@example.com"},
		"token":   {verifyToken},
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Your email has been verified.") {
		t.Fatalf("verify submit: want success, got %d: %s", rec.Code, rec.Body.String())
	}

	resetToken := requestDevTokenForTest(t, srv.Handler, fmt.Sprintf("/api/projects/%s/auth/request-password-reset", slug), "action@example.com")
	rec = getJSON(srv.Handler, fmt.Sprintf("/auth/reset-password?project=%s&email=action%%40example.com&token=%s", slug, url.QueryEscape(resetToken)), "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Customer Portal") || !strings.Contains(rec.Body.String(), "New password") {
		t.Fatalf("reset page: want 200 with project name and password field, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postForm(srv.Handler, "/auth/reset-password", url.Values{
		"project":  {slug},
		"email":    {"action@example.com"},
		"token":    {resetToken},
		"password": {"new-password-123"},
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Your password has been changed.") {
		t.Fatalf("reset submit: want success, got %d: %s", rec.Code, rec.Body.String())
	}
	_ = loginAppUserForTest(t, srv.Handler, slug, "action@example.com", "new-password-123")
}

func TestAppAuthEmailFailureDoesNotFailRequests(t *testing.T) {
	app, _ := newIntegrationApp(t)
	app.Config.AuthDevTokens = false
	app.Mailer = failingMailer{}
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)

	_ = signupAppUserForTest(t, srv.Handler, slug, "mail-fail@example.com")

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/request-verification", slug), "", `{"email":"mail-fail@example.com"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request verification with failing mailer: want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "devToken") {
		t.Fatalf("dev token must not leak with failing mailer: %s", rec.Body.String())
	}

	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/request-password-reset", slug), "", `{"email":"mail-fail@example.com"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request reset with failing mailer: want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "devToken") {
		t.Fatalf("reset dev token must not leak with failing mailer: %s", rec.Body.String())
	}
}

type recordingMailer struct {
	messages []core.MailMessage
}

func (m *recordingMailer) Send(_ context.Context, msg core.MailMessage) error {
	m.messages = append(m.messages, msg)
	return nil
}

type failingMailer struct{}

func (failingMailer) Send(context.Context, core.MailMessage) error {
	return fmt.Errorf("smtp unavailable")
}

func tokenFromMail(t *testing.T, msg core.MailMessage, prefix string) string {
	t.Helper()
	if msg.To == "" || !strings.Contains(msg.Text, "/auth/") {
		t.Fatalf("bad auth email message: %+v", msg)
	}
	re := regexp.MustCompile(prefix + `[A-Za-z0-9_-]+`)
	token := re.FindString(msg.Text)
	if token == "" {
		t.Fatalf("token with prefix %s missing from email: %s", prefix, msg.Text)
	}
	return token
}

func TestAppAuthUnlocksRecordRulesAndLogoutAllInvalidatesAccess(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)

	createCollectionBody := `{
		"name":"posts",
		"type":"base",
		"fields":[
			{"name":"title","type":"text","required":true},
			{"name":"owner","type":"relation","options":{"collection":"users"}}
		],
		"viewRule":"owner = @request.auth.id"
	}`
	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), adminToken, createCollectionBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	serviceKey := createAPIKeyForRecords(t, srv.Handler, adminToken, slug, "service")
	user := signupAppUserForTest(t, srv.Handler, slug, "owner@example.com")
	record := createRecordForTest(t, srv.Handler, slug, serviceKey, fmt.Sprintf(`{"title":"Private","owner":%q}`, user.User.ID))

	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records/%s", slug, record["id"]), user.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner view: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/auth/logout-all", slug), user.Token, `{}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout all: want 204, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records/%s", slug, record["id"]), user.Token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old access after logout-all: want 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthUsersHiddenColumnsStayOutOfRecordsAPI(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)
	serviceKey := createAPIKeyForRecords(t, srv.Handler, adminToken, slug, "service")
	user := signupAppUserForTest(t, srv.Handler, slug, "hidden@example.com")

	rec := getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/users/records/%s", slug, user.User.ID), serviceKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("service get user record: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var record map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"email", "password_hash", "token_key", "email_normalized", "disabled_at", "last_login_at"} {
		if _, ok := record[forbidden]; ok {
			t.Fatalf("users record leaked %q: %s", forbidden, rec.Body.String())
		}
	}

	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/users/records", slug), serviceKey, `{"password_hash":"plaintext"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("hidden auth write: want 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func signupAppUserForTest(t *testing.T, handler http.Handler, slug string, email string) appAuthTestResult {
	t.Helper()
	rec := postJSON(handler, fmt.Sprintf("/api/projects/%s/auth/signup", slug), "", fmt.Sprintf(`{"email":%q,"password":"password-123"}`, email))
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	return decodeAppAuthResult(t, rec)
}

func loginAppUserForTest(t *testing.T, handler http.Handler, slug string, email string, password string) appAuthTestResult {
	t.Helper()
	rec := postJSON(handler, fmt.Sprintf("/api/projects/%s/auth/login", slug), "", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
	if rec.Code != http.StatusOK {
		t.Fatalf("app login: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	return decodeAppAuthResult(t, rec)
}

func refreshAppSessionForTest(t *testing.T, handler http.Handler, slug string, refreshToken string) appAuthTestResult {
	t.Helper()
	rec := postJSON(handler, fmt.Sprintf("/api/projects/%s/auth/refresh", slug), "", fmt.Sprintf(`{"refreshToken":%q}`, refreshToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	return decodeAppAuthResult(t, rec)
}

func decodeAppAuthResult(t *testing.T, rec *httptest.ResponseRecorder) appAuthTestResult {
	t.Helper()
	var out appAuthTestResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Token == "" || out.RefreshToken == "" || out.User.ID == "" {
		t.Fatalf("incomplete auth response: %s", rec.Body.String())
	}
	return out
}

func requestDevTokenForTest(t *testing.T, handler http.Handler, path string, email string) string {
	t.Helper()
	rec := postJSON(handler, path, "", fmt.Sprintf(`{"email":%q}`, email))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request dev token: want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Accepted bool   `json:"accepted"`
		DevToken string `json:"devToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Accepted || out.DevToken == "" {
		t.Fatalf("missing dev token: %s", rec.Body.String())
	}
	return out.DevToken
}

func postForm(handler http.Handler, path string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
