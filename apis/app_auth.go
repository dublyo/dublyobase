package apis

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
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
	s.sendAuthTokenEmail(r, result.User.Email, "verify_email")
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
	s.deliverAuthTokenEmail(r.Context(), r.PathValue("slug"), result)
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
	s.deliverAuthTokenEmail(r.Context(), r.PathValue("slug"), result)
	writeJSON(w, http.StatusAccepted, result)
}

func (s *server) sendAuthTokenEmail(r *http.Request, email string, tokenType string) {
	var result *core.AuthTokenRequestResult
	var err error
	now := time.Now()
	projectSlug := r.PathValue("slug")
	switch tokenType {
	case "verify_email":
		result, err = core.RequestEmailVerification(r.Context(), s.app.Pool, s.app.Config, projectSlug, email, s.clientIP(r), r.UserAgent(), now)
	case "password_reset":
		result, err = core.RequestPasswordReset(r.Context(), s.app.Pool, s.app.Config, projectSlug, email, s.clientIP(r), r.UserAgent(), now)
	default:
		err = core.ErrValidation
	}
	if err != nil {
		s.app.Log.Warn("auth email token creation failed", "project", projectSlug, "type", tokenType, "err", err)
		return
	}
	s.deliverAuthTokenEmail(r.Context(), projectSlug, result)
}

func (s *server) deliverAuthTokenEmail(ctx context.Context, projectSlug string, result *core.AuthTokenRequestResult) {
	if result == nil || result.Token == "" {
		return
	}
	mailer := s.app.Mailer
	msgCfg := s.app.Config
	if mailer == nil {
		effectiveCfg, err := core.EffectiveSMTPConfig(ctx, s.app.Pool, s.app.Config)
		if err != nil {
			s.app.Log.Warn("auth email settings load failed", "project", projectSlug, "type", result.Type, "err", err)
			return
		}
		msgCfg = effectiveCfg
		mailer = core.NewMailer(effectiveCfg)
	}
	msg, err := core.BuildAuthTokenEmail(msgCfg, result.Type, projectSlug, result.ProjectName, result.Email, result.Token)
	if err != nil {
		s.app.Log.Warn("auth email build failed", "project", projectSlug, "type", result.Type, "err", err)
		return
	}
	if err := mailer.Send(ctx, msg); err != nil {
		s.app.Log.Warn("auth email send failed", "project", projectSlug, "type", result.Type, "err", err)
	}
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

func (s *server) authVerifyPage(w http.ResponseWriter, r *http.Request) {
	input := authActionInput{
		Project: r.URL.Query().Get("project"),
		Email:   r.URL.Query().Get("email"),
		Token:   r.URL.Query().Get("token"),
	}
	s.writeAuthActionPage(w, http.StatusOK, authActionPageData{
		Kind:        "verify",
		Project:     input.Project,
		ProjectName: s.authActionProjectName(r.Context(), input.Project),
		Email:       input.Email,
		Token:       input.Token,
	})
}

func (s *server) authVerifySubmit(w http.ResponseWriter, r *http.Request) {
	input, ok := parseAuthActionForm(w, r)
	if !ok {
		return
	}
	user, err := core.ConfirmEmailVerification(
		r.Context(),
		s.app.Pool,
		input.Project,
		input.Email,
		input.Token,
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	)
	data := authActionPageData{
		Kind:        "verify",
		Project:     input.Project,
		ProjectName: s.authActionProjectName(r.Context(), input.Project),
		Email:       input.Email,
		Token:       input.Token,
	}
	if err != nil {
		data.Error = "This verification link is invalid or expired."
		s.writeAuthActionPage(w, http.StatusUnauthorized, data)
		return
	}
	data.Email = user.Email
	data.Success = "Your email has been verified."
	s.writeAuthActionPage(w, http.StatusOK, data)
}

func (s *server) authResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	input := authActionInput{
		Project: r.URL.Query().Get("project"),
		Email:   r.URL.Query().Get("email"),
		Token:   r.URL.Query().Get("token"),
	}
	s.writeAuthActionPage(w, http.StatusOK, authActionPageData{
		Kind:        "reset",
		Project:     input.Project,
		ProjectName: s.authActionProjectName(r.Context(), input.Project),
		Email:       input.Email,
		Token:       input.Token,
	})
}

func (s *server) authResetPasswordSubmit(w http.ResponseWriter, r *http.Request) {
	input, ok := parseAuthActionForm(w, r)
	if !ok {
		return
	}
	password := r.FormValue("password")
	user, err := core.ConfirmPasswordReset(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		input.Project,
		input.Email,
		input.Token,
		password,
		s.clientIP(r),
		r.UserAgent(),
		time.Now(),
	)
	data := authActionPageData{
		Kind:        "reset",
		Project:     input.Project,
		ProjectName: s.authActionProjectName(r.Context(), input.Project),
		Email:       input.Email,
		Token:       input.Token,
	}
	if err != nil {
		data.Error = "This reset link is invalid, expired, or the new password is not valid."
		s.writeAuthActionPage(w, http.StatusUnauthorized, data)
		return
	}
	data.Email = user.Email
	data.Success = "Your password has been changed."
	s.writeAuthActionPage(w, http.StatusOK, data)
}

type authActionInput struct {
	Project string
	Email   string
	Token   string
}

type authActionPageData struct {
	Kind        string
	Project     string
	ProjectName string
	Email       string
	Token       string
	Success     string
	Error       string
}

func parseAuthActionForm(w http.ResponseWriter, r *http.Request) (authActionInput, bool) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "bad_form", "invalid form submission")
		return authActionInput{}, false
	}
	return authActionInput{
		Project: r.FormValue("project"),
		Email:   r.FormValue("email"),
		Token:   r.FormValue("token"),
	}, true
}

func (s *server) authActionProjectName(ctx context.Context, projectSlug string) string {
	project, err := core.GetProject(ctx, s.app.Pool, projectSlug)
	if err == nil && strings.TrimSpace(project.Name) != "" {
		return project.Name
	}
	return core.NormalizeProjectSlug(projectSlug)
}

func (s *server) writeAuthActionPage(w http.ResponseWriter, status int, data authActionPageData) {
	action := "/auth/verify"
	title := "Verify your email"
	description := "Confirm your email address to finish setting up your account."
	button := "Verify email"
	passwordField := ""
	if data.Kind == "reset" {
		action = "/auth/reset-password"
		title = "Reset password"
		description = "Choose a new password for your account."
		button = "Change password"
		passwordField = `<label>New password<input name="password" type="password" minlength="8" autocomplete="new-password" required></label>`
	}
	projectName := strings.TrimSpace(data.ProjectName)
	if projectName == "" {
		projectName = "Dublyobase app"
	}
	statusHTML := ""
	if data.Success != "" {
		statusHTML = `<div class="status success">` + html.EscapeString(data.Success) + `</div>`
	}
	if data.Error != "" {
		statusHTML = `<div class="status error">` + html.EscapeString(data.Error) + `</div>`
	}
	formHTML := ""
	if data.Success == "" {
		formHTML = fmt.Sprintf(`<form method="post" action="%s">
  <input type="hidden" name="project" value="%s">
  <label>Email<input name="email" type="email" autocomplete="email" value="%s" required></label>
  <label>Token<input name="token" value="%s" autocomplete="one-time-code" required></label>
  %s
  <button type="submit">%s</button>
</form>`,
			html.EscapeString(action),
			html.EscapeString(core.NormalizeProjectSlug(data.Project)),
			html.EscapeString(core.NormalizeEmail(data.Email)),
			html.EscapeString(strings.TrimSpace(data.Token)),
			passwordField,
			html.EscapeString(button),
		)
	}
	page := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex,nofollow">
  <title>%s</title>
  <style>
    :root { color-scheme: light dark; font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { min-height: 100vh; margin: 0; display: grid; place-items: center; background: #f4f6f8; color: #1f2328; }
    main { width: min(440px, calc(100vw - 32px)); background: #fff; border: 1px solid #d0d7de; border-radius: 8px; padding: 28px; box-shadow: 0 16px 40px rgba(31, 35, 40, .12); }
    h1 { margin: 0 0 8px; font-size: 24px; line-height: 1.2; }
    p { margin: 0 0 20px; color: #57606a; line-height: 1.5; }
    .app { margin-bottom: 18px; font-weight: 700; color: #0969da; }
    label { display: grid; gap: 6px; margin-top: 14px; font-weight: 600; font-size: 14px; }
    input { min-height: 42px; border: 1px solid #d0d7de; border-radius: 6px; padding: 0 12px; font: inherit; }
    button { width: 100%%; min-height: 44px; margin-top: 18px; border: 0; border-radius: 6px; background: #0969da; color: #fff; font: inherit; font-weight: 700; cursor: pointer; }
    .status { margin: 16px 0; padding: 12px; border-radius: 6px; line-height: 1.45; }
    .success { background: #dafbe1; color: #1a7f37; }
    .error { background: #ffebe9; color: #cf222e; }
    @media (prefers-color-scheme: dark) {
      body { background: #0d1117; color: #e6edf3; }
      main { background: #161b22; border-color: #30363d; box-shadow: none; }
      p { color: #8b949e; }
      input { background: #0d1117; color: #e6edf3; border-color: #30363d; }
    }
  </style>
</head>
<body>
  <main>
    <div class="app">%s</div>
    <h1>%s</h1>
    <p>%s</p>
    %s
    %s
  </main>
</body>
</html>`,
		html.EscapeString(title),
		html.EscapeString(projectName),
		html.EscapeString(title),
		html.EscapeString(description),
		statusHTML,
		formHTML,
	)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(page))
}
