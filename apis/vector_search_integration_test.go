package apis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/dublyo/dublyobase/core"
)

// Vector search covers the retrieval half of RAG: store an embedding per row,
// then ask for the rows nearest a query embedding.
func TestVectorFieldAndNearestNeighbourSearch(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	ctx := context.Background()
	var available bool
	if err := app.Pool.QueryRow(ctx,
		`select exists(select 1 from pg_available_extensions where name = 'vector')`).Scan(&available); err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Skip("pgvector is not installed on this database")
	}

	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"chunks","type":"base","fields":[
			{"name":"body","type":"text"},
			{"name":"embedding","type":"vector","options":{"dimensions":3,"metric":"cosine"}}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: %d %s", rec.Code, rec.Body.String())
	}

	// The column gets an HNSW index, or the search is just a sequential scan.
	schema, _ := core.ProjectNames(slug)
	var indexdef string
	if err := app.Pool.QueryRow(ctx,
		`select indexdef from pg_indexes where schemaname = $1 and indexname = 'dbo_ix_chunks_embedding_hnsw'`,
		schema).Scan(&indexdef); err != nil {
		t.Fatalf("no hnsw index was created: %v", err)
	}
	if !containsAll(indexdef, "hnsw", "vector_cosine_ops") {
		t.Errorf("unexpected index: %s", indexdef)
	}

	seed := []struct{ body, vec string }{
		{"pointing right", "[1,0,0]"},
		{"almost right", "[0.9,0.1,0]"},
		{"pointing up", "[0,1,0]"},
	}
	for _, row := range seed {
		body := fmt.Sprintf(`{"body":%q,"embedding":%s}`, row.body, row.vec)
		if rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/chunks/records", slug), token, body); rec.Code != http.StatusCreated {
			t.Fatalf("seed %s: %d %s", row.body, rec.Code, rec.Body.String())
		}
	}

	// A vector reads back as the array it was written as, not pgvector's text.
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/chunks/records?perPage=50", slug), token)
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Items {
		if _, ok := item["embedding"].([]any); !ok {
			t.Fatalf("embedding came back as %T, want an array: %#v", item["embedding"], item["embedding"])
		}
	}

	// Nearest neighbours, closest first.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/chunks/records/search", slug), token,
		`{"field":"embedding","vector":[1,0,0],"limit":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		got = append(got, fmt.Sprint(item["body"]))
	}
	want := []string{"pointing right", "almost right", "pointing up"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("nearest order = %v, want %v", got, want)
		}
	}

	// Errors that should be caught before they reach the database.
	for _, tc := range []struct{ name, body string }{
		{"wrong dimensions", `{"body":"bad","embedding":[1,0]}`},
		{"not numbers", `{"body":"bad","embedding":["a","b","c"]}`},
	} {
		rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/chunks/records", slug), token, tc.body)
		if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d (%s), want a rejection", tc.name, rec.Code, rec.Body.String())
		}
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/chunks/records/search", slug), token,
		`{"field":"body","vector":[1,0,0]}`)
	if rec.Code < 400 {
		t.Errorf("searching a non-vector field should fail, got %d", rec.Code)
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/chunks/records/search", slug), token,
		`{"field":"embedding","vector":[1,0]}`)
	if rec.Code < 400 {
		t.Errorf("a wrong-length query vector should fail, got %d", rec.Code)
	}
}
