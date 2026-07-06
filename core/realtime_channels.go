package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RealtimePresence struct {
	Channel    string         `json:"channel"`
	SessionID  string         `json:"sessionId"`
	UserID     string         `json:"userId,omitempty"`
	State      map[string]any `json:"state"`
	LastSeenAt time.Time      `json:"lastSeenAt"`
	ExpiresAt  time.Time      `json:"expiresAt"`
}

type RealtimeBroadcastRow struct {
	ID        int64          `json:"id"`
	SourceID  string         `json:"sourceId"`
	Project   string         `json:"project"`
	Channel   string         `json:"channel"`
	Event     string         `json:"event"`
	UserID    string         `json:"userId,omitempty"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"createdAt"`
}

func UpsertRealtimePresence(ctx context.Context, pool *pgxpool.Pool, projectSlug string, channel string, sessionID string, userID string, state map[string]any, ttl time.Duration) error {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return err
	}
	channel, err = normalizeRealtimeChannel(channel)
	if err != nil {
		return err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(sessionID) > 120 {
		return fmt.Errorf("%w: realtime sessionId is required", ErrValidation)
	}
	if ttl <= 0 || ttl > 5*time.Minute {
		ttl = 90 * time.Second
	}
	rawState, err := json.Marshal(redactAuditData(state))
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		insert into _dbo.realtime_presence (project_id, project_slug, channel, user_id, session_id, state, last_seen_at, expires_at)
		values ($1, $2, $3, $4, $5, $6::jsonb, now(), now() + $7::interval)
		on conflict (project_id, channel, session_id) do update
		set user_id = excluded.user_id,
			state = excluded.state,
			last_seen_at = now(),
			expires_at = excluded.expires_at`,
		project.ID,
		project.Slug,
		channel,
		userID,
		sessionID,
		rawState,
		fmt.Sprintf("%d seconds", int(ttl.Seconds())),
	)
	return err
}

func RemoveRealtimePresence(ctx context.Context, pool *pgxpool.Pool, projectSlug string, channel string, sessionID string) error {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return err
	}
	channel, err = normalizeRealtimeChannel(channel)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		delete from _dbo.realtime_presence
		where project_id = $1 and channel = $2 and session_id = $3`,
		project.ID,
		channel,
		strings.TrimSpace(sessionID),
	)
	return err
}

func ListRealtimePresence(ctx context.Context, pool *pgxpool.Pool, projectSlug string, channel string, limit int) ([]RealtimePresence, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	channel, err = normalizeRealtimeChannel(channel)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
		select channel, session_id, user_id, state, last_seen_at, expires_at
		from _dbo.realtime_presence
		where project_id = $1 and channel = $2 and expires_at > now()
		order by last_seen_at desc
		limit $3`,
		project.ID,
		channel,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]RealtimePresence, 0)
	for rows.Next() {
		var item RealtimePresence
		var raw []byte
		if err := rows.Scan(&item.Channel, &item.SessionID, &item.UserID, &raw, &item.LastSeenAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &item.State)
		}
		if item.State == nil {
			item.State = map[string]any{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func InsertRealtimeBroadcast(ctx context.Context, pool *pgxpool.Pool, sourceID string, projectSlug string, channel string, event string, userID string, payload map[string]any) error {
	if pool == nil || sourceID == "" {
		return nil
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return err
	}
	channel, err = normalizeRealtimeChannel(channel)
	if err != nil {
		return err
	}
	event = strings.TrimSpace(event)
	if event == "" || len(event) > 120 || strings.ContainsAny(event, "\r\n\t") {
		return fmt.Errorf("%w: realtime event is invalid", ErrValidation)
	}
	rawPayload, err := json.Marshal(redactAuditData(payload))
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		with inserted as (
			insert into _dbo.realtime_broadcasts (source_id, project_id, project_slug, channel, event, user_id, payload)
			values ($1, $2, $3, $4, $5, $6, $7::jsonb)
			returning id::text
		)
		select pg_notify('dbo_realtime_broadcast', (select id from inserted))`,
		sourceID,
		project.ID,
		project.Slug,
		channel,
		event,
		userID,
		rawPayload,
	)
	return err
}

func ListRealtimeBroadcastsAfter(ctx context.Context, pool *pgxpool.Pool, afterID int64, sourceID string, limit int) ([]RealtimeBroadcastRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
		select id, source_id, project_slug, channel, event, user_id, payload, created_at
		from _dbo.realtime_broadcasts
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
	out := make([]RealtimeBroadcastRow, 0)
	for rows.Next() {
		var item RealtimeBroadcastRow
		var raw []byte
		if err := rows.Scan(&item.ID, &item.SourceID, &item.Project, &item.Channel, &item.Event, &item.UserID, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &item.Payload)
		}
		if item.Payload == nil {
			item.Payload = map[string]any{}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func PruneRealtimeChannelState(ctx context.Context, pool *pgxpool.Pool, retention time.Duration) error {
	if retention <= 0 {
		retention = 24 * time.Hour
	}
	cutoff := time.Now().UTC().Add(-retention)
	if _, err := pool.Exec(ctx, `delete from _dbo.realtime_presence where expires_at < now()`); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `delete from _dbo.realtime_broadcasts where created_at < $1`, cutoff)
	return err
}

func normalizeRealtimeChannel(channel string) (string, error) {
	channel = strings.TrimSpace(channel)
	if channel == "" || len(channel) > 120 {
		return "", fmt.Errorf("%w: realtime channel is required", ErrValidation)
	}
	if strings.ContainsAny(channel, "\r\n\t") {
		return "", fmt.Errorf("%w: realtime channel is invalid", ErrValidation)
	}
	return channel, nil
}
