package apis

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/dublyo/dublyobase/core"
)

type adminAuthKey struct{}

func (s *server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		auth, err := core.FindAdminByToken(r.Context(), s.app.Pool, token, time.Now())
		if err != nil {
			writeCoreError(w, err)
			return
		}
		ctx := context.WithValue(r.Context(), adminAuthKey{}, auth)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *server) requireAdminReady(next http.Handler) http.Handler {
	return s.requireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := adminAuth(r)
		if auth == nil {
			writeCoreError(w, core.ErrUnauthorized)
			return
		}
		if auth.Admin.MustChangePassword {
			writeCoreError(w, core.ErrPasswordChangeRequired)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func bearerToken(r *http.Request) string {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if authz == "" {
		return ""
	}
	typ, token, ok := strings.Cut(authz, " ")
	if !ok || !strings.EqualFold(typ, "bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func adminAuth(r *http.Request) *core.AdminAuth {
	auth, _ := r.Context().Value(adminAuthKey{}).(*core.AdminAuth)
	return auth
}
