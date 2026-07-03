package apis

import (
	"encoding/json"
	"net/http"

	"github.com/dublyo/dublyobase/core"
)

type collectionUpdateRequest struct {
	Name              *string         `json:"name"`
	Fields            json.RawMessage `json:"fields"`
	DropMissingFields bool            `json:"dropMissingFields"`
	ListRule          *string         `json:"listRule"`
	ViewRule          *string         `json:"viewRule"`
	CreateRule        *string         `json:"createRule"`
	UpdateRule        *string         `json:"updateRule"`
	DeleteRule        *string         `json:"deleteRule"`
	Options           json.RawMessage `json:"options,omitempty"`
}

type deleteCollectionRequest struct {
	Confirm string `json:"confirm"`
}

func (s *server) listCollections(w http.ResponseWriter, r *http.Request) {
	collections, err := core.ListCollections(r.Context(), s.app.Pool, r.PathValue("slug"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": collections})
}

func (s *server) createCollection(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	var req core.CollectionInput
	if !decodeJSON(w, r, &req) {
		return
	}
	collection, err := core.CreateCollection(
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
	writeJSON(w, http.StatusCreated, collection)
}

func (s *server) getCollection(w http.ResponseWriter, r *http.Request) {
	collection, err := core.GetCollection(r.Context(), s.app.Pool, r.PathValue("slug"), r.PathValue("name"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, collection)
}

func (s *server) updateCollection(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	var req collectionUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	input := core.CollectionUpdateInput{
		Name:              req.Name,
		DropMissingFields: req.DropMissingFields,
		ListRule:          req.ListRule,
		ViewRule:          req.ViewRule,
		CreateRule:        req.CreateRule,
		UpdateRule:        req.UpdateRule,
		DeleteRule:        req.DeleteRule,
		Options:           req.Options,
	}
	if req.Fields != nil {
		fields, err := core.ParseFields(req.Fields)
		if err != nil {
			writeCoreError(w, err)
			return
		}
		input.Fields = fields
		input.FieldsSet = true
	}
	collection, err := core.UpdateCollection(
		r.Context(),
		s.app.Pool,
		auth.Admin.ID,
		r.PathValue("slug"),
		r.PathValue("name"),
		input,
		s.clientIP(r),
		r.UserAgent(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, collection)
}

func (s *server) deleteCollection(w http.ResponseWriter, r *http.Request) {
	auth := adminAuth(r)
	if auth == nil {
		writeCoreError(w, core.ErrUnauthorized)
		return
	}
	confirm := r.URL.Query().Get("confirm")
	if confirm == "" && r.ContentLength != 0 {
		var req deleteCollectionRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		confirm = req.Confirm
	}
	if err := core.DeleteCollection(
		r.Context(),
		s.app.Pool,
		auth.Admin.ID,
		r.PathValue("slug"),
		r.PathValue("name"),
		confirm,
		s.clientIP(r),
		r.UserAgent(),
	); err != nil {
		writeCoreError(w, err)
		return
	}
	storageCfg, err := core.EffectiveStorageConfig(r.Context(), s.app.Pool, s.app.Config)
	if err != nil {
		s.app.Log.Warn("collection file cleanup config failed", "project", r.PathValue("slug"), "collection", r.PathValue("name"), "err", err)
	} else if err := core.RemoveCollectionStorage(r.Context(), storageCfg, r.PathValue("slug"), r.PathValue("name")); err != nil {
		s.app.Log.Warn("collection file cleanup failed", "project", r.PathValue("slug"), "collection", r.PathValue("name"), "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}
