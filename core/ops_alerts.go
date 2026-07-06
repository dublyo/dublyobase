package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type OpsAlert struct {
	ID         string         `json:"id"`
	ProjectID  *string        `json:"projectId,omitempty"`
	Project    string         `json:"project,omitempty"`
	Severity   string         `json:"severity"`
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	Metadata   map[string]any `json:"metadata"`
	ResolvedAt *time.Time     `json:"resolvedAt,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

func ListOpsAlerts(ctx context.Context, pool *pgxpool.Pool, projectSlug string, limit int) ([]OpsAlert, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 250 {
		limit = 50
	}
	rows, err := pool.Query(ctx, `
		select id, project_id, severity, code, message, metadata, resolved_at, created_at
		from _dbo.ops_alerts
		where project_id = $1
		order by created_at desc
		limit $2`,
		project.ID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []OpsAlert{}
	for rows.Next() {
		var item OpsAlert
		var raw []byte
		var projectID sql.NullString
		var resolvedAt sql.NullTime
		if err := rows.Scan(&item.ID, &projectID, &item.Severity, &item.Code, &item.Message, &raw, &resolvedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		if projectID.Valid {
			item.ProjectID = &projectID.String
		}
		if resolvedAt.Valid {
			item.ResolvedAt = &resolvedAt.Time
		}
		item.Project = project.Slug
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &item.Metadata)
		}
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func RefreshProjectOpsAlerts(ctx context.Context, pool *pgxpool.Pool, projectSlug string, now time.Time) ([]OpsAlert, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	metrics, err := GetProjectMetrics(ctx, pool, project.Slug, 24, now)
	if err != nil {
		return nil, err
	}
	if metrics.Quota.Enabled {
		if metrics.Quota.MaxAppUsers > 0 && metrics.AppUsers*100 >= metrics.Quota.MaxAppUsers*80 {
			_ = insertOpsAlert(ctx, pool, project.ID, "warning", "quota.app_users_near_limit", "App users are near the configured quota.", map[string]any{
				"appUsers": metrics.AppUsers,
				"limit":    metrics.Quota.MaxAppUsers,
			})
		}
		if metrics.Quota.MaxStorageMB > 0 {
			limitBytes := int64(metrics.Quota.MaxStorageMB) * 1024 * 1024
			if limitBytes > 0 && metrics.StorageBytes*100 >= limitBytes*80 {
				_ = insertOpsAlert(ctx, pool, project.ID, "warning", "quota.storage_near_limit", "Storage usage is near the configured quota.", map[string]any{
					"storageBytes": metrics.StorageBytes,
					"limitBytes":   limitBytes,
				})
			}
		}
	}
	if metrics.Requests.Total >= 20 && metrics.Requests.Errors*100 >= metrics.Requests.Total*5 {
		_ = insertOpsAlert(ctx, pool, project.ID, "warning", "requests.error_rate", "Request error rate is above five percent in the selected window.", map[string]any{
			"total":  metrics.Requests.Total,
			"errors": metrics.Requests.Errors,
		})
	}
	if metrics.Requests.P95Duration > 2000 {
		_ = insertOpsAlert(ctx, pool, project.ID, "warning", "requests.high_latency", "P95 request latency is above 2000 ms.", map[string]any{
			"p95DurationMs": metrics.Requests.P95Duration,
		})
	}
	return ListOpsAlerts(ctx, pool, project.Slug, 50)
}

func insertOpsAlert(ctx context.Context, pool *pgxpool.Pool, projectID string, severity string, code string, message string, metadata map[string]any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx, `
		insert into _dbo.ops_alerts (project_id, severity, code, message, metadata)
		select $1, $2, $3, $4, $5::jsonb
		where not exists (
			select 1
			from _dbo.ops_alerts
			where project_id = $1 and code = $3 and resolved_at is null and created_at > now() - interval '1 hour'
		)`,
		projectID,
		severity,
		code,
		message,
		raw,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return nil
}

func ResolveOpsAlert(ctx context.Context, pool *pgxpool.Pool, projectSlug string, id string, now time.Time) error {
	if err := ValidateUUID(id); err != nil {
		return err
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return err
	}
	tag, err := pool.Exec(ctx, `
		update _dbo.ops_alerts
		set resolved_at = coalesce(resolved_at, $1)
		where project_id = $2 and id = $3`,
		now.UTC(),
		project.ID,
		id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRecordNotFound
	}
	return nil
}

func FormatOpsAlertCount(count int) string {
	return fmt.Sprintf("%d", count)
}
