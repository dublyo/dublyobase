package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// A multi-value field is an array column. Filtering it with _eq used to reach
// Postgres as `uuid[] = 'some-uuid'`, which failed with "malformed array
// literal" — a database internal surfaced for a filter that reads fine.
func TestFilterMultiValueFields(t *testing.T) {
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
	mk(`{"name":"cats","type":"base","fields":[{"name":"name","type":"text"}]}`)
	mk(`{"name":"items","type":"base","fields":[
		{"name":"title","type":"text"},
		{"name":"cats","type":"relation","options":{"collection":"cats","multiple":true}},
		{"name":"labels","type":"select","options":{"values":["red","green","blue"],"multiple":true}}]}`)

	newRec := func(coll, body string) string {
		rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/%s/records", slug, coll), token, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", coll, rec.Code, rec.Body.String())
		}
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return fmt.Sprint(out["id"])
	}
	shirts := newRec("cats", `{"name":"shirts"}`)
	bags := newRec("cats", `{"name":"bags"}`)
	newRec("items", fmt.Sprintf(`{"title":"oxford","cats":[%q],"labels":["red","blue"]}`, shirts))
	newRec("items", fmt.Sprintf(`{"title":"tote","cats":[%q],"labels":["green"]}`, bags))
	newRec("items", fmt.Sprintf(`{"title":"combo","cats":[%q,%q],"labels":["blue"]}`, shirts, bags))

	titles := func(filter string) []string {
		path := fmt.Sprintf("/api/projects/%s/collections/items/records?perPage=50&filter=%s", slug, url.QueryEscape(filter))
		rec := getJSON(srv.Handler, path, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("filter %s: %d %s", filter, rec.Code, rec.Body.String())
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

	if got := titles(fmt.Sprintf(`{"cats":{"_eq":%q}}`, shirts)); len(got) != 2 {
		t.Errorf("_eq on a multi-relation: got %v, want oxford and combo", got)
	}
	if got := titles(fmt.Sprintf(`{"cats":{"_neq":%q}}`, shirts)); len(got) != 1 || got[0] != "tote" {
		t.Errorf("_neq on a multi-relation: got %v, want tote", got)
	}
	if got := titles(fmt.Sprintf(`{"cats":{"_in":[%q,%q]}}`, shirts, bags)); len(got) != 3 {
		t.Errorf("_in covering both: got %v, want all three", got)
	}
	if got := titles(fmt.Sprintf(`{"cats":{"_in":[%q]}}`, bags)); len(got) != 2 {
		t.Errorf("_in on one: got %v, want tote and combo", got)
	}
	if got := titles(`{"labels":{"_eq":"blue"}}`); len(got) != 2 {
		t.Errorf("_eq on a multi-select: got %v, want oxford and combo", got)
	}
	if got := titles(`{"labels":{"_in":["green"]}}`); len(got) != 1 || got[0] != "tote" {
		t.Errorf("_in on a multi-select: got %v, want tote", got)
	}
	// combining with an ordinary predicate still works
	if got := titles(fmt.Sprintf(`{"cats":{"_eq":%q},"title":{"_eq":"combo"}}`, shirts)); len(got) != 1 {
		t.Errorf("multi-value plus scalar: got %v", got)
	}
}
