package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SchemaVersion struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Project   string          `json:"project"`
	Version   int             `json:"version"`
	Label     string          `json:"label"`
	Snapshot  json.RawMessage `json:"snapshot,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

func CreateSchemaVersion(ctx context.Context, pool *pgxpool.Pool, adminID string, projectSlug string, label string, ip string, userAgent string) (*SchemaVersion, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	label = strings.TrimSpace(label)
	if len(label) > 120 {
		return nil, fmt.Errorf("%w: schema version label is too long", ErrValidation)
	}
	export, err := ExportCollections(ctx, pool, project.Slug)
	if err != nil {
		return nil, err
	}
	snapshot, err := json.Marshal(export)
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, collectionAdvisoryLockID); err != nil {
		return nil, err
	}
	var version int
	if err := tx.QueryRow(ctx, `
		select coalesce(max(version), 0) + 1
		from _dbo.schema_versions
		where project_id = $1`,
		project.ID,
	).Scan(&version); err != nil {
		return nil, err
	}
	item := &SchemaVersion{ProjectID: project.ID, Project: project.Slug, Version: version, Label: label, Snapshot: snapshot}
	if err := tx.QueryRow(ctx, `
		insert into _dbo.schema_versions (project_id, version, label, snapshot, created_by_admin_id)
		values ($1, $2, $3, $4::jsonb, $5)
		returning id, created_at`,
		project.ID,
		version,
		label,
		snapshot,
		adminID,
	).Scan(&item.ID, &item.CreatedAt); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "schema.version.create",
		TargetType: "project",
		TargetID:   project.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "version": version, "label": label},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

func ListSchemaVersions(ctx context.Context, pool *pgxpool.Pool, projectSlug string, limit int) ([]SchemaVersion, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 250 {
		limit = 50
	}
	rows, err := pool.Query(ctx, `
		select id, project_id, version, label, created_at
		from _dbo.schema_versions
		where project_id = $1
		order by version desc
		limit $2`,
		project.ID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SchemaVersion{}
	for rows.Next() {
		var item SchemaVersion
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.Version, &item.Label, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Project = project.Slug
		items = append(items, item)
	}
	return items, rows.Err()
}

func GetSchemaVersion(ctx context.Context, pool *pgxpool.Pool, projectSlug string, id string) (*SchemaVersion, error) {
	if err := ValidateUUID(id); err != nil {
		return nil, err
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	item := &SchemaVersion{Project: project.Slug}
	if err := pool.QueryRow(ctx, `
		select id, project_id, version, label, snapshot, created_at
		from _dbo.schema_versions
		where project_id = $1 and id = $2`,
		project.ID,
		id,
	).Scan(&item.ID, &item.ProjectID, &item.Version, &item.Label, &item.Snapshot, &item.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return item, nil
}
