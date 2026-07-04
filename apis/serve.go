// Package apis wires the single HTTP surface: admin UI, REST API, realtime and
// file uploads — all served from one process on one port (default :8080).
package apis

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/dublyo/dublyobase/core"
	"github.com/dublyo/dublyobase/ui"
)

type server struct {
	app                   *core.App
	setupLimiter          *rateLimiter
	loginLimiter          *rateLimiter
	changePasswordLimiter *rateLimiter
	authLimiter           *rateLimiter
	mcpLimiter            *rateLimiter
}

// NewServer builds the HTTP server for an App.
func NewServer(app *core.App) *http.Server {
	s := &server{
		app:                   app,
		setupLimiter:          newRateLimiter(5, time.Minute),
		loginLimiter:          newRateLimiter(10, time.Minute),
		changePasswordLimiter: newRateLimiter(5, time.Minute),
		authLimiter:           newRateLimiter(30, time.Minute),
		mcpLimiter:            newRateLimiter(120, time.Minute),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /ready", s.ready)
	mux.Handle("POST /setup", s.limitByIP("setup", s.setupLimiter, http.HandlerFunc(s.setup)))
	mux.Handle("POST /admin/api/auth/login", s.limitByIP("login", s.loginLimiter, http.HandlerFunc(s.adminLogin)))
	mux.Handle("POST /admin/api/auth/logout", s.requireAdmin(http.HandlerFunc(s.adminLogout)))
	mux.Handle("POST /admin/api/auth/change-password", s.limitByIP("admin-change-password", s.changePasswordLimiter, s.requireAdmin(http.HandlerFunc(s.adminChangePassword))))
	mux.Handle("GET /admin/api/me", s.requireAdmin(http.HandlerFunc(s.adminMe)))
	mux.Handle("GET /admin/api/projects", s.requireAdminReady(http.HandlerFunc(s.adminListProjects)))
	mux.Handle("POST /admin/api/projects", s.requireAdminReady(http.HandlerFunc(s.adminCreateProject)))
	mux.Handle("GET /admin/api/projects/{slug}", s.requireAdminReady(http.HandlerFunc(s.adminGetProject)))
	mux.Handle("GET /admin/api/projects/{slug}/api-keys", s.requireAdminReady(http.HandlerFunc(s.adminListAPIKeys)))
	mux.Handle("POST /admin/api/projects/{slug}/api-keys", s.requireAdminReady(http.HandlerFunc(s.adminCreateAPIKey)))
	mux.Handle("DELETE /admin/api/projects/{slug}/api-keys/{id}", s.requireAdminReady(http.HandlerFunc(s.adminRevokeAPIKey)))
	mux.Handle("GET /admin/api/projects/{slug}/collections/export", s.requireAdminReady(http.HandlerFunc(s.adminExportCollections)))
	mux.Handle("POST /admin/api/projects/{slug}/collections/import", s.requireAdminReady(http.HandlerFunc(s.adminImportCollections)))
	mux.Handle("GET /admin/api/projects/{slug}/schema/discover", s.requireAdminReady(http.HandlerFunc(s.adminDiscoverSchema)))
	mux.Handle("POST /admin/api/projects/{slug}/schema/import", s.requireAdminReady(http.HandlerFunc(s.adminImportSchemaTables)))
	mux.Handle("POST /admin/api/projects/{slug}/sql", s.requireAdminReady(http.HandlerFunc(s.adminRunSQL)))
	mux.Handle("GET /admin/api/audit-log", s.requireAdminReady(http.HandlerFunc(s.adminListAuditLog)))
	mux.Handle("GET /admin/api/settings", s.requireAdminReady(http.HandlerFunc(s.adminGetSettings)))
	mux.Handle("PUT /admin/api/settings/smtp", s.requireAdminReady(http.HandlerFunc(s.adminUpdateSMTPSettings)))
	mux.Handle("POST /admin/api/settings/smtp/test", s.requireAdminReady(http.HandlerFunc(s.adminTestSMTPSettings)))
	mux.Handle("PUT /admin/api/settings/storage", s.requireAdminReady(http.HandlerFunc(s.adminUpdateStorageSettings)))
	mux.Handle("POST /admin/api/settings/storage/test", s.requireAdminReady(http.HandlerFunc(s.adminTestStorageSettings)))
	mux.Handle("GET /admin/api/cron-jobs", s.requireAdminReady(http.HandlerFunc(s.adminListCronJobs)))
	mux.Handle("POST /admin/api/cron-jobs", s.requireAdminReady(http.HandlerFunc(s.adminCreateCronJob)))
	mux.Handle("GET /admin/api/cron-jobs/{id}/runs", s.requireAdminReady(http.HandlerFunc(s.adminListCronRuns)))
	mux.Handle("POST /admin/api/cron-jobs/{id}/run", s.requireAdminReady(http.HandlerFunc(s.adminRunCronJob)))
	mux.Handle("GET /admin/api/backups", s.requireAdminReady(http.HandlerFunc(s.adminListBackupJobs)))
	mux.Handle("POST /admin/api/backups", s.requireAdminReady(http.HandlerFunc(s.adminCreateBackupJob)))
	mux.Handle("GET /admin/api/backups/{id}/runs", s.requireAdminReady(http.HandlerFunc(s.adminListBackupRuns)))
	mux.Handle("POST /admin/api/backups/{id}/run", s.requireAdminReady(http.HandlerFunc(s.adminRunBackupJob)))
	mux.Handle("GET /admin/api/mcp/tokens", s.requireAdminReady(http.HandlerFunc(s.adminListMCPTokens)))
	mux.Handle("POST /admin/api/mcp/tokens", s.requireAdminReady(http.HandlerFunc(s.adminCreateMCPToken)))
	mux.Handle("DELETE /admin/api/mcp/tokens/{id}", s.requireAdminReady(http.HandlerFunc(s.adminRevokeMCPToken)))
	mux.Handle("POST /mcp", s.limitByIP("mcp", s.mcpLimiter, http.HandlerFunc(s.mcp)))
	mux.Handle("GET /auth/verify", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.authVerifyPage)))
	mux.Handle("POST /auth/verify", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.authVerifySubmit)))
	mux.Handle("GET /auth/reset-password", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.authResetPasswordPage)))
	mux.Handle("POST /auth/reset-password", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.authResetPasswordSubmit)))
	mux.Handle("POST /api/projects/{slug}/auth/signup", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appSignup)))
	mux.Handle("POST /api/projects/{slug}/auth/login", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appLogin)))
	mux.Handle("POST /api/projects/{slug}/auth/refresh", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appRefresh)))
	mux.Handle("POST /api/projects/{slug}/auth/logout", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appLogout)))
	mux.Handle("POST /api/projects/{slug}/auth/logout-all", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appLogoutAll)))
	mux.Handle("GET /api/projects/{slug}/auth/me", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appMe)))
	mux.Handle("POST /api/projects/{slug}/auth/request-verification", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appRequestVerification)))
	mux.Handle("POST /api/projects/{slug}/auth/confirm-verification", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appConfirmVerification)))
	mux.Handle("POST /api/projects/{slug}/auth/request-password-reset", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appRequestPasswordReset)))
	mux.Handle("POST /api/projects/{slug}/auth/confirm-password-reset", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appConfirmPasswordReset)))
	mux.Handle("GET /api/projects/{slug}/collections", s.requireAdminReady(http.HandlerFunc(s.listCollections)))
	mux.Handle("POST /api/projects/{slug}/collections", s.requireAdminReady(http.HandlerFunc(s.createCollection)))
	mux.Handle("GET /api/projects/{slug}/collections/{name}", s.requireAdminReady(http.HandlerFunc(s.getCollection)))
	mux.Handle("PATCH /api/projects/{slug}/collections/{name}", s.requireAdminReady(http.HandlerFunc(s.updateCollection)))
	mux.Handle("DELETE /api/projects/{slug}/collections/{name}", s.requireAdminReady(http.HandlerFunc(s.deleteCollection)))
	mux.HandleFunc("GET /api/projects/{slug}/collections/{name}/records", s.listRecords)
	mux.HandleFunc("POST /api/projects/{slug}/collections/{name}/records", s.createRecord)
	mux.HandleFunc("GET /api/projects/{slug}/collections/{name}/records/{id}", s.getRecord)
	mux.HandleFunc("PATCH /api/projects/{slug}/collections/{name}/records/{id}", s.updateRecord)
	mux.HandleFunc("DELETE /api/projects/{slug}/collections/{name}/records/{id}", s.deleteRecord)
	mux.HandleFunc("POST /api/projects/{slug}/files/{collection}/{recordId}/{field}", s.uploadFiles)
	mux.HandleFunc("POST /api/projects/{slug}/files/{collection}/{recordId}/{field}/uploads", s.createFileUploadSession)
	mux.HandleFunc("PUT /api/projects/{slug}/files/uploads/{uploadId}/chunks/{index}", s.uploadFileChunk)
	mux.HandleFunc("POST /api/projects/{slug}/files/uploads/{uploadId}/complete", s.completeFileUploadSession)
	mux.HandleFunc("DELETE /api/projects/{slug}/files/uploads/{uploadId}", s.cancelFileUploadSession)
	mux.HandleFunc("POST /api/projects/{slug}/files/{collection}/{recordId}/{field}/{fileId}/token", s.createFileToken)
	mux.HandleFunc("GET /api/projects/{slug}/files/{collection}/{recordId}/{field}/{fileId}/{filename}", s.downloadFile)
	mux.Handle("/", adminUIHandler(ui.DistFS()))

	return &http.Server{
		Addr:              app.Config.Addr(),
		Handler:           s.withMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

// adminUIHandler serves static admin assets, exposes the admin SPA only below
// /_/, and keeps the root path as a generic decoy page.
func adminUIHandler(dist fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			writeDecoyRoot(w)
			return
		}
		if isReservedAPIPath(p) {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error":   "not_found",
				"message": "route not found",
			})
			return
		}
		if p != "" {
			if f, err := dist.Open(p); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
			if !isAdminUIPath(p) || rejectsSPAFallback(p) {
				http.NotFound(w, r)
				return
			}
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

func writeDecoyRoot(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex,nofollow">
  <title>Access denied</title>
  <style>
    :root { color-scheme: dark; font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { min-height: 100vh; margin: 0; display: grid; place-items: center; background: #0b0d10; color: #e5e7eb; }
    main { width: min(560px, calc(100vw - 48px)); border: 1px solid #2f3642; background: #11151b; padding: 32px; }
    h1 { margin: 0 0 12px; font-size: 22px; line-height: 1.2; }
    p { margin: 0; color: #9ca3af; line-height: 1.6; }
    code { color: #f87171; }
  </style>
</head>
<body>
  <main>
    <h1>Access denied</h1>
    <p>This endpoint is not public. The request has been rejected and logged.</p>
    <p><code>403</code> restricted service</p>
  </main>
</body>
</html>`))
}

func isAdminUIPath(p string) bool {
	return p == "_" || strings.HasPrefix(p, "_/")
}

func isReservedAPIPath(p string) bool {
	return p == "api" || strings.HasPrefix(p, "api/") ||
		p == "admin/api" || strings.HasPrefix(p, "admin/api/") ||
		p == "mcp"
}

func rejectsSPAFallback(p string) bool {
	if path.Ext(p) != "" || strings.HasPrefix(p, ".") || strings.Contains(p, "/.") {
		return true
	}
	first, _, _ := strings.Cut(strings.ToLower(p), "/")
	switch first {
	case "actuator", "backup", "backups", "config", "debug", "home", "laravel",
		"old", "phpmyadmin", "pma", "root", "server-status", "storage",
		"temp", "tmp", "vendor", "wp-admin", "wp-content", "wp-includes":
		return true
	default:
		return false
	}
}

// health reports liveness of the app and its dependencies. It must answer
// within ~3s even under load or the container orchestrator will kill us.
// Detail strings are logged, never returned: this route is public through the
// proxy, and pg error text leaks hostnames, roles and paths.
func (s *server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	healthy := true

	dbStatus := "ok"
	if err := s.app.Pool.Ping(ctx); err != nil {
		s.app.Log.Warn("health: db ping failed", "err", err)
		dbStatus = "error"
		healthy = false
	}

	storageStatus := "ok"
	if err := s.checkStorage(); err != nil {
		s.app.Log.Warn("health: storage check failed", "err", err)
		storageStatus = "error"
		healthy = false
	}

	status, code := "ok", http.StatusOK
	if !healthy {
		status, code = "degraded", http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status":  status,
		"db":      dbStatus,
		"storage": storageStatus,
		"version": core.Version,
	})
}

// ready is 503 while migrations run, 200 once the app can serve traffic.
func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	if !s.app.Ready() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "migrating"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

// checkStorage verifies the configured backend is actually usable — for local
// storage that means proving a write succeeds (a read-only mount must not
// report ok until the first upload fails).
func (s *server) checkStorage() error {
	switch s.app.Config.StorageType {
	case core.StorageLocal:
		p := s.app.Config.StorageLocalPath
		if err := os.MkdirAll(p, 0o750); err != nil {
			return err
		}
		f, err := os.CreateTemp(p, ".healthz-*")
		if err != nil {
			return err
		}
		name := f.Name()
		f.Close()
		return os.Remove(name)
	case core.StorageS3:
		// A real HEAD-bucket check lands with the storage backend milestone.
		if s.app.Config.S3Bucket == "" {
			return errNoBucket
		}
		return nil
	default:
		return errUnknownStorage
	}
}

var (
	errNoBucket       = &configError{"S3_BUCKET not set"}
	errUnknownStorage = &configError{"unknown storage type"}
)

type configError struct{ msg string }

func (e *configError) Error() string { return e.msg }

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
