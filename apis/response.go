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

func writeErrorDetails(w http.ResponseWriter, status int, code string, message string, details map[string]any) {
	writeJSON(w, status, errorBody{Error: code, Message: message, Details: details})
}

func writeValidation(w http.ResponseWriter, message string) {
	writeError(w, http.StatusUnprocessableEntity, "validation_failed", message)
}

func writeCoreError(w http.ResponseWriter, err error) {
	status, code, message, details := coreErrorResponse(err)
	if details != nil {
		writeErrorDetails(w, status, code, message, details)
		return
	}
	writeError(w, status, code, message)
}

func coreErrorResponse(err error) (int, string, string, map[string]any) {
	switch {
	case errors.Is(err, core.ErrUnauthorized):
		return http.StatusUnauthorized, "unauthorized", "authentication required", nil
	case errors.Is(err, core.ErrSessionExpired):
		return http.StatusUnauthorized, "session_expired", "session expired", nil
	case errors.Is(err, core.ErrInvalidAuthToken):
		return http.StatusUnauthorized, "invalid_auth_token", "invalid auth token", nil
	case errors.Is(err, core.ErrInvalidCredentials):
		return http.StatusUnauthorized, "invalid_credentials", "invalid email or password", nil
	case errors.Is(err, core.ErrInvalidRefreshToken):
		return http.StatusUnauthorized, "invalid_refresh_token", "invalid refresh token", nil
	case errors.Is(err, core.ErrAdminDisabled):
		return http.StatusForbidden, "admin_disabled", "admin is disabled", nil
	case errors.Is(err, core.ErrForbidden):
		return http.StatusForbidden, "forbidden", "action is not allowed", nil
	case errors.Is(err, core.ErrPasswordChangeRequired):
		return http.StatusForbidden, "password_change_required", "admin password must be changed before accessing the control panel", nil
	case errors.Is(err, core.ErrUserDisabled):
		return http.StatusForbidden, "user_disabled", "user is disabled", nil
	case errors.Is(err, core.ErrSetupClosed):
		return http.StatusGone, "setup_closed", "setup is closed", nil
	case errors.Is(err, core.ErrAdminExists):
		return http.StatusConflict, "admin_exists", "admin already exists", nil
	case errors.Is(err, core.ErrUserExists):
		return http.StatusConflict, "user_exists", "user already exists", nil
	case errors.Is(err, core.ErrProjectExists):
		return http.StatusConflict, "project_exists", "project already exists", nil
	case errors.Is(err, core.ErrProjectNotFound):
		return http.StatusNotFound, "project_not_found", "project not found", nil
	case errors.Is(err, core.ErrProvisioningConflict):
		return http.StatusConflict, "provisioning_conflict", "project database objects already exist", nil
	case errors.Is(err, core.ErrQuotaExceeded):
		return http.StatusTooManyRequests, "quota_exceeded", err.Error(), nil
	case errors.Is(err, core.ErrRecordConflict):
		return http.StatusConflict, "record_conflict", err.Error(), nil
	case errors.Is(err, core.ErrCollectionExists):
		return http.StatusConflict, "collection_exists", "collection already exists", nil
	case errors.Is(err, core.ErrCollectionNotFound):
		return http.StatusNotFound, "collection_not_found", "collection not found", nil
	case errors.Is(err, core.ErrDestructiveChange):
		return http.StatusConflict, "destructive_change", "destructive schema change requires explicit confirmation", nil
	case errors.Is(err, core.ErrNotImplemented):
		return http.StatusNotImplemented, "not_implemented", "feature not implemented", nil
	case errors.Is(err, core.ErrSchemaDrift):
		return http.StatusInternalServerError, "schema_drift", "collection metadata and database schema are out of sync", nil
	case errors.Is(err, core.ErrRecordNotFound):
		return http.StatusNotFound, "record_not_found", "record not found", nil
	case errors.Is(err, core.ErrInvalidFilter):
		return http.StatusUnprocessableEntity, "invalid_filter", err.Error(), nil
	case errors.Is(err, core.ErrInvalidRule):
		return http.StatusUnprocessableEntity, "invalid_rule", err.Error(), nil
	case errors.Is(err, core.ErrFileNotFound):
		return http.StatusNotFound, "file_not_found", "file not found", nil
	case errors.Is(err, core.ErrFileTooLarge):
		return http.StatusRequestEntityTooLarge, "file_too_large", "file exceeds the configured upload limit", nil
	case errors.Is(err, core.ErrUploadNotFound):
		return http.StatusNotFound, "upload_not_found", "upload not found", nil
	case errors.Is(err, core.ErrUploadExpired):
		return http.StatusGone, "upload_expired", "upload expired", nil
	case errors.Is(err, core.ErrUploadConflict):
		return http.StatusConflict, "upload_conflict", "upload session is not open", nil
	case errors.Is(err, core.ErrChecksumMismatch):
		return http.StatusUnprocessableEntity, "checksum_mismatch", "checksum does not match uploaded bytes", nil
	case errors.Is(err, core.ErrRLSDenied):
		return http.StatusForbidden, "rls_denied", "record access denied by RLS", map[string]any{"policy": "record_access"}
	case errors.Is(err, core.ErrValidation):
		return http.StatusUnprocessableEntity, "validation_failed", err.Error(), nil
	default:
		return http.StatusInternalServerError, "internal_error", "internal server error", nil
	}
}
