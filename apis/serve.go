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
	quotaLimiter          *rateLimiter
	realtimeLimiter       *rateLimiter
	realtime              *realtimeHub
	realtimeSourceID      string
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
		quotaLimiter:          newRateLimiter(0, time.Minute),
		realtimeLimiter:       newRateLimiter(60, time.Minute),
		realtime:              newRealtimeHub(),
		realtimeSourceID:      newRealtimeSourceID(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /ready", s.ready)
	mux.Handle("POST /setup", s.limitByIP("setup", s.setupLimiter, http.HandlerFunc(s.setup)))
	mux.Handle("POST /admin/api/auth/login", s.limitByIP("login", s.loginLimiter, http.HandlerFunc(s.adminLogin)))
	mux.Handle("POST /admin/api/auth/logout", s.requireAdmin(http.HandlerFunc(s.adminLogout)))
	mux.Handle("POST /admin/api/auth/change-password", s.limitByIP("admin-change-password", s.changePasswordLimiter, s.requireAdmin(http.HandlerFunc(s.adminChangePassword))))
	mux.Handle("PATCH /admin/api/auth/email", s.requireAdminReady(http.HandlerFunc(s.adminChangeEmail)))
	mux.Handle("GET /admin/api/me", s.requireAdmin(http.HandlerFunc(s.adminMe)))
	mux.Handle("GET /admin/api/admins", s.requireAdminReady(http.HandlerFunc(s.adminListAdmins)))
	mux.Handle("POST /admin/api/admins", s.requireAdminReady(http.HandlerFunc(s.adminCreateAdmin)))
	mux.Handle("GET /admin/api/projects", s.requireAdminReady(http.HandlerFunc(s.adminListProjects)))
	mux.Handle("POST /admin/api/projects", s.requireAdminReady(http.HandlerFunc(s.adminCreateProject)))
	mux.Handle("GET /admin/api/projects/{slug}", s.requireAdminReady(http.HandlerFunc(s.adminGetProject)))
	mux.Handle("PUT /admin/api/projects/{slug}/cors", s.requireAdminReady(http.HandlerFunc(s.adminUpdateProjectCORS)))
	mux.Handle("GET /admin/api/projects/{slug}/auth-settings", s.requireAdminReady(http.HandlerFunc(s.adminGetProjectAuthSettings)))
	mux.Handle("PUT /admin/api/projects/{slug}/auth-settings", s.requireAdminReady(http.HandlerFunc(s.adminUpdateProjectAuthSettings)))
	mux.Handle("GET /admin/api/projects/{slug}/quotas", s.requireAdminReady(http.HandlerFunc(s.adminGetProjectQuotas)))
	mux.Handle("PUT /admin/api/projects/{slug}/quotas", s.requireAdminReady(http.HandlerFunc(s.adminUpdateProjectQuotas)))
	mux.Handle("GET /admin/api/projects/{slug}/metrics", s.requireAdminReady(http.HandlerFunc(s.adminGetProjectMetrics)))
	mux.Handle("GET /admin/api/projects/{slug}/insights", s.requireAdminReady(http.HandlerFunc(s.adminGetProjectInsights)))
	mux.Handle("GET /admin/api/projects/{slug}/collections/{name}/insights", s.requireAdminReady(http.HandlerFunc(s.adminGetCollectionInsights)))
	mux.Handle("GET /admin/api/projects/{slug}/ops/alerts", s.requireAdminReady(http.HandlerFunc(s.adminListOpsAlerts)))
	mux.Handle("POST /admin/api/projects/{slug}/ops/alerts/{id}/resolve", s.requireAdminReady(http.HandlerFunc(s.adminResolveOpsAlert)))
	mux.Handle("GET /admin/api/projects/{slug}/webhooks", s.requireAdminReady(http.HandlerFunc(s.adminListWebhooks)))
	mux.Handle("POST /admin/api/projects/{slug}/webhooks", s.requireAdminReady(http.HandlerFunc(s.adminCreateWebhook)))
	mux.Handle("DELETE /admin/api/projects/{slug}/webhooks/{id}", s.requireAdminReady(http.HandlerFunc(s.adminDeleteWebhook)))
	mux.Handle("GET /admin/api/projects/{slug}/webhooks/{id}/deliveries", s.requireAdminReady(http.HandlerFunc(s.adminListWebhookDeliveries)))
	mux.Handle("GET /admin/api/projects/{slug}/api-keys", s.requireAdminReady(http.HandlerFunc(s.adminListAPIKeys)))
	mux.Handle("POST /admin/api/projects/{slug}/api-keys", s.requireAdminReady(http.HandlerFunc(s.adminCreateAPIKey)))
	mux.Handle("DELETE /admin/api/projects/{slug}/api-keys/{id}", s.requireAdminReady(http.HandlerFunc(s.adminRevokeAPIKey)))
	mux.Handle("GET /admin/api/projects/{slug}/collections/export", s.requireAdminReady(http.HandlerFunc(s.adminExportCollections)))
	mux.Handle("POST /admin/api/projects/{slug}/collections/import", s.requireAdminReady(http.HandlerFunc(s.adminImportCollections)))
	mux.Handle("GET /admin/api/projects/{slug}/schema/discover", s.requireAdminReady(http.HandlerFunc(s.adminDiscoverSchema)))
	mux.Handle("POST /admin/api/projects/{slug}/schema/import", s.requireAdminReady(http.HandlerFunc(s.adminImportSchemaTables)))
	mux.Handle("GET /admin/api/projects/{slug}/schema/versions", s.requireAdminReady(http.HandlerFunc(s.adminListSchemaVersions)))
	mux.Handle("POST /admin/api/projects/{slug}/schema/versions", s.requireAdminReady(http.HandlerFunc(s.adminCreateSchemaVersion)))
	mux.Handle("GET /admin/api/projects/{slug}/schema/versions/{id}", s.requireAdminReady(http.HandlerFunc(s.adminGetSchemaVersion)))
	mux.Handle("GET /admin/api/projects/{slug}/sdk/typescript", s.requireAdminReady(http.HandlerFunc(s.adminGenerateTypeScriptSDK)))
	mux.Handle("POST /admin/api/projects/{slug}/sql", s.requireAdminReady(http.HandlerFunc(s.adminRunSQL)))
	mux.Handle("GET /admin/api/audit-log", s.requireAdminReady(http.HandlerFunc(s.adminListAuditLog)))
	mux.Handle("DELETE /admin/api/audit-log", s.requireAdminReady(http.HandlerFunc(s.adminClearAuditLog)))
	mux.Handle("GET /admin/api/request-logs", s.requireAdminReady(http.HandlerFunc(s.adminListRequestLogs)))
	mux.Handle("DELETE /admin/api/request-logs", s.requireAdminReady(http.HandlerFunc(s.adminClearRequestLogs)))
	mux.Handle("GET /admin/api/settings", s.requireAdminReady(http.HandlerFunc(s.adminGetSettings)))
	mux.Handle("PUT /admin/api/settings/smtp", s.requireAdminReady(http.HandlerFunc(s.adminUpdateSMTPSettings)))
	mux.Handle("POST /admin/api/settings/smtp/test", s.requireAdminReady(http.HandlerFunc(s.adminTestSMTPSettings)))
	mux.Handle("PUT /admin/api/settings/storage", s.requireAdminReady(http.HandlerFunc(s.adminUpdateStorageSettings)))
	mux.Handle("POST /admin/api/settings/storage/test", s.requireAdminReady(http.HandlerFunc(s.adminTestStorageSettings)))
	mux.Handle("PUT /admin/api/settings/cors", s.requireAdminReady(http.HandlerFunc(s.adminUpdateCORSSettings)))
	mux.Handle("PUT /admin/api/settings/logs", s.requireAdminReady(http.HandlerFunc(s.adminUpdateLogSettings)))
	mux.Handle("GET /admin/api/cron-jobs", s.requireAdminReady(http.HandlerFunc(s.adminListCronJobs)))
	mux.Handle("POST /admin/api/cron-jobs", s.requireAdminReady(http.HandlerFunc(s.adminCreateCronJob)))
	mux.Handle("PATCH /admin/api/cron-jobs/{id}", s.requireAdminReady(http.HandlerFunc(s.adminUpdateCronJob)))
	mux.Handle("DELETE /admin/api/cron-jobs/{id}", s.requireAdminReady(http.HandlerFunc(s.adminDeleteCronJob)))
	mux.Handle("GET /admin/api/cron-jobs/{id}/runs", s.requireAdminReady(http.HandlerFunc(s.adminListCronRuns)))
	mux.Handle("POST /admin/api/cron-jobs/{id}/run", s.requireAdminReady(http.HandlerFunc(s.adminRunCronJob)))
	mux.Handle("GET /admin/api/backups", s.requireAdminReady(http.HandlerFunc(s.adminListBackupJobs)))
	mux.Handle("POST /admin/api/backups", s.requireAdminReady(http.HandlerFunc(s.adminCreateBackupJob)))
	mux.Handle("GET /admin/api/backups/{id}/runs", s.requireAdminReady(http.HandlerFunc(s.adminListBackupRuns)))
	mux.Handle("POST /admin/api/backups/{id}/run", s.requireAdminReady(http.HandlerFunc(s.adminRunBackupJob)))
	mux.Handle("GET /admin/api/backups/{id}/runs/{runId}/download", s.requireAdminReady(http.HandlerFunc(s.adminDownloadBackupRun)))
	mux.Handle("POST /admin/api/restores", s.requireAdminReady(http.HandlerFunc(s.adminRestoreBackup)))
	mux.Handle("GET /admin/api/mcp/tokens", s.requireAdminReady(http.HandlerFunc(s.adminListMCPTokens)))
	mux.Handle("POST /admin/api/mcp/tokens", s.requireAdminReady(http.HandlerFunc(s.adminCreateMCPToken)))
	mux.Handle("DELETE /admin/api/mcp/tokens/{id}", s.requireAdminReady(http.HandlerFunc(s.adminRevokeMCPToken)))
	mux.Handle("POST /mcp", s.limitByIP("mcp", s.mcpLimiter, http.HandlerFunc(s.mcp)))
	mux.Handle("GET /auth/verify", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.authVerifyPage)))
	mux.Handle("POST /auth/verify", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.authVerifySubmit)))
	mux.Handle("GET /auth/reset-password", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.authResetPasswordPage)))
	mux.Handle("POST /auth/reset-password", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.authResetPasswordSubmit)))
	mux.Handle("GET /auth/email-change", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.authEmailChangePage)))
	mux.Handle("POST /auth/email-change", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.authEmailChangeSubmit)))
	mux.Handle("POST /api/projects/{slug}/auth/signup", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appSignup)))
	mux.Handle("POST /api/projects/{slug}/auth/login", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appLogin)))
	mux.Handle("POST /api/projects/{slug}/auth/request-otp", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appRequestOTP)))
	mux.Handle("POST /api/projects/{slug}/auth/login-otp", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appLoginOTP)))
	mux.Handle("GET /api/projects/{slug}/auth/oauth/{provider}/start", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appOAuthStart)))
	mux.Handle("GET /api/projects/{slug}/auth/oauth/{provider}/callback", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appOAuthCallback)))
	mux.Handle("POST /api/projects/{slug}/auth/mfa/enroll", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appMFAEnroll)))
	mux.Handle("POST /api/projects/{slug}/auth/mfa/confirm", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appMFAConfirm)))
	mux.Handle("POST /api/projects/{slug}/auth/mfa/verify", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appMFAVerify)))
	mux.Handle("POST /api/projects/{slug}/auth/mfa/recovery", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appMFARecovery)))
	mux.Handle("POST /api/projects/{slug}/auth/mfa/disable", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appMFADisable)))
	mux.Handle("POST /api/projects/{slug}/auth/refresh", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appRefresh)))
	mux.Handle("POST /api/projects/{slug}/auth/logout", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appLogout)))
	mux.Handle("POST /api/projects/{slug}/auth/logout-all", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appLogoutAll)))
	mux.Handle("GET /api/projects/{slug}/auth/sessions", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appListSessions)))
	mux.Handle("DELETE /api/projects/{slug}/auth/sessions/{sessionId}", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appRevokeSession)))
	mux.Handle("GET /api/projects/{slug}/auth/me", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appMe)))
	mux.Handle("POST /api/projects/{slug}/auth/request-email-change", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appRequestEmailChange)))
	mux.Handle("POST /api/projects/{slug}/auth/confirm-email-change", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appConfirmEmailChange)))
	mux.Handle("POST /api/projects/{slug}/auth/request-verification", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appRequestVerification)))
	mux.Handle("POST /api/projects/{slug}/auth/confirm-verification", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appConfirmVerification)))
	mux.Handle("POST /api/projects/{slug}/auth/request-password-reset", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appRequestPasswordReset)))
	mux.Handle("POST /api/projects/{slug}/auth/confirm-password-reset", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appConfirmPasswordReset)))
	mux.Handle("GET /api/projects/{slug}/orgs", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appListOrganizations)))
	mux.Handle("POST /api/projects/{slug}/orgs", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appCreateOrganization)))
	mux.Handle("GET /api/projects/{slug}/orgs/{orgId}/members", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appListOrganizationMembers)))
	mux.Handle("POST /api/projects/{slug}/orgs/{orgId}/invitations", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appCreateOrganizationInvitation)))
	mux.Handle("POST /api/projects/{slug}/org-invitations/accept", s.limitByIP("app-auth", s.authLimiter, http.HandlerFunc(s.appAcceptOrganizationInvitation)))
	mux.Handle("GET /api/projects/{slug}/realtime", s.limitByIP("realtime", s.realtimeLimiter, http.HandlerFunc(s.realtimeStream)))
	mux.Handle("GET /api/projects/{slug}/realtime/ws", s.limitByIP("realtime", s.realtimeLimiter, http.HandlerFunc(s.realtimeWebSocket)))
	mux.HandleFunc("POST /api/projects/{slug}/batch", s.batchRecords)
	mux.Handle("GET /api/projects/{slug}/collections", s.requireAdminReady(http.HandlerFunc(s.listCollections)))
	mux.Handle("POST /api/projects/{slug}/collections", s.requireAdminReady(http.HandlerFunc(s.createCollection)))
	mux.Handle("GET /api/projects/{slug}/collections/{name}", s.requireAdminReady(http.HandlerFunc(s.getCollection)))
	mux.Handle("PATCH /api/projects/{slug}/collections/{name}", s.requireAdminReady(http.HandlerFunc(s.updateCollection)))
	mux.Handle("DELETE /api/projects/{slug}/collections/{name}", s.requireAdminReady(http.HandlerFunc(s.deleteCollection)))
	mux.HandleFunc("GET /api/projects/{slug}/collections/{name}/records/export", s.exportRecords)
	mux.HandleFunc("GET /api/projects/{slug}/collections/{name}/records/aggregate/export", s.exportAggregate)
	mux.HandleFunc("GET /api/projects/{slug}/collections/{name}/records/aggregate", s.aggregateRecords)
	mux.HandleFunc("GET /api/projects/{slug}/collections/{name}/records", s.listRecords)
	mux.HandleFunc("POST /api/projects/{slug}/collections/{name}/records", s.createRecord)
	mux.HandleFunc("GET /api/projects/{slug}/collections/{name}/records/{id}/history", s.recordHistory)
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

	srv := &http.Server{
		Addr:              app.Config.Addr(),
		Handler:           s.withMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	// Deliver a swept event exactly as the request path would have.
	app.OutboxPublish = func(ctx context.Context, project core.Project, event core.OutboxEvent) error {
		var record core.Record
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &record)
		}
		action := map[string]string{"insert": realtimeActionCreate, "update": realtimeActionUpdate, "delete": realtimeActionDelete}[event.Action]
		if action == "" {
			return nil
		}
		s.publishRealtimeRecord(ctx, project.Slug, event.Collection, action, event.RecordID, record)
		return core.EnqueueRecordWebhookDeliveries(ctx, s.app.Pool, project.Slug, event.Collection, action, event.RecordID, record)
	}

	fanoutCtx, cancelFanout := context.WithCancel(context.Background())
	srv.RegisterOnShutdown(cancelFanout)
	go s.runRealtimeFanout(fanoutCtx)
	return srv
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
				setAdminUICacheHeaders(w, p)
				fileServer.ServeHTTP(w, r)
				return
			}
			if !isAdminUIPath(p) || rejectsSPAFallback(p) {
				http.NotFound(w, r)
				return
			}
		}
		setAdminUICacheHeaders(w, "index.html")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

func setAdminUICacheHeaders(w http.ResponseWriter, p string) {
	if p == "" || p == "index.html" || strings.HasSuffix(p, ".html") || strings.HasSuffix(p, ".txt") {
		w.Header().Set("Cache-Control", "no-store")
		return
	}
	if strings.HasPrefix(p, "_next/static/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
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

// ready is 503 until startup work has completed, then 200 once the app can serve traffic.
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
