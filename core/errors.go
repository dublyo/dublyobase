package core

import "errors"

var (
	ErrAdminDisabled        = errors.New("admin disabled")
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrProjectExists        = errors.New("project exists")
	ErrProjectNotFound      = errors.New("project not found")
	ErrProvisioningConflict = errors.New("provisioning conflict")
	ErrSessionExpired       = errors.New("session expired")
	ErrSetupClosed          = errors.New("setup closed")
	ErrUnauthorized         = errors.New("unauthorized")
	ErrValidation           = errors.New("validation failed")
)
