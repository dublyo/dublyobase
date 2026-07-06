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
	ProjectID                   string         `json:"projectId"`
	ProjectSlug                 string         `json:"projectSlug"`
	AccessTokenMinutes          int            `json:"accessTokenMinutes"`
	RefreshTokenDays            int            `json:"refreshTokenDays"`
	VerifyTokenHours            int            `json:"verifyTokenHours"`
	ResetTokenHours             int            `json:"resetTokenHours"`
	OTPEnabled                  bool           `json:"otpEnabled"`
	OTPTokenMinutes             int            `json:"otpTokenMinutes"`
	MFAEnabled                  bool           `json:"mfaEnabled"`
	MFARequired                 bool           `json:"mfaRequired"`
	EmailChangeEnabled          bool           `json:"emailChangeEnabled"`
	EmailChangeRequiresPassword bool           `json:"emailChangeRequiresPassword"`
	Templates                   AuthTemplates  `json:"templates"`
	Providers                   map[string]any `json:"providers"`
	CreatedAt                   time.Time      `json:"createdAt,omitempty"`
	UpdatedAt                   time.Time      `json:"updatedAt,omitempty"`
}

type AuthTemplates struct {
	VerifySubject      string `json:"verifySubject,omitempty"`
	VerifyBody         string `json:"verifyBody,omitempty"`
	ResetSubject       string `json:"resetSubject,omitempty"`
	ResetBody          string `json:"resetBody,omitempty"`
	OTPSubject         string `json:"otpSubject,omitempty"`
	OTPBody            string `json:"otpBody,omitempty"`
	EmailChangeSubject string `json:"emailChangeSubject,omitempty"`
	EmailChangeBody    string `json:"emailChangeBody,omitempty"`
	InvitationSubject  string `json:"invitationSubject,omitempty"`
	InvitationBody     string `json:"invitationBody,omitempty"`
}

type ProjectAuthSettingsInput struct {
	ProjectID                   string         `json:"projectId,omitempty"`
	ProjectSlug                 string         `json:"projectSlug,omitempty"`
	AccessTokenMinutes          int            `json:"accessTokenMinutes"`
	RefreshTokenDays            int            `json:"refreshTokenDays"`
	VerifyTokenHours            int            `json:"verifyTokenHours"`
	ResetTokenHours             int            `json:"resetTokenHours"`
	OTPEnabled                  *bool          `json:"otpEnabled,omitempty"`
	OTPTokenMinutes             int            `json:"otpTokenMinutes"`
	MFAEnabled                  *bool          `json:"mfaEnabled,omitempty"`
	MFARequired                 *bool          `json:"mfaRequired,omitempty"`
	EmailChangeEnabled          *bool          `json:"emailChangeEnabled,omitempty"`
	EmailChangeRequiresPassword *bool          `json:"emailChangeRequiresPassword,omitempty"`
	Templates                   AuthTemplates  `json:"templates"`
	Providers                   map[string]any `json:"providers"`
	CreatedAt                   time.Time      `json:"createdAt,omitempty"`
	UpdatedAt                   time.Time      `json:"updatedAt,omitempty"`
}

func DefaultProjectAuthSettings(project *Project) *ProjectAuthSettings {
	settings := &ProjectAuthSettings{
		AccessTokenMinutes:          appAccessTokenMinutesDefault,
		RefreshTokenDays:            appRefreshTokenDaysDefault,
		VerifyTokenHours:            emailVerifyTokenHoursDefault,
		ResetTokenHours:             passwordResetHoursDefault,
		OTPEnabled:                  true,
		OTPTokenMinutes:             10,
		EmailChangeEnabled:          true,
		EmailChangeRequiresPassword: true,
		Templates:                   defaultAuthTemplates(),
		Providers:                   map[string]any{},
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

func UpdateProjectAuthSettings(ctx context.Context, pool *pgxpool.Pool, cfg *Config, adminID string, projectSlug string, input ProjectAuthSettingsInput, ip string, userAgent string) (*ProjectAuthSettings, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	current, err := getProjectAuthSettingsByProjectPrivate(ctx, pool, project)
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
	if input.OTPEnabled != nil {
		next.OTPEnabled = *input.OTPEnabled
	}
	if input.OTPTokenMinutes != 0 {
		next.OTPTokenMinutes = input.OTPTokenMinutes
	}
	if input.MFAEnabled != nil {
		next.MFAEnabled = *input.MFAEnabled
	}
	if input.MFARequired != nil {
		next.MFARequired = *input.MFARequired
	}
	if input.EmailChangeEnabled != nil {
		next.EmailChangeEnabled = *input.EmailChangeEnabled
	}
	if input.EmailChangeRequiresPassword != nil {
		next.EmailChangeRequiresPassword = *input.EmailChangeRequiresPassword
	}
	next.Templates = mergeAuthTemplates(defaultAuthTemplates(), input.Templates)
	if input.Providers != nil {
		providers, err := normalizeAuthProviders(cfg, current.Providers, input.Providers)
		if err != nil {
			return nil, err
		}
		next.Providers = providers
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
			(project_id, access_token_minutes, refresh_token_days, verify_token_hours, reset_token_hours, otp_enabled, otp_token_minutes, mfa_enabled, mfa_required, email_change_enabled, email_change_requires_password, templates, providers)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb)
		on conflict (project_id) do update
		set access_token_minutes = excluded.access_token_minutes,
			refresh_token_days = excluded.refresh_token_days,
			verify_token_hours = excluded.verify_token_hours,
			reset_token_hours = excluded.reset_token_hours,
			otp_enabled = excluded.otp_enabled,
			otp_token_minutes = excluded.otp_token_minutes,
			mfa_enabled = excluded.mfa_enabled,
			mfa_required = excluded.mfa_required,
			email_change_enabled = excluded.email_change_enabled,
			email_change_requires_password = excluded.email_change_requires_password,
			templates = excluded.templates,
			providers = excluded.providers,
			updated_at = now()
		returning created_at, updated_at`,
		project.ID,
		next.AccessTokenMinutes,
		next.RefreshTokenDays,
		next.VerifyTokenHours,
		next.ResetTokenHours,
		next.OTPEnabled,
		next.OTPTokenMinutes,
		next.MFAEnabled,
		next.MFARequired,
		next.EmailChangeEnabled,
		next.EmailChangeRequiresPassword,
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
			"project":                     project.Slug,
			"accessTokenMinutes":          next.AccessTokenMinutes,
			"refreshTokenDays":            next.RefreshTokenDays,
			"verifyTokenHours":            next.VerifyTokenHours,
			"resetTokenHours":             next.ResetTokenHours,
			"otpEnabled":                  next.OTPEnabled,
			"otpTokenMinutes":             next.OTPTokenMinutes,
			"mfaEnabled":                  next.MFAEnabled,
			"mfaRequired":                 next.MFARequired,
			"emailChangeEnabled":          next.EmailChangeEnabled,
			"emailChangeRequiresPassword": next.EmailChangeRequiresPassword,
			"oauthProviderCount":          len(next.Providers),
		},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	next.Providers = publicAuthProviders(next.Providers)
	return &next, nil
}

func getProjectAuthSettingsByProject(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, project *Project) (*ProjectAuthSettings, error) {
	return getProjectAuthSettingsByProjectMode(ctx, q, project, false)
}

func getProjectAuthSettingsByProjectPrivate(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, project *Project) (*ProjectAuthSettings, error) {
	return getProjectAuthSettingsByProjectMode(ctx, q, project, true)
}

func getProjectAuthSettingsByProjectMode(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, project *Project, includePrivate bool) (*ProjectAuthSettings, error) {
	settings := DefaultProjectAuthSettings(project)
	var rawTemplates []byte
	var rawProviders []byte
	err := q.QueryRow(ctx, `
		select access_token_minutes, refresh_token_days, verify_token_hours, reset_token_hours,
		       otp_enabled, otp_token_minutes, mfa_enabled, mfa_required, email_change_enabled, email_change_requires_password,
		       templates, providers, created_at, updated_at
		from _dbo.project_auth_settings
		where project_id = $1`,
		project.ID,
	).Scan(
		&settings.AccessTokenMinutes,
		&settings.RefreshTokenDays,
		&settings.VerifyTokenHours,
		&settings.ResetTokenHours,
		&settings.OTPEnabled,
		&settings.OTPTokenMinutes,
		&settings.MFAEnabled,
		&settings.MFARequired,
		&settings.EmailChangeEnabled,
		&settings.EmailChangeRequiresPassword,
		&rawTemplates,
		&rawProviders,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	)
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
	if !includePrivate {
		settings.Providers = publicAuthProviders(settings.Providers)
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
	if settings.OTPTokenMinutes < 1 || settings.OTPTokenMinutes > 60 {
		return fmt.Errorf("%w: otpTokenMinutes must be between 1 and 60", ErrValidation)
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
	if err := validateAuthTemplate(settings.Templates.OTPSubject, 200, "otp subject"); err != nil {
		return err
	}
	if err := validateAuthTemplate(settings.Templates.OTPBody, 8000, "otp body"); err != nil {
		return err
	}
	if err := validateAuthTemplate(settings.Templates.EmailChangeSubject, 200, "email change subject"); err != nil {
		return err
	}
	if err := validateAuthTemplate(settings.Templates.EmailChangeBody, 8000, "email change body"); err != nil {
		return err
	}
	if err := validateAuthTemplate(settings.Templates.InvitationSubject, 200, "invitation subject"); err != nil {
		return err
	}
	return validateAuthTemplate(settings.Templates.InvitationBody, 8000, "invitation body")
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
	if strings.TrimSpace(next.OTPSubject) != "" {
		base.OTPSubject = strings.TrimSpace(next.OTPSubject)
	}
	if strings.TrimSpace(next.OTPBody) != "" {
		base.OTPBody = strings.TrimSpace(next.OTPBody)
	}
	if strings.TrimSpace(next.EmailChangeSubject) != "" {
		base.EmailChangeSubject = strings.TrimSpace(next.EmailChangeSubject)
	}
	if strings.TrimSpace(next.EmailChangeBody) != "" {
		base.EmailChangeBody = strings.TrimSpace(next.EmailChangeBody)
	}
	if strings.TrimSpace(next.InvitationSubject) != "" {
		base.InvitationSubject = strings.TrimSpace(next.InvitationSubject)
	}
	if strings.TrimSpace(next.InvitationBody) != "" {
		base.InvitationBody = strings.TrimSpace(next.InvitationBody)
	}
	return base
}

func defaultAuthTemplates() AuthTemplates {
	return AuthTemplates{
		VerifySubject:      "Verify your email for {APP_NAME}",
		VerifyBody:         "Verify your email for {APP_NAME}.\n\nOpen this link:\n{LINK}\n\nToken:\n{TOKEN}\n",
		ResetSubject:       "Reset your {APP_NAME} password",
		ResetBody:          "Reset your password for {APP_NAME}.\n\nOpen this link:\n{LINK}\n\nToken:\n{TOKEN}\n",
		OTPSubject:         "Your {APP_NAME} login code",
		OTPBody:            "Use this one-time login code for {APP_NAME}:\n\n{TOKEN}\n\nThis code expires soon.\n",
		EmailChangeSubject: "Confirm your new email for {APP_NAME}",
		EmailChangeBody:    "Confirm the new email address for {APP_NAME}.\n\nNew email: {NEW_EMAIL}\n\nOpen this link:\n{LINK}\n\nToken:\n{TOKEN}\n",
		InvitationSubject:  "You are invited to {APP_NAME}",
		InvitationBody:     "You were invited to {APP_NAME}.\n\nOpen this link:\n{LINK}\n\nInvitation token:\n{TOKEN}\n",
	}
}

func normalizeAuthProviders(cfg *Config, current map[string]any, input map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for key, value := range input {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || len(key) > 50 || strings.ContainsAny(key, "\r\n\t /\\") {
			continue
		}
		raw, ok := value.(map[string]any)
		if !ok {
			continue
		}
		currentRaw, _ := current[key].(map[string]any)
		next, err := normalizeAuthProvider(cfg, currentRaw, raw)
		if err != nil {
			return nil, err
		}
		out[key] = next
	}
	return out, nil
}

func normalizeAuthProvider(cfg *Config, current map[string]any, input map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for key, item := range input {
		key = strings.TrimSpace(key)
		lower := strings.ToLower(key)
		switch {
		case key == "":
			continue
		case lower == "clientsecretcipher" || lower == "secretcipher" || strings.HasSuffix(lower, "cipher"):
			continue
		case lower == "clientsecret" || lower == "secret" || strings.Contains(lower, "password"):
			secret := strings.TrimSpace(fmt.Sprint(item))
			if secret == "" || secret == "[set]" {
				continue
			}
			if cfg == nil {
				return nil, fmt.Errorf("%w: JWT_SECRET is required for OAuth provider secrets", ErrValidation)
			}
			ciphertext, err := encryptSecret(cfg.JWTSecret, secret)
			if err != nil {
				return nil, err
			}
			out["clientSecretCipher"] = ciphertext
		case lower == "clearclientsecret":
			continue
		default:
			out[key] = sanitizeProviderValue(item)
		}
	}
	clearSecret, _ := input["clearClientSecret"].(bool)
	if !clearSecret && out["clientSecretCipher"] == nil {
		if cipherText, _ := current["clientSecretCipher"].(string); strings.TrimSpace(cipherText) != "" {
			out["clientSecretCipher"] = cipherText
		}
	}
	return out, nil
}

func sanitizeProviderValue(value any) any {
	switch raw := value.(type) {
	case string:
		return strings.TrimSpace(raw)
	case bool:
		return raw
	case float64, int, int64:
		return raw
	case []any:
		out := make([]any, 0, len(raw))
		for _, item := range raw {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return fmt.Sprint(raw)
	}
}

func publicAuthProviders(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		raw, ok := value.(map[string]any)
		if !ok {
			continue
		}
		item := map[string]any{}
		for field, fieldValue := range raw {
			lower := strings.ToLower(field)
			switch {
			case lower == "clientsecretcipher" || lower == "secretcipher" || strings.HasSuffix(lower, "cipher"):
				if strings.TrimSpace(fmt.Sprint(fieldValue)) != "" {
					item["clientSecretSet"] = true
				}
			case lower == "clientsecret" || lower == "secret" || strings.Contains(lower, "password"):
				if strings.TrimSpace(fmt.Sprint(fieldValue)) != "" {
					item["clientSecretSet"] = true
				}
			default:
				item[field] = fieldValue
			}
		}
		out[key] = item
	}
	return out
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

func (s ProjectAuthSettings) OTPTokenTTL() time.Duration {
	return time.Duration(s.OTPTokenMinutes) * time.Minute
}
