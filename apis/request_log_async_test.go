package apis

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/dublyo/dublyobase/core"
)

// The access-log insert used to run inside the handler, so every request waited
// for a write it did not depend on. It is now queued and written in batches.
func TestRequestLogsAreWrittenAsynchronously(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	ctx := context.Background()

	if rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"rows","type":"base","fields":[{"name":"n","type":"number"}]}`); rec.Code != http.StatusCreated {
		t.Fatalf("collection: %d %s", rec.Code, rec.Body.String())
	}

	countLogs := func() int {
		var n int
		if err := app.Pool.QueryRow(ctx,
			`select count(*) from _dbo.request_logs where project_slug = $1`, slug).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := countLogs()

	const requests = 40
	for i := 0; i < requests; i++ {
		if rec := getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/rows/records", slug), token); rec.Code != http.StatusOK {
			t.Fatalf("read %d: %d", i, rec.Code)
		}
	}

	// Draining is what makes them visible; that it is not immediate is the point.
	app.RequestLogs.Close()
	after := countLogs()
	if after-before < requests {
		t.Errorf("logged %d of %d requests after drain", after-before, requests)
	}
	written, dropped := app.RequestLogs.Stats()
	if written < int64(requests) {
		t.Errorf("writer reported %d written, want at least %d", written, requests)
	}
	if dropped != 0 {
		t.Errorf("dropped %d events at this volume", dropped)
	}
}

// A full buffer must drop rather than block, since a stalled access log would
// otherwise stall the request it describes.
func TestRequestLogWriterDropsRatherThanBlocks(t *testing.T) {
	w := core.NewRequestLogWriter(nil) // no pool: Record is a no-op and must not block
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100000; i++ {
			w.Record(core.RequestLogEvent{Method: "GET", Path: "/x", Status: 200})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Record blocked")
	}
}
