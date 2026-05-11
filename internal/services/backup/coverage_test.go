package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"budget2/internal/services/storage"
)

// storageNew is a small helper that builds a real (unencrypted) Storage
// instance pointed at a temp dir, so we can wire it into Config.Store and
// exercise the `if s.cfg.Store != nil` branch in snapshotLocked.
func storageNew(t *testing.T) (*storage.Storage, error) {
	t.Helper()
	return storage.New(t.TempDir())
}

// ---- meta.go -------------------------------------------------------------

func TestLoadMeta_MalformedJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(metaPath(dir), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := loadMeta(dir)
	if err == nil {
		t.Fatal("expected error parsing malformed meta JSON")
	}
	if !strings.Contains(err.Error(), "parse meta") {
		t.Fatalf("expected parse-meta error, got %v", err)
	}
}

func TestLoadMeta_ReadFileErrorPropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix file permissions")
	}
	dir := t.TempDir()
	// Create the meta file with no read permission so ReadFile fails with a
	// non-NotExist error (covers the explicit error-return branch in loadMeta).
	mp := metaPath(dir)
	if err := os.WriteFile(mp, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(mp, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(mp, 0o600) })

	_, err := loadMeta(dir)
	if err == nil {
		t.Fatal("expected unreadable meta to error")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err should not be NotExist: %v", err)
	}
}

func TestWriteMetaAtomic_MkdirAllError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix file permissions")
	}
	parent := t.TempDir()
	// Make parent read-only so MkdirAll(subdir) fails.
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	subdir := filepath.Join(parent, "nested")
	err := writeMetaAtomic(subdir, Meta{TS: "20260101_000000"})
	if err == nil {
		t.Fatal("expected MkdirAll failure")
	}
}

func TestWriteMetaAtomic_WriteFileError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix file permissions")
	}
	dir := t.TempDir()
	// Make the directory itself read-only AFTER it exists, so MkdirAll on the
	// same path succeeds (no-op) but WriteFile to the .tmp inside fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := writeMetaAtomic(dir, Meta{TS: "20260101_000000"})
	if err == nil {
		t.Fatal("expected WriteFile failure on read-only dir")
	}
}

func TestWriteMetaFailure_RecoversFromUnreadablePrior(t *testing.T) {
	dir := t.TempDir()
	// Plant a malformed meta so loadMeta inside writeMetaFailure errors and
	// the fallback "prior = Meta{}" branch executes.
	if err := os.WriteFile(metaPath(dir), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)
	if err := writeMetaFailure(dir, "boom", now); err != nil {
		t.Fatal(err)
	}
	got, err := loadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastError != "boom" {
		t.Fatalf("LastError got %q want %q", got.LastError, "boom")
	}
	if got.LastAttemptTS != now.UTC().Format("20060102_150405") {
		t.Fatalf("LastAttemptTS got %q", got.LastAttemptTS)
	}
	if got.TS != "" || got.FileCount != 0 {
		t.Fatalf("prior should have been zero-valued, got %+v", got)
	}
}

// ---- retention.go --------------------------------------------------------

func TestListBackupTimes_SkipsUnparseableTimestamp(t *testing.T) {
	dir := t.TempDir()
	// File matches glob but has a non-parseable timestamp; must be skipped.
	bad := filepath.Join(dir, backupNamePrefix+"NOT_A_DATE"+backupNameSuffix)
	if err := os.WriteFile(bad, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	good := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	makeBackup(t, dir, good)

	out, err := listBackupTimes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 valid backup, got %d (%+v)", len(out), out)
	}
}

func TestApplyRetention_RemoveErrorPropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix file permissions")
	}
	dir := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// Make a backup well outside the retention window so it would be removed.
	old := now.AddDate(0, -10, 0) // older than monthly window
	makeBackup(t, dir, old)
	// Also a fresh one so the retention loop iterates over both.
	makeBackup(t, dir, now)

	// Lock down the directory so os.Remove fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := applyRetention(dir, now)
	if err == nil {
		t.Fatal("expected retention to fail when removal is denied")
	}
	if !strings.Contains(err.Error(), "retention: remove") {
		t.Fatalf("expected retention remove error, got %v", err)
	}
}

// ---- service.go ----------------------------------------------------------

func TestNew_RequiresBackupDir(t *testing.T) {
	_, err := New(Config{BackupDir: "", DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error when BackupDir is empty")
	}
	if !strings.Contains(err.Error(), "BackupDir required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_RequiresDataDir(t *testing.T) {
	_, err := New(Config{BackupDir: t.TempDir(), DataDir: ""})
	if err == nil {
		t.Fatal("expected error when DataDir is empty")
	}
	if !strings.Contains(err.Error(), "DataDir required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNew_LoadEnabledErrorPropagates(t *testing.T) {
	backupDir := t.TempDir()
	dataDir := t.TempDir()
	// Write malformed JSON into the enabled-flag path so loadEnabled
	// (called by New) returns a parse error.
	settings := filepath.Join(dataDir, "settings")
	if err := os.MkdirAll(settings, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings, "auto_backup.json"),
		[]byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err == nil {
		t.Fatal("expected New to surface loadEnabled error")
	}
	if !strings.Contains(err.Error(), "load enabled flag") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestService_DataDirReturnsConfigured(t *testing.T) {
	dataDir := t.TempDir()
	svc, err := New(Config{BackupDir: t.TempDir(), DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.DataDir(); got != dataDir {
		t.Fatalf("DataDir=%q want %q", got, dataDir)
	}
}

func TestLoadEnabled_MalformedJSONErrors(t *testing.T) {
	// Independent of New: build a Service manually so we can call loadEnabled
	// directly and confirm the unmarshal-error branch.
	backupDir := t.TempDir()
	dataDir := t.TempDir()
	settings := filepath.Join(dataDir, "settings")
	if err := os.MkdirAll(settings, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings, "auto_backup.json"),
		[]byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Service{cfg: Config{BackupDir: backupDir, DataDir: dataDir}, clock: realClock{}}
	if err := s.loadEnabled(); err == nil {
		t.Fatal("expected loadEnabled to surface JSON error")
	}
}

func TestLoadEnabled_HonorsFalsePersistedValue(t *testing.T) {
	backupDir := t.TempDir()
	dataDir := t.TempDir()
	settings := filepath.Join(dataDir, "settings")
	if err := os.MkdirAll(settings, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings, "auto_backup.json"),
		[]byte(`{"enabled":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Enabled() {
		t.Fatal("Enabled() should reflect persisted false")
	}
}

func TestSaveEnabled_MkdirAllError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix file permissions")
	}
	dataDir := t.TempDir()
	// Strip write on dataDir so MkdirAll(dataDir/settings) fails.
	if err := os.Chmod(dataDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0o700) })

	s := &Service{cfg: Config{BackupDir: t.TempDir(), DataDir: dataDir}, clock: realClock{}, enabled: true}
	if err := s.saveEnabled(true); err == nil {
		t.Fatal("expected MkdirAll failure inside saveEnabled")
	}
}

func TestSaveEnabled_WriteFileError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix file permissions")
	}
	dataDir := t.TempDir()
	settings := filepath.Join(dataDir, "settings")
	if err := os.MkdirAll(settings, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make settings dir read-only so the .tmp WriteFile fails while
	// MkdirAll(existing) succeeds.
	if err := os.Chmod(settings, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(settings, 0o700) })

	s := &Service{cfg: Config{BackupDir: t.TempDir(), DataDir: dataDir}, clock: realClock{}, enabled: true}
	if err := s.saveEnabled(true); err == nil {
		t.Fatal("expected WriteFile failure inside saveEnabled")
	}
}

// ---- scheduler.go -------------------------------------------------------

func TestRun_RespectsCtxCancellation(t *testing.T) {
	// Cover the exported Run entrypoint (currently 0%). We can't wait for an
	// hour tick — we just verify the goroutine returns when ctx is cancelled.
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Run(ctx, time.Hour)
		close(done)
	}()
	// Cancel after a brief grace period so the initial SnapshotIfStale has a
	// chance to start (irrelevant for coverage of Run itself).
	time.AfterFunc(50*time.Millisecond, cancel)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestRunWith_InitialSnapshotErrorIsLogged(t *testing.T) {
	// Force the initial SnapshotIfStale to fail by pointing DataDir at a
	// nonexistent path — filepath.Walk inside buildZip will surface the
	// error, which runWith logs and continues. We then cancel the ctx to
	// confirm the goroutine still exits cleanly.
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	backupDir := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), "does-not-exist")
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.runWith(ctx, make(chan time.Time), time.Hour)
		close(done)
	}()
	// Give the initial snapshot a moment to fail-and-log.
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runWith did not exit after cancel following initial error")
	}
}

func TestRunWith_TickSnapshotErrorIsLogged(t *testing.T) {
	// Drive the "tick" branch where SnapshotIfStale returns an error.
	dataDir := filepath.Join(t.TempDir(), "missing")
	backupDir := t.TempDir()
	clk := newFakeClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}

	ticks := make(chan time.Time, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() {
		svc.runWith(ctx, ticks, time.Hour)
		close(done)
	}()
	// First, let the initial-run attempt fail/finish.
	time.Sleep(50 * time.Millisecond)
	// Fire a tick. SnapshotIfStale will again fail because DataDir is missing.
	ticks <- clk.Now()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runWith did not exit after tick-error path")
	}
}

// ---- snapshot.go --------------------------------------------------------

func TestSnapshotIfStale_UnreadableMetaTriggersSnapshot(t *testing.T) {
	// loadMeta returns an error → SnapshotIfStale takes the "treat as stale"
	// branch and calls Snapshot. We seed a malformed last_backup.json.
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	if err := os.WriteFile(metaPath(backupDir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SnapshotIfStale(context.Background(), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(zips) != 1 {
		t.Fatalf("expected 1 zip (unreadable meta should snapshot), got %d", len(zips))
	}
}

func TestSnapshotIfStale_UnparseableTSTriggersSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	// Valid JSON but TS is non-parseable.
	bad, _ := json.Marshal(Meta{TS: "garbage-ts", FileCount: 1})
	if err := os.WriteFile(metaPath(backupDir), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SnapshotIfStale(context.Background(), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(zips) != 1 {
		t.Fatalf("expected 1 zip (unparseable TS should snapshot), got %d", len(zips))
	}
}

func TestSnapshotLocked_MkdirBackupDirFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix file permissions")
	}
	parent := t.TempDir()
	// parent is read-only so MkdirAll(parent/nested) inside snapshotLocked fails.
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	backupDir := filepath.Join(parent, "nested-backup")
	dataDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc := &Service{cfg: Config{BackupDir: backupDir, DataDir: dataDir}, clock: realClock{}, enabled: true}
	err := svc.Snapshot(context.Background())
	if err == nil {
		t.Fatal("expected mkdir failure to bubble up")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}

func TestSnapshotLocked_BuildZipFailsRecordsFailureMeta(t *testing.T) {
	// Force buildZip to fail by making DataDir nonexistent. snapshotLocked
	// should remove tmp, write failure meta, and return wrapped error.
	backupDir := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), "absent")
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Snapshot(context.Background()); err == nil {
		t.Fatal("expected build-zip failure")
	}
	// Failure meta should record LastError.
	m, err := loadMeta(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if m.LastError == "" {
		t.Fatal("expected LastError to be populated on build failure")
	}
	if !strings.Contains(m.LastError, "build zip") {
		t.Fatalf("LastError got %q", m.LastError)
	}
	// No final zip should be present.
	zips, _ := filepath.Glob(filepath.Join(backupDir, backupNamePrefix+"*"+backupNameSuffix))
	if len(zips) != 0 {
		t.Fatalf("expected no zip after failure, got %d", len(zips))
	}
}

func TestSnapshotLocked_VerifyZipFails(t *testing.T) {
	// We can't easily produce a build-success / verify-failure pair, but we
	// can independently exercise verifyZip's error paths in their own tests
	// below. Skip placeholder.
	t.Skip("verifyZip error paths covered by TestVerifyZip_* tests below")
}

func TestSnapshot_BuildZipContextCancelled(t *testing.T) {
	// Pre-cancel the context. buildZip's filepath.Walk hits the ctx.Done()
	// check on its first file visit and returns context.Canceled.
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	// Enough files that Walk will iterate at least once.
	seedDataDir(t, dataDir, map[string][]byte{
		"a.csv":     []byte("x"),
		"b.csv":     []byte("y"),
		"sub/c.csv": []byte("z"),
	})
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = svc.Snapshot(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context cancellation in error, got %v", err)
	}
}

func TestCleanOrphanTmp_KeepsRecentTmp(t *testing.T) {
	// Cover the "info.ModTime is recent → leave alone" path: place a .tmp
	// dated in the future-ish window (now-ish) so it survives, and an old
	// one that gets removed.
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	recent := filepath.Join(backupDir, "budget_backup_RECENT.zip.tmp")
	old := filepath.Join(backupDir, "budget_backup_OLD.zip.tmp")
	if err := os.WriteFile(recent, []byte("r"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("o"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(recent, now, now); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(old, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("recent .tmp should still exist: %v", err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old .tmp should be removed, stat err=%v", err)
	}
}

func TestCleanOrphanTmp_StatErrorIgnored(t *testing.T) {
	// Glob returns a path that disappears before Stat — covers the
	// `if err != nil { continue }` branch. We simulate by manipulating a
	// Service directly and creating a broken symlink so Stat fails.
	if runtime.GOOS == "windows" {
		t.Skip("symlinks differ on windows")
	}
	backupDir := t.TempDir()
	broken := filepath.Join(backupDir, "broken.zip.tmp")
	if err := os.Symlink(filepath.Join(backupDir, "missing-target"), broken); err != nil {
		t.Fatal(err)
	}
	s := &Service{cfg: Config{BackupDir: backupDir, DataDir: t.TempDir()}, clock: realClock{}}
	// Should not panic / error.
	s.cleanOrphanTmp()
}

func TestBuildZip_WalkErrorPropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix file permissions")
	}
	dataDir := t.TempDir()
	// Subdir we cannot read → filepath.Walk surfaces a walkErr.
	sub := filepath.Join(dataDir, "secret")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f.csv"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o700) })

	backupDir := t.TempDir()
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.Snapshot(context.Background())
	if err == nil {
		t.Fatal("expected walk error to propagate")
	}
}

// ---- verifyZip ----------------------------------------------------------

func TestLoadEnabled_NonNotExistReadErrorPropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix file permissions")
	}
	dataDir := t.TempDir()
	settings := filepath.Join(dataDir, "settings")
	if err := os.MkdirAll(settings, 0o755); err != nil {
		t.Fatal(err)
	}
	enabledFile := filepath.Join(settings, "auto_backup.json")
	if err := os.WriteFile(enabledFile, []byte(`{"enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Strip read permission so ReadFile returns a non-NotExist error.
	if err := os.Chmod(enabledFile, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(enabledFile, 0o600) })

	s := &Service{cfg: Config{BackupDir: t.TempDir(), DataDir: dataDir}, clock: realClock{}, enabled: true}
	err := s.loadEnabled()
	if err == nil {
		t.Fatal("expected loadEnabled to surface read error")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err should not be NotExist: %v", err)
	}
}

func TestBuildZip_OpenFileFailsWhenTmpPathExists(t *testing.T) {
	// buildZip uses O_CREATE|O_EXCL; if the tmp path already exists, OpenFile
	// fails. We can't easily trigger this through Snapshot (it generates a
	// fresh ts each call) so invoke buildZip directly via a helper Service.
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc := &Service{cfg: Config{BackupDir: backupDir, DataDir: dataDir}, clock: realClock{}, enabled: true}

	tmpPath := filepath.Join(backupDir, "preexisting.zip.tmp")
	if err := os.WriteFile(tmpPath, []byte("collision"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := svc.buildZip(context.Background(), tmpPath)
	if err == nil {
		t.Fatal("expected buildZip to fail when tmpPath already exists")
	}
}

func TestSnapshotLocked_RenameFailsRecordsFailureMeta(t *testing.T) {
	// Force the os.Rename(tmpPath, finalPath) call inside snapshotLocked to
	// fail by pre-creating a non-empty directory at the finalPath. On Linux,
	// rename(file → non-empty-dir) fails with ENOTEMPTY/EISDIR.
	if runtime.GOOS == "windows" {
		t.Skip("rename semantics differ on windows")
	}
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})

	clk := newFakeClock(time.Date(2026, 5, 1, 12, 34, 56, 0, time.UTC))
	ts := clk.Now().UTC().Format("20060102_150405")
	finalPath := filepath.Join(backupDir, backupNamePrefix+ts+backupNameSuffix)
	if err := os.MkdirAll(finalPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Non-empty so rename can't replace it.
	if err := os.WriteFile(filepath.Join(finalPath, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.Snapshot(context.Background())
	if err == nil {
		t.Fatal("expected rename failure to bubble up")
	}
	if !strings.Contains(err.Error(), "rename") {
		t.Fatalf("expected rename error, got %v", err)
	}
	m, mErr := loadMeta(backupDir)
	if mErr != nil {
		t.Fatal(mErr)
	}
	if !strings.Contains(m.LastError, "rename") {
		t.Fatalf("expected rename failure in meta, got %q", m.LastError)
	}
}

func TestSnapshotLocked_RetentionFailureRecordsMetaButReturnsNil(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix file permissions")
	}
	// Layout:
	//   parent/        (writable)
	//     backup/      (writable until just before retention runs — we make
	//                  retention fail by planting an unremovable old backup
	//                  using directory-level permissions).
	// Strategy: pre-seed the backup dir with a very old zip, then make the
	// backup dir read-only AFTER the snapshot writes its new file. Difficult
	// to time precisely — instead we use a subdirectory of backupDir as the
	// "old" backup, since os.Remove on a non-empty subdir fails. But
	// retention only Globs files matching budget_backup_*.zip, so a directory
	// with that name would match.
	backupDir := t.TempDir()
	dataDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})

	// Plant a directory whose name matches the retention glob AND falls
	// outside all retention tiers, so retention tries os.Remove on it (which
	// fails because directories require RemoveAll, not Remove). The
	// snapshotLocked error path logs a failure meta but still returns nil.
	oldName := backupNamePrefix + "20200101_000000" + backupNameSuffix
	oldPath := filepath.Join(backupDir, oldName)
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// Put a file inside so os.Remove fails (directory not empty).
	if err := os.WriteFile(filepath.Join(oldPath, "junk"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot should still return nil on retention failure, got %v", err)
	}
	// Meta should record the retention error in LastError.
	m, err := loadMeta(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.LastError, "retention") {
		t.Fatalf("expected retention error in LastError, got %q", m.LastError)
	}
}

func TestVerifyZip_OpenReaderFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-zip.zip")
	if err := os.WriteFile(path, []byte("not actually a zip"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyZip(path); err == nil {
		t.Fatal("expected verifyZip to fail on non-zip data")
	}
}

func TestVerifyZip_OKZipPasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyZip(path); err != nil {
		t.Fatalf("verifyZip on a well-formed zip: %v", err)
	}
}

func TestSnapshot_WithStoreCoversIsEncryptedBranch(t *testing.T) {
	// Cover the "s.cfg.Store != nil" branch in snapshotLocked by handing the
	// service a real (unencrypted) Storage instance. We don't need encryption
	// enabled — we just need IsEncrypted() to be invoked.
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})

	// Use a separate dir for Storage; it doesn't need to match DataDir.
	store, err := storageNew(t)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Snapshot(context.Background()); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	m, err := loadMeta(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Encrypted {
		t.Fatal("expected Encrypted=false for unencrypted store")
	}
}

func TestBuildZip_ReadFileErrorPropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix file permissions")
	}
	dataDir := t.TempDir()
	// File the Walk can see (parent readable) but cannot read (file mode 0).
	bad := filepath.Join(dataDir, "unreadable.csv")
	if err := os.WriteFile(bad, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o600) })

	backupDir := t.TempDir()
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.Snapshot(context.Background())
	if err == nil {
		t.Fatal("expected ReadFile error to bubble up through buildZip")
	}
	if !strings.Contains(err.Error(), "build zip") {
		t.Fatalf("expected wrapped build-zip error, got %v", err)
	}
	// Failure meta must also be present.
	m, err := loadMeta(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if m.LastError == "" {
		t.Fatal("expected LastError populated on ReadFile failure")
	}
}

func TestVerifyZip_EntryOpenFails(t *testing.T) {
	// Build a real zip, then corrupt the local file header's compression
	// method to an unsupported value. zip.OpenReader (which parses only the
	// central directory) still succeeds, but f.Open() — which parses the
	// local header — fails. Covers the `rc, err := f.Open()` error branch
	// in verifyZip.
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-local-header.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("entry.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "content"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// Local file header signature is "PK\x03\x04" at offset 0. The
	// compression-method field is at offsets 8-9 (little-endian). Set it to
	// 0x99 (unsupported) so f.Open() fails with ErrAlgorithm.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !(len(data) > 10 && data[0] == 'P' && data[1] == 'K' && data[2] == 0x03 && data[3] == 0x04) {
		t.Fatalf("zip does not start with local-file-header signature: % x", data[:8])
	}
	data[8] = 0x99
	data[9] = 0x00
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// We also need the central directory entry to advertise the same bad
	// compression method, otherwise zip.OpenReader / zip.File.Open use the
	// central-dir value and never touch the broken local header. The CD
	// signature is "PK\x01\x02". Its compression-method field is at +10.
	cdSig := []byte{'P', 'K', 0x01, 0x02}
	idx := -1
	for i := 0; i+len(cdSig) <= len(data); i++ {
		if data[i] == cdSig[0] && data[i+1] == cdSig[1] && data[i+2] == cdSig[2] && data[i+3] == cdSig[3] {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("could not locate central directory header")
	}
	data[idx+10] = 0x99
	data[idx+11] = 0x00
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	err = verifyZip(path)
	if err == nil {
		t.Fatal("expected verifyZip to fail on bad compression method")
	}
}

func TestVerifyZip_CorruptEntryFailsCRC(t *testing.T) {
	// Build a real zip, then flip a byte in the compressed payload so that
	// reading the entry surfaces a CRC mismatch through the io.Copy loop.
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "hello world hello world"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// Mutate a byte in the middle of the file — typically lands in the
	// compressed data, not the central directory, so zip.OpenReader still
	// works but reading the entry fails CRC.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 40 {
		t.Fatal("zip too small to corrupt safely")
	}
	// Local file header is at offset 0; payload starts after ~30 bytes plus
	// filename. Flip a byte at offset 35 which is inside the data region.
	data[35] ^= 0xFF
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	err = verifyZip(path)
	if err == nil {
		t.Fatal("expected verifyZip to detect corrupted entry")
	}
}
