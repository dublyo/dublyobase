package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type auditExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// AuditEvent is one control-plane action. It intentionally stores generic
// metadata only; secrets and plaintext tokens must never enter audit rows.
type AuditEvent struct {
	AdminID    *string
	Action     string
	TargetType string
	TargetID   string
	IP         string
	UserAgent  string
	Data       map[string]any
}

type AuditLogEntry struct {
	ID         string         `json:"id"`
	AdminID    *string        `json:"adminId,omitempty"`
	Action     string         `json:"action"`
	TargetType string         `json:"targetType"`
	TargetID   string         `json:"targetId"`
	IP         string         `json:"ip"`
	UserAgent  string         `json:"userAgent"`
	Data       map[string]any `json:"data"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type AuditLogListResult struct {
	Items      []AuditLogEntry `json:"items"`
	Page       int             `json:"page"`
	PerPage    int             `json:"perPage"`
	TotalItems int             `json:"totalItems"`
}

type AuditLogFilter struct {
	Project string
	Action  string
	Target  string
	Search  string
	Page    int
	PerPage int
}

const auditRetentionAdvisoryLockID = int64(326_326_009)

func InsertAudit(ctx context.Context, exec auditExecer, event AuditEvent) error {
	data := []byte(`{}`)
	if event.Data != nil {
		encoded, err := json.Marshal(event.Data)
		if err != nil {
			return err
		}
		data = encoded
	}
	_, err := exec.Exec(ctx, `
		insert into _dbo.audit_log (admin_id, action, target_type, target_id, ip, user_agent, data)
		values ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
		event.AdminID,
		event.Action,
		event.TargetType,
		event.TargetID,
		event.IP,
		event.UserAgent,
		data,
	)
	return err
}

func ListAuditLog(ctx context.Context, pool *pgxpool.Pool, projectSlug string, page int, perPage int) (*AuditLogListResult, error) {
	return ListAuditLogFiltered(ctx, pool, AuditLogFilter{Project: projectSlug, Page: page, PerPage: perPage})
}

func ListAuditLogFiltered(ctx context.Context, pool *pgxpool.Pool, filter AuditLogFilter) (*AuditLogListResult, error) {
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
	projectSlug := NormalizeProjectSlug(filter.Project)
	args := []any{}
	clauses := []string{}
	if projectSlug != "" {
		if err := ValidateProjectSlug(projectSlug); err != nil {
			return nil, err
		}
		args = append(args, projectSlug)
		clauses = append(clauses, `(data->>'project' = $1 or data->>'slug' = $1)`)
	}
	action := strings.TrimSpace(filter.Action)
	if action != "" {
		if len(action) > 120 {
			return nil, fmt.Errorf("%w: audit action filter is too long", ErrValidation)
		}
		args = append(args, action)
		clauses = append(clauses, `action = $`+strconv.Itoa(len(args)))
	}
	target := strings.TrimSpace(filter.Target)
	if target != "" {
		if len(target) > 160 {
			return nil, fmt.Errorf("%w: audit target filter is too long", ErrValidation)
		}
		args = append(args, "%"+strings.ToLower(target)+"%")
		clauses = append(clauses, `(lower(target_type) like $`+strconv.Itoa(len(args))+` or lower(target_id) like $`+strconv.Itoa(len(args))+`)`)
	}
	search := strings.TrimSpace(filter.Search)
	if search != "" {
		if len(search) > 200 {
			return nil, fmt.Errorf("%w: audit search is too long", ErrValidation)
		}
		args = append(args, "%"+strings.ToLower(search)+"%")
		pos := strconv.Itoa(len(args))
		clauses = append(clauses, `(lower(action) like $`+pos+` or lower(target_type) like $`+pos+` or lower(target_id) like $`+pos+` or lower(ip) like $`+pos+` or lower(user_agent) like $`+pos+` or lower(data::text) like $`+pos+`)`)
	}
	where := ""
	if len(clauses) > 0 {
		where = "where " + strings.Join(clauses, " and ")
	}

	result := &AuditLogListResult{Items: make([]AuditLogEntry, 0), Page: page, PerPage: perPage}
	countSQL := `select count(*) from _dbo.audit_log ` + where
	if err := pool.QueryRow(ctx, countSQL, args...).Scan(&result.TotalItems); err != nil {
		return nil, err
	}

	limitPos := len(args) + 1
	offsetPos := len(args) + 2
	queryArgs := append(append([]any{}, args...), perPage, (page-1)*perPage)
	rows, err := pool.Query(ctx, `
		select id, admin_id, action, target_type, target_id, ip, user_agent, data, created_at
		from _dbo.audit_log
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
		var entry AuditLogEntry
		var adminID sql.NullString
		var rawData []byte
		if err := rows.Scan(
			&entry.ID,
			&adminID,
			&entry.Action,
			&entry.TargetType,
			&entry.TargetID,
			&entry.IP,
			&entry.UserAgent,
			&rawData,
			&entry.CreatedAt,
		); err != nil {
			return nil, err
		}
		if adminID.Valid {
			entry.AdminID = &adminID.String
		}
		if len(rawData) > 0 {
			_ = json.Unmarshal(rawData, &entry.Data)
		}
		entry.Data = redactAuditData(entry.Data)
		result.Items = append(result.Items, entry)
	}
	return result, rows.Err()
}

func ClearAuditLog(ctx context.Context, pool *pgxpool.Pool, adminID string, ip string, userAgent string) (int64, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `delete from _dbo.audit_log`)
	if err != nil {
		return 0, err
	}
	deleted := tag.RowsAffected()
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "logs.audit.clear",
		TargetType: "audit_log",
		TargetID:   "all",
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"deleted": deleted},
	}); err != nil {
		return 0, err
	}
	return deleted, tx.Commit(ctx)
}

func PruneAuditLog(ctx context.Context, pool *pgxpool.Pool, retentionDays int, retentionCount int) (int64, error) {
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
	if err := conn.QueryRow(ctx, `select pg_try_advisory_lock($1)`, auditRetentionAdvisoryLockID).Scan(&locked); err != nil {
		return 0, err
	}
	if !locked {
		return 0, nil
	}
	defer conn.Exec(context.Background(), `select pg_advisory_unlock($1)`, auditRetentionAdvisoryLockID)

	ageTag, err := conn.Exec(ctx, `
		delete from _dbo.audit_log
		where created_at < now() - ($1::text || ' days')::interval`,
		retentionDays,
	)
	if err != nil {
		return 0, err
	}
	countTag, err := conn.Exec(ctx, `
		delete from _dbo.audit_log
		where id in (
			select id
			from (
				select id, row_number() over (order by created_at desc, id desc) as rn
				from _dbo.audit_log
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

func redactAuditData(data map[string]any) map[string]any {
	if data == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(data))
	for key, value := range data {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") ||
			strings.Contains(lower, "password") ||
			strings.Contains(lower, "secret") ||
			strings.Contains(lower, "credential") ||
			strings.Contains(lower, "authorization") {
			out[key] = "[redacted]"
			continue
		}
		out[key] = value
	}
	return out
}
