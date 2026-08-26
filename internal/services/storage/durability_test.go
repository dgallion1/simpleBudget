package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// durability_test.go exercises the fsync-before-publish and
// fsync-the-directory-after-publish behavior added to atomicWrite,
// createExclusive, and saveConfig (T8). It overrides the fileSync/syncDir
// seams declared in storage.go rather than touching real file permissions,
// so the suite stays root-clean: there is no chmod fixture here for root's
// CAP_DAC_OVERRIDE to defeat.
//
// Every test below overrides one or both package-level vars and restores
// them via t.Cleanup. None of them run with t.Parallel: two such overrides
// racing in parallel would stomp on each other's state.

var errInjectedSync = errors.New("durability_test: injected sync failure")

// overrideFileSync replaces the package-level fileSync seam for the
// duration of the calling test.
func overrideFileSync(t *testing.T, fn func(f *os.File) error) {
	t.Helper()
	orig := fileSync
	fileSync = fn
	t.Cleanup(func() { fileSync = orig })
}

// overrideSyncDir replaces the package-level syncDir seam for the duration
// of the calling test.
func overrideSyncDir(t *testing.T, fn func(dir string) error) {
	t.Helper()
	orig := syncDir
	syncDir = fn
	t.Cleanup(func() { syncDir = orig })
}

// realSyncDir performs the same open/Sync/close sequence the production
// syncDir does, so a test recording *that* a call happened can still let
// the real fsync run rather than skip it.
func realSyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// ---------------------------------------------------------------------
// atomicWrite
// ---------------------------------------------------------------------

// TestAtomicWriteSyncsStagingFileAndDirectoryOnSuccess defends the durability
// half of atomicWrite's publish: the staging file must be fsync'd before the
// rename, and the destination directory must be fsync'd after it. Deleting
// either call from atomicWrite leaves one of syncedStaging/syncedDir empty
// and fails this test.
func TestAtomicWriteSyncsStagingFileAndDirectoryOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.csv")

	var syncedStaging, syncedDir string
	overrideFileSync(t, func(f *os.File) error {
		syncedStaging = f.Name()
		return f.Sync()
	})
	overrideSyncDir(t, func(d string) error {
		syncedDir = d
		return realSyncDir(d)
	})

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.atomicWrite(path, []byte("hello"), 0644); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}

	if syncedStaging == "" {
		t.Fatal("fileSync was not called on the staging file during a successful publish")
	}
	if !IsStagingName(filepath.Base(syncedStaging)) {
		t.Errorf("fileSync was called on %q, which is not a staging name", syncedStaging)
	}
	if syncedDir != dir {
		t.Errorf("syncDir called with %q, want %q", syncedDir, dir)
	}
}

// TestAtomicWriteSyncFailureAbortsPublish defends the error-handling half:
// when fileSync fails, atomicWrite must return the error, must not replace
// the destination's existing content, and must not leave the staging file
// behind. Ignoring fileSync's error (rather than returning it) makes this
// test's error check fail; deleting the fileSync call outright removes the
// only way this test can inject a failure, which also fails it since the
// destination would then be replaced.
func TestAtomicWriteSyncFailureAbortsPublish(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.csv")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatalf("WriteFile setup: %v", err)
	}

	overrideFileSync(t, func(f *os.File) error { return errInjectedSync })

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.atomicWrite(path, []byte("replacement"), 0644); !errors.Is(err, errInjectedSync) {
		t.Fatalf("atomicWrite error = %v, want errInjectedSync", err)
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(got) != "original" {
		t.Errorf("destination content = %q, want unchanged %q — a sync failure must not publish", got, "original")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "ledger.csv" {
			t.Errorf("staging leftover after a sync failure: %s", e.Name())
		}
	}
}

// TestAtomicWriteDirSyncFailurePropagates defends the second seam: a syncDir
// failure, occurring after a rename that already landed, must still be
// returned to the caller rather than swallowed — the caller must not be told
// the write is durable when it isn't.
func TestAtomicWriteDirSyncFailurePropagates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.csv")

	overrideSyncDir(t, func(d string) error { return errInjectedSync })

	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.atomicWrite(path, []byte("hello"), 0644); !errors.Is(err, errInjectedSync) {
		t.Fatalf("atomicWrite error = %v, want errInjectedSync", err)
	}

	// The rename already landed before syncDir runs, so the content is in
	// place even though the durability guarantee is not — the point of this
	// test is only that the error reaches the caller.
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
}

// ---------------------------------------------------------------------
// createExclusive
// ---------------------------------------------------------------------

// TestCreateExclusiveSyncsStagingFileAndDirectoryOnSuccess is
// TestAtomicWriteSyncsStagingFileAndDirectoryOnSuccess's counterpart for the
// link-based publish path.
func TestCreateExclusiveSyncsStagingFileAndDirectoryOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.csv")

	var syncedStaging, syncedDir string
	overrideFileSync(t, func(f *os.File) error {
		syncedStaging = f.Name()
		return f.Sync()
	})
	overrideSyncDir(t, func(d string) error {
		syncedDir = d
		return realSyncDir(d)
	})

	if err := createExclusive(path, []byte("hello"), 0644); err != nil {
		t.Fatalf("createExclusive: %v", err)
	}

	if syncedStaging == "" {
		t.Fatal("fileSync was not called on the staging file during a successful publish")
	}
	if !IsStagingName(filepath.Base(syncedStaging)) {
		t.Errorf("fileSync was called on %q, which is not a staging name", syncedStaging)
	}
	if syncedDir != dir {
		t.Errorf("syncDir called with %q, want %q", syncedDir, dir)
	}
}

// TestCreateExclusiveSyncFailureAbortsPublish is
// TestAtomicWriteSyncFailureAbortsPublish's counterpart: a fileSync failure
// must stop the link from ever being made, and must not leave the staging
// file behind.
func TestCreateExclusiveSyncFailureAbortsPublish(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.csv")

	overrideFileSync(t, func(f *os.File) error { return errInjectedSync })

	if err := createExclusive(path, []byte("hello"), 0644); !errors.Is(err, errInjectedSync) {
		t.Fatalf("createExclusive error = %v, want errInjectedSync", err)
	}

	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("destination created despite a sync failure: stat err=%v", statErr)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		t.Errorf("staging leftover after a sync failure: %s", e.Name())
	}
}

// TestCreateExclusiveDirSyncFailurePropagates is
// TestAtomicWriteDirSyncFailurePropagates's counterpart for the link-based
// publish path.
func TestCreateExclusiveDirSyncFailurePropagates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.csv")

	overrideSyncDir(t, func(d string) error { return errInjectedSync })

	if err := createExclusive(path, []byte("hello"), 0644); !errors.Is(err, errInjectedSync) {
		t.Fatalf("createExclusive error = %v, want errInjectedSync", err)
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
}

// ---------------------------------------------------------------------
// saveConfig
// ---------------------------------------------------------------------

// TestSaveConfigSyncsStagingFileAndDirectoryOnSuccess is
// TestAtomicWriteSyncsStagingFileAndDirectoryOnSuccess's counterpart for the
// encryption config's own publish path — the file whose loss makes every
// other encrypted file unreadable.
func TestSaveConfigSyncsStagingFileAndDirectoryOnSuccess(t *testing.T) {
	dir := t.TempDir()

	var syncedStaging, syncedDir string
	overrideFileSync(t, func(f *os.File) error {
		syncedStaging = f.Name()
		return f.Sync()
	})
	overrideSyncDir(t, func(d string) error {
		syncedDir = d
		return realSyncDir(d)
	})

	if err := saveConfig(dir, &EncryptionConfig{Method: AuthMethodPassword}); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	if syncedStaging == "" {
		t.Fatal("fileSync was not called on the staging file during a successful save")
	}
	if !IsStagingName(filepath.Base(syncedStaging)) {
		t.Errorf("fileSync was called on %q, which is not a staging name", syncedStaging)
	}
	if syncedDir != dir {
		t.Errorf("syncDir called with %q, want %q", syncedDir, dir)
	}
}

// TestSaveConfigSyncFailureAbortsPublish is
// TestAtomicWriteSyncFailureAbortsPublish's counterpart: a fileSync failure
// must not replace an existing config on disk, and must leave nothing behind.
func TestSaveConfigSyncFailureAbortsPublish(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, configFile)
	if err := os.WriteFile(configPath, []byte("original"), 0600); err != nil {
		t.Fatalf("WriteFile setup: %v", err)
	}

	overrideFileSync(t, func(f *os.File) error { return errInjectedSync })

	err := saveConfig(dir, &EncryptionConfig{Method: AuthMethodAge})
	if !errors.Is(err, errInjectedSync) {
		t.Fatalf("saveConfig error = %v, want one wrapping errInjectedSync", err)
	}

	got, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(got) != "original" {
		t.Errorf("config content = %q, want unchanged %q — a sync failure must not publish", got, "original")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != configFile {
			t.Errorf("staging leftover after a sync failure: %s", e.Name())
		}
	}
}

// TestSaveConfigDirSyncFailurePropagates is
// TestAtomicWriteDirSyncFailurePropagates's counterpart: a syncDir failure
// after a successful rename must still reach the caller.
func TestSaveConfigDirSyncFailurePropagates(t *testing.T) {
	dir := t.TempDir()

	overrideSyncDir(t, func(d string) error { return errInjectedSync })

	err := saveConfig(dir, &EncryptionConfig{Method: AuthMethodPassword})
	if !errors.Is(err, errInjectedSync) {
		t.Fatalf("saveConfig error = %v, want errInjectedSync", err)
	}

	// The rename already landed before syncDir runs, so the config is in
	// place even though the durability guarantee is not.
	loaded, loadErr := loadConfig(dir)
	if loadErr != nil {
		t.Fatalf("loadConfig: %v", loadErr)
	}
	if loaded == nil || loaded.Method != AuthMethodPassword {
		t.Errorf("config = %+v, want the rename to have landed even though syncDir failed", loaded)
	}
}
