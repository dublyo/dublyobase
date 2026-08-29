package apis

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/dublyo/dublyobase/core"
)

// A restored backup is missing exactly two things: the cluster's roles, which
// pg_dump never writes because they are not database objects, and the grants,
// which pg_restore --no-privileges strips. Every RLS policy names a role and
// calls a function in _dbo, so both losses leave the instance holding all its
// rows and serving none of them — while pg_restore exits 0.
//
// This reproduces that state and checks the instance repairs itself.
func TestReconcileRebuildsProjectSecurityAfterRestore(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"notes","type":"base","fields":[{"name":"body","type":"text"}],
		  "listRule":"","viewRule":"","createRule":"","updateRule":"","deleteRule":""}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: %d %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/notes/records", slug), token,
		`{"body":"survives the restore"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create record: %d %s", rec.Code, rec.Body.String())
	}

	ctx := context.Background()
	schema, roles := core.ProjectNames(slug)
	countPolicies := func() int {
		var n int
		if err := app.Pool.QueryRow(ctx,
			`select count(*) from pg_policies where schemaname = $1`, schema).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := countPolicies()
	if before == 0 {
		t.Fatal("expected policies on a fresh project")
	}

	// Simulate the restored state: policies and roles gone, _dbo usage stripped.
	for _, stmt := range []string{
		fmt.Sprintf(`drop owned by %q, %q, %q`, roles.Anon, roles.Authenticated, roles.Service),
		fmt.Sprintf(`drop role %q`, roles.Anon),
		fmt.Sprintf(`drop role %q`, roles.Authenticated),
		fmt.Sprintf(`drop role %q`, roles.Service),
		`revoke usage on schema _dbo from public`,
	} {
		if _, err := app.Pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("simulating restore (%s): %v", stmt, err)
		}
	}
	if n := countPolicies(); n != 0 {
		t.Fatalf("expected the simulated restore to leave 0 policies, got %d", n)
	}

	if err := core.ReconcileProjectSecurity(ctx, app.Pool, app.Log); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Roles are back.
	var present int
	if err := app.Pool.QueryRow(ctx, `select count(*) from pg_roles where rolname = any($1)`,
		[]string{roles.Anon, roles.Authenticated, roles.Service}).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if present != 3 {
		t.Errorf("roles rebuilt = %d, want 3", present)
	}

	// Policies are back.
	if after := countPolicies(); after != before {
		t.Errorf("policies rebuilt = %d, want %d", after, before)
	}

	// PUBLIC can reach _dbo again, or every policy denies on its first call.
	var usable bool
	if err := app.Pool.QueryRow(ctx,
		`select has_schema_privilege('public', '_dbo', 'usage')`).Scan(&usable); err != nil {
		t.Fatal(err)
	}
	if !usable {
		t.Error("_dbo usage was not restored to public")
	}

	// And the data is servable again.
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/notes/records", slug), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("read after reconcile: %d %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !contains(body, "survives the restore") {
		t.Errorf("record missing after reconcile: %s", body)
	}

	// Running it again must change nothing.
	if err := core.ReconcileProjectSecurity(ctx, app.Pool, app.Log); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if after := countPolicies(); after != before {
		t.Errorf("second run changed policy count to %d, want %d", after, before)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}()
}
