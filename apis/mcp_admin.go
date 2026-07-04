package apis

import (
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) adminListMCPTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := core.ListMCPTokens(r.Context(), s.app.Pool)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": tokens})
}

func (s *server) adminCreateMCPToken(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	var input core.MCPTokenInput
	if !decodeJSON(w, r, &input) {
		return
	}
	token, err := core.CreateMCPToken(r.Context(), s.app.Pool, auth.Admin.ID, input, s.clientIP(r), r.UserAgent())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, token)
}

func (s *server) adminRevokeMCPToken(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if err := core.RevokeMCPToken(r.Context(), s.app.Pool, auth.Admin.ID, r.PathValue("id"), s.clientIP(r), r.UserAgent()); err != nil {
		writeCoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
