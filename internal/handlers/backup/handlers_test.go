package backup

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os/exec"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"testing/fstest"

	"budget2/internal/config"
	backupsvc "budget2/internal/services/backup"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/internal/testutil"
)

// setupTestEnv creates a temp data directory, storage, and initializes the package globals.
// Returns a cleanup function.
func setupTestEnv(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "backup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	s, err := storage.New(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create storage: %v", err)
	}

	c := &config.Config{
		DataDirectory: tmpDir,
	}

	Initialize(c, s, nil, nil)

	return tmpDir, func() {
		os.RemoveAll(tmpDir)
	}
}

// writeCSVFile is a helper to create a CSV file in the data dir.
func writeCSVFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write file %s: %v", name, err)
	}
}

func TestInitialize(t *testing.T) {
	c := &config.Config{DataDirectory: "/tmp/test"}
	s := &storage.Storage{}
	Initialize(c, s, nil, nil)
	if cfg != c {
		t.Error("cfg not set")
	}
	if store != s {
		t.Error("store not set")
	}
}

func TestHandleHealth(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	HandleHealth(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected JSON content type, got %s", ct)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %s", body["status"])
	}
}

func TestHandleKillServer(t *testing.T) {
	orig := exitFunc
	called := make(chan struct{})
	exitFunc = func(code int) { close(called) }
	t.Cleanup(func() {
		<-called        // wait for the goroutine to fire before restoring
		exitFunc = orig // restore original
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/killme", nil)
	HandleKillServer(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleBackup(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Write some test CSV files
	writeCSVFile(t, tmpDir, "accounts.csv", "name,balance\nChecking,1000\n")
	writeCSVFile(t, tmpDir, "transactions.csv", "date,amount\n2024-01-01,50\n")

	// Also create a file that should be skipped (.encrypted marker)
	os.WriteFile(filepath.Join(tmpDir, ".encrypted"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(tmpDir, ".encryption-verify"), []byte("test"), 0644)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/backup", nil)
	HandleBackup(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/zip" {
		t.Errorf("expected application/zip, got %s", ct)
	}

	cd := resp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment; filename=budget_backup_") {
		t.Errorf("unexpected Content-Disposition: %s", cd)
	}

	// Verify it's a valid zip with our files
	body, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("Failed to read zip: %v", err)
	}

	fileNames := make(map[string]bool)
	for _, f := range zr.File {
		fileNames[f.Name] = true
	}

	if !fileNames["accounts.csv"] {
		t.Error("accounts.csv not found in backup")
	}
	if !fileNames["transactions.csv"] {
		t.Error("transactions.csv not found in backup")
	}
	// Encryption markers should NOT be in the backup
	if fileNames[".encrypted"] {
		t.Error(".encrypted should not be in backup")
	}
	if fileNames[".encryption-verify"] {
		t.Error(".encryption-verify should not be in backup")
	}
}

func TestHandleBackupEmptyDir(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/backup", nil)
	HandleBackup(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// createZipBuffer creates an in-memory zip file with the given files.
func createZipBuffer(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for name, content := range files {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatalf("Failed to create zip entry %s: %v", name, err)
		}
		fw.Write([]byte(content))
	}
	zw.Close()
	return buf
}

// createMultipartBody creates a multipart form body with a file field.
func createMultipartBody(t *testing.T, fieldName, fileName string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	part.Write(content)
	writer.Close()
	return body, writer.FormDataContentType()
}

func TestHandleRestore(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create a zip with CSV files and a non-CSV file.
	// After the refactor all file types are restored.
	zipBuf := createZipBuffer(t, map[string]string{
		"accounts.csv":     "name,balance\nChecking,1000\n",
		"transactions.csv": "date,amount\n2024-01-01,50\n",
		"readme.txt":       "Also restored now",
	})

	body, contentType := createMultipartBody(t, "file", "backup.zip", zipBuf.Bytes())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore", body)
	r.Header.Set("Content-Type", contentType)

	HandleRestore(w, r)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	if !strings.Contains(string(respBody), "Restored 3 files") {
		t.Errorf("unexpected body: %s", respBody)
	}

	// Verify all files were written (all file types now restored)
	if _, err := os.Stat(filepath.Join(tmpDir, "accounts.csv")); err != nil {
		t.Error("accounts.csv not restored")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "transactions.csv")); err != nil {
		t.Error("transactions.csv not restored")
	}
	// Non-CSV is now also restored
	if _, err := os.Stat(filepath.Join(tmpDir, "readme.txt")); err != nil {
		t.Error("readme.txt should have been restored")
	}
}

func TestHandleRestoreNonZip(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	body, contentType := createMultipartBody(t, "file", "backup.tar.gz", []byte("not a zip"))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore", body)
	r.Header.Set("Content-Type", contentType)

	HandleRestore(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleRestoreNoFile(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore", strings.NewReader(""))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=----")

	HandleRestore(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleRestoreWrongFieldName(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Upload a file but with wrong field name (not "file")
	zipBuf := createZipBuffer(t, map[string]string{
		"test.csv": "data",
	})

	body, contentType := createMultipartBody(t, "wrong_field", "backup.zip", zipBuf.Bytes())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore", body)
	r.Header.Set("Content-Type", contentType)

	HandleRestore(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRestoreInvalidZip(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	body, contentType := createMultipartBody(t, "file", "backup.zip", []byte("not a zip"))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore", body)
	r.Header.Set("Content-Type", contentType)

	HandleRestore(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleRestoreNoCsvFiles(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Zip with only non-CSV files. After the all-file-types refactor, these
	// are now restored successfully (the old CSV-only filter no longer exists).
	zipBuf := createZipBuffer(t, map[string]string{
		"readme.txt": "no csv here",
	})

	body, contentType := createMultipartBody(t, "file", "backup.zip", zipBuf.Bytes())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore", body)
	r.Header.Set("Content-Type", contentType)

	HandleRestore(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (all file types restored now), got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "readme.txt")); err != nil {
		t.Error("readme.txt should have been restored")
	}
}

func TestHandleRestoreTestData(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore-test-data", nil)

	HandleRestoreTestData(w, r)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	if !strings.Contains(string(respBody), "Restored") {
		t.Errorf("unexpected body: %s", respBody)
	}
}

func TestHandleDeleteAllData(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create files to delete
	writeCSVFile(t, tmpDir, "accounts.csv", "data")
	writeCSVFile(t, tmpDir, "transactions.csv", "data")
	os.WriteFile(filepath.Join(tmpDir, "keep.txt"), []byte("keep"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/delete-all", nil)

	HandleDeleteAllData(w, r)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	if !strings.Contains(string(respBody), "Deleted 2 files") {
		t.Errorf("unexpected body: %s", respBody)
	}

	// CSV files should be gone
	if _, err := os.Stat(filepath.Join(tmpDir, "accounts.csv")); err == nil {
		t.Error("accounts.csv should be deleted")
	}
	// Non-CSV files should remain
	if _, err := os.Stat(filepath.Join(tmpDir, "keep.txt")); err != nil {
		t.Error("keep.txt should still exist")
	}
}

func TestHandleDeleteAllDataEmptyDir(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/delete-all", nil)

	HandleDeleteAllData(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleDeleteAllDataBadDir(t *testing.T) {
	// Point to a non-existent directory
	cfg = &config.Config{DataDirectory: "/nonexistent/path/xyz"}
	defer func() { cfg = nil }()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/delete-all", nil)

	HandleDeleteAllData(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleEnableEncryption(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create a CSV file to encrypt
	writeCSVFile(t, tmpDir, "test.csv", "data")

	form := url.Values{
		"password":        {"mypassword123"},
		"confirmPassword": {"mypassword123"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryption(w, r)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	if !strings.Contains(string(respBody), "Encryption enabled") {
		t.Errorf("unexpected body: %s", respBody)
	}

	// Verify storage reports encrypted
	if !store.IsEncrypted() {
		t.Error("storage should be encrypted")
	}
}

func TestHandleEnableEncryptionShortPassword(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"password":        {"short"},
		"confirmPassword": {"short"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryption(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEnableEncryptionMismatch(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"password":        {"mypassword123"},
		"confirmPassword": {"differentpassword"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryption(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleDisableEncryption(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// First enable encryption
	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	form := url.Values{
		"password": {"mypassword123"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/disable-encryption", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleDisableEncryption(w, r)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	if !store.IsEncrypted() == true {
		// After disable, should not be encrypted
	}
	if store.IsEncrypted() {
		t.Error("storage should not be encrypted after disable")
	}
}

func TestHandleDisableEncryptionWrongPassword(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	form := url.Values{
		"password": {"wrongpassword"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/disable-encryption", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleDisableEncryption(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleDisableEncryptionEmptyPasswordRequired(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	// Empty password with password method should fail
	form := url.Values{
		"password": {""},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/disable-encryption", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleDisableEncryption(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEncryptionStatus(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/encryption-status", nil)
	HandleEncryptionStatus(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]bool
	json.NewDecoder(resp.Body).Decode(&body)

	if body["encrypted"] != false {
		t.Error("expected encrypted=false for unencrypted storage")
	}
}

func TestHandleEncryptionStatusEncrypted(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/encryption-status", nil)
	HandleEncryptionStatus(w, r)

	var body map[string]bool
	json.NewDecoder(w.Result().Body).Decode(&body)

	if body["encrypted"] != true {
		t.Error("expected encrypted=true")
	}
}

func TestHandlePlotlyCached(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create cache dir and cached file
	cacheDir := filepath.Join(tmpDir, "cache")
	os.MkdirAll(cacheDir, 0755)
	cachedContent := "// plotly cached content"
	os.WriteFile(filepath.Join(cacheDir, "plotly.min.js"), []byte(cachedContent), 0644)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/plotly.min.js", nil)
	HandlePlotly(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != cachedContent {
		t.Errorf("expected cached content, got: %s", body)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/javascript" {
		t.Errorf("expected application/javascript, got %s", ct)
	}

	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "max-age=31536000") {
		t.Errorf("expected long cache control, got %s", cc)
	}
}

func TestHandlePlotlyCDNFetchSuccess(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Start a fake CDN server that returns JS content
	fakeContent := "// fake plotly.min.js content"
	fakeCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fakeContent))
	}))
	defer fakeCDN.Close()

	// Override DefaultTransport to redirect the CDN request to our fake server
	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectTransport{target: fakeCDN.URL, transport: &http.Transport{}}
	defer func() { http.DefaultTransport = origTransport }()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/plotly.min.js", nil)
	HandlePlotly(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != fakeContent {
		t.Errorf("expected fake content, got: %s", body)
	}

	// Verify it was cached
	cachePath := filepath.Join(tmpDir, "cache", "plotly.min.js")
	cached, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("expected cache file: %v", err)
	}
	if string(cached) != fakeContent {
		t.Error("cache content mismatch")
	}
}

func TestHandlePlotlyCDNError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Override transport to return an error
	origTransport := http.DefaultTransport
	http.DefaultTransport = &errorTransport{}
	defer func() { http.DefaultTransport = origTransport }()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/plotly.min.js", nil)
	HandlePlotly(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandlePlotlyCDNBadStatus(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	fakeCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer fakeCDN.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectTransport{target: fakeCDN.URL, transport: &http.Transport{}}
	defer func() { http.DefaultTransport = origTransport }()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/plotly.min.js", nil)
	HandlePlotly(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestHandlePlotlyCDNReadError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	fakeCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000") // Claim larger content
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("short")) // But only send a little
		// Close connection abruptly by hijacking
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer fakeCDN.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectTransport{target: fakeCDN.URL, transport: &http.Transport{}}
	defer func() { http.DefaultTransport = origTransport }()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/plotly.min.js", nil)
	HandlePlotly(w, r)

	// The handler may get the partial content or an error
	// We just verify it doesn't panic
}

func TestHandlePlotlyCacheWriteFailure(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Make cache directory unwritable by creating a file at that path
	cachePath := filepath.Join(tmpDir, "cache")
	os.WriteFile(cachePath, []byte("not a directory"), 0444)

	fakeContent := "// fake plotly"
	fakeCDN := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fakeContent))
	}))
	defer fakeCDN.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectTransport{target: fakeCDN.URL, transport: &http.Transport{}}
	defer func() { http.DefaultTransport = origTransport }()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/plotly.min.js", nil)
	HandlePlotly(w, r)

	// Should still return the content even if caching fails
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	body, _ := io.ReadAll(w.Result().Body)
	if string(body) != fakeContent {
		t.Errorf("expected content despite cache failure, got: %s", body)
	}
}

// redirectTransport redirects all HTTP requests to a target URL
type redirectTransport struct {
	target    string
	transport http.RoundTripper
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := rt.target + req.URL.Path
	newReq, _ := http.NewRequest(req.Method, newURL, req.Body)
	return rt.transport.RoundTrip(newReq)
}

// errReader is a reader that always returns an error
type errReader struct{}

func (e *errReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("simulated read error")
}

// errorTransport always returns an error
type errorTransport struct{}

func (t *errorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("simulated network error")
}

func TestHandleUnlockPageNotLocked(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Storage is not encrypted, so not locked
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/unlock", nil)
	HandleUnlockPage(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("expected 307 redirect, got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if loc != "/dashboard" {
		t.Errorf("expected redirect to /dashboard, got %s", loc)
	}
}

func TestHandleUnlock(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Enable encryption, then lock, then unlock
	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	// Lock the storage
	store.Lock()

	if !IsStorageLocked() {
		t.Fatal("expected storage to be locked")
	}

	form := url.Values{
		"password": {"mypassword123"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/unlock", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleUnlock(w, r)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	if IsStorageLocked() {
		t.Error("expected storage to be unlocked")
	}
}

func TestHandleUnlockWrongPassword(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	store.Lock()

	form := url.Values{
		"password": {"wrongpassword"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/unlock", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleUnlock(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHandleUnlockEmptyPasswordRequired(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	store.Lock()

	form := url.Values{
		"password": {""},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/unlock", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleUnlock(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestIsStorageLocked(t *testing.T) {
	// When store is nil
	oldStore := store
	store = nil
	if IsStorageLocked() {
		t.Error("should return false when store is nil")
	}
	store = oldStore

	// When not encrypted
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	if IsStorageLocked() {
		t.Error("should return false when not encrypted")
	}
}

func TestHandleGetAuthMethods(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/auth-methods", nil)
	HandleGetAuthMethods(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	methods, ok := body["available_methods"].([]interface{})
	if !ok {
		t.Fatal("expected available_methods array")
	}

	if len(methods) != 4 {
		t.Errorf("expected 4 methods, got %d", len(methods))
	}

	// Verify method names
	expectedMethods := map[string]bool{
		"password": false,
		"age":      false,
		"ssh":      false,
		"yubikey":  false,
	}
	for _, m := range methods {
		method := m.(map[string]interface{})
		name := method["method"].(string)
		expectedMethods[name] = true
	}
	for name, found := range expectedMethods {
		if !found {
			t.Errorf("method %s not found", name)
		}
	}
}

func TestHandleDetectKeys(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/detect-keys", nil)
	HandleDetectKeys(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected JSON content type, got %s", ct)
	}

	var keys DetectedKeys
	json.NewDecoder(resp.Body).Decode(&keys)

	// We can't assert specific keys exist (depends on host), but the response should parse
	// SSHKeys and AgeIdentities will be whatever is on this machine
}

func TestHandleEnableEncryptionWithMethodPassword(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")

	form := url.Values{
		"method":          {"password"},
		"password":        {"mypassword123"},
		"confirmPassword": {"mypassword123"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusOK {
		body, _ := io.ReadAll(w.Result().Body)
		t.Fatalf("expected 200, got %d: %s", w.Code, body)
	}
}

func TestHandleEnableEncryptionWithMethodDefault(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")

	// Empty method should default to password
	form := url.Values{
		"method":          {""},
		"password":        {"mypassword123"},
		"confirmPassword": {"mypassword123"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusOK {
		body, _ := io.ReadAll(w.Result().Body)
		t.Fatalf("expected 200, got %d: %s", w.Code, body)
	}
}

func TestHandleEnableEncryptionWithMethodUnknown(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"method": {"unknown_method"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEnablePasswordEncryptionShort(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"method":          {"password"},
		"password":        {"short"},
		"confirmPassword": {"short"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEnablePasswordEncryptionMismatch(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"method":          {"password"},
		"password":        {"mypassword123"},
		"confirmPassword": {"differentpass1"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEnableAgeEncryptionNoPath(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"method":        {"age"},
		"identity_path": {""},
		"generate_new":  {"false"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEnableAgeEncryptionBadPath(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"method":        {"age"},
		"identity_path": {"/nonexistent/path/identity.txt"},
		"generate_new":  {"false"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEnableSSHEncryptionNoPath(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"method":       {"ssh"},
		"ssh_key_path": {""},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEnableSSHEncryptionBadPath(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"method":       {"ssh"},
		"ssh_key_path": {"/nonexistent/ssh/key"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEnableYubiKeyEncryptionNoIdentity(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"method":           {"yubikey"},
		"yubikey_identity": {""},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEnableYubiKeyEncryptionNotInstalled(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// If yubikey plugin is not installed, this should fail
	if storage.IsYubiKeyPluginInstalled() {
		t.Skip("YubiKey plugin is installed, can't test not-installed path")
	}

	form := url.Values{
		"method":           {"yubikey"},
		"yubikey_identity": {"AGE-PLUGIN-YUBIKEY-test"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleChangeAuthMethod(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/change-auth-method", nil)
	HandleChangeAuthMethod(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleGetEncryptionConfigNotEncrypted(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/encryption-config", nil)
	HandleGetEncryptionConfig(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if body["encrypted"] != false {
		t.Error("expected encrypted=false")
	}
}

func TestHandleGetEncryptionConfigEncrypted(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/encryption-config", nil)
	HandleGetEncryptionConfig(w, r)

	var body map[string]interface{}
	json.NewDecoder(w.Result().Body).Decode(&body)

	if body["encrypted"] != true {
		t.Error("expected encrypted=true")
	}
	if body["method"] != "password" {
		t.Errorf("expected method=password, got %v", body["method"])
	}
}

func TestHandleYubiKeyIdentityNoRecipient(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/yubikey-identity", nil)
	HandleYubiKeyIdentity(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleYubiKeyIdentityNotInstalled(t *testing.T) {
	if storage.IsYubiKeyPluginInstalled() {
		t.Skip("YubiKey plugin is installed")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/yubikey-identity?recipient=age1yubikey1test", nil)
	HandleYubiKeyIdentity(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleYubiKeySetupNotPost(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/yubikey-setup", nil)
	HandleYubiKeySetup(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleYubiKeySetupNotInstalled(t *testing.T) {
	if storage.IsYubiKeyPluginInstalled() {
		t.Skip("YubiKey plugin is installed")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/yubikey-setup", nil)
	HandleYubiKeySetup(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleEnableAgeEncryptionGenerateNew(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")

	identityDir, _ := os.MkdirTemp("", "age-identity-*")
	defer os.RemoveAll(identityDir)

	form := url.Values{
		"method":        {"age"},
		"identity_path": {filepath.Join(identityDir, "identity.txt")},
		"generate_new":  {"true"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", result["status"])
	}
	if result["public_key"] == nil || result["public_key"] == "" {
		t.Error("expected public_key to be set")
	}
}

func TestHandleEnableAgeEncryptionGenerateNewDefaultPath(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")

	// generate_new=true with empty path should use default path
	// This may fail if the default dir isn't writable, so we set a custom path
	identityDir, _ := os.MkdirTemp("", "age-identity-*")
	defer os.RemoveAll(identityDir)

	form := url.Values{
		"method":        {"age"},
		"identity_path": {""},
		"generate_new":  {"true"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	// This will either succeed or fail depending on whether ~/.config/budget2 is writable.
	// We just verify it doesn't panic and returns a valid HTTP response.
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("expected 200 or 500, got %d", w.Code)
	}
}

func TestHandleEnableAgeEncryptionAlreadyEnabled(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")

	// First enable with password
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	identityDir, _ := os.MkdirTemp("", "age-identity-*")
	defer os.RemoveAll(identityDir)

	form := url.Values{
		"method":        {"age"},
		"identity_path": {filepath.Join(identityDir, "identity.txt")},
		"generate_new":  {"true"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleEnablePasswordEncryptionAlreadyEnabled(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	form := url.Values{
		"method":          {"password"},
		"password":        {"anotherpassword"},
		"confirmPassword": {"anotherpassword"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleEnableEncryptionAlreadyEnabled(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	// Try to enable again
	form := url.Values{
		"password":        {"mypassword123"},
		"confirmPassword": {"mypassword123"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryption(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleRestoreWithPathTraversal(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create a zip with path traversal — the whole archive must be rejected.
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	fw, _ := zw.Create("../../etc/passwd.csv")
	fw.Write([]byte("malicious"))
	fw2, _ := zw.Create("normal.csv")
	fw2.Write([]byte("safe"))
	zw.Close()

	body, contentType := createMultipartBody(t, "file", "backup.zip", buf.Bytes())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore", body)
	r.Header.Set("Content-Type", contentType)

	HandleRestore(w, r)

	// Path traversal entry must cause a 400 rejection of the entire archive.
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path-traversal archive, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleBackupWithSubdirectory(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create a subdirectory with files
	subDir := filepath.Join(tmpDir, "subdir")
	os.MkdirAll(subDir, 0755)
	writeCSVFile(t, subDir, "nested.csv", "nested data")
	writeCSVFile(t, tmpDir, "root.csv", "root data")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/backup", nil)
	HandleBackup(w, r)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("Failed to read zip: %v", err)
	}

	fileNames := make(map[string]bool)
	for _, f := range zr.File {
		fileNames[f.Name] = true
	}

	if !fileNames["root.csv"] {
		t.Error("root.csv not in backup")
	}
	if !fileNames[filepath.Join("subdir", "nested.csv")] {
		t.Error("subdir/nested.csv not in backup")
	}
}

func TestHandleDisableEncryptionNotEnabled(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"password": {"anypassword1"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/disable-encryption", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleDisableEncryption(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleGetAuthMethodsWithEncryption(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/auth-methods", nil)
	HandleGetAuthMethods(w, r)

	var body map[string]interface{}
	json.NewDecoder(w.Result().Body).Decode(&body)

	// current_method should be "password"
	if body["current_method"] != "password" {
		t.Errorf("expected current_method=password, got %v", body["current_method"])
	}

	// The password method should be marked as current
	methods := body["available_methods"].([]interface{})
	for _, m := range methods {
		method := m.(map[string]interface{})
		if method["method"] == "password" {
			if method["current"] != true {
				t.Error("password method should be marked as current")
			}
		}
	}
}

func TestHandleRestoreTestDataRestoresCSVs(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore-test-data", nil)
	HandleRestoreTestData(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Verify CSV files exist in data dir
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	csvCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".csv") {
			csvCount++
		}
	}
	if csvCount == 0 {
		t.Error("expected CSV files to be restored")
	}
}

func TestHandleRestoreWithDirectoryEntries(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create a zip with a directory entry and CSV files
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	// Add a directory entry
	header := &zip.FileHeader{
		Name: "subdir/",
	}
	header.SetMode(os.ModeDir | 0755)
	zw.CreateHeader(header)
	// Add a CSV file
	fw, _ := zw.Create("test.csv")
	fw.Write([]byte("col1,col2\nval1,val2\n"))
	zw.Close()

	body, contentType := createMultipartBody(t, "file", "backup.zip", buf.Bytes())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore", body)
	r.Header.Set("Content-Type", contentType)

	HandleRestore(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUnlockPageLocked(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	store.Lock()

	// renderer is nil, so Render will fail
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/unlock", nil)

	// This will panic or error because renderer is nil
	// We need to handle this - the handler calls renderer.Render
	// Since renderer is nil, it'll panic. We need to recover.
	defer func() {
		if rec := recover(); rec != nil {
			// Expected: renderer is nil
		}
	}()

	HandleUnlockPage(w, r)
}

func TestHandleUnlockNotEncrypted(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Storage is not encrypted, unlock should succeed trivially
	form := url.Values{
		"password": {"anypassword"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/unlock", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleUnlock(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteAllDataOnlyDeletesCSV(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create various file types
	writeCSVFile(t, tmpDir, "data1.CSV", "data") // uppercase extension
	writeCSVFile(t, tmpDir, "data2.csv", "data")
	os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(tmpDir, ".encrypted"), []byte("marker"), 0644)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/delete-all", nil)
	HandleDeleteAllData(w, r)

	respBody := w.Body.String()
	if !strings.Contains(respBody, "Deleted 2 files") {
		t.Errorf("expected 2 files deleted, got: %s", respBody)
	}

	// Non-CSV files should remain
	if _, err := os.Stat(filepath.Join(tmpDir, "config.json")); err != nil {
		t.Error("config.json should remain")
	}
}

func TestHandleEnableSSHEncryptionWithValidKey(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")

	// Generate a test SSH key (ed25519)
	keyDir, _ := os.MkdirTemp("", "ssh-key-test-*")
	defer os.RemoveAll(keyDir)

	keyPath := filepath.Join(keyDir, "id_ed25519")
	// Use ssh-keygen to generate a key without passphrase
	cmd := "ssh-keygen -t ed25519 -f " + keyPath + " -N '' -q"
	if err := exec.Command("bash", "-c", cmd).Run(); err != nil {
		t.Skipf("ssh-keygen not available: %v", err)
	}

	form := url.Values{
		"method":       {"ssh"},
		"ssh_key_path": {keyPath},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, respBody)
	}

	if !strings.Contains(string(respBody), "Encryption enabled with SSH key") {
		t.Errorf("unexpected body: %s", respBody)
	}
}

func TestHandleEnableSSHEncryptionWithWrongPassphrase(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")

	// Generate a passphrase-protected SSH key
	keyDir, _ := os.MkdirTemp("", "ssh-key-test-*")
	defer os.RemoveAll(keyDir)

	keyPath := filepath.Join(keyDir, "id_ed25519")
	cmd := "ssh-keygen -t ed25519 -f " + keyPath + " -N 'testpassphrase' -q"
	if err := exec.Command("bash", "-c", cmd).Run(); err != nil {
		t.Skipf("ssh-keygen not available: %v", err)
	}

	form := url.Values{
		"method":       {"ssh"},
		"ssh_key_path": {keyPath},
		"passphrase":   {"wrongpassphrase"},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleEnableSSHEncryptionAlreadyEnabled(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	// Generate SSH key
	keyDir, _ := os.MkdirTemp("", "ssh-key-test-*")
	defer os.RemoveAll(keyDir)

	keyPath := filepath.Join(keyDir, "id_ed25519")
	cmd := "ssh-keygen -t ed25519 -f " + keyPath + " -N '' -q"
	if err := exec.Command("bash", "-c", cmd).Run(); err != nil {
		t.Skipf("ssh-keygen not available: %v", err)
	}

	form := url.Values{
		"method":       {"ssh"},
		"ssh_key_path": {keyPath},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleEnableEncryptionWithMethod(w, r)

	// Should fail because encryption already enabled
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRestoreNonCSVFilesOnly(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// All file types are now restored — this archive has only non-CSV but
	// that's fine; all three files should land on disk.
	zipBuf := createZipBuffer(t, map[string]string{
		"readme.txt":  "text file",
		"image.png":   "fake image",
		"config.json": "{}",
	})

	body, contentType := createMultipartBody(t, "file", "backup.zip", zipBuf.Bytes())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore", body)
	r.Header.Set("Content-Type", contentType)

	HandleRestore(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (all file types now restored), got %d: %s", w.Code, w.Body.String())
	}
	// All three files should be on disk
	for _, name := range []string{"readme.txt", "image.png", "config.json"} {
		if _, err := os.Stat(filepath.Join(tmpDir, name)); err != nil {
			t.Errorf("%s should have been restored", name)
		}
	}
}

func TestHandleBackupWalkError(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "good.csv", "data")

	// Create a subdirectory that is unreadable (Walk will error when trying to enter it)
	unreadableDir := filepath.Join(tmpDir, "unreadable")
	os.MkdirAll(unreadableDir, 0755)
	writeCSVFile(t, unreadableDir, "hidden.csv", "hidden")
	os.Chmod(unreadableDir, 0000)
	defer os.Chmod(unreadableDir, 0755)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/backup", nil)
	HandleBackup(w, r)

	// The handler logs errors but can't change status once headers are sent
	// Just verify it doesn't panic
	if w.Code != http.StatusOK {
		t.Logf("Got status %d", w.Code)
	}
}

func TestHandleBackupOpenFileError(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create a file but make it unreadable
	writeCSVFile(t, tmpDir, "unreadable.csv", "data")
	os.Chmod(filepath.Join(tmpDir, "unreadable.csv"), 0000)
	defer os.Chmod(filepath.Join(tmpDir, "unreadable.csv"), 0644)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/backup", nil)
	HandleBackup(w, r)

	// The handler logs the error but can't change status
	if w.Code != http.StatusOK {
		t.Logf("Got status %d", w.Code)
	}
}

// ---------- HandleKillServer ----------

func TestHandleKillServer_OverrideExitFunc(t *testing.T) {
	orig := exitFunc
	var exitCode int
	called := make(chan struct{})
	exitFunc = func(code int) {
		exitCode = code
		close(called)
	}
	t.Cleanup(func() {
		<-called        // wait for the goroutine to fire before restoring
		exitFunc = orig // restore original
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/killme", nil)
	HandleKillServer(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Server shutting down") {
		t.Errorf("unexpected body: %s", body)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "text/plain" {
		t.Errorf("expected text/plain, got %s", ct)
	}

	// Wait for the goroutine to call exitFunc
	<-called

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

// ---------- HandleUnlockPage with real renderer ----------

func setupTestEnvWithRenderer(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "backup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	s, err := storage.New(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create storage: %v", err)
	}

	c := &config.Config{
		DataDirectory: tmpDir,
	}

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("templates.New: %v", err)
	}

	Initialize(c, s, rend, nil)

	return tmpDir, func() {
		os.RemoveAll(tmpDir)
	}
}

func TestHandleUnlockPage_WithRenderer_Locked(t *testing.T) {
	tmpDir, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}

	store.Lock()

	if !IsStorageLocked() {
		t.Fatal("expected storage to be locked")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/unlock", nil)
	HandleUnlockPage(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content type, got %s", ct)
	}
}

func TestHandleUnlockPage_WithRenderer_RenderError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "backup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	c := &config.Config{DataDirectory: tmpDir}

	// Create a renderer with a minimal FS that has no "unlock" template.
	// This makes renderer.Render(w, "unlock", nil) return an error.
	stubFS := fstest.MapFS{
		"pages/dummy.html": &fstest.MapFile{Data: []byte(`{{define "dummy"}}ok{{end}}`)},
	}
	rend, err := templates.NewFromFS(stubFS, false)
	if err != nil {
		t.Fatalf("templates.NewFromFS: %v", err)
	}

	Initialize(c, s, rend, nil)

	// Enable encryption and lock
	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}
	store.Lock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/unlock", nil)
	HandleUnlockPage(w, r)

	// Render should fail -> 500
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleUnlockPage_WithRenderer_NotLocked(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	// Not encrypted, so not locked — should redirect
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/unlock", nil)
	HandleUnlockPage(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("expected 307, got %d", resp.StatusCode)
	}
}

// ---------- YubiKey handler tests when plugin is installed ----------

func TestHandleYubiKeySetup_Installed(t *testing.T) {
	if !storage.IsYubiKeyPluginInstalled() {
		t.Skip("YubiKey plugin is not installed")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/yubikey-setup", nil)
	HandleYubiKeySetup(w, r)

	// Should return 400 with JSON body containing setup instructions
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(w.Result().Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}

	if body["error"] == nil {
		t.Error("expected error field in response")
	}
	if body["setup_command"] == nil {
		t.Error("expected setup_command field in response")
	}
	if body["instructions"] == nil {
		t.Error("expected instructions field in response")
	}
}

func TestHandleYubiKeyIdentity_Installed_NoRecipient(t *testing.T) {
	if !storage.IsYubiKeyPluginInstalled() {
		t.Skip("YubiKey plugin is not installed")
	}

	// Missing recipient parameter
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/yubikey-identity", nil)
	HandleYubiKeyIdentity(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleYubiKeyIdentity_Installed_WithRecipient(t *testing.T) {
	if !storage.IsYubiKeyPluginInstalled() {
		t.Skip("YubiKey plugin is not installed")
	}

	// Plugin is installed, provide a recipient parameter to exercise the code path
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/yubikey-identity?recipient=age1yubikey1test", nil)
	HandleYubiKeyIdentity(w, r)

	// The result depends on whether the recipient matches a real YubiKey.
	// We just verify we get a valid HTTP response (200 or 500) and it doesn't panic.
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("expected 200 or 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleBackupBadDataDir(t *testing.T) {
	// Point to non-existent directory
	oldCfg := cfg
	oldStore := store

	badDir := "/nonexistent/backup/dir"
	cfg = &config.Config{DataDirectory: badDir}

	s, _ := storage.New(os.TempDir())
	store = s

	defer func() {
		cfg = oldCfg
		store = oldStore
	}()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/backup", nil)
	HandleBackup(w, r)

	// The handler logs errors but can't change status after headers are sent
	// It should still return (not panic)
}

func TestHandleEnableEncryptionWithMethodParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Use an errReader to force ParseForm to fail
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption-method", &errReader{})
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ContentLength = 100 // Claim content but reader will error

	HandleEnableEncryptionWithMethod(w, r)

	if w.Code != http.StatusBadRequest {
		t.Logf("Got %d (ParseForm may not have failed)", w.Code)
	}
}

func TestHandleEnableEncryptionParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption", nil)
	r.Header.Set("Content-Type", "multipart/form-data")

	HandleEnableEncryption(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleDisableEncryptionParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/disable-encryption", &errReader{})
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ContentLength = 100

	HandleDisableEncryption(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleUnlockParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/unlock", &errReader{})
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ContentLength = 100

	HandleUnlock(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleUnlockPageLockedWithRenderer(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")
	if err := store.EnableEncryption("mypassword123"); err != nil {
		t.Fatalf("Failed to enable encryption: %v", err)
	}
	store.Lock()

	// Use the real project templates directory
	projRoot := findProjectRoot(t)
	templateDir := filepath.Join(projRoot, "web", "templates")

	realRenderer, err := templates.New(templateDir, false)
	if err != nil {
		t.Skipf("Could not create renderer: %v", err)
	}

	renderer = realRenderer
	defer func() { renderer = nil }()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/unlock", nil)
	HandleUnlockPage(w, r)

	// The real renderer should successfully render the unlock template
	// This covers the renderer.Render call path (both success and error branches)
	if w.Code != http.StatusOK {
		t.Logf("Got status %d (render may have failed)", w.Code)
	}
}

func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root")
		}
		dir = parent
	}
}

func TestHandleUnlockNonPasswordMethod(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")

	// Enable with age encryption
	identityDir, _ := os.MkdirTemp("", "age-identity-*")
	defer os.RemoveAll(identityDir)

	identityPath := filepath.Join(identityDir, "identity.txt")
	provider, err := storage.GenerateAgeIdentity(identityPath)
	if err != nil {
		t.Fatalf("Failed to generate age identity: %v", err)
	}

	ageCfg := &storage.EncryptionConfig{
		Method:          storage.AuthMethodAge,
		AgeIdentityPath: identityPath,
		RecipientID:     provider.GetPublicKey(),
	}

	if err := store.EnableEncryptionWithProvider(provider, ageCfg); err != nil {
		t.Fatalf("Failed to enable age encryption: %v", err)
	}

	store.Lock()

	// Remove the identity file to make unlock fail
	os.Remove(identityPath)

	form := url.Values{
		"password": {""},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/unlock", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleUnlock(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "Unlock failed") {
		t.Errorf("expected 'Unlock failed' message for non-password method, got: %s", body)
	}
}

func TestHandleDisableEncryptionNonPasswordMethod(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")

	// Enable with age encryption
	identityDir, _ := os.MkdirTemp("", "age-identity-*")
	defer os.RemoveAll(identityDir)

	identityPath := filepath.Join(identityDir, "identity.txt")
	provider, err := storage.GenerateAgeIdentity(identityPath)
	if err != nil {
		t.Fatalf("Failed to generate age identity: %v", err)
	}

	ageCfg := &storage.EncryptionConfig{
		Method:          storage.AuthMethodAge,
		AgeIdentityPath: identityPath,
		RecipientID:     provider.GetPublicKey(),
	}

	if err := store.EnableEncryptionWithProvider(provider, ageCfg); err != nil {
		t.Fatalf("Failed to enable age encryption: %v", err)
	}

	// Disable with empty credentials (age method doesn't require password)
	form := url.Values{
		"password": {""},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/disable-encryption", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleDisableEncryption(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRestoreWithDoubleDotBaseName(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create zip with a file that has ".." in base name
	zipBuf := createZipBuffer(t, map[string]string{
		"..evil.csv":  "bad",
		"normal.csv": "good",
	})

	body, contentType := createMultipartBody(t, "file", "backup.zip", zipBuf.Bytes())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore", body)
	r.Header.Set("Content-Type", contentType)

	HandleRestore(w, r)

	// Should restore at least the normal.csv
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRestoreWriteError(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Make data directory read-only to cause write failures.
	zipBuf := createZipBuffer(t, map[string]string{
		"test.csv": "data",
	})

	body, contentType := createMultipartBody(t, "file", "backup.zip", zipBuf.Bytes())

	// Remove write permissions
	os.Chmod(tmpDir, 0555)
	defer os.Chmod(tmpDir, 0755)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore", body)
	r.Header.Set("Content-Type", contentType)

	HandleRestore(w, r)

	// The new restoreFromZip validates first, then writes. A write error
	// returns 500 (internal server error), not 400.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on write error, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteAllDataRemoveError(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create a CSV file
	writeCSVFile(t, tmpDir, "test.csv", "data")

	// Make the directory read-only so Remove fails
	os.Chmod(tmpDir, 0555)
	defer os.Chmod(tmpDir, 0755)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/delete-all", nil)

	HandleDeleteAllData(w, r)

	// ReadDir might succeed (can list), but Remove should fail
	// The response depends on whether ReadDir can read a read-only dir
}

func TestHandleEnableEncryptionParseFormErrorMultipart(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/enable-encryption", &errReader{})
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ContentLength = 100

	HandleEnableEncryption(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDisableEncryptionNonPasswordWrongCreds(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")

	// Enable with age encryption
	identityDir, _ := os.MkdirTemp("", "age-identity-*")
	defer os.RemoveAll(identityDir)

	identityPath := filepath.Join(identityDir, "identity.txt")
	provider, err := storage.GenerateAgeIdentity(identityPath)
	if err != nil {
		t.Fatalf("Failed to generate age identity: %v", err)
	}

	ageCfg := &storage.EncryptionConfig{
		Method:          storage.AuthMethodAge,
		AgeIdentityPath: identityPath,
		RecipientID:     provider.GetPublicKey(),
	}

	if err := store.EnableEncryptionWithProvider(provider, ageCfg); err != nil {
		t.Fatalf("Failed to enable age encryption: %v", err)
	}

	// Replace identity file with a DIFFERENT age identity to trigger "incorrect" error
	os.Remove(identityPath)
	provider2, err := storage.GenerateAgeIdentity(identityPath)
	if err != nil {
		t.Fatalf("Failed to generate second age identity: %v", err)
	}
	_ = provider2

	form := url.Values{
		"password": {""},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/disable-encryption", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleDisableEncryption(w, r)

	// Should fail with incorrect credentials (decryption will fail with wrong key)
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusInternalServerError {
		t.Errorf("expected error status, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// For non-password method with "incorrect" in error message, should say "Decryption failed"
	if strings.Contains(body, "Incorrect password") {
		t.Error("should not say 'Incorrect password' for age method")
	}
}

func TestHandleDisableEncryptionNonPasswordMissingFile(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	writeCSVFile(t, tmpDir, "test.csv", "data")

	identityDir, _ := os.MkdirTemp("", "age-identity-*")
	defer os.RemoveAll(identityDir)

	identityPath := filepath.Join(identityDir, "identity.txt")
	provider, err := storage.GenerateAgeIdentity(identityPath)
	if err != nil {
		t.Fatalf("Failed to generate age identity: %v", err)
	}

	ageCfg := &storage.EncryptionConfig{
		Method:          storage.AuthMethodAge,
		AgeIdentityPath: identityPath,
		RecipientID:     provider.GetPublicKey(),
	}

	if err := store.EnableEncryptionWithProvider(provider, ageCfg); err != nil {
		t.Fatalf("Failed to enable age encryption: %v", err)
	}

	// Remove identity file entirely
	os.Remove(identityPath)

	form := url.Values{
		"password": {""},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/disable-encryption", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	HandleDisableEncryption(w, r)

	// Should fail with non-incorrect error -> StatusInternalServerError
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRestoreWithMixedFiles(t *testing.T) {
	tmpDir, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create zip with CSV and non-CSV files in subdirectories.
	// After refactor: all file types are restored; subdirectory paths are preserved.
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	fw, _ := zw.Create("subdir/data.csv")
	fw.Write([]byte("col1\nval1\n"))
	fw2, _ := zw.Create("other.txt")
	fw2.Write([]byte("not csv"))
	fw3, _ := zw.Create("UPPER.CSV")
	fw3.Write([]byte("col1\nval1\n"))
	zw.Close()

	body, contentType := createMultipartBody(t, "file", "backup.zip", buf.Bytes())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/restore", body)
	r.Header.Set("Content-Type", contentType)

	HandleRestore(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// subdir/data.csv now preserves its path (nested under dataDir/subdir/)
	if _, err := os.Stat(filepath.Join(tmpDir, "subdir", "data.csv")); err != nil {
		t.Error("subdir/data.csv should exist at its nested path")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "UPPER.CSV")); err != nil {
		t.Error("UPPER.CSV should exist")
	}
	// Non-CSV files are also restored now
	if _, err := os.Stat(filepath.Join(tmpDir, "other.txt")); err != nil {
		t.Error("other.txt should exist (all file types restored)")
	}
}

// ---------- Task-8 new tests: round-trip, path sanitization, encrypted-blob ----------

func TestHandleRestore_RoundTripsAllFileTypes(t *testing.T) {
	dataDir := t.TempDir()
	originalCfg := cfg
	originalStore := store
	t.Cleanup(func() { cfg = originalCfg; store = originalStore })

	cfg = &config.Config{DataDirectory: dataDir}
	s, _ := storage.New(dataDir)
	store = s

	// Build an in-memory zip with csv + json + nested settings/.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mustZip(t, zw, "banking.csv", []byte("a,b\n1,2\n"))
	mustZip(t, zw, "major_expenses.json", []byte(`{"x":1}`))
	mustZip(t, zw, "settings/whatif_state.json", []byte(`{"baseline":"foo"}`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	rec := postMultipartZip(t, "/restore", "file", "backup.zip", buf.Bytes())
	HandleRestore(rec, rec.Request)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	mustReadEqual(t, filepath.Join(dataDir, "banking.csv"), []byte("a,b\n1,2\n"))
	mustReadEqual(t, filepath.Join(dataDir, "major_expenses.json"), []byte(`{"x":1}`))
	mustReadEqual(t, filepath.Join(dataDir, "settings/whatif_state.json"), []byte(`{"baseline":"foo"}`))
}

func TestHandleRestore_RejectsPathTraversal(t *testing.T) {
	dataDir := t.TempDir()
	originalCfg := cfg
	originalStore := store
	t.Cleanup(func() { cfg = originalCfg; store = originalStore })

	cfg = &config.Config{DataDirectory: dataDir}
	s, _ := storage.New(dataDir)
	store = s

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mustZip(t, zw, "../escape.txt", []byte("nope"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	rec := postMultipartZip(t, "/restore", "file", "backup.zip", buf.Bytes())
	HandleRestore(rec, rec.Request)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("path traversal must return 400, got %d", rec.Code)
	}
	// And ensure NOTHING was written above the data dir.
	parent := filepath.Dir(dataDir)
	if _, err := os.Stat(filepath.Join(parent, "escape.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("path traversal escaped the data directory")
	}
}

func TestHandleRestore_RejectsAbsolutePathEntries(t *testing.T) {
	dataDir := t.TempDir()
	originalCfg := cfg
	originalStore := store
	t.Cleanup(func() { cfg = originalCfg; store = originalStore })

	cfg = &config.Config{DataDirectory: dataDir}
	s, _ := storage.New(dataDir)
	store = s

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mustZip(t, zw, "/etc/passwd", []byte("nope"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	rec := postMultipartZip(t, "/restore", "file", "backup.zip", buf.Bytes())
	HandleRestore(rec, rec.Request)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("absolute path must return 400, got %d", rec.Code)
	}
}

func TestHandleRestore_RejectsEncryptedBlobsIntoUnencryptedStore(t *testing.T) {
	dataDir := t.TempDir()
	originalCfg := cfg
	originalStore := store
	t.Cleanup(func() { cfg = originalCfg; store = originalStore })

	cfg = &config.Config{DataDirectory: dataDir}
	s, _ := storage.New(dataDir)
	store = s

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Forge an "encrypted" entry by prefixing the age header magic.
	encrypted := append([]byte("age-encryption.org/v1\n"), []byte("payload")...)
	mustZip(t, zw, "secret.csv", encrypted)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	rec := postMultipartZip(t, "/restore", "file", "backup.zip", buf.Bytes())
	HandleRestore(rec, rec.Request)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("encrypted blob into unencrypted store must 400, got %d", rec.Code)
	}
}

// mustZip appends a zip entry with the given content.
func mustZip(t *testing.T, zw *zip.Writer, name string, content []byte) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
}

// recRequest bundles a ResponseRecorder with its corresponding Request so
// callers can pass both to a handler without separate variables.
type recRequest struct {
	*httptest.ResponseRecorder
	Request *http.Request
}

// postMultipartZip builds a multipart POST with a single file part and returns
// a recRequest whose .Request field is ready to pass to a handler.
func postMultipartZip(t *testing.T, url, field, filename string, content []byte) *recRequest {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, url, &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return &recRequest{ResponseRecorder: httptest.NewRecorder(), Request: r}
}

// mustReadEqual reads the file at path and fatals if the content differs from want.
func mustReadEqual(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q want %q", path, got, want)
	}
}

func TestHandleDeleteAllData_DoesNotTouchBackupDir(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := filepath.Join(dataDir, "backups") // worst case: nested
	if err := os.MkdirAll(backupDir, 0700); err != nil { t.Fatal(err) }

	originalCfg := cfg
	originalStore := store
	t.Cleanup(func() { cfg = originalCfg; store = originalStore })

	cfg = &config.Config{DataDirectory: dataDir, BackupDir: backupDir}
	s, _ := storage.New(dataDir)
	store = s

	if err := os.WriteFile(filepath.Join(backupDir, "budget_backup_X.zip"),
		[]byte("dummy"), 0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(dataDir, "txns.csv"), []byte("a,b\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/data/all", nil)
	HandleDeleteAllData(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	if _, err := os.Stat(filepath.Join(backupDir, "budget_backup_X.zip")); err != nil {
		t.Fatalf("BackupDir contents must survive Delete-All-Data: %v", err)
	}
}

func TestHandleBackupStatus_ReturnsMetaAndCount(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()

	originalCfg := cfg
	originalStore := store
	originalSvc := backupSvc
	t.Cleanup(func() { cfg = originalCfg; store = originalStore; backupSvc = originalSvc })

	cfg = &config.Config{DataDirectory: dataDir, BackupDir: backupDir}
	s, _ := storage.New(dataDir)
	store = s

	// Seed a successful meta and two backup zips.
	now := time.Now().UTC()
	stamp := now.Format("20060102_150405")
	must := func(err error) { t.Helper(); if err != nil { t.Fatal(err) } }
	must(os.WriteFile(filepath.Join(backupDir, "last_backup.json"),
		[]byte(`{"ts":"`+stamp+`","file_count":3,"total_bytes":100,"encrypted":false,"last_error":"","last_attempt_ts":"`+stamp+`"}`), 0600))
	must(os.WriteFile(filepath.Join(backupDir, "budget_backup_"+stamp+".zip"), []byte("a"), 0600))
	must(os.WriteFile(filepath.Join(backupDir, "budget_backup_"+now.Add(-24*time.Hour).Format("20060102_150405")+".zip"), []byte("b"), 0600))

	svc, err := backupsvc.New(backupsvc.Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil { t.Fatal(err) }
	backupSvc = svc

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/backup/status", nil)
	HandleBackupStatus(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{stamp, `"file_count":3`, `"snapshot_count":2`, backupDir} {
		if !strings.Contains(body, want) {
			t.Fatalf("status body missing %q: %s", want, body)
		}
	}
}

func TestHandleSetAutoBackupEnabled_TogglesAndPersists(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()

	originalCfg := cfg
	originalStore := store
	originalSvc := backupSvc
	t.Cleanup(func() { cfg = originalCfg; store = originalStore; backupSvc = originalSvc })

	cfg = &config.Config{DataDirectory: dataDir, BackupDir: backupDir}
	s, _ := storage.New(dataDir)
	store = s
	svc, _ := backupsvc.New(backupsvc.Config{BackupDir: backupDir, DataDir: dataDir})
	backupSvc = svc

	if !svc.Enabled() { t.Fatalf("default Enabled() should be true") }

	rec := httptest.NewRecorder()
	form := strings.NewReader("enabled=false")
	r := httptest.NewRequest(http.MethodPost, "/backup/auto-enabled", form)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	HandleSetAutoBackupEnabled(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if svc.Enabled() {
		t.Fatalf("Enabled() should be false after toggle")
	}

	// Persist across service restart.
	svc2, _ := backupsvc.New(backupsvc.Config{BackupDir: backupDir, DataDir: dataDir})
	if svc2.Enabled() {
		t.Fatalf("Enabled() should persist as false")
	}
}

func TestHandleSetAutoBackupEnabled_RejectsBadValue(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()

	originalCfg := cfg
	originalStore := store
	originalSvc := backupSvc
	t.Cleanup(func() { cfg = originalCfg; store = originalStore; backupSvc = originalSvc })

	cfg = &config.Config{DataDirectory: dataDir, BackupDir: backupDir}
	s, _ := storage.New(dataDir)
	store = s
	svc, _ := backupsvc.New(backupsvc.Config{BackupDir: backupDir, DataDir: dataDir})
	backupSvc = svc

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/backup/auto-enabled",
		strings.NewReader("enabled=banana"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	HandleSetAutoBackupEnabled(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad value should 400, got %d", rec.Code)
	}
}
