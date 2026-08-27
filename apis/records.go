package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
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
	// Aggregation used to be accepted and silently dropped here, so a caller
	// asking for sum/groupBy got a plain page of rows back and could read it as
	// a report. Point them at the endpoint that actually does it.
	for _, param := range []string{"aggregate", "groupBy", "group_by"} {
		if r.URL.Query().Has(param) {
			writeError(w, http.StatusBadRequest, "validation_error",
				fmt.Sprintf("%q is not supported on the records list; use GET .../records/aggregate", param))
			return
		}
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
	body, err := json.Marshal(normalizeDirectusFilterNode(root))
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

func normalizeDirectusFilterNode(value any) any {
	body, ok := value.(map[string]any)
	if !ok {
		return value
	}
	out := make(map[string]any, len(body))
	for key, child := range body {
		child = normalizeDirectusFilterNode(child)
		if key == "_and" || key == "_or" {
			if list, ok := directusNumericMapToList(child); ok {
				out[key] = list
				continue
			}
		}
		out[key] = child
	}
	return out
}

func directusNumericMapToList(value any) ([]any, bool) {
	body, ok := value.(map[string]any)
	if !ok || len(body) == 0 {
		return nil, false
	}
	indexes := make([]int, 0, len(body))
	byIndex := make(map[int]any, len(body))
	for key, child := range body {
		index, err := strconv.Atoi(key)
		if err != nil || index < 0 {
			return nil, false
		}
		indexes = append(indexes, index)
		byIndex[index] = child
	}
	sort.Ints(indexes)
	out := make([]any, 0, len(indexes))
	for _, index := range indexes {
		out = append(out, byIndex[index])
	}
	return out, true
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
	auth, err := core.ResolveRecordAuthForOrg(r.Context(), s.app.Pool, s.app.Config, r.PathValue("slug"), bearerToken(r), activeOrgID(r), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return nil, false
	}
	if !s.checkResolvedProjectQuota(w, r, auth) {
		return nil, false
	}
	return auth, true
}

// activeOrgID reads the organization the caller wants to act in. Browser
// EventSource clients cannot set headers, so realtime also accepts it as a
// query parameter; membership is verified server-side either way.
func activeOrgID(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Org-Id")); v != "" {
		return v
	}
	return strings.TrimSpace(r.URL.Query().Get("org"))
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

// aggregateRecords answers grouped aggregate queries:
//
//	GET .../records/aggregate?aggregate=sum:amount,count:*&groupBy=stage
//
// It runs under the caller's role, so the numbers only ever cover rows the
// caller may read.
func (s *server) aggregateRecords(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	// Accept exactly the filter forms the record list accepts. Reading only
	// `filter` meant `filter[stage][_eq]=won` was dropped and the caller got a
	// total over every row — a wrong report that looks like a right one.
	filter := query.Get("filter")
	if filter == "" {
		filter = directusFilterQuery(r)
	}
	groupBy := splitAggregateParam(query["groupBy"])
	if len(groupBy) == 0 {
		groupBy = splitAggregateParam(query["group_by"])
	}
	input := core.AggregateInput{
		Aggregates: splitAggregateParam(query["aggregate"]),
		GroupBy:    groupBy,
		Filter:     filter,
		Search:     query.Get("search"),
		Limit:      queryInt(r, "limit", 0),
	}
	result, err := core.AggregateRecords(r.Context(), s.app.Pool, auth, r.PathValue("name"), input)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func splitAggregateParam(values []string) []string {
	out := []string{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
