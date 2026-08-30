package apis

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Pointing a relation at a collection that does not exist is a fault in the
// request, not a missing resource at the URL. It used to answer 404 with a bare
// "collection not found", naming neither the field nor the target — unhelpful
// when a whole schema is being created in one pass and one line is wrong.
func TestRelationToMissingCollectionNamesIt(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"carts","type":"base","fields":[
			{"name":"device","type":"relation","options":{"collection":"register_devices"}}]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d (%s), want 422", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"device", "register_devices"} {
		if !strings.Contains(body, want) {
			t.Errorf("message should name %q: %s", want, body)
		}
	}

	// The existing, clearer error for a relation with no target at all is
	// unchanged.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"carts2","type":"base","fields":[{"name":"device","type":"relation","options":{}}]}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("missing target: got %d, want 422", rec.Code)
	}

	// And a relation to a collection that does exist still works.
	if rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"devices","type":"base","fields":[{"name":"os","type":"text"}]}`); rec.Code != http.StatusCreated {
		t.Fatalf("devices: %d %s", rec.Code, rec.Body.String())
	}
	if rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"carts3","type":"base","fields":[{"name":"device","type":"relation","options":{"collection":"devices"}}]}`); rec.Code != http.StatusCreated {
		t.Errorf("valid relation: %d %s", rec.Code, rec.Body.String())
	}
}
