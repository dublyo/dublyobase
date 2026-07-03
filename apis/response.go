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
	case errors.Is(err, core.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
	case errors.Is(err, core.ErrAdminDisabled):
		writeError(w, http.StatusForbidden, "admin_disabled", "admin is disabled")
	case errors.Is(err, core.ErrSetupClosed):
		writeError(w, http.StatusGone, "setup_closed", "setup is closed")
	case errors.Is(err, core.ErrProjectExists):
		writeError(w, http.StatusConflict, "project_exists", "project already exists")
	case errors.Is(err, core.ErrProjectNotFound):
		writeError(w, http.StatusNotFound, "project_not_found", "project not found")
	case errors.Is(err, core.ErrProvisioningConflict):
		writeError(w, http.StatusConflict, "provisioning_conflict", "project database objects already exist")
	case errors.Is(err, core.ErrValidation):
		writeValidation(w, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
