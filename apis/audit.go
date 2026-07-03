package apis

import (
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) adminListAuditLog(w http.ResponseWriter, r *http.Request) {
	result, err := core.ListAuditLog(
		r.Context(),
		s.app.Pool,
		r.URL.Query().Get("project"),
		queryInt(r, "page", 1),
		queryInt(r, "perPage", 30),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
