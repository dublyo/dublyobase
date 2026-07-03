package apis

import (
	"errors"
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

type errorBody struct {
	Error   string         `json:"error"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, errorBody{Error: code, Message: message})
}

func writeValidation(w http.ResponseWriter, message string) {
	writeError(w, http.StatusUnprocessableEntity, "validation_failed", message)
}

func writeCoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
	case errors.Is(err, core.ErrSessionExpired):
		writeError(w, http.StatusUnauthorized, "session_expired", "session expired")
	case errors.Is(err, core.ErrInvalidAuthToken):
		writeError(w, http.StatusUnauthorized, "invalid_auth_token", "invalid auth token")
	case errors.Is(err, core.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
	case errors.Is(err, core.ErrInvalidRefreshToken):
		writeError(w, http.StatusUnauthorized, "invalid_refresh_token", "invalid refresh token")
	case errors.Is(err, core.ErrAdminDisabled):
		writeError(w, http.StatusForbidden, "admin_disabled", "admin is disabled")
	case errors.Is(err, core.ErrUserDisabled):
		writeError(w, http.StatusForbidden, "user_disabled", "user is disabled")
	case errors.Is(err, core.ErrSetupClosed):
		writeError(w, http.StatusGone, "setup_closed", "setup is closed")
	case errors.Is(err, core.ErrUserExists):
		writeError(w, http.StatusConflict, "user_exists", "user already exists")
	case errors.Is(err, core.ErrProjectExists):
		writeError(w, http.StatusConflict, "project_exists", "project already exists")
	case errors.Is(err, core.ErrProjectNotFound):
		writeError(w, http.StatusNotFound, "project_not_found", "project not found")
	case errors.Is(err, core.ErrProvisioningConflict):
		writeError(w, http.StatusConflict, "provisioning_conflict", "project database objects already exist")
	case errors.Is(err, core.ErrCollectionExists):
		writeError(w, http.StatusConflict, "collection_exists", "collection already exists")
	case errors.Is(err, core.ErrCollectionNotFound):
		writeError(w, http.StatusNotFound, "collection_not_found", "collection not found")
	case errors.Is(err, core.ErrDestructiveChange):
		writeError(w, http.StatusConflict, "destructive_change", "destructive schema change requires explicit confirmation")
	case errors.Is(err, core.ErrNotImplemented):
		writeError(w, http.StatusNotImplemented, "not_implemented", "feature not implemented")
	case errors.Is(err, core.ErrSchemaDrift):
		writeError(w, http.StatusInternalServerError, "schema_drift", "collection metadata and database schema are out of sync")
	case errors.Is(err, core.ErrRecordNotFound):
		writeError(w, http.StatusNotFound, "record_not_found", "record not found")
	case errors.Is(err, core.ErrInvalidFilter):
		writeError(w, http.StatusUnprocessableEntity, "invalid_filter", err.Error())
	case errors.Is(err, core.ErrInvalidRule):
		writeError(w, http.StatusUnprocessableEntity, "invalid_rule", err.Error())
	case errors.Is(err, core.ErrFileNotFound):
		writeError(w, http.StatusNotFound, "file_not_found", "file not found")
	case errors.Is(err, core.ErrFileTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "file exceeds the configured upload limit")
	case errors.Is(err, core.ErrRLSDenied):
		writeError(w, http.StatusForbidden, "rls_denied", "record access denied by RLS")
	case errors.Is(err, core.ErrValidation):
		writeValidation(w, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
