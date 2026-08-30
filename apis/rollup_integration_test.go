package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// A rollup keeps a parent field equal to an aggregate of its children, so an
// order's line subtotal cannot drift from its lines and no caller has to
// remember to recompute it.
func TestRollupFieldsAreMaintained(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	mk := func(body string) {
		if rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token, body); rec.Code != http.StatusCreated {
			t.Fatalf("create collection: %d %s", rec.Code, rec.Body.String())
		}
	}
	mk(`{"name":"orders","type":"base","fields":[{"name":"ref","type":"text"}]}`)
	mk(`{"name":"lines","type":"base","fields":[
		{"name":"order","type":"relation","options":{"collection":"orders"}},
		{"name":"qty","type":"number","options":{"onlyInt":true}},
		{"name":"amount","type":"decimal","options":{"precision":12,"scale":4}}]}`)

	// add the rollups to the parent now that the child exists
	rec := patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/orders", slug), token, `{"fields":[
		{"name":"ref","type":"text"},
		{"name":"line_subtotal","type":"decimal","options":{"precision":12,"scale":4,
			"rollup":{"collection":"lines","field":"order","aggregate":"sum","source":"amount"}}},
		{"name":"line_count","type":"number","options":{"onlyInt":true,
			"rollup":{"collection":"lines","field":"order","aggregate":"count"}}}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("add rollups: %d %s", rec.Code, rec.Body.String())
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
	read := func(id string) (string, float64) {
		rec := getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/orders/records/%s", slug, id), token)
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		count, _ := out["line_count"].(float64)
		return fmt.Sprint(out["line_subtotal"]), count
	}

	o1 := newRec("orders", `{"ref":"A"}`)
	o2 := newRec("orders", `{"ref":"B"}`)

	if sum, n := read(o1); sum != "0.0000" || n != 0 {
		t.Errorf("empty order: subtotal=%s count=%v, want 0.0000 and 0", sum, n)
	}

	l1 := newRec("lines", fmt.Sprintf(`{"order":%q,"qty":1,"amount":"10.5000"}`, o1))
	if sum, n := read(o1); sum != "10.5000" || n != 1 {
		t.Errorf("after insert: subtotal=%s count=%v, want 10.5000 and 1", sum, n)
	}
	newRec("lines", fmt.Sprintf(`{"order":%q,"qty":2,"amount":"4.2500"}`, o1))
	if sum, n := read(o1); sum != "14.7500" || n != 2 {
		t.Errorf("after second insert: subtotal=%s count=%v, want 14.7500 and 2", sum, n)
	}

	// updating a child's amount moves the parent
	if rec := patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/lines/records/%s", slug, l1), token, `{"amount":"20.0000"}`); rec.Code != http.StatusOK {
		t.Fatalf("update line: %d %s", rec.Code, rec.Body.String())
	}
	if sum, _ := read(o1); sum != "24.2500" {
		t.Errorf("after child update: subtotal=%s, want 24.2500", sum)
	}

	// re-parenting must correct both sides
	if rec := patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/lines/records/%s", slug, l1), token, fmt.Sprintf(`{"order":%q}`, o2)); rec.Code != http.StatusOK {
		t.Fatalf("move line: %d %s", rec.Code, rec.Body.String())
	}
	if sum, n := read(o1); sum != "4.2500" || n != 1 {
		t.Errorf("old parent after move: subtotal=%s count=%v, want 4.2500 and 1", sum, n)
	}
	if sum, n := read(o2); sum != "20.0000" || n != 1 {
		t.Errorf("new parent after move: subtotal=%s count=%v, want 20.0000 and 1", sum, n)
	}

	// deleting a child brings it back down
	if rec := deleteJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/lines/records/%s", slug, l1), token, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete line: %d %s", rec.Code, rec.Body.String())
	}
	if sum, n := read(o2); sum != "0.0000" || n != 0 {
		t.Errorf("after delete: subtotal=%s count=%v, want 0.0000 and 0", sum, n)
	}

	// a client cannot write the value itself
	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/orders/records/%s", slug, o1), token, `{"line_subtotal":"999.0000"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("writing a rollup: got %d (%s), want 422", rec.Code, rec.Body.String())
	}
	if sum, _ := read(o1); sum != "4.2500" {
		t.Errorf("rollup changed after a rejected write: %s", sum)
	}
}

// Adding a rollup to a collection that already has rows must fill it in, not
// leave every existing parent reading zero until a child happens to be touched.
func TestRollupBackfillsExistingRows(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"carts","type":"base","fields":[{"name":"ref","type":"text"}]}`)
	postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"items","type":"base","fields":[
			{"name":"cart","type":"relation","options":{"collection":"carts"}},
			{"name":"amount","type":"decimal","options":{"precision":12,"scale":2}}]}`)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/carts/records", slug), token, `{"ref":"X"}`)
	var cart map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cart)
	id := fmt.Sprint(cart["id"])
	for _, amt := range []string{"3.00", "4.50"} {
		postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/items/records", slug), token,
			fmt.Sprintf(`{"cart":%q,"amount":%q}`, id, amt))
	}

	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/carts", slug), token, `{"fields":[
		{"name":"ref","type":"text"},
		{"name":"total","type":"decimal","options":{"precision":12,"scale":2,
			"rollup":{"collection":"items","field":"cart","aggregate":"sum","source":"amount"}}}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("add rollup: %d %s", rec.Code, rec.Body.String())
	}
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/carts/records/%s", slug, id), token)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	if fmt.Sprint(out["total"]) != "7.50" {
		t.Errorf("backfill: total=%v, want 7.50", out["total"])
	}
}
