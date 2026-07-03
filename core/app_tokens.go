package core

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	appAccessTokenTTL   = time.Hour
	appRefreshTokenTTL  = 7 * 24 * time.Hour
	emailVerifyTokenTTL = 24 * time.Hour
	passwordResetTTL    = time.Hour

	appRefreshTokenPrefix  = "dbo_refresh_"
	emailVerifyTokenPrefix = "dbo_verify_"
	passwordResetPrefix    = "dbo_reset_"
)

type appClaims struct {
	Role       string `json:"role"`
	Project    string `json:"project"`
	Collection string `json:"collection,omitempty"`
	TokenKey   string `json:"token_key,omitempty"`
	jwt.RegisteredClaims
}

func generateOpaqueToken(prefix string) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func generateTokenKey() (string, error) {
	return generateOpaqueToken("")
}

func GenerateAppRefreshToken() (string, error) {
	return generateOpaqueToken(appRefreshTokenPrefix)
}

func GenerateEmailVerificationToken() (string, error) {
	return generateOpaqueToken(emailVerifyTokenPrefix)
}

func GeneratePasswordResetToken() (string, error) {
	return generateOpaqueToken(passwordResetPrefix)
}

func GenerateAppAccessToken(secret string, projectSlug string, userID string, tokenKey string, now time.Time) (string, time.Time, error) {
	if len(secret) < 32 {
		return "", time.Time{}, ErrUnauthorized
	}
	expiresAt := now.UTC().Add(appAccessTokenTTL)
	claims := appClaims{
		Role:       string(RecordRoleAuthenticated),
		Project:    projectSlug,
		Collection: "users",
		TokenKey:   tokenKey,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now.UTC()),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func parseAppJWT(secret string, token string, now time.Time) (*appClaims, error) {
	if len(secret) < 32 {
		return nil, ErrInvalidAuthToken
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrInvalidAuthToken
	}
	claims := &appClaims{}
	parsed, err := jwt.ParseWithClaims(
		token,
		claims,
		func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return []byte(secret), nil
		},
		jwt.WithTimeFunc(func() time.Time { return now.UTC() }),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil || !parsed.Valid {
		return nil, ErrInvalidAuthToken
	}
	if claims.Subject == "" || claims.Project == "" || claims.Role == "" || claims.TokenKey == "" {
		return nil, ErrInvalidAuthToken
	}
	return claims, nil
}
