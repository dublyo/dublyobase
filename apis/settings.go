package apis

import (
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

type smtpTestRequest struct {
	To string `json:"to"`
}

func (s *server) adminGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := core.GetPublicInstanceSettings(r.Context(), s.app.Pool, s.app.Config)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *server) adminUpdateSMTPSettings(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	var body core.SMTPSettingsInput
	if !decodeJSON(w, r, &body) {
		return
	}
	settings, err := core.UpdateSMTPSettings(r.Context(), s.app.Pool, s.app.Config, auth.Admin.ID, body, s.clientIP(r), r.UserAgent())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *server) adminTestSMTPSettings(w http.ResponseWriter, r *http.Request) {
	var body smtpTestRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	to := core.NormalizeEmail(body.To)
	if err := core.ValidateSMTPTestRecipient(to); err != nil {
		writeCoreError(w, err)
		return
	}
	cfg, err := core.EffectiveSMTPConfig(r.Context(), s.app.Pool, s.app.Config)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	if cfg.SMTPHost == "" {
		writeCoreError(w, core.ErrValidation)
		return
	}
	mailer := core.NewMailer(cfg)
	if err := mailer.Send(r.Context(), core.MailMessage{
		From:    cfg.SMTPFrom,
		To:      to,
		Subject: "Dublyobase SMTP test",
		Text:    "Dublyobase SMTP settings are working.\n",
	}); err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent"})
}

func (s *server) adminUpdateStorageSettings(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	var body core.StorageSettingsInput
	if !decodeJSON(w, r, &body) {
		return
	}
	settings, err := core.UpdateStorageSettings(r.Context(), s.app.Pool, s.app.Config, auth.Admin.ID, body, s.clientIP(r), r.UserAgent())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *server) adminTestStorageSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := core.EffectiveStorageConfig(r.Context(), s.app.Pool, s.app.Config)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	if err := core.TestObjectStore(r.Context(), cfg); err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
