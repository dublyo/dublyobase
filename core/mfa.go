package core

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const mfaChallengePrefix = "dbo_mfa_"

type MFAEnrollmentStart struct {
	FactorID   string `json:"factorId"`
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauthUrl"`
}

type MFAEnrollmentConfirm struct {
	FactorID      string   `json:"factorId"`
	RecoveryCodes []string `json:"recoveryCodes"`
}

func StartMFAEnrollment(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, accessToken string, name string, now time.Time) (*MFAEnrollmentStart, error) {
	project, user, err := ResolveAppAccessToken(ctx, pool, cfg, projectSlug, accessToken, now)
	if err != nil {
		return nil, err
	}
	settings, err := getProjectAuthSettingsByProject(ctx, pool, project)
	if err != nil {
		return nil, err
	}
	if !settings.MFAEnabled {
		return nil, fmt.Errorf("%w: MFA is disabled for this project", ErrValidation)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Authenticator app"
	}
	if len(name) > 80 {
		return nil, fmt.Errorf("%w: MFA factor name is too long", ErrValidation)
	}
	secret, err := generateTOTPSecret()
	if err != nil {
		return nil, err
	}
	ciphertext, err := encryptSecret(cfg.JWTSecret, secret)
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		delete from _dbo.mfa_factors
		where project_id = $1 and collection = 'users' and user_id = $2 and enabled = false`,
		project.ID,
		user.ID,
	); err != nil {
		return nil, err
	}
	var factorID string
	if err := tx.QueryRow(ctx, `
		insert into _dbo.mfa_factors (project_id, collection, user_id, type, name, secret_cipher, enabled)
		values ($1, 'users', $2, 'totp', $3, $4, false)
		returning id`,
		project.ID,
		user.ID,
		name,
		ciphertext,
	).Scan(&factorID); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_user.mfa_enroll_start",
		TargetType: "app_user",
		TargetID:   user.ID,
		Data:       map[string]any{"project": project.Slug, "factorId": factorID},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &MFAEnrollmentStart{
		FactorID:   factorID,
		Secret:     secret,
		OtpauthURL: totpAuthURL(project.Name, user.Email, secret),
	}, nil
}

func ConfirmMFAEnrollment(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, accessToken string, factorID string, code string, now time.Time) (*MFAEnrollmentConfirm, error) {
	if err := ValidateUUID(factorID); err != nil {
		return nil, err
	}
	project, user, err := ResolveAppAccessToken(ctx, pool, cfg, projectSlug, accessToken, now)
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	secret, err := getMFAFactorSecretForUpdate(ctx, tx, cfg, project.ID, user.ID, factorID, false)
	if err != nil {
		return nil, err
	}
	if !verifyTOTP(secret, code, now) {
		return nil, ErrInvalidAuthToken
	}
	if _, err := tx.Exec(ctx, `
		update _dbo.mfa_factors
		set enabled = true, verified_at = $1, updated_at = $1
		where id = $2 and project_id = $3 and collection = 'users' and user_id = $4`,
		now.UTC(),
		factorID,
		project.ID,
		user.ID,
	); err != nil {
		return nil, err
	}
	codes, err := replaceRecoveryCodesTx(ctx, tx, project.ID, user.ID)
	if err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_user.mfa_enroll_confirm",
		TargetType: "app_user",
		TargetID:   user.ID,
		Data:       map[string]any{"project": project.Slug, "factorId": factorID},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &MFAEnrollmentConfirm{FactorID: factorID, RecoveryCodes: codes}, nil
}

func VerifyMFALogin(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, mfaToken string, code string, ip string, userAgent string, now time.Time) (*AppAuthResult, error) {
	return verifyMFALoginCode(ctx, pool, cfg, projectSlug, mfaToken, code, false, ip, userAgent, now)
}

func VerifyMFARecoveryLogin(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, mfaToken string, code string, ip string, userAgent string, now time.Time) (*AppAuthResult, error) {
	return verifyMFALoginCode(ctx, pool, cfg, projectSlug, mfaToken, code, true, ip, userAgent, now)
}

func DisableMFA(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, accessToken string, code string, now time.Time) error {
	project, user, err := ResolveAppAccessToken(ctx, pool, cfg, projectSlug, accessToken, now)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if ok, err := verifyUserMFAFactorTx(ctx, tx, cfg, project.ID, user.ID, code, now); err != nil {
		return err
	} else if !ok {
		return ErrInvalidAuthToken
	}
	if _, err := tx.Exec(ctx, `
		update _dbo.mfa_factors
		set enabled = false, updated_at = $1
		where project_id = $2 and collection = 'users' and user_id = $3 and enabled = true`,
		now.UTC(),
		project.ID,
		user.ID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update _dbo.mfa_recovery_codes
		set used_at = coalesce(used_at, $1)
		where project_id = $2 and collection = 'users' and user_id = $3 and used_at is null`,
		now.UTC(),
		project.ID,
		user.ID,
	); err != nil {
		return err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_user.mfa_disable",
		TargetType: "app_user",
		TargetID:   user.ID,
		Data:       map[string]any{"project": project.Slug},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func mfaLoginRequiredTx(ctx context.Context, tx pgx.Tx, projectID string, userID string, settings *ProjectAuthSettings) (bool, error) {
	if settings == nil || !settings.MFAEnabled {
		return false, nil
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		select exists (
			select 1
			from _dbo.mfa_factors
			where project_id = $1 and collection = 'users' and user_id = $2 and enabled = true
		)`,
		projectID,
		userID,
	).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func createMFAChallengeTx(ctx context.Context, tx pgx.Tx, projectID string, user AppUser, ip string, userAgent string, now time.Time) (*AppAuthResult, error) {
	token, err := generateOpaqueToken(mfaChallengePrefix)
	if err != nil {
		return nil, err
	}
	expiresAt := now.UTC().Add(10 * time.Minute)
	if _, err := tx.Exec(ctx, `
		insert into _dbo.mfa_challenges (project_id, collection, user_id, token_hash, ip, user_agent, expires_at)
		values ($1, 'users', $2, $3, $4, $5, $6)`,
		projectID,
		user.ID,
		HashToken(token),
		ip,
		userAgent,
		expiresAt,
	); err != nil {
		return nil, err
	}
	return &AppAuthResult{User: user, MFARequired: true, MFAToken: token, MFAExpiresAt: &expiresAt}, nil
}

func verifyMFALoginCode(ctx context.Context, pool *pgxpool.Pool, cfg *Config, projectSlug string, mfaToken string, code string, recovery bool, ip string, userAgent string, now time.Time) (*AppAuthResult, error) {
	mfaToken = strings.TrimSpace(mfaToken)
	if !strings.HasPrefix(mfaToken, mfaChallengePrefix) {
		return nil, ErrInvalidAuthToken
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
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
	challenge, user, err := getMFAChallengeForUpdate(ctx, tx, project, mfaToken)
	if err != nil {
		return nil, err
	}
	if challenge.UsedAt.Valid || !challenge.ExpiresAt.After(now.UTC()) {
		return nil, ErrInvalidAuthToken
	}
	var ok bool
	if recovery {
		ok, err = verifyMFARecoveryCodeTx(ctx, tx, project.ID, user.ID, code, now)
	} else {
		ok, err = verifyUserMFAFactorTx(ctx, tx, cfg, project.ID, user.ID, code, now)
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrInvalidAuthToken
	}
	refreshToken, session, err := createAppSessionTx(ctx, tx, project.ID, user.ID, "", ip, userAgent, now, settings.RefreshTokenTTL())
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `update _dbo.mfa_challenges set used_at = $1 where id = $2`, now.UTC(), challenge.ID); err != nil {
		return nil, err
	}
	result, err := buildAppAuthResult(cfg, project, user.AppUser, user.TokenKey, refreshToken, session.ExpiresAt, now, settings.AccessTokenTTL())
	if err != nil {
		return nil, err
	}
	action := "app_user.mfa_login"
	if recovery {
		action = "app_user.mfa_recovery_login"
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     action,
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
	return result, nil
}

type mfaChallengeCredential struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
	UsedAt    pgxNullTime
}

type pgxNullTime struct {
	Time  time.Time
	Valid bool
}

func (n *pgxNullTime) Scan(value any) error {
	if value == nil {
		n.Valid = false
		return nil
	}
	t, ok := value.(time.Time)
	if !ok {
		return fmt.Errorf("invalid time value")
	}
	n.Time = t
	n.Valid = true
	return nil
}

func getMFAChallengeForUpdate(ctx context.Context, tx pgx.Tx, project *Project, mfaToken string) (*mfaChallengeCredential, *appUserCredential, error) {
	table := quoteIdent(project.SchemaName, authUsersCollection)
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		select c.id, c.user_id, c.expires_at, c.used_at,
		       u.id, u.email, u.verified, u.created, u.updated, u.password_hash, u.token_key, u.disabled_at
		from _dbo.mfa_challenges c
		join %s u on u.id = c.user_id
		where c.project_id = $1 and c.collection = 'users' and c.token_hash = $2
		for update of c`,
		table,
	), project.ID, HashToken(mfaToken))
	challenge := &mfaChallengeCredential{}
	user := &appUserCredential{}
	if err := row.Scan(
		&challenge.ID,
		&challenge.UserID,
		&challenge.ExpiresAt,
		&challenge.UsedAt,
		&user.ID,
		&user.Email,
		&user.Verified,
		&user.Created,
		&user.Updated,
		&user.PasswordHash,
		&user.TokenKey,
		&user.DisabledAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, ErrInvalidAuthToken
		}
		return nil, nil, err
	}
	if user.DisabledAt.Valid {
		return nil, nil, ErrUserDisabled
	}
	return challenge, user, nil
}

func getMFAFactorSecretForUpdate(ctx context.Context, tx pgx.Tx, cfg *Config, projectID string, userID string, factorID string, enabledOnly bool) (string, error) {
	query := `
		select secret_cipher
		from _dbo.mfa_factors
		where project_id = $1 and collection = 'users' and user_id = $2 and id = $3`
	if enabledOnly {
		query += ` and enabled = true`
	}
	query += ` for update`
	var cipherText string
	if err := tx.QueryRow(ctx, query, projectID, userID, factorID).Scan(&cipherText); err != nil {
		if err == pgx.ErrNoRows {
			return "", ErrInvalidAuthToken
		}
		return "", err
	}
	return decryptSecret(cfg.JWTSecret, cipherText)
}

func verifyUserMFAFactorTx(ctx context.Context, tx pgx.Tx, cfg *Config, projectID string, userID string, code string, now time.Time) (bool, error) {
	rows, err := tx.Query(ctx, `
		select id, secret_cipher
		from _dbo.mfa_factors
		where project_id = $1 and collection = 'users' and user_id = $2 and enabled = true
		for update`,
		projectID,
		userID,
	)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var cipherText string
		if err := rows.Scan(&id, &cipherText); err != nil {
			return false, err
		}
		secret, err := decryptSecret(cfg.JWTSecret, cipherText)
		if err != nil {
			return false, err
		}
		if verifyTOTP(secret, code, now) {
			if _, err := tx.Exec(ctx, `update _dbo.mfa_factors set last_used_at = $1, updated_at = $1 where id = $2`, now.UTC(), id); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, rows.Err()
}

func replaceRecoveryCodesTx(ctx context.Context, tx pgx.Tx, projectID string, userID string) ([]string, error) {
	if _, err := tx.Exec(ctx, `
		delete from _dbo.mfa_recovery_codes
		where project_id = $1 and collection = 'users' and user_id = $2`,
		projectID,
		userID,
	); err != nil {
		return nil, err
	}
	codes := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		code, err := generateRecoveryCode()
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			insert into _dbo.mfa_recovery_codes (project_id, collection, user_id, code_hash)
			values ($1, 'users', $2, $3)`,
			projectID,
			userID,
			HashToken(normalizeRecoveryCode(code)),
		); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

func verifyMFARecoveryCodeTx(ctx context.Context, tx pgx.Tx, projectID string, userID string, code string, now time.Time) (bool, error) {
	codeHash := HashToken(normalizeRecoveryCode(code))
	tag, err := tx.Exec(ctx, `
		update _dbo.mfa_recovery_codes
		set used_at = $1
		where project_id = $2 and collection = 'users' and user_id = $3 and code_hash = $4 and used_at is null`,
		now.UTC(),
		projectID,
		userID,
		codeHash,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func generateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

func generateRecoveryCode() (string, error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	encoded := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
	if len(encoded) > 16 {
		encoded = encoded[:16]
	}
	return encoded[:4] + "-" + encoded[4:8] + "-" + encoded[8:12] + "-" + encoded[12:16], nil
}

func normalizeRecoveryCode(code string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), " ", ""))
}

func verifyTOTP(secret string, code string, now time.Time) bool {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if len(code) != 6 {
		return false
	}
	counter := now.UTC().Unix() / 30
	for offset := int64(-1); offset <= 1; offset++ {
		if hmac.Equal([]byte(totpCode(secret, counter+offset)), []byte(code)) {
			return true
		}
	}
	return false
}

func totpCode(secret string, counter int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return ""
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	binaryCode := (uint32(sum[offset])&0x7f)<<24 |
		(uint32(sum[offset+1])&0xff)<<16 |
		(uint32(sum[offset+2])&0xff)<<8 |
		(uint32(sum[offset+3]) & 0xff)
	value := binaryCode % uint32(math.Pow10(6))
	return fmt.Sprintf("%06d", value)
}

func totpAuthURL(issuer string, email string, secret string) string {
	if issuer = strings.TrimSpace(issuer); issuer == "" {
		issuer = "Dublyobase"
	}
	label := issuer + ":" + email
	values := url.Values{}
	values.Set("secret", secret)
	values.Set("issuer", issuer)
	values.Set("algorithm", "SHA1")
	values.Set("digits", "6")
	values.Set("period", "30")
	return "otpauth://totp/" + url.PathEscape(label) + "?" + values.Encode()
}
