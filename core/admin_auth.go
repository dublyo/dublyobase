package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	adminSessionTTL      = 24 * time.Hour
	adminTokenPrefix     = "dbo_admin_"
	setupAdvisoryLockID  = int64(326_326_002)
	minAdminPasswordSize = 12

	BootstrapAdminEmail    = "admin@example.com"
	BootstrapAdminPassword = "dublyo"
)

type Admin struct {
	ID                 string `json:"id"`
	Email              string `json:"email"`
	MustChangePassword bool   `json:"mustChangePassword"`
}

type AdminSession struct {
	ID        string    `json:"id"`
	AdminID   string    `json:"adminId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type AdminAuth struct {
	Admin   Admin
	Session AdminSession
}

type AdminLoginResult struct {
	Token   string
	Admin   Admin
	Session AdminSession
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func ValidateAdminEmail(email string) error {
	if email == "" {
		return fmt.Errorf("%w: email is required", ErrValidation)
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return fmt.Errorf("%w: email is invalid", ErrValidation)
	}
	return nil
}

func ValidateAdminPassword(password string) error {
	if len(password) < minAdminPasswordSize {
		return fmt.Errorf("%w: password must be at least %d characters", ErrValidation, minAdminPasswordSize)
	}
	return nil
}

func IsBootstrapAdminCredential(email string, password string) bool {
	return NormalizeEmail(email) == BootstrapAdminEmail && password == BootstrapAdminPassword
}

func GenerateAdminToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return adminTokenPrefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func CreateFirstAdmin(ctx context.Context, pool *pgxpool.Pool, email, password, ip, userAgent string) (*Admin, error) {
	return CreateFirstAdminWithCost(ctx, pool, email, password, bcrypt.DefaultCost, ip, userAgent)
}

func CreateFirstAdminWithCost(ctx context.Context, pool *pgxpool.Pool, email, password string, bcryptCost int, ip, userAgent string) (*Admin, error) {
	return createFirstAdminWithOptions(ctx, pool, email, password, bcryptCost, false, false, ip, userAgent)
}

func CreateBootstrapAdmin(ctx context.Context, pool *pgxpool.Pool, bcryptCost int, ip, userAgent string) (*Admin, error) {
	return createFirstAdminWithOptions(ctx, pool, BootstrapAdminEmail, BootstrapAdminPassword, bcryptCost, true, true, ip, userAgent)
}

func createFirstAdminWithOptions(ctx context.Context, pool *pgxpool.Pool, email, password string, bcryptCost int, allowBootstrapPassword bool, mustChangePassword bool, ip, userAgent string) (*Admin, error) {
	email = NormalizeEmail(email)
	if err := ValidateAdminEmail(email); err != nil {
		return nil, err
	}
	if !allowBootstrapPassword {
		if err := ValidateAdminPassword(password); err != nil {
			return nil, err
		}
	} else if !IsBootstrapAdminCredential(email, password) {
		return nil, fmt.Errorf("%w: bootstrap admin credentials are fixed", ErrValidation)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, setupAdvisoryLockID); err != nil {
		return nil, err
	}

	var count int
	if err := tx.QueryRow(ctx, `select count(*) from _dbo.admins`).Scan(&count); err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrSetupClosed
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, err
	}

	admin := &Admin{Email: email, MustChangePassword: mustChangePassword}
	if err := tx.QueryRow(ctx,
		`insert into _dbo.admins (email, password_hash, must_change_password)
		 values ($1, $2, $3)
		 returning id, must_change_password`,
		email,
		string(hash),
		mustChangePassword,
	).Scan(&admin.ID, &admin.MustChangePassword); err != nil {
		return nil, err
	}

	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &admin.ID,
		Action:     "admin.setup",
		TargetType: "admin",
		TargetID:   admin.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"email": email, "mustChangePassword": mustChangePassword},
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return admin, nil
}

func LoginAdmin(ctx context.Context, pool *pgxpool.Pool, email, password, ip, userAgent string, now time.Time) (*AdminLoginResult, error) {
	email = NormalizeEmail(email)
	if err := ValidateAdminEmail(email); err != nil {
		return nil, ErrInvalidCredentials
	}

	var admin Admin
	var passwordHash string
	var disabledAt sql.NullTime
	if err := pool.QueryRow(ctx,
		`select id, email, password_hash, disabled_at, must_change_password from _dbo.admins where email = $1`,
		email,
	).Scan(&admin.ID, &admin.Email, &passwordHash, &disabledAt, &admin.MustChangePassword); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if disabledAt.Valid {
		return nil, ErrAdminDisabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	token, err := GenerateAdminToken()
	if err != nil {
		return nil, err
	}
	expiresAt := now.UTC().Add(adminSessionTTL)
	session := AdminSession{AdminID: admin.ID, ExpiresAt: expiresAt}
	if err := pool.QueryRow(ctx, `
		insert into _dbo.admin_sessions (admin_id, token_hash, ip, user_agent, expires_at)
		values ($1, $2, $3, $4, $5)
		returning id`,
		admin.ID,
		HashToken(token),
		ip,
		userAgent,
		expiresAt,
	).Scan(&session.ID); err != nil {
		return nil, err
	}

	if err := InsertAudit(ctx, pool, AuditEvent{
		AdminID:    &admin.ID,
		Action:     "admin.login",
		TargetType: "admin",
		TargetID:   admin.ID,
		IP:         ip,
		UserAgent:  userAgent,
	}); err != nil {
		return nil, err
	}

	return &AdminLoginResult{Token: token, Admin: admin, Session: session}, nil
}

func FindAdminByToken(ctx context.Context, pool *pgxpool.Pool, token string, now time.Time) (*AdminAuth, error) {
	if !strings.HasPrefix(token, adminTokenPrefix) {
		return nil, ErrUnauthorized
	}

	var auth AdminAuth
	var disabledAt sql.NullTime
	if err := pool.QueryRow(ctx, `
		select a.id, a.email, a.must_change_password, a.disabled_at, s.id, s.admin_id, s.expires_at
		from _dbo.admin_sessions s
		join _dbo.admins a on a.id = s.admin_id
		where s.token_hash = $1 and s.revoked_at is null`,
		HashToken(token),
	).Scan(
		&auth.Admin.ID,
		&auth.Admin.Email,
		&auth.Admin.MustChangePassword,
		&disabledAt,
		&auth.Session.ID,
		&auth.Session.AdminID,
		&auth.Session.ExpiresAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if disabledAt.Valid {
		return nil, ErrAdminDisabled
	}
	if !auth.Session.ExpiresAt.After(now.UTC()) {
		return nil, ErrSessionExpired
	}

	_, _ = pool.Exec(ctx, `update _dbo.admin_sessions set last_seen_at = now() where id = $1`, auth.Session.ID)
	return &auth, nil
}

func RevokeAdminSession(ctx context.Context, pool *pgxpool.Pool, sessionID string) error {
	_, err := pool.Exec(ctx,
		`update _dbo.admin_sessions set revoked_at = now() where id = $1 and revoked_at is null`,
		sessionID,
	)
	return err
}

func ChangeAdminPassword(ctx context.Context, pool *pgxpool.Pool, adminID string, sessionID string, currentPassword string, newPassword string, bcryptCost int, ip string, userAgent string) (*Admin, error) {
	if err := ValidateAdminPassword(newPassword); err != nil {
		return nil, err
	}
	if currentPassword == newPassword {
		return nil, fmt.Errorf("%w: new password must be different from the current password", ErrValidation)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var admin Admin
	var passwordHash string
	var disabledAt sql.NullTime
	if err := tx.QueryRow(ctx, `
		select id, email, password_hash, disabled_at, must_change_password
		from _dbo.admins
		where id = $1
		for update`,
		adminID,
	).Scan(&admin.ID, &admin.Email, &passwordHash, &disabledAt, &admin.MustChangePassword); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if disabledAt.Valid {
		return nil, ErrAdminDisabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(currentPassword)); err != nil {
		return nil, ErrInvalidCredentials
	}
	wasForced := admin.MustChangePassword

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `
		update _dbo.admins
		set password_hash = $1,
		    must_change_password = false,
		    updated_at = now()
		where id = $2
		returning id, email, must_change_password`,
		string(hash),
		adminID,
	).Scan(&admin.ID, &admin.Email, &admin.MustChangePassword); err != nil {
		return nil, err
	}

	if sessionID != "" {
		if _, err := tx.Exec(ctx, `
			update _dbo.admin_sessions
			set revoked_at = now()
			where admin_id = $1
			  and id <> $2
			  and revoked_at is null`,
			adminID,
			sessionID,
		); err != nil {
			return nil, err
		}
	}

	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &admin.ID,
		Action:     "admin.password_changed",
		TargetType: "admin",
		TargetID:   admin.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"forced": wasForced},
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &admin, nil
}
