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
	"golang.org/x/crypto/bcrypt"
)

func TestRecordsAPIAndRLS(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)
	schema, roles := core.ProjectNames(slug)

	createCollectionBody := `{
		"name":"posts",
		"type":"base",
		"fields":[
			{"name":"title","type":"text","required":true,"searchable":true},
			{"name":"published","type":"bool"},
			{"name":"owner","type":"relation","options":{"collection":"users"}}
		],
		"listRule":"published = true",
		"viewRule":"published = true || owner = @request.auth.id",
		"updateRule":"owner = @request.auth.id"
	}`
	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), adminToken, createCollectionBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	serviceKey := createAPIKeyForRecords(t, srv.Handler, adminToken, slug, "service")
	keys := getJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/api-keys", slug), adminToken)
	if keys.Code != http.StatusOK {
		t.Fatalf("list keys: want 200, got %d: %s", keys.Code, keys.Body.String())
	}

	user1 := signupAppUserForTest(t, srv.Handler, slug, "owner1@example.com")
	user2 := signupAppUserForTest(t, srv.Handler, slug, "owner2@example.com")
	owner1 := user1.User.ID
	owner2 := user2.User.ID
	publicRecord := createRecordForTest(t, srv.Handler, slug, serviceKey, fmt.Sprintf(`{"title":"Public","published":true,"owner":%q}`, owner2))
	privateRecord := createRecordForTest(t, srv.Handler, slug, serviceKey, fmt.Sprintf(`{"title":"Private","owner":%q}`, owner1))

	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records", slug), serviceKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("service list: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertRecordListCount(t, rec.Body.Bytes(), 2)

	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records", slug), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("anon list: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertRecordListCount(t, rec.Body.Bytes(), 1)

	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records/%s", slug, publicRecord["id"]), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("anon view public: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records/%s", slug, privateRecord["id"]), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("anon view private: want 404, got %d: %s", rec.Code, rec.Body.String())
	}

	user1Token := user1.Token
	user2Token := user2.Token
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records/%s", slug, privateRecord["id"]), user1Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner view private: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records/%s", slug, privateRecord["id"]), user1Token, `{"title":"Private updated"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner update private: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records/%s", slug, privateRecord["id"]), user2Token, `{"title":"Blocked"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner update private: want 404, got %d: %s", rec.Code, rec.Body.String())
	}

	assertDirectRoleCount(t, app.Pool, roles.Anon, schema, "list", `{"role":"anon","project":"`+slug+`"}`, "", 1)
	assertDirectRoleCount(t, app.Pool, roles.Authenticated, schema, "view", fmt.Sprintf(`{"role":"authenticated","project":%q,"sub":%q}`, slug, owner1), fmt.Sprintf("where id = '%s'", privateRecord["id"]), 1)
	assertDirectRoleCount(t, app.Pool, roles.Service, schema, "list", `{"role":"service","project":"`+slug+`"}`, "", 2)

	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records?filter=title%%20=%%20%%22Public%%22&sort=-created&fields=id,title", slug), serviceKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered service list: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertRecordListCount(t, rec.Body.Bytes(), 1)
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records?filter=%%7B%%22title%%22%%3A%%7B%%22_icontains%%22%%3A%%22pub%%22%%7D%%7D&limit=10", slug), serviceKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("JSON filtered service list: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertRecordListCount(t, rec.Body.Bytes(), 1)
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records?filter[title][_icontains]=pub&limit=10", slug), serviceKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("bracket filtered service list: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertRecordListCount(t, rec.Body.Bytes(), 1)
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records?search=pub&perPage=10", slug), serviceKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("search service list: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	assertRecordListCount(t, rec.Body.Bytes(), 1)
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records?filter=missing%%20=%%20true", slug), serviceKey)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid filter: want 422, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = deleteJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records/%s", slug, privateRecord["id"]), serviceKey, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("service delete: want 204, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/posts/records/%s", slug, privateRecord["id"]), serviceKey)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted: want 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPocketBaseStyleFieldOptionsAPI(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)
	schema, _ := core.ProjectNames(slug)
	serviceKey := createAPIKeyForRecords(t, srv.Handler, adminToken, slug, "service")

	createCollectionBody := `{
		"name":"entries",
		"type":"base",
		"fields":[
			{"name":"title","type":"text","required":true,"presentable":true,"help":"Shown in relation previews","options":{"min":3,"max":30,"pattern":"^[A-Z].*"}},
			{"name":"body","type":"editor","options":{"maxSize":64}},
			{"name":"secret","type":"password","required":true,"hidden":true,"options":{"min":8,"max":20,"cost":4}},
			{"name":"score","type":"number","options":{"onlyInt":true,"min":1,"max":10}},
			{"name":"contact","type":"email","options":{"onlyDomains":["example.com"]}},
			{"name":"labels","type":"select","required":true,"options":{"values":["a","b","c"],"maxSelect":2}},
			{"name":"created_auto","type":"autodate","options":{"onCreate":true}},
			{"name":"touched_auto","type":"autodate","options":{"onCreate":true,"onUpdate":true}}
		]
	}`
	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), adminToken, createCollectionBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	record := createRecordInCollectionForTest(t, srv.Handler, slug, "entries", serviceKey, `{
		"title":"Hello",
		"body":"<p>Hello</p>",
		"secret":"secret-123",
		"score":4,
		"contact":"me@example.com",
		"labels":["a","b"]
	}`)
	if _, ok := record["secret"]; ok {
		t.Fatalf("password field must not be returned: %+v", record)
	}
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/entries/records?fields=secret", slug), serviceKey)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("password field projection: want 422, got %d: %s", rec.Code, rec.Body.String())
	}
	createdAuto, _ := record["created_auto"].(string)
	touchedAuto, _ := record["touched_auto"].(string)
	if createdAuto == "" || touchedAuto == "" {
		t.Fatalf("autodate fields missing from create response: %+v", record)
	}

	var passwordHash string
	if err := app.Pool.QueryRow(context.Background(), fmt.Sprintf(`select secret from %s where id = $1`, pgx.Identifier{schema, "entries"}.Sanitize()), record["id"]).Scan(&passwordHash); err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("secret-123")); err != nil {
		t.Fatalf("password was not stored as bcrypt hash: %v", err)
	}

	time.Sleep(2 * time.Millisecond)
	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/entries/records/%s", slug, record["id"]), serviceKey, `{"title":"Hello updated"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch record: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated["created_auto"] != createdAuto {
		t.Fatalf("created_auto changed on update: before=%v after=%v", createdAuto, updated["created_auto"])
	}
	if updated["touched_auto"] == touchedAuto {
		t.Fatalf("touched_auto did not change on update: %+v", updated)
	}

	invalidCases := map[string]string{
		"bad title pattern": `{"title":"lower","secret":"secret-123","labels":["a"]}`,
		"bad body size":     `{"title":"Hello","body":"` + strings.Repeat("x", 65) + `","secret":"secret-123","labels":["a"]}`,
		"bad secret":        `{"title":"Hello","secret":"short","labels":["a"]}`,
		"bad score":         `{"title":"Hello","secret":"secret-123","score":1.5,"labels":["a"]}`,
		"bad email domain":  `{"title":"Hello","secret":"secret-123","contact":"me@example.net","labels":["a"]}`,
		"too many labels":   `{"title":"Hello","secret":"secret-123","labels":["a","b","c"]}`,
		"autodate write":    `{"title":"Hello","secret":"secret-123","labels":["a"],"created_auto":"2026-07-04T00:00:00Z"}`,
	}
	for name, body := range invalidCases {
		rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/entries/records", slug), serviceKey, body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: want 422, got %d: %s", name, rec.Code, rec.Body.String())
		}
	}
}

func createAPIKeyForRecords(t *testing.T, handler http.Handler, adminToken string, slug string, typ string) string {
	t.Helper()
	rec := postJSON(handler, fmt.Sprintf("/admin/api/projects/%s/api-keys", slug), adminToken, fmt.Sprintf(`{"name":"%s key","type":%q}`, typ, typ))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create API key: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Key == "" {
		t.Fatal("created API key response missing key")
	}
	return body.Key
}

func createRecordForTest(t *testing.T, handler http.Handler, slug string, token string, body string) map[string]any {
	t.Helper()
	rec := postJSON(handler, fmt.Sprintf("/api/projects/%s/collections/posts/records", slug), token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create record: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["id"] == "" {
		t.Fatalf("record missing id: %v", out)
	}
	return out
}

func assertRecordListCount(t *testing.T, body []byte, want int) {
	t.Helper()
	var out struct {
		Items      []map[string]any `json:"items"`
		TotalItems int              `json:"totalItems"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != want || out.TotalItems != want {
		t.Fatalf("record list count = len:%d total:%d, want %d; body=%s", len(out.Items), out.TotalItems, want, string(body))
	}
}

func assertDirectRoleCount(t *testing.T, pool *pgxpool.Pool, role string, schema string, operation string, claims string, where string, want int) {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(context.Background(), fmt.Sprintf(`set local role %s`, pgx.Identifier{role}.Sanitize())); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `select set_config('request.jwt.claims', $1, true)`, claims); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `select set_config('request.operation', $1, true)`, operation); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `select set_config('search_path', $1, true)`, schema+", pg_catalog"); err != nil {
		t.Fatal(err)
	}
	query := fmt.Sprintf(`select count(*) from %s %s`, pgx.Identifier{schema, "posts"}.Sanitize(), where)
	var count int
	if err := tx.QueryRow(context.Background(), query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("direct role %s count = %d, want %d", role, count, want)
	}
}
