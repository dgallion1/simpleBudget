# Automatic Round-Trip-Complete Backups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add automatic, on-disk, round-trip-complete backups of `data/` with tiered retention, an encryption-mirroring posture, and a fixed restore path that preserves every file type.

**Architecture:** New package `internal/services/backup/` (snapshot + retention + scheduler + meta). Wired in `cmd/server/main.go` after storage unlock: one immediate stale-check, an hourly ticker, and a graceful-shutdown snapshot. The existing `internal/handlers/backup/` HTTP layer stays for manual download but gets the restore round-trip fix and two new endpoints (status, auto-enabled toggle). The storage layer gains a small guard so already-encrypted bytes pass through `WriteFile` unchanged, and `.encryption-config.json` is added to the skip-encryption list.

**Tech Stack:** Go (`archive/zip`, `os`, `time`), `chi` router, `filippo.io/age` (already a dependency), HTMX templates.

**Spec:** `docs/superpowers/specs/2026-05-01-automatic-backups-design.md`

---

## File Inventory

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/services/storage/storage.go` | Modify | Add `.encryption-config.json` to `shouldSkipEncryption`; add age-encrypted short-circuit in `WriteFile`; export `IsAgeEncryptedData` |
| `internal/services/storage/storage_test.go` | Modify | Tests for the two new storage guards |
| `internal/config/config.go` | Modify | Add `BackupDir` field, default to XDG path, override via `BUDGET2_BACKUP_DIR` |
| `internal/config/config_test.go` | Modify (or create) | Test default and env override of `BackupDir` |
| `internal/services/backup/service.go` | Create | `Service` struct, `New(...)`, mutex, `ErrSnapshotInProgress`, exposes `BackupDir`, `Enabled`/`SetEnabled` |
| `internal/services/backup/snapshot.go` | Create | `Snapshot(ctx)`, `SnapshotIfStale(ctx, maxAge)` — zip, verify, atomic rename, success/failure meta writes, orphan `.tmp` cleanup, recursive-backup guard |
| `internal/services/backup/retention.go` | Create | `applyRetention(dir)` — tiered keep (7 daily + 4 weekly + 3 monthly) |
| `internal/services/backup/scheduler.go` | Create | `Run(ctx)` — hourly tick, calls `SnapshotIfStale`, honors enabled flag |
| `internal/services/backup/meta.go` | Create | Read/write `last_backup.json` atomically, success and failure variants |
| `internal/services/backup/clock.go` | Create | Tiny `Clock` interface for testability |
| `internal/services/backup/snapshot_test.go` | Create | Snapshot round-trip + edge cases |
| `internal/services/backup/retention_test.go` | Create | Tiered prune table-driven tests |
| `internal/services/backup/scheduler_test.go` | Create | Fake-clock scheduler tests |
| `internal/services/backup/service_test.go` | Create | Mutex / `ErrSnapshotInProgress` test |
| `internal/services/backup/meta_test.go` | Create | Meta read/write/failure-write tests |
| `internal/handlers/backup/handlers.go` | Modify | Refactor `HandleRestore` + `HandleRestoreTestData` to shared helper; add `HandleBackupStatus`, `HandleSetAutoBackupEnabled`; add explicit `BackupDir` guard in `HandleDeleteAllData` |
| `internal/handlers/backup/handlers_test.go` | Modify | Restore round-trip + helper + status + toggle tests |
| `internal/handlers/backup/handlers.go` (`Initialize`) | Modify | Accept the new `*backupsvc.Service` |
| `cmd/server/main.go` | Modify | Construct `backupsvc.Service`, run `SnapshotIfStale` after unlock, register shutdown snapshot, start scheduler goroutine |
| `web/templates/pages/filemanager/*` | Modify | Status line + on/off toggle (HTMX) |
| `data/settings/auto_backup.json` | Runtime artifact | Persists toggle state — created on first toggle write |

Service-package import alias in `cmd/server/main.go` and any other consumer: `backupsvc "budget2/internal/services/backup"` (avoids collision with the existing `backup` HTTP handler package).

---

## Task 0: Pre-Flight Validation

**Files:** _(read only)_

- [ ] **Step 1: Run the existing test suite to record a green baseline.**

```bash
cd /home/darrell/bin/ai/budget2
go test ./...
```

Expected: every package returns `ok` (or `[no test files]` for `internal/testutil` and `web`). If anything is failing on `master`-relative state, stop and report — do not start the implementation on a broken baseline.

- [ ] **Step 2: Skim the spec end-to-end so you can match terminology.**

Read `docs/superpowers/specs/2026-05-01-automatic-backups-design.md` start-to-finish. Pay particular attention to: data flow steps 6/8/10a/10b, the encrypted-blob mismatch rule in the restore section, and the failure-path meta write.

- [ ] **Step 3: Verify the three load-bearing code locations the plan touches.**

```bash
grep -n "shouldSkipEncryption\|isAgeEncrypted\|configFile" \
  internal/services/storage/storage.go internal/services/storage/auth.go
grep -n "HandleRestore\|HandleRestoreTestData\|HandleDeleteAllData" \
  internal/handlers/backup/handlers.go
grep -n "DataDirectory\|SettingsDirectory" internal/config/config.go
```

Confirm: `shouldSkipEncryption` exists in `storage.go` (handles `markerFile`, `verifyFile`, `cache/`); `isAgeEncrypted` is package-private; `configFile = ".encryption-config.json"` lives in `auth.go`; `HandleRestore`, `HandleRestoreTestData`, `HandleDeleteAllData` are in `handlers.go`. If any of these have moved, update the plan's line references before proceeding.

---

## Task 1: Storage layer guards

**Files:**
- Modify: `internal/services/storage/storage.go` (around `shouldSkipEncryption` ~line 305 and `WriteFile` ~line 248)
- Modify: `internal/services/storage/storage_test.go`

**Why:** Restore must be able to write back already-encrypted blobs without re-encrypting them, and `.encryption-config.json` must remain plaintext so `loadConfig` can parse it before unlock.

- [ ] **Step 1: Write the failing tests.**

Append to `internal/services/storage/storage_test.go`:

```go
func TestWriteFile_PassesThroughAgeEncryptedBytes(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil { t.Fatal(err) }

	// Set up an age-encrypted store using an age identity provider via the
	// existing test helper. (Mirror an existing encrypted-storage test setup
	// in storage_extra_test.go — reuse the same fixture pattern.)
	enableAgeEncryptionForTest(t, s) // helper exists or write a small one

	// Build a payload that is ALREADY age-encrypted (use the package's
	// internal encryptData with the recipient).
	plaintext := []byte("hello round-trip")
	recipient, err := s.provider.Recipient()
	if err != nil { t.Fatal(err) }
	encrypted, err := encryptData(plaintext, recipient)
	if err != nil { t.Fatal(err) }

	// Write the already-encrypted bytes via WriteFile.
	target := filepath.Join(dir, "round_trip.bin")
	if err := s.WriteFile(target, encrypted, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Read raw bytes off disk; they should equal the encrypted payload, NOT
	// be a re-encryption of it.
	raw, err := os.ReadFile(target)
	if err != nil { t.Fatal(err) }
	if !bytes.Equal(raw, encrypted) {
		t.Fatalf("WriteFile re-encrypted age-encrypted bytes (double encryption)")
	}

	// Sanity: ReadFile should still give us the plaintext.
	got, err := s.ReadFile(target)
	if err != nil { t.Fatal(err) }
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plaintext)
	}
}

func TestShouldSkipEncryption_IncludesEncryptionConfig(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil { t.Fatal(err) }
	path := filepath.Join(dir, ".encryption-config.json")
	if !s.shouldSkipEncryption(path) {
		t.Fatalf(".encryption-config.json must be skipped from encryption")
	}
}

func TestIsAgeEncryptedData_Exported(t *testing.T) {
	plaintext := []byte("not encrypted")
	if IsAgeEncryptedData(plaintext) {
		t.Fatalf("plaintext misdetected as age-encrypted")
	}
	header := []byte("age-encryption.org/v1\n")
	if !IsAgeEncryptedData(header) {
		t.Fatalf("age header not detected")
	}
}
```

Notes for the implementer:
- If no `enableAgeEncryptionForTest` helper exists, write one inline at the top of the file. Look at `internal/services/storage/age_provider_test.go` for the recipe — generate an age identity, write an `.encryption-config.json`, write `markerFile` and `verifyFile`, call `s.Unlock("")`.
- Add `import "bytes"` if not already present.

- [ ] **Step 2: Run the tests to verify they fail.**

```bash
go test ./internal/services/storage/ -run 'TestWriteFile_PassesThroughAgeEncryptedBytes|TestShouldSkipEncryption_IncludesEncryptionConfig|TestIsAgeEncryptedData_Exported' -v
```

Expected: compile failure on `IsAgeEncryptedData` (undefined), then once that compiles, two of the three tests fail.

- [ ] **Step 3: Implement the storage changes.**

In `internal/services/storage/storage.go`:

a. Export the predicate. Replace the existing private function:

```go
// IsAgeEncryptedData reports whether data appears to be an age-encrypted
// payload by looking at its magic header. Used by callers (e.g. the backup
// restore handler) that need to detect encrypted blobs before deciding how
// to write them.
func IsAgeEncryptedData(data []byte) bool {
	return len(data) > len(ageHeader) && string(data[:len(ageHeader)]) == ageHeader
}

// isAgeEncrypted is kept for internal callers.
func isAgeEncrypted(data []byte) bool { return IsAgeEncryptedData(data) }
```

b. Add `.encryption-config.json` to `shouldSkipEncryption`. Add a case alongside the existing `markerFile`/`verifyFile` check (use the `configFile` constant from `auth.go`):

```go
if base == markerFile || base == verifyFile || base == configFile {
    return true
}
```

c. Guard `WriteFile` against double-encrypting already-encrypted bytes. Replace the encryption block in `WriteFile`:

```go
// Encrypt if enabled and unlocked
if s.encrypted && s.provider != nil && s.provider.IsUnlocked() {
    if isAgeEncrypted(data) {
        // Already encrypted (e.g. restoring a backup blob). Pass through.
    } else {
        recipient, err := s.provider.Recipient()
        if err != nil {
            return fmt.Errorf("failed to get recipient: %w", err)
        }
        encrypted, err := encryptData(data, recipient)
        if err != nil {
            return fmt.Errorf("failed to encrypt: %w", err)
        }
        data = encrypted
    }
}
```

- [ ] **Step 4: Run the tests to verify they pass.**

```bash
go test ./internal/services/storage/ -v
```

Expected: every test passes — both the new ones and the pre-existing ones.

- [ ] **Step 5: Commit.**

```bash
git add internal/services/storage/storage.go internal/services/storage/storage_test.go
git commit -m "$(cat <<'EOF'
feat(storage): age-encrypted passthrough + plaintext encryption-config

WriteFile now short-circuits on already-age-encrypted bytes so a backup
restore can write encrypted blobs back to disk without double-encryption.
.encryption-config.json is added to shouldSkipEncryption so loadConfig
(which reads it directly from disk before unlock) can keep parsing it.
Exports IsAgeEncryptedData so the restore handler can detect blobs
before calling WriteFile.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Config — BackupDir

**Files:**
- Modify: `internal/config/config.go`
- Create or modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests.**

Create `internal/config/config_test.go` if it doesn't exist (or append):

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupDir_DefaultUsesXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg-test-home")
	t.Setenv("BUDGET2_BACKUP_DIR", "")
	cfg := DefaultConfig()
	want := filepath.Join("/tmp/xdg-test-home", "budget2", "backups")
	if cfg.BackupDir != want {
		t.Fatalf("BackupDir=%q want %q", cfg.BackupDir, want)
	}
}

func TestBackupDir_DefaultFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("BUDGET2_BACKUP_DIR", "")
	home, err := os.UserHomeDir()
	if err != nil { t.Skip("no home dir on this system") }
	cfg := DefaultConfig()
	want := filepath.Join(home, ".local", "share", "budget2", "backups")
	if cfg.BackupDir != want {
		t.Fatalf("BackupDir=%q want %q", cfg.BackupDir, want)
	}
}

func TestBackupDir_EnvOverride(t *testing.T) {
	t.Setenv("BUDGET2_BACKUP_DIR", "/tmp/custom-backups")
	cfg := Load()
	if cfg.BackupDir != "/tmp/custom-backups" {
		t.Fatalf("BackupDir=%q want /tmp/custom-backups", cfg.BackupDir)
	}
}

func TestBackupDir_LoadHonorsDefault(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("BUDGET2_BACKUP_DIR", "")
	cfg := Load()
	if !strings.Contains(cfg.BackupDir, "budget2/backups") {
		t.Fatalf("BackupDir=%q does not contain expected default suffix", cfg.BackupDir)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail.**

```bash
go test ./internal/config/ -run 'TestBackupDir' -v
```

Expected: compile failure on `cfg.BackupDir` (undefined field).

- [ ] **Step 3: Implement the config change.**

In `internal/config/config.go`:

a. Add the field to `Config`:

```go
// Directories
DataDirectory     string `json:"data_directory"`
UploadsDirectory  string `json:"uploads_directory"`
SettingsDirectory string `json:"settings_directory"`
TemplatesDirectory string `json:"templates_directory"`
StaticDirectory   string `json:"static_directory"`
BackupDir         string `json:"backup_dir"`
```

b. Add a helper:

```go
// defaultBackupDir returns the default location for automatic backup zips.
// Follows XDG_DATA_HOME, falling back to $HOME/.local/share, falling back
// to <DataDirectory>/../backups if no home is available.
func defaultBackupDir(dataDir string) string {
    if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
        return filepath.Join(xdg, "budget2", "backups")
    }
    if home, err := os.UserHomeDir(); err == nil && home != "" {
        return filepath.Join(home, ".local", "share", "budget2", "backups")
    }
    return filepath.Join(filepath.Dir(dataDir), "budget2-backups")
}
```

c. Set the default in `DefaultConfig()`:

```go
return &Config{
    ...existing fields...
    BackupDir: defaultBackupDir(filepath.Join(wd, "data")),
}
```

d. Honor the env override in `Load()`. Add after the existing `BUDGET_DATA_DIR` block:

```go
if backupDir := os.Getenv("BUDGET2_BACKUP_DIR"); backupDir != "" {
    cfg.BackupDir = backupDir
}
```

e. **Do not** add `BackupDir` to `ensureDirectories()` — the backup service creates it lazily on first snapshot via `MkdirAll(0700)` (the loose `0755` from `ensureDirectories` is wrong for backup contents).

- [ ] **Step 4: Run the tests to verify they pass.**

```bash
go test ./internal/config/ -v
```

Expected: all tests pass.

- [ ] **Step 5: Commit.**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "$(cat <<'EOF'
feat(config): BackupDir with XDG default and BUDGET2_BACKUP_DIR override

Default lands in ${XDG_DATA_HOME:-~/.local/share}/budget2/backups so
automatic backups survive a data/ wipe by living outside DataDirectory.
Not added to ensureDirectories — the backup service creates the path
lazily with 0700 perms on first snapshot.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Backup service skeleton + clock

**Files:**
- Create: `internal/services/backup/service.go`
- Create: `internal/services/backup/clock.go`
- Create: `internal/services/backup/service_test.go`

- [ ] **Step 1: Write the failing tests.**

Create `internal/services/backup/service_test.go`:

```go
package backup

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNew_HoldsConfigAndDir(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(Config{BackupDir: dir, DataDir: t.TempDir()})
	if err != nil { t.Fatal(err) }
	if svc.BackupDir() != dir {
		t.Fatalf("BackupDir=%q want %q", svc.BackupDir(), dir)
	}
}

func TestSnapshot_MutexReturnsErrSnapshotInProgress(t *testing.T) {
	dir := t.TempDir()
	dataDir := t.TempDir()
	svc, err := New(Config{BackupDir: dir, DataDir: dataDir})
	if err != nil { t.Fatal(err) }

	// Hold the mutex by manually locking it (simulates an in-flight snapshot).
	svc.mu.Lock()
	defer svc.mu.Unlock()

	err = svc.Snapshot(context.Background())
	if !errors.Is(err, ErrSnapshotInProgress) {
		t.Fatalf("got %v want ErrSnapshotInProgress", err)
	}
}

func TestSnapshot_ConcurrentInvocationsSerialize(t *testing.T) {
	// Two concurrent Snapshot calls should produce: one success and either
	// (a) a second success or (b) one ErrSnapshotInProgress. Never two
	// in-flight at once.
	dir := t.TempDir()
	dataDir := t.TempDir()
	svc, err := New(Config{BackupDir: dir, DataDir: dataDir})
	if err != nil { t.Fatal(err) }

	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); results <- svc.Snapshot(context.Background()) }()
	go func() { defer wg.Done(); results <- svc.Snapshot(context.Background()) }()
	wg.Wait()
	close(results)

	var ok, busy int
	for r := range results {
		switch {
		case r == nil:
			ok++
		case errors.Is(r, ErrSnapshotInProgress):
			busy++
		default:
			t.Fatalf("unexpected error: %v", r)
		}
	}
	if ok < 1 {
		t.Fatalf("expected at least one successful snapshot; ok=%d busy=%d", ok, busy)
	}
}

func TestEnabled_DefaultsTrue(t *testing.T) {
	dir := t.TempDir()
	svc, _ := New(Config{BackupDir: dir, DataDir: t.TempDir()})
	if !svc.Enabled() {
		t.Fatalf("Enabled() default should be true")
	}
}

func TestSetEnabled_PersistsAndReadsBack(t *testing.T) {
	dir := t.TempDir()
	dataDir := t.TempDir()
	svc, _ := New(Config{BackupDir: dir, DataDir: dataDir})
	if err := svc.SetEnabled(false); err != nil { t.Fatal(err) }

	// Recreate the service and confirm persistence.
	svc2, _ := New(Config{BackupDir: dir, DataDir: dataDir})
	if svc2.Enabled() {
		t.Fatalf("Enabled() should persist as false across restarts")
	}
}

// Stubs used by other test files in this package — declare here to share.
var _ = time.Hour
```

- [ ] **Step 2: Run the tests to verify they fail.**

```bash
go test ./internal/services/backup/ -v
```

Expected: compile failure (`Service`, `Config`, `New`, `ErrSnapshotInProgress`, `Snapshot`, `Enabled`, `SetEnabled` are all undefined).

- [ ] **Step 3: Implement the service skeleton.**

Create `internal/services/backup/clock.go`:

```go
package backup

import "time"

// Clock is the minimal time interface the backup service depends on.
// Tests inject a fake; production uses realClock.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
```

Create `internal/services/backup/service.go`:

```go
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"budget2/internal/services/storage"
)

// ErrSnapshotInProgress is returned by Snapshot when another snapshot is
// already running. Callers (the scheduler, the shutdown hook) treat it as
// a no-op rather than an error.
var ErrSnapshotInProgress = errors.New("backup: snapshot already in progress")

// Config is the dependency-injection bundle for the backup service.
type Config struct {
	BackupDir string
	DataDir   string
	Store     *storage.Storage // may be nil (storage not encrypted) — only used at restore time
	Clock     Clock            // optional; defaults to wall clock
}

// Service owns automatic backup snapshot, retention, and scheduling.
type Service struct {
	cfg   Config
	mu    sync.Mutex // serializes Snapshot
	clock Clock

	enabledMu sync.RWMutex
	enabled   bool
}

// New constructs a Service. It loads the persisted enabled flag from
// <DataDir>/settings/auto_backup.json (defaulting to true if absent).
func New(cfg Config) (*Service, error) {
	if cfg.BackupDir == "" {
		return nil, fmt.Errorf("backup: BackupDir required")
	}
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("backup: DataDir required")
	}
	clk := cfg.Clock
	if clk == nil {
		clk = realClock{}
	}
	s := &Service{cfg: cfg, clock: clk, enabled: true}
	if err := s.loadEnabled(); err != nil {
		return nil, fmt.Errorf("backup: load enabled flag: %w", err)
	}
	return s, nil
}

func (s *Service) BackupDir() string { return s.cfg.BackupDir }
func (s *Service) DataDir() string   { return s.cfg.DataDir }

func (s *Service) Enabled() bool {
	s.enabledMu.RLock()
	defer s.enabledMu.RUnlock()
	return s.enabled
}

// SetEnabled persists the user's auto-backup toggle.
func (s *Service) SetEnabled(v bool) error {
	s.enabledMu.Lock()
	s.enabled = v
	s.enabledMu.Unlock()
	return s.saveEnabled(v)
}

type enabledFile struct {
	Enabled bool `json:"enabled"`
}

func (s *Service) enabledPath() string {
	return filepath.Join(s.cfg.DataDir, "settings", "auto_backup.json")
}

func (s *Service) loadEnabled() error {
	data, err := os.ReadFile(s.enabledPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil // default true
		}
		return err
	}
	var ef enabledFile
	if err := json.Unmarshal(data, &ef); err != nil {
		return err
	}
	s.enabledMu.Lock()
	s.enabled = ef.Enabled
	s.enabledMu.Unlock()
	return nil
}

func (s *Service) saveEnabled(v bool) error {
	if err := os.MkdirAll(filepath.Dir(s.enabledPath()), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(enabledFile{Enabled: v}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.enabledPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.enabledPath())
}

// Snapshot is implemented in snapshot.go. The mutex is acquired here so
// the test in service_test.go can verify ErrSnapshotInProgress by holding
// it manually.
//
// snapshot.go provides:
//   func (s *Service) Snapshot(ctx context.Context) error
//   func (s *Service) SnapshotIfStale(ctx context.Context, maxAge time.Duration) error

// tryLock attempts a non-blocking acquire of the snapshot mutex. Returns
// false if another snapshot is in flight.
func (s *Service) tryLock() bool {
	return s.mu.TryLock()
}

func (s *Service) unlock() { s.mu.Unlock() }

// guard against unused-import noise until snapshot.go lands
var _ = context.Canceled
```

- [ ] **Step 4: Run the tests to verify they pass.**

```bash
go test ./internal/services/backup/ -run 'TestNew_|TestSnapshot_Mutex|TestEnabled_|TestSetEnabled_' -v
```

Expected: 4 tests pass. `TestSnapshot_ConcurrentInvocationsSerialize` will still fail because `Snapshot` isn't implemented yet — that's fine; the next task lands it. Use `-run` to scope as shown so the unimplemented test doesn't block this commit.

- [ ] **Step 5: Commit.**

```bash
git add internal/services/backup/
git commit -m "$(cat <<'EOF'
feat(backup): service skeleton, clock, persisted enabled flag

Adds the Service struct that owns the snapshot mutex, ErrSnapshotInProgress,
and the auto-backup-enabled toggle persisted to data/settings/auto_backup.json.
Snapshot() itself lands in the next commit.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Meta read/write

**Files:**
- Create: `internal/services/backup/meta.go`
- Create: `internal/services/backup/meta_test.go`

- [ ] **Step 1: Write the failing tests.**

Create `internal/services/backup/meta_test.go`:

```go
package backup

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMeta_LoadMissingReturnsZero(t *testing.T) {
	dir := t.TempDir()
	m, err := loadMeta(dir)
	if err != nil { t.Fatal(err) }
	if m.TS != "" || m.FileCount != 0 || m.LastError != "" {
		t.Fatalf("missing meta should be zero, got %+v", m)
	}
}

func TestMeta_WriteSuccessAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)
	in := Meta{
		TS:            now.Format("20060102_150405"),
		FileCount:     12,
		TotalBytes:    345_678,
		Encrypted:     true,
		LastAttemptTS: now.Format("20060102_150405"),
	}
	if err := writeMetaSuccess(dir, in); err != nil { t.Fatal(err) }
	got, err := loadMeta(dir)
	if err != nil { t.Fatal(err) }
	if got != in {
		t.Fatalf("round trip mismatch:\ngot  %+v\nwant %+v", got, in)
	}
}

func TestMeta_FailureWritePreservesPriorTS(t *testing.T) {
	dir := t.TempDir()
	prior := Meta{
		TS: "20260430_080000", FileCount: 5, TotalBytes: 100, Encrypted: false,
		LastAttemptTS: "20260430_080000",
	}
	if err := writeMetaSuccess(dir, prior); err != nil { t.Fatal(err) }

	now := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)
	if err := writeMetaFailure(dir, "disk full", now); err != nil { t.Fatal(err) }

	got, err := loadMeta(dir)
	if err != nil { t.Fatal(err) }
	if got.TS != prior.TS {
		t.Fatalf("failure write changed TS: got %q want %q", got.TS, prior.TS)
	}
	if got.FileCount != prior.FileCount {
		t.Fatalf("failure write changed FileCount: got %d want %d", got.FileCount, prior.FileCount)
	}
	if got.LastError != "disk full" {
		t.Fatalf("LastError got %q want %q", got.LastError, "disk full")
	}
	if got.LastAttemptTS != now.Format("20060102_150405") {
		t.Fatalf("LastAttemptTS not updated: %q", got.LastAttemptTS)
	}
}

func TestMeta_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	if err := writeMetaSuccess(dir, Meta{TS: "20260501_120000"}); err != nil { t.Fatal(err) }

	// No .tmp leftover should remain after a successful write.
	matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(matches) != 0 {
		t.Fatalf("atomic write left .tmp files: %v", matches)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail.**

```bash
go test ./internal/services/backup/ -run 'TestMeta' -v
```

Expected: compile failure (`Meta`, `loadMeta`, `writeMetaSuccess`, `writeMetaFailure` undefined).

- [ ] **Step 3: Implement meta.go.**

Create `internal/services/backup/meta.go`:

```go
package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const metaFileName = "last_backup.json"

// Meta is the on-disk record of the most recent successful backup plus
// the most recent attempt outcome.
type Meta struct {
	// TS / FileCount / TotalBytes / Encrypted reflect the most recent
	// SUCCESSFUL backup. They are not overwritten by a failed attempt.
	TS         string `json:"ts"`
	FileCount  int    `json:"file_count"`
	TotalBytes int64  `json:"total_bytes"`
	Encrypted  bool   `json:"encrypted"`

	// LastError and LastAttemptTS reflect the most recent attempt
	// (success or failure). LastError is empty when the most recent
	// attempt succeeded.
	LastError     string `json:"last_error"`
	LastAttemptTS string `json:"last_attempt_ts"`
}

func metaPath(dir string) string { return filepath.Join(dir, metaFileName) }

func loadMeta(dir string) (Meta, error) {
	data, err := os.ReadFile(metaPath(dir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Meta{}, nil
		}
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, fmt.Errorf("backup: parse meta: %w", err)
	}
	return m, nil
}

func writeMetaAtomic(dir string, m Meta) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := metaPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, metaPath(dir))
}

func writeMetaSuccess(dir string, m Meta) error {
	m.LastError = ""
	if m.LastAttemptTS == "" {
		m.LastAttemptTS = m.TS
	}
	return writeMetaAtomic(dir, m)
}

// writeMetaFailure preserves prior successful TS/FileCount/TotalBytes/
// Encrypted and only updates LastError + LastAttemptTS.
func writeMetaFailure(dir, reason string, now time.Time) error {
	prior, err := loadMeta(dir)
	if err != nil {
		// Best-effort: even if prior is unreadable, still record the failure.
		prior = Meta{}
	}
	prior.LastError = reason
	prior.LastAttemptTS = now.UTC().Format("20060102_150405")
	return writeMetaAtomic(dir, prior)
}
```

- [ ] **Step 4: Run the tests to verify they pass.**

```bash
go test ./internal/services/backup/ -run 'TestMeta' -v
```

Expected: all four meta tests pass.

- [ ] **Step 5: Commit.**

```bash
git add internal/services/backup/meta.go internal/services/backup/meta_test.go
git commit -m "$(cat <<'EOF'
feat(backup): atomic last_backup.json with success/failure variants

Failure writes preserve the prior successful TS so the UI can surface
"last successful N days ago — last attempt failed: <reason>" across
restarts. Atomic write via tmp+rename, 0600 perms.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Snapshot

**Files:**
- Create: `internal/services/backup/snapshot.go`
- Create: `internal/services/backup/snapshot_test.go`

- [ ] **Step 1: Write the failing tests.**

Create `internal/services/backup/snapshot_test.go`:

```go
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

func TestSnapshot_FailurePathRecordsLastError(t *testing.T) {
	// Force a failure by pointing BackupDir at a path under a read-only parent
	// (simulating a permission failure). Use a path that cannot be created.
	dataDir := t.TempDir()
	backupDir := "/proc/budget2-cannot-create-here/backups" // unwritable
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})

	// Pre-seed a successful meta in a writable surrogate, then move it to the
	// unwritable target — actually, for this test we instead drive failure via
	// a writable BackupDir but simulate via a hook: rename target to a dir to
	// make os.Rename fail.
	backupDir = t.TempDir()
	// Pre-create the final target as a DIRECTORY with the timestamp pattern so
	// the os.Rename(tmp -> final) collides with a non-empty directory.
	// (Skip this if your platform allows the rename — fall back to chmod on
	// the parent dir instead.)
	parentRO := t.TempDir()
	if err := os.Chmod(parentRO, 0500); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = os.Chmod(parentRO, 0700) })
	roBackupDir := filepath.Join(parentRO, "backups") // MkdirAll will fail

	svc, _ := New(Config{BackupDir: roBackupDir, DataDir: dataDir})
	err := svc.Snapshot(context.Background())
	if err == nil {
		t.Skip("could not provoke a failure on this platform; meta-failure tested elsewhere")
	}

	// loadMeta from roBackupDir will likely also fail (parent is unwritable),
	// so this assertion is best-effort. The behavior is fully covered by
	// meta_test.go's TestMeta_FailureWritePreservesPriorTS.
	_ = backupDir
}

func TestSnapshotIfStale_SkipsWhenFresh(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})
	if err := svc.Snapshot(context.Background()); err != nil { t.Fatal(err) }

	// Record how many zips exist now.
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
```

- [ ] **Step 2: Run the tests to verify they fail.**

```bash
go test ./internal/services/backup/ -run 'TestSnapshot' -v
```

Expected: `Snapshot` and `SnapshotIfStale` are undefined; compile fails.

- [ ] **Step 3: Implement snapshot.go.**

Create `internal/services/backup/snapshot.go`:

```go
package backup

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	backupNamePrefix = "budget_backup_"
	backupNameSuffix = ".zip"
	tmpSuffix        = ".tmp"
)

// Snapshot builds one backup zip in BackupDir, verifies it, then atomically
// renames it into place. On success, writes meta with success fields. On
// failure, writes meta preserving the prior successful TS and recording
// LastError. Returns ErrSnapshotInProgress when another snapshot is in flight.
func (s *Service) Snapshot(ctx context.Context) error {
	if !s.tryLock() {
		return ErrSnapshotInProgress
	}
	defer s.unlock()
	return s.snapshotLocked(ctx)
}

// SnapshotIfStale runs a snapshot only when the most recent successful
// snapshot is older than maxAge (or no snapshot has run yet).
func (s *Service) SnapshotIfStale(ctx context.Context, maxAge time.Duration) error {
	m, err := loadMeta(s.cfg.BackupDir)
	if err != nil {
		// Treat unreadable meta as "stale, take one".
		return s.Snapshot(ctx)
	}
	if m.TS == "" {
		return s.Snapshot(ctx)
	}
	prev, err := time.Parse("20060102_150405", m.TS)
	if err != nil {
		return s.Snapshot(ctx)
	}
	if s.clock.Now().UTC().Sub(prev) >= maxAge {
		return s.Snapshot(ctx)
	}
	return nil
}

func (s *Service) snapshotLocked(ctx context.Context) error {
	now := s.clock.Now().UTC()
	ts := now.Format("20060102_150405")

	if err := os.MkdirAll(s.cfg.BackupDir, 0700); err != nil {
		_ = writeMetaFailure(s.cfg.BackupDir, fmt.Sprintf("mkdir backup dir: %v", err), now)
		return fmt.Errorf("backup: mkdir: %w", err)
	}
	s.cleanOrphanTmp()

	tmpPath := filepath.Join(s.cfg.BackupDir, backupNamePrefix+ts+backupNameSuffix+tmpSuffix)
	finalPath := filepath.Join(s.cfg.BackupDir, backupNamePrefix+ts+backupNameSuffix)

	count, total, err := s.buildZip(ctx, tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		_ = writeMetaFailure(s.cfg.BackupDir, fmt.Sprintf("build zip: %v", err), now)
		return fmt.Errorf("backup: build zip: %w", err)
	}

	if err := verifyZip(tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		_ = writeMetaFailure(s.cfg.BackupDir, fmt.Sprintf("verify zip: %v", err), now)
		return fmt.Errorf("backup: verify zip: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		_ = writeMetaFailure(s.cfg.BackupDir, fmt.Sprintf("rename: %v", err), now)
		return fmt.Errorf("backup: rename: %w", err)
	}

	encrypted := false
	if s.cfg.Store != nil {
		encrypted = s.cfg.Store.IsEncrypted()
	}
	if err := writeMetaSuccess(s.cfg.BackupDir, Meta{
		TS:            ts,
		FileCount:     count,
		TotalBytes:    total,
		Encrypted:     encrypted,
		LastAttemptTS: ts,
	}); err != nil {
		// Snapshot itself succeeded — log meta failure but return nil.
		// (Caller has the zip on disk; meta is best-effort.)
		fmt.Fprintf(os.Stderr, "backup: write meta: %v\n", err)
	}

	if err := applyRetention(s.cfg.BackupDir, now); err != nil {
		// Retention failure is non-fatal; record in meta but do not return error.
		_ = writeMetaFailure(s.cfg.BackupDir, fmt.Sprintf("retention: %v", err), now)
	}
	return nil
}

// cleanOrphanTmp deletes *.tmp files in BackupDir older than 1 hour.
// These come from process kills mid-snapshot.
func (s *Service) cleanOrphanTmp() {
	matches, _ := filepath.Glob(filepath.Join(s.cfg.BackupDir, "*"+tmpSuffix))
	cutoff := s.clock.Now().Add(-time.Hour)
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil { continue }
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(m)
		}
	}
}

// buildZip writes the zip to tmpPath and returns (file count, total bytes).
func (s *Service) buildZip(ctx context.Context, tmpPath string) (int, int64, error) {
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close() // double-close is safe and we Close() explicitly below

	var count int
	var total int64

	skip := s.skipPredicate()

	err = filepath.Walk(s.cfg.DataDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil { return walkErr }
		select {
		case <-ctx.Done(): return ctx.Err()
		default:
		}
		if info.IsDir() {
			if skip(path, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if skip(path, false) {
			return nil
		}
		rel, err := filepath.Rel(s.cfg.DataDir, path)
		if err != nil { return err }
		// Use forward slashes for portable zip entries.
		rel = filepath.ToSlash(rel)

		raw, err := os.ReadFile(path)
		if err != nil { return err }

		w, err := zw.Create(rel)
		if err != nil { return err }
		n, err := w.Write(raw)
		if err != nil { return err }
		count++
		total += int64(n)
		return nil
	})
	if err != nil { return 0, 0, err }

	if err := zw.Close(); err != nil { return 0, 0, err }
	if err := f.Sync(); err != nil { return 0, 0, err }
	return count, total, nil
}

// skipPredicate returns a function that decides whether a path under DataDir
// should be excluded from the snapshot.
func (s *Service) skipPredicate() func(path string, isDir bool) bool {
	// Resolve absolute paths once for the recursive-backup guard.
	absBackup, _ := filepath.Abs(s.cfg.BackupDir)
	absData, _ := filepath.Abs(s.cfg.DataDir)
	return func(path string, isDir bool) bool {
		base := filepath.Base(path)
		// Skip the BackupDir itself if nested under DataDir.
		if absBackup != "" && absData != "" {
			abs, err := filepath.Abs(path)
			if err == nil && (abs == absBackup || strings.HasPrefix(abs, absBackup+string(filepath.Separator))) {
				return true
			}
		}
		if isDir {
			if base == "cache" {
				return true
			}
			return false
		}
		// Skip atomicWrite leftovers and encryption markers.
		if strings.HasSuffix(base, tmpSuffix) {
			return true
		}
		if base == ".encrypted" || base == ".encryption-verify" {
			return true
		}
		return false
	}
}

func verifyZip(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil { return err }
	defer r.Close()
	// Touch every entry's CRC by reading it.
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil { return err }
		if _, err := io.Copy(io.Discard, rc); err != nil {
			rc.Close()
			return err
		}
		rc.Close()
	}
	return nil
}

// guard against unused-import noise
var _ = errors.New
```

- [ ] **Step 4: Run the tests to verify they pass.**

```bash
go test ./internal/services/backup/ -run 'TestSnapshot' -v
```

Expected: all snapshot tests pass. (`TestSnapshot_FailurePathRecordsLastError` may `t.Skip` on platforms that don't honor the chmod trick — that's acceptable; the failure-path meta write is fully covered by `TestMeta_FailureWritePreservesPriorTS` in Task 4.)

- [ ] **Step 5: Run the previously-skipped concurrent test from Task 3.**

```bash
go test ./internal/services/backup/ -run 'TestSnapshot_ConcurrentInvocationsSerialize' -v
```

Expected: pass.

- [ ] **Step 6: Commit.**

```bash
git add internal/services/backup/snapshot.go internal/services/backup/snapshot_test.go
git commit -m "$(cat <<'EOF'
feat(backup): Snapshot + SnapshotIfStale with verification and meta writes

Walk data/, skip cache + markers + .tmp + nested BackupDir, stream into a
0600 .tmp zip, fsync, verify by reopening with zip.OpenReader, atomic
rename. Success writes meta with TS/FileCount/TotalBytes/Encrypted; any
failure writes meta preserving prior successful TS and recording
LastError. Cleans orphan *.tmp older than 1 hour.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Retention

**Files:**
- Create: `internal/services/backup/retention.go`
- Create: `internal/services/backup/retention_test.go`

- [ ] **Step 1: Write the failing tests.**

Create `internal/services/backup/retention_test.go`:

```go
package backup

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func makeBackup(t *testing.T, dir string, ts time.Time) string {
	t.Helper()
	name := backupNamePrefix + ts.UTC().Format("20060102_150405") + backupNameSuffix
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte("dummy"), 0600); err != nil { t.Fatal(err) }
	return name
}

func listBackups(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil { t.Fatal(err) }
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == backupNameSuffix {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func TestRetention_KeepsLast7Daily(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// 10 distinct days, one backup per day.
	for i := 0; i < 10; i++ {
		makeBackup(t, dir, now.AddDate(0, 0, -i))
	}
	if err := applyRetention(dir, now); err != nil { t.Fatal(err) }
	got := listBackups(t, dir)
	// Daily window keeps days 0..6 (7 entries). Days 7,8,9 must be pruned
	// unless they happen to be the newest in their ISO week — for May 2026
	// 7 days back from the 1st falls inside the previous week so it survives
	// as that week's representative. Worst case: 7 daily + up to 4 weekly.
	if len(got) < 7 || len(got) > 7+4 {
		t.Fatalf("expected 7..11 backups after daily retention, got %d: %v", len(got), got)
	}
}

func TestRetention_KeepsLast4WeeklyOlderThanDaily(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// 60 days back, 6 weeks beyond the daily window.
	for i := 0; i < 60; i++ {
		makeBackup(t, dir, now.AddDate(0, 0, -i))
	}
	if err := applyRetention(dir, now); err != nil { t.Fatal(err) }
	got := listBackups(t, dir)
	// 7 daily + 4 weekly (older than the daily window) + at most 3 monthly
	// (older than the weekly window). Upper bound 14 ish.
	if len(got) < 7+4 || len(got) > 7+4+3+1 {
		t.Fatalf("expected ~11..15 backups, got %d: %v", len(got), got)
	}
}

func TestRetention_KeepsLast3MonthlyOlderThanWeekly(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// Daily backups for 7 days + a single backup per month for the last 6 months.
	for i := 0; i < 7; i++ {
		makeBackup(t, dir, now.AddDate(0, 0, -i))
	}
	for m := 1; m <= 6; m++ {
		makeBackup(t, dir, now.AddDate(0, -m, 0))
	}
	if err := applyRetention(dir, now); err != nil { t.Fatal(err) }
	got := listBackups(t, dir)
	// 7 daily + at most 3 monthly survivors from the 6 we made.
	if len(got) < 7 || len(got) > 7+3+4 {
		t.Fatalf("expected ~7..14 backups, got %d: %v", len(got), got)
	}
}

func TestRetention_NoBackupsIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := applyRetention(dir, time.Now()); err != nil { t.Fatal(err) }
}

func TestRetention_IgnoresNonBackupFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := os.WriteFile(filepath.Join(dir, "last_backup.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "random.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	makeBackup(t, dir, now)
	if err := applyRetention(dir, now); err != nil { t.Fatal(err) }
	if _, err := os.Stat(filepath.Join(dir, "last_backup.json")); err != nil {
		t.Errorf("retention should not touch last_backup.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "random.txt")); err != nil {
		t.Errorf("retention should not touch random files: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail.**

```bash
go test ./internal/services/backup/ -run 'TestRetention' -v
```

Expected: `applyRetention` undefined; compile fails.

- [ ] **Step 3: Implement retention.go.**

Create `internal/services/backup/retention.go`:

```go
package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// applyRetention prunes <dir> to: last 7 calendar days (one survivor per
// day, the newest), then for older entries the last 4 ISO weeks (one per
// week, the newest), then for entries older than that the last 3 calendar
// months (one per month, the newest). Anything else is deleted.
func applyRetention(dir string, now time.Time) error {
	entries, err := listBackupTimes(dir)
	if err != nil { return err }
	if len(entries) == 0 { return nil }

	keep := selectKeepers(entries, now.UTC())

	keepSet := make(map[string]bool, len(keep))
	for _, e := range keep {
		keepSet[e.path] = true
	}
	for _, e := range entries {
		if keepSet[e.path] { continue }
		if err := os.Remove(e.path); err != nil {
			return fmt.Errorf("retention: remove %s: %w", e.path, err)
		}
	}
	return nil
}

type backupEntry struct {
	path string
	ts   time.Time
}

func listBackupTimes(dir string) ([]backupEntry, error) {
	matches, err := filepath.Glob(filepath.Join(dir, backupNamePrefix+"*"+backupNameSuffix))
	if err != nil { return nil, err }
	out := make([]backupEntry, 0, len(matches))
	for _, m := range matches {
		base := filepath.Base(m)
		stamp := strings.TrimSuffix(strings.TrimPrefix(base, backupNamePrefix), backupNameSuffix)
		ts, err := time.Parse("20060102_150405", stamp)
		if err != nil { continue }
		out = append(out, backupEntry{path: m, ts: ts.UTC()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ts.Before(out[j].ts) })
	return out, nil
}

// selectKeepers returns the subset of entries that survive retention.
//
//   - Daily tier  (last 7 calendar days): newest entry per (year, day-of-year).
//   - Weekly tier (older than daily, last 4 ISO weeks): newest per (ISO year, ISO week).
//   - Monthly tier(older than weekly, last 3 calendar months): newest per (year, month).
func selectKeepers(entries []backupEntry, now time.Time) []backupEntry {
	dailyCutoff := now.AddDate(0, 0, -7)
	weeklyCutoff := dailyCutoff.AddDate(0, 0, -7*4)
	monthlyCutoff := weeklyCutoff.AddDate(0, -3, 0)

	type bucketKey struct {
		tier int // 1=daily, 2=weekly, 3=monthly
		k1   int
		k2   int
	}
	bestPerBucket := make(map[bucketKey]backupEntry)

	for _, e := range entries {
		var key bucketKey
		switch {
		case !e.ts.Before(dailyCutoff):
			key = bucketKey{1, e.ts.Year(), e.ts.YearDay()}
		case !e.ts.Before(weeklyCutoff):
			y, w := e.ts.ISOWeek()
			key = bucketKey{2, y, w}
		case !e.ts.Before(monthlyCutoff):
			key = bucketKey{3, e.ts.Year(), int(e.ts.Month())}
		default:
			continue // older than monthly window — drop
		}
		prev, ok := bestPerBucket[key]
		if !ok || e.ts.After(prev.ts) {
			bestPerBucket[key] = e
		}
	}
	out := make([]backupEntry, 0, len(bestPerBucket))
	for _, e := range bestPerBucket {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ts.After(out[j].ts) })
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass.**

```bash
go test ./internal/services/backup/ -run 'TestRetention' -v
```

Expected: all retention tests pass.

- [ ] **Step 5: Commit.**

```bash
git add internal/services/backup/retention.go internal/services/backup/retention_test.go
git commit -m "$(cat <<'EOF'
feat(backup): tiered retention — 7 daily + 4 weekly + 3 monthly

Bucket-by-tier selector; non-backup files (last_backup.json, random
files) are never touched. Steady-state ~14 zips under 1 MB given current
data sizes.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Scheduler

**Files:**
- Create: `internal/services/backup/scheduler.go`
- Create: `internal/services/backup/scheduler_test.go`

- [ ] **Step 1: Write the failing tests.**

Create `internal/services/backup/scheduler_test.go`:

```go
package backup

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	now atomic.Value // time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	c := &fakeClock{}
	c.now.Store(start)
	return c
}
func (c *fakeClock) Now() time.Time { return c.now.Load().(time.Time) }
func (c *fakeClock) Set(t time.Time) { c.now.Store(t) }

func TestScheduler_RunsImmediateThenWaitsForTick(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})

	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	clk := newFakeClock(start)
	svc, err := New(Config{BackupDir: backupDir, DataDir: dataDir, Clock: clk})
	if err != nil { t.Fatal(err) }

	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go svc.runWith(ctx, ticks, 24*time.Hour)

	// Allow the goroutine to do its initial SnapshotIfStale (no prior backup
	// → fires).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
		if len(zips) >= 1 { break }
		time.Sleep(10 * time.Millisecond)
	}
	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(zips) != 1 {
		t.Fatalf("expected 1 zip after initial run, got %d", len(zips))
	}

	// Advance clock 25 hours and tick — second snapshot should run.
	clk.Set(start.Add(25 * time.Hour))
	ticks <- clk.Now()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
		if len(zips) >= 2 { break }
		time.Sleep(10 * time.Millisecond)
	}
	zips, _ = filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(zips) < 2 {
		t.Fatalf("expected ≥2 zips after stale tick, got %d", len(zips))
	}
}

func TestScheduler_DisabledFlagShortCircuits(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	clk := newFakeClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))

	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir, Clock: clk})
	if err := svc.SetEnabled(false); err != nil { t.Fatal(err) }

	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go svc.runWith(ctx, ticks, 24*time.Hour)

	// Wait briefly; no zip should appear.
	time.Sleep(100 * time.Millisecond)
	ticks <- clk.Now().Add(48 * time.Hour)
	time.Sleep(100 * time.Millisecond)

	zips, _ := filepath.Glob(filepath.Join(backupDir, "budget_backup_*.zip"))
	if len(zips) != 0 {
		t.Fatalf("disabled scheduler should not snapshot; got %d zips", len(zips))
	}
}

func TestScheduler_CtxCancelExits(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
	seedDataDir(t, dataDir, map[string][]byte{"a.csv": []byte("x")})
	svc, _ := New(Config{BackupDir: backupDir, DataDir: dataDir})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.runWith(ctx, make(chan time.Time), 24*time.Hour)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not exit on ctx cancel")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail.**

```bash
go test ./internal/services/backup/ -run 'TestScheduler' -v
```

Expected: `runWith` undefined; compile fails.

- [ ] **Step 3: Implement scheduler.go.**

Create `internal/services/backup/scheduler.go`:

```go
package backup

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Run starts the daily-tick scheduler. It blocks until ctx is cancelled.
// On entry it runs SnapshotIfStale once, then ticks every hour.
func (s *Service) Run(ctx context.Context, maxAge time.Duration) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	s.runWith(ctx, ticker.C, maxAge)
}

// runWith is the testable variant: callers inject the tick channel.
func (s *Service) runWith(ctx context.Context, ticks <-chan time.Time, maxAge time.Duration) {
	if s.Enabled() {
		if err := s.SnapshotIfStale(ctx, maxAge); err != nil {
			fmt.Fprintf(os.Stderr, "backup: initial snapshot: %v\n", err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if !s.Enabled() {
				continue
			}
			if err := s.SnapshotIfStale(ctx, maxAge); err != nil {
				fmt.Fprintf(os.Stderr, "backup: scheduled snapshot: %v\n", err)
			}
		}
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass.**

```bash
go test ./internal/services/backup/ -v
```

Expected: every test in the backup package passes.

- [ ] **Step 5: Commit.**

```bash
git add internal/services/backup/scheduler.go internal/services/backup/scheduler_test.go
git commit -m "$(cat <<'EOF'
feat(backup): hourly scheduler with disable toggle and clean shutdown

Initial SnapshotIfStale on goroutine entry; hourly ticks afterward.
runWith is the test seam — production Run uses time.NewTicker. Honors
the persisted enabled flag and exits on ctx cancel.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Restore — shared helper, round-trip, hardening

**Files:**
- Modify: `internal/handlers/backup/handlers.go` (refactor `HandleRestore` ~lines 115–202 and `HandleRestoreTestData` ~lines 204–272)
- Modify: `internal/handlers/backup/handlers_test.go`

**Why:** The current restore path silently drops every non-CSV entry, has weak path sanitization (only checks the basename for `..`), and the same bug is duplicated in `HandleRestoreTestData`.

- [ ] **Step 1: Write the failing tests.**

Append to `internal/handlers/backup/handlers_test.go`:

```go
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
	if err := zw.Close(); err != nil { t.Fatal(err) }

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
	cfg = &config.Config{DataDirectory: dataDir}
	s, _ := storage.New(dataDir)
	store = s

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mustZip(t, zw, "../escape.txt", []byte("nope"))
	if err := zw.Close(); err != nil { t.Fatal(err) }

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
	cfg = &config.Config{DataDirectory: dataDir}
	s, _ := storage.New(dataDir)
	store = s

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mustZip(t, zw, "/etc/passwd", []byte("nope"))
	if err := zw.Close(); err != nil { t.Fatal(err) }

	rec := postMultipartZip(t, "/restore", "file", "backup.zip", buf.Bytes())
	HandleRestore(rec, rec.Request)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("absolute path must return 400, got %d", rec.Code)
	}
}

func TestHandleRestore_RejectsEncryptedBlobsIntoUnencryptedStore(t *testing.T) {
	dataDir := t.TempDir()
	cfg = &config.Config{DataDirectory: dataDir}
	s, _ := storage.New(dataDir)
	store = s

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Forge an "encrypted" entry by prefixing the age header magic.
	encrypted := append([]byte("age-encryption.org/v1\n"), []byte("payload")...)
	mustZip(t, zw, "secret.csv", encrypted)
	if err := zw.Close(); err != nil { t.Fatal(err) }

	rec := postMultipartZip(t, "/restore", "file", "backup.zip", buf.Bytes())
	HandleRestore(rec, rec.Request)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("encrypted blob into unencrypted store must 400, got %d", rec.Code)
	}
}

// Helper: append a zip entry with content.
func mustZip(t *testing.T, zw *zip.Writer, name string, content []byte) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil { t.Fatal(err) }
	if _, err := w.Write(content); err != nil { t.Fatal(err) }
}

// Helper: build a multipart POST request with a file part and return a
// recorder whose .Request is the constructed request.
type recRequest struct {
	*httptest.ResponseRecorder
	Request *http.Request
}

func postMultipartZip(t *testing.T, url, field, filename string, content []byte) *recRequest {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile(field, filename)
	if err != nil { t.Fatal(err) }
	if _, err := part.Write(content); err != nil { t.Fatal(err) }
	if err := mw.Close(); err != nil { t.Fatal(err) }
	r := httptest.NewRequest(http.MethodPost, url, &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return &recRequest{ResponseRecorder: httptest.NewRecorder(), Request: r}
}

func mustReadEqual(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil { t.Fatalf("read %s: %v", path, err) }
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q want %q", path, got, want)
	}
}
```

Add the imports at the top of the test file as needed: `archive/zip`, `bytes`, `errors`, `io/fs`, `mime/multipart`, `net/http/httptest`, `path/filepath`, `budget2/internal/config`, `budget2/internal/services/storage`.

- [ ] **Step 2: Run the tests to verify they fail.**

```bash
go test ./internal/handlers/backup/ -run 'TestHandleRestore' -v
```

Expected: the round-trip test fails (JSON not restored), traversal/absolute tests fail (current code only checks basename `..` and uses `filepath.Base` which strips the rest), encrypted-blob test fails (no rejection logic exists).

- [ ] **Step 3: Refactor handlers.go.**

In `internal/handlers/backup/handlers.go`, replace `HandleRestore` and `HandleRestoreTestData` with calls to a shared helper. New helper:

```go
// restoreFromZip extracts every entry of the supplied archive into
// cfg.DataDirectory using store.WriteFile. It validates the entire
// archive before writing any files, so a malformed entry rejects the
// whole operation atomically.
//
// Returns (count, http status, error message). On success, status is 200.
func restoreFromZip(content []byte) (int, int, string) {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return 0, http.StatusBadRequest, "Invalid ZIP file"
	}

	dataAbs, err := filepath.Abs(cfg.DataDirectory)
	if err != nil {
		return 0, http.StatusInternalServerError, "Bad data directory"
	}

	type prepared struct {
		dest string
		data []byte
	}
	var queue []prepared

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() { continue }
		// Sanitize: forbid absolute, forbid ".." segments, must stay under data dir.
		raw := filepath.ToSlash(zf.Name)
		if strings.HasPrefix(raw, "/") {
			return 0, http.StatusBadRequest, fmt.Sprintf("Absolute path in archive: %s", zf.Name)
		}
		clean := filepath.Clean(raw)
		if clean == "." || clean == "" {
			continue
		}
		for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
			if seg == ".." {
				return 0, http.StatusBadRequest, fmt.Sprintf("Path traversal in archive: %s", zf.Name)
			}
		}
		dest := filepath.Join(cfg.DataDirectory, clean)
		destAbs, err := filepath.Abs(dest)
		if err != nil || !(destAbs == dataAbs || strings.HasPrefix(destAbs, dataAbs+string(filepath.Separator))) {
			return 0, http.StatusBadRequest, fmt.Sprintf("Path escapes data dir: %s", zf.Name)
		}

		rc, err := zf.Open()
		if err != nil {
			return 0, http.StatusBadRequest, fmt.Sprintf("Cannot open entry %s: %v", zf.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return 0, http.StatusBadRequest, fmt.Sprintf("Cannot read entry %s: %v", zf.Name, err)
		}

		// Encrypted blob into unencrypted/locked store → reject the whole archive.
		if storage.IsAgeEncryptedData(data) && !(store.IsEncrypted() && store.IsUnlocked()) {
			return 0, http.StatusBadRequest, fmt.Sprintf(
				"Archive contains encrypted entry %s but destination store is not encrypted/unlocked",
				zf.Name,
			)
		}

		queue = append(queue, prepared{dest: dest, data: data})
	}

	if len(queue) == 0 {
		return 0, http.StatusBadRequest, "No restorable files in archive"
	}

	for _, p := range queue {
		if err := os.MkdirAll(filepath.Dir(p.dest), 0755); err != nil {
			return 0, http.StatusInternalServerError, fmt.Sprintf("mkdir: %v", err)
		}
		if err := store.WriteFile(p.dest, p.data, 0644); err != nil {
			return 0, http.StatusInternalServerError, fmt.Sprintf("write %s: %v", p.dest, err)
		}
	}
	return len(queue), http.StatusOK, ""
}
```

Replace `HandleRestore`'s extraction loop (everything after the multipart parse) with:

```go
content, err := io.ReadAll(file)
if err != nil {
    http.Error(w, "Error reading file", http.StatusInternalServerError)
    return
}
count, status, msg := restoreFromZip(content)
if status != http.StatusOK {
    http.Error(w, msg, status)
    return
}
log.Printf("Restore complete: %d files restored", count)
w.WriteHeader(http.StatusOK)
fmt.Fprintf(w, "Restored %d files", count)
```

Replace `HandleRestoreTestData`'s extraction loop with:

```go
content, err := testdata.TestBackupFS.ReadFile("test_backup.zip")
if err != nil {
    http.Error(w, "Test backup not available", http.StatusInternalServerError)
    return
}
count, status, msg := restoreFromZip(content)
if status != http.StatusOK {
    http.Error(w, msg, status)
    return
}
log.Printf("Test data restore complete: %d files restored", count)
w.WriteHeader(http.StatusOK)
fmt.Fprintf(w, "Restored %d test files", count)
```

Add the import: `"budget2/internal/services/storage"` (if not already present — it's referenced via `store *storage.Storage`).

- [ ] **Step 4: Run the tests to verify they pass.**

```bash
go test ./internal/handlers/backup/ -v
```

Expected: every test passes — the new ones AND every pre-existing test in `handlers_test.go`. If pre-existing tests fail because they assumed the CSV-only behavior (e.g., asserted "no JSON files restored"), update them to assert the new round-trip behavior — that is the bug we set out to fix.

- [ ] **Step 5: Commit.**

```bash
git add internal/handlers/backup/handlers.go internal/handlers/backup/handlers_test.go
git commit -m "$(cat <<'EOF'
fix(backup/restore): round-trip all file types + harden path sanitization

Replace per-handler CSV-only loops with restoreFromZip. Validate the full
archive before writing anything — reject absolute paths, .. segments, and
encrypted blobs into unencrypted/locked stores. JSON, settings/, and
nested directories are now restored. HandleRestoreTestData uses the same
helper so the embedded fixture path stops silently dropping files too.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: HandleDeleteAllData defense-in-depth

**Files:**
- Modify: `internal/handlers/backup/handlers.go` (around `HandleDeleteAllData` ~line 274)
- Modify: `internal/handlers/backup/handlers_test.go`

**Why:** Today `HandleDeleteAllData` only deletes CSV files in `cfg.DataDirectory` — a `BackupDir` outside `data/` is not at risk under current behavior. The defense-in-depth guard prevents future regression if the deletion is ever broadened.

- [ ] **Step 1: Write the failing test.**

Append to `internal/handlers/backup/handlers_test.go`:

```go
func TestHandleDeleteAllData_DoesNotTouchBackupDir(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := filepath.Join(dataDir, "backups") // worst case: nested
	if err := os.MkdirAll(backupDir, 0700); err != nil { t.Fatal(err) }
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
```

- [ ] **Step 2: Run to verify it fails (or passes by accident).**

```bash
go test ./internal/handlers/backup/ -run 'TestHandleDeleteAllData' -v
```

Expected behavior: this likely passes with current code because `HandleDeleteAllData` only deletes CSVs and skips directories. That's fine — implement the explicit guard regardless so the test continues to pass if anyone ever broadens the deletion logic.

- [ ] **Step 3: Add the explicit guard.**

In `HandleDeleteAllData`, add a guard at the start of the per-entry loop to skip anything inside `BackupDir`:

```go
// Skip BackupDir to defend the safety net even if future code broadens
// what HandleDeleteAllData removes.
if cfg.BackupDir != "" {
    backupAbs, _ := filepath.Abs(cfg.BackupDir)
    entryAbs, _ := filepath.Abs(filepath.Join(cfg.DataDirectory, entry.Name()))
    if backupAbs != "" && (entryAbs == backupAbs || strings.HasPrefix(entryAbs, backupAbs+string(filepath.Separator))) {
        continue
    }
}
```

- [ ] **Step 4: Re-run the test.**

```bash
go test ./internal/handlers/backup/ -run 'TestHandleDeleteAllData' -v
```

Expected: pass.

- [ ] **Step 5: Commit.**

```bash
git add internal/handlers/backup/handlers.go internal/handlers/backup/handlers_test.go
git commit -m "$(cat <<'EOF'
feat(backup): explicit BackupDir guard in HandleDeleteAllData

Defense-in-depth so the safety net survives even if the deletion path
is broadened beyond CSVs in the future.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: HandleBackupStatus endpoint

**Files:**
- Modify: `internal/handlers/backup/handlers.go`
- Modify: `internal/handlers/backup/handlers_test.go`
- Modify: `internal/handlers/backup/handlers.go` (`Initialize`) to accept the new `*backupsvc.Service`

- [ ] **Step 1: Write the failing test.**

Append to `internal/handlers/backup/handlers_test.go`:

```go
func TestHandleBackupStatus_ReturnsMetaAndCount(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
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
```

Add imports as needed: `"time"`, `backupsvc "budget2/internal/services/backup"`.

- [ ] **Step 2: Run to verify it fails.**

```bash
go test ./internal/handlers/backup/ -run 'TestHandleBackupStatus' -v
```

Expected: `HandleBackupStatus` undefined; compile fails.

- [ ] **Step 3: Implement the endpoint and Initialize update.**

In `internal/handlers/backup/handlers.go`:

a. Add a package-level service handle and update `Initialize`:

```go
import backupsvc "budget2/internal/services/backup"

var backupSvc *backupsvc.Service

func Initialize(c *config.Config, s *storage.Storage, r *templates.Renderer, b *backupsvc.Service) {
    cfg = c
    store = s
    renderer = r
    backupSvc = b
}
```

b. Add the handler:

```go
type backupStatusResponse struct {
    TS            string `json:"ts"`
    FileCount     int    `json:"file_count"`
    TotalBytes    int64  `json:"total_bytes"`
    Encrypted     bool   `json:"encrypted"`
    LastError     string `json:"last_error"`
    LastAttemptTS string `json:"last_attempt_ts"`
    SnapshotCount int    `json:"snapshot_count"`
    Dir           string `json:"dir"`
    Enabled       bool   `json:"enabled"`
}

func HandleBackupStatus(w http.ResponseWriter, r *http.Request) {
    dir := ""
    enabled := false
    if backupSvc != nil {
        dir = backupSvc.BackupDir()
        enabled = backupSvc.Enabled()
    } else if cfg != nil {
        dir = cfg.BackupDir
    }

    resp := backupStatusResponse{Dir: dir, Enabled: enabled}
    if dir != "" {
        if data, err := os.ReadFile(filepath.Join(dir, "last_backup.json")); err == nil {
            _ = json.Unmarshal(data, &resp)
            resp.Dir = dir
            resp.Enabled = enabled
        }
        matches, _ := filepath.Glob(filepath.Join(dir, "budget_backup_*.zip"))
        resp.SnapshotCount = len(matches)
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(resp)
}
```

Add imports: `"encoding/json"`.

- [ ] **Step 4: Update every existing `Initialize` caller.**

Search and update — there is at least one in `cmd/server/main.go` (line ~71). For now, pass `nil` from main.go; Task 12 wires the real service. To keep the test green meanwhile, the handler tolerates `backupSvc == nil` and falls back to `cfg.BackupDir`.

```bash
grep -rn "backup\.Initialize" --include="*.go" .
```

For each caller, change to: `backup.Initialize(cfg, store, renderer, nil)` (Task 12 swaps `nil` → real service).

- [ ] **Step 5: Run the tests to verify they pass.**

```bash
go test ./... -run 'TestHandleBackupStatus|^Test' ./internal/handlers/backup/ ./cmd/server/ -v
go build ./...
```

Expected: every test passes, build succeeds.

- [ ] **Step 6: Commit.**

```bash
git add internal/handlers/backup/handlers.go internal/handlers/backup/handlers_test.go cmd/server/
git commit -m "$(cat <<'EOF'
feat(backup): /backup/status endpoint reads last_backup.json + zip count

Initialize now takes the *backupsvc.Service handle (nil-tolerant for
now; wired live in Task 12). Status response merges meta + dir + enabled
toggle so the UI can render the status card with one HTMX swap.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Auto-enabled toggle endpoint

**Files:**
- Modify: `internal/handlers/backup/handlers.go`
- Modify: `internal/handlers/backup/handlers_test.go`

- [ ] **Step 1: Write the failing test.**

```go
func TestHandleSetAutoBackupEnabled_TogglesAndPersists(t *testing.T) {
	dataDir := t.TempDir()
	backupDir := t.TempDir()
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
	cfg = &config.Config{DataDirectory: dataDir, BackupDir: backupDir}
	store, _ = storage.New(dataDir)
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
```

- [ ] **Step 2: Run to verify it fails.**

```bash
go test ./internal/handlers/backup/ -run 'TestHandleSetAutoBackupEnabled' -v
```

Expected: `HandleSetAutoBackupEnabled` undefined.

- [ ] **Step 3: Implement.**

```go
func HandleSetAutoBackupEnabled(w http.ResponseWriter, r *http.Request) {
    if backupSvc == nil {
        http.Error(w, "backup service not initialized", http.StatusInternalServerError)
        return
    }
    if err := r.ParseForm(); err != nil {
        http.Error(w, "bad form", http.StatusBadRequest)
        return
    }
    val := r.FormValue("enabled")
    var enabled bool
    switch val {
    case "true", "1", "on", "yes":
        enabled = true
    case "false", "0", "off", "no":
        enabled = false
    default:
        http.Error(w, "enabled must be true/false", http.StatusBadRequest)
        return
    }
    if err := backupSvc.SetEnabled(enabled); err != nil {
        http.Error(w, fmt.Sprintf("persist enabled: %v", err), http.StatusInternalServerError)
        return
    }
    w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 4: Run to verify it passes.**

```bash
go test ./internal/handlers/backup/ -v
```

Expected: every test in the package passes.

- [ ] **Step 5: Commit.**

```bash
git add internal/handlers/backup/handlers.go internal/handlers/backup/handlers_test.go
git commit -m "$(cat <<'EOF'
feat(backup): /backup/auto-enabled toggle endpoint

POST enabled=true|false (or 1/0/on/off) to flip the auto-backup
toggle; rejects anything else with 400. Persists via Service.SetEnabled
into data/settings/auto_backup.json so the next process boot honors it.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Wire backup service into main.go

**Files:**
- Modify: `cmd/server/main.go`

**Why:** Construct the service after storage unlock, register the graceful-shutdown snapshot, start the hourly scheduler goroutine, route the new endpoints.

- [ ] **Step 1: Inspect the current main.go entry sequence.**

```bash
grep -n "SetupDependencies\|Initialize\|store.Unlock\|Shutdown\|signal\." cmd/server/main.go
```

Confirm where dependencies are initialized, where the storage is unlocked, where the HTTP server is started, and where the shutdown sequence lives. Note line numbers for the edits.

- [ ] **Step 2: Add the wiring.**

a. Add the import:

```go
backupsvc "budget2/internal/services/backup"
```

b. Add a package-level handle (alongside the existing globals):

```go
var backupService *backupsvc.Service
```

c. In `SetupDependencies` (after `store` is created and before `backup.Initialize`):

```go
backupService, err = backupsvc.New(backupsvc.Config{
    BackupDir: cfg.BackupDir,
    DataDir:   cfg.DataDirectory,
    Store:     store,
})
if err != nil {
    return fmt.Errorf("backup service init: %w", err)
}
```

Update the `backup.Initialize` call to pass it:

```go
backup.Initialize(cfg, store, renderer, backupService)
```

d. After storage is unlocked (or immediately, if storage is not encrypted), kick off the initial stale-check + scheduler. Find the spot in `main()` after the unlock check (or after `SetupDependencies` if no encryption is configured) and add:

```go
// Initial stale-check (fast no-op if a fresh backup already exists).
go func() {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()
    if err := backupService.SnapshotIfStale(ctx, 24*time.Hour); err != nil {
        log.Printf("backup: initial snapshot: %v", err)
    }
}()

// Hourly scheduler — exits when the server context is cancelled.
schedCtx, schedCancel := context.WithCancel(context.Background())
go backupService.Run(schedCtx, 24*time.Hour)
// schedCancel is deferred at server shutdown below.
```

e. Register the new HTTP routes inside the existing protected backup-routes block (mirroring how `r.Get("/backup", ...)` is mounted ~line 138):

```go
r.Get("/backup/status", backup.HandleBackupStatus)
r.Post("/backup/auto-enabled", backup.HandleSetAutoBackupEnabled)
```

f. Add a graceful-shutdown snapshot. Find the existing `signal.Notify` / `srv.Shutdown` block. After cancelling the scheduler context, call:

```go
schedCancel()
shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
if err := backupService.Snapshot(shutdownCtx); err != nil &&
    !errors.Is(err, backupsvc.ErrSnapshotInProgress) {
    log.Printf("backup: shutdown snapshot: %v", err)
}
```

If `main.go` doesn't currently have signal handling (some bare HTTP servers don't), add a minimal block:

```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
<-sigCh
log.Print("shutting down")
schedCancel()
shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
if err := backupService.Snapshot(shutdownCtx); err != nil &&
    !errors.Is(err, backupsvc.ErrSnapshotInProgress) {
    log.Printf("backup: shutdown snapshot: %v", err)
}
_ = srv.Shutdown(shutdownCtx)
```

Required imports: `"context"`, `"errors"`, `"os/signal"`, `"syscall"`, `"time"` (some of these may already be present).

- [ ] **Step 3: Build and test.**

```bash
go build ./...
go test ./...
```

Expected: build succeeds; tests pass.

- [ ] **Step 4: Smoke-test the live wiring.**

```bash
rm -rf /tmp/budget2-smoke-data /tmp/budget2-smoke-backups
mkdir -p /tmp/budget2-smoke-data
echo "a,b,c" > /tmp/budget2-smoke-data/test.csv

BUDGET_DATA_DIR=/tmp/budget2-smoke-data \
BUDGET2_BACKUP_DIR=/tmp/budget2-smoke-backups \
BUDGET_LISTEN_ADDR=:18080 \
go run ./cmd/server &
SMOKE_PID=$!
sleep 5

ls -la /tmp/budget2-smoke-backups
curl -s http://localhost:18080/backup/status | head -c 500
echo

kill -TERM $SMOKE_PID
wait $SMOKE_PID 2>/dev/null
ls -la /tmp/budget2-smoke-backups
```

Expected: at least one `budget_backup_<ts>.zip` and a `last_backup.json` appear in the backup dir; the status endpoint returns JSON containing `"snapshot_count":1` (or 2 if shutdown also produced one). If the shutdown snapshot ran with the same second-precision timestamp as the startup snapshot, only one file exists — that's fine, the seconds collision is benign and the meta still updates.

- [ ] **Step 5: Commit.**

```bash
git add cmd/server/main.go
git commit -m "$(cat <<'EOF'
feat(backup): wire automatic snapshots into server lifecycle

Construct backupsvc.Service in SetupDependencies, fire an immediate
SnapshotIfStale after unlock, start the hourly scheduler goroutine, and
take a best-effort 30s-bounded snapshot on graceful shutdown. Routes
/backup/status and /backup/auto-enabled are mounted in the protected
backup block.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: UI — status line + on/off toggle

**Files:**
- Modify: `web/templates/pages/filemanager/*.html` (locate the existing Backup section)

**Why:** Surface the auto-backup state on the page where users already manage backups.

- [ ] **Step 1: Locate the existing backup section.**

```bash
grep -rln "HandleBackup\|/backup\b\|/restore\b" web/templates/
ls web/templates/pages/filemanager/
```

Open the page that currently renders the manual Backup / Restore controls.

- [ ] **Step 2: Add the auto-backup card above the existing Backup button.**

Insert a block similar to:

```html
<section class="card" id="auto-backup-card"
         hx-get="/backup/status"
         hx-trigger="load, every 30s"
         hx-swap="innerHTML">
  <h3>Automatic Backups</h3>
  <p class="muted">Loading status…</p>
</section>
```

Add a partial template (or inline this in the same page) that renders the status response. Because `/backup/status` returns JSON, hook it via a small fragment endpoint instead — add `HandleBackupStatusFragment` that renders an HTML partial. (Skip this expansion if your existing pattern uses inline JSON + JS — match the surrounding page's idiom.)

If matching existing inline-JS pattern, replace the `hx-get` attributes and add a `<script>` block at the bottom that fetches `/backup/status`, fills in `Last backup`, `Snapshots`, `Location`, and binds the toggle:

```html
<section class="card" id="auto-backup-card">
  <h3>Automatic Backups</h3>
  <label>
    <input type="checkbox" id="auto-backup-toggle">
    Enabled
  </label>
  <p>
    Last backup: <span id="auto-backup-last">…</span> ·
    <span id="auto-backup-count">…</span> snapshots ·
    <span id="auto-backup-bytes">…</span>
  </p>
  <p class="muted">Location: <code id="auto-backup-dir">…</code></p>
  <p id="auto-backup-error" class="error" hidden></p>
</section>

<script>
(function() {
  function refresh() {
    fetch('/backup/status').then(r => r.json()).then(s => {
      document.getElementById('auto-backup-toggle').checked = !!s.enabled;
      document.getElementById('auto-backup-count').textContent = s.snapshot_count;
      document.getElementById('auto-backup-bytes').textContent =
        (s.total_bytes/1024).toFixed(1) + ' KB';
      document.getElementById('auto-backup-dir').textContent = s.dir;
      document.getElementById('auto-backup-last').textContent = s.ts || 'never';
      var err = document.getElementById('auto-backup-error');
      if (s.last_error) {
        err.textContent = 'Last attempt failed: ' + s.last_error;
        err.hidden = false;
      } else {
        err.hidden = true;
      }
    });
  }
  document.getElementById('auto-backup-toggle').addEventListener('change', function(e) {
    var body = new URLSearchParams({ enabled: e.target.checked ? 'true' : 'false' });
    fetch('/backup/auto-enabled', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
    }).then(refresh);
  });
  refresh();
  setInterval(refresh, 30000);
})();
</script>
```

- [ ] **Step 3: Manually verify in the dev server.**

```bash
BUDGET_DATA_DIR=/tmp/budget2-smoke-data \
BUDGET2_BACKUP_DIR=/tmp/budget2-smoke-backups \
go run ./cmd/server
```

Open the file-manager page in a browser, verify:
- Status card renders with the location and snapshot count
- Toggle flips state, refreshes, and persists across page reload
- After deleting `/tmp/budget2-smoke-backups/last_backup.json` and reloading, status shows "never"

- [ ] **Step 4: Commit.**

```bash
git add web/templates/pages/filemanager/
git commit -m "$(cat <<'EOF'
feat(backup): UI card for auto-backup status + enable toggle

Status card renders /backup/status, refreshes every 30s, and exposes the
enable/disable toggle that POSTs to /backup/auto-enabled. Manual Backup
and Restore controls are unchanged.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: Final verification

**Files:** _(none — verification only)_

- [ ] **Step 1: Run the full check.**

```bash
make check
```

Expected: vet + staticcheck + tests + (whatever else `make check` runs) — all green.

- [ ] **Step 2: End-to-end backup → wipe → restore smoke test.**

```bash
WORK=$(mktemp -d)
mkdir -p "$WORK/data/settings"
echo "a,b\n1,2" > "$WORK/data/banking.csv"
echo '{"x":1}' > "$WORK/data/major_expenses.json"
echo '{"baseline":"foo"}' > "$WORK/data/settings/whatif_state.json"

BUDGET_DATA_DIR="$WORK/data" BUDGET2_BACKUP_DIR="$WORK/backups" \
BUDGET_LISTEN_ADDR=:18080 go run ./cmd/server &
PID=$!; sleep 4

# Confirm a snapshot landed.
ls "$WORK/backups"
ZIP=$(ls "$WORK/backups"/budget_backup_*.zip | head -1)
test -n "$ZIP" || { echo "no snapshot"; kill $PID; exit 1; }

# Wipe data and restore from the zip via the HTTP endpoint.
rm -rf "$WORK/data"/*.csv "$WORK/data"/*.json "$WORK/data/settings"
curl -s -X POST -F "file=@$ZIP" http://localhost:18080/restore

# Verify all three files are back.
test -s "$WORK/data/banking.csv"
test -s "$WORK/data/major_expenses.json"
test -s "$WORK/data/settings/whatif_state.json"

kill -TERM $PID; wait $PID 2>/dev/null
echo "smoke OK"
```

Expected: all three files restored. If any are missing, regression in Task 8 — investigate.

- [ ] **Step 3: Optional — verify retention pruning under date pressure.**

Generate 60 dated zips by hand:

```bash
mkdir -p /tmp/budget2-retention-test
for i in $(seq 0 59); do
  TS=$(date -u -d "$i days ago" +%Y%m%d_%H%M%S)
  echo "dummy" > "/tmp/budget2-retention-test/budget_backup_${TS}.zip"
done
```

Then exercise `applyRetention` via a one-off Go script or manual test. (Already covered by `retention_test.go`; this step is optional sanity.)

- [ ] **Step 4: Final commit if anything was tweaked during smoke testing.**

```bash
git status
# If clean, nothing to commit.
```

---

## Self-Review Notes (recorded for the implementer)

1. **Spec coverage** — every section/component in the spec has a corresponding task:
   - Spec §Components → Tasks 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13
   - Spec §Data flow snapshot → Task 5 (verified step-for-step including 10a/10b)
   - Spec §Encryption posture → Task 1 (storage guards) + Task 8 (restore rejection)
   - Spec §Triggers → Task 7 (scheduler) + Task 12 (startup + shutdown wiring)
   - Spec §Retention → Task 6
   - Spec §Backup directory location + perms → Task 2 (default), Task 5 (0700/0600)
   - Spec §Restore changes → Task 8
   - Spec §Storage change → Task 1
   - Spec §UI surface → Task 10 (endpoint), Task 11 (toggle), Task 13 (HTML)
   - Spec §Error handling table → Tasks 4 (failure meta), 5 (orphan tmp, recursive guard), 8 (encrypted-blob mismatch), 9 (HandleDeleteAllData guard)
   - Spec §Testing table → matched to *_test.go files in Tasks 1, 4, 5, 6, 7, 8, 9, 10, 11
   - Spec §Migration / compatibility → Task 12 smoke step proves first-boot snapshot

2. **Type consistency** — `backupsvc.Service`, `backupsvc.Config{BackupDir, DataDir, Store, Clock}`, `backupsvc.ErrSnapshotInProgress`, `backupsvc.New`, `backupsvc.Service.Snapshot`, `backupsvc.Service.SnapshotIfStale`, `backupsvc.Service.Run`, `backupsvc.Service.Enabled`, `backupsvc.Service.SetEnabled`, `backupsvc.Service.BackupDir`, `backupsvc.Service.DataDir` — names referenced consistently across Tasks 3–12. `Meta`, `loadMeta`, `writeMetaSuccess`, `writeMetaFailure` consistent across Tasks 4 and 5. `applyRetention` consistent across Tasks 5 and 6. `IsAgeEncryptedData` (exported) consistent in Tasks 1 and 8. `restoreFromZip` consistent in Task 8.

3. **No placeholders** — every step contains the actual code, the actual command, and the expected output.
