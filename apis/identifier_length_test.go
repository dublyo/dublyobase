package apis

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dublyo/dublyobase/core"
)

// PostgreSQL truncates identifiers at 63 characters rather than rejecting them,
// so two generated names that differ only past the limit become one object. The
// sync drops by name before creating, so the second definition destroyed the
// first: a collection asking for a three-column unique key plus a second unique
// key kept only the second, and rows the first would have allowed were rejected
// by a constraint nobody asked for.
func TestLongGeneratedIndexNamesAreRefused(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	ctx := context.Background()
	schema, _ := core.ProjectNames(slug)

	const coll = "eav_entity_scoped_attribute_values_for_catalog_products"
	body := fmt.Sprintf(`{"name":%q,"type":"base","fields":[
		{"name":"entity_id","type":"text"},
		{"name":"attribute_id","type":"text"},
		{"name":"store_id","type":"text"}],
	  "options":{"indexes":[
		{"name":"unique_entity_attribute_store_scope_lookup","fields":["entity_id","attribute_id","store_id"],"unique":true},
		{"name":"unique_entity_attribute_store_scope_second","fields":["attribute_id"],"unique":true}]}}`, coll)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token, body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d (%s), want 422 rather than a silently merged index", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too long") {
		t.Errorf("message should explain the length: %s", rec.Body.String())
	}

	// Nothing was created, so no half-applied schema is left behind.
	var n int
	if err := app.Pool.QueryRow(ctx,
		`select count(*) from pg_tables where schemaname = $1 and tablename = $2`, schema, coll).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("a rejected collection left a table behind")
	}

	// Names that fit still work, and both indexes really exist.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"eav_values","type":"base","fields":[
			{"name":"entity_id","type":"text"},
			{"name":"attribute_id","type":"text"},
			{"name":"store_id","type":"text"}],
		  "options":{"indexes":[
			{"name":"entity_attribute_store","fields":["entity_id","attribute_id","store_id"],"unique":true},
			{"name":"attribute_lookup","fields":["attribute_id"]}]}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("short names: %d %s", rec.Code, rec.Body.String())
	}
	if err := app.Pool.QueryRow(ctx,
		`select count(*) from pg_indexes where schemaname = $1 and tablename = 'eav_values' and indexname like 'dbo_ix_%'`,
		schema).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("created %d indexes, want 2", n)
	}

	// The three-column unique is the one enforced: a second row differing in
	// entity and store is legitimate EAV data and must be allowed.
	mk := func(e, a, s string) int {
		return postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/eav_values/records", slug), token,
			fmt.Sprintf(`{"entity_id":%q,"attribute_id":%q,"store_id":%q}`, e, a, s)).Code
	}
	if code := mk("1", "73", "0"); code != http.StatusCreated {
		t.Fatalf("first row: %d", code)
	}
	if code := mk("2", "73", "1"); code != http.StatusCreated {
		t.Errorf("a different entity and store with the same attribute must be allowed, got %d", code)
	}
	if code := mk("1", "73", "0"); code != http.StatusConflict {
		t.Errorf("an exact duplicate must be rejected, got %d", code)
	}

	// A long check name is refused for the same reason.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		fmt.Sprintf(`{"name":%q,"type":"base","fields":[{"name":"n","type":"number"}],
		  "options":{"checks":[{"name":"a_very_long_check_name_that_will_not_fit_once_prefixed","expression":"n > 0"}]}}`,
			"another_quite_long_collection_name_for_checks"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("long check name: got %d (%s), want 422", rec.Code, rec.Body.String())
	}
}
