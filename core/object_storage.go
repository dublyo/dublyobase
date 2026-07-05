package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var emptyPayloadSHA256 = hex.EncodeToString(sha256.New().Sum(nil))

type ObjectInfo struct {
	Size        int64
	ContentType string
	ModTime     time.Time
}

type StoredObject struct {
	Body io.ReadCloser
	Info ObjectInfo
}

type ObjectStore interface {
	Put(ctx context.Context, key string, r io.ReadSeeker, size int64, contentType string, payloadSHA256 string) error
	Get(ctx context.Context, key string) (*StoredObject, error)
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) error
}

func NewObjectStore(cfg *Config) (ObjectStore, error) {
	switch cfg.StorageType {
	case StorageLocal:
		return localObjectStore{cfg: cfg}, nil
	case StorageS3:
		return newS3ObjectStore(cfg)
	default:
		return nil, fmt.Errorf("%w: unsupported storage type %q", ErrValidation, cfg.StorageType)
	}
}

func TestObjectStore(ctx context.Context, cfg *Config) error {
	store, err := NewObjectStore(cfg)
	if err != nil {
		return err
	}
	id, err := newFileID()
	if err != nil {
		return err
	}
	key := filepath.ToSlash(filepath.Join("_health", id+".txt"))
	body := strings.NewReader("dublyobase storage test\n")
	hash := sha256.Sum256([]byte("dublyobase storage test\n"))
	if err := store.Put(ctx, key, body, int64(body.Len()), "text/plain; charset=utf-8", hex.EncodeToString(hash[:])); err != nil {
		return err
	}
	obj, err := store.Get(ctx, key)
	if err != nil {
		_ = store.Delete(ctx, key)
		return err
	}
	_, readErr := io.Copy(io.Discard, obj.Body)
	closeErr := obj.Body.Close()
	if readErr != nil {
		_ = store.Delete(ctx, key)
		return readErr
	}
	if closeErr != nil {
		_ = store.Delete(ctx, key)
		return closeErr
	}
	return store.Delete(ctx, key)
}

type localObjectStore struct {
	cfg *Config
}

func (s localObjectStore) Put(_ context.Context, key string, r io.ReadSeeker, _ int64, _ string, _ string) error {
	parts, err := storageKeyParts(key)
	if err != nil {
		return err
	}
	fullPath, err := localStoragePath(s.cfg, parts...)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		return err
	}
	tmp := fullPath + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	removeTmp := true
	defer func() {
		out.Close()
		if removeTmp {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, fullPath); err != nil {
		return err
	}
	removeTmp = false
	return nil
}

func (s localObjectStore) Get(_ context.Context, key string) (*StoredObject, error) {
	parts, err := storageKeyParts(key)
	if err != nil {
		return nil, err
	}
	fullPath, err := localStoragePath(s.cfg, parts...)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &StoredObject{Body: f, Info: ObjectInfo{Size: stat.Size(), ModTime: stat.ModTime()}}, nil
}

func (s localObjectStore) Delete(_ context.Context, key string) error {
	parts, err := storageKeyParts(key)
	if err != nil {
		return err
	}
	fullPath, err := localStoragePath(s.cfg, parts...)
	if err != nil {
		return err
	}
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return removeDirAndEmptyParents(s.cfg, filepath.Dir(fullPath))
}

func (s localObjectStore) DeletePrefix(_ context.Context, prefix string) error {
	parts, err := storageKeyParts(prefix)
	if err != nil {
		return err
	}
	p, err := localStoragePath(s.cfg, parts...)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(p); err != nil {
		return err
	}
	return removeDirAndEmptyParents(s.cfg, filepath.Dir(p))
}

type s3ObjectStore struct {
	endpoint       string
	bucket         string
	region         string
	accessKey      string
	secretKey      string
	prefix         string
	useSSL         bool
	forcePathStyle bool
	client         *http.Client
}

func newS3ObjectStore(cfg *Config) (*s3ObjectStore, error) {
	endpoint, useSSL, err := normalizeS3Endpoint(cfg.S3Endpoint, cfg.S3UseSSL)
	if err != nil {
		return nil, err
	}
	if endpoint == "" || strings.TrimSpace(cfg.S3Bucket) == "" || strings.TrimSpace(cfg.S3AccessKey) == "" || cfg.S3SecretKey == "" {
		return nil, fmt.Errorf("%w: S3 endpoint, bucket, access key and secret key are required", ErrValidation)
	}
	region := strings.TrimSpace(cfg.S3Region)
	if region == "" {
		region = "us-east-1"
	}
	return &s3ObjectStore{
		endpoint:       endpoint,
		bucket:         strings.TrimSpace(cfg.S3Bucket),
		region:         region,
		accessKey:      strings.TrimSpace(cfg.S3AccessKey),
		secretKey:      cfg.S3SecretKey,
		prefix:         strings.Trim(strings.TrimSpace(cfg.S3Prefix), "/"),
		useSSL:         useSSL,
		forcePathStyle: cfg.S3ForcePathStyle,
		client: &http.Client{
			Timeout: 2 * smtpNetworkTimeout,
			Transport: &http.Transport{
				Proxy:       http.ProxyFromEnvironment,
				DialContext: publicTCPDialer(2 * smtpNetworkTimeout),
			},
		},
	}, nil
}

func (s *s3ObjectStore) Put(ctx context.Context, key string, r io.ReadSeeker, size int64, contentType string, payloadSHA256 string) error {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if payloadSHA256 == "" {
		return fmt.Errorf("%w: S3 upload requires payload checksum", ErrValidation)
	}
	headers := map[string]string{
		"Content-Type": contentType,
	}
	req, err := s.signedRequest(ctx, http.MethodPut, s.objectKey(key), nil, r, size, payloadSHA256, headers)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return s3StatusError(resp)
	}
	return nil
}

func (s *s3ObjectStore) Get(ctx context.Context, key string) (*StoredObject, error) {
	req, err := s.signedRequest(ctx, http.MethodGet, s.objectKey(key), nil, nil, 0, emptyPayloadSHA256, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, ErrFileNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, s3StatusError(resp)
	}
	info := ObjectInfo{
		Size:        resp.ContentLength,
		ContentType: resp.Header.Get("Content-Type"),
	}
	if lastModified := resp.Header.Get("Last-Modified"); lastModified != "" {
		if parsed, err := http.ParseTime(lastModified); err == nil {
			info.ModTime = parsed
		}
	}
	return &StoredObject{Body: resp.Body, Info: info}, nil
}

func (s *s3ObjectStore) Delete(ctx context.Context, key string) error {
	req, err := s.signedRequest(ctx, http.MethodDelete, s.objectKey(key), nil, nil, 0, emptyPayloadSHA256, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return s3StatusError(resp)
	}
	return nil
}

func (s *s3ObjectStore) DeletePrefix(ctx context.Context, prefix string) error {
	prefix = s.objectKey(strings.TrimSuffix(prefix, "/") + "/")
	continuation := ""
	for {
		keys, next, err := s.listKeys(ctx, prefix, continuation)
		if err != nil {
			return err
		}
		for _, key := range keys {
			if err := s.Delete(ctx, s.trimObjectPrefix(key)); err != nil {
				return err
			}
		}
		if next == "" {
			return nil
		}
		continuation = next
	}
}

func (s *s3ObjectStore) listKeys(ctx context.Context, prefix string, continuation string) ([]string, string, error) {
	query := url.Values{}
	query.Set("list-type", "2")
	query.Set("prefix", prefix)
	if continuation != "" {
		query.Set("continuation-token", continuation)
	}
	req, err := s.signedRequest(ctx, http.MethodGet, "", query, nil, 0, emptyPayloadSHA256, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", s3StatusError(resp)
	}
	var result struct {
		XMLName               xml.Name `xml:"ListBucketResult"`
		IsTruncated           bool     `xml:"IsTruncated"`
		NextContinuationToken string   `xml:"NextContinuationToken"`
		Contents              []struct {
			Key string `xml:"Key"`
		} `xml:"Contents"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", err
	}
	keys := make([]string, 0, len(result.Contents))
	for _, item := range result.Contents {
		keys = append(keys, item.Key)
	}
	if !result.IsTruncated {
		return keys, "", nil
	}
	return keys, result.NextContinuationToken, nil
}

func (s *s3ObjectStore) signedRequest(ctx context.Context, method string, key string, query url.Values, body io.Reader, size int64, payloadSHA256 string, extraHeaders map[string]string) (*http.Request, error) {
	if query == nil {
		query = url.Values{}
	}
	u, canonicalURI := s.objectURL(key)
	u.RawQuery = canonicalQuery(query)
	if body == nil {
		body = http.NoBody
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if size > 0 {
		req.ContentLength = size
	}
	req.Header.Set("Host", u.Host)
	amzDate := time.Now().UTC().Format("20060102T150405Z")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadSHA256)
	for name, value := range extraHeaders {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(name, value)
		}
	}
	canonicalHeaders, signedHeaders := canonicalHeaders(req.Header, u.Host)
	canonical := strings.Join([]string{
		method,
		canonicalURI,
		u.RawQuery,
		canonicalHeaders,
		signedHeaders,
		payloadSHA256,
	}, "\n")
	date := amzDate[:8]
	scope := date + "/" + s.region + "/s3/aws4_request"
	hash := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(hash[:])
	signature := hex.EncodeToString(hmacSHA256(signingKey(s.secretKey, date, s.region), []byte(toSign)))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.accessKey+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	return req, nil
}

func (s *s3ObjectStore) objectURL(key string) (*url.URL, string) {
	scheme := "http"
	if s.useSSL {
		scheme = "https"
	}
	escapedKey := escapeS3Path(key)
	if s.forcePathStyle {
		path := "/" + s.bucket
		if escapedKey != "" {
			path += "/" + escapedKey
		}
		return &url.URL{Scheme: scheme, Host: s.endpoint, Path: path}, path
	}
	path := "/"
	if escapedKey != "" {
		path += escapedKey
	}
	return &url.URL{Scheme: scheme, Host: s.bucket + "." + s.endpoint, Path: path}, path
}

func (s *s3ObjectStore) objectKey(key string) string {
	key = strings.Trim(strings.TrimSpace(filepath.ToSlash(key)), "/")
	if s.prefix == "" {
		return key
	}
	if key == "" {
		return s.prefix
	}
	return s.prefix + "/" + key
}

func (s *s3ObjectStore) trimObjectPrefix(key string) string {
	key = strings.Trim(strings.TrimSpace(filepath.ToSlash(key)), "/")
	if s.prefix == "" {
		return key
	}
	return strings.TrimPrefix(strings.TrimPrefix(key, s.prefix), "/")
}

func storageKeyParts(key string) ([]string, error) {
	key = strings.Trim(strings.TrimSpace(filepath.ToSlash(key)), "/")
	if key == "" {
		return nil, fmt.Errorf("%w: invalid storage key", ErrValidation)
	}
	parts := strings.Split(key, "/")
	for _, part := range parts {
		if !safeStorageSegment(part) {
			return nil, fmt.Errorf("%w: invalid storage key", ErrValidation)
		}
	}
	return parts, nil
}

func escapeS3Path(key string) string {
	if key == "" {
		return ""
	}
	parts := strings.Split(key, "/")
	for i, part := range parts {
		parts[i] = uriEncode(part)
	}
	return strings.Join(parts, "/")
}

func canonicalQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		vals := append([]string{}, values[key]...)
		sort.Strings(vals)
		for _, value := range vals {
			parts = append(parts, uriEncode(key)+"="+uriEncode(value))
		}
	}
	return strings.Join(parts, "&")
}

func canonicalHeaders(headers http.Header, host string) (string, string) {
	canon := map[string]string{"host": host}
	for key, values := range headers {
		lower := strings.ToLower(key)
		if lower == "authorization" {
			continue
		}
		trimmed := make([]string, 0, len(values))
		for _, value := range values {
			trimmed = append(trimmed, strings.Join(strings.Fields(value), " "))
		}
		canon[lower] = strings.Join(trimmed, ",")
	}
	keys := make([]string, 0, len(canon))
	for key := range canon {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte(':')
		b.WriteString(canon[key])
		b.WriteByte('\n')
	}
	return b.String(), strings.Join(keys, ";")
}

func uriEncode(s string) string {
	escaped := url.QueryEscape(s)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}

func signingKey(secret string, date string, region string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key []byte, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func s3StatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("s3 request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
}
