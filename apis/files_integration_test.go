package apis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dublyo/dublyobase/core"
)

type multipartTestFile struct {
	Name string
	Body []byte
}

func TestFileUploadProtectedDownloadThumbnailAndCleanup(t *testing.T) {
	app, _ := newIntegrationApp(t)
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)
	serviceKey := createAPIKeyForRecords(t, srv.Handler, adminToken, slug, "service")
	owner := signupAppUserForTest(t, srv.Handler, slug, "owner@example.com")
	other := signupAppUserForTest(t, srv.Handler, slug, "other@example.com")

	createCollectionBody := `{
		"name":"assets",
		"type":"base",
		"fields":[
			{"name":"title","type":"text","required":true},
			{"name":"owner","type":"relation","options":{"collection":"users"}},
			{"name":"avatar","type":"file"},
			{"name":"gallery","type":"file","options":{"multiple":true}}
		],
		"viewRule":"owner = @request.auth.id",
		"updateRule":"owner = @request.auth.id"
	}`
	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), adminToken, createCollectionBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create file collection: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	record := createRecordInCollectionForTest(t, srv.Handler, slug, "assets", serviceKey, fmt.Sprintf(`{"title":"Avatar","owner":%q}`, owner.User.ID))
	recordID := record["id"].(string)

	rec = patchJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/assets/records/%s", slug, recordID), owner.Token, `{"avatar":{"id":"bad"}}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("direct file JSON write: want 422, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postMultipartFiles(srv.Handler, fmt.Sprintf("/api/projects/%s/files/assets/%s/avatar", slug, recordID), owner.Token, []multipartTestFile{
		{Name: "avatar.png", Body: testPNG(t, 8, 6)},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload avatar: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	uploaded := decodeRecordMap(t, rec)
	avatar := singleFileMeta(t, uploaded, "avatar")
	if avatar.ID == "" || avatar.Name != "avatar.png" || avatar.Size == 0 || avatar.Mime != "image/png" {
		t.Fatalf("bad avatar metadata: %+v", avatar)
	}
	avatarPath := filepath.Join(app.Config.StorageLocalPath, filepath.FromSlash(avatar.Path))
	if _, err := os.Stat(avatarPath); err != nil {
		t.Fatalf("uploaded file missing on disk: %v", err)
	}

	downloadPath := fmt.Sprintf("/api/projects/%s/files/assets/%s/avatar/%s/avatar.png", slug, recordID, avatar.ID)
	tokenPath := fmt.Sprintf("/api/projects/%s/files/assets/%s/avatar/%s/token", slug, recordID, avatar.ID)
	rec = getJSON(srv.Handler, downloadPath, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("download without token: want 401, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = postJSON(srv.Handler, tokenPath, other.Token, `{}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner token: want 404, got %d: %s", rec.Code, rec.Body.String())
	}
	token := createFileTokenForTest(t, srv.Handler, tokenPath, owner.Token)

	rec = getJSON(srv.Handler, downloadPath+"?token="+token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("download with token: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != len(testPNG(t, 8, 6)) {
		t.Fatalf("download body size = %d, want original image bytes", rec.Body.Len())
	}

	rec = getJSON(srv.Handler, downloadPath+"?thumb=4x3&token="+token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("download thumb: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if cfg.Width != 4 || cfg.Height != 3 {
		t.Fatalf("thumbnail size = %dx%d, want 4x3", cfg.Width, cfg.Height)
	}

	rec = postMultipartFiles(srv.Handler, fmt.Sprintf("/api/projects/%s/files/assets/%s/avatar", slug, recordID), owner.Token, []multipartTestFile{
		{Name: "replacement.txt", Body: []byte("replacement")},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("replace avatar: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Dir(avatarPath)); !os.IsNotExist(err) {
		t.Fatalf("old avatar directory should be removed, stat err=%v", err)
	}

	rec = postMultipartFiles(srv.Handler, fmt.Sprintf("/api/projects/%s/files/assets/%s/gallery", slug, recordID), owner.Token, []multipartTestFile{
		{Name: "one.txt", Body: []byte("one")},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload gallery: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = postMultipartFiles(srv.Handler, fmt.Sprintf("/api/projects/%s/files/assets/%s/gallery?mode=append", slug, recordID), owner.Token, []multipartTestFile{
		{Name: "two.txt", Body: []byte("two")},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("append gallery: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	gallery := multiFileMeta(t, decodeRecordMap(t, rec), "gallery")
	if len(gallery) != 2 || gallery[0].Name != "one.txt" || gallery[1].Name != "two.txt" {
		t.Fatalf("bad gallery metadata: %+v", gallery)
	}

	rec = deleteJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections/assets/records/%s", slug, recordID), serviceKey, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete record: want 204, got %d: %s", rec.Code, rec.Body.String())
	}
	recordDir := filepath.Join(app.Config.StorageLocalPath, slug, "assets", recordID)
	if _, err := os.Stat(recordDir); !os.IsNotExist(err) {
		t.Fatalf("record storage directory should be removed, stat err=%v", err)
	}
}

func TestFileUploadLimit(t *testing.T) {
	app, _ := newIntegrationApp(t)
	app.Config.MaxUploadMB = 1
	srv := NewServer(app)
	adminToken := setupAdmin(t, srv.Handler, "admin@example.com")
	slug := createProjectForCollections(t, srv.Handler, adminToken)
	serviceKey := createAPIKeyForRecords(t, srv.Handler, adminToken, slug, "service")

	rec := postJSON(srv.Handler, fmt.Sprintf("/api/projects/%s/collections", slug), adminToken, `{
		"name":"assets",
		"type":"base",
		"fields":[
			{"name":"title","type":"text","required":true},
			{"name":"blob","type":"file"}
		]
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create collection: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	record := createRecordInCollectionForTest(t, srv.Handler, slug, "assets", serviceKey, `{"title":"Too big"}`)
	recordID := record["id"].(string)
	rec = postMultipartFiles(srv.Handler, fmt.Sprintf("/api/projects/%s/files/assets/%s/blob", slug, recordID), serviceKey, []multipartTestFile{
		{Name: "big.bin", Body: bytes.Repeat([]byte("x"), 1024*1024+1)},
	})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload: want 413, got %d: %s", rec.Code, rec.Body.String())
	}
	recordDir := filepath.Join(app.Config.StorageLocalPath, slug, "assets", recordID)
	if _, err := os.Stat(recordDir); !os.IsNotExist(err) {
		t.Fatalf("oversize upload should not leave record directory, stat err=%v", err)
	}
}

func postMultipartFiles(handler http.Handler, path string, token string, files []multipartTestFile) *httptest.ResponseRecorder {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range files {
		part, err := writer.CreateFormFile("file", file.Name)
		if err != nil {
			panic(err)
		}
		if _, err := part.Write(file.Body); err != nil {
			panic(err)
		}
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	req := httptest.NewRequest("POST", path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func createFileTokenForTest(t *testing.T, handler http.Handler, path string, token string) string {
	t.Helper()
	rec := postJSON(handler, path, token, `{}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create file token: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Token == "" {
		t.Fatalf("file token missing: %s", rec.Body.String())
	}
	return out.Token
}

func createRecordInCollectionForTest(t *testing.T, handler http.Handler, slug string, collection string, token string, body string) map[string]any {
	t.Helper()
	rec := postJSON(handler, fmt.Sprintf("/api/projects/%s/collections/%s/records", slug, collection), token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s record: want 201, got %d: %s", collection, rec.Code, rec.Body.String())
	}
	return decodeRecordMap(t, rec)
}

func decodeRecordMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func singleFileMeta(t *testing.T, record map[string]any, field string) core.FileMeta {
	t.Helper()
	b, err := json.Marshal(record[field])
	if err != nil {
		t.Fatal(err)
	}
	var meta core.FileMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatal(err)
	}
	return meta
}

func multiFileMeta(t *testing.T, record map[string]any, field string) []core.FileMeta {
	t.Helper()
	b, err := json.Marshal(record[field])
	if err != nil {
		t.Fatal(err)
	}
	var meta []core.FileMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatal(err)
	}
	return meta
}

func testPNG(t *testing.T, width int, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 30), B: 180, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
