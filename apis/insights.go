package apis

import (
	"net/http"
	"time"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) adminGetProjectInsights(w http.ResponseWriter, r *http.Request) {
	insights, err := core.GetProjectInsights(r.Context(), s.app.Pool, r.PathValue("slug"), queryInt(r, "hours", 24), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, insights)
}

func (s *server) adminGetCollectionInsights(w http.ResponseWriter, r *http.Request) {
	insights, err := core.GetCollectionInsights(r.Context(), s.app.Pool, r.PathValue("slug"), r.PathValue("name"), queryInt(r, "hours", 24), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, insights)
}
