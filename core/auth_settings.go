package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProjectAuthSettings struct {
	ProjectID          string         `json:"projectId"`
	ProjectSlug        string         `json:"projectSlug"`
	AccessTokenMinutes int            `json:"accessTokenMinutes"`
	RefreshTokenDays   int            `json:"refreshTokenDays"`
	VerifyTokenHours   int            `json:"verifyTokenHours"`
	ResetTokenHours    int            `json:"resetTokenHours"`
	EmailChangeEnabled bool           `json:"emailChangeEnabled"`
	Templates          AuthTemplates  `json:"templates"`
	Providers          map[string]any `json:"providers"`
	CreatedAt          time.Time      `json:"createdAt,omitempty"`
	UpdatedAt          time.Time      `json:"updatedAt,omitempty"`
}

type AuthTemplates struct {
	VerifySubject      string `json:"verifySubject,omitempty"`
	VerifyBody         string `json:"verifyBody,omitempty"`
	ResetSubject       string `json:"resetSubject,omitempty"`
	ResetBody          string `json:"resetBody,omitempty"`
	EmailChangeSubject string `json:"emailChangeSubject,omitempty"`
	EmailChangeBody    string `json:"emailChangeBody,omitempty"`
}

type ProjectAuthSettingsInput struct {
	AccessTokenMinutes int            `json:"accessTokenMinutes"`
	RefreshTokenDays   int            `json:"refreshTokenDays"`
	VerifyTokenHours   int            `json:"verifyTokenHours"`
	ResetTokenHours    int            `json:"resetTokenHours"`
	EmailChangeEnabled *bool          `json:"emailChangeEnabled,omitempty"`
	Templates          AuthTemplates  `json:"templates"`
	Providers          map[string]any `json:"providers"`
}

func DefaultProjectAuthSettings(project *Project) *ProjectAuthSettings {
	settings := &ProjectAuthSettings{
		AccessTokenMinutes: appAccessTokenMinutesDefault,
		RefreshTokenDays:   appRefreshTokenDaysDefault,
		VerifyTokenHours:   emailVerifyTokenHoursDefault,
		ResetTokenHours:    passwordResetHoursDefault,
		EmailChangeEnabled: true,
		Templates:          defaultAuthTemplates(),
		Providers:          map[string]any{},
	}
	if project != nil {
		settings.ProjectID = project.ID
		settings.ProjectSlug = project.Slug
	}
	return settings
}

func GetProjectAuthSettings(ctx context.Context, pool *pgxpool.Pool, projectSlug string) (*ProjectAuthSettings, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	settings, err := getProjectAuthSettingsByProject(ctx, pool, project)
	if err != nil {
		return nil, err
	}
	return settings, nil
}

func UpdateProjectAuthSettings(ctx context.Context, pool *pgxpool.Pool, adminID string, projectSlug string, input ProjectAuthSettingsInput, ip string, userAgent string) (*ProjectAuthSettings, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	current, err := getProjectAuthSettingsByProject(ctx, pool, project)
	if err != nil {
		return nil, err
	}
	next := *current
	if input.AccessTokenMinutes != 0 {
		next.AccessTokenMinutes = input.AccessTokenMinutes
	}
	if input.RefreshTokenDays != 0 {
		next.RefreshTokenDays = input.RefreshTokenDays
	}
	if input.VerifyTokenHours != 0 {
		next.VerifyTokenHours = input.VerifyTokenHours
	}
	if input.ResetTokenHours != 0 {
		next.ResetTokenHours = input.ResetTokenHours
	}
	if input.EmailChangeEnabled != nil {
		next.EmailChangeEnabled = *input.EmailChangeEnabled
	}
	next.Templates = mergeAuthTemplates(defaultAuthTemplates(), input.Templates)
	if input.Providers != nil {
		next.Providers = sanitizeAuthProviders(input.Providers)
	}
	if err := validateProjectAuthSettings(&next); err != nil {
		return nil, err
	}
	templates, err := json.Marshal(next.Templates)
	if err != nil {
		return nil, err
	}
	providers, err := json.Marshal(next.Providers)
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx, `
		insert into _dbo.project_auth_settings
			(project_id, access_token_minutes, refresh_token_days, verify_token_hours, reset_token_hours, email_change_enabled, templates, providers)
		values ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb)
		on conflict (project_id) do update
		set access_token_minutes = excluded.access_token_minutes,
			refresh_token_days = excluded.refresh_token_days,
			verify_token_hours = excluded.verify_token_hours,
			reset_token_hours = excluded.reset_token_hours,
			email_change_enabled = excluded.email_change_enabled,
			templates = excluded.templates,
			providers = excluded.providers,
			updated_at = now()
		returning created_at, updated_at`,
		project.ID,
		next.AccessTokenMinutes,
		next.RefreshTokenDays,
		next.VerifyTokenHours,
		next.ResetTokenHours,
		next.EmailChangeEnabled,
		templates,
		providers,
	).Scan(&next.CreatedAt, &next.UpdatedAt); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "settings.auth.update",
		TargetType: "project",
		TargetID:   project.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data: map[string]any{
			"project":            project.Slug,
			"accessTokenMinutes": next.AccessTokenMinutes,
			"refreshTokenDays":   next.RefreshTokenDays,
			"verifyTokenHours":   next.VerifyTokenHours,
			"resetTokenHours":    next.ResetTokenHours,
			"emailChangeEnabled": next.EmailChangeEnabled,
			"oauthProviderCount": len(next.Providers),
		},
	}); err != nil {
		return nil, err
	}
	return &next, tx.Commit(ctx)
}

func getProjectAuthSettingsByProject(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, project *Project) (*ProjectAuthSettings, error) {
	settings := DefaultProjectAuthSettings(project)
	var rawTemplates []byte
	var rawProviders []byte
	err := q.QueryRow(ctx, `
		select access_token_minutes, refresh_token_days, verify_token_hours, reset_token_hours, email_change_enabled, templates, providers, created_at, updated_at
		from _dbo.project_auth_settings
		where project_id = $1`,
		project.ID,
	).Scan(&settings.AccessTokenMinutes, &settings.RefreshTokenDays, &settings.VerifyTokenHours, &settings.ResetTokenHours, &settings.EmailChangeEnabled, &rawTemplates, &rawProviders, &settings.CreatedAt, &settings.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return settings, nil
		}
		return nil, err
	}
	var templates AuthTemplates
	if len(rawTemplates) > 0 {
		_ = json.Unmarshal(rawTemplates, &templates)
	}
	settings.Templates = mergeAuthTemplates(defaultAuthTemplates(), templates)
	if len(rawProviders) > 0 {
		_ = json.Unmarshal(rawProviders, &settings.Providers)
	}
	if settings.Providers == nil {
		settings.Providers = map[string]any{}
	}
	if err := validateProjectAuthSettings(settings); err != nil {
		return DefaultProjectAuthSettings(project), nil
	}
	return settings, nil
}

func validateProjectAuthSettings(settings *ProjectAuthSettings) error {
	if settings.AccessTokenMinutes < 5 || settings.AccessTokenMinutes > 1440 {
		return fmt.Errorf("%w: accessTokenMinutes must be between 5 and 1440", ErrValidation)
	}
	if settings.RefreshTokenDays < 1 || settings.RefreshTokenDays > 365 {
		return fmt.Errorf("%w: refreshTokenDays must be between 1 and 365", ErrValidation)
	}
	if settings.VerifyTokenHours < 1 || settings.VerifyTokenHours > 168 {
		return fmt.Errorf("%w: verifyTokenHours must be between 1 and 168", ErrValidation)
	}
	if settings.ResetTokenHours < 1 || settings.ResetTokenHours > 72 {
		return fmt.Errorf("%w: resetTokenHours must be between 1 and 72", ErrValidation)
	}
	if err := validateAuthTemplate(settings.Templates.VerifySubject, 200, "verify subject"); err != nil {
		return err
	}
	if err := validateAuthTemplate(settings.Templates.VerifyBody, 8000, "verify body"); err != nil {
		return err
	}
	if err := validateAuthTemplate(settings.Templates.ResetSubject, 200, "reset subject"); err != nil {
		return err
	}
	if err := validateAuthTemplate(settings.Templates.ResetBody, 8000, "reset body"); err != nil {
		return err
	}
	if err := validateAuthTemplate(settings.Templates.EmailChangeSubject, 200, "email change subject"); err != nil {
		return err
	}
	return validateAuthTemplate(settings.Templates.EmailChangeBody, 8000, "email change body")
}

func validateAuthTemplate(value string, max int, label string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%w: %s is required", ErrValidation, label)
	}
	if len(value) > max {
		return fmt.Errorf("%w: %s is too long", ErrValidation, label)
	}
	return nil
}

func mergeAuthTemplates(base AuthTemplates, next AuthTemplates) AuthTemplates {
	if strings.TrimSpace(next.VerifySubject) != "" {
		base.VerifySubject = strings.TrimSpace(next.VerifySubject)
	}
	if strings.TrimSpace(next.VerifyBody) != "" {
		base.VerifyBody = strings.TrimSpace(next.VerifyBody)
	}
	if strings.TrimSpace(next.ResetSubject) != "" {
		base.ResetSubject = strings.TrimSpace(next.ResetSubject)
	}
	if strings.TrimSpace(next.ResetBody) != "" {
		base.ResetBody = strings.TrimSpace(next.ResetBody)
	}
	if strings.TrimSpace(next.EmailChangeSubject) != "" {
		base.EmailChangeSubject = strings.TrimSpace(next.EmailChangeSubject)
	}
	if strings.TrimSpace(next.EmailChangeBody) != "" {
		base.EmailChangeBody = strings.TrimSpace(next.EmailChangeBody)
	}
	return base
}

func defaultAuthTemplates() AuthTemplates {
	return AuthTemplates{
		VerifySubject:      "Verify your email for {APP_NAME}",
		VerifyBody:         "Verify your email for {APP_NAME}.\n\nOpen this link:\n{LINK}\n\nToken:\n{TOKEN}\n",
		ResetSubject:       "Reset your {APP_NAME} password",
		ResetBody:          "Reset your password for {APP_NAME}.\n\nOpen this link:\n{LINK}\n\nToken:\n{TOKEN}\n",
		EmailChangeSubject: "Confirm your new email for {APP_NAME}",
		EmailChangeBody:    "Confirm the new email address for {APP_NAME}.\n\nNew email: {NEW_EMAIL}\n\nOpen this link:\n{LINK}\n\nToken:\n{TOKEN}\n",
	}
}

func sanitizeAuthProviders(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || len(key) > 50 {
			continue
		}
		if strings.ContainsAny(key, "\r\n\t /\\") {
			continue
		}
		out[key] = redactProviderSecrets(value)
	}
	return out
}

func redactProviderSecrets(value any) any {
	switch raw := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(raw))
		for key, item := range raw {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "secret") || strings.Contains(lower, "password") {
				if strings.TrimSpace(fmt.Sprint(item)) != "" {
					out[key] = "[set]"
				}
				continue
			}
			out[key] = redactProviderSecrets(item)
		}
		return out
	case []any:
		out := make([]any, len(raw))
		for i := range raw {
			out[i] = redactProviderSecrets(raw[i])
		}
		return out
	default:
		return value
	}
}

func (s ProjectAuthSettings) AccessTokenTTL() time.Duration {
	return time.Duration(s.AccessTokenMinutes) * time.Minute
}

func (s ProjectAuthSettings) RefreshTokenTTL() time.Duration {
	return time.Duration(s.RefreshTokenDays) * 24 * time.Hour
}

func (s ProjectAuthSettings) VerifyTokenTTL() time.Duration {
	return time.Duration(s.VerifyTokenHours) * time.Hour
}

func (s ProjectAuthSettings) ResetTokenTTL() time.Duration {
	return time.Duration(s.ResetTokenHours) * time.Hour
}
