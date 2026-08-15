package restore

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	backupsvc "budget2/internal/services/backup"
	"budget2/internal/services/storage"
)

// zipOf builds an in-memory archive from name->content pairs. Names are used
// verbatim so a test can plant a traversal or absolute path.
func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// newService wires a Service over a temp data dir with a real Storage and a
// real backup service, mirroring how cmd/server wires it. Gate is nil: the
// nil-gate path is exercised by TestFromZipWithoutAGateStillRestores.
func newService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "backups")
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	svc, err := backupsvc.New(backupsvc.Config{BackupDir: backupDir, DataDir: dir, Store: store})
	if err != nil {
		t.Fatalf("backup.New: %v", err)
	}
	return New(Deps{DataDir: dir, BackupDir: backupDir, Store: store, Backups: svc}), dir
}

func TestFromZipRestoresAndPrunes(t *testing.T) {
	s, dir := newService(t)
	// A file the archive does not contain must be pruned.
	if err := os.WriteFile(filepath.Join(dir, "stale.csv"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed stale: %v", err)
	}

	res, err := s.FromZip(context.Background(), zipOf(t, map[string]string{
		"fresh.csv":       "Date,Amount\n2024-01-01,-1.00\n",
		"sub/nested.json": "{}",
	}))
	if err != nil {
		t.Fatalf("FromZip: %v", err)
	}
	if res.Restored != 2 {
		t.Errorf("Restored = %d, want 2", res.Restored)
	}
	if res.Pruned != 1 {
		t.Errorf("Pruned = %d, want 1 (stale.csv)", res.Pruned)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.csv")); !os.IsNotExist(err) {
		t.Errorf("stale.csv survived the restore (stat err = %v)", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "sub", "nested.json"))
	if err != nil || string(got) != "{}" {
		t.Errorf("nested entry not restored: content=%q err=%v", got, err)
	}
}

func TestFromZipRejectsUnsafePaths(t *testing.T) {
	for _, tc := range []struct{ name, entry string }{
		{"traversal", "../escape.csv"},
		{"absolute", "/etc/passwd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, dir := newService(t)
			_, err := s.FromZip(context.Background(), zipOf(t, map[string]string{tc.entry: "x"}))
			if !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("err = %v, want ErrUnsafePath", err)
			}
			// The whole archive is rejected, so nothing may have been written.
			entries, rerr := os.ReadDir(dir)
			if rerr != nil {
				t.Fatalf("readdir: %v", rerr)
			}
			if len(entries) != 0 {
				t.Errorf("data dir is not empty after a rejected archive: %v", entries)
			}
		})
	}
}

func TestFromZipRejectsAnInvalidArchive(t *testing.T) {
	s, _ := newService(t)
	if _, err := s.FromZip(context.Background(), []byte("not a zip")); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("err = %v, want ErrInvalidArchive", err)
	}
}

func TestFromZipRejectsAnArchiveWithNothingRestorable(t *testing.T) {
	s, _ := newService(t)
	// An archive of only directory entries has no files to restore.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("emptydir/"); err != nil {
		t.Fatalf("zip create dir: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if _, err := s.FromZip(context.Background(), buf.Bytes()); !errors.Is(err, ErrEmptyArchive) {
		t.Fatalf("err = %v, want ErrEmptyArchive", err)
	}
}

func TestFromZipReportsAMissingBackupService(t *testing.T) {
	s, _ := newService(t)
	s.deps.Backups = nil
	_, err := s.FromZip(context.Background(), zipOf(t, map[string]string{"a.csv": "x"}))
	if !errors.Is(err, ErrNoBackupService) {
		t.Fatalf("err = %v, want ErrNoBackupService", err)
	}
}

// The gate is optional wiring, and its absence must not stop a restore -- it
// must only make the restore unserialized (and say so in the log). A nil gate
// panicking would take down the server on a wiring mistake.
func TestFromZipWithoutAGateStillRestores(t *testing.T) {
	s, dir := newService(t)
	if s.deps.Gate != nil {
		t.Fatal("test setup: gate should be nil")
	}
	if _, err := s.FromZip(context.Background(), zipOf(t, map[string]string{"a.csv": "x"})); err != nil {
		t.Fatalf("FromZip with nil gate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.csv")); err != nil {
		t.Errorf("a.csv not restored: %v", err)
	}
}

// The gate must be acquired before any write and released only after the
// prune, or a concurrent settings save can interleave with a half-restored
// settings directory. Recording the data dir's contents at acquire and
// release time is what proves the bracket, not merely that it was called.
func TestFromZipHoldsTheGateAcrossWriteAndPrune(t *testing.T) {
	s, dir := newService(t)
	if err := os.WriteFile(filepath.Join(dir, "stale.csv"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed stale: %v", err)
	}

	var atAcquire, atRelease []string
	names := func() []string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("readdir: %v", err)
		}
		var out []string
		for _, e := range entries {
			out = append(out, e.Name())
		}
		return out
	}
	s.deps.Gate = RewriteGateFunc(func() func() {
		atAcquire = names()
		return func() { atRelease = names() }
	})

	if _, err := s.FromZip(context.Background(), zipOf(t, map[string]string{"fresh.csv": "x"})); err != nil {
		t.Fatalf("FromZip: %v", err)
	}
	if len(atAcquire) != 1 || atAcquire[0] != "stale.csv" {
		t.Errorf("at acquire the dir held %v, want only the pre-restore stale.csv: the gate was taken after writing began", atAcquire)
	}
	if len(atRelease) != 1 || atRelease[0] != "fresh.csv" {
		t.Errorf("at release the dir held %v, want only fresh.csv: the gate was released before the prune finished", atRelease)
	}
}
