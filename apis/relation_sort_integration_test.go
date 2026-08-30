package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// Sorting by a field on a related row, e.g. products by their category name.
// Filters, expand and export could already walk relations; sort could not.
func TestSortThroughRelations(t *testing.T) {
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
	mk(`{"name":"brands","type":"base","fields":[{"name":"name","type":"text"}]}`)
	mk(`{"name":"cats","type":"base","fields":[
		{"name":"name","type":"text"},
		{"name":"brand","type":"relation","options":{"collection":"brands"}}]}`)
	mk(`{"name":"products","type":"base","fields":[
		{"name":"title","type":"text"},
		{"name":"cat","type":"relation","options":{"collection":"cats"}}]}`)

	newRec := func(coll, body string) string {
		rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/%s/records", slug, coll), token, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", coll, rec.Code, rec.Body.String())
		}
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return fmt.Sprint(out["id"])
	}
	zulu := newRec("brands", `{"name":"Zulu"}`)
	alpha := newRec("brands", `{"name":"Alpha"}`)
	shirts := newRec("cats", fmt.Sprintf(`{"name":"Shirts","brand":%q}`, zulu))
	bags := newRec("cats", fmt.Sprintf(`{"name":"Bags","brand":%q}`, alpha))
	newRec("products", fmt.Sprintf(`{"title":"tee","cat":%q}`, shirts))
	newRec("products", fmt.Sprintf(`{"title":"tote","cat":%q}`, bags))
	newRec("products", `{"title":"orphan"}`)

	order := func(sort string) []string {
		path := fmt.Sprintf("/api/projects/%s/collections/products/records?perPage=50&sort=%s", slug, url.QueryEscape(sort))
		rec := getJSON(srv.Handler, path, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("sort %s: %d %s", sort, rec.Code, rec.Body.String())
		}
		var out struct {
			Items []map[string]any `json:"items"`
		}
		json.Unmarshal(rec.Body.Bytes(), &out)
		got := make([]string, 0, len(out.Items))
		for _, item := range out.Items {
			got = append(got, fmt.Sprint(item["title"]))
		}
		return got
	}

	// one hop: Bags before Shirts
	// Ascending puts nulls last, so the categorised rows lead.
	if got := order("cat.name"); got[0] != "tote" || got[1] != "tee" || got[2] != "orphan" {
		t.Errorf("sort by cat.name: got %v, want tote, tee, orphan", got)
	}
	// Descending puts nulls first, which is Postgres's default and what a plain
	// column sort already does — the row with no category leads, then Shirts,
	// then Bags. Kept rather than special-cased so both kinds of sort agree.
	if got := order("-cat.name"); got[0] != "orphan" || got[1] != "tee" || got[2] != "tote" {
		t.Errorf("descending: got %v, want orphan, tee, tote", got)
	}
	// two hops: Alpha before Zulu
	if got := order("cat.brand.name"); got[0] != "tote" || got[1] != "tee" {
		t.Errorf("sort by cat.brand.name: got %v, want tote then tee", got)
	}
	// a row with no related record must still appear
	if got := order("cat.name"); len(got) != 3 {
		t.Errorf("row with a null relation was dropped: %v", got)
	}
	// combining a relation sort with a local one
	if got := order("cat.name,title"); len(got) != 3 {
		t.Errorf("combined sort: %v", got)
	}

	for _, bad := range []string{"cat.nope", "title.name", "a.b.c.d.e.f"} {
		path := fmt.Sprintf("/api/projects/%s/collections/products/records?sort=%s", slug, url.QueryEscape(bad))
		rec := getJSON(srv.Handler, path, token)
		if rec.Code < 400 || rec.Code >= 500 {
			t.Errorf("sort %q: got %d (%s), want a 4xx", bad, rec.Code, rec.Body.String())
		}
	}
}
