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
	rec = postMCP(srv.Handler, mcpToken, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":3,
		"method":"tools/call",
		"params":{
			"name":"collections.create",
			"arguments":{
				"projectSlug":%q,
				"name":"mcp_posts",
				"type":"base",
				"fields":[{"name":"title","type":"text","required":true,"options":{}}]
			}
		}
	}`, projectSlug))
	if rec.Code != http.StatusOK || !strings.Contains(mcpToolText(t, rec.Body.Bytes()), `"mcp_posts"`) {
		t.Fatalf("mcp collection create failed: status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = postMCP(srv.Handler, mcpToken, fmt.Sprintf(`{
		"jsonrpc":"2.0",
		"id":4,
		"method":"tools/call",
		"params":{
			"name":"records.create",
			"arguments":{"projectSlug":%q,"collection":"mcp_posts","data":{"title":"via mcp"}}
		}
	}`, projectSlug))
	if rec.Code != http.StatusOK || !strings.Contains(mcpToolText(t, rec.Body.Bytes()), `"via mcp"`) {
		t.Fatalf("mcp record create failed: status=%d body=%s", rec.Code, rec.Body.String())
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
