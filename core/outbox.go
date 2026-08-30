package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A transactional outbox. Realtime events and webhook deliveries used to be
// written after the record transaction committed, so a crash in that window
// lost the event with nothing left to say it was ever owed. The outbox row is
// now written by the same trigger that records history, inside the same
// transaction as the write itself: if the row exists, the event exists.
//
// Delivery stays fast. The request still publishes immediately after commit and
// marks the row done; the worker exists only to sweep up rows nobody marked,
// which is exactly the crash case. That keeps latency unchanged and makes loss
// impossible rather than unlikely.

const outboxTable = "dbo_event_outbox"

type OutboxEvent struct {
	ID         int64
	Collection string
	RecordID   string
	Action     string
	Payload    json.RawMessage
	Attempts   int
}

func ensureProjectOutbox(ctx context.Context, tx pgx.Tx, project *Project) error {
	table := quoteIdent(project.SchemaName, outboxTable)
	statements := []string{
		fmt.Sprintf(`create table if not exists %s (
			id           bigserial primary key,
			collection   text        not null,
			record_id    text        not null,
			action       text        not null,
			payload      jsonb,
			created_at   timestamptz not null default now(),
			published_at timestamptz,
			attempts     integer     not null default 0,
			last_error   text        not null default ''
		)`, table),
		// Partial index: the sweep only ever asks for undelivered rows, and this
		// keeps that query independent of how large the delivered history grows.
		fmt.Sprintf(`create index if not exists %s on %s (id) where published_at is null`,
			quoteIdent(outboxTable+"_pending_idx"), table),
		fmt.Sprintf(`revoke all on %s from public`, table),
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// LatestOutboxID finds the row the write that just happened produced, so the
// request can mark its own event delivered and keep the sweep off it.
func LatestOutboxID(ctx context.Context, pool *pgxpool.Pool, schemaName, collection, recordID, action string) (int64, bool) {
	var id int64
	err := pool.QueryRow(ctx, fmt.Sprintf(`
		select id from %s
		where collection = $1 and record_id = $2 and action = $3 and published_at is null
		order by id desc limit 1`, quoteIdent(schemaName, outboxTable)),
		collection, recordID, action).Scan(&id)
	if err != nil {
		return 0, false
	}
	return id, true
}

// MarkOutboxPublished records that the request delivered this event itself, so
// the sweep leaves it alone.
func MarkOutboxPublished(ctx context.Context, pool *pgxpool.Pool, schemaName string, id int64) error {
	_, err := pool.Exec(ctx, fmt.Sprintf(
		`update %s set published_at = now() where id = $1 and published_at is null`,
		quoteIdent(schemaName, outboxTable)), id)
	return err
}

// ClaimPendingOutbox returns events written at least minAge ago that nobody has
// marked delivered. The delay matters: without it the sweep would race the
// request that is about to publish the event normally, and every event would be
// delivered twice.
func ClaimPendingOutbox(ctx context.Context, pool *pgxpool.Pool, schemaName string, minAge time.Duration, limit int) ([]OutboxEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := pool.Query(ctx, fmt.Sprintf(`
		update %s set attempts = attempts + 1
		where id in (
			select id from %s
			where published_at is null and created_at < now() - $1::interval and attempts < 10
			order by id limit $2
			for update skip locked
		)
		returning id, collection, record_id, action, payload, attempts`,
		quoteIdent(schemaName, outboxTable), quoteIdent(schemaName, outboxTable)),
		fmt.Sprintf("%d milliseconds", minAge.Milliseconds()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OutboxEvent{}
	for rows.Next() {
		var e OutboxEvent
		if err := rows.Scan(&e.ID, &e.Collection, &e.RecordID, &e.Action, &e.Payload, &e.Attempts); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneOutbox drops delivered rows once they are well past useful. Undelivered
// rows are never pruned: an event nobody could deliver is a fault to look at,
// not litter to sweep away.
func PruneOutbox(ctx context.Context, pool *pgxpool.Pool, schemaName string, keep time.Duration) (int64, error) {
	tag, err := pool.Exec(ctx, fmt.Sprintf(
		`delete from %s where published_at is not null and published_at < now() - $1::interval`,
		quoteIdent(schemaName, outboxTable)),
		fmt.Sprintf("%d seconds", int(keep.Seconds())))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// OutboxProjects lists projects that have an outbox to sweep.
func OutboxProjects(ctx context.Context, pool *pgxpool.Pool) ([]Project, error) {
	projects, err := ListProjects(ctx, pool)
	if err != nil {
		return nil, err
	}
	out := []Project{}
	for _, p := range projects {
		var exists bool
		if err := pool.QueryRow(ctx,
			`select exists(select 1 from information_schema.tables where table_schema = $1 and table_name = $2)`,
			p.SchemaName, outboxTable).Scan(&exists); err != nil {
			continue
		}
		if exists {
			out = append(out, p)
		}
	}
	return out, nil
}

// OutboxPublisher delivers one swept event. The HTTP layer supplies it, because
// realtime fanout and webhook enqueue live there.
type OutboxPublisher func(ctx context.Context, project Project, event OutboxEvent) error

const (
	// sweepBatchSize is how many events one claim takes. Small enough that a
	// failure loses little work, large enough not to be chatty.
	sweepBatchSize = 200
	// maxSweepBatches and sweepTimeBudget bound one project's turn, so a large
	// backlog drains quickly without starving the other projects or the rest of
	// the ops worker.
	maxSweepBatches = 50
	sweepTimeBudget = 20 * time.Second
)

// SweepOutbox delivers events the request that created them never marked done —
// which in practice means the process died between COMMIT and publish. It runs
// on the ops worker, coordinated by an advisory lock so several replicas do not
// each deliver the same event.
func SweepOutbox(ctx context.Context, pool *pgxpool.Pool, log Logger, publish OutboxPublisher, minAge time.Duration) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `select pg_try_advisory_lock($1)`, outboxLockID).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil // another replica is sweeping
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `select pg_advisory_unlock($1)`, outboxLockID)
	}()

	projects, err := OutboxProjects(ctx, pool)
	if err != nil {
		return err
	}
	for _, project := range projects {
		// Keep draining while there is a backlog, rather than taking one batch
		// per tick and leaving the rest. A bulk import writes one event per
		// row, so a single load can leave hundreds of thousands behind; at one
		// batch a minute that took the better part of a day to clear, and the
		// table stayed large the whole time. The budget still bounds the work
		// so a backlog cannot monopolise the worker.
		swept := 0
		deadline := time.Now().Add(sweepTimeBudget)
		for batch := 0; batch < maxSweepBatches; batch++ {
			events, err := ClaimPendingOutbox(ctx, pool, project.SchemaName, minAge, sweepBatchSize)
			if err != nil {
				log.Warn("outbox claim failed", "project", project.Slug, "err", err)
				break
			}
			if len(events) == 0 {
				break
			}
			failed := false
			for _, event := range events {
				if err := publish(ctx, project, event); err != nil {
					_, _ = pool.Exec(ctx, fmt.Sprintf(`update %s set last_error = $1 where id = $2`,
						quoteIdent(project.SchemaName, outboxTable)), err.Error(), event.ID)
					log.Warn("outbox publish failed", "project", project.Slug, "event", event.ID, "err", err)
					failed = true
					continue
				}
				if err := MarkOutboxPublished(ctx, pool, project.SchemaName, event.ID); err != nil {
					log.Warn("outbox mark failed", "project", project.Slug, "event", event.ID, "err", err)
				}
			}
			swept += len(events)
			// Draining continues only while delivery is working. A failed event
			// stays unpublished, so carrying on would re-claim it and spend all
			// ten of its attempts inside this one sweep — the retries are meant
			// to be spread across ticks so a downstream has time to recover.
			if failed || ctx.Err() != nil || time.Now().After(deadline) {
				break
			}
		}
		if swept > 0 {
			log.Info("outbox swept undelivered events", "project", project.Slug, "count", swept)
		}
		if _, err := PruneOutbox(ctx, pool, project.SchemaName, 7*24*time.Hour); err != nil {
			log.Warn("outbox prune failed", "project", project.Slug, "err", err)
		}
	}
	return nil
}

// Logger is the small slice of *slog.Logger this file needs, so core does not
// depend on the concrete type.
type Logger interface {
	Warn(msg string, args ...any)
	Info(msg string, args ...any)
}

const outboxLockID int64 = 326_326_012
