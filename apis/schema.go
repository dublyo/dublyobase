package apis

import (
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) adminDiscoverSchema(w http.ResponseWriter, r *http.Request) {
	result, err := core.DiscoverSchemaTables(r.Context(), s.app.Pool, r.PathValue("slug"), core.SchemaDiscoveryInput{
		Schema: r.URL.Query().Get("schema"),
		Table:  r.URL.Query().Get("table"),
	})
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) adminImportSchemaTables(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	var input core.SchemaImportInput
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := core.ImportSchemaTables(
		r.Context(),
		s.app.Pool,
		auth.Admin.ID,
		r.PathValue("slug"),
		input,
		s.clientIP(r),
		r.UserAgent(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
