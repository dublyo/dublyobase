package apis

import (
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

type projectRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func (s *server) adminListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := core.ListProjects(r.Context(), s.app.Pool)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": projects})
}

func (s *server) adminCreateProject(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	var req projectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	project, err := core.ProvisionProject(
		r.Context(),
		s.app.Pool,
		auth.Admin.ID,
		req.Slug,
		req.Name,
		s.clientIP(r),
		r.UserAgent(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (s *server) adminGetProject(w http.ResponseWriter, r *http.Request) {
	project, err := core.GetProject(r.Context(), s.app.Pool, r.PathValue("slug"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}
