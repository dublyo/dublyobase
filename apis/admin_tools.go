package apis

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

type collectionImportRequest struct {
	Items             []core.CollectionSchemaItem `json:"items"`
	Collections       []core.CollectionSchemaItem `json:"collections"`
	Mode              string                      `json:"mode"`
	DryRun            *bool                       `json:"dryRun"`
	DropMissingFields bool                        `json:"dropMissingFields"`
}

func (s *server) adminExportCollections(w http.ResponseWriter, r *http.Request) {
	result, err := core.ExportCollections(r.Context(), s.app.Pool, r.PathValue("slug"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) adminImportCollections(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	raw, ok := decodeRawJSON(w, r, 4<<20)
	if !ok {
		return
	}
	input, err := parseCollectionImport(raw)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	result, err := core.ImportCollections(
		r.Context(),
		s.app.Pool,
		auth.Admin.ID,
		r.PathValue("slug"),
		input,
		s.clientIP(r),
		r.UserAgent(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) adminRunSQL(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	var req core.AdminSQLInput
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := core.ExecuteAdminSQL(
		r.Context(),
		s.app.Pool,
		auth.Admin.ID,
		r.PathValue("slug"),
		req,
		s.clientIP(r),
		r.UserAgent(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func decodeRawJSON(w http.ResponseWriter, r *http.Request, maxBytes int64) (json.RawMessage, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return nil, false
	}
	raw := json.RawMessage(body)
	if !json.Valid(raw) {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return nil, false
	}
	return raw, true
}

func parseCollectionImport(raw json.RawMessage) (core.CollectionImportInput, error) {
	var direct []core.CollectionSchemaItem
	if err := json.Unmarshal(raw, &direct); err == nil && direct != nil {
		return core.CollectionImportInput{Items: direct, Mode: core.CollectionImportCreateMissing, DryRun: true}, nil
	}

	var req collectionImportRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return core.CollectionImportInput{}, fmt.Errorf("%w: invalid collection import JSON", core.ErrValidation)
	}
	items := req.Items
	if len(items) == 0 && len(req.Collections) > 0 {
		items = req.Collections
	}
	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}
	if len(items) == 0 {
		return core.CollectionImportInput{}, fmt.Errorf("%w: import JSON must include items or collections", core.ErrValidation)
	}
	if req.Mode == "" {
		req.Mode = core.CollectionImportCreateMissing
	}
	if req.DryRun == nil && !looksLikeDublyobaseExport(raw) {
		return core.CollectionImportInput{}, fmt.Errorf("%w: dryRun must be explicit for custom import envelopes", core.ErrValidation)
	}
	return core.CollectionImportInput{
		Items:             items,
		Mode:              req.Mode,
		DryRun:            dryRun,
		DropMissingFields: req.DropMissingFields,
	}, nil
}

func looksLikeDublyobaseExport(raw json.RawMessage) bool {
	var envelope struct {
		Project    string          `json:"project"`
		ExportedAt json.RawMessage `json:"exportedAt"`
		Items      json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return false
	}
	return envelope.Project != "" && envelope.ExportedAt != nil && envelope.Items != nil
}
