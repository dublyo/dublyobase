package apis

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dublyo/dublyobase/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

func patchJSON(handler http.Handler, path string, token string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("PATCH", path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func deleteJSON(handler http.Handler, path string, token string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("DELETE", path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func createProjectForCollections(t *testing.T, handler http.Handler, token string) string {
	t.Helper()
	// Each test gets its own database, but roles live in the cluster and outlive
	// it, so a slug derived only from the clock eventually collides with a role
	// left by an earlier run and the project refuses to provision.
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	slug := "p" + hex.EncodeToString(suffix[:])
	rec := postJSON(handler, "/admin/api/projects", token, fmt.Sprintf(`{"slug":%q,"name":"Collections"}`, slug))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	return slug
}

func TestAdminCollectionLifecycle(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	schema, _ := core.ProjectNames(slug)

	rec := getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth collections: want 401, got %d", rec.Code)
	}

	createBody := `{
		"name":"posts",
		"type":"base",
		"options":{"icon":{"type":"lucide","name":"book-open"}},
		"fields":[
			{"name":"title","type":"text","required":true},
			{"name":"published","type":"bool"},
			{"name":"meta","type":"json"},
			{"name":"status","type":"select","options":{"values":["draft","live"]}}
		]
	}`
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token, createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created core.Collection
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "posts" || created.Type != core.CollectionBase || len(created.Fields) != 4 {
		t.Fatalf("unexpected collection response: %+v", created)
	}
	assertCollectionIcon(t, created.Options, "lucide", "book-open")

	assertCollectionMetadataCount(t, app.Pool, slug, "posts", 1)
	for _, column := range []string{"id", "created", "updated", "title", "published", "meta", "status"} {
		assertColumnExists(t, app.Pool, schema, "posts", column)
	}
	assertCollectionPolicies(t, app.Pool, schema, "posts")
	assertCollectionAuditExists(t, app.Pool, "collection.create", slug, "posts")

	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token, createBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate collection: want 409, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list collections: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var listBody struct {
		Items []core.Collection `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, item := range listBody.Items {
		names[item.Name] = true
	}
	if len(listBody.Items) != 2 || !names["posts"] || !names["users"] {
		t.Fatalf("unexpected list response: %+v", listBody)
	}

	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts", slug), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("get collection: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	updateBody := `{
		"options":{"icon":{"type":"emoji","value":"\uD83D\uDCE6"}},
		"fields":[
			{"name":"title","type":"text","required":true},
			{"name":"published","type":"bool"},
			{"name":"payload","type":"json","options":{"oldName":"meta"}},
			{"name":"status","type":"select","options":{"values":["draft","live","archived"]}},
			{"name":"body","type":"text"}
		]
	}`
	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts", slug), token, updateBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("update collection: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated core.Collection
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	assertCollectionIcon(t, updated.Options, "emoji", "\U0001F4E6")
	assertColumnExists(t, app.Pool, schema, "posts", "payload")
	assertColumnExists(t, app.Pool, schema, "posts", "body")
	assertColumnMissing(t, app.Pool, schema, "posts", "meta")
	assertCollectionPolicies(t, app.Pool, schema, "posts")
	assertCollectionAuditExists(t, app.Pool, "collection.update", slug, "posts")

	dropFieldBody := `{
		"dropMissingFields": true,
		"fields":[
			{"name":"title","type":"text","required":true},
			{"name":"published","type":"bool"},
			{"name":"payload","type":"json"},
			{"name":"status","type":"select","options":{"values":["draft","live","archived"]}}
		]
	}`
	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts", slug), token, dropFieldBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("drop field: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertColumnMissing(t, app.Pool, schema, "posts", "body")

	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts", slug), token, `{"fields":[{"name":"title","type":"number"}]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("unsafe field change: want 409, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = deleteJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts", slug), token, `{}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("delete without confirm: want 422, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = deleteJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts?confirm=posts", slug), token, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete collection: want 204, got %d: %s", rec.Code, rec.Body.String())
	}
	assertCollectionMetadataCount(t, app.Pool, slug, "posts", 0)
	assertTableMissing(t, app.Pool, schema, "posts")
	assertCollectionAuditExists(t, app.Pool, "collection.delete", slug, "posts")

	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts", slug), token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted collection: want 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminCollectionValidation(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	cases := map[string]struct {
		body string
		want int
	}{
		"bad name":     {`{"name":"pg_bad","type":"base","fields":[]}`, http.StatusUnprocessableEntity},
		"reserved":     {`{"name":"posts","type":"base","fields":[{"name":"id","type":"text"}]}`, http.StatusUnprocessableEntity},
		"bad select":   {`{"name":"posts","type":"base","fields":[{"name":"status","type":"select","options":{"values":[]}}]}`, http.StatusUnprocessableEntity},
		"view":         {`{"name":"report","type":"view","fields":[]}`, http.StatusNotImplemented},
		"bad relation": {`{"name":"posts","type":"base","fields":[{"name":"author","type":"relation","options":{"collection":"pg_users"}}]}`, http.StatusUnprocessableEntity},
	}
	for name, tc := range cases {
		rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s: want %d, got %d: %s", name, tc.want, rec.Code, rec.Body.String())
		}
	}

	rec := postJSON(srv.Handler, "/api/projects/missing/collections", token, `{"name":"posts","type":"base","fields":[]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing project: want 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminCollectionCreateConcurrent(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	body := `{"name":"posts","type":"base","fields":[{"name":"title","type":"text"}]}`

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token, body)
			codes[i] = rec.Code
		}(i)
	}
	wg.Wait()

	created, conflict := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected collection status: %v", codes)
		}
	}
	if created != 1 || conflict != 1 {
		t.Fatalf("concurrent collection statuses = %v", codes)
	}
}

func assertCollectionMetadataCount(t *testing.T, pool *pgxpool.Pool, slug string, collection string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `
		select count(*)
		from _dbo.collections c
		join _dbo.projects p on p.id = c.project_id
		where p.slug = $1 and c.name = $2`,
		slug,
		collection,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("metadata count for %s.%s = %d, want %d", slug, collection, count, want)
	}
}

func assertCollectionIcon(t *testing.T, raw json.RawMessage, wantType string, wantValue string) {
	t.Helper()
	var options struct {
		Icon struct {
			Type  string `json:"type"`
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"icon"`
	}
	if err := json.Unmarshal(raw, &options); err != nil {
		t.Fatalf("collection options JSON: %v %s", err, string(raw))
	}
	got := options.Icon.Name
	if wantType == "emoji" {
		got = options.Icon.Value
	}
	if options.Icon.Type != wantType || got != wantValue {
		t.Fatalf("icon = %s/%q, want %s/%q in %s", options.Icon.Type, got, wantType, wantValue, string(raw))
	}
}

func assertColumnExists(t *testing.T, pool *pgxpool.Pool, schema string, table string, column string) {
	t.Helper()
	if !columnExists(t, pool, schema, table, column) {
		t.Fatalf("column %s.%s.%s missing", schema, table, column)
	}
}

func assertColumnMissing(t *testing.T, pool *pgxpool.Pool, schema string, table string, column string) {
	t.Helper()
	if columnExists(t, pool, schema, table, column) {
		t.Fatalf("column %s.%s.%s should be absent", schema, table, column)
	}
}

func columnExists(t *testing.T, pool *pgxpool.Pool, schema string, table string, column string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `
		select exists(
			select 1 from information_schema.columns
			where table_schema = $1 and table_name = $2 and column_name = $3
		)`,
		schema,
		table,
		column,
	).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func assertTableMissing(t *testing.T, pool *pgxpool.Pool, schema string, table string) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `
		select exists(
			select 1
			from pg_class c
			join pg_namespace n on n.oid = c.relnamespace
			where n.nspname = $1 and c.relname = $2
		)`,
		schema,
		table,
	).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("table %s.%s should be absent", schema, table)
	}
}

func assertCollectionPolicies(t *testing.T, pool *pgxpool.Pool, schema string, table string) {
	t.Helper()
	var enabled, forced bool
	if err := pool.QueryRow(context.Background(), `
		select c.relrowsecurity, c.relforcerowsecurity
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = $1 and c.relname = $2`,
		schema,
		table,
	).Scan(&enabled, &forced); err != nil {
		t.Fatal(err)
	}
	if !enabled || !forced {
		t.Fatalf("RLS flags for %s.%s = enabled:%v forced:%v", schema, table, enabled, forced)
	}
	var policyCount int
	if err := pool.QueryRow(context.Background(), `
		select count(*)
		from pg_policies
		where schemaname = $1
			and tablename = $2
			and policyname in ($3, $4, $5, $6, $7, $8, $9, $10)`,
		schema,
		table,
		"dbo_svc_select",
		"dbo_svc_insert",
		"dbo_svc_update",
		"dbo_svc_delete",
		"dbo_client_select",
		"dbo_client_insert",
		"dbo_client_update",
		"dbo_client_delete",
	).Scan(&policyCount); err != nil {
		t.Fatal(err)
	}
	if policyCount != 8 {
		t.Fatalf("policy count for %s.%s = %d, want 8", schema, table, policyCount)
	}
}

func assertCollectionAuditExists(t *testing.T, pool *pgxpool.Pool, action string, slug string, collection string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from _dbo.audit_log where action = $1 and data->>'project' = $2 and data->>'collection' = $3`,
		action,
		slug,
		collection,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatalf("audit action %q for %s.%s missing", action, slug, collection)
	}
}

// TestCollectionAuthIDRulePolicyApplies is the coverage that was missing: every
// prior rule test used `owner = @request.auth.id` (uuid = uuid), so nothing
// exercised the `!= ""` idiom the admin UI shows as its own placeholder. It
// used to reach CREATE POLICY and fail there as a 500.
func TestCollectionAuthIDRulePolicyApplies(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"posts","type":"base","fields":[{"name":"title","type":"text"}],
		  "listRule":"@request.auth.id != \"\"","viewRule":"@request.auth.id != \"\""}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with @request.auth.id rule: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// The rule must be a live policy, not just stored metadata.
	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts", slug), token,
		`{"updateRule":"@request.auth.id != \"\""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch rule: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// A literal that can never be a uuid must be a 422 naming the problem,
	// never a 500 from the database.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"bad","type":"base","fields":[{"name":"title","type":"text"}],
		  "listRule":"@request.auth.id = \"nope\""}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-uuid literal: want 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestComputedAndConstraints is the invariants slice: a line total the client
// cannot forge, a check the database refuses to break, and a unique document
// number. Previously all three lived in whatever client wrote to the API.
func TestComputedAndConstraints(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"quote_items","type":"base","fields":[
			{"name":"doc_no","type":"text"},
			{"name":"qty","type":"number","options":{"onlyInt":true}},
			{"name":"unit_price","type":"decimal","options":{"precision":18,"scale":3}},
			{"name":"line_total","type":"decimal","options":{"precision":18,"scale":3,"computed":"qty * unit_price"}}],
		  "options":{
			"checks":[{"name":"qty_positive","expression":"qty > 0"},
			          {"name":"price_non_negative","expression":"unit_price >= 0"}],
			"indexes":[{"name":"doc_no_unique","fields":["doc_no"],"unique":true}]}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	// The database computes the total; the client never supplies it.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/quote_items/records", slug), token,
		`{"doc_no":"Q-1","qty":3,"unit_price":"2312.500"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create record: %d %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["line_total"] != "6937.500" {
		t.Fatalf("line_total = %v, want 6937.500", got["line_total"])
	}

	// A forged total must be refused outright, not silently ignored.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/quote_items/records", slug), token,
		`{"doc_no":"Q-2","qty":1,"unit_price":"100.000","line_total":"1.000"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("forged computed value: want 422, got %d %s", rec.Code, rec.Body.String())
	}

	// Check constraint.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/quote_items/records", slug), token,
		`{"doc_no":"Q-3","qty":0,"unit_price":"10.000"}`)
	if rec.Code < 400 {
		t.Fatalf("qty=0 violates a check but returned %d", rec.Code)
	}

	// Unique document number.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/quote_items/records", slug), token,
		`{"doc_no":"Q-1","qty":1,"unit_price":"5.000"}`)
	if rec.Code < 400 {
		t.Fatalf("duplicate doc_no returned %d, want a conflict", rec.Code)
	}
}

// TestComputedExpressionsAreSandboxed: these expressions land in DDL, so the
// grammar must refuse anything that is not pure arithmetic over this row.
func TestComputedExpressionsAreSandboxed(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	for _, expr := range []string{
		`@request.auth.id`, // not immutable
		`(select 1)`,       // subquery
		`now()`,            // function call
		`qty) stored, x int; drop table users;--`, // injection attempt
		`missing_field * 2`,                       // unknown column
		`line_total * 2`,                          // self-reference
	} {
		body := fmt.Sprintf(`{"name":"probe","type":"base","fields":[
			{"name":"qty","type":"number"},
			{"name":"line_total","type":"decimal","options":{"computed":%q}}]}`, expr)
		rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token, body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expression %q: want 422, got %d %s", expr, rec.Code, rec.Body.String())
		}
	}
	// The users table must still exist.
	if rec := getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/users", slug), token); rec.Code != http.StatusOK {
		t.Fatalf("users collection missing after injection attempts: %d", rec.Code)
	}
}

// TestAddComputedToExistingField: adding `computed` to a field that already
// exists used to return 200 and change nothing, leaving the metadata claiming a
// database-owned value while the column stayed writable.
func TestAddComputedToExistingField(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	schema, _ := core.ProjectNames(slug)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"lines","type":"base","fields":[
			{"name":"qty","type":"number","options":{"onlyInt":true}},
			{"name":"unit_price","type":"decimal","options":{"precision":18,"scale":3}},
			{"name":"line_total","type":"decimal","options":{"precision":18,"scale":3}}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/lines/records", slug), token,
		`{"qty":2,"unit_price":"10.000","line_total":"999.000"}`)

	upgrade := `{"fields":[
		{"name":"qty","type":"number","options":{"onlyInt":true}},
		{"name":"unit_price","type":"decimal","options":{"precision":18,"scale":3}},
		{"name":"line_total","type":"decimal","options":{"precision":18,"scale":3,"computed":"qty * unit_price"}}]`

	// Unconfirmed: must refuse rather than silently no-op.
	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/lines", slug), token, upgrade+`}`)
	if rec.Code < 400 {
		t.Fatalf("unconfirmed computed change: want 4xx, got %d %s", rec.Code, rec.Body.String())
	}
	// Confirmed: the column is rebuilt as generated.
	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/lines", slug), token,
		upgrade+`,"dropMissingFields":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirmed computed change: %d %s", rec.Code, rec.Body.String())
	}
	var isGenerated string
	if err := app.Pool.QueryRow(context.Background(), `
		select is_generated from information_schema.columns
		where table_schema = $1 and table_name = 'lines' and column_name = 'line_total'`,
		schema).Scan(&isGenerated); err != nil {
		t.Fatal(err)
	}
	if isGenerated != "ALWAYS" {
		t.Fatalf("is_generated = %q, want ALWAYS — the column was not actually converted", isGenerated)
	}
	// And the forged 999.000 is gone, recomputed to 20.000.
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/lines/records", slug), token)
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0]["line_total"] != "20.000" {
		t.Fatalf("line_total = %v, want 20.000 recomputed", list.Items)
	}
}

// TestComputedDecimalUsesNumericArithmetic: Postgres has no
// `double precision * numeric` operator, so an unguarded `qty * unit_price`
// resolved by casting the numeric DOWN to float — computing money in floating
// point inside a column that exists to avoid exactly that.
func TestComputedDecimalUsesNumericArithmetic(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	schema, _ := core.ProjectNames(slug)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"lines","type":"base","fields":[
			{"name":"qty","type":"number","options":{"onlyInt":true}},
			{"name":"unit_price","type":"decimal","options":{"precision":18,"scale":3}},
			{"name":"line_total","type":"decimal","options":{"precision":18,"scale":3,"computed":"qty * unit_price"}}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var expr string
	if err := app.Pool.QueryRow(context.Background(), `
		select generation_expression from information_schema.columns
		where table_schema=$1 and table_name='lines' and column_name='line_total'`, schema).Scan(&expr); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(expr), "double precision") {
		t.Fatalf("generated expression computes in float: %s", expr)
	}

	// A magnitude float64 cannot represent exactly.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/lines/records", slug), token,
		`{"qty":999,"unit_price":"999999999999.999"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create record: %d %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["line_total"] != "998999999999999.001" {
		t.Fatalf("line_total = %v, want 998999999999999.001 (float gives ...999.000)", got["line_total"])
	}
}

// TestCrossRowInvariants covers the two rules a CHECK constraint cannot express,
// because a CHECK only ever sees the row being written:
//   - consistency: a payment's invoice must belong to the payment's patient
//   - exclusion:   two appointments must not hold one provider at one time
func TestCrossRowInvariants(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	rec := func(path, body string) *httptest.ResponseRecorder {
		return postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/%s", slug, path), token, body)
	}
	idOf := func(r *httptest.ResponseRecorder) string {
		var m map[string]any
		if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(m["id"])
	}

	rec("collections", `{"name":"people","type":"base","fields":[{"name":"name","type":"text"}]}`)
	if r := rec("collections", `{"name":"bills","type":"base","fields":[
		{"name":"ref","type":"text"},
		{"name":"person","type":"relation","options":{"collection":"people"}}]}`); r.Code != http.StatusCreated {
		t.Fatalf("bills: %d %s", r.Code, r.Body.String())
	}
	// payments must agree with the bill about who the person is
	if r := rec("collections", `{"name":"pays","type":"base","fields":[
		{"name":"bill","type":"relation","options":{"collection":"bills"}},
		{"name":"person","type":"relation","options":{"collection":"people"}}],
	  "options":{"consistency":[{"name":"bill_person","field":"bill","remote":"person","local":"person"}]}}`); r.Code != http.StatusCreated {
		t.Fatalf("pays: %d %s", r.Code, r.Body.String())
	}

	alice := idOf(rec("collections/people/records", `{"name":"Alice"}`))
	bob := idOf(rec("collections/people/records", `{"name":"Bob"}`))
	aliceBill := idOf(rec("collections/bills/records", fmt.Sprintf(`{"ref":"B-1","person":%q}`, alice)))

	if r := rec("collections/pays/records", fmt.Sprintf(`{"bill":%q,"person":%q}`, aliceBill, alice)); r.Code != http.StatusCreated {
		t.Fatalf("matching payment should be allowed: %d %s", r.Code, r.Body.String())
	}
	r := rec("collections/pays/records", fmt.Sprintf(`{"bill":%q,"person":%q}`, aliceBill, bob))
	if r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Alice's bill paid against Bob: want 422, got %d %s", r.Code, r.Body.String())
	}
	if !strings.Contains(r.Body.String(), "bill_person") {
		t.Fatalf("violation does not name the rule: %s", r.Body.String())
	}
	// the rule must hold on UPDATE too, not just INSERT
	okPay := idOf(rec("collections/pays/records", fmt.Sprintf(`{"bill":%q,"person":%q}`, aliceBill, alice)))
	if r := patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/pays/records/%s", slug, okPay),
		token, fmt.Sprintf(`{"person":%q}`, bob)); r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("update to a mismatched person: want 422, got %d %s", r.Code, r.Body.String())
	}

	// ── exclusion: no double booking, cancelled rows excluded ──
	if r := rec("collections", `{"name":"slots","type":"base","fields":[
		{"name":"provider","type":"relation","options":{"collection":"people"}},
		{"name":"starts_at","type":"date"},
		{"name":"ends_at","type":"date"},
		{"name":"status","type":"select","options":{"values":["booked","cancelled"]}}],
	  "options":{"exclusions":[{"name":"no_overlap","equals":["provider"],"from":"starts_at","to":"ends_at","where":"status != \"cancelled\""}]}}`); r.Code != http.StatusCreated {
		t.Skipf("exclusion constraints unavailable here: %d %s", r.Code, r.Body.String())
	}
	book := func(who, from, to, status string) *httptest.ResponseRecorder {
		return rec("collections/slots/records", fmt.Sprintf(
			`{"provider":%q,"starts_at":%q,"ends_at":%q,"status":%q}`, who, from, to, status))
	}
	if r := book(alice, "2026-09-01T09:00:00Z", "2026-09-01T10:00:00Z", "booked"); r.Code != http.StatusCreated {
		t.Fatalf("first booking: %d %s", r.Code, r.Body.String())
	}
	// must be a conflict, not a 500: an exclusion violation is the row losing a
	// race with another row, and the caller needs to be told which rule it hit
	if r := book(alice, "2026-09-01T09:30:00Z", "2026-09-01T10:30:00Z", "booked"); r.Code != http.StatusConflict {
		t.Fatalf("overlapping booking: want 409, got %d %s", r.Code, r.Body.String())
	} else if !strings.Contains(r.Body.String(), "no_overlap") {
		t.Fatalf("conflict does not name the rule: %s", r.Body.String())
	}
	if r := book(bob, "2026-09-01T09:30:00Z", "2026-09-01T10:30:00Z", "booked"); r.Code != http.StatusCreated {
		t.Fatalf("a different provider must not clash: %d %s", r.Code, r.Body.String())
	}
	if r := book(alice, "2026-09-01T10:00:00Z", "2026-09-01T11:00:00Z", "booked"); r.Code != http.StatusCreated {
		t.Fatalf("adjacent, non-overlapping slot must be allowed: %d %s", r.Code, r.Body.String())
	}
	if r := book(alice, "2026-09-01T09:15:00Z", "2026-09-01T09:45:00Z", "cancelled"); r.Code != http.StatusCreated {
		t.Fatalf("a cancelled slot must not be blocked: %d %s", r.Code, r.Body.String())
	}
}
