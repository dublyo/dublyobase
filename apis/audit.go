package apis

import (
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) adminListAuditLog(w http.ResponseWriter, r *http.Request) {
	result, err := core.ListAuditLogFiltered(
		r.Context(),
		s.app.Pool,
		core.AuditLogFilter{
			Project: r.URL.Query().Get("project"),
			Action:  r.URL.Query().Get("action"),
			Target:  r.URL.Query().Get("target"),
			Search:  r.URL.Query().Get("search"),
			Page:    queryInt(r, "page", 1),
			PerPage: queryInt(r, "perPage", 30),
		},
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
