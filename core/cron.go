package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cronRunnerLockID int64 = 326_326_007

type CronJob struct {
	ID             string            `json:"id"`
	ProjectID      *string           `json:"projectId,omitempty"`
	Name           string            `json:"name"`
	Type           string            `json:"type"`
	Schedule       string            `json:"schedule"`
	Timezone       string            `json:"timezone"`
	Enabled        bool              `json:"enabled"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	RetryCount     int               `json:"retryCount"`
	Method         string            `json:"method"`
	URL            string            `json:"url"`
	Headers        map[string]string `json:"headers"`
	Body           string            `json:"body"`
	LastRunAt      *time.Time        `json:"lastRunAt,omitempty"`
	NextRunAt      *time.Time        `json:"nextRunAt,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

type CronJobInput struct {
	ProjectSlug     string            `json:"projectSlug,omitempty"`
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	Schedule        string            `json:"schedule"`
	Timezone        string            `json:"timezone"`
	Enabled         bool              `json:"enabled"`
	TimeoutSeconds  int               `json:"timeoutSeconds"`
	RetryCount      int               `json:"retryCount"`
	Method          string            `json:"method"`
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers"`
	Body            string            `json:"body"`
	EnabledProvided bool              `json:"-"`
}

type CronRun struct {
	ID         string     `json:"id"`
	JobID      string     `json:"jobId"`
	Status     string     `json:"status"`
	Attempt    int        `json:"attempt"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	StatusCode *int       `json:"statusCode,omitempty"`
	Error      string     `json:"error"`
	Output     string     `json:"output"`
}

// cronAllowPrivateTargets mirrors Config.CronAllowPrivateTargets. Cron
// validation and execution are reached from places without a Config in hand, so
// the setting is latched at startup rather than threaded through every call.
var cronAllowPrivateTargets bool

// SetCronAllowPrivateTargets is called once during boot.
func SetCronAllowPrivateTargets(v bool) { cronAllowPrivateTargets = v }

func ValidateCronJobInput(input *CronJobInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return fmt.Errorf("%w: cron name is required", ErrValidation)
	}
	if input.Type == "" {
		input.Type = "http"
	}
	if input.Type != "http" {
		return fmt.Errorf("%w: unsupported cron type %q", ErrValidation, input.Type)
	}
	if input.Timezone == "" {
		input.Timezone = "UTC"
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 30
	}
	if input.TimeoutSeconds < 1 || input.TimeoutSeconds > 600 {
		return fmt.Errorf("%w: timeoutSeconds must be between 1 and 600", ErrValidation)
	}
	if input.RetryCount < 0 || input.RetryCount > 10 {
		return fmt.Errorf("%w: retryCount must be between 0 and 10", ErrValidation)
	}
	input.Method = strings.ToUpper(strings.TrimSpace(input.Method))
	if input.Method == "" {
		input.Method = http.MethodGet
	}
	switch input.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return fmt.Errorf("%w: unsupported HTTP method", ErrValidation)
	}
	parsed, err := url.ParseRequestURI(strings.TrimSpace(input.URL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: cron URL must be absolute", ErrValidation)
	}
	// Reject at save time as well as dial time, so a bad target is a validation
	// error on the form rather than a failed run discovered later.
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: cron URL must be http or https", ErrValidation)
	}
	if !cronAllowPrivateTargets {
		if err := validatePublicOutboundHost(parsed.Hostname()); err != nil {
			return err
		}
	}
	input.URL = parsed.String()
	if _, err := NextScheduledTime(input.Schedule, input.Timezone, time.Now().UTC()); err != nil {
		return err
	}
	if input.Headers == nil {
		input.Headers = map[string]string{}
	}
	for name := range input.Headers {
		if strings.ContainsAny(name, "\r\n:") || strings.TrimSpace(name) == "" {
			return fmt.Errorf("%w: invalid header name", ErrValidation)
		}
	}
	return nil
}

func CreateCronJob(ctx context.Context, pool *pgxpool.Pool, adminID string, input CronJobInput, ip string, userAgent string) (*CronJob, error) {
	if !input.EnabledProvided {
		input.Enabled = true
	}
	if err := ValidateCronJobInput(&input); err != nil {
		return nil, err
	}
	var projectID *string
	if input.ProjectSlug != "" {
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
	headers, err := json.Marshal(input.Headers)
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	job, err := scanCronJob(tx.QueryRow(ctx, `
		insert into _dbo.cron_jobs
			(project_id, name, type, schedule, timezone, enabled, timeout_seconds, retry_count, method, url, headers, body, next_run_at, created_by_admin_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14)
		returning id, project_id, name, type, schedule, timezone, enabled, timeout_seconds, retry_count, method, url, headers, body, last_run_at, next_run_at, created_at, updated_at`,
		projectID,
		input.Name,
		input.Type,
		input.Schedule,
		input.Timezone,
		input.Enabled,
		input.TimeoutSeconds,
		input.RetryCount,
		input.Method,
		input.URL,
		headers,
		input.Body,
		next,
		nullString(adminID),
	))
	if err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "cron.create",
		TargetType: "cron_job",
		TargetID:   job.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"name": job.Name, "type": job.Type, "schedule": job.Schedule},
	}); err != nil {
		return nil, err
	}
	return job, tx.Commit(ctx)
}

func ListCronJobs(ctx context.Context, pool *pgxpool.Pool) ([]CronJob, error) {
	rows, err := pool.Query(ctx, `
		select id, project_id, name, type, schedule, timezone, enabled, timeout_seconds, retry_count, method, url, headers, body, last_run_at, next_run_at, created_at, updated_at
		from _dbo.cron_jobs
		order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CronJob, 0)
	for rows.Next() {
		job, err := scanCronJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	return out, rows.Err()
}

func ListCronRuns(ctx context.Context, pool *pgxpool.Pool, jobID string, limit int) ([]CronRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := pool.Query(ctx, `
		select id, job_id, status, attempt, started_at, finished_at, status_code, error, output
		from _dbo.cron_runs
		where job_id = $1
		order by started_at desc
		limit $2`, jobID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CronRun, 0)
	for rows.Next() {
		run, err := scanCronRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	return out, rows.Err()
}

func RunCronJob(ctx context.Context, pool *pgxpool.Pool, adminID string, jobID string, ip string, userAgent string) (*CronRun, error) {
	job, err := getCronJob(ctx, pool, jobID)
	if err != nil {
		return nil, err
	}
	run, err := executeCronJob(ctx, pool, job)
	if err != nil {
		return nil, err
	}
	_ = InsertAudit(ctx, pool, AuditEvent{
		AdminID:    &adminID,
		Action:     "cron.run",
		TargetType: "cron_job",
		TargetID:   job.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"name": job.Name, "status": run.Status},
	})
	return run, nil
}

func RunDueCronJobs(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err := tx.QueryRow(ctx, `select pg_try_advisory_xact_lock($1)`, cronRunnerLockID).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	rows, err := tx.Query(ctx, `
		select id, project_id, name, type, schedule, timezone, enabled, timeout_seconds, retry_count, method, url, headers, body, last_run_at, next_run_at, created_at, updated_at
		from _dbo.cron_jobs
		where enabled and next_run_at is not null and next_run_at <= $1
		order by next_run_at asc
		limit 5
		for update skip locked`, now.UTC())
	if err != nil {
		return err
	}
	var jobs []CronJob
	for rows.Next() {
		job, err := scanCronJob(rows)
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
		if _, err := executeCronJob(ctx, pool, &jobs[i]); err != nil && ctx.Err() != nil {
			return err
		}
	}
	return nil
}

func getCronJob(ctx context.Context, pool *pgxpool.Pool, id string) (*CronJob, error) {
	job, err := scanCronJob(pool.QueryRow(ctx, `
		select id, project_id, name, type, schedule, timezone, enabled, timeout_seconds, retry_count, method, url, headers, body, last_run_at, next_run_at, created_at, updated_at
		from _dbo.cron_jobs
		where id = $1`, id))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrValidation
		}
		return nil, err
	}
	return job, nil
}

func executeCronJob(ctx context.Context, pool *pgxpool.Pool, job *CronJob) (*CronRun, error) {
	attempts := job.RetryCount + 1
	var final *CronRun
	for attempt := 1; attempt <= attempts; attempt++ {
		run, err := createCronRun(ctx, pool, job.ID, attempt)
		if err != nil {
			return nil, err
		}
		status, statusCode, output, runErr := executeHTTPJob(ctx, job)
		if runErr != nil {
			status = "error"
		}
		if err := finishCronRun(ctx, pool, run.ID, status, statusCode, output, runErr); err != nil {
			return nil, err
		}
		run.Status = status
		run.StatusCode = statusCode
		run.Output = output
		if runErr != nil {
			run.Error = runErr.Error()
		}
		now := time.Now().UTC()
		next, nextErr := NextScheduledTime(job.Schedule, job.Timezone, now)
		if nextErr == nil {
			_, _ = pool.Exec(ctx, `update _dbo.cron_jobs set last_run_at = $1, next_run_at = $2, updated_at = now() where id = $3`, now, next, job.ID)
		}
		final = run
		if runErr == nil {
			break
		}
	}
	return final, nil
}

func executeHTTPJob(ctx context.Context, job *CronJob) (string, *int, string, error) {
	timeout := time.Duration(job.TimeoutSeconds) * time.Second
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if job.Body != "" && job.Method != http.MethodGet {
		body = strings.NewReader(job.Body)
	}
	req, err := http.NewRequestWithContext(reqCtx, job.Method, job.URL, body)
	if err != nil {
		return "error", nil, "", err
	}
	for key, value := range job.Headers {
		req.Header.Set(key, value)
	}
	// Cron was the last outbound path calling out with the default dialer, so a
	// job URL could reach the cloud metadata endpoint or a service bound to
	// localhost and return up to 4KB of it in the run output. Webhooks, OAuth,
	// SMTP and S3 all already routed through this guard.
	client := &http.Client{Timeout: timeout}
	if !cronAllowPrivateTargets {
		client.Transport = &http.Transport{DialContext: publicTCPDialer(timeout)}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "error", nil, "", err
	}
	defer resp.Body.Close()
	limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	statusCode := resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "error", &statusCode, string(limited), fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return "success", &statusCode, string(limited), nil
}

func createCronRun(ctx context.Context, pool *pgxpool.Pool, jobID string, attempt int) (*CronRun, error) {
	return scanCronRun(pool.QueryRow(ctx, `
		insert into _dbo.cron_runs (job_id, status, attempt)
		values ($1, 'running', $2)
		returning id, job_id, status, attempt, started_at, finished_at, status_code, error, output`, jobID, attempt))
}

func finishCronRun(ctx context.Context, pool *pgxpool.Pool, runID string, status string, statusCode *int, output string, runErr error) error {
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}
	if len(output) > 4096 {
		output = output[:4096]
	}
	_, err := pool.Exec(ctx, `
		update _dbo.cron_runs
		set status = $1, status_code = $2, output = $3, error = $4, finished_at = now()
		where id = $5`, status, statusCode, output, errText, runID)
	return err
}

type cronJobScanner interface{ Scan(dest ...any) error }

func scanCronJob(row cronJobScanner) (*CronJob, error) {
	var job CronJob
	var headers []byte
	if err := row.Scan(&job.ID, &job.ProjectID, &job.Name, &job.Type, &job.Schedule, &job.Timezone, &job.Enabled, &job.TimeoutSeconds, &job.RetryCount, &job.Method, &job.URL, &headers, &job.Body, &job.LastRunAt, &job.NextRunAt, &job.CreatedAt, &job.UpdatedAt); err != nil {
		return nil, err
	}
	if len(headers) > 0 {
		_ = json.Unmarshal(headers, &job.Headers)
	}
	if job.Headers == nil {
		job.Headers = map[string]string{}
	}
	return &job, nil
}

type cronRunScanner interface{ Scan(dest ...any) error }

func scanCronRun(row cronRunScanner) (*CronRun, error) {
	var run CronRun
	if err := row.Scan(&run.ID, &run.JobID, &run.Status, &run.Attempt, &run.StartedAt, &run.FinishedAt, &run.StatusCode, &run.Error, &run.Output); err != nil {
		return nil, err
	}
	return &run, nil
}

func headersFromRaw(raw json.RawMessage) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}
	var headers map[string]string
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&headers); err != nil {
		return nil, fmt.Errorf("%w: headers must be an object of strings", ErrValidation)
	}
	return headers, nil
}

func nullString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
