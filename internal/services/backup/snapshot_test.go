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
	if err != nil { t.Fatal(err) }
	defer r.Close()
	out := make(map[string][]byte)
	for _, f := range r.File {
		if f.FileInfo().IsDir() { continue }
		rc, err := f.Open()
		if err != nil { t.Fatal(err) }
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil { t.Fatal(err) }
		out[f.Name] = data
	}
	return out
}

func TestSnapshot_RoundTripsAllFileTypes(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{
		"banking.csv":             []byte("a,b,c\n1,2,3\n"),
		"major_expenses.json":     []byte(`{"x":1}`),
		"transaction_pins.json":   []byte(`{"y":2}`),
		"settings/auto_backup.json": []byte(`{"enabled":true}`),
		"settings/whatif_state.json": []byte(`{"baseline":"foo"}`),
	})
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err != nil { t.Fatal(err) }

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
		"keep.csv":             []byte("keep"),
		"cache/plotly.min.js":  []byte("BIG"),
		".encrypted":           []byte("marker"),
		".encryption-verify":   []byte("verify"),
		"foo.tmp":              []byte("partial"),
	})
	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err := svc.Snapshot(context.Background()); err != nil { t.Fatal(err) }
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

func TestSnapshot_RecursiveBackupGuard(t *testing.T) {
	// BackupDir nested under DataDir must be skipped to avoid recursion.
	dataDir := t.TempDir()
	backupDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil { t.Fatal(err) }
	seedDataDir(t, dataDir, map[string][]byte{
		"keep.csv": []byte("keep"),
	})
	// Pre-existing backup file inside backupDir — must NOT be re-zipped.
	if err := os.WriteFile(filepath.Join(backupDir, "budget_backup_OLD.zip"),
		[]byte("DUMMY"), 0600); err != nil { t.Fatal(err) }

	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err := svc.Snapshot(context.Background()); err != nil { t.Fatal(err) }
	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_2*.zip"))
	if len(zips) != 1 { t.Fatalf("want exactly 1 fresh zip, got %d", len(zips)) }
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
	if err := os.WriteFile(orphan, []byte("orphan"), 0600); err != nil { t.Fatal(err) }
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil { t.Fatal(err) }

	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err := svc.Snapshot(context.Background()); err != nil { t.Fatal(err) }
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan .tmp not cleaned up")
	}
}

func TestSnapshotIfStale_SkipsWhenFresh(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err := svc.Snapshot(context.Background()); err != nil { t.Fatal(err) }

	before, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if err := svc.SnapshotIfStale(context.Background(), 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	after, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(after) != len(before) {
		t.Fatalf("SnapshotIfStale created a new zip when fresh: before=%d after=%d", len(before), len(after))
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
