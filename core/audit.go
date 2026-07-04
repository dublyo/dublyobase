package core

import (
	"context"
	"database/sql"
	"encoding/json"
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
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 30
	}
	if perPage > 500 {
		perPage = 500
	}
	projectSlug = NormalizeProjectSlug(projectSlug)
	args := []any{}
	where := ""
	if projectSlug != "" {
		if err := ValidateProjectSlug(projectSlug); err != nil {
			return nil, err
		}
		args = append(args, projectSlug)
		where = `where (data->>'project' = $1 or data->>'slug' = $1)`
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
