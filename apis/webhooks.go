package apis

import (
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) adminListWebhooks(w http.ResponseWriter, r *http.Request) {
	hooks, err := core.ListWebhooks(r.Context(), s.app.Pool, r.PathValue("slug"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": hooks})
}

func (s *server) adminCreateWebhook(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	var req core.WebhookInput
	if !decodeJSON(w, r, &req) {
		return
	}
	hook, err := core.CreateWebhook(r.Context(), s.app.Pool, s.app.Config, auth.Admin.ID, r.PathValue("slug"), req, s.clientIP(r), r.UserAgent())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, hook)
}

func (s *server) adminDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if err := core.DeleteWebhook(r.Context(), s.app.Pool, auth.Admin.ID, r.PathValue("slug"), r.PathValue("id"), s.clientIP(r), r.UserAgent()); err != nil {
		writeCoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) adminListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	deliveries, err := core.ListWebhookDeliveries(r.Context(), s.app.Pool, r.PathValue("slug"), r.PathValue("id"), queryInt(r, "limit", 30))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": deliveries})
}

func (s *server) enqueueRecordWebhooks(r *http.Request, auth *core.RecordAuth, collection string, action string, record map[string]any) {
	id := ""
	model, err := core.GetCollection(r.Context(), s.app.Pool, auth.Project.Slug, collection)
	if err == nil {
		id, _ = record[core.RecordPrimaryKeyField(model)].(string)
	}
	if id == "" {
		id, _ = record["id"].(string)
	}
	if err := core.EnqueueRecordWebhookDeliveries(r.Context(), s.app.Pool, auth.Project.Slug, collection, action, id, record); err != nil {
		s.app.Log.Warn("webhook enqueue failed", "project", auth.Project.Slug, "collection", collection, "action", action, "err", err)
	}
}
