// Package apis wires the HTTP surface: the admin SPA and the REST/realtime API.
package apis

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/dublyobase/dublyobase/core"
	"github.com/dublyobase/dublyobase/ui"
)

// NewServer builds the HTTP server for an App. For M0 it exposes /api/health
// and serves the embedded admin UI shell; feature routes arrive in later phases.
func NewServer(app *core.App) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		clusters := make([]map[string]any, 0)
		for _, c := range app.Supervisor.Clusters() {
			clusters = append(clusters, map[string]any{
				"version": c.Version.String(),
				"port":    c.Port,
				"running": c.Running(),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":   "ok",
			"app":      app.Settings.AppName,
			"clusters": clusters,
		})
	})

	mux.Handle("/", http.FileServer(http.FS(ui.DistFS())))

	return &http.Server{
		Addr:              app.Settings.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
