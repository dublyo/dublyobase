package apis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dublyo/dublyobase/core"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newIntegrationApp(t *testing.T) (*core.App, func()) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	adminCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	adminDB := adminCfg.ConnConfig.Config.Database
	if adminDB == "" {
		adminDB = "postgres"
	}
	adminCfg.ConnConfig.Config.Database = "postgres"
	adminPool, err := pgxpool.NewWithConfig(context.Background(), adminCfg)
	if err != nil {
		t.Fatal(err)
	}

	dbName := fmt.Sprintf("dublyobase_api_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(context.Background(), `create database `+pgx.Identifier{dbName}.Sanitize()); err != nil {
		adminPool.Close()
		if pgErrCode(err) == "42501" {
			t.Skipf("TEST_DATABASE_URL user cannot create databases: %v", err)
		}
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	cleanup := func() {
		if pool != nil {
			pool.Close()
		}
		_, _ = adminPool.Exec(context.Background(), `
			select pg_terminate_backend(pid)
			from pg_stat_activity
			where datname = $1 and pid <> pg_backend_pid()`,
			dbName,
		)
		_, _ = adminPool.Exec(context.Background(), `drop database if exists `+pgx.Identifier{dbName}.Sanitize())
		adminCfg.ConnConfig.Config.Database = adminDB
		adminPool.Close()
	}
	t.Cleanup(cleanup)

	testCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	testCfg.ConnConfig.Config.Database = dbName
	pool, err = pgxpool.NewWithConfig(context.Background(), testCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Migrate(context.Background(), pool, testLogger()); err != nil {
		t.Fatal(err)
	}

	cfg := &core.Config{
		Host: "127.0.0.1", Port: "0",
		StorageType:       core.StorageLocal,
		StorageLocalPath:  t.TempDir(),
		CORSOrigins:       []string{"*"},
		TrustProxyHeaders: true,
		JWTSecret:         "test-jwt-secret-must-be-at-least-32-bytes",
		AppURL:            "http://127.0.0.1",
		BcryptCost:        4,
		AuthDevTokens:     true,
		MaxUploadMB:       64,
	}
	app := core.NewApp(cfg, pool, testLogger())
	app.SetReady(true)

	return app, cleanup
}

func postJSON(handler http.Handler, path string, token string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func getJSON(handler http.Handler, path string, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func putJSON(handler http.Handler, path string, token string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("PUT", path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func setupAdmin(t *testing.T, handler http.Handler, email string) string {
	t.Helper()
	rec := postJSON(handler, "/setup", "", fmt.Sprintf(`{"email":%q,"password":"password-123"}`, email))
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	return loginAdmin(t, handler, email)
}

func loginAdmin(t *testing.T, handler http.Handler, email string) string {
	t.Helper()
	rec := postJSON(handler, "/admin/api/auth/login", "", fmt.Sprintf(`{"email":%q,"password":"password-123"}`, email))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body.Token, "dbo_admin_") {
		t.Fatalf("unexpected token %q", body.Token)
	}
	return body.Token
}

func TestSetupLoginMeAndLogout(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)

	token := setupAdmin(t, srv.Handler, "Admin@Example.COM")

	rec := postJSON(srv.Handler, "/setup", "", `{"email":"other@example.com","password":"password-123"}`)
	if rec.Code != http.StatusGone {
		t.Fatalf("second setup: want 410, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = getJSON(srv.Handler, "/admin/api/me", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var storedHash string
	if err := app.Pool.QueryRow(context.Background(), `select token_hash from _dbo.admin_sessions limit 1`).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == token || strings.Contains(storedHash, token) {
		t.Fatal("session table must store token hash, not plaintext")
	}

	rec = postJSON(srv.Handler, "/admin/api/auth/logout", token, `{}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: want 204, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = getJSON(srv.Handler, "/admin/api/me", token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: want 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSetupUnavailableUntilAppReady(t *testing.T) {
	app, _ := newIntegrationApp(t)
	app.SetReady(false)
	srv := NewServer(app)

	rec := postJSON(srv.Handler, "/setup", "", `{"email":"race@example.com","password":"password-123"}`)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "setup_starting") {
		t.Fatalf("setup during boot: want 503 setup_starting, got %d: %s", rec.Code, rec.Body.String())
	}

	app.SetReady(true)
	rec = postJSON(srv.Handler, "/setup", "", `{"email":"race@example.com","password":"password-123"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup after ready: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBootstrapAdminRequiresPasswordChange(t *testing.T) {
	app, _ := newIntegrationApp(t)
	_, bootstrapPassword, err := core.CreateBootstrapAdmin(context.Background(), app.Pool, app.Config.BcryptCost, "", "")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(app)

	rec := postJSON(srv.Handler, "/admin/api/auth/login", "", fmt.Sprintf(`{"email":"admin@example.com","password":%q}`, bootstrapPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap login: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var loginBody struct {
		Token string     `json:"token"`
		Admin core.Admin `json:"admin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &loginBody); err != nil {
		t.Fatal(err)
	}
	if loginBody.Token == "" || !loginBody.Admin.MustChangePassword {
		t.Fatalf("bootstrap login must return forced-change admin: %s", rec.Body.String())
	}

	rec = getJSON(srv.Handler, "/admin/api/me", loginBody.Token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"mustChangePassword":true`) {
		t.Fatalf("me should allow forced-change session: code=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = getJSON(srv.Handler, "/admin/api/projects", loginBody.Token)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "password_change_required") {
		t.Fatalf("forced-change admin API: want 403 password_change_required, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postJSON(srv.Handler, "/admin/api/auth/change-password", loginBody.Token, `{"currentPassword":"wrong","newPassword":"changed-pass-123"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password: want 401, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postJSON(srv.Handler, "/admin/api/auth/change-password", loginBody.Token, fmt.Sprintf(`{"currentPassword":%q,"newPassword":"changed-pass-123"}`, bootstrapPassword))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"mustChangePassword":false`) {
		t.Fatalf("change password: want unlocked admin, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = getJSON(srv.Handler, "/admin/api/projects", loginBody.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin API after password change: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postJSON(srv.Handler, "/admin/api/auth/login", "", fmt.Sprintf(`{"email":"admin@example.com","password":%q}`, bootstrapPassword))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old bootstrap password after change: want 401, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, "/admin/api/auth/login", "", `{"email":"admin@example.com","password":"changed-pass-123"}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"mustChangePassword":true`) {
		t.Fatalf("new password login: want normal admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOwnerCreatesSuperAdmin(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	ownerToken := setupAdmin(t, srv.Handler, "owner@example.com")

	rec := postJSON(srv.Handler, "/admin/api/admins", ownerToken, `{"email":"super@example.com","password":"temporary-pass-123"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"role":"super_admin"`) || !strings.Contains(rec.Body.String(), `"mustChangePassword":true`) {
		t.Fatalf("owner create super admin: want 201 super_admin forced change, got %d: %s", rec.Code, rec.Body.String())
	}

	superLogin := postJSON(srv.Handler, "/admin/api/auth/login", "", `{"email":"super@example.com","password":"temporary-pass-123"}`)
	if superLogin.Code != http.StatusOK {
		t.Fatalf("super admin login: want 200, got %d: %s", superLogin.Code, superLogin.Body.String())
	}
	var loginBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(superLogin.Body.Bytes(), &loginBody); err != nil {
		t.Fatal(err)
	}

	rec = postJSON(srv.Handler, "/admin/api/admins", loginBody.Token, `{"email":"blocked@example.com","password":"temporary-pass-123"}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "password_change_required") {
		t.Fatalf("forced-change super admin should be blocked before ready APIs, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postJSON(srv.Handler, "/admin/api/auth/change-password", loginBody.Token, `{"currentPassword":"temporary-pass-123","newPassword":"changed-pass-123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("super admin change password: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postJSON(srv.Handler, "/admin/api/admins", loginBody.Token, `{"email":"blocked@example.com","password":"temporary-pass-123"}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "forbidden") {
		t.Fatalf("super admin must not create admins, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = getJSON(srv.Handler, "/admin/api/admins", ownerToken)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"role":"owner"`) || !strings.Contains(rec.Body.String(), `"role":"super_admin"`) {
		t.Fatalf("list admins: want owner and super admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeCORSSettings(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := fmt.Sprintf("cors%d", time.Now().UnixNano()%1_000_000_000)

	rec := postJSON(srv.Handler, "/admin/api/projects", token, fmt.Sprintf(`{"slug":%q,"name":"CORS Demo"}`, slug))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = putJSON(srv.Handler, "/admin/api/settings/cors", token, `{"adminOrigins":["https://admin.example.com"],"allowWildcard":false}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"adminOrigins":["https://admin.example.com"]`) {
		t.Fatalf("update admin CORS: want saved origin, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = putJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/cors", slug), token, `{"publicOrigins":["https://app.example.com"],"allowWildcard":false}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"publicOrigins":["https://app.example.com"]`) {
		t.Fatalf("update project CORS: want saved origin, got %d: %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest("OPTIONS", "/admin/api/settings", nil)
	req.Header.Set("Origin", "https://admin.example.com")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://admin.example.com" {
		t.Fatalf("admin CORS ACAO = %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}

	req = httptest.NewRequest("OPTIONS", fmt.Sprintf("/api/projects/%s/collections/users/records", slug), nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("project CORS ACAO = %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestForcedChangeAdminCannotUseRecordRoutes(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := fmt.Sprintf("p%d", time.Now().UnixNano()%1_000_000_000)

	rec := postJSON(srv.Handler, "/admin/api/projects", token, fmt.Sprintf(`{"slug":%q,"name":"Forced Change Demo"}`, slug))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := app.Pool.Exec(context.Background(), `update _dbo.admins set must_change_password = true`); err != nil {
		t.Fatal(err)
	}

	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/users/records", slug), token)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "password_change_required") {
		t.Fatalf("forced-change record route: want 403 password_change_required, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSetupConcurrent(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := postJSON(srv.Handler, "/setup", "", fmt.Sprintf(`{"email":"admin%d@example.com","password":"password-123"}`, i))
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	created, closed := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusGone:
			closed++
		default:
			t.Fatalf("unexpected setup status: %v", codes)
		}
	}
	if created != 1 || closed != 1 {
		t.Fatalf("concurrent setup statuses = %v", codes)
	}
}

func TestAdminProjectProvisioning(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")

	rec := getJSON(srv.Handler, "/admin/api/projects", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth projects: want 401, got %d", rec.Code)
	}

	rec = getJSON(srv.Handler, "/admin/api/projects", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty projects: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var emptyProjects struct {
		Items []core.Project `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &emptyProjects); err != nil {
		t.Fatal(err)
	}
	if emptyProjects.Items == nil {
		t.Fatalf("empty projects must encode as [] not null: %s", rec.Body.String())
	}

	slug := fmt.Sprintf("p%d", time.Now().UnixNano()%1_000_000_000)
	rec = postJSON(srv.Handler, "/admin/api/projects", token, fmt.Sprintf(`{"slug":%q,"name":"Demo"}`, slug))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postJSON(srv.Handler, "/admin/api/projects", token, fmt.Sprintf(`{"slug":%q,"name":"Demo Again"}`, slug))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate project: want 409, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = getJSON(srv.Handler, "/admin/api/projects/"+slug, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("get project: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	schemaName, roles := core.ProjectNames(slug)
	assertSchemaExists(t, app.Pool, schemaName)
	for _, role := range []string{roles.Anon, roles.Authenticated, roles.Service} {
		assertRoleExists(t, app.Pool, role)
	}
	assertCollectionMetadataCount(t, app.Pool, slug, "users", 1)
	for _, column := range []string{"id", "created", "updated", "email", "email_normalized", "verified", "password_hash", "token_key", "disabled_at", "last_login_at"} {
		assertColumnExists(t, app.Pool, schemaName, "users", column)
	}
	assertAuditExists(t, app.Pool, "project.create", slug)
}

func TestAdminProjectCreateConcurrent(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := fmt.Sprintf("p%d", time.Now().UnixNano()%1_000_000_000)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := postJSON(srv.Handler, "/admin/api/projects", token, fmt.Sprintf(`{"slug":%q,"name":"Demo"}`, slug))
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	created, conflict := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected project status: %v", codes)
		}
	}
	if created != 1 || conflict != 1 {
		t.Fatalf("concurrent project statuses = %v", codes)
	}
}

func TestAdminAuditLogEndpoint(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := fmt.Sprintf("p%d", time.Now().UnixNano()%1_000_000_000)

	rec := postJSON(srv.Handler, "/admin/api/projects", token, fmt.Sprintf(`{"slug":%q,"name":"Audit Demo"}`, slug))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = getJSON(srv.Handler, "/admin/api/audit-log?project="+slug+"&perPage=5", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit log: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items      []core.AuditLogEntry `json:"items"`
		TotalItems int                  `json:"totalItems"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TotalItems == 0 || len(body.Items) == 0 {
		t.Fatalf("audit response missing entries: %s", rec.Body.String())
	}
	if body.Items[0].Action == "" || body.Items[0].Data == nil {
		t.Fatalf("audit entry shape incomplete: %+v", body.Items[0])
	}
}

func TestAdminSettingsEndpoints(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")

	rec := getJSON(srv.Handler, "/admin/api/settings", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-value") {
		t.Fatalf("settings leaked unexpected secret: %s", rec.Body.String())
	}

	rec = putJSON(srv.Handler, "/admin/api/settings/smtp", token, `{
		"enabled": true,
		"host": "smtp.example.com",
		"port": "587",
		"from": "Dublyobase <no-reply@example.com>",
		"username": "mailer",
		"password": "smtp-secret-value"
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update smtp: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "smtp-secret-value") || !strings.Contains(rec.Body.String(), `"passwordSet":true`) {
		t.Fatalf("smtp response must mask secret and show passwordSet: %s", rec.Body.String())
	}

	rec = putJSON(srv.Handler, "/admin/api/settings/storage", token, `{
		"type": "s3",
		"s3": {
			"endpoint": "https://s3.example.com",
			"bucket": "dublyobase",
			"region": "auto",
			"accessKey": "key-id",
			"secretKey": "s3-secret-value",
			"prefix": "prod/uploads",
			"useSSL": true,
			"forcePathStyle": true
		}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update storage: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "s3-secret-value") || !strings.Contains(rec.Body.String(), `"secretKeySet":true`) {
		t.Fatalf("storage response must mask secret and show secretKeySet: %s", rec.Body.String())
	}

	rec = putJSON(srv.Handler, "/admin/api/settings/logs", token, `{"retentionDays":45,"retentionCount":5000}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update logs settings: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"retentionDays":45`) || !strings.Contains(rec.Body.String(), `"retentionCount":5000`) {
		t.Fatalf("logs settings response missing retention values: %s", rec.Body.String())
	}

	var encryptedCount int
	if err := app.Pool.QueryRow(context.Background(), `
		select (
			data->'smtp'->>'passwordCipher' like 'v1:%'
			and data->'storage'->'s3'->>'secretKeyCipher' like 'v1:%'
		)::int
		from _dbo.instance_settings
		where id = true`,
	).Scan(&encryptedCount); err != nil {
		t.Fatal(err)
	}
	if encryptedCount != 1 {
		t.Fatal("settings secrets must be encrypted in instance_settings")
	}

	rec = putJSON(srv.Handler, "/admin/api/settings/storage", token, `{"type":"local","s3":{}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("switch storage local: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, "/admin/api/settings/storage/test", token, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("test local storage: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLogRetentionPruneUsesIntegerInterval(t *testing.T) {
	app, _ := newIntegrationApp(t)
	if _, err := core.PruneAuditLog(context.Background(), app.Pool, 30, 100); err != nil {
		t.Fatalf("prune audit log: %v", err)
	}
	if _, err := core.PruneRequestLogs(context.Background(), app.Pool, 30, 100); err != nil {
		t.Fatalf("prune request logs: %v", err)
	}
}

func TestDisabledAdminCannotUseSession(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")

	if _, err := app.Pool.Exec(context.Background(), `update _dbo.admins set disabled_at = now()`); err != nil {
		t.Fatal(err)
	}
	rec := getJSON(srv.Handler, "/admin/api/me", token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disabled admin: want 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func assertSchemaExists(t *testing.T, pool *pgxpool.Pool, schema string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `select exists(select 1 from pg_namespace where nspname = $1)`, schema).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatalf("schema %q missing", schema)
	}
}

func assertRoleExists(t *testing.T, pool *pgxpool.Pool, role string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `select exists(select 1 from pg_roles where rolname = $1)`, role).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatalf("role %q missing", role)
	}
}

func assertAuditExists(t *testing.T, pool *pgxpool.Pool, action string, targetSlug string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from _dbo.audit_log where action = $1 and data->>'slug' = $2`,
		action,
		targetSlug,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatalf("audit action %q for slug %q missing", action, targetSlug)
	}
}

func pgErrCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
