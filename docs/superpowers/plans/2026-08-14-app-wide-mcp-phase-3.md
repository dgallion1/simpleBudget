# App-wide MCP — Phase 3 (curate) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the MCP server five major-expense curation tools — `list_major_expenses`, `list_exceptions`, `pin_transactions`, `upsert_major_expense`, `delete_major_expense` — so a model can read the Major Expenses page's data and make the same edits a human makes there.

**Architecture:** A new `internal/services/mcpsvc/curate` package calls the same `*dataloader.DataLoader` methods the `/major-expenses` handlers call, and recomputes the page's view by calling `majorexpenses.Match` with the page's own options. No handler logic is extracted — unlike Phase 2, the analysis this phase needs already lives in an exported service (`internal/services/majorexpenses`). The write tools snapshot their target file before writing, which requires the Phase 1 `Snapshotter` to move out of `mcpsvc/plan` into a leaf package both `plan` and `curate` can import.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk` v1.7.0, chi v5.

**Spec:** `docs/superpowers/specs/2026-08-12-app-wide-mcp-design.md` (Phase 3)
**Builds on:** `docs/superpowers/plans/2026-08-13-app-wide-mcp-phase-2.md`
**Branch:** `feat/mcp-curate` off `master`.

## Global Constraints

These are carried forward from the spec's "Constraints learned in phases 1 and 2" plus the Phase 2 constraints that still bind. Every task's requirements implicitly include this section.

- Go 1.26. **No new module dependencies.**
- Every tool handler's first line is `defer recoverToError("<tool_name>", &err)`. The go-sdk dispatches each tool call on its own goroutine with no recover of its own, and `middleware.Recoverer` runs on the HTTP request goroutine — a missing defer takes down the user's web server, not one call.
- Tool `Description` strings are the consuming model's only documentation. Write them to be read by a model with no other context: say what the numbers mean, what window they cover, what they exclude, and — for the write tools — exactly what changes on disk.
- **Read the twelve existing tool descriptions before writing a new one** (`internal/services/mcpsvc/plan/register.go`, `internal/services/mcpsvc/spend/*.go`). They now agree with each other on merchant identity, duplicate handling, window semantics and signs. A thirteenth that quietly disagrees is worse than one that is merely vague.
- **The sign convention is settled up front and is not negotiable per tool** (see "Sign convention" below). Refunds are `Outflow` rows with a *positive* amount (`internal/services/dataloader/classifier.go:83-93`). Phase 2 shipped `summarize_spending` with a `by_category` breakdown summed gross of refunds against a net `total_expenses`; do not repeat it.
- **Every tool that loads transactions calls `ts.Active()` immediately after loading,** before any date defaulting, so suppressed duplicate-resolution losers are excluded and the default window is computed over active rows.
- **A moved function's new description must be written from its implementation, not its name.** This binds Task 1 specifically.
- **Never enumerate the tests to move by name.** Task 1 moves a file: run `LSP findReferences` on each moved symbol, take the union of files containing a caller, and report that list before moving anything.
- **Gate the move on `go tool cover -func` before and after,** for both source and destination packages, in the task's report.
- The dependency direction holds: `mcpsvc` imports `plan`, `spend`, `curate`, and `snapshot`; none of them import `mcpsvc`. Service packages must not import any `handlers` package.
- Per this repo's `CLAUDE.md`: before editing a function/method/type, check callers with the `LSP` tool (`incomingCalls` / `findReferences`). Never rename a symbol with find-and-replace.
- Verify with `go build ./... && go vet ./... && go test ./... && staticcheck ./...`. Run tests **bare** — never pipe through `grep`/`head` without `set -o pipefail`.
- Pre-commit runs `make check`; never bypass with `--no-verify`.
- Do not run `go test -race` as part of each task; `make race` stays opt-in.

## Sign convention (settled here, once)

Two shapes appear in this phase's output and they are deliberately different:

- **Per-transaction `amount` fields are SIGNED, exactly as stored** — a purchase is negative, a refund is positive. This matches `search_transactions` and `get_anomalies`, and it means a hash returned by `list_exceptions` and a row returned by `search_transactions` show the same number.
- **Per-expense `total` fields are NET SPEND, normally POSITIVE** — computed as `sum(-t.Amount)` over the group, so a refund reduces the total instead of inflating it. A group whose refunds outweigh its purchases has a **negative** total. This matches `summarize_spending`'s `by_category` rows and the Major Expenses page's own displayed total (`internal/handlers/majorexpenses/handlers.go:376`).

Both rules must appear in the tool descriptions and in `serverInstructions` (Task 8).

## Hashes are the pin address, and are not unique

Pins are keyed by `Transaction.Hash`, which is `sha256(date|lowercased-trimmed-description|amount)[:8]` (`internal/models/transaction.go:60`). Two genuinely distinct transactions with the same date, description and amount share one hash, so pinning one pins both. This is already true of the Major Expenses page. Every tool that emits or accepts a hash must say so.

## File Structure

**Created:**
- `internal/services/mcpsvc/snapshot/snapshot.go` + `snapshot_test.go` — the Phase 1 `Snapshotter`, moved verbatim out of `plan` so `curate` can use it too. Leaf package: imports nothing from `mcpsvc`.
- `internal/services/mcpsvc/curate/register.go` — `Deps`, the three narrow store interfaces, `recoverToError`, `load`, the shared page-view helper, and the `Register` dispatcher.
- `internal/services/mcpsvc/curate/expenses.go` + `expenses_test.go` — `list_major_expenses`.
- `internal/services/mcpsvc/curate/exceptions.go` + `exceptions_test.go` — `list_exceptions`.
- `internal/services/mcpsvc/curate/pins.go` + `pins_test.go` — `pin_transactions`.
- `internal/services/mcpsvc/curate/upsert.go` + `upsert_test.go` — `upsert_major_expense`.
- `internal/services/mcpsvc/curate/delete.go` + `delete_test.go` — `delete_major_expense`.
- `internal/services/mcpsvc/curate/register_test.go` — the shared `connect` / `decodeToolResult` / `toolErrorText` test harness.

**Deleted:**
- `internal/services/mcpsvc/plan/snapshot.go`, `internal/services/mcpsvc/plan/snapshot_test.go` (moved, not rewritten).

**Modified:**
- `internal/services/mcpsvc/plan/register.go` — `Deps.Snapshots` becomes `*snapshot.Snapshotter`.
- `internal/services/mcpsvc/server.go` — constructs two `Snapshotter`s, registers `curate`, updates `serverInstructions`.
- `internal/services/mcpsvc/server_test.go` — tool count 12 → 17, one bump per task; pinned instruction claims updated.
- `internal/services/mcpsvc/spend/search.go` — `transactionRow` grows a `hash` field.

**Why the `snapshot` package exists instead of putting `Snapshotter` in `mcpsvc`:** the spec says "`snapshot.go` moves to `mcpsvc` as shared write infrastructure", but `mcpsvc` imports `plan`, so a `*mcpsvc.Snapshotter` in `plan.Deps` is an import cycle. A leaf package under `mcpsvc/` gets the same sharing with the dependency direction intact.

---

### Task 1: Move `Snapshotter` into its own leaf package

A pure move. `Snapshotter` is already dir-scoped at construction (`NewSnapshotter(settingsDir, snapshotDir)`), so `curate` can have its own instance pointed at the data directory and the `Ensure(name, now)` signature does not change.

**Files:**
- Create: `internal/services/mcpsvc/snapshot/snapshot.go`
- Create: `internal/services/mcpsvc/snapshot/snapshot_test.go`
- Delete: `internal/services/mcpsvc/plan/snapshot.go`
- Delete: `internal/services/mcpsvc/plan/snapshot_test.go`
- Modify: `internal/services/mcpsvc/plan/register.go:25-29`
- Modify: `internal/services/mcpsvc/server.go:66-70`

**Interfaces:**
- Produces:
  - `snapshot.New(sourceDir, snapshotDir string) *snapshot.Snapshotter`
  - `(*snapshot.Snapshotter).Ensure(name string, now time.Time) (string, error)`
- Consumes: nothing new.

- [ ] **Step 1: Enumerate the callers before touching anything**

Run the `LSP` tool `findReferences` on each of these symbols in `internal/services/mcpsvc/plan/snapshot.go` and record every file that appears:

- `Snapshotter` (line 20)
- `NewSnapshotter` (line 28)
- `Ensure` (line 51)
- `validateScenarioName` (line 87)
- `snapshotTimeLayout` (line 14)

Report the union of files as the move's blast radius **before** editing. Do not derive this list from test-function names or from `grep` on the word "snapshot".

- [ ] **Step 2: Record the before-coverage**

```bash
go test -coverprofile=/tmp/before-plan.out ./internal/services/mcpsvc/plan/ && go tool cover -func=/tmp/before-plan.out | tail -1
```

Record the total. There is no destination package yet; its "before" is 0%.

- [ ] **Step 3: Move the two files with git mv**

```bash
mkdir -p internal/services/mcpsvc/snapshot
git mv internal/services/mcpsvc/plan/snapshot.go internal/services/mcpsvc/snapshot/snapshot.go
git mv internal/services/mcpsvc/plan/snapshot_test.go internal/services/mcpsvc/snapshot/snapshot_test.go
```

- [ ] **Step 4: Rename the package and generalize the identifiers**

In `internal/services/mcpsvc/snapshot/snapshot.go`: change `package plan` to a documented `package snapshot`, rename `NewSnapshotter` → `New`, rename `validateScenarioName` → `validateName`, rename the `settingsDir` field to `sourceDir`, and rename the `scenario` parameter to `name`. Keep the body byte-identical otherwise.

Per the Global Constraints, the doc comments must describe what the code does, not what the old names suggested. The header and `Ensure`'s comment become:

```go
// Package snapshot copies a file to a backup directory before an MCP tool
// first writes to it, so an unwanted tool-driven change has a recovery path.
// It is a leaf package: mcpsvc/plan and mcpsvc/curate each construct their
// own instance pointed at their own source directory, and neither imports
// mcpsvc itself.
package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// snapshotTimeLayout deliberately avoids RFC3339: its colons survive Linux but
// break extraction on Windows and exFAT.
const snapshotTimeLayout = "2006-01-02T15-04-05Z"

// Snapshotter copies a file out of sourceDir before this process first writes
// to it.
//
// Once per (process, name), not once per process: a session that writes to
// two different files must back up both, and a session that writes to the
// same file twice keeps the copy of its ORIGINAL state rather than
// overwriting it with the state after the first change.
type Snapshotter struct {
	sourceDir   string
	snapshotDir string

	mu   sync.Mutex
	done map[string]string // name -> snapshot path
}

func New(sourceDir, snapshotDir string) *Snapshotter {
	return &Snapshotter{
		sourceDir:   sourceDir,
		snapshotDir: snapshotDir,
		done:        make(map[string]string),
	}
}

// Ensure copies sourceDir/name into the snapshot directory if this process
// has not already done so, returning the snapshot path.
//
// It copies the file's raw bytes with os.ReadFile rather than hard-linking
// it, so the backup is a point-in-time copy that a later in-place rewrite of
// the source cannot alter. When the store is encrypted the bytes are
// ciphertext, which is what a hand-restore back into that same store needs.
//
// A missing source file is an error, and the caller must abort its write on
// it: every caller snapshots BEFORE writing precisely so that a failure here
// prevents an unrecoverable change.
//
// Precondition: name must be a bare filename with no path separators or ".."
// segments (filepath.Base(name) == name, and it must not contain ".."). This
// is the only recovery path for an unwanted write to the user's real data, so
// it validates its own input rather than trusting the caller: name is
// rejected, and nothing is read or written, before any file I/O happens. This
// mirrors retirement.SettingsManager.scenarioPath's validation.
func (s *Snapshotter) Ensure(name string, now time.Time) (string, error) {
```

The body of `Ensure` keeps `validateName(name)`, the mutex, the `done` lookup, `filepath.Join(s.sourceDir, name)`, the `MkdirAll`, and the `%s.%s.bak` destination format exactly as they were. `validateName`'s comment keeps its "can't escape sourceDir on the read side or snapshotDir on the write side via `../`" rationale with "scenario" replaced by "name".

- [ ] **Step 5: Update the test file to the new package and names**

In `internal/services/mcpsvc/snapshot/snapshot_test.go`: `package snapshot`, `NewSnapshotter(` → `New(`, `validateScenarioName(` → `validateName(`. Do not delete, rename, or reword any test — this is a move.

- [ ] **Step 6: Update `plan.Deps`**

In `internal/services/mcpsvc/plan/register.go`, add the import and change the field type:

```go
import (
	// ...
	"budget2/internal/services/mcpsvc/snapshot"
)

// Deps is what the planner tools need. The settings manager is the server's
// own instance, not a second one opened on the same directory: it owns the
// active-scenario selection, the settings cache, and the write lock.
type Deps struct {
	Settings  *retirement.SettingsManager
	Snapshots *snapshot.Snapshotter
	BaseURL   string
}
```

`apply_changes`'s call site (`deps.Snapshots.Ensure(expected, time.Now())`) is unchanged.

- [ ] **Step 7: Update `NewServer`**

In `internal/services/mcpsvc/server.go`, swap the import `"budget2/internal/services/mcpsvc/plan"` alongside a new `"budget2/internal/services/mcpsvc/snapshot"`, and change the construction:

```go
	plan.Register(s, plan.Deps{
		Settings:  deps.Settings,
		Snapshots: snapshot.New(deps.SettingsDir, deps.SnapshotDir),
		BaseURL:   deps.BaseURL,
	})
```

- [ ] **Step 8: Build and run the affected packages**

```bash
go build ./... && go test ./internal/services/mcpsvc/... ./cmd/server/
```

Expected: PASS. Every test that lived in `plan/snapshot_test.go` now runs under `snapshot`.

- [ ] **Step 9: Record the after-coverage**

```bash
go test -coverprofile=/tmp/after-plan.out ./internal/services/mcpsvc/plan/ && go tool cover -func=/tmp/after-plan.out | tail -1
go test -coverprofile=/tmp/after-snap.out ./internal/services/mcpsvc/snapshot/ && go tool cover -func=/tmp/after-snap.out | tail -1
```

Report all three numbers (plan before, plan after, snapshot after) in the task report. `plan`'s percentage may move because its statement count shrank; what must not happen is a *test* disappearing. If any test from the old file no longer runs, stop and report.

- [ ] **Step 10: Verify and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add -A
git commit -m "refactor(mcp): move Snapshotter into its own leaf package

curate's write tools need the same pre-write backup that apply_changes uses.
Snapshotter cannot live in mcpsvc itself -- mcpsvc imports plan, so a
*mcpsvc.Snapshotter in plan.Deps would be an import cycle -- so it moves to
mcpsvc/snapshot, which imports nothing from mcpsvc. Behavior is unchanged;
only the package, the constructor name, and the dir field's name differ."
```

---

### Task 2: Expose the transaction hash in `search_transactions`

Pins are addressed by hash. Without this, a transaction found through search cannot be pinned without re-finding it through a curate tool.

**Files:**
- Modify: `internal/services/mcpsvc/spend/search.go:30-36` (`transactionRow`), `:74-89` (description), `:164-172` (row build)
- Modify: `internal/services/mcpsvc/spend/search_test.go`

**Interfaces:**
- Produces: `search_transactions` result rows gain `hash` (string). Consumed by Task 5's `pin_transactions`.

- [ ] **Step 1: Write the failing test**

Append to `internal/services/mcpsvc/spend/search_test.go`:

```go
// TestSearchReturnsTheTransactionHash pins the field pin_transactions uses to
// address a row. Without it, "find this charge, then pin it" cannot be done
// with these tools at all.
func TestSearchReturnsTheTransactionHash(t *testing.T) {
	txn := models.Transaction{
		Date:            time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC),
		Description:     "CITY WATER DEPT",
		Category:        "Utilities",
		Amount:          -88.10,
		TransactionType: models.Outflow,
	}
	txn.Hash = txn.ComputeHash()

	cs := connect(t, Deps{Transactions: stubTransactions{
		ts: models.NewTransactionSet([]models.Transaction{txn}),
	}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_transactions",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	out := decodeToolResult[searchOutput](t, res)
	if len(out.Transactions) != 1 {
		t.Fatalf("got %d rows, want 1", len(out.Transactions))
	}
	if out.Transactions[0].Hash != txn.Hash {
		t.Errorf("hash = %q, want %q", out.Transactions[0].Hash, txn.Hash)
	}
}
```

Add `"time"` to the test file's imports if it is not already there.

- [ ] **Step 2: Run it to verify it fails**

```bash
go test ./internal/services/mcpsvc/spend/ -run TestSearchReturnsTheTransactionHash -v
```

Expected: FAIL — `out.Transactions[0].Hash` undefined.

- [ ] **Step 3: Add the field**

In `search.go`:

```go
type transactionRow struct {
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Amount      float64 `json:"amount"`
	Type        string  `json:"type"`
	// Hash is the identifier the curation tools use to pin this transaction
	// to a major expense. It is derived from date + lower-cased description +
	// amount, so two distinct transactions sharing all three share one hash
	// and are pinned together.
	Hash string `json:"hash"`
}
```

and in the row build:

```go
			rows = append(rows, transactionRow{
				Date:        t.Date.Format("2006-01-02"),
				Description: t.Label(),
				Category:    t.Category,
				Amount:      t.Amount,
				Type:        string(t.TransactionType),
				Hash:        t.Hash,
			})
```

- [ ] **Step 4: Extend the description**

Append this sentence to the `search_transactions` `Description`, immediately before the final "Transactions the user has already marked..." sentence:

```go
			"Each row carries a `hash`, which is what pin_transactions uses to attach that transaction to a " +
			"major expense; the hash is derived from date + lower-cased description + amount, so two genuinely " +
			"distinct transactions that share all three share one hash and pinning either pins both. " +
```

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/services/mcpsvc/spend/
```

Expected: PASS.

- [ ] **Step 6: Verify and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add -A
git commit -m "feat(mcp): return the transaction hash from search_transactions

The hash is how a transaction is addressed when pinning it to a major
expense. Without it in search results, a model that found a charge with
search_transactions had no way to act on it."
```

---

### Task 3: `curate` package skeleton and `list_major_expenses`

Establishes `Deps`, the store interfaces, and the shared page-view helper every later task builds on.

**Files:**
- Create: `internal/services/mcpsvc/curate/register.go`
- Create: `internal/services/mcpsvc/curate/register_test.go`
- Create: `internal/services/mcpsvc/curate/expenses.go`
- Create: `internal/services/mcpsvc/curate/expenses_test.go`
- Modify: `internal/services/mcpsvc/server.go`
- Modify: `internal/services/mcpsvc/server_test.go`

**Interfaces:**
- Consumes: `snapshot.New` / `(*Snapshotter).Ensure` (Task 1); `majorexpenses.Match`, `majorexpenses.MatchOptions`, `majorexpenses.MatchResult` (existing, `internal/services/majorexpenses/engine.go`); `*dataloader.DataLoader`'s major-expense and pin methods.
- Produces, all in package `curate`:
  - `type TransactionSource interface { LoadData() (*models.TransactionSet, error) }`
  - `type ExpenseStore interface { LoadMajorExpenses() ([]models.MajorExpense, error); AddMajorExpense(models.MajorExpense) ([]models.MajorExpense, error); UpdateMajorExpense(string, models.MajorExpense) ([]models.MajorExpense, error); ArchiveMajorExpense(string) error; RestoreMajorExpense(string) error; LoadDeletedMajorExpenses() ([]models.DeletedMajorExpense, error) }`
  - `type PinStore interface { LoadTransactionPins() (map[string]string, error); SetTransactionPins(map[string]string) (int, error) }`
  - `type Deps struct { Transactions TransactionSource; Expenses ExpenseStore; Pins PinStore; Store *storage.Storage; Snapshots *snapshot.Snapshotter }`
  - `func Register(s *mcp.Server, deps Deps)`
  - `func recoverToError(tool string, err *error)`
  - `func (d Deps) load() (*models.TransactionSet, error)`
  - `func (d Deps) pageView(startDate, endDate string) (*view, error)` and `type view struct { Start, End time.Time; Expenses []models.MajorExpense; Pins map[string]string; Match majorexpenses.MatchResult }`
  - `func parseWindowDate(field, value string) (*time.Time, error)` (a copy — `spend`'s is unexported and packages must not depend on each other)
  - `func registerListExpenses(s *mcp.Server, deps Deps)`
  - `type pinnableRow struct { Hash, Date, Description, Category string; Amount float64; Pinned bool }`

- [ ] **Step 1: Write `register.go`**

```go
// Package curate serves the Major Expenses page's data and edits over MCP:
// which declared expenses exist, which transactions matched them, what fell
// through as an exception, and the four writes that change any of that.
//
// It calls the same *dataloader.DataLoader methods the /major-expenses
// handlers call and recomputes the page's view with the page's own
// majorexpenses.Match options, so a tool answer and the page agree.
package curate

import (
	"fmt"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/majorexpenses"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// These mirror internal/handlers/majorexpenses/handlers.go's package
// constants. They are duplicated rather than exported from the handler
// package because a service must not import a handlers package; the
// duplication is pinned by TestThresholdsMatchThePage.
const (
	defaultUnknownThreshold = 100.0
	defaultNewWindowDays    = 30
)

// File names in the data directory that the write tools snapshot before
// changing. They mirror the unexported constants in
// internal/services/dataloader.
const (
	majorExpensesFile        = "major_expenses.json"
	transactionPinsFile      = "transaction_pins.json"
	deletedMajorExpensesFile = "deleted_major_expenses.json"
)

// TransactionSource loads the full transaction history. *dataloader.DataLoader
// satisfies it via LoadData, so no adapter is needed in production. The
// interface exists so tests can substitute a canned models.TransactionSet
// instead of driving real CSV parsing and classification.
type TransactionSource interface {
	LoadData() (*models.TransactionSet, error)
}

// ExpenseStore reads and writes the declared major expenses and their
// soft-delete archive. *dataloader.DataLoader satisfies it. Tests use a real
// loader over t.TempDir() rather than a stub: these methods write JSON files
// whose on-disk layout is part of what is being tested.
type ExpenseStore interface {
	LoadMajorExpenses() ([]models.MajorExpense, error)
	AddMajorExpense(models.MajorExpense) ([]models.MajorExpense, error)
	UpdateMajorExpense(string, models.MajorExpense) ([]models.MajorExpense, error)
	ArchiveMajorExpense(string) error
	RestoreMajorExpense(string) error
	LoadDeletedMajorExpenses() ([]models.DeletedMajorExpense, error)
}

// PinStore reads and writes transaction_pins.json.
// *dataloader.DataLoader satisfies it.
type PinStore interface {
	LoadTransactionPins() (map[string]string, error)
	SetTransactionPins(map[string]string) (int, error)
}

// Deps is what the curation tools need. Store is optional and used only to
// turn a locked store into a clear message instead of a parse failure.
// Snapshots is required by the write tools, which abort rather than write
// without a backup; the read tools never touch it.
type Deps struct {
	Transactions TransactionSource
	Expenses     ExpenseStore
	Pins         PinStore
	Store        *storage.Storage
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

// load returns the full ledger, reporting a locked store as such rather than
// letting ciphertext surface as a parse error.
func (d Deps) load() (*models.TransactionSet, error) {
	if d.Store != nil && d.Store.IsEncrypted() && !d.Store.IsUnlocked() {
		return nil, fmt.Errorf(
			"cannot load transaction history: storage is encrypted and locked; unlock it via the budget2 web UI (/unlock) first")
	}
	return d.Transactions.LoadData()
}

// parseWindowDate parses a YYYY-MM-DD date parameter. An empty value is not
// an error -- it means "no bound on this side" -- and returns a nil pointer.
// An unparseable value returns an error naming the offending field and value,
// meant to surface as a tool error rather than a panic.
//
// Duplicated from mcpsvc/spend rather than shared: the two packages are
// siblings and neither may import the other.
func parseWindowDate(field, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, fmt.Errorf("%s %q is not a valid date (want YYYY-MM-DD): %w", field, value, err)
	}
	return &t, nil
}

// view is the recomputed Major Expenses page state for one window.
type view struct {
	Start    time.Time
	End      time.Time
	Expenses []models.MajorExpense
	Pins     map[string]string
	Match    majorexpenses.MatchResult
}

// pageView reproduces internal/handlers/majorexpenses.buildPageData's
// pipeline exactly: load, drop transactions the user resolved as duplicates,
// narrow to the window, keep only outflows, then match.
//
// The order matters and is not incidental. Active() comes first so the
// MinDate/MaxDate window defaults are computed over active rows only. The
// outflow filter comes before Match so that income whose description happens
// to contain a keyword cannot inflate a group's count or total.
func (d Deps) pageView(startDate, endDate string) (*view, error) {
	start, err := parseWindowDate("start_date", startDate)
	if err != nil {
		return nil, err
	}
	end, err := parseWindowDate("end_date", endDate)
	if err != nil {
		return nil, err
	}

	expenses, err := d.Expenses.LoadMajorExpenses()
	if err != nil {
		return nil, fmt.Errorf("load major expenses: %w", err)
	}
	if expenses == nil {
		expenses = []models.MajorExpense{}
	}

	ts, err := d.load()
	if err != nil {
		return nil, err
	}
	ts = ts.Active()

	pins, err := d.Pins.LoadTransactionPins()
	if err != nil {
		return nil, fmt.Errorf("load transaction pins: %w", err)
	}

	from := ts.MinDate()
	if start != nil {
		from = *start
	}
	to := ts.MaxDate()
	if end != nil {
		to = *end
	}

	outflows := ts.FilterByDateRange(from, to).FilterByType(models.Outflow)

	return &view{
		Start:    from,
		End:      to,
		Expenses: expenses,
		Pins:     pins,
		Match: majorexpenses.Match(outflows, expenses, majorexpenses.MatchOptions{
			UnknownLargeThreshold: defaultUnknownThreshold,
			NewMerchantWindow:     time.Duration(defaultNewWindowDays) * 24 * time.Hour,
			Pins:                  pins,
		}),
	}, nil
}

// pinnableRow is a transaction as the curation tools report it: the signed
// amount exactly as stored, plus the hash needed to act on it.
type pinnableRow struct {
	Hash        string  `json:"hash"`
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Amount      float64 `json:"amount"`
	Pinned      bool    `json:"pinned"`
}

func rowFor(t models.Transaction, pinned map[string]bool) pinnableRow {
	return pinnableRow{
		Hash:        t.Hash,
		Date:        t.Date.Format("2006-01-02"),
		Description: t.Label(),
		Category:    t.Category,
		Amount:      t.Amount,
		Pinned:      pinned[t.Hash],
	}
}

// formatDay renders a window bound for output. The zero time -- what MinDate
// returns for an empty ledger -- renders as empty rather than "0001-01-01".
func formatDay(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// Register adds the curation tools to s.
func Register(s *mcp.Server, deps Deps) {
	registerListExpenses(s, deps)
}
```

- [ ] **Step 2: Write the test harness**

Create `internal/services/mcpsvc/curate/register_test.go`:

```go
package curate

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/mcpsvc/snapshot"
	"budget2/internal/services/storage"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubTransactions struct {
	ts  *models.TransactionSet
	err error
}

func (s stubTransactions) LoadData() (*models.TransactionSet, error) { return s.ts, s.err }

// newDeps builds Deps backed by a real DataLoader over a temp directory --
// the write tools' whole job is to change JSON files on disk, so stubbing the
// store would test nothing -- with the ledger itself stubbed, since building
// exact match groups through real CSV parsing would be indirect and brittle.
// It returns the data directory so tests can assert on the files.
func newDeps(t *testing.T, txns []models.Transaction) (Deps, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	loader := dataloader.New(dir, store)
	for i := range txns {
		if txns[i].Hash == "" {
			txns[i].Hash = txns[i].ComputeHash()
		}
	}
	return Deps{
		Transactions: stubTransactions{ts: models.NewTransactionSet(txns)},
		Expenses:     loader,
		Pins:         loader,
		Store:        store,
		Snapshots:    snapshot.New(dir, filepath.Join(dir, "snapshots")),
	}, dir
}

func connect(t *testing.T, deps Deps) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0.0.0"}, nil)
	Register(srv, deps)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return res
}

func decodeToolResult[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool call returned an error result: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal StructuredContent into %T: %v", out, err)
	}
	return out
}

func toolErrorText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if !res.IsError {
		t.Fatalf("expected an error result, got: %+v", res.StructuredContent)
	}
	if len(res.Content) == 0 {
		t.Fatal("error result has no content describing the failure")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("error content = %#v, want text", res.Content[0])
	}
	return text.Text
}

// TestThresholdsMatchThePage pins curate's copies of the Major Expenses
// page's thresholds. They are duplicated because a service may not import a
// handlers package; if the page's values change and these do not, a tool and
// the page will disagree about which transactions are exceptions.
func TestThresholdsMatchThePage(t *testing.T) {
	if defaultUnknownThreshold != 100.0 {
		t.Errorf("defaultUnknownThreshold = %v; internal/handlers/majorexpenses/handlers.go declares 100.0",
			defaultUnknownThreshold)
	}
	if defaultNewWindowDays != 30 {
		t.Errorf("defaultNewWindowDays = %v; internal/handlers/majorexpenses/handlers.go declares 30",
			defaultNewWindowDays)
	}
}
```

- [ ] **Step 3: Write the failing test for `list_major_expenses`**

Create `internal/services/mcpsvc/curate/expenses_test.go`:

```go
package curate

import (
	"testing"
	"time"

	"budget2/internal/models"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ledger is a small fixture: two mortgage payments, one mortgage refund, one
// unmatched large charge, and one income row that mentions "mortgage" and
// must therefore NOT be counted against the mortgage expense.
func ledger() []models.Transaction {
	return []models.Transaction{
		{Date: day(2026, 1, 5), Description: "MORTGAGE PAYMENT", Category: "Housing", Amount: -2000, TransactionType: models.Outflow},
		{Date: day(2026, 2, 5), Description: "MORTGAGE PAYMENT", Category: "Housing", Amount: -2000, TransactionType: models.Outflow},
		{Date: day(2026, 2, 9), Description: "MORTGAGE ESCROW REFUND", Category: "Housing", Amount: 300, TransactionType: models.Outflow},
		{Date: day(2026, 2, 14), Description: "ACME ROOFING", Category: "Home", Amount: -4500, TransactionType: models.Outflow},
		{Date: day(2026, 2, 20), Description: "MORTGAGE ESCROW DEPOSIT", Category: "Income", Amount: 1200, TransactionType: models.Income},
	}
}

func TestListMajorExpensesReportsNetSpendAndMatchCounts(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"mortgage"},
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[listExpensesOutput](t, call(t, cs, "list_major_expenses",
		map[string]any{"include_transactions": true}))

	if len(out.Expenses) != 1 {
		t.Fatalf("got %d expenses, want 1", len(out.Expenses))
	}
	e := out.Expenses[0]
	// Three outflows match "mortgage"; the Income row does not, because the
	// outflow filter runs before matching.
	if e.Count != 3 {
		t.Errorf("count = %d, want 3 (two payments + one refund, income excluded)", e.Count)
	}
	// Net spend: -(-2000) + -(-2000) + -(300) = 3700.
	if e.Total != 3700 {
		t.Errorf("total = %v, want 3700 (net of the 300 refund)", e.Total)
	}
	// Per-transaction amounts stay signed exactly as stored.
	var sawRefund bool
	for _, r := range e.Transactions {
		if r.Amount == 300 {
			sawRefund = true
		}
	}
	if !sawRefund {
		t.Errorf("expected the +300 refund row reported with its stored sign, got %+v", e.Transactions)
	}
	if out.UnmatchedCount != 1 || out.UnmatchedTotal != 4500 {
		t.Errorf("unmatched = (%d, %v), want (1, 4500)", out.UnmatchedCount, out.UnmatchedTotal)
	}
	if out.TotalDeclared != 3700 {
		t.Errorf("total_declared = %v, want 3700", out.TotalDeclared)
	}
}

func TestListMajorExpensesCountsPinsAndHonorsTheWindow(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-roof", Name: "Roof", Keywords: nil,
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	if _, err := deps.Pins.SetTransactionPins(map[string]string{roof.ComputeHash(): "me-roof"}); err != nil {
		t.Fatalf("SetTransactionPins: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[listExpensesOutput](t, call(t, cs, "list_major_expenses", map[string]any{
		"start_date": "2026-02-01", "end_date": "2026-02-28",
	}))
	if len(out.Expenses) != 1 {
		t.Fatalf("got %d expenses, want 1", len(out.Expenses))
	}
	if out.Expenses[0].PinnedCount != 1 || out.Expenses[0].Count != 1 {
		t.Errorf("count/pinned = %d/%d, want 1/1", out.Expenses[0].Count, out.Expenses[0].PinnedCount)
	}
	if out.Start != "2026-02-01" || out.End != "2026-02-28" {
		t.Errorf("window = %s..%s, want 2026-02-01..2026-02-28", out.Start, out.End)
	}
}

func TestListMajorExpensesReportsTheSoftDeleteArchiveOnRequest(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-gone", Name: "Retired Thing", Keywords: []string{"nothing"},
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	if err := deps.Expenses.ArchiveMajorExpense("me-gone"); err != nil {
		t.Fatalf("ArchiveMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	without := decodeToolResult[listExpensesOutput](t, call(t, cs, "list_major_expenses", map[string]any{}))
	if len(without.Deleted) != 0 {
		t.Errorf("deleted archive returned without include_deleted: %+v", without.Deleted)
	}
	with := decodeToolResult[listExpensesOutput](t, call(t, cs, "list_major_expenses",
		map[string]any{"include_deleted": true}))
	if len(with.Deleted) != 1 || with.Deleted[0].ID != "me-gone" {
		t.Errorf("deleted = %+v, want one entry me-gone", with.Deleted)
	}
}

func TestListMajorExpensesRejectsABadDate(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)
	msg := toolErrorText(t, call(t, cs, "list_major_expenses", map[string]any{"start_date": "March 2026"}))
	if msg == "" {
		t.Fatal("expected an error naming the bad date")
	}
}
```

- [ ] **Step 4: Run to verify it fails**

```bash
go test ./internal/services/mcpsvc/curate/ -v
```

Expected: FAIL to compile — `listExpensesOutput` and the tool are undefined.

- [ ] **Step 5: Write `expenses.go`**

```go
package curate

import (
	"context"
	"sort"
	"strings"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listExpensesInput struct {
	StartDate           string `json:"start_date,omitempty" jsonschema:"earliest date to include, inclusive, YYYY-MM-DD; defaults to the first transaction on record"`
	EndDate             string `json:"end_date,omitempty" jsonschema:"latest date to include, inclusive, YYYY-MM-DD; defaults to the last transaction on record"`
	IncludeTransactions bool   `json:"include_transactions,omitempty" jsonschema:"return each expense's matched transactions with their hashes, not just the counts"`
	IncludeDeleted      bool   `json:"include_deleted,omitempty" jsonschema:"also return the soft-deleted expenses that can still be restored"`
}

type majorExpenseRow struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	Keywords           []string      `json:"keywords"`
	ExpectedMin        float64       `json:"expected_min"`
	ExpectedMax        float64       `json:"expected_max"`
	Notes              string        `json:"notes,omitempty"`
	IsInternalTransfer bool          `json:"is_internal_transfer"`
	Count              int           `json:"count"`
	PinnedCount        int           `json:"pinned_count"`
	Total              float64       `json:"total"`
	Transactions       []pinnableRow `json:"transactions,omitempty"`
}

type deletedExpenseRow struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DeletedAt    string `json:"deleted_at"`
	PinnedHashes int    `json:"pinned_hashes"`
}

type listExpensesOutput struct {
	Start          string              `json:"start"`
	End            string              `json:"end"`
	Expenses       []majorExpenseRow   `json:"expenses"`
	Deleted        []deletedExpenseRow `json:"deleted,omitempty"`
	TotalDeclared  float64             `json:"total_declared"`
	UnmatchedCount int                 `json:"unmatched_count"`
	UnmatchedTotal float64             `json:"unmatched_total"`
	Note           string              `json:"note,omitempty"`
}

func registerListExpenses(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_major_expenses",
		Description: "List the declared major expenses -- the user's own labels for spending they already " +
			"understand -- with how many transactions matched each one in the window and what they came to. " +
			"A transaction matches an expense by keyword, by amount range, or because the user pinned it there " +
			"explicitly; a pin overrides the other two. Only OUTFLOWS are matched, so income whose description " +
			"happens to contain a keyword is never counted. Window defaults to the full transaction history. " +
			"`total` and `total_declared` are NET SPEND and are normally POSITIVE: a refund inside a group " +
			"REDUCES its total, and a group whose refunds outweigh its purchases has a negative total. The " +
			"per-transaction `amount` under include_transactions is the opposite -- SIGNED exactly as stored, " +
			"so a purchase is negative and a refund positive, matching search_transactions. `unmatched_count` " +
			"and `unmatched_total` are the in-window outflows that matched nothing; that gap is why the app's " +
			"overall spending exceeds the declared total. Each returned transaction carries the `hash` that " +
			"pin_transactions needs; hashes come from date + lower-cased description + amount, so two " +
			"identical-looking transactions share one and pinning either pins both. Transactions the user has " +
			"already resolved as duplicates are excluded, matching every other aggregate in the app. This tool " +
			"reads only; upsert_major_expense and delete_major_expense are the writes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listExpensesInput) (res *mcp.CallToolResult, out listExpensesOutput, err error) {
		defer recoverToError("list_major_expenses", &err)

		v, err := deps.pageView(in.StartDate, in.EndDate)
		if err != nil {
			return nil, listExpensesOutput{}, err
		}

		rows := make([]majorExpenseRow, 0, len(v.Expenses))
		var totalDeclared float64
		for _, e := range v.Expenses {
			group := v.Match.Groups[e.ID]
			sorted := append([]models.Transaction(nil), group...)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.After(sorted[j].Date) })

			var total float64
			pinned := 0
			txRows := make([]pinnableRow, 0, len(sorted))
			for _, t := range sorted {
				// Net spend: purchases are negative and refunds positive in
				// the classifier's Outflow convention, so negating the raw
				// amount makes spending positive and lets a refund subtract.
				total += -t.Amount
				if v.Match.PinnedHashes[t.Hash] {
					pinned++
				}
				if in.IncludeTransactions {
					txRows = append(txRows, rowFor(t, v.Match.PinnedHashes))
				}
			}
			totalDeclared += total

			keywords := e.Keywords
			if keywords == nil {
				keywords = []string{}
			}
			rows = append(rows, majorExpenseRow{
				ID:                 e.ID,
				Name:               e.Name,
				Keywords:           keywords,
				ExpectedMin:        e.ExpectedMin,
				ExpectedMax:        e.ExpectedMax,
				Notes:              e.Notes,
				IsInternalTransfer: e.IsInternalTransfer,
				Count:              len(sorted),
				PinnedCount:        pinned,
				Total:              total,
				Transactions:       txRows,
			})
		}
		sort.Slice(rows, func(i, j int) bool {
			return strings.ToLower(strings.TrimSpace(rows[i].Name)) <
				strings.ToLower(strings.TrimSpace(rows[j].Name))
		})

		var unmatchedTotal float64
		for _, t := range v.Match.Unmatched {
			unmatchedTotal += -t.Amount
		}

		out = listExpensesOutput{
			Start:          formatDay(v.Start),
			End:            formatDay(v.End),
			Expenses:       rows,
			TotalDeclared:  totalDeclared,
			UnmatchedCount: len(v.Match.Unmatched),
			UnmatchedTotal: unmatchedTotal,
		}
		if len(rows) == 0 {
			out.Note = "no major expenses are declared yet; use upsert_major_expense to create one"
		}

		if in.IncludeDeleted {
			deleted, err := deps.Expenses.LoadDeletedMajorExpenses()
			if err != nil {
				return nil, listExpensesOutput{}, err
			}
			sort.Slice(deleted, func(i, j int) bool { return deleted[i].DeletedAt.After(deleted[j].DeletedAt) })
			for _, d := range deleted {
				out.Deleted = append(out.Deleted, deletedExpenseRow{
					ID:           d.Expense.ID,
					Name:         d.Expense.Name,
					DeletedAt:    d.DeletedAt.Format("2006-01-02"),
					PinnedHashes: len(d.PinnedHashes),
				})
			}
		}
		return nil, out, nil
	})
}
```

- [ ] **Step 6: Run the tests**

```bash
go test ./internal/services/mcpsvc/curate/ -v
```

Expected: PASS.

- [ ] **Step 7: Wire `curate` into `NewServer`**

In `internal/services/mcpsvc/server.go`, add the `curate` import and register it inside the existing `if deps.Loader != nil` block, after `spend.Register`:

```go
	if deps.Loader != nil {
		spend.Register(s, spend.Deps{
			Transactions:  deps.Loader,
			Store:         deps.Store,
			Settings:      deps.Settings,
			MajorExpenses: deps.Loader,
		})
		// The data directory comes off the loader rather than a new Deps
		// field so the files curate snapshots are, by construction, the same
		// files the loader writes.
		curate.Register(s, curate.Deps{
			Transactions: deps.Loader,
			Expenses:     deps.Loader,
			Pins:         deps.Loader,
			Store:        deps.Store,
			// A separate snapshot subdirectory: plan snapshots files from the
			// settings dir and curate from the data dir, and nothing stops
			// the two directories from holding a file of the same name.
			Snapshots: snapshot.New(deps.Loader.CSVDirectory, filepath.Join(deps.SnapshotDir, "data")),
		})
	}
```

Add `"path/filepath"` to the imports.

- [ ] **Step 8: Bump the tool-count test**

In `internal/services/mcpsvc/server_test.go`, rename `TestNewServerRegistersAllTwelveTools` to `TestNewServerRegistersAllThirteenTools`, add `"list_major_expenses"` to the wanted list, and change `!= 12` / `12 tools` to `13`.

- [ ] **Step 9: Verify and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add -A
git commit -m "feat(mcp): add list_major_expenses and the curate package

curate recomputes the Major Expenses page's view by calling the same
majorexpenses.Match options the page uses, over the same loader, so a tool
answer and the page cannot disagree. Per-expense totals are net spend
(positive); per-transaction amounts stay signed as stored."
```

---

### Task 4: `list_exceptions`

**Files:**
- Create: `internal/services/mcpsvc/curate/exceptions.go`
- Create: `internal/services/mcpsvc/curate/exceptions_test.go`
- Modify: `internal/services/mcpsvc/curate/register.go` (add `registerListExceptions` to `Register`)
- Modify: `internal/services/mcpsvc/server_test.go` (13 → 14)

**Interfaces:**
- Consumes: `Deps.pageView`, `pinnableRow`, `rowFor`, `formatDay`, `parseWindowDate` (Task 3).
- Produces: `func registerListExceptions(s *mcp.Server, deps Deps)`; `type exceptionRow`; `type bucketView`; `type listExceptionsOutput`.

- [ ] **Step 1: Write the failing test**

Create `internal/services/mcpsvc/curate/exceptions_test.go`:

```go
package curate

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestListExceptionsSplitsTheThreeBuckets(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	// A range that the 2000 payments sit outside of, so both are anomalous.
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"mortgage"},
		ExpectedMin: 100, ExpectedMax: 500,
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[listExceptionsOutput](t, call(t, cs, "list_exceptions", map[string]any{}))

	if out.Unmatched == nil || out.Unmatched.Total != 1 {
		t.Fatalf("unmatched = %+v, want 1 row (ACME ROOFING)", out.Unmatched)
	}
	if !out.Unmatched.Rows[0].OverThreshold {
		t.Errorf("a 4500 unmatched outflow must be flagged over the %v threshold", out.Threshold)
	}
	if out.Unmatched.Rows[0].Hash == "" {
		t.Error("exception rows must carry the hash pin_transactions needs")
	}
	if out.Anomalous == nil || out.Anomalous.Total == 0 {
		t.Fatalf("anomalous = %+v, want the out-of-range mortgage payments", out.Anomalous)
	}
	if out.Anomalous.Rows[0].MajorExpenseName != "Mortgage" {
		t.Errorf("anomalous row names %q, want Mortgage", out.Anomalous.Rows[0].MajorExpenseName)
	}
	if out.NewMerchants == nil {
		t.Error("new_merchants bucket missing")
	}
}

func TestListExceptionsFiltersOneBucketByTextAndAmount(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)

	out := decodeToolResult[listExceptionsOutput](t, call(t, cs, "list_exceptions", map[string]any{
		"bucket": "unmatched",
		"search": "roofing",
	}))
	if out.Anomalous != nil || out.NewMerchants != nil {
		t.Error("naming one bucket must omit the others")
	}
	if out.Unmatched.Total != 1 || !strings.Contains(strings.ToLower(out.Unmatched.Rows[0].Description), "roofing") {
		t.Errorf("search did not narrow to the roofing row: %+v", out.Unmatched)
	}

	none := decodeToolResult[listExceptionsOutput](t, call(t, cs, "list_exceptions", map[string]any{
		"bucket": "unmatched", "min_amount": 100000,
	}))
	if none.Unmatched.Total != 0 {
		t.Errorf("min_amount 100000 should have matched nothing, got %d", none.Unmatched.Total)
	}
	if none.Note == "" {
		t.Error("an empty result must carry an explanatory note rather than looking like a bug")
	}
}

func TestListExceptionsCapsRowsAndReportsTheTrueTotal(t *testing.T) {
	txns := make([]models.Transaction, 0, 60)
	for i := 0; i < 60; i++ {
		txns = append(txns, models.Transaction{
			Date: day(2026, 1, 1).AddDate(0, 0, i), Description: "UNKNOWN VENDOR " + string(rune('A'+i%26)),
			Category: "Misc", Amount: float64(-10 - i), TransactionType: models.Outflow,
		})
	}
	deps, _ := newDeps(t, txns)
	cs := connect(t, deps)

	out := decodeToolResult[listExceptionsOutput](t, call(t, cs, "list_exceptions", map[string]any{
		"bucket": "unmatched", "limit": 5,
	}))
	if out.Unmatched.Total != 60 {
		t.Errorf("total = %d, want the full 60 matches, not the returned count", out.Unmatched.Total)
	}
	if out.Unmatched.Returned != 5 || len(out.Unmatched.Rows) != 5 {
		t.Errorf("returned = %d / %d rows, want 5", out.Unmatched.Returned, len(out.Unmatched.Rows))
	}
}

func TestListExceptionsRejectsAnUnknownBucket(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)
	msg := toolErrorText(t, call(t, cs, "list_exceptions", map[string]any{"bucket": "weird"}))
	if !strings.Contains(msg, "weird") {
		t.Errorf("error should name the bad bucket, got: %s", msg)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
go test ./internal/services/mcpsvc/curate/ -run TestListExceptions -v
```

Expected: FAIL to compile.

- [ ] **Step 3: Write `exceptions.go`**

```go
package curate

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultExceptionLimit = 50
	maxExceptionLimit     = 200
)

type listExceptionsInput struct {
	Bucket    string  `json:"bucket,omitempty" jsonschema:"which bucket to return: unmatched, anomalous, or new_merchants; omit for all three"`
	StartDate string  `json:"start_date,omitempty" jsonschema:"earliest date to include, inclusive, YYYY-MM-DD"`
	EndDate   string  `json:"end_date,omitempty" jsonschema:"latest date to include, inclusive, YYYY-MM-DD"`
	Search    string  `json:"search,omitempty" jsonschema:"case-insensitive substring matched against the row's description"`
	MinAmount float64 `json:"min_amount,omitempty" jsonschema:"smallest absolute dollar amount to include; 0 means unset, not \"greater than zero\""`
	MaxAmount float64 `json:"max_amount,omitempty" jsonschema:"largest absolute dollar amount to include; 0 means unset"`
	Limit     int     `json:"limit,omitempty" jsonschema:"rows per bucket, default 50, maximum 200; each bucket still reports its full total"`
}

type exceptionRow struct {
	Hash        string  `json:"hash"`
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Amount      float64 `json:"amount"`
	// OverThreshold is set on unmatched rows at or above the notable-amount
	// threshold reported alongside the buckets.
	OverThreshold bool `json:"over_threshold,omitempty"`
	// The remaining fields are populated for anomalous rows only.
	MajorExpenseID   string  `json:"major_expense_id,omitempty"`
	MajorExpenseName string  `json:"major_expense_name,omitempty"`
	ExpectedMin      float64 `json:"expected_min,omitempty"`
	ExpectedMax      float64 `json:"expected_max,omitempty"`
	// FirstSeen is populated for new_merchants rows only.
	FirstSeen string `json:"first_seen,omitempty"`
}

type bucketView struct {
	Total    int            `json:"total"`
	Returned int            `json:"returned"`
	Rows     []exceptionRow `json:"rows"`
}

type listExceptionsOutput struct {
	Start                 string      `json:"start"`
	End                   string      `json:"end"`
	Threshold             float64     `json:"threshold"`
	NewMerchantWindowDays int         `json:"new_merchant_window_days"`
	Unmatched             *bucketView `json:"unmatched,omitempty"`
	Anomalous             *bucketView `json:"anomalous,omitempty"`
	NewMerchants          *bucketView `json:"new_merchants,omitempty"`
	Note                  string      `json:"note,omitempty"`
}

// keepRow applies the text and amount filters shared by all three buckets.
func keepRow(r exceptionRow, search string, min, max float64) bool {
	if search != "" && !strings.Contains(strings.ToLower(r.Description), strings.ToLower(search)) {
		return false
	}
	amt := r.Amount
	if amt < 0 {
		amt = -amt
	}
	if min > 0 && amt < min {
		return false
	}
	if max > 0 && amt > max {
		return false
	}
	return true
}

// buildBucket filters, reports the true match count, and returns at most
// limit rows. Total is the number that matched, not the number returned, so a
// caller cannot mistake a truncated list for the whole bucket.
func buildBucket(rows []exceptionRow, in listExceptionsInput, limit int) *bucketView {
	kept := make([]exceptionRow, 0, len(rows))
	for _, r := range rows {
		if keepRow(r, in.Search, in.MinAmount, in.MaxAmount) {
			kept = append(kept, r)
		}
	}
	total := len(kept)
	if len(kept) > limit {
		kept = kept[:limit]
	}
	return &bucketView{Total: total, Returned: len(kept), Rows: kept}
}

func registerListExceptions(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_exceptions",
		Description: "List the transactions the Major Expenses page flags for attention, in three buckets. " +
			"`unmatched`: in-window outflows that matched no declared major expense, biggest first -- these are " +
			"the spending the user has not labelled yet, and `over_threshold` marks the ones at or above the " +
			"notable-amount threshold reported in `threshold`. `anomalous`: transactions that DID match a " +
			"declared expense but whose amount fell outside that expense's own expected range, with the range " +
			"included so the gap is visible; an explicitly pinned transaction is never called anomalous, " +
			"because the user has already said it belongs. `new_merchants`: descriptions never seen before the " +
			"trailing window reported in `new_merchant_window_days`, counted relative to the last transaction " +
			"in range rather than today. Only OUTFLOWS are considered; income is not an exception. Amounts are " +
			"SIGNED exactly as stored, so a purchase is negative and a refund positive. Every row carries the " +
			"`hash` that pin_transactions needs; hashes come from date + lower-cased description + amount, so " +
			"two identical-looking transactions share one and pinning either pins both. search/min_amount/" +
			"max_amount narrow every bucket you asked for; min_amount and max_amount compare against the " +
			"ABSOLUTE amount, and 0 means the bound is unset. Each bucket's `total` is the full number of " +
			"matches and `returned` is how many rows came back, so check them against each other before " +
			"concluding you have seen everything. Transactions the user has already resolved as duplicates are " +
			"excluded. This tool reads only.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listExceptionsInput) (res *mcp.CallToolResult, out listExceptionsOutput, err error) {
		defer recoverToError("list_exceptions", &err)

		bucket := strings.ToLower(strings.TrimSpace(in.Bucket))
		switch bucket {
		case "", "unmatched", "anomalous", "new_merchants":
		default:
			return nil, listExceptionsOutput{}, fmt.Errorf(
				"bucket %q is not recognized; use \"unmatched\", \"anomalous\", \"new_merchants\", or omit it for all three", in.Bucket)
		}

		limit := in.Limit
		if limit <= 0 {
			limit = defaultExceptionLimit
		}
		if limit > maxExceptionLimit {
			limit = maxExceptionLimit
		}

		v, err := deps.pageView(in.StartDate, in.EndDate)
		if err != nil {
			return nil, listExceptionsOutput{}, err
		}

		out = listExceptionsOutput{
			Start:                 formatDay(v.Start),
			End:                   formatDay(v.End),
			Threshold:             v.Match.Exceptions.Threshold,
			NewMerchantWindowDays: v.Match.Exceptions.NewWindowDays,
		}

		matched := 0
		if bucket == "" || bucket == "unmatched" {
			// Biggest-first by absolute amount, matching the page: the rows
			// worth labelling are at the top, and the long sub-threshold tail
			// follows.
			unmatched := append([]models.Transaction(nil), v.Match.Unmatched...)
			sort.Slice(unmatched, func(i, j int) bool {
				return unmatched[i].AbsAmount() > unmatched[j].AbsAmount()
			})
			rows := make([]exceptionRow, 0, len(unmatched))
			for _, t := range unmatched {
				r := exceptionRow{
					Hash: t.Hash, Date: t.Date.Format("2006-01-02"), Description: t.Label(),
					Category: t.Category, Amount: t.Amount,
				}
				r.OverThreshold = v.Match.Exceptions.Threshold > 0 && t.AbsAmount() >= v.Match.Exceptions.Threshold
				rows = append(rows, r)
			}
			out.Unmatched = buildBucket(rows, in, limit)
			matched += out.Unmatched.Total
		}
		if bucket == "" || bucket == "anomalous" {
			rows := make([]exceptionRow, 0, len(v.Match.Exceptions.Anomalous))
			for _, a := range v.Match.Exceptions.Anomalous {
				rows = append(rows, exceptionRow{
					Hash: a.Transaction.Hash, Date: a.Transaction.Date.Format("2006-01-02"),
					Description: a.Transaction.Label(), Category: a.Transaction.Category,
					Amount: a.Transaction.Amount, MajorExpenseID: a.MajorExpenseID,
					MajorExpenseName: a.MajorExpenseName, ExpectedMin: a.ExpectedMin, ExpectedMax: a.ExpectedMax,
				})
			}
			out.Anomalous = buildBucket(rows, in, limit)
			matched += out.Anomalous.Total
		}
		if bucket == "" || bucket == "new_merchants" {
			rows := make([]exceptionRow, 0, len(v.Match.Exceptions.NewMerchants))
			for _, n := range v.Match.Exceptions.NewMerchants {
				rows = append(rows, exceptionRow{
					Hash: n.Transaction.Hash, Date: n.Transaction.Date.Format("2006-01-02"),
					Description: n.Transaction.Label(), Category: n.Transaction.Category,
					Amount: n.Transaction.Amount, FirstSeen: n.FirstSeen.Format("2006-01-02"),
				})
			}
			out.NewMerchants = buildBucket(rows, in, limit)
			matched += out.NewMerchants.Total
		}

		if matched == 0 {
			out.Note = "nothing matched; either the filters are too narrow, the window holds no outflows, or every in-window outflow is already matched to a declared major expense"
		}
		return nil, out, nil
	})
}
```

- [ ] **Step 4: Add it to `Register`**

In `register.go`:

```go
// Register adds the curation tools to s.
func Register(s *mcp.Server, deps Deps) {
	registerListExpenses(s, deps)
	registerListExceptions(s, deps)
}
```

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/services/mcpsvc/curate/ -v
```

Expected: PASS.

- [ ] **Step 6: Bump the tool-count test**

In `internal/services/mcpsvc/server_test.go`: rename to `TestNewServerRegistersAllFourteenTools`, add `"list_exceptions"`, change `13` to `14`.

- [ ] **Step 7: Verify and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add -A
git commit -m "feat(mcp): add list_exceptions

Returns the Major Expenses page's three buckets with the hash each row needs
to be acted on. Each bucket reports its full match count separately from the
number of rows returned, so a truncated list cannot read as a complete one."
```

---

### Task 5: `pin_transactions`

The first write tool. It snapshots `transaction_pins.json` before changing it, and refuses a filter that matches more rows than the cap rather than making a large edit on a model's say-so.

**Files:**
- Create: `internal/services/mcpsvc/curate/pins.go`
- Create: `internal/services/mcpsvc/curate/pins_test.go`
- Modify: `internal/services/mcpsvc/curate/register.go`
- Modify: `internal/services/mcpsvc/server_test.go` (14 → 15)

**Interfaces:**
- Consumes: `Deps.pageView`, `Deps.Snapshots`, `PinStore.SetTransactionPins`, `transactionPinsFile` (Task 3).
- Produces: `func registerPin(s *mcp.Server, deps Deps)`; `type pinFilter`; `type pinInput`; `type pinOutput`.

- [ ] **Step 1: Write the failing test**

Create `internal/services/mcpsvc/curate/pins_test.go`:

```go
package curate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"budget2/internal/models"
)

func mortgageExpense(t *testing.T, deps Deps) {
	t.Helper()
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"mortgage"},
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
}

func TestPinTransactionsAttachesNamedHashesAndSnapshotsFirst(t *testing.T) {
	deps, dir := newDeps(t, ledger())
	mortgageExpense(t, deps)
	// Seed the pins file so there is something to snapshot; Ensure treats a
	// missing source as an error the write must abort on.
	if _, err := deps.Pins.SetTransactionPins(map[string]string{"seed": "me-mortgage"}); err != nil {
		t.Fatalf("seed pins: %v", err)
	}
	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	cs := connect(t, deps)

	out := decodeToolResult[pinOutput](t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage",
		"hashes":     []any{roof.ComputeHash()},
	}))
	if out.Changed != 1 || out.Matched != 1 {
		t.Errorf("matched/changed = %d/%d, want 1/1", out.Matched, out.Changed)
	}
	if out.SnapshotPath == "" {
		t.Fatal("a write must report the snapshot taken before it")
	}
	if _, err := os.Stat(out.SnapshotPath); err != nil {
		t.Errorf("snapshot %s does not exist: %v", out.SnapshotPath, err)
	}
	pins, err := deps.Pins.LoadTransactionPins()
	if err != nil {
		t.Fatalf("LoadTransactionPins: %v", err)
	}
	if pins[roof.ComputeHash()] != "me-mortgage" {
		t.Errorf("pin not written: %+v", pins)
	}
	if _, err := os.Stat(filepath.Join(dir, "transaction_pins.json")); err != nil {
		t.Errorf("pins file missing: %v", err)
	}
}

func TestPinTransactionsUnpinsWithoutAnExpenseID(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	mortgageExpense(t, deps)
	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	if _, err := deps.Pins.SetTransactionPins(map[string]string{roof.ComputeHash(): "me-mortgage"}); err != nil {
		t.Fatalf("seed pin: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[pinOutput](t, call(t, cs, "pin_transactions", map[string]any{
		"unpin":  true,
		"hashes": []any{roof.ComputeHash()},
	}))
	if !out.Unpinned || out.Changed != 1 {
		t.Errorf("unpinned/changed = %v/%d, want true/1", out.Unpinned, out.Changed)
	}
	pins, _ := deps.Pins.LoadTransactionPins()
	if _, still := pins[roof.ComputeHash()]; still {
		t.Errorf("pin survived the unpin: %+v", pins)
	}
}

func TestPinTransactionsPinsEveryRowMatchingAFilter(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{ID: "me-home", Name: "Home Repair"}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	if _, err := deps.Pins.SetTransactionPins(map[string]string{"seed": "me-home"}); err != nil {
		t.Fatalf("seed pins: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[pinOutput](t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-home",
		"filter":     map[string]any{"search": "roofing", "unmatched_only": true},
	}))
	if out.Matched != 1 || out.Changed != 1 {
		t.Errorf("matched/changed = %d/%d, want 1/1", out.Matched, out.Changed)
	}
	if len(out.Hashes) != 1 {
		t.Errorf("hashes = %v, want the one row acted on", out.Hashes)
	}
}

func TestPinTransactionsRefusesAFilterWiderThanTheCap(t *testing.T) {
	txns := make([]models.Transaction, 0, maxBulkPin+5)
	for i := 0; i < maxBulkPin+5; i++ {
		txns = append(txns, models.Transaction{
			Date: day(2026, 1, 1).AddDate(0, 0, i), Description: "WIDE VENDOR", Category: "Misc",
			Amount: float64(-10 - i), TransactionType: models.Outflow,
		})
	}
	deps, _ := newDeps(t, txns)
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{ID: "me-wide", Name: "Wide"}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-wide",
		"filter":     map[string]any{"search": "wide"},
	}))
	if !strings.Contains(msg, "narrow") {
		t.Errorf("the refusal must tell the caller to narrow the filter, got: %s", msg)
	}
	pins, _ := deps.Pins.LoadTransactionPins()
	if len(pins) != 0 {
		t.Errorf("a refused bulk pin must write nothing, got %d pins", len(pins))
	}
}

func TestPinTransactionsRejectsAnUnknownExpense(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)
	msg := toolErrorText(t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "nope", "hashes": []any{"abc"},
	}))
	if !strings.Contains(msg, "nope") {
		t.Errorf("error should name the missing expense, got: %s", msg)
	}
}

func TestPinTransactionsRejectsAmbiguousOrEmptyTargeting(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	mortgageExpense(t, deps)
	cs := connect(t, deps)

	both := toolErrorText(t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage", "hashes": []any{"abc"},
		"filter": map[string]any{"search": "x"},
	}))
	if both == "" {
		t.Error("supplying both hashes and filter must be refused")
	}
	neither := toolErrorText(t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage",
	}))
	if neither == "" {
		t.Error("supplying neither hashes nor filter must be refused")
	}
}

func TestPinTransactionsReportsAFilterThatMatchedNothing(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	mortgageExpense(t, deps)
	cs := connect(t, deps)

	out := decodeToolResult[pinOutput](t, call(t, cs, "pin_transactions", map[string]any{
		"expense_id": "me-mortgage",
		"filter":     map[string]any{"search": "no such merchant"},
	}))
	if out.Matched != 0 || out.Changed != 0 {
		t.Errorf("matched/changed = %d/%d, want 0/0", out.Matched, out.Changed)
	}
	if out.Note == "" {
		t.Error("a filter that matched nothing must say so rather than looking like a silent success")
	}
	if out.SnapshotPath != "" {
		t.Error("nothing was written, so nothing should have been snapshotted")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
go test ./internal/services/mcpsvc/curate/ -run TestPin -v
```

Expected: FAIL to compile.

- [ ] **Step 3: Write `pins.go`**

```go
package curate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxBulkPin caps a filter-driven pin. The Major Expenses page has no cap,
// but it has a human looking at the filtered list before clicking; a tool
// does not, so a filter wider than this is refused with its match count
// rather than applied.
const maxBulkPin = 200

type pinFilter struct {
	StartDate     string  `json:"start_date,omitempty" jsonschema:"earliest date to include, inclusive, YYYY-MM-DD"`
	EndDate       string  `json:"end_date,omitempty" jsonschema:"latest date to include, inclusive, YYYY-MM-DD"`
	Search        string  `json:"search,omitempty" jsonschema:"case-insensitive substring matched against the transaction's description"`
	Category      string  `json:"category,omitempty" jsonschema:"exact category name"`
	MinAmount     float64 `json:"min_amount,omitempty" jsonschema:"smallest absolute dollar amount to include; 0 means unset"`
	MaxAmount     float64 `json:"max_amount,omitempty" jsonschema:"largest absolute dollar amount to include; 0 means unset"`
	UnmatchedOnly bool    `json:"unmatched_only,omitempty" jsonschema:"restrict to transactions that currently match no declared major expense"`
}

type pinInput struct {
	ExpenseID string     `json:"expense_id,omitempty" jsonschema:"id of the major expense to pin to, from list_major_expenses; required unless unpin is true"`
	Hashes    []string   `json:"hashes,omitempty" jsonschema:"transaction hashes to act on, from list_exceptions, list_major_expenses or search_transactions; supply this or filter, not both"`
	Filter    *pinFilter `json:"filter,omitempty" jsonschema:"act on every outflow matching these conditions instead of named hashes; supply this or hashes, not both"`
	Unpin     bool       `json:"unpin,omitempty" jsonschema:"remove the pins instead of setting them, so the transactions fall back to keyword and amount matching; expense_id is then ignored"`
}

type pinOutput struct {
	ExpenseID    string   `json:"expense_id,omitempty"`
	ExpenseName  string   `json:"expense_name,omitempty"`
	Unpinned     bool     `json:"unpinned"`
	Matched      int      `json:"matched"`
	Changed      int      `json:"changed"`
	Hashes       []string `json:"hashes"`
	SnapshotPath string   `json:"snapshot_path,omitempty"`
	Note         string   `json:"note,omitempty"`
}

// resolveFilter returns the hashes of every in-window outflow the filter
// selects, in a deterministic order. It runs over the same view the read
// tools report, so what a caller saw in list_exceptions is what a filter here
// selects.
func (d Deps) resolveFilter(f pinFilter) ([]string, error) {
	v, err := d.pageView(f.StartDate, f.EndDate)
	if err != nil {
		return nil, err
	}

	candidates := make([]models.Transaction, 0)
	if f.UnmatchedOnly {
		candidates = append(candidates, v.Match.Unmatched...)
	} else {
		for _, group := range v.Match.Groups {
			candidates = append(candidates, group...)
		}
		candidates = append(candidates, v.Match.Unmatched...)
	}

	seen := make(map[string]bool, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, t := range candidates {
		if t.Hash == "" || seen[t.Hash] {
			continue
		}
		if f.Category != "" && !strings.EqualFold(t.Category, f.Category) {
			continue
		}
		if f.Search != "" && !strings.Contains(strings.ToLower(t.Label()), strings.ToLower(f.Search)) {
			continue
		}
		amt := t.AbsAmount()
		if f.MinAmount > 0 && amt < f.MinAmount {
			continue
		}
		if f.MaxAmount > 0 && amt > f.MaxAmount {
			continue
		}
		seen[t.Hash] = true
		out = append(out, t.Hash)
	}
	sort.Strings(out)
	return out, nil
}

func registerPin(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "pin_transactions",
		Description: "Attach transactions to a declared major expense, or detach them. THIS WRITES TO THE " +
			"USER'S DATA. A pin is a manual override: it wins over the expense's keywords and amount range, so " +
			"it is how a transaction that should belong to an expense but does not look like it gets counted " +
			"there. Target the transactions EITHER by naming `hashes` (from list_exceptions, " +
			"list_major_expenses or search_transactions) OR by giving a `filter`, never both. A filter selects " +
			"in-window outflows the same way the read tools report them, and is REFUSED if it selects more " +
			"than 200 -- narrow it and call again rather than expecting a partial write. Set `unpin` to true " +
			"to remove pins instead; the transactions then fall back to keyword and amount matching, and " +
			"expense_id is ignored. A hash is derived from date + lower-cased description + amount, so two " +
			"genuinely distinct transactions sharing all three share one hash and are pinned or unpinned " +
			"TOGETHER. `matched` is how many transactions were targeted and `changed` how many pins actually " +
			"differed, so changed can be smaller when some were already pinned where you asked. The pins file " +
			"is copied to a .bak before this session's first change to it; later changes in the same session " +
			"are not separately recoverable. An already-open Major Expenses page does NOT refresh itself -- it " +
			"shows stale data until reloaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pinInput) (res *mcp.CallToolResult, out pinOutput, err error) {
		defer recoverToError("pin_transactions", &err)

		hasHashes := len(in.Hashes) > 0
		hasFilter := in.Filter != nil
		if hasHashes == hasFilter {
			return nil, pinOutput{}, fmt.Errorf(
				"supply exactly one of hashes or filter: hashes names the transactions directly, filter selects them by condition")
		}

		expenseName := ""
		if !in.Unpin {
			if strings.TrimSpace(in.ExpenseID) == "" {
				return nil, pinOutput{}, fmt.Errorf("expense_id is required unless unpin is true")
			}
			expenses, err := deps.Expenses.LoadMajorExpenses()
			if err != nil {
				return nil, pinOutput{}, err
			}
			for _, e := range expenses {
				if e.ID == in.ExpenseID {
					expenseName = e.Name
					break
				}
			}
			if expenseName == "" {
				return nil, pinOutput{}, fmt.Errorf(
					"no major expense has id %q; call list_major_expenses for the current ids, or create one with upsert_major_expense", in.ExpenseID)
			}
		}

		var hashes []string
		if hasHashes {
			seen := make(map[string]bool, len(in.Hashes))
			for _, h := range in.Hashes {
				if h = strings.TrimSpace(h); h != "" && !seen[h] {
					seen[h] = true
					hashes = append(hashes, h)
				}
			}
		} else {
			hashes, err = deps.resolveFilter(*in.Filter)
			if err != nil {
				return nil, pinOutput{}, err
			}
			if len(hashes) > maxBulkPin {
				return nil, pinOutput{}, fmt.Errorf(
					"that filter selects %d transactions, over the %d limit for one call; narrow it (a tighter date range, a more specific search, or an amount bound) and call again",
					len(hashes), maxBulkPin)
			}
		}

		out = pinOutput{
			ExpenseID: in.ExpenseID, ExpenseName: expenseName, Unpinned: in.Unpin,
			Matched: len(hashes), Hashes: hashes,
		}
		if in.Unpin {
			out.ExpenseID = ""
		}
		if len(hashes) == 0 {
			out.Hashes = []string{}
			out.Note = "nothing was targeted, so nothing was written; the filter matched no in-window outflow"
			return nil, out, nil
		}

		// Before the write, never after: a failed snapshot must abort it.
		snapPath, err := deps.Snapshots.Ensure(transactionPinsFile, time.Now())
		if err != nil {
			return nil, pinOutput{}, err
		}
		out.SnapshotPath = snapPath

		target := in.ExpenseID
		if in.Unpin {
			target = "" // SetTransactionPins deletes on an empty expense id.
		}
		updates := make(map[string]string, len(hashes))
		for _, h := range hashes {
			updates[h] = target
		}
		changed, err := deps.Pins.SetTransactionPins(updates)
		if err != nil {
			return nil, pinOutput{}, err
		}
		out.Changed = changed
		if changed == 0 {
			out.Note = "every targeted transaction was already in that state; nothing changed"
		}
		return nil, out, nil
	})
}
```

- [ ] **Step 4: Add it to `Register`**

```go
func Register(s *mcp.Server, deps Deps) {
	registerListExpenses(s, deps)
	registerListExceptions(s, deps)
	registerPin(s, deps)
}
```

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/services/mcpsvc/curate/ -v
```

Expected: PASS. If `TestPinTransactionsAttachesNamedHashesAndSnapshotsFirst` fails on the snapshot because `transaction_pins.json` does not exist, that is the intended abort-before-write behavior — the test seeds the file for exactly this reason. Do **not** soften `Ensure` to tolerate a missing source.

- [ ] **Step 6: Bump the tool-count test**

`server_test.go`: rename to `TestNewServerRegistersAllFifteenTools`, add `"pin_transactions"`, change `14` to `15`.

- [ ] **Step 7: Verify and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add -A
git commit -m "feat(mcp): add pin_transactions

Pins and unpins transactions against a declared major expense, by named hash
or by filter. A filter selecting more than 200 rows is refused with its count
rather than applied: the page's bulk-pin button has a human looking at the
filtered list first, and a tool does not. The pins file is snapshotted before
this session's first change to it."
```

---

### Task 6: `upsert_major_expense`

The sparse-write problem the spec calls out for `/whatif/apply` applies here directly: `dataloader.UpdateMajorExpense` copies `Name`, `Keywords`, `ExpectedMin`, `ExpectedMax`, `Notes` and `IsInternalTransfer` wholesale from its argument, so a partial update built from a zero struct would silently blank every field the caller did not mention. Optional scalars are therefore pointers, and `keywords` uses nil-vs-empty-slice.

The validation rules themselves are **extracted, not duplicated**: they currently live inline in `internal/handlers/majorexpenses.parseExpenseForm` and move to `internal/services/majorexpenses.Validate`, which both the handler and this tool call. A service must not import a handlers package, and a private copy would let the page and the tools drift apart on what a valid definition is with nothing to catch it.

**Files:**
- Create: `internal/services/majorexpenses/validate.go`
- Create: `internal/services/majorexpenses/validate_test.go`
- Modify: `internal/handlers/majorexpenses/handlers.go:515-575` (`parseExpenseForm` delegates)
- Create: `internal/services/mcpsvc/curate/upsert.go`
- Create: `internal/services/mcpsvc/curate/upsert_test.go`
- Modify: `internal/services/mcpsvc/curate/register.go`
- Modify: `internal/services/mcpsvc/server_test.go` (15 → 16)

**Interfaces:**
- Consumes: `ExpenseStore.LoadMajorExpenses/AddMajorExpense/UpdateMajorExpense`, `PinStore.SetTransactionPins`, `Deps.Snapshots`, `majorExpensesFile`, `transactionPinsFile`, `majorExpenseRow` (Tasks 3, 5).
- Produces: `func majorexpenses.Validate(models.MajorExpense) error`; `func registerUpsert(s *mcp.Server, deps Deps)`; `type upsertInput`; `type upsertOutput`.

- [ ] **Step 0a: Enumerate what depends on the rules before moving them**

Run the `LSP` tool `findReferences` on `parseExpenseForm` (`internal/handlers/majorexpenses/handlers.go:515`) and report the union of files containing a caller — including test files — before editing. Do not derive this list from test-function names.

`parseExpenseForm` itself is NOT moving: it still parses the HTTP form. Only the rules it applies after parsing move. Its existing tests must therefore keep passing **unchanged**, which is the extraction's own regression check.

- [ ] **Step 0b: Record the before-coverage**

```bash
go test -coverprofile=/tmp/before-svc.out ./internal/services/majorexpenses/ && go tool cover -func=/tmp/before-svc.out | tail -1
go test -coverprofile=/tmp/before-hnd.out ./internal/handlers/majorexpenses/ && go tool cover -func=/tmp/before-hnd.out | tail -1
```

- [ ] **Step 0c: Write `internal/services/majorexpenses/validate.go`**

The error strings are copied **byte-for-byte** from `parseExpenseForm`. They are what the handler's existing tests assert on, and changing one turns a pure extraction into a behavior change.

```go
package majorexpenses

import (
	"fmt"
	"strings"

	"budget2/internal/models"
)

// maxNameLen bounds a definition's display name.
const maxNameLen = 200

// Validate reports whether a major-expense definition is one the app will
// accept. It is the single source of these rules: the Major Expenses page
// applies them to a parsed HTML form, and the MCP curation tools apply them
// to a tool call, and the two must not drift.
//
// A definition is valid in exactly three configurations:
//
//  1. At least one keyword. An amount range is then optional and is used only
//     to flag anomalies, not to decide whether a transaction matches.
//  2. No keywords, but BOTH ExpectedMin and ExpectedMax set. This matches by
//     amount alone, which is how a fixed-dollar charge whose description
//     varies gets captured; setting them equal matches that one amount.
//  3. No keywords and no bounds at all: a pin-only target, which matches
//     nothing automatically and collects transactions the user pins to it by
//     hand.
//
// Setting exactly one bound with no keyword is the rejected case. It matches
// nothing on its own and almost always means the other bound was forgotten.
//
// Validate reads Name with surrounding whitespace ignored but does not modify
// its argument; callers that persist the definition are expected to have
// trimmed it already.
func Validate(me models.MajorExpense) error {
	name := strings.TrimSpace(me.Name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if len(name) > maxNameLen {
		return fmt.Errorf("name is too long (max %d chars)", maxNameLen)
	}
	if me.ExpectedMin < 0 {
		return fmt.Errorf("expected_min cannot be negative")
	}
	if me.ExpectedMax < 0 {
		return fmt.Errorf("expected_max cannot be negative")
	}
	if me.ExpectedMin > 0 && me.ExpectedMax > 0 && me.ExpectedMin > me.ExpectedMax {
		return fmt.Errorf("expected_min cannot exceed expected_max")
	}
	if len(me.Keywords) == 0 && (me.ExpectedMin > 0) != (me.ExpectedMax > 0) {
		return fmt.Errorf("set BOTH Min and Max to match by amount, or leave both blank to create a pin-only target")
	}
	// A transfer filter only makes sense if it can match something
	// automatically -- pin-only doesn't filter at load time. Require at least
	// a keyword or an amount rule.
	if me.IsInternalTransfer && len(me.Keywords) == 0 && me.ExpectedMin == 0 && me.ExpectedMax == 0 {
		return fmt.Errorf("internal-transfer filter needs at least one keyword or an amount range to match against")
	}
	return nil
}
```

- [ ] **Step 0d: Write `internal/services/majorexpenses/validate_test.go`**

```go
package majorexpenses

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestValidateAcceptsTheThreeValidShapes(t *testing.T) {
	cases := map[string]models.MajorExpense{
		"keyword only":            {Name: "Mortgage", Keywords: []string{"mortgage"}},
		"keyword with a range":    {Name: "Mortgage", Keywords: []string{"mortgage"}, ExpectedMin: 1900, ExpectedMax: 2100},
		"amount only, both bounds": {Name: "Quarterly Check", ExpectedMin: 450, ExpectedMax: 450},
		"pin-only target":         {Name: "Amazon — Books"},
		"transfer with a keyword": {Name: "Transfer", Keywords: []string{"xfer"}, IsInternalTransfer: true},
	}
	for name, me := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Validate(me); err != nil {
				t.Errorf("Validate(%+v) = %v, want nil", me, err)
			}
		})
	}
}

func TestValidateRejectsTheInvalidShapes(t *testing.T) {
	cases := []struct {
		name string
		me   models.MajorExpense
		want string
	}{
		{"no name", models.MajorExpense{Keywords: []string{"x"}}, "name is required"},
		{"blank name", models.MajorExpense{Name: "   ", Keywords: []string{"x"}}, "name is required"},
		{"name too long", models.MajorExpense{Name: strings.Repeat("a", 201), Keywords: []string{"x"}}, "too long"},
		{"negative min", models.MajorExpense{Name: "A", ExpectedMin: -1}, "expected_min cannot be negative"},
		{"negative max", models.MajorExpense{Name: "A", ExpectedMax: -1}, "expected_max cannot be negative"},
		{"min above max", models.MajorExpense{Name: "A", ExpectedMin: 100, ExpectedMax: 10}, "cannot exceed"},
		{"only min, no keyword", models.MajorExpense{Name: "A", ExpectedMin: 100}, "set BOTH Min and Max"},
		{"only max, no keyword", models.MajorExpense{Name: "A", ExpectedMax: 100}, "set BOTH Min and Max"},
		{"transfer with nothing to match", models.MajorExpense{Name: "A", IsInternalTransfer: true}, "internal-transfer filter needs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.me)
			if err == nil {
				t.Fatalf("Validate(%+v) = nil, want an error mentioning %q", tc.me, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestValidateAllowsOnlyAMaxWhenAKeywordIsPresent pins the asymmetry: the
// both-or-neither rule applies only when there is no keyword, because a
// keyword-matched group uses a one-sided bound purely for anomaly detection.
func TestValidateAllowsOnlyAMaxWhenAKeywordIsPresent(t *testing.T) {
	if err := Validate(models.MajorExpense{Name: "A", Keywords: []string{"x"}, ExpectedMax: 100}); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}
```

- [ ] **Step 0e: Delegate from the handler**

In `internal/handlers/majorexpenses/handlers.go`, `parseExpenseForm` keeps its parsing and its two `invalid expected_min/expected_max: %w` wrapping errors — those are parse failures, not rule violations — and hands the assembled definition to the service for the rules:

```go
// parseExpenseForm extracts a MajorExpense from form values without
// stamping ID/timestamps — those are set by the storage layer or
// preserved on update. The rules for what makes a definition valid live
// in majorexpenseengine.Validate, shared with the MCP curation tools so
// the page and the tools cannot disagree about what is acceptable.
func parseExpenseForm(r *http.Request) (models.MajorExpense, error) {
	expectedMin, err := parseFormFloat(r, "expected_min")
	if err != nil {
		return models.MajorExpense{}, fmt.Errorf("invalid expected_min: %w", err)
	}
	expectedMax, err := parseFormFloat(r, "expected_max")
	if err != nil {
		return models.MajorExpense{}, fmt.Errorf("invalid expected_max: %w", err)
	}

	me := models.MajorExpense{
		Name:               strings.TrimSpace(r.FormValue("name")),
		Keywords:           splitAndTrim(r.FormValue("keywords"), ","),
		ExpectedMin:        expectedMin,
		ExpectedMax:        expectedMax,
		Notes:              strings.TrimSpace(r.FormValue("notes")),
		IsInternalTransfer: parseFormBool(r, "is_internal_transfer"),
	}
	if err := majorexpenseengine.Validate(me); err != nil {
		return models.MajorExpense{}, err
	}
	return me, nil
}
```

Note the one deliberate ordering change: both amount fields are now parsed before the name is checked, so a request with both a blank name and an unparseable amount reports the amount error rather than the name error. If any existing test asserts the opposite, restore the original order by parsing the name first and calling `Validate` at the end — the rules are what is being shared, not the sequencing.

- [ ] **Step 0f: Run the affected packages and record the after-coverage**

```bash
go test ./internal/services/majorexpenses/ ./internal/handlers/majorexpenses/
go test -coverprofile=/tmp/after-svc.out ./internal/services/majorexpenses/ && go tool cover -func=/tmp/after-svc.out | tail -1
go test -coverprofile=/tmp/after-hnd.out ./internal/handlers/majorexpenses/ && go tool cover -func=/tmp/after-hnd.out | tail -1
```

Expected: PASS, with **no edits to any existing handler test**. Report all four coverage numbers. If a handler test had to change, say exactly which and why — that means the extraction changed behavior and is no longer a move.

- [ ] **Step 0g: Commit the extraction on its own**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add -A
git commit -m "refactor(majorexpenses): extract definition validation into the service

The MCP curation tools need the same rules the Major Expenses form applies,
and a service may not import a handlers package. Sharing them beats a second
copy that can drift: the page and the tools would disagree about what a valid
definition is with nothing to catch it. parseExpenseForm keeps its parsing
and its parse-failure wrapping; only the rules moved, error strings included."
```

- [ ] **Step 1: Write the failing test**

Create `internal/services/mcpsvc/curate/upsert_test.go`:

```go
package curate

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestUpsertCreatesAnExpenseAndReturnsItsID(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)

	out := decodeToolResult[upsertOutput](t, call(t, cs, "upsert_major_expense", map[string]any{
		"name": "Mortgage", "keywords": []any{"mortgage"}, "expected_min": 1900.0, "expected_max": 2100.0,
	}))
	if !out.Created || out.ID == "" {
		t.Fatalf("created/id = %v/%q, want a new id", out.Created, out.ID)
	}
	list, err := deps.Expenses.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("LoadMajorExpenses: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Mortgage" || list[0].ExpectedMax != 2100 {
		t.Errorf("stored = %+v", list)
	}
}

// TestUpsertLeavesUnmentionedFieldsAlone is the load-bearing test of this
// task: UpdateMajorExpense copies every field from its argument, so a sparse
// update assembled from a zero struct would blank keywords and the range.
func TestUpsertLeavesUnmentionedFieldsAlone(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"mortgage", "escrow"},
		ExpectedMin: 1900, ExpectedMax: 2100, Notes: "original",
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	out := decodeToolResult[upsertOutput](t, call(t, cs, "upsert_major_expense", map[string]any{
		"id": "me-mortgage", "notes": "refinanced 2026",
	}))
	if out.Created {
		t.Error("naming an existing id must update, not create")
	}
	list, _ := deps.Expenses.LoadMajorExpenses()
	got := list[0]
	if got.Notes != "refinanced 2026" {
		t.Errorf("notes = %q, want the update applied", got.Notes)
	}
	if len(got.Keywords) != 2 || got.ExpectedMin != 1900 || got.ExpectedMax != 2100 || got.Name != "Mortgage" {
		t.Errorf("an update mentioning only notes changed something else: %+v", got)
	}
}

func TestUpsertClearsKeywordsOnlyWithAnExplicitEmptyList(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-pinonly", Name: "Pin Only", Keywords: []string{"amazon"},
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	cs := connect(t, deps)

	call(t, cs, "upsert_major_expense", map[string]any{"id": "me-pinonly", "keywords": []any{}})
	list, _ := deps.Expenses.LoadMajorExpenses()
	if len(list[0].Keywords) != 0 {
		t.Errorf("keywords = %v, want cleared by the explicit empty list", list[0].Keywords)
	}
}

func TestUpsertRejectsTheSameConfigurationsThePageRejects(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"no name on create", map[string]any{"keywords": []any{"x"}}, "name"},
		{"negative min", map[string]any{"name": "A", "expected_min": -5.0}, "negative"},
		{"min above max", map[string]any{"name": "A", "expected_min": 100.0, "expected_max": 10.0}, "exceed"},
		{"half a range with no keyword", map[string]any{"name": "A", "expected_min": 100.0}, "BOTH"},
		{"transfer with nothing to match", map[string]any{"name": "A", "is_internal_transfer": true}, "internal-transfer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := toolErrorText(t, call(t, cs, "upsert_major_expense", tc.args))
			if !strings.Contains(msg, tc.want) {
				t.Errorf("error %q does not mention %q", msg, tc.want)
			}
		})
	}
}

func TestUpsertAcceptsAPinOnlyTarget(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)
	out := decodeToolResult[upsertOutput](t, call(t, cs, "upsert_major_expense", map[string]any{
		"name": "Amazon — Books",
	}))
	if !out.Created {
		t.Error("a pin-only target (no keywords, no range) is a valid configuration on the page and must be here")
	}
}

func TestUpsertCanCreateAndPinInOneCall(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	cs := connect(t, deps)

	out := decodeToolResult[upsertOutput](t, call(t, cs, "upsert_major_expense", map[string]any{
		"name": "Roof", "pin_hash": roof.ComputeHash(),
	}))
	if !out.Pinned {
		t.Fatal("pin_hash should have been pinned to the new expense")
	}
	pins, _ := deps.Pins.LoadTransactionPins()
	if pins[roof.ComputeHash()] != out.ID {
		t.Errorf("pins = %+v, want the hash attached to %s", pins, out.ID)
	}
}

func TestUpsertRejectsAnUnknownID(t *testing.T) {
	deps, _ := newDeps(t, ledger())
	cs := connect(t, deps)
	msg := toolErrorText(t, call(t, cs, "upsert_major_expense", map[string]any{"id": "nope", "notes": "x"}))
	if !strings.Contains(msg, "nope") {
		t.Errorf("error should name the missing id, got: %s", msg)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
go test ./internal/services/mcpsvc/curate/ -run TestUpsert -v
```

Expected: FAIL to compile.

- [ ] **Step 3: Write `upsert.go`**

```go
package curate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/majorexpenses"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type upsertInput struct {
	ID                 string    `json:"id,omitempty" jsonschema:"id of an existing expense to edit, from list_major_expenses; omit to create a new one"`
	Name               *string   `json:"name,omitempty" jsonschema:"display name, required when creating; max 200 characters"`
	Keywords           []string  `json:"keywords,omitempty" jsonschema:"case-insensitive substrings matched against a transaction's description; omit to leave unchanged, pass an empty list to clear"`
	ExpectedMin        *float64  `json:"expected_min,omitempty" jsonschema:"low end of the expected amount, as a positive dollar figure; with expected_max equal it matches that exact amount, and a wider range flags anything outside it as anomalous"`
	ExpectedMax        *float64  `json:"expected_max,omitempty" jsonschema:"high end of the expected amount, as a positive dollar figure"`
	Notes              *string   `json:"notes,omitempty" jsonschema:"free-text note shown with the expense"`
	IsInternalTransfer *bool     `json:"is_internal_transfer,omitempty" jsonschema:"treat matches as money moving between the user's own accounts, dropping them from spending totals instead of counting them as spending"`
	PinHash            string   `json:"pin_hash,omitempty" jsonschema:"a transaction hash to pin to this expense in the same call, so the transaction that prompted the expense is matched even if the keywords would not have caught it"`
}

type upsertOutput struct {
	ID           string          `json:"id"`
	Created      bool            `json:"created"`
	Expense      majorExpenseRow `json:"expense"`
	Pinned       bool            `json:"pinned"`
	SnapshotPath string          `json:"snapshot_path,omitempty"`
}

// trimKeywords drops blank entries, matching the page's splitAndTrim. A
// non-nil but fully blank list still clears the keywords.
func trimKeywords(in []string) []string {
	out := make([]string, 0, len(in))
	for _, k := range in {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

func registerUpsert(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "upsert_major_expense",
		Description: "Create a declared major expense, or edit an existing one. THIS WRITES TO THE USER'S " +
			"DATA. Omit `id` to create; give an `id` from list_major_expenses to edit. On an edit, every field " +
			"you omit KEEPS ITS CURRENT VALUE -- to clear the keywords pass an explicitly empty list, and to " +
			"clear an amount bound pass 0. A definition is valid in exactly three shapes: at least one keyword " +
			"(an amount range is then optional and only flags anomalies); no keywords but BOTH expected_min " +
			"and expected_max set, which matches by amount alone and is how fixed-dollar charges with varying " +
			"descriptions are captured (setting them equal matches that one amount); or no keywords and no " +
			"range at all, a pin-only target you attach transactions to with pin_transactions. Setting only " +
			"one bound without a keyword is refused. expected_min/expected_max are POSITIVE dollar figures " +
			"even though the transactions they match are negative. is_internal_transfer marks the entry as " +
			"money moving between the user's own accounts: its matches are dropped from spending totals " +
			"instead of counted as spending, which changes what every other tool reports, so do not set it " +
			"unless the user says the money did not leave their household. pin_hash pins one transaction to " +
			"the expense in the same call, which is how you make sure the charge that prompted the expense is " +
			"matched even when the keywords would have missed it. The definitions file is copied to a .bak " +
			"before this session's first change to it. An already-open Major Expenses page does NOT refresh " +
			"itself -- it shows stale data until reloaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in upsertInput) (res *mcp.CallToolResult, out upsertOutput, err error) {
		defer recoverToError("upsert_major_expense", &err)

		existing, err := deps.Expenses.LoadMajorExpenses()
		if err != nil {
			return nil, upsertOutput{}, err
		}

		var (
			target  models.MajorExpense
			create  = strings.TrimSpace(in.ID) == ""
			found   bool
		)
		if !create {
			for _, e := range existing {
				if e.ID == in.ID {
					target, found = e, true
					break
				}
			}
			if !found {
				return nil, upsertOutput{}, fmt.Errorf(
					"no major expense has id %q; call list_major_expenses for the current ids, or omit id to create a new expense", in.ID)
			}
		}

		// Apply only what the caller actually sent. UpdateMajorExpense copies
		// every one of these fields from its argument, so anything not merged
		// in here would be blanked on disk.
		if in.Name != nil {
			target.Name = strings.TrimSpace(*in.Name)
		}
		if in.Keywords != nil {
			target.Keywords = trimKeywords(in.Keywords)
		}
		if in.ExpectedMin != nil {
			target.ExpectedMin = *in.ExpectedMin
		}
		if in.ExpectedMax != nil {
			target.ExpectedMax = *in.ExpectedMax
		}
		if in.Notes != nil {
			target.Notes = strings.TrimSpace(*in.Notes)
		}
		if in.IsInternalTransfer != nil {
			target.IsInternalTransfer = *in.IsInternalTransfer
		}

		// The page's own rules, shared rather than restated, so a definition
		// the Major Expenses form would refuse is refused here identically.
		if err := majorexpenses.Validate(target); err != nil {
			return nil, upsertOutput{}, err
		}

		// Before the write, never after: a failed snapshot must abort it. A
		// create is the one case where the definitions file may legitimately
		// not exist yet, and Ensure treats a missing source as an error, so a
		// first-ever create skips the snapshot -- there is no prior state to
		// recover.
		var snapPath string
		if len(existing) > 0 {
			snapPath, err = deps.Snapshots.Ensure(majorExpensesFile, time.Now())
			if err != nil {
				return nil, upsertOutput{}, err
			}
		}

		if create {
			target.ID = uuid.New().String()
			if _, err := deps.Expenses.AddMajorExpense(target); err != nil {
				return nil, upsertOutput{}, err
			}
		} else if _, err := deps.Expenses.UpdateMajorExpense(target.ID, target); err != nil {
			return nil, upsertOutput{}, err
		}

		out = upsertOutput{
			ID:      target.ID,
			Created: create,
			Expense: majorExpenseRow{
				ID: target.ID, Name: target.Name, Keywords: trimKeywords(target.Keywords),
				ExpectedMin: target.ExpectedMin, ExpectedMax: target.ExpectedMax,
				Notes: target.Notes, IsInternalTransfer: target.IsInternalTransfer,
			},
			SnapshotPath: snapPath,
		}
		if out.Expense.Keywords == nil {
			out.Expense.Keywords = []string{}
		}

		// Pin failure does not roll back the create, matching the page's
		// create-and-pin affordance: the definition is the durable part and
		// the pin can be reapplied with pin_transactions.
		if h := strings.TrimSpace(in.PinHash); h != "" {
			if _, err := deps.Pins.SetTransactionPins(map[string]string{h: target.ID}); err == nil {
				out.Pinned = true
			}
		}
		return nil, out, nil
	})
}
```

- [ ] **Step 4: Add it to `Register`**

```go
func Register(s *mcp.Server, deps Deps) {
	registerListExpenses(s, deps)
	registerListExceptions(s, deps)
	registerPin(s, deps)
	registerUpsert(s, deps)
}
```

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/services/mcpsvc/curate/ -v
```

Expected: PASS. If the go-sdk's schema inference rejects a `*float64` or `*bool` field, do not switch to non-pointer types — that reintroduces the sparse-write bug. Report it and stop; the fallback is a `clear` string list, which needs a decision.

- [ ] **Step 6: Bump the tool-count test**

`server_test.go`: rename to `TestNewServerRegistersAllSixteenTools`, add `"upsert_major_expense"`, change `15` to `16`.

- [ ] **Step 7: Verify and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add -A
git commit -m "feat(mcp): add upsert_major_expense

Optional scalars are pointers and keywords is nil-vs-empty, so an update
that mentions one field cannot blank the others: UpdateMajorExpense copies
every field from its argument, which is the same sparse-write trap that made
/whatif/apply necessary. Validation mirrors the page's parseExpenseForm
rules, including the three valid definition shapes."
```

---

### Task 7: `delete_major_expense`

**Files:**
- Create: `internal/services/mcpsvc/curate/delete.go`
- Create: `internal/services/mcpsvc/curate/delete_test.go`
- Modify: `internal/services/mcpsvc/curate/register.go`
- Modify: `internal/services/mcpsvc/server_test.go` (16 → 17)

**Interfaces:**
- Consumes: `ExpenseStore.ArchiveMajorExpense/RestoreMajorExpense/LoadMajorExpenses/LoadDeletedMajorExpenses`, `PinStore.LoadTransactionPins`, `Deps.Snapshots`, the three file-name constants (Tasks 3, 5).
- Produces: `func registerDelete(s *mcp.Server, deps Deps)`; `type deleteInput`; `type deleteOutput`.

- [ ] **Step 1: Write the failing test**

Create `internal/services/mcpsvc/curate/delete_test.go`:

```go
package curate

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func seedForDelete(t *testing.T) (Deps, string) {
	t.Helper()
	deps, dir := newDeps(t, ledger())
	if _, err := deps.Expenses.AddMajorExpense(models.MajorExpense{
		ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"mortgage"},
	}); err != nil {
		t.Fatalf("AddMajorExpense: %v", err)
	}
	roof := models.Transaction{Date: day(2026, 2, 14), Description: "ACME ROOFING", Amount: -4500}
	if _, err := deps.Pins.SetTransactionPins(map[string]string{roof.ComputeHash(): "me-mortgage"}); err != nil {
		t.Fatalf("seed pin: %v", err)
	}
	return deps, dir
}

func TestDeleteArchivesTheExpenseAndDetachesItsPins(t *testing.T) {
	deps, _ := seedForDelete(t)
	cs := connect(t, deps)

	out := decodeToolResult[deleteOutput](t, call(t, cs, "delete_major_expense",
		map[string]any{"id": "me-mortgage"}))
	if out.Restored || out.Name != "Mortgage" {
		t.Errorf("out = %+v, want an archive of Mortgage", out)
	}
	if out.PinsDetached != 1 {
		t.Errorf("pins_detached = %d, want 1", out.PinsDetached)
	}
	if len(out.SnapshotPaths) == 0 {
		t.Error("a write must report the snapshots taken before it")
	}
	active, _ := deps.Expenses.LoadMajorExpenses()
	if len(active) != 0 {
		t.Errorf("expense still active: %+v", active)
	}
	deleted, _ := deps.Expenses.LoadDeletedMajorExpenses()
	if len(deleted) != 1 || deleted[0].Expense.ID != "me-mortgage" {
		t.Errorf("archive = %+v, want the expense preserved for restore", deleted)
	}
	pins, _ := deps.Pins.LoadTransactionPins()
	if len(pins) != 0 {
		t.Errorf("pins survived the archive: %+v", pins)
	}
}

func TestDeleteWithRestoreBringsTheExpenseAndItsPinsBack(t *testing.T) {
	deps, _ := seedForDelete(t)
	cs := connect(t, deps)
	call(t, cs, "delete_major_expense", map[string]any{"id": "me-mortgage"})

	out := decodeToolResult[deleteOutput](t, call(t, cs, "delete_major_expense",
		map[string]any{"id": "me-mortgage", "restore": true}))
	if !out.Restored {
		t.Error("restored flag not set")
	}
	if out.PinsRestored != 1 {
		t.Errorf("pins_restored = %d, want 1", out.PinsRestored)
	}
	active, _ := deps.Expenses.LoadMajorExpenses()
	if len(active) != 1 || active[0].ID != "me-mortgage" {
		t.Errorf("active = %+v, want the expense back", active)
	}
	deleted, _ := deps.Expenses.LoadDeletedMajorExpenses()
	if len(deleted) != 0 {
		t.Errorf("archive = %+v, want it emptied", deleted)
	}
}

func TestDeleteRejectsAnUnknownID(t *testing.T) {
	deps, _ := seedForDelete(t)
	cs := connect(t, deps)

	msg := toolErrorText(t, call(t, cs, "delete_major_expense", map[string]any{"id": "nope"}))
	if !strings.Contains(msg, "nope") {
		t.Errorf("error should name the missing id, got: %s", msg)
	}
	restoreMsg := toolErrorText(t, call(t, cs, "delete_major_expense",
		map[string]any{"id": "nope", "restore": true}))
	if !strings.Contains(restoreMsg, "nope") {
		t.Errorf("restore error should name the missing id, got: %s", restoreMsg)
	}
}

func TestDeleteRequiresAnID(t *testing.T) {
	deps, _ := seedForDelete(t)
	cs := connect(t, deps)
	if msg := toolErrorText(t, call(t, cs, "delete_major_expense", map[string]any{})); msg == "" {
		t.Error("an empty id must be refused")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
go test ./internal/services/mcpsvc/curate/ -run TestDelete -v
```

Expected: FAIL to compile.

- [ ] **Step 3: Write `delete.go`**

```go
package curate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type deleteInput struct {
	ID      string `json:"id" jsonschema:"id of the major expense, from list_major_expenses (or from the deleted list when restoring)"`
	Restore bool   `json:"restore,omitempty" jsonschema:"bring a previously deleted expense back instead of deleting one"`
}

type deleteOutput struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Restored      bool     `json:"restored"`
	PinsDetached  int      `json:"pins_detached,omitempty"`
	PinsRestored  int      `json:"pins_restored,omitempty"`
	SnapshotPaths []string `json:"snapshot_paths"`
	Note          string   `json:"note,omitempty"`
}

// countPinsTo returns how many transactions are currently pinned to id.
func countPinsTo(pins map[string]string, id string) int {
	n := 0
	for _, target := range pins {
		if target == id {
			n++
		}
	}
	return n
}

func registerDelete(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "delete_major_expense",
		Description: "Delete a declared major expense, or restore one that was deleted. THIS WRITES TO THE " +
			"USER'S DATA. Deleting is a SOFT delete, the same one the Major Expenses page performs: the " +
			"definition is moved to an archive along with a record of every transaction pinned to it, so " +
			"calling this again with restore set to true brings both back. Nothing about the transactions " +
			"themselves changes -- they simply stop being grouped under this expense, so they reappear as " +
			"unmatched in list_exceptions and the app's unmatched spending total grows by their sum. A " +
			"restore reattaches a captured pin only if that transaction is not currently pinned somewhere " +
			"else, so a hash reassigned in the meantime keeps its newer pin. Use list_major_expenses with " +
			"include_deleted to see what can be restored. The affected files are each copied to a .bak before " +
			"this session's first change to them. An already-open Major Expenses page does NOT refresh itself " +
			"-- it shows stale data until reloaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteInput) (res *mcp.CallToolResult, out deleteOutput, err error) {
		defer recoverToError("delete_major_expense", &err)

		id := strings.TrimSpace(in.ID)
		if id == "" {
			return nil, deleteOutput{}, fmt.Errorf("id is required; call list_major_expenses for the current ids")
		}

		// Resolve the name and the pin count from the side the operation
		// reads FROM, before anything is written.
		var name string
		if in.Restore {
			deleted, err := deps.Expenses.LoadDeletedMajorExpenses()
			if err != nil {
				return nil, deleteOutput{}, err
			}
			for _, d := range deleted {
				if d.Expense.ID == id {
					name = d.Expense.Name
					break
				}
			}
			if name == "" {
				return nil, deleteOutput{}, fmt.Errorf(
					"no deleted major expense has id %q; call list_major_expenses with include_deleted to see what can be restored", id)
			}
		} else {
			active, err := deps.Expenses.LoadMajorExpenses()
			if err != nil {
				return nil, deleteOutput{}, err
			}
			for _, e := range active {
				if e.ID == id {
					name = e.Name
					break
				}
			}
			if name == "" {
				return nil, deleteOutput{}, fmt.Errorf(
					"no active major expense has id %q; call list_major_expenses for the current ids", id)
			}
		}

		pinsBefore, err := deps.Pins.LoadTransactionPins()
		if err != nil {
			return nil, deleteOutput{}, err
		}

		// Before the write, never after: a failed snapshot must abort it.
		// Archive and restore each rewrite all three files, so all three are
		// backed up; a missing one has no prior state to lose, so its absence
		// is not fatal here the way it is for a single-file write.
		var paths []string
		now := time.Now()
		for _, f := range []string{majorExpensesFile, deletedMajorExpensesFile, transactionPinsFile} {
			p, err := deps.Snapshots.Ensure(f, now)
			if err != nil {
				continue
			}
			paths = append(paths, p)
		}
		if len(paths) == 0 {
			return nil, deleteOutput{}, fmt.Errorf(
				"refusing to write: none of %s, %s or %s could be copied to a backup first",
				majorExpensesFile, deletedMajorExpensesFile, transactionPinsFile)
		}

		if in.Restore {
			if err := deps.Expenses.RestoreMajorExpense(id); err != nil {
				return nil, deleteOutput{}, err
			}
		} else if err := deps.Expenses.ArchiveMajorExpense(id); err != nil {
			return nil, deleteOutput{}, err
		}

		pinsAfter, err := deps.Pins.LoadTransactionPins()
		if err != nil {
			return nil, deleteOutput{}, err
		}

		out = deleteOutput{ID: id, Name: name, Restored: in.Restore, SnapshotPaths: paths}
		if in.Restore {
			out.PinsRestored = countPinsTo(pinsAfter, id) - countPinsTo(pinsBefore, id)
			if out.PinsRestored < 0 {
				out.PinsRestored = 0
			}
		} else {
			out.PinsDetached = countPinsTo(pinsBefore, id) - countPinsTo(pinsAfter, id)
			if out.PinsDetached < 0 {
				out.PinsDetached = 0
			}
			out.Note = "deleted expenses are recoverable: call this again with restore set to true, or use the Major Expenses page"
		}
		return nil, out, nil
	})
}
```

- [ ] **Step 4: Add it to `Register`**

```go
func Register(s *mcp.Server, deps Deps) {
	registerListExpenses(s, deps)
	registerListExceptions(s, deps)
	registerPin(s, deps)
	registerUpsert(s, deps)
	registerDelete(s, deps)
}
```

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/services/mcpsvc/curate/ -v
```

Expected: PASS.

- [ ] **Step 6: Bump the tool-count test**

`server_test.go`: rename to `TestNewServerRegistersAllSeventeenTools`, add `"delete_major_expense"`, change `16` to `17`. The final wanted list is the twelve from Phase 1/2 plus `list_major_expenses`, `list_exceptions`, `pin_transactions`, `upsert_major_expense`, `delete_major_expense`.

- [ ] **Step 7: Verify and commit**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
git add -A
git commit -m "feat(mcp): add delete_major_expense

Soft-deletes through ArchiveMajorExpense, the same path the page uses, and
restores through RestoreMajorExpense with restore:true, so a model that
deletes the wrong expense can put it back without the browser."
```

---

### Task 8: Server instructions, README, and the phase's own verification

`serverInstructions` claims a "COMPLETE list of all six" spend tools' sign conventions and is pinned by `TestServerInstructionsCarryLoadBearingClaims`. Five tools that report money have just arrived; both the text and the pinned claims must account for them.

**Files:**
- Modify: `internal/services/mcpsvc/server.go` (`serverInstructions`)
- Modify: `internal/services/mcpsvc/server_test.go` (`TestServerInstructionsCarryLoadBearingClaims`)
- Modify: `README.md:312-330`
- Modify: `docs/superpowers/specs/2026-08-12-app-wide-mcp-design.md` (phase 3 → Implemented)

- [ ] **Step 1: Extend `serverInstructions`**

Keep the existing text exactly as it is, with one correction and one addition.

The correction: the sentence currently reads `"Expense amounts are not signed the same way across the spending tools -- this is the COMPLETE list of all six, not a sample:"`. Change `all six` to `all six SPENDING tools`, so the completeness claim stays true and stays scoped:

```go
		"change. Expense amounts are not signed the same way across the spending tools -- this is the " +
		"COMPLETE list of all six SPENDING tools, not a sample: signed in search_transactions (expenses " +
```

The addition, appended after the final sentence about duplicate exclusion:

```go
		" There are also five curation tools covering the user's declared \"major expenses\" -- their own " +
		"labels for spending they already understand. list_major_expenses and list_exceptions read; " +
		"pin_transactions, upsert_major_expense and delete_major_expense WRITE TO THE USER'S DATA and have " +
		"no in-app undo beyond the .bak copy each takes before its first change of a session, so confirm " +
		"with the user before calling one. In these tools a per-expense `total` is NET SPEND and normally " +
		"POSITIVE (a refund reduces it, and a total can go negative), while a per-transaction `amount` is " +
		"SIGNED as stored, negative for a purchase -- the same split the spending tools use. Transactions " +
		"are addressed by `hash`, derived from date + lower-cased description + amount, so two " +
		"identical-looking transactions share one hash and are pinned together. Only outflows are matched " +
		"against major expenses; income never is. Pages other than the what-if planner do not refresh " +
		"themselves, so a curation write leaves an already-open Major Expenses tab showing stale data."
```

- [ ] **Step 2: Extend the pinned claims test**

In `TestServerInstructionsCarryLoadBearingClaims`, update `"COMPLETE list of all six"` to `"COMPLETE list of all six SPENDING tools"` and add these to the list:

```go
		// The five curation tools: which read, which write, and the sign
		// split that differs from the spending tools' own.
		"five curation tools",
		"pin_transactions, upsert_major_expense and delete_major_expense WRITE",
		"`total` is NET SPEND and normally POSITIVE",
		"`amount` is SIGNED as stored",
		"two identical-looking transactions share one hash",
		"Only outflows are matched against major expenses",
```

- [ ] **Step 3: Run the mcpsvc tests**

```bash
go test ./internal/services/mcpsvc/...
```

Expected: PASS.

- [ ] **Step 4: Update the README**

In `README.md`, after the "Six spending tools..." paragraph block, add:

```markdown
Five curation tools cover the Major Expenses page. `list_major_expenses` returns
the declared expenses with each one's in-window match count and net total, and
`list_exceptions` returns the three buckets the page flags — unmatched outflows,
transactions outside their expense's expected range, and first-time merchants.
The other three **write**: `pin_transactions` attaches or detaches transactions
by hash or by filter (a filter selecting more than 200 is refused rather than
applied), `upsert_major_expense` creates or edits a definition with omitted
fields left untouched, and `delete_major_expense` soft-deletes — and, with
`restore`, undoes that. Each write copies the file it changes to a `.bak` under
`<backup-dir>/mcp-snapshots/data` before this session's first change to it.
Only the what-if page polls for changes, so an MCP curation write leaves an
already-open Major Expenses tab stale until it is reloaded.
```

Also change the "Six planner tools" sentence's surrounding context if it now reads as the complete inventory; the section should make clear the server exposes seventeen tools in three groups.

- [ ] **Step 5: Mark Phase 3 implemented in the spec**

In `docs/superpowers/specs/2026-08-12-app-wide-mcp-design.md`, under "## Phases", change item 3 to:

```markdown
3. **`curate`.** Major-expense reads and writes. **Implemented.** All five tools
   (`list_major_expenses`, `list_exceptions`, `pin_transactions`,
   `upsert_major_expense`, `delete_major_expense`) are registered.
   `pin_transactions` also unpins and `delete_major_expense` also restores, so
   every write has a reversal that does not require the browser.
   `Snapshotter` moved to `internal/services/mcpsvc/snapshot` — a leaf package
   rather than `mcpsvc` itself, because `mcpsvc` imports `plan` and a
   `*mcpsvc.Snapshotter` in `plan.Deps` would be an import cycle.
```

Also update the `curate` row of the "Tool surface" table's preamble if it still implies the writes are one-way.

- [ ] **Step 6: Full verification**

```bash
go build ./... && go vet ./... && go test ./... && staticcheck ./...
```

Expected: all four clean. Then confirm the diff touches only what was intended:

```bash
git status && git diff --stat master
```

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "docs(mcp): describe the curation tools to the model and the reader

serverInstructions is read on every connection and is the model's closest
thing to a system prompt; its 'COMPLETE list of all six' sign-convention
claim is now scoped to the spending tools, and the curation tools' own sign
split, hash semantics and write/undo story are stated there rather than left
to be inferred from five separate descriptions."
```

- [ ] **Step 8: Manual verification against the page**

Not automatable, and it is the check this phase most needs — the whole design premise is that a tool answer and the page cannot disagree.

```bash
go run ./cmd/server
```

Then, against the running server:

1. Open `http://localhost:8080/major-expenses`. Note the declared total, the unmatched count and total, and the counts in the three exception cards.
2. Call `list_major_expenses` and `list_exceptions` with no arguments. The totals and counts must match the page's, for the page's default window.
3. Create a throwaway pin-only expense with `upsert_major_expense`, pin one unmatched transaction to it with `pin_transactions`, and reload the page: the transaction must appear under that expense, marked as pinned.
4. Unpin it (`pin_transactions` with `unpin: true`) and delete the expense (`delete_major_expense`), then reload: the page must be back to its original state, with the expense in the deleted panel.
5. Confirm `.bak` files exist under `<backup-dir>/mcp-snapshots/data`.

Report any disagreement between a tool figure and the page rather than adjusting the tool to match — a mismatch means the pipeline diverged from `buildPageData`, which is the bug this phase is designed not to have.

---

## Self-review notes

**Spec coverage.** All five Phase 3 tools have a task (3, 4, 5, 6, 7). The spec's write-safety requirement (`Snapshotter` → the owning service's write path → report the change) is Task 1 plus the snapshot step in Tasks 5–7. The spec's "known limitation" that non-what-if pages do not poll is stated in three tool descriptions and in `serverInstructions`. The spec's error policy (locked storage names `/unlock`; empty results carry a note rather than erroring) is in `Deps.load` and in the `Note` fields of Tasks 3, 4, 5.

**Deliberate deviations, both justified inline:**
1. `Snapshotter` goes to `mcpsvc/snapshot`, not `mcpsvc` — the spec's placement is an import cycle.
2. `pin_transactions` also unpins and `delete_major_expense` also restores. The spec's table lists neither; without them an MCP-driven mistake can only be undone from the browser.
3. Task 6 extracts the definition-validation rules into `internal/services/majorexpenses.Validate` rather than giving `curate` its own copy. The spec's "Where the logic lives" section says Phase 3 needs no extraction, and for the *analysis* that holds — `majorexpenses.Match` was already a service. The validation rules were not, and a second copy of them would let the page and the tools disagree about what a valid definition is with nothing to catch it.

**Deliberate duplications that remain, and why:** `parseWindowDate` is copied from `mcpsvc/spend` (sibling packages, neither may import the other; 8 lines), and the `$100` / 30-day thresholds are copied from the handler (a service may not import a handlers package) but are pinned by `TestThresholdsMatchThePage`, so a change on the page's side that is not mirrored here fails a test rather than silently disagreeing.

**Not in scope, carried to Phase 4:** the guarded two-step confirm-token pattern (`restore_backup`, `set_encryption`, `shutdown_server`), and the still-open question of `/whatif/state` and `/whatif/apply`, which have had no consumer since Phase 1 and remain an unauthenticated JSON write path.
