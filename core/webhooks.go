package core

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const webhookRunnerLockID int64 = 326_326_011

type Webhook struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"projectId"`
	ProjectSlug    string    `json:"projectSlug"`
	Name           string    `json:"name"`
	URL            string    `json:"url"`
	Events         []string  `json:"events"`
	Enabled        bool      `json:"enabled"`
	SecretSet      bool      `json:"secretSet"`
	Secret         string    `json:"secret,omitempty"`
	TimeoutSeconds int       `json:"timeoutSeconds"`
	MaxAttempts    int       `json:"maxAttempts"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type WebhookInput struct {
	Name           string   `json:"name"`
	URL            string   `json:"url"`
	Events         []string `json:"events"`
	Enabled        *bool    `json:"enabled,omitempty"`
	Secret         *string  `json:"secret,omitempty"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	MaxAttempts    int      `json:"maxAttempts"`
}

type WebhookDelivery struct {
	ID             string         `json:"id"`
	WebhookID      string         `json:"webhookId"`
	ProjectID      string         `json:"projectId"`
	Event          string         `json:"event"`
	Status         string         `json:"status"`
	Attempts       int            `json:"attempts"`
	NextAttemptAt  time.Time      `json:"nextAttemptAt"`
	LastStatusCode *int           `json:"lastStatusCode,omitempty"`
	Error          string         `json:"error"`
	RequestBody    map[string]any `json:"requestBody"`
	ResponseBody   string         `json:"responseBody"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type webhookSendJob struct {
	Delivery WebhookDelivery
	Webhook  Webhook
	Secret   string
}

func CreateWebhook(ctx context.Context, pool *pgxpool.Pool, cfg *Config, adminID string, projectSlug string, input WebhookInput, ip string, userAgent string) (*Webhook, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	if err := validateWebhookInput(&input); err != nil {
		return nil, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	secret := ""
	if input.Secret != nil {
		secret = strings.TrimSpace(*input.Secret)
	}
	if secret == "" {
		secret, err = generateWebhookSecret()
		if err != nil {
			return nil, err
		}
	}
	secretCipher, err := encryptSecret(cfg.JWTSecret, secret)
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	hook, err := scanWebhook(tx.QueryRow(ctx, `
		insert into _dbo.webhooks
			(project_id, name, url, events, enabled, secret_cipher, timeout_seconds, max_attempts, created_by_admin_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		returning id, project_id, $10::text, name, url, events, enabled, secret_cipher <> '', timeout_seconds, max_attempts, created_at, updated_at`,
		project.ID,
		input.Name,
		input.URL,
		input.Events,
		enabled,
		secretCipher,
		input.TimeoutSeconds,
		input.MaxAttempts,
		nullString(adminID),
		project.Slug,
	))
	if err != nil {
		if pgErrCode(err) == "23505" {
			return nil, fmt.Errorf("%w: webhook name already exists", ErrValidation)
		}
		return nil, err
	}
	hook.Secret = secret
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "webhook.create",
		TargetType: "webhook",
		TargetID:   hook.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "name": hook.Name, "eventCount": len(hook.Events)},
	}); err != nil {
		return nil, err
	}
	return hook, tx.Commit(ctx)
}

func ListWebhooks(ctx context.Context, pool *pgxpool.Pool, projectSlug string) ([]Webhook, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `
		select id, project_id, $2::text, name, url, events, enabled, secret_cipher <> '', timeout_seconds, max_attempts, created_at, updated_at
		from _dbo.webhooks
		where project_id = $1
		order by created_at desc`,
		project.ID,
		project.Slug,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Webhook, 0)
	for rows.Next() {
		hook, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *hook)
	}
	return out, rows.Err()
}

func DeleteWebhook(ctx context.Context, pool *pgxpool.Pool, adminID string, projectSlug string, id string, ip string, userAgent string) error {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `delete from _dbo.webhooks where project_id = $1 and id = $2`, project.ID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrValidation
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "webhook.delete",
		TargetType: "webhook",
		TargetID:   id,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ListWebhookDeliveries(ctx context.Context, pool *pgxpool.Pool, projectSlug string, webhookID string, limit int) ([]WebhookDelivery, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `
		select d.id, d.webhook_id, d.project_id, d.event, d.status, d.attempts, d.next_attempt_at,
		       d.last_status_code, d.error, d.request_body, d.response_body, d.created_at, d.updated_at
		from _dbo.webhook_deliveries d
		join _dbo.webhooks h on h.id = d.webhook_id
		where h.project_id = $1 and h.id = $2
		order by d.created_at desc
		limit $3`,
		project.ID,
		webhookID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]WebhookDelivery, 0)
	for rows.Next() {
		delivery, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *delivery)
	}
	return out, rows.Err()
}

func EnqueueRecordWebhookDeliveries(ctx context.Context, pool *pgxpool.Pool, projectSlug string, collection string, action string, recordID string, record Record) error {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return err
	}
	event := "records." + action
	payload := map[string]any{
		"event":      event,
		"project":    project.Slug,
		"collection": collection,
		"action":     action,
		"recordId":   recordID,
		"record":     record,
		"createdAt":  time.Now().UTC().Format(time.RFC3339Nano),
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	rows, err := pool.Query(ctx, `
		select id, events
		from _dbo.webhooks
		where project_id = $1 and enabled`,
		project.ID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	type target struct {
		id     string
		events []string
	}
	targets := []target{}
	for rows.Next() {
		var item target
		if err := rows.Scan(&item.id, &item.events); err != nil {
			return err
		}
		if webhookMatchesEvent(item.events, event, collection, action) {
			targets = append(targets, item)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, target := range targets {
		if _, err := pool.Exec(ctx, `
			insert into _dbo.webhook_deliveries (webhook_id, project_id, event, request_body)
			values ($1, $2, $3, $4::jsonb)`,
			target.id,
			project.ID,
			event,
			rawPayload,
		); err != nil {
			return err
		}
	}
	return nil
}

func RunDueWebhookDeliveries(ctx context.Context, pool *pgxpool.Pool, cfg *Config, now time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err := tx.QueryRow(ctx, `select pg_try_advisory_xact_lock($1)`, webhookRunnerLockID).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	rows, err := tx.Query(ctx, `
		select d.id, d.webhook_id, d.project_id, d.event, d.status, d.attempts, d.next_attempt_at,
		       d.last_status_code, d.error, d.request_body, d.response_body, d.created_at, d.updated_at,
		       h.id, h.project_id, p.slug, h.name, h.url, h.events, h.enabled, h.secret_cipher <> '', h.timeout_seconds, h.max_attempts, h.created_at, h.updated_at,
		       h.secret_cipher
		from _dbo.webhook_deliveries d
		join _dbo.webhooks h on h.id = d.webhook_id
		join _dbo.projects p on p.id = d.project_id
		where h.enabled
			and d.status in ('pending', 'error')
			and d.next_attempt_at <= $1
			and d.attempts < h.max_attempts
		order by d.next_attempt_at asc
		limit 10
		for update of d skip locked`,
		now.UTC(),
	)
	if err != nil {
		return err
	}
	jobs := []webhookSendJob{}
	for rows.Next() {
		var job webhookSendJob
		var secretCipher string
		if err := scanWebhookSendJob(rows, &job, &secretCipher); err != nil {
			rows.Close()
			return err
		}
		if secretCipher != "" {
			secret, err := decryptSecret(cfg.JWTSecret, secretCipher)
			if err != nil {
				rows.Close()
				return err
			}
			job.Secret = secret
		}
		jobs = append(jobs, job)
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
		if err := sendWebhookDelivery(ctx, pool, &jobs[i]); err != nil && ctx.Err() != nil {
			return err
		}
	}
	return nil
}

func validateWebhookInput(input *WebhookInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return fmt.Errorf("%w: webhook name is required", ErrValidation)
	}
	if len(input.Name) > 120 {
		return fmt.Errorf("%w: webhook name is too long", ErrValidation)
	}
	parsed, err := url.ParseRequestURI(strings.TrimSpace(input.URL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: webhook URL must be absolute", ErrValidation)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%w: webhook URL must use http or https", ErrValidation)
	}
	if err := validatePublicOutboundHost(parsed.Hostname()); err != nil {
		return err
	}
	input.URL = parsed.String()
	if len(input.Events) == 0 {
		input.Events = []string{"records.*"}
	}
	if len(input.Events) > 25 {
		return fmt.Errorf("%w: webhook supports at most 25 events", ErrValidation)
	}
	for i := range input.Events {
		event := strings.ToLower(strings.TrimSpace(input.Events[i]))
		if !validWebhookEvent(event) {
			return fmt.Errorf("%w: invalid webhook event %q", ErrValidation, input.Events[i])
		}
		input.Events[i] = event
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 10
	}
	if input.TimeoutSeconds < 1 || input.TimeoutSeconds > 60 {
		return fmt.Errorf("%w: timeoutSeconds must be between 1 and 60", ErrValidation)
	}
	if input.MaxAttempts == 0 {
		input.MaxAttempts = 5
	}
	if input.MaxAttempts < 1 || input.MaxAttempts > 20 {
		return fmt.Errorf("%w: maxAttempts must be between 1 and 20", ErrValidation)
	}
	return nil
}

func validWebhookEvent(event string) bool {
	if event == "*" || event == "records.*" {
		return true
	}
	if event == "records.create" || event == "records.update" || event == "records.delete" {
		return true
	}
	parts := strings.Split(event, ".")
	return len(parts) == 2 && ValidateDataIdentifier("webhook collection event", parts[0]) == nil &&
		(parts[1] == "create" || parts[1] == "update" || parts[1] == "delete")
}

func webhookMatchesEvent(events []string, event string, collection string, action string) bool {
	collectionEvent := strings.ToLower(collection + "." + action)
	for _, candidate := range events {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "*" || candidate == event || candidate == "records.*" || candidate == collectionEvent {
			return true
		}
	}
	return false
}

func generateWebhookSecret() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "dbo_whsec_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func sendWebhookDelivery(ctx context.Context, pool *pgxpool.Pool, job *webhookSendJob) error {
	body, err := json.Marshal(job.Delivery.RequestBody)
	if err != nil {
		return finishWebhookDelivery(ctx, pool, job, nil, "", err)
	}
	timeout := time.Duration(job.Webhook.TimeoutSeconds) * time.Second
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, job.Webhook.URL, bytes.NewReader(body))
	if err != nil {
		return finishWebhookDelivery(ctx, pool, job, nil, "", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "dublyobase-webhooks/1")
	req.Header.Set("X-Dublyobase-Event", job.Delivery.Event)
	req.Header.Set("X-Dublyobase-Delivery", job.Delivery.ID)
	if job.Secret != "" {
		req.Header.Set("X-Dublyobase-Signature", webhookSignature(job.Secret, body))
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:       http.ProxyFromEnvironment,
			DialContext: publicTCPDialer(timeout),
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return finishWebhookDelivery(ctx, pool, job, nil, "", err)
	}
	defer resp.Body.Close()
	limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	statusCode := resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return finishWebhookDelivery(ctx, pool, job, &statusCode, string(limited), fmt.Errorf("HTTP %d", resp.StatusCode))
	}
	return finishWebhookDelivery(ctx, pool, job, &statusCode, string(limited), nil)
}

func finishWebhookDelivery(ctx context.Context, pool *pgxpool.Pool, job *webhookSendJob, statusCode *int, response string, runErr error) error {
	attempts := job.Delivery.Attempts + 1
	status := "success"
	errText := ""
	nextAttempt := time.Now().UTC()
	if runErr != nil {
		status = "error"
		errText = runErr.Error()
		delay := time.Duration(1<<minInt(attempts, 8)) * time.Minute
		nextAttempt = nextAttempt.Add(delay)
	}
	if len(response) > 4096 {
		response = response[:4096]
	}
	_, err := pool.Exec(ctx, `
		update _dbo.webhook_deliveries
		set status = $1,
			attempts = $2,
			next_attempt_at = $3,
			last_status_code = $4,
			error = $5,
			response_body = $6,
			updated_at = now()
		where id = $7`,
		status,
		attempts,
		nextAttempt,
		statusCode,
		errText,
		response,
		job.Delivery.ID,
	)
	return err
}

func webhookSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

type webhookScanner interface{ Scan(dest ...any) error }

func scanWebhook(row webhookScanner) (*Webhook, error) {
	var hook Webhook
	if err := row.Scan(&hook.ID, &hook.ProjectID, &hook.ProjectSlug, &hook.Name, &hook.URL, &hook.Events, &hook.Enabled, &hook.SecretSet, &hook.TimeoutSeconds, &hook.MaxAttempts, &hook.CreatedAt, &hook.UpdatedAt); err != nil {
		return nil, err
	}
	return &hook, nil
}

func scanWebhookDelivery(row webhookScanner) (*WebhookDelivery, error) {
	var delivery WebhookDelivery
	var rawBody []byte
	if err := row.Scan(&delivery.ID, &delivery.WebhookID, &delivery.ProjectID, &delivery.Event, &delivery.Status, &delivery.Attempts, &delivery.NextAttemptAt, &delivery.LastStatusCode, &delivery.Error, &rawBody, &delivery.ResponseBody, &delivery.CreatedAt, &delivery.UpdatedAt); err != nil {
		return nil, err
	}
	if len(rawBody) > 0 {
		_ = json.Unmarshal(rawBody, &delivery.RequestBody)
	}
	if delivery.RequestBody == nil {
		delivery.RequestBody = map[string]any{}
	}
	return &delivery, nil
}

func scanWebhookSendJob(row webhookScanner, job *webhookSendJob, secretCipher *string) error {
	var deliveryBody []byte
	err := row.Scan(
		&job.Delivery.ID,
		&job.Delivery.WebhookID,
		&job.Delivery.ProjectID,
		&job.Delivery.Event,
		&job.Delivery.Status,
		&job.Delivery.Attempts,
		&job.Delivery.NextAttemptAt,
		&job.Delivery.LastStatusCode,
		&job.Delivery.Error,
		&deliveryBody,
		&job.Delivery.ResponseBody,
		&job.Delivery.CreatedAt,
		&job.Delivery.UpdatedAt,
		&job.Webhook.ID,
		&job.Webhook.ProjectID,
		&job.Webhook.ProjectSlug,
		&job.Webhook.Name,
		&job.Webhook.URL,
		&job.Webhook.Events,
		&job.Webhook.Enabled,
		&job.Webhook.SecretSet,
		&job.Webhook.TimeoutSeconds,
		&job.Webhook.MaxAttempts,
		&job.Webhook.CreatedAt,
		&job.Webhook.UpdatedAt,
		secretCipher,
	)
	if err != nil {
		return err
	}
	if len(deliveryBody) > 0 {
		_ = json.Unmarshal(deliveryBody, &job.Delivery.RequestBody)
	}
	if job.Delivery.RequestBody == nil {
		job.Delivery.RequestBody = map[string]any{}
	}
	return nil
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
