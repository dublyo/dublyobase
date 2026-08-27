package apis

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
	"github.com/jackc/pgx/v5"
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
	mu                   sync.RWMutex
	nextID               uint64
	subscribers          map[uint64]*realtimeSubscriber
	broadcastSubscribers map[uint64]*realtimeBroadcastSubscriber
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

type realtimeBroadcastSubscriber struct {
	id      uint64
	project string
	channel string
	ch      chan realtimeBroadcastEvent
}

type realtimeBroadcastEvent struct {
	Project string
	Channel string
	Event   string
	UserID  string
	Payload map[string]any
	At      time.Time
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
	return &realtimeHub{
		subscribers:          map[uint64]*realtimeSubscriber{},
		broadcastSubscribers: map[uint64]*realtimeBroadcastSubscriber{},
	}
}

func newRealtimeSourceID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("srv_%d", time.Now().UnixNano())
	}
	return "srv_" + base64.RawURLEncoding.EncodeToString(raw[:])
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

func (h *realtimeHub) subscribeBroadcast(project string, channel string) *realtimeBroadcastSubscriber {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	sub := &realtimeBroadcastSubscriber{
		id:      h.nextID,
		project: project,
		channel: channel,
		ch:      make(chan realtimeBroadcastEvent, 64),
	}
	h.broadcastSubscribers[sub.id] = sub
	return sub
}

func (h *realtimeHub) unsubscribeBroadcast(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.broadcastSubscribers, id)
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

func (h *realtimeHub) publishBroadcast(ev realtimeBroadcastEvent) {
	if ev.Project == "" || ev.Channel == "" || ev.Event == "" {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, sub := range h.broadcastSubscribers {
		if sub.project != ev.Project || sub.channel != ev.Channel {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
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
	auth, err := core.ResolveRecordAuthForOrg(r.Context(), s.app.Pool, s.app.Config, r.PathValue("slug"), token, activeOrgID(r), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return nil, false
	}
	if !s.checkResolvedProjectQuota(w, r, auth) {
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
	ev := realtimeEvent{
		Project:    projectSlug,
		Collection: collectionName,
		Action:     action,
		ID:         id,
		Record:     record,
		At:         time.Now().UTC(),
	}
	s.persistRealtimeEvent(ctx, ev)
	s.realtime.publish(ev)
}

func (s *server) persistRealtimeEvent(ctx context.Context, ev realtimeEvent) {
	if s.app.Pool == nil || s.realtimeSourceID == "" {
		return
	}
	insertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := core.InsertRealtimeEvent(insertCtx, s.app.Pool, s.realtimeSourceID, ev.Project, ev.Collection, ev.Action, ev.ID, ev.Record); err != nil {
		s.app.Log.Warn("realtime event insert failed", "project", ev.Project, "collection", ev.Collection, "action", ev.Action, "err", err)
	}
}

func (s *server) runRealtimeFanout(ctx context.Context) {
	if s.app.Pool == nil || s.realtime == nil || s.realtimeSourceID == "" {
		return
	}
	wake := make(chan struct{}, 1)
	go s.runRealtimeNotifyListener(ctx, wake)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var lastRecordID int64
	var lastBroadcastID int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			s.pollRealtimeFanout(ctx, &lastRecordID, &lastBroadcastID)
		case <-ticker.C:
			s.pollRealtimeFanout(ctx, &lastRecordID, &lastBroadcastID)
		}
	}
}

func (s *server) runRealtimeNotifyListener(ctx context.Context, wake chan<- struct{}) {
	for {
		if ctx.Err() != nil {
			return
		}
		conn, err := pgx.ConnectConfig(ctx, s.app.Pool.Config().ConnConfig.Copy())
		if err != nil {
			s.app.Log.Warn("realtime notify connect failed", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}
		if _, err := conn.Exec(ctx, `listen dbo_realtime`); err != nil {
			_ = conn.Close(context.Background())
			s.app.Log.Warn("realtime listen failed", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}
		if _, err := conn.Exec(ctx, `listen dbo_realtime_broadcast`); err != nil {
			_ = conn.Close(context.Background())
			s.app.Log.Warn("realtime broadcast listen failed", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}
		for ctx.Err() == nil {
			if _, err := conn.WaitForNotification(ctx); err != nil {
				if ctx.Err() == nil {
					s.app.Log.Warn("realtime notification wait failed", "err", err)
				}
				break
			}
			select {
			case wake <- struct{}{}:
			default:
			}
		}
		_ = conn.Close(context.Background())
	}
}

func (s *server) pollRealtimeFanout(ctx context.Context, lastRecordID *int64, lastBroadcastID *int64) {
	pollCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	events, err := core.ListRealtimeEventsAfter(pollCtx, s.app.Pool, *lastRecordID, s.realtimeSourceID, 100)
	if err == nil {
		for _, item := range events {
			if item.ID > *lastRecordID {
				*lastRecordID = item.ID
			}
			s.realtime.publish(realtimeEvent{
				Project:    item.Project,
				Collection: item.Collection,
				Action:     item.Action,
				ID:         item.RecordID,
				Record:     item.Record,
				At:         item.CreatedAt,
			})
		}
		_ = core.PruneRealtimeEvents(pollCtx, s.app.Pool, 24*time.Hour)
	} else if ctx.Err() == nil {
		s.app.Log.Warn("realtime fanout poll failed", "err", err)
	}
	broadcasts, err := core.ListRealtimeBroadcastsAfter(pollCtx, s.app.Pool, *lastBroadcastID, s.realtimeSourceID, 100)
	if err == nil {
		for _, item := range broadcasts {
			if item.ID > *lastBroadcastID {
				*lastBroadcastID = item.ID
			}
			s.realtime.publishBroadcast(realtimeBroadcastEvent{
				Project: item.Project,
				Channel: item.Channel,
				Event:   item.Event,
				UserID:  item.UserID,
				Payload: item.Payload,
				At:      item.CreatedAt,
			})
		}
		_ = core.PruneRealtimeChannelState(pollCtx, s.app.Pool, 24*time.Hour)
	} else if ctx.Err() == nil {
		s.app.Log.Warn("realtime broadcast fanout poll failed", "err", err)
	}
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

	if auth.Role == core.RecordRoleService {
		payload.Record = ev.Record
		return payload, true
	}

	if ev.ID == "" {
		return payload, false
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
