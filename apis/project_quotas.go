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

func (s *server) checkResolvedProjectQuota(w http.ResponseWriter, r *http.Request, auth *core.RecordAuth) bool {
	if auth == nil {
		return true
	}
	quotas, err := core.GetProjectQuotas(r.Context(), s.app.Pool, auth.Project.Slug)
	if err != nil {
		writeCoreError(w, err)
		return false
	}
	if !quotas.Enabled || quotas.RequestsPerMinute <= 0 {
		return true
	}
	principal := s.quotaPrincipal(r, auth)
	if principal == "" {
		return true
	}
	key := fmt.Sprintf("project-principal:%s:%s", quotas.ProjectSlug, principal)
	if !s.quotaLimiter.allowLimit(key, quotas.RequestsPerMinute) {
		writeCoreError(w, fmt.Errorf("%w: project principal request limit exceeded", core.ErrQuotaExceeded))
		return false
	}
	return true
}

func (s *server) quotaPrincipal(r *http.Request, auth *core.RecordAuth) string {
	token := bearerToken(r)
	if token != "" {
		hash := core.HashToken(token)
		if len(hash) > 16 {
			hash = hash[:16]
		}
		return string(auth.Role) + ":token:" + hash
	}
	if auth.Subject != "" {
		return string(auth.Role) + ":user:" + auth.Subject
	}
	if ip := s.clientIP(r); ip != "" {
		return string(auth.Role) + ":ip:" + ip
	}
	return string(auth.Role)
}
