package apis

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dublyo/dublyobase/core"
)

type cronJobRequest struct {
	ProjectSlug    string            `json:"projectSlug"`
	Name           string            `json:"name"`
	Type           string            `json:"type"`
	Schedule       string            `json:"schedule"`
	Timezone       string            `json:"timezone"`
	Enabled        *bool             `json:"enabled"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	RetryCount     int               `json:"retryCount"`
	Method         string            `json:"method"`
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers"`
	Body           string            `json:"body"`
}

type backupJobRequest struct {
	Name           string `json:"name"`
	Scope          string `json:"scope"`
	ProjectSlug    string `json:"projectSlug"`
	Schedule       string `json:"schedule"`
	Timezone       string `json:"timezone"`
	Enabled        *bool  `json:"enabled"`
	RetentionDays  int    `json:"retentionDays"`
	RetentionCount int    `json:"retentionCount"`
}

func (s *server) adminListCronJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := core.ListCronJobs(r.Context(), s.app.Pool)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": jobs})
}

func (s *server) adminCreateCronJob(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	var req cronJobRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	input := core.CronJobInput{
		ProjectSlug:     req.ProjectSlug,
		Name:            req.Name,
		Type:            req.Type,
		Schedule:        req.Schedule,
		Timezone:        req.Timezone,
		TimeoutSeconds:  req.TimeoutSeconds,
		RetryCount:      req.RetryCount,
		Method:          req.Method,
		URL:             req.URL,
		Headers:         req.Headers,
		Body:            req.Body,
		EnabledProvided: req.Enabled != nil,
	}
	if req.Enabled != nil {
		input.Enabled = *req.Enabled
	}
	job, err := core.CreateCronJob(r.Context(), s.app.Pool, auth.Admin.ID, input, s.clientIP(r), r.UserAgent())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (s *server) adminUpdateCronJob(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	var req cronJobRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	input := core.CronJobInput{
		ProjectSlug:     req.ProjectSlug,
		Name:            req.Name,
		Type:            req.Type,
		Schedule:        req.Schedule,
		Timezone:        req.Timezone,
		TimeoutSeconds:  req.TimeoutSeconds,
		RetryCount:      req.RetryCount,
		Method:          req.Method,
		URL:             req.URL,
		Headers:         req.Headers,
		Body:            req.Body,
		EnabledProvided: req.Enabled != nil,
	}
	if req.Enabled != nil {
		input.Enabled = *req.Enabled
	}
	job, err := core.UpdateCronJob(r.Context(), s.app.Pool, auth.Admin.ID, r.PathValue("id"), input, s.clientIP(r), r.UserAgent())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *server) adminDeleteCronJob(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if err := core.DeleteCronJob(r.Context(), s.app.Pool, auth.Admin.ID, r.PathValue("id"), s.clientIP(r), r.UserAgent()); err != nil {
		writeCoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) adminListCronRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := core.ListCronRuns(r.Context(), s.app.Pool, r.PathValue("id"), 30)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

func (s *server) adminRunCronJob(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	run, err := core.RunCronJob(r.Context(), s.app.Pool, auth.Admin.ID, r.PathValue("id"), s.clientIP(r), r.UserAgent())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *server) adminListBackupJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := core.ListBackupJobs(r.Context(), s.app.Pool)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": jobs})
}

func (s *server) adminCreateBackupJob(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	var req backupJobRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	input := core.BackupJobInput{
		Name:            req.Name,
		Scope:           req.Scope,
		ProjectSlug:     req.ProjectSlug,
		Schedule:        req.Schedule,
		Timezone:        req.Timezone,
		RetentionDays:   req.RetentionDays,
		RetentionCount:  req.RetentionCount,
		EnabledProvided: req.Enabled != nil,
	}
	if req.Enabled != nil {
		input.Enabled = *req.Enabled
	}
	job, err := core.CreateBackupJob(r.Context(), s.app.Pool, auth.Admin.ID, input, s.clientIP(r), r.UserAgent())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (s *server) adminListBackupRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := core.ListBackupRuns(r.Context(), s.app.Pool, r.PathValue("id"), 30)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

func (s *server) adminRunBackupJob(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	storageCfg, err := core.EffectiveStorageConfig(r.Context(), s.app.Pool, s.app.Config)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	run, err := core.RunBackupJob(r.Context(), s.app.Pool, storageCfg, auth.Admin.ID, r.PathValue("id"), s.clientIP(r), r.UserAgent())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *server) adminDownloadBackupRun(w http.ResponseWriter, r *http.Request) {
	storageCfg, err := core.EffectiveStorageConfig(r.Context(), s.app.Pool, s.app.Config)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	run, obj, err := core.OpenBackupRun(r.Context(), s.app.Pool, storageCfg, r.PathValue("id"), r.PathValue("runId"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	defer obj.Body.Close()
	name := filepath.Base(run.StorageKey)
	if name == "." || name == "/" || strings.TrimSpace(name) == "" {
		name = run.ID + ".dump"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, name))
	if obj.Info.Size > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", obj.Info.Size))
	}
	if _, err := io.Copy(w, obj.Body); err != nil {
		s.app.Log.Warn("backup download stream failed", "run", run.ID, "err", err)
	}
}

func (s *server) adminRestoreBackup(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "bad_multipart", "restore requires multipart form data")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeCoreError(w, fmt.Errorf("%w: restore file is required", core.ErrValidation))
		return
	}
	defer file.Close()
	tmp, err := os.CreateTemp("", "dublyobase-restore-*.dump")
	if err != nil {
		writeCoreError(w, err)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, io.LimitReader(file, 512<<20)); err != nil {
		tmp.Close()
		writeCoreError(w, err)
		return
	}
	if err := tmp.Close(); err != nil {
		writeCoreError(w, err)
		return
	}
	job, err := core.RunRestoreFile(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		auth.Admin.ID,
		r.FormValue("mode"),
		header.Filename,
		tmpPath,
		r.FormValue("confirm"),
		s.clientIP(r),
		r.UserAgent(),
	)
	if err != nil {
		if job != nil {
			writeJSON(w, http.StatusUnprocessableEntity, job)
			return
		}
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, job)
}
