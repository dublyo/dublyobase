package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const oauthStatePrefix = "dbo_oauth_state_"

type OAuthStartResult struct {
	Provider         string    `json:"provider"`
	AuthorizationURL string    `json:"authorizationUrl"`
	State            string    `json:"state"`
	ExpiresAt        time.Time `json:"expiresAt"`
}

type OAuthCallbackResult struct {
	Auth        *AppAuthResult `json:"auth"`
	RedirectURL string         `json:"redirectUrl,omitempty"`
	Provider    string         `json:"provider"`
}

type OAuthProviderConfig struct {
	Provider     string
	Enabled      bool
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	EmailsURL    string
	Scopes       []string
}

type OAuthUserProfile struct {
	ProviderUserID string         `json:"providerUserId"`
	Email          string         `json:"email"`
	EmailVerified  bool           `json:"emailVerified"`
	DisplayName    string         `json:"displayName"`
	AvatarURL      string         `json:"avatarUrl"`
	Raw            map[string]any `json:"raw"`
}

type oauthStateRow struct {
	ID          string
	Provider    string
	RedirectURL string
	ExpiresAt   time.Time
	UsedAt      sql.NullTime
}

func StartOAuthLogin(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, provider string, redirectURL string, now time.Time) (*OAuthStartResult, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	providerCfg, err := getOAuthProviderConfig(ctx, pool, cfg, project, provider)
	if err != nil {
		return nil, err
	}
	redirectURL = strings.TrimSpace(redirectURL)
	if redirectURL != "" {
		if err := validateOAuthReturnURL(cfg, project, redirectURL); err != nil {
			return nil, err
		}
	}
	state, err := generateOpaqueToken(oauthStatePrefix)
	if err != nil {
		return nil, err
	}
	expiresAt := now.UTC().Add(10 * time.Minute)
	callbackURL := oauthCallbackURL(cfg, project.Slug, providerCfg.Provider)
	if err := validateOAuthEndpoint(callbackURL); err != nil {
		return nil, err
	}
	authURL, err := buildOAuthAuthorizationURL(providerCfg, callbackURL, state)
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, `
		insert into _dbo.oauth_states (project_id, provider, state_hash, redirect_url, expires_at)
		values ($1, $2, $3, $4, $5)`,
		project.ID,
		providerCfg.Provider,
		HashToken(state),
		redirectURL,
		expiresAt,
	); err != nil {
		return nil, err
	}
	return &OAuthStartResult{
		Provider:         providerCfg.Provider,
		AuthorizationURL: authURL,
		State:            state,
		ExpiresAt:        expiresAt,
	}, nil
}

func CompleteOAuthLogin(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, provider string, code string, state string, ip string, userAgent string, now time.Time) (*OAuthCallbackResult, error) {
	code = strings.TrimSpace(code)
	state = strings.TrimSpace(state)
	if code == "" || !strings.HasPrefix(state, oauthStatePrefix) {
		return nil, ErrInvalidAuthToken
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	providerCfg, err := getOAuthProviderConfig(ctx, pool, cfg, project, provider)
	if err != nil {
		return nil, err
	}
	stateRow, err := consumeOAuthState(ctx, pool, project, providerCfg.Provider, state, now)
	if err != nil {
		return nil, err
	}
	callbackURL := oauthCallbackURL(cfg, project.Slug, providerCfg.Provider)
	accessToken, err := exchangeOAuthCode(ctx, providerCfg, callbackURL, code)
	if err != nil {
		return nil, err
	}
	profile, err := fetchOAuthProfile(ctx, providerCfg, accessToken)
	if err != nil {
		return nil, err
	}
	result, err := loginOAuthProfile(ctx, pool, cfg, project, providerCfg.Provider, profile, ip, userAgent, now)
	if err != nil {
		return nil, err
	}
	return &OAuthCallbackResult{Auth: result, RedirectURL: stateRow.RedirectURL, Provider: providerCfg.Provider}, nil
}

func getOAuthProviderConfig(ctx context.Context, pool *pgxpool.Pool, cfg *Config, project *Project, provider string) (*OAuthProviderConfig, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	defaults, ok := defaultOAuthProvider(provider)
	if !ok {
		return nil, fmt.Errorf("%w: unsupported OAuth provider", ErrValidation)
	}
	settings, err := getProjectAuthSettingsByProjectPrivate(ctx, pool, project)
	if err != nil {
		return nil, err
	}
	rawProvider, _ := settings.Providers[provider].(map[string]any)
	enabled, _ := rawProvider["enabled"].(bool)
	if !enabled {
		return nil, fmt.Errorf("%w: OAuth provider is disabled", ErrValidation)
	}
	defaults.Enabled = enabled
	defaults.ClientID = stringMapValue(rawProvider, "clientId", "clientID", "client_id")
	if defaults.ClientID == "" {
		return nil, fmt.Errorf("%w: OAuth clientId is required", ErrValidation)
	}
	secretCipher := stringMapValue(rawProvider, "clientSecretCipher")
	if secretCipher == "" {
		return nil, fmt.Errorf("%w: OAuth clientSecret is required", ErrValidation)
	}
	secret, err := decryptSecret(cfg.JWTSecret, secretCipher)
	if err != nil {
		return nil, err
	}
	defaults.ClientSecret = secret
	if value := stringMapValue(rawProvider, "authUrl", "authURL", "authorizationUrl", "authorizationURL"); value != "" {
		defaults.AuthURL = value
	}
	if value := stringMapValue(rawProvider, "tokenUrl", "tokenURL"); value != "" {
		defaults.TokenURL = value
	}
	if value := stringMapValue(rawProvider, "userInfoUrl", "userInfoURL", "userinfoUrl", "userinfoURL"); value != "" {
		defaults.UserInfoURL = value
	}
	if scopes := stringSliceMapValue(rawProvider, "scopes", "scope"); len(scopes) > 0 {
		defaults.Scopes = scopes
	}
	if err := validateOAuthEndpoint(defaults.AuthURL); err != nil {
		return nil, fmt.Errorf("authUrl %w", err)
	}
	if err := validateOAuthEndpoint(defaults.TokenURL); err != nil {
		return nil, fmt.Errorf("tokenUrl %w", err)
	}
	if err := validateOAuthEndpoint(defaults.UserInfoURL); err != nil {
		return nil, fmt.Errorf("userInfoUrl %w", err)
	}
	return defaults, nil
}

func defaultOAuthProvider(provider string) (*OAuthProviderConfig, bool) {
	switch provider {
	case "google":
		return &OAuthProviderConfig{
			Provider:    provider,
			AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:    "https://oauth2.googleapis.com/token",
			UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
			Scopes:      []string{"openid", "email", "profile"},
		}, true
	case "github":
		return &OAuthProviderConfig{
			Provider:    provider,
			AuthURL:     "https://github.com/login/oauth/authorize",
			TokenURL:    "https://github.com/login/oauth/access_token",
			UserInfoURL: "https://api.github.com/user",
			EmailsURL:   "https://api.github.com/user/emails",
			Scopes:      []string{"read:user", "user:email"},
		}, true
	case "facebook":
		return &OAuthProviderConfig{
			Provider:    provider,
			AuthURL:     "https://www.facebook.com/v25.0/dialog/oauth",
			TokenURL:    "https://graph.facebook.com/v25.0/oauth/access_token",
			UserInfoURL: "https://graph.facebook.com/v25.0/me?fields=id,email,name,picture",
			Scopes:      []string{"email", "public_profile"},
		}, true
	case "oidc":
		return &OAuthProviderConfig{
			Provider: provider,
			Scopes:   []string{"openid", "email", "profile"},
		}, true
	default:
		return nil, false
	}
}

func buildOAuthAuthorizationURL(provider *OAuthProviderConfig, callbackURL string, state string) (string, error) {
	parsed, err := url.Parse(provider.AuthURL)
	if err != nil {
		return "", err
	}
	values := parsed.Query()
	values.Set("response_type", "code")
	values.Set("client_id", provider.ClientID)
	values.Set("redirect_uri", callbackURL)
	values.Set("scope", strings.Join(provider.Scopes, " "))
	values.Set("state", state)
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

func consumeOAuthState(ctx context.Context, pool *pgxpool.Pool, project *Project, provider string, state string, now time.Time) (*oauthStateRow, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `
		select id, provider, redirect_url, expires_at, used_at
		from _dbo.oauth_states
		where project_id = $1 and provider = $2 and state_hash = $3
		for update`,
		project.ID,
		provider,
		HashToken(state),
	)
	var item oauthStateRow
	if err := row.Scan(&item.ID, &item.Provider, &item.RedirectURL, &item.ExpiresAt, &item.UsedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidAuthToken
		}
		return nil, err
	}
	if item.UsedAt.Valid || !item.ExpiresAt.After(now.UTC()) {
		return nil, ErrInvalidAuthToken
	}
	if _, err := tx.Exec(ctx, `update _dbo.oauth_states set used_at = $1 where id = $2`, now.UTC(), item.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &item, nil
}

func exchangeOAuthCode(ctx context.Context, provider *OAuthProviderConfig, callbackURL string, code string) (string, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", callbackURL)
	values.Set("client_id", provider.ClientID)
	values.Set("client_secret", provider.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := oauthHTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: OAuth token exchange failed with status %d", ErrUnauthorized, resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", err
	}
	if body.AccessToken == "" {
		return "", fmt.Errorf("%w: OAuth access token missing", ErrUnauthorized)
	}
	return body.AccessToken, nil
}

func fetchOAuthProfile(ctx context.Context, provider *OAuthProviderConfig, accessToken string) (*OAuthUserProfile, error) {
	raw, err := oauthGETJSON(ctx, provider.UserInfoURL, accessToken)
	if err != nil {
		return nil, err
	}
	profile := parseOAuthProfile(provider.Provider, raw)
	if provider.Provider == "github" && profile.Email == "" && provider.EmailsURL != "" {
		emails, err := oauthGETJSONArray(ctx, provider.EmailsURL, accessToken)
		if err == nil {
			applyGitHubPrimaryEmail(profile, emails)
		}
	}
	if profile.ProviderUserID == "" {
		return nil, fmt.Errorf("%w: OAuth profile id missing", ErrUnauthorized)
	}
	if profile.Email != "" {
		email, err := normalizeAppEmail(profile.Email)
		if err != nil {
			return nil, err
		}
		profile.Email = email
	}
	return profile, nil
}

func oauthGETJSON(ctx context.Context, rawURL string, accessToken string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := oauthHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: OAuth userinfo failed with status %d", ErrUnauthorized, resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func oauthGETJSONArray(ctx context.Context, rawURL string, accessToken string) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := oauthHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: OAuth profile request failed with status %d", ErrUnauthorized, resp.StatusCode)
	}
	var out []map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func parseOAuthProfile(provider string, raw map[string]any) *OAuthUserProfile {
	profile := &OAuthUserProfile{Raw: raw}
	switch provider {
	case "github":
		profile.ProviderUserID = stringifyOAuthValue(raw["id"])
		profile.Email = stringifyOAuthValue(raw["email"])
		profile.EmailVerified = profile.Email != ""
		profile.DisplayName = firstNonEmptyString(raw, "name", "login")
		profile.AvatarURL = stringifyOAuthValue(raw["avatar_url"])
	case "facebook":
		profile.ProviderUserID = stringifyOAuthValue(raw["id"])
		profile.Email = stringifyOAuthValue(raw["email"])
		profile.EmailVerified = profile.Email != ""
		profile.DisplayName = stringifyOAuthValue(raw["name"])
		if picture, _ := raw["picture"].(map[string]any); picture != nil {
			if data, _ := picture["data"].(map[string]any); data != nil {
				profile.AvatarURL = stringifyOAuthValue(data["url"])
			}
		}
	default:
		profile.ProviderUserID = stringifyOAuthValue(raw["sub"])
		profile.Email = stringifyOAuthValue(raw["email"])
		profile.EmailVerified, _ = raw["email_verified"].(bool)
		profile.DisplayName = firstNonEmptyString(raw, "name", "preferred_username")
		profile.AvatarURL = stringifyOAuthValue(raw["picture"])
	}
	return profile
}

func applyGitHubPrimaryEmail(profile *OAuthUserProfile, emails []map[string]any) {
	for _, item := range emails {
		email := stringifyOAuthValue(item["email"])
		primary, _ := item["primary"].(bool)
		verified, _ := item["verified"].(bool)
		if email != "" && verified && (primary || profile.Email == "") {
			profile.Email = email
			profile.EmailVerified = true
			if primary {
				return
			}
		}
	}
}

func loginOAuthProfile(ctx context.Context, pool *pgxpool.Pool, cfg *Config, project *Project, provider string, profile *OAuthUserProfile, ip string, userAgent string, now time.Time) (*AppAuthResult, error) {
	if profile.Email == "" {
		return nil, fmt.Errorf("%w: OAuth email is required", ErrValidation)
	}
	settings, err := getProjectAuthSettingsByProject(ctx, pool, project)
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := ensureAuthUsersCollectionTx(ctx, tx, project); err != nil {
		return nil, err
	}
	user, err := findOAuthAccountUserTx(ctx, tx, project, provider, profile.ProviderUserID)
	if err != nil {
		return nil, err
	}
	if user == nil && profile.EmailVerified {
		if existing, err := getAppUserByEmailTx(ctx, tx, project, profile.Email); err == nil {
			user = existing
		} else if !errors.Is(err, ErrUnauthorized) {
			return nil, err
		}
	}
	if user == nil {
		if err := enforceMaxAppUsersQuotaTx(ctx, tx, project); err != nil {
			return nil, err
		}
		user, err = createOAuthAppUserTx(ctx, tx, cfg, project, profile, now)
		if err != nil {
			return nil, err
		}
	}
	if user.DisabledAt.Valid {
		return nil, ErrUserDisabled
	}
	if err := upsertOAuthAccountTx(ctx, tx, project, provider, profile, user.ID); err != nil {
		return nil, err
	}
	table := quoteIdent(project.SchemaName, authUsersCollection)
	if _, err := tx.Exec(ctx, fmt.Sprintf(`update %s set last_login_at = $1, updated = now() where id = $2 and disabled_at is null`, table), now.UTC(), user.ID); err != nil {
		return nil, err
	}
	if required, err := mfaLoginRequiredTx(ctx, tx, project.ID, user.ID, settings); err != nil {
		return nil, err
	} else if required {
		result, err := createMFAChallengeTx(ctx, tx, project.ID, user.AppUser, ip, userAgent, now)
		if err != nil {
			return nil, err
		}
		if err := InsertAudit(ctx, tx, AuditEvent{
			Action:     "app_user.oauth_mfa_required",
			TargetType: "app_user",
			TargetID:   user.ID,
			IP:         ip,
			UserAgent:  userAgent,
			Data:       map[string]any{"project": project.Slug, "provider": provider, "collection": authUsersCollection},
		}); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return result, nil
	}
	refreshToken, session, err := createAppSessionTx(ctx, tx, project.ID, user.ID, "", ip, userAgent, now, settings.RefreshTokenTTL())
	if err != nil {
		return nil, err
	}
	result, err := buildAppAuthResult(cfg, project, user.AppUser, user.TokenKey, refreshToken, session.ExpiresAt, now, settings.AccessTokenTTL())
	if err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_user.oauth_login",
		TargetType: "app_user",
		TargetID:   user.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "provider": provider, "collection": authUsersCollection},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func findOAuthAccountUserTx(ctx context.Context, tx pgx.Tx, project *Project, provider string, providerUserID string) (*appUserCredential, error) {
	var userID string
	err := tx.QueryRow(ctx, `
		select user_id
		from _dbo.oauth_accounts
		where project_id = $1 and collection = 'users' and provider = $2 and provider_user_id = $3`,
		project.ID,
		provider,
		providerUserID,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return getAppUserByIDTx(ctx, tx, project, userID)
}

func createOAuthAppUserTx(ctx context.Context, tx pgx.Tx, cfg *Config, project *Project, profile *OAuthUserProfile, now time.Time) (*appUserCredential, error) {
	randomPassword, err := generateOpaqueToken("oauth_")
	if err != nil {
		return nil, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(randomPassword), appBcryptCost(cfg))
	if err != nil {
		return nil, err
	}
	tokenKey, err := generateTokenKey()
	if err != nil {
		return nil, err
	}
	table := quoteIdent(project.SchemaName, authUsersCollection)
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		insert into %s (email, email_normalized, verified, password_hash, token_key, last_login_at)
		values ($1, $1, $2, $3, $4, $5)
		returning id, email, verified, created, updated, password_hash, token_key, disabled_at`,
		table,
	), profile.Email, profile.EmailVerified, string(passwordHash), tokenKey, now.UTC())
	return scanAppUserCredential(row)
}

func upsertOAuthAccountTx(ctx context.Context, tx pgx.Tx, project *Project, provider string, profile *OAuthUserProfile, userID string) error {
	raw, err := json.Marshal(profile.Raw)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		insert into _dbo.oauth_accounts
			(project_id, collection, user_id, provider, provider_user_id, email, email_verified, display_name, avatar_url, raw_profile)
		values ($1, 'users', $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
		on conflict (project_id, provider, provider_user_id) do update
		set user_id = excluded.user_id,
			email = excluded.email,
			email_verified = excluded.email_verified,
			display_name = excluded.display_name,
			avatar_url = excluded.avatar_url,
			raw_profile = excluded.raw_profile,
			updated_at = now()`,
		project.ID,
		userID,
		provider,
		profile.ProviderUserID,
		profile.Email,
		profile.EmailVerified,
		profile.DisplayName,
		profile.AvatarURL,
		raw,
	)
	return err
}

func oauthCallbackURL(cfg *Config, projectSlug string, provider string) string {
	base := ""
	if cfg != nil {
		base = strings.TrimRight(cfg.AppURL, "/")
	}
	if base == "" {
		base = "http://localhost:8080"
	}
	return base + "/api/projects/" + url.PathEscape(projectSlug) + "/auth/oauth/" + url.PathEscape(provider) + "/callback"
}

func validateOAuthReturnURL(cfg *Config, project *Project, rawURL string) error {
	if len(rawURL) > 2000 {
		return fmt.Errorf("%w: redirect URL is too long", ErrValidation)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: redirect URL must be absolute", ErrValidation)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: redirect URL must use http or https", ErrValidation)
	}
	allowed := map[string]struct{}{}
	if cfg != nil && cfg.AppURL != "" {
		if appURL, err := url.Parse(cfg.AppURL); err == nil && appURL.Host != "" {
			allowed[strings.ToLower(appURL.Scheme+"://"+appURL.Host)] = struct{}{}
		}
	}
	for _, origin := range project.CORS.PublicOrigins {
		if origin == "*" {
			continue
		}
		if originURL, err := url.Parse(origin); err == nil && originURL.Scheme != "" && originURL.Host != "" {
			allowed[strings.ToLower(originURL.Scheme+"://"+originURL.Host)] = struct{}{}
		}
	}
	_, ok := allowed[strings.ToLower(parsed.Scheme+"://"+parsed.Host)]
	if !ok {
		return fmt.Errorf("%w: redirect URL origin is not allowed by project CORS", ErrValidation)
	}
	return nil
}

func validateOAuthEndpoint(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: OAuth endpoint must be absolute", ErrValidation)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("%w: OAuth endpoint must use https", ErrValidation)
	}
	if err := validatePublicOutboundHost(parsed.Hostname()); err != nil {
		return err
	}
	return nil
}

func oauthHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: publicTCPDialer(15 * time.Second),
		},
	}
}

func stringMapValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fmt.Sprint(values[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func stringSliceMapValue(values map[string]any, keys ...string) []string {
	for _, key := range keys {
		switch value := values[key].(type) {
		case string:
			return splitOAuthScope(value)
		case []any:
			out := make([]string, 0, len(value))
			for _, item := range value {
				if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
					out = append(out, text)
				}
			}
			return out
		case []string:
			out := make([]string, 0, len(value))
			for _, item := range value {
				if item = strings.TrimSpace(item); item != "" {
					out = append(out, item)
				}
			}
			return out
		}
	}
	return nil
}

func splitOAuthScope(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

func stringifyOAuthValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func firstNonEmptyString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringifyOAuthValue(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func OAuthRedirectWithFragment(redirectURL string, result *AppAuthResult) string {
	if redirectURL == "" || result == nil {
		return ""
	}
	parsed, err := url.Parse(redirectURL)
	if err != nil {
		return ""
	}
	values := url.Values{}
	values.Set("token", result.Token)
	values.Set("refreshToken", result.RefreshToken)
	values.Set("expiresAt", result.ExpiresAt.Format(time.RFC3339))
	values.Set("refreshExpiresAt", result.RefreshExpiresAt.Format(time.RFC3339))
	parsed.Fragment = values.Encode()
	return parsed.String()
}

func OAuthJSONPage(result *OAuthCallbackResult) []byte {
	body, _ := json.MarshalIndent(result, "", "  ")
	var page bytes.Buffer
	page.WriteString("<!doctype html><meta charset=\"utf-8\"><title>OAuth complete</title><pre>")
	page.WriteString(strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(string(body)))
	page.WriteString("</pre>")
	return page.Bytes()
}
