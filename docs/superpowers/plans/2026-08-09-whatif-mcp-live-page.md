# What-If MCP Live Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the what-if MCP server write ten settings fields to the active retirement scenario, and make an open browser tab notice the change on its own within ~2 seconds.

**Architecture:** A new typed sparse endpoint (`POST /whatif/apply`) reuses the existing `Apply` override logic server-side, wrapped in a single `SettingsManager` write lock. A revision counter bumped at every state-changing site lets a sentinel element poll `GET /whatif/poll`, which returns `204 No Content` when nothing changed and the results partial plus an `HX-Trigger` header when it did. The MCP gains an HTTP client that verifies it is talking to a server about the same plan it reads from disk, and snapshots a scenario before its first write.

**Tech Stack:** Go 1.25+, chi v5, `github.com/modelcontextprotocol/go-sdk/mcp`, html/template, htmx 2.0.4, Playwright (smoke test only).

**Spec:** `docs/superpowers/specs/2026-08-09-whatif-mcp-live-page-design.md`. Read it before Task 1 — it explains *why* the obvious implementations are wrong, and several tasks below will look arbitrary without it.

## Global Constraints

- **Branch:** `feat/whatif-mcp-live-page`. It already exists and holds the spec commits.
- **No new Go module dependencies.** Everything needed is already in `go.mod`.
- **Every commit must pass** `go build ./... && go vet ./... && go test ./... && staticcheck ./...`. A pre-commit hook enforces this; do not bypass it.
- **NEVER pipe test output through `grep`/`head`.** The pipe reports the last command's exit code, so a red suite reads as exit 0. Run the command bare. (This rule is in `CLAUDE.md`; it was violated while writing the spec and produced a wrong call-site count.)
- **Go toolchain:** use `go` as it resolves on PATH. If it is missing, `~/go-sdk/go/bin/go`.
- **Do not add type hints, refactors, or `__init__`-equivalent scaffolding** beyond what a task specifies.
- **Ten writable fields**, verbatim: `monthly_living_expenses`, `projection_years`, `inflation_rate`, `investment_return`, `filing_status`, `roth_conversion_amount`, `roth_conversion_start_year`, `roth_conversion_end_year`, `social_security_claim_age`, `spouse_claim_age`. `healthcare_inflation` is **preview-only** and must be rejected by the write path.

---

### Task 1: Extract the `overrides` package (pure move, no logic change)

`handlers/whatif` will need `Overrides` and `Apply` in Task 6. Having a web handler import `whatifmcp` would make the HTTP layer depend on the MCP layer. Move the settings-mutation vocabulary somewhere both can import.

This task changes **no behavior**. If any test needs editing beyond its package clause and imports, you have moved something you should not have.

**Files:**
- Create: `internal/services/retirement/overrides/overrides.go`
- Create: `internal/services/retirement/overrides/overrides_test.go`
- Modify: `internal/services/whatifmcp/overrides.go`
- Modify: `internal/services/whatifmcp/overrides_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `overrides.Overrides` — struct, eleven pointer fields, JSON tags unchanged
  - `overrides.Apply(base *models.WhatIfSettings, o Overrides) (*models.WhatIfSettings, error)`
  - `whatifmcp.Overrides` remains valid as a type alias

- [ ] **Step 1: Create the new package with the moved code**

Create `internal/services/retirement/overrides/overrides.go`. Move from `internal/services/whatifmcp/overrides.go`, unchanged apart from the package clause and dropped imports: the `Overrides` struct (all field tags verbatim), `Apply`, and `validate`.

```go
// Package overrides is the sparse settings-mutation vocabulary shared by the
// MCP server and the web handlers. A nil pointer means "leave unchanged".
package overrides

import (
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// Overrides is a sparse set of scenario changes. A nil pointer means "leave
// unchanged" — that is why every field is a pointer rather than a value.
type Overrides struct {
	// ... copy all eleven fields verbatim from whatifmcp/overrides.go:14-26
}

// Apply returns a deep copy of base with the overrides applied. base is never
// mutated. Invalid values are rejected before any engine work, naming the field.
func Apply(base *models.WhatIfSettings, o Overrides) (*models.WhatIfSettings, error) {
	// ... copy verbatim from whatifmcp/overrides.go:30-101, including the
	// PerYearOverrides re-attach comment
}

func (o Overrides) validate() error {
	// ... copy verbatim from whatifmcp/overrides.go:103-134
}
```

Do **not** move `preparedWithOverrides` or `RunWithOverrides`. They call `retirement.RunFull` and `engine.New`; moving them would create an import cycle (`retirement` → `overrides` → `retirement`).

- [ ] **Step 2: Replace the moved code in `whatifmcp` with an alias**

In `internal/services/whatifmcp/overrides.go`, delete the struct, `Apply`, and `validate`. Keep `preparedWithOverrides` and `RunWithOverrides`. Add:

```go
import (
	"budget2/internal/services/retirement/overrides"
	// ... existing imports, minus any now unused
)

// Overrides is the shared sparse settings vocabulary. Aliased rather than
// re-declared so tool schemas and existing call sites are unaffected by the
// move to internal/services/retirement/overrides.
type Overrides = overrides.Overrides

// Apply is re-exported so this package's callers need not import both.
var Apply = overrides.Apply
```

`server.go` needs no edit — the alias keeps `Overrides` resolving.

- [ ] **Step 3: Split the test file**

`internal/services/whatifmcp/overrides_test.go` mixes tests that move with tests that stay. Split by target:

Move to `internal/services/retirement/overrides/overrides_test.go` (change `package whatifmcp` → `package overrides`, drop `Apply` qualifiers if any):
- `TestApply_ChangesOnlyTheNamedField` (:19)
- `TestApply_PreservesPerYearOverridesAcrossDeepCopy` (:36)
- `TestApply_RejectsInvalidValuesNamingTheField` (:73)
- `TestApply_EachFieldChangesOnlyItsDestination` (:114)

Keep in `whatifmcp`:
- `TestPreparedWithOverrides_PreservesPerYearOverridesAcrossBoundary` (:57)
- `TestRunWithOverrides_HigherExpensesLowerFinalBalance` (:266)
- `TestRunWithOverrides_OmitsMonteCarlo` (:281)

Both halves use `ptr` and `ptrInt` (:291-292). **Copy them into both files** — they are two-line helpers and sharing them across packages is not worth an export.

- [ ] **Step 4: Verify the move changed nothing**

Run: `go build ./... && go vet ./... && go test ./... && staticcheck ./...`
Expected: all PASS. Same test count as before the move, just distributed across two packages.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/overrides/ internal/services/whatifmcp/overrides.go internal/services/whatifmcp/overrides_test.go
git commit -m "refactor: extract Overrides and Apply into their own package

A web handler will need this vocabulary in the live-page work. Importing
whatifmcp from handlers/whatif would make the HTTP layer depend on the MCP
layer; the true dependency is on settings mutation, which neither owns.

Pure move: no logic change. RunWithOverrides stays in whatifmcp because it
calls retirement.RunFull and would cycle."
```

---

### Task 2: Harden validation for a persisted write

`Apply`'s validation was written when its output was a discarded preview copy. Once the result is saved, three gaps become real: unbounded rate fields can wedge the page, `healthcare_inflation` is unreachable from the UI, and a Roth window set on disabled conversions is a silent no-op.

**Files:**
- Modify: `internal/services/retirement/overrides/overrides.go`
- Modify: `internal/services/retirement/overrides/overrides_test.go`

**Interfaces:**
- Consumes: `overrides.Overrides`, `overrides.Apply` (Task 1)
- Produces: `func (o Overrides) ValidateWritable() error` — returns non-nil when the override set contains a field that may be previewed but not persisted, or a combination that would persist as a no-op. `Apply` does **not** call it; only the write path does.

- [ ] **Step 1: Write the failing tests**

Add to `internal/services/retirement/overrides/overrides_test.go`:

```go
func TestValidate_RejectsAbsurdRates(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    Overrides
		want string
	}{
		{"inflation too high", Overrides{InflationRate: ptr(500)}, "inflation_rate"},
		{"inflation too low", Overrides{InflationRate: ptr(-50)}, "inflation_rate"},
		{"return too high", Overrides{InvestmentReturn: ptr(1000)}, "investment_return"},
		{"return too low", Overrides{InvestmentReturn: ptr(-99)}, "investment_return"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.o.validate()
			if err == nil {
				t.Fatalf("expected an error naming %s, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name the field %q", err, tc.want)
			}
		})
	}
}

func TestValidate_AcceptsPlausibleRates(t *testing.T) {
	o := Overrides{InflationRate: ptr(2.5), InvestmentReturn: ptr(7)}
	if err := o.validate(); err != nil {
		t.Fatalf("plausible rates rejected: %v", err)
	}
	// Zero must stay legal: investment_return 0 means "use the asset allocation".
	if err := (Overrides{InvestmentReturn: ptr(0)}).validate(); err != nil {
		t.Fatalf("investment_return 0 must remain legal: %v", err)
	}
}

func TestValidateWritable_RejectsHealthcareInflation(t *testing.T) {
	err := Overrides{HealthcareInflation: ptr(6)}.ValidateWritable()
	if err == nil {
		t.Fatal("expected healthcare_inflation to be rejected on the write path")
	}
	if !strings.Contains(err.Error(), "healthcare_inflation") {
		t.Fatalf("error %q does not name the field", err)
	}
}

func TestValidateWritable_AllowsTheTenWritableFields(t *testing.T) {
	o := Overrides{
		MonthlyLivingExpenses:  ptr(5000),
		ProjectionYears:        ptrInt(30),
		InflationRate:          ptr(2.5),
		InvestmentReturn:       ptr(7),
		FilingStatus:           ptrStr("married_joint"),
		RothConversionAmount:   ptr(50000),
		RothConversionStart:    ptrInt(1),
		RothConversionEnd:      ptrInt(10),
		SocialSecurityClaimAge: ptrInt(67),
		SpouseClaimAge:         ptrInt(65),
	}
	if err := o.ValidateWritable(); err != nil {
		t.Fatalf("the ten writable fields were rejected: %v", err)
	}
}

func TestValidateWritable_RejectsRothWindowWithoutAmount(t *testing.T) {
	err := Overrides{RothConversionStart: ptrInt(1), RothConversionEnd: ptrInt(5)}.ValidateWritable()
	if err == nil {
		t.Fatal("expected a Roth window with no amount to be rejected")
	}
	if !strings.Contains(err.Error(), "roth_conversion_amount") {
		t.Fatalf("error %q should name the missing field", err)
	}
}

func ptrStr(s string) *string { return &s }
```

Add `"strings"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/services/retirement/overrides/ -run 'TestValidate' -v`
Expected: FAIL — `ValidateWritable` undefined, and the rate tests fail because `validate` currently returns nil for those inputs.

- [ ] **Step 3: Implement**

In `overrides.go`, first add the error type that lets handlers distinguish "the caller sent a bad value" (400) from "the server failed" (500):

```go
// ValidationError marks an override value the caller can fix. Handlers map it
// to 400; anything else is a server-side failure.
type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }
```

Wrap **every** `return fmt.Errorf(...)` in `validate()` and `ValidateWritable()` as `return &ValidationError{Err: fmt.Errorf(...)}`. `Error()` delegates to the wrapped error, so the existing `TestApply_RejectsInvalidValuesNamingTheField` assertions on message content keep passing unchanged.

Then add to `validate()`, before its final `return nil`:

```go
	// Bounds on the rate fields. These were unbounded while Apply's output was
	// a discarded preview copy; once it is persisted, a value that produces a
	// NaN or an engine panic turns every GET /whatif into a 500 via
	// middleware.Recoverer, with no in-app undo. The range is deliberately wide
	// — it rejects nonsense, not unusual plans.
	if r := o.InflationRate; r != nil && (*r < -20 || *r > 50) {
		return fmt.Errorf("inflation_rate must be between -20 and 50 percent, got %v", *r)
	}
	if r := o.InvestmentReturn; r != nil && (*r < -20 || *r > 50) {
		return fmt.Errorf("investment_return must be between -20 and 50 percent, got %v", *r)
	}
```

Then add:

```go
// ValidateWritable reports whether this override set may be persisted, as
// opposed to merely previewed. Apply deliberately does not call it: run_scenario
// is allowed a wider field set than apply_changes.
func (o Overrides) ValidateWritable() error {
	// HealthcareInflation is legacy for the single-person model
	// (models/whatif.go:118). Once HealthcarePersons is populated — which the
	// migration in settings.go:309-330 does for any plan with MonthlyHealthcare
	// > 0 — it is read only by analysis/present_value.go. It has no form control
	// anywhere in web/templates/, so persisting it would write a value the user
	// can neither see nor revert, and which does not move the charts.
	if o.HealthcareInflation != nil {
		return fmt.Errorf("healthcare_inflation cannot be saved: it is legacy for the single-person healthcare model, has no control in the UI, and does not affect the projection once healthcare persons are configured; use run_scenario to preview it")
	}
	// A Roth window with no amount is a silent no-op when conversions are
	// disabled (followups §5). Harmless as a preview, a broken contract as a write.
	if (o.RothConversionStart != nil || o.RothConversionEnd != nil) && o.RothConversionAmount == nil {
		return fmt.Errorf("roth_conversion_start_year/end_year cannot be saved without roth_conversion_amount: the window has no effect unless conversions are enabled by a non-zero amount")
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/services/retirement/overrides/ -v`
Expected: PASS, including the pre-existing `TestApply_*` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/overrides/
git commit -m "feat(overrides): bound the rate fields and gate the writable set

Validation was written for a preview copy that got discarded. Persisting the
result changes the stakes: an absurd rate that NaNs or panics the engine turns
every GET /whatif into a 500 through middleware.Recoverer, and there is no
in-app undo.

ValidateWritable is separate from validate because run_scenario is allowed a
wider field set than apply_changes -- healthcare_inflation stays previewable
while being unwritable."
```

---

### Task 3: Revision counter with an explicit bump at every state-changing site

The page needs to know *that* something changed without recomputing *what*. One monotonic integer does it.

The counter must bump wherever observable state changes — which is not the same as wherever a file is written. `RenameScenario` writes directly via `sm.store.WriteFile`, `DeleteScenario` can silently revert the active scenario, and backup restore replaces the whole directory. Conversely `loadInternalContext` calls `saveInternal` on a *read* when a migration fires, and must **not** bump.

**Files:**
- Modify: `internal/services/retirement/settings.go`
- Modify: `internal/services/retirement/settings_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func (sm *SettingsManager) Revision() int` — current counter, RLock-guarded
  - `func (sm *SettingsManager) saveInternalAndBump(settings *models.WhatIfSettings) error` — private; caller must hold the write lock

- [ ] **Step 1: Write the failing tests**

Add to `internal/services/retirement/settings_test.go`:

```go
func TestRevision_BumpsOnSave(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)

	settings, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	before := sm.Revision()
	if err := sm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := sm.Revision(); got <= before {
		t.Fatalf("Revision did not advance across Save: %d -> %d", before, got)
	}
}

func TestRevision_BumpsOnSwitchAndCreateScenario(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)
	if _, err := sm.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	beforeCreate := sm.Revision()
	if _, err := sm.CreateScenario("alt"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	afterCreate := sm.Revision()
	if afterCreate <= beforeCreate {
		t.Fatalf("CreateScenario did not bump: %d -> %d", beforeCreate, afterCreate)
	}

	if err := sm.SwitchScenario("whatif.json"); err != nil {
		t.Fatalf("SwitchScenario: %v", err)
	}
	if got := sm.Revision(); got <= afterCreate {
		t.Fatalf("SwitchScenario did not bump: %d -> %d", afterCreate, got)
	}
}

func TestRevision_DoesNotBumpOnCacheMissLoad(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)
	if _, err := sm.Load(); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	sm.InvalidateCache()
	afterInvalidate := sm.Revision()

	// A cache-miss load may internally re-save when decode reports a migration.
	// That is a read, not a change: the page must not re-render for it.
	if _, err := sm.Load(); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if got := sm.Revision(); got != afterInvalidate {
		t.Fatalf("a cache-miss load bumped the revision: %d -> %d", afterInvalidate, got)
	}
}

func TestRevision_RaceClean(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)
	settings, err := sm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sm.Save(settings)
			_ = sm.Revision()
		}()
	}
	wg.Wait()
	if sm.Revision() < 8 {
		t.Fatalf("expected at least 8 bumps, got %d", sm.Revision())
	}
}
```

Add `"sync"` to the test file's imports if absent.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/services/retirement/ -run TestRevision -v`
Expected: FAIL — `sm.Revision` undefined.

- [ ] **Step 3: Add the field, accessor, and bumping wrapper**

In `settings.go`, add to the struct (`settings.go:~120`):

```go
type SettingsManager struct {
	settingsDir string
	filename    string
	store       *storage.Storage
	mu          sync.RWMutex
	cache       *models.WhatIfSettings

	// revision advances whenever something changes what the what-if page
	// should display. It exists so a polling page can detect a change without
	// recomputing the analysis. In-memory and not persisted: it only has to be
	// monotonic within one process, because a page load reads the current value
	// as its baseline.
	revision int
}
```

Add near `Save`:

```go
// Revision returns the current display revision.
func (sm *SettingsManager) Revision() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.revision
}

// bumpLocked advances the revision. Caller must hold the write lock.
func (sm *SettingsManager) bumpLocked() {
	sm.revision++
}

// saveInternalAndBump is saveInternal plus a revision bump. Every mutation path
// calls this; saveInternal itself is left un-bumping because loadInternalContext
// calls it on a *read* when decode reports a migration, and a cache-miss load
// must not make every open page re-render.
func (sm *SettingsManager) saveInternalAndBump(settings *models.WhatIfSettings) error {
	if err := sm.saveInternal(settings); err != nil {
		return err
	}
	sm.bumpLocked()
	return nil
}
```

- [ ] **Step 4: Redirect the mutation call sites**

Replace `sm.saveInternal(` with `sm.saveInternalAndBump(` at **every** call site in `settings.go` **except** the one inside `loadInternalContext` (around `:541`, in the migration branch) and the one inside `saveInternalAndBump` itself.

Find them with:

```bash
grep -n "sm.saveInternal(" internal/services/retirement/settings.go
```

Expected: 23 call sites. Leave `loadInternalContext`'s alone; convert the other 22. Read each line before editing — do not blanket-replace.

- [ ] **Step 5: Add bumps to the non-save state changes**

These change what the page shows without going through `saveInternal`. Add `sm.bumpLocked()` inside each, while the write lock is held:

- `SwitchScenario` (`:1590`) — after `sm.cache = nil`
- `DeleteScenario` (`:1716-1727`) — after the removal and any active-scenario reconciliation
- `RenameScenario` (`:1772`) — after the direct `sm.store.WriteFile`
- `InvalidateCache` (`:419-423`) — after clearing the cache
- `BeginExternalRewrite`'s returned `end()` (`:487-496`) — after the cache drop

The backup-restore case is the one that matters most: `BeginExternalRewrite` rewrites the settings directory wholesale with no `saveInternal` call, so without this bump every open tab would show pre-restore figures indefinitely — and the user, now trusting the page to update itself, would have no reason to reload.

If any of these methods does not currently hold the write lock at the point you add the call, use `sm.mu.Lock()`/`defer sm.mu.Unlock()` consistent with its neighbors rather than calling `Revision()` re-entrantly (that would deadlock on `sync.RWMutex`).

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/services/retirement/ -run TestRevision -race -v`
Expected: PASS, no race reports.

Then the full suite: `go test ./... -race`
Expected: PASS. If an existing test now fails, a call site was converted that should not have been.

- [ ] **Step 7: Commit**

```bash
git add internal/services/retirement/settings.go internal/services/retirement/settings_test.go
git commit -m "feat(settings): add a display revision counter

Bumped at every site that changes what the what-if page should show, not
merely where a file is written: RenameScenario writes directly via
store.WriteFile, DeleteScenario can silently revert the active scenario, and
BeginExternalRewrite (backup restore) replaces the whole directory. Missing
the last one would leave every open tab on pre-restore figures forever.

Suppressed on loadInternalContext's migration save, which is a read."
```

---

### Task 4: `ApplyOverrides` under a single write lock

`Load` returns `sm.cache` — the shared pointer — and releases the lock before returning (`settings.go:386-414`). A handler doing `Load` → `Apply` → `Save` holds nothing across its read-modify-write, unlike all 22 existing mutations. That loses data.

**Files:**
- Modify: `internal/services/retirement/settings.go`
- Modify: `internal/services/retirement/settings_test.go`

**Interfaces:**
- Consumes: `overrides.Overrides`, `overrides.Apply` (Task 1); `saveInternalAndBump` (Task 3)
- Produces: `func (sm *SettingsManager) ApplyOverrides(o overrides.Overrides) (*models.WhatIfSettings, int, error)` — returns the saved settings, the revision this write produced, and an error. Callers use the returned revision; they must not call `Revision()` afterwards, which under concurrency can report another writer's number.

- [ ] **Step 1: Write the failing tests**

```go
func TestApplyOverrides_PersistsAndReturnsItsOwnRevision(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)
	if _, err := sm.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := 4321.0
	saved, rev, err := sm.ApplyOverrides(overrides.Overrides{MonthlyLivingExpenses: &want})
	if err != nil {
		t.Fatalf("ApplyOverrides: %v", err)
	}
	if saved.MonthlyLivingExpenses != want {
		t.Fatalf("returned settings has %v, want %v", saved.MonthlyLivingExpenses, want)
	}
	if rev == 0 {
		t.Fatal("ApplyOverrides returned revision 0")
	}

	sm.InvalidateCache()
	reloaded, err := sm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.MonthlyLivingExpenses != want {
		t.Fatalf("value did not persist: got %v, want %v", reloaded.MonthlyLivingExpenses, want)
	}
}

// The regression this whole design exists to prevent.
func TestApplyOverrides_DoesNotLoseAConcurrentUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	sm := NewSettingsManager(tmpDir, store)
	if _, err := sm.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			amount := float64(10000 + i)
			if _, _, err := sm.ApplyOverrides(overrides.Overrides{RothConversionAmount: &amount}); err != nil {
				t.Errorf("ApplyOverrides: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if _, err := sm.UpdateSettings(map[string]interface{}{
				"monthly_living_expenses": float64(3000 + i),
			}); err != nil {
				t.Errorf("UpdateSettings: %v", err)
				return
			}
		}
	}()
	wg.Wait()

	sm.InvalidateCache()
	final, err := sm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	// Both writers touched disjoint fields. If the apply path did a
	// read-modify-write outside the lock, one writer's field reverts to its
	// zero/default value.
	if final.MonthlyLivingExpenses < 3000 {
		t.Fatalf("UpdateSettings' field was lost: %v", final.MonthlyLivingExpenses)
	}
	if final.RothConversion == nil || final.RothConversion.AnnualAmount < 10000 {
		t.Fatalf("ApplyOverrides' field was lost: %+v", final.RothConversion)
	}
}
```

Add `"budget2/internal/services/retirement/overrides"` to the test imports.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/services/retirement/ -run TestApplyOverrides -race -v`
Expected: FAIL — `sm.ApplyOverrides` undefined.

- [ ] **Step 3: Implement**

Add to `settings.go`, next to `UpdateSettings`:

```go
// ApplyOverrides applies a sparse override set to the active scenario and saves
// it, returning the saved settings and the revision this write produced.
//
// The whole body runs under one write lock. A caller doing Load → Apply → Save
// would not: Load returns the shared cache pointer and releases the lock, so a
// concurrent UpdateSettings between the load and the save is silently reverted.
// Every other mutation on this type loads, modifies, and saves inside one lock;
// this is no exception.
//
// The returned revision is this write's own. Callers must not read Revision()
// afterwards — under concurrency that can be a different writer's number.
func (sm *SettingsManager) ApplyOverrides(o overrides.Overrides) (*models.WhatIfSettings, int, error) {
	if err := o.ValidateWritable(); err != nil {
		return nil, 0, err
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	current, err := sm.loadInternal()
	if err != nil {
		return nil, 0, err
	}
	updated, err := overrides.Apply(current, o)
	if err != nil {
		return nil, 0, err
	}
	if err := sm.saveInternalAndBump(updated); err != nil {
		return nil, 0, err
	}
	sm.cache = updated
	return updated, sm.revision, nil
}
```

Add the `overrides` import.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/services/retirement/ -run TestApplyOverrides -race -v`
Expected: PASS, no race reports.

**If the race detector flags `handleWhatIfRothConversion` or `handleWhatIfSocialSecurity`** mutating the shared `sm.cache` pointer, that is a pre-existing bug this feature amplifies (a 2s-per-tab poll turns rare concurrent reads into continuous ones). Fixing it is in scope: make those handlers deep-copy before mutating, or route them through a locked manager method. Do it in this task and note it in the commit.

- [ ] **Step 5: Commit**

```bash
git add internal/services/retirement/settings.go internal/services/retirement/settings_test.go
git commit -m "feat(settings): ApplyOverrides under a single write lock

Load returns the shared cache pointer and drops the lock, so a handler doing
Load/Apply/Save has no read-modify-write protection. Concretely: apply loads,
the user drags a slider and UpdateSettings commits, apply saves its stale copy
and the slider's change is gone -- then the poll faithfully renders the
reverted number two seconds later.

Returns its own revision rather than leaving callers to read Revision(), which
under concurrency can be another writer's number."
```

---

### Task 5: `GET /whatif/state`

The MCP must be able to prove it is talking to the right server about the right plan before it writes. `GET /api/health` returns only `{"status":"ok"}` and cannot distinguish budget2 from anything else on port 8080.

**Files:**
- Create: `internal/handlers/whatif/handlers_live.go`
- Create: `internal/handlers/whatif/handlers_live_test.go`
- Modify: `internal/handlers/whatif/handlers.go:746` (route registration block)

**Interfaces:**
- Consumes: `retirementMgr.Revision()` (Task 3), `retirementMgr.ActiveFilename()`
- Produces: `GET /whatif/state` → `{"app":"budget2","settings_dir":"<abs>","active":"<file>","revision":<int>}`

- [ ] **Step 1: Write the failing test**

Create `internal/handlers/whatif/handlers_live_test.go`:

```go
package whatif

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHandleWhatIfState_ReportsIdentityAndState(t *testing.T) {
	_, settingsDir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfState(w, httptest.NewRequest("GET", "/whatif/state", nil))

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var got struct {
		App         string `json:"app"`
		SettingsDir string `json:"settings_dir"`
		Active      string `json:"active"`
		Revision    int    `json:"revision"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.App != "budget2" {
		t.Errorf("app = %q, want budget2", got.App)
	}
	if !filepath.IsAbs(got.SettingsDir) {
		t.Errorf("settings_dir %q is not absolute", got.SettingsDir)
	}
	wantAbs, _ := filepath.Abs(settingsDir)
	if got.SettingsDir != wantAbs {
		t.Errorf("settings_dir = %q, want %q", got.SettingsDir, wantAbs)
	}
	if got.Active == "" {
		t.Error("active is empty")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/handlers/whatif/ -run TestHandleWhatIfState -v`
Expected: FAIL — `handleWhatIfState` undefined.

- [ ] **Step 3: Implement**

Create `internal/handlers/whatif/handlers_live.go`:

```go
package whatif

import (
	"encoding/json"
	"net/http"
	"path/filepath"
)

// appIdentity is the literal reported by GET /whatif/state. A client compares
// it before writing: /api/health returns only {"status":"ok"} and cannot
// distinguish this server from anything else listening on the same port.
const appIdentity = "budget2"

type stateResponse struct {
	App         string `json:"app"`
	SettingsDir string `json:"settings_dir"`
	Active      string `json:"active"`
	Revision    int    `json:"revision"`
}

// handleWhatIfState reports which plan this server is serving and how many
// times it has changed. The settings directory is absolute so a client can
// compare it against its own resolved path without guessing about relative
// bases -- a mismatch means reads and writes would land on different plans.
func handleWhatIfState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if retirementMgr == nil {
		http.Error(w, "settings manager not initialized", http.StatusInternalServerError)
		return
	}
	dir, err := filepath.Abs(retirementMgr.SettingsDir())
	if err != nil {
		http.Error(w, "resolving settings directory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(stateResponse{
		App:         appIdentity,
		SettingsDir: dir,
		Active:      retirementMgr.ActiveFilename(),
		Revision:    retirementMgr.Revision(),
	})
}
```

`SettingsDir()` does not exist yet. Add it to `internal/services/retirement/settings.go`:

```go
// SettingsDir returns the directory this manager reads scenarios from.
func (sm *SettingsManager) SettingsDir() string {
	return sm.settingsDir
}
```

`settingsDir` is set once in `NewSettingsManager` and never mutated, so no lock is needed.

- [ ] **Step 4: Register the route**

In `internal/handlers/whatif/handlers.go`, in `RegisterRoutes`, next to the other `/whatif/scenarios` routes:

```go
	r.Get("/whatif/state", handleWhatIfState)
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/handlers/whatif/ -run TestHandleWhatIfState -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/handlers/whatif/handlers_live.go internal/handlers/whatif/handlers_live_test.go internal/handlers/whatif/handlers.go internal/services/retirement/settings.go
git commit -m "feat(whatif): GET /whatif/state reports identity and revision

A client must be able to prove it is talking to budget2 about the same plan
it reads from disk before it writes. /api/health returns only {status: ok}."
```

---

### Task 6: `POST /whatif/apply`

**Files:**
- Modify: `internal/handlers/whatif/handlers_live.go`
- Modify: `internal/handlers/whatif/handlers_live_test.go`
- Modify: `internal/handlers/whatif/handlers.go` (route registration)

**Interfaces:**
- Consumes: `retirementMgr.ApplyOverrides` (Task 4), `overrides.Overrides` (Task 1)
- Produces: `POST /whatif/apply`, JSON body of the `Overrides` shape → `{"scenario":"<file>","applied":{…},"revision":<int>}`

- [ ] **Step 1: Write the failing tests**

These are the regression tests that justify the entire design. Add to `handlers_live_test.go`:

```go
func postApply(t *testing.T, body string) *http.Response {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/apply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handleWhatIfApply(w, req)
	return w.Result()
}

func TestHandleWhatIfApply_PersistsAndBumpsRevision(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	before := rm.Revision()
	resp := postApply(t, `{"monthly_living_expenses": 4200}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var got struct {
		Scenario string `json:"scenario"`
		Revision int    `json:"revision"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Revision <= before {
		t.Fatalf("revision did not advance: %d -> %d", before, got.Revision)
	}
	if got.Scenario == "" {
		t.Error("scenario is empty")
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.MonthlyLivingExpenses != 4200 {
		t.Fatalf("value did not persist: %v", settings.MonthlyLivingExpenses)
	}
}

// The trap that ruled out posting /whatif/roth-conversion: that handler reads
// Enabled from a checkbox, so a partial post disables the conversions it was
// meant to size.
func TestHandleWhatIfApply_PositiveRothAmountLeavesConversionsEnabled(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.RothConversion = &models.RothConversionConfig{Enabled: true, AnnualAmount: 25000, StartYear: 1, EndYear: 10}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if resp := postApply(t, `{"roth_conversion_amount": 50000}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	rm.InvalidateCache()
	after, err := rm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.RothConversion == nil || !after.RothConversion.Enabled {
		t.Fatal("a positive amount must leave conversions enabled")
	}
	if after.RothConversion.AnnualAmount != 50000 {
		t.Fatalf("amount = %v, want 50000", after.RothConversion.AnnualAmount)
	}
	if after.RothConversion.StartYear != 1 || after.RothConversion.EndYear != 10 {
		t.Fatalf("the window was clobbered: %+v", after.RothConversion)
	}
}

// The documented-but-surprising semantics: amount 0 DISABLES. Asserting it so
// the behavior is pinned rather than discovered.
func TestHandleWhatIfApply_ZeroRothAmountDisablesConversions(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.RothConversion = &models.RothConversionConfig{Enabled: true, AnnualAmount: 25000}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if resp := postApply(t, `{"roth_conversion_amount": 0}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	rm.InvalidateCache()
	after, err := rm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.RothConversion != nil && after.RothConversion.Enabled {
		t.Fatal("amount 0 must disable conversions -- see overrides.go Apply")
	}
}

// The trap that ruled out posting /whatif/social-security: that handler nils
// the entire config when FRABenefit <= 0, which a partial post always triggers.
func TestHandleWhatIfApply_SpouseClaimAgeLeavesSocialSecurityIntact(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 3000, FRA: 67, ClaimAge: 67, SpouseFRABenefit: 1500, SpouseFRA: 67, SpouseClaimAge: 67,
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if resp := postApply(t, `{"spouse_claim_age": 65}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	rm.InvalidateCache()
	after, err := rm.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.SocialSecurity == nil {
		t.Fatal("the Social Security config was deleted by a partial write")
	}
	if after.SocialSecurity.SpouseClaimAge != 65 {
		t.Fatalf("spouse claim age = %d, want 65", after.SocialSecurity.SpouseClaimAge)
	}
	if after.SocialSecurity.ClaimAge != 67 {
		t.Fatalf("the primary claim age was reset to %d", after.SocialSecurity.ClaimAge)
	}
	if after.SocialSecurity.FRABenefit != 3000 {
		t.Fatalf("FRABenefit was reset to %v", after.SocialSecurity.FRABenefit)
	}
}

func TestHandleWhatIfApply_RejectsUnwritableAndInvalid(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	for _, tc := range []struct{ name, body, wantIn string }{
		{"healthcare_inflation", `{"healthcare_inflation": 6}`, "healthcare_inflation"},
		{"absurd return", `{"investment_return": 900}`, "investment_return"},
		{"claim age", `{"social_security_claim_age": 40}`, "social_security_claim_age"},
		{"roth window only", `{"roth_conversion_start_year": 2}`, "roth_conversion_amount"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := postApply(t, tc.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), tc.wantIn) {
				t.Fatalf("error %q does not name %q", body, tc.wantIn)
			}
		})
	}
}

func TestHandleWhatIfApply_RejectsMalformedJSON(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	if resp := postApply(t, `{"monthly_living_expenses":`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
```

Add imports: `io`, `strings`, `budget2/internal/models`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/handlers/whatif/ -run TestHandleWhatIfApply -v`
Expected: FAIL — `handleWhatIfApply` undefined.

- [ ] **Step 3: Implement**

Add to `handlers_live.go`:

```go
type applyResponse struct {
	Scenario string              `json:"scenario"`
	Applied  overrides.Overrides `json:"applied"`
	Revision int                 `json:"revision"`
}

// handleWhatIfApply writes a sparse override set to the active scenario.
//
// This exists instead of the MCP posting the existing forms because
// parseFormFloat returns (0, nil) for an absent key, so inside the
// non-spec-driven handlers "field absent" and "field is zero" are
// indistinguishable. A partial post to /whatif/roth-conversion disables
// conversions; one to /whatif/social-security deletes the config outright.
func handleWhatIfApply(w http.ResponseWriter, r *http.Request) {
	if retirementMgr == nil {
		http.Error(w, "settings manager not initialized", http.StatusInternalServerError)
		return
	}

	var o overrides.Overrides
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		http.Error(w, "invalid overrides JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	_, revision, err := retirementMgr.ApplyOverrides(o)
	if err != nil {
		// Validation errors name their field; surface them verbatim. Save-time
		// failures (ValidatePersons, validateChainInternal) land here too, and
		// fall through to statusForMutationError's 400/404/409/500 mapping.
		status := statusForMutationError(err)
		var ve *overrides.ValidationError
		if errors.As(err, &ve) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(applyResponse{
		Scenario: retirementMgr.ActiveFilename(),
		Applied:  o,
		Revision: revision,
	})
}
```

Add imports: `errors`, and `budget2/internal/services/retirement/overrides`.

`statusForMutationError` (`handlers.go:127`) maps chain/scenario errors to 400/404/409 and everything else to 500. `overrides.ValidationError` (Task 2) is what distinguishes a caller-fixable bad value from a server failure — without the `errors.As` check above, a rejected claim age would surface as a 500.

- [ ] **Step 4: Register the route**

```go
	r.Post("/whatif/apply", handleWhatIfApply)
```

- [ ] **Step 5: Run to verify they pass**

Run: `go test ./internal/handlers/whatif/ -run TestHandleWhatIfApply -v`
Expected: PASS, all six.

Then: `go test ./... -race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/handlers/whatif/ internal/services/retirement/overrides/
git commit -m "feat(whatif): POST /whatif/apply, a typed sparse write endpoint

Reuses Apply server-side so preview (run_scenario) and commit (apply_changes)
share one code path. The regression tests pin the two behaviors that made the
existing form routes unusable: a positive Roth amount must not disable
conversions, and a lone spouse_claim_age must not delete the Social Security
config."
```

---

### Task 7: Split the OOB blocks out of the results partial

`whatif-results` (`web/templates/pages/whatif.html:140-303`) carries OOB swaps that replace the entire left column: the portfolio-settings card, rate assumptions, spending phases, social security, income/expense lists, and four quick-adjust panels.

That is right for a user-initiated mutation and wrong for a background poll. The feature's premise is that a human and the MCP touch the plan at once, so the colliding case is the normal case: you are half-way through typing `4200` when an `apply_changes` lands, and two seconds later the poll replaces the card under your cursor.

**Files:**
- Modify: `web/templates/pages/whatif.html:140-303`
- Modify: `internal/handlers/whatif/handlers.go:318-326` (`renderWhatIfResults`)
- Modify: `internal/handlers/whatif/handlers_live_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - template `whatif-results` — results content only, no OOB blocks
  - template `whatif-results-with-oob` — invokes `whatif-results` then the OOB blocks
  - `renderWhatIfResultsOnly(w, settings, analysis)` — renders `whatif-results`

- [ ] **Step 1: Restructure the template**

In `web/templates/pages/whatif.html`, rename the existing `{{define "whatif-results"}}` (line 140) to `{{define "whatif-results-with-oob"}}`, and make its first line invoke a new results-only template. The new structure:

```gotemplate
{{define "whatif-results"}}
<div id="whatif-completeness-wrapper">
    {{template "whatif-completeness" .}}
</div>
{{end}}

{{define "whatif-results-with-oob"}}
{{template "whatif-results" .}}
{{/* OOB updates for HTMX - these update the left column when results change.
     Deliberately NOT part of "whatif-results": the background poll renders
     that one, and must never rewrite a control the user is currently holding. */}}
<template>
    ... everything from the old line 145 through the end, verbatim ...
</template>
{{end}}
```

Move the entire OOB body — both `<template>` blocks and everything between them, old lines 144-302 — into `whatif-results-with-oob`, unchanged.

Verify the call site at line 126 (`{{template "whatif-results" .}}` inside `<div id="whatif-results">`) still refers to the **results-only** template. On first page load the left column is rendered directly by the page, so it needs no OOB swap.

- [ ] **Step 2: Point the mutating handlers at the OOB variant**

In `internal/handlers/whatif/handlers.go`, change `renderWhatIfResults` to render the OOB variant, and add a results-only sibling:

```go
// renderWhatIfResults renders the results partial plus the out-of-band swaps
// that resync the left column. Used by every user-initiated mutation.
func renderWhatIfResults(w http.ResponseWriter, settings *models.WhatIfSettings, analysis *models.WhatIfAnalysis) {
	renderResultsTemplate(w, "whatif-results-with-oob", settings, analysis)
}

// renderWhatIfResultsOnly renders the results column alone, with no OOB swaps.
// The background poll uses this: it must not rewrite a left-column control the
// user may be typing into or dragging.
func renderWhatIfResultsOnly(w http.ResponseWriter, settings *models.WhatIfSettings, analysis *models.WhatIfAnalysis) {
	renderResultsTemplate(w, "whatif-results", settings, analysis)
}

func renderResultsTemplate(w http.ResponseWriter, name string, settings *models.WhatIfSettings, analysis *models.WhatIfAnalysis) {
	partialData := buildResultsPartialData(settings, analysis, completeness.Check(settings))
	if renderer != nil {
		_ = renderer.RenderPartial(w, name, partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}
```

- [ ] **Step 3: Write the test**

```go
func TestRenderWhatIfResultsOnly_EmitsNoOOBSwaps(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	analysis, err := runAnalysisWithCache(context.Background(), settings)
	if err != nil {
		t.Fatalf("analysis: %v", err)
	}

	w := httptest.NewRecorder()
	renderWhatIfResultsOnly(w, settings, analysis)
	body := w.Body.String()

	if strings.Contains(body, "hx-swap-oob") {
		t.Error("the poll's partial must not contain OOB swaps -- it would rewrite left-column controls the user is holding")
	}
	if !strings.Contains(body, "whatif-completeness-wrapper") {
		t.Error("expected the results content to be present")
	}

	w2 := httptest.NewRecorder()
	renderWhatIfResults(w2, settings, analysis)
	if !strings.Contains(w2.Body.String(), "hx-swap-oob") {
		t.Error("user-initiated mutations must still resync the left column via OOB")
	}
}
```

Add `"context"` to imports.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/handlers/whatif/ -v`
Expected: PASS, including the existing handler tests — they use `renderWhatIfResults`, whose output is unchanged.

- [ ] **Step 5: Commit**

```bash
git add web/templates/pages/whatif.html internal/handlers/whatif/
git commit -m "refactor(whatif): separate the OOB left-column swaps from the results partial

The background poll cannot emit them. Its whole purpose is to land while the
user is doing something else, and the OOB blocks replace the portfolio card,
the rate card, the phases card, and both source lists -- destroying a
half-typed value, the focus, and any in-progress slider drag."
```

---

### Task 8: `GET /whatif/poll`

**Files:**
- Modify: `internal/handlers/whatif/handlers_live.go`
- Modify: `internal/handlers/whatif/handlers_live_test.go`
- Modify: `internal/handlers/whatif/handlers.go` (route)
- Modify: `cmd/server/main.go:112` (logger exclusion)

**Interfaces:**
- Consumes: `retirementMgr.Revision()` (Task 3), `renderWhatIfResultsOnly` (Task 7)
- Produces: `GET /whatif/poll?since=N` → `204` when unchanged; `200` + results-only partial + `HX-Trigger: {"whatif:revision":<n>}` when changed

- [ ] **Step 1: Write the failing tests**

```go
func getPoll(t *testing.T, query string) *http.Response {
	t.Helper()
	w := httptest.NewRecorder()
	handleWhatIfPoll(w, httptest.NewRequest("GET", "/whatif/poll"+query, nil))
	return w.Result()
}

func TestHandleWhatIfPoll_204WhenUnchanged(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	current := rm.Revision()
	resp := getPoll(t, fmt.Sprintf("?since=%d", current))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Fatalf("204 must have an empty body, got %d bytes", len(body))
	}
}

func TestHandleWhatIfPoll_200AndTriggerWhenStale(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	stale := rm.Revision()
	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	resp := getPoll(t, fmt.Sprintf("?since=%d", stale))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	trigger := resp.Header.Get("HX-Trigger")
	if trigger == "" {
		t.Fatal("missing HX-Trigger header -- the client cannot advance its baseline without it")
	}
	var parsed map[string]int
	if err := json.Unmarshal([]byte(trigger), &parsed); err != nil {
		t.Fatalf("HX-Trigger %q is not JSON: %v", trigger, err)
	}
	if parsed["whatif:revision"] != rm.Revision() {
		t.Fatalf("HX-Trigger revision = %d, want %d", parsed["whatif:revision"], rm.Revision())
	}

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "hx-swap-oob") {
		t.Error("the poll response must not contain OOB swaps")
	}
}

func TestHandleWhatIfPoll_MissingOrMalformedSinceRendersFully(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	for _, q := range []string{"", "?since=", "?since=banana", "?since=-3"} {
		t.Run("since="+q, func(t *testing.T) {
			// A bad parameter must show fresh figures, never suppress them.
			if resp := getPoll(t, q); resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
		})
	}
}

func TestHandleWhatIfPoll_RevisionAheadOfCounterStillRenders(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	// A tab held across a server restart has a baseline above the fresh
	// counter. The comparison must be inequality, not `revision > since`.
	resp := getPoll(t, fmt.Sprintf("?since=%d", rm.Revision()+500))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
```

Add `"fmt"` to imports.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/handlers/whatif/ -run TestHandleWhatIfPoll -v`
Expected: FAIL — `handleWhatIfPoll` undefined.

- [ ] **Step 3: Implement**

```go
// handleWhatIfPoll is the page's change detector. It returns 204 when nothing
// has changed since the caller's baseline -- htmx performs no swap on 204, so
// the common case costs one integer comparison and runs no analysis, which is
// what makes a 2s poll acceptable.
//
// The comparison is inequality, never `revision > since`: the counter is
// in-memory, so a tab held across a server restart has a baseline above the
// fresh counter and must still re-render.
func handleWhatIfPoll(w http.ResponseWriter, r *http.Request) {
	if retirementMgr == nil {
		http.Error(w, "settings manager not initialized", http.StatusInternalServerError)
		return
	}

	// An absent or malformed `since` parses to 0, producing a full render.
	// That is the safe direction: a bad parameter shows fresh figures rather
	// than silently suppressing them.
	since, _ := strconv.Atoi(r.URL.Query().Get("since"))

	current := retirementMgr.Revision()
	if since == current {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	analysis, err := runAnalysisWithCache(r.Context(), settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// The client advances its baseline from this header. It cannot come from
	// the body: the response swaps into #whatif-results and never touches the
	// polling element's own attributes.
	trigger, err := json.Marshal(map[string]int{"whatif:revision": current})
	if err == nil {
		w.Header().Set("HX-Trigger", string(trigger))
	}
	renderWhatIfResultsOnly(w, settings, analysis)
}
```

Add `"strconv"` to imports.

- [ ] **Step 4: Register the route and exclude it from the logger**

Route, in `RegisterRoutes`:

```go
	r.Get("/whatif/poll", handleWhatIfPoll)
```

In `cmd/server/main.go`, replace the bare `r.Use(middleware.Logger)` (line 112) with a wrapper that skips the poll. At 2s per tab it would emit roughly 1800 log lines per hour per open tab:

```go
	// middleware.Logger, except for the what-if poll: at one request per tab
	// every 2s it would bury every other line in the log.
	r.Use(func(next http.Handler) http.Handler {
		logged := middleware.Logger(next)
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/whatif/poll" {
				next.ServeHTTP(w, req)
				return
			}
			logged.ServeHTTP(w, req)
		})
	})
```

- [ ] **Step 5: Run to verify they pass**

Run: `go test ./internal/handlers/whatif/ ./cmd/server/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/handlers/whatif/ cmd/server/main.go
git commit -m "feat(whatif): GET /whatif/poll, 204 when nothing changed

htmx performs no swap on 204, so the common case is one integer comparison
and no analysis. The new revision travels in an HX-Trigger header because the
response swaps into #whatif-results and cannot update the polling element's
own attributes.

Excluded from middleware.Logger: 1800 lines/hour/tab otherwise."
```

---

### Task 9: The polling sentinel and its guards

`hx-swap="outerHTML"` on `#whatif-results` cannot be used. That container is the wrapper `<div>` at `whatif.html:125`, and the partial renders its *contents*. An `outerHTML` swap would replace the container with a response that does not contain it: after one 200 poll the container is gone, polling stops, and all ~40 `hx-target="#whatif-results"` sites start raising `htmx:targetError`. It would also break chart reload, because htmx sets `detail.target` to the request's original target — for `outerHTML`, the removed node — and `charts.js:536` passes exactly that as the scope it queries.

**Files:**
- Modify: `web/templates/pages/whatif.html` (near line 125)
- Create: `web/static/js/whatif-poll.js`
- Modify: `web/templates/pages/whatif.html` (script include)

**Interfaces:**
- Consumes: `GET /whatif/poll` (Task 8)
- Produces: `window.__whatifRevision` — the client's baseline, read by the sentinel's `hx-vals`

- [ ] **Step 1: Add the sentinel element**

In `web/templates/pages/whatif.html`, immediately **after** the `<div id="whatif-results">…</div>` block (which ends around line 127), add:

```html
    {{/* Change detector. A sibling, never itself swapped, so its 2s timer is
         never disturbed and detail.target stays #whatif-results for the
         afterSettle listeners in charts.js, base.html, and portfolio-settings. */}}
    <div id="whatif-poll"
         hx-get="/whatif/poll"
         hx-vals='js:{since: window.__whatifRevision || 0}'
         hx-trigger="every 2s"
         hx-target="#whatif-results"
         hx-swap="innerHTML"
         hx-sync="#whatif-results:drop"></div>
```

`hx-sync="#whatif-results:drop"` makes htmx drop a poll while a user-initiated request to the same target is in flight. A dropped poll costs nothing — the next one two seconds later still sees the stale baseline.

- [ ] **Step 2: Write the baseline listener and focus guard**

Create `web/static/js/whatif-poll.js`:

```javascript
// Baseline for GET /whatif/poll. The server answers 204 when this matches its
// revision, which is the whole reason a 2s poll is affordable.
window.__whatifRevision = window.__whatifRevision || 0;

// Every response that changes the plan carries the new revision in an
// HX-Trigger header, which htmx raises as this event. It cannot ride in the
// body: responses swap into #whatif-results and never touch the polling
// element's attributes.
document.body.addEventListener('whatif:revision', function (evt) {
    var next = evt.detail;
    if (next && typeof next.value !== 'undefined') {
        next = next.value;
    }
    next = parseInt(next, 10);
    if (!isNaN(next)) {
        window.__whatifRevision = next;
    }
});

// Do not swap the results column out from under someone who is typing in it or
// dragging a control. The premise of this feature is that a human and the MCP
// touch the plan at the same time, so this is the normal case, not the edge.
document.body.addEventListener('htmx:confirm', function (evt) {
    if (!evt.detail || !evt.detail.elt || evt.detail.elt.id !== 'whatif-poll') {
        return;
    }
    var active = document.activeElement;
    if (!active || active === document.body) {
        return;
    }
    var interactive = active.matches('input, select, textarea, [contenteditable="true"]');
    if (interactive) {
        // Skip this tick; the next one is 2s away and the baseline is unchanged.
        evt.preventDefault();
    }
});
```

- [ ] **Step 3: Include the script**

In `web/templates/pages/whatif.html`, alongside the other page script includes (near line 132):

```html
<script src="/static/js/whatif-poll.js"></script>
```

- [ ] **Step 4: Emit `HX-Trigger` from the mutating handlers too**

Without this, every slider drag is followed within 2s by a redundant full re-render: the mutating handlers' responses swap `innerHTML` and never update the client's baseline.

In `internal/handlers/whatif/handlers.go`, in `renderRecalc` (`:138-146`), set the header before rendering:

```go
func renderRecalc(w http.ResponseWriter, r *http.Request, settings *models.WhatIfSettings) {
	analysis, err := runAnalysisWithCache(r.Context(), settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if retirementMgr != nil {
		if trigger, err := json.Marshal(map[string]int{"whatif:revision": retirementMgr.Revision()}); err == nil {
			w.Header().Set("HX-Trigger", string(trigger))
		}
	}
	renderWhatIfResults(w, settings, analysis)
}
```

- [ ] **Step 5: Add a handler test for the header**

```go
func TestRenderRecalc_CarriesRevisionHeader(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{"monthly_living_expenses": {"5100"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	trigger := resp.Header.Get("HX-Trigger")
	if trigger == "" {
		t.Fatal("a user-initiated mutation must advance the client baseline too, or the poll re-renders redundantly 2s later")
	}
	var parsed map[string]int
	if err := json.Unmarshal([]byte(trigger), &parsed); err != nil {
		t.Fatalf("HX-Trigger %q is not JSON: %v", trigger, err)
	}
	if parsed["whatif:revision"] != rm.Revision() {
		t.Fatalf("header revision = %d, want %d", parsed["whatif:revision"], rm.Revision())
	}
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/handlers/whatif/ -v`
Expected: PASS

Browser behavior is verified in Task 15, not here — it cannot be tested from Go.

- [ ] **Step 7: Commit**

```bash
git add web/templates/pages/whatif.html web/static/js/whatif-poll.js internal/handlers/whatif/
git commit -m "feat(whatif): sentinel-driven poll with baseline and focus guards

Not hx-swap=outerHTML on #whatif-results: that container is the wrapper and
the partial renders its contents, so one poll would delete the container,
stop polling, break ~40 hx-target sites, and break chart reload (afterSettle
reports the removed node as detail.target).

Mutating handlers emit the same HX-Trigger so a slider drag advances the
baseline and is not followed by a redundant re-render 2s later."
```

---

### Task 10: Honor `BUDGET_DATA_DIR` in the MCP binary

`cmd/whatif-mcp` reads only its `-data` flag and otherwise hardcodes `./data/settings`, while `cmd/server` resolves through `config.Load()`. With a custom data dir plus a stale `./data/settings`, the MCP answers about the wrong plan. This is `docs/whatif-mcp-followups-2026-08-09.md` §3.

**Files:**
- Modify: `cmd/whatif-mcp/main.go`
- Modify: `cmd/whatif-mcp/main_test.go`

**Interfaces:**
- Produces: `resolveDataDir(flagValue string, env func(string) string) string` — flag wins, then `BUDGET_DATA_DIR/settings`, then `./data/settings`

- [ ] **Step 1: Write the failing tests**

```go
func TestResolveDataDir_Precedence(t *testing.T) {
	noEnv := func(string) string { return "" }
	withEnv := func(v string) func(string) string {
		return func(k string) string {
			if k == "BUDGET_DATA_DIR" {
				return v
			}
			return ""
		}
	}

	if got := resolveDataDir("/explicit/flag", withEnv("/from/env")); got != "/explicit/flag" {
		t.Errorf("flag must win: got %q", got)
	}
	if got, want := resolveDataDir("", withEnv("/from/env")), filepath.Join("/from/env", "settings"); got != want {
		t.Errorf("env: got %q, want %q", got, want)
	}
	if got, want := resolveDataDir("", noEnv), filepath.Join("data", "settings"); got != want {
		t.Errorf("default: got %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/whatif-mcp/ -run TestResolveDataDir -v`
Expected: FAIL — signature mismatch (current `resolveDataDir` takes one argument).

- [ ] **Step 3: Implement**

```go
// resolveDataDir returns the settings directory, resolving it the way
// cmd/server does: an explicit flag wins, then BUDGET_DATA_DIR (to which
// config.Load appends "settings"), then ./data/settings.
//
// Taking env as a parameter keeps this testable without mutating process
// state. Honoring BUDGET_DATA_DIR closes followups §3: with a custom data dir
// and a stale ./data/settings, this server would answer about the wrong plan.
func resolveDataDir(flagValue string, env func(string) string) string {
	if flagValue != "" {
		return flagValue
	}
	if dataDir := env("BUDGET_DATA_DIR"); dataDir != "" {
		return filepath.Join(dataDir, "settings")
	}
	return filepath.Join("data", "settings")
}
```

Update the call in `main`: `settingsDir := resolveDataDir(*dir, os.Getenv)`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./cmd/whatif-mcp/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/whatif-mcp/
git commit -m "fix(whatif-mcp): honor BUDGET_DATA_DIR (followups §3)

cmd/server resolves through config.Load; this binary hardcoded
./data/settings. With a custom data dir and a stale ./data/settings it would
answer about a different plan than the web UI, with no symptom."
```

---

### Task 11: The live HTTP client

**Files:**
- Create: `internal/services/whatifmcp/live.go`
- Create: `internal/services/whatifmcp/live_test.go`

**Interfaces:**
- Consumes: `GET /whatif/state` (Task 5), `POST /whatif/apply` (Task 6)
- Produces:
  - `type State struct { App, SettingsDir, Active string; Revision int }`
  - `func NewClient(baseURL, settingsDir string) *Client`
  - `func (c *Client) State(ctx) (State, error)` — errors when app or settings dir mismatch
  - `func (c *Client) Apply(ctx, Overrides) (ApplyResult, error)`
  - `func (c *Client) EnsureServer(ctx) (State, bool, error)` — bool reports whether it spawned
  - `func ResolveBaseURL(env func(string) string) string`
  - `func spawnArgs(settingsDir string) (dataDir string, err error)`

- [ ] **Step 1: Write the failing tests**

```go
package whatifmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func stateServer(t *testing.T, s State) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/whatif/state" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s)
	}))
}

func TestClientState_AdoptsMatchingInstance(t *testing.T) {
	dir := t.TempDir()
	srv := stateServer(t, State{App: "budget2", SettingsDir: dir, Active: "whatif.json", Revision: 7})
	defer srv.Close()

	got, err := NewClient(srv.URL, dir).State(context.Background())
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if got.Revision != 7 || got.Active != "whatif.json" {
		t.Fatalf("unexpected state: %+v", got)
	}
}

func TestClientState_RefusesDifferentPlan(t *testing.T) {
	mine := t.TempDir()
	theirs := t.TempDir()
	srv := stateServer(t, State{App: "budget2", SettingsDir: theirs, Active: "whatif.json"})
	defer srv.Close()

	_, err := NewClient(srv.URL, mine).State(context.Background())
	if err == nil {
		t.Fatal("expected a refusal when the server serves a different settings dir")
	}
	// Both paths must appear, or the user cannot tell which one they meant.
	if !strings.Contains(err.Error(), mine) || !strings.Contains(err.Error(), theirs) {
		t.Fatalf("error must name both paths, got: %v", err)
	}
}

func TestClientState_RefusesForeignApp(t *testing.T) {
	dir := t.TempDir()
	srv := stateServer(t, State{App: "something-else", SettingsDir: dir})
	defer srv.Close()

	if _, err := NewClient(srv.URL, dir).State(context.Background()); err == nil {
		t.Fatal("expected a refusal when the app identity does not match")
	}
}

func TestClientState_RefusesUnparseableBody(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not budget2</html>"))
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL, dir).State(context.Background()); err == nil {
		t.Fatal("expected a refusal for a non-JSON body")
	}
}

func TestClientState_ErrorsCleanlyWhenRefused(t *testing.T) {
	dir := t.TempDir()
	srv := stateServer(t, State{App: "budget2", SettingsDir: dir})
	url := srv.URL
	srv.Close() // nothing is listening now

	_, err := NewClient(url, dir).State(context.Background())
	if err == nil {
		t.Fatal("expected an error when the connection is refused")
	}
}

func TestSpawnArgs_DerivesDataDirFromSettingsDir(t *testing.T) {
	// config.Load appends "settings" to BUDGET_DATA_DIR, so the env var must
	// carry the PARENT. Passing the settings dir itself yields <S>/settings and
	// the identity check refuses a server we just launched.
	got, err := spawnArgs(filepath.Join("/home/u/budget2", "data", "settings"))
	if err != nil {
		t.Fatalf("spawnArgs: %v", err)
	}
	if want := filepath.Join("/home/u/budget2", "data"); got != want {
		t.Fatalf("BUDGET_DATA_DIR = %q, want %q", got, want)
	}
}

func TestSpawnArgs_RefusesUnreachableSettingsDir(t *testing.T) {
	// No BUDGET_DATA_DIR value can produce this settings path, so guessing
	// would silently serve the wrong plan.
	if _, err := spawnArgs("/somewhere/custom-plan-dir"); err == nil {
		t.Fatal("expected a refusal for a settings dir not named \"settings\"")
	}
}

func TestResolveBaseURL(t *testing.T) {
	withEnv := func(k string) string {
		if k == "BUDGET_SERVER_URL" {
			return "http://localhost:9999"
		}
		return ""
	}
	if got := ResolveBaseURL(withEnv); got != "http://localhost:9999" {
		t.Errorf("env override ignored: %q", got)
	}
	if got := ResolveBaseURL(func(string) string { return "" }); got != "http://localhost:8080" {
		t.Errorf("default = %q, want http://localhost:8080", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/services/whatifmcp/ -run 'TestClient|TestSpawnArgs|TestResolveBaseURL' -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement**

Create `internal/services/whatifmcp/live.go`:

```go
package whatifmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"time"
)

// State is what GET /whatif/state reports.
type State struct {
	App         string `json:"app"`
	SettingsDir string `json:"settings_dir"`
	Active      string `json:"active"`
	Revision    int    `json:"revision"`
}

// ApplyResult is what POST /whatif/apply returns.
type ApplyResult struct {
	Scenario string    `json:"scenario"`
	Applied  Overrides `json:"applied"`
	Revision int       `json:"revision"`
}

// Client talks to a running cmd/server. Every call verifies the server is
// budget2 and is serving the same settings directory this process reads, so a
// stray verify instance on another port cannot absorb a write meant for the
// real plan.
type Client struct {
	baseURL     string
	settingsDir string
	http        *http.Client
}

func NewClient(baseURL, settingsDir string) *Client {
	return &Client{
		baseURL:     baseURL,
		settingsDir: settingsDir,
		http:        &http.Client{Timeout: 10 * time.Second},
	}
}

// ResolveBaseURL returns the server URL: BUDGET_SERVER_URL, else the default
// cmd/server listen address (config.DefaultConfig uses :8080).
func ResolveBaseURL(env func(string) string) string {
	if u := env("BUDGET_SERVER_URL"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

// State fetches and verifies the server's identity.
func (c *Client) State(ctx context.Context) (State, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/whatif/state", nil)
	if err != nil {
		return State{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return State{}, fmt.Errorf("no budget2 server reachable at %s: %w", c.baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return State{}, fmt.Errorf("%s/whatif/state returned %s; this may not be a budget2 server", c.baseURL, resp.Status)
	}

	var s State
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return State{}, fmt.Errorf("%s answered but is not a budget2 server (unparseable /whatif/state): %w", c.baseURL, err)
	}
	if s.App != "budget2" {
		return State{}, fmt.Errorf("%s is running %q, not budget2; refusing to write", c.baseURL, s.App)
	}

	mine, err := filepath.Abs(c.settingsDir)
	if err != nil {
		return State{}, err
	}
	theirs, err := filepath.Abs(s.SettingsDir)
	if err != nil {
		return State{}, err
	}
	if mine != theirs {
		return State{}, fmt.Errorf(
			"refusing to write: this server is serving %s but these tools read %s. "+
				"Point BUDGET_SERVER_URL at the right instance, or start the MCP server with -data %s",
			theirs, mine, theirs)
	}
	return s, nil
}

// Apply posts a sparse override set.
func (c *Client) Apply(ctx context.Context, o Overrides) (ApplyResult, error) {
	body, err := json.Marshal(o)
	if err != nil {
		return ApplyResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/whatif/apply", bytes.NewReader(body))
	if err != nil {
		return ApplyResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("apply: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		msg := new(bytes.Buffer)
		_, _ = msg.ReadFrom(resp.Body)
		return ApplyResult{}, fmt.Errorf("apply rejected (%s): %s", resp.Status, bytes.TrimSpace(msg.Bytes()))
	}

	var out ApplyResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ApplyResult{}, fmt.Errorf("apply: decoding response: %w", err)
	}
	return out, nil
}

// spawnArgs derives the BUDGET_DATA_DIR value for a settings directory.
//
// config.Load does SettingsDirectory = filepath.Join(dataDir, "settings"), so
// the env var must carry the PARENT of the settings dir. Passing the settings
// dir itself would produce a server serving <S>/settings, which then fails the
// identity check this same client performs -- a refusal caused entirely by the
// spawn.
func spawnArgs(settingsDir string) (string, error) {
	abs, err := filepath.Abs(settingsDir)
	if err != nil {
		return "", err
	}
	if filepath.Base(abs) != "settings" {
		return "", fmt.Errorf(
			"cannot start a server for %s: BUDGET_DATA_DIR always resolves to <dir>/settings, "+
				"so no value produces this path. Start cmd/server yourself and set BUDGET_SERVER_URL", abs)
	}
	return filepath.Dir(abs), nil
}

// EnsureServer returns the state of a usable server, starting one if nothing is
// listening. The bool reports whether this call started it.
//
// A spawned server is deliberately detached so it outlives this process and the
// user's browser tab keeps working after the session ends. /killme stops it.
func (c *Client) EnsureServer(ctx context.Context) (State, bool, error) {
	if s, err := c.State(ctx); err == nil {
		return s, false, nil
	} else if !isConnectionRefused(err) {
		// A reachable server that failed verification must not be replaced by
		// a second one -- report the mismatch instead.
		return State{}, false, err
	}

	dataDir, err := spawnArgs(c.settingsDir)
	if err != nil {
		return State{}, false, err
	}

	cmd := exec.Command("go", "run", "./cmd/server")
	cmd.Env = append(envWithout("BUDGET_DATA_DIR"), "BUDGET_DATA_DIR="+dataDir)
	if err := cmd.Start(); err != nil {
		return State{}, false, fmt.Errorf("starting cmd/server: %w", err)
	}
	go func() { _ = cmd.Wait() }() // reap; never block the tool call

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := c.State(ctx); err == nil {
			return s, true, nil
		}
		select {
		case <-ctx.Done():
			return State{}, false, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return State{}, false, fmt.Errorf(
		"started cmd/server but it did not become healthy at %s within 15s; "+
			"start it yourself with BUDGET_DATA_DIR=%s and retry", c.baseURL, dataDir)
}
```

Add the two small helpers in the same file: `isConnectionRefused(err error) bool` (use `errors.Is(err, syscall.ECONNREFUSED)`, falling back to a `*net.OpError` check) and `envWithout(key string) []string` (copy `os.Environ()` dropping `key=`).

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/services/whatifmcp/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/services/whatifmcp/live.go internal/services/whatifmcp/live_test.go
git commit -m "feat(whatif-mcp): HTTP client that verifies the instance before writing

The MCP resolves its settings dir from a flag; the server resolves from
BUDGET_DATA_DIR. Nothing makes them agree, and scripts/whatif-verify.sh runs
instances on :8099 against a throwaway /tmp copy. A settings-dir mismatch is a
hard refusal naming both paths.

spawnArgs passes the PARENT of the settings dir, because config.Load appends
'settings' -- passing S would make the server fail our own identity check."
```

---

### Task 12: Snapshot before the first write

**Files:**
- Create: `internal/services/whatifmcp/snapshot.go`
- Create: `internal/services/whatifmcp/snapshot_test.go`

**Interfaces:**
- Produces:
  - `func NewSnapshotter(settingsDir, snapshotDir string) *Snapshotter`
  - `func (s *Snapshotter) Ensure(scenario string, now time.Time) (string, error)` — returns the snapshot path, or the existing one if this process already snapshotted that scenario

- [ ] **Step 1: Write the failing tests**

```go
func TestSnapshotter_CopiesOncePerScenario(t *testing.T) {
	settingsDir := t.TempDir()
	snapDir := t.TempDir()
	content := []byte(`{"monthly_living_expenses":4000}`)
	if err := os.WriteFile(filepath.Join(settingsDir, "whatif.json"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewSnapshotter(settingsDir, snapDir)
	now := time.Date(2026, 8, 9, 14, 22, 3, 0, time.UTC)

	first, err := s.Ensure("whatif.json", now)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	got, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("reading snapshot: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("snapshot is not byte-equal to the source")
	}
	if strings.Contains(filepath.Base(first), ":") {
		t.Errorf("filename %q contains a colon; it breaks extraction on Windows and exFAT", filepath.Base(first))
	}

	second, err := s.Ensure("whatif.json", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if second != first {
		t.Errorf("snapshotted the same scenario twice: %q then %q", first, second)
	}
}

func TestSnapshotter_SnapshotsEachScenarioSeparately(t *testing.T) {
	settingsDir := t.TempDir()
	snapDir := t.TempDir()
	for _, name := range []string{"whatif.json", "whatif-alt.json"} {
		if err := os.WriteFile(filepath.Join(settingsDir, name), []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := NewSnapshotter(settingsDir, snapDir)
	now := time.Now()
	// Switching scenarios mid-conversation must back up the second plan too.
	a, err := s.Ensure("whatif.json", now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Ensure("whatif-alt.json", now)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two scenarios shared one snapshot")
	}
}

func TestSnapshotter_WritesOutsideTheSettingsDir(t *testing.T) {
	settingsDir := t.TempDir()
	snapDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(settingsDir, "whatif.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := NewSnapshotter(settingsDir, snapDir).Ensure("whatif.json", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// backup.SkipPredicate does not exclude .bak, so a snapshot inside the data
	// directory would be swept into every backup zip from then on.
	rel, err := filepath.Rel(settingsDir, path)
	if err == nil && !strings.HasPrefix(rel, "..") {
		t.Fatalf("snapshot %q is inside the settings dir %q", path, settingsDir)
	}
}

func TestSnapshotter_FailsWhenSourceUnreadable(t *testing.T) {
	settingsDir := t.TempDir()
	snapDir := t.TempDir()
	// No scenario file written: Ensure must fail so the caller aborts before
	// the POST rather than writing unbacked.
	if _, err := NewSnapshotter(settingsDir, snapDir).Ensure("whatif.json", time.Now()); err == nil {
		t.Fatal("expected an error for a missing source file")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/services/whatifmcp/ -run TestSnapshotter -v`
Expected: FAIL — `NewSnapshotter` undefined.

- [ ] **Step 3: Implement**

```go
package whatifmcp

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// snapshotTimeLayout deliberately avoids RFC3339: its colons survive Linux but
// break extraction on Windows and exFAT.
const snapshotTimeLayout = "2006-01-02T15-04-05Z"

// Snapshotter copies a scenario before this process first writes to it.
//
// Once per (process, scenario), not once per process: switching scenarios in
// the UI mid-conversation must back up the second plan too.
type Snapshotter struct {
	settingsDir string
	snapshotDir string

	mu   sync.Mutex
	done map[string]string // scenario -> snapshot path
}

func NewSnapshotter(settingsDir, snapshotDir string) *Snapshotter {
	return &Snapshotter{
		settingsDir: settingsDir,
		snapshotDir: snapshotDir,
		done:        make(map[string]string),
	}
}

// Ensure copies scenario to the snapshot directory if this process has not
// already done so, returning the snapshot path.
//
// It READS the source rather than linking it: followups §3 records that this
// server detects encryption in the wrong directory, so a blind copy of
// ciphertext would "succeed" and the caller's abort-before-write guarantee
// would not fire.
func (s *Snapshotter) Ensure(scenario string, now time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if path, ok := s.done[scenario]; ok {
		return path, nil
	}

	src := filepath.Join(s.settingsDir, scenario)
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("cannot snapshot %s before writing: %w", scenario, err)
	}

	if err := os.MkdirAll(s.snapshotDir, 0o755); err != nil {
		return "", fmt.Errorf("creating snapshot directory: %w", err)
	}

	dst := filepath.Join(s.snapshotDir,
		fmt.Sprintf("%s.%s.bak", scenario, now.UTC().Format(snapshotTimeLayout)))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("writing snapshot %s: %w", dst, err)
	}

	s.done[scenario] = dst
	return dst, nil
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/services/whatifmcp/ -run TestSnapshotter -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/services/whatifmcp/snapshot.go internal/services/whatifmcp/snapshot_test.go
git commit -m "feat(whatif-mcp): snapshot a scenario before this process writes it

Written outside the data directory: backup.SkipPredicate excludes cache dirs,
tmp leftovers, and encryption-state files, but not .bak -- snapshots left in
data/settings would be swept into every subsequent backup zip, and each new
one would itself trigger a fresh snapshot.

Reads the source rather than copying blind, so an unreadable (e.g. encrypted)
scenario aborts the write instead of producing a useless backup."
```

---

### Task 13: The `open_page` and `apply_changes` tools

**Files:**
- Modify: `internal/services/whatifmcp/server.go`
- Modify: `internal/services/whatifmcp/scenarios.go` (active-scenario resolution)
- Modify: `internal/services/whatifmcp/server_test.go`
- Modify: `cmd/whatif-mcp/main.go` (wiring)

**Interfaces:**
- Consumes: `Client` (Task 11), `Snapshotter` (Task 12)
- Produces: MCP tools `open_page` and `apply_changes`

- [ ] **Step 1: Extend `NewServer` to accept the live pieces**

Change the signature to `NewServer(src *Source, live *Client, snaps *Snapshotter) *mcp.Server`. Update `cmd/whatif-mcp/main.go`:

```go
	src := whatifmcp.NewSource(settingsDir, store)
	baseURL := whatifmcp.ResolveBaseURL(os.Getenv)
	live := whatifmcp.NewClient(baseURL, settingsDir)
	snaps := whatifmcp.NewSnapshotter(settingsDir, filepath.Join(settingsDir, "..", "..", "budget2-mcp-snapshots"))

	server := whatifmcp.NewServer(src, live, snaps)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("whatif-mcp: %v", err)
	}
```

Resolve the snapshot directory with `filepath.Clean` and prefer `BUDGET2_BACKUP_DIR` when set, matching `config.BackupDir`'s env override:

```go
func resolveSnapshotDir(settingsDir string, env func(string) string) string {
	if d := env("BUDGET2_BACKUP_DIR"); d != "" {
		return filepath.Join(d, "mcp-snapshots")
	}
	return filepath.Clean(filepath.Join(settingsDir, "..", "..", "budget2-mcp-snapshots"))
}
```

- [ ] **Step 2: Add `open_page`**

```go
type openPageInput struct {
	Scenario string `json:"scenario,omitempty" jsonschema:"saved scenario filename to switch to first; omit to use the active one"`
}

type openPageOutput struct {
	URL      string `json:"url"`
	Started  bool   `json:"started"`
	Active   string `json:"active"`
	Revision int    `json:"revision"`
}

mcp.AddTool(s, &mcp.Tool{
	Name: "open_page",
	Description: "Return the URL of the what-if page, starting the budget2 web server first if " +
		"nothing is running. Call this before apply_changes. The page updates itself, so a tab " +
		"opened from this URL will show later changes without being reloaded.",
}, func(ctx context.Context, _ *mcp.CallToolRequest, in openPageInput) (res *mcp.CallToolResult, out openPageOutput, err error) {
	defer recoverToError("open_page", &err)

	state, started, err := live.EnsureServer(ctx)
	if err != nil {
		return nil, openPageOutput{}, err
	}
	if in.Scenario != "" && in.Scenario != state.Active {
		if err := live.SwitchScenario(ctx, in.Scenario); err != nil {
			return nil, openPageOutput{}, err
		}
		if state, err = live.State(ctx); err != nil {
			return nil, openPageOutput{}, err
		}
	}
	return nil, openPageOutput{
		URL:      live.BaseURL() + "/whatif",
		Started:  started,
		Active:   state.Active,
		Revision: state.Revision,
	}, nil
})
```

Add `func (c *Client) BaseURL() string { return c.baseURL }` and `SwitchScenario(ctx, name)` (form POST to `/whatif/scenarios/switch` with `filename=<name>`; check the handler's expected field name in `handlers_scenarios.go` and match it exactly) to `live.go`.

- [ ] **Step 3: Add `apply_changes`**

```go
type applyChangesInput struct {
	Scenario  string    `json:"scenario,omitempty" jsonschema:"saved scenario filename; omit for the active one"`
	Overrides Overrides `json:"overrides" jsonschema:"settings to change and save; omitted fields keep their current value"`
}

type applyChangesOutput struct {
	Scenario       string       `json:"scenario"`
	Applied        Overrides    `json:"applied"`
	RevisionBefore int          `json:"revision_before"`
	RevisionAfter  int          `json:"revision_after"`
	SnapshotPath   string       `json:"snapshot_path"`
	Analysis       AnalysisView `json:"analysis"`
}

mcp.AddTool(s, &mcp.Tool{
	Name: "apply_changes",
	Description: "Save changed assumptions to the retirement plan and return the resulting analysis. " +
		"THIS MODIFIES THE SAVED PLAN — use run_scenario to check a claim without writing. " +
		"An open what-if page picks the change up within about two seconds. A copy of the scenario " +
		"is saved before the first write of the session; recovering from an unwanted change means " +
		"restoring that .bak file by hand. Note two behaviors: roth_conversion_amount of 0 DISABLES " +
		"conversions, and healthcare_inflation cannot be saved (preview it with run_scenario). " +
		"Read the whatif://assumptions resource before drawing conclusions.",
}, func(ctx context.Context, _ *mcp.CallToolRequest, in applyChangesInput) (res *mcp.CallToolResult, out applyChangesOutput, err error) {
	defer recoverToError("apply_changes", &err)

	state, _, err := live.EnsureServer(ctx)
	if err != nil {
		return nil, applyChangesOutput{}, err
	}
	if in.Scenario != "" && in.Scenario != state.Active {
		if err := live.SwitchScenario(ctx, in.Scenario); err != nil {
			return nil, applyChangesOutput{}, err
		}
		if state, err = live.State(ctx); err != nil {
			return nil, applyChangesOutput{}, err
		}
	}

	// Before the POST, never after: a failed snapshot must abort the write.
	snapPath, err := snaps.Ensure(state.Active, time.Now())
	if err != nil {
		return nil, applyChangesOutput{}, err
	}

	result, err := live.Apply(ctx, in.Overrides)
	if err != nil {
		return nil, applyChangesOutput{}, err
	}

	settings, name, err := src.Load(result.Scenario)
	if err != nil {
		return nil, applyChangesOutput{}, err
	}
	prepared, err := prepare.From(settings)
	if err != nil {
		return nil, applyChangesOutput{}, fmt.Errorf("prepare %s: %w", name, err)
	}
	a := retirement.RunFull(engine.New(), engine.Input{Prepared: prepared})

	return nil, applyChangesOutput{
		Scenario:       result.Scenario,
		Applied:        result.Applied,
		RevisionBefore: state.Revision,
		RevisionAfter:  result.Revision,
		SnapshotPath:   snapPath,
		Analysis:       ShapeAnalysis(a, true),
	}, nil
})
```

- [ ] **Step 4: Resolve the active scenario over HTTP**

In `scenarios.go`, `Source.Load("")` currently falls back to the hardcoded `whatif.json`. The active filename is in-process state in the web server, so a separate process always guessed wrong — followups §4.

Give `Source` an optional `live *Client` and, when `Load` is called with an empty name, ask `live.State(ctx)` first, falling back to `whatif.json` when no verified server is reachable. Keep the fallback: reads must still work with the server down.

- [ ] **Step 5: Update the server instructions constant**

`serverInstructions` (`server.go:71-76`) describes read-only tools. Append:

```
apply_changes writes to the saved plan; run_scenario does not. Prefer run_scenario
while exploring, and apply_changes only when the user has settled on a change.
```

- [ ] **Step 6: Add tool tests**

Extend `server_test.go` with a table-driven check that all six tools are registered with non-empty descriptions, and that `apply_changes`'s description contains `"MODIFIES THE SAVED PLAN"`. Drive `apply_changes` against an `httptest` server that fakes `/whatif/state` and `/whatif/apply`, asserting that a snapshot file exists on disk afterwards and that a snapshot failure prevents the POST (point the snapshotter at a settings dir with no scenario file and assert the fake server received no request).

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/services/whatifmcp/ ./cmd/whatif-mcp/ -race -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/services/whatifmcp/ cmd/whatif-mcp/
git commit -m "feat(whatif-mcp): open_page and apply_changes

apply_changes snapshots before the POST, never after, so a failed snapshot
aborts the write rather than leaving it unbacked. Reports both revisions so a
caller can say plainly whether the page will update instead of inferring it
from a 200.

Source.Load(\"\") now resolves the active scenario over HTTP when a verified
server is reachable, closing followups §4 -- the active filename is in-process
state in the web server, so a separate process always guessed whatif.json."
```

---

### Task 14: Correct the documentation that is now false

These are not cosmetic. They state a security posture the user relies on when adding the server.

**Files:**
- Modify: `cmd/whatif-mcp/main.go:1-4`
- Modify: `internal/services/whatifmcp/scenarios.go:22-24`
- Modify: `README.md` (MCP section)
- Modify: `internal/services/whatifmcp/assumptions.md` if it repeats the read-only claim

- [ ] **Step 1: Fix the package doc**

`cmd/whatif-mcp/main.go` currently says "it never writes to the data directory and makes no network calls." All three clauses are now false. Replace:

```go
// Command whatif-mcp serves the what-if retirement planner over MCP on stdio,
// so a plan can be discussed in Claude Code. It reads saved scenarios and runs
// the projection engine.
//
// Most tools are read-only. apply_changes is not: it saves changed assumptions
// to the active scenario through the running web server, after copying that
// scenario to a snapshot outside the data directory. The server is contacted
// over HTTP on localhost and started if it is not already running.
```

- [ ] **Step 2: Fix the `Source` doc**

`scenarios.go:22-24` claims "Read-only by construction: it exposes no method that writes to the settings directory." That remains true of `Source` itself — verify it, and if so, narrow the comment to say so explicitly rather than implying the package is read-only.

- [ ] **Step 3: Update the README**

In the MCP section: list the six tools, mark `apply_changes` as writing, document `BUDGET_SERVER_URL` and `BUDGET_DATA_DIR`, state where snapshots go, and note that `.mcp.json` runs `go run ./cmd/whatif-mcp`, which triggers `go mod download` on a fresh clone — real network egress at first launch, from the toolchain rather than the server.

- [ ] **Step 4: Verify no stale claims remain**

Run: `grep -rn "read-only\|never writes\|no network calls" cmd/whatif-mcp/ internal/services/whatifmcp/ README.md`
Expected: every remaining hit is accurate in its context.

- [ ] **Step 5: Commit**

```bash
git add cmd/whatif-mcp/main.go internal/services/whatifmcp/ README.md
git commit -m "docs: correct the read-only claims the write path invalidates

The package doc promised it never writes to the data directory and makes no
network calls. It now writes snapshots, calls localhost over HTTP, and can
spawn a server. That is a change in what a user consents to when they add
this MCP server, not a comment nit."
```

---

### Task 15: Browser smoke test

The swap mechanism cannot be verified in Go. The sentinel, the `HX-Trigger` baseline update, chart reload after a polled update, and the focus guard all need a real browser — and they are the mechanism the entire feature rests on.

**Files:**
- Create: `docs/superpowers/specs/2026-08-09-whatif-mcp-live-page-smoke.md`
- Create: `scripts/whatif-poll-smoke.js` (Playwright)

**Interfaces:**
- Consumes: everything above; `scripts/whatif-verify.sh` for a throwaway instance.

- [ ] **Step 1: Write the smoke document**

Follow the shape of `docs/superpowers/specs/2026-05-03-major-expense-checkbox-pinning-smoke.md`. Each check states the action, the expected observation, and how to tell a pass from a plausible-looking failure:

1. **Container survives.** Load `/whatif` on the verify instance. Wait 10s (five poll cycles). Assert `#whatif-results` still exists and `#whatif-poll` still exists. *This is the check that would have caught the `outerHTML` design.*
2. **Idle polls are 204.** With no changes, assert every `/whatif/poll` response over 10s has status 204.
3. **External change appears.** `curl -X POST /whatif/apply -d '{"monthly_living_expenses": 9876}'`. Within 3s, assert the results column shows the new figure without a reload.
4. **Charts redraw.** After check 3, assert `#chart-projection` has non-empty rendered content and that a `/whatif/chart/projection` request was made after the poll swap.
5. **Baseline advances.** After check 3, assert subsequent polls return to 204 — if they keep returning 200, the `HX-Trigger` listener is not updating `window.__whatifRevision` and the page is re-rendering every 2s.
6. **No redundant re-render after a user edit.** Change a slider in the UI. Assert the following poll is 204, not 200.
7. **Focus guard.** Focus the monthly-expenses input and type without blurring. Trigger an external apply. Assert the input's value and focus survive for at least 6s.
8. **Existing swaps unaffected.** Click "Sync", add an income source, and switch scenarios. Assert no `htmx:targetError` appears in the console for the whole session.

- [ ] **Step 2: Write the Playwright script**

`scripts/whatif-poll-smoke.js`, driving `http://localhost:8099` (the `whatif-verify.sh` port), asserting checks 1-8 and collecting console errors throughout. Fail the run if any `htmx:targetError` or uncaught exception is observed.

- [ ] **Step 3: Run it against a throwaway instance**

```bash
scripts/whatif-verify.sh start 8099
node scripts/whatif-poll-smoke.js
scripts/whatif-verify.sh stop 8099
```

Expected: all eight checks pass, no console errors.

`whatif-verify.sh` runs against a **copy** of `data/`, so the smoke test cannot touch the real plan.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-08-09-whatif-mcp-live-page-smoke.md scripts/whatif-poll-smoke.js
git commit -m "test: browser smoke test for the what-if poll mechanism

The sentinel, the HX-Trigger baseline, chart reload, and the focus guard have
no Go-testable surface, and they are the mechanism the whole feature rests on.
Check 1 (container survives five poll cycles) is the one that would have caught
the outerHTML design before it shipped."
```

---

## Final verification

- [ ] Run the full suite with the race detector: `go test ./... -race`
- [ ] Run the static checks: `go build ./... && go vet ./... && staticcheck ./...`
- [ ] Run the browser smoke test (Task 15, steps 3)
- [ ] Manual check the spec's own §*Manual verification* item: drive `get_analysis` and `apply_changes` from a real Claude Code session against the real plan and confirm the figures match the what-if page. If they disagree, the shaping or the run path is wrong.
- [ ] Confirm `git diff master --stat` touches only what these tasks name.
