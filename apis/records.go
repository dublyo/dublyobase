package apis

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) listRecords(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	opts := core.RecordListOptions{
		Page:    queryInt(r, "page", 1),
		PerPage: queryInt(r, "perPage", 30),
		Sort:    r.URL.Query().Get("sort"),
		Filter:  r.URL.Query().Get("filter"),
		Fields:  r.URL.Query().Get("fields"),
	}
	result, err := core.ListRecords(r.Context(), s.app.Pool, auth, r.PathValue("name"), opts)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) createRecord(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	var body map[string]json.RawMessage
	if !decodeJSON(w, r, &body) {
		return
	}
	record, err := core.CreateRecord(r.Context(), s.app.Pool, auth, r.PathValue("name"), body)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (s *server) getRecord(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	record, err := core.GetRecord(r.Context(), s.app.Pool, auth, r.PathValue("name"), r.PathValue("id"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *server) updateRecord(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	var body map[string]json.RawMessage
	if !decodeJSON(w, r, &body) {
		return
	}
	record, err := core.UpdateRecord(r.Context(), s.app.Pool, auth, r.PathValue("name"), r.PathValue("id"), body)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *server) deleteRecord(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	deleted, err := core.DeleteRecord(r.Context(), s.app.Pool, auth, r.PathValue("name"), r.PathValue("id"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	if id, _ := deleted["id"].(string); id != "" {
		if err := core.RemoveRecordStorage(s.app.Config, auth.Project.Slug, r.PathValue("name"), id); err != nil {
			s.app.Log.Warn("record file cleanup failed", "project", auth.Project.Slug, "collection", r.PathValue("name"), "record", id, "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) resolveRecordAuth(w http.ResponseWriter, r *http.Request) (*core.RecordAuth, bool) {
	auth, err := core.ResolveRecordAuth(r.Context(), s.app.Pool, s.app.Config, r.PathValue("slug"), bearerToken(r), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return nil, false
	}
	return auth, true
}

func queryInt(r *http.Request, name string, def int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}
