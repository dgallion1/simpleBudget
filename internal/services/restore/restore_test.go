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

// ---------- pruneExtras walk-error accounting ----------

func TestPruneExtras_WalkAndRemoveFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission fixtures do not block root")
	}
	s, dir := newService(t)

	dataAbs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	skip := backupsvc.SkipPredicate(dir, s.deps.BackupDir)

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
	removed, failures := s.pruneExtras(dataAbs, archive, skip)

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

// An age-encrypted entry going into a store that is not encrypted-and-unlocked
// must be rejected: writing decrypted-looking ciphertext as plaintext would
// silently corrupt the restored file. storage.IsAgeEncryptedData looks only at
// the "age-encryption.org" magic header, so any payload starting with it
// (and longer than the header) qualifies -- the content after it need not be
// real age ciphertext.
func TestFromZipRejectsEncryptedEntryIntoUnlockedStore(t *testing.T) {
	s, dir := newService(t)
	// newService's store has no encryption marker file, so IsEncrypted() is
	// false and this entry must be rejected regardless of IsUnlocked().
	encryptedLooking := "age-encryption.org/v1\n-> X25519 fake-payload-bytes-follow\n"

	_, err := s.FromZip(context.Background(), zipOf(t, map[string]string{
		"secret.csv": encryptedLooking,
	}))
	if !errors.Is(err, ErrEncryptedEntry) {
		t.Fatalf("err = %v, want ErrEncryptedEntry", err)
	}
	// The whole archive is rejected, so nothing may have been written.
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatalf("readdir: %v", rerr)
	}
	if len(entries) != 0 {
		t.Errorf("data dir is not empty after a rejected archive: %v", entries)
	}
}

// A write failure (here: os.MkdirAll cannot create a directory because a
// regular file already occupies that path, so it fails ENOTDIR) must surface
// as ErrWriteFailed. Planting a file where a directory is needed works
// unprivileged and, unlike chmod, is not defeated when tests run as root.
func TestFromZipReportsWriteFailure(t *testing.T) {
	s, dir := newService(t)
	// "sub" must be a directory for "sub/nested.json" to be written, but it
	// already exists as a regular file.
	if err := os.WriteFile(filepath.Join(dir, "sub"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("seed blocking file: %v", err)
	}

	_, err := s.FromZip(context.Background(), zipOf(t, map[string]string{
		"sub/nested.json": "{}",
	}))
	if !errors.Is(err, ErrWriteFailed) {
		t.Fatalf("err = %v, want ErrWriteFailed", err)
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
