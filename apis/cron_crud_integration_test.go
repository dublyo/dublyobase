package apis

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Cron jobs could be created but never corrected or removed, so a job saved
// with the wrong URL — or one pointing somewhere it should not — was permanent.
func TestCronJobUpdateAndDelete(t *testing.T) {
	app, cleanup := newIntegrationApp(t)
	defer cleanup()
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")

	rec := postJSON(srv.Handler, "/admin/api/cron-jobs", adminToken, `{
		"name": "nightly", "type": "http", "schedule": "@every 1h", "timezone": "UTC",
		"method": "GET", "url": "https://example.com/old"
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var job struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}

	rec = patchJSON(srv.Handler, "/admin/api/cron-jobs/"+job.ID, adminToken, `{
		"name": "nightly", "type": "http", "schedule": "@every 2h", "timezone": "UTC",
		"method": "GET", "url": "https://example.com/new", "enabled": false
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated struct {
		URL      string `json:"url"`
		Schedule string `json:"schedule"`
		Enabled  bool   `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.URL != "https://example.com/new" || updated.Schedule != "@every 2h" || updated.Enabled {
		t.Fatalf("update did not apply: %+v", updated)
	}

	// An update must not be a way around the outbound-target check.
	rec = patchJSON(srv.Handler, "/admin/api/cron-jobs/"+job.ID, adminToken, `{
		"name": "nightly", "type": "http", "schedule": "@every 2h", "timezone": "UTC",
		"method": "GET", "url": "http://169.254.169.254/latest/meta-data/"
	}`)
	if rec.Code == http.StatusOK {
		t.Error("update accepted a link-local target")
	}

	rec = deleteJSON(srv.Handler, "/admin/api/cron-jobs/"+job.ID, adminToken, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = getJSON(srv.Handler, "/admin/api/cron-jobs", adminToken)
	if strings.Contains(rec.Body.String(), job.ID) {
		t.Errorf("deleted job still listed: %s", rec.Body.String())
	}

	// Deleting twice must not silently succeed.
	if rec = deleteJSON(srv.Handler, "/admin/api/cron-jobs/"+job.ID, adminToken, ""); rec.Code == http.StatusNoContent {
		t.Error("deleting a missing job reported success")
	}
}
