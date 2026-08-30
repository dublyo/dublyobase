package core

import "errors"

var (
	ErrAdminDisabled          = errors.New("admin disabled")
	ErrAdminExists            = errors.New("admin exists")
	ErrForbidden              = errors.New("forbidden")
	ErrInvalidAuthToken       = errors.New("invalid auth token")
	ErrInvalidCredentials     = errors.New("invalid credentials")
	ErrInvalidRefreshToken    = errors.New("invalid refresh token")
	ErrPasswordChangeRequired = errors.New("password change required")
	ErrProjectExists          = errors.New("project exists")
	ErrProjectNotFound        = errors.New("project not found")
	ErrProvisioningConflict   = errors.New("provisioning conflict")
	ErrQuotaExceeded          = errors.New("quota exceeded")
	ErrRecordConflict         = errors.New("record conflict")
	ErrRecordNotFound         = errors.New("record not found")
	// ErrTimeout is a query the database cancelled for taking too long. It is a
	// transient load condition rather than a fault in the request, so it must
	// not read as an internal error.
	ErrTimeout          = errors.New("the database cancelled the query for taking too long")
	ErrInvalidFilter    = errors.New("invalid filter")
	ErrInvalidRule      = errors.New("invalid rule")
	ErrFileNotFound     = errors.New("file not found")
	ErrFileTooLarge     = errors.New("file too large")
	ErrUploadConflict   = errors.New("upload conflict")
	ErrUploadExpired    = errors.New("upload expired")
	ErrUploadNotFound   = errors.New("upload not found")
	ErrChecksumMismatch = errors.New("checksum mismatch")
	ErrRLSDenied        = errors.New("rls denied")
	ErrSessionExpired   = errors.New("session expired")
	ErrSetupClosed      = errors.New("setup closed")
	ErrUnauthorized     = errors.New("unauthorized")
	ErrUserDisabled     = errors.New("user disabled")
	ErrUserExists       = errors.New("user exists")
	ErrValidation       = errors.New("validation failed")
)
