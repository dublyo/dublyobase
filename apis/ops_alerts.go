package apis

import (
	"net/http"
	"time"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) adminListOpsAlerts(w http.ResponseWriter, r *http.Request) {
	var (
		items []core.OpsAlert
		err   error
	)
	if queryBool(r, "refresh") {
		items, err = core.RefreshProjectOpsAlerts(r.Context(), s.app.Pool, r.PathValue("slug"), time.Now())
	} else {
		items, err = core.ListOpsAlerts(r.Context(), s.app.Pool, r.PathValue("slug"), queryInt(r, "limit", 50))
	}
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) adminResolveOpsAlert(w http.ResponseWriter, r *http.Request) {
	if err := core.ResolveOpsAlert(r.Context(), s.app.Pool, r.PathValue("slug"), r.PathValue("id"), time.Now()); err != nil {
		writeCoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
