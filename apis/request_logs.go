package apis

import (
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) adminListRequestLogs(w http.ResponseWriter, r *http.Request) {
	result, err := core.ListRequestLogs(
		r.Context(),
		s.app.Pool,
		core.RequestLogFilter{
			Project: r.URL.Query().Get("project"),
			Method:  r.URL.Query().Get("method"),
			Status:  queryInt(r, "status", 0),
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
