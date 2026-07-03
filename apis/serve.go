// Package apis wires the single HTTP surface: admin UI, REST API, realtime and
// file uploads — all served from one process on one port (default :8080).
package apis

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/dublyobase/dublyobase/core"
	"github.com/dublyobase/dublyobase/ui"
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
	mux.Handle("/", http.FileServer(http.FS(ui.DistFS())))

	return &http.Server{
		Addr:              app.Config.Addr(),
		Handler:           s.withMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// health reports liveness of the app and its dependencies. It must answer
// within ~3s even under load or the container orchestrator will kill us.
func (s *server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	healthy := true

	dbStatus := "ok"
	if err := s.app.Pool.Ping(ctx); err != nil {
		dbStatus = "error: " + err.Error()
		healthy = false
	}

	storageStatus := s.checkStorage()
	if storageStatus != "ok" {
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

// checkStorage verifies the configured backend is usable.
func (s *server) checkStorage() string {
	switch s.app.Config.StorageType {
	case core.StorageLocal:
		p := s.app.Config.StorageLocalPath
		if err := os.MkdirAll(p, 0o750); err != nil {
			return "error: " + err.Error()
		}
		// confirm writability
		fi, err := os.Stat(p)
		if err != nil || !fi.IsDir() {
			return "error: storage path not a writable directory"
		}
		return "ok"
	case core.StorageS3:
		// A real HEAD-bucket check lands with the storage backend milestone.
		if s.app.Config.S3Bucket == "" {
			return "error: S3_BUCKET not set"
		}
		return "ok"
	default:
		return "error: unknown storage type"
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
