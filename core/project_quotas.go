package core

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProjectQuotas struct {
	ProjectID             string    `json:"projectId"`
	ProjectSlug           string    `json:"projectSlug"`
	Enabled               bool      `json:"enabled"`
	RequestsPerMinute     int       `json:"requestsPerMinute"`
	AuthRequestsPerMinute int       `json:"authRequestsPerMinute"`
	MaxAppUsers           int       `json:"maxAppUsers"`
	MaxStorageMB          int       `json:"maxStorageMb"`
	CreatedAt             time.Time `json:"createdAt,omitempty"`
	UpdatedAt             time.Time `json:"updatedAt,omitempty"`
}

type ProjectQuotasInput struct {
	Enabled               *bool `json:"enabled,omitempty"`
	RequestsPerMinute     int   `json:"requestsPerMinute"`
	AuthRequestsPerMinute int   `json:"authRequestsPerMinute"`
	MaxAppUsers           int   `json:"maxAppUsers"`
	MaxStorageMB          int   `json:"maxStorageMb"`
}

type ProjectMetrics struct {
	ProjectID        string        `json:"projectId"`
	ProjectSlug      string        `json:"projectSlug"`
	WindowHours      int           `json:"windowHours"`
	AppUsers         int           `json:"appUsers"`
	ActiveSessions   int           `json:"activeSessions"`
	Organizations    int           `json:"organizations"`
	StorageBytes     int64         `json:"storageBytes"`
	Requests         RequestMetric `json:"requests"`
	Quota            ProjectQuotas `json:"quota"`
	WindowStartedAt  time.Time     `json:"windowStartedAt"`
	WindowFinishedAt time.Time     `json:"windowFinishedAt"`
}

type RequestMetric struct {
	Total       int     `json:"total"`
	Errors      int     `json:"errors"`
	AvgDuration float64 `json:"avgDurationMs"`
	P95Duration float64 `json:"p95DurationMs"`
}

func DefaultProjectQuotas(project *Project) *ProjectQuotas {
	quotas := &ProjectQuotas{}
	if project != nil {
		quotas.ProjectID = project.ID
		quotas.ProjectSlug = project.Slug
	}
	return quotas
}

func GetProjectQuotas(ctx context.Context, pool *pgxpool.Pool, projectSlug string) (*ProjectQuotas, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	return getProjectQuotasByProject(ctx, pool, project)
}

func UpdateProjectQuotas(ctx context.Context, pool *pgxpool.Pool, adminID string, projectSlug string, input ProjectQuotasInput, ip string, userAgent string) (*ProjectQuotas, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	current, err := getProjectQuotasByProject(ctx, pool, project)
	if err != nil {
		return nil, err
	}
	next := *current
	if input.Enabled != nil {
		next.Enabled = *input.Enabled
	}
	next.RequestsPerMinute = input.RequestsPerMinute
	next.AuthRequestsPerMinute = input.AuthRequestsPerMinute
	next.MaxAppUsers = input.MaxAppUsers
	next.MaxStorageMB = input.MaxStorageMB
	if err := validateProjectQuotas(&next); err != nil {
		return nil, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx, `
		insert into _dbo.project_quotas
			(project_id, enabled, requests_per_minute, auth_requests_per_minute, max_app_users, max_storage_mb)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (project_id) do update
		set enabled = excluded.enabled,
			requests_per_minute = excluded.requests_per_minute,
			auth_requests_per_minute = excluded.auth_requests_per_minute,
			max_app_users = excluded.max_app_users,
			max_storage_mb = excluded.max_storage_mb,
			updated_at = now()
		returning created_at, updated_at`,
		project.ID,
		next.Enabled,
		next.RequestsPerMinute,
		next.AuthRequestsPerMinute,
		next.MaxAppUsers,
		next.MaxStorageMB,
	).Scan(&next.CreatedAt, &next.UpdatedAt); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "settings.quotas.update",
		TargetType: "project",
		TargetID:   project.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data: map[string]any{
			"project":               project.Slug,
			"enabled":               next.Enabled,
			"requestsPerMinute":     next.RequestsPerMinute,
			"authRequestsPerMinute": next.AuthRequestsPerMinute,
			"maxAppUsers":           next.MaxAppUsers,
			"maxStorageMb":          next.MaxStorageMB,
		},
	}); err != nil {
		return nil, err
	}
	return &next, tx.Commit(ctx)
}

func getProjectQuotasByProject(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, project *Project) (*ProjectQuotas, error) {
	quotas := DefaultProjectQuotas(project)
	err := q.QueryRow(ctx, `
		select enabled, requests_per_minute, auth_requests_per_minute, max_app_users, max_storage_mb, created_at, updated_at
		from _dbo.project_quotas
		where project_id = $1`,
		project.ID,
	).Scan(
		&quotas.Enabled,
		&quotas.RequestsPerMinute,
		&quotas.AuthRequestsPerMinute,
		&quotas.MaxAppUsers,
		&quotas.MaxStorageMB,
		&quotas.CreatedAt,
		&quotas.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return quotas, nil
		}
		return nil, err
	}
	if err := validateProjectQuotas(quotas); err != nil {
		return DefaultProjectQuotas(project), nil
	}
	return quotas, nil
}

func validateProjectQuotas(quotas *ProjectQuotas) error {
	if quotas.RequestsPerMinute < 0 || quotas.RequestsPerMinute > 1_000_000 {
		return fmt.Errorf("%w: requestsPerMinute must be between 0 and 1000000", ErrValidation)
	}
	if quotas.AuthRequestsPerMinute < 0 || quotas.AuthRequestsPerMinute > 1_000_000 {
		return fmt.Errorf("%w: authRequestsPerMinute must be between 0 and 1000000", ErrValidation)
	}
	if quotas.MaxAppUsers < 0 || quotas.MaxAppUsers > 100_000_000 {
		return fmt.Errorf("%w: maxAppUsers must be between 0 and 100000000", ErrValidation)
	}
	if quotas.MaxStorageMB < 0 || quotas.MaxStorageMB > 1_000_000_000 {
		return fmt.Errorf("%w: maxStorageMb must be between 0 and 1000000000", ErrValidation)
	}
	return nil
}

func GetProjectMetrics(ctx context.Context, pool *pgxpool.Pool, projectSlug string, hours int, now time.Time) (*ProjectMetrics, error) {
	if hours <= 0 {
		hours = 24
	}
	if hours > 2160 {
		hours = 2160
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	quotas, err := getProjectQuotasByProject(ctx, pool, project)
	if err != nil {
		return nil, err
	}
	windowStart := now.UTC().Add(-time.Duration(hours) * time.Hour)
	metrics := &ProjectMetrics{
		ProjectID:        project.ID,
		ProjectSlug:      project.Slug,
		WindowHours:      hours,
		Quota:            *quotas,
		WindowStartedAt:  windowStart,
		WindowFinishedAt: now.UTC(),
	}

	if exists, err := tableExists(ctx, pool, project.SchemaName, authUsersCollection); err != nil {
		return nil, err
	} else if exists {
		if err := pool.QueryRow(ctx, fmt.Sprintf(`select count(*) from %s`, quoteIdent(project.SchemaName, authUsersCollection))).Scan(&metrics.AppUsers); err != nil {
			return nil, err
		}
	}
	if exists, err := tableExists(ctx, pool, project.SchemaName, appOrganizationsTable); err != nil {
		return nil, err
	} else if exists {
		if err := pool.QueryRow(ctx, fmt.Sprintf(`select count(*) from %s`, quoteIdent(project.SchemaName, appOrganizationsTable))).Scan(&metrics.Organizations); err != nil {
			return nil, err
		}
	}
	storageBytes, err := ProjectStorageUsageBytes(ctx, pool, project)
	if err != nil {
		return nil, err
	}
	metrics.StorageBytes = storageBytes
	if err := pool.QueryRow(ctx, `
		select count(*)
		from _dbo.sessions
		where project_id = $1 and collection = 'users' and revoked_at is null and expires_at > $2`,
		project.ID,
		now.UTC(),
	).Scan(&metrics.ActiveSessions); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, `
		select count(*),
		       count(*) filter (where status >= 500),
		       coalesce(avg(duration_ms), 0),
		       coalesce(percentile_cont(0.95) within group (order by duration_ms), 0)
		from _dbo.request_logs
		where project_id = $1 and created_at >= $2`,
		project.ID,
		windowStart,
	).Scan(&metrics.Requests.Total, &metrics.Requests.Errors, &metrics.Requests.AvgDuration, &metrics.Requests.P95Duration); err != nil {
		return nil, err
	}
	return metrics, nil
}

func EnsureProjectStorageQuota(ctx context.Context, pool *pgxpool.Pool, project *Project, incomingBytes int64) error {
	if incomingBytes <= 0 {
		return nil
	}
	quotas, err := getProjectQuotasByProject(ctx, pool, project)
	if err != nil {
		return err
	}
	if !quotas.Enabled || quotas.MaxStorageMB <= 0 {
		return nil
	}
	used, err := ProjectStorageUsageBytes(ctx, pool, project)
	if err != nil {
		return err
	}
	limit := int64(quotas.MaxStorageMB) * 1024 * 1024
	if used+incomingBytes > limit {
		return fmt.Errorf("%w: project storage quota exceeded", ErrQuotaExceeded)
	}
	return nil
}

func ProjectStorageUsageBytes(ctx context.Context, pool *pgxpool.Pool, project *Project) (int64, error) {
	collections, err := ListCollections(ctx, pool, project.Slug)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, collection := range collections {
		if collection.Type == CollectionView {
			continue
		}
		table := quoteIdent(project.SchemaName, collection.Name)
		for _, field := range collection.Fields {
			if field.Type != "file" {
				continue
			}
			column := quoteIdent(field.Name)
			var fieldTotal int64
			if err := pool.QueryRow(ctx, fmt.Sprintf(`
				select coalesce(sum(size_bytes), 0)::bigint
				from (
					select case
						when jsonb_typeof(%[1]s) = 'object' and (%[1]s->>'size') ~ '^[0-9]+$'
							then (%[1]s->>'size')::bigint
						when jsonb_typeof(%[1]s) = 'array'
							then (
								select coalesce(sum((item->>'size')::bigint), 0)::bigint
								from jsonb_array_elements(%[1]s) item
								where item ? 'size' and (item->>'size') ~ '^[0-9]+$'
							)
						else 0
					end as size_bytes
					from %[2]s
					where %[1]s is not null
				) sizes`,
				column,
				table,
			)).Scan(&fieldTotal); err != nil {
				return 0, mapRecordDBError(err)
			}
			total += fieldTotal
		}
	}
	return total, nil
}

func tableExists(ctx context.Context, pool *pgxpool.Pool, schemaName string, tableName string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `select to_regclass($1) is not null`, schemaName+"."+tableName).Scan(&exists)
	return exists, err
}
