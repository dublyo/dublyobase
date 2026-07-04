package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const fileTokenTTL = 5 * time.Minute
const maxThumbnailSourcePixels = 40_000_000

type FileMeta struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Size    int64             `json:"size"`
	Mime    string            `json:"mime"`
	Created string            `json:"created"`
	Path    string            `json:"path"`
	Thumbs  map[string]string `json:"thumbs,omitempty"`
}

type FileTokenClaims struct {
	Kind       string `json:"kind"`
	Project    string `json:"project"`
	Collection string `json:"collection"`
	RecordID   string `json:"record"`
	Field      string `json:"field"`
	FileID     string `json:"file"`
	jwt.RegisteredClaims
}

type sniffBuffer struct {
	buf []byte
}

func (s *sniffBuffer) Write(p []byte) (int, error) {
	if len(s.buf) < 512 {
		n := 512 - len(s.buf)
		if n > len(p) {
			n = len(p)
		}
		s.buf = append(s.buf, p[:n]...)
	}
	return len(p), nil
}

func MaxUploadBytes(cfg *Config) int64 {
	return int64(cfg.MaxUploadMB) * 1024 * 1024
}

func StoreUploadedFile(ctx context.Context, cfg *Config, projectSlug string, collectionName string, recordID string, fieldName string, filename string, r io.Reader) (FileMeta, error) {
	if err := ValidateUUID(recordID); err != nil {
		return FileMeta{}, err
	}
	fileID, err := newFileID()
	if err != nil {
		return FileMeta{}, err
	}
	store, err := NewObjectStore(cfg)
	if err != nil {
		return FileMeta{}, err
	}

	tmp, err := os.CreateTemp("", "dublyobase-upload-*")
	if err != nil {
		return FileMeta{}, err
	}
	defer func() {
		tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	maxBytes := MaxUploadBytes(cfg)
	sniff := &sniffBuffer{}
	hash := sha256.New()
	limited := &io.LimitedReader{R: r, N: maxBytes + 1}
	size, err := io.Copy(io.MultiWriter(tmp, sniff, hash), limited)
	if err != nil {
		return FileMeta{}, err
	}
	if err := tmp.Sync(); err != nil {
		return FileMeta{}, err
	}
	if size > maxBytes {
		return FileMeta{}, ErrFileTooLarge
	}

	mime := http.DetectContentType(sniff.buf)
	rel := filepath.ToSlash(filepath.Join(projectSlug, collectionName, recordID, fieldName, fileID, "original"))
	if err := store.Put(ctx, rel, tmp, size, mime, hex.EncodeToString(hash.Sum(nil))); err != nil {
		return FileMeta{}, err
	}
	return FileMeta{
		ID:      fileID,
		Name:    sanitizeFilename(filename),
		Size:    size,
		Mime:    mime,
		Created: time.Now().UTC().Format(time.RFC3339Nano),
		Path:    rel,
	}, nil
}

func UpdateRecordFileField(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, recordID string, fieldName string, mode string, newFiles []FileMeta) (Record, []FileMeta, error) {
	if err := ValidateUUID(recordID); err != nil {
		return nil, nil, err
	}
	if len(newFiles) == 0 {
		return nil, nil, fmt.Errorf("%w: at least one file is required", ErrValidation)
	}
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return nil, nil, err
	}
	fieldName = NormalizeIdentifier(fieldName)
	field, err := fileField(collection, fieldName)
	if err != nil {
		return nil, nil, err
	}
	multiple := fieldIsMultiple(field)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "replace"
	}
	if mode != "replace" && mode != "append" {
		return nil, nil, fmt.Errorf("%w: file upload mode must be replace or append", ErrValidation)
	}
	if !multiple && (mode == "append" || len(newFiles) > 1) {
		return nil, nil, fmt.Errorf("%w: field %q accepts one file", ErrValidation, field.Name)
	}

	columns := allRecordColumns(collection)
	table := quoteIdent(auth.Project.SchemaName, collection.Name)
	var out Record
	var removed []FileMeta
	err = withRecordTx(ctx, pool, auth, "update", func(tx pgx.Tx) error {
		var oldRaw []byte
		query := fmt.Sprintf(`select %s from %s where id = $1 for update`, quoteIdent(field.Name), table)
		if err := tx.QueryRow(ctx, query, recordID).Scan(&oldRaw); err != nil {
			if err == pgx.ErrNoRows {
				return ErrRecordNotFound
			}
			return mapRecordDBError(err)
		}
		oldFiles, err := parseFileMetas(oldRaw, multiple)
		if err != nil {
			return err
		}
		next := newFiles
		if mode == "append" {
			next = append(append([]FileMeta{}, oldFiles...), newFiles...)
		} else {
			removed = oldFiles
		}
		if err := validateFileFieldSelection(field, next); err != nil {
			return err
		}
		value, err := fileMetasJSON(next, multiple)
		if err != nil {
			return err
		}
		update := fmt.Sprintf(`update %s set %s = $1::jsonb, updated = now() where id = $2 returning %s`,
			table,
			quoteIdent(field.Name),
			selectList(columns),
		)
		record, err := queryOneRecord(ctx, tx, update, columns, value, recordID)
		if err != nil {
			return err
		}
		out = record
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return out, removed, nil
}

func AuthorizeFileUpload(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, recordID string, fieldName string) error {
	if err := ValidateUUID(recordID); err != nil {
		return err
	}
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return err
	}
	fieldName = NormalizeIdentifier(fieldName)
	if _, err := fileField(collection, fieldName); err != nil {
		return err
	}
	table := quoteIdent(auth.Project.SchemaName, collection.Name)
	return withRecordTx(ctx, pool, auth, "update", func(tx pgx.Tx) error {
		var id string
		query := fmt.Sprintf(`select id from %s where id = $1 for update`, table)
		if err := tx.QueryRow(ctx, query, recordID).Scan(&id); err != nil {
			if err == pgx.ErrNoRows {
				return ErrRecordNotFound
			}
			return mapRecordDBError(err)
		}
		return nil
	})
}

func MintFileToken(ctx context.Context, pool *pgxpool.Pool, cfg *Config, auth *RecordAuth, collectionName string, recordID string, fieldName string, fileID string, now time.Time) (string, time.Time, error) {
	record, err := GetRecord(ctx, pool, auth, collectionName, recordID)
	if err != nil {
		return "", time.Time{}, err
	}
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return "", time.Time{}, err
	}
	field, err := fileField(collection, fieldName)
	if err != nil {
		return "", time.Time{}, err
	}
	files, err := parseFileMetas(record[field.Name], fieldIsMultiple(field))
	if err != nil {
		return "", time.Time{}, err
	}
	if _, ok := findFileMeta(files, fileID); !ok {
		return "", time.Time{}, ErrFileNotFound
	}
	expiresAt := now.UTC().Add(fileTokenTTL)
	claims := FileTokenClaims{
		Kind:       "file",
		Project:    auth.Project.Slug,
		Collection: collection.Name,
		RecordID:   recordID,
		Field:      field.Name,
		FileID:     fileID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now.UTC()),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func ParseFileToken(secret string, token string, now time.Time) (*FileTokenClaims, error) {
	if len(secret) < 32 {
		return nil, ErrInvalidAuthToken
	}
	claims := &FileTokenClaims{}
	parsed, err := jwt.ParseWithClaims(
		strings.TrimSpace(token),
		claims,
		func(t *jwt.Token) (any, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return []byte(secret), nil
		},
		jwt.WithTimeFunc(func() time.Time { return now.UTC() }),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil || !parsed.Valid || claims.Kind != "file" {
		return nil, ErrInvalidAuthToken
	}
	if claims.Project == "" || claims.Collection == "" || claims.RecordID == "" || claims.Field == "" || claims.FileID == "" {
		return nil, ErrInvalidAuthToken
	}
	return claims, nil
}

func GetFileForToken(ctx context.Context, pool *pgxpool.Pool, claims *FileTokenClaims) (FileMeta, error) {
	project, err := GetProject(ctx, pool, claims.Project)
	if err != nil {
		return FileMeta{}, err
	}
	collection, err := GetCollection(ctx, pool, claims.Project, claims.Collection)
	if err != nil {
		return FileMeta{}, err
	}
	field, err := fileField(collection, claims.Field)
	if err != nil {
		return FileMeta{}, err
	}
	_, roles := ProjectNames(project.Slug)
	auth := newRecordAuth(project, RecordRoleService, roles.Service, "", "")
	table := quoteIdent(project.SchemaName, collection.Name)
	var meta FileMeta
	err = withRecordTx(ctx, pool, auth, "view", func(tx pgx.Tx) error {
		var raw []byte
		query := fmt.Sprintf(`select %s from %s where id = $1`, quoteIdent(field.Name), table)
		if err := tx.QueryRow(ctx, query, claims.RecordID).Scan(&raw); err != nil {
			if err == pgx.ErrNoRows {
				return ErrRecordNotFound
			}
			return mapRecordDBError(err)
		}
		files, err := parseFileMetas(raw, fieldIsMultiple(field))
		if err != nil {
			return err
		}
		found, ok := findFileMeta(files, claims.FileID)
		if !ok {
			return ErrFileNotFound
		}
		meta = found
		return nil
	})
	return meta, err
}

func FilePath(cfg *Config, meta FileMeta) (string, error) {
	if meta.Path == "" {
		return "", ErrFileNotFound
	}
	return localStoragePath(cfg, strings.Split(filepath.ToSlash(meta.Path), "/")...)
}

func OpenStoredFile(ctx context.Context, cfg *Config, meta FileMeta, thumb string) (*StoredObject, string, error) {
	if meta.Path == "" {
		return nil, "", ErrFileNotFound
	}
	store, err := NewObjectStore(cfg)
	if err != nil {
		return nil, "", err
	}
	contentType := meta.Mime
	key := meta.Path
	if thumb != "" {
		thumbMeta, err := EnsureThumbnail(ctx, cfg, meta, thumb)
		if err != nil {
			return nil, "", err
		}
		key = thumbMeta.Path
		contentType = thumbMeta.Mime
	}
	obj, err := store.Get(ctx, key)
	if err != nil {
		return nil, "", err
	}
	if contentType == "" {
		contentType = obj.Info.ContentType
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return obj, contentType, nil
}

func ThumbnailPath(cfg *Config, meta FileMeta, thumb string) (string, error) {
	if _, _, normalized, err := ParseThumbSize(thumb); err != nil {
		return "", err
	} else {
		thumb = normalized
	}
	original, err := FilePath(cfg, meta)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(original), "thumbs", thumb+".jpg"), nil
}

func EnsureThumbnail(ctx context.Context, cfg *Config, meta FileMeta, thumb string) (FileMeta, error) {
	w, h, normalized, err := ParseThumbSize(thumb)
	if err != nil {
		return FileMeta{}, err
	}
	store, err := NewObjectStore(cfg)
	if err != nil {
		return FileMeta{}, err
	}
	thumbKey := filepath.ToSlash(path.Join(path.Dir(filepath.ToSlash(meta.Path)), "thumbs", normalized+".jpg"))
	if obj, err := store.Get(ctx, thumbKey); err == nil {
		obj.Body.Close()
		return FileMeta{ID: meta.ID, Name: normalized + ".jpg", Size: obj.Info.Size, Mime: "image/jpeg", Created: meta.Created, Path: thumbKey}, nil
	}
	original, err := store.Get(ctx, meta.Path)
	if err != nil {
		return FileMeta{}, err
	}
	cfgInfo, _, err := image.DecodeConfig(original.Body)
	original.Body.Close()
	if err != nil {
		return FileMeta{}, fmt.Errorf("%w: thumbnail source must be an image", ErrValidation)
	}
	if cfgInfo.Width <= 0 || cfgInfo.Height <= 0 || cfgInfo.Width*cfgInfo.Height > maxThumbnailSourcePixels {
		return FileMeta{}, fmt.Errorf("%w: thumbnail source dimensions are too large", ErrValidation)
	}
	original, err = store.Get(ctx, meta.Path)
	if err != nil {
		return FileMeta{}, err
	}
	defer original.Body.Close()
	src, _, err := image.Decode(original.Body)
	if err != nil {
		return FileMeta{}, fmt.Errorf("%w: thumbnail source must be an image", ErrValidation)
	}
	dst := resizeNearest(src, w, h)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82}); err != nil {
		return FileMeta{}, err
	}
	hash := sha256.Sum256(buf.Bytes())
	reader := bytes.NewReader(buf.Bytes())
	if err := store.Put(ctx, thumbKey, reader, int64(buf.Len()), "image/jpeg", hex.EncodeToString(hash[:])); err != nil {
		return FileMeta{}, err
	}
	return FileMeta{ID: meta.ID, Name: normalized + ".jpg", Size: int64(buf.Len()), Mime: "image/jpeg", Created: time.Now().UTC().Format(time.RFC3339Nano), Path: thumbKey}, nil
}

func ParseThumbSize(raw string) (int, int, string, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(raw)), "x")
	if len(parts) != 2 {
		return 0, 0, "", fmt.Errorf("%w: thumb must use WxH format", ErrValidation)
	}
	w, errW := strconv.Atoi(parts[0])
	h, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil || w < 1 || h < 1 || w > 2000 || h > 2000 {
		return 0, 0, "", fmt.Errorf("%w: thumb dimensions must be between 1 and 2000", ErrValidation)
	}
	return w, h, fmt.Sprintf("%dx%d", w, h), nil
}

func RemoveStoredFiles(ctx context.Context, cfg *Config, files []FileMeta) error {
	store, err := NewObjectStore(cfg)
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.Path == "" {
			continue
		}
		if err := store.DeletePrefix(ctx, path.Dir(filepath.ToSlash(file.Path))); err != nil {
			return err
		}
	}
	return nil
}

func RemoveRecordStorage(ctx context.Context, cfg *Config, projectSlug string, collectionName string, recordID string) error {
	if err := ValidateUUID(recordID); err != nil {
		return err
	}
	store, err := NewObjectStore(cfg)
	if err != nil {
		return err
	}
	return store.DeletePrefix(ctx, filepath.ToSlash(filepath.Join(projectSlug, collectionName, recordID)))
}

func RemoveCollectionStorage(ctx context.Context, cfg *Config, projectSlug string, collectionName string) error {
	store, err := NewObjectStore(cfg)
	if err != nil {
		return err
	}
	return store.DeletePrefix(ctx, filepath.ToSlash(filepath.Join(projectSlug, collectionName)))
}

func fileField(collection *Collection, fieldName string) (Field, error) {
	fieldName = NormalizeIdentifier(fieldName)
	field, ok := fieldByName(collection.Fields)[fieldName]
	if !ok {
		return Field{}, fmt.Errorf("%w: unknown field %q", ErrValidation, fieldName)
	}
	if field.Type != "file" {
		return Field{}, fmt.Errorf("%w: field %q is not a file field", ErrValidation, field.Name)
	}
	return field, nil
}

func parseFileMetas(raw any, multiple bool) ([]FileMeta, error) {
	if raw == nil {
		return nil, nil
	}
	var b []byte
	switch v := raw.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		var err error
		b, err = json.Marshal(v)
		if err != nil {
			return nil, err
		}
	}
	if len(b) == 0 || string(b) == "null" {
		return nil, nil
	}
	if multiple {
		var out []FileMeta
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, fmt.Errorf("%w: invalid file metadata", ErrValidation)
		}
		return validFileMetas(out)
	}
	var one FileMeta
	if err := json.Unmarshal(b, &one); err != nil {
		return nil, fmt.Errorf("%w: invalid file metadata", ErrValidation)
	}
	if one.ID == "" {
		return nil, nil
	}
	out, err := validFileMetas([]FileMeta{one})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func fileMetasJSON(files []FileMeta, multiple bool) (any, error) {
	if len(files) == 0 && !multiple {
		return nil, nil
	}
	var v any = files
	if !multiple {
		v = files[0]
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func validFileMetas(files []FileMeta) ([]FileMeta, error) {
	seen := map[string]struct{}{}
	for _, file := range files {
		if err := ValidateUUID(file.ID); err != nil {
			return nil, fmt.Errorf("%w: invalid file id", ErrValidation)
		}
		if file.Path == "" {
			return nil, fmt.Errorf("%w: invalid file path", ErrValidation)
		}
		if _, ok := seen[file.ID]; ok {
			return nil, fmt.Errorf("%w: duplicate file id", ErrValidation)
		}
		seen[file.ID] = struct{}{}
	}
	return files, nil
}

func validateFileFieldSelection(field Field, files []FileMeta) error {
	if field.Required && len(files) == 0 {
		return fmt.Errorf("%w: field %q requires a file", ErrValidation, field.Name)
	}
	if maxSelect, ok := intOption(field.Options, "maxSelect"); ok && maxSelect > 0 && len(files) > maxSelect {
		return fmt.Errorf("%w: field %q accepts no more than %d file(s)", ErrValidation, field.Name, maxSelect)
	}
	maxSize := maxSizeOption(field, 0)
	if maxSize > 0 {
		for _, file := range files {
			if file.Size > maxSize {
				return fmt.Errorf("%w: field %q file exceeds max size", ErrValidation, field.Name)
			}
		}
	}
	allowedTypes := stringSlice(field.Options["mimeTypes"])
	if len(allowedTypes) == 0 {
		return nil
	}
	for _, file := range files {
		allowed := false
		fileMime := baseMIMEType(file.Mime)
		for _, mimeType := range allowedTypes {
			allowedMime := strings.ToLower(strings.TrimSpace(mimeType))
			if fileMime == allowedMime || strings.HasSuffix(allowedMime, "/*") && strings.HasPrefix(fileMime, strings.TrimSuffix(allowedMime, "*")) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: field %q does not allow MIME type %s", ErrValidation, field.Name, file.Mime)
		}
	}
	return nil
}

func baseMIMEType(value string) string {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(value))
	}
	return strings.ToLower(mediaType)
}

func findFileMeta(files []FileMeta, fileID string) (FileMeta, bool) {
	for _, file := range files {
		if file.ID == fileID {
			return file, true
		}
	}
	return FileMeta{}, false
}

func localStoragePath(cfg *Config, parts ...string) (string, error) {
	root := filepath.Clean(cfg.StorageLocalPath)
	clean := []string{root}
	for _, part := range parts {
		if !safeStorageSegment(part) {
			return "", fmt.Errorf("%w: invalid storage path", ErrValidation)
		}
		clean = append(clean, part)
	}
	p := filepath.Join(clean...)
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: invalid storage path", ErrValidation)
	}
	return p, nil
}

func removeDirAndEmptyParents(cfg *Config, dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	root := filepath.Clean(cfg.StorageLocalPath)
	for parent := filepath.Dir(dir); parent != root && parent != "." && parent != string(filepath.Separator); parent = filepath.Dir(parent) {
		if err := os.Remove(parent); err != nil {
			break
		}
	}
	return nil
}

func safeStorageSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`+"\x00")
}

func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	if len(name) > 180 {
		name = name[:180]
	}
	return name
}

func newFileID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return formatUUIDBytes(b), nil
}

func resizeNearest(src image.Image, width int, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	b := src.Bounds()
	for y := 0; y < height; y++ {
		sy := b.Min.Y + y*b.Dy()/height
		for x := 0; x < width; x++ {
			sx := b.Min.X + x*b.Dx()/width
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
