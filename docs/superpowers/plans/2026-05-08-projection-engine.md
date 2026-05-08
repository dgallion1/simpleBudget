# Projection Engine + Analyses Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract a stateless Projection Engine from the 3,162-LOC `Calculator`, decompose its 10 analyses into a new `analysis` package, replace `Calculator.RunFullAnalysis` with `retirement.RunFull(eng, in)`, and delete the `Calculator` type.

**Architecture:** Strangler-fig refactor across 8 commits on a single branch (`feat/projection-engine`). Each commit compiles and passes `go test ./...`. A temporary parity test (`TestParity_FullAnalysis_AcrossFixtures`) added in commit 1 and deleted in commit 8 guards every intermediate state. Calculator methods become one-line delegators progressively until the type is removed.

**Tech Stack:** Go 1.x (existing module). No new third-party dependencies. Standard library `math/rand`, `testing`, `math`.

**Spec:** [`docs/superpowers/specs/2026-05-08-calculator-orchestrator-design.md`](../specs/2026-05-08-calculator-orchestrator-design.md)

---

## Preconditions

1. `feat/prepared-settings` is merged to `dev`. If not yet merged, branch
   `feat/projection-engine` off `feat/prepared-settings` instead and rebase
   on `dev` once it lands.
2. `go test ./...` is green on the chosen base branch.
3. No active edits to files in `internal/services/retirement/` outside this
   plan's branch.

## File structure (final state, after Task 8)

```
internal/services/retirement/
├── prepare/                        (unchanged)
├── engine/                         NEW
│   ├── engine.go                   Engine, New, Run
│   ├── input.go                    Input, PreparedChainLink
│   ├── month.go                    runMonthlyLoop (private)
│   ├── income.go                   totalIncome (private)
│   ├── expense.go                  totalExpenses, expenseBreakdown (private)
│   └── healthcare.go               healthcarePV (private)
├── analysis/                       NEW
│   ├── rmd.go                      BuildRMD
│   ├── sustainability.go           Score
│   ├── explainability.go           BuildExplainability
│   ├── budget_fit.go               BudgetFit
│   ├── present_value.go            PresentValue
│   ├── sensitivity.go              Sensitivity
│   ├── failure_points.go           FailurePoints
│   ├── monte_carlo.go              MonteCarlo
│   ├── ss.go                       SSAnalysis, SSPortfolio
│   ├── backtest.go                 HistoricalBacktest
│   ├── perturb.go                  perturbAndPrepare (private)
│   └── *_test.go                   migrated test files
├── history/                        NEW
│   └── data.go                     Data type, DefaultData, Sequence, AvailableStartYears, Stats
├── orchestrator.go                 NEW — RunFull
├── eligibility.go                  NEW — SSPortfolioEligible, FirstRMDCalendarYear
├── chain.go                        modified (returns []engine.PreparedChainLink)
├── settings.go                     unchanged
├── tax.go                          unchanged
├── guardrails.go                   unchanged
└── (DELETED: calculator.go, rmd.go, social_security.go, backtest.go, historical_data.go, parity_test.go)
```

## Per-commit invariants

After every task in this plan:
- `go build ./...` succeeds.
- `go test ./...` passes (parity test enforces semantic equivalence).
- `go vet ./...` is clean.
- The pre-commit hook passes.

If any of these fail, fix before committing.

---

## Task 0: Branch setup

**Files:** none (git only).

- [ ] **Step 1: Confirm base branch state.**

Run: `git status && git log --oneline -1`
Expected: clean working tree, on `dev` (or `feat/prepared-settings` if dev hasn't received the prepared-settings merge yet).

- [ ] **Step 2: Create the feature branch.**

Run: `git checkout -b feat/projection-engine`
Expected: switched to new branch.

- [ ] **Step 3: Verify green baseline.**

Run: `go test ./...`
Expected: all packages pass.

No commit on this task.

---

## Task 1 — split into 1a/1b/1c/1d

**Why split:** Initial execution surfaced that the projection loop's helper
graph fans out across the entire retirement subsystem (~30 free functions
and ~5 types shared with backtest, MC, BudgetFit, PresentValue). Moving
RunProjection's body in a single commit would require relocating ~1500-2000
LOC of helpers in lockstep. The split below pre-extracts shared primitives
into engine/ across three preparatory commits, leaving Task 1d as a
mechanical body-move once the helpers are already in place.

End state of all four sub-tasks: identical to what the original Task 1
described — engine package owns the projection loop and its primitives,
Calculator's methods are one-line delegators, parity test guards
correctness.

---

## Task 1a: Engine skeleton + easy helper moves (healthcare, income, expense)

**Files:**
- Create: `internal/services/retirement/engine/input.go`
- Create: `internal/services/retirement/engine/engine.go`
- Create: `internal/services/retirement/engine/healthcare.go`
- Create: `internal/services/retirement/engine/income.go`
- Create: `internal/services/retirement/engine/expense.go`
- Modify: `internal/services/retirement/calculator.go`

### Part A — Engine package skeleton

- [ ] **Step 1: Create `engine/input.go`.**

```go
package engine

import (
	"budget2/internal/services/retirement/prepare"
)

// Input bundles everything Engine.Run needs. Chain may be nil for
// single-scenario projections.
type Input struct {
	Prepared prepare.PreparedSettings
	Chain    []PreparedChainLink
}

// PreparedChainLink describes a scenario transition that fires when the
// reference person reaches TransitionAge. Settings is the prepared
// snapshot for the post-transition scenario.
type PreparedChainLink struct {
	ScenarioFilename string
	TransitionAge    int
	Settings         prepare.PreparedSettings
}
```

- [ ] **Step 2: Create `engine/engine.go`.**

In Task 1a, `Engine.Run` is a stub — its real body (`runMonthlyLoop`)
arrives in Task 1d. The stub panics so any accidental caller fails
loudly during the migration. Calculator.RunProjection retains its
original body during 1a/1b/1c.

```go
// Package engine runs deterministic retirement projections from prepared
// settings. It has no caching, no global state, and Run is a pure
// function of its Input.
package engine

import (
	"budget2/internal/models"
)

// Engine is a stateless projection runner. Future deepening (caching,
// tracing, fault injection) can land on the struct without changing
// call sites.
type Engine struct{}

// New returns an Engine. Cheap; callers may construct per request.
func New() *Engine { return &Engine{} }

// Run produces a deterministic monthly projection for in. Returns a
// fully populated *models.ProjectionResult. Never returns nil. Run is a
// pure function of in.
//
// During Task 1a/1b/1c the body is a stub; the real implementation
// arrives in Task 1d once all helper dependencies have been moved into
// this package.
func (e *Engine) Run(in Input) *models.ProjectionResult {
	panic("engine.Run: not yet implemented (arrives in Task 1d)")
}
```

- [ ] **Step 3: Add type alias and constructor delegation in `chain.go` and `calculator.go`.**

In `internal/services/retirement/calculator.go`, replace the existing
`PreparedChainLink` type definition (calculator.go:14-19) with a type
alias so all callers still compile:

```go
// PreparedChainLink is re-exported from the engine package so existing
// callers (handlers, tests) keep compiling during the migration. The
// alias is removed in Task 8 alongside Calculator.
type PreparedChainLink = engine.PreparedChainLink
```

Add the import: `"budget2/internal/services/retirement/engine"`.

`Calculator.ResolvedChain []PreparedChainLink` keeps the same shape via
the alias; no other change needed in this step.

- [ ] **Step 4: Verify the alias compiles.**

Run: `go build ./...`
Expected: success.

### Part B — Move projection helpers into engine

- [ ] **Step 5: Move `calculateHealthcarePV` to `engine/healthcare.go`.**

Source: `internal/services/retirement/calculator.go:114-145`.
Destination: `internal/services/retirement/engine/healthcare.go`.

Convert from `func (c *Calculator) calculateHealthcarePV(...)` to free
function `func healthcarePV(s *models.WhatIfSettings, person models.HealthcarePerson, discountRate float64, totalMonths int) float64`.
Replace `c.Settings` references with `s`. Keep the body otherwise verbatim.

In `calculator.go`, leave a one-line delegator:
```go
func (c *Calculator) calculateHealthcarePV(person models.HealthcarePerson, discountRate float64, totalMonths int) float64 {
	// Delegator during migration; removed in Task 8.
	return engineHealthcarePV(c.Settings, person, discountRate, totalMonths)
}
```

Because `healthcarePV` is unexported and Calculator is in a different
package, expose a package-internal handle. Add at the top of
`engine/healthcare.go`:
```go
// HealthcarePVForCalculator is a parity-window export so Calculator's
// delegator can call into engine. Removed in Task 8.
func HealthcarePVForCalculator(s *models.WhatIfSettings, person models.HealthcarePerson, discountRate float64, totalMonths int) float64 {
	return healthcarePV(s, person, discountRate, totalMonths)
}
```

Then in `calculator.go`, the delegator becomes
`engine.HealthcarePVForCalculator(c.Settings, person, discountRate, totalMonths)`.

- [ ] **Step 6: Run tests after the helper move.**

Run: `go test ./internal/services/retirement/...`
Expected: all retirement-package tests pass.

- [ ] **Step 7: Move `CalculateTotalIncome` to `engine/income.go`.**

Source: `internal/services/retirement/calculator.go:146-583` (the entire
function body).
Destination: `internal/services/retirement/engine/income.go`.

Free function: `func totalIncome(s *models.WhatIfSettings, month int) float64`.
Replace every `c.Settings` reference with `s`. Internal helpers it calls
(if any) move with it (they already operate on settings, not Calculator).

Mirror the parity-window export:
```go
func TotalIncomeForCalculator(s *models.WhatIfSettings, month int) float64 {
	return totalIncome(s, month)
}
```

In `calculator.go`, replace `func (c *Calculator) CalculateTotalIncome(month int) float64` with:
```go
func (c *Calculator) CalculateTotalIncome(month int) float64 {
	return engine.TotalIncomeForCalculator(c.Settings, month)
}
```

- [ ] **Step 8: Verify after income move.**

Run: `go test ./internal/services/retirement/...`
Expected: pass.

- [ ] **Step 9: Move `CalculateTotalExpenses` and `CalculateExpenseBreakdown` to `engine/expense.go`.**

Source: `calculator.go:584-1033` (both functions and any private helpers
they exclusively use).
Destination: `engine/expense.go`.

Free functions: `totalExpenses(s *models.WhatIfSettings, month int) float64`
and `expenseBreakdown(s *models.WhatIfSettings, month int) ExpenseBreakdown`.

`ExpenseBreakdown` type currently lives in `calculator.go`. Move the type
definition to `engine/expense.go` as well; in `calculator.go` add:
```go
type ExpenseBreakdown = engine.ExpenseBreakdown
```

Add parity-window exports:
```go
func TotalExpensesForCalculator(s *models.WhatIfSettings, month int) float64 {
	return totalExpenses(s, month)
}

func ExpenseBreakdownForCalculator(s *models.WhatIfSettings, month int) ExpenseBreakdown {
	return expenseBreakdown(s, month)
}
```

Calculator methods become one-line delegators.

- [ ] **Step 10: Verify after expense move.**

Run: `go test ./...`
Expected: all packages pass. Engine.Run is still a panic stub but no
caller invokes it yet — Calculator.RunProjection retains its original
body in Task 1a.

- [ ] **Step 11: Run vet.**

Run: `go vet ./...`
Expected: clean.

- [ ] **Step 12: Commit Task 1a.**

```bash
git add internal/services/retirement/engine/ internal/services/retirement/calculator.go
git commit -m "$(cat <<'EOF'
feat(retirement): introduce engine package skeleton + healthcare/income/expense

Establishes the engine package with the Engine type, Input, and
PreparedChainLink (alias-imported back into retirement). Engine.Run is
a panic stub during the migration; the real body arrives in Task 1d
once the helper dependencies are fully relocated.

Moves three settings-driven helpers into engine as private functions
with parity-window export shims so Calculator's methods continue to
work as one-line delegators:
- calculateHealthcarePV  → engine.healthcarePV
- CalculateTotalIncome   → engine.totalIncome
- CalculateTotalExpenses → engine.totalExpenses
- CalculateExpenseBreakdown → engine.expenseBreakdown

Tasks 1b/1c relocate the deeply-shared primitives (taxable account,
projection-tax accumulator, guardrails/RMD helpers used by the loop);
Task 1d moves the projection body itself and wires up the parity test.

Tracker: docs/superpowers/specs/2026-05-08-architecture-deepening.md
Spec:    docs/superpowers/specs/2026-05-08-calculator-orchestrator-design.md

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 1b: Pre-extract taxable account + projection-tax accumulator into engine

**Goal:** Move the two largest shared primitives from
`internal/services/retirement/calculator.go` and
`internal/services/retirement/tax.go` into `engine/`, with parity-window
export shims so retirement-side callers (calculator, backtest, etc.)
continue to compile during the migration window.

**Files (likely):**
- Create: `internal/services/retirement/engine/taxable.go`
  - Symbols moved: `taxableAccountState`, `newTaxableAccountState`,
    `applyGrowth`, `withdraw`, `addCash`, `syncAssumptions`,
    `buildTaxableReturnComponents`, `taxableReturnComponents`,
    `taxableGrowthResult`, plus any private helpers exclusively used by
    these.
- Create: `internal/services/retirement/engine/projtax.go`
  - Symbols moved: `projectionTaxAccumulator`, `projectedTaxSnapshot`,
    `applyMonth`, `estimateMonthlySnapshot`, `annualizedInputs`,
    `estimateMonthlyTaxes`.
- Modify: `internal/services/retirement/calculator.go`,
  `internal/services/retirement/tax.go`,
  `internal/services/retirement/backtest.go` — switch every reference
  from the now-moved symbol to `engine.X` (parity-window exports).

**Concrete steps:** the implementer should enumerate the dependency
graph for each tier-1 primitive (e.g.,
`grep -rn "newTaxableAccountState\|taxableAccountState{" internal/`),
then move the symbols in a single commit. For every moved private
symbol, add an exported `XForCalculator` (or similar) shim in engine
so the retirement-side caller compiles.

**Verification:** `go build ./...` and `go test ./...` pass at end of
task. Engine.Run remains a panic stub.

**Commit message:**

```
refactor(retirement): pre-extract taxable account + projection-tax accumulator into engine

Moves the two largest shared primitives — the taxable account state
machine and the projection tax accumulator — into the engine package
with parity-window export shims. Calculator, tax.go, and backtest.go
now reach these primitives through engine.X rather than calling the
now-private engine helpers directly.

Sets the stage for Task 1c (guardrails/RMD/SS helpers used by the
projection loop) and Task 1d (the projection body itself).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

The implementer for 1b is dispatched after 1a lands, so the actual
symbol list is finalized against the post-1a tree (some helpers may
already be in engine).

---

## Task 1c: Pre-extract remaining projection-loop helpers into engine

**Goal:** Move the tier-2 primitives — guardrails state, RMD-day
helpers, big-ticket apply, monthly income breakdown, expense rebase —
into `engine/`, again with parity-window exports.

**Files (likely):**
- Create or extend: `internal/services/retirement/engine/guardrails.go`,
  `engine/rmdcalc.go`, `engine/bigticket.go`, `engine/incomebreakdown.go`,
  `engine/expenserebase.go`. Final naming TBD by 1c implementer based
  on what naturally clusters.
- Modify: `internal/services/retirement/calculator.go`,
  `guardrails.go`, `rmd.go`, `tax.go`, `backtest.go` — switch
  references.

**Notes for 1c implementer:**
- Free functions in `rmd.go` like `RMDApplies`, `CalculateRMD`,
  `RMDAgeForCalendarYear`, `rmdTriggerMonth` are used by both the
  projection loop and `BuildRMDAnalysis`. Move to engine.
- `FirstRMDCalendarYear` is also called from handler code; keep
  exported in engine and update any external callers in Task 7.
- `newGuardrailState` and friends in `guardrails.go` move to engine.
- `executeTaxAwarePortfolioMonth` in `tax.go` is the meeting point of
  several primitives; whether it stays in tax.go or moves to engine is
  implementer's call based on what minimizes shim count. Default:
  move to engine.

**Verification:** `go build ./...` and `go test ./...` pass. Engine.Run
remains a panic stub.

**Commit message:**

```
refactor(retirement): pre-extract guardrails/RMD/loop helpers into engine

Continues the pre-extraction in preparation for moving RunProjection's
body into engine. Moves the tier-2 shared primitives (guardrails state
machine, RMD-day helpers, big-ticket apply, monthly income breakdown,
expense rebase, tax-aware portfolio month) into engine, with parity-
window export shims for retirement-side callers.

After this commit, Engine.Run's body has every helper it needs sitting
in the engine package — Task 1d is then the mechanical move of the
loop body itself.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## Task 1d: Move RunProjection body + parity scaffolding

By the time 1d runs, every helper RunProjection needs already lives in
engine (private names with parity-window exports). 1d's body move
becomes mechanical.

**Files:**
- Create: `internal/services/retirement/engine/month.go`
- Create: `internal/services/retirement/parity_test.go`
- Create: `internal/services/retirement/parity_helpers_test.go`
- Create: `internal/services/retirement/parity_fixtures_test.go`
- Modify: `internal/services/retirement/engine/engine.go` (replace
  panic stub)
- Modify: `internal/services/retirement/calculator.go` (RunProjection
  becomes a one-line delegator; add `Calculator.SetMonteCarloSeedForParity`).

### Part C (was) — Move RunProjection body into engine

- [ ] **Step 10d: Replace Engine.Run's panic stub.**

Update `internal/services/retirement/engine/engine.go` so `Run`
delegates to `runMonthlyLoop` (which is added in Step 11):

```go
func (e *Engine) Run(in Input) *models.ProjectionResult {
	return runMonthlyLoop(in)
}
```

This will not compile until Step 11 lands. That is intentional — both
edits land in the same commit.

- [ ] **Step 11: Move `RunProjection` body to `engine/month.go`.**

Source: `calculator.go:1034-1388` (the entire function body including
`findSteadyStateMonth` if helper is private and only used by RunProjection).
Destination: `engine/month.go`.

Convert the body into:
```go
package engine

func runMonthlyLoop(in Input) *models.ProjectionResult {
	primarySettings := in.Prepared.Settings()
	activeSettings := primarySettings
	chain := in.Chain
	nextChainIdx := 0
	s := activeSettings
	// ... rest of the body verbatim, replacing:
	//   c.Settings        → s   (or activeSettings; mirror current usage)
	//   c.ResolvedChain   → chain
	//   c.calculateHealthcarePV(...) → healthcarePV(s, ...)
	//   c.CalculateTotalIncome(m)    → totalIncome(s, m)
	//   c.CalculateTotalExpenses(m)  → totalExpenses(s, m)
	//   c.CalculateExpenseBreakdown(m) → expenseBreakdown(s, m)
}
```

Move `findSteadyStateMonth` (calculator.go:1680-1715) to `engine/month.go`
as private `findSteadyStateMonth(months []models.ProjectionMonth) int`.
Adjust internal callers.

In `calculator.go`, replace the `RunProjection` body with a one-line delegator:
```go
func (c *Calculator) RunProjection() *models.ProjectionResult {
	return engine.New().Run(engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}
```

If `findSteadyStateMonth` has callers outside RunProjection, expose a
parity-window forwarder; otherwise it is fully internal to the engine.

- [ ] **Step 12: Build and test after the projection move.**

Run: `go build ./... && go test ./internal/services/retirement/...`
Expected: build succeeds; all retirement tests pass.

### Part D — Parity test scaffolding

- [ ] **Step 13: Add the parity helpers file `parity_helpers_test.go`.**

Create `internal/services/retirement/parity_helpers_test.go`:

```go
//go:build !short

package retirement

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// runFullForParity assembles a *models.WhatIfAnalysis the same way
// Calculator.RunFullAnalysis does, but using the engine + analysis
// packages where they exist. As later tasks land, this helper shrinks
// until it is identical to RunFull (Task 6) and can be replaced by it.
//
// During Task 1, the only available engine surface is engine.Run, so
// runFullForParity falls back to running the projection through the
// engine and constructing a Calculator for everything else. Task 2-6
// each replace one block here with the corresponding analysis call.
func runFullForParity(eng *engine.Engine, in engine.Input, mcSeed int64) *models.WhatIfAnalysis {
	tmp := NewCalculatorWithChain(in.Prepared, in.Chain)
	tmp.SetMonteCarloSeedForParity(mcSeed)
	out := tmp.RunFullAnalysis()
	// Replace the projection in the result with one produced via engine.Run
	// to confirm equivalence end-to-end.
	out.Projection = eng.Run(in)
	return out
}

// compareWhatIfAnalysis performs a tolerant deep-equal. Returns "" when
// the inputs match, else a short diff describing the first mismatch.
func compareWhatIfAnalysis(a, b *models.WhatIfAnalysis) string {
	if a == nil || b == nil {
		if a == b {
			return ""
		}
		return fmt.Sprintf("nil mismatch: a=%v b=%v", a == nil, b == nil)
	}
	return diffValues("WhatIfAnalysis", reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem())
}

const floatTol = 1e-9
const floatRelTol = 1e-12

func diffValues(path string, a, b reflect.Value) string {
	if a.Type() != b.Type() {
		return fmt.Sprintf("%s: type mismatch %v vs %v", path, a.Type(), b.Type())
	}
	switch a.Kind() {
	case reflect.Ptr, reflect.Interface:
		if a.IsNil() != b.IsNil() {
			return fmt.Sprintf("%s: nil mismatch a=%v b=%v", path, a.IsNil(), b.IsNil())
		}
		if a.IsNil() {
			return ""
		}
		return diffValues(path, a.Elem(), b.Elem())
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			fname := a.Type().Field(i).Name
			if d := diffValues(path+"."+fname, a.Field(i), b.Field(i)); d != "" {
				return d
			}
		}
		return ""
	case reflect.Slice, reflect.Array:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: length %d vs %d", path, a.Len(), b.Len())
		}
		for i := 0; i < a.Len(); i++ {
			if d := diffValues(fmt.Sprintf("%s[%d]", path, i), a.Index(i), b.Index(i)); d != "" {
				return d
			}
		}
		return ""
	case reflect.Map:
		if a.Len() != b.Len() {
			return fmt.Sprintf("%s: map length %d vs %d", path, a.Len(), b.Len())
		}
		for _, k := range a.MapKeys() {
			bv := b.MapIndex(k)
			if !bv.IsValid() {
				return fmt.Sprintf("%s: missing key %v in b", path, k.Interface())
			}
			if d := diffValues(fmt.Sprintf("%s[%v]", path, k.Interface()), a.MapIndex(k), bv); d != "" {
				return d
			}
		}
		return ""
	case reflect.Float32, reflect.Float64:
		x, y := a.Float(), b.Float()
		if math.IsNaN(x) && math.IsNaN(y) {
			return ""
		}
		d := math.Abs(x - y)
		if d <= floatTol {
			return ""
		}
		mag := math.Max(math.Abs(x), math.Abs(y))
		if mag > 0 && d/mag <= floatRelTol {
			return ""
		}
		return fmt.Sprintf("%s: float %g vs %g (delta %g)", path, x, y, d)
	case reflect.String:
		if a.String() != b.String() {
			return fmt.Sprintf("%s: %q vs %q", path, a.String(), b.String())
		}
		return ""
	default:
		if !reflect.DeepEqual(a.Interface(), b.Interface()) {
			as := fmt.Sprintf("%v", a.Interface())
			bs := fmt.Sprintf("%v", b.Interface())
			if len(as) > 80 {
				as = as[:80] + "…"
			}
			if len(bs) > 80 {
				bs = bs[:80] + "…"
			}
			return fmt.Sprintf("%s: %s vs %s", path, strings.TrimSpace(as), strings.TrimSpace(bs))
		}
		return ""
	}
}
```

- [ ] **Step 14: Add `Calculator.SetMonteCarloSeedForParity` (temporary).**

In `calculator.go`, add immediately after the `Calculator` struct definition:

```go
// monteCarloSeedOverride lets parity tests force a deterministic MC RNG
// so byte-equal comparisons are possible. It is set only by
// SetMonteCarloSeedForParity. Removed in Task 8 alongside Calculator.
type monteCarloSeedOverride struct {
	set  bool
	seed int64
}

func (c *Calculator) SetMonteCarloSeedForParity(seed int64) {
	c.mcSeedOverride = monteCarloSeedOverride{set: true, seed: seed}
}
```

Add the field to the `Calculator` struct:
```go
type Calculator struct {
	Prepared       prepare.PreparedSettings
	Settings       *models.WhatIfSettings
	ResolvedChain  []PreparedChainLink
	mcSeedOverride monteCarloSeedOverride // parity-window only; removed in Task 8
}
```

In `RunMonteCarloSimulation` (calculator.go:2203), at the top where the
RNG is created (today the seed is computed from `time.Now()` if not
explicitly set), insert:
```go
if c.mcSeedOverride.set {
	rng = rand.New(rand.NewSource(c.mcSeedOverride.seed))
} else {
	// existing seed logic unchanged
}
```

The exact patch depends on the current RNG construction; preserve the
default branch and add the override branch in front.

- [ ] **Step 15: Add the parity test file `parity_test.go`.**

Create `internal/services/retirement/parity_test.go`:

```go
//go:build !short

package retirement

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

type parityFixture struct {
	Name     string
	Settings *models.WhatIfSettings
	MCSeed   int64
}

// parityFixtures returns a small representative set covering projection
// paths most likely to drift: baseline solo, MFJ with SS, chain-linked
// scenarios, RMD-active, guardrails on/off, taxable+tax-deferred mix.
func parityFixtures(t testing.TB) []parityFixture {
	t.Helper()
	return []parityFixture{
		{Name: "baseline-solo", Settings: parityBaselineSolo(), MCSeed: 0xCAFEF00D},
		{Name: "mfj-with-ss", Settings: parityMFJWithSS(), MCSeed: 0xCAFEF00D},
		{Name: "rmd-active", Settings: parityRMDActive(), MCSeed: 0xCAFEF00D},
		{Name: "guardrails-on", Settings: parityGuardrailsOn(), MCSeed: 0xCAFEF00D},
		{Name: "taxable-mix", Settings: parityTaxableMix(), MCSeed: 0xCAFEF00D},
	}
}

func TestParity_FullAnalysis_AcrossFixtures(t *testing.T) {
	for _, f := range parityFixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			calc := NewCalculator(prepare.MustFrom(t, f.Settings))
			calc.SetMonteCarloSeedForParity(f.MCSeed)
			old := calc.RunFullAnalysis()

			in := engine.Input{Prepared: prepare.MustFrom(t, f.Settings)}
			new := runFullForParity(engine.New(), in, f.MCSeed)

			if diff := compareWhatIfAnalysis(old, new); diff != "" {
				t.Fatalf("parity diff:\n%s", diff)
			}
		})
	}
}
```

- [ ] **Step 16: Add fixture builders.**

Create `internal/services/retirement/parity_fixtures_test.go` with five
small builders. Use the existing `helpers_test.go` patterns
(`newTestCalc`) as a guide for how to construct settings. Each fixture
returns a fully formed `*models.WhatIfSettings`. Example skeleton (adapt
field-by-field by inspecting `helpers_test.go` for canonical defaults):

```go
//go:build !short

package retirement

import "budget2/internal/models"

func parityBaselineSolo() *models.WhatIfSettings {
	s := defaultParitySettings()
	return s
}

func parityMFJWithSS() *models.WhatIfSettings {
	s := defaultParitySettings()
	// add SocialSecurity, spouse, MFJ filing status
	return s
}

func parityRMDActive() *models.WhatIfSettings {
	s := defaultParitySettings()
	// CurrentAge close to 73, large TaxDeferred balance
	return s
}

func parityGuardrailsOn() *models.WhatIfSettings {
	s := defaultParitySettings()
	// s.Guardrails = &models.GuardrailConfig{Enabled: true, ...}
	return s
}

func parityTaxableMix() *models.WhatIfSettings {
	s := defaultParitySettings()
	// 33/33/34 split TaxDeferredPercent, RothPercent, taxable
	return s
}

// defaultParitySettings returns a WhatIfSettings populated with the
// minimum fields required for prepare.From to succeed. Mirrors the
// shape of helpers_test.go's existing fixture conventions. Values are
// copied from helpers_test.go's `defaultSettings()` if present; if
// not, use the smallest set that yields prepare.From success.
func defaultParitySettings() *models.WhatIfSettings {
	// implementer: copy from helpers_test.go's defaultSettings or
	// equivalent. Do NOT introduce new field defaults.
}
```

The implementing engineer should inspect `helpers_test.go` and reuse the
existing `defaultSettings()`-style helper if available (rename to
`defaultParitySettings` if needed to avoid collision). The five fixtures
are minimal variations on that base.

- [ ] **Step 17: Run the parity test.**

Run: `go test -run TestParity_FullAnalysis_AcrossFixtures ./internal/services/retirement/`
Expected: all 5 sub-tests pass. Each comparison's `old` path uses
`Calculator.RunFullAnalysis`; `new` path uses `runFullForParity`, which
in Task 1 only diverges in the projection slice (engine vs. calc-internal
projection). Both should be identical.

- [ ] **Step 18: Run full test suite.**

Run: `go test ./...`
Expected: all packages pass.

- [ ] **Step 19: Run vet.**

Run: `go vet ./...`
Expected: clean.

- [ ] **Step 20: Commit.**

```bash
git add internal/services/retirement/engine/ \
        internal/services/retirement/parity_test.go \
        internal/services/retirement/parity_helpers_test.go \
        internal/services/retirement/parity_fixtures_test.go \
        internal/services/retirement/calculator.go \
        internal/services/retirement/chain.go
git commit -m "$(cat <<'EOF'
feat(retirement): introduce engine package with Run

Extracts Calculator.RunProjection plus its private helpers
(CalculateTotalIncome/Expenses/ExpenseBreakdown/calculateHealthcarePV/
findSteadyStateMonth) into a new engine package behind a tiny
Engine.Run(Input) interface. Calculator's methods become one-line
delegators routed through parity-window export shims.

Adds parity scaffolding (parity_test.go, runFullForParity,
compareWhatIfAnalysis, parity fixtures, Calculator MC seed override)
that guards every intermediate state of this refactor and is removed
in the final cleanup commit.

Tracker: docs/superpowers/specs/2026-05-08-architecture-deepening.md
Spec:    docs/superpowers/specs/2026-05-08-calculator-orchestrator-design.md

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Pure post-projection analyses (RMD, Sustainability, Explainability)

**Files:**
- Create: `internal/services/retirement/analysis/rmd.go`
- Create: `internal/services/retirement/analysis/sustainability.go`
- Create: `internal/services/retirement/analysis/explainability.go`
- Create: `internal/services/retirement/analysis/rmd_test.go` (and friends, migrated)
- Modify: `internal/services/retirement/calculator.go`
- Modify: `internal/services/retirement/rmd.go`
- Modify: `internal/services/retirement/parity_helpers_test.go`
- Move test files: `rmd_test.go`, `rmd_birth_year_test.go`, `rmd_calendar_year_test.go`, `rmd_start_age_projection_test.go`, `rmd_tax_test.go`, `rmd_timing_test.go`, `calculator_rmd_gross_test.go` from `retirement/` to `retirement/analysis/`

- [ ] **Step 1: Create `analysis/rmd.go` with `BuildRMD`.**

Move the body of `Calculator.BuildRMDAnalysis(projection *models.ProjectionResult)`
from `rmd.go:225` to `analysis/rmd.go` as:

```go
package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// BuildRMD constructs the RMD analysis from a completed projection and
// the input settings. Equivalent to the previous
// Calculator.BuildRMDAnalysis.
func BuildRMD(proj *models.ProjectionResult, in engine.Input) *models.RMDAnalysis {
	s := in.Prepared.Settings()
	// Body: copy verbatim from rmd.go:226 onward, replacing:
	//   c.Settings  → s
	//   c.X         → analysis-internal helper or engine call
}
```

Any private helpers that `BuildRMDAnalysis` exclusively uses (e.g.,
`firstRMDYear` if private) move with it.

- [ ] **Step 2: Make `Calculator.BuildRMDAnalysis` a delegator.**

In `rmd.go`:
```go
func (c *Calculator) BuildRMDAnalysis(projection *models.ProjectionResult) *models.RMDAnalysis {
	return analysis.BuildRMD(projection, engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}
```

Add the imports if missing.

- [ ] **Step 3: Create `analysis/sustainability.go`.**

Move `Calculator.CalculateSustainabilityScore(projection)` from
`calculator.go:1790-1795` to `analysis/sustainability.go` as
`func Score(proj *models.ProjectionResult) *models.SustainabilityScore`.

In `calculator.go`, the method becomes:
```go
func (c *Calculator) CalculateSustainabilityScore(projection *models.ProjectionResult) *models.SustainabilityScore {
	return analysis.Score(projection)
}
```

- [ ] **Step 4: Create `analysis/explainability.go`.**

Move `Calculator.buildProjectionExplainability(projection)` from
`calculator.go:3048-3121` to `analysis/explainability.go` as
`func BuildExplainability(proj *models.ProjectionResult, in engine.Input) *models.ProjectionExplainability`.
Replace `c.Settings` with `in.Prepared.Settings()`.

In `calculator.go`:
```go
func (c *Calculator) buildProjectionExplainability(projection *models.ProjectionResult) *models.ProjectionExplainability {
	return analysis.BuildExplainability(projection, engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}
```

- [ ] **Step 5: Migrate RMD test files.**

For each of `rmd_test.go`, `rmd_birth_year_test.go`,
`rmd_calendar_year_test.go`, `rmd_start_age_projection_test.go`,
`rmd_tax_test.go`, `rmd_timing_test.go`, `calculator_rmd_gross_test.go`:

1. Move the file from `internal/services/retirement/` to
   `internal/services/retirement/analysis/`.
2. Change `package retirement` → `package analysis`.
3. Add imports: `engine`, `prepare`, parent `retirement` if needed for
   helpers like `FirstRMDCalendarYear` (still exported there).
4. Replace `c := newTestCalc(t, s); c.BuildRMDAnalysis(proj)` patterns
   with `analysis.BuildRMD(proj, engine.Input{Prepared: prepare.MustFrom(t, s)})`.
5. Tests that exercise `FirstRMDCalendarYear` (a free function in
   parent) keep `retirement.FirstRMDCalendarYear(s)` calls.

- [ ] **Step 6: Run the migrated analysis tests.**

Run: `go test ./internal/services/retirement/analysis/`
Expected: all migrated tests pass.

- [ ] **Step 7: Run the full retirement test suite (parent package).**

Run: `go test ./internal/services/retirement/`
Expected: pass. `Calculator.BuildRMDAnalysis` now routes through
`analysis.BuildRMD` and existing parent-package tests still see
identical output.

- [ ] **Step 8: Update `runFullForParity` in `parity_helpers_test.go`.**

Now that `analysis.BuildRMD` exists, replace the relevant block in
`runFullForParity`:

```go
// Before (Task 1):
out := tmp.RunFullAnalysis()
out.Projection = eng.Run(in)

// After (Task 2):
proj := eng.Run(in)
tmp := NewCalculatorWithChain(in.Prepared, in.Chain)
tmp.SetMonteCarloSeedForParity(mcSeed)
out := tmp.RunFullAnalysis()
out.Projection = proj
out.RMD = analysis.BuildRMD(proj, in)
out.Sustainability = analysis.Score(proj)
out.ProjectionExplainability = analysis.BuildExplainability(proj, in)
```

- [ ] **Step 9: Run the parity test.**

Run: `go test -run TestParity_FullAnalysis_AcrossFixtures ./internal/services/retirement/`
Expected: pass.

- [ ] **Step 10: Run all tests + vet.**

Run: `go test ./... && go vet ./...`
Expected: pass.

- [ ] **Step 11: Commit.**

```bash
git add internal/services/retirement/analysis/ \
        internal/services/retirement/calculator.go \
        internal/services/retirement/rmd.go \
        internal/services/retirement/parity_helpers_test.go
git rm internal/services/retirement/rmd_test.go \
       internal/services/retirement/rmd_birth_year_test.go \
       internal/services/retirement/rmd_calendar_year_test.go \
       internal/services/retirement/rmd_start_age_projection_test.go \
       internal/services/retirement/rmd_tax_test.go \
       internal/services/retirement/rmd_timing_test.go \
       internal/services/retirement/calculator_rmd_gross_test.go
git commit -m "$(cat <<'EOF'
refactor(retirement): extract pure post-projection analyses into analysis pkg

Extracts BuildRMDAnalysis, CalculateSustainabilityScore, and
buildProjectionExplainability into the new analysis package. Calculator
methods become one-line delegators. Tests for these analyses migrate
from retirement/ into analysis/.

The parity test (TestParity_FullAnalysis_AcrossFixtures) now compares
RMD, Sustainability, and Explainability directly via the new analysis
functions; the rest still goes through Calculator until later commits.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Settings-only analyses (BudgetFit, PresentValue)

**Files:**
- Create: `internal/services/retirement/analysis/budget_fit.go`
- Create: `internal/services/retirement/analysis/present_value.go`
- Move test file: `calculator_pv_test.go` from `retirement/` to `retirement/analysis/`
- Modify: `internal/services/retirement/calculator.go`
- Modify: `internal/services/retirement/parity_helpers_test.go`

- [ ] **Step 1: Create `analysis/budget_fit.go`.**

Move `Calculator.CalculateBudgetFit()` from `calculator.go:1389-1679` to
`analysis/budget_fit.go` as `func BudgetFit(in engine.Input) *models.BudgetFitAnalysis`.

Replace `c.Settings` with `in.Prepared.Settings()`. Any private helpers
exclusively used by `CalculateBudgetFit` move with it.

- [ ] **Step 2: Make `Calculator.CalculateBudgetFit` a delegator.**

```go
func (c *Calculator) CalculateBudgetFit() *models.BudgetFitAnalysis {
	return analysis.BudgetFit(engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}
```

- [ ] **Step 3: Create `analysis/present_value.go`.**

Move `Calculator.CalculatePresentValueAnalysis()` from
`calculator.go:1716-1789` to `analysis/present_value.go` as
`func PresentValue(in engine.Input) *models.PresentValueAnalysis`.
Replace `c.Settings` with `in.Prepared.Settings()`. Any
`c.calculateHealthcarePV` calls become
`engine.HealthcarePVForCalculator(s, ...)` (still exposed via parity
window export).

- [ ] **Step 4: Make `Calculator.CalculatePresentValueAnalysis` a delegator.**

```go
func (c *Calculator) CalculatePresentValueAnalysis() *models.PresentValueAnalysis {
	return analysis.PresentValue(engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}
```

- [ ] **Step 5: Migrate `calculator_pv_test.go`.**

Move file from `retirement/` to `retirement/analysis/`. Change package,
update calls:
- `c := newTestCalc(t, s); c.CalculatePresentValueAnalysis()` →
  `analysis.PresentValue(engine.Input{Prepared: prepare.MustFrom(t, s)})`.

- [ ] **Step 6: Run analysis tests.**

Run: `go test ./internal/services/retirement/analysis/`
Expected: pass.

- [ ] **Step 7: Update `runFullForParity` to use new analyses.**

Add in the appropriate spot:
```go
out.BudgetFit = analysis.BudgetFit(in)
out.PresentValue = analysis.PresentValue(in)
```

- [ ] **Step 8: Run parity + full tests.**

Run: `go test ./... && go vet ./...`
Expected: pass.

- [ ] **Step 9: Commit.**

```bash
git add internal/services/retirement/analysis/budget_fit.go \
        internal/services/retirement/analysis/present_value.go \
        internal/services/retirement/analysis/calculator_pv_test.go \
        internal/services/retirement/calculator.go \
        internal/services/retirement/parity_helpers_test.go
git rm internal/services/retirement/calculator_pv_test.go
git commit -m "$(cat <<'EOF'
refactor(retirement): extract settings-only analyses into analysis pkg

Extracts CalculateBudgetFit and CalculatePresentValueAnalysis into the
analysis package. Both take engine.Input and read only in.Prepared;
their Calculator methods become one-line delegators. The PV test file
moves alongside the function.

Parity test now verifies BudgetFit and PresentValue end-to-end.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Sensitivity, FailurePoints, MonteCarlo

**Files:**
- Create: `internal/services/retirement/analysis/sensitivity.go`
- Create: `internal/services/retirement/analysis/failure_points.go`
- Create: `internal/services/retirement/analysis/monte_carlo.go`
- Create: `internal/services/retirement/analysis/perturb.go`
- Migrate: `calculator_failure_test.go` (split: failure-point assertions to `analysis/failure_points_test.go`; remaining full-orchestration assertions stay in parent)
- Modify: `internal/services/retirement/calculator.go`
- Modify: `internal/services/retirement/parity_helpers_test.go`

- [ ] **Step 1: Create `analysis/perturb.go`.**

Move `perturbAndPrepare` from `calculator.go:49-55` to
`analysis/perturb.go` as a private package-level function:

```go
package analysis

import (
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// perturbAndPrepare deep-copies and re-prepares a perturbed configuration.
// Perturbations of an already-prepared snapshot only change scalar
// parameters (returns, inflation, expenses), so the result must always be
// valid; an error here indicates a bug.
func perturbAndPrepare(modified *models.WhatIfSettings) prepare.PreparedSettings {
	p, err := prepare.From(modified)
	if err != nil {
		panic(fmt.Sprintf("retirement/analysis: perturbation produced invalid settings: %v", err))
	}
	return p
}
```

In `calculator.go`, replace the body of `perturbAndPrepare` with a call
to a parity-window export `analysis.PerturbAndPrepareForCalculator` (so
existing internal Calculator callers still work). Add at the end of
`analysis/perturb.go`:
```go
// PerturbAndPrepareForCalculator is a parity-window export so Calculator
// can call into analysis. Removed in Task 8.
func PerturbAndPrepareForCalculator(modified *models.WhatIfSettings) prepare.PreparedSettings {
	return perturbAndPrepare(modified)
}
```

- [ ] **Step 2: Create `analysis/sensitivity.go`.**

Move `Calculator.CalculateSensitivity()` from `calculator.go:1796-1863`
to `analysis/sensitivity.go` as
`func Sensitivity(eng *engine.Engine, in engine.Input) []models.SensitivityResult`.

Replace per-perturbation pattern:
```go
// before
modified := /* perturb */
prepared := perturbAndPrepare(modified)
calc := NewCalculator(prepared)
projection := calc.RunProjection()

// after
modified := /* perturb */
prepared := perturbAndPrepare(modified)
projection := eng.Run(engine.Input{
	Prepared: prepared,
	Chain:    in.Chain,
})
```

- [ ] **Step 3: Make `Calculator.CalculateSensitivity` a delegator.**

```go
func (c *Calculator) CalculateSensitivity() []models.SensitivityResult {
	return analysis.Sensitivity(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}
```

- [ ] **Step 4: Create `analysis/failure_points.go`.**

Move `Calculator.CalculateFailurePoints()` and the four
`findXThreshold` helpers from `calculator.go:1864-2202` to
`analysis/failure_points.go`. Public surface:
```go
func FailurePoints(eng *engine.Engine, in engine.Input) *models.FailurePointAnalysis
```
Helpers (`findReturnThreshold`, `findInflationThreshold`,
`findExpensesThreshold`, `findPortfolioThreshold`) become private
package-level functions taking `(eng *engine.Engine, in engine.Input)`.

- [ ] **Step 5: Make `Calculator.CalculateFailurePoints` a delegator.**

```go
func (c *Calculator) CalculateFailurePoints() *models.FailurePointAnalysis {
	return analysis.FailurePoints(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}
```

Calculator's per-threshold helpers (`findReturnThreshold` etc.) become
delegators or are removed entirely if no other callers reference them.
Verify with: `grep -n "findReturnThreshold\|findInflationThreshold\|findExpensesThreshold\|findPortfolioThreshold" internal/services/retirement/*.go`. If only `CalculateFailurePoints` calls them, delete from `calculator.go` after moving.

- [ ] **Step 6: Create `analysis/monte_carlo.go`.**

Move `Calculator.RunMonteCarloSimulation(runs int)` plus its private
helpers (`runSingleMonteCarloSimulation`, `generateAssetReturns`,
`generateYearlyReturns`, `calculateSequenceRiskImpact`,
`calculateSequenceRiskBreakdown`, `createDistributionBuckets`) from
`calculator.go:2203-3047` to `analysis/monte_carlo.go`.

Public surface:
```go
func MonteCarlo(eng *engine.Engine, in engine.Input, runs int, seed int64) *models.MonteCarloAnalysis
```

Seed semantics: `seed == 0` means "auto-seed from time" (preserve
current behavior); any non-zero seed is used directly.

The internal projection runs become `eng.Run(engine.Input{Prepared:
perturbed, Chain: in.Chain})`.

- [ ] **Step 7: Make `Calculator.RunMonteCarloSimulation` a delegator.**

```go
func (c *Calculator) RunMonteCarloSimulation(runs int) *models.MonteCarloAnalysis {
	seed := int64(0)
	if c.mcSeedOverride.set {
		seed = c.mcSeedOverride.seed
	}
	return analysis.MonteCarlo(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	}, runs, seed)
}
```

This consolidates the seed override: from now on, both old and new
paths route through `analysis.MonteCarlo`, so the override is
effectively a single switch.

- [ ] **Step 8: Migrate `calculator_failure_test.go`.**

Inspect `calculator_failure_test.go`. Tests that assert
`CalculateFailurePoints` output move to `analysis/failure_points_test.go`
with calls rewritten to `analysis.FailurePoints(engine.New(), engine.Input{...})`.
Tests that assert end-to-end `WhatIfAnalysis` shape (rare in this file)
stay in parent and switch from `calc.RunFullAnalysis` to
`RunFull(engine.New(), engine.Input{...})` after Task 6 lands; for now
they continue calling `calc.RunFullAnalysis` (delegator).

- [ ] **Step 9: Update `runFullForParity`.**

```go
out.Sensitivity = analysis.Sensitivity(eng, in)
out.FailurePoints = analysis.FailurePoints(eng, in)
out.MonteCarlo = analysis.MonteCarlo(eng, in, MonteCarloRuns, mcSeed)
```

`MonteCarloRuns` is currently a magic 1000 inside Calculator. Hoist as
an unexported constant in `parity_helpers_test.go`:
```go
const parityMonteCarloRuns = 1000
```
Use that.

- [ ] **Step 10: Run parity + full tests + vet.**

Run: `go test ./... && go vet ./...`
Expected: pass.

If parity diff appears in MC fields, the seed override is not threading
correctly. Confirm: `Calculator.RunMonteCarloSimulation` reads
`c.mcSeedOverride` and passes it through.

- [ ] **Step 11: Commit.**

```bash
git add internal/services/retirement/analysis/sensitivity.go \
        internal/services/retirement/analysis/failure_points.go \
        internal/services/retirement/analysis/monte_carlo.go \
        internal/services/retirement/analysis/perturb.go \
        internal/services/retirement/analysis/failure_points_test.go \
        internal/services/retirement/calculator.go \
        internal/services/retirement/parity_helpers_test.go
git rm internal/services/retirement/calculator_failure_test.go
git commit -m "$(cat <<'EOF'
refactor(retirement): extract sensitivity, failure-points, MC into analysis pkg

Extracts CalculateSensitivity, CalculateFailurePoints, and
RunMonteCarloSimulation into the analysis package. Each takes
*engine.Engine + engine.Input. Monte Carlo's RNG seed becomes an
explicit parameter (0 = auto-seed from time, preserving current
behavior).

Calculator's parity-window MC seed override now flows through
analysis.MonteCarlo, so old and new paths share the same RNG when the
parity test sets a deterministic seed.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: SS, Backtest, and history package

**Files:**
- Create: `internal/services/retirement/analysis/ss.go`
- Create: `internal/services/retirement/analysis/backtest.go`
- Create: `internal/services/retirement/history/data.go`
- Create: `internal/services/retirement/history/data_test.go`
- Migrate: `social_security_test.go` → `analysis/ss_test.go`
- Migrate: `backtest_test.go` → `analysis/backtest_test.go`
- Modify: `internal/services/retirement/calculator.go`, `social_security.go`, `backtest.go`, `historical_data.go`
- Modify: `internal/services/retirement/parity_helpers_test.go`

### Part A — history package

- [ ] **Step 1: Create `history/data.go`.**

```go
// Package history exposes historical market data used by backtest
// analyses. It is a leaf package: nothing else in the retirement
// subsystem depends on it except analysis/ and the parent retirement
// package.
package history

import "budget2/internal/models"

// Data is a sequence of annual market data points.
type Data []models.HistoricalYear

// DefaultData returns the canonical 1928-2024 historical dataset.
func DefaultData() Data {
	return defaultData
}

// Sequence returns yearsNeeded years starting from startYear, wrapping
// (continuing from the beginning) if the request runs past the end of
// the dataset.
func Sequence(data Data, startYear, yearsNeeded int) Data {
	// body: copy of GetHistoricalSequence from historical_data.go:126,
	// generalized to accept any Data slice instead of HistoricalReturns.
}

// AvailableStartYears returns every starting year from which a sequence
// of projectionYears length is available without wrap-around.
func AvailableStartYears(data Data, projectionYears int) []int {
	// body: copy of GetAvailableStartYears from historical_data.go:144,
	// taking data explicitly.
}

// Stats computes aggregate statistics across the dataset.
func Stats(data Data) (avgStock, avgBond, avgCash, avgInflation, stockStdDev, bondStdDev float64) {
	// body: copy of GetHistoricalStats from historical_data.go:161,
	// taking data explicitly.
}

// defaultData is the canonical historical sequence.
var defaultData = Data{
	// copy verbatim from HistoricalReturns in historical_data.go:18-119
}
```

The implementing engineer copies the body of
`historical_data.go:18-195` and adapts the function signatures to take
`data Data` rather than reading from package-level
`HistoricalReturns`.

- [ ] **Step 2: Add a thin compatibility wrapper in `historical_data.go`.**

Keep the parent-package functions as one-line wrappers so existing
callers compile during the migration:

```go
func GetHistoricalReturns() []HistoricalYear {
	return []HistoricalYear(history.DefaultData())
}

func GetHistoricalSequence(startYear, yearsNeeded int) []HistoricalYear {
	return []HistoricalYear(history.Sequence(history.DefaultData(), startYear, yearsNeeded))
}

func GetAvailableStartYears(projectionYears int) []int {
	return history.AvailableStartYears(history.DefaultData(), projectionYears)
}

func GetHistoricalStats() (avgStock, avgBond, avgCash, avgInflation, stockStdDev, bondStdDev float64) {
	return history.Stats(history.DefaultData())
}
```

The `HistoricalReturns` global remains for now (still referenced by
existing tests). It is removed in Task 8.

- [ ] **Step 3: Move `historical_data_test.go` (if any) or add a smoke test.**

If `historical_data_test.go` does not exist, create a minimal smoke
test in `history/data_test.go`:
```go
package history

import "testing"

func TestDefaultData_NotEmpty(t *testing.T) {
	if len(DefaultData()) == 0 {
		t.Fatal("DefaultData returned empty")
	}
}

func TestSequence_RoundTrip(t *testing.T) {
	d := DefaultData()
	got := Sequence(d, 1990, 5)
	if len(got) != 5 {
		t.Fatalf("Sequence length = %d, want 5", len(got))
	}
}
```

- [ ] **Step 4: Build + test the history package.**

Run: `go test ./internal/services/retirement/history/`
Expected: pass.

### Part B — SS analysis

- [ ] **Step 5: Create `analysis/ss.go`.**

Move `Calculator.RunSSAnalysis()`, `Calculator.RunSSPortfolioAnalysis(ssAnalysis)`,
`Calculator.buildSSPortfolioOptions(...)`, `Calculator.runSSPortfolioCellMC(...)`,
and `Calculator.cloneSettingsWithClaimAges(...)` from
`social_security.go:421-690` to `analysis/ss.go`.

Public surface:
```go
func SSAnalysis(eng *engine.Engine, in engine.Input) *models.SSComparisonAnalysis
func SSPortfolio(eng *engine.Engine, in engine.Input, ss *models.SSComparisonAnalysis) *models.SSPortfolioAnalysis
```

Private helpers (`buildSSPortfolioOptions`, `runSSPortfolioCellMC`,
`cloneSettingsWithClaimAges`) become package-level functions in
`analysis/ss.go` taking `(eng *engine.Engine, in engine.Input)` where
they used `c.*` previously.

- [ ] **Step 6: Make Calculator's SS methods delegators.**

```go
func (c *Calculator) RunSSAnalysis() *models.SSComparisonAnalysis {
	return analysis.SSAnalysis(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}

func (c *Calculator) RunSSPortfolioAnalysis(ss *models.SSComparisonAnalysis) *models.SSPortfolioAnalysis {
	return analysis.SSPortfolio(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	}, ss)
}
```

- [ ] **Step 7: Split and migrate `social_security_test.go`.**

`social_security_test.go` mixes two concerns: tests for the
`SSPortfolioEligible` predicate (which stays in parent
`retirement/`) and tests for `RunSSAnalysis` /
`RunSSPortfolioAnalysis` (which move to `analysis/`).

Concretely:

1. **Identify the eligibility tests.** Run:
   ```bash
   grep -n "^func Test" internal/services/retirement/social_security_test.go
   ```
   Tests whose body calls `SSPortfolioEligible(...)` directly (today's
   `TestSSPortfolioEligible` at line 556 is the canonical case) stay
   in parent.

2. **Create `analysis/ss_test.go`.** Copy every test that is NOT an
   eligibility test from `social_security_test.go`. Change the package
   to `analysis`, add imports (`engine`, `prepare`, parent
   `retirement` for free functions like `FirstRMDCalendarYear`), and
   rewrite call sites:
   - `c := newTestCalc(t, s); c.RunSSAnalysis()` →
     `analysis.SSAnalysis(engine.New(), engine.Input{Prepared: prepare.MustFrom(t, s)})`
   - `c.RunSSPortfolioAnalysis(ss)` →
     `analysis.SSPortfolio(engine.New(), engine.Input{Prepared: prepare.MustFrom(t, s)}, ss)`

3. **Trim `retirement/social_security_test.go`.** Delete every test
   that was copied to `analysis/ss_test.go`; leave only the
   eligibility tests behind. Verify it still compiles in the
   retirement package.

4. **Verify the split.** Run:
   ```bash
   go test ./internal/services/retirement/ -run TestSSPortfolioEligible
   go test ./internal/services/retirement/analysis/
   ```
   Expected: both pass.

### Part C — Backtest

- [ ] **Step 8: Create `analysis/backtest.go`.**

Move `Calculator.RunHistoricalBacktest()` and
`Calculator.runSingleHistoricalSequence(startYear int) HistoricalSequenceResult`
from `backtest.go:35-417` to `analysis/backtest.go`.

Public surface:
```go
func HistoricalBacktest(eng *engine.Engine, in engine.Input, data history.Data) *models.HistoricalBacktestAnalysis
```

Replace internal calls to `GetHistoricalReturns()`,
`GetHistoricalSequence(startYear, yearsNeeded)`,
`GetAvailableStartYears(projectionYears)`, `GetHistoricalStats()` with
`data` / `history.Sequence(data, ...)` / `history.AvailableStartYears(data, ...)` /
`history.Stats(data)` correspondingly.

Internal projection runs use `eng.Run(engine.Input{Prepared: perturbed, Chain: in.Chain})`.

- [ ] **Step 9: Make `Calculator.RunHistoricalBacktest` a delegator.**

```go
func (c *Calculator) RunHistoricalBacktest() *models.HistoricalBacktestAnalysis {
	return analysis.HistoricalBacktest(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	}, history.DefaultData())
}
```

`HistoricalSequenceResult` (currently in `backtest.go`) moves to
`analysis/backtest.go` as a private struct. If exported callers exist,
expose via parity-window forwarder.

- [ ] **Step 10: Migrate `backtest_test.go`.**

Move from `retirement/` to `retirement/analysis/`. Update package and
calls.

### Part D — parity test update

- [ ] **Step 11: Update `runFullForParity`.**

```go
out.SocialSecurity = analysis.SSAnalysis(eng, in)
if out.SocialSecurity != nil && SSPortfolioEligible(in.Prepared.Settings()) {
	out.SocialSecurity.Portfolio = analysis.SSPortfolio(eng, in, out.SocialSecurity)
}
out.HistoricalBacktest = analysis.HistoricalBacktest(eng, in, history.DefaultData())
```

- [ ] **Step 12: Run parity + full tests.**

Run: `go test ./... && go vet ./...`
Expected: pass.

- [ ] **Step 13: Commit.**

```bash
git add internal/services/retirement/analysis/ss.go \
        internal/services/retirement/analysis/backtest.go \
        internal/services/retirement/analysis/ss_test.go \
        internal/services/retirement/analysis/backtest_test.go \
        internal/services/retirement/history/ \
        internal/services/retirement/calculator.go \
        internal/services/retirement/social_security.go \
        internal/services/retirement/backtest.go \
        internal/services/retirement/historical_data.go \
        internal/services/retirement/social_security_test.go \
        internal/services/retirement/parity_helpers_test.go
git rm internal/services/retirement/backtest_test.go
git commit -m "$(cat <<'EOF'
refactor(retirement): extract SS, backtest, and history package

Extracts RunSSAnalysis, RunSSPortfolioAnalysis, and
RunHistoricalBacktest into the analysis package. Calculator methods
become one-line delegators.

Historical market data moves to a new leaf package
internal/services/retirement/history (Data, DefaultData, Sequence,
AvailableStartYears, Stats). The parent package's GetHistoricalReturns
et al. remain as thin wrappers during migration; they go away with
Calculator in the cleanup commit.

Test files for SS and backtest migrate to analysis/. The
SSPortfolioEligible sub-tests stay in parent because the predicate
itself stays there.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Orchestrator + eligibility

**Files:**
- Create: `internal/services/retirement/orchestrator.go`
- Create: `internal/services/retirement/eligibility.go`
- Modify: `internal/services/retirement/calculator.go`, `social_security.go`, `rmd.go`
- Modify: `internal/services/retirement/parity_helpers_test.go`

- [ ] **Step 1: Create `eligibility.go`.**

Move `SSPortfolioEligible` from `social_security.go:65-71` to
`eligibility.go`. Move `FirstRMDCalendarYear` from `rmd.go:94-100` to
`eligibility.go`. Both stay in `package retirement`.

```go
package retirement

import (
	"budget2/internal/models"
)

// SSPortfolioEligible returns true when the settings indicate Social
// Security portfolio analysis should be computed.
func SSPortfolioEligible(s *models.WhatIfSettings) bool {
	// body verbatim from social_security.go:65-71
}

// FirstRMDCalendarYear returns the first calendar year in which the
// older person must take an RMD.
func FirstRMDCalendarYear(s *models.WhatIfSettings) int {
	// body verbatim from rmd.go:94-100
}
```

- [ ] **Step 2: Remove the originals from social_security.go and rmd.go.**

Delete the now-duplicated functions; update internal callers if they
referenced the receivers (they referenced free functions, so no
change needed).

- [ ] **Step 3: Create `orchestrator.go`.**

`RunFull` is the public entry point; `runFullWithSeed` is an unexported
variant that threads an explicit MC seed (used by Calculator's
parity-window delegator). Both share a single body so there is no
duplication to maintain. In Task 8, `runFullWithSeed` is inlined back
into `RunFull` and deleted.

```go
package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
)

// MonteCarloRuns is the default number of Monte Carlo iterations.
const MonteCarloRuns = 1000

// MonteCarloSeed is the default RNG seed for orchestrator-driven runs.
// 0 = auto-seed from time, matching Calculator.RunFullAnalysis behavior.
const MonteCarloSeed int64 = 0

// RunFull executes the full what-if analysis fan-out for in. Returns a
// fully populated *models.WhatIfAnalysis. Replaces
// Calculator.RunFullAnalysis.
func RunFull(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
	return runFullWithSeed(eng, in, MonteCarloSeed)
}

// runFullWithSeed is RunFull with an explicit MC seed. The
// parity-window Calculator delegator uses this to thread its seed
// override through. Inlined back into RunFull and deleted in Task 8.
func runFullWithSeed(eng *engine.Engine, in engine.Input, mcSeed int64) *models.WhatIfAnalysis {
	proj := eng.Run(in)

	explainability := analysis.BuildExplainability(proj, in)
	budgetFit := analysis.BudgetFit(in)
	presentValue := analysis.PresentValue(in)
	sustainability := analysis.Score(proj)
	sensitivity := analysis.Sensitivity(eng, in)
	failurePoints := analysis.FailurePoints(eng, in)
	monteCarlo := analysis.MonteCarlo(eng, in, MonteCarloRuns, mcSeed)
	rmd := analysis.BuildRMD(proj, in)
	backtest := analysis.HistoricalBacktest(eng, in, history.DefaultData())

	if backtest != nil && monteCarlo != nil && monteCarlo.Stats != nil {
		backtest.MonteCarloSuccessRate = monteCarlo.Stats.SuccessRate
		backtest.HistoricalVsMC = backtest.SuccessRate - monteCarlo.Stats.SuccessRate
	}

	var ssAnalysis *models.SSComparisonAnalysis
	settings := in.Prepared.Settings()
	if settings.SocialSecurity != nil && settings.SocialSecurity.FRABenefit > 0 {
		ssAnalysis = analysis.SSAnalysis(eng, in)
		if ssAnalysis != nil && SSPortfolioEligible(settings) {
			ssAnalysis.Portfolio = analysis.SSPortfolio(eng, in, ssAnalysis)
		}
	}

	return &models.WhatIfAnalysis{
		Settings:                 settings,
		Projection:               proj,
		ProjectionExplainability: explainability,
		BudgetFit:                budgetFit,
		PresentValue:             presentValue,
		Sustainability:           sustainability,
		Sensitivity:              sensitivity,
		FailurePoints:            failurePoints,
		MonteCarlo:               monteCarlo,
		RMD:                      rmd,
		HistoricalBacktest:       backtest,
		SocialSecurity:           ssAnalysis,
	}
}
```

- [ ] **Step 4: Make `Calculator.RunFullAnalysis` a delegator.**

In `calculator.go`, replace the body of
`RunFullAnalysis` (calculator.go:3122-3162) with:

```go
func (c *Calculator) RunFullAnalysis() *models.WhatIfAnalysis {
	seed := MonteCarloSeed
	if c.mcSeedOverride.set {
		seed = c.mcSeedOverride.seed
	}
	return runFullWithSeed(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	}, seed)
}
```

- [ ] **Step 5: Replace `runFullForParity` with a call to `RunFull`.**

In `parity_helpers_test.go`:
```go
func runFullForParity(eng *engine.Engine, in engine.Input, mcSeed int64) *models.WhatIfAnalysis {
	return runFullWithSeed(eng, in, mcSeed)
}
```

The helper's body is now a single call. Keep the indirection in case
fixture-level overrides are needed.

- [ ] **Step 6: Run parity + full tests + vet.**

Run: `go test ./... && go vet ./...`
Expected: pass. Both old (`Calculator.RunFullAnalysis`) and new
(`RunFull`) paths now route through the same orchestrator code.

- [ ] **Step 7: Commit.**

```bash
git add internal/services/retirement/orchestrator.go \
        internal/services/retirement/eligibility.go \
        internal/services/retirement/calculator.go \
        internal/services/retirement/social_security.go \
        internal/services/retirement/rmd.go \
        internal/services/retirement/parity_helpers_test.go
git commit -m "$(cat <<'EOF'
feat(retirement): add RunFull orchestrator + eligibility helpers

Adds retirement.RunFull(eng, in) orchestrator: a free function that
fans out across the analysis package and assembles a *WhatIfAnalysis
identical to Calculator.RunFullAnalysis. Calculator becomes a
one-line delegator routed through runFullWithSeed (a parity-window
variant carrying the MC seed override).

Consolidates SSPortfolioEligible and FirstRMDCalendarYear into a new
eligibility.go file in the parent package.

Parity test now compares Calculator.RunFullAnalysis vs
runFullWithSeed end-to-end.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Migrate handler call sites

**Files:**
- Modify: `internal/handlers/whatif/handlers.go`
- Modify: `internal/handlers/whatif/handlers_rates.go`
- Modify: `internal/handlers/whatif/handlers_test.go`

- [ ] **Step 1: Add `getEngine` helper at the top of `handlers.go`.**

After the existing imports and package-level vars, insert:
```go
var sharedEngine = engine.New()

func getEngine() *engine.Engine { return sharedEngine }
```

Add the import: `"budget2/internal/services/retirement/engine"`.

- [ ] **Step 2: Rewrite `buildCalculator` as `buildEngineInput`.**

Replace `handlers.go:80-117` (`func buildCalculator(...)`) with:

```go
// buildEngineInput resolves the prepared settings (and chain, if any)
// for the given top-level WhatIfSettings, and returns the engine.Input
// that the orchestrator and engine consume.
func buildEngineInput(settings *models.WhatIfSettings) (engine.Input, string, error) {
	prepared, err := prepare.From(settings)
	if err != nil {
		// existing error mapping unchanged — see ScenarioValidationError
		// branches at handlers.go:43-78
		return engine.Input{}, "", err
	}

	if len(settings.ScenarioChain) == 0 {
		return engine.Input{Prepared: prepared}, computeSettingsHash(settings), nil
	}

	chain := make([]engine.PreparedChainLink, 0, len(settings.ScenarioChain))
	// existing chain-resolution loop body unchanged (handlers.go:92-116);
	// only the slice element type changes.
	// ...
	return engine.Input{Prepared: prepared, Chain: chain}, combinedHash, nil
}
```

- [ ] **Step 3: Update `runAnalysisWithCache` body.**

At `handlers.go:131-135`, replace:
```go
calc, hashData, err := buildCalculator(settings)
if err != nil {
	return nil, err
}
analysis := calc.RunFullAnalysis()
```
with:
```go
in, hashData, err := buildEngineInput(settings)
if err != nil {
	return nil, err
}
analysis := retirement.RunFull(getEngine(), in)
```

- [ ] **Step 4: Update the no-guardrails chart bypass.**

At `handlers.go:670-685`, replace the Calculator construction with:
```go
clone := *settings
clone.Guardrails = nil
prepared, err := prepare.From(&clone)
if err != nil {
	// existing error path
}
projection := getEngine().Run(engine.Input{Prepared: prepared})
```

- [ ] **Step 5: Update `handlers_rates.go:93`.**

Replace `analysis := calc.RunFullAnalysis()` with:
```go
analysis := retirement.RunFull(getEngine(), in)
```

Also rename the upstream `buildCalculator` call to `buildEngineInput`
and update the receiving variable name.

- [ ] **Step 6: Update handler tests at lines 2785, 2814, 2834.**

The three existing `buildCalculator` unit tests assert hash behavior,
error mapping, and that the constructor returns a non-nil Calculator.
Rewrite them to exercise `buildEngineInput`:

- The hash assertions stay identical (same hash inputs).
- The error-mapping assertions stay identical.
- The "non-nil Calculator" assertion becomes "Prepared is populated and
  Chain length matches the input ScenarioChain length".

Example pattern (adapt to the existing test names):
```go
func TestBuildEngineInput_NoChain(t *testing.T) {
	s := defaultSettings()
	in, hash, err := buildEngineInput(s)
	if err != nil {
		t.Fatalf("buildEngineInput: %v", err)
	}
	if hash == "" {
		t.Fatal("hash is empty")
	}
	if len(in.Chain) != 0 {
		t.Errorf("Chain len = %d, want 0", len(in.Chain))
	}
	// Prepared is a value type; check it's not the zero value
	if in.Prepared.Settings() == nil {
		t.Error("Prepared.Settings() is nil")
	}
}
```

- [ ] **Step 7: Update the test fixture at `handlers_test.go:3059`.**

Original:
```go
calc := retirement.NewCalculator(prepare.MustFrom(t, s))
// ... follow-on calls to calc.X(...)
```

Rewrite to:
```go
eng := engine.New()
in := engine.Input{Prepared: prepare.MustFrom(t, s)}
// follow-on calls become eng.Run(in) or analysis.X(...) as appropriate
```

Inspect the two follow-on lines and replace each:
- `calc.RunProjection()` → `eng.Run(in)`
- `calc.RunFullAnalysis()` → `retirement.RunFull(eng, in)`
- `calc.BuildRMDAnalysis(proj)` → `analysis.BuildRMD(proj, in)`
- etc.

Add the imports the test file needs: `engine`, `analysis`.

- [ ] **Step 8: Run handler tests.**

Run: `go test ./internal/handlers/whatif/`
Expected: pass.

- [ ] **Step 9: Run full tests + vet.**

Run: `go test ./... && go vet ./...`
Expected: pass.

- [ ] **Step 10: Commit.**

```bash
git add internal/handlers/whatif/
git commit -m "$(cat <<'EOF'
refactor(handlers): migrate whatif handlers to engine + RunFull

Replaces buildCalculator with buildEngineInput, swaps the three
Calculator call sites (runAnalysisWithCache, no-guardrails chart
bypass, handlers_rates) for engine.Run / retirement.RunFull, and
updates the three buildCalculator unit tests + the single
NewCalculator test fixture in handlers_test.go.

Calculator still exists for backwards compat — the parity test
continues to use it to compare against RunFull until the cleanup
commit.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Delete Calculator and parity scaffolding

**Files:**
- Delete: `internal/services/retirement/calculator.go`
- Delete: `internal/services/retirement/rmd.go`
- Delete: `internal/services/retirement/social_security.go`
- Delete: `internal/services/retirement/backtest.go`
- Delete: `internal/services/retirement/historical_data.go`
- Delete: `internal/services/retirement/parity_test.go`
- Delete: `internal/services/retirement/parity_helpers_test.go`
- Delete: `internal/services/retirement/parity_fixtures_test.go`
- Modify: `internal/services/retirement/chain.go` (use `engine.PreparedChainLink` directly; remove the alias)
- Modify: `internal/services/retirement/orchestrator.go` (remove `runFullWithSeed`)
- Modify: `internal/services/retirement/engine/healthcare.go`, `engine/income.go`, `engine/expense.go` (remove `*ForCalculator` parity-window exports)
- Modify: `internal/services/retirement/analysis/perturb.go` (remove `PerturbAndPrepareForCalculator`)

- [ ] **Step 1: Verify no remaining Calculator callers.**

Run: `grep -rn "retirement\.NewCalculator\|retirement\.Calculator\b\|retirement\.NewCalculatorWithChain" --include="*.go" .`
Expected: no matches outside the parity test files (which we're about
to delete).

If any other matches exist, this task is premature — go fix them
under Task 7's scope.

- [ ] **Step 2: Delete `calculator.go`, `rmd.go`, `social_security.go`, `backtest.go`.**

Run:
```bash
git rm internal/services/retirement/calculator.go \
       internal/services/retirement/rmd.go \
       internal/services/retirement/social_security.go \
       internal/services/retirement/backtest.go
```

- [ ] **Step 3: Delete parity scaffolding.**

Run:
```bash
git rm internal/services/retirement/parity_test.go \
       internal/services/retirement/parity_helpers_test.go \
       internal/services/retirement/parity_fixtures_test.go
```

- [ ] **Step 4: Delete `historical_data.go` and confirm history is the only source.**

Run: `grep -rn "GetHistoricalReturns\|GetHistoricalSequence\|GetAvailableStartYears\|GetHistoricalStats\|HistoricalReturns\b\|HistoricalYear" --include="*.go" .`
Expected: matches only in `internal/services/retirement/history/` and in
test files that have already migrated to use `history.*`.

If callers outside `history/` remain, update them to use `history.*`
first.

Then run: `git rm internal/services/retirement/historical_data.go`

- [ ] **Step 5: Strip parity-window exports from `engine` and `analysis`.**

In `engine/healthcare.go`, delete `HealthcarePVForCalculator`.
In `engine/income.go`, delete `TotalIncomeForCalculator`.
In `engine/expense.go`, delete `TotalExpensesForCalculator` and
`ExpenseBreakdownForCalculator`.
In `analysis/perturb.go`, delete `PerturbAndPrepareForCalculator`.

- [ ] **Step 6: Remove `runFullWithSeed` from `orchestrator.go`.**

Replace `RunFull`'s call to `runFullWithSeed` (if RunFull had been
delegating) with direct calls. Today RunFull already does direct calls
(see Task 6 step 3); only delete the helper. Search:
```bash
grep -n "runFullWithSeed" internal/services/retirement/
```
Expected: only orchestrator.go references. Delete the function.

- [ ] **Step 7: Replace the `PreparedChainLink` alias in `chain.go`.**

If `chain.go` still has `type PreparedChainLink = engine.PreparedChainLink`
(introduced in Task 1), delete it. Update any local variable
declarations to use `engine.PreparedChainLink` directly.

- [ ] **Step 8: Build.**

Run: `go build ./...`
Expected: success. If anything still references `retirement.Calculator`,
the build fails at the call site.

- [ ] **Step 9: Run full tests + vet.**

Run: `go test ./... && go vet ./...`
Expected: pass. The parity test is gone; the engine and analysis tests
plus the migrated parent-package tests stand alone.

- [ ] **Step 10: Verify expected absence.**

Run: `grep -rn "retirement\.NewCalculator\|retirement\.Calculator\b" --include="*.go" .`
Expected: no matches.

Run: `ls internal/services/retirement/calculator.go internal/services/retirement/rmd.go internal/services/retirement/social_security.go internal/services/retirement/backtest.go internal/services/retirement/historical_data.go 2>&1`
Expected: all "No such file or directory".

- [ ] **Step 11: Commit.**

```bash
git add -A internal/services/retirement/ internal/handlers/whatif/
git commit -m "$(cat <<'EOF'
refactor(retirement): delete Calculator and parity scaffolding

Final cleanup of the projection-engine refactor. Removes:
- Calculator type and all its methods (calculator.go, rmd.go,
  social_security.go, backtest.go).
- Historical data globals (historical_data.go); history package is
  the sole source.
- Parity test, helpers, and fixtures (parity_test.go,
  parity_helpers_test.go, parity_fixtures_test.go).
- All *ForCalculator parity-window export shims in engine/.
- PerturbAndPrepareForCalculator in analysis/.
- runFullWithSeed orchestrator helper.
- PreparedChainLink alias in retirement/chain.go (callers use
  engine.PreparedChainLink directly).

End state: complexity concentrates in engine + analysis + a small
RunFull orchestrator. The Calculator type that hosted ten analyses on
3,162 LOC is gone.

Tracker:
- Update docs/superpowers/specs/2026-05-08-architecture-deepening.md
  Candidate #1 status to Landed with this commit's SHA.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 12: Update the architecture-deepening tracker.**

Edit `docs/superpowers/specs/2026-05-08-architecture-deepening.md`
Candidate #1 entry:
```markdown
**Status:** Landed on `feat/projection-engine` with commits
<SHAs from Tasks 1-8>. Final cleanup commit: <SHA from Task 8 step 11>.
Plan: `docs/superpowers/plans/2026-05-08-projection-engine.md`.
```

Commit:
```bash
git add docs/superpowers/specs/2026-05-08-architecture-deepening.md
git commit -m "$(cat <<'EOF'
docs(architecture-deepening): mark candidate #1 as landed

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Final verification

- [ ] `git log --oneline dev..HEAD` shows roughly 9 commits (8 refactor + 1 tracker update).
- [ ] `go build ./...` clean.
- [ ] `go test ./...` clean.
- [ ] `go vet ./...` clean.
- [ ] `wc -l internal/services/retirement/*.go internal/services/retirement/engine/*.go internal/services/retirement/analysis/*.go internal/services/retirement/history/*.go` shows roughly 4,000 LOC total (down from ~5,500 today).
- [ ] No `retirement.Calculator` or `retirement.NewCalculator` reference anywhere.
- [ ] Tracker entry for Candidate #1 says **Landed**.

PR title: `Candidate #1: Calculator-as-orchestrator → Projection Engine + Analyses`.
PR body: link to spec and plan; one paragraph summarizing the eight commits.
