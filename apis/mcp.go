package apis

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dublyo/dublyobase/core"
	"github.com/jackc/pgx/v5/pgxpool"
)

const mcpProtocolVersion = "2025-06-18"

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type mcpToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content []mcpToolContent `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

func (s *server) mcp(w http.ResponseWriter, r *http.Request) {
	token, err := core.FindMCPToken(r.Context(), s.app.Pool, bearerToken(r), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req mcpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCPError(w, nil, -32700, "parse error")
		return
	}
	if req.JSONRPC != "2.0" {
		writeMCPError(w, req.ID, -32600, "invalid JSON-RPC version")
		return
	}

	switch req.Method {
	case "initialize":
		writeMCPResult(w, req.ID, map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{"name": "Dublyobase", "version": core.Version},
		})
	case "notifications/initialized":
		writeMCPResult(w, req.ID, map[string]any{})
	case "ping":
		writeMCPResult(w, req.ID, map[string]any{})
	case "tools/list":
		writeMCPResult(w, req.ID, map[string]any{"tools": mcpToolsForToken(token)})
	case "tools/call":
		s.handleMCPToolCall(w, r, token, req)
	default:
		writeMCPError(w, req.ID, -32601, "method not found")
	}
}

func (s *server) handleMCPToolCall(w http.ResponseWriter, r *http.Request, token *core.MCPToken, req mcpRequest) {
	var params mcpCallParams
	if len(req.Params) == 0 {
		writeMCPError(w, req.ID, -32602, "missing tool call params")
		return
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || strings.TrimSpace(params.Name) == "" {
		writeMCPError(w, req.ID, -32602, "invalid tool call params")
		return
	}
	name := strings.ToLower(strings.TrimSpace(params.Name))
	if !token.Allows(name) {
		writeMCPResult(w, req.ID, mcpTextResult(fmt.Sprintf("tool %q is not allowed for this token", name), true))
		return
	}

	result, projectSlug, callErr := s.callMCPTool(r.Context(), token, name, params.Arguments, s.clientIP(r), r.UserAgent())
	status := "success"
	if callErr != nil {
		status = "error"
	}
	_ = core.InsertAudit(r.Context(), s.app.Pool, core.AuditEvent{
		AdminID:    token.CreatedByAdminID,
		Action:     "mcp.tool.call",
		TargetType: "mcp_tool",
		TargetID:   name,
		IP:         s.clientIP(r),
		UserAgent:  r.UserAgent(),
		Data: map[string]any{
			"scope":       token.Scope,
			"project":     projectSlug,
			"tokenPrefix": token.Prefix,
			"status":      status,
		},
	})
	if callErr != nil {
		writeMCPResult(w, req.ID, mcpTextResult(callErr.Error(), true))
		return
	}
	writeMCPResult(w, req.ID, mcpJSONResult(result))
}

func (s *server) callMCPTool(ctx context.Context, token *core.MCPToken, name string, rawArgs json.RawMessage, ip string, userAgent string) (any, string, error) {
	args, err := mcpArgs(rawArgs)
	if err != nil {
		return nil, "", err
	}

	switch name {
	case "projects.list":
		if token.Scope != core.MCPAdminScope {
			return nil, token.ProjectSlug, core.ErrUnauthorized
		}
		projects, err := core.ListProjects(ctx, s.app.Pool)
		return map[string]any{"items": projects}, "", err
	case "projects.create":
		if token.Scope != core.MCPAdminScope {
			return nil, token.ProjectSlug, core.ErrUnauthorized
		}
		adminID, err := mcpAdminID(token)
		if err != nil {
			return nil, "", err
		}
		var input struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		}
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		project, err := core.ProvisionProject(ctx, s.app.Pool, adminID, input.Slug, input.Name, ip, userAgent)
		if project != nil {
			return project, project.Slug, err
		}
		return nil, "", err
	case "collections.list":
		projectSlug, err := mcpProjectSlug(token, args)
		if err != nil {
			return nil, "", err
		}
		items, err := core.ListCollections(ctx, s.app.Pool, projectSlug)
		return map[string]any{"items": items}, projectSlug, err
	case "collections.create":
		adminID, err := mcpAdminID(token)
		if err != nil {
			return nil, "", err
		}
		var input struct {
			ProjectSlug string              `json:"projectSlug"`
			Name        string              `json:"name"`
			Type        core.CollectionType `json:"type"`
			Fields      []core.Field        `json:"fields"`
			ListRule    *string             `json:"listRule"`
			ViewRule    *string             `json:"viewRule"`
			CreateRule  *string             `json:"createRule"`
			UpdateRule  *string             `json:"updateRule"`
			DeleteRule  *string             `json:"deleteRule"`
			Options     json.RawMessage     `json:"options"`
		}
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		projectSlug, err := mcpProjectSlugValue(token, input.ProjectSlug)
		if err != nil {
			return nil, "", err
		}
		collection, err := core.CreateCollection(ctx, s.app.Pool, adminID, projectSlug, core.CollectionInput{
			Name:       input.Name,
			Type:       input.Type,
			Fields:     input.Fields,
			ListRule:   input.ListRule,
			ViewRule:   input.ViewRule,
			CreateRule: input.CreateRule,
			UpdateRule: input.UpdateRule,
			DeleteRule: input.DeleteRule,
			Options:    input.Options,
		}, ip, userAgent)
		return collection, projectSlug, err
	case "collections.update":
		adminID, err := mcpAdminID(token)
		if err != nil {
			return nil, "", err
		}
		var input struct {
			ProjectSlug       string       `json:"projectSlug"`
			Collection        string       `json:"collection"`
			NewName           *string      `json:"newName"`
			Fields            []core.Field `json:"fields"`
			DropMissingFields bool         `json:"dropMissingFields"`
			ListRule          *string      `json:"listRule"`
			ViewRule          *string      `json:"viewRule"`
			CreateRule        *string      `json:"createRule"`
			UpdateRule        *string      `json:"updateRule"`
			DeleteRule        *string      `json:"deleteRule"`
			Options           json.RawMessage
		}
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		projectSlug, err := mcpProjectSlugValue(token, input.ProjectSlug)
		if err != nil {
			return nil, "", err
		}
		_, fieldsSet := args["fields"]
		collection, err := core.UpdateCollection(ctx, s.app.Pool, adminID, projectSlug, input.Collection, core.CollectionUpdateInput{
			Name:              input.NewName,
			Fields:            input.Fields,
			FieldsSet:         fieldsSet,
			DropMissingFields: input.DropMissingFields,
			ListRule:          input.ListRule,
			ViewRule:          input.ViewRule,
			CreateRule:        input.CreateRule,
			UpdateRule:        input.UpdateRule,
			DeleteRule:        input.DeleteRule,
			Options:           input.Options,
		}, ip, userAgent)
		return collection, projectSlug, err
	case "schema.discover":
		var input struct {
			ProjectSlug string `json:"projectSlug"`
			Schema      string `json:"schema"`
			Table       string `json:"table"`
		}
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		projectSlug, err := mcpProjectSlugValue(token, input.ProjectSlug)
		if err != nil {
			return nil, "", err
		}
		result, err := core.DiscoverSchemaTables(ctx, s.app.Pool, projectSlug, core.SchemaDiscoveryInput{Schema: input.Schema, Table: input.Table})
		return result, projectSlug, err
	case "schema.import":
		adminID, err := mcpAdminID(token)
		if err != nil {
			return nil, "", err
		}
		var input struct {
			ProjectSlug string                  `json:"projectSlug"`
			Items       []core.SchemaImportItem `json:"items"`
			DryRun      bool                    `json:"dryRun"`
		}
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		projectSlug, err := mcpProjectSlugValue(token, input.ProjectSlug)
		if err != nil {
			return nil, "", err
		}
		result, err := core.ImportSchemaTables(ctx, s.app.Pool, adminID, projectSlug, core.SchemaImportInput{Items: input.Items, DryRun: input.DryRun}, ip, userAgent)
		return result, projectSlug, err
	case "records.list":
		var input struct {
			ProjectSlug string `json:"projectSlug"`
			Collection  string `json:"collection"`
			Page        int    `json:"page"`
			PerPage     int    `json:"perPage"`
			Offset      int    `json:"offset"`
			Sort        string `json:"sort"`
			Filter      string `json:"filter"`
			Search      string `json:"search"`
			Fields      string `json:"fields"`
		}
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		projectSlug, err := mcpProjectSlugValue(token, input.ProjectSlug)
		if err != nil {
			return nil, "", err
		}
		project, err := core.GetProject(ctx, s.app.Pool, projectSlug)
		if err != nil {
			return nil, projectSlug, err
		}
		result, err := core.ListRecords(ctx, s.app.Pool, core.ServiceRecordAuth(project), input.Collection, core.RecordListOptions{
			Page: input.Page, PerPage: input.PerPage, Offset: input.Offset, Sort: input.Sort, Filter: input.Filter, Search: input.Search, Fields: input.Fields,
		})
		return result, projectSlug, err
	case "records.create":
		return s.mcpWriteRecord(ctx, token, rawArgs, "create")
	case "records.update":
		return s.mcpWriteRecord(ctx, token, rawArgs, "update")
	case "records.delete":
		var input struct {
			ProjectSlug string `json:"projectSlug"`
			Collection  string `json:"collection"`
			ID          string `json:"id"`
		}
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		projectSlug, err := mcpProjectSlugValue(token, input.ProjectSlug)
		if err != nil {
			return nil, "", err
		}
		project, err := core.GetProject(ctx, s.app.Pool, projectSlug)
		if err != nil {
			return nil, projectSlug, err
		}
		deleted, err := core.DeleteRecord(ctx, s.app.Pool, core.ServiceRecordAuth(project), input.Collection, input.ID)
		return deleted, projectSlug, err
	case "files.upload_base64":
		return s.mcpUploadBase64(ctx, token, rawArgs)
	case "users.create":
		var input struct {
			ProjectSlug string `json:"projectSlug"`
			Email       string `json:"email"`
			Password    string `json:"password"`
		}
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		projectSlug, err := mcpProjectSlugValue(token, input.ProjectSlug)
		if err != nil {
			return nil, "", err
		}
		user, err := core.SignupAppUser(ctx, s.app.Pool, s.app.Config, projectSlug, input.Email, input.Password, ip, userAgent, time.Now())
		return user, projectSlug, err
	case "settings.smtp.update":
		if token.Scope != core.MCPAdminScope {
			return nil, token.ProjectSlug, core.ErrUnauthorized
		}
		adminID, err := mcpAdminID(token)
		if err != nil {
			return nil, "", err
		}
		var input core.SMTPSettingsInput
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		settings, err := core.UpdateSMTPSettings(ctx, s.app.Pool, s.app.Config, adminID, input, ip, userAgent)
		return settings, "", err
	case "settings.storage.update":
		if token.Scope != core.MCPAdminScope {
			return nil, token.ProjectSlug, core.ErrUnauthorized
		}
		adminID, err := mcpAdminID(token)
		if err != nil {
			return nil, "", err
		}
		var input core.StorageSettingsInput
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		settings, err := core.UpdateStorageSettings(ctx, s.app.Pool, s.app.Config, adminID, input, ip, userAgent)
		return settings, "", err
	case "settings.storage.test":
		if token.Scope != core.MCPAdminScope {
			return nil, token.ProjectSlug, core.ErrUnauthorized
		}
		cfg, err := core.EffectiveStorageConfig(ctx, s.app.Pool, s.app.Config)
		if err != nil {
			return nil, "", err
		}
		if err := core.TestObjectStore(ctx, cfg); err != nil {
			return nil, "", err
		}
		return map[string]string{"status": "ok"}, "", nil
	case "cron.list":
		jobs, err := core.ListCronJobs(ctx, s.app.Pool)
		return map[string]any{"items": jobs}, "", err
	case "cron.create":
		adminID, err := mcpAdminID(token)
		if err != nil {
			return nil, "", err
		}
		var input core.CronJobInput
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		if token.Scope == core.MCPProjectScope {
			input.ProjectSlug = token.ProjectSlug
		}
		_, enabledSet := args["enabled"]
		input.EnabledProvided = enabledSet
		job, err := core.CreateCronJob(ctx, s.app.Pool, adminID, input, ip, userAgent)
		return job, input.ProjectSlug, err
	case "cron.run":
		adminID, err := mcpAdminID(token)
		if err != nil {
			return nil, "", err
		}
		var input struct {
			ID string `json:"id"`
		}
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		run, err := core.RunCronJob(ctx, s.app.Pool, adminID, input.ID, ip, userAgent)
		return run, "", err
	case "backups.list":
		jobs, err := core.ListBackupJobs(ctx, s.app.Pool)
		if err != nil {
			return nil, token.ProjectSlug, err
		}
		if token.Scope == core.MCPProjectScope {
			filtered := make([]core.BackupJob, 0)
			for _, job := range jobs {
				if job.Scope == "project" && job.ProjectSlug == token.ProjectSlug {
					filtered = append(filtered, job)
				}
			}
			return map[string]any{"items": filtered}, token.ProjectSlug, nil
		}
		return map[string]any{"items": jobs}, "", nil
	case "backups.create":
		adminID, err := mcpAdminID(token)
		if err != nil {
			return nil, "", err
		}
		var input core.BackupJobInput
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		if token.Scope == core.MCPProjectScope {
			input.Scope = "project"
			input.ProjectSlug = token.ProjectSlug
		}
		_, enabledSet := args["enabled"]
		input.EnabledProvided = enabledSet
		job, err := core.CreateBackupJob(ctx, s.app.Pool, adminID, input, ip, userAgent)
		return job, input.ProjectSlug, err
	case "backups.run":
		adminID, err := mcpAdminID(token)
		if err != nil {
			return nil, "", err
		}
		var input struct {
			ID string `json:"id"`
		}
		if err := decodeMCPArgs(rawArgs, &input); err != nil {
			return nil, "", err
		}
		if token.Scope == core.MCPProjectScope {
			ok, err := mcpProjectCanUseBackup(ctx, s.app.Pool, token.ProjectSlug, input.ID)
			if err != nil {
				return nil, token.ProjectSlug, err
			}
			if !ok {
				return nil, token.ProjectSlug, core.ErrUnauthorized
			}
		}
		cfg, err := core.EffectiveStorageConfig(ctx, s.app.Pool, s.app.Config)
		if err != nil {
			return nil, token.ProjectSlug, err
		}
		run, err := core.RunBackupJob(ctx, s.app.Pool, cfg, adminID, input.ID, ip, userAgent)
		return run, token.ProjectSlug, err
	default:
		return nil, "", fmt.Errorf("%w: unsupported MCP tool %q", core.ErrValidation, name)
	}
}

func (s *server) mcpWriteRecord(ctx context.Context, token *core.MCPToken, rawArgs json.RawMessage, mode string) (any, string, error) {
	var input struct {
		ProjectSlug string                     `json:"projectSlug"`
		Collection  string                     `json:"collection"`
		ID          string                     `json:"id"`
		Data        map[string]json.RawMessage `json:"data"`
	}
	if err := decodeMCPArgs(rawArgs, &input); err != nil {
		return nil, "", err
	}
	projectSlug, err := mcpProjectSlugValue(token, input.ProjectSlug)
	if err != nil {
		return nil, "", err
	}
	project, err := core.GetProject(ctx, s.app.Pool, projectSlug)
	if err != nil {
		return nil, projectSlug, err
	}
	auth := core.ServiceRecordAuth(project)
	if mode == "create" {
		record, err := core.CreateRecord(ctx, s.app.Pool, auth, input.Collection, input.Data)
		return record, projectSlug, err
	}
	record, err := core.UpdateRecord(ctx, s.app.Pool, auth, input.Collection, input.ID, input.Data)
	return record, projectSlug, err
}

func (s *server) mcpUploadBase64(ctx context.Context, token *core.MCPToken, rawArgs json.RawMessage) (any, string, error) {
	var input struct {
		ProjectSlug string `json:"projectSlug"`
		Collection  string `json:"collection"`
		RecordID    string `json:"recordId"`
		Field       string `json:"field"`
		Filename    string `json:"filename"`
		DataBase64  string `json:"dataBase64"`
		Mode        string `json:"mode"`
	}
	if err := decodeMCPArgs(rawArgs, &input); err != nil {
		return nil, "", err
	}
	projectSlug, err := mcpProjectSlugValue(token, input.ProjectSlug)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(input.Filename) == "" || strings.TrimSpace(input.DataBase64) == "" {
		return nil, projectSlug, fmt.Errorf("%w: filename and dataBase64 are required", core.ErrValidation)
	}
	project, err := core.GetProject(ctx, s.app.Pool, projectSlug)
	if err != nil {
		return nil, projectSlug, err
	}
	auth := core.ServiceRecordAuth(project)
	if err := core.AuthorizeFileUpload(ctx, s.app.Pool, auth, input.Collection, input.RecordID, input.Field); err != nil {
		return nil, projectSlug, err
	}
	data, err := decodeBase64Data(input.DataBase64)
	if err != nil {
		return nil, projectSlug, err
	}
	storageCfg, err := core.EffectiveStorageConfig(ctx, s.app.Pool, s.app.Config)
	if err != nil {
		return nil, projectSlug, err
	}
	meta, err := core.StoreUploadedFile(ctx, storageCfg, projectSlug, input.Collection, input.RecordID, input.Field, input.Filename, bytes.NewReader(data))
	if err != nil {
		return nil, projectSlug, err
	}
	record, removed, err := core.UpdateRecordFileField(ctx, s.app.Pool, auth, input.Collection, input.RecordID, input.Field, input.Mode, []core.FileMeta{meta})
	if err != nil {
		_ = core.RemoveStoredFiles(ctx, storageCfg, []core.FileMeta{meta})
		return nil, projectSlug, err
	}
	_ = core.RemoveStoredFiles(ctx, storageCfg, removed)
	return map[string]any{"record": record, "file": meta}, projectSlug, nil
}

func mcpProjectCanUseBackup(ctx context.Context, pool *pgxpool.Pool, projectSlug string, jobID string) (bool, error) {
	// This narrow helper is intentionally read-only; project MCP tokens must
	// not be able to trigger full-instance or cross-project backups.
	rows, err := pool.Query(ctx, `
		select id
		from _dbo.backup_jobs b
		join _dbo.projects p on p.id = b.project_id
		where b.id = $1 and b.scope = 'project' and p.slug = $2`, jobID, projectSlug)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

func mcpAdminID(token *core.MCPToken) (string, error) {
	if token.CreatedByAdminID == nil || strings.TrimSpace(*token.CreatedByAdminID) == "" {
		return "", fmt.Errorf("%w: MCP token is missing its creating admin", core.ErrValidation)
	}
	return *token.CreatedByAdminID, nil
}

func mcpArgs(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("%w: MCP arguments must be an object", core.ErrValidation)
	}
	if args == nil {
		args = map[string]json.RawMessage{}
	}
	return args, nil
}

func decodeMCPArgs(raw json.RawMessage, out any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte(`{}`)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: invalid MCP arguments", core.ErrValidation)
	}
	return nil
}

func mcpProjectSlug(token *core.MCPToken, args map[string]json.RawMessage) (string, error) {
	var projectSlug string
	if raw, ok := args["projectSlug"]; ok {
		_ = json.Unmarshal(raw, &projectSlug)
	}
	return mcpProjectSlugValue(token, projectSlug)
}

func mcpProjectSlugValue(token *core.MCPToken, projectSlug string) (string, error) {
	if token.Scope == core.MCPProjectScope {
		return token.ProjectSlug, nil
	}
	projectSlug = core.NormalizeProjectSlug(projectSlug)
	if projectSlug == "" {
		return "", fmt.Errorf("%w: projectSlug is required", core.ErrValidation)
	}
	return projectSlug, core.ValidateProjectSlug(projectSlug)
}

func decodeBase64Data(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if before, after, ok := strings.Cut(raw, ","); ok && strings.Contains(before, ";base64") {
		raw = after
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err == nil {
		return data, nil
	}
	data, err = base64.RawStdEncoding.DecodeString(raw)
	if err == nil {
		return data, nil
	}
	return nil, fmt.Errorf("%w: dataBase64 is invalid", core.ErrValidation)
}

func mcpJSONResult(v any) mcpToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		data = []byte(`{"status":"ok"}`)
	}
	return mcpTextResult(string(data), false)
}

func mcpTextResult(text string, isError bool) mcpToolResult {
	return mcpToolResult{
		Content: []mcpToolContent{{Type: "text", Text: text}},
		IsError: isError,
	}
}

func writeMCPResult(w http.ResponseWriter, id json.RawMessage, result any) {
	if len(id) == 0 {
		id = []byte("null")
	}
	writeJSON(w, http.StatusOK, mcpResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeMCPError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = []byte("null")
	}
	writeJSON(w, http.StatusOK, mcpResponse{JSONRPC: "2.0", ID: id, Error: &mcpError{Code: code, Message: message}})
}

func mcpToolsForToken(token *core.MCPToken) []mcpTool {
	all := map[string]mcpTool{
		"projects.list":           mcpToolDef("projects.list", "List Dublyobase projects.", map[string]any{}),
		"projects.create":         mcpToolDef("projects.create", "Create a new project/schema.", mcpObjectSchema([]string{"slug", "name"}, map[string]any{"slug": stringSchema(), "name": stringSchema()})),
		"collections.list":        mcpToolDef("collections.list", "List collections for a project.", projectSchema()),
		"collections.create":      mcpToolDef("collections.create", "Create a collection and its Postgres table.", collectionCreateSchema()),
		"collections.update":      mcpToolDef("collections.update", "Update fields or rules for a collection.", collectionUpdateSchema()),
		"schema.discover":         mcpToolDef("schema.discover", "Discover existing non-system Postgres tables and import readiness.", schemaDiscoverSchema()),
		"schema.import":           mcpToolDef("schema.import", "Import existing primary-key tables as Dublyobase collections.", schemaImportSchema()),
		"records.list":            mcpToolDef("records.list", "List records using service-role access for the scoped project.", recordsListSchema()),
		"records.create":          mcpToolDef("records.create", "Create a record.", recordWriteSchema(false)),
		"records.update":          mcpToolDef("records.update", "Update a record.", recordWriteSchema(true)),
		"records.delete":          mcpToolDef("records.delete", "Delete a record.", mcpObjectSchema([]string{"collection", "id"}, map[string]any{"projectSlug": stringSchema(), "collection": stringSchema(), "id": stringSchema()})),
		"files.upload_base64":     mcpToolDef("files.upload_base64", "Upload one base64 file to a file field.", fileUploadSchema()),
		"users.create":            mcpToolDef("users.create", "Create an app auth user.", mcpObjectSchema([]string{"email", "password"}, map[string]any{"projectSlug": stringSchema(), "email": stringSchema(), "password": stringSchema()})),
		"settings.smtp.update":    mcpToolDef("settings.smtp.update", "Update instance SMTP settings.", smtpSettingsSchema()),
		"settings.storage.update": mcpToolDef("settings.storage.update", "Update instance storage settings.", storageSettingsSchema()),
		"settings.storage.test":   mcpToolDef("settings.storage.test", "Run a storage write/read/delete test.", map[string]any{}),
		"cron.list":               mcpToolDef("cron.list", "List HTTP cron jobs.", map[string]any{}),
		"cron.create":             mcpToolDef("cron.create", "Create an HTTP cron job.", cronCreateSchema()),
		"cron.run":                mcpToolDef("cron.run", "Run a cron job now.", idSchema()),
		"backups.list":            mcpToolDef("backups.list", "List backup jobs.", map[string]any{}),
		"backups.create":          mcpToolDef("backups.create", "Create a full or project pg_dump backup job.", backupCreateSchema()),
		"backups.run":             mcpToolDef("backups.run", "Run a backup job now.", idSchema()),
	}
	out := make([]mcpTool, 0, len(token.AllowedTools))
	for _, name := range core.DefaultMCPTools(core.MCPAdminScope) {
		if token.Allows(name) {
			out = append(out, all[name])
		}
	}
	for _, name := range core.DefaultMCPTools(core.MCPProjectScope) {
		if token.Allows(name) {
			if _, exists := all[name]; exists && !mcpHasTool(out, name) {
				out = append(out, all[name])
			}
		}
	}
	return out
}

func mcpHasTool(tools []mcpTool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func mcpToolDef(name string, description string, schema map[string]any) mcpTool {
	if schema == nil || len(schema) == 0 {
		schema = mcpObjectSchema(nil, map[string]any{})
	}
	return mcpTool{Name: name, Description: description, InputSchema: schema}
}

func mcpObjectSchema(required []string, props map[string]any) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema() map[string]any { return map[string]any{"type": "string"} }

func integerSchema() map[string]any { return map[string]any{"type": "integer"} }

func booleanSchema() map[string]any { return map[string]any{"type": "boolean"} }

func objectSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": true}
}

func projectSchema() map[string]any {
	return mcpObjectSchema(nil, map[string]any{"projectSlug": stringSchema()})
}

func idSchema() map[string]any {
	return mcpObjectSchema([]string{"id"}, map[string]any{"id": stringSchema()})
}

func collectionCreateSchema() map[string]any {
	return mcpObjectSchema([]string{"name", "fields"}, map[string]any{
		"projectSlug": stringSchema(),
		"name":        stringSchema(),
		"type":        stringSchema(),
		"fields":      map[string]any{"type": "array", "items": objectSchema()},
		"listRule":    stringSchema(),
		"viewRule":    stringSchema(),
		"createRule":  stringSchema(),
		"updateRule":  stringSchema(),
		"deleteRule":  stringSchema(),
		"options":     objectSchema(),
	})
}

func collectionUpdateSchema() map[string]any {
	return mcpObjectSchema([]string{"collection"}, map[string]any{
		"projectSlug":       stringSchema(),
		"collection":        stringSchema(),
		"newName":           stringSchema(),
		"fields":            map[string]any{"type": "array", "items": objectSchema()},
		"dropMissingFields": booleanSchema(),
		"listRule":          stringSchema(),
		"viewRule":          stringSchema(),
		"createRule":        stringSchema(),
		"updateRule":        stringSchema(),
		"deleteRule":        stringSchema(),
		"options":           objectSchema(),
	})
}

func schemaDiscoverSchema() map[string]any {
	return mcpObjectSchema(nil, map[string]any{
		"projectSlug": stringSchema(),
		"schema":      stringSchema(),
		"table":       stringSchema(),
	})
}

func schemaImportSchema() map[string]any {
	return mcpObjectSchema([]string{"items"}, map[string]any{
		"projectSlug": stringSchema(),
		"dryRun":      booleanSchema(),
		"items": map[string]any{
			"type": "array",
			"items": mcpObjectSchema([]string{"schema", "table"}, map[string]any{
				"schema": stringSchema(),
				"table":  stringSchema(),
				"name":   stringSchema(),
			}),
		},
	})
}

func recordsListSchema() map[string]any {
	return mcpObjectSchema([]string{"collection"}, map[string]any{
		"projectSlug": stringSchema(),
		"collection":  stringSchema(),
		"page":        integerSchema(),
		"perPage":     integerSchema(),
		"offset":      integerSchema(),
		"sort":        stringSchema(),
		"filter":      stringSchema(),
		"search":      stringSchema(),
		"fields":      stringSchema(),
	})
}

func recordWriteSchema(update bool) map[string]any {
	required := []string{"collection", "data"}
	props := map[string]any{"projectSlug": stringSchema(), "collection": stringSchema(), "data": objectSchema()}
	if update {
		required = []string{"collection", "id", "data"}
		props["id"] = stringSchema()
	}
	return mcpObjectSchema(required, props)
}

func fileUploadSchema() map[string]any {
	return mcpObjectSchema([]string{"collection", "recordId", "field", "filename", "dataBase64"}, map[string]any{
		"projectSlug": stringSchema(),
		"collection":  stringSchema(),
		"recordId":    stringSchema(),
		"field":       stringSchema(),
		"filename":    stringSchema(),
		"dataBase64":  stringSchema(),
		"mode":        stringSchema(),
	})
}

func smtpSettingsSchema() map[string]any {
	return mcpObjectSchema([]string{"enabled"}, map[string]any{
		"enabled":       booleanSchema(),
		"host":          stringSchema(),
		"port":          stringSchema(),
		"username":      stringSchema(),
		"password":      stringSchema(),
		"clearPassword": booleanSchema(),
		"from":          stringSchema(),
	})
}

func storageSettingsSchema() map[string]any {
	return mcpObjectSchema([]string{"type"}, map[string]any{
		"type": stringSchema(),
		"s3": mcpObjectSchema(nil, map[string]any{
			"endpoint":       stringSchema(),
			"bucket":         stringSchema(),
			"region":         stringSchema(),
			"accessKey":      stringSchema(),
			"secretKey":      stringSchema(),
			"clearSecretKey": booleanSchema(),
			"prefix":         stringSchema(),
			"useSSL":         booleanSchema(),
			"forcePathStyle": booleanSchema(),
		}),
	})
}

func cronCreateSchema() map[string]any {
	return mcpObjectSchema([]string{"name", "schedule", "url"}, map[string]any{
		"projectSlug":    stringSchema(),
		"name":           stringSchema(),
		"type":           stringSchema(),
		"schedule":       stringSchema(),
		"timezone":       stringSchema(),
		"enabled":        booleanSchema(),
		"timeoutSeconds": integerSchema(),
		"retryCount":     integerSchema(),
		"method":         stringSchema(),
		"url":            stringSchema(),
		"headers":        objectSchema(),
		"body":           stringSchema(),
	})
}

func backupCreateSchema() map[string]any {
	return mcpObjectSchema([]string{"name", "schedule"}, map[string]any{
		"name":           stringSchema(),
		"scope":          stringSchema(),
		"projectSlug":    stringSchema(),
		"schedule":       stringSchema(),
		"timezone":       stringSchema(),
		"enabled":        booleanSchema(),
		"retentionDays":  integerSchema(),
		"retentionCount": integerSchema(),
	})
}
