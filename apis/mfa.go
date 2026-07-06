package apis

import (
	"net/http"
	"time"

	"github.com/dublyo/dublyobase/core"
)

type mfaEnrollRequest struct {
	Name string `json:"name"`
}

type mfaConfirmRequest struct {
	FactorID string `json:"factorId"`
	Code     string `json:"code"`
}

type mfaVerifyRequest struct {
	MFAToken string `json:"mfaToken"`
	Code     string `json:"code"`
}

func (s *server) appMFAEnroll(w http.ResponseWriter, r *http.Request) {
	if !s.checkProjectQuota(w, r, r.PathValue("slug"), true) {
		return
	}
	var req mfaEnrollRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := core.StartMFAEnrollment(r.Context(), s.app.Pool, s.app.Config, r.PathValue("slug"), bearerToken(r), req.Name, time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *server) appMFAConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.checkProjectQuota(w, r, r.PathValue("slug"), true) {
		return
	}
	var req mfaConfirmRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := core.ConfirmMFAEnrollment(r.Context(), s.app.Pool, s.app.Config, r.PathValue("slug"), bearerToken(r), req.FactorID, req.Code, time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) appMFAVerify(w http.ResponseWriter, r *http.Request) {
	if !s.checkProjectQuota(w, r, r.PathValue("slug"), true) {
		return
	}
	var req mfaVerifyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := core.VerifyMFALogin(r.Context(), s.app.Pool, s.app.Config, r.PathValue("slug"), req.MFAToken, req.Code, s.clientIP(r), r.UserAgent(), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) appMFARecovery(w http.ResponseWriter, r *http.Request) {
	if !s.checkProjectQuota(w, r, r.PathValue("slug"), true) {
		return
	}
	var req mfaVerifyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := core.VerifyMFARecoveryLogin(r.Context(), s.app.Pool, s.app.Config, r.PathValue("slug"), req.MFAToken, req.Code, s.clientIP(r), r.UserAgent(), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) appMFADisable(w http.ResponseWriter, r *http.Request) {
	if !s.checkProjectQuota(w, r, r.PathValue("slug"), true) {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := core.DisableMFA(r.Context(), s.app.Pool, s.app.Config, r.PathValue("slug"), bearerToken(r), req.Code, time.Now()); err != nil {
		writeCoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
