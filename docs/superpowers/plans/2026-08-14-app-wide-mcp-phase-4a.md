# App-wide MCP — Phase 4a (serialization + housekeeping) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `*dataloader.DataLoader` safe for concurrent use, then give the MCP server six read-mostly housekeeping tools — `get_status`, `list_data_files`, `list_duplicates`, `resolve_duplicates`, `undo_resolve`, `run_backup` — so a model can see what state the app is in and clear the duplicate queue without the browser.

**Architecture:** Tasks 1–3 add two mutexes to `DataLoader`: `stateMu` guards the derived fields `LoadData` stamps and later callers read; `writeMu` makes each load→modify→save sequence over the JSON sidecar files one critical section. No public signature changes — each public method takes the lock and calls new unexported `*Locked` helpers, which never take it. Tasks 4–9 add `internal/services/mcpsvc/admin`, a fifth sibling under `mcpsvc` alongside `plan`, `spend` and `curate`, calling the same `*dataloader.DataLoader`, `*storage.Storage`, `*retirement.SettingsManager` and `*backup.Service` instances the HTTP handlers use.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk` v1.7.0, chi v5.

**Spec:** `docs/superpowers/specs/2026-08-12-app-wide-mcp-design.md` (Phase 4, and the "Serializing the data writes" section)
**Builds on:** `docs/superpowers/plans/2026-08-14-app-wide-mcp-phase-3.md`
**Branch:** `feat/mcp-admin` off `master`.

## Scope: this is Phase 4a, not all of Phase 4

The spec's Phase 4 listed nine `admin` tools. This plan implements six. The other three are deferred to a Phase 4b plan for reasons found while writing this one:

- **`restore_backup` and `set_encryption` have no service to call.** `restoreFromZip` (~225 lines) and every encryption enable/disable/change-method path live *unexported inside `internal/handlers/backup`* (`internal/handlers/backup/handlers.go:424`, `:743-810`, `:999-1190`). The spec forbids a service package importing any `handlers` package, so both tools need a Phase-2-style extraction first. That is its own body of work and does not belong in the same branch as six tools that call existing services.
- **`set_encryption` is additionally descoped by decision, not just sequencing.** Enabling encryption requires a password, SSH passphrase or age identity, which would travel through a tool argument into a model's context and transcript. The user's decision: encryption state is **reported read-only** by `get_status`, and changing it stays a human-at-the-browser operation. Phase 4b should treat "enable encryption over MCP" as out of scope permanently, not deferred.
- **`shutdown_server`** is deferred with the other guarded operations because it needs the same single-use confirm-token infrastructure, which is Phase 4b's first task.

The confirm-token machinery described under the spec's "Guarded operations" is therefore **not** built in this plan. Nothing in Tasks 4–9 is guarded that way: `resolve_duplicates` and `undo_resolve` write, but they write one small JSON file, snapshot it first, and each reverses the other.

## Global Constraints

Carried forward from the spec's "Constraints learned in phases 1 and 2" plus the Phase 3 constraints that still bind. Every task's requirements implicitly include this section.

- Go 1.26. **No new module dependencies.**
- Every tool handler's first line is `defer recoverToError("<tool_name>", &err)`. The go-sdk dispatches each tool call on its own goroutine with no recover of its own, and `middleware.Recoverer` runs on the HTTP request goroutine — a missing defer takes down the user's web server, not one call.
- Tool `Description` strings are the consuming model's only documentation. Write them to be read by a model with no other context: say what the numbers mean, what they exclude, and — for the write tools — exactly what changes on disk.
- **Read the seventeen existing tool descriptions before writing a new one** (`internal/services/mcpsvc/plan/register.go`, `internal/services/mcpsvc/spend/*.go`, `internal/services/mcpsvc/curate/*.go`). They agree with each other on merchant identity, duplicate handling, window semantics and signs. An eighteenth that quietly disagrees is worse than one that is merely vague.
- **Per-transaction `amount` fields are SIGNED, exactly as stored** — a purchase is negative. This is what `search_transactions`, `get_anomalies` and the curate tools do. Only `list_duplicates` in this phase reports per-transaction money, and it follows this rule.
- **Validate every caller-supplied key against live state before writing.** This is the direct lesson from Phase 3's carried-forward list: `upsert_major_expense`'s `pin_hash` writes a dead key because it never checked the hash was pinnable. `resolve_duplicates` and `undo_resolve` take a `pair_key` from the model; a hallucinated or stale key must come back as a tool error naming what *is* available, never as a silently-written decision.
- **Snapshot before writing, and tolerate only `fs.ErrNotExist`.** Copy the exact pattern from `internal/services/mcpsvc/curate/delete.go:104-131`: `Snapshots.Ensure` before the write; `errors.Is(err, fs.ErrNotExist)` is skipped with a note (a fresh install has nothing to lose); any other `Ensure` error aborts the call before anything is written.
- **A test must discriminate, not merely pass.** Red-before/green-after does not prove a test guards its claim. For every test in Tasks 1–3, the task's report must state the mutation that was applied to the production code and confirm the test failed under it. A concurrency test that passes with the mutex *removed* is worthless.
- **Never enumerate the tests to change by name.** Task 1 renames an exported field. Run `LSP findReferences` on it, take the union of files containing a reference, and report that list before editing anything.
- The dependency direction holds: `mcpsvc` imports `plan`, `spend`, `curate`, `admin` and `snapshot`; none of them import `mcpsvc`, and none may import any `handlers` package.
- Per this repo's `CLAUDE.md`: before editing a function/method/type, check callers with the `LSP` tool (`incomingCalls` / `findReferences`). Never rename a symbol with find-and-replace.
- Verify with `go build ./... && go vet ./... && go test ./... && staticcheck ./...`. Run tests **bare** — never pipe through `grep`/`head` without `set -o pipefail`.
- **Tasks 1–3 are the exception to the no-race-on-commit rule.** They exist to fix data races, so each ends with `go test -race ./internal/services/dataloader/` in addition to the bare suite. `make race` stays opt-in for every other task.
- Pre-commit runs `make check`; never bypass with `--no-verify`.

## Locking design (settled here, once)

Two mutexes on `DataLoader`, with distinct jobs. Getting these confused is the one way this phase can deadlock in production.

| Mutex | Guards | Held by | Never held while |
|-------|--------|---------|------------------|
| `stateMu sync.RWMutex` | `filteredTransferCount`, `enabledFiles`, `unresolvedDuplicates`, `resolvedDuplicates` — the in-memory fields `LoadData` stamps and later callers read | field accessors and the assignment sites inside a load; held for the assignment only, never across file I/O | calling any public method |
| `writeMu sync.Mutex` | each load→modify→save sequence over a JSON sidecar file, as one critical section | every public method that reads or writes a sidecar file | — |

**`writeMu` is not reentrant.** `sync.Mutex` deadlocks on a re-acquire by the same goroutine. So the invariant is absolute:

> A public method takes `writeMu` and then calls **only** `*Locked` helpers. A `*Locked` helper **never** takes `writeMu` and never calls a public method.

Public read methods (`LoadMajorExpenses`, `LoadTransactionPins`, `LoadAliases`, `LoadDuplicateDecisions`, `LoadDeletedMajorExpenses`, `LoadAmazonEnrichment`) take `writeMu` too, briefly. Without that a reader can observe `ArchiveMajorExpense` between its archive write and its active-list write, which is precisely the interleaving the spec describes.

`LoadDataContext` does **not** hold `writeMu` across its run — it calls the public `LoadX` methods, each of which acquires and releases. That is deliberate: a full CSV load can take seconds and must not block a pin write for its duration. It costs consistency *across* the four sidecar reads within one load, which no caller depends on.

**Lock ordering:** no method ever holds both. `stateMu` sites do no file I/O; `writeMu` sites touch no derived state. Keep it that way.

---

### Task 1: Guard the derived state `LoadData` stamps

`LoadDataContext` writes `dl.FilteredTransferCount` (`loader.go:532`), `dl.unresolvedDuplicates` and `dl.resolvedDuplicates` (`loader.go:753-754`) on every call, while `UnresolvedDuplicateCount()`, `UnresolvedDuplicates()` and `ResolvedDuplicates()` read them from other goroutines (`internal/handlers/duplicates/handlers.go:72-73`, `internal/templates/page_data.go:20`). `SetEnabledFiles` writes `dl.enabledFiles` from an HTTP handler (`internal/handlers/explorer/handlers.go:465`) while `LoadDataContext:174` and `GetFileInfo:593` read it. All four are `-race`-detectable data races today, between two ordinary browser requests. `list_duplicates` (Task 6) reads two of them.

**Files:**
- Modify: `internal/services/dataloader/loader.go` (struct at `:31-40`, `SetEnabledFiles` at `:130`, `LoadDataContext` at `:174`, `filterInternalTransfers` at `:532`, `GetFileInfo` at `:593`, accessors at `:727-746`, `applyDuplicateDetection` at `:753`)
- Test: `internal/services/dataloader/concurrency_test.go` (create)
- Modify: whichever test files reference `FilteredTransferCount` — enumerate them, do not assume

**Interfaces:**
- Consumes: nothing.
- Produces: `func (dl *DataLoader) FilteredTransfers() int` replacing the exported field `FilteredTransferCount`. `UnresolvedDuplicateCount() int`, `UnresolvedDuplicates() []DuplicatePair`, `ResolvedDuplicates() []DuplicatePair`, `SetEnabledFiles([]string)` and `GetFileInfo() ([]models.FileInfo, error)` keep their exact current signatures.

- [ ] **Step 1: Enumerate every reference to the field being renamed**

Run `LSP findReferences` with the cursor on `FilteredTransferCount` at `internal/services/dataloader/loader.go:33`. Report the full list of files and line numbers in the task report **before** editing anything. Do the same for `SetEnabledFiles`, `UnresolvedDuplicates`, `ResolvedDuplicates` and `UnresolvedDuplicateCount` to confirm no production caller outside `internal/handlers` and `internal/templates`.

Expected shape of the answer (verify, do not trust): `FilteredTransferCount` has no production reader outside the package — only `internal/services/dataloader/coverage_test.go`. That is what makes the rename safe.

- [ ] **Step 2: Write the failing race test**

Create `internal/services/dataloader/concurrency_test.go`:

```go
package dataloader

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"budget2/internal/services/storage"
)

// newRaceLoader builds a loader over a temp dir holding one small CSV, so
// LoadData does real work (and really stamps the derived fields) without
// the test depending on a fixture.
func newRaceLoader(t *testing.T) *DataLoader {
	t.Helper()
	dir := t.TempDir()
	csv := "Date,Description,Amount,Status\n" +
		"2024-01-05,CHECK #1001,-250.00,Posted\n" +
		"2024-01-03,ACME BILL PAY,-250.00,Scheduled\n" +
		"2024-01-09,GROCERY STORE,-42.10,\n"
	if err := os.WriteFile(filepath.Join(dir, "a.csv"), []byte(csv), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return New(dir, store)
}

// TestDerivedStateIsRaceFree exercises the fields LoadData stamps against
// the accessors that read them, plus the enabled-files map an HTTP handler
// can rewrite mid-load. It asserts nothing about values -- the assertion is
// the race detector's, and this test is only meaningful under -race.
func TestDerivedStateIsRaceFree(t *testing.T) {
	loader := newRaceLoader(t)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := loader.LoadData(); err != nil {
				t.Errorf("LoadData: %v", err)
			}
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = loader.UnresolvedDuplicateCount()
			_ = loader.UnresolvedDuplicates()
			_ = loader.ResolvedDuplicates()
			_ = loader.FilteredTransfers()
			if _, err := loader.GetFileInfo(); err != nil {
				t.Errorf("GetFileInfo: %v", err)
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			loader.SetEnabledFiles([]string{"a.csv"})
		}()
	}
	wg.Wait()
}
```

- [ ] **Step 3: Run it and confirm it fails**

Run: `go test -race ./internal/services/dataloader/ -run TestDerivedStateIsRaceFree -count=1`
Expected: FAIL, with `WARNING: DATA RACE` naming `unresolvedDuplicates`, `FilteredTransferCount` and/or `enabledFiles`. It will not compile until `FilteredTransfers` exists — add the accessor in Step 4 and re-run; the failure you must see is the race warning, not the compile error.

- [ ] **Step 4: Add `stateMu` and the accessor**

In `internal/services/dataloader/loader.go`, replace the struct:

```go
// DataLoader handles loading and preprocessing of financial data from CSV files.
//
// It is safe for concurrent use. Two mutexes with distinct jobs:
//
//   - stateMu guards the derived fields LoadData stamps and later callers
//     read. Held only across the assignment or the read, never across file
//     I/O.
//   - writeMu (see transaction_pins.go) makes each load->modify->save
//     sequence over a JSON sidecar file one critical section.
//
// No method holds both. stateMu sites do no file I/O; writeMu sites touch no
// derived state.
type DataLoader struct {
	CSVDirectory string
	store        *storage.Storage

	// stateMu guards every field below it.
	stateMu               sync.RWMutex
	filteredTransferCount int
	enabledFiles          map[string]bool

	// Populated by every LoadData call. Read-only for callers.
	unresolvedDuplicates []DuplicatePair
	resolvedDuplicates   []DuplicatePair
}
```

Add `"sync"` to the import block. Then:

```go
// SetEnabledFiles sets which files should be loaded
func (dl *DataLoader) SetEnabledFiles(files []string) {
	set := make(map[string]bool, len(files))
	for _, f := range files {
		set[f] = true
	}
	dl.stateMu.Lock()
	dl.enabledFiles = set
	dl.stateMu.Unlock()
}

// enabledFilesSnapshot returns the current enabled-file set for one load to
// work against. A caller that rewrites the set mid-load therefore affects the
// next load, not this one -- which is both race-free and the behavior a user
// clicking "apply" expects.
func (dl *DataLoader) enabledFilesSnapshot() map[string]bool {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	out := make(map[string]bool, len(dl.enabledFiles))
	for k, v := range dl.enabledFiles {
		out[k] = v
	}
	return out
}

// FilteredTransfers returns how many internal transfers the most recent load
// filtered out. Replaces the former exported FilteredTransferCount field,
// which could not be read safely while another goroutine was loading.
func (dl *DataLoader) FilteredTransfers() int {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	return dl.filteredTransferCount
}
```

In `LoadDataContext`, take one snapshot before the file loop and use it inside:

```go
	enabled := dl.enabledFilesSnapshot()
```

and change the skip check at `:174` to `if len(enabled) > 0 && !enabled[filename] {`.

In `GetFileInfo`, do the same — one `enabled := dl.enabledFilesSnapshot()` before the loop, and `if len(enabled) > 0 { enabled := enabled[filename] ... }` rewritten to avoid shadowing:

```go
		fileEnabled := true
		if len(enabled) > 0 {
			fileEnabled = enabled[filename]
		}
```
with `Enabled: fileEnabled` in the `models.FileInfo` literal.

In `filterInternalTransfers`, replace the two field writes at `:532-534`:

```go
	count := initialCount - len(filtered)
	dl.stateMu.Lock()
	dl.filteredTransferCount = count
	dl.stateMu.Unlock()
	if count > 0 {
		log.Printf("Filtered %d internal transfers", count)
	}
```

In `applyDuplicateDetection`, the two nil-assignments at `:753-754` and every later write to `dl.unresolvedDuplicates` / `dl.resolvedDuplicates` must move under `stateMu`. Build both slices in locals first, then publish once at the end of the function:

```go
	dl.stateMu.Lock()
	dl.unresolvedDuplicates = unresolved
	dl.resolvedDuplicates = resolved
	dl.stateMu.Unlock()
```

Read the rest of `applyDuplicateDetection` (`loader.go:752` to the end of the function) and convert its existing appends to append to the locals. Do not leave a single direct write to either field outside a `stateMu` section.

Finally, guard the three accessors:

```go
func (dl *DataLoader) UnresolvedDuplicateCount() int {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	return len(dl.unresolvedDuplicates)
}

func (dl *DataLoader) UnresolvedDuplicates() []DuplicatePair {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	out := make([]DuplicatePair, len(dl.unresolvedDuplicates))
	copy(out, dl.unresolvedDuplicates)
	return out
}

func (dl *DataLoader) ResolvedDuplicates() []DuplicatePair {
	dl.stateMu.RLock()
	defer dl.stateMu.RUnlock()
	out := make([]DuplicatePair, len(dl.resolvedDuplicates))
	copy(out, dl.resolvedDuplicates)
	return out
}
```

- [ ] **Step 5: Update every reference found in Step 1**

Change each `loader.FilteredTransferCount` reference to `loader.FilteredTransfers()`. Use the list from Step 1, not a text search. `New` no longer needs to initialize `enabledFiles` (a nil map reads fine and `SetEnabledFiles` always replaces it), but leaving the initialization is harmless — keep it.

- [ ] **Step 6: Run the race test and the package suite**

Run: `go test -race ./internal/services/dataloader/ -count=1`
Expected: PASS, no `DATA RACE` warnings.

Run: `go build ./... && go test ./... && go vet ./... && staticcheck ./...`
Expected: all green.

- [ ] **Step 7: Prove the test discriminates**

Temporarily delete the `dl.stateMu.Lock()` / `Unlock()` pair around the `unresolvedDuplicates` publish in `applyDuplicateDetection`. Run `go test -race ./internal/services/dataloader/ -run TestDerivedStateIsRaceFree -count=1`.
Expected: FAIL with a `DATA RACE` warning. Restore the lock, re-run, confirm PASS. **Report both outputs in the task report.** If the mutated build still passes, the test is not exercising what it claims and must be strengthened before proceeding.

- [ ] **Step 8: Commit**

```bash
git add internal/services/dataloader/
git commit -m "fix(dataloader): guard the derived state LoadData stamps

LoadDataContext wrote FilteredTransferCount, unresolvedDuplicates and
resolvedDuplicates while the duplicates page and the nav badge read them
from other request goroutines, and an explorer POST rewrote enabledFiles
mid-load. All four were -race-detectable races between two ordinary
browser requests.

FilteredTransferCount becomes the FilteredTransfers() accessor; it had no
production reader outside this package."
```

---

### Task 2: Serialize the single-file read-modify-write sequences

`SetTransactionPins` loads `transaction_pins.json`, edits the map in memory, and writes it back. Two concurrent calls both load the same map and the second write erases the first's changes. `storage.Storage.WriteFile` takes only an `RLock` (`internal/services/storage/storage.go:279-290`), which is shared — it serializes nothing between callers. The same shape applies to `SetTransactionPin`, `PrunePinsForMissingExpenses`, `SaveAlias`, `SaveDuplicateDecision`, `ClearDuplicateDecision`, `AddMajorExpense`, `UpdateMajorExpense` and `DeleteMajorExpense`.

**Files:**
- Modify: `internal/services/dataloader/transaction_pins.go` (all of it)
- Modify: `internal/services/dataloader/major_expenses.go:22-120` (load/save/add/update/delete; the archive/restore trio is Task 3)
- Modify: `internal/services/dataloader/duplicate_decisions.go:50-108`
- Modify: `internal/services/dataloader/amazon_enrichment.go:26-53`
- Modify: `internal/services/dataloader/loader.go:673-705` (`LoadAliases`, `SaveAlias`)
- Test: `internal/services/dataloader/concurrency_test.go` (extend)

**Interfaces:**
- Consumes: nothing from Task 1 (the two mutexes are independent).
- Produces: unexported helpers used by Task 3 —
  `loadMajorExpensesLocked() ([]models.MajorExpense, error)`,
  `saveMajorExpensesLocked([]models.MajorExpense) error`,
  `loadDeletedMajorExpensesLocked() ([]models.DeletedMajorExpense, error)`,
  `saveDeletedMajorExpensesLocked([]models.DeletedMajorExpense) error`,
  `loadTransactionPinsLocked() (map[string]string, error)`,
  `writePinsLocked(map[string]string) error`.
  Every public method keeps its exact current signature.

- [ ] **Step 1: Write the failing lost-update test**

Append to `internal/services/dataloader/concurrency_test.go`:

```go
// TestConcurrentPinWritesDoNotLoseUpdates pins 32 distinct hashes from 32
// goroutines. Each call is a load->modify->save over one file, so without a
// lock around the whole sequence the later writers save a map they read
// before the earlier writers' changes landed, and pins vanish.
func TestConcurrentPinWritesDoNotLoseUpdates(t *testing.T) {
	loader := newRaceLoader(t)

	const n = 32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hash := fmt.Sprintf("hash-%02d", i)
			if _, err := loader.SetTransactionPins(map[string]string{hash: "expense-1"}); err != nil {
				t.Errorf("SetTransactionPins(%s): %v", hash, err)
			}
		}(i)
	}
	wg.Wait()

	pins, err := loader.LoadTransactionPins()
	if err != nil {
		t.Fatalf("LoadTransactionPins: %v", err)
	}
	if len(pins) != n {
		t.Fatalf("pins on disk = %d, want %d -- concurrent writes lost updates", len(pins), n)
	}
	for i := 0; i < n; i++ {
		hash := fmt.Sprintf("hash-%02d", i)
		if pins[hash] != "expense-1" {
			t.Errorf("pin %s = %q, want %q", hash, pins[hash], "expense-1")
		}
	}
}
```

Add `"fmt"` to the test file's imports.

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./internal/services/dataloader/ -run TestConcurrentPinWritesDoNotLoseUpdates -count=5`
Expected: FAIL on most or all of the 5 runs, with `pins on disk = <n < 32>, want 32`. If it passes all five, raise `n` to 64 and re-run before concluding anything — a lost update needs the interleaving to actually happen. Record the observed counts in the task report.

- [ ] **Step 3: Add `writeMu` and convert `transaction_pins.go`**

Add the field to the `DataLoader` struct in `loader.go` (below `store`, above `stateMu`):

```go
	// writeMu makes each load->modify->save sequence over a JSON sidecar
	// file ONE critical section. storage.Storage locks only around an
	// individual WriteFile -- and only with an RLock, which is shared -- so
	// it does nothing for a caller that reads, edits in memory and writes
	// back.
	//
	// NOT reentrant. The invariant, without exception: a public method takes
	// writeMu and then calls only *Locked helpers; a *Locked helper never
	// takes writeMu and never calls a public method.
	writeMu sync.Mutex
```

Rewrite `internal/services/dataloader/transaction_pins.go` below the path helper:

```go
// LoadTransactionPins reads the hash → MajorExpense.ID mapping from disk.
// Returns an empty map if the file does not exist.
func (dl *DataLoader) LoadTransactionPins() (map[string]string, error) {
	dl.writeMu.Lock()
	defer dl.writeMu.Unlock()
	return dl.loadTransactionPinsLocked()
}

// loadTransactionPinsLocked is LoadTransactionPins' body. Caller holds writeMu.
func (dl *DataLoader) loadTransactionPinsLocked() (map[string]string, error) {
	path := dl.transactionPinsPath()
	data, err := dl.store.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	pins := make(map[string]string)
	if err := json.Unmarshal(data, &pins); err != nil {
		return nil, fmt.Errorf("invalid transaction_pins file: %w", err)
	}
	return pins, nil
}

// writePinsLocked marshals and persists the pin map. Caller holds writeMu.
func (dl *DataLoader) writePinsLocked(pins map[string]string) error {
	data, err := json.MarshalIndent(pins, "", "  ")
	if err != nil {
		return err
	}
	return dl.store.WriteFile(dl.transactionPinsPath(), data, 0644)
}

// SetTransactionPin pins a transaction (by hash) to a major-expense ID.
// An empty expenseID removes the pin.
func (dl *DataLoader) SetTransactionPin(hash, expenseID string) error {
	if hash == "" {
		return fmt.Errorf("transaction hash is required")
	}
	dl.writeMu.Lock()
	defer dl.writeMu.Unlock()
	pins, err := dl.loadTransactionPinsLocked()
	if err != nil {
		return fmt.Errorf("load transaction pins: %w", err)
	}
	if expenseID == "" {
		delete(pins, hash)
	} else {
		pins[hash] = expenseID
	}
	return dl.writePinsLocked(pins)
}

// ClearTransactionPin removes the pin for a transaction hash. No-op if
// the hash isn't pinned.
func (dl *DataLoader) ClearTransactionPin(hash string) error {
	return dl.SetTransactionPin(hash, "")
}

// SetTransactionPins writes many hash → expense-ID pins in one disk
// round-trip. Existing pins for hashes not in the input map are left
// untouched; pins in the input map with an empty expenseID are removed.
// Empty hashes are silently skipped so callers don't have to filter
// upstream. Returns the number of pins actually changed.
func (dl *DataLoader) SetTransactionPins(updates map[string]string) (int, error) {
	if len(updates) == 0 {
		return 0, nil
	}
	dl.writeMu.Lock()
	defer dl.writeMu.Unlock()
	pins, err := dl.loadTransactionPinsLocked()
	if err != nil {
		return 0, err
	}
	changed := 0
	for hash, expenseID := range updates {
		if hash == "" {
			continue
		}
		if expenseID == "" {
			if _, ok := pins[hash]; ok {
				delete(pins, hash)
				changed++
			}
			continue
		}
		if pins[hash] != expenseID {
			pins[hash] = expenseID
			changed++
		}
	}
	if changed == 0 {
		return 0, nil
	}
	if err := dl.writePinsLocked(pins); err != nil {
		return 0, err
	}
	return changed, nil
}

// PrunePinsForMissingExpenses drops pins whose target ID is not in the
// supplied list of valid expense IDs. Used by DeleteMajorExpense and on
// startup to prevent orphaned pins from quietly hiding transactions.
func (dl *DataLoader) PrunePinsForMissingExpenses(validIDs map[string]bool) error {
	dl.writeMu.Lock()
	defer dl.writeMu.Unlock()
	pins, err := dl.loadTransactionPinsLocked()
	if err != nil {
		return err
	}
	changed := false
	for hash, id := range pins {
		if !validIDs[id] {
			delete(pins, hash)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return dl.writePinsLocked(pins)
}
```

Note `ClearTransactionPin` calls the *public* `SetTransactionPin` and therefore must **not** hold `writeMu` itself. That is the one method in this file that stays lock-free, and the reason is worth the two-line comment it does not currently have — add one.

- [ ] **Step 4: Convert the four remaining single-file sequences**

`internal/services/dataloader/major_expenses.go` — `LoadMajorExpenses`, `SaveMajorExpenses`, `LoadDeletedMajorExpenses` and `SaveDeletedMajorExpenses` each become a public wrapper taking `writeMu` around a `*Locked` helper holding the current body. `AddMajorExpense`, `UpdateMajorExpense` and `DeleteMajorExpense` take `writeMu` for the whole sequence and call the locked helpers:

```go
// AddMajorExpense appends a new entry, stamping CreatedAt/UpdatedAt, and
// returns the resulting slice.
func (dl *DataLoader) AddMajorExpense(me models.MajorExpense) ([]models.MajorExpense, error) {
	dl.writeMu.Lock()
	defer dl.writeMu.Unlock()
	list, err := dl.loadMajorExpensesLocked()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	me.CreatedAt = now
	me.UpdatedAt = now
	list = append(list, me)
	if err := dl.saveMajorExpensesLocked(list); err != nil {
		return nil, err
	}
	return list, nil
}
```

Apply the identical shape to `UpdateMajorExpense` (`:71-97`) and `DeleteMajorExpense` (`:105-120`), replacing their `dl.LoadMajorExpenses()` / `dl.SaveMajorExpenses(...)` calls with the locked helpers. **Leave `ArchiveMajorExpense`, `RestoreMajorExpense` and `DiscardDeletedMajorExpense` alone — they are Task 3.** They currently call the public `LoadMajorExpenses` etc., which is still correct (just unserialized) at the end of this task; they must not be half-converted.

`internal/services/dataloader/duplicate_decisions.go` — `LoadDuplicateDecisions` gets a `loadDuplicateDecisionsLocked` body and a locking wrapper. `SaveDuplicateDecision` and `ClearDuplicateDecision` take `writeMu` for their whole sequence and call `loadDuplicateDecisionsLocked` + `writeDecisions` (already unexported and lock-free — rename it `writeDecisionsLocked` for consistency with the invariant, and say so in its doc comment).

`internal/services/dataloader/amazon_enrichment.go` — `LoadAmazonEnrichment` and `SaveAmazonEnrichment` each take `writeMu` around their existing bodies. Neither is a read-modify-write today, but a caller that reads-then-saves (`cmd/enrich-amazon/main.go:149` does exactly that) is one, and the lock is what makes that caller correct.

`internal/services/dataloader/loader.go:673-705` — `LoadAliases` gets a `loadAliasesLocked` body plus a locking wrapper; `SaveAlias` takes `writeMu` and calls `loadAliasesLocked`.

**One trap to check for.** `applyAliases` (`:709`), `applyMajorExpenseNames` and `applyAmazonEnrichment` are called from inside `LoadDataContext` and call the *public* `LoadAliases` / `LoadMajorExpenses` / `LoadTransactionPins` / `LoadAmazonEnrichment`. `LoadDataContext` does not hold `writeMu`, so those acquisitions are fine. Verify this by reading each `apply*` function and confirming none of them is ever called from a method that already holds the lock. Report the finding.

- [ ] **Step 5: Run the lost-update test and the full suite**

Run: `go test ./internal/services/dataloader/ -run TestConcurrentPinWritesDoNotLoseUpdates -count=20`
Expected: PASS on all 20.

Run: `go test -race ./internal/services/dataloader/ -count=1`
Expected: PASS, no `DATA RACE`.

Run: `go build ./... && go test ./... && go vet ./... && staticcheck ./...`
Expected: all green. A deadlock shows up here as a test *hang*, not a failure — if any package stops producing output for more than a minute, a public method is calling another public method while holding `writeMu`. Find it with `SIGQUIT` (`kill -QUIT <pid>`) and read the goroutine dump.

- [ ] **Step 6: Prove the test discriminates**

Temporarily remove the `dl.writeMu.Lock()` / `defer dl.writeMu.Unlock()` pair from `SetTransactionPins` only. Run `go test ./internal/services/dataloader/ -run TestConcurrentPinWritesDoNotLoseUpdates -count=5`.
Expected: FAIL. Restore, re-run, confirm PASS. Report both outputs.

- [ ] **Step 7: Commit**

```bash
git add internal/services/dataloader/
git commit -m "fix(dataloader): serialize the single-file read-modify-write sequences

Every pin, alias, decision and major-expense write loads a JSON file,
edits it in memory and writes it back. storage.Storage takes only an
RLock around the individual WriteFile, which serializes nothing between
callers, so two concurrent writers both saved a map read before the
other's change landed.

writeMu now covers each whole sequence. Public methods take it and call
new *Locked helpers; the helpers never take it. Archive/Restore/Discard
are deliberately untouched here -- they span three files and are next."
```

---

### Task 3: Serialize the multi-file major-expense sequences

`ArchiveMajorExpense` performs three separate writes — archive, then the active list, then pins (`major_expenses.go:216-238`). Interleave it with `AddMajorExpense` and the two lists diverge: `AddMajorExpense` loads `[A,B]`, `ArchiveMajorExpense(B)` writes the archive and then the active list as `[A]`, and the pending add saves `[A,B,C]`. `B` now exists in **both** files, and `RestoreMajorExpense` refuses it from then on with "active major expense with id already exists". This is the sharp case the spec names.

**Files:**
- Modify: `internal/services/dataloader/major_expenses.go:173-343` (`ArchiveMajorExpense`, `RestoreMajorExpense`, `DiscardDeletedMajorExpense`)
- Test: `internal/services/dataloader/concurrency_test.go` (extend)

**Interfaces:**
- Consumes: from Task 2 — `loadMajorExpensesLocked`, `saveMajorExpensesLocked`, `loadDeletedMajorExpensesLocked`, `saveDeletedMajorExpensesLocked`, `loadTransactionPinsLocked`, `writePinsLocked`.
- Produces: a package-level test seam `var testHookAfterExpenseLoad func()`. Public signatures unchanged.

- [ ] **Step 1: Add the test seam**

At the top of `internal/services/dataloader/major_expenses.go`, below the `majorExpensesFile` constant:

```go
// testHookAfterExpenseLoad, when non-nil, is called by AddMajorExpense after
// it has loaded the active list and while it still holds writeMu. It exists
// so a test can prove that a concurrent ArchiveMajorExpense really blocks for
// the whole load->modify->save sequence rather than merely usually losing the
// race. Nil in production; the only writer is a test in this package.
var testHookAfterExpenseLoad func()
```

Call it in `AddMajorExpense`, immediately after the `loadMajorExpensesLocked` error check:

```go
	if testHookAfterExpenseLoad != nil {
		testHookAfterExpenseLoad()
	}
```

- [ ] **Step 2: Write the failing interleaving test**

Append to `internal/services/dataloader/concurrency_test.go`:

```go
// TestArchiveCannotInterleaveWithAdd is deterministic, not probabilistic.
// It parks AddMajorExpense between its load and its save, then starts an
// ArchiveMajorExpense and asserts the archive makes NO progress while the add
// holds the write lock. Without the lock the archive runs to completion
// during the park, its active-list write is then overwritten by the add's
// stale [A,B] + C, and B ends up in both files.
func TestArchiveCannotInterleaveWithAdd(t *testing.T) {
	loader := newRaceLoader(t)

	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "A", Name: "Rent"}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, err := loader.AddMajorExpense(models.MajorExpense{ID: "B", Name: "Insurance"}); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	parked := make(chan struct{})
	release := make(chan struct{})
	testHookAfterExpenseLoad = func() {
		testHookAfterExpenseLoad = nil // fire once; the archive path must not re-trigger it
		close(parked)
		<-release
	}
	t.Cleanup(func() { testHookAfterExpenseLoad = nil })

	addDone := make(chan error, 1)
	go func() {
		_, err := loader.AddMajorExpense(models.MajorExpense{ID: "C", Name: "Utilities"})
		addDone <- err
	}()
	<-parked

	archiveDone := make(chan error, 1)
	go func() { archiveDone <- loader.ArchiveMajorExpense("B") }()

	select {
	case err := <-archiveDone:
		t.Fatalf("ArchiveMajorExpense completed (err=%v) while AddMajorExpense held the write lock", err)
	case <-time.After(200 * time.Millisecond):
		// Correct: the archive is blocked on writeMu.
	}

	close(release)
	if err := <-addDone; err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	if err := <-archiveDone; err != nil {
		t.Fatalf("ArchiveMajorExpense: %v", err)
	}

	active, err := loader.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("LoadMajorExpenses: %v", err)
	}
	deleted, err := loader.LoadDeletedMajorExpenses()
	if err != nil {
		t.Fatalf("LoadDeletedMajorExpenses: %v", err)
	}
	activeIDs := map[string]bool{}
	for _, e := range active {
		activeIDs[e.ID] = true
	}
	for _, d := range deleted {
		if activeIDs[d.Expense.ID] {
			t.Fatalf("expense %s is in BOTH the active list and the archive", d.Expense.ID)
		}
	}
	if !activeIDs["C"] {
		t.Error("the added expense C is missing from the active list")
	}
	if activeIDs["B"] {
		t.Error("the archived expense B is still in the active list")
	}
}
```

Add `"time"` and `"budget2/internal/models"` to the test file's imports.

- [ ] **Step 3: Run it and confirm it fails**

Run: `go test ./internal/services/dataloader/ -run TestArchiveCannotInterleaveWithAdd -count=1`
Expected: FAIL with `ArchiveMajorExpense completed (err=<nil>) while AddMajorExpense held the write lock`. `ArchiveMajorExpense` still calls the public `LoadMajorExpenses` at this point, which takes `writeMu` — so it may instead block and the test may pass for the *wrong reason*. Read the failure carefully: if it passes at this step, that is because Task 2 already serialized the archive's *first* load, not because the sequence is atomic. Confirm the real gap by also running the invariant half against the current code — comment out the `select` block and check whether `B` lands in both files. Record which of the two you observed.

- [ ] **Step 4: Convert the three multi-file methods**

Take `writeMu` once at the top of each and route every inner call through the locked helpers. `ArchiveMajorExpense` becomes:

```go
func (dl *DataLoader) ArchiveMajorExpense(id string) error {
	if id == "" {
		return fmt.Errorf("expense id is required")
	}
	dl.writeMu.Lock()
	defer dl.writeMu.Unlock()

	active, err := dl.loadMajorExpensesLocked()
	if err != nil {
		return err
	}
	var (
		target    models.MajorExpense
		targetIdx = -1
	)
	for i, me := range active {
		if me.ID == id {
			target = me
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return fmt.Errorf("major expense not found: %s", id)
	}

	pins, err := dl.loadTransactionPinsLocked()
	if err != nil {
		return err
	}
	pinnedHashes := make([]string, 0)
	for hash, eid := range pins {
		if eid == id {
			pinnedHashes = append(pinnedHashes, hash)
		}
	}

	deleted, err := dl.loadDeletedMajorExpensesLocked()
	if err != nil {
		return err
	}
	deleted = append(deleted, models.DeletedMajorExpense{
		Expense:      target,
		DeletedAt:    time.Now().UTC(),
		PinnedHashes: pinnedHashes,
	})
	if err := dl.saveDeletedMajorExpensesLocked(deleted); err != nil {
		return err
	}

	out := make([]models.MajorExpense, 0, len(active)-1)
	out = append(out, active[:targetIdx]...)
	out = append(out, active[targetIdx+1:]...)
	if err := dl.saveMajorExpensesLocked(out); err != nil {
		return err
	}

	if len(pinnedHashes) > 0 {
		for _, h := range pinnedHashes {
			delete(pins, h)
		}
		if err := dl.writePinsLocked(pins); err != nil {
			return err
		}
	}
	return nil
}
```

Note the pin write now goes through `writePinsLocked` instead of the inline `json.MarshalIndent` + `dl.store.WriteFile` — same bytes, one fewer place to get the path wrong. Apply the same treatment to `RestoreMajorExpense` (`:253-318`) and `DiscardDeletedMajorExpense` (`:322-343`).

Update the write-order comment on `ArchiveMajorExpense` to say the ordering now protects against a *crash* only — an interleaving with another writer is no longer possible:

```go
// Write order is archive → active list → pins, so a crash mid-operation
// leaves the user with a recoverable duplicate (an entry in both lists)
// rather than data loss. A concurrent writer can no longer produce that
// duplicate: the whole sequence is one writeMu critical section.
// RestoreMajorExpense reverses this.
```

- [ ] **Step 5: Run the test and the full suite**

Run: `go test ./internal/services/dataloader/ -run TestArchiveCannotInterleaveWithAdd -count=10`
Expected: PASS on all 10.

Run: `go test -race ./internal/services/dataloader/ -count=1 && go build ./... && go test ./... && go vet ./... && staticcheck ./...`
Expected: all green. Watch for hangs — see Task 2 Step 5.

- [ ] **Step 6: Prove the test discriminates**

Temporarily remove the `dl.writeMu.Lock()` / `defer` pair from `ArchiveMajorExpense` **and** point its inner calls back at the public `LoadMajorExpenses` / `LoadTransactionPins` / `LoadDeletedMajorExpenses` / `SaveDeletedMajorExpenses` / `SaveMajorExpenses`, reproducing the pre-task code. Run the test.
Expected: FAIL, on the `completed ... while AddMajorExpense held the write lock` assertion. Restore, re-run, confirm PASS. Report both outputs.

- [ ] **Step 7: Commit**

```bash
git add internal/services/dataloader/
git commit -m "fix(dataloader): make archive/restore/discard one critical section

ArchiveMajorExpense is three separate writes. Interleaved with a pending
AddMajorExpense, the add's stale active list overwrote the archive's, and
the expense ended up in BOTH files -- after which RestoreMajorExpense
refuses it forever with 'active major expense with id already exists'.

All three multi-file sequences now hold writeMu end to end. The test is
deterministic: it parks the add between load and save via a package test
hook and asserts the archive makes no progress."
```

---

### Task 4: `admin` package skeleton, `get_status`, and the wiring

The first tool. It must work when the store is **locked** — that is exactly when the user needs to know why every other tool is failing — so it reports encryption state before it attempts anything that reads the ledger.

**Files:**
- Create: `internal/services/mcpsvc/admin/register.go`
- Create: `internal/services/mcpsvc/admin/status.go`
- Create: `internal/services/mcpsvc/admin/register_test.go`
- Create: `internal/services/mcpsvc/admin/status_test.go`
- Modify: `internal/services/backup/meta.go` (export a reader)
- Modify: `internal/services/mcpsvc/server.go:24-31` (add `Backups` to `Deps`), `:83-115` (register `admin`)
- Modify: `cmd/server/main.go:111-118` (pass `backupService`)

**Interfaces:**
- Consumes: nothing from Tasks 1–3 beyond their unchanged public signatures.
- Produces:
  - `func (s *backup.Service) Meta() (backup.Meta, error)`
  - `admin.Deps{Transactions TransactionSource, Files FileLister, Duplicates DuplicateSource, Decisions DecisionStore, Store *storage.Storage, Settings SettingsSource, Backups BackupService, Snapshots *snapshot.Snapshotter}`
  - `func admin.Register(s *mcp.Server, deps Deps)`
  - `admin.recoverToError(tool string, err *error)`

- [ ] **Step 1: Export the backup meta reader**

In `internal/services/backup/meta.go`, add below `loadMeta`:

```go
// Meta returns the on-disk record of the most recent backup attempt. A
// missing file is not an error -- it means no backup has run yet, and the
// zero Meta says exactly that.
func (s *Service) Meta() (Meta, error) { return loadMeta(s.cfg.BackupDir) }
```

- [ ] **Step 2: Write the failing test**

Create `internal/services/mcpsvc/admin/register_test.go` with the harness, copied from `internal/services/mcpsvc/curate/register_test.go:51-113` (`connect`, `call`, `decodeToolResult`, `toolErrorText` — identical bodies, `package admin`), plus:

```go
package admin

import (
	"os"
	"path/filepath"
	"testing"

	"budget2/internal/models"
	backupsvc "budget2/internal/services/backup"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"
)

type stubTransactions struct {
	ts  *models.TransactionSet
	err error
}

func (s stubTransactions) LoadData() (*models.TransactionSet, error) { return s.ts, s.err }

// newDeps builds Deps over a temp directory with a real DataLoader, Storage,
// SettingsManager and backup Service. Nothing here is stubbed except the
// ledger itself: these tools report on-disk state, so a fake store would test
// nothing. Returns the data directory so tests can assert on files.
func newDeps(t *testing.T, txns []models.Transaction) (Deps, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	loader := dataloader.New(dir, store)
	settingsDir := filepath.Join(dir, "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings: %v", err)
	}
	backupDir := filepath.Join(dir, "backups")
	svc, err := backupsvc.New(backupsvc.Config{BackupDir: backupDir, DataDir: dir, Store: store})
	if err != nil {
		t.Fatalf("backup.New: %v", err)
	}
	for i := range txns {
		if txns[i].Hash == "" {
			txns[i].Hash = txns[i].ComputeHash()
		}
	}
	return Deps{
		Transactions: stubTransactions{ts: models.NewTransactionSet(txns)},
		Files:        loader,
		Duplicates:   loader,
		Decisions:    loader,
		Store:        store,
		Settings:     retirement.NewSettingsManager(settingsDir, store),
		Backups:      svc,
		Snapshots:    snapshot.New(dir, filepath.Join(dir, "snapshots")),
	}, dir
}
```

Create `internal/services/mcpsvc/admin/status_test.go`:

```go
package admin

import "testing"

func TestGetStatusReportsAnUnencryptedStore(t *testing.T) {
	deps, dir := newDeps(t, nil)
	cs := connect(t, deps)

	out := decodeToolResult[statusOutput](t, call(t, cs, "get_status", map[string]any{}))

	if out.DataDir != dir {
		t.Errorf("data_dir = %q, want %q", out.DataDir, dir)
	}
	if out.Encrypted {
		t.Error("encrypted = true, want false for a plain temp dir")
	}
	if !out.Unlocked {
		t.Error("unlocked = false; an unencrypted store is always readable")
	}
	if out.AuthMethod != "" {
		t.Errorf("auth_method = %q, want empty when not encrypted", out.AuthMethod)
	}
	if out.Backup.Dir == "" {
		t.Error("backup.dir is empty, want the configured backup directory")
	}
	if out.Backup.LastBackupTS != "" {
		t.Errorf("backup.last_backup_ts = %q, want empty before any backup runs", out.Backup.LastBackupTS)
	}
	if out.UnresolvedDuplicates == nil {
		t.Fatal("unresolved_duplicates is null on an unlocked store; want a count")
	}
	if *out.UnresolvedDuplicates != 0 {
		t.Errorf("unresolved_duplicates = %d, want 0", *out.UnresolvedDuplicates)
	}
}
```

- [ ] **Step 3: Run it and confirm it fails**

Run: `go test ./internal/services/mcpsvc/admin/ -count=1`
Expected: FAIL to build — `package admin` has no non-test files. That is the expected red.

- [ ] **Step 4: Write `register.go`**

```go
// Package admin serves the app's housekeeping state over MCP: what the
// storage, backup and settings layers are doing, which CSV files are loaded,
// and the near-duplicate review queue the /duplicates page owns.
//
// It calls the same *dataloader.DataLoader, *storage.Storage,
// *retirement.SettingsManager and *backup.Service instances the HTTP handlers
// use, so a tool answer and a page cannot disagree about the app's state.
package admin

import (
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/backup"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// duplicateDecisionsFile is the sidecar the write tools snapshot before
// changing. It mirrors the unexported constant in
// internal/services/dataloader.
const duplicateDecisionsFile = "duplicate_decisions.json"

// TransactionSource loads the full transaction history.
// *dataloader.DataLoader satisfies it via LoadData. The interface exists so
// tests can substitute a canned models.TransactionSet.
type TransactionSource interface {
	LoadData() (*models.TransactionSet, error)
}

// FileLister reports the CSV inventory. *dataloader.DataLoader satisfies it.
type FileLister interface {
	GetFileInfo() ([]models.FileInfo, error)
}

// DuplicateSource exposes the near-duplicate detection results cached by the
// most recent LoadData. *dataloader.DataLoader satisfies it.
//
// Every caller must LoadData FIRST: these three return the previous load's
// results, and on a freshly constructed loader they return nothing at all.
type DuplicateSource interface {
	UnresolvedDuplicates() []dataloader.DuplicatePair
	ResolvedDuplicates() []dataloader.DuplicatePair
	UnresolvedDuplicateCount() int
}

// DecisionStore reads and writes duplicate_decisions.json.
// *dataloader.DataLoader satisfies it.
type DecisionStore interface {
	LoadDuplicateDecisions() (map[string]dataloader.DuplicateDecision, error)
	SaveDuplicateDecision(string, dataloader.DuplicateDecision) error
	ClearDuplicateDecision(string) error
}

// SettingsSource reports the retirement planner's state.
// *retirement.SettingsManager satisfies it.
type SettingsSource interface {
	Revision() int
	ActiveScenario() string
	SettingsDir() string
}

// BackupService runs and reports on data-directory snapshots.
// *backup.Service satisfies it.
type BackupService interface {
	BackupDir() string
	DataDir() string
	Enabled() bool
	Meta() (backup.Meta, error)
	Snapshot(ctx context.Context) error
}

// Deps is what the housekeeping tools need. Every field may be nil except
// Transactions: a nil dependency makes the tools that need it fail their own
// call with a named error rather than being absent from the tool list, which
// matches how plan and curate behave.
type Deps struct {
	Transactions TransactionSource
	Files        FileLister
	Duplicates   DuplicateSource
	Decisions    DecisionStore
	Store        *storage.Storage
	Settings     SettingsSource
	Backups      BackupService
	Snapshots    *snapshot.Snapshotter
}

// recoverToError converts a panic into an error so a bad definition fails one
// tool call instead of terminating the session. The go-sdk dispatches every
// tool call on its own goroutine with no recover of its own, so this must run
// via a defer inside each handler closure.
func recoverToError(tool string, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s panicked: %v", tool, r)
	}
}

// locked reports whether the store is encrypted and not currently unlocked,
// which is the one condition under which nothing can read the ledger.
func (d Deps) locked() bool {
	return d.Store != nil && d.Store.IsEncrypted() && !d.Store.IsUnlocked()
}

// load returns the full ledger, reporting a locked store as such rather than
// letting ciphertext surface as a parse error.
func (d Deps) load() (*models.TransactionSet, error) {
	if d.locked() {
		return nil, fmt.Errorf(
			"cannot load transaction history: storage is encrypted and locked; unlock it via the budget2 web UI (/unlock) first")
	}
	if d.Transactions == nil {
		return nil, fmt.Errorf("transaction source is not configured on this server")
	}
	return d.Transactions.LoadData()
}

// Register adds the housekeeping tools to s.
func Register(s *mcp.Server, deps Deps) {
	registerStatus(s, deps)
}
```

Add `"context"` to the import block (`BackupService.Snapshot` needs it).

- [ ] **Step 5: Write `status.go`**

```go
package admin

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type statusInput struct{}

type backupStatus struct {
	Enabled       bool   `json:"enabled"`
	Dir           string `json:"dir"`
	LastBackupTS  string `json:"last_backup_ts,omitempty"`
	FileCount     int    `json:"file_count,omitempty"`
	TotalBytes    int64  `json:"total_bytes,omitempty"`
	Encrypted     bool   `json:"encrypted"`
	LastAttemptTS string `json:"last_attempt_ts,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

type planStatus struct {
	SettingsDir    string `json:"settings_dir"`
	Revision       int    `json:"revision"`
	ActiveScenario string `json:"active_scenario"`
}

type statusOutput struct {
	DataDir    string `json:"data_dir"`
	Encrypted  bool   `json:"encrypted"`
	Unlocked   bool   `json:"unlocked"`
	AuthMethod string `json:"auth_method,omitempty"`

	// UnresolvedDuplicates is a pointer so a locked store reports null --
	// "not knowable right now" -- rather than 0, which would read as "the
	// queue is empty" and is the opposite of the truth.
	UnresolvedDuplicates *int `json:"unresolved_duplicates"`
	CSVFileCount         *int `json:"csv_file_count"`

	Plan   planStatus   `json:"plan"`
	Backup backupStatus `json:"backup"`

	Notes []string `json:"notes,omitempty"`
}

func registerStatus(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "get_status",
		Description: "Report the budget2 server's own state: where its data lives, whether that data is " +
			"encrypted and currently unlocked, how the retirement plan's saved settings are versioned, when the " +
			"last backup ran, and how many near-duplicate transaction pairs are waiting for review. Call this " +
			"FIRST when another tool fails for a reason you cannot explain -- an encrypted store that is locked " +
			"makes every ledger-reading tool fail, and this is the only tool that still answers in that state. " +
			"When the store is locked, unresolved_duplicates and csv_file_count are null (meaning 'cannot be " +
			"determined right now'), NOT zero, and a note says so. auth_method names how the user unlocks it " +
			"(password, ssh, age, yubikey) and is empty when the store is not encrypted. revision counts saved " +
			"changes to the retirement plan and is the same counter apply_changes reports. backup.last_backup_ts " +
			"is the most recent SUCCESSFUL backup (format YYYYMMDD_HHMMSS, UTC); backup.last_attempt_ts and " +
			"backup.last_error describe the most recent ATTEMPT, so a non-empty last_error with an older " +
			"last_backup_ts means backups are currently failing. This tool reads only -- it changes nothing.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ statusInput) (res *mcp.CallToolResult, out statusOutput, err error) {
		defer recoverToError("get_status", &err)

		out = statusOutput{}
		if deps.Store != nil {
			out.DataDir = deps.Store.BaseDir()
			out.Encrypted = deps.Store.IsEncrypted()
			out.Unlocked = !deps.Store.IsEncrypted() || deps.Store.IsUnlocked()
			if out.Encrypted {
				out.AuthMethod = string(deps.Store.GetAuthMethod())
			}
		} else {
			out.Unlocked = true
			out.Notes = append(out.Notes, "no storage layer is configured on this server; encryption state is unknown")
		}

		if deps.Settings != nil {
			out.Plan = planStatus{
				SettingsDir:    deps.Settings.SettingsDir(),
				Revision:       deps.Settings.Revision(),
				ActiveScenario: deps.Settings.ActiveScenario(),
			}
		}

		if deps.Backups != nil {
			out.Backup.Enabled = deps.Backups.Enabled()
			out.Backup.Dir = deps.Backups.BackupDir()
			meta, metaErr := deps.Backups.Meta()
			if metaErr != nil {
				out.Notes = append(out.Notes, "the backup record could not be read: "+metaErr.Error())
			} else {
				out.Backup.LastBackupTS = meta.TS
				out.Backup.FileCount = meta.FileCount
				out.Backup.TotalBytes = meta.TotalBytes
				out.Backup.Encrypted = meta.Encrypted
				out.Backup.LastAttemptTS = meta.LastAttemptTS
				out.Backup.LastError = meta.LastError
			}
		} else {
			out.Notes = append(out.Notes, "no backup service is configured on this server")
		}

		// Everything below needs to read the data directory. On a locked
		// store that is impossible, and reporting 0 would be a lie the model
		// cannot detect -- so leave the counts null and say why.
		if deps.locked() {
			out.Notes = append(out.Notes,
				"storage is encrypted and locked, so the duplicate queue and CSV inventory could not be counted; unlock via the web UI (/unlock)")
			return nil, out, nil
		}

		if deps.Files != nil {
			infos, ferr := deps.Files.GetFileInfo()
			if ferr != nil {
				out.Notes = append(out.Notes, "the CSV inventory could not be read: "+ferr.Error())
			} else {
				n := len(infos)
				out.CSVFileCount = &n
			}
		}

		if deps.Duplicates != nil {
			// The duplicate queue is recomputed by LoadData and cached on the
			// loader; without this load the count reflects whatever the last
			// page request happened to leave behind, or nothing at all on a
			// server no one has browsed yet.
			if _, lerr := deps.load(); lerr != nil {
				out.Notes = append(out.Notes, "the duplicate queue could not be counted: "+lerr.Error())
			} else {
				n := deps.Duplicates.UnresolvedDuplicateCount()
				out.UnresolvedDuplicates = &n
			}
		}

		return nil, out, nil
	})
}
```

- [ ] **Step 6: Run the test**

Run: `go test ./internal/services/mcpsvc/admin/ -count=1`
Expected: PASS.

- [ ] **Step 7: Wire `admin` into the server**

In `internal/services/mcpsvc/server.go`, add to `Deps`:

```go
	Backups *backup.Service
```

with `"budget2/internal/services/backup"` imported, and register inside the existing `if deps.Loader != nil {` block, after `curate.Register`:

```go
		admin.Register(s, admin.Deps{
			Transactions: deps.Loader,
			Files:        deps.Loader,
			Duplicates:   deps.Loader,
			Decisions:    deps.Loader,
			Store:        deps.Store,
			Settings:     deps.Settings,
			Backups:      deps.Backups,
			// The same data-directory snapshot destination curate uses: both
			// write sidecar JSON files that live in the data dir, and a
			// restore is a hand-copy either way.
			Snapshots: snapshot.New(deps.Loader.CSVDirectory, filepath.Join(deps.SnapshotDir, "data")),
		})
```

A nil `deps.Backups` is not an error — `get_status` reports it in `notes`. But `deps.Settings` being a typed nil `*retirement.SettingsManager` assigned to the `SettingsSource` interface would be non-nil-but-panicking, so guard it the way `NewServer`'s doc comment already describes for `plan`: leave it, and note in the doc comment that a nil `Settings` is a programming error, not a supported configuration.

In `cmd/server/main.go:111-118`, add `Backups: backupService,` to the `mcpsvc.Deps` literal.

- [ ] **Step 8: Verify and commit**

Run: `go build ./... && go test ./... && go vet ./... && staticcheck ./...`
Expected: all green.

```bash
git add internal/services/mcpsvc/ internal/services/backup/meta.go cmd/server/main.go
git commit -m "feat(mcp): add the admin package and get_status

get_status is the one tool that still answers when the store is
encrypted and locked -- which is exactly when every other tool fails and
the model has no way to find out why. Counts it cannot determine in that
state are null with a note, never zero."
```

---

### Task 5: `list_data_files`

**Files:**
- Create: `internal/services/mcpsvc/admin/files.go`
- Create: `internal/services/mcpsvc/admin/files_test.go`
- Modify: `internal/services/mcpsvc/admin/register.go` (`Register`)

**Interfaces:**
- Consumes: `Deps.Files` (`FileLister`), `Deps.locked()` from Task 4.
- Produces: `registerFiles(s *mcp.Server, deps Deps)`.

- [ ] **Step 1: Write the failing test**

Create `internal/services/mcpsvc/admin/files_test.go`:

```go
package admin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListDataFilesReportsEachCSV(t *testing.T) {
	deps, dir := newDeps(t, nil)
	csv := "Date,Description,Amount\n" +
		"2024-01-05,GROCERY STORE,-42.10\n" +
		"2024-03-20,HARDWARE STORE,-88.00\n"
	if err := os.WriteFile(filepath.Join(dir, "checking.csv"), []byte(csv), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[filesOutput](t, call(t, cs, "list_data_files", map[string]any{}))

	if out.Count != 1 {
		t.Fatalf("count = %d, want 1; files = %+v", out.Count, out.Files)
	}
	f := out.Files[0]
	if f.Name != "checking.csv" {
		t.Errorf("name = %q, want checking.csv", f.Name)
	}
	if !f.Enabled {
		t.Error("enabled = false; with no explicit selection every file is loaded")
	}
	if f.Transactions != 2 {
		t.Errorf("transactions = %d, want 2", f.Transactions)
	}
	if f.MinDate != "2024-01-05" || f.MaxDate != "2024-03-20" {
		t.Errorf("date coverage = %s..%s, want 2024-01-05..2024-03-20", f.MinDate, f.MaxDate)
	}
	if f.SizeBytes != int64(len(csv)) {
		t.Errorf("size_bytes = %d, want %d", f.SizeBytes, len(csv))
	}
}

func TestListDataFilesOnAnEmptyDirectoryIsNotAnError(t *testing.T) {
	deps, _ := newDeps(t, nil)
	cs := connect(t, deps)

	out := decodeToolResult[filesOutput](t, call(t, cs, "list_data_files", map[string]any{}))

	if out.Count != 0 {
		t.Errorf("count = %d, want 0", out.Count)
	}
	if out.Note == "" {
		t.Error("note is empty; an empty inventory must explain itself rather than look like a failure")
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `go test ./internal/services/mcpsvc/admin/ -run TestListDataFiles -count=1`
Expected: FAIL to build — `filesOutput` undefined.

- [ ] **Step 3: Write `files.go`**

```go
package admin

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type filesInput struct{}

type dataFile struct {
	Name         string `json:"name"`
	SizeBytes    int64  `json:"size_bytes"`
	Enabled      bool   `json:"enabled"`
	Transactions int    `json:"transactions"`
	MinDate      string `json:"min_date,omitempty"`
	MaxDate      string `json:"max_date,omitempty"`
}

type filesOutput struct {
	Count int        `json:"count"`
	Files []dataFile `json:"files"`
	Note  string     `json:"note,omitempty"`
}

func registerFiles(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_data_files",
		Description: "List the CSV files in the user's data directory, with each one's size, date coverage " +
			"and row count. Use it to answer what periods the ledger actually covers and which bank exports are " +
			"loaded. IMPORTANT: transactions here is a RAW row count from a fast scan of the file, so the sum " +
			"across files will NOT match search_transactions -- loading drops internal transfers, merges exact " +
			"duplicates, and suppresses rows the user resolved as near-duplicates, and rows shared between two " +
			"overlapping exports are counted once per file here but once overall there. Treat it as the size of " +
			"the input, not the size of the ledger. enabled is false only when the user has explicitly narrowed " +
			"the selection on the Explorer page; with no selection every file is enabled. min_date/max_date are " +
			"YYYY-MM-DD and are empty for a file whose dates could not be parsed. This tool reads only -- it " +
			"changes nothing, and it does not tell you whether a file's contents are sound.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ filesInput) (res *mcp.CallToolResult, out filesOutput, err error) {
		defer recoverToError("list_data_files", &err)

		if deps.locked() {
			return nil, filesOutput{}, fmt.Errorf(
				"cannot read the data directory: storage is encrypted and locked; unlock it via the budget2 web UI (/unlock) first")
		}
		if deps.Files == nil {
			return nil, filesOutput{}, fmt.Errorf("no data loader is configured on this server")
		}

		infos, err := deps.Files.GetFileInfo()
		if err != nil {
			return nil, filesOutput{}, fmt.Errorf("read data directory: %w", err)
		}

		out = filesOutput{Files: make([]dataFile, 0, len(infos))}
		for _, i := range infos {
			out.Files = append(out.Files, dataFile{
				Name:         i.Name,
				SizeBytes:    i.Size,
				Enabled:      i.Enabled,
				Transactions: i.Transactions,
				MinDate:      i.MinDate,
				MaxDate:      i.MaxDate,
			})
		}
		out.Count = len(out.Files)
		if out.Count == 0 {
			out.Note = "the data directory contains no CSV files; the user has not imported any bank exports yet"
		}
		return nil, out, nil
	})
}
```

Add `registerFiles(s, deps)` to `Register` in `register.go`.

- [ ] **Step 4: Run the test**

Run: `go test ./internal/services/mcpsvc/admin/ -count=1`
Expected: PASS.

- [ ] **Step 5: Verify and commit**

Run: `go build ./... && go test ./... && go vet ./... && staticcheck ./...`

```bash
git add internal/services/mcpsvc/admin/
git commit -m "feat(mcp): add list_data_files

Its transactions field is a raw per-file scan count, which deliberately
does not reconcile with search_transactions. The description says so
rather than leaving the model to discover it by subtraction."
```

---

### Task 6: `list_duplicates`

**Files:**
- Create: `internal/services/mcpsvc/admin/duplicates.go`
- Create: `internal/services/mcpsvc/admin/duplicates_test.go`
- Modify: `internal/services/mcpsvc/admin/register.go` (`Register`)

**Interfaces:**
- Consumes: `Deps.load()`, `Deps.Duplicates`, `Deps.Decisions`.
- Produces: `registerListDuplicates(s *mcp.Server, deps Deps)`, plus `duplicateRow`, `duplicatePairOut` and `pairsFrom([]dataloader.DuplicatePair, bool) []duplicatePairOut`, which Task 7 reuses to name the available keys in its error message.

The stub `TransactionSource` in `newDeps` bypasses real CSV parsing, but `UnresolvedDuplicates()` is populated by `LoadData` on the *real loader* — a stub cannot fill it. So this task's tests need a loader whose `LoadData` really ran. Add a second constructor to `register_test.go` rather than contorting `newDeps`.

- [ ] **Step 1: Add the real-loader test constructor**

Append to `internal/services/mcpsvc/admin/register_test.go`:

```go
// newLiveDeps builds Deps whose TransactionSource is the SAME real
// *dataloader.DataLoader as Duplicates, over a CSV containing one bill-pay →
// posted-check pair. The duplicate tools read state that only a real LoadData
// stamps, so a stubbed ledger cannot exercise them.
func newLiveDeps(t *testing.T) (Deps, string) {
	t.Helper()
	deps, dir := newDeps(t, nil)
	csv := "Date,Description,Amount,Status\n" +
		"2024-02-03,ACME INSURANCE BILL PAY,-250.00,Scheduled\n" +
		"2024-02-06,CHECK #1042,-250.00,Posted\n" +
		"2024-02-10,GROCERY STORE,-42.10,\n"
	if err := os.WriteFile(filepath.Join(dir, "checking.csv"), []byte(csv), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	loader, ok := deps.Files.(*dataloader.DataLoader)
	if !ok {
		t.Fatalf("Files is %T, want *dataloader.DataLoader", deps.Files)
	}
	deps.Transactions = loader
	return deps, dir
}
```

- [ ] **Step 2: Write the failing test**

Create `internal/services/mcpsvc/admin/duplicates_test.go`:

```go
package admin

import (
	"strings"
	"testing"
)

func TestListDuplicatesReturnsTheDetectedPair(t *testing.T) {
	deps, _ := newLiveDeps(t)
	cs := connect(t, deps)

	out := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{}))

	if out.UnresolvedCount != 1 {
		t.Fatalf("unresolved_count = %d, want 1; pairs = %+v", out.UnresolvedCount, out.Unresolved)
	}
	p := out.Unresolved[0]
	if p.PairKey == "" {
		t.Error("pair_key is empty; resolve_duplicates cannot be called without it")
	}
	amounts := []float64{p.Left.Amount, p.Right.Amount}
	for _, a := range amounts {
		if a != -250.00 {
			t.Errorf("amount = %v, want -250 (signed as stored, expenses negative)", a)
		}
	}
	if p.Left.Hash == "" || p.Right.Hash == "" {
		t.Error("a side is missing its hash; resolve_duplicates needs both to name a winner")
	}
	if p.Left.Hash == p.Right.Hash {
		t.Error("both sides share a hash; they are not two distinct transactions")
	}
	if out.ResolvedCount != 0 {
		t.Errorf("resolved_count = %d, want 0 before any decision", out.ResolvedCount)
	}
}

// The store in a t.TempDir() is never encrypted, so the locked path cannot be
// reached here without building an encrypted fixture. What this test pins is
// the weaker but still load-bearing half: a load failure surfaces as a tool
// error, NOT as an empty queue. An empty queue would tell the model the user
// has nothing to review, which is the opposite of "we could not look".
func TestListDuplicatesReportsALoadFailureRatherThanAnEmptyQueue(t *testing.T) {
	deps, _ := newLiveDeps(t)
	deps.Transactions = stubTransactions{err: errNoLedger}
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "list_duplicates", map[string]any{}))
	if !strings.Contains(msg, errNoLedger.Error()) {
		t.Errorf("error text %q does not carry the underlying cause %q", msg, errNoLedger)
	}
}
```

Declare the sentinel next to the stub in `register_test.go`:

```go
var errNoLedger = errors.New("ledger unavailable")
```

- [ ] **Step 3: Run it and confirm it fails**

Run: `go test ./internal/services/mcpsvc/admin/ -run TestListDuplicates -count=1`
Expected: FAIL to build — `duplicatesOutput` undefined.

- [ ] **Step 4: Write `duplicates.go`**

```go
package admin

import (
	"context"
	"fmt"
	"math"

	"budget2/internal/services/dataloader"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type duplicatesInput struct {
	IncludeResolved bool `json:"include_resolved,omitempty" jsonschema:"also return pairs the user has already resolved as kept_winner"`
}

// duplicateRow is one side of a candidate pair. amount is SIGNED exactly as
// stored, matching search_transactions and the curate tools.
type duplicateRow struct {
	Hash        string  `json:"hash"`
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
	Status      string  `json:"status,omitempty"`
}

type duplicatePairOut struct {
	PairKey string       `json:"pair_key"`
	Left    duplicateRow `json:"left"`
	Right   duplicateRow `json:"right"`
}

type duplicatesOutput struct {
	UnresolvedCount int                `json:"unresolved_count"`
	Unresolved      []duplicatePairOut `json:"unresolved"`
	ResolvedCount   int                `json:"resolved_count"`
	Resolved        []duplicatePairOut `json:"resolved,omitempty"`
	Note            string             `json:"note,omitempty"`
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func rowFor(t models.Transaction) duplicateRow {
	return duplicateRow{
		Hash:        t.Hash,
		Date:        t.Date.Format("2006-01-02"),
		Description: t.Label(),
		Amount:      round2(t.Amount),
		Status:      t.Status,
	}
}

// pairsFrom shapes detection results for output. Order is detection order,
// which is stable for a given ledger.
func pairsFrom(pairs []dataloader.DuplicatePair) []duplicatePairOut {
	out := make([]duplicatePairOut, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, duplicatePairOut{
			PairKey: p.Key,
			Left:    rowFor(p.Left),
			Right:   rowFor(p.Right),
		})
	}
	return out
}

func registerListDuplicates(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_duplicates",
		Description: "List pairs of transactions that look like the same payment recorded twice -- a scheduled " +
			"bill pay and a posted check for the identical amount within 7 days -- and are waiting for the user " +
			"to decide between them. This is the queue behind the app's Duplicates page. Each pair has a " +
			"pair_key, which is what resolve_duplicates and undo_resolve take; the two sides are called left and " +
			"right and the order carries NO meaning about which is the real charge. amount is the SIGNED amount " +
			"exactly as stored (an expense is negative), matching search_transactions for the same transaction. " +
			"Both sides are still LIVE and both are still counted in every spending total until the user " +
			"resolves the pair, so an unresolved queue means the app is currently double-counting those " +
			"amounts. Set include_resolved to also see pairs already settled as kept_winner, whose losing side " +
			"is excluded from every total. This tool reads only -- it changes nothing.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in duplicatesInput) (res *mcp.CallToolResult, out duplicatesOutput, err error) {
		defer recoverToError("list_duplicates", &err)

		if deps.Duplicates == nil {
			return nil, duplicatesOutput{}, fmt.Errorf("no data loader is configured on this server")
		}
		// Detection results are recomputed and cached by LoadData; without
		// this the tool reports whatever the last page request left behind.
		if _, err := deps.load(); err != nil {
			return nil, duplicatesOutput{}, err
		}

		unresolved := pairsFrom(deps.Duplicates.UnresolvedDuplicates())
		out = duplicatesOutput{
			UnresolvedCount: len(unresolved),
			Unresolved:      unresolved,
		}
		resolved := deps.Duplicates.ResolvedDuplicates()
		out.ResolvedCount = len(resolved)
		if in.IncludeResolved {
			out.Resolved = pairsFrom(resolved)
		}
		if out.UnresolvedCount == 0 {
			out.Note = "nothing is waiting for review; no candidate pairs were detected, or the user has already decided every one"
		}
		return nil, out, nil
	})
}
```

Add `"budget2/internal/models"` to the imports, and `registerListDuplicates(s, deps)` to `Register`.

- [ ] **Step 5: Run the test**

Run: `go test ./internal/services/mcpsvc/admin/ -count=1`
Expected: PASS.

- [ ] **Step 6: Verify and commit**

Run: `go build ./... && go test ./... && go vet ./... && staticcheck ./...`

```bash
git add internal/services/mcpsvc/admin/
git commit -m "feat(mcp): add list_duplicates

Loads before reading the queue: the unresolved/resolved pairs are
recomputed and cached by LoadData, so a tool that skipped the load would
report whatever the last browser request happened to leave behind."
```

---

### Task 7: `resolve_duplicates`

The first write in this package. The `pair_key` comes from a model, so it is validated against the live unresolved set before anything is written — the direct lesson from `upsert_major_expense`'s unvalidated `pin_hash`.

**Files:**
- Create: `internal/services/mcpsvc/admin/resolve.go`
- Create: `internal/services/mcpsvc/admin/resolve_test.go`
- Modify: `internal/services/mcpsvc/admin/register.go` (`Register`)

**Interfaces:**
- Consumes: `Deps.Decisions`, `Deps.Duplicates`, `Deps.Snapshots`, `pairsFrom` and `duplicateDecisionsFile` from Tasks 4 and 6.
- Produces: `registerResolve(s *mcp.Server, deps Deps)`, plus `findPair(pairs []dataloader.DuplicatePair, key string) (dataloader.DuplicatePair, bool)` reused by Task 8.

- [ ] **Step 1: Write the failing tests**

Create `internal/services/mcpsvc/admin/resolve_test.go`:

```go
package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pendingPairKey resolves the one candidate pair newLiveDeps creates.
func pendingPairKey(t *testing.T, cs *mcp.ClientSession) (key, keptHash, suppressedHash string) {
	t.Helper()
	out := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{}))
	if out.UnresolvedCount != 1 {
		t.Fatalf("unresolved_count = %d, want 1", out.UnresolvedCount)
	}
	p := out.Unresolved[0]
	return p.PairKey, p.Left.Hash, p.Right.Hash
}

func TestResolveDuplicatesWritesTheDecision(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)
	key, kept, suppressed := pendingPairKey(t, cs)

	out := decodeToolResult[resolveOutput](t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key":        key,
		"outcome":         "kept_winner",
		"kept_hash":       kept,
		"suppressed_hash": suppressed,
	}))

	if out.PairKey != key {
		t.Errorf("pair_key = %q, want %q", out.PairKey, key)
	}
	if len(out.SnapshotPaths) == 0 && out.Note == "" {
		t.Error("neither a snapshot path nor a note explaining its absence")
	}
	if out.UnresolvedRemaining != 0 {
		t.Errorf("unresolved_remaining = %d, want 0", out.UnresolvedRemaining)
	}

	data, err := os.ReadFile(filepath.Join(dir, "duplicate_decisions.json"))
	if err != nil {
		t.Fatalf("read decisions file: %v", err)
	}
	if !strings.Contains(string(data), key) {
		t.Errorf("decisions file does not mention the pair key:\n%s", data)
	}
	if !strings.Contains(string(data), "kept_winner") {
		t.Errorf("decisions file does not record the outcome:\n%s", data)
	}
}

func TestResolveDuplicatesRejectsAnUnknownPairKey(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key":        "deadbeefdeadbeef",
		"outcome":         "kept_both",
	}))
	if !strings.Contains(msg, "deadbeefdeadbeef") {
		t.Errorf("error does not name the rejected key: %s", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "duplicate_decisions.json")); !os.IsNotExist(err) {
		t.Errorf("a decisions file was written for an unknown key (stat err = %v)", err)
	}
}

func TestResolveDuplicatesRejectsAHashNotInThePair(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)
	key, kept, _ := pendingPairKey(t, cs)

	msg := toolErrorText(t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key":        key,
		"outcome":         "kept_winner",
		"kept_hash":       kept,
		"suppressed_hash": "not-in-this-pair",
	}))
	if !strings.Contains(msg, "not-in-this-pair") {
		t.Errorf("error does not name the offending hash: %s", msg)
	}
	if _, err := os.Stat(filepath.Join(dir, "duplicate_decisions.json")); !os.IsNotExist(err) {
		t.Errorf("a decisions file was written for a hash outside the pair (stat err = %v)", err)
	}
}

func TestResolveDuplicatesRejectsAnUnknownOutcome(t *testing.T) {
	deps, _ := newLiveDeps(t)
	cs := connect(t, deps)
	key, _, _ := pendingPairKey(t, cs)

	msg := toolErrorText(t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key": key,
		"outcome":  "kept_neither",
	}))
	if !strings.Contains(msg, "kept_neither") {
		t.Errorf("error does not name the rejected outcome: %s", msg)
	}
}
```

`mcpClientSession` in the helper signature is a placeholder: use the concrete `*mcp.ClientSession` returned by `connect`, importing `"github.com/modelcontextprotocol/go-sdk/mcp"`.

- [ ] **Step 2: Run and confirm they fail**

Run: `go test ./internal/services/mcpsvc/admin/ -run TestResolveDuplicates -count=1`
Expected: FAIL to build — `resolveOutput` undefined.

- [ ] **Step 3: Write `resolve.go`**

```go
package admin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"budget2/internal/services/dataloader"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type resolveInput struct {
	PairKey        string `json:"pair_key" jsonschema:"the pair_key from list_duplicates"`
	Outcome        string `json:"outcome" jsonschema:"kept_winner to keep one side and exclude the other from every total, or kept_both to declare them two genuinely separate payments"`
	KeptHash       string `json:"kept_hash,omitempty" jsonschema:"required for kept_winner: the hash of the side to KEEP, from list_duplicates"`
	SuppressedHash string `json:"suppressed_hash,omitempty" jsonschema:"required for kept_winner: the hash of the side to EXCLUDE, from list_duplicates"`
}

type resolveOutput struct {
	PairKey             string   `json:"pair_key"`
	Outcome             string   `json:"outcome"`
	SuppressedHash      string   `json:"suppressed_hash,omitempty"`
	UnresolvedRemaining int      `json:"unresolved_remaining"`
	SnapshotPaths       []string `json:"snapshot_paths,omitempty"`
	Note                string   `json:"note,omitempty"`
}

// findPair locates a candidate pair by key.
func findPair(pairs []dataloader.DuplicatePair, key string) (dataloader.DuplicatePair, bool) {
	for _, p := range pairs {
		if p.Key == key {
			return p, true
		}
	}
	return dataloader.DuplicatePair{}, false
}

// availableKeys renders the currently-resolvable keys for an error message, so
// a model handed a stale or invented key can correct itself in one turn.
func availableKeys(pairs []dataloader.DuplicatePair) string {
	if len(pairs) == 0 {
		return "no pairs are currently awaiting review"
	}
	keys := make([]string, 0, len(pairs))
	for _, p := range pairs {
		keys = append(keys, p.Key)
	}
	return "currently awaiting review: " + strings.Join(keys, ", ")
}

// ensureDecisionsSnapshot copies duplicate_decisions.json aside before a
// write. A file that does not exist yet is not an error -- a first decision
// on a fresh install has nothing to lose -- but any OTHER failure aborts,
// because it is not evidence the file is absent and overwriting it would be
// unrecoverable. This mirrors curate/delete.go.
func ensureDecisionsSnapshot(deps Deps) (paths []string, note string, err error) {
	if deps.Snapshots == nil {
		return nil, "", fmt.Errorf("refusing to write: no snapshot directory is configured on this server")
	}
	p, err := deps.Snapshots.Ensure(duplicateDecisionsFile, time.Now())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "no .bak was taken: " + duplicateDecisionsFile + " did not exist yet, so there was no prior state to protect", nil
		}
		return nil, "", fmt.Errorf("refusing to write: could not back up %s: %w", duplicateDecisionsFile, err)
	}
	return []string{p}, "", nil
}

func registerResolve(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "resolve_duplicates",
		Description: "Settle one pair from list_duplicates. THIS WRITES TO THE USER'S DATA. Two outcomes: " +
			"kept_winner means the two rows are the same payment recorded twice -- the suppressed_hash side is " +
			"then EXCLUDED from every spending total, trend and analysis app-wide, though the row itself is " +
			"never deleted from the CSV; kept_both means they are genuinely two separate payments that happen " +
			"to match, and both stay counted, with the pair no longer re-flagged. kept_winner requires both " +
			"kept_hash and suppressed_hash and both must belong to THIS pair -- a hash from anywhere else is " +
			"refused, not written. kept_both ignores the hashes. A pair_key that is not currently awaiting " +
			"review is refused with the list of keys that are, so do not guess one: call list_duplicates first. " +
			"undo_resolve reverses this exactly. duplicate_decisions.json is copied to a .bak before this " +
			"session's first change to it. An already-open Duplicates page does NOT refresh itself -- it shows " +
			"stale data until reloaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in resolveInput) (res *mcp.CallToolResult, out resolveOutput, err error) {
		defer recoverToError("resolve_duplicates", &err)

		key := strings.TrimSpace(in.PairKey)
		if key == "" {
			return nil, resolveOutput{}, fmt.Errorf("pair_key is required; call list_duplicates for the current keys")
		}
		if deps.Duplicates == nil || deps.Decisions == nil {
			return nil, resolveOutput{}, fmt.Errorf("no data loader is configured on this server")
		}

		outcome := strings.TrimSpace(in.Outcome)
		switch outcome {
		case dataloader.DuplicateOutcomeKeptWinner, dataloader.DuplicateOutcomeKeptBoth:
		default:
			return nil, resolveOutput{}, fmt.Errorf(
				"outcome %q is not one this app understands; use %q or %q",
				in.Outcome, dataloader.DuplicateOutcomeKeptWinner, dataloader.DuplicateOutcomeKeptBoth)
		}

		// Refresh detection, then validate the key against what is actually
		// pending. Writing a decision for a key no pair carries would leave a
		// dead entry the user can never see or clear from the page.
		if _, err := deps.load(); err != nil {
			return nil, resolveOutput{}, err
		}
		pending := deps.Duplicates.UnresolvedDuplicates()
		pair, ok := findPair(pending, key)
		if !ok {
			return nil, resolveOutput{}, fmt.Errorf(
				"pair_key %q is not awaiting review (it may already be resolved, or it may not exist); %s",
				key, availableKeys(pending))
		}

		decision := dataloader.DuplicateDecision{Outcome: outcome}
		if outcome == dataloader.DuplicateOutcomeKeptWinner {
			kept := strings.TrimSpace(in.KeptHash)
			suppressed := strings.TrimSpace(in.SuppressedHash)
			if kept == "" || suppressed == "" {
				return nil, resolveOutput{}, fmt.Errorf(
					"kept_winner requires both kept_hash and suppressed_hash; this pair's sides are %s and %s",
					pair.Left.Hash, pair.Right.Hash)
			}
			if kept == suppressed {
				return nil, resolveOutput{}, fmt.Errorf(
					"kept_hash and suppressed_hash are the same transaction (%s); they must be the two different sides of the pair", kept)
			}
			for _, h := range []string{kept, suppressed} {
				if h != pair.Left.Hash && h != pair.Right.Hash {
					return nil, resolveOutput{}, fmt.Errorf(
						"hash %q does not belong to pair %s; its two sides are %s and %s",
						h, key, pair.Left.Hash, pair.Right.Hash)
				}
			}
			decision.KeptHash = kept
			decision.SuppressedHash = suppressed
		}

		paths, note, err := ensureDecisionsSnapshot(deps)
		if err != nil {
			return nil, resolveOutput{}, err
		}

		if err := deps.Decisions.SaveDuplicateDecision(key, decision); err != nil {
			return nil, resolveOutput{}, err
		}

		out = resolveOutput{
			PairKey:        key,
			Outcome:        outcome,
			SuppressedHash: decision.SuppressedHash,
			SnapshotPaths:  paths,
			Note:           note,
		}
		// Re-load so the reported remainder reflects the decision just made.
		if _, err := deps.load(); err == nil {
			out.UnresolvedRemaining = deps.Duplicates.UnresolvedDuplicateCount()
		}
		return nil, out, nil
	})
}
```

Add `registerResolve(s, deps)` to `Register`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/services/mcpsvc/admin/ -count=1`
Expected: PASS.

- [ ] **Step 5: Prove the validation tests discriminate**

Temporarily delete the `for _, h := range []string{kept, suppressed}` membership check. Run `go test ./internal/services/mcpsvc/admin/ -run TestResolveDuplicatesRejectsAHashNotInThePair -count=1`.
Expected: FAIL. Restore, confirm PASS.

Then temporarily delete the `if !ok {` unknown-key branch (returning the zero `pair` instead). Run `go test ./internal/services/mcpsvc/admin/ -run TestResolveDuplicatesRejectsAnUnknownPairKey -count=1`.
Expected: FAIL. Restore, confirm PASS. **Report both mutations and both outputs.**

- [ ] **Step 6: Verify and commit**

Run: `go build ./... && go test ./... && go vet ./... && staticcheck ./...`

```bash
git add internal/services/mcpsvc/admin/
git commit -m "feat(mcp): add resolve_duplicates

The pair_key and both hashes come from a model, so all three are checked
against the live detection results before anything is written -- the
lesson from upsert_major_expense's pin_hash, which writes a dead key
because it never validated one. A rejected key comes back with the list
of keys that would have worked."
```

---

### Task 8: `undo_resolve`

**Files:**
- Create: `internal/services/mcpsvc/admin/undo.go`
- Create: `internal/services/mcpsvc/admin/undo_test.go`
- Modify: `internal/services/mcpsvc/admin/register.go` (`Register`)

**Interfaces:**
- Consumes: `Deps.Decisions`, `ensureDecisionsSnapshot`, `Deps.load()`.
- Produces: `registerUndo(s *mcp.Server, deps Deps)`.

`ClearDuplicateDecision` is a silent no-op for a key with no decision (`duplicate_decisions.go:103-105`). A tool that reported success in that case would tell a model it undid something it did not. So the tool checks the decisions map first and errors instead.

- [ ] **Step 1: Write the failing tests**

Create `internal/services/mcpsvc/admin/undo_test.go`:

```go
package admin

import (
	"strings"
	"testing"
)

func TestUndoResolveRestoresThePairToTheQueue(t *testing.T) {
	deps, _ := newLiveDeps(t)
	cs := connect(t, deps)
	key, kept, suppressed := pendingPairKey(t, cs)

	_ = decodeToolResult[resolveOutput](t, call(t, cs, "resolve_duplicates", map[string]any{
		"pair_key":        key,
		"outcome":         "kept_winner",
		"kept_hash":       kept,
		"suppressed_hash": suppressed,
	}))

	out := decodeToolResult[undoOutput](t, call(t, cs, "undo_resolve", map[string]any{"pair_key": key}))

	if out.PairKey != key {
		t.Errorf("pair_key = %q, want %q", out.PairKey, key)
	}
	if out.PreviousOutcome != "kept_winner" {
		t.Errorf("previous_outcome = %q, want kept_winner", out.PreviousOutcome)
	}
	if out.UnresolvedRemaining != 1 {
		t.Errorf("unresolved_remaining = %d, want 1 -- the pair should be back in the queue", out.UnresolvedRemaining)
	}

	after := decodeToolResult[duplicatesOutput](t, call(t, cs, "list_duplicates", map[string]any{}))
	if after.UnresolvedCount != 1 {
		t.Errorf("list_duplicates unresolved_count = %d after undo, want 1", after.UnresolvedCount)
	}
}

func TestUndoResolveRefusesAPairWithNoDecision(t *testing.T) {
	deps, _ := newLiveDeps(t)
	cs := connect(t, deps)
	key, _, _ := pendingPairKey(t, cs)

	msg := toolErrorText(t, call(t, cs, "undo_resolve", map[string]any{"pair_key": key}))
	if !strings.Contains(msg, key) {
		t.Errorf("error does not name the key: %s", msg)
	}
}
```

- [ ] **Step 2: Run and confirm they fail**

Run: `go test ./internal/services/mcpsvc/admin/ -run TestUndoResolve -count=1`
Expected: FAIL to build — `undoOutput` undefined.

- [ ] **Step 3: Write `undo.go`**

```go
package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type undoInput struct {
	PairKey string `json:"pair_key" jsonschema:"the pair_key of a pair that was already resolved"`
}

type undoOutput struct {
	PairKey             string   `json:"pair_key"`
	PreviousOutcome     string   `json:"previous_outcome"`
	UnresolvedRemaining int      `json:"unresolved_remaining"`
	SnapshotPaths       []string `json:"snapshot_paths,omitempty"`
	Note                string   `json:"note,omitempty"`
}

func registerUndo(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "undo_resolve",
		Description: "Reverse one resolve_duplicates decision, putting the pair back in the review queue. " +
			"THIS WRITES TO THE USER'S DATA. It is the exact inverse: a suppressed transaction becomes live " +
			"again and re-enters every spending total, and the pair is re-flagged for review. This is the app's " +
			"own Undo button, not a general undo -- it reverses a duplicate decision and nothing else. A " +
			"pair_key with no decision recorded against it is refused rather than silently succeeding, so a " +
			"success here always means something actually changed. duplicate_decisions.json is copied to a .bak " +
			"before this session's first change to it. An already-open Duplicates page does NOT refresh itself " +
			"-- it shows stale data until reloaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in undoInput) (res *mcp.CallToolResult, out undoOutput, err error) {
		defer recoverToError("undo_resolve", &err)

		key := strings.TrimSpace(in.PairKey)
		if key == "" {
			return nil, undoOutput{}, fmt.Errorf("pair_key is required; call list_duplicates with include_resolved to see what can be undone")
		}
		if deps.Decisions == nil {
			return nil, undoOutput{}, fmt.Errorf("no data loader is configured on this server")
		}

		// ClearDuplicateDecision is a silent no-op for an unknown key, which
		// would let this tool claim it undid something it did not.
		decisions, err := deps.Decisions.LoadDuplicateDecisions()
		if err != nil {
			return nil, undoOutput{}, err
		}
		prior, ok := decisions[key]
		if !ok {
			return nil, undoOutput{}, fmt.Errorf(
				"pair_key %q has no decision recorded against it, so there is nothing to undo; call list_duplicates with include_resolved to see what does", key)
		}

		paths, note, err := ensureDecisionsSnapshot(deps)
		if err != nil {
			return nil, undoOutput{}, err
		}

		if err := deps.Decisions.ClearDuplicateDecision(key); err != nil {
			return nil, undoOutput{}, err
		}

		out = undoOutput{
			PairKey:         key,
			PreviousOutcome: prior.Outcome,
			SnapshotPaths:   paths,
			Note:            note,
		}
		if _, err := deps.load(); err == nil && deps.Duplicates != nil {
			out.UnresolvedRemaining = deps.Duplicates.UnresolvedDuplicateCount()
		}
		return nil, out, nil
	})
}
```

Add `registerUndo(s, deps)` to `Register`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/services/mcpsvc/admin/ -count=1`
Expected: PASS.

- [ ] **Step 5: Prove the no-decision test discriminates**

Temporarily delete the `if !ok {` branch. Run `go test ./internal/services/mcpsvc/admin/ -run TestUndoResolveRefusesAPairWithNoDecision -count=1`.
Expected: FAIL — the call now succeeds and returns no error text. Restore, confirm PASS. Report both outputs.

- [ ] **Step 6: Verify and commit**

Run: `go build ./... && go test ./... && go vet ./... && staticcheck ./...`

```bash
git add internal/services/mcpsvc/admin/
git commit -m "feat(mcp): add undo_resolve

ClearDuplicateDecision is a silent no-op for an unknown key, so the tool
checks the decisions map first: a success from undo_resolve always means
something actually changed."
```

---

### Task 9: `run_backup`

**Files:**
- Create: `internal/services/mcpsvc/admin/backup.go`
- Create: `internal/services/mcpsvc/admin/backup_test.go`
- Modify: `internal/services/mcpsvc/admin/register.go` (`Register`)

**Interfaces:**
- Consumes: `Deps.Backups` (`BackupService`).
- Produces: `registerRunBackup(s *mcp.Server, deps Deps)`.

This tool writes, but only into the backup directory — it never touches the user's data — so it takes no `Snapshotter`. `Service.Snapshot` is not gated on `Enabled()` (only `SnapshotIfStale` is), so an explicit call works with auto-backup off; the output reports the toggle so the model does not mistake one manual backup for a restored schedule.

- [ ] **Step 1: Write the failing test**

Create `internal/services/mcpsvc/admin/backup_test.go`:

```go
package admin

import (
	"path/filepath"
	"testing"
)

func TestRunBackupWritesAnArchive(t *testing.T) {
	deps, dir := newLiveDeps(t)
	cs := connect(t, deps)

	out := decodeToolResult[runBackupOutput](t, call(t, cs, "run_backup", map[string]any{}))

	if !out.Ran {
		t.Fatalf("ran = false, note = %q", out.Note)
	}
	if out.TS == "" {
		t.Error("ts is empty; a successful backup records its timestamp")
	}
	if out.FileCount == 0 {
		t.Error("file_count = 0; the data directory had at least one CSV")
	}
	matches, err := filepath.Glob(filepath.Join(dir, "backups", "budget_backup_*.zip"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("archives on disk = %d, want 1", len(matches))
	}
	if out.Dir != filepath.Join(dir, "backups") {
		t.Errorf("dir = %q, want %q", out.Dir, filepath.Join(dir, "backups"))
	}
}

func TestRunBackupReportsAnInProgressSnapshot(t *testing.T) {
	deps, _ := newLiveDeps(t)
	deps.Backups = busyBackups{inner: deps.Backups}
	cs := connect(t, deps)

	out := decodeToolResult[runBackupOutput](t, call(t, cs, "run_backup", map[string]any{}))

	if out.Ran {
		t.Error("ran = true, want false when a snapshot is already in flight")
	}
	if out.Note == "" {
		t.Error("note is empty; a skipped backup must say why")
	}
}
```

Add the stub to `register_test.go`:

```go
// busyBackups reports every snapshot as already in flight, the condition the
// real service signals when the scheduler tick and a manual run collide.
type busyBackups struct{ inner BackupService }

func (b busyBackups) BackupDir() string                { return b.inner.BackupDir() }
func (b busyBackups) DataDir() string                  { return b.inner.DataDir() }
func (b busyBackups) Enabled() bool                    { return b.inner.Enabled() }
func (b busyBackups) Meta() (backupsvc.Meta, error)    { return b.inner.Meta() }
func (b busyBackups) Snapshot(ctx context.Context) error { return backupsvc.ErrSnapshotInProgress }
```

with `"context"` imported by the test file.

- [ ] **Step 2: Run and confirm it fails**

Run: `go test ./internal/services/mcpsvc/admin/ -run TestRunBackup -count=1`
Expected: FAIL to build — `runBackupOutput` undefined.

- [ ] **Step 3: Write `backup.go`**

```go
package admin

import (
	"context"
	"errors"
	"fmt"

	backupsvc "budget2/internal/services/backup"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type runBackupInput struct{}

type runBackupOutput struct {
	Ran        bool   `json:"ran"`
	Dir        string `json:"dir"`
	TS         string `json:"ts,omitempty"`
	FileCount  int    `json:"file_count,omitempty"`
	TotalBytes int64  `json:"total_bytes,omitempty"`
	Encrypted  bool   `json:"encrypted"`
	AutoEnabled bool  `json:"auto_backup_enabled"`
	Note       string `json:"note,omitempty"`
}

func registerRunBackup(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "run_backup",
		Description: "Take one backup of the user's data directory right now: a timestamped, verified zip " +
			"written into the backup directory, which is OUTSIDE the data directory. This adds a file; it " +
			"changes nothing about the user's data and cannot lose anything. Use it before suggesting a change " +
			"the user might want to walk back. It is a full snapshot, not incremental, and old archives are " +
			"pruned by the app's retention policy, so running it repeatedly costs disk. If a scheduled backup " +
			"is already in flight this returns ran=false with a note rather than queuing a second one -- that " +
			"is a skip, not a failure. auto_backup_enabled reports whether the app also backs up on its own " +
			"schedule; a manual run here does NOT turn that back on if the user disabled it. When the data is " +
			"encrypted the archive contains ciphertext, so restoring it needs the same key.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ runBackupInput) (res *mcp.CallToolResult, out runBackupOutput, err error) {
		defer recoverToError("run_backup", &err)

		if deps.Backups == nil {
			return nil, runBackupOutput{}, fmt.Errorf("no backup service is configured on this server")
		}

		out = runBackupOutput{
			Dir:         deps.Backups.BackupDir(),
			AutoEnabled: deps.Backups.Enabled(),
		}

		if err := deps.Backups.Snapshot(ctx); err != nil {
			if errors.Is(err, backupsvc.ErrSnapshotInProgress) {
				out.Note = "a backup was already running, so this call did not start a second one; the in-flight backup is still completing"
				return nil, out, nil
			}
			return nil, runBackupOutput{}, fmt.Errorf("backup failed: %w", err)
		}

		out.Ran = true
		meta, metaErr := deps.Backups.Meta()
		if metaErr != nil {
			out.Note = "the backup completed but its record could not be read back: " + metaErr.Error()
			return nil, out, nil
		}
		out.TS = meta.TS
		out.FileCount = meta.FileCount
		out.TotalBytes = meta.TotalBytes
		out.Encrypted = meta.Encrypted
		return nil, out, nil
	})
}
```

Add `registerRunBackup(s, deps)` to `Register`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/services/mcpsvc/admin/ -count=1`
Expected: PASS.

- [ ] **Step 5: Verify and commit**

Run: `go build ./... && go test ./... && go vet ./... && staticcheck ./...`

```bash
git add internal/services/mcpsvc/admin/
git commit -m "feat(mcp): add run_backup

ErrSnapshotInProgress is a skip, not a failure: it returns ran=false
with a note rather than surfacing an error the model would report as
'the backup failed'."
```

---

### Task 10: Tell the model, and the reader, that these tools exist

Six tools registered but undescribed at the server level is exactly the gap `serverInstructions` exists to close. It currently ends by describing the curation tools and claims the spending list is "the COMPLETE list of all six SPENDING tools" — still true, but the sentence now sits in a server offering 23.

**Files:**
- Modify: `internal/services/mcpsvc/server.go:37-75` (`serverInstructions`), `:77-82` (`NewServer` doc comment)
- Modify: `internal/services/mcpsvc/server_test.go`
- Modify: `docs/superpowers/specs/2026-08-12-app-wide-mcp-design.md` (Phases list, "Serializing the data writes", "Carried out of phase 3")
- Modify: `README.md` (the MCP section, if it enumerates tools — check first)

- [ ] **Step 1: Extend the existing registration test**

`internal/services/mcpsvc/server_test.go` already has `TestNewServerRegistersAllSeventeenTools`, which asserts each name is present *and* that the total is exactly 17 — so it is already failing after Task 9 with `expected exactly 17 tools, got 23`. Do not add a second registration test. Rename it and extend both halves:

```go
func TestNewServerRegistersAllTwentyThreeTools(t *testing.T) {
```

and inside, add the six names to the `want` slice and correct the count:

```go
	for _, want := range []string{
		"list_scenarios", "get_analysis", "get_months", "run_scenario", "open_page", "apply_changes",
		"get_anomalies", "get_price_creep", "search_transactions", "summarize_spending", "get_recurring",
		"get_trends", "list_major_expenses", "list_exceptions", "pin_transactions", "upsert_major_expense",
		"delete_major_expense",
		"get_status", "list_data_files", "list_duplicates", "resolve_duplicates", "undo_resolve",
		"run_backup",
	} {
		if !got[want] {
			t.Errorf("tool %q not registered; got %v", want, toolNames(res.Tools))
		}
	}
	if len(res.Tools) != 23 {
		t.Errorf("expected exactly 23 tools, got %d: %v", len(res.Tools), toolNames(res.Tools))
	}
```

Also update the test's doc comment: its explanation of why `Deps{}` is not used ("with a nil Loader, NewServer's own `if deps.Loader != nil` guard skips `spend.Register` entirely") now covers `admin.Register` too, since Task 4 put it inside the same guard. Say so, or the next reader will assume `admin` is registered unconditionally.

- [ ] **Step 2: Run it and confirm the count assertion drove the change**

Run: `go test ./internal/services/mcpsvc/ -run TestNewServerRegistersAll -count=1`
Expected: PASS after the edit. Then delete `"get_status"` from the `want` slice and re-run: expected FAIL on the missing-name branch. Restore it, then change `23` to `24` and re-run: expected FAIL on the count branch. Both directions must fire. Restore and confirm PASS.

- [ ] **Step 3: Extend `serverInstructions`**

Append to the constant, after the curation paragraph:

```go
	" Finally, six HOUSEKEEPING tools describe the app itself rather than the money in it. get_status is " +
	"the one to call FIRST when another tool fails inexplicably: if the user's data is encrypted and " +
	"currently locked, every ledger-reading tool fails and get_status is the only one that still answers. " +
	"list_data_files inventories the bank exports on disk; its per-file row counts are raw and do NOT sum " +
	"to search_transactions' totals. list_duplicates is the queue of transaction pairs that look like one " +
	"payment recorded twice -- while a pair is unresolved BOTH sides are counted, so an unresolved queue " +
	"means the spending totals are inflated by those amounts, and saying so is often more useful than the " +
	"totals themselves. resolve_duplicates and undo_resolve WRITE TO THE USER'S DATA and exactly reverse " +
	"each other; confirm with the user before calling either, and never invent a pair_key -- call " +
	"list_duplicates and use one from there. run_backup adds a zip to the backup directory and changes " +
	"nothing else, so it is safe to call before suggesting anything the user might want to walk back."
```

Update the `NewServer` doc comment to mention that a nil `Backups` degrades `get_status` and disables nothing else, and that a nil `Settings` is a programming error rather than a supported configuration.

- [ ] **Step 4: Pin the new load-bearing claims**

`TestServerInstructionsCarryLoadBearingClaims` in the same file exists so that changing a tool's behavior without updating the text fails a test. Every claim added in Step 3 that a future edit could invalidate goes in its `want` list:

```go
		// The six housekeeping tools: which read, which write, and the two
		// claims a behavior change would silently falsify.
		"six HOUSEKEEPING tools",
		"get_status is the one to call FIRST",
		"the only one that still answers",
		"do NOT sum to search_transactions' totals",
		"while a pair is unresolved BOTH sides are counted",
		"resolve_duplicates and undo_resolve WRITE TO THE USER'S DATA",
		"never invent a pair_key",
		"run_backup adds a zip to the backup directory and changes nothing else",
```

Note the existing entry `"COMPLETE list of all six SPENDING tools"` stays correct — this phase adds no spend tool — but confirm by reading the sentence in context that "six" is still scoped to spending and cannot be misread as the whole surface now that a second group of six exists. If it can, disambiguate the wording and update the pinned string to match.

Run: `go test ./internal/services/mcpsvc/ -count=1`
Expected: PASS. Then delete one sentence from `serverInstructions` and confirm the test fails naming the missing claim; restore.

- [ ] **Step 5: Update the design doc**

In `docs/superpowers/specs/2026-08-12-app-wide-mcp-design.md`:

- Mark phase 4 in the **Phases** list as split: `4a` **Implemented** — write serialization plus the six housekeeping tools; `4b` — the guarded operations, blocked on extracting `restoreFromZip` and the encryption handlers out of `internal/handlers/backup`.
- Rewrite **"Serializing the data writes"** in the past tense, and record what the fix actually turned out to be: two mutexes, not one — `writeMu` for the load→modify→save sequences and `stateMu` for the derived fields `LoadData` stamps, the second of which the section did not anticipate and which was a live `-race` failure between two ordinary browser requests. Record that the crash-consistent write order question it raised was settled by leaving the archive → active → pins order as is: it now protects only against a crash, since an interleaving writer is no longer possible.
- Add to **"Carried out of phase 3"**, or a new "Carried out of phase 4a" section: `set_encryption` is descoped permanently, not deferred — enabling encryption requires a credential that would travel through a tool argument into a model's transcript, and the state is reported read-only by `get_status` instead.
- Note that `ClearTransactionPin` is the one `dataloader` write method that deliberately does not take `writeMu`, because it delegates to the public `SetTransactionPin`, which does.

- [ ] **Step 6: Check the README**

Read the MCP section of `README.md`. If it enumerates the tool surface, add the six; if it only describes the endpoint and `.mcp.json`, leave it alone. Report which.

- [ ] **Step 7: Verify and commit**

Run: `go build ./... && go test ./... && go vet ./... && staticcheck ./...`
Run: `git diff master --stat` and confirm the branch touches only `internal/services/dataloader/`, `internal/services/mcpsvc/`, `internal/services/backup/meta.go`, `cmd/server/main.go`, and the two docs.

```bash
git add internal/services/mcpsvc/ docs/ README.md
git commit -m "docs(mcp): describe the housekeeping tools to the model and the reader

serverInstructions leads with get_status because it is the only tool
that still answers when the store is locked, which is the state that
makes every other tool fail for a reason the model cannot see."
```

---

## Self-review notes

Checked against the spec while writing:

- **Spec coverage.** Phase 4's nine tools: six implemented here (Tasks 4–9); `restore_backup`, `set_encryption` and `shutdown_server` explicitly deferred to 4b with reasons stated in the Scope section. The "Serializing the data writes" section is Tasks 1–3. "Write safety" (snapshot → write path → report) holds for both write tools. "Errors" (locked storage reports as locked; empty data returns an empty result with a note) is honored in `get_status`, `list_data_files` and `list_duplicates`. "Testing" (real `mcp.Client` over in-memory transport) is the harness copied into `register_test.go`; the router test is Task 10's registration test.
- **Guarded-operation tests** from the spec's Testing section are not here because no guarded operation is. They belong in 4b with the token infrastructure.
- **Type consistency.** `ensureDecisionsSnapshot` returns `(paths []string, note string, err error)` and is called identically in Tasks 7 and 8. `findPair` and `availableKeys` are defined in Task 7 and only used there; `pairsFrom` and `rowFor` are defined in Task 6 and used only there. `duplicateDecisionsFile` is declared once, in Task 4's `register.go`. `BackupService.Meta()` returns `backup.Meta`, matching the method added to the service in Task 4 Step 1.
- **One thing the implementer must confirm rather than trust.** Task 6's `rowFor` collides by name with nothing in `admin`, but `curate` has a function of the same name with a different signature. They are separate packages and neither imports the other, so this is legal — but if a later task moves shared helpers up, that is the collision to expect.
