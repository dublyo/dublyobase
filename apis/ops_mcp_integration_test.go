package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAdminOpsAndMCP(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	projectSlug := createProjectForCollections(t, srv.Handler, adminToken)

	var cronHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Dublyobase-Test") == "yes" {
			cronHits.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	cronBody := fmt.Sprintf(`{
		"projectSlug": %q,
		"name": "integration ping",
		"type": "http",
		"schedule": "@every 1m",
		"timezone": "UTC",
		"method": "POST",
		"url": %q,
		"headers": {"X-Dublyobase-Test": "yes"},
		"body": "ping"
	}`, projectSlug, target.URL)
	rec := postJSON(srv.Handler, "/admin/api/cron-jobs", adminToken, cronBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create cron status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cron struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cron); err != nil || cron.ID == "" {
		t.Fatalf("bad cron response: %v %s", err, rec.Body.String())
	}
	rec = postJSON(srv.Handler, "/admin/api/cron-jobs/"+cron.ID+"/run", adminToken, `{}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("run cron status=%d body=%s", rec.Code, rec.Body.String())
	}
	if cronHits.Load() != 1 {
		t.Fatalf("cron target hits=%d, want 1", cronHits.Load())
	}

	backupBody := fmt.Sprintf(`{
		"name": "integration project backup",
		"scope": "project",
		"projectSlug": %q,
		"schedule": "0 2 * * *",
		"timezone": "UTC"
	}`, projectSlug)
	rec = postJSON(srv.Handler, "/admin/api/backups", adminToken, backupBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create backup status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = postJSON(srv.Handler, "/admin/api/mcp/tokens", adminToken, `{
		"scope": "admin",
		"name": "integration admin",
		"allowedTools": ["projects.list", "collections.list", "collections.create", "records.create", "records.list"]
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create mcp status=%d body=%s", rec.Code, rec.Body.String())
	}
	mcpToken := decodeToken(t, rec.Body.Bytes())

	rec = postMCP(srv.Handler, mcpToken, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"protocolVersion"`) {
		t.Fatalf("mcp initialize failed: status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = postMCP(srv.Handler, mcpToken, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"collections.create"`) {
		t.Fatalf("mcp tools/list failed: status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = postMCPTool(srv.Handler, mcpToken, 3, "collections.create", map[string]any{
		"projectSlug": projectSlug,
		"name":        "mcp_posts",
		"type":        "base",
		"options":     map[string]any{"icon": map[string]any{"type": "lucide", "name": "book-open"}},
		"fields": []map[string]any{
			{"name": "title", "type": "text", "required": true, "searchable": true, "options": map[string]any{"min": 1, "max": 120}},
			{"name": "rich_body", "type": "editor", "options": map[string]any{"maxSize": 2048}},
			{"name": "secret", "type": "password", "options": map[string]any{"min": 8, "cost": 4}},
			{"name": "views", "type": "number", "searchable": true, "options": map[string]any{"onlyInt": true, "min": 0, "max": 10000}},
			{"name": "active", "type": "bool", "searchable": true},
			{"name": "launch_at", "type": "date", "searchable": true},
			{"name": "published_at", "type": "autodate", "options": map[string]any{"onCreate": true}},
			{"name": "contact", "type": "email", "options": map[string]any{"onlyDomains": []string{"example.com"}}},
			{"name": "website", "type": "url", "options": map[string]any{"max": 200}},
			{"name": "status", "type": "select", "searchable": true, "options": map[string]any{"values": []string{"draft", "live"}}},
			{"name": "payload", "type": "json", "options": map[string]any{"maxSize": 2048}},
			{"name": "owner", "type": "relation", "options": map[string]any{"collection": "users"}},
			{"name": "attachment", "type": "file", "options": map[string]any{"multiple": true, "maxSelect": 2, "maxSize": 1024, "mimeTypes": []string{"text/plain"}}},
		},
	})
	createdCollection := mcpToolJSON[mcpCollectionResponse](t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || createdCollection.Name != "mcp_posts" || len(createdCollection.Fields) != 13 {
		t.Fatalf("mcp collection create failed: status=%d body=%s", rec.Code, rec.Body.String())
	}

	owner := signupAppUserForTest(t, srv.Handler, projectSlug, "mcp-owner@example.com")
	for i := 1; i <= 12; i++ {
		status := "draft"
		if i%2 == 0 {
			status = "live"
		}
		rec = postMCPTool(srv.Handler, mcpToken, 10+i, "records.create", map[string]any{
			"projectSlug": projectSlug,
			"collection":  "mcp_posts",
			"data": map[string]any{
				"title":     fmt.Sprintf("via mcp %02d", i),
				"rich_body": fmt.Sprintf("<p>Body %d</p>", i),
				"secret":    fmt.Sprintf("password-%02d", i),
				"views":     i,
				"active":    i%2 == 0,
				"launch_at": "2026-07-04T12:00:00Z",
				"contact":   fmt.Sprintf("user%02d@example.com", i),
				"website":   fmt.Sprintf("https://example.com/posts/%d", i),
				"status":    status,
				"payload":   map[string]any{"index": i},
				"owner":     owner.User.ID,
			},
		})
		if rec.Code != http.StatusOK || !strings.Contains(mcpToolText(t, rec.Body.Bytes()), fmt.Sprintf(`"via mcp %02d"`, i)) {
			t.Fatalf("mcp record create %d failed: status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}

	rec = postMCPTool(srv.Handler, mcpToken, 40, "records.list", map[string]any{
		"projectSlug": projectSlug,
		"collection":  "mcp_posts",
		"page":        1,
		"perPage":     10,
		"sort":        "views",
	})
	firstPage := mcpToolJSON[recordListResponse](t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || firstPage.Page != 1 || firstPage.PerPage != 10 || firstPage.TotalItems != 12 || len(firstPage.Items) != 10 {
		t.Fatalf("mcp first page = %+v status=%d body=%s", firstPage, rec.Code, rec.Body.String())
	}
	rec = postMCPTool(srv.Handler, mcpToken, 41, "records.list", map[string]any{
		"projectSlug": projectSlug,
		"collection":  "mcp_posts",
		"page":        2,
		"perPage":     10,
		"sort":        "views",
	})
	secondPage := mcpToolJSON[recordListResponse](t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || secondPage.Page != 2 || secondPage.PerPage != 10 || secondPage.TotalItems != 12 || len(secondPage.Items) != 2 {
		t.Fatalf("mcp second page = %+v status=%d body=%s", secondPage, rec.Code, rec.Body.String())
	}
	rec = postMCPTool(srv.Handler, mcpToken, 42, "records.list", map[string]any{
		"projectSlug": projectSlug,
		"collection":  "mcp_posts",
		"perPage":     999,
	})
	cappedPage := mcpToolJSON[recordListResponse](t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || cappedPage.PerPage != 500 || cappedPage.TotalItems != 12 || len(cappedPage.Items) != 12 {
		t.Fatalf("mcp capped page = %+v status=%d body=%s", cappedPage, rec.Code, rec.Body.String())
	}

	rec = postJSON(srv.Handler, "/admin/api/mcp/tokens", adminToken, fmt.Sprintf(`{
		"scope": "project",
		"projectSlug": %q,
		"name": "integration project"
	}`, projectSlug))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project mcp status=%d body=%s", rec.Code, rec.Body.String())
	}
	projectMCPToken := decodeToken(t, rec.Body.Bytes())
	rec = postMCP(srv.Handler, projectMCPToken, `{"jsonrpc":"2.0","id":5,"method":"tools/list","params":{}}`)
	if strings.Contains(rec.Body.String(), `"projects.list"`) || strings.Contains(rec.Body.String(), `"settings.storage.update"`) {
		t.Fatalf("project scoped token exposed admin tools: %s", rec.Body.String())
	}
}

func postMCP(handler http.Handler, token string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func postMCPTool(handler http.Handler, token string, id int, name string, args any) *httptest.ResponseRecorder {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	})
	if err != nil {
		panic(err)
	}
	return postMCP(handler, token, string(body))
}

func decodeToken(t *testing.T, body []byte) string {
	t.Helper()
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Token == "" {
		t.Fatalf("bad token response: %v %s", err, string(body))
	}
	return out.Token
}

type mcpCollectionResponse struct {
	Name   string `json:"name"`
	Fields []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"fields"`
}

type recordListResponse struct {
	Items      []map[string]any `json:"items"`
	Page       int              `json:"page"`
	PerPage    int              `json:"perPage"`
	TotalItems int              `json:"totalItems"`
}

func mcpToolJSON[T any](t *testing.T, body []byte) T {
	t.Helper()
	var out T
	text := mcpToolText(t, body)
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("bad mcp tool JSON: %v text=%s", err, text)
	}
	return out
}

func mcpToolText(t *testing.T, body []byte) string {
	t.Helper()
	var out struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("bad mcp response: %v %s", err, string(body))
	}
	if out.Result.IsError {
		t.Fatalf("mcp tool returned error: %s", string(body))
	}
	if len(out.Result.Content) == 0 {
		t.Fatalf("mcp response missing content: %s", string(body))
	}
	return out.Result.Content[0].Text
}
