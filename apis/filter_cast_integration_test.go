package apis

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// Filter values come straight from user input — an unfilled search box bound to
// a filter sends "". Comparing that to a uuid, number or timestamp column is a
// cast Postgres refuses, and the failure used to surface as a 500.
func TestFilterCastFailuresAreValidationErrors(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"threads","type":"base","fields":[{"name":"title","type":"text"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("threads: %d %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"posts","type":"base","fields":[
			{"name":"thread","type":"relation","options":{"collection":"threads"}},
			{"name":"score","type":"number","options":{"onlyInt":true}},
			{"name":"posted_at","type":"date"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("posts: %d %s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		name   string
		filter string
	}{
		{"relation vs empty string", `{"thread":{"_eq":""}}`},
		{"relation vs non-uuid", `{"thread":{"_eq":"not-a-uuid"}}`},
		{"relation neq empty string", `{"thread":{"_neq":""}}`},
		{"number vs empty string", `{"score":{"_eq":""}}`},
		{"number vs word", `{"score":{"_gt":"lots"}}`},
		{"date vs empty string", `{"posted_at":{"_eq":""}}`},
		// note: Postgres accepts "yesterday" and "today" as real timestamp
		// literals, so an invalid date has to be genuinely malformed.
		{"date vs nonsense", `{"posted_at":{"_lt":"2026-13-45"}}`},
	}
	for _, tc := range cases {
		path := fmt.Sprintf("/api/projects/%s/collections/posts/records?filter=%s", slug, url.QueryEscape(tc.filter))
		rec := getJSON(srv.Handler, path, token)
		if rec.Code >= 500 {
			t.Errorf("%s: got %d (%s), want a 4xx", tc.name, rec.Code, rec.Body.String())
			continue
		}
		if rec.Code < 400 {
			t.Errorf("%s: got %d, want a rejection", tc.name, rec.Code)
		}
	}

	// A null comparison is meaningful and must keep working.
	path := fmt.Sprintf("/api/projects/%s/collections/posts/records?filter=%s", slug, url.QueryEscape(`{"thread":{"_eq":null}}`))
	if rec := getJSON(srv.Handler, path, token); rec.Code != http.StatusOK {
		t.Errorf("null relation filter: got %d (%s), want 200", rec.Code, rec.Body.String())
	}
}
