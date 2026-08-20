package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/services/storage"
)

// helper: populate a fake data dir with the file-types we expect to back up
func seedDataDir(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func zipEntries(t *testing.T, zipPath string) map[string][]byte {
	t.Helper()
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out := make(map[string][]byte)
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = data
	}
	return out
}

func TestSnapshot_RoundTripsAllFileTypes(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{
		"banking.csv":                []byte("a,b,c\n1,2,3\n"),
		"major_expenses.json":        []byte(`{"x":1}`),
		"transaction_pins.json":      []byte(`{"y":2}`),
		"settings/auto_backup.json":  []byte(`{"enabled":true}`),
		"settings/whatif_state.json": []byte(`{"baseline":"foo"}`),
	})
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(zips) != 1 {
		t.Fatalf("want 1 zip, got %d (%v)", len(zips), zips)
	}
	got := zipEntries(t, zips[0])
	for rel, want := range map[string][]byte{
		"banking.csv":               []byte("a,b,c\n1,2,3\n"),
		"major_expenses.json":       []byte(`{"x":1}`),
		"transaction_pins.json":     []byte(`{"y":2}`),
		"settings/auto_backup.json": []byte(`{"enabled":true}`),
	} {
		if !bytes.Equal(got[rel], want) {
			t.Errorf("entry %q mismatch\ngot  %q\nwant %q", rel, got[rel], want)
		}
	}
}

func TestSnapshot_SkipsCacheAndMarkers(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{
		"keep.csv":            []byte("keep"),
		"cache/plotly.min.js": []byte("BIG"),
		".encrypted":          []byte("marker"),
		".encryption-verify":  []byte("verify"),
		"foo.tmp":             []byte("partial"),
	})
	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err := svc.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	got := zipEntries(t, zips[0])
	if _, ok := got["keep.csv"]; !ok {
		t.Errorf("keep.csv missing from backup")
	}
	for skipped := range map[string]bool{
		"cache/plotly.min.js": true, ".encrypted": true,
		".encryption-verify": true, "foo.tmp": true,
	} {
		if _, ok := got[skipped]; ok {
			t.Errorf("entry %q should have been skipped", skipped)
		}
	}
}

func TestSkipPredicate_FilesUnderSkipListedDirs(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	skip := SkipPredicate(dataDir, backupDir)

	cases := []struct {
		rel   string
		isDir bool
		want  bool
	}{
		// Files under a cache/ ancestor are skipped even when the predicate
		// is consulted flat (no directory walk), as restore does.
		{filepath.Join("cache", "plotly.min.js"), false, true},
		{filepath.Join("cache", "sub", "x.js"), false, true},
		// The cache directory itself.
		{"cache", true, true},
		{filepath.Join("nested", "cache"), true, true},
		// No false positives on names merely starting with "cache".
		{"cachefile.csv", false, false},
		{filepath.Join("cachedir", "keep.csv"), false, false},
		// Ordinary files stay included.
		{"keep.csv", false, false},
		{filepath.Join("settings", "whatif.json"), false, false},
	}
	for _, tc := range cases {
		path := filepath.Join(dataDir, tc.rel)
		if got := skip(path, tc.isDir); got != tc.want {
			t.Errorf("skip(%q, isDir=%v) = %v, want %v", tc.rel, tc.isDir, got, tc.want)
		}
	}
}

// newStagingName derives a name in exactly the form atomicWrite and
// createExclusive stage under — os.CreateTemp(dir, base+storage.
// StagingSuffix+"*") — using storage's own exported separator rather than a
// hand-written ".tmp-" literal, so this fixture cannot silently drift out of
// sync with what the storage package actually produces. The file is created
// on disk (a real os.CreateTemp call, not a string built by hand) and
// removed again; only its generated name is kept.
func newStagingName(t *testing.T, dir, destBase string) string {
	t.Helper()
	f, err := os.CreateTemp(dir, destBase+storage.StagingSuffix+"*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	name := filepath.Base(f.Name())
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Remove(f.Name()); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	return name
}

// TestSkipPredicate_StagingNames pins the cross-package contract from R11
// attempt 3: SkipPredicate must recognise a staging name via
// storage.IsStagingName, not a hardcoded literal, and must keep recognising
// both the current random-suffixed form and the pre-fix fixed-suffix form
// left behind by crashes before staging names were randomised. It must NOT
// treat every file that merely contains ".tmp" as a leftover.
func TestSkipPredicate_StagingNames(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	skip := SkipPredicate(dataDir, backupDir)

	// Current form: <dest-base>.tmp-<random>, derived from the real staging
	// path (see newStagingName), not typed out by hand.
	current := newStagingName(t, dataDir, "sidecar.json")
	if !skip(filepath.Join(dataDir, current), false) {
		t.Errorf("SkipPredicate did not skip %q, a name produced by the real "+
			"atomicWrite/createExclusive staging path", current)
	}

	// Legacy form: staging files orphaned by a crash before this change —
	// path+".tmp" with no random suffix — must still be swept up.
	const legacy = "sidecar.json.tmp"
	if !skip(filepath.Join(dataDir, legacy), false) {
		t.Errorf("SkipPredicate did not skip %q, the pre-fix staging name "+
			"(pre-existing crash orphans must still be recognised)", legacy)
	}

	// A legitimate data file whose name merely contains ".tmp" as a
	// substring — not as the exact legacy suffix, and not followed by a
	// decimal random component after StagingSuffix — is real user data and
	// must be backed up, not silently dropped.
	const legitimate = "report.tmp-notes.csv"
	if skip(filepath.Join(dataDir, legitimate), false) {
		t.Errorf("SkipPredicate skipped %q, a legitimate file that merely "+
			"contains \".tmp\"/StagingSuffix as a substring — it is not a "+
			"staging leftover and must not be excluded from backups", legitimate)
	}
}

func TestSnapshot_RecursiveBackupGuard(t *testing.T) {
	// BackupDir nested under DataDir must be skipped to avoid recursion.
	dataDir := t.TempDir()
	backupDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	seedDataDir(t, dataDir, map[string][]byte{
		"keep.csv": []byte("keep"),
	})
	// Pre-existing backup file inside backupDir — must NOT be re-zipped.
	if err := os.WriteFile(filepath.Join(backupDir, "budget_backup_OLD.zip"),
		[]byte("DUMMY"), 0600); err != nil {
		t.Fatal(err)
	}

	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err := svc.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_2*.zip"))
	if len(zips) != 1 {
		t.Fatalf("want exactly 1 fresh zip, got %d", len(zips))
	}
	got := zipEntries(t, zips[0])
	for k := range got {
		if strings.HasPrefix(k, "backups/") {
			t.Fatalf("backup zip recursively included BackupDir entry: %q", k)
		}
	}
}

func TestSnapshot_OrphanTmpCleanedUp(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	// Plant an orphan .tmp older than 1 hour.
	orphan := filepath.Join(backupDir, "budget_backup_OLD.zip.tmp")
	if err := os.WriteFile(orphan, []byte("orphan"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}

	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err := svc.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan .tmp not cleaned up")
	}
}

func TestSnapshotIfStale_SkipsWhenFresh(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err := svc.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}

	before, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if err := svc.SnapshotIfStale(context.Background(), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	after, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(after) != len(before) {
		t.Fatalf("SnapshotIfStale created a new zip when fresh: before=%d after=%d", len(before), len(after))
	}
}

func TestSnapshotIfStale_NoOpWhenDisabled(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err := svc.SetEnabled(false); err != nil {
		t.Fatal(err)
	}

	if err := svc.SnapshotIfStale(context.Background(), 24*time.Hour); err != nil {
		t.Fatalf("SnapshotIfStale should be a no-op when disabled, got %v", err)
	}
	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(zips) != 0 {
		t.Fatalf("disabled SnapshotIfStale created %d zips, want 0", len(zips))
	}
}

func TestSnapshotIfStale_FiresWhenStale(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})

	// Write a stale meta directly.
	staleTS := time.Now().Add(-48 * time.Hour).UTC().Format("20060102_150405")
	if err := writeMetaSuccess(backupDir, Meta{TS: staleTS, FileCount: 1, TotalBytes: 1}); err != nil {
		t.Fatal(err)
	}

	if err := svc.SnapshotIfStale(context.Background(), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(zips) != 1 {
		t.Fatalf("expected 1 fresh zip, got %d", len(zips))
	}
}

func TestSnapshot_ExcludesEncryptionStateFiles(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{
		"banking.csv":             []byte("a,b\n"),
		".encryption-config.json": []byte(`{"method":"age"}`),
		".encrypted":              []byte("marker"),
		".encryption-verify":      []byte("verify"),
	})
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(zips) != 1 {
		t.Fatalf("want 1 zip, got %v", zips)
	}
	got := zipEntries(t, zips[0])
	if _, ok := got["banking.csv"]; !ok {
		t.Fatal("banking.csv missing from snapshot")
	}
	for _, name := range []string{".encryption-config.json", ".encrypted", ".encryption-verify"} {
		if _, ok := got[name]; ok {
			t.Errorf("snapshot must not contain encryption-state file %s", name)
		}
	}
}

func TestSnapshotAndHold_SerializesUntilRelease(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"banking.csv": []byte("a\n")})
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}

	release, err := svc.SnapshotAndHold(context.Background())
	if err != nil {
		t.Fatalf("SnapshotAndHold: %v", err)
	}
	if err := svc.Snapshot(context.Background()); !errors.Is(err, ErrSnapshotInProgress) {
		t.Fatalf("Snapshot while lock held: want ErrSnapshotInProgress, got %v", err)
	}
	release()
	if err := svc.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot after release: %v", err)
	}
}

func TestSnapshotAndHold_ReleasesLockOnSnapshotFailure(t *testing.T) {
	dataDir := t.TempDir()
	// BackupDir is a file, so MkdirAll inside the snapshot fails.
	backupDir := filepath.Join(dataDir, "backup-file")
	if err := os.WriteFile(backupDir, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}
	seedDataDir(t, dataDir, map[string][]byte{"banking.csv": []byte("a\n")})
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.SnapshotAndHold(context.Background()); err == nil {
		t.Fatal("SnapshotAndHold should fail when BackupDir is a file")
	}
	// The lock must not leak: a snapshot attempt reaches the real error,
	// not ErrSnapshotInProgress.
	if err := svc.Snapshot(context.Background()); errors.Is(err, ErrSnapshotInProgress) {
		t.Fatalf("lock leaked after failed SnapshotAndHold: %v", err)
	}
}
