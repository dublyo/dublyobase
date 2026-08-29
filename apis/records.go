package apis

import (
	"context"
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
	s.markDelivered(r.Context(), auth, r.PathValue("name"), "insert", fmt.Sprint(record["id"]))
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
	record, err := core.UpdateRecordVersioned(r.Context(), s.app.Pool, auth,
		r.PathValue("name"), r.PathValue("id"), body, expectedVersion(r))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	s.publishRealtimeRecord(r.Context(), auth.Project.Slug, r.PathValue("name"), realtimeActionUpdate, r.PathValue("id"), record)
	s.enqueueRecordWebhooks(r, auth, r.PathValue("name"), realtimeActionUpdate, record)
	s.markDelivered(r.Context(), auth, r.PathValue("name"), "update", r.PathValue("id"))
	writeJSON(w, http.StatusOK, record)
}

func (s *server) deleteRecord(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	deleted, err := core.DeleteRecordVersioned(r.Context(), s.app.Pool, auth,
		r.PathValue("name"), r.PathValue("id"), expectedVersion(r))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	s.publishRealtimeRecord(r.Context(), auth.Project.Slug, r.PathValue("name"), realtimeActionDelete, r.PathValue("id"), deleted)
	s.enqueueRecordWebhooks(r, auth, r.PathValue("name"), realtimeActionDelete, deleted)
	s.markDelivered(r.Context(), auth, r.PathValue("name"), "delete", r.PathValue("id"))
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

// exportRecords streams the collection as CSV, honouring the same filter,
// search, sort and field projection as the record list so the file matches the
// view it came from. It runs under the caller's role, so the export can never
// contain a row the caller could not read.
func (s *server) exportRecords(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	filter := query.Get("filter")
	if filter == "" {
		filter = directusFilterQuery(r)
	}
	name := r.PathValue("name")
	opts := core.RecordExportOptions{
		Filter:        filter,
		Search:        query.Get("search"),
		Sort:          query.Get("sort"),
		Fields:        query.Get("fields"),
		Limit:         queryInt(r, "limit", 0),
		RelationsAsID: strings.EqualFold(query.Get("relations"), "id"),
	}
	// Validate before committing to a 200 and a download: once the first byte
	// is written the status is fixed and an error can only arrive as a corrupt
	// file.
	if err := core.ValidateExportOptions(r.Context(), s.app.Pool, auth, name, opts); err != nil {
		writeCoreError(w, err)
		return
	}
	xlsx := strings.EqualFold(query.Get("format"), "xlsx")
	setExportHeaders(w, name, "", xlsx)
	var err error
	if xlsx {
		_, err = core.ExportRecordsXLSX(r.Context(), s.app.Pool, auth, name, opts, w)
	} else {
		_, err = core.ExportRecordsCSV(r.Context(), s.app.Pool, auth, name, opts, w)
	}
	if err != nil {
		// Clean if nothing was written; once streaming has begun the client
		// sees a short file, which is why the row cap exists.
		writeCoreError(w, err)
		return
	}
}

// expectedVersion reads the row version a caller believes it is updating.
// If-Match is the HTTP-native spelling; the query parameter exists for clients
// that cannot set headers.
func expectedVersion(r *http.Request) string {
	if v := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`); v != "" {
		return v
	}
	return strings.TrimSpace(r.URL.Query().Get("version"))
}

// recordHistory returns the write trail for one record: who changed it, when,
// which fields moved, and the full before/after.
func (s *server) recordHistory(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	entries, err := core.ListRecordHistory(r.Context(), s.app.Pool, auth,
		r.PathValue("name"), r.PathValue("id"), queryInt(r, "limit", 0))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": entries})
}

func setExportHeaders(w http.ResponseWriter, name, suffix string, xlsx bool) {
	ext, mime := "csv", "text/csv; charset=utf-8"
	if xlsx {
		ext = "xlsx"
		mime = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q",
		fmt.Sprintf("%s%s-%s.%s", name, suffix, time.Now().UTC().Format("20060102-150405"), ext)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

// exportAggregate streams a grouped aggregate as CSV. Totals are computed in
// Postgres, so a report spanning two one-to-many relations cannot be inflated
// by the fan-out a flat join would produce.
func (s *server) exportAggregate(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	filter := query.Get("filter")
	if filter == "" {
		filter = directusFilterQuery(r)
	}
	groupBy := splitAggregateParam(query["groupBy"])
	if len(groupBy) == 0 {
		groupBy = splitAggregateParam(query["group_by"])
	}
	name := r.PathValue("name")
	input := core.AggregateInput{
		Aggregates: splitAggregateParam(query["aggregate"]),
		GroupBy:    groupBy,
		Filter:     filter,
		Search:     query.Get("search"),
		Limit:      queryInt(r, "limit", 0),
	}
	// Validate before committing to a 200 and a download.
	if _, err := core.AggregateRecords(r.Context(), s.app.Pool, auth, name, input); err != nil {
		writeCoreError(w, err)
		return
	}
	xlsx := strings.EqualFold(query.Get("format"), "xlsx")
	setExportHeaders(w, name, "-summary", xlsx)
	var err error
	if xlsx {
		_, err = core.ExportAggregateXLSX(r.Context(), s.app.Pool, auth, name, input, w)
	} else {
		_, err = core.ExportAggregateCSV(r.Context(), s.app.Pool, auth, name, input, w)
	}
	if err != nil {
		writeCoreError(w, err)
		return
	}
}

// markDelivered tells the outbox this event went out on the request path, so
// the sweep leaves it alone. Best effort: if it fails, the sweep republishes,
// which is the safe direction to be wrong in — a duplicate event is recoverable,
// a lost one is not.
func (s *server) markDelivered(ctx context.Context, auth *core.RecordAuth, collection, action, id string) {
	if s.app.Pool == nil || id == "" || id == "<nil>" {
		return
	}
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if outboxID, ok := core.LatestOutboxID(markCtx, s.app.Pool, auth.Project.SchemaName, collection, id, action); ok {
		_ = core.MarkOutboxPublished(markCtx, s.app.Pool, auth.Project.SchemaName, outboxID)
	}
}

type vectorSearchRequest struct {
	Field     string    `json:"field"`
	Vector    []float64 `json:"vector"`
	Limit     int       `json:"limit"`
	Page      int       `json:"page"`
	Filter    string    `json:"filter"`
	Fields    string    `json:"fields"`
	Expand    string    `json:"expand"`
	SkipTotal bool      `json:"skipTotal"`
}

// searchRecordsByVector runs a nearest-neighbour search.
//
// It is a POST because the query vector is the payload: an embedding is
// commonly 1536 numbers, which no URL should be asked to carry. Everything
// else routes through the ordinary list path, so the same row-level security,
// filters and paging apply.
func (s *server) searchRecordsByVector(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	var req vectorSearchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Field) == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "field is required")
		return
	}
	if len(req.Vector) == 0 {
		writeError(w, http.StatusBadRequest, "validation_error", "vector is required")
		return
	}
	perPage := req.Limit
	if perPage <= 0 {
		perPage = 25
	}
	page := req.Page
	if page <= 0 {
		page = 1
	}
	opts := core.RecordListOptions{
		Page:       page,
		PerPage:    perPage,
		Filter:     req.Filter,
		Fields:     req.Fields,
		Expand:     req.Expand,
		SkipTotal:  req.SkipTotal,
		NearField:  req.Field,
		NearVector: req.Vector,
	}
	result, err := core.ListRecords(r.Context(), s.app.Pool, auth, r.PathValue("name"), opts)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
