package apis

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) listRecords(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	perPage := queryInt(r, "perPage", 25)
	if r.URL.Query().Get("limit") != "" {
		perPage = queryInt(r, "limit", perPage)
	}
	filter := r.URL.Query().Get("filter")
	if strings.TrimSpace(filter) == "" {
		filter = directusFilterQuery(r)
	}
	opts := core.RecordListOptions{
		Page:      queryInt(r, "page", 1),
		PerPage:   perPage,
		Offset:    queryInt(r, "offset", 0),
		Sort:      r.URL.Query().Get("sort"),
		Filter:    filter,
		Search:    r.URL.Query().Get("search"),
		Fields:    r.URL.Query().Get("fields"),
		Expand:    r.URL.Query().Get("expand"),
		SkipTotal: queryBool(r, "skipTotal"),
	}
	result, err := core.ListRecords(r.Context(), s.app.Pool, auth, r.PathValue("name"), opts)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func directusFilterQuery(r *http.Request) string {
	root := map[string]any{}
	for key, values := range r.URL.Query() {
		if !strings.HasPrefix(key, "filter[") || len(values) == 0 {
			continue
		}
		segments := directusFilterSegments(key)
		if len(segments) < 2 || segments[0] != "filter" {
			continue
		}
		assignFilterValue(root, segments[1:], values[len(values)-1])
	}
	if len(root) == 0 {
		return ""
	}
	body, err := json.Marshal(root)
	if err != nil {
		return ""
	}
	return string(body)
}

func directusFilterSegments(key string) []string {
	var segments []string
	for key != "" {
		start := strings.IndexByte(key, '[')
		if start < 0 {
			segments = append(segments, key)
			break
		}
		if start > 0 {
			segments = append(segments, key[:start])
		}
		key = key[start+1:]
		end := strings.IndexByte(key, ']')
		if end < 0 {
			break
		}
		segments = append(segments, key[:end])
		key = key[end+1:]
	}
	return segments
}

func assignFilterValue(root map[string]any, segments []string, value string) {
	if len(segments) == 0 {
		return
	}
	if len(segments) == 1 {
		root[segments[0]] = value
		return
	}
	next, _ := root[segments[0]].(map[string]any)
	if next == nil {
		next = map[string]any{}
		root[segments[0]] = next
	}
	assignFilterValue(next, segments[1:], value)
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
	s.publishRealtimeRecord(r.Context(), auth.Project.Slug, r.PathValue("name"), realtimeActionCreate, "", record)
	s.enqueueRecordWebhooks(r, auth, r.PathValue("name"), realtimeActionCreate, record)
	writeJSON(w, http.StatusCreated, record)
}

func (s *server) getRecord(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	record, err := core.GetRecordWithOptions(r.Context(), s.app.Pool, auth, r.PathValue("name"), r.PathValue("id"), r.URL.Query().Get("expand"))
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
	s.publishRealtimeRecord(r.Context(), auth.Project.Slug, r.PathValue("name"), realtimeActionUpdate, r.PathValue("id"), record)
	s.enqueueRecordWebhooks(r, auth, r.PathValue("name"), realtimeActionUpdate, record)
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
	s.publishRealtimeRecord(r.Context(), auth.Project.Slug, r.PathValue("name"), realtimeActionDelete, r.PathValue("id"), deleted)
	s.enqueueRecordWebhooks(r, auth, r.PathValue("name"), realtimeActionDelete, deleted)
	if id, _ := deleted["id"].(string); id != "" {
		storageCfg, err := core.EffectiveStorageConfig(r.Context(), s.app.Pool, s.app.Config)
		if err != nil {
			s.app.Log.Warn("record file cleanup config failed", "project", auth.Project.Slug, "collection", r.PathValue("name"), "record", id, "err", err)
		} else if err := core.RemoveRecordStorage(r.Context(), storageCfg, auth.Project.Slug, r.PathValue("name"), id); err != nil {
			s.app.Log.Warn("record file cleanup failed", "project", auth.Project.Slug, "collection", r.PathValue("name"), "record", id, "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) resolveRecordAuth(w http.ResponseWriter, r *http.Request) (*core.RecordAuth, bool) {
	if !s.checkProjectQuota(w, r, r.PathValue("slug"), false) {
		return nil, false
	}
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

func queryBool(r *http.Request, name string) bool {
	raw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(name)))
	return raw == "1" || raw == "true" || raw == "yes"
}
