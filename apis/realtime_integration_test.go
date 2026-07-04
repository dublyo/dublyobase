package apis

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type testSSEEvent struct {
	Event string
	ID    string
	Data  string
}

func TestRealtimeRecordEvents(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)
	serviceKey := createAPIKeyForRecords(t, srv.Handler, adminToken, slug, "service")

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), adminToken, `{
		"name":"posts",
		"type":"base",
		"fields":[{"name":"title","type":"text","required":true}]
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reader, closeStream := openRealtimeStream(t, ctx, ts, slug, serviceKey, "posts", "create,update,delete")
	defer closeStream()

	ready := readSSEEvent(t, reader)
	if ready.Event != "ready" {
		t.Fatalf("first SSE event = %q, want ready", ready.Event)
	}

	record := createRecordInCollectionForTest(t, srv.Handler, slug, "posts", serviceKey, `{"title":"Draft"}`)
	created := readRealtimePayload(t, reader, "record.create")
	if created.Collection != "posts" || created.Action != "create" || created.ID != record["id"] {
		t.Fatalf("bad create payload: %+v record=%+v", created, record)
	}
	if created.Record["title"] != "Draft" {
		t.Fatalf("create record title = %v", created.Record["title"])
	}

	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records/%s", slug, record["id"]), serviceKey, `{"title":"Published"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch record: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated := readRealtimePayload(t, reader, "record.update")
	if updated.ID != record["id"] || updated.Record["title"] != "Published" {
		t.Fatalf("bad update payload: %+v", updated)
	}

	rec = deleteJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records/%s", slug, record["id"]), serviceKey, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete record: want 204, got %d: %s", rec.Code, rec.Body.String())
	}
	deleted := readRealtimePayload(t, reader, "record.delete")
	if deleted.ID != record["id"] || deleted.Record["title"] != "Published" {
		t.Fatalf("bad delete payload: %+v", deleted)
	}
}

func TestRealtimeHonorsViewRules(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)
	serviceKey := createAPIKeyForRecords(t, srv.Handler, adminToken, slug, "service")

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), adminToken, `{
		"name":"posts",
		"type":"base",
		"fields":[
			{"name":"title","type":"text","required":true},
			{"name":"published","type":"bool"}
		],
		"listRule":"published = true",
		"viewRule":"published = true"
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reader, closeStream := openRealtimeStream(t, ctx, ts, slug, "", "posts", "create")
	defer closeStream()

	ready := readSSEEvent(t, reader)
	if ready.Event != "ready" {
		t.Fatalf("first SSE event = %q, want ready", ready.Event)
	}

	createRecordInCollectionForTest(t, srv.Handler, slug, "posts", serviceKey, `{"title":"Private","published":false}`)
	createRecordInCollectionForTest(t, srv.Handler, slug, "posts", serviceKey, `{"title":"Public","published":true}`)

	event := readRealtimePayload(t, reader, "record.create")
	if event.Record["title"] != "Public" {
		t.Fatalf("anon stream leaked or missed rule-filtered events: %+v", event)
	}
}

func openRealtimeStream(t *testing.T, ctx context.Context, ts *httptest.Server, slug string, token string, collection string, events string) (*bufio.Reader, func()) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/projects/%s/realtime?collection=%s&events=%s", ts.URL, slug, collection, events), nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		t.Fatalf("open realtime stream: want 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	return bufio.NewReader(resp.Body), func() { resp.Body.Close() }
}

func readRealtimePayload(t *testing.T, reader *bufio.Reader, wantEvent string) realtimePayload {
	t.Helper()
	event := readSSEEvent(t, reader)
	if event.Event != wantEvent {
		t.Fatalf("SSE event = %q, want %q; data=%s", event.Event, wantEvent, event.Data)
	}
	var payload realtimePayload
	if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
		t.Fatalf("decode realtime payload: %v; data=%s", err, event.Data)
	}
	return payload
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) testSSEEvent {
	t.Helper()
	var event testSSEEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE event: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if event.Event != "" || event.Data != "" || event.ID != "" {
				return event
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch key {
		case "event":
			event.Event = value
		case "id":
			event.ID = value
		case "data":
			if event.Data != "" {
				event.Data += "\n"
			}
			event.Data += value
		}
	}
}
