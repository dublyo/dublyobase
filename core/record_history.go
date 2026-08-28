package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Record history answers "who changed this, when, and what did it look like
// before" — the question an amendment log, a dispute and an audit all reduce to.
//
// Capture is a Postgres trigger rather than Go code on the write path, so it
// cannot be bypassed: a write from the admin SQL console, a migration, or any
// other client is recorded exactly like an API call. The actor is read from the
// same request claims the RLS policies use, so it is whoever the row-level
// security believed was writing.
//
// The table lives in the project's own schema, which means a per-project
// pg_dump carries its history with it.

const historyTable = "dbo_record_history"

type RecordHistoryEntry struct {
	ID         int64           `json:"id"`
	Collection string          `json:"collection"`
	RecordID   string          `json:"recordId"`
	Action     string          `json:"action"`
	ActorRole  string          `json:"actorRole"`
	ActorID    string          `json:"actorId,omitempty"`
	TxID       int64           `json:"txId"`
	OccurredAt time.Time       `json:"occurredAt"`
	Changed    []string        `json:"changed,omitempty"`
	Before     json.RawMessage `json:"before,omitempty"`
	After      json.RawMessage `json:"after,omitempty"`
}

// ensureProjectHistory creates the history table and the shared trigger
// function for a project. Safe to call repeatedly.
func ensureProjectHistory(ctx context.Context, tx pgx.Tx, project *Project) error {
	schema := quoteIdent(project.SchemaName)
	table := quoteIdent(project.SchemaName, historyTable)
	_, roles := ProjectNames(project.Slug)

	statements := []string{
		fmt.Sprintf(`create table if not exists %s (
			id           bigserial primary key,
			collection   text        not null,
			record_id    text        not null,
			action       text        not null,
			actor_role   text        not null default '',
			actor_id     text        not null default '',
			tx_id        bigint      not null,
			occurred_at  timestamptz not null default now(),
			changed      text[]      not null default '{}',
			before       jsonb,
			after        jsonb
		)`, table),
		fmt.Sprintf(`create index if not exists %s on %s (collection, record_id, id desc)`,
			quoteIdent(historyTable+"_record_idx"), table),
		fmt.Sprintf(`create index if not exists %s on %s (occurred_at desc)`,
			quoteIdent(historyTable+"_time_idx"), table),

		// History is append-only from the application's side: the project roles
		// may read it and nothing more. Even the service role cannot rewrite it,
		// because an audit trail a caller can edit is not an audit trail. The
		// trigger writes as SECURITY DEFINER and so is unaffected.
		fmt.Sprintf(`revoke all on %s from public`, table),
		fmt.Sprintf(`grant select on %s to %s, %s, %s`, table,
			quoteIdent(roles.Anon), quoteIdent(roles.Authenticated), quoteIdent(roles.Service)),

		fmt.Sprintf(`create or replace function %s() returns trigger
language plpgsql security definer set search_path = %s, pg_catalog as $fn$
declare
  before_row jsonb;
  after_row  jsonb;
  changed_keys text[] := '{}';
  key text;
begin
  if tg_op = 'DELETE' then
    before_row := to_jsonb(old);
  elsif tg_op = 'INSERT' then
    after_row := to_jsonb(new);
  else
    before_row := to_jsonb(old);
    after_row  := to_jsonb(new);
    -- record which columns actually moved, so a diff does not have to be
    -- recomputed by every reader
    -- `updated` is maintained by the platform and moves on every write, so
    -- listing it would drown the field that actually changed. It is still in
    -- before/after; only the summary omits it.
    for key in select jsonb_object_keys(after_row) loop
      if key <> 'updated' and before_row -> key is distinct from after_row -> key then
        changed_keys := array_append(changed_keys, key);
      end if;
    end loop;
    if changed_keys = '{}' then
      return new;  -- an update that changed nothing meaningful is not history
    end if;
  end if;

  insert into %s (collection, record_id, action, actor_role, actor_id, tx_id, changed, before, after)
  values (
    tg_table_name,
    coalesce(after_row ->> 'id', before_row ->> 'id', ''),
    lower(tg_op),
    coalesce(_dbo.request_role(), ''),
    coalesce(_dbo.request_claim('sub'), ''),
    txid_current(),
    changed_keys,
    before_row,
    after_row
  );
  if tg_op = 'DELETE' then return old; end if;
  return new;
end
$fn$;`, quoteIdent(project.SchemaName, historyTable+"_fn"), schema, table),
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// syncCollectionHistory attaches (or removes) the history trigger for one
// collection. History is on by default; options.history = false opts out, for a
// high-churn table where the trail is not worth the write amplification.
func syncCollectionHistory(ctx context.Context, tx pgx.Tx, project *Project, collection *Collection) error {
	if err := ensureProjectHistory(ctx, tx, project); err != nil {
		return err
	}
	table := quoteIdent(project.SchemaName, collection.Name)
	trigger := quoteIdent("dbo_hist_" + collection.Name)
	if _, err := tx.Exec(ctx, fmt.Sprintf(`drop trigger if exists %s on %s`, trigger, table)); err != nil {
		return err
	}
	if !collectionHistoryEnabled(collection) {
		return nil
	}
	_, err := tx.Exec(ctx, fmt.Sprintf(
		`create trigger %s after insert or update or delete on %s for each row execute function %s()`,
		trigger, table, quoteIdent(project.SchemaName, historyTable+"_fn")))
	return err
}

type historyOptions struct {
	History *bool `json:"history"`
}

func collectionHistoryEnabled(collection *Collection) bool {
	if collection == nil || len(collection.Options) == 0 {
		return true
	}
	var opts historyOptions
	if err := json.Unmarshal(collection.Options, &opts); err != nil {
		return true
	}
	return opts.History == nil || *opts.History
}

// ListRecordHistory returns the trail for one record, newest first. It runs
// under the caller's role, and first checks the caller can actually read the
// record — otherwise the history endpoint would be a way to read rows that
// row-level security hides.
func ListRecordHistory(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, recordID string, limit int) ([]RecordHistoryEntry, error) {
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return nil, err
	}
	if _, err := GetRecord(ctx, pool, auth, collectionName, recordID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := []RecordHistoryEntry{}
	err = withRecordTxForCollection(ctx, pool, auth, collection, "view", func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, fmt.Sprintf(`
			select id, collection, record_id, action, actor_role, actor_id, tx_id, occurred_at, changed, before, after
			from %s where collection = $1 and record_id = $2
			order by id desc limit $3`, quoteIdent(auth.Project.SchemaName, historyTable)),
			collection.Name, recordID, limit)
		if err != nil {
			return mapRecordDBError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var e RecordHistoryEntry
			if err := rows.Scan(&e.ID, &e.Collection, &e.RecordID, &e.Action, &e.ActorRole,
				&e.ActorID, &e.TxID, &e.OccurredAt, &e.Changed, &e.Before, &e.After); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
