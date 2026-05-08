# Plan: `PreparedSettings` — engine input witness type

**Date:** 2026-05-08
**Tracks:** `docs/superpowers/specs/2026-05-08-architecture-deepening.md` (Candidate #2)
**Status:** Ready to execute
**Branch (proposed):** `feat/prepared-settings`

---

## Goal

Introduce a `PreparedSettings` witness type at the retirement engine boundary. It
is constructable only via `prepare.From(*WhatIfSettings)`, which deep-copies the
config, runs all load-time normalization (age derivation, phase reference,
person validation), and returns an immutable-by-convention snapshot.

`Calculator` and chain types take `PreparedSettings` instead of
`*WhatIfSettings`. Persistence and form handlers continue to use
`*WhatIfSettings`.

This kills three concrete footguns:
1. `clone := *settings` shallow-copies past the normalized derived state
   (`handlers.go:652`).
2. Perturbation analyses (sensitivity, failure-points) defensive-copy only the
   3-4 slices they currently happen to touch; new perturbed fields could alias
   silently. Deep-copy at the boundary fixes this for all fields.
3. The contract "Calculator expects normalized settings" is undocumented and
   unenforced. After this change, the type system enforces it.

## Non-Goals

- We are **not** moving the projection engine out of `Calculator`. That is
  Candidate #1.
- We are **not** removing `Load()`'s normalization (Phase C — deferred). The
  engine becomes the second normalization point; `From()` is idempotent.
- We are **not** introducing typed accessors on `PreparedSettings`. The escape
  hatch `Settings() *WhatIfSettings` stays. Tightening waits until #1.
- We are **not** moving the per-account allocation methods, healthcare math, or
  any other domain helpers off `*WhatIfSettings`. Only load-time normalization
  moves (Task 3).

## Final shape (what the codebase looks like after all 3 tasks land)

```
internal/services/retirement/
├── prepare/                       # NEW
│   ├── prepare.go                 # PreparedSettings type, From(), MustFrom(), DeepCopy()
│   ├── normalize.go               # extracted: NormalizePhaseAgeReference, ComputeAges
│   ├── validate.go                # extracted: ValidatePersons
│   ├── prepare_test.go
│   ├── normalize_test.go
│   └── validate_test.go
├── calculator.go                  # NewCalculator takes PreparedSettings
├── chain.go                       # PreparedChainLink replaces ResolvedScenarioChainLink
├── settings.go                    # Load still normalizes (Phase C deferred)
└── ...

internal/handlers/whatif/
├── handlers.go                    # buildCalculator calls prepare.From()
└── ...

internal/models/whatif.go          # ComputeAges/NormalizePhaseAgeReference/
                                   #   ValidatePersons removed (moved to prepare).
                                   # All other methods stay.
```

Caller pattern (handlers, post-refactor):

```go
settings, err := retirementMgr.Load()
if err != nil { ... }
prepared, err := prepare.From(settings)
if err != nil { ... }
calc := retirement.NewCalculator(prepared)
analysis := calc.RunFullAnalysis()
```

Perturbation pattern (sensitivity, etc.):

```go
modCfg := *c.Prepared.Settings()       // shallow copy of config
modCfg.InvestmentReturn = scenario.ParamValue
modPrepared, _ := prepare.From(&modCfg) // deep-copies + re-normalizes
modCalc := NewCalculator(modPrepared)
modCalc.RunProjection()
```

Test pattern:

```go
settings := buildTestSettings()
prepared := prepare.MustFrom(t, settings)
calc := retirement.NewCalculator(prepared)
```

---

## Task 1 — Create `prepare` package, no caller changes

**Goal:** Land the type and `From()` as a standalone, mergeable PR. Nothing
outside the new package changes. The new package is unused by production code
at the end of this task.

**Why land separately:** Establishes the type and tests in isolation. If the
deep-copy strategy is wrong or `From()`'s semantics need adjustment, we find
out before committing to the migration.

### Files to create

#### `internal/services/retirement/prepare/prepare.go`

```go
// Package prepare turns a user-facing WhatIfSettings configuration into a
// PreparedSettings witness that the retirement engine accepts.
//
// Preparation is:
//   1. Deep-copy the config (so mutations to the original don't leak).
//   2. Normalize derived state (phase reference, ages, RMD timing migration).
//   3. Validate cross-field invariants (persons, etc).
//
// The witness type's purpose is to make "I expect normalized settings" a
// compile-time guarantee at the engine boundary instead of a documented
// convention scattered across Load/Save/chain.
package prepare

import (
    "encoding/json"
    "fmt"
    "testing"

    "budget2/internal/models"
)

// PreparedSettings is the retirement engine's input. Constructable only via
// From() or MustFrom(). The underlying *WhatIfSettings has been deep-copied
// and normalized; treat it as read-only.
type PreparedSettings struct {
    s *models.WhatIfSettings
}

// Settings returns the prepared snapshot. Callers MUST NOT mutate the returned
// pointer; doing so violates the prepared invariants and may corrupt cached
// analyses. (We can't enforce this in the type system without exposing a
// large set of typed accessors; see plan 2026-05-08-prepared-settings.md.)
func (p PreparedSettings) Settings() *models.WhatIfSettings {
    return p.s
}

// IsZero reports whether p is the zero value (i.e. constructed without From()).
func (p PreparedSettings) IsZero() bool {
    return p.s == nil
}

// From deep-copies, normalizes, and validates a configuration, returning a
// PreparedSettings ready for the engine.
//
// From is idempotent: passing already-normalized settings produces an
// equivalent PreparedSettings (the deep copy still happens).
func From(cfg *models.WhatIfSettings) (PreparedSettings, error) {
    if cfg == nil {
        return PreparedSettings{}, fmt.Errorf("prepare.From: nil settings")
    }
    clone, err := DeepCopy(cfg)
    if err != nil {
        return PreparedSettings{}, fmt.Errorf("prepare.From: deep copy: %w", err)
    }
    // NOTE (Task 1): these methods still live on *WhatIfSettings. Task 3
    // moves them into this package as functions and these calls become
    // normalizePhaseAgeReference(clone), computeAges(clone), etc.
    clone.NormalizePhaseAgeReference()
    clone.ComputeAges()
    if err := clone.ValidatePersons(); err != nil {
        return PreparedSettings{}, fmt.Errorf("prepare.From: validate: %w", err)
    }
    return PreparedSettings{s: clone}, nil
}

// MustFrom wraps From for tests. It t.Fatals on error.
func MustFrom(tb testing.TB, cfg *models.WhatIfSettings) PreparedSettings {
    tb.Helper()
    p, err := From(cfg)
    if err != nil {
        tb.Fatalf("prepare.MustFrom: %v", err)
    }
    return p
}

// DeepCopy returns a deep clone of cfg. It uses JSON round-trip for simplicity;
// json:"-" fields (CurrentAge, SpouseAge) are dropped by marshal but are
// re-derived by ComputeAges immediately after, so the round-trip is lossless
// for our purposes.
//
// Performance: ~microseconds per call. Monte Carlo (1000 iterations) and
// failure-points (~80 iterations) are projection-bound, not deep-copy-bound.
// Replace with structure-aware copy only if profiling proves it.
func DeepCopy(cfg *models.WhatIfSettings) (*models.WhatIfSettings, error) {
    raw, err := json.Marshal(cfg)
    if err != nil {
        return nil, fmt.Errorf("marshal: %w", err)
    }
    out := &models.WhatIfSettings{}
    if err := json.Unmarshal(raw, out); err != nil {
        return nil, fmt.Errorf("unmarshal: %w", err)
    }
    return out, nil
}
```

#### `internal/services/retirement/prepare/prepare_test.go`

Tests to write (each as a separate `Test*` function):

1. `TestFrom_NilReturnsError` — `From(nil)` returns a non-zero error and the
   zero `PreparedSettings`.
2. `TestFrom_HappyPath` — given a minimal valid settings (one person with
   birth month, valid StartDate, allocations summing to 100), `From()` returns
   non-zero PreparedSettings with `Settings().CurrentAge > 0`.
3. `TestFrom_DeepCopy_OriginalMutationDoesNotLeak` — call `From()`, then
   mutate the original's `PortfolioValue`; verify `prepared.Settings().PortfolioValue`
   is unchanged.
4. `TestFrom_DeepCopy_SliceMutationDoesNotLeak` — same with `IncomeSources`
   slice.
5. `TestFrom_NormalizesPhaseReference` — pass settings with
   `PhaseAgeReference: "garbage"`; verify the prepared snapshot has the
   normalized value.
6. `TestFrom_ComputesAges` — pass settings with `CurrentAge: 0` but valid
   `StartDate` and `Persons`; verify `prepared.Settings().CurrentAge` is
   populated.
7. `TestFrom_ValidationErrorPropagates` — pass settings that fail
   `ValidatePersons`; verify `From()` returns wrapped error.
8. `TestFrom_Idempotent` — `From(From(s).Settings())` produces an equivalent
   prepared snapshot (same field values).
9. `TestPreparedSettings_IsZero` — zero value reports `IsZero() == true`;
   `From()`-constructed reports false.
10. `TestDeepCopy_PreservesAllJSONFields` — round-trip a fully-populated
    settings; assert every JSON-tagged field is preserved.
11. `TestMustFrom_FatalsOnError` — wrap in `testing.T`-mock-like check that
    `MustFrom(t, nil)` calls `t.Fatal`. Use `t.Run` with a sub-`testing.T`
    captured via the standard pattern.

### Verification at end of Task 1

```bash
go test ./internal/services/retirement/prepare/...
go build ./...
```

No production callsite has changed. No existing test should break. PR is small
(one new package, ~200 lines code + ~300 lines tests).

---

## Task 2 — Migrate engine + handlers to `PreparedSettings`

**Goal:** `Calculator`, chain types, and handlers all consume
`PreparedSettings`. After this PR, `*WhatIfSettings` only crosses the
prepare boundary; the engine never sees raw config.

**Single PR.** Mechanical, broad-but-shallow changes. Many test files touch.

### File-by-file

#### `internal/services/retirement/calculator.go`

1. Add field on `Calculator`:
   ```go
   type Calculator struct {
       Prepared prepare.PreparedSettings
       Settings *models.WhatIfSettings  // shortcut: == Prepared.Settings()
       // ... existing fields
   }
   ```
   Keep the existing `Settings` field for migration ergonomics — it's read by
   ~hundreds of lines internally and we don't want to touch those in this PR.
   Populate it from `Prepared.Settings()`.

2. Update `NewCalculator`:
   ```go
   func NewCalculator(prepared prepare.PreparedSettings) *Calculator {
       return &Calculator{
           Prepared: prepared,
           Settings: prepared.Settings(),
           // ... rest unchanged
       }
   }
   ```
   Delete the old signature `func NewCalculator(settings *models.WhatIfSettings)`.

3. Update `NewCalculatorWithChain`:
   ```go
   func NewCalculatorWithChain(prepared prepare.PreparedSettings, chain []PreparedChainLink) *Calculator {
       // ... constructs the resolved chain internally
   }
   ```

4. Update the 5 perturbation sites at `calculator.go:1801, 1897, 1956, 2018, 2081`:
   ```go
   // BEFORE
   modSettings := *c.Settings
   modSettings.IncomeSources = append([]models.IncomeSource{}, c.Settings.IncomeSources...)
   modSettings.InvestmentReturn = scenario.ParamValue
   modCalc := NewCalculator(&modSettings)

   // AFTER
   modCfg := *c.Settings
   modCfg.InvestmentReturn = scenario.ParamValue
   modPrepared, err := prepare.From(&modCfg)
   if err != nil {
       // these sites previously could not fail; document and decide:
       //   option A: panic — perturbation of an already-prepared snapshot
       //             should never produce invalid persons/etc.
       //   option B: log + skip the perturbation, return zero impact
       // RECOMMEND: option A (panic) — invalid means a bug, not a runtime case.
       panic(fmt.Sprintf("perturbation produced invalid settings: %v", err))
   }
   modCalc := NewCalculator(modPrepared)
   ```
   The defensive `append([]T{}, c.Settings.X...)` slice copies become **redundant**
   (deep copy in `From()` covers it) — delete them.

#### `internal/services/retirement/chain.go`

1. Replace type:
   ```go
   // DELETE: type ResolvedScenarioChainLink struct { ScenarioFilename string; TransitionAge int; Settings *models.WhatIfSettings }
   // ADD:
   type PreparedChainLink struct {
       ScenarioFilename string
       TransitionAge    int
       Settings         prepare.PreparedSettings
   }
   ```

2. Delete the `prepared.NormalizePhaseAgeReference()` and
   `prepared.ComputeAges()` calls at `chain.go:55-56`. Preparation now happens
   in `From()` before the chain link is constructed.

3. Any internal usage of `link.Settings.X` becomes `link.Settings.Settings().X`.

#### `internal/handlers/whatif/handlers.go`

1. Update `buildCalculator` (lines 79-107):
   ```go
   func buildCalculator(settings *models.WhatIfSettings) (*retirement.Calculator, string, error) {
       hashData := getSettingsHash(settings)
       prepared, err := prepare.From(settings)
       if err != nil {
           return nil, "", fmt.Errorf("prepare primary settings: %w", err)
       }

       if len(settings.ScenarioChain) == 0 {
           return retirement.NewCalculator(prepared), hashData, nil
       }

       chain := make([]retirement.PreparedChainLink, 0, len(settings.ScenarioChain))
       for _, link := range settings.ScenarioChain {
           linked, err := retirementMgr.LoadScenarioSettings(link.ScenarioFilename)
           if err != nil {
               return nil, "", fmt.Errorf("failed to load chained scenario %s: %w", link.ScenarioFilename, err)
           }
           hashData += getSettingsHash(linked)

           linkedPrepared, err := prepare.From(linked)
           if err != nil {
               return nil, "", fmt.Errorf("prepare chained scenario %s: %w", link.ScenarioFilename, err)
           }
           chain = append(chain, retirement.PreparedChainLink{
               ScenarioFilename: link.ScenarioFilename,
               TransitionAge:    link.TransitionAge,
               Settings:         linkedPrepared,
           })
       }

       combined := sha256.Sum256([]byte(hashData))
       combinedHash := fmt.Sprintf("%x", combined[:8])

       return retirement.NewCalculatorWithChain(prepared, chain), combinedHash, nil
   }
   ```

2. Update `handleWhatIfProjectionChartNoGuardrails` (lines 642-663):
   ```go
   func handleWhatIfProjectionChartNoGuardrails(w http.ResponseWriter, r *http.Request) {
       settings, err := retirementMgr.Load()
       if err != nil { /* unchanged */ }

       clone := *settings
       clone.Guardrails = nil

       prepared, err := prepare.From(&clone)
       if err != nil {
           w.Header().Set("Content-Type", "application/json")
           w.WriteHeader(http.StatusInternalServerError)
           json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
           return
       }
       calc := retirement.NewCalculator(prepared)
       projection := calc.RunProjection()
       // ... rest unchanged
   }
   ```

#### `internal/services/retirement/settings.go`

`Load()`/`Save()`/`LoadScenarioSettings()` continue to call the model methods
`NormalizePhaseAgeReference()`/`ComputeAges()` directly. **Do not remove
those calls in this task.** They're idempotent with `From()` and provide a
defense-in-depth normalization at the persistence boundary. (Removal is
deferred to Phase C.)

#### `internal/services/retirement/*_test.go`

Mechanical sweep. Pattern to find:
```bash
grep -rn 'NewCalculator\(\|NewCalculatorWithChain\(' internal/services/retirement/*_test.go
```

For each match:
```go
// BEFORE
calc := NewCalculator(settings)

// AFTER
calc := NewCalculator(prepare.MustFrom(t, settings))
```

Add `"budget2/internal/services/retirement/prepare"` import.

For test files that use a helper to build settings, add `prepare.MustFrom`
once inside the helper.

#### `internal/handlers/whatif/handlers_test.go`

This file (8,382 lines) constructs settings and calls handlers — it does **not**
construct Calculators directly. Handlers internally call `buildCalculator` /
`runAnalysisWithCache`, which now go through `prepare.From()`. Therefore most of
this file is unchanged. Only direct `retirement.NewCalculator(...)` callsites in
this file need updating. Grep first:

```bash
grep -n 'retirement\.NewCalculator\|retirement\.NewCalculatorWithChain' internal/handlers/whatif/*_test.go
```

If empty, no changes needed in this file.

#### Other test files

```bash
grep -rln 'NewCalculator\b\|NewCalculatorWithChain\b' internal/ --include='*.go'
```

Update each. Estimate: ~15-25 test files, mostly under
`internal/services/retirement/`.

### Verification at end of Task 2

```bash
go build ./...
go test ./...
```

If a test fails because it constructed `*WhatIfSettings` with invalid persons
(now caught by `From()` validation), inspect:
- If the test was actually exercising a "what happens with invalid persons?"
  path against the engine, that's a test that needs to either fix the input or
  rewrite to test `prepare.From` validation directly.
- If the test was just sloppy setup, fix the input.

### Cross-task test gating

Per project convention (see `MEMORY.md`:
`feedback_tskip_for_cross_task_deps.md`), if any test depends on Task 3's
extracted-functions API, gate it with:

```go
t.Skip("Re-enabled by Task 3 — depends on prepare.NormalizePhaseAgeReference function.")
```

Expectation: zero such gates needed for Task 2, since Task 3 only moves
implementation, not exposed API.

---

## Task 3 — Move normalization functions out of `*WhatIfSettings`

**Goal:** The methods `ComputeAges`, `NormalizePhaseAgeReference`, and
`ValidatePersons` move from `models/whatif.go` to package-level functions in
`prepare`. The model file shrinks; load-time logic becomes locality of
`prepare`.

**Why separate from Task 2:** Task 2 establishes the seam without risking the
move. Task 3 moves implementation. If Task 3 is reverted, the seam survives.

### What moves

From `internal/models/whatif.go`:
- `func (s *WhatIfSettings) ComputeAges()` (lines 324-351)
- `func (s *WhatIfSettings) NormalizePhaseAgeReference()` (lines 353-364)
- `func (s *WhatIfSettings) ValidatePersons() error` (lines 365-427)

To `internal/services/retirement/prepare/`:
- `func ComputeAges(s *models.WhatIfSettings)` in `normalize.go`
- `func NormalizePhaseAgeReference(s *models.WhatIfSettings)` in `normalize.go`
- `func ValidatePersons(s *models.WhatIfSettings) error` in `validate.go`

### What stays as methods on `*WhatIfSettings`

Everything else, including:
- `HasSpouse`, `GetPrimaryPerson`, `GetSpousePerson`, `FindPerson`, `PersonAge`
- `GetYoungerAge`, `GetOlderAge`, `GetPhaseReferenceAge`, `PrimaryAgeAt`,
  `SpouseAgeAt`
- All allocation helpers (`EffectiveStockPercent`, `GetTaxDeferredAllocation`,
  etc.)
- `GetSpendingMultiplier`, `SpendingMultiplierAt`, `GetTotalHealthcareCost`
- `HasMultiPersonHealthcare`, `GetExpectedReturnFromAllocation`,
  `GlidePathStockPct`, `GetAllocationAtYear`
- `GetProjectionTiming`, `GetTaxableQualifiedDividendPercent`

These are domain helpers — pure reads off the struct. They aren't load-time
prep. They stay where they are.

### Production callers to migrate

```bash
grep -rn 'ComputeAges\|NormalizePhaseAgeReference\|ValidatePersons' internal/ --include='*.go' | grep -v _test.go
```

Expected hits (from earlier survey):
- `internal/handlers/whatif/handlers_healthcare.go:22, 159` — comments only,
  no code change.
- `internal/models/whatif.go:848` — internal call inside model code; replace
  with `prepare.ComputeAges(settings)` (creates an import: models → prepare,
  which is a **cycle**: prepare already imports models). **This is a problem.**

**Cycle resolution:** the call at `models/whatif.go:848` must move out of the
model package, OR `ComputeAges` stays as a method (with the canonical
implementation copied — not great), OR we keep the function only in `prepare`
and refactor the caller.

Read `whatif.go:848` first:

```bash
sed -n '840,860p' internal/models/whatif.go
```

Decide based on what the caller is. Likely it's a constructor like
`NewWhatIfSettings()` that initializes ages — in which case we can either
inline the work or move that constructor to `prepare`.

- `internal/services/retirement/chain.go:55-56` — already removed in Task 2.
- `internal/services/retirement/settings.go:275-276, 317-321, 503-507` — these
  call `settings.NormalizePhaseAgeReference()` then `settings.ComputeAges()`.
  Replace with `prepare.NormalizePhaseAgeReference(settings)` then
  `prepare.ComputeAges(settings)`. (This file is in `package retirement`, can
  import the `prepare` sub-package.)
- `internal/services/retirement/prepare/prepare.go` — replace
  `clone.NormalizePhaseAgeReference()` with `NormalizePhaseAgeReference(clone)`
  (same package, just function call).

### Test callers

```bash
grep -rn 'ComputeAges\|NormalizePhaseAgeReference\|ValidatePersons' internal/ --include='*_test.go'
```

Mechanical update: `s.ComputeAges()` → `prepare.ComputeAges(s)`. Each test
file gains the prepare import.

### Final state of `models/whatif.go`

Three methods removed (~65 lines). File drops from 1,433 to ~1,365 LOC. Not
the headline win, but the *meaning* of those 65 lines has moved: load-time
prep concerns now live with the engine, not with the data struct.

### Verification at end of Task 3

```bash
go build ./...
go test ./...
go vet ./...
```

---

## Test strategy

### What tests we add (Task 1)

Eleven `prepare_test.go` cases enumerated above. These are unit tests against
the new package — fast, isolated, no fixtures.

### What tests we expect to break (Task 2)

- Any retirement-package test that constructed an *invalid* settings (e.g.,
  conflicting persons) and previously got past `NewCalculator()` will now fail
  at `MustFrom(t, settings)`. Each break is informative: either the test was
  exercising invalid-input-handling at the wrong layer (move it to a `prepare`
  test), or the test was sloppy.

### What tests we don't change

- `internal/handlers/whatif/handlers_test.go` — handlers still take
  `*WhatIfSettings`. The internal pipe (`buildCalculator → prepare.From →
  Calculator`) is opaque to the handler tests.

### Coverage expectations

The `prepare` package targets 100% line coverage. The extracted functions in
Task 3 retain their existing test coverage (the tests follow them — see Test
callers above).

---

## Risks and mitigations

### Risk: deep-copy via JSON drops a field we didn't notice

Verified: only `CurrentAge` and `SpouseAge` carry `json:"-"`. Both are
recomputed by `ComputeAges` immediately after the clone. Mitigation: Task 1
test #10 (`TestDeepCopy_PreservesAllJSONFields`) round-trips a fully-populated
settings and asserts equality field-by-field.

### Risk: `Calculator.Settings *WhatIfSettings` still exported

True. We're keeping it as a migration shortcut. Anyone who reaches in and
mutates it bypasses the prepared snapshot. Mitigation: grep at end of Task 2
for any `c.Settings.X = ` outside test files; should be zero (verified during
plan-writing — the 5 perturbation sites all use `modSettings := *c.Settings`,
not in-place mutation). The `Settings` field can be unexported in a follow-up
once #1's analysis modules settle.

### Risk: perturbation analyses now pay deep-copy + re-normalization cost

For sensitivity (4 calls), failure-points (~80), and Monte Carlo (1000),
total added cost is on the order of milliseconds — negligible against the
projection itself which loops over months. Mitigation: profile only if
something feels slow; do not pre-optimize. Plan note: see "Open question α"
in grilling — we explicitly chose to *not* add a `Perturb()` skip-normalize
fast path, because it reintroduces the footgun this refactor exists to kill.

### Risk: a test depends on un-normalized settings reaching the engine

Possible but unlikely. If it happens, the fix is one of:
- Inline-normalize in the test fixture (test no longer expects un-normalized).
- Test moves to `prepare_test.go` (it was actually testing normalization).
- Document as a deliberate edge case and use a back-door test helper that
  bypasses `From()` (`prepare.unsafeFromForTesting`). **Strongly prefer not.**

### Rollback

Each task is one PR. To roll back:
- Task 3: revert PR. Methods restored on `*WhatIfSettings`. `prepare` calls
  fall through to method calls.
- Task 2: revert PR. Engine takes `*WhatIfSettings` again. `prepare` package
  becomes unused (but doesn't break builds).
- Task 1: revert PR. Package gone.

---

## Out of scope (explicit)

These are tempting but explicitly **not part of this plan**:

- Removing the `Settings *WhatIfSettings` field from `Calculator`. Defer until
  #1 carves the projection engine out.
- Replacing the JSON-roundtrip `DeepCopy` with a structure-aware copy. Profile
  first.
- Removing `Load()`'s normalization in `settings.go`. Defer to Phase C.
- Adding typed accessors on `PreparedSettings`. Defer until #1.
- Centralizing the chain-prep loop into `prepare.FromChain`. Decided in
  grilling (Q2): chain assembly is scenario-stitching, not preparation.
- Touching the projection engine internals. That's Candidate #1.

---

## Estimated size

- Task 1: ~200 LOC production, ~300 LOC tests. 1 small PR.
- Task 2: ~50 LOC production change + ~100-200 LOC mechanical test updates.
  Touches ~20 files. 1 broad-but-shallow PR.
- Task 3: ~80 LOC moved, ~30 callsite updates. 1 medium PR.

Total: 3 PRs, lands incrementally on `dev`.

---

## Tracker update on completion

Update `docs/superpowers/specs/2026-05-08-architecture-deepening.md`
Candidate #2 status to `Landed` with the merge commit SHAs of all three
PRs. Then start grilling Candidate #1 (or #3 if #1's RMD-from-projection
work is still in flight).
