package apis

import (
	"net/http"
	"time"

	"github.com/dublyo/dublyobase/core"
)

type refreshTokenRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type emailRequest struct {
	Email string `json:"email"`
}

type confirmVerificationRequest struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

type confirmPasswordResetRequest struct {
	Email    string `json:"email"`
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (s *server) appSignup(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := core.SignupAppUser(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		r.PathValue("slug"),
		req.Email,
		req.Password,
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *server) appLogin(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := core.LoginAppUser(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		r.PathValue("slug"),
		req.Email,
		req.Password,
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) appRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshTokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := core.RefreshAppSession(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		r.PathValue("slug"),
		req.RefreshToken,
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) appLogout(w http.ResponseWriter, r *http.Request) {
	var req refreshTokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := core.LogoutAppSession(
		r.Context(),
		s.app.Pool,
		r.PathValue("slug"),
		req.RefreshToken,
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	); err != nil {
		writeCoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) appLogoutAll(w http.ResponseWriter, r *http.Request) {
	project, user, err := core.ResolveAppAccessToken(r.Context(), s.app.Pool, s.app.Config, r.PathValue("slug"), bearerToken(r), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	if err := core.LogoutAllAppSessions(
		r.Context(),
		s.app.Pool,
		project,
		user.ID,
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	); err != nil {
		writeCoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) appMe(w http.ResponseWriter, r *http.Request) {
	_, user, err := core.ResolveAppAccessToken(r.Context(), s.app.Pool, s.app.Config, r.PathValue("slug"), bearerToken(r), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *server) appRequestVerification(w http.ResponseWriter, r *http.Request) {
	var req emailRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := core.RequestEmailVerification(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		r.PathValue("slug"),
		req.Email,
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *server) appConfirmVerification(w http.ResponseWriter, r *http.Request) {
	var req confirmVerificationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	user, err := core.ConfirmEmailVerification(
		r.Context(),
		s.app.Pool,
		r.PathValue("slug"),
		req.Email,
		req.Token,
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (s *server) appRequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req emailRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := core.RequestPasswordReset(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		r.PathValue("slug"),
		req.Email,
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *server) appConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req confirmPasswordResetRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	user, err := core.ConfirmPasswordReset(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		r.PathValue("slug"),
		req.Email,
		req.Token,
		req.Password,
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}
