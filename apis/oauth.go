package apis

import (
	"net/http"
	"strings"
	"time"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) appOAuthStart(w http.ResponseWriter, r *http.Request) {
	if !s.checkProjectQuota(w, r, r.PathValue("slug"), true) {
		return
	}
	result, err := core.StartOAuthLogin(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		r.PathValue("slug"),
		r.PathValue("provider"),
		r.URL.Query().Get("redirect"),
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, result)
		return
	}
	http.Redirect(w, r, result.AuthorizationURL, http.StatusFound)
}

func (s *server) appOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if providerErr := strings.TrimSpace(r.URL.Query().Get("error")); providerErr != "" {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	result, err := core.CompleteOAuthLogin(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		r.PathValue("slug"),
		r.PathValue("provider"),
		r.URL.Query().Get("code"),
		r.URL.Query().Get("state"),
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	if target := core.OAuthRedirectWithFragment(result.RedirectURL, result.Auth); target != "" {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, result)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(core.OAuthJSONPage(result))
}

func wantsJSON(r *http.Request) bool {
	if strings.EqualFold(r.URL.Query().Get("format"), "json") {
		return true
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(accept, "application/json")
}
