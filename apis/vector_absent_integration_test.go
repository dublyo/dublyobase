package apis

import (
	"fmt"
	"testing"
)

func TestVectorFieldRefusedWithoutPgvector(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	var available bool
	if err := app.Pool.QueryRow(t.Context(),
		`select exists(select 1 from pg_available_extensions where name = 'vector')`).Scan(&available); err != nil {
		t.Fatal(err)
	}
	if available {
		t.Skip("pgvector is installed here; this covers the database that lacks it")
	}
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"chunks","type":"base","fields":[{"name":"embedding","type":"vector","options":{"dimensions":3}}]}`)
	if rec.Code >= 500 {
		t.Fatalf("got %d (%s), want a clear 4xx", rec.Code, rec.Body.String())
	}
	if rec.Code < 400 {
		t.Fatalf("got %d, want a rejection when pgvector is absent", rec.Code)
	}
	if !containsAll(rec.Body.String(), "pgvector") {
		t.Errorf("message should name pgvector: %s", rec.Body.String())
	}
}
