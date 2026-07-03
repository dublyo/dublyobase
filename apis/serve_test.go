package apis

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dublyo/dublyobase/core"
	"github.com/dublyo/dublyobase/ui"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// deadPool returns a lazily-connected pool pointed at a closed port: valid
// object, every Ping fails fast.
func deadPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(),
		"postgres://nobody@127.0.0.1:1/nothing?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newTestApp(t *testing.T, pool *pgxpool.Pool) *core.App {
	t.Helper()
	cfg := &core.Config{
		Host: "127.0.0.1", Port: "0",
		StorageType:      core.StorageLocal,
		StorageLocalPath: t.TempDir(),
		CORSOrigins:      []string{"*"},
	}
	return core.NewApp(cfg, pool, testLogger())
}

func TestReadyEndpoint(t *testing.T) {
	app := newTestApp(t, deadPool(t))
	srv := NewServer(app)

	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("before SetReady: want 503, got %d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "migrating" {
		t.Fatalf(`want {"status":"migrating"}, got %v`, body)
	}

	app.SetReady(true)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/ready", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("after SetReady: want 200, got %d", rec.Code)
	}
}

func TestHealthDegradedShapeAndSpeed(t *testing.T) {
	app := newTestApp(t, deadPool(t))
	srv := NewServer(app)

	start := time.Now()
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("dead DB: want 503, got %d", rec.Code)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("/health must answer <3s, took %s", elapsed)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"status", "db", "storage", "version"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("missing %q in /health body: %v", key, body)
		}
	}
	if body["status"] != "degraded" {
		t.Fatalf("want degraded, got %v", body["status"])
	}
	// Never leak connection detail (hostnames, roles) to this public route.
	if s, _ := body["db"].(string); s != "error" {
		t.Fatalf(`db must be exactly "error" (no internals), got %q`, s)
	}
	if s, _ := body["storage"].(string); s != "ok" {
		t.Fatalf("storage (tempdir) should be ok, got %v", body["storage"])
	}
}

func TestHealthOKWithRealDB(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	app := newTestApp(t, pool)
	srv := NewServer(app)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("want status ok, got %s", rec.Body.String())
	}
}

func TestSPAFallback(t *testing.T) {
	app := newTestApp(t, deadPool(t))
	srv := NewServer(app)

	for _, route := range []string{"/", "/collections/users", "/admin/settings"} {
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", route, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: want 200 (SPA fallback), got %d", route, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "dublyobase") {
			t.Fatalf("GET %s: fallback must serve index.html", route)
		}
	}

	// sanity: the embedded FS actually contains the shell
	if _, err := ui.DistFS().Open("index.html"); err != nil {
		t.Fatalf("embedded index.html missing: %v", err)
	}
}

func TestReservedAPIPrefixesDoNotServeSPA(t *testing.T) {
	app := newTestApp(t, deadPool(t))
	srv := NewServer(app)

	for _, route := range []string{"/api/health", "/api/projects/demo", "/admin/api/projects"} {
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", route, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s: want API 404, got %d", route, rec.Code)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("GET %s: API miss must return JSON, got %q", route, rec.Header().Get("Content-Type"))
		}
		if strings.Contains(rec.Body.String(), "<html") || strings.Contains(rec.Body.String(), "admin panel") {
			t.Fatalf("GET %s: API miss must not serve the SPA: %s", route, rec.Body.String())
		}
	}
}
