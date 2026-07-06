package apis

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/dublyo/dublyobase/core"
)

type realtimeWSMessage struct {
	Type    string         `json:"type"`
	Channel string         `json:"channel,omitempty"`
	Event   string         `json:"event,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
	State   map[string]any `json:"state,omitempty"`
}

type realtimeWSEnvelope struct {
	Type        string                  `json:"type"`
	Project     string                  `json:"project,omitempty"`
	Channel     string                  `json:"channel,omitempty"`
	Event       string                  `json:"event,omitempty"`
	UserID      string                  `json:"userId,omitempty"`
	Data        any                     `json:"data,omitempty"`
	Payload     map[string]any          `json:"payload,omitempty"`
	Presence    []core.RealtimePresence `json:"presence,omitempty"`
	Collections []string                `json:"collections,omitempty"`
	Events      []string                `json:"events,omitempty"`
	TS          string                  `json:"ts,omitempty"`
	Error       string                  `json:"error,omitempty"`
}

func (s *server) realtimeWebSocket(w http.ResponseWriter, r *http.Request) {
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
	channel, ok := realtimeWSChannel(w, r.URL.Query().Get("channel"))
	if !ok {
		return
	}

	conn, err := websocket.Accept(w, r, realtimeWSAcceptOptions(auth))
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	conn.SetReadLimit(64 * 1024)

	recordSub := s.realtime.subscribe(auth.Project.Slug, collections, actions)
	defer s.realtime.unsubscribe(recordSub.id)
	broadcastSub := s.realtime.subscribeBroadcast(auth.Project.Slug, channel)
	defer s.realtime.unsubscribeBroadcast(broadcastSub.id)

	socketSessionID := "ws_" + strings.TrimPrefix(newRealtimeSourceID(), "srv_")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = core.RemoveRealtimePresence(ctx, s.app.Pool, auth.Project.Slug, channel, socketSessionID)
	}()

	incoming := make(chan realtimeWSMessage, 16)
	readErr := make(chan error, 1)
	go readRealtimeWS(r.Context(), conn, incoming, readErr)

	if err := writeRealtimeWS(r.Context(), conn, realtimeWSEnvelope{
		Type:        "ready",
		Project:     auth.Project.Slug,
		Channel:     channel,
		Collections: collectionList,
		Events:      actionList,
		TS:          time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case err := <-readErr:
			if err == nil || websocket.CloseStatus(err) == websocket.StatusNormalClosure || websocket.CloseStatus(err) == websocket.StatusGoingAway {
				return
			}
			return
		case msg := <-incoming:
			if err := s.handleRealtimeWSMessage(r.Context(), conn, auth, channel, socketSessionID, msg); err != nil {
				_ = writeRealtimeWS(r.Context(), conn, realtimeWSEnvelope{Type: "error", Error: err.Error(), TS: time.Now().UTC().Format(time.RFC3339Nano)})
			}
		case <-heartbeat.C:
			if err := writeRealtimeWS(r.Context(), conn, realtimeWSEnvelope{Type: "ping", TS: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
				return
			}
		case ev := <-recordSub.ch:
			payload, visible := s.realtimePayload(r.Context(), auth, ev)
			if !visible {
				continue
			}
			if err := writeRealtimeWS(r.Context(), conn, realtimeWSEnvelope{
				Type:    "record." + ev.Action,
				Project: ev.Project,
				Data:    payload,
				TS:      time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return
			}
		case ev := <-broadcastSub.ch:
			if err := writeRealtimeWS(r.Context(), conn, realtimeWSEnvelope{
				Type:    "broadcast",
				Project: ev.Project,
				Channel: ev.Channel,
				Event:   ev.Event,
				UserID:  ev.UserID,
				Payload: ev.Payload,
				TS:      ev.At.Format(time.RFC3339Nano),
			}); err != nil {
				return
			}
		}
	}
}

func (s *server) handleRealtimeWSMessage(ctx context.Context, conn *websocket.Conn, auth *core.RecordAuth, defaultChannel string, sessionID string, msg realtimeWSMessage) error {
	channel := strings.TrimSpace(msg.Channel)
	if channel == "" {
		channel = defaultChannel
	}
	switch strings.TrimSpace(msg.Type) {
	case "ping":
		return writeRealtimeWS(ctx, conn, realtimeWSEnvelope{Type: "pong", TS: time.Now().UTC().Format(time.RFC3339Nano)})
	case "presence.update":
		if err := core.UpsertRealtimePresence(ctx, s.app.Pool, auth.Project.Slug, channel, sessionID, auth.Subject, msg.State, 90*time.Second); err != nil {
			return err
		}
		presence, err := core.ListRealtimePresence(ctx, s.app.Pool, auth.Project.Slug, channel, 100)
		if err != nil {
			return err
		}
		return writeRealtimeWS(ctx, conn, realtimeWSEnvelope{Type: "presence", Project: auth.Project.Slug, Channel: channel, Presence: presence, TS: time.Now().UTC().Format(time.RFC3339Nano)})
	case "presence.list":
		presence, err := core.ListRealtimePresence(ctx, s.app.Pool, auth.Project.Slug, channel, 100)
		if err != nil {
			return err
		}
		return writeRealtimeWS(ctx, conn, realtimeWSEnvelope{Type: "presence", Project: auth.Project.Slug, Channel: channel, Presence: presence, TS: time.Now().UTC().Format(time.RFC3339Nano)})
	case "broadcast":
		event := strings.TrimSpace(msg.Event)
		if event == "" {
			event = "message"
		}
		if err := core.InsertRealtimeBroadcast(ctx, s.app.Pool, s.realtimeSourceID, auth.Project.Slug, channel, event, auth.Subject, msg.Payload); err != nil {
			return err
		}
		s.realtime.publishBroadcast(realtimeBroadcastEvent{
			Project: auth.Project.Slug,
			Channel: channel,
			Event:   event,
			UserID:  auth.Subject,
			Payload: msg.Payload,
			At:      time.Now().UTC(),
		})
		return nil
	default:
		return core.ErrValidation
	}
}

func readRealtimeWS(ctx context.Context, conn *websocket.Conn, incoming chan<- realtimeWSMessage, readErr chan<- error) {
	for {
		typ, raw, err := conn.Read(ctx)
		if err != nil {
			readErr <- err
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var msg realtimeWSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		select {
		case incoming <- msg:
		case <-ctx.Done():
			readErr <- ctx.Err()
			return
		}
	}
}

func writeRealtimeWS(ctx context.Context, conn *websocket.Conn, envelope realtimeWSEnvelope) error {
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	return conn.Write(writeCtx, websocket.MessageText, raw)
}

func realtimeWSAcceptOptions(auth *core.RecordAuth) *websocket.AcceptOptions {
	options := &websocket.AcceptOptions{}
	for _, origin := range auth.Project.CORS.PublicOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			options.InsecureSkipVerify = true
			continue
		}
		options.OriginPatterns = append(options.OriginPatterns, origin)
	}
	return options
}

func realtimeWSChannel(w http.ResponseWriter, raw string) (string, bool) {
	channel := strings.TrimSpace(raw)
	if channel == "" {
		return "default", true
	}
	if len(channel) > 120 || strings.ContainsAny(channel, "\r\n\t") {
		writeValidation(w, "invalid realtime channel")
		return "", false
	}
	return channel, true
}
