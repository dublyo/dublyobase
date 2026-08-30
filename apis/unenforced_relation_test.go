package apis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/dublyo/dublyobase/core"
)

// An unenforced relation keeps the column and everything built on it — expand,
// relation filters, sorting — but creates no foreign key. It exists for
// importing a schema whose references the original database never checked.
func TestUnenforcedRelation(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	ctx := context.Background()
	schema, _ := core.ProjectNames(slug)

	mk := func(body string) *http.Response { //nolint:unparam
		rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create collection: %d %s", rec.Code, rec.Body.String())
		}
		return nil
	}
	mk(`{"name":"customers","type":"base","fields":[{"name":"email","type":"text"}]}`)
	mk(`{"name":"devices","type":"base","fields":[
		{"name":"os","type":"text"},
		{"name":"customer","type":"relation","options":{"collection":"customers","enforced":false}}]}`)
	mk(`{"name":"orders","type":"base","fields":[
		{"name":"ref","type":"text"},
		{"name":"customer","type":"relation","options":{"collection":"customers"}}]}`)

	fkCount := func(table string) int {
		var n int
		if err := app.Pool.QueryRow(ctx, `
			select count(*) from pg_constraint con
			join pg_class cls on cls.oid = con.conrelid
			join pg_namespace ns on ns.oid = cls.relnamespace
			where ns.nspname = $1 and cls.relname = $2 and con.contype = 'f'`,
			schema, table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := fkCount("devices"); got != 0 {
		t.Errorf("unenforced relation created %d foreign keys, want 0", got)
	}
	if got := fkCount("orders"); got != 1 {
		t.Errorf("ordinary relation created %d foreign keys, want 1", got)
	}

	newRec := func(coll, body string) string {
		rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/%s/records", slug, coll), token, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", coll, rec.Code, rec.Body.String())
		}
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return fmt.Sprint(out["id"])
	}
	cust := newRec("customers", `{"email":"a@b.test"}`)
	newRec("devices", fmt.Sprintf(`{"os":"ios","customer":%q}`, cust))

	// expand and relation filters still work off the column
	rec := getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/devices/records?expand=customer", slug), token)
	if rec.Code != http.StatusOK || !contains(rec.Body.String(), "a@b.test") {
		t.Errorf("expand on an unenforced relation: %d %s", rec.Code, rec.Body.String())
	}
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/devices/records?filter=%%7B%%22customer.email%%22%%3A%%7B%%22_eq%%22%%3A%%22a%%40b.test%%22%%7D%%7D", slug), token)
	if rec.Code != http.StatusOK {
		t.Errorf("relation filter on an unenforced relation: %d %s", rec.Code, rec.Body.String())
	}

	// deleting the target is allowed and leaves the reference dangling, which
	// is the behaviour being reproduced
	if rec := deleteJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/customers/records/%s", slug, cust), token, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete customer: %d %s", rec.Code, rec.Body.String())
	}
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/devices/records", slug), token)
	if !contains(rec.Body.String(), cust) {
		t.Errorf("unenforced reference should survive the delete: %s", rec.Body.String())
	}

	// a typo in the target is still caught
	badRec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"bad","type":"base","fields":[
			{"name":"x","type":"relation","options":{"collection":"nope","enforced":false}}]}`)
	if badRec.Code != http.StatusUnprocessableEntity {
		t.Errorf("typo in an unenforced target: got %d, want 422", badRec.Code)
	}

	// onDelete has nothing to act on without a foreign key
	badRec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"bad2","type":"base","fields":[
			{"name":"x","type":"relation","options":{"collection":"customers","enforced":false,"onDelete":"cascade"}}]}`)
	if badRec.Code != http.StatusUnprocessableEntity {
		t.Errorf("unenforced plus onDelete: got %d (%s), want 422", badRec.Code, badRec.Body.String())
	}
}
