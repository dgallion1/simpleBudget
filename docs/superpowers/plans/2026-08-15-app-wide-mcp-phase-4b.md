# App-wide MCP — Phase 4b (restore extraction + confirm-token guard) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the restore logic out of `internal/handlers/backup` into a testable service, and ship `shutdown_server` behind a reusable confirm-token guard.

**Architecture:** `restoreFromZip` and `pruneRestoreExtras` move to a new leaf package `internal/services/restore` that returns typed sentinel errors instead of HTTP status codes; the two handlers become thin and map those sentinels back to the statuses they return today. A second leaf package `internal/services/mcpsvc/confirm` provides a single-use, tool-bound, args-bound, TTL'd token registry, and `admin` gains `shutdown_server` as its first consumer.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk/mcp`, chi, `crypto/rand`, `crypto/subtle`.

**Spec:** `docs/superpowers/specs/2026-08-15-app-wide-mcp-phase-4b-design.md`

## Global Constraints

- `restore_backup` is NOT built in this plan. Neither is `list_backups`. Neither is `set_encryption` (descoped permanently). Do not add them.
- A service package may NEVER import a `handlers` package. That rule is why this extraction exists.
- Every `Deps` field stays nil-able in the established style: a nil dependency fails that one tool call with a named error rather than dropping the tool from the list.
- Tool handlers keep `defer recoverToError("<tool>", &err)` so a panic fails one call instead of the session.
- `go build ./... && go vet ./... && go test ./... && staticcheck ./...` must pass before every commit. The pre-commit hook runs `make check` (this plus `govulncheck`); never bypass it with `--no-verify`.
- NEVER run `go test ./... | grep ...` — the pipe masks the exit code. Run tests bare.
- Error strings stay lowercase and unpunctuated (staticcheck ST1005 is on by default and there is no `staticcheck.conf` in this repo). User-facing capitalized text belongs in the handler, not in a sentinel.
- Every new test must be verified by mutation: disable the branch it claims to guard and confirm the test fails. Red-before/green-after is not sufficient evidence — it has passed vacuous tests in this repo before.

---

### Task 1: The `restore` service

Move the restore logic to a service that returns Go errors. This task creates the package and its tests; the handlers keep working through their existing code and are not touched until Task 2. Both copies coexist for one commit, which is deliberate — it keeps this task independently reviewable and the suite green throughout.

**Files:**
- Create: `internal/services/restore/restore.go`
- Create: `internal/services/restore/restore_test.go`
- Read for reference: `internal/handlers/backup/handlers.go:424-553` (`restoreFromZip`), `:558-566` (`restoreResult`), `:583-647` (`pruneRestoreExtras`), `:36-70` (package globals, `SettingsRewriteGate`, `RewriteGateFunc`)

**Interfaces:**
- Consumes: `backupsvc.SkipPredicate(dataDir, backupDir string) func(path string, isDir bool) bool`, `backupsvc.ErrSnapshotInProgress`, `(*backupsvc.Service).SnapshotAndHold(ctx) (func(), error)`, `storage.IsAgeEncryptedData([]byte) bool`, `(*storage.Storage).WriteFile(path string, data []byte, perm os.FileMode) error`, `(*storage.Storage).Remove(path string) error`, `(*storage.Storage).IsEncrypted() bool`, `(*storage.Storage).IsUnlocked() bool`.
- Produces: `restore.New(Deps) *Service`, `(*Service).FromZip(ctx, content []byte) (Result, error)`, `restore.Deps`, `restore.Result`, `restore.SnapshotHolder`, `restore.SettingsRewriteGate`, `restore.RewriteGateFunc`, and the nine sentinels below. Task 2 consumes all of it.

- [ ] **Step 1: Create the package with its types and sentinels**

Create `internal/services/restore/restore.go`:

```go
// Package restore rewrites the data directory from a backup archive. It is
// the inverse of internal/services/backup's snapshot: that package makes the
// zip, this one puts it back and prunes what the archive does not contain.
//
// It lives outside internal/services/backup deliberately. Restore depends on
// the settings rewrite gate and the prune-extras walk, neither of which the
// snapshot side knows anything about; keeping them apart keeps a deliberately
// small service small. The dependency runs one way -- restore imports backup,
// never the reverse.
package restore

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	backupsvc "budget2/internal/services/backup"
	"budget2/internal/services/storage"
)

// Sentinels. The handler maps these to the HTTP statuses it has always
// returned; a future MCP tool can classify them without parsing prose. Each
// is wrapped with %w at its raise site so the offending entry name survives
// into the message.
var (
	// 400-class: the archive or its contents are the problem.
	ErrInvalidArchive  = errors.New("invalid zip archive")
	ErrUnsafePath      = errors.New("unsafe path in archive")
	ErrUnreadableEntry = errors.New("cannot read archive entry")
	ErrEncryptedEntry  = errors.New("encrypted entry cannot be restored into a store that is not encrypted and unlocked")
	ErrEmptyArchive    = errors.New("no restorable files in archive")

	// 500-class: the server or its wiring is the problem.
	ErrBadDataDir      = errors.New("bad data directory")
	ErrNoBackupService = errors.New("no backup service is configured")
	ErrSnapshotFailed  = errors.New("safety snapshot failed")
	ErrWriteFailed     = errors.New("write failed")
)

// SnapshotHolder takes the safety snapshot and holds the snapshot lock for
// the duration of the restore. *backupsvc.Service satisfies it.
//
// NOT named Snapshotter: internal/services/mcpsvc/snapshot already exports a
// Snapshotter and it is an entirely different thing (sidecar .bak copies, not
// archive locks). Two types with one name in one codebase is how the wrong
// one gets wired.
type SnapshotHolder interface {
	SnapshotAndHold(ctx context.Context) (func(), error)
}

// SettingsRewriteGate serializes an external rewrite of the settings files
// (a restore's write+prune phase) against the settings manager's saves.
// Acquiring it holds the manager's lock -- no in-flight save can interleave
// with a half-restored settings dir -- and the returned end func carries the
// post-rewrite bookkeeping (cache invalidation, active-scenario
// reconciliation) inside the same critical section. Implemented by
// retirement.SettingsManager.
type SettingsRewriteGate interface {
	BeginExternalRewrite() (end func())
}

// RewriteGateFunc adapts a bare acquire function to SettingsRewriteGate.
type RewriteGateFunc func() (end func())

// BeginExternalRewrite implements SettingsRewriteGate.
func (f RewriteGateFunc) BeginExternalRewrite() func() { return f() }

// Deps is everything a restore needs. Gate may be nil (see FromZip); the
// others are required and their absence is reported as an error rather than
// a panic.
type Deps struct {
	DataDir   string
	BackupDir string
	Store     *storage.Storage
	Backups   SnapshotHolder
	Gate      SettingsRewriteGate
}

// Result summarizes a FromZip run: files written, stale files pruned, archive
// entries dropped by the skip list, and prune removals that failed (details
// in the server log).
type Result struct {
	Restored         int
	Pruned           int
	SkippedProtected int
	PruneFailures    int
}

// Service restores archives into one data directory.
type Service struct{ deps Deps }

// New returns a Service bound to deps.
func New(d Deps) *Service { return &Service{deps: d} }
```

- [ ] **Step 2: Port `FromZip`**

Append to `internal/services/restore/restore.go`. This is `restoreFromZip` moved verbatim except for the substitutions below — do not redesign the flow, and do not reorder the defers.

Substitutions to apply while moving:

| In the handler | In the service |
|---|---|
| `cfg.DataDirectory` | `s.deps.DataDir` |
| `resolvedBackupDir()` | `s.deps.BackupDir` |
| `store` | `s.deps.Store` |
| `backupSvc` | `s.deps.Backups` |
| `restoreGate` | `s.deps.Gate` |
| `return res, http.Status..., "msg"` | `return res, fmt.Errorf("...: %w", ErrX)` |

```go
// FromZip restores content over the data directory and prunes files the
// archive does not contain. It is destructive by design: a file present on
// disk but absent from the archive is removed unless the skip predicate
// protects it.
func (s *Service) FromZip(ctx context.Context, content []byte) (Result, error) {
	var res Result
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return res, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}

	dataAbs, err := filepath.Abs(s.deps.DataDir)
	if err != nil {
		return res, fmt.Errorf("%w %q: %v", ErrBadDataDir, s.deps.DataDir, err)
	}
	skip := backupsvc.SkipPredicate(s.deps.DataDir, s.deps.BackupDir)

	type prepared struct {
		dest string
		data []byte
	}
	var queue []prepared
	archiveEntries := make(map[string]struct{})
	skippedEntries := make(map[string]struct{})

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		// Sanitize: forbid absolute, forbid ".." segments, must stay under data dir.
		raw := filepath.ToSlash(zf.Name)
		if strings.HasPrefix(raw, "/") {
			return res, fmt.Errorf("%w: absolute path %s", ErrUnsafePath, zf.Name)
		}
		clean := filepath.Clean(raw)
		if clean == "." || clean == "" {
			continue
		}
		for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
			if seg == ".." {
				return res, fmt.Errorf("%w: path traversal in %s", ErrUnsafePath, zf.Name)
			}
		}
		dest := filepath.Join(s.deps.DataDir, clean)
		destAbs, err := filepath.Abs(dest)
		if err != nil || !(destAbs == dataAbs || strings.HasPrefix(destAbs, dataAbs+string(filepath.Separator))) {
			return res, fmt.Errorf("%w: %s escapes the data directory", ErrUnsafePath, zf.Name)
		}
		// SkipPredicate is ancestor-aware for file paths, so entries under
		// skip-listed directories (e.g. cache/plotly.min.js) are dropped too.
		// Deduped like restored, so duplicate zip entries count once.
		if skip(dest, false) {
			if _, dup := skippedEntries[destAbs]; !dup {
				skippedEntries[destAbs] = struct{}{}
				res.SkippedProtected++
			}
			continue
		}

		rc, err := zf.Open()
		if err != nil {
			return res, fmt.Errorf("%w %s: %v", ErrUnreadableEntry, zf.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return res, fmt.Errorf("%w %s: %v", ErrUnreadableEntry, zf.Name, err)
		}

		// Encrypted blob into unencrypted/locked store -> reject the whole archive.
		if storage.IsAgeEncryptedData(data) && !(s.deps.Store.IsEncrypted() && s.deps.Store.IsUnlocked()) {
			return res, fmt.Errorf("%w: entry %s", ErrEncryptedEntry, zf.Name)
		}

		queue = append(queue, prepared{dest: dest, data: data})
		archiveEntries[destAbs] = struct{}{}
	}

	if len(queue) == 0 {
		return res, ErrEmptyArchive
	}

	if s.deps.Backups == nil {
		return res, ErrNoBackupService
	}
	// Hold the snapshot lock for the whole restore so a scheduled snapshot
	// (or a second restore) cannot capture a half-restored data dir.
	release, err := s.deps.Backups.SnapshotAndHold(ctx)
	if err != nil {
		if errors.Is(err, backupsvc.ErrSnapshotInProgress) {
			// Returned unwrapped-in-kind so the caller's errors.Is sees the
			// backup package's own identity rather than a restore alias.
			return res, fmt.Errorf("%w", backupsvc.ErrSnapshotInProgress)
		}
		return res, fmt.Errorf("%w: %v", ErrSnapshotFailed, err)
	}
	defer release()

	// Serialize the entire write+prune against settings saves. The gate
	// (when wired) holds the SettingsManager's lock until the deferred
	// endRewrite runs at function exit -- i.e. after pruneExtras -- so no
	// save can interleave with a half-restored settings directory, and
	// endRewrite's cache drop + active-scenario reconciliation happen
	// inside the same critical section. Nothing between here and return
	// may call a SettingsManager method (that would deadlock).
	if s.deps.Gate != nil {
		endRewrite := s.deps.Gate.BeginExternalRewrite()
		defer endRewrite()
	} else {
		// A nil gate means the service was wired without a settings manager
		// -- the restore proceeds UNSERIALIZED against settings saves (the
		// race the gate exists to prevent). Loud so a wiring regression is
		// visible instead of silently racy.
		log.Printf("restore: running without a restore gate; concurrent settings saves are not serialized (pass a SettingsRewriteGate in Deps)")
	}

	for _, p := range queue {
		if err := os.MkdirAll(filepath.Dir(p.dest), 0755); err != nil {
			return res, fmt.Errorf("%w: mkdir %s: %v", ErrWriteFailed, filepath.Dir(p.dest), err)
		}
		if err := s.deps.Store.WriteFile(p.dest, p.data, 0644); err != nil {
			return res, fmt.Errorf("%w: write %s: %v", ErrWriteFailed, p.dest, err)
		}
	}

	res.Pruned, res.PruneFailures = s.pruneExtras(dataAbs, archiveEntries, skip)
	if res.PruneFailures > 0 {
		log.Printf("restore prune completed with %d failures", res.PruneFailures)
	}
	// archiveEntries (not queue) so duplicate zip entries count once.
	res.Restored = len(archiveEntries)
	return res, nil
}
```

- [ ] **Step 3: Port `pruneRestoreExtras` as a method**

Append to `internal/services/restore/restore.go`. Move `internal/handlers/backup/handlers.go:583-647` verbatim, changing only the receiver and `store` → `s.deps.Store`:

```go
// pruneExtras removes files under dataAbs that the archive did not contain,
// then removes directories left empty, deepest first. Returns (removed,
// failures); failures are logged with their cause and never abort the
// restore, because a half-pruned tree is still a correctly restored one.
func (s *Service) pruneExtras(dataAbs string, archiveEntries map[string]struct{}, skip func(path string, isDir bool) bool) (int, int) {
	// Body moved verbatim from internal/handlers/backup/handlers.go:583-647.
	// Only substitution: every `store.Remove(...)` becomes
	// `s.deps.Store.Remove(...)`. Keep the deepest-first sort, the
	// ErrNotExist tolerance, the non-empty-dir `continue`, and every log line.
}
```

Copy the real body in — the comment above describes the move, it does not replace it.

- [ ] **Step 4: Write the service tests**

Create `internal/services/restore/restore_test.go`. These are the first tests this logic has ever had that do not go through an HTTP request.

```go
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
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/services/restore/ -v`
Expected: all PASS.

- [ ] **Step 6: Prove the gate-bracket test discriminates**

Temporarily move the `if s.deps.Gate != nil { ... }` block in `FromZip` to AFTER the `for _, p := range queue` write loop. Run `go test ./internal/services/restore/ -run TestFromZipHoldsTheGateAcrossWriteAndPrune`. Expected: FAIL on the "at acquire" assertion. Restore the block to its original position and re-run to confirm PASS. Do not commit the mutation.

- [ ] **Step 7: Commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add internal/services/restore/
git commit -m "feat(restore): extract the restore logic into a service"
```

---

### Task 2: Switch the handlers over and delete the old copy

**Files:**
- Modify: `internal/handlers/backup/handlers.go` — delete `restoreFromZip` (`:424-553`), `restoreResult` (`:558-566`), `pruneRestoreExtras` (`:583-647`); rewrite `HandleRestore` (`:649-683`) and `HandleRestoreTestData` (`:685-700`); alias the gate types; build the service in `Initialize` (`:63-70`)
- Modify: `internal/handlers/backup/handlers_test.go`, `internal/handlers/backup/coverage_gaps_test.go` — only as the compiler and the enumeration in Step 1 require

**Interfaces:**
- Consumes: everything Task 1 produced.
- Produces: no new exported API. `backup.SettingsRewriteGate` and `backup.RewriteGateFunc` become type aliases, so `cmd/server`'s `backup.Initialize(cfg, store, renderer, backupService, retirementMgr)` and every existing test using `backup.RewriteGateFunc(...)` keep compiling unchanged.

- [ ] **Step 1: Enumerate what actually references the moving symbols**

Do NOT work from a list of test names. Run `LSP` `findReferences` on each of `restoreFromZip`, `restoreResult`, `pruneRestoreExtras`, `restoreResponseMessage`, and `restoreGate`, and write down every hit. Hand-written test inventories have been undercounted every time they have been tried in this repo. The enumeration is the input to Steps 5 and 6.

- [ ] **Step 2: Record the coverage baseline**

Run: `go test ./internal/handlers/backup/ -cover`
Write the percentage down. Step 8 compares against it.

- [ ] **Step 3: Alias the gate types and build the service in `Initialize`**

In `internal/handlers/backup/handlers.go`, replace the `SettingsRewriteGate` interface, the `RewriteGateFunc` type and its method (`:36-56`) with aliases, and keep the doc comments pointing at the new home:

```go
// SettingsRewriteGate serializes an external rewrite of the settings files
// against the settings manager's saves. Defined in internal/services/restore,
// aliased here so existing callers and tests keep compiling.
type SettingsRewriteGate = restore.SettingsRewriteGate

// RewriteGateFunc adapts a bare acquire function to SettingsRewriteGate.
type RewriteGateFunc = restore.RewriteGateFunc
```

Replace the `restoreGate` package variable with the service, and build it in `Initialize`:

```go
// restoreSvc performs the actual restore. Built in Initialize so the gate and
// the data/backup directories cannot be registered out of order or forgotten.
var restoreSvc *restore.Service

func Initialize(c *config.Config, s *storage.Storage, r *templates.Renderer, b *backupsvc.Service, gate SettingsRewriteGate) {
	cfg = c
	store = s
	renderer = r
	backupSvc = b
	restoreSvc = restore.New(restore.Deps{
		DataDir:   c.DataDirectory,
		BackupDir: resolvedBackupDir(),
		Store:     s,
		Backups:   b,
		Gate:      gate,
	})
}
```

`resolvedBackupDir()` reads `backupSvc` and `cfg`, so it must be called AFTER those two assignments — the ordering above is load-bearing.

Note: `Initialize` may be called with a nil `*backupsvc.Service` in tests. `restore.Deps.Backups` is an interface, so assigning a nil `*backupsvc.Service` to it yields a non-nil interface holding a nil pointer and `ErrNoBackupService` would never fire — the exact typed-nil trap fixed in phase 4a's `69019f8`. Guard it:

```go
	rd := restore.Deps{
		DataDir:   c.DataDirectory,
		BackupDir: resolvedBackupDir(),
		Store:     s,
		Gate:      gate,
	}
	if b != nil {
		rd.Backups = b
	}
	restoreSvc = restore.New(rd)
```

Use this guarded form, not the first one.

- [ ] **Step 4: Add the error mapping and rewrite both handlers**

Add to `internal/handlers/backup/handlers.go`, next to `restoreResponseMessage`:

```go
// restoreFailure maps a restore service error to the status and message this
// endpoint has always returned. The three static messages are preserved
// verbatim because they are user-facing; the detail-carrying cases render the
// service's own message, which now names the offending entry.
func restoreFailure(err error) (int, string) {
	switch {
	case errors.Is(err, backupsvc.ErrSnapshotInProgress):
		return http.StatusConflict, "a backup is currently running; retry shortly"
	case errors.Is(err, restore.ErrInvalidArchive):
		return http.StatusBadRequest, "Invalid ZIP file"
	case errors.Is(err, restore.ErrEmptyArchive):
		return http.StatusBadRequest, "No restorable files in archive"
	case errors.Is(err, restore.ErrUnsafePath),
		errors.Is(err, restore.ErrUnreadableEntry),
		errors.Is(err, restore.ErrEncryptedEntry):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, restore.ErrBadDataDir):
		return http.StatusInternalServerError, "Bad data directory"
	case errors.Is(err, restore.ErrNoBackupService):
		return http.StatusInternalServerError, "Backup service not initialized"
	default:
		return http.StatusInternalServerError, err.Error()
	}
}
```

Change `restoreResponseMessage`'s parameter type from `restoreResult` to `restore.Result` and its field reads to the exported names (`res.Restored`, `res.Pruned`, `res.SkippedProtected`, `res.PruneFailures`). Its output text does not change.

Rewrite the two call sites. `HandleRestore`'s body up to and including `io.ReadAll` is unchanged; only the tail changes:

```go
	if restoreSvc == nil {
		http.Error(w, "Restore service not initialized", http.StatusInternalServerError)
		return
	}
	res, err := restoreSvc.FromZip(r.Context(), content)
	if err != nil {
		status, msg := restoreFailure(err)
		http.Error(w, msg, status)
		return
	}
	log.Printf("Restore complete: %d files restored, %d stale files removed, %d protected entries skipped, %d prune failures",
		res.Restored, res.Pruned, res.SkippedProtected, res.PruneFailures)
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, restoreResponseMessage(res, "files"))
```

`HandleRestoreTestData` takes the identical treatment, keeping its own log prefix ("Test data restore complete: ...") and its `"test files"` noun.

- [ ] **Step 5: Delete the old implementation**

Delete `restoreFromZip`, `restoreResult` and `pruneRestoreExtras` from `internal/handlers/backup/handlers.go`. Remove any import that is now unused (`archive/zip`, `bytes`, `sort` are likely candidates — let the compiler tell you; do not guess).

- [ ] **Step 6: Move the tests the enumeration found**

Tests that exercise the moved logic directly (archive sanitization, prune behavior, counting) belong in `internal/services/restore`; tests that exercise the HTTP contract (status codes, multipart parsing, response text) stay in `internal/handlers/backup`. Use the Step 1 enumeration to decide each one — a test that only ever called `restoreFromZip` moves; a test that goes through `httptest` stays. When a moved test's assertions were written against HTTP status codes, rewrite them against the sentinels.

- [ ] **Step 7: Run everything**

Run: `go build ./... && go vet ./... && go test ./... && staticcheck ./...`
Expected: all pass. The `handlers/backup` suite must be green without changing what any HTTP test asserts about status codes.

- [ ] **Step 8: Confirm no coverage was lost**

Run: `go test ./internal/handlers/backup/ ./internal/services/restore/ -cover`
Compare against Step 2's baseline. Coverage may move between the two packages; it may not disappear. If the combined figure dropped, find the untested path and add the test before committing.

- [ ] **Step 9: Commit**

```bash
git add internal/handlers/backup/ internal/services/restore/
git commit -m "refactor(backup): call the restore service instead of owning restore"
```

---

### Task 3: The `confirm` token registry

**Files:**
- Create: `internal/services/mcpsvc/confirm/confirm.go`
- Create: `internal/services/mcpsvc/confirm/confirm_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `confirm.NewRegistry(ttl time.Duration) *Registry`, `(*Registry).Mint(tool string, args any) (token string, expiresAt time.Time, err error)`, `(*Registry).Redeem(token, tool string, args any) error`, and `confirm.ErrBadToken`. Task 4 consumes all of it.

- [ ] **Step 1: Write the package**

Create `internal/services/mcpsvc/confirm/confirm.go`:

```go
// Package confirm issues and redeems single-use confirmation tokens for
// destructive MCP tools.
//
// A guarded tool's first call performs nothing, returns a preview, and mints
// a token; a second call must echo that token to proceed. The token is bound
// to the tool name and to a hash of the arguments, is single-use, and expires.
//
// What this buys, stated plainly: deliberateness, not consent. A model can
// mint and redeem a token inside one turn without a human ever seeing the
// preview. It raises the bar from "a stray tool call does the thing" to "the
// model must decide twice with a preview in between". It is NOT a substitute
// for a human clicking a button.
//
// It lives below the tool subpackages rather than in mcpsvc, following the
// precedent internal/services/mcpsvc/snapshot set: mcpsvc imports its
// subpackages, so a shared type declared there and used in a subpackage's
// Deps would be an import cycle.
package confirm

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrBadToken is returned by Redeem for every rejection -- unknown, expired,
// replayed, wrong tool, wrong arguments. One identity on purpose: the caller
// tells the model to start over, and distinguishing "expired" from "forged"
// only helps a caller trying to work around the guard.
var ErrBadToken = errors.New("confirmation token is not valid for this call; call the tool again without a token to get a fresh preview and token")

type entry struct {
	tool     string
	argsHash string
	expires  time.Time
}

// Registry holds outstanding tokens in memory. A restart drops every token,
// which is a legitimate way to invalidate all of them.
type Registry struct {
	mu  sync.Mutex
	m   map[string]entry
	ttl time.Duration
	now func() time.Time // injectable so expiry tests need not sleep
}

// NewRegistry returns a Registry whose tokens live for ttl.
func NewRegistry(ttl time.Duration) *Registry {
	return &Registry{m: make(map[string]entry), ttl: ttl, now: time.Now}
}

// hashArgs renders args as canonical JSON and hashes it. Marshaling a struct
// is field-order deterministic, and a map's keys are sorted by encoding/json,
// so the same arguments always produce the same hash.
func hashArgs(args any) (string, error) {
	b, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("cannot hash confirmation arguments: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Mint issues a token bound to tool and args. The returned expiry is what the
// tool reports to the model so it knows how long it has.
func (r *Registry) Mint(tool string, args any) (string, time.Time, error) {
	h, err := hashArgs(args)
	if err != nil {
		return "", time.Time{}, err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, fmt.Errorf("cannot generate a confirmation token: %w", err)
	}
	token := hex.EncodeToString(buf)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()
	expires := r.now().Add(r.ttl)
	r.m[token] = entry{tool: tool, argsHash: h, expires: expires}
	return token, expires, nil
}

// Redeem consumes token for tool/args. A successful redeem deletes the token,
// so a replay is refused. Every failure returns ErrBadToken.
func (r *Registry) Redeem(token, tool string, args any) error {
	h, err := hashArgs(args)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()

	// Find by constant-time compare rather than a map lookup on attacker-
	// supplied input. The threat model here is thin, but a token comparison
	// that is not constant-time is a finding waiting to be filed.
	var found string
	var e entry
	for k, v := range r.m {
		if subtle.ConstantTimeCompare([]byte(k), []byte(token)) == 1 {
			found, e = k, v
			break
		}
	}
	if found == "" {
		return ErrBadToken
	}
	// Consume before validating the rest: a token presented for the wrong
	// tool or arguments is spent, not retryable.
	delete(r.m, found)

	if e.tool != tool || e.argsHash != h || !r.now().Before(e.expires) {
		return ErrBadToken
	}
	return nil
}

// sweepLocked drops expired entries. Callers hold r.mu.
func (r *Registry) sweepLocked() {
	now := r.now()
	for k, v := range r.m {
		if !now.Before(v.expires) {
			delete(r.m, k)
		}
	}
}
```

- [ ] **Step 2: Write the tests**

Create `internal/services/mcpsvc/confirm/confirm_test.go`:

```go
package confirm

import (
	"errors"
	"testing"
	"time"
)

type args struct {
	Name string `json:"name"`
}

func TestRedeemAcceptsAFreshToken(t *testing.T) {
	r := NewRegistry(time.Minute)
	tok, expires, err := r.Mint("shutdown_server", args{})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if tok == "" {
		t.Fatal("Mint returned an empty token")
	}
	if !expires.After(time.Now()) {
		t.Errorf("expiry %v is not in the future", expires)
	}
	if err := r.Redeem(tok, "shutdown_server", args{}); err != nil {
		t.Fatalf("Redeem of a fresh token: %v", err)
	}
}

// The whole point of single-use: a token that worked once must not work
// again, or a guarded operation can be repeated from one confirmation.
func TestRedeemRefusesAReplay(t *testing.T) {
	r := NewRegistry(time.Minute)
	tok, _, _ := r.Mint("shutdown_server", args{})
	if err := r.Redeem(tok, "shutdown_server", args{}); err != nil {
		t.Fatalf("first Redeem: %v", err)
	}
	if err := r.Redeem(tok, "shutdown_server", args{}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("replayed Redeem returned %v, want ErrBadToken", err)
	}
}

// Without tool binding, a token minted for a harmless preview could be spent
// on a different, destructive tool.
func TestRedeemRefusesAnotherTool(t *testing.T) {
	r := NewRegistry(time.Minute)
	tok, _, _ := r.Mint("shutdown_server", args{})
	if err := r.Redeem(tok, "restore_backup", args{}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("cross-tool Redeem returned %v, want ErrBadToken", err)
	}
}

// Without args binding, a preview of one operation could confirm a different
// one -- the model previews restoring archive A and then restores archive B.
func TestRedeemRefusesDifferentArguments(t *testing.T) {
	r := NewRegistry(time.Minute)
	tok, _, _ := r.Mint("restore_backup", args{Name: "a.zip"})
	if err := r.Redeem(tok, "restore_backup", args{Name: "b.zip"}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("mismatched-args Redeem returned %v, want ErrBadToken", err)
	}
}

func TestRedeemRefusesAnExpiredToken(t *testing.T) {
	r := NewRegistry(time.Minute)
	base := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return base }
	tok, _, _ := r.Mint("shutdown_server", args{})

	r.now = func() time.Time { return base.Add(time.Minute + time.Second) }
	if err := r.Redeem(tok, "shutdown_server", args{}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("expired Redeem returned %v, want ErrBadToken", err)
	}
}

func TestRedeemRefusesAnUnknownToken(t *testing.T) {
	r := NewRegistry(time.Minute)
	if err := r.Redeem("deadbeef", "shutdown_server", args{}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("unknown Redeem returned %v, want ErrBadToken", err)
	}
}

// A token presented for the wrong tool must be SPENT, not merely refused --
// otherwise a caller can probe tool names until one sticks.
func TestAWrongToolRedeemSpendsTheToken(t *testing.T) {
	r := NewRegistry(time.Minute)
	tok, _, _ := r.Mint("shutdown_server", args{})
	if err := r.Redeem(tok, "restore_backup", args{}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("cross-tool Redeem returned %v, want ErrBadToken", err)
	}
	if err := r.Redeem(tok, "shutdown_server", args{}); !errors.Is(err, ErrBadToken) {
		t.Fatalf("Redeem after a wrong-tool attempt returned %v, want ErrBadToken (the token should have been consumed)", err)
	}
}

func TestMintReturnsDistinctTokens(t *testing.T) {
	r := NewRegistry(time.Minute)
	a, _, _ := r.Mint("shutdown_server", args{})
	b, _, _ := r.Mint("shutdown_server", args{})
	if a == b {
		t.Fatal("two Mint calls returned the same token")
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `go test ./internal/services/mcpsvc/confirm/ -v`
Expected: all PASS.

- [ ] **Step 4: Prove single-use discriminates**

Comment out the `delete(r.m, found)` line in `Redeem`. Run `go test ./internal/services/mcpsvc/confirm/`. Expected: `TestRedeemRefusesAReplay` and `TestAWrongToolRedeemSpendsTheToken` both FAIL. Restore the line and re-run to confirm PASS. Do not commit the mutation.

- [ ] **Step 5: Commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add internal/services/mcpsvc/confirm/
git commit -m "feat(mcp): add the confirm-token registry for guarded tools"
```

---

### Task 4: `shutdown_server`

**Files:**
- Create: `internal/services/mcpsvc/admin/shutdown.go`
- Create: `internal/services/mcpsvc/admin/shutdown_test.go`
- Modify: `internal/services/mcpsvc/admin/register.go` — add two `Deps` fields and one `register` call
- Modify: `internal/services/mcpsvc/server.go` — add two `Deps` fields and wire them
- Modify: `cmd/server/main.go:111-119` — supply the real shutdown func and a registry
- Modify: `internal/services/mcpsvc/server_test.go` — the tool-count test

**Interfaces:**
- Consumes: `confirm.NewRegistry`, `(*Registry).Mint`, `(*Registry).Redeem`, `confirm.ErrBadToken` from Task 3.
- Produces: the `shutdown_server` tool. Task 5 documents it.

- [ ] **Step 1: Add the Deps fields**

In `internal/services/mcpsvc/admin/register.go`, add to `Deps`:

```go
	// Confirm mints and redeems the two-step tokens guarded tools require. A
	// nil registry makes those tools refuse rather than run unguarded.
	Confirm *confirm.Registry

	// Shutdown stops the server. It is a func, never a direct os.Exit call,
	// because a test that invokes the real thing kills the test binary. Nil
	// means this server has no shutdown path wired.
	Shutdown func()
```

Add `"budget2/internal/services/mcpsvc/confirm"` to the imports, and add `registerShutdown(s, deps)` to the end of `Register`.

- [ ] **Step 2: Write the tool**

Create `internal/services/mcpsvc/admin/shutdown.go`:

```go
package admin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// shutdownExitDelay is how long the tool waits before stopping the process,
// so the JSON-RPC response is written first. A handler that exits inline
// never delivers its result. Mirrors handlers/backup's /killme, which sleeps
// 100ms for the same reason.
const shutdownExitDelay = 250 * time.Millisecond

type shutdownInput struct {
	ConfirmToken string `json:"confirm_token,omitempty" jsonschema:"the token returned by a previous call; omit it to get the preview and a fresh token"`
}

type shutdownOutput struct {
	Confirmed        bool   `json:"confirmed"`
	ConfirmToken     string `json:"confirm_token,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	WhatWouldHappen  string `json:"what_would_happen,omitempty"`
	Note             string `json:"note,omitempty"`
}

const shutdownConsequences = "the budget2 server process stops; every MCP tool in this session stops answering, " +
	"any open browser tab stops working, and NOTHING in this session can start it again -- the user must " +
	"restart the server themselves, and then restart this session, because the tools are only registered if " +
	"the server was already running when the session began"

func registerShutdown(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "shutdown_server",
		Description: "Stop the budget2 server. THIS IS NOT RECOVERABLE FROM INSIDE THIS SESSION: after it runs, " +
			"every tool here stops working and nothing in this session can bring the server back -- the user " +
			"must restart it themselves and then start a new session, because these tools are only registered " +
			"if the server was already running when the session began. Do not call this to 'restart' anything; " +
			"there is no restart. Two steps: call it with no arguments to get a description of what would happen " +
			"plus a confirm_token, show that to the user, and only call again with the token if they say yes. " +
			"The token is single-use, bound to this tool, and expires; a wrong or reused one is refused and you " +
			"must start over. Confirming twice yourself is not the user agreeing -- the second call is for after " +
			"they have actually answered.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in shutdownInput) (res *mcp.CallToolResult, out shutdownOutput, err error) {
		defer recoverToError("shutdown_server", &err)

		if deps.Shutdown == nil {
			return nil, shutdownOutput{}, fmt.Errorf("no shutdown path is configured on this server")
		}
		if deps.Confirm == nil {
			return nil, shutdownOutput{}, fmt.Errorf("no confirmation registry is configured on this server, so this guarded tool cannot run")
		}

		token := strings.TrimSpace(in.ConfirmToken)
		if token == "" {
			fresh, expires, mintErr := deps.Confirm.Mint("shutdown_server", shutdownInput{})
			if mintErr != nil {
				return nil, shutdownOutput{}, mintErr
			}
			return nil, shutdownOutput{
				Confirmed:       false,
				ConfirmToken:    fresh,
				ExpiresAt:       expires.UTC().Format(time.RFC3339),
				WhatWouldHappen: shutdownConsequences,
				Note:            "nothing has been shut down; show the user what_would_happen and call again with confirm_token ONLY if they agree",
			}, nil
		}

		// The minted args are the zero input, not `in` -- `in` carries the
		// token itself, so hashing it would never match what Mint recorded.
		if err := deps.Confirm.Redeem(token, "shutdown_server", shutdownInput{}); err != nil {
			return nil, shutdownOutput{}, err
		}

		// Return first, exit after. Reversing these loses the response.
		shutdown := deps.Shutdown
		time.AfterFunc(shutdownExitDelay, shutdown)

		return nil, shutdownOutput{
			Confirmed: true,
			Note:      "the server is shutting down; this is the last answer any tool in this session will give",
		}, nil
	})
}
```

- [ ] **Step 3: Write the tests**

Create `internal/services/mcpsvc/admin/shutdown_test.go`:

```go
package admin

import (
	"sync/atomic"
	"testing"
	"time"

	"budget2/internal/services/mcpsvc/confirm"
)

// shutdownDeps returns Deps wired with a RECORDING shutdown func. Never wire
// the real os.Exit here: it would kill the test binary.
func shutdownDeps(t *testing.T) (Deps, *atomic.Int32) {
	t.Helper()
	deps, _ := newLiveDeps(t)
	var calls atomic.Int32
	deps.Shutdown = func() { calls.Add(1) }
	deps.Confirm = confirm.NewRegistry(time.Minute)
	return deps, &calls
}

// The first call is the whole guard: if it shuts down, the two-step protocol
// is decorative.
func TestShutdownFirstCallDoesNotShutDown(t *testing.T) {
	deps, calls := shutdownDeps(t)
	cs := connect(t, deps)

	out := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{}))
	if out.Confirmed {
		t.Error("confirmed = true on the first call")
	}
	if out.ConfirmToken == "" {
		t.Error("no confirm_token returned, so the operation can never be confirmed")
	}
	if out.WhatWouldHappen == "" {
		t.Error("no what_would_happen returned; the user has nothing to agree to")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("shutdown func called %d times on the preview call, want 0", got)
	}
}

func TestShutdownSecondCallWithTheTokenShutsDown(t *testing.T) {
	deps, calls := shutdownDeps(t)
	cs := connect(t, deps)

	first := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{}))
	second := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{
		"confirm_token": first.ConfirmToken,
	}))
	if !second.Confirmed {
		t.Error("confirmed = false after redeeming a valid token")
	}
	// The exit is deferred so the response lands first; wait for it.
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("shutdown func called %d times after confirmation, want 1", got)
	}
}

func TestShutdownRefusesABadToken(t *testing.T) {
	deps, calls := shutdownDeps(t)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "shutdown_server", map[string]any{"confirm_token": "not-a-real-token"}))
	if msg == "" {
		t.Error("no error message explaining the refusal")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("shutdown func called %d times with a bad token, want 0", got)
	}
}

func TestShutdownRefusesAReplayedToken(t *testing.T) {
	deps, calls := shutdownDeps(t)
	cs := connect(t, deps)

	first := decodeToolResult[shutdownOutput](t, call(t, cs, "shutdown_server", map[string]any{}))
	call(t, cs, "shutdown_server", map[string]any{"confirm_token": first.ConfirmToken})
	res := call(t, cs, "shutdown_server", map[string]any{"confirm_token": first.ConfirmToken})
	if !res.IsError {
		t.Fatal("a replayed token was accepted")
	}
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("shutdown func called %d times, want exactly 1 (the replay must not shut down a second time)", got)
	}
}

// A nil Shutdown must fail this one call with a named error, matching how
// every other admin dependency behaves. It must NOT panic.
func TestShutdownWithoutAShutdownFuncReportsIt(t *testing.T) {
	deps, _ := newLiveDeps(t)
	deps.Confirm = confirm.NewRegistry(time.Minute)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "shutdown_server", map[string]any{}))
	if msg == "" {
		t.Error("no error message naming the missing shutdown path")
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/services/mcpsvc/admin/ -run TestShutdown -v`
Expected: all PASS.

- [ ] **Step 5: Prove the guard discriminates**

In `shutdown.go`, temporarily move the `time.AfterFunc(shutdownExitDelay, shutdown)` line ABOVE the `if token == ""` block. Run `go test ./internal/services/mcpsvc/admin/ -run TestShutdownFirstCallDoesNotShutDown`. Expected: FAIL with "shutdown func called 1 times on the preview call". Revert and re-run to confirm PASS. Do not commit the mutation.

- [ ] **Step 6: Wire it through `mcpsvc` and `cmd/server`**

In `internal/services/mcpsvc/server.go`, add to `Deps`:

```go
	// Shutdown stops the server process. Nil disables shutdown_server's
	// ability to act (the tool still registers and still reports why).
	Shutdown func()
```

In `NewServer`, extend the `adminDeps` literal built inside the `if deps.Loader != nil` block:

```go
		adminDeps.Confirm = confirm.NewRegistry(5 * time.Minute)
		if deps.Shutdown != nil {
			adminDeps.Shutdown = deps.Shutdown
		}
```

The registry is constructed per server, so tokens never outlive the process. Add the `confirm` and `time` imports.

Update the `NewServer` doc comment: a nil `Shutdown` is a supported configuration in which `shutdown_server` registers and reports "no shutdown path is configured" — the same shape as the nil `Backups` paragraph already there.

In `cmd/server/main.go`, add to the `mcpsvc.Deps` literal at `:111-119`:

```go
		Shutdown:    func() { os.Exit(0) },
```

`cmd/server/main.go` already imports `os` (line 17), so no import change is needed there.

Note that `admin.Register` is called only inside `NewServer`'s `if deps.Loader != nil` block, so a server built without a loader has no `shutdown_server` either. That is pre-existing behavior for the whole admin package and is not changed here.

- [ ] **Step 7: Update the tool-count test**

In `internal/services/mcpsvc/server_test.go`, rename `TestNewServerRegistersAllTwentyThreeTools` to `TestNewServerRegistersAllTwentyFourTools`, add `"shutdown_server"` to the expected-name list, and change both `23`s to `24`.

- [ ] **Step 8: Run everything and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add internal/services/mcpsvc/ cmd/server/main.go
git commit -m "feat(mcp): add shutdown_server behind the confirm-token guard"
```

---

### Task 5: Tell the model, and the reader, that it exists

**Files:**
- Modify: `internal/services/mcpsvc/server.go` — `serverInstructions`
- Modify: `internal/services/mcpsvc/server_test.go` — `TestServerInstructionsCarryLoadBearingClaims`
- Modify: `README.md` — the MCP tool list, if one is present (check first; do not invent a section)

**Interfaces:**
- Consumes: the `shutdown_server` tool from Task 4.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Extend `serverInstructions`**

Append to the housekeeping sentence in `serverInstructions`, keeping the existing wording intact:

```go
	" One tool is guarded: shutdown_server stops the server, and after it runs nothing in this session can " +
	"undo that -- every tool stops answering and only the user can start the server again. It takes two " +
	"calls: the first returns what would happen plus a single-use confirm_token, the second must echo that " +
	"token. Calling it twice yourself is NOT the user agreeing; show them the first call's answer and wait."
```

The "six HOUSEKEEPING tools" phrase earlier in the same text is now wrong — the admin package registers seven. Change it to "seven HOUSEKEEPING tools" and update the pinned substring in the test accordingly.

- [ ] **Step 2: Pin the new claims**

Add to the `want` list in `TestServerInstructionsCarryLoadBearingClaims`:

```go
		"seven HOUSEKEEPING tools",
		"shutdown_server stops the server",
		"Calling it twice yourself is NOT the user agreeing",
```

and remove the now-stale `"six HOUSEKEEPING tools"` entry.

- [ ] **Step 3: Check the README**

Run: `grep -n "get_status\|list_duplicates\|MCP" README.md`
If a tool list exists, add `shutdown_server` to it with a one-line description that says it is guarded and unrecoverable. If no such list exists, skip this step — do not create one.

- [ ] **Step 4: Run everything and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add internal/services/mcpsvc/ README.md
git commit -m "docs(mcp): describe the guarded shutdown tool to the model and the reader"
```

---

## Self-review notes

Checked against the spec while writing:

- **Spec coverage.** The extraction (spec "The extraction") is Tasks 1–2, including the two named invariants — the gate/snapshot bracket is asserted by `TestFromZipHoldsTheGateAcrossWriteAndPrune`, and the nil-gate loud-proceed by `TestFromZipWithoutAGateStillRestores`. The sentinel table maps one-to-one onto Task 1 Step 1 and Task 2 Step 4. The guard (spec "The confirm-token guard") is Task 3; all five properties it lists get a named test. `shutdown_server` (spec section of the same name) is Task 4, including the return-before-exit ordering and the injected `Shutdown`. Everything in "Out of scope" stays unbuilt.
- **The typed-nil trap is guarded twice.** Task 2 Step 3 assigns `restore.Deps.Backups` only when the concrete pointer is non-nil, and Task 4 Step 6 does the same for `adminDeps.Shutdown`. Both are the shape of the bug phase 4a shipped and then fixed in `69019f8`; a service Deps field that is an interface must never receive a possibly-nil concrete pointer unconditionally.
- **Type consistency.** `restore.Result`'s exported field names are used identically in Task 1 (`res.Restored`), Task 2's `restoreResponseMessage` and Task 2's log lines. `SnapshotHolder` is named that everywhere and never `Snapshotter`. `confirm.Registry`'s three methods are called with the same signatures in Tasks 3 and 4. `shutdownInput{}` — the ZERO value, not `in` — is what both `Mint` and `Redeem` hash; passing `in` would make every redeem fail, and that is the single easiest mistake to make in Task 4.
- **One thing the implementer must confirm rather than trust.** Task 2 Step 1's `findReferences` enumeration is the only input to the test move. Do not substitute a grep for a test-name list, and do not trust this plan to know which tests exist — it deliberately names none, because every hand-written test inventory in this repo has been undercounted.
