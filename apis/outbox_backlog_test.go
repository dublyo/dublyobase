package apis

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/dublyo/dublyobase/core"
)

// A bulk load writes one event per row, so one import can leave a very large
// backlog. The sweep used to take a single batch per tick, which meant a
// quarter-million events needed the better part of a day to clear while the
// table stayed large. It now keeps draining while delivery is healthy.
func TestOutboxSweepDrainsABacklog(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	ctx := context.Background()

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"rows","type":"base","fields":[{"name":"n","type":"number","options":{"onlyInt":true}}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("collection: %d %s", rec.Code, rec.Body.String())
	}
	schema, _ := core.ProjectNames(slug)

	// A backlog larger than one batch, aged past the sweep's minimum.
	const backlog = 700
	if _, err := app.Pool.Exec(ctx, fmt.Sprintf(`
		insert into %s.rows (id, n, created, updated)
		select gen_random_uuid(), g, now(), now() from generate_series(1, %d) g`, schema, backlog)); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Pool.Exec(ctx, fmt.Sprintf(
		`update %s.dbo_event_outbox set created_at = now() - interval '5 minutes'`, schema)); err != nil {
		t.Fatal(err)
	}

	pending := func() int {
		var n int
		if err := app.Pool.QueryRow(ctx, fmt.Sprintf(
			`select count(*) from %s.dbo_event_outbox where published_at is null`, schema)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if before := pending(); before < backlog {
		t.Fatalf("expected a backlog of at least %d, got %d", backlog, before)
	}

	delivered := 0
	publish := func(context.Context, core.Project, core.OutboxEvent) error {
		delivered++
		return nil
	}
	if err := core.SweepOutbox(ctx, app.Pool, app.Log, publish, time.Minute); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if after := pending(); after != 0 {
		t.Errorf("one sweep left %d events pending; the backlog should have drained", after)
	}
	if delivered < backlog {
		t.Errorf("delivered %d of %d", delivered, backlog)
	}
}

// A failing downstream must still get its retries spread over ticks rather than
// having all ten spent inside one sweep.
func TestOutboxSweepStopsDrainingWhenDeliveryFails(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	ctx := context.Background()

	postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"rows","type":"base","fields":[{"name":"n","type":"number","options":{"onlyInt":true}}]}`)
	schema, _ := core.ProjectNames(slug)
	if _, err := app.Pool.Exec(ctx, fmt.Sprintf(`
		insert into %s.rows (id, n, created, updated)
		select gen_random_uuid(), g, now(), now() from generate_series(1, 500) g`, schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Pool.Exec(ctx, fmt.Sprintf(
		`update %s.dbo_event_outbox set created_at = now() - interval '5 minutes'`, schema)); err != nil {
		t.Fatal(err)
	}

	publish := func(context.Context, core.Project, core.OutboxEvent) error {
		return errors.New("downstream unavailable")
	}
	if err := core.SweepOutbox(ctx, app.Pool, app.Log, publish, time.Minute); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	var maxAttempts int
	if err := app.Pool.QueryRow(ctx, fmt.Sprintf(
		`select coalesce(max(attempts), 0) from %s.dbo_event_outbox`, schema)).Scan(&maxAttempts); err != nil {
		t.Fatal(err)
	}
	if maxAttempts > 1 {
		t.Errorf("one sweep spent %d attempts on a failing downstream; retries should be spread across ticks", maxAttempts)
	}
}
