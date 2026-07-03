package apis

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/dublyo/dublyobase/core"
)

type createFileUploadSessionRequest struct {
	Filename       string `json:"filename"`
	Size           int64  `json:"size"`
	ChunkSize      int64  `json:"chunkSize"`
	Mode           string `json:"mode"`
	ChecksumSHA256 string `json:"checksumSha256"`
}

type completeFileUploadSessionRequest struct {
	ChecksumSHA256 string `json:"checksumSha256"`
}

func (s *server) createFileUploadSession(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	if _, err := core.CleanupExpiredFileUploadSessions(r.Context(), s.app.Pool, s.app.Config, time.Now(), 50); err != nil {
		s.app.Log.Warn("expired file upload cleanup failed", "project", auth.Project.Slug, "err", err)
	}
	var body createFileUploadSessionRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	session, err := core.CreateFileUploadSession(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		auth,
		core.CreateFileUploadSessionInput{
			Collection:     r.PathValue("collection"),
			RecordID:       r.PathValue("recordId"),
			Field:          r.PathValue("field"),
			Filename:       body.Filename,
			Mode:           body.Mode,
			TotalSize:      body.Size,
			ChunkSize:      body.ChunkSize,
			ChecksumSHA256: body.ChecksumSHA256,
		},
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (s *server) uploadFileChunk(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		writeCoreError(w, core.ErrValidation)
		return
	}
	session, err := core.GetOpenFileUploadSession(
		r.Context(),
		s.app.Pool,
		auth,
		r.PathValue("slug"),
		r.PathValue("uploadId"),
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	chunk, err := core.StoreFileUploadChunk(
		s.app.Config,
		session,
		index,
		r.Header.Get("X-Checksum-SHA256"),
		r.Body,
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	if err := core.RecordFileUploadChunk(r.Context(), s.app.Pool, session, chunk); err != nil {
		_ = core.RemoveFileUploadChunk(s.app.Config, session, index)
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chunk)
}

func (s *server) completeFileUploadSession(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	var body completeFileUploadSessionRequest
	if !decodeOptionalJSON(w, r, &body) {
		return
	}
	session, err := core.ClaimFileUploadSessionForComplete(
		r.Context(),
		s.app.Pool,
		auth,
		r.PathValue("slug"),
		r.PathValue("uploadId"),
		time.Now(),
	)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	meta, err := core.AssembleFileUploadSession(s.app.Config, session, body.ChecksumSHA256)
	if err != nil {
		_ = core.ReopenFileUploadSession(r.Context(), s.app.Pool, session.ID)
		writeCoreError(w, err)
		return
	}
	record, removed, err := core.UpdateRecordFileField(
		r.Context(),
		s.app.Pool,
		auth,
		session.Collection,
		session.RecordID,
		session.Field,
		session.Mode,
		[]core.FileMeta{meta},
	)
	if err != nil {
		_ = core.RemoveStoredFiles(s.app.Config, []core.FileMeta{meta})
		_ = core.ReopenFileUploadSession(r.Context(), s.app.Pool, session.ID)
		writeCoreError(w, err)
		return
	}
	if err := core.RemoveStoredFiles(s.app.Config, removed); err != nil {
		s.app.Log.Warn("replaced file cleanup failed", "project", auth.Project.Slug, "collection", session.Collection, "record", session.RecordID, "err", err)
	}
	if err := core.MarkFileUploadSessionCompleted(r.Context(), s.app.Pool, session.ID); err != nil {
		s.app.Log.Warn("file upload completion mark failed", "project", auth.Project.Slug, "upload", session.ID, "err", err)
	}
	if err := core.RemoveFileUploadTemp(s.app.Config, session); err != nil {
		s.app.Log.Warn("file upload temp cleanup failed", "project", auth.Project.Slug, "upload", session.ID, "err", err)
	}
	writeJSON(w, http.StatusCreated, record)
}

func (s *server) cancelFileUploadSession(w http.ResponseWriter, r *http.Request) {
	auth, ok := s.resolveRecordAuth(w, r)
	if !ok {
		return
	}
	if err := core.CancelFileUploadSession(
		r.Context(),
		s.app.Pool,
		s.app.Config,
		auth,
		r.PathValue("slug"),
		r.PathValue("uploadId"),
		time.Now(),
	); err != nil {
		writeCoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if err == io.EOF {
			return true
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return false
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return false
	}
	return true
}
