package apis

import (
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

type apiKeyRequest struct {
	Name string          `json:"name"`
	Type core.APIKeyType `json:"type"`
}

func (s *server) adminListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := core.ListAPIKeys(r.Context(), s.app.Pool, r.PathValue("slug"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": keys})
}

func (s *server) adminCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	var req apiKeyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	key, err := core.CreateAPIKey(
		r.Context(),
		s.app.Pool,
		auth.Admin.ID,
		r.PathValue("slug"),
		req.Name,
		req.Type,
		s.clientIP(r),
		r.UserAgent(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, key)
}

func (s *server) adminRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	if err := core.RevokeAPIKey(
		r.Context(),
		s.app.Pool,
		auth.Admin.ID,
		r.PathValue("slug"),
		r.PathValue("id"),
		s.clientIP(r),
		r.UserAgent(),
	); err != nil {
		writeCoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
