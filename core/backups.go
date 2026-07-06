package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const backupRunnerLockID int64 = 326_326_008

type BackupJob struct {
	ID             string     `json:"id"`
	ProjectID      *string    `json:"projectId,omitempty"`
	ProjectSlug    string     `json:"projectSlug,omitempty"`
	Name           string     `json:"name"`
	Scope          string     `json:"scope"`
	Schedule       string     `json:"schedule"`
	Timezone       string     `json:"timezone"`
	Enabled        bool       `json:"enabled"`
	RetentionDays  int        `json:"retentionDays"`
	RetentionCount int        `json:"retentionCount"`
	LastRunAt      *time.Time `json:"lastRunAt,omitempty"`
	NextRunAt      *time.Time `json:"nextRunAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type BackupJobInput struct {
	Name            string `json:"name"`
	Scope           string `json:"scope"`
	ProjectSlug     string `json:"projectSlug,omitempty"`
	Schedule        string `json:"schedule"`
	Timezone        string `json:"timezone"`
	Enabled         bool   `json:"enabled"`
	EnabledProvided bool   `json:"-"`
	RetentionDays   int    `json:"retentionDays"`
	RetentionCount  int    `json:"retentionCount"`
}

type BackupRun struct {
	ID         string     `json:"id"`
	JobID      string     `json:"jobId"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	StorageKey string     `json:"storageKey"`
	SizeBytes  int64      `json:"sizeBytes"`
	Error      string     `json:"error"`
}

type RestoreJob struct {
	ID         string     `json:"id"`
	AdminID    *string    `json:"adminId,omitempty"`
	Mode       string     `json:"mode"`
	Source     string     `json:"source"`
	FileName   string     `json:"fileName"`
	Status     string     `json:"status"`
	Output     string     `json:"output"`
	Error      string     `json:"error"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

func ValidateBackupJobInput(input *BackupJobInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return fmt.Errorf("%w: backup name is required", ErrValidation)
	}
	if input.Scope == "" {
		input.Scope = "project"
	}
	if input.Scope != "full" && input.Scope != "project" {
		return fmt.Errorf("%w: backup scope must be full or project", ErrValidation)
	}
	if input.Scope == "project" && strings.TrimSpace(input.ProjectSlug) == "" {
		return fmt.Errorf("%w: project backup requires projectSlug", ErrValidation)
	}
	if input.Scope == "full" && strings.TrimSpace(input.ProjectSlug) != "" {
		return fmt.Errorf("%w: full backup must not set projectSlug", ErrValidation)
	}
	if input.Timezone == "" {
		input.Timezone = "UTC"
	}
	if input.RetentionDays == 0 {
		input.RetentionDays = 14
	}
	if input.RetentionDays < 1 || input.RetentionDays > 3650 {
		return fmt.Errorf("%w: retentionDays must be between 1 and 3650", ErrValidation)
	}
	if input.RetentionCount == 0 {
		input.RetentionCount = 10
	}
	if input.RetentionCount < 1 || input.RetentionCount > 1000 {
		return fmt.Errorf("%w: retentionCount must be between 1 and 1000", ErrValidation)
	}
	if _, err := NextScheduledTime(input.Schedule, input.Timezone, time.Now().UTC()); err != nil {
		return err
	}
	return nil
}

func CreateBackupJob(ctx context.Context, pool *pgxpool.Pool, adminID string, input BackupJobInput, ip string, userAgent string) (*BackupJob, error) {
	if !input.EnabledProvided {
		input.Enabled = true
	}
	if err := ValidateBackupJobInput(&input); err != nil {
		return nil, err
	}
	var projectID *string
	if input.Scope == "project" {
		project, err := GetProject(ctx, pool, input.ProjectSlug)
		if err != nil {
			return nil, err
		}
		projectID = &project.ID
	}
	next, err := NextScheduledTime(input.Schedule, input.Timezone, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	job, err := scanBackupJob(tx.QueryRow(ctx, `
		insert into _dbo.backup_jobs
			(project_id, name, scope, schedule, timezone, enabled, retention_days, retention_count, next_run_at, created_by_admin_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		returning id, project_id, coalesce((select slug from _dbo.projects where id = project_id), ''), name, scope, schedule, timezone, enabled, retention_days, retention_count, last_run_at, next_run_at, created_at, updated_at`,
		projectID,
		input.Name,
		input.Scope,
		input.Schedule,
		input.Timezone,
		input.Enabled,
		input.RetentionDays,
		input.RetentionCount,
		next,
		nullString(adminID),
	))
	if err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "backup.create",
		TargetType: "backup_job",
		TargetID:   job.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"name": job.Name, "scope": job.Scope, "project": job.ProjectSlug},
	}); err != nil {
		return nil, err
	}
	return job, tx.Commit(ctx)
}

func ListBackupJobs(ctx context.Context, pool *pgxpool.Pool) ([]BackupJob, error) {
	rows, err := pool.Query(ctx, `
		select b.id, b.project_id, coalesce(p.slug, ''), b.name, b.scope, b.schedule, b.timezone, b.enabled, b.retention_days, b.retention_count, b.last_run_at, b.next_run_at, b.created_at, b.updated_at
		from _dbo.backup_jobs b
		left join _dbo.projects p on p.id = b.project_id
		order by b.created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BackupJob, 0)
	for rows.Next() {
		job, err := scanBackupJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	return out, rows.Err()
}

func ListBackupRuns(ctx context.Context, pool *pgxpool.Pool, jobID string, limit int) ([]BackupRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := pool.Query(ctx, `
		select id, job_id, status, started_at, finished_at, storage_key, size_bytes, error
		from _dbo.backup_runs
		where job_id = $1
		order by started_at desc
		limit $2`, jobID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BackupRun, 0)
	for rows.Next() {
		run, err := scanBackupRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	return out, rows.Err()
}

func OpenBackupRun(ctx context.Context, pool *pgxpool.Pool, cfg *Config, jobID string, runID string) (*BackupRun, *StoredObject, error) {
	run, err := getBackupRun(ctx, pool, jobID, runID)
	if err != nil {
		return nil, nil, err
	}
	if run.Status != "success" || strings.TrimSpace(run.StorageKey) == "" {
		return nil, nil, fmt.Errorf("%w: backup run has no downloadable artifact", ErrValidation)
	}
	store, err := NewObjectStore(cfg)
	if err != nil {
		return nil, nil, err
	}
	obj, err := store.Get(ctx, run.StorageKey)
	if err != nil {
		return nil, nil, err
	}
	return run, obj, nil
}

func RunBackupJob(ctx context.Context, pool *pgxpool.Pool, cfg *Config, adminID string, jobID string, ip string, userAgent string) (*BackupRun, error) {
	job, err := getBackupJob(ctx, pool, jobID)
	if err != nil {
		return nil, err
	}
	run, err := executeBackupJob(ctx, pool, cfg, job)
	if err != nil {
		return nil, err
	}
	_ = InsertAudit(ctx, pool, AuditEvent{
		AdminID:    &adminID,
		Action:     "backup.run",
		TargetType: "backup_job",
		TargetID:   job.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"name": job.Name, "scope": job.Scope, "status": run.Status, "storageKey": run.StorageKey},
	})
	return run, nil
}

func RunRestoreFile(ctx context.Context, pool *pgxpool.Pool, cfg *Config, adminID string, mode string, fileName string, filePath string, confirm string, ip string, userAgent string) (*RestoreJob, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "dry_run"
	}
	if mode != "dry_run" && mode != "restore" {
		return nil, fmt.Errorf("%w: restore mode must be dry_run or restore", ErrValidation)
	}
	if mode == "restore" && strings.TrimSpace(confirm) != "RESTORE_DATABASE" {
		return nil, fmt.Errorf("%w: restore requires confirm=RESTORE_DATABASE", ErrValidation)
	}
	if strings.TrimSpace(filePath) == "" {
		return nil, fmt.Errorf("%w: restore file is required", ErrValidation)
	}
	job, err := createRestoreJob(ctx, pool, adminID, mode, "upload", filepath.Base(fileName))
	if err != nil {
		return nil, err
	}
	output, runErr := runRestoreCommand(ctx, cfg, mode, filePath)
	if err := finishRestoreJob(ctx, pool, job.ID, output, runErr); err != nil {
		return nil, err
	}
	job.Output = output
	status := "success"
	if runErr != nil {
		status = "error"
		job.Error = runErr.Error()
	}
	job.Status = status
	now := time.Now().UTC()
	job.FinishedAt = &now
	_ = InsertAudit(ctx, pool, AuditEvent{
		AdminID:    &adminID,
		Action:     "backup.restore." + mode,
		TargetType: "restore_job",
		TargetID:   job.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"mode": mode, "status": status, "fileName": filepath.Base(fileName)},
	})
	if runErr != nil {
		return job, runErr
	}
	return job, nil
}

func RunDueBackupJobs(ctx context.Context, pool *pgxpool.Pool, cfg *Config, now time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err := tx.QueryRow(ctx, `select pg_try_advisory_xact_lock($1)`, backupRunnerLockID).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	rows, err := tx.Query(ctx, `
		select b.id, b.project_id, coalesce(p.slug, ''), b.name, b.scope, b.schedule, b.timezone, b.enabled, b.retention_days, b.retention_count, b.last_run_at, b.next_run_at, b.created_at, b.updated_at
		from _dbo.backup_jobs b
		left join _dbo.projects p on p.id = b.project_id
		where b.enabled and b.next_run_at is not null and b.next_run_at <= $1
		order by b.next_run_at asc
		limit 2
		for update of b skip locked`, now.UTC())
	if err != nil {
		return err
	}
	var jobs []BackupJob
	for rows.Next() {
		job, err := scanBackupJob(rows)
		if err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, *job)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for i := range jobs {
		if _, err := executeBackupJob(ctx, pool, cfg, &jobs[i]); err != nil && ctx.Err() != nil {
			return err
		}
	}
	return nil
}

func getBackupJob(ctx context.Context, pool *pgxpool.Pool, id string) (*BackupJob, error) {
	job, err := scanBackupJob(pool.QueryRow(ctx, `
		select b.id, b.project_id, coalesce(p.slug, ''), b.name, b.scope, b.schedule, b.timezone, b.enabled, b.retention_days, b.retention_count, b.last_run_at, b.next_run_at, b.created_at, b.updated_at
		from _dbo.backup_jobs b
		left join _dbo.projects p on p.id = b.project_id
		where b.id = $1`, id))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrValidation
		}
		return nil, err
	}
	return job, nil
}

func getBackupRun(ctx context.Context, pool *pgxpool.Pool, jobID string, runID string) (*BackupRun, error) {
	run, err := scanBackupRun(pool.QueryRow(ctx, `
		select id, job_id, status, started_at, finished_at, storage_key, size_bytes, error
		from _dbo.backup_runs
		where job_id = $1 and id = $2`,
		jobID,
		runID,
	))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrValidation
		}
		return nil, err
	}
	return run, nil
}

func runRestoreCommand(ctx context.Context, cfg *Config, mode string, filePath string) (string, error) {
	args := []string{}
	if mode == "dry_run" {
		args = []string{"--list", filePath}
	} else {
		args = []string{"--clean", "--if-exists", "--no-owner", "--no-privileges", "--dbname", cfg.DatabaseURL, filePath}
	}
	cmd := exec.CommandContext(ctx, "pg_restore", args...)
	cmd.Env = append(os.Environ(), "PGCONNECT_TIMEOUT=15")
	output, err := cmd.CombinedOutput()
	out := strings.TrimSpace(string(output))
	if len(out) > 20000 {
		out = out[:20000]
	}
	if err != nil {
		return out, fmt.Errorf("pg_restore failed: %s", out)
	}
	return out, nil
}

func createRestoreJob(ctx context.Context, pool *pgxpool.Pool, adminID string, mode string, source string, fileName string) (*RestoreJob, error) {
	return scanRestoreJob(pool.QueryRow(ctx, `
		insert into _dbo.restore_jobs (admin_id, mode, source, file_name, status)
		values ($1, $2, $3, $4, 'running')
		returning id, admin_id, mode, source, file_name, status, output, error, created_at, finished_at`,
		nullString(adminID),
		mode,
		source,
		fileName,
	))
}

func finishRestoreJob(ctx context.Context, pool *pgxpool.Pool, jobID string, output string, runErr error) error {
	status := "success"
	errText := ""
	if runErr != nil {
		status = "error"
		errText = runErr.Error()
	}
	_, err := pool.Exec(ctx, `
		update _dbo.restore_jobs
		set status = $1, output = $2, error = $3, finished_at = now()
		where id = $4`,
		status,
		output,
		errText,
		jobID,
	)
	return err
}

func executeBackupJob(ctx context.Context, pool *pgxpool.Pool, cfg *Config, job *BackupJob) (*BackupRun, error) {
	run, err := createBackupRun(ctx, pool, job.ID)
	if err != nil {
		return nil, err
	}
	key, size, runErr := dumpAndStoreBackup(ctx, pool, cfg, job)
	status := "success"
	if runErr != nil {
		status = "error"
	}
	if err := finishBackupRun(ctx, pool, run.ID, status, key, size, runErr); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	next, nextErr := NextScheduledTime(job.Schedule, job.Timezone, now)
	if nextErr == nil {
		_, _ = pool.Exec(ctx, `update _dbo.backup_jobs set last_run_at = $1, next_run_at = $2, updated_at = now() where id = $3`, now, next, job.ID)
	}
	run.Status = status
	run.StorageKey = key
	run.SizeBytes = size
	if runErr != nil {
		run.Error = runErr.Error()
	}
	return run, nil
}

func dumpAndStoreBackup(ctx context.Context, pool *pgxpool.Pool, cfg *Config, job *BackupJob) (string, int64, error) {
	tmp, err := os.CreateTemp("", "dublyobase-backup-*.dump")
	if err != nil {
		return "", 0, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	args := pgDumpBaseArgs(cfg.DatabaseURL, tmpPath)
	if job.Scope == "project" {
		project, err := GetProject(ctx, pool, job.ProjectSlug)
		if err != nil {
			return "", 0, err
		}
		args = append(args, "--schema", project.SchemaName)
	}
	cmd := exec.CommandContext(ctx, "pg_dump", args...)
	cmd.Env = append(os.Environ(), "PGCONNECT_TIMEOUT=15")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", 0, fmt.Errorf("pg_dump failed: %s", strings.TrimSpace(string(output)))
	}
	file, err := os.Open(tmpPath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", 0, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	store, err := NewObjectStore(cfg)
	if err != nil {
		return "", 0, err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	name := strings.NewReplacer(" ", "-", "/", "-", "\\", "-").Replace(strings.ToLower(job.Name))
	if name == "" {
		name = job.Scope
	}
	key := filepath.ToSlash(filepath.Join("backups", job.Scope, stamp+"-"+name+".dump"))
	if err := store.Put(ctx, key, file, stat.Size(), "application/octet-stream", hex.EncodeToString(hash.Sum(nil))); err != nil {
		return "", 0, err
	}
	return key, stat.Size(), nil
}

func pgDumpBaseArgs(databaseURL string, filePath string) []string {
	return []string{"--format=custom", "--no-owner", "--no-privileges", "--dbname", databaseURL, "--file", filePath}
}

func createBackupRun(ctx context.Context, pool *pgxpool.Pool, jobID string) (*BackupRun, error) {
	return scanBackupRun(pool.QueryRow(ctx, `
		insert into _dbo.backup_runs (job_id, status)
		values ($1, 'running')
		returning id, job_id, status, started_at, finished_at, storage_key, size_bytes, error`, jobID))
}

func finishBackupRun(ctx context.Context, pool *pgxpool.Pool, runID string, status string, key string, size int64, runErr error) error {
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}
	_, err := pool.Exec(ctx, `
		update _dbo.backup_runs
		set status = $1, storage_key = $2, size_bytes = $3, error = $4, finished_at = now()
		where id = $5`, status, key, size, errText, runID)
	return err
}

type backupJobScanner interface{ Scan(dest ...any) error }

func scanBackupJob(row backupJobScanner) (*BackupJob, error) {
	var job BackupJob
	if err := row.Scan(&job.ID, &job.ProjectID, &job.ProjectSlug, &job.Name, &job.Scope, &job.Schedule, &job.Timezone, &job.Enabled, &job.RetentionDays, &job.RetentionCount, &job.LastRunAt, &job.NextRunAt, &job.CreatedAt, &job.UpdatedAt); err != nil {
		return nil, err
	}
	return &job, nil
}

type backupRunScanner interface{ Scan(dest ...any) error }

func scanBackupRun(row backupRunScanner) (*BackupRun, error) {
	var run BackupRun
	if err := row.Scan(&run.ID, &run.JobID, &run.Status, &run.StartedAt, &run.FinishedAt, &run.StorageKey, &run.SizeBytes, &run.Error); err != nil {
		return nil, err
	}
	return &run, nil
}

type restoreJobScanner interface{ Scan(dest ...any) error }

func scanRestoreJob(row restoreJobScanner) (*RestoreJob, error) {
	var job RestoreJob
	if err := row.Scan(&job.ID, &job.AdminID, &job.Mode, &job.Source, &job.FileName, &job.Status, &job.Output, &job.Error, &job.CreatedAt, &job.FinishedAt); err != nil {
		return nil, err
	}
	return &job, nil
}
