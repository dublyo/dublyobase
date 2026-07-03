package apis

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) uploadFiles(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	maxBytes := core.MaxUploadBytes(s.app.Config)
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
			core.RemoveStoredFiles(s.app.Config, files)
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
			s.app.Config,
			auth.Project.Slug,
			r.PathValue("collection"),
			r.PathValue("recordId"),
			r.PathValue("field"),
			part.FileName(),
			part,
		)
		part.Close()
		if err != nil {
			core.RemoveStoredFiles(s.app.Config, files)
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
		core.RemoveStoredFiles(s.app.Config, files)
		writeCoreError(w, err)
		return
	}
	if err := core.RemoveStoredFiles(s.app.Config, removed); err != nil {
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

	filePath := ""
	contentType := meta.Mime
	if thumb := r.URL.Query().Get("thumb"); thumb != "" {
		filePath, err = core.EnsureThumbnail(s.app.Config, meta, thumb)
		contentType = "image/jpeg"
	} else {
		filePath, err = core.FilePath(s.app.Config, meta)
	}
	if err != nil {
		writeCoreError(w, err)
		return
	}
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			writeCoreError(w, core.ErrFileNotFound)
			return
		}
		writeCoreError(w, err)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		writeCoreError(w, err)
		return
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": meta.Name}))
	http.ServeContent(w, r, filepath.Base(meta.Name), stat.ModTime(), f)
}

func stringsContainsTooLarge(s string) bool {
	return strings.Contains(s, "request body too large")
}
