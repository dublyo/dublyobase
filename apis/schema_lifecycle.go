package apis

import (
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) adminListSchemaVersions(w http.ResponseWriter, r *http.Request) {
	items, err := core.ListSchemaVersions(r.Context(), s.app.Pool, r.PathValue("slug"), queryInt(r, "limit", 50))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) adminCreateSchemaVersion(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	var req struct {
		Label string `json:"label"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	item, err := core.CreateSchemaVersion(r.Context(), s.app.Pool, auth.Admin.ID, r.PathValue("slug"), req.Label, s.clientIP(r), r.UserAgent())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *server) adminGetSchemaVersion(w http.ResponseWriter, r *http.Request) {
	item, err := core.GetSchemaVersion(r.Context(), s.app.Pool, r.PathValue("slug"), r.PathValue("id"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *server) adminGenerateTypeScriptSDK(w http.ResponseWriter, r *http.Request) {
	source, err := core.GenerateTypeScriptSDK(r.Context(), s.app.Pool, r.PathValue("slug"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="dublyobase-client.ts"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(source))
}
