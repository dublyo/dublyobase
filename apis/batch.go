package apis

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dublyo/dublyobase/core"
)

type batchRequest struct {
	Atomic   bool             `json:"atomic"`
	Requests []batchOperation `json:"requests"`
}

type batchOperation struct {
	Method     string                     `json:"method"`
	Collection string                     `json:"collection"`
	ID         string                     `json:"id"`
	Body       map[string]json.RawMessage `json:"body"`
}

type batchOperationResult struct {
	Status int            `json:"status"`
	Body   map[string]any `json:"body,omitempty"`
	Error  string         `json:"error,omitempty"`
}

func (s *server) batchRecords(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	var req batchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Atomic {
		writeError(w, http.StatusBadRequest, "validation_error", "atomic batch is not available yet; send atomic=false")
		return
	}
	if len(req.Requests) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "batch requires at least one request")
		return
	}
	if len(req.Requests) > 50 {
		writeError(w, http.StatusBadRequest, "validation_error", "batch supports at most 50 requests")
		return
	}
	results := make([]batchOperationResult, 0, len(req.Requests))
	for _, op := range req.Requests {
		results = append(results, s.executeBatchOperation(r, auth, op))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": results})
}

func (s *server) executeBatchOperation(r *http.Request, auth *core.RecordAuth, op batchOperation) batchOperationResult {
	op.Method = strings.ToUpper(strings.TrimSpace(op.Method))
	op.Collection = core.NormalizeIdentifier(op.Collection)
	if op.Collection == "" {
		return batchError(http.StatusBadRequest, "collection is required")
	}
	switch op.Method {
	case http.MethodGet:
		if strings.TrimSpace(op.ID) == "" {
			return batchError(http.StatusBadRequest, "id is required for GET")
		}
		record, err := core.GetRecord(r.Context(), s.app.Pool, auth, op.Collection, op.ID)
		if err != nil {
			return batchCoreError(err)
		}
		return batchOK(http.StatusOK, record)
	case http.MethodPost:
		record, err := core.CreateRecord(r.Context(), s.app.Pool, auth, op.Collection, op.Body)
		if err != nil {
			return batchCoreError(err)
		}
		s.publishRealtimeRecord(r.Context(), auth.Project.Slug, op.Collection, realtimeActionCreate, "", record)
		s.enqueueRecordWebhooks(r, auth, op.Collection, realtimeActionCreate, record)
		return batchOK(http.StatusCreated, record)
	case http.MethodPatch, http.MethodPut:
		if strings.TrimSpace(op.ID) == "" {
			return batchError(http.StatusBadRequest, "id is required for update")
		}
		record, err := core.UpdateRecord(r.Context(), s.app.Pool, auth, op.Collection, op.ID, op.Body)
		if err != nil {
			return batchCoreError(err)
		}
		s.publishRealtimeRecord(r.Context(), auth.Project.Slug, op.Collection, realtimeActionUpdate, op.ID, record)
		s.enqueueRecordWebhooks(r, auth, op.Collection, realtimeActionUpdate, record)
		return batchOK(http.StatusOK, record)
	case http.MethodDelete:
		if strings.TrimSpace(op.ID) == "" {
			return batchError(http.StatusBadRequest, "id is required for DELETE")
		}
		deleted, err := core.DeleteRecord(r.Context(), s.app.Pool, auth, op.Collection, op.ID)
		if err != nil {
			return batchCoreError(err)
		}
		s.publishRealtimeRecord(r.Context(), auth.Project.Slug, op.Collection, realtimeActionDelete, op.ID, deleted)
		s.enqueueRecordWebhooks(r, auth, op.Collection, realtimeActionDelete, deleted)
		return batchOK(http.StatusNoContent, deleted)
	default:
		return batchError(http.StatusBadRequest, "unsupported batch method")
	}
}

func batchOK(status int, body map[string]any) batchOperationResult {
	return batchOperationResult{Status: status, Body: body}
}

func batchError(status int, message string) batchOperationResult {
	return batchOperationResult{Status: status, Error: message}
}

func batchCoreError(err error) batchOperationResult {
	code, _, message, _ := coreErrorResponse(err)
	return batchOperationResult{Status: code, Error: message}
}
