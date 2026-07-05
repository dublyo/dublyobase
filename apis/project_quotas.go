package apis

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) adminGetProjectQuotas(w http.ResponseWriter, r *http.Request) {
	quotas, err := core.GetProjectQuotas(r.Context(), s.app.Pool, r.PathValue("slug"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, quotas)
}

func (s *server) adminUpdateProjectQuotas(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	var req core.ProjectQuotasInput
	if !decodeJSON(w, r, &req) {
		return
	}
	quotas, err := core.UpdateProjectQuotas(r.Context(), s.app.Pool, auth.Admin.ID, r.PathValue("slug"), req, s.clientIP(r), r.UserAgent())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, quotas)
}

func (s *server) adminGetProjectMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := core.GetProjectMetrics(r.Context(), s.app.Pool, r.PathValue("slug"), queryInt(r, "hours", 24), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *server) checkProjectQuota(w http.ResponseWriter, r *http.Request, slug string, authEndpoint bool) bool {
	quotas, err := core.GetProjectQuotas(r.Context(), s.app.Pool, slug)
	if err != nil {
		writeCoreError(w, err)
		return false
	}
	if !quotas.Enabled {
		return true
	}
	if authEndpoint && quotas.AuthRequestsPerMinute > 0 {
		key := fmt.Sprintf("project-auth:%s:%s", quotas.ProjectSlug, s.clientIP(r))
		if !s.quotaLimiter.allowLimit(key, quotas.AuthRequestsPerMinute) {
			writeCoreError(w, fmt.Errorf("%w: project auth request limit exceeded", core.ErrQuotaExceeded))
			return false
		}
	}
	if quotas.RequestsPerMinute > 0 {
		key := fmt.Sprintf("project-public:%s:%s", quotas.ProjectSlug, s.clientIP(r))
		if !s.quotaLimiter.allowLimit(key, quotas.RequestsPerMinute) {
			writeCoreError(w, fmt.Errorf("%w: project request limit exceeded", core.ErrQuotaExceeded))
			return false
		}
	}
	return true
}
