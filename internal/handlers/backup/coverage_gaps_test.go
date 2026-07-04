package backup

// coverage_gaps_test.go targets branches left uncovered by handlers_test.go:
// HandleOpenBackupDir (via PATH stubs), stream-write failures in the backup
// zips, restoreFromZip's malformed-archive rejections, pruneRestoreExtras
// walk-error accounting, the testdata embed fallback, auto-backup toggle
// errors, and the non-hardware YubiKey branches (via a stubbed
// age-plugin-yubikey on PATH).
//
// Documented ceilings (branches intentionally NOT covered, no production
// seam without code changes):
//   - handlers.go:132-135  HandleOpenBackupDir darwin/windows exec branches
//     (runtime.GOOS is compile-time on this box).
//   - handlers.go:220,226  HandleBackup: filepath.Rel cannot fail for two
//     absolute paths, and zw.Create only fails when flushing a previous
//     entry fails, but the walk aborts on the first copy error.
//   - handlers.go:330,336  same two branches in HandleBackupPlaintext.
//   - handlers.go:384,416  restoreFromZip: filepath.Abs fails only if
//     Getwd fails; the "escapes data dir" return is shadowed by the earlier
//     ".." rejection for any representable zip name.
//   - handlers.go:614  HandleRestore: io.ReadAll of a multipart part that
//     ParseMultipartForm already buffered in full cannot fail.
//   - handlers.go:541,570,579  pruneRestoreExtras: Abs error (Getwd),
//     Walk root error (the walk func swallows every error), and the
//     dirs-loop skip guard (skip-listed dirs are never appended).
//   - handlers.go:1112-1126  handleEnableYubiKeyEncryption success path
//     needs a real YubiKey (age plugin protocol) — hardware ceiling.

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"budget2/internal/config"
	backupsvc "budget2/internal/services/backup"
	"budget2/internal/services/storage"
	"budget2/testdata"
)

// saveGlobals snapshots the package globals and restores them on cleanup.
func saveGlobals(t *testing.T) {
	t.Helper()
	origCfg, origStore, origSvc := cfg, store, backupSvc
	t.Cleanup(func() { cfg = origCfg; store = origStore; backupSvc = origSvc })
}

// stubBinary drops an executable shell script named name into a fresh dir
// and points PATH at (only) that dir.
func stubBinary(t *testing.T, name, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return dir
}

// failingResponseWriter fails every body write, simulating a client that
// disconnected mid-download. Headers still work so handlers don't panic.
type failingResponseWriter struct {
	h      http.Header
	status int
}

func (f *failingResponseWriter) Header() http.Header {
	if f.h == nil {
		f.h = make(http.Header)
	}
	return f.h
}
func (f *failingResponseWriter) WriteHeader(code int)      { f.status = code }
func (f *failingResponseWriter) Write([]byte) (int, error) { return 0, errors.New("client went away") }

// urlencodedPost builds a POST request with an x-www-form-urlencoded body.
func urlencodedPost(target, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// ---------- resolvedBackupDir / HandleBackupStatus ----------

func TestResolvedBackupDir_NoServiceNoConfig(t *testing.T) {
	saveGlobals(t)
	backupSvc = nil
	cfg = nil
	if got := resolvedBackupDir(); got != "" {
		t.Fatalf("resolvedBackupDir() = %q, want empty", got)
	}
}

func TestHandleBackupStatus_FallsBackToConfigDir(t *testing.T) {
	saveGlobals(t)
	backupDir := t.TempDir()
	backupSvc = nil
	cfg = &config.Config{DataDirectory: t.TempDir(), BackupDir: backupDir}

	rec := httptest.NewRecorder()
	HandleBackupStatus(rec, httptest.NewRequest(http.MethodGet, "/backup/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var resp backupStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if resp.Dir != backupDir {
		t.Fatalf("Dir = %q, want config fallback %q", resp.Dir, backupDir)
	}
	if resp.Enabled {
		t.Fatalf("Enabled must be false with no backup service")
	}
}

// ---------- HandleOpenBackupDir ----------

func TestHandleOpenBackupDir_NotConfigured(t *testing.T) {
	saveGlobals(t)
	backupSvc = nil
	cfg = nil

	rec := httptest.NewRecorder()
	HandleOpenBackupDir(rec, httptest.NewRequest(http.MethodPost, "/backup/open", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not configured") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestHandleOpenBackupDir_StatError(t *testing.T) {
	saveGlobals(t)
	// BackupDir under a regular file -> Stat fails with ENOTDIR, which is
	// not IsNotExist, so the handler hits the stat-error branch.
	base := t.TempDir()
	blocker := filepath.Join(base, "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupSvc = nil
	cfg = &config.Config{BackupDir: filepath.Join(blocker, "sub")}

	rec := httptest.NewRecorder()
	HandleOpenBackupDir(rec, httptest.NewRequest(http.MethodPost, "/backup/open", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "stat backup dir") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestHandleOpenBackupDir_MkdirFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory write permissions do not block root")
	}
	saveGlobals(t)
	roParent := t.TempDir()
	if err := os.Chmod(roParent, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roParent, 0o755) })
	backupSvc = nil
	cfg = &config.Config{BackupDir: filepath.Join(roParent, "missing")}

	rec := httptest.NewRecorder()
	HandleOpenBackupDir(rec, httptest.NewRequest(http.MethodPost, "/backup/open", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "could not be created") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestHandleOpenBackupDir_LaunchFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH-based launcher stubbing is POSIX-specific")
	}
	saveGlobals(t)
	backupDir := t.TempDir()
	backupSvc = nil
	cfg = &config.Config{BackupDir: backupDir}
	t.Setenv("PATH", t.TempDir()) // no xdg-open/open anywhere

	rec := httptest.NewRecorder()
	HandleOpenBackupDir(rec, httptest.NewRequest(http.MethodPost, "/backup/open", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "could not launch file manager") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestHandleOpenBackupDir_SuccessExistingDirViaService(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("stubs the linux xdg-open branch")
	}
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	_ = dataDir
	stubBinary(t, "xdg-open", "#!/bin/sh\nexit 0\n")

	// The service's backup dir exists (created by the service); stat succeeds.
	dir := backupSvc.BackupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	HandleOpenBackupDir(rec, httptest.NewRequest(http.MethodPost, "/backup/open", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if resp["status"] != "ok" || resp["dir"] != dir {
		t.Fatalf("resp = %v, want status ok dir %q", resp, dir)
	}
}

func TestHandleOpenBackupDir_CreatesMissingDir(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("stubs the linux xdg-open branch")
	}
	saveGlobals(t)
	missing := filepath.Join(t.TempDir(), "not-yet")
	backupSvc = nil
	cfg = &config.Config{BackupDir: missing}
	stubBinary(t, "xdg-open", "#!/bin/sh\nexit 0\n")

	rec := httptest.NewRecorder()
	HandleOpenBackupDir(rec, httptest.NewRequest(http.MethodPost, "/backup/open", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if fi, err := os.Stat(missing); err != nil || !fi.IsDir() {
		t.Fatalf("handler must create the missing backup dir: fi=%v err=%v", fi, err)
	}
}

// ---------- HandleBackup: streaming write failure ----------

func TestHandleBackup_StreamWriteFailure(t *testing.T) {
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)

	// Incompressible payload larger than the zip writer's internal buffer so
	// the underlying ResponseWriter failure surfaces during io.Copy and again
	// at the deferred zw.Close.
	payload := make([]byte, 64*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "big.csv"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	w := &failingResponseWriter{}
	HandleBackup(w, httptest.NewRequest(http.MethodGet, "/backup", nil))

	// The handler can only log the failure (headers already sent); the
	// contract is that it returns without panicking and set zip headers.
	if got := w.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type = %q", got)
	}
}

// ---------- HandleBackupPlaintext ----------

func TestHandleBackupPlaintext_LockedStore(t *testing.T) {
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	writeCSVFile(t, dataDir, "x.csv", "data")
	if err := store.EnableEncryption("correct-password-1"); err != nil {
		t.Fatal(err)
	}
	// A fresh Storage over the same dir sees the encryption marker but has
	// no credentials: encrypted + locked.
	locked, err := storage.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store = locked

	w := httptest.NewRecorder()
	HandleBackupPlaintext(w, urlencodedPost("/backup/plaintext", url.Values{"password": {"correct-password-1"}}.Encode()))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "locked") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestHandleBackupPlaintext_ParseFormError(t *testing.T) {
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	writeCSVFile(t, dataDir, "x.csv", "data")
	if err := store.EnableEncryption("correct-password-1"); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	HandleBackupPlaintext(w, urlencodedPost("/backup/plaintext", "%ZZ"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid form data") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

// enableAgeEncryption switches the current test store to age-method
// encryption with a generated identity.
func enableAgeEncryption(t *testing.T) {
	t.Helper()
	identityPath := filepath.Join(t.TempDir(), "identity.txt")
	provider, err := storage.GenerateAgeIdentity(identityPath)
	if err != nil {
		t.Fatalf("GenerateAgeIdentity: %v", err)
	}
	ec := &storage.EncryptionConfig{
		Method:          storage.AuthMethodAge,
		AgeIdentityPath: identityPath,
		RecipientID:     provider.GetPublicKey(),
	}
	if err := store.EnableEncryptionWithProvider(provider, ec); err != nil {
		t.Fatalf("EnableEncryptionWithProvider: %v", err)
	}
}

func TestHandleBackupPlaintext_AgeMethodRequiresConfirm(t *testing.T) {
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	writeCSVFile(t, dataDir, "x.csv", "data")
	enableAgeEncryption(t)

	w := httptest.NewRecorder()
	HandleBackupPlaintext(w, urlencodedPost("/backup/plaintext", url.Values{"confirm": {"export"}}.Encode()))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "EXPORT") {
		t.Fatalf("body must tell the user to type EXPORT: %s", w.Body.String())
	}
}

func TestHandleBackupPlaintext_AgeMethodConfirmExports(t *testing.T) {
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	plaintext := "date,amount\n2024-02-02,7\n"
	writeCSVFile(t, dataDir, "t.csv", plaintext)
	// A skip-listed directory exercises the SkipDir branch of the walk.
	if err := os.MkdirAll(filepath.Join(dataDir, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "cache", "plotly.min.js"), []byte("js"), 0o644); err != nil {
		t.Fatal(err)
	}
	enableAgeEncryption(t)

	w := httptest.NewRecorder()
	HandleBackupPlaintext(w, urlencodedPost("/backup/plaintext", url.Values{"confirm": {"EXPORT"}}.Encode()))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["t.csv"] {
		t.Fatalf("export missing t.csv: %v", names)
	}
	if names["cache/plotly.min.js"] {
		t.Fatalf("export must skip cache/: %v", names)
	}
}

func TestHandleBackupPlaintext_OpenFileErrorAbortsWalk(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("file permissions do not block root")
	}
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	writeCSVFile(t, dataDir, "x.csv", "data")
	if err := store.EnableEncryption("correct-password-1"); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(dataDir, "x.csv")
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })

	w := httptest.NewRecorder()
	HandleBackupPlaintext(w, urlencodedPost("/backup/plaintext", url.Values{"password": {"correct-password-1"}}.Encode()))
	// Headers are already sent when the walk fails; the deferred zip Close
	// still flushes a central directory, but the unreadable entry must carry
	// no data (the walk aborted before any bytes were copied).
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err == nil {
		for _, f := range zr.File {
			if f.Name == "x.csv" && f.UncompressedSize64 != 0 {
				t.Fatalf("unreadable file must not contribute data to the export (size=%d)", f.UncompressedSize64)
			}
		}
	}
}

func TestHandleBackupPlaintext_WalkDirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not block root")
	}
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	writeCSVFile(t, dataDir, "a.csv", "data")
	if err := store.EnableEncryption("correct-password-1"); err != nil {
		t.Fatal(err)
	}
	// Executable-only directory: Walk can lstat it but not list it, so the
	// walk callback receives a non-nil walkErr.
	noList := filepath.Join(dataDir, "zz-nolist")
	if err := os.MkdirAll(noList, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noList, "hidden.csv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(noList, 0o311); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(noList, 0o755) })

	w := httptest.NewRecorder()
	HandleBackupPlaintext(w, urlencodedPost("/backup/plaintext", url.Values{"password": {"correct-password-1"}}.Encode()))
	// Walk aborts with the error; handler logs and returns after headers.
	if got := w.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestHandleBackupPlaintext_StreamWriteFailure(t *testing.T) {
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	// Must be >= 128KiB: for smaller payloads the flate writer only
	// surfaces the underlying write error at zw.Close, not during io.Copy.
	payload := make([]byte, 256*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "big.csv"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.EnableEncryption("correct-password-1"); err != nil {
		t.Fatal(err)
	}

	w := &failingResponseWriter{}
	r := urlencodedPost("/backup/plaintext", url.Values{"password": {"correct-password-1"}}.Encode())
	HandleBackupPlaintext(w, r)
	if got := w.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type = %q", got)
	}
}

// ---------- restoreFromZip edge cases ----------

func TestHandleRestore_SkipsDotEntry(t *testing.T) {
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mustZip(t, zw, ".", []byte("")) // cleans to "." -> ignored
	mustZip(t, zw, "real.csv", []byte("kept"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	rec := postMultipartZip(t, "/restore", "file", "backup.zip", buf.Bytes())
	HandleRestore(rec, rec.Request)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	mustReadEqual(t, filepath.Join(dataDir, "real.csv"), []byte("kept"))
	if !strings.Contains(rec.Body.String(), "Restored 1 files") {
		t.Fatalf("dot entry must not count as restored: %s", rec.Body.String())
	}
}

func TestHandleRestore_RejectsCorruptLocalHeader(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mustZip(t, zw, "a.csv", []byte("hello"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	raw[0] ^= 0xFF // corrupt the local file header signature ("PK\x03\x04")

	rec := postMultipartZip(t, "/restore", "file", "backup.zip", raw)
	HandleRestore(rec, rec.Request)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Cannot open entry") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestHandleRestore_RejectsCorruptEntryData(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)

	marker := []byte("UNIQUE-PAYLOAD-MARKER-0123456789")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Stored (uncompressed) so the payload bytes appear verbatim and a flip
	// produces a CRC mismatch at read time.
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: "a.csv", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(marker); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	idx := bytes.Index(raw, marker)
	if idx < 0 {
		t.Fatal("stored payload not found in archive bytes")
	}
	raw[idx+3] ^= 0xFF

	rec := postMultipartZip(t, "/restore", "file", "backup.zip", raw)
	HandleRestore(rec, rec.Request)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Cannot read entry") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestHandleRestore_OnlyProtectedEntriesIsRejected(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mustZip(t, zw, ".encrypted", []byte("marker")) // skip-listed
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	rec := postMultipartZip(t, "/restore", "file", "backup.zip", buf.Bytes())
	HandleRestore(rec, rec.Request)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "No restorable files") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestHandleRestore_MkdirFailureReturns500(t *testing.T) {
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	// A regular file where the archive needs a directory: MkdirAll fails
	// with ENOTDIR (works even as root).
	if err := os.WriteFile(filepath.Join(dataDir, "sub"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}

	zipBuf := createZipBuffer(t, map[string]string{"sub/x.csv": "data"})
	rec := postMultipartZip(t, "/restore", "file", "backup.zip", zipBuf.Bytes())
	HandleRestore(rec, rec.Request)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "mkdir") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestHandleRestore_WriteFileFailureReturns500(t *testing.T) {
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	// A directory where the archive has a file: WriteFile fails with EISDIR
	// (works even as root).
	if err := os.MkdirAll(filepath.Join(dataDir, "data.csv"), 0o755); err != nil {
		t.Fatal(err)
	}

	zipBuf := createZipBuffer(t, map[string]string{"data.csv": "data"})
	rec := postMultipartZip(t, "/restore", "file", "backup.zip", zipBuf.Bytes())
	HandleRestore(rec, rec.Request)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "write") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// ---------- pruneRestoreExtras walk-error accounting ----------

func TestPruneRestoreExtras_WalkAndRemoveFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission fixtures do not block root")
	}
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)

	dataAbs, err := filepath.Abs(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	skip := backupsvc.SkipPredicate(dataDir, resolvedBackupDir())

	keep := filepath.Join(dataAbs, "keep.csv")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dataAbs, "stale.csv")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	chmod := func(path string, mode os.FileMode) {
		t.Helper()
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
	}

	// noread (0311): lstat ok, listing fails -> walkErr with dir info ->
	// failures++ and SkipDir (never queued for removal).
	noread := filepath.Join(dataAbs, "noread")
	if err := os.MkdirAll(noread, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noread, "junk.csv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	chmod(noread, 0o311)

	// nolstat (0444): listing works but lstat of children fails -> walkErr
	// with nil info -> failures++, non-dir branch returns nil.
	nolstat := filepath.Join(dataAbs, "nolstat")
	if err := os.MkdirAll(nolstat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nolstat, "child.csv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	chmod(nolstat, 0o444)

	// lockedparent (0555) holding an empty dir: the empty dir cannot be
	// unlinked (parent read-only) but ReadDir shows it empty -> failures++.
	lockedParent := filepath.Join(dataAbs, "lockedparent")
	emptyChild := filepath.Join(lockedParent, "emptychild")
	if err := os.MkdirAll(emptyChild, 0o755); err != nil {
		t.Fatal(err)
	}
	chmod(lockedParent, 0o555)

	archive := map[string]struct{}{keep: {}}
	removed, failures := pruneRestoreExtras(dataAbs, archive, skip)

	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (stale.csv only)", removed)
	}
	// Three failures: the noread listing error (filepath.Walk reports a
	// dir whose readdir fails in a single callback, so it is skipped and
	// never queued for removal), the nolstat child lstat error, and the
	// emptychild unlink failure.
	if failures != 3 {
		t.Fatalf("failures = %d, want 3", failures)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("archive entry keep.csv must survive: %v", err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale.csv must be pruned, err=%v", err)
	}
}

// ---------- HandleRestoreTestData ----------

func TestHandleRestoreTestData_ServiceMissingReturns500(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	backupSvc = nil

	w := httptest.NewRecorder()
	HandleRestoreTestData(w, httptest.NewRequest(http.MethodPost, "/restore/test-data", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Backup service not initialized") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestHandleRestoreTestData_EmbeddedArchiveMissing(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)

	orig := testdata.TestBackupFS
	testdata.TestBackupFS = embed.FS{} // zero FS: ReadFile fails
	t.Cleanup(func() { testdata.TestBackupFS = orig })

	w := httptest.NewRecorder()
	HandleRestoreTestData(w, httptest.NewRequest(http.MethodPost, "/restore/test-data", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Test backup not available") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

// ---------- HandleSetAutoBackupEnabled ----------

func TestHandleSetAutoBackupEnabled_ServiceMissing(t *testing.T) {
	saveGlobals(t)
	backupSvc = nil

	w := httptest.NewRecorder()
	HandleSetAutoBackupEnabled(w, urlencodedPost("/backup/auto-enabled", "enabled=true"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", w.Code, w.Body.String())
	}
}

func TestHandleSetAutoBackupEnabled_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)

	w := httptest.NewRecorder()
	HandleSetAutoBackupEnabled(w, urlencodedPost("/backup/auto-enabled", "%ZZ"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "bad form") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestHandleSetAutoBackupEnabled_EnableTrue(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)

	// Off, then back on: exercises the true-branch of the toggle.
	if err := backupSvc.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	HandleSetAutoBackupEnabled(w, urlencodedPost("/backup/auto-enabled", "enabled=on"))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !backupSvc.Enabled() {
		t.Fatalf("Enabled() must be true after enabled=on")
	}
}

func TestHandleSetAutoBackupEnabled_PersistError(t *testing.T) {
	dataDir, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	// A regular file named "settings" blocks MkdirAll inside SetEnabled
	// (ENOTDIR, so this also fails for root).
	if err := os.WriteFile(filepath.Join(dataDir, "settings"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	HandleSetAutoBackupEnabled(w, urlencodedPost("/backup/auto-enabled", "enabled=false"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "persist enabled") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

// ---------- YubiKey non-hardware branches (PATH stubs) ----------

func TestHandleYubiKeyIdentity_PluginMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // guarantee age-plugin-yubikey is absent

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/yubikey/identity?recipient=age1yubikey1xyz", nil)
	HandleYubiKeyIdentity(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not installed") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestHandleYubiKeyIdentity_PluginFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-specific")
	}
	stubBinary(t, "age-plugin-yubikey", "#!/bin/sh\nexit 3\n")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/yubikey/identity?recipient=age1yubikey1xyz", nil)
	HandleYubiKeyIdentity(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", w.Code, w.Body.String())
	}
}

func TestHandleYubiKeyIdentity_ReturnsIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-specific")
	}
	stubBinary(t, "age-plugin-yubikey",
		"#!/bin/sh\necho '# recipient: age1yubikey1stub'\necho 'AGE-PLUGIN-YUBIKEY-STUBIDENTITY'\n")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/yubikey/identity?recipient=age1yubikey1stub", nil)
	HandleYubiKeyIdentity(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if !strings.Contains(resp["identity"], "AGE-PLUGIN-YUBIKEY-STUBIDENTITY") {
		t.Fatalf("identity = %q", resp["identity"])
	}
	if resp["recipient"] != "age1yubikey1stub" {
		t.Fatalf("recipient = %q", resp["recipient"])
	}
}

func TestHandleYubiKeySetup_PluginMissingDeterministic(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	w := httptest.NewRecorder()
	HandleYubiKeySetup(w, httptest.NewRequest(http.MethodPost, "/yubikey/setup", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not installed") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestHandleEnableYubiKey_PluginMissingDeterministic(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	t.Setenv("PATH", t.TempDir())

	form := url.Values{"method": {"yubikey"}, "yubikey_identity": {"AGE-PLUGIN-YUBIKEY-X"}}
	w := httptest.NewRecorder()
	HandleEnableEncryptionWithMethod(w, urlencodedPost("/enable-encryption-method", form.Encode()))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not installed") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestHandleEnableYubiKey_BadIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-specific")
	}
	_, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	stubBinary(t, "age-plugin-yubikey", "#!/bin/sh\nexit 0\n")

	form := url.Values{"method": {"yubikey"}, "yubikey_identity": {"not-a-valid-identity"}}
	w := httptest.NewRecorder()
	HandleEnableEncryptionWithMethod(w, urlencodedPost("/enable-encryption-method", form.Encode()))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Failed to load YubiKey") {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestHandleEnableYubiKey_BadIdentityWithRecipient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-specific")
	}
	_, cleanup := setupTestEnv(t)
	t.Cleanup(cleanup)
	stubBinary(t, "age-plugin-yubikey", "#!/bin/sh\nexit 0\n")

	form := url.Values{
		"method":            {"yubikey"},
		"yubikey_identity":  {"not-a-valid-identity"},
		"yubikey_recipient": {"age1yubikey1notreal"},
	}
	w := httptest.NewRecorder()
	HandleEnableEncryptionWithMethod(w, urlencodedPost("/enable-encryption-method", form.Encode()))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Failed to load YubiKey") {
		t.Fatalf("body: %s", w.Body.String())
	}
}
