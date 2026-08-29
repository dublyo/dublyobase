package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// A relation filter builds subqueries against related tables. Those run as the
// same database role as the outer query, so row-level security has to apply to
// them too — otherwise traversal would be a way to read, or infer, rows the
// caller cannot see directly.
func TestRelationFilterRespectsRowLevelSecurity(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	admin := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, admin)

	mk := func(body string) {
		if rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), admin, body); rec.Code != http.StatusCreated {
			t.Fatalf("create collection: %d %s", rec.Code, rec.Body.String())
		}
	}
	// notes are readable by everyone; secrets are readable by nobody.
	mk(`{"name":"secrets","type":"base","fields":[{"name":"codeword","type":"text"}],
	     "listRule":"1 = 0","viewRule":"1 = 0"}`)
	mk(`{"name":"notes","type":"base","fields":[
		{"name":"secret","type":"relation","options":{"collection":"secrets"}},
		{"name":"label","type":"text"}],
	     "listRule":"","viewRule":""}`)

	newRec := func(coll, body string) string {
		rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/%s/records", slug, coll), admin, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", coll, rec.Code, rec.Body.String())
		}
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return fmt.Sprint(out["id"])
	}
	s1 := newRec("secrets", `{"codeword":"orange"}`)
	s2 := newRec("secrets", `{"codeword":"violet"}`)
	newRec("notes", fmt.Sprintf(`{"secret":%q,"label":"first"}`, s1))
	newRec("notes", fmt.Sprintf(`{"secret":%q,"label":"second"}`, s2))

	anonList := func(filter string) (int, []string) {
		path := fmt.Sprintf("/api/projects/%s/collections/notes/records?perPage=50", slug)
		if filter != "" {
			path += "&filter=" + url.QueryEscape(filter)
		}
		rec := getJSON(srv.Handler, path, "")
		var out struct {
			Items []map[string]any `json:"items"`
		}
		json.Unmarshal(rec.Body.Bytes(), &out)
		labels := make([]string, 0, len(out.Items))
		for _, item := range out.Items {
			labels = append(labels, fmt.Sprint(item["label"]))
		}
		return rec.Code, labels
	}

	// Baseline: notes are public, secrets are not.
	code, all := anonList("")
	if code != http.StatusOK || len(all) != 2 {
		t.Fatalf("notes should be readable: %d %v", code, all)
	}
	rec := getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/secrets/records", slug), "")
	if body := rec.Body.String(); contains(body, "orange") || contains(body, "violet") {
		t.Fatalf("secrets should not be readable at all: %s", body)
	}

	// The filter must not become a read channel: a caller who cannot see any
	// secret must not be able to tell which note points at "orange".
	code, filtered := anonList(`{"secret.codeword":{"_eq":"orange"}}`)
	if code >= 500 {
		t.Fatalf("relation filter into a hidden table should not 500: %d", code)
	}
	if len(filtered) != 0 {
		t.Errorf("filtering on a hidden table leaked a row: %v", filtered)
	}
	// And the negative case must not invert into a full listing either.
	_, other := anonList(`{"secret.codeword":{"_neq":"orange"}}`)
	if len(other) != 0 {
		t.Errorf("negated filter on a hidden table leaked rows: %v", other)
	}

	// The admin, who can read secrets, still gets the right answer.
	path := fmt.Sprintf("/api/projects/%s/collections/notes/records?filter=%s", slug, url.QueryEscape(`{"secret.codeword":{"_eq":"orange"}}`))
	rec = getJSON(srv.Handler, path, admin)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin filter: %d %s", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), "first") || contains(rec.Body.String(), "second") {
		t.Errorf("admin should see exactly the first note: %s", rec.Body.String())
	}
}
