package apis

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// A console that answers a typo with "internal server error" gives the operator
// no way to tell a mistake in their own query from a fault in the server.
func TestSQLConsoleReportsQueryErrors(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	path := fmt.Sprintf("/admin/api/projects/%s/sql", slug)

	cases := []struct {
		name     string
		query    string
		contains string
	}{
		{"unknown table", "select * from does_not_exist", "does_not_exist"},
		{"syntax error", "select * frm anything", "syntax"},
		{"unknown column", "select nope from information_schema.tables", "nope"},
	}
	for _, tc := range cases {
		rec := postJSON(srv.Handler, path, token, fmt.Sprintf(`{"query":%q}`, tc.query))
		if rec.Code >= 500 {
			t.Errorf("%s: got %d (%s), want a 4xx naming the problem", tc.name, rec.Code, rec.Body.String())
			continue
		}
		if rec.Code < 400 {
			t.Errorf("%s: got %d, want a rejection", tc.name, rec.Code)
			continue
		}
		if !strings.Contains(strings.ToLower(rec.Body.String()), tc.contains) {
			t.Errorf("%s: message %q does not mention %q", tc.name, rec.Body.String(), tc.contains)
		}
	}

	// A working query must still work.
	rec := postJSON(srv.Handler, path, token, `{"query":"select 1 as n"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid query: got %d (%s)", rec.Code, rec.Body.String())
	}
}
