package core

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RealtimeEventRow struct {
	ID         int64     `json:"id"`
	SourceID   string    `json:"sourceId"`
	Project    string    `json:"project"`
	Collection string    `json:"collection"`
	Action     string    `json:"action"`
	RecordID   string    `json:"recordId"`
	Record     Record    `json:"record"`
	CreatedAt  time.Time `json:"createdAt"`
}

func InsertRealtimeEvent(ctx context.Context, pool *pgxpool.Pool, sourceID string, projectSlug string, collection string, action string, recordID string, record Record) error {
	if pool == nil || sourceID == "" || projectSlug == "" || collection == "" || action == "" {
		return nil
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		with inserted as (
			insert into _dbo.realtime_events (source_id, project_slug, collection, action, record_id, record)
			values ($1, $2, $3, $4, $5, $6::jsonb)
			returning id::text
		)
		select pg_notify('dbo_realtime', (select id from inserted))`,
		sourceID,
		projectSlug,
		collection,
		action,
		recordID,
		raw,
	)
	return err
}

func ListRealtimeEventsAfter(ctx context.Context, pool *pgxpool.Pool, afterID int64, sourceID string, limit int) ([]RealtimeEventRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
		select id, source_id, project_slug, collection, action, record_id, record, created_at
		from _dbo.realtime_events
		where id > $1 and source_id <> $2
		order by id asc
		limit $3`,
		afterID,
		sourceID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RealtimeEventRow, 0)
	for rows.Next() {
		var item RealtimeEventRow
		var raw []byte
		if err := rows.Scan(&item.ID, &item.SourceID, &item.Project, &item.Collection, &item.Action, &item.RecordID, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &item.Record)
		}
		if item.Record == nil {
			item.Record = Record{}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func PruneRealtimeEvents(ctx context.Context, pool *pgxpool.Pool, retention time.Duration) error {
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	_, err := pool.Exec(ctx, `delete from _dbo.realtime_events where created_at < $1`, time.Now().UTC().Add(-retention))
	return err
}
