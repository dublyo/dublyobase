package core

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"
)

func testRecordCollection() *Collection {
	return &Collection{
		Name: "posts",
		Type: CollectionBase,
		Fields: []Field{
			{Name: "title", Type: "text", Required: true},
			{Name: "summary", Type: "text", Options: map[string]any{"min": 3, "max": 12, "pattern": "^[A-Z].*"}},
			{Name: "body", Type: "editor", Options: map[string]any{"maxSize": 64}},
			{Name: "secret", Type: "password", Options: map[string]any{"min": 8, "cost": 4}},
			{Name: "internal_note", Type: "text", Hidden: true},
			{Name: "score", Type: "number", Options: map[string]any{"onlyInt": true, "min": 1, "max": 10}},
			{Name: "contact", Type: "email", Options: map[string]any{"onlyDomains": []string{"example.com"}}},
			{Name: "published", Type: "bool"},
			{Name: "status", Type: "select", Options: map[string]any{"values": []string{"draft", "live"}}},
			{Name: "tags", Type: "select", Options: map[string]any{"values": []string{"a", "b", "c"}, "maxSelect": 2}},
			{Name: "owner", Type: "relation", Options: map[string]any{"collection": "users"}},
			{Name: "reviewers", Type: "relation", Options: map[string]any{"collection": "users", "maxSelect": 2}},
			{Name: "created_auto", Type: "autodate", Options: map[string]any{"onCreate": true}},
		},
	}
}

func TestNormalizeCreatePayload(t *testing.T) {
	payload, err := normalizeCreatePayload(testRecordCollection(), map[string]json.RawMessage{
		"title":     json.RawMessage(`"Hello"`),
		"published": json.RawMessage(`true`),
		"status":    json.RawMessage(`"draft"`),
		"owner":     json.RawMessage(`"9c10d5b9-3a23-4f25-91c3-09a40d7e9f7e"`),
		"secret":    json.RawMessage(`"password-123"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Columns) != 5 {
		t.Fatalf("payload = %+v", payload)
	}
	for i, column := range payload.Columns {
		if column == "secret" {
			hash, ok := payload.Values[i].(string)
			if !ok || bcrypt.CompareHashAndPassword([]byte(hash), []byte("password-123")) != nil {
				t.Fatalf("password was not bcrypt hashed: %T %v", payload.Values[i], payload.Values[i])
			}
		}
	}
}

func TestNormalizeRequiredNumberAndBoolAllowZeroValues(t *testing.T) {
	collection := &Collection{
		Name: "metrics",
		Type: CollectionBase,
		Fields: []Field{
			{Name: "name", Type: "text", Required: true},
			{Name: "count", Type: "number", Required: true, Options: map[string]any{"onlyInt": true, "min": 0}},
			{Name: "enabled", Type: "bool", Required: true},
		},
	}
	payload, err := normalizeCreatePayload(collection, map[string]json.RawMessage{
		"name":    json.RawMessage(`"zero"`),
		"count":   json.RawMessage(`0`),
		"enabled": json.RawMessage(`false`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Columns) != 3 {
		t.Fatalf("payload columns = %v", payload.Columns)
	}
}

func TestNormalizeRecordPayloadRejectsBadInput(t *testing.T) {
	cases := map[string]map[string]json.RawMessage{
		"missing required": {"published": json.RawMessage(`true`)},
		"system field":     {"id": json.RawMessage(`"9c10d5b9-3a23-4f25-91c3-09a40d7e9f7e"`), "title": json.RawMessage(`"Hello"`)},
		"unknown field":    {"title": json.RawMessage(`"Hello"`), "missing": json.RawMessage(`true`)},
		"duplicate field":  {"title": json.RawMessage(`"Hello"`), "Title": json.RawMessage(`"Second"`)},
		"bad select":       {"title": json.RawMessage(`"Hello"`), "status": json.RawMessage(`"bad"`)},
		"too many select":  {"title": json.RawMessage(`"Hello"`), "tags": json.RawMessage(`["a","b","c"]`)},
		"bad relation":     {"title": json.RawMessage(`"Hello"`), "owner": json.RawMessage(`"bad"`)},
		"too many relation": {"title": json.RawMessage(`"Hello"`), "reviewers": json.RawMessage(`[
			"9c10d5b9-3a23-4f25-91c3-09a40d7e9f7e",
			"9c10d5b9-3a23-4f25-91c3-09a40d7e9f7f",
			"9c10d5b9-3a23-4f25-91c3-09a40d7e9f70"
		]`)},
		"bad text pattern": {"title": json.RawMessage(`"Hello"`), "summary": json.RawMessage(`"lower"`)},
		"bad number":       {"title": json.RawMessage(`"Hello"`), "score": json.RawMessage(`1.5`)},
		"bad email domain": {"title": json.RawMessage(`"Hello"`), "contact": json.RawMessage(`"me@example.net"`)},
		"bad editor size":  {"title": json.RawMessage(`"Hello"`), "body": json.RawMessage(`"` + strings.Repeat("x", 65) + `"`)},
		"autodate write":   {"title": json.RawMessage(`"Hello"`), "created_auto": json.RawMessage(`"2026-07-04T00:00:00Z"`)},
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
	if _, err := projectionColumns(c, "secret"); !errors.Is(err, ErrValidation) {
		t.Fatalf("password projection err = %v, want ErrValidation", err)
	}
	if _, err := projectionColumns(c, "internal_note"); !errors.Is(err, ErrValidation) {
		t.Fatalf("hidden projection err = %v, want ErrValidation", err)
	}
	if _, err := orderByClause(c, "-created,title"); err != nil {
		t.Fatal(err)
	}
	if _, err := orderByClause(c, "bad"); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad sort err = %v", err)
	}
}

func TestDirectusJSONFilterCompilation(t *testing.T) {
	c := testRecordCollection()
	expr, err := CompileRecordListFilter(`{"_or":[{"title":{"_icontains":"hello"}},{"score":{"_gte":7}}],"status":{"_in":["draft","live"]}}`, "", c)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL := `((((lower(coalesce("title"::text, '')) like $1)) or (("score" >= $2))) and ("status" in ($3, $4)))`
	if expr.SQL != wantSQL {
		t.Fatalf("SQL = %q, want %q", expr.SQL, wantSQL)
	}
	if len(expr.Args) != 4 || expr.Args[0] != "%hello%" || expr.Args[1] != int64(7) || expr.Args[2] != "draft" || expr.Args[3] != "live" {
		t.Fatalf("args = %#v", expr.Args)
	}
}

func TestRecordSearchUsesOnlySearchableFields(t *testing.T) {
	c := testRecordCollection()
	c.Fields[0].Searchable = true
	c.Fields[1].Searchable = false
	c.Fields[5].Searchable = true
	expr, err := CompileRecordListFilter("", "Hello", c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(expr.SQL, "summary") || !strings.Contains(expr.SQL, `"title"::text`) {
		t.Fatalf("search SQL should use only searchable fields, got %q", expr.SQL)
	}
	if strings.Contains(expr.SQL, `"score"`) {
		t.Fatalf("non-numeric search should not include numeric field: %q", expr.SQL)
	}

	expr, err = CompileRecordListFilter("", "7", c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(expr.SQL, `"score" =`) {
		t.Fatalf("numeric search should include searchable number field: %q", expr.SQL)
	}
}

func TestNormalizeListOptionsSupportsAllowedPageSizesAndOffset(t *testing.T) {
	opts := normalizeListOptions(RecordListOptions{Page: 2, PerPage: 250})
	if opts.PerPage != 250 || opts.Offset != 250 {
		t.Fatalf("page size normalization = %+v", opts)
	}
	opts = normalizeListOptions(RecordListOptions{PerPage: 999, Offset: 1000})
	if opts.PerPage != 500 || opts.Page != 3 || opts.Offset != 1000 {
		t.Fatalf("offset normalization = %+v", opts)
	}
}

func TestNormalizeDBValueFormatsUUIDArrays(t *testing.T) {
	first := [16]byte{0x9c, 0x10, 0xd5, 0xb9, 0x3a, 0x23, 0x4f, 0x25, 0x91, 0xc3, 0x09, 0xa4, 0x0d, 0x7e, 0x9f, 0x7e}
	second := [16]byte{0x56, 0xf3, 0xa6, 0x2c, 0x9a, 0x5d, 0x46, 0x47, 0xbd, 0xea, 0x5f, 0x0f, 0xff, 0x2e, 0xcd, 0xd3}

	gotBytes, ok := normalizeDBValue([][16]byte{first, second}).([]string)
	if !ok {
		t.Fatalf("[][16]byte normalized to %T, want []string", gotBytes)
	}
	if gotBytes[0] != "9c10d5b9-3a23-4f25-91c3-09a40d7e9f7e" || gotBytes[1] != "56f3a62c-9a5d-4647-bdea-5f0fff2ecdd3" {
		t.Fatalf("unexpected UUID array formatting: %v", gotBytes)
	}

	gotPgtype, ok := normalizeDBValue([]pgtype.UUID{{Bytes: first, Valid: true}, {Bytes: second, Valid: true}}).([]any)
	if !ok {
		t.Fatalf("[]pgtype.UUID normalized to %T, want []any", gotPgtype)
	}
	if gotPgtype[0] != "9c10d5b9-3a23-4f25-91c3-09a40d7e9f7e" || gotPgtype[1] != "56f3a62c-9a5d-4647-bdea-5f0fff2ecdd3" {
		t.Fatalf("unexpected pgtype UUID array formatting: %v", gotPgtype)
	}
}
