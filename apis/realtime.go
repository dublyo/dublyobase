package apis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dublyo/dublyobase/core"
)

const (
	realtimeActionCreate = "create"
	realtimeActionUpdate = "update"
	realtimeActionDelete = "delete"
)

var realtimeActions = map[string]struct{}{
	realtimeActionCreate: {},
	realtimeActionUpdate: {},
	realtimeActionDelete: {},
}

type realtimeHub struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]*realtimeSubscriber
}

type realtimeSubscriber struct {
	id          uint64
	project     string
	collections map[string]struct{}
	actions     map[string]struct{}
	ch          chan realtimeEvent
}

type realtimeEvent struct {
	Project    string
	Collection string
	Action     string
	ID         string
	Record     core.Record
	At         time.Time
}

type realtimePayload struct {
	Project    string      `json:"project"`
	Collection string      `json:"collection"`
	Action     string      `json:"action"`
	ID         string      `json:"id,omitempty"`
	Record     core.Record `json:"record,omitempty"`
	At         string      `json:"ts"`
}

func newRealtimeHub() *realtimeHub {
	return &realtimeHub{subscribers: map[uint64]*realtimeSubscriber{}}
}

func (h *realtimeHub) subscribe(project string, collections map[string]struct{}, actions map[string]struct{}) *realtimeSubscriber {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	sub := &realtimeSubscriber{
		id:          h.nextID,
		project:     project,
		collections: collections,
		actions:     actions,
		ch:          make(chan realtimeEvent, 64),
	}
	h.subscribers[sub.id] = sub
	return sub
}

func (h *realtimeHub) unsubscribe(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subscribers, id)
}

func (h *realtimeHub) publish(ev realtimeEvent) {
	if ev.Project == "" || ev.Collection == "" || ev.Action == "" {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, sub := range h.subscribers {
		if !sub.matches(ev) {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			// Keep write paths non-blocking. A slow realtime client can miss
			// events, but it must never stall record writes for the project.
		}
	}
}

func (s *realtimeSubscriber) matches(ev realtimeEvent) bool {
	if s.project != ev.Project {
		return false
	}
	if len(s.collections) > 0 {
		if _, ok := s.collections[ev.Collection]; !ok {
			return false
		}
	}
	if len(s.actions) > 0 {
		if _, ok := s.actions[ev.Action]; !ok {
			return false
		}
	}
	return true
}

func (s *server) realtimeStream(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRealtimeAuth(w, r)
	if !ok {
		return
	}
	collections, collectionList, ok := s.realtimeCollections(w, r.Context(), auth.Project.Slug, r)
	if !ok {
		return
	}
	actions, actionList, ok := realtimeActionFilter(w, r)
	if !ok {
		return
	}

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	setSSEHeaders(w)
	w.WriteHeader(http.StatusOK)

	sub := s.realtime.subscribe(auth.Project.Slug, collections, actions)
	defer s.realtime.unsubscribe(sub.id)

	if err := writeSSEEvent(w, "ready", "", map[string]any{
		"status":      "ready",
		"project":     auth.Project.Slug,
		"collections": collectionList,
		"events":      actionList,
	}); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case ev := <-sub.ch:
			payload, visible := s.realtimePayload(r.Context(), auth, ev)
			if !visible {
				continue
			}
			if err := writeSSEEvent(w, "record."+ev.Action, ev.ID, payload); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

func (s *server) resolveRealtimeAuth(w http.ResponseWriter, r *http.Request) (*core.RecordAuth, bool) {
	token := bearerToken(r)
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("access_token"))
	}
	auth, err := core.ResolveRecordAuth(r.Context(), s.app.Pool, s.app.Config, r.PathValue("slug"), token, time.Now())
	if err != nil {
		writeCoreError(w, err)
		return nil, false
	}
	return auth, true
}

func (s *server) realtimeCollections(w http.ResponseWriter, ctx context.Context, projectSlug string, r *http.Request) (map[string]struct{}, []string, bool) {
	raw := append([]string{}, r.URL.Query()["collection"]...)
	raw = append(raw, r.URL.Query()["collections"]...)
	collections := map[string]struct{}{}
	for _, item := range raw {
		for _, name := range strings.Split(item, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if len(collections) >= 64 {
				writeValidation(w, "too many realtime collections")
				return nil, nil, false
			}
			collection, err := core.GetCollection(ctx, s.app.Pool, projectSlug, name)
			if err != nil {
				writeCoreError(w, err)
				return nil, nil, false
			}
			if collection.Type == core.CollectionView {
				writeValidation(w, "realtime does not support view collections")
				return nil, nil, false
			}
			collections[collection.Name] = struct{}{}
		}
	}
	if len(collections) == 0 {
		return nil, []string{}, true
	}
	list := keys(collections)
	return collections, list, true
}

func realtimeActionFilter(w http.ResponseWriter, r *http.Request) (map[string]struct{}, []string, bool) {
	raw := append([]string{}, r.URL.Query()["event"]...)
	raw = append(raw, r.URL.Query()["events"]...)
	actions := map[string]struct{}{}
	for _, item := range raw {
		for _, action := range strings.Split(item, ",") {
			action = strings.TrimPrefix(strings.TrimSpace(action), "record.")
			if action == "" {
				continue
			}
			if _, ok := realtimeActions[action]; !ok {
				writeValidation(w, "unsupported realtime event")
				return nil, nil, false
			}
			actions[action] = struct{}{}
		}
	}
	if len(actions) == 0 {
		return nil, []string{realtimeActionCreate, realtimeActionUpdate, realtimeActionDelete}, true
	}
	list := keys(actions)
	return actions, list, true
}

func (s *server) publishRealtimeRecord(ctx context.Context, projectSlug string, collectionName string, action string, id string, record core.Record) {
	if s.realtime == nil {
		return
	}
	if id == "" {
		id = s.realtimeRecordID(ctx, projectSlug, collectionName, record)
	}
	s.realtime.publish(realtimeEvent{
		Project:    projectSlug,
		Collection: collectionName,
		Action:     action,
		ID:         id,
		Record:     record,
		At:         time.Now().UTC(),
	})
}

func (s *server) realtimeRecordID(ctx context.Context, projectSlug string, collectionName string, record core.Record) string {
	if len(record) == 0 {
		return ""
	}
	collection, err := core.GetCollection(ctx, s.app.Pool, projectSlug, collectionName)
	if err == nil {
		if id := recordValueString(record[core.RecordPrimaryKeyField(collection)]); id != "" {
			return id
		}
	}
	return recordValueString(record["id"])
}

func (s *server) realtimePayload(ctx context.Context, auth *core.RecordAuth, ev realtimeEvent) (realtimePayload, bool) {
	payload := realtimePayload{
		Project:    ev.Project,
		Collection: ev.Collection,
		Action:     ev.Action,
		ID:         ev.ID,
		At:         ev.At.Format(time.RFC3339Nano),
	}

	if ev.Action == realtimeActionDelete {
		if auth.Role == core.RecordRoleService {
			payload.Record = ev.Record
			return payload, true
		}
		return payload, false
	}

	if ev.ID == "" {
		if auth.Role != core.RecordRoleService {
			return payload, false
		}
		payload.Record = ev.Record
		return payload, true
	}

	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	record, err := core.GetRecord(checkCtx, s.app.Pool, auth, ev.Collection, ev.ID)
	if err != nil {
		if errors.Is(err, core.ErrRecordNotFound) || errors.Is(err, core.ErrRLSDenied) || errors.Is(err, core.ErrUnauthorized) {
			return payload, false
		}
		s.app.Log.Warn("realtime visibility check failed", "project", ev.Project, "collection", ev.Collection, "record", ev.ID, "err", err)
		return payload, false
	}
	payload.Record = record
	return payload, true
}

func writeSSEEvent(w http.ResponseWriter, event string, id string, data any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}
	if id != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", strings.ReplaceAll(id, "\n", "")); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", body)
	return err
}

func recordValueString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	case int:
		return strconv.Itoa(value)
	case int8:
		return strconv.FormatInt(int64(value), 10)
	case int16:
		return strconv.FormatInt(int64(value), 10)
	case int32:
		return strconv.FormatInt(int64(value), 10)
	case int64:
		return strconv.FormatInt(value, 10)
	case uint:
		return strconv.FormatUint(uint64(value), 10)
	case uint8:
		return strconv.FormatUint(uint64(value), 10)
	case uint16:
		return strconv.FormatUint(uint64(value), 10)
	case uint32:
		return strconv.FormatUint(uint64(value), 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

func keys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
