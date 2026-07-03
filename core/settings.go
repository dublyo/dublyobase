package core

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const secretCipherPrefix = "v1"

type PublicInstanceSettings struct {
	SMTP    PublicSMTPSettings    `json:"smtp"`
	Storage PublicStorageSettings `json:"storage"`
}

type PublicSMTPSettings struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	Username    string `json:"username"`
	PasswordSet bool   `json:"passwordSet"`
	From        string `json:"from"`
	Source      string `json:"source"`
}

type SMTPSettingsInput struct {
	Enabled       bool    `json:"enabled"`
	Host          string  `json:"host"`
	Port          string  `json:"port"`
	Username      string  `json:"username"`
	Password      *string `json:"password,omitempty"`
	ClearPassword bool    `json:"clearPassword"`
	From          string  `json:"from"`
}

type PublicStorageSettings struct {
	Type      StorageType      `json:"type"`
	LocalPath string           `json:"localPath"`
	S3        PublicS3Settings `json:"s3"`
	Source    string           `json:"source"`
}

type PublicS3Settings struct {
	Endpoint       string `json:"endpoint"`
	Bucket         string `json:"bucket"`
	Region         string `json:"region"`
	AccessKey      string `json:"accessKey"`
	SecretKeySet   bool   `json:"secretKeySet"`
	Prefix         string `json:"prefix"`
	UseSSL         bool   `json:"useSSL"`
	ForcePathStyle bool   `json:"forcePathStyle"`
}

type StorageSettingsInput struct {
	Type StorageType     `json:"type"`
	S3   S3SettingsInput `json:"s3"`
}

type S3SettingsInput struct {
	Endpoint       string  `json:"endpoint"`
	Bucket         string  `json:"bucket"`
	Region         string  `json:"region"`
	AccessKey      string  `json:"accessKey"`
	SecretKey      *string `json:"secretKey,omitempty"`
	ClearSecretKey bool    `json:"clearSecretKey"`
	Prefix         string  `json:"prefix"`
	UseSSL         bool    `json:"useSSL"`
	ForcePathStyle bool    `json:"forcePathStyle"`
}

type storedInstanceSettings struct {
	SMTP    storedSMTPSettings    `json:"smtp,omitempty"`
	Storage storedStorageSettings `json:"storage,omitempty"`
}

type storedSMTPSettings struct {
	Configured     bool   `json:"configured,omitempty"`
	Enabled        bool   `json:"enabled"`
	Host           string `json:"host,omitempty"`
	Port           string `json:"port,omitempty"`
	Username       string `json:"username,omitempty"`
	PasswordCipher string `json:"passwordCipher,omitempty"`
	From           string `json:"from,omitempty"`
}

type storedStorageSettings struct {
	Configured bool             `json:"configured,omitempty"`
	Type       StorageType      `json:"type,omitempty"`
	S3         storedS3Settings `json:"s3,omitempty"`
}

type storedS3Settings struct {
	Endpoint        string `json:"endpoint,omitempty"`
	Bucket          string `json:"bucket,omitempty"`
	Region          string `json:"region,omitempty"`
	AccessKey       string `json:"accessKey,omitempty"`
	SecretKeyCipher string `json:"secretKeyCipher,omitempty"`
	Prefix          string `json:"prefix,omitempty"`
	UseSSL          bool   `json:"useSSL"`
	ForcePathStyle  bool   `json:"forcePathStyle"`
}

func GetPublicInstanceSettings(ctx context.Context, pool *pgxpool.Pool, cfg *Config) (*PublicInstanceSettings, error) {
	stored, err := loadStoredInstanceSettings(ctx, pool)
	if err != nil {
		return nil, err
	}
	return publicInstanceSettings(cfg, stored), nil
}

func UpdateSMTPSettings(ctx context.Context, pool *pgxpool.Pool, cfg *Config, adminID string, input SMTPSettingsInput, ip string, userAgent string) (*PublicInstanceSettings, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	stored, err := loadStoredInstanceSettingsTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	next, err := normalizeSMTPInput(cfg, stored.SMTP, input)
	if err != nil {
		return nil, err
	}
	stored.SMTP = next
	if err := saveStoredInstanceSettings(ctx, tx, stored); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "settings.smtp.update",
		TargetType: "settings",
		TargetID:   "smtp",
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"enabled": next.Enabled, "host": next.Host, "from": next.From},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return publicInstanceSettings(cfg, stored), nil
}

func UpdateStorageSettings(ctx context.Context, pool *pgxpool.Pool, cfg *Config, adminID string, input StorageSettingsInput, ip string, userAgent string) (*PublicInstanceSettings, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	stored, err := loadStoredInstanceSettingsTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	next, err := normalizeStorageInput(cfg, stored.Storage, input)
	if err != nil {
		return nil, err
	}
	stored.Storage = next
	if err := saveStoredInstanceSettings(ctx, tx, stored); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "settings.storage.update",
		TargetType: "settings",
		TargetID:   "storage",
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"type": next.Type, "endpoint": next.S3.Endpoint, "bucket": next.S3.Bucket},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return publicInstanceSettings(cfg, stored), nil
}

func EffectiveSMTPConfig(ctx context.Context, pool *pgxpool.Pool, cfg *Config) (*Config, error) {
	stored, err := loadStoredInstanceSettings(ctx, pool)
	if err != nil {
		return nil, err
	}
	out := *cfg
	if !stored.SMTP.Configured {
		return &out, nil
	}
	if !stored.SMTP.Enabled {
		out.SMTPHost = ""
		out.SMTPPort = "587"
		out.SMTPUser = ""
		out.SMTPPassword = ""
		out.SMTPFrom = ""
		return &out, nil
	}
	out.SMTPHost = stored.SMTP.Host
	out.SMTPPort = stored.SMTP.Port
	out.SMTPUser = stored.SMTP.Username
	out.SMTPFrom = stored.SMTP.From
	out.SMTPPassword = ""
	if stored.SMTP.PasswordCipher != "" {
		password, err := decryptSecret(cfg.JWTSecret, stored.SMTP.PasswordCipher)
		if err != nil {
			return nil, err
		}
		out.SMTPPassword = password
	}
	return &out, nil
}

func EffectiveStorageConfig(ctx context.Context, pool *pgxpool.Pool, cfg *Config) (*Config, error) {
	stored, err := loadStoredInstanceSettings(ctx, pool)
	if err != nil {
		return nil, err
	}
	out := *cfg
	if !stored.Storage.Configured {
		return &out, nil
	}
	out.StorageType = stored.Storage.Type
	if stored.Storage.Type == StorageS3 {
		out.S3Endpoint = stored.Storage.S3.Endpoint
		out.S3Bucket = stored.Storage.S3.Bucket
		out.S3Region = stored.Storage.S3.Region
		out.S3AccessKey = stored.Storage.S3.AccessKey
		out.S3Prefix = stored.Storage.S3.Prefix
		out.S3UseSSL = stored.Storage.S3.UseSSL
		out.S3ForcePathStyle = stored.Storage.S3.ForcePathStyle
		out.S3SecretKey = ""
		if stored.Storage.S3.SecretKeyCipher != "" {
			secret, err := decryptSecret(cfg.JWTSecret, stored.Storage.S3.SecretKeyCipher)
			if err != nil {
				return nil, err
			}
			out.S3SecretKey = secret
		}
	}
	return &out, nil
}

func StoredS3RuntimeOptions(ctx context.Context, pool *pgxpool.Pool) (prefix string, useSSL bool, forcePathStyle bool, configured bool, err error) {
	stored, err := loadStoredInstanceSettings(ctx, pool)
	if err != nil || !stored.Storage.Configured || stored.Storage.Type != StorageS3 {
		return "", true, true, false, err
	}
	return stored.Storage.S3.Prefix, stored.Storage.S3.UseSSL, stored.Storage.S3.ForcePathStyle, true, nil
}

func loadStoredInstanceSettings(ctx context.Context, pool *pgxpool.Pool) (storedInstanceSettings, error) {
	return loadStoredInstanceSettingsTx(ctx, pool)
}

func loadStoredInstanceSettingsTx(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) (storedInstanceSettings, error) {
	var raw json.RawMessage
	err := q.QueryRow(ctx, `select data from _dbo.instance_settings where id = true`).Scan(&raw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return storedInstanceSettings{}, nil
		}
		return storedInstanceSettings{}, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return storedInstanceSettings{}, nil
	}
	var stored storedInstanceSettings
	if err := json.Unmarshal(raw, &stored); err != nil {
		return storedInstanceSettings{}, fmt.Errorf("%w: invalid instance settings", ErrValidation)
	}
	return stored, nil
}

func saveStoredInstanceSettings(ctx context.Context, tx pgx.Tx, stored storedInstanceSettings) error {
	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		insert into _dbo.instance_settings (id, data, updated_at)
		values (true, $1::jsonb, now())
		on conflict (id) do update
		set data = excluded.data,
			updated_at = now()`,
		raw,
	)
	return err
}

func publicInstanceSettings(cfg *Config, stored storedInstanceSettings) *PublicInstanceSettings {
	return &PublicInstanceSettings{
		SMTP:    publicSMTPSettings(cfg, stored.SMTP),
		Storage: publicStorageSettings(cfg, stored.Storage),
	}
}

func publicSMTPSettings(cfg *Config, stored storedSMTPSettings) PublicSMTPSettings {
	if !stored.Configured {
		return PublicSMTPSettings{
			Enabled:     strings.TrimSpace(cfg.SMTPHost) != "",
			Host:        cfg.SMTPHost,
			Port:        cfg.SMTPPort,
			Username:    cfg.SMTPUser,
			PasswordSet: cfg.SMTPPassword != "",
			From:        cfg.SMTPFrom,
			Source:      "env",
		}
	}
	return PublicSMTPSettings{
		Enabled:     stored.Enabled,
		Host:        stored.Host,
		Port:        stored.Port,
		Username:    stored.Username,
		PasswordSet: stored.PasswordCipher != "",
		From:        stored.From,
		Source:      "database",
	}
}

func publicStorageSettings(cfg *Config, stored storedStorageSettings) PublicStorageSettings {
	if !stored.Configured {
		return PublicStorageSettings{
			Type:      cfg.StorageType,
			LocalPath: cfg.StorageLocalPath,
			S3: PublicS3Settings{
				Endpoint:       cfg.S3Endpoint,
				Bucket:         cfg.S3Bucket,
				Region:         cfg.S3Region,
				AccessKey:      cfg.S3AccessKey,
				SecretKeySet:   cfg.S3SecretKey != "",
				Prefix:         cfg.S3Prefix,
				UseSSL:         cfg.S3UseSSL,
				ForcePathStyle: cfg.S3ForcePathStyle,
			},
			Source: "env",
		}
	}
	return PublicStorageSettings{
		Type:      stored.Type,
		LocalPath: cfg.StorageLocalPath,
		S3: PublicS3Settings{
			Endpoint:       stored.S3.Endpoint,
			Bucket:         stored.S3.Bucket,
			Region:         stored.S3.Region,
			AccessKey:      stored.S3.AccessKey,
			SecretKeySet:   stored.S3.SecretKeyCipher != "",
			Prefix:         stored.S3.Prefix,
			UseSSL:         stored.S3.UseSSL,
			ForcePathStyle: stored.S3.ForcePathStyle,
		},
		Source: "database",
	}
}

func normalizeSMTPInput(cfg *Config, current storedSMTPSettings, input SMTPSettingsInput) (storedSMTPSettings, error) {
	next := storedSMTPSettings{
		Configured: true,
		Enabled:    input.Enabled,
		Host:       strings.TrimSpace(input.Host),
		Port:       strings.TrimSpace(input.Port),
		Username:   strings.TrimSpace(input.Username),
		From:       strings.TrimSpace(input.From),
	}
	if next.Port == "" {
		next.Port = "587"
	}
	if input.ClearPassword {
		next.PasswordCipher = ""
	} else if input.Password != nil {
		password := *input.Password
		if password != "" {
			ciphertext, err := encryptSecret(cfg.JWTSecret, password)
			if err != nil {
				return storedSMTPSettings{}, err
			}
			next.PasswordCipher = ciphertext
		} else {
			next.PasswordCipher = current.PasswordCipher
		}
	} else {
		next.PasswordCipher = current.PasswordCipher
	}
	if !next.Enabled {
		next.Host = ""
		next.Port = "587"
		next.Username = ""
		next.PasswordCipher = ""
		next.From = ""
		return next, nil
	}
	if next.Host == "" {
		return storedSMTPSettings{}, fmt.Errorf("%w: SMTP host is required", ErrValidation)
	}
	testCfg := *cfg
	testCfg.SMTPHost = next.Host
	testCfg.SMTPPort = next.Port
	testCfg.SMTPUser = next.Username
	testCfg.SMTPPassword = "x"
	if next.PasswordCipher == "" {
		testCfg.SMTPPassword = ""
	}
	testCfg.SMTPFrom = next.From
	if err := validateSMTPConfig(&testCfg); err != nil {
		return storedSMTPSettings{}, err
	}
	return next, nil
}

func normalizeStorageInput(cfg *Config, current storedStorageSettings, input StorageSettingsInput) (storedStorageSettings, error) {
	typ := input.Type
	if typ == "" {
		typ = StorageLocal
	}
	if typ != StorageLocal && typ != StorageS3 {
		return storedStorageSettings{}, fmt.Errorf("%w: storage type must be local or s3", ErrValidation)
	}
	next := storedStorageSettings{Configured: true, Type: typ}
	if typ == StorageLocal {
		return next, nil
	}
	s3 := input.S3
	endpoint, useSSL, err := normalizeS3Endpoint(s3.Endpoint, s3.UseSSL)
	if err != nil {
		return storedStorageSettings{}, err
	}
	region := strings.TrimSpace(s3.Region)
	if region == "" {
		region = "us-east-1"
	}
	prefix := strings.Trim(strings.TrimSpace(s3.Prefix), "/")
	if prefix != "" {
		for _, segment := range strings.Split(prefix, "/") {
			if !safeStorageSegment(segment) {
				return storedStorageSettings{}, fmt.Errorf("%w: invalid S3 prefix", ErrValidation)
			}
		}
	}
	next.S3 = storedS3Settings{
		Endpoint:       endpoint,
		Bucket:         strings.TrimSpace(s3.Bucket),
		Region:         region,
		AccessKey:      strings.TrimSpace(s3.AccessKey),
		Prefix:         prefix,
		UseSSL:         useSSL,
		ForcePathStyle: s3.ForcePathStyle,
	}
	if input.S3.ClearSecretKey {
		next.S3.SecretKeyCipher = ""
	} else if input.S3.SecretKey != nil {
		secret := *input.S3.SecretKey
		if secret != "" {
			ciphertext, err := encryptSecret(cfg.JWTSecret, secret)
			if err != nil {
				return storedStorageSettings{}, err
			}
			next.S3.SecretKeyCipher = ciphertext
		} else {
			next.S3.SecretKeyCipher = current.S3.SecretKeyCipher
		}
	} else {
		next.S3.SecretKeyCipher = current.S3.SecretKeyCipher
	}
	if next.S3.Endpoint == "" {
		return storedStorageSettings{}, fmt.Errorf("%w: S3 endpoint is required", ErrValidation)
	}
	if next.S3.Bucket == "" {
		return storedStorageSettings{}, fmt.Errorf("%w: S3 bucket is required", ErrValidation)
	}
	if next.S3.AccessKey == "" || next.S3.SecretKeyCipher == "" {
		return storedStorageSettings{}, fmt.Errorf("%w: S3 access key and secret key are required", ErrValidation)
	}
	return next, nil
}

func normalizeS3Endpoint(raw string, defaultUseSSL bool) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", defaultUseSSL, nil
	}
	useSSL := defaultUseSSL
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return "", false, fmt.Errorf("%w: S3 endpoint is invalid", ErrValidation)
		}
		switch u.Scheme {
		case "https":
			useSSL = true
		case "http":
			useSSL = false
		default:
			return "", false, fmt.Errorf("%w: S3 endpoint scheme must be http or https", ErrValidation)
		}
		if strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
			return "", false, fmt.Errorf("%w: S3 endpoint must not include a path, query, or fragment", ErrValidation)
		}
		raw = u.Host
	}
	raw = strings.TrimRight(raw, "/")
	host, port, err := net.SplitHostPort(raw)
	if err == nil {
		if host == "" || port == "" {
			return "", false, fmt.Errorf("%w: S3 endpoint is invalid", ErrValidation)
		}
		return raw, useSSL, nil
	}
	if strings.Contains(raw, "/") || strings.ContainsAny(raw, " \t\r\n") {
		return "", false, fmt.Errorf("%w: S3 endpoint is invalid", ErrValidation)
	}
	return raw, useSSL, nil
}

func encryptSecret(master string, plaintext string) (string, error) {
	if len(master) < 32 {
		return "", fmt.Errorf("%w: JWT_SECRET is required to encrypt settings", ErrValidation)
	}
	block, err := aes.NewCipher(settingsKey(master))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), []byte(secretCipherPrefix))
	return strings.Join([]string{
		secretCipherPrefix,
		base64.RawURLEncoding.EncodeToString(nonce),
		base64.RawURLEncoding.EncodeToString(ciphertext),
	}, ":"), nil
}

func decryptSecret(master string, encoded string) (string, error) {
	parts := strings.Split(encoded, ":")
	if len(parts) != 3 || parts[0] != secretCipherPrefix {
		return "", fmt.Errorf("%w: unsupported encrypted settings format", ErrValidation)
	}
	nonce, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("%w: invalid encrypted settings", ErrValidation)
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("%w: invalid encrypted settings", ErrValidation)
	}
	block, err := aes.NewCipher(settingsKey(master))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(secretCipherPrefix))
	if err != nil {
		return "", fmt.Errorf("%w: could not decrypt settings secret", ErrValidation)
	}
	return string(plain), nil
}

func settingsKey(master string) []byte {
	sum := sha256.Sum256([]byte("dublyobase-settings:" + master))
	return sum[:]
}

func ValidateSMTPTestRecipient(email string) error {
	email = NormalizeEmail(email)
	if _, err := mail.ParseAddress(email); err != nil {
		return fmt.Errorf("%w: test recipient must be a valid email", ErrValidation)
	}
	return nil
}
