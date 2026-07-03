package core

import (
	"encoding/json"
	"errors"
	"testing"
)

func testRecordCollection() *Collection {
	return &Collection{
		Name: "posts",
		Type: CollectionBase,
		Fields: []Field{
			{Name: "title", Type: "text", Required: true},
			{Name: "published", Type: "bool"},
			{Name: "status", Type: "select", Options: map[string]any{"values": []string{"draft", "live"}}},
			{Name: "owner", Type: "relation", Options: map[string]any{"collection": "users"}},
		},
	}
}

func TestNormalizeCreatePayload(t *testing.T) {
	payload, err := normalizeCreatePayload(testRecordCollection(), map[string]json.RawMessage{
		"title":     json.RawMessage(`"Hello"`),
		"published": json.RawMessage(`true`),
		"status":    json.RawMessage(`"draft"`),
		"owner":     json.RawMessage(`"9c10d5b9-3a23-4f25-91c3-09a40d7e9f7e"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Columns) != 4 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestNormalizeRecordPayloadRejectsBadInput(t *testing.T) {
	cases := map[string]map[string]json.RawMessage{
		"missing required": {"published": json.RawMessage(`true`)},
		"system field":     {"id": json.RawMessage(`"9c10d5b9-3a23-4f25-91c3-09a40d7e9f7e"`), "title": json.RawMessage(`"Hello"`)},
		"unknown field":    {"title": json.RawMessage(`"Hello"`), "missing": json.RawMessage(`true`)},
		"duplicate field":  {"title": json.RawMessage(`"Hello"`), "Title": json.RawMessage(`"Second"`)},
		"bad select":       {"title": json.RawMessage(`"Hello"`), "status": json.RawMessage(`"bad"`)},
		"bad relation":     {"title": json.RawMessage(`"Hello"`), "owner": json.RawMessage(`"bad"`)},
	}
	for name, body := range cases {
		if _, err := normalizeCreatePayload(testRecordCollection(), body); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s: err = %v, want ErrValidation", name, err)
		}
	}
}

func TestProjectionAndSortValidation(t *testing.T) {
	c := testRecordCollection()
	columns, err := projectionColumns(c, "id,title,owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(columns) != 3 {
		t.Fatalf("columns = %v", columns)
	}
	if _, err := projectionColumns(c, "bad"); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad projection err = %v", err)
	}
	if _, err := orderByClause(c, "-created,title"); err != nil {
		t.Fatal(err)
	}
	if _, err := orderByClause(c, "bad"); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad sort err = %v", err)
	}
}
