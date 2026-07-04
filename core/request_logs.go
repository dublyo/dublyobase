package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const requestLogRetentionAdvisoryLockID = int64(326_326_010)

type RequestLogEntry struct {
	ID          string         `json:"id"`
	ProjectID   *string        `json:"projectId,omitempty"`
	ProjectSlug string         `json:"projectSlug"`
	Method      string         `json:"method"`
	Path        string         `json:"path"`
	Status      int            `json:"status"`
	DurationMS  int            `json:"durationMs"`
	IP          string         `json:"ip"`
	UserAgent   string         `json:"userAgent"`
	RequestID   string         `json:"requestId"`
	Error       string         `json:"error"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"createdAt"`
}

type RequestLogEvent struct {
	ProjectSlug string
	Method      string
	Path        string
	Status      int
	DurationMS  int
	IP          string
	UserAgent   string
	RequestID   string
	Error       string
	Metadata    map[string]any
}

type RequestLogListResult struct {
	Items      []RequestLogEntry `json:"items"`
	Page       int               `json:"page"`
	PerPage    int               `json:"perPage"`
	TotalItems int               `json:"totalItems"`
}

type RequestLogFilter struct {
	Project string
	Method  string
	Status  int
	Search  string
	Page    int
	PerPage int
}

func InsertRequestLog(ctx context.Context, pool *pgxpool.Pool, event RequestLogEvent) error {
	if pool == nil {
		return nil
	}
	event.Method = strings.ToUpper(strings.TrimSpace(event.Method))
	event.Path = strings.TrimSpace(event.Path)
	if event.Method == "" || event.Path == "" {
		return nil
	}
	if len(event.Path) > 1000 {
		event.Path = event.Path[:1000]
	}
	event.ProjectSlug = NormalizeProjectSlug(event.ProjectSlug)
	metadata := []byte(`{}`)
	if event.Metadata != nil {
		encoded, err := json.Marshal(redactAuditData(event.Metadata))
		if err != nil {
			return err
		}
		metadata = encoded
	}
	_, err := pool.Exec(ctx, `
		insert into _dbo.request_logs
			(project_id, project_slug, method, path, status, duration_ms, ip, user_agent, request_id, error, metadata)
		values (
			(select id from _dbo.projects where slug = nullif($1, '')),
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb
		)`,
		event.ProjectSlug,
		event.Method,
		event.Path,
		event.Status,
		event.DurationMS,
		truncateString(event.IP, 200),
		truncateString(event.UserAgent, 500),
		truncateString(event.RequestID, 200),
		truncateString(event.Error, 500),
		metadata,
	)
	return err
}

func ListRequestLogs(ctx context.Context, pool *pgxpool.Pool, filter RequestLogFilter) (*RequestLogListResult, error) {
	page := filter.Page
	perPage := filter.PerPage
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	if perPage > 500 {
		perPage = 500
	}
	args := []any{}
	clauses := []string{}
	projectSlug := NormalizeProjectSlug(filter.Project)
	if projectSlug != "" {
		if err := ValidateProjectSlug(projectSlug); err != nil {
			return nil, err
		}
		args = append(args, projectSlug)
		clauses = append(clauses, `project_slug = $`+strconv.Itoa(len(args)))
	}
	method := strings.ToUpper(strings.TrimSpace(filter.Method))
	if method != "" {
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		default:
			return nil, fmt.Errorf("%w: unsupported request log method", ErrValidation)
		}
		args = append(args, method)
		clauses = append(clauses, `method = $`+strconv.Itoa(len(args)))
	}
	if filter.Status > 0 {
		if filter.Status < 100 || filter.Status > 599 {
			return nil, fmt.Errorf("%w: status must be between 100 and 599", ErrValidation)
		}
		args = append(args, filter.Status)
		clauses = append(clauses, `status = $`+strconv.Itoa(len(args)))
	}
	search := strings.TrimSpace(filter.Search)
	if search != "" {
		if len(search) > 200 {
			return nil, fmt.Errorf("%w: request log search is too long", ErrValidation)
		}
		args = append(args, "%"+strings.ToLower(search)+"%")
		pos := strconv.Itoa(len(args))
		clauses = append(clauses, `(lower(path) like $`+pos+` or lower(ip) like $`+pos+` or lower(user_agent) like $`+pos+` or lower(error) like $`+pos+` or lower(metadata::text) like $`+pos+`)`)
	}
	where := ""
	if len(clauses) > 0 {
		where = "where " + strings.Join(clauses, " and ")
	}

	result := &RequestLogListResult{Items: make([]RequestLogEntry, 0), Page: page, PerPage: perPage}
	if err := pool.QueryRow(ctx, `select count(*) from _dbo.request_logs `+where, args...).Scan(&result.TotalItems); err != nil {
		return nil, err
	}
	limitPos := len(args) + 1
	offsetPos := len(args) + 2
	queryArgs := append(append([]any{}, args...), perPage, (page-1)*perPage)
	rows, err := pool.Query(ctx, `
		select id, project_id, project_slug, method, path, status, duration_ms, ip, user_agent, request_id, error, metadata, created_at
		from _dbo.request_logs
		`+where+`
		order by created_at desc
		limit $`+strconv.Itoa(limitPos)+` offset $`+strconv.Itoa(offsetPos),
		queryArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry RequestLogEntry
		var rawMetadata []byte
		if err := rows.Scan(&entry.ID, &entry.ProjectID, &entry.ProjectSlug, &entry.Method, &entry.Path, &entry.Status, &entry.DurationMS, &entry.IP, &entry.UserAgent, &entry.RequestID, &entry.Error, &rawMetadata, &entry.CreatedAt); err != nil {
			return nil, err
		}
		if len(rawMetadata) > 0 {
			_ = json.Unmarshal(rawMetadata, &entry.Metadata)
		}
		entry.Metadata = redactAuditData(entry.Metadata)
		result.Items = append(result.Items, entry)
	}
	return result, rows.Err()
}

func PruneRequestLogs(ctx context.Context, pool *pgxpool.Pool, retentionDays int, retentionCount int) (int64, error) {
	if retentionDays < 1 {
		retentionDays = defaultLogRetentionDays
	}
	if retentionCount < 100 {
		retentionCount = defaultLogRetentionCount
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `select pg_try_advisory_lock($1)`, requestLogRetentionAdvisoryLockID).Scan(&locked); err != nil {
		return 0, err
	}
	if !locked {
		return 0, nil
	}
	defer conn.Exec(context.Background(), `select pg_advisory_unlock($1)`, requestLogRetentionAdvisoryLockID)
	ageTag, err := conn.Exec(ctx, `
		delete from _dbo.request_logs
		where created_at < now() - ($1::text || ' days')::interval`,
		retentionDays,
	)
	if err != nil {
		return 0, err
	}
	countTag, err := conn.Exec(ctx, `
		delete from _dbo.request_logs
		where id in (
			select id from (
				select id, row_number() over (order by created_at desc, id desc) as rn
				from _dbo.request_logs
			) ranked
			where rn > $1
		)`,
		retentionCount,
	)
	if err != nil {
		return ageTag.RowsAffected(), err
	}
	return ageTag.RowsAffected() + countTag.RowsAffected(), nil
}

func truncateString(v string, max int) string {
	v = strings.TrimSpace(v)
	if len(v) <= max {
		return v
	}
	return v[:max]
}
