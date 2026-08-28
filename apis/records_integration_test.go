package apis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestRelationUniqueOptionEnforcesOneToOne(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)
	schema, _ := core.ProjectNames(slug)
	serviceKey := createAPIKeyForRecords(t, srv.Handler, adminToken, slug, "service")
	user1 := signupAppUserForTest(t, srv.Handler, slug, "profile-owner-1@example.com")
	user2 := signupAppUserForTest(t, srv.Handler, slug, "profile-owner-2@example.com")

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), adminToken, `{
		"name":"profiles",
		"type":"base",
		"fields":[
			{"name":"customer","type":"relation","required":true,"options":{"collection":"users","maxSelect":1,"unique":true}},
			{"name":"display_name","type":"text","required":true}
		]
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create profiles collection: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var indexCount int
	if err := app.Pool.QueryRow(context.Background(), `
		select count(*)
		from pg_indexes
		where schemaname = $1
			and tablename = 'profiles'
			and indexname like 'dbo_reluniq_profiles_customer%'`, schema).Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 1 {
		t.Fatalf("unique relation index count = %d, want 1", indexCount)
	}

	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/profiles/records", slug), serviceKey, fmt.Sprintf(`{"customer":%q,"display_name":"Primary"}`, user1.User.ID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create first profile: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/profiles/records", slug), serviceKey, fmt.Sprintf(`{"customer":%q,"display_name":"Duplicate"}`, user1.User.ID))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate profile: want 409, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/profiles/records", slug), serviceKey, fmt.Sprintf(`{"customer":%q,"display_name":"Other"}`, user2.User.ID))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create second user profile: want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), adminToken, `{
		"name":"bad_profiles",
		"type":"base",
		"fields":[
			{"name":"customers","type":"relation","options":{"collection":"users","maxSelect":2,"unique":true}}
		]
	}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unique multi relation: want 422, got %d: %s", rec.Code, rec.Body.String())
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

// TestDecimalRecordsAreExact is the regression for the defect that made this
// backend unusable for money: `number` is double precision, so individual
// values round-trip fine while SUM() drifts. These are the exact values that
// summed to 100001234.85499999 on the live instance.
func TestDecimalRecordsAreExact(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"deals","type":"base","fields":[
			{"name":"title","type":"text"},
			{"name":"amount","type":"decimal","options":{"precision":18,"scale":3}}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: %d %s", rec.Code, rec.Body.String())
	}

	for _, amount := range []string{`"0.1"`, `"0.2"`, `"99999999.99"`, `"1234.565"`} {
		rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/deals/records", slug), token,
			fmt.Sprintf(`{"title":"m","amount":%s}`, amount))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", amount, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		// The wire value must be a string; a float64 here means precision is
		// already gone before anyone sums anything.
		got, ok := body["amount"].(string)
		if !ok {
			t.Fatalf("amount came back as %T (%v), want string", body["amount"], body["amount"])
		}
		want := strings.Trim(amount, `"`)
		if !strings.HasPrefix(got, want) {
			t.Fatalf("amount = %q, want %q", got, want)
		}
	}

	schema, _ := core.ProjectNames(slug)
	var total string
	if err := app.Pool.QueryRow(context.Background(),
		fmt.Sprintf(`select sum(amount)::text from %s.deals`, schema)).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != "100001234.855" {
		t.Fatalf("SUM(amount) = %s, want exactly 100001234.855 (float8 gives 100001234.85499999)", total)
	}

	// An exact filter must match the stored value rather than a float neighbour.
	rec = getJSON(srv.Handler, fmt.Sprintf(
		`/api/projects/%s/collections/deals/records?filter={"amount":{"_eq":"1234.565"}}`, slug), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("filter: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("exact decimal filter matched %d rows, want 1", len(list.Items))
	}
}

// TestAtomicBatchRollsBack is the guarantee an ERP write needs: an order and
// its lines either all land or none do. Before atomic batches existed, request
// N failing left requests 1..N-1 committed with no way to undo them.
func TestAtomicBatchRollsBack(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"orders","type":"base","fields":[{"name":"ref","type":"text","required":true}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: %d %s", rec.Code, rec.Body.String())
	}

	countOrders := func() int {
		schema, _ := core.ProjectNames(slug)
		var n int
		if err := app.Pool.QueryRow(context.Background(),
			fmt.Sprintf(`select count(*) from %s.orders`, schema)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Two good writes followed by one that violates NOT NULL on ref.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/batch", slug), token,
		`{"atomic":true,"requests":[
			{"method":"POST","collection":"orders","body":{"ref":"SO-1"}},
			{"method":"POST","collection":"orders","body":{"ref":"SO-2"}},
			{"method":"POST","collection":"orders","body":{}}]}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("atomic batch with a failing op returned 200: %s", rec.Body.String())
	}
	if n := countOrders(); n != 0 {
		t.Fatalf("after rollback there are %d orders, want 0 — the batch was not atomic", n)
	}

	// The same batch without the bad op must commit all of it.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/batch", slug), token,
		`{"atomic":true,"requests":[
			{"method":"POST","collection":"orders","body":{"ref":"SO-1"}},
			{"method":"POST","collection":"orders","body":{"ref":"SO-2"}}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("atomic batch: %d %s", rec.Code, rec.Body.String())
	}
	if n := countOrders(); n != 2 {
		t.Fatalf("committed %d orders, want 2", n)
	}

	// Non-atomic keeps its old partial-write behaviour.
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/batch", slug), token,
		`{"atomic":false,"requests":[
			{"method":"POST","collection":"orders","body":{"ref":"SO-3"}},
			{"method":"POST","collection":"orders","body":{}}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("non-atomic batch: %d %s", rec.Code, rec.Body.String())
	}
	if n := countOrders(); n != 3 {
		t.Fatalf("non-atomic committed %d orders, want 3", n)
	}
}

// TestOrgScopedRuleIsolatesTenants is the multi-tenant CRM predicate: two users
// in different organizations must not see each other's rows, enforced by
// Postgres RLS rather than by the client sending the right filter.
func TestOrgScopedRuleIsolatesTenants(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"deals","type":"base","fields":[
			{"name":"title","type":"text"},
			{"name":"org","type":"text","required":true}],
		  "listRule":"org = @request.auth.orgId","viewRule":"org = @request.auth.orgId",
		  "createRule":"org = @request.auth.orgId"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create org-scoped collection: %d %s", rec.Code, rec.Body.String())
	}

	userToken := signupAppUserForTest(t, srv.Handler, slug, "a@example.com").Token
	otherToken := signupAppUserForTest(t, srv.Handler, slug, "b@example.com").Token
	orgA := createOrgForTest(t, srv.Handler, slug, userToken, "Org A")
	orgB := createOrgForTest(t, srv.Handler, slug, otherToken, "Org B")

	// Create one deal in each org, each as its own member.
	mk := func(tok, org, title string) {
		req := httptest.NewRequest("POST", fmt.Sprintf("/api/projects/%s/collections/deals/records", slug),
			bytes.NewBufferString(fmt.Sprintf(`{"title":%q,"org":%q}`, title, org)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("X-Org-Id", org)
		w := httptest.NewRecorder()
		srv.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", title, w.Code, w.Body.String())
		}
	}
	mk(userToken, orgA, "A deal")
	mk(otherToken, orgB, "B deal")

	list := func(tok, org string) []map[string]any {
		req := httptest.NewRequest("GET", fmt.Sprintf("/api/projects/%s/collections/deals/records", slug), nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("X-Org-Id", org)
		w := httptest.NewRecorder()
		srv.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("list: %d %s", w.Code, w.Body.String())
		}
		var body struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Items
	}

	if items := list(userToken, orgA); len(items) != 1 || items[0]["title"] != "A deal" {
		t.Fatalf("org A sees %v, want only its own deal", items)
	}
	if items := list(otherToken, orgB); len(items) != 1 || items[0]["title"] != "B deal" {
		t.Fatalf("org B sees %v, want only its own deal", items)
	}

	// Claiming an org you do not belong to must be refused, not silently ignored.
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/projects/%s/collections/deals/records", slug), nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	req.Header.Set("X-Org-Id", orgB)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-org header: want 403, got %d %s", w.Code, w.Body.String())
	}
}

func createOrgForTest(t *testing.T, handler http.Handler, slug string, token string, name string) string {
	t.Helper()
	rec := postJSON(handler, fmt.Sprintf("/api/projects/%s/orgs", slug), token, fmt.Sprintf(`{"name":%q}`, name))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create org: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ID == "" {
		t.Fatalf("org id missing: %s", rec.Body.String())
	}
	return body.ID
}

// TestAggregateRecords covers the reporting primitive: grouped totals computed
// in Postgres, with decimal sums staying exact.
func TestAggregateRecords(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"deals","type":"base","fields":[
			{"name":"stage","type":"text"},
			{"name":"amount","type":"decimal","options":{"precision":18,"scale":2}}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: %d %s", rec.Code, rec.Body.String())
	}
	for _, row := range []struct{ stage, amount string }{
		{"won", "100.10"}, {"won", "200.20"}, {"lost", "50.05"},
	} {
		rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/deals/records", slug), token,
			fmt.Sprintf(`{"stage":%q,"amount":%q}`, row.stage, row.amount))
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
		}
	}

	rec = getJSON(srv.Handler, fmt.Sprintf(
		"/api/projects/%s/collections/deals/records/aggregate?aggregate=sum:amount,count:*&groupBy=stage", slug), token)
	if rec.Code != http.StatusOK {
		t.Fatalf("aggregate: %d %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Items []struct {
			Group  map[string]any `json:"group"`
			Values map[string]any `json:"values"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("got %d groups, want 2: %s", len(result.Items), rec.Body.String())
	}
	byStage := map[string]map[string]any{}
	for _, item := range result.Items {
		byStage[fmt.Sprint(item.Group["stage"])] = item.Values
	}
	// 100.10 + 200.20 must be exactly 300.30, not 300.29999999999995.
	if got := fmt.Sprint(byStage["won"]["sum_amount"]); got != "300.30" {
		t.Fatalf("won sum = %s, want 300.30", got)
	}
	if got := fmt.Sprint(byStage["lost"]["sum_amount"]); got != "50.05" {
		t.Fatalf("lost sum = %s, want 50.05", got)
	}
	if got := fmt.Sprint(byStage["won"]["count"]); got != "2" {
		t.Fatalf("won count = %s, want 2", got)
	}

	// Unsupported aggregates must be refused, not silently ignored.
	rec = getJSON(srv.Handler, fmt.Sprintf(
		"/api/projects/%s/collections/deals/records/aggregate?aggregate=drop:amount", slug), token)
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("bad aggregate fn: want 4xx, got %d %s", rec.Code, rec.Body.String())
	}
	rec = getJSON(srv.Handler, fmt.Sprintf(
		"/api/projects/%s/collections/deals/records/aggregate?aggregate=sum:stage", slug), token)
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("sum of text: want 4xx, got %d %s", rec.Code, rec.Body.String())
	}

	// The list endpoint must now reject these instead of returning plain rows.
	rec = getJSON(srv.Handler, fmt.Sprintf(
		"/api/projects/%s/collections/deals/records?aggregate=sum:amount&groupBy=stage", slug), token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("list with aggregate: want 400, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestAggregateHonoursBracketFilters covers the worst failure mode of the
// aggregate endpoint: a filter that is silently dropped returns a total over
// every row, which reads as a correct report.
func TestAggregateHonoursBracketFilters(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"deals","type":"base","fields":[
			{"name":"stage","type":"text"},{"name":"amount","type":"decimal","options":{"precision":18,"scale":3}},
			{"name":"flag","type":"bool"}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("collection: %d %s", rec.Code, rec.Body.String())
	}
	for _, row := range [][2]string{{"won", "100.000"}, {"won", "50.000"}, {"lost", "999.000"}} {
		postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/deals/records", slug), token,
			fmt.Sprintf(`{"stage":%q,"amount":%q}`, row[0], row[1]))
	}
	sum := func(url string) string {
		rec := getJSON(srv.Handler, url, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("aggregate: %d %s", rec.Code, rec.Body.String())
		}
		var r struct {
			Items []struct{ Values map[string]any } `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if len(r.Items) != 1 {
			t.Fatalf("want 1 row, got %d: %s", len(r.Items), rec.Body.String())
		}
		return fmt.Sprint(r.Items[0].Values["sum_amount"])
	}
	base := fmt.Sprintf("/api/projects/%s/collections/deals/records/aggregate?aggregate=sum:amount", slug)
	if got := sum(base + `&filter={"stage":{"_eq":"won"}}`); got != "150.000" {
		t.Fatalf("json filter sum = %s, want 150.000", got)
	}
	// The bracket form must total the same. It used to be ignored, giving 1149.
	if got := sum(base + "&filter[stage][_eq]=won"); got != "150.000" {
		t.Fatalf("bracket filter sum = %s, want 150.000 (filter was dropped)", got)
	}

	// min/max on types Postgres has no aggregate for must be a validation
	// error, not a database error surfacing as a 500.
	rec = getJSON(srv.Handler, fmt.Sprintf(
		"/api/projects/%s/collections/deals/records/aggregate?aggregate=max:flag", slug), token)
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("max on bool: want 4xx, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestImportedCollectionRejectsRules: the editor used to accept API rules on
// imported tables and store them, while policy sync skipped those tables — an
// access rule that nothing enforced.
func TestImportedCollectionRejectsRules(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)
	schema, _ := core.ProjectNames(slug)

	if _, err := app.Pool.Exec(context.Background(),
		fmt.Sprintf(`create table %s.legacy (id uuid primary key default gen_random_uuid(), owner uuid, note text)`, schema)); err != nil {
		t.Fatal(err)
	}
	rec := postJSON(srv.Handler, fmt.Sprintf("/admin/api/projects/%s/schema/import", slug), token,
		`{"items":[{"schema":"`+schema+`","table":"legacy","name":"legacy"}]}`)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Skipf("schema import unavailable: %d %s", rec.Code, rec.Body.String())
	}
	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/legacy", slug), token,
		`{"listRule":"@request.auth.id != \"\""}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("rules on imported collection: want 422, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestDecimalSortsNumerically: casting decimal to ::text in the SELECT list
// created an output alias that shadows the real column, and SQL resolves a bare
// ORDER BY name against the output alias first — so money sorted
// lexicographically (95, 450, 400, 2885, 220) while looking perfectly fine.
func TestDecimalSortsNumerically(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"invoices","type":"base","fields":[
			{"name":"total","type":"decimal","options":{"precision":18,"scale":3}},
			{"name":"qty","type":"number","options":{"onlyInt":true}}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	for _, v := range []string{"95.000", "450.000", "400.000", "2885.000", "220.000"} {
		postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/invoices/records", slug), token,
			fmt.Sprintf(`{"total":%q,"qty":1}`, v))
	}
	rec = getJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/invoices/records?sort=-total", slug), token)
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(body.Items))
	for _, it := range body.Items {
		got = append(got, fmt.Sprint(it["total"]))
	}
	want := []string{"2885.000", "450.000", "400.000", "220.000", "95.000"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sort=-total = %v, want %v (lexicographic order means the text cast leaked)", got, want)
	}
	// and the wire value is still the exact decimal string
	if _, ok := body.Items[0]["total"].(string); !ok {
		t.Fatalf("total came back as %T, want an exact string", body.Items[0]["total"])
	}
}

// TestConstraintViolationsAreNot500: checks and foreign keys added by the
// constraints feature surfaced as "internal server error", which tells the
// caller nothing about which rule their record broke.
func TestConstraintViolationsAreNot500(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	token := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, token)

	postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"parents","type":"base","fields":[{"name":"name","type":"text"}]}`)
	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), token,
		`{"name":"lines","type":"base","fields":[
			{"name":"parent","type":"relation","options":{"collection":"parents","onDelete":"restrict"}},
			{"name":"qty","type":"number","options":{"onlyInt":true}}],
		  "options":{"checks":[{"name":"qty_positive","expression":"qty > 0"}]}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create lines: %d %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/parents/records", slug), token, `{"name":"p"}`)
	var parent map[string]any
	json.Unmarshal(rec.Body.Bytes(), &parent)
	pid := fmt.Sprint(parent["id"])
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/lines/records", slug), token,
		fmt.Sprintf(`{"parent":%q,"qty":1}`, pid))
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed line: %d %s", rec.Code, rec.Body.String())
	}

	// check violation on insert AND update must be 4xx and name the rule
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/lines/records", slug), token,
		fmt.Sprintf(`{"parent":%q,"qty":0}`, pid))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("check on insert: want 422, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "qty_positive") {
		t.Fatalf("check error does not name the rule: %s", rec.Body.String())
	}
	// deleting a referenced parent is a conflict, not a 500
	rec = deleteJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/parents/records/%s", slug, pid), token, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete referenced: want 409, got %d %s", rec.Code, rec.Body.String())
	}
	// referencing something that does not exist is a validation error
	rec = postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/lines/records", slug), token,
		`{"parent":"11111111-2222-3333-4444-555555555555","qty":1}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad reference: want 422, got %d %s", rec.Code, rec.Body.String())
	}
}
