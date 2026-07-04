package apis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dublyo/dublyobase/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdminCollectionSyncEndpoints(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	source := createProjectForCollections(t, srv.Handler, token)
	target := createProjectForCollections(t, srv.Handler, token)
	targetSchema, _ := core.ProjectNames(target)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", source), token, `{
		"name":"posts",
		"type":"base",
		"fields":[{"name":"title","type":"text","required":true}]
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create source collection: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = getJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/collections/export", source), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("export collections: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var exported core.CollectionExportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	var posts core.CollectionSchemaItem
	for _, item := range exported.Items {
		if item.Name == "posts" {
			posts = item
			break
		}
	}
	if posts.Name != "posts" || posts.System {
		t.Fatalf("posts export missing or wrong: %+v", exported.Items)
	}
	importBody, err := json.Marshal(map[string]any{
		"items":  []core.CollectionSchemaItem{posts},
		"mode":   core.CollectionImportCreateMissing,
		"dryRun": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/collections/import", target), token, string(importBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview import: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var preview core.CollectionImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if !preview.DryRun || preview.Created != 1 {
		t.Fatalf("unexpected preview: %+v", preview)
	}

	importBody, err = json.Marshal(map[string]any{
		"items":  []core.CollectionSchemaItem{posts},
		"mode":   core.CollectionImportCreateMissing,
		"dryRun": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/collections/import", target), token, string(importBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("apply import: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertColumnExists(t, app.Pool, targetSchema, "posts", "title")

	posts.Fields = append(posts.Fields, core.Field{Name: "body", Type: "text"})
	importBody, err = json.Marshal(map[string]any{
		"items":  []core.CollectionSchemaItem{posts},
		"mode":   core.CollectionImportUpsert,
		"dryRun": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/collections/import", target), token, string(importBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert import: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertColumnExists(t, app.Pool, targetSchema, "posts", "body")
	assertProjectAuditExists(t, app.Pool, "collections.import", target)
}

func TestAdminSQLConsole(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/sql", slug), token, `{"query":"select 1 as one","maxRows":10}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("select sql: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var result core.AdminSQLResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Columns) != 1 || result.Columns[0].Name != "one" || len(result.Rows) != 1 || result.Rows[0][0] != float64(1) {
		t.Fatalf("unexpected select result: %+v", result)
	}

	rec = postJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/sql", slug), token, `{"query":"create table console_check(id int)","maxRows":10}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create sql: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result = core.AdminSQLResult{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ReadOnly || !strings.Contains(strings.ToUpper(result.Command), "CREATE") {
		t.Fatalf("unexpected create result: %+v", result)
	}

	rec = postJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/sql", slug), token, `{"query":"select 1; select 2","maxRows":10}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("multi statement sql: want 422, got %d: %s", rec.Code, rec.Body.String())
	}
	assertProjectAuditExists(t, app.Pool, "sql.execute", slug)
}

func assertProjectAuditExists(t *testing.T, pool *pgxpool.Pool, action string, slug string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from _dbo.audit_log where action = $1 and data->>'project' = $2`,
		action,
		slug,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatalf("audit action %q for project %q missing", action, slug)
	}
}
