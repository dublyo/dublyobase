package apis

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/dublyo/dublyobase/core"
)

// Text search compiles to a leading-wildcard LIKE, which no btree index can
// serve, so it read the whole table every time. A searchable text field now
// gets a GIN trigram index on the same expression the search uses.
func TestSearchableFieldsGetATrigramIndex(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"articles","type":"base","fields":[
			{"name":"title","type":"text","searchable":true},
			{"name":"body","type":"editor","searchable":true},
			{"name":"slug","type":"text"},
			{"name":"views","type":"number","searchable":true}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	ctx := context.Background()
	schema, _ := core.ProjectNames(slug)
	indexes := map[string]string{}
	rows, err := app.Pool.Query(ctx, `
		select indexname, indexdef from pg_indexes
		where schemaname = $1 and tablename = 'articles'`, schema)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			t.Fatal(err)
		}
		indexes[name] = def
	}
	rows.Close()

	// Searchable free-text fields are indexed.
	for _, field := range []string{"title", "body"} {
		name := "dbo_ix_articles_" + field + "_trgm"
		def, ok := indexes[name]
		if !ok {
			t.Errorf("no trigram index for searchable field %q (have %v)", field, keysOf(indexes))
			continue
		}
		if !containsAll(def, "gin", "gin_trgm_ops", "lower") {
			t.Errorf("%s is not the expected trigram index: %s", name, def)
		}
	}
	// A field nobody searches is not indexed, and a number gains nothing from
	// trigrams even when it is searchable.
	for _, field := range []string{"slug", "views"} {
		if _, ok := indexes["dbo_ix_articles_"+field+"_trgm"]; ok {
			t.Errorf("unexpected trigram index on %q", field)
		}
	}

	// The search still returns what it did before.
	for _, body := range []string{
		`{"title":"Postgres full text","body":"<p>indexing notes</p>","views":1}`,
		`{"title":"Something else","body":"<p>unrelated</p>","views":2}`,
	} {
		if rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/articles/records", slug), token, body); rec.Code != http.StatusCreated {
			t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
		}
	}
	path := fmt.Sprintf("/api/projects/%s/collections/articles/records?search=%s", slug, url.QueryEscape("postgres"))
	rec = getJSON(srv.Handler, path, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	if !containsAll(rec.Body.String(), "Postgres full text") || containsAll(rec.Body.String(), "Something else") {
		t.Errorf("search returned the wrong rows: %s", rec.Body.String())
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func containsAll(haystack string, needles ...string) bool {
	for _, n := range needles {
		found := false
		for i := 0; i+len(n) <= len(haystack); i++ {
			if haystack[i:i+len(n)] == n {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
