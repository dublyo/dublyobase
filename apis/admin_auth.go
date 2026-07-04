package apis

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/dublyo/dublyobase/core"
)

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *server) setup(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	admin, err := core.CreateFirstAdminWithCost(
		r.Context(),
		s.app.Pool,
		req.Email,
		req.Password,
		s.app.Config.BcryptCost,
		s.clientIP(r),
		r.UserAgent(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"admin": admin})
}

func (s *server) adminLogin(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := core.LoginAdmin(
		r.Context(),
		s.app.Pool,
		req.Email,
		req.Password,
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	)
	if err != nil {
		if errors.Is(err, core.ErrInvalidCredentials) {
			_ = core.InsertAudit(r.Context(), s.app.Pool, core.AuditEvent{
				Action:     "admin.login_failed",
				TargetType: "admin",
				TargetID:   core.NormalizeEmail(req.Email),
				IP:         s.clientIP(r),
				UserAgent:  r.UserAgent(),
			})
		}
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":     result.Token,
		"expiresAt": result.Session.ExpiresAt,
		"admin":     result.Admin,
	})
}

func (s *server) adminLogout(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	if err := core.RevokeAdminSession(r.Context(), s.app.Pool, auth.Session.ID); err != nil {
		writeCoreError(w, err)
		return
	}
	_ = core.InsertAudit(r.Context(), s.app.Pool, core.AuditEvent{
		AdminID:    &auth.Admin.ID,
		Action:     "admin.logout",
		TargetType: "admin",
		TargetID:   auth.Admin.ID,
		IP:         s.clientIP(r),
		UserAgent:  r.UserAgent(),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) adminChangePassword(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	var req changePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	admin, err := core.ChangeAdminPassword(
		r.Context(),
		s.app.Pool,
		auth.Admin.ID,
		auth.Session.ID,
		req.CurrentPassword,
		req.NewPassword,
		s.app.Config.BcryptCost,
		s.clientIP(r),
		r.UserAgent(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"admin": admin})
}

func (s *server) adminMe(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"admin": auth.Admin,
		"session": map[string]any{
			"id":        auth.Session.ID,
			"expiresAt": auth.Session.ExpiresAt,
		},
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return false
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return false
	}
	return true
}
