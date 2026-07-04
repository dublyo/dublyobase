package apis

import (
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) adminGetProjectAuthSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := core.GetProjectAuthSettings(r.Context(), s.app.Pool, r.PathValue("slug"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *server) adminUpdateProjectAuthSettings(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	var req core.ProjectAuthSettingsInput
	if !decodeJSON(w, r, &req) {
		return
	}
	settings, err := core.UpdateProjectAuthSettings(r.Context(), s.app.Pool, auth.Admin.ID, r.PathValue("slug"), req, s.clientIP(r), r.UserAgent())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}
