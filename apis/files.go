package apis

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) uploadFiles(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	if err := core.AuthorizeFileUpload(r.Context(), s.app.Pool, auth, r.PathValue("collection"), r.PathValue("recordId"), r.PathValue("field")); err != nil {
		writeCoreError(w, err)
		return
	}
	storageCfg, err := core.EffectiveStorageConfig(r.Context(), s.app.Pool, s.app.Config)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	maxBytes := core.MaxUploadBytes(storageCfg)
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024*1024)
	reader, err := r.MultipartReader()
	if err != nil {
		writeCoreError(w, fmt.Errorf("%w: multipart form required", core.ErrValidation))
		return
	}

	var files []core.FileMeta
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = core.RemoveStoredFiles(r.Context(), storageCfg, files)
			if stringsContainsTooLarge(err.Error()) {
				writeCoreError(w, core.ErrFileTooLarge)
				return
			}
			writeCoreError(w, fmt.Errorf("%w: invalid multipart upload", core.ErrValidation))
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			part.Close()
			continue
		}
		meta, err := core.StoreUploadedFile(
			r.Context(),
			storageCfg,
			auth.Project.Slug,
			r.PathValue("collection"),
			r.PathValue("recordId"),
			r.PathValue("field"),
			part.FileName(),
			part,
		)
		part.Close()
		if err != nil {
			_ = core.RemoveStoredFiles(r.Context(), storageCfg, files)
			if stringsContainsTooLarge(err.Error()) {
				writeCoreError(w, core.ErrFileTooLarge)
				return
			}
			writeCoreError(w, err)
			return
		}
		files = append(files, meta)
	}
	if len(files) == 0 {
		writeCoreError(w, fmt.Errorf("%w: multipart field \"file\" is required", core.ErrValidation))
		return
	}

	record, removed, err := core.UpdateRecordFileField(
		r.Context(),
		s.app.Pool,
		auth,
		r.PathValue("collection"),
		r.PathValue("recordId"),
		r.PathValue("field"),
		r.URL.Query().Get("mode"),
		files,
	)
	if err != nil {
		_ = core.RemoveStoredFiles(r.Context(), storageCfg, files)
		writeCoreError(w, err)
		return
	}
	if err := core.RemoveStoredFiles(r.Context(), storageCfg, removed); err != nil {
		s.app.Log.Warn("replaced file cleanup failed", "project", auth.Project.Slug, "collection", r.PathValue("collection"), "record", r.PathValue("recordId"), "err", err)
	}
	writeJSON(w, http.StatusCreated, record)
}

func (s *server) createFileToken(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	token, expiresAt, err := core.MintFileToken(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		auth,
		r.PathValue("collection"),
		r.PathValue("recordId"),
		r.PathValue("field"),
		r.PathValue("fileId"),
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":     token,
		"expiresAt": expiresAt.UTC().Format(time.RFC3339Nano),
	})
}

func (s *server) downloadFile(w http.ResponseWriter, r *http.Request) {
	claims, err := core.ParseFileToken(s.app.Config.JWTSecret, r.URL.Query().Get("token"), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	if claims.Project != r.PathValue("slug") ||
		claims.Collection != r.PathValue("collection") ||
		claims.RecordID != r.PathValue("recordId") ||
		claims.Field != r.PathValue("field") ||
		claims.FileID != r.PathValue("fileId") {
		writeCoreError(w, core.ErrInvalidAuthToken)
		return
	}
	meta, err := core.GetFileForToken(r.Context(), s.app.Pool, claims)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	storageCfg, err := core.EffectiveStorageConfig(r.Context(), s.app.Pool, s.app.Config)
	if err != nil {
		writeCoreError(w, err)
		return
	}

	obj, contentType, err := core.OpenStoredFile(r.Context(), storageCfg, meta, r.URL.Query().Get("thumb"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	defer obj.Body.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": meta.Name}))
	if obj.Info.Size >= 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", obj.Info.Size))
	}
	if _, err := io.Copy(w, obj.Body); err != nil {
		s.app.Log.Warn("file download stream failed", "project", claims.Project, "collection", claims.Collection, "record", claims.RecordID, "err", err)
	}
}

func stringsContainsTooLarge(s string) bool {
	return strings.Contains(s, "request body too large")
}
