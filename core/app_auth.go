package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type AppAuthResult struct {
	Token            string    `json:"token"`
	ExpiresAt        time.Time `json:"expiresAt"`
	RefreshToken     string    `json:"refreshToken"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt"`
	User             AppUser   `json:"user"`
}

type AuthTokenRequestResult struct {
	Accepted    bool   `json:"accepted"`
	DevToken    string `json:"devToken,omitempty"`
	Email       string `json:"-"`
	Token       string `json:"-"`
	Type        string `json:"-"`
	ProjectName string `json:"-"`
}

type appRefreshSession struct {
	ID        string
	FamilyID  string
	ExpiresAt time.Time
}

type appSessionCredential struct {
	SessionID string
	UserID    string
	FamilyID  string
	ExpiresAt time.Time
	RotatedAt sql.NullTime
	RevokedAt sql.NullTime
	User      appUserCredential
}

func SignupAppUser(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, email string, password string, ip string, userAgent string, now time.Time) (*AppAuthResult, error) {
	email, err := normalizeAppEmail(email)
	if err != nil {
		return nil, err
	}
	if err := ValidateAppUserPassword(password); err != nil {
		return nil, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), appBcryptCost(cfg))
	if err != nil {
		return nil, err
	}
	tokenKey, err := generateTokenKey()
	if err != nil {
		return nil, err
	}
	project, err := GetProject(ctx, pool, projectSlug)
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

	table := quoteIdent(project.SchemaName, authUsersCollection)
	var user AppUser
	if err := tx.QueryRow(ctx, fmt.Sprintf(`
		insert into %s (email, email_normalized, password_hash, token_key)
		values ($1, $2, $3, $4)
		returning id, email, verified, created, updated`,
		table,
	), email, email, string(passwordHash), tokenKey).Scan(
		&user.ID,
		&user.Email,
		&user.Verified,
		&user.Created,
		&user.Updated,
	); err != nil {
		if pgErrCode(err) == "23505" {
			return nil, ErrUserExists
		}
		return nil, err
	}

	refreshToken, session, err := createAppSessionTx(ctx, tx, project.ID, user.ID, "", ip, userAgent, now)
	if err != nil {
		return nil, err
	}
	result, err := buildAppAuthResult(cfg, project, user, tokenKey, refreshToken, session.ExpiresAt, now)
	if err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_user.signup",
		TargetType: "app_user",
		TargetID:   user.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "collection": authUsersCollection, "email": email},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func LoginAppUser(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, email string, password string, ip string, userAgent string, now time.Time) (*AppAuthResult, error) {
	email = NormalizeEmail(email)
	if err := ValidateAppUserEmail(email); err != nil {
		return nil, ErrInvalidCredentials
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	if _, err := EnsureAuthUsersCollection(ctx, pool, project.Slug); err != nil {
		return nil, err
	}
	cred, err := getAppUserByEmail(ctx, pool, project, email)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if cred.DisabledAt.Valid {
		return nil, ErrUserDisabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(cred.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	table := quoteIdent(project.SchemaName, authUsersCollection)
	tag, err := tx.Exec(ctx, fmt.Sprintf(`
		update %s
		set last_login_at = $1, updated = now()
		where id = $2 and disabled_at is null`,
		table,
	), now.UTC(), cred.ID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrUserDisabled
	}
	refreshToken, session, err := createAppSessionTx(ctx, tx, project.ID, cred.ID, "", ip, userAgent, now)
	if err != nil {
		return nil, err
	}
	result, err := buildAppAuthResult(cfg, project, cred.AppUser, cred.TokenKey, refreshToken, session.ExpiresAt, now)
	if err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_user.login",
		TargetType: "app_user",
		TargetID:   cred.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "collection": authUsersCollection},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func RefreshAppSession(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, refreshToken string, ip string, userAgent string, now time.Time) (*AppAuthResult, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if !strings.HasPrefix(refreshToken, appRefreshTokenPrefix) {
		return nil, ErrInvalidRefreshToken
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	if _, err := EnsureAuthUsersCollection(ctx, pool, project.Slug); err != nil {
		return nil, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	session, err := getRefreshSessionForUpdate(ctx, tx, project, refreshToken)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, err
	}
	if session.RotatedAt.Valid {
		if err := revokeSessionFamilyTx(ctx, tx, project.ID, session.FamilyID, now); err != nil {
			return nil, err
		}
		if err := InsertAudit(ctx, tx, AuditEvent{
			Action:     "app_user.refresh_replay",
			TargetType: "app_user",
			TargetID:   session.UserID,
			IP:         ip,
			UserAgent:  userAgent,
			Data:       map[string]any{"project": project.Slug, "familyId": session.FamilyID},
		}); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, ErrInvalidRefreshToken
	}
	if session.RevokedAt.Valid || !session.ExpiresAt.After(now.UTC()) {
		return nil, ErrInvalidRefreshToken
	}
	if session.User.DisabledAt.Valid {
		return nil, ErrUserDisabled
	}

	newRefreshToken, newSession, err := createAppSessionTx(ctx, tx, project.ID, session.UserID, session.FamilyID, ip, userAgent, now)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		update _dbo.sessions
		set revoked_at = $1, rotated_at = $1, replaced_by = $2, last_seen_at = $1
		where id = $3`,
		now.UTC(),
		newSession.ID,
		session.SessionID,
	); err != nil {
		return nil, err
	}
	result, err := buildAppAuthResult(cfg, project, session.User.AppUser, session.User.TokenKey, newRefreshToken, newSession.ExpiresAt, now)
	if err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_user.refresh",
		TargetType: "app_user",
		TargetID:   session.UserID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "familyId": session.FamilyID},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func LogoutAppSession(ctx context.Context, pool *pgxpool.Pool, projectSlug string, refreshToken string, ip string, userAgent string, now time.Time) error {
	refreshToken = strings.TrimSpace(refreshToken)
	if !strings.HasPrefix(refreshToken, appRefreshTokenPrefix) {
		return ErrInvalidRefreshToken
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return err
	}
	if _, err := EnsureAuthUsersCollection(ctx, pool, project.Slug); err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	session, err := getRefreshSessionForUpdate(ctx, tx, project, refreshToken)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return ErrInvalidRefreshToken
		}
		return err
	}
	if session.RevokedAt.Valid || !session.ExpiresAt.After(now.UTC()) {
		return ErrInvalidRefreshToken
	}
	if _, err := tx.Exec(ctx, `update _dbo.sessions set revoked_at = $1, last_seen_at = $1 where id = $2`, now.UTC(), session.SessionID); err != nil {
		return err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_user.logout",
		TargetType: "app_user",
		TargetID:   session.UserID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func LogoutAllAppSessions(ctx context.Context, pool *pgxpool.Pool, project *Project, userID string, ip string, userAgent string, now time.Time) error {
	if err := ValidateUUID(userID); err != nil {
		return err
	}
	tokenKey, err := generateTokenKey()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	table := quoteIdent(project.SchemaName, authUsersCollection)
	tag, err := tx.Exec(ctx, fmt.Sprintf(`
		update %s
		set token_key = $1, updated = now()
		where id = $2 and disabled_at is null`,
		table,
	), tokenKey, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidAuthToken
	}
	if _, err := tx.Exec(ctx, `
		update _dbo.sessions
		set revoked_at = coalesce(revoked_at, $1), last_seen_at = $1
		where project_id = $2 and collection = 'users' and user_id = $3 and revoked_at is null`,
		now.UTC(),
		project.ID,
		userID,
	); err != nil {
		return err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_user.logout_all",
		TargetType: "app_user",
		TargetID:   userID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ResolveAppAccessToken(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, token string, now time.Time) (*Project, *AppUser, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, nil, err
	}
	claims, err := parseAppJWT(cfg.JWTSecret, token, now)
	if err != nil {
		return nil, nil, ErrInvalidAuthToken
	}
	if !validAppClaimsForProject(claims, project) {
		return nil, nil, ErrInvalidAuthToken
	}
	cred, err := getAppUserByID(ctx, pool, project, claims.Subject)
	if err != nil {
		if errors.Is(err, ErrUserDisabled) {
			return nil, nil, err
		}
		return nil, nil, ErrInvalidAuthToken
	}
	if cred.TokenKey != claims.TokenKey {
		return nil, nil, ErrInvalidAuthToken
	}
	return project, &cred.AppUser, nil
}

func RequestEmailVerification(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, email string, ip string, userAgent string, now time.Time) (*AuthTokenRequestResult, error) {
	return requestAuthToken(ctx, pool, cfg, projectSlug, email, "verify_email", emailVerifyTokenTTL, GenerateEmailVerificationToken, ip, userAgent, now)
}

func RequestPasswordReset(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, email string, ip string, userAgent string, now time.Time) (*AuthTokenRequestResult, error) {
	return requestAuthToken(ctx, pool, cfg, projectSlug, email, "password_reset", passwordResetTTL, GeneratePasswordResetToken, ip, userAgent, now)
}

func ConfirmEmailVerification(ctx context.Context, pool *pgxpool.Pool, projectSlug string, email string, token string, ip string, userAgent string, now time.Time) (*AppUser, error) {
	email, err := normalizeAppEmail(email)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(strings.TrimSpace(token), emailVerifyTokenPrefix) {
		return nil, ErrInvalidAuthToken
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	if _, err := EnsureAuthUsersCollection(ctx, pool, project.Slug); err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	authToken, user, err := getAuthTokenForUpdate(ctx, tx, project, "verify_email", token)
	if err != nil {
		return nil, err
	}
	if user.Email != email {
		return nil, ErrInvalidAuthToken
	}
	if err := validateAuthTokenUse(authToken, user, now); err != nil {
		return nil, err
	}
	table := quoteIdent(project.SchemaName, authUsersCollection)
	if err := tx.QueryRow(ctx, fmt.Sprintf(`
		update %s
		set verified = true, updated = now()
		where id = $1
		returning id, email, verified, created, updated`,
		table,
	), user.ID).Scan(&user.ID, &user.Email, &user.Verified, &user.Created, &user.Updated); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `update _dbo.auth_tokens set used_at = $1 where id = $2`, now.UTC(), authToken.ID); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_user.verify_email",
		TargetType: "app_user",
		TargetID:   user.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &user.AppUser, nil
}

func ConfirmPasswordReset(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, email string, token string, password string, ip string, userAgent string, now time.Time) (*AppUser, error) {
	email, err := normalizeAppEmail(email)
	if err != nil {
		return nil, err
	}
	if err := ValidateAppUserPassword(password); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(strings.TrimSpace(token), passwordResetPrefix) {
		return nil, ErrInvalidAuthToken
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), appBcryptCost(cfg))
	if err != nil {
		return nil, err
	}
	tokenKey, err := generateTokenKey()
	if err != nil {
		return nil, err
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	if _, err := EnsureAuthUsersCollection(ctx, pool, project.Slug); err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	authToken, user, err := getAuthTokenForUpdate(ctx, tx, project, "password_reset", token)
	if err != nil {
		return nil, err
	}
	if user.Email != email {
		return nil, ErrInvalidAuthToken
	}
	if err := validateAuthTokenUse(authToken, user, now); err != nil {
		return nil, err
	}
	table := quoteIdent(project.SchemaName, authUsersCollection)
	if err := tx.QueryRow(ctx, fmt.Sprintf(`
		update %s
		set password_hash = $1, token_key = $2, updated = now()
		where id = $3
		returning id, email, verified, created, updated`,
		table,
	), string(passwordHash), tokenKey, user.ID).Scan(&user.ID, &user.Email, &user.Verified, &user.Created, &user.Updated); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `update _dbo.auth_tokens set used_at = $1 where id = $2`, now.UTC(), authToken.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		update _dbo.sessions
		set revoked_at = coalesce(revoked_at, $1), last_seen_at = $1
		where project_id = $2 and collection = 'users' and user_id = $3 and revoked_at is null`,
		now.UTC(),
		project.ID,
		user.ID,
	); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_user.password_reset",
		TargetType: "app_user",
		TargetID:   user.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &user.AppUser, nil
}

func requestAuthToken(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, email string, tokenType string, ttl time.Duration, generate func() (string, error), ip string, userAgent string, now time.Time) (*AuthTokenRequestResult, error) {
	email, err := normalizeAppEmail(email)
	if err != nil {
		return nil, err
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	if _, err := EnsureAuthUsersCollection(ctx, pool, project.Slug); err != nil {
		return nil, err
	}
	cred, err := getAppUserByEmail(ctx, pool, project, email)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return &AuthTokenRequestResult{Accepted: true, Email: email, Type: tokenType, ProjectName: project.Name}, nil
		}
		return nil, err
	}
	if cred.DisabledAt.Valid {
		return &AuthTokenRequestResult{Accepted: true, Email: email, Type: tokenType, ProjectName: project.Name}, nil
	}
	token, err := generate()
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		insert into _dbo.auth_tokens (project_id, collection, user_id, type, token_hash, expires_at)
		values ($1, 'users', $2, $3, $4, $5)`,
		project.ID,
		cred.ID,
		tokenType,
		HashToken(token),
		now.UTC().Add(ttl),
	); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_user." + tokenType + "_request",
		TargetType: "app_user",
		TargetID:   cred.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	out := &AuthTokenRequestResult{Accepted: true, Email: email, Token: token, Type: tokenType, ProjectName: project.Name}
	if cfg != nil && cfg.AuthDevTokens {
		out.DevToken = token
	}
	return out, nil
}

func createAppSessionTx(ctx context.Context, tx pgx.Tx, projectID string, userID string, familyID string, ip string, userAgent string, now time.Time) (string, appRefreshSession, error) {
	token, err := GenerateAppRefreshToken()
	if err != nil {
		return "", appRefreshSession{}, err
	}
	expiresAt := now.UTC().Add(appRefreshTokenTTL)
	var session appRefreshSession
	if err := tx.QueryRow(ctx, `
		insert into _dbo.sessions (project_id, collection, user_id, token_hash, family_id, ip, user_agent, expires_at)
		values ($1, 'users', $2, $3, coalesce(nullif($4, '')::uuid, gen_random_uuid()), $5, $6, $7)
		returning id, family_id, expires_at`,
		projectID,
		userID,
		HashToken(token),
		familyID,
		ip,
		userAgent,
		expiresAt,
	).Scan(&session.ID, &session.FamilyID, &session.ExpiresAt); err != nil {
		return "", appRefreshSession{}, err
	}
	return token, session, nil
}

func getRefreshSessionForUpdate(ctx context.Context, tx pgx.Tx, project *Project, refreshToken string) (*appSessionCredential, error) {
	table := quoteIdent(project.SchemaName, authUsersCollection)
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		select s.id, s.user_id, s.family_id, s.expires_at, s.rotated_at, s.revoked_at,
		       u.id, u.email, u.verified, u.created, u.updated, u.password_hash, u.token_key, u.disabled_at
		from _dbo.sessions s
		join %s u on u.id = s.user_id
		where s.project_id = $1 and s.collection = 'users' and s.token_hash = $2
		for update of s`,
		table,
	), project.ID, HashToken(refreshToken))
	var session appSessionCredential
	if err := row.Scan(
		&session.SessionID,
		&session.UserID,
		&session.FamilyID,
		&session.ExpiresAt,
		&session.RotatedAt,
		&session.RevokedAt,
		&session.User.ID,
		&session.User.Email,
		&session.User.Verified,
		&session.User.Created,
		&session.User.Updated,
		&session.User.PasswordHash,
		&session.User.TokenKey,
		&session.User.DisabledAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	return &session, nil
}

func revokeSessionFamilyTx(ctx context.Context, tx pgx.Tx, projectID string, familyID string, now time.Time) error {
	_, err := tx.Exec(ctx, `
		update _dbo.sessions
		set revoked_at = coalesce(revoked_at, $1), last_seen_at = $1
		where project_id = $2 and family_id = $3`,
		now.UTC(),
		projectID,
		familyID,
	)
	return err
}

func buildAppAuthResult(cfg *Config, project *Project, user AppUser, tokenKey string, refreshToken string, refreshExpiresAt time.Time, now time.Time) (*AppAuthResult, error) {
	token, expiresAt, err := GenerateAppAccessToken(cfg.JWTSecret, project.Slug, user.ID, tokenKey, now)
	if err != nil {
		return nil, err
	}
	return &AppAuthResult{
		Token:            token,
		ExpiresAt:        expiresAt,
		RefreshToken:     refreshToken,
		RefreshExpiresAt: refreshExpiresAt,
		User:             user,
	}, nil
}

func validAppClaimsForProject(claims *appClaims, project *Project) bool {
	if claims.Project != project.Slug || claims.Role != string(RecordRoleAuthenticated) || claims.Collection != authUsersCollection {
		return false
	}
	return ValidateUUID(claims.Subject) == nil
}

func getAppUserByEmail(ctx context.Context, pool *pgxpool.Pool, project *Project, email string) (*appUserCredential, error) {
	table := quoteIdent(project.SchemaName, authUsersCollection)
	row := pool.QueryRow(ctx, fmt.Sprintf(`
		select id, email, verified, created, updated, password_hash, token_key, disabled_at
		from %s
		where email_normalized = $1`,
		table,
	), email)
	return scanAppUserCredential(row)
}

func getAppUserByID(ctx context.Context, pool *pgxpool.Pool, project *Project, userID string) (*appUserCredential, error) {
	if err := ValidateUUID(userID); err != nil {
		return nil, ErrInvalidAuthToken
	}
	table := quoteIdent(project.SchemaName, authUsersCollection)
	row := pool.QueryRow(ctx, fmt.Sprintf(`
		select id, email, verified, created, updated, password_hash, token_key, disabled_at
		from %s
		where id = $1`,
		table,
	), userID)
	cred, err := scanAppUserCredential(row)
	if err != nil {
		return nil, err
	}
	if cred.DisabledAt.Valid {
		return nil, ErrUserDisabled
	}
	return cred, nil
}

type authTokenCredential struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
	UsedAt    sql.NullTime
}

func getAuthTokenForUpdate(ctx context.Context, tx pgx.Tx, project *Project, tokenType string, token string) (*authTokenCredential, *appUserCredential, error) {
	table := quoteIdent(project.SchemaName, authUsersCollection)
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		select t.id, t.user_id, t.expires_at, t.used_at,
		       u.id, u.email, u.verified, u.created, u.updated, u.password_hash, u.token_key, u.disabled_at
		from _dbo.auth_tokens t
		join %s u on u.id = t.user_id
		where t.project_id = $1 and t.collection = 'users' and t.type = $2 and t.token_hash = $3
		for update of t`,
		table,
	), project.ID, tokenType, HashToken(strings.TrimSpace(token)))
	authToken := &authTokenCredential{}
	user := &appUserCredential{}
	if err := row.Scan(
		&authToken.ID,
		&authToken.UserID,
		&authToken.ExpiresAt,
		&authToken.UsedAt,
		&user.ID,
		&user.Email,
		&user.Verified,
		&user.Created,
		&user.Updated,
		&user.PasswordHash,
		&user.TokenKey,
		&user.DisabledAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrInvalidAuthToken
		}
		return nil, nil, err
	}
	return authToken, user, nil
}

func validateAuthTokenUse(token *authTokenCredential, user *appUserCredential, now time.Time) error {
	if token.UsedAt.Valid || !token.ExpiresAt.After(now.UTC()) {
		return ErrInvalidAuthToken
	}
	if user.DisabledAt.Valid {
		return ErrUserDisabled
	}
	return nil
}

func appBcryptCost(cfg *Config) int {
	if cfg == nil || cfg.BcryptCost == 0 {
		return bcrypt.DefaultCost
	}
	return cfg.BcryptCost
}
