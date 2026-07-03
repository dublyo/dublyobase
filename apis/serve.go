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
	app *core.App
}

// NewServer builds the HTTP server for an App.
func NewServer(app *core.App) *http.Server {
	s := &server{app: app}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /ready", s.ready)
	mux.Handle("/", spaHandler(ui.DistFS()))

	return &http.Server{
		Addr:              app.Config.Addr(),
		Handler:           s.withMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// spaHandler serves the embedded admin SPA: real files are served as-is;
// any other path falls back to index.html so client-side routes deep-link.
func spaHandler(dist fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
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
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

func isReservedAPIPath(p string) bool {
	return p == "api" || strings.HasPrefix(p, "api/") ||
		p == "admin/api" || strings.HasPrefix(p, "admin/api/")
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
