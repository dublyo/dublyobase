package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	FileUploadSessionTTL = 24 * time.Hour
	maxUploadChunks      = 10_000
)

type FileUploadSession struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"-"`
	ProjectSlug       string     `json:"project"`
	Collection        string     `json:"collection"`
	RecordID          string     `json:"record"`
	Field             string     `json:"field"`
	FileID            string     `json:"fileId"`
	Filename          string     `json:"filename"`
	Mode              string     `json:"mode"`
	TotalSize         int64      `json:"totalSize"`
	ChunkSize         int64      `json:"chunkSize"`
	TotalChunks       int        `json:"totalChunks"`
	ChecksumSHA256    string     `json:"checksumSha256,omitempty"`
	Status            string     `json:"status"`
	CreatorRole       string     `json:"-"`
	CreatorSubject    string     `json:"-"`
	CreatorCollection string     `json:"-"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	ExpiresAt         time.Time  `json:"expiresAt"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
	CanceledAt        *time.Time `json:"canceledAt,omitempty"`
}

type CreateFileUploadSessionInput struct {
	Collection     string
	RecordID       string
	Field          string
	Filename       string
	Mode           string
	TotalSize      int64
	ChunkSize      int64
	ChecksumSHA256 string
}

type FileUploadChunk struct {
	Index          int    `json:"index"`
	Size           int64  `json:"size"`
	ChecksumSHA256 string `json:"checksumSha256"`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func CreateFileUploadSession(ctx context.Context, pool *pgxpool.Pool, cfg *Config, auth *RecordAuth, input CreateFileUploadSessionInput, now time.Time) (*FileUploadSession, error) {
	if err := ValidateUUID(input.RecordID); err != nil {
		return nil, err
	}
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, input.Collection)
	if err != nil {
		return nil, err
	}
	fieldName := NormalizeIdentifier(input.Field)
	field, err := fileField(collection, fieldName)
	if err != nil {
		return nil, err
	}
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = "replace"
	}
	if mode != "replace" && mode != "append" {
		return nil, fmt.Errorf("%w: file upload mode must be replace or append", ErrValidation)
	}
	if !fieldIsMultiple(field) && mode == "append" {
		return nil, fmt.Errorf("%w: field %q accepts one file", ErrValidation, field.Name)
	}
	if input.TotalSize <= 0 {
		return nil, fmt.Errorf("%w: upload size must be greater than zero", ErrValidation)
	}
	if input.TotalSize > MaxUploadBytes(cfg) {
		return nil, ErrFileTooLarge
	}
	if maxSize := maxSizeOption(field, 0); maxSize > 0 && input.TotalSize > maxSize {
		return nil, ErrFileTooLarge
	}
	if input.ChunkSize <= 0 {
		return nil, fmt.Errorf("%w: chunk size must be greater than zero", ErrValidation)
	}
	if input.ChunkSize > input.TotalSize {
		return nil, fmt.Errorf("%w: chunk size must not exceed upload size", ErrValidation)
	}
	totalChunks := int((input.TotalSize + input.ChunkSize - 1) / input.ChunkSize)
	if totalChunks > maxUploadChunks {
		return nil, fmt.Errorf("%w: upload has too many chunks", ErrValidation)
	}
	checksum, err := normalizeSHA256(input.ChecksumSHA256)
	if err != nil {
		return nil, err
	}
	if err := ensureRecordCanUploadFile(ctx, pool, auth, collection, input.RecordID); err != nil {
		return nil, err
	}
	if err := EnsureProjectStorageQuota(ctx, pool, &auth.Project, input.TotalSize); err != nil {
		return nil, err
	}
	filename := sanitizeFilename(input.Filename)
	expiresAt := now.UTC().Add(FileUploadSessionTTL)

	row := pool.QueryRow(ctx, `
		insert into _dbo.file_upload_sessions
			(project_id, collection, record_id, field, filename, mode, total_size, chunk_size, total_chunks,
			 checksum_sha256, creator_role, creator_subject, creator_collection, expires_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		returning id::text, project_id::text, $15::text, collection, record_id::text, field, file_id::text,
			filename, mode, total_size, chunk_size, total_chunks, checksum_sha256, status,
			creator_role, creator_subject, creator_collection, created_at, updated_at, expires_at, completed_at, canceled_at`,
		auth.Project.ID,
		collection.Name,
		input.RecordID,
		field.Name,
		filename,
		mode,
		input.TotalSize,
		input.ChunkSize,
		totalChunks,
		checksum,
		string(auth.Role),
		auth.Subject,
		auth.Collection,
		expiresAt,
		auth.Project.Slug,
	)
	return scanFileUploadSession(row)
}

func GetOpenFileUploadSession(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, projectSlug string, uploadID string, now time.Time) (*FileUploadSession, error) {
	session, err := getFileUploadSession(ctx, pool, projectSlug, uploadID)
	if err != nil {
		return nil, err
	}
	if err := authorizeFileUploadSession(auth, session); err != nil {
		return nil, err
	}
	if session.Status != "open" {
		return nil, ErrUploadConflict
	}
	if !now.UTC().Before(session.ExpiresAt) {
		return nil, ErrUploadExpired
	}
	return session, nil
}

func ClaimFileUploadSessionForComplete(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, projectSlug string, uploadID string, now time.Time) (*FileUploadSession, error) {
	if err := ValidateUUID(uploadID); err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, fileUploadSessionSelectSQL()+` where s.id = $1 and p.slug = $2 for update`, uploadID, projectSlug)
	session, err := scanFileUploadSession(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrUploadNotFound
		}
		return nil, err
	}
	if err := authorizeFileUploadSession(auth, session); err != nil {
		return nil, err
	}
	if session.Status != "open" {
		return nil, ErrUploadConflict
	}
	if !now.UTC().Before(session.ExpiresAt) {
		return nil, ErrUploadExpired
	}
	if err := validateUploadedChunks(ctx, tx, session); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		update _dbo.file_upload_sessions
		set status = 'completing', updated_at = now()
		where id = $1 and status = 'open'`,
		session.ID,
	); err != nil {
		return nil, err
	}
	session.Status = "completing"
	session.UpdatedAt = now.UTC()
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return session, nil
}

func ReopenFileUploadSession(ctx context.Context, pool *pgxpool.Pool, uploadID string) error {
	_, err := pool.Exec(ctx, `
		update _dbo.file_upload_sessions
		set status = 'open', updated_at = now()
		where id = $1 and status = 'completing'`,
		uploadID,
	)
	return err
}

func MarkFileUploadSessionCompleted(ctx context.Context, pool *pgxpool.Pool, uploadID string) error {
	_, err := pool.Exec(ctx, `
		update _dbo.file_upload_sessions
		set status = 'completed', completed_at = now(), updated_at = now()
		where id = $1 and status = 'completing'`,
		uploadID,
	)
	return err
}

func CancelFileUploadSession(ctx context.Context, pool *pgxpool.Pool, cfg *Config, auth *RecordAuth, projectSlug string, uploadID string, now time.Time) error {
	session, err := GetOpenFileUploadSession(ctx, pool, auth, projectSlug, uploadID, now)
	if err != nil {
		return err
	}
	cmd, err := pool.Exec(ctx, `
		update _dbo.file_upload_sessions
		set status = 'canceled', canceled_at = now(), updated_at = now()
		where id = $1 and status = 'open'`,
		session.ID,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrUploadConflict
	}
	return RemoveFileUploadTemp(cfg, session)
}

func StoreFileUploadChunk(cfg *Config, session *FileUploadSession, index int, checksumHeader string, r io.Reader) (FileUploadChunk, error) {
	expectedSize, err := session.ExpectedChunkSize(index)
	if err != nil {
		return FileUploadChunk{}, err
	}
	expectedChecksum, err := normalizeSHA256(checksumHeader)
	if err != nil {
		return FileUploadChunk{}, err
	}
	chunkPath, err := fileUploadChunkPath(cfg, session, index)
	if err != nil {
		return FileUploadChunk{}, err
	}
	if err := os.MkdirAll(filepath.Dir(chunkPath), 0o750); err != nil {
		return FileUploadChunk{}, err
	}
	tmp := chunkPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return FileUploadChunk{}, err
	}
	removeTmp := true
	defer func() {
		f.Close()
		if removeTmp {
			_ = os.Remove(tmp)
		}
	}()

	hash := sha256.New()
	limited := &io.LimitedReader{R: r, N: expectedSize + 1}
	size, err := io.Copy(io.MultiWriter(f, hash), limited)
	if err != nil {
		return FileUploadChunk{}, err
	}
	if err := f.Close(); err != nil {
		return FileUploadChunk{}, err
	}
	if size != expectedSize {
		return FileUploadChunk{}, fmt.Errorf("%w: chunk size does not match expected size", ErrValidation)
	}
	checksum := hex.EncodeToString(hash.Sum(nil))
	if expectedChecksum != "" && checksum != expectedChecksum {
		return FileUploadChunk{}, ErrChecksumMismatch
	}
	if err := os.Rename(tmp, chunkPath); err != nil {
		return FileUploadChunk{}, err
	}
	removeTmp = false
	return FileUploadChunk{Index: index, Size: size, ChecksumSHA256: checksum}, nil
}

func RecordFileUploadChunk(ctx context.Context, pool *pgxpool.Pool, session *FileUploadSession, chunk FileUploadChunk) error {
	row := pool.QueryRow(ctx, `
		with open_session as (
			select id
			from _dbo.file_upload_sessions
			where id = $1 and status = 'open' and expires_at > now()
		)
		insert into _dbo.file_upload_chunks (session_id, chunk_index, size, checksum_sha256)
		select id, $2, $3, $4
		from open_session
		on conflict (session_id, chunk_index) do update
		set size = excluded.size,
			checksum_sha256 = excluded.checksum_sha256,
			updated_at = now()
		returning chunk_index`,
		session.ID,
		chunk.Index,
		chunk.Size,
		chunk.ChecksumSHA256,
	)
	var stored int
	if err := row.Scan(&stored); err != nil {
		if err == pgx.ErrNoRows {
			return ErrUploadConflict
		}
		return err
	}
	return nil
}

func AssembleFileUploadSession(ctx context.Context, cfg *Config, session *FileUploadSession, checksumOverride string) (FileMeta, error) {
	checksum, err := normalizeSHA256(checksumOverride)
	if err != nil {
		return FileMeta{}, err
	}
	if session.ChecksumSHA256 != "" {
		if checksum != "" && checksum != session.ChecksumSHA256 {
			return FileMeta{}, fmt.Errorf("%w: final checksum does not match session checksum", ErrValidation)
		}
		checksum = session.ChecksumSHA256
	}

	store, err := NewObjectStore(cfg)
	if err != nil {
		return FileMeta{}, err
	}
	dir, err := fileUploadSessionDir(cfg, session)
	if err != nil {
		return FileMeta{}, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return FileMeta{}, err
	}
	assembledPath := filepath.Join(dir, "assembled.tmp")
	out, err := os.OpenFile(assembledPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return FileMeta{}, err
	}
	defer func() {
		out.Close()
	}()

	hash := sha256.New()
	sniff := &sniffBuffer{}
	var written int64
	for index := 0; index < session.TotalChunks; index++ {
		chunkPath, err := fileUploadChunkPath(cfg, session, index)
		if err != nil {
			return FileMeta{}, err
		}
		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			if os.IsNotExist(err) {
				return FileMeta{}, fmt.Errorf("%w: missing chunk", ErrValidation)
			}
			return FileMeta{}, err
		}
		n, copyErr := io.Copy(io.MultiWriter(out, hash, sniff), chunkFile)
		closeErr := chunkFile.Close()
		if copyErr != nil {
			return FileMeta{}, copyErr
		}
		if closeErr != nil {
			return FileMeta{}, closeErr
		}
		written += n
	}
	if err := out.Close(); err != nil {
		return FileMeta{}, err
	}
	if written != session.TotalSize {
		return FileMeta{}, fmt.Errorf("%w: assembled file size does not match session size", ErrValidation)
	}
	finalChecksum := hex.EncodeToString(hash.Sum(nil))
	if checksum != "" && finalChecksum != checksum {
		return FileMeta{}, ErrChecksumMismatch
	}
	in, err := os.Open(assembledPath)
	if err != nil {
		return FileMeta{}, err
	}
	defer in.Close()
	rel := filepath.ToSlash(filepath.Join(session.ProjectSlug, session.Collection, session.RecordID, session.Field, session.FileID, "original"))
	if err := store.Put(ctx, rel, in, written, http.DetectContentType(sniff.buf), finalChecksum); err != nil {
		return FileMeta{}, err
	}

	return FileMeta{
		ID:      session.FileID,
		Name:    sanitizeFilename(session.Filename),
		Size:    written,
		Mime:    http.DetectContentType(sniff.buf),
		Created: time.Now().UTC().Format(time.RFC3339Nano),
		Path:    rel,
	}, nil
}

func RemoveFileUploadTemp(cfg *Config, session *FileUploadSession) error {
	dir, err := fileUploadSessionDir(cfg, session)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func RemoveFileUploadChunk(cfg *Config, session *FileUploadSession, index int) error {
	chunkPath, err := fileUploadChunkPath(cfg, session, index)
	if err != nil {
		return err
	}
	return os.Remove(chunkPath)
}

func CleanupExpiredFileUploadSessions(ctx context.Context, pool *pgxpool.Pool, cfg *Config, now time.Time, limit int) (int, error) {
	if limit < 1 {
		limit = 50
	}
	rows, err := pool.Query(ctx, fileUploadSessionSelectSQL()+`
		where s.status in ('open', 'completing') and s.expires_at <= $1
		order by s.expires_at asc
		limit $2`,
		now.UTC(),
		limit,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var sessions []*FileUploadSession
	for rows.Next() {
		session, err := scanFileUploadSession(rows)
		if err != nil {
			return 0, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, session := range sessions {
		_, err := pool.Exec(ctx, `
			update _dbo.file_upload_sessions
			set status = 'canceled', canceled_at = now(), updated_at = now()
			where id = $1 and status in ('open', 'completing')`,
			session.ID,
		)
		if err != nil {
			return len(sessions), err
		}
		if err := RemoveFileUploadTemp(cfg, session); err != nil {
			return len(sessions), err
		}
	}
	return len(sessions), nil
}

func (s *FileUploadSession) ExpectedChunkSize(index int) (int64, error) {
	if index < 0 || index >= s.TotalChunks {
		return 0, fmt.Errorf("%w: chunk index out of range", ErrValidation)
	}
	offset := int64(index) * s.ChunkSize
	remaining := s.TotalSize - offset
	if remaining < s.ChunkSize {
		return remaining, nil
	}
	return s.ChunkSize, nil
}

func ensureRecordCanUploadFile(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collection *Collection, recordID string) error {
	table := quoteIdent(auth.Project.SchemaName, collection.Name)
	return withRecordTx(ctx, pool, auth, "update", func(tx pgx.Tx) error {
		var id string
		query := fmt.Sprintf(`select id::text from %s where id = $1`, table)
		if err := tx.QueryRow(ctx, query, recordID).Scan(&id); err != nil {
			if err == pgx.ErrNoRows {
				return ErrRecordNotFound
			}
			return mapRecordDBError(err)
		}
		return nil
	})
}

func validateUploadedChunks(ctx context.Context, tx pgx.Tx, session *FileUploadSession) error {
	rows, err := tx.Query(ctx, `
		select chunk_index, size
		from _dbo.file_upload_chunks
		where session_id = $1
		order by chunk_index asc`,
		session.ID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	seen := make(map[int]int64, session.TotalChunks)
	for rows.Next() {
		var index int
		var size int64
		if err := rows.Scan(&index, &size); err != nil {
			return err
		}
		seen[index] = size
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index := 0; index < session.TotalChunks; index++ {
		size, ok := seen[index]
		if !ok {
			return fmt.Errorf("%w: all chunks must be uploaded before completion", ErrValidation)
		}
		expected, err := session.ExpectedChunkSize(index)
		if err != nil {
			return err
		}
		if size != expected {
			return fmt.Errorf("%w: stored chunk size does not match expected size", ErrValidation)
		}
	}
	return nil
}

func getFileUploadSession(ctx context.Context, pool *pgxpool.Pool, projectSlug string, uploadID string) (*FileUploadSession, error) {
	if err := ValidateUUID(uploadID); err != nil {
		return nil, err
	}
	row := pool.QueryRow(ctx, fileUploadSessionSelectSQL()+` where s.id = $1 and p.slug = $2`, uploadID, projectSlug)
	session, err := scanFileUploadSession(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrUploadNotFound
		}
		return nil, err
	}
	return session, nil
}

func fileUploadSessionSelectSQL() string {
	return `
		select s.id::text, s.project_id::text, p.slug, s.collection, s.record_id::text, s.field, s.file_id::text,
			s.filename, s.mode, s.total_size, s.chunk_size, s.total_chunks, s.checksum_sha256, s.status,
			s.creator_role, s.creator_subject, s.creator_collection,
			s.created_at, s.updated_at, s.expires_at, s.completed_at, s.canceled_at
		from _dbo.file_upload_sessions s
		join _dbo.projects p on p.id = s.project_id`
}

func scanFileUploadSession(row rowScanner) (*FileUploadSession, error) {
	session := &FileUploadSession{}
	if err := row.Scan(
		&session.ID,
		&session.ProjectID,
		&session.ProjectSlug,
		&session.Collection,
		&session.RecordID,
		&session.Field,
		&session.FileID,
		&session.Filename,
		&session.Mode,
		&session.TotalSize,
		&session.ChunkSize,
		&session.TotalChunks,
		&session.ChecksumSHA256,
		&session.Status,
		&session.CreatorRole,
		&session.CreatorSubject,
		&session.CreatorCollection,
		&session.CreatedAt,
		&session.UpdatedAt,
		&session.ExpiresAt,
		&session.CompletedAt,
		&session.CanceledAt,
	); err != nil {
		return nil, err
	}
	return session, nil
}

func authorizeFileUploadSession(auth *RecordAuth, session *FileUploadSession) error {
	if auth.Project.Slug != session.ProjectSlug {
		return ErrUploadNotFound
	}
	if auth.Role == RecordRoleService {
		return nil
	}
	if session.CreatorRole != string(auth.Role) {
		return ErrUploadNotFound
	}
	if session.CreatorSubject != auth.Subject || session.CreatorCollection != auth.Collection {
		return ErrUploadNotFound
	}
	return nil
}

func fileUploadSessionDir(cfg *Config, session *FileUploadSession) (string, error) {
	return localStoragePath(cfg, "_uploads", session.ProjectSlug, session.ID)
}

func fileUploadChunkPath(cfg *Config, session *FileUploadSession, index int) (string, error) {
	if _, err := session.ExpectedChunkSize(index); err != nil {
		return "", err
	}
	dir, err := fileUploadSessionDir(cfg, session)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, strconv.Itoa(index)+".part"), nil
}

func normalizeSHA256(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "", nil
	}
	if len(raw) != sha256.Size*2 {
		return "", fmt.Errorf("%w: checksumSha256 must be a 64-character hex SHA-256", ErrValidation)
	}
	if _, err := hex.DecodeString(raw); err != nil {
		return "", fmt.Errorf("%w: checksumSha256 must be hex", ErrValidation)
	}
	return raw, nil
}
