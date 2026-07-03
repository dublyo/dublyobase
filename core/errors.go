package core

import "errors"

var (
	ErrAdminDisabled        = errors.New("admin disabled")
	ErrInvalidAuthToken     = errors.New("invalid auth token")
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrInvalidRefreshToken  = errors.New("invalid refresh token")
	ErrProjectExists        = errors.New("project exists")
	ErrProjectNotFound      = errors.New("project not found")
	ErrProvisioningConflict = errors.New("provisioning conflict")
	ErrRecordNotFound       = errors.New("record not found")
	ErrInvalidFilter        = errors.New("invalid filter")
	ErrInvalidRule          = errors.New("invalid rule")
	ErrFileNotFound         = errors.New("file not found")
	ErrFileTooLarge         = errors.New("file too large")
	ErrUploadConflict       = errors.New("upload conflict")
	ErrUploadExpired        = errors.New("upload expired")
	ErrUploadNotFound       = errors.New("upload not found")
	ErrChecksumMismatch     = errors.New("checksum mismatch")
	ErrRLSDenied            = errors.New("rls denied")
	ErrSessionExpired       = errors.New("session expired")
	ErrSetupClosed          = errors.New("setup closed")
	ErrUnauthorized         = errors.New("unauthorized")
	ErrUserDisabled         = errors.New("user disabled")
	ErrUserExists           = errors.New("user exists")
	ErrValidation           = errors.New("validation failed")
)
