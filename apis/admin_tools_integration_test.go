package apis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dublyo/dublyobase/core"
	"github.com/jackc/pgx/v5"
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

func TestAdminSchemaDiscoveryImportAndTakeover(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	legacySchema := fmt.Sprintf("legacy_%d", time.Now().UnixNano()%1_000_000_000)
	legacyIdent := pgx.Identifier{legacySchema}.Sanitize()
	if _, err := app.Pool.Exec(context.Background(), `create schema `+legacyIdent); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Pool.Exec(context.Background(), fmt.Sprintf(`
		create table %s (
			id uuid primary key default gen_random_uuid(),
			created timestamptz not null default now(),
			updated timestamptz not null default now(),
			name text not null
		);
		create table %s (
			id uuid primary key default gen_random_uuid(),
			created timestamptz not null default now(),
			updated timestamptz not null default now(),
			author_id uuid references %s(id),
			title text not null,
			views int not null default 0
		);
		create table %s (
			id text primary key,
			name text not null,
			rank int not null default 0
		);
		insert into %s (id, name, rank) values ('cuid_text_1', 'Text primary key row', 7);
		create table %s (
			title text not null
		);`,
		pgx.Identifier{legacySchema, "authors"}.Sanitize(),
		pgx.Identifier{legacySchema, "articles"}.Sanitize(),
		pgx.Identifier{legacySchema, "authors"}.Sanitize(),
		pgx.Identifier{legacySchema, "text_ids"}.Sanitize(),
		pgx.Identifier{legacySchema, "text_ids"}.Sanitize(),
		pgx.Identifier{legacySchema, "without_pk"}.Sanitize(),
	)); err != nil {
		t.Fatal(err)
	}

	rec := getJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/schema/discover?schema=%s", slug, legacySchema), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("discover schema: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var discovery core.SchemaDiscoveryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &discovery); err != nil {
		t.Fatal(err)
	}
	found := map[string]core.DiscoveredTable{}
	for _, table := range discovery.Items {
		found[table.Table] = table
	}
	if !found["authors"].CanImport || !found["articles"].CanImport || !found["text_ids"].CanImport {
		t.Fatalf("authors/articles should be importable: %+v", found)
	}
	if found["text_ids"].PrimaryKey == nil || found["text_ids"].PrimaryKey.Field != "id" {
		t.Fatalf("text primary key should be exposed as id: %+v", found["text_ids"].PrimaryKey)
	}
	if found["text_ids"].PrimaryKey.HasDefault {
		t.Fatalf("text primary key should not report a database default: %+v", found["text_ids"].PrimaryKey)
	}
	if found["without_pk"].CanImport || !strings.Contains(found["without_pk"].Reason, "primary key") {
		t.Fatalf("without_pk should be read-only: %+v", found["without_pk"])
	}
	if !found["articles"].StandardSystemColumns || len(found["articles"].ForeignKeys) != 1 {
		t.Fatalf("articles discovery missing standard columns or FK: %+v", found["articles"])
	}

	importBody := fmt.Sprintf(`{
		"dryRun": true,
		"items": [
			{"schema":%q,"table":"authors","name":"legacy_authors"},
			{"schema":%q,"table":"articles","name":"legacy_articles"},
			{"schema":%q,"table":"text_ids","name":"legacy_text_ids"},
			{"schema":%q,"table":"without_pk","name":"legacy_without_pk"}
		]
	}`, legacySchema, legacySchema, legacySchema, legacySchema)
	rec = postJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/schema/import", slug), token, importBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview schema import: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var preview core.CollectionImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if !preview.DryRun || preview.Created != 3 || preview.Skipped != 1 {
		t.Fatalf("unexpected import preview: %+v", preview)
	}

	importBody = strings.Replace(importBody, `"dryRun": true`, `"dryRun": false`, 1)
	rec = postJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/schema/import", slug), token, importBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply schema import: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var applied core.CollectionImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Created != 3 || applied.Skipped != 1 {
		t.Fatalf("unexpected import result: %+v", applied)
	}

	author := createRecordInCollectionForTest(t, srv.Handler, slug, "legacy_authors", token, `{"name":"Ada"}`)
	authorID, ok := author["id"].(string)
	if !ok || authorID == "" {
		t.Fatalf("imported author record missing id: %+v", author)
	}
	article := createRecordInCollectionForTest(t, srv.Handler, slug, "legacy_articles", token, fmt.Sprintf(`{"title":"First","author_id":%q,"views":3}`, authorID))
	if article["title"] != "First" || article["views"] == nil {
		t.Fatalf("unexpected imported article response: %+v", article)
	}
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/legacy_articles/records?filter[title][_eq]=First&perPage=10", slug), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list imported records: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertRecordListCount(t, rec.Body.Bytes(), 1)

	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/legacy_text_ids/records?fields=id,name,rank&filter[id][_eq]=cuid_text_1&perPage=10", slug), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list imported text-id records: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var textIDList core.RecordListResult
	if err := json.Unmarshal(rec.Body.Bytes(), &textIDList); err != nil {
		t.Fatal(err)
	}
	if len(textIDList.Items) != 1 || textIDList.Items[0]["id"] != "cuid_text_1" || textIDList.Items[0]["name"] != "Text primary key row" {
		t.Fatalf("unexpected text-id list result: %+v", textIDList.Items)
	}
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/legacy_text_ids/records/cuid_text_1", slug), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("get imported text-id record: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/legacy_text_ids/records", slug), token, `{"name":"Missing id","rank":8}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create imported text-id record without id: want 422, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/legacy_text_ids/records", slug), token, `{"id":"cuid_text_2","name":"Created through API","rank":8}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create imported text-id record: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var createdTextID core.Record
	if err := json.Unmarshal(rec.Body.Bytes(), &createdTextID); err != nil {
		t.Fatal(err)
	}
	if createdTextID["id"] != "cuid_text_2" || createdTextID["name"] != "Created through API" {
		t.Fatalf("unexpected imported text-id create result: %+v", createdTextID)
	}
	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/legacy_text_ids/records/cuid_text_2", slug), token, `{"id":"cuid_text_3"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("patch imported primary key: want 422, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/legacy_articles", slug), token, `{"fields":[{"name":"title","type":"text"},{"name":"summary","type":"text"}]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("unmanaged imported field edit: want 409, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/legacy_articles", slug), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("get imported collection: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var imported core.Collection
	if err := json.Unmarshal(rec.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	var options map[string]any
	if err := json.Unmarshal(imported.Options, &options); err != nil {
		t.Fatal(err)
	}
	var authorRelation core.Field
	for _, field := range imported.Fields {
		if field.Name == "author_id" {
			authorRelation = field
			break
		}
	}
	if authorRelation.Type != "relation" || authorRelation.Options["collection"] != "legacy_authors" {
		t.Fatalf("imported relation should target renamed collection legacy_authors: %+v", authorRelation)
	}
	options["managed"] = true
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	managedBody, err := json.Marshal(map[string]any{
		"options": json.RawMessage(optionsJSON),
		"fields":  append(imported.Fields, core.Field{Name: "summary", Type: "text"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/legacy_articles", slug), token, string(managedBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("managed imported field edit: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertColumnExists(t, app.Pool, legacySchema, "articles", "summary")
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
