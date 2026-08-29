package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// Filtering through a relation, e.g. "conversation.workspace.plan", which
// previously required fetching every related id first.
func TestFilterThroughRelations(t *testing.T) {
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
	mk(`{"name":"orgs","type":"base","fields":[{"name":"plan","type":"text"}]}`)
	mk(`{"name":"teams","type":"base","fields":[
		{"name":"org","type":"relation","options":{"collection":"orgs"}},
		{"name":"name","type":"text"}]}`)
	mk(`{"name":"tickets","type":"base","fields":[
		{"name":"team","type":"relation","options":{"collection":"teams"}},
		{"name":"title","type":"text"},
		{"name":"score","type":"number","options":{"onlyInt":true}}]}`)

	newRec := func(coll, body string) string {
		rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/%s/records", slug, coll), token, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", coll, rec.Code, rec.Body.String())
		}
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return fmt.Sprint(out["id"])
	}
	free := newRec("orgs", `{"plan":"free"}`)
	paid := newRec("orgs", `{"plan":"enterprise"}`)
	teamA := newRec("teams", fmt.Sprintf(`{"org":%q,"name":"alpha"}`, free))
	teamB := newRec("teams", fmt.Sprintf(`{"org":%q,"name":"beta"}`, paid))
	newRec("tickets", fmt.Sprintf(`{"team":%q,"title":"free one","score":1}`, teamA))
	newRec("tickets", fmt.Sprintf(`{"team":%q,"title":"free two","score":5}`, teamA))
	newRec("tickets", fmt.Sprintf(`{"team":%q,"title":"paid one","score":9}`, teamB))

	list := func(filter string) []string {
		path := fmt.Sprintf("/api/projects/%s/collections/tickets/records?perPage=50&filter=%s", slug, url.QueryEscape(filter))
		rec := getJSON(srv.Handler, path, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("filter %s: %d %s", filter, rec.Code, rec.Body.String())
		}
		var out struct {
			Items []map[string]any `json:"items"`
		}
		json.Unmarshal(rec.Body.Bytes(), &out)
		titles := make([]string, 0, len(out.Items))
		for _, item := range out.Items {
			titles = append(titles, fmt.Sprint(item["title"]))
		}
		return titles
	}

	if got := list(`{"team.name":{"_eq":"alpha"}}`); len(got) != 2 {
		t.Errorf("one hop: got %v, want the two alpha tickets", got)
	}
	if got := list(`{"team.org.plan":{"_eq":"free"}}`); len(got) != 2 {
		t.Errorf("two hops: got %v, want the two free tickets", got)
	}
	if got := list(`{"team.org.plan":{"_eq":"enterprise"}}`); len(got) != 1 || got[0] != "paid one" {
		t.Errorf("two hops (enterprise): got %v", got)
	}
	// combined with a filter on the root collection
	if got := list(`{"team.org.plan":{"_eq":"free"},"score":{"_gt":3}}`); len(got) != 1 || got[0] != "free two" {
		t.Errorf("relation + local filter: got %v", got)
	}
	// operators other than equality still work at the leaf
	if got := list(`{"team.name":{"_icontains":"alph"}}`); len(got) != 2 {
		t.Errorf("_icontains through a relation: got %v", got)
	}
	// a relation that points nowhere matches nothing rather than erroring
	if got := list(`{"team.org.plan":{"_eq":"nonexistent"}}`); len(got) != 0 {
		t.Errorf("no match should be empty: got %v", got)
	}

	// Rejections
	for _, tc := range []struct{ name, filter string }{
		{"unknown leaf", `{"team.org.nope":{"_eq":"x"}}`},
		{"not a relation", `{"title.name":{"_eq":"x"}}`},
		{"too deep", `{"team.org.plan.a.b.c":{"_eq":"x"}}`},
		{"empty segment", `{"team..name":{"_eq":"x"}}`},
	} {
		path := fmt.Sprintf("/api/projects/%s/collections/tickets/records?filter=%s", slug, url.QueryEscape(tc.filter))
		rec := getJSON(srv.Handler, path, token)
		if rec.Code < 400 || rec.Code >= 500 {
			t.Errorf("%s: got %d (%s), want a 4xx", tc.name, rec.Code, rec.Body.String())
		}
	}
}
