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

	testCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	testCfg.ConnConfig.Config.Database = dbName
	pool, err := pgxpool.NewWithConfig(context.Background(), testCfg)
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

	cleanup := func() {
		pool.Close()
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
