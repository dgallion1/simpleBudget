# Candidate #1 — Calculator-as-orchestrator → Projection Engine + Analyses

**Created:** 2026-05-08
**Status:** Spec — reviewed and updated
**Tracker:** [`docs/superpowers/specs/2026-05-08-architecture-deepening.md`](2026-05-08-architecture-deepening.md) Candidate #1
**Branch (proposed):** `feat/projection-engine` (off `dev`)
**Predecessor:** PreparedSettings (Candidate #2, landed)
**Successor:** Engine-level reuse (Candidate #3), AnalysisService (Candidate #4)

---

## Summary

`internal/services/retirement/calculator.go` (3,162 LOC) hosts ten distinct
analyses as receiver methods on a single `Calculator` struct that holds three
fields. `RunFullAnalysis` (calculator.go:3122) is a 40-line fan-out. Tests can
only reach analyses through the full Calculator surface, which is one cause of
the 8.3k-line `handlers_test.go`.

This change extracts a **stateless `Engine`** that owns the projection loop,
moves each analysis to a dedicated **`analysis`** package as a free function
with the smallest input it actually needs, and replaces `Calculator` with a
top-level `RunFull(eng, in)` orchestrator. The `Calculator` type is deleted.

End state: complexity concentrates into a deep `engine` module behind a small
interface; analyses become independently callable, independently testable
units; handlers depend on three small things (`engine.New()`,
`engine.Input{...}`, `retirement.RunFull(...)`) instead of one fat type.

## Goals

1. Extract `Engine.Run(Input) *ProjectionResult` as a deep, stateless module.
2. Decompose each of the 10 analyses into a free function in a new `analysis`
   package, taking the minimum input it needs (`*ProjectionResult`,
   `engine.Input`, or `*engine.Engine` + `engine.Input`).
3. Replace `Calculator.RunFullAnalysis` with `retirement.RunFull(eng, in)`.
4. Delete the `Calculator` type and its constructors.
5. Migrate the production handler call sites plus the handler test fixtures
   that directly exercise `buildCalculator` / `NewCalculator`.
6. Land it in one branch (`feat/projection-engine`) as ~8 reviewable commits,
   each compiling and passing `go test ./...`.

## Non-goals

- Engine-level caching or parallel projection runs (Candidate #3).
- Promotion of `runAnalysisWithCache` to an `AnalysisService` package
  (Candidate #4).
- Changes to `prepare/PreparedSettings` (already landed, Candidate #2).
- Changes to `WhatIfAnalysis` shape, JSON output, templates, or view
  handlers beyond the calculator-to-engine call-site migration listed below.
- Adding new analyses, removing existing analyses, or changing any analysis's
  numeric output. Parity is byte-equal for ints/bools and within `1e-9` for
  floats.
- Performance optimizations of any kind.

---

## Architecture — package layout

```
internal/services/retirement/
├── prepare/                        (unchanged — landed in Candidate #2)
│   └── PreparedSettings, From, MustFrom, NormalizePhaseAgeReference, ComputeAges
│
├── engine/                         (NEW)
│   ├── engine.go                   — Engine, New, Run
│   ├── input.go                    — Input, PreparedChainLink
│   ├── month.go                    — internal monthly state machine
│   ├── income.go                   — internal: total-income computation
│   ├── expense.go                  — internal: total-expense + breakdown
│   └── healthcare.go               — internal: healthcare PV/Medicare transition
│
├── analysis/                       (NEW)
│   ├── rmd.go                      — BuildRMD(proj, in) *RMDAnalysis
│   ├── sustainability.go           — Score(proj) *SustainabilityScore
│   ├── explainability.go           — BuildExplainability(proj, in) *ProjectionExplainability
│   ├── budget_fit.go               — BudgetFit(in) *BudgetFitAnalysis
│   ├── present_value.go            — PresentValue(in) *PresentValueAnalysis
│   ├── sensitivity.go              — Sensitivity(eng, in) []SensitivityResult
│   ├── failure_points.go           — FailurePoints(eng, in) *FailurePointAnalysis
│   ├── monte_carlo.go              — MonteCarlo(eng, in, runs, seed) *MonteCarloAnalysis
│   ├── ss.go                       — SSAnalysis, SSPortfolio
│   ├── backtest.go                 — HistoricalBacktest(eng, in, data) *HistoricalBacktestAnalysis
│   ├── perturb.go                  — internal: deep-copy-and-mutate helpers shared by Cat-C
│   └── *_test.go                   — siblings, migrated from retirement/calculator_*_test.go
│
├── history/                        (NEW)
│   └── data.go                     — HistoricalYear alias/data helpers, DefaultData, Sequence, Stats
│
├── orchestrator.go                 — package retirement; RunFull(eng, in) *WhatIfAnalysis
├── eligibility.go                  — package retirement; SSPortfolioEligible, FirstRMDCalendarYear, etc.
│
├── chain.go                        (modified — exports []engine.PreparedChainLink)
├── settings.go                     (unchanged)
├── tax.go                          (unchanged)
├── historical_data.go              (deleted or reduced to compatibility wrappers during migration)
├── guardrails.go                   (unchanged)
│
└── (DELETED in commit 8: calculator.go, rmd.go, social_security.go, backtest.go)
```

### Why these boundaries

- **`engine` owns projection-loop math.** Internal helpers
  (`CalculateTotalIncome`, `CalculateTotalExpenses`, `CalculateExpenseBreakdown`,
  `calculateHealthcarePV`, `findSteadyStateMonth`, etc.) become unexported
  engine internals. No external caller invokes them today.
- **`analysis` owns per-analysis logic.** One file per analysis. Tests live
  beside their code. Shared helpers (`perturbAndPrepare`) live in
  `analysis/perturb.go`.
- **`orchestrator.go` is a single function in the parent package.** A whole
  package for one symbol is overkill. Future Candidate #4 will introduce an
  `AnalysisService` that absorbs or wraps it.
- **`tax.go`, `settings.go`, `guardrails.go`, `chain.go` stay put.** They are
  pure helpers / data plumbing already shaped right.

### Import direction (no cycles)

```
handlers/whatif → retirement → analysis → engine → prepare
                            ↘ engine ↗
                            ↘ history ↗
```

`engine` depends only on `prepare` and `models`. `analysis` depends on
`engine`, `prepare`, `models`, and `history`. The parent `retirement` package
may import `engine`, `analysis`, and `history`; the reverse is forbidden.
`chain.go` produces `[]engine.PreparedChainLink` (canonical type lives in
`engine`). Historical market data moves out of the parent package because
`analysis/backtest.go` cannot import `retirement` while `retirement` imports
`analysis`.

`history.Data` is intentionally small:

```go
package history

import "budget2/internal/models"

type Data []models.HistoricalYear

func DefaultData() Data { /* copy of built-in 1928-2024 data */ }
func Sequence(data Data, startYear, yearsNeeded int) Data
func AvailableStartYears(data Data, projectionYears int) []int
func Stats(data Data) (avgStock, avgBond, avgCash, avgInflation, stockStdDev, bondStdDev float64)
```

During migration, parent-package wrappers such as `GetHistoricalReturns` may
delegate to `history.DefaultData()` so existing tests compile. By commit 8,
callers should use `history` directly or go through `analysis.HistoricalBacktest`.

---

## Engine

```go
package engine

import (
    "budget2/internal/models"
    "budget2/internal/services/retirement/prepare"
)

type Input struct {
    Prepared prepare.PreparedSettings
    Chain    []PreparedChainLink
}

type PreparedChainLink struct {
    ScenarioFilename string
    TransitionAge    int
    Settings         prepare.PreparedSettings
}

type Engine struct{}

func New() *Engine { return &Engine{} }

// Run produces a deterministic monthly projection for in.
// Returns a fully populated *models.ProjectionResult. Never returns nil.
// Run is a pure function of in: same Input, same *ProjectionResult.
func (e *Engine) Run(in Input) *models.ProjectionResult { /* … */ }
```

### Determinism contract

`Engine.Run(in)` is a pure function of `in`. No package-level state, no
clocks, no RNG inside the engine. Monte Carlo's RNG is owned by
`analysis.MonteCarlo`, which seeds and re-runs the engine with perturbed
inputs.

### Why a struct, not free functions

The empty-struct shape is deliberate:
1. Future Candidate #3 (engine-level caching, parallel runs, tracing) has a
   place to land without changing call signatures.
2. Tests can wrap with a `tracingEngine` that records every Run call.
3. Zero-field struct = zero overhead today.

### Why `Input` is a struct

Adding fields later (`RNGSeed`, `MaxYears`, `BreakOn` for failure-point binary
search) does not ripple to call sites. Reads naturally:
`eng.Run(engine.Input{Prepared: p, Chain: chain})`.

---

## Analysis function signatures

Three categories, distinguished by what each analysis actually needs.

### Category A — Pure post-projection summarizers

Take `*ProjectionResult` (and `engine.Input` for tax tables / age math).

```go
// analysis/rmd.go
func BuildRMD(proj *models.ProjectionResult, in engine.Input) *models.RMDAnalysis

// analysis/sustainability.go
func Score(proj *models.ProjectionResult) *models.SustainabilityScore

// analysis/explainability.go
func BuildExplainability(proj *models.ProjectionResult, in engine.Input) *models.ProjectionExplainability
```

### Category B — Settings-only

```go
// analysis/budget_fit.go
func BudgetFit(in engine.Input) *models.BudgetFitAnalysis

// analysis/present_value.go
func PresentValue(in engine.Input) *models.PresentValueAnalysis
```

These read `in.Prepared` only. Taking `engine.Input` instead of bare
`prepare.PreparedSettings` keeps every analysis's call shape uniform — the
orchestrator passes the same `in` to every analysis.

### Category C — Engine consumers (perturb + re-run)

```go
// analysis/sensitivity.go
func Sensitivity(eng *engine.Engine, in engine.Input) []models.SensitivityResult

// analysis/failure_points.go
func FailurePoints(eng *engine.Engine, in engine.Input) *models.FailurePointAnalysis

// analysis/monte_carlo.go
func MonteCarlo(eng *engine.Engine, in engine.Input, runs int, seed int64) *models.MonteCarloAnalysis

// analysis/ss.go
func SSAnalysis(eng *engine.Engine, in engine.Input) *models.SSComparisonAnalysis
func SSPortfolio(eng *engine.Engine, in engine.Input, ss *models.SSComparisonAnalysis) *models.SSPortfolioAnalysis

// analysis/backtest.go
func HistoricalBacktest(eng *engine.Engine, in engine.Input, data history.Data) *models.HistoricalBacktestAnalysis
```

### Perturbation pattern

Cat-C analyses replace the current "deep-copy `Settings`, mutate, prepare,
NewCalculator, RunProjection" sequence with:

```go
func Sensitivity(eng *engine.Engine, in engine.Input) []models.SensitivityResult {
    base := eng.Run(in)
    results := make([]models.SensitivityResult, 0, 4)
    for _, perturb := range standardPerturbations() {
        modified := perturb(in.Prepared.Settings()) // deep-copy + mutate
        prepared := perturbAndPrepare(modified)     // private helper, today's calculator.go:49
        results = append(results, compare(base, eng.Run(engine.Input{
            Prepared: prepared,
            Chain:    in.Chain,                      // chain rides along
        })))
    }
    return results
}
```

`perturbAndPrepare` (today at calculator.go:49) moves to `analysis/perturb.go`.

### Two intentional behavior changes

1. **`MonteCarlo` takes an explicit `seed int64`.** Today the seed is buried
   in the function body. Lifting it to a parameter unlocks deterministic
   tests at near-zero cost. Convention: `seed == 0` means "auto-seed from
   time" (matches today's behavior). `MonteCarloSeed = 0` is the default
   used by `RunFull`.
2. **`HistoricalBacktest` takes `history.Data` explicitly** instead of
   reading a parent-package global. Same testability win. The orchestrator
   passes `history.DefaultData()`; tests can pass synthetic history.

These are the only behavior changes in the design. Numeric output is
unchanged on the parity test.

---

## Orchestrator

```go
// internal/services/retirement/orchestrator.go
package retirement

import (
    "budget2/internal/models"
    "budget2/internal/services/retirement/analysis"
    "budget2/internal/services/retirement/engine"
    "budget2/internal/services/retirement/history"
)

const MonteCarloRuns = 1000
const MonteCarloSeed int64 = 0 // 0 = auto-seed from time (matches Calculator.RunFullAnalysis)

// RunFull executes the full what-if analysis fan-out for in.
// Returns a fully populated *models.WhatIfAnalysis. Replaces
// Calculator.RunFullAnalysis.
func RunFull(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
    proj := eng.Run(in)

    explainability := analysis.BuildExplainability(proj, in)
    budgetFit      := analysis.BudgetFit(in)
    presentValue   := analysis.PresentValue(in)
    sustainability := analysis.Score(proj)
    sensitivity    := analysis.Sensitivity(eng, in)
    failurePoints  := analysis.FailurePoints(eng, in)
    monteCarlo     := analysis.MonteCarlo(eng, in, MonteCarloRuns, MonteCarloSeed)
    rmd            := analysis.BuildRMD(proj, in)
    backtest       := analysis.HistoricalBacktest(eng, in, history.DefaultData())

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

Line-by-line equivalent of today's `Calculator.RunFullAnalysis`
(calculator.go:3122-3162). No semantic changes. The orchestrator does not
cache; the handler-side `runAnalysisWithCache` stays put for Candidate #4.

`SSPortfolioEligible` (today in `social_security.go`) and
`FirstRMDCalendarYear` (today exported from parent) consolidate into
`eligibility.go` in the parent package.

---

## Call-site migration

Outside the parent `retirement` package, the production migration is
mechanical: `handlers.go`, `handlers_rates.go`, and one handler test fixture
stop constructing `Calculator` directly. The `buildCalculator` unit tests are
updated because the helper now returns `engine.Input` instead of
`*retirement.Calculator`.

### `handlers/whatif/handlers.go:80-117` — `buildCalculator` → `buildEngineInput`

```go
// after
func buildEngineInput(settings *models.WhatIfSettings) (engine.Input, string, error) {
    prepared, err := prepare.From(settings)
    if err != nil { /* unchanged error mapping */ }

    if len(settings.ScenarioChain) == 0 {
        return engine.Input{Prepared: prepared}, computeSettingsHash(settings), nil
    }

    chain := make([]engine.PreparedChainLink, 0, len(settings.ScenarioChain))
    for /* … unchanged loop … */ {
        chain = append(chain, engine.PreparedChainLink{...})
    }
    return engine.Input{Prepared: prepared, Chain: chain}, combinedHash, nil
}
```

The handler tests that currently assert `buildCalculator` behavior
(`handlers_test.go:2785`, `2814`, `2834`) become `buildEngineInput` tests.
They continue to assert hash behavior and error mapping, plus the returned
`engine.Input` shape (`Prepared` is populated, `Chain` length and transition
ages match).

### `handlers/whatif/handlers.go:131-135` — `runAnalysisWithCache` body

```go
// after
in, hashData, err := buildEngineInput(settings)
if err != nil { return nil, err }
analysis := retirement.RunFull(getEngine(), in)
```

with package-level helper:

```go
var sharedEngine = engine.New()
func getEngine() *engine.Engine { return sharedEngine }
```

The `cache` global stays untouched (Candidate #4).

### `handlers/whatif/handlers.go:670-685` — no-guardrails chart bypass

```go
// after
clone := *settings
clone.Guardrails = nil
prepared, err := prepare.From(&clone)
if err != nil { /* … */ }
projection := getEngine().Run(engine.Input{Prepared: prepared})
```

### `handlers/whatif/handlers_rates.go:93`

`calc.RunFullAnalysis()` → `retirement.RunFull(getEngine(), in)`, with the
`buildCalculator` → `buildEngineInput` swap a few lines up.

### `handlers/whatif/handlers_test.go:3059`

```go
// after
eng := engine.New()
in  := engine.Input{Prepared: prepare.MustFrom(t, s)}
// the two follow-on lines that called calc.X(...) become eng.Run(in) /
// analysis.X(...) calls.
```

### Parent-package tests that stay in `retirement/`

Tests that primarily assert orchestration or parent helpers stay in the parent
package and switch from `newTestCalc(...).RunFullAnalysis()` to
`RunFull(engine.New(), engine.Input{...})`. Current examples:

- `projection_planned_test.go`
- `calculator_failure_test.go:370`
- the F-072 integration tests in `calculator_test.go`

### Tests inside `retirement/`

Tests migrate alongside the code they exercise. Mapping:

| Test file (today) | Destination |
|-------------------|-------------|
| `rmd_test.go`, `rmd_birth_year_test.go`, `rmd_calendar_year_test.go`, `rmd_start_age_projection_test.go`, `rmd_tax_test.go`, `rmd_timing_test.go`, `calculator_rmd_gross_test.go` | `analysis/rmd_*_test.go` |
| `social_security_test.go` | `analysis/ss_test.go` |
| `backtest_test.go` | `analysis/backtest_test.go` |
| `calculator_pv_test.go` | `analysis/present_value_test.go` |
| `calculator_failure_test.go` | split: failure-point assertions to `analysis/failure_points_test.go`; full-orchestration assertion stays in parent |
| `calculator_test.go`, `calculator_expense_test.go`, `calculator_delay_test.go`, `calculator_coverage_test.go`, `coverage_gaps*_test.go`, `taxable_simulation_test.go`, `duration_test.go` | split by majority: projection-loop assertions to `engine/*_test.go`; extracted-analysis assertions to `analysis/*_test.go`; full-orchestration assertions stay in parent |
| `projection_planned_test.go` | stay in parent unless the assertions are narrowed to projection-only behavior during migration |
| `chain_test.go`, `helpers_test.go`, `settings_test.go`, `settings_crud_test.go`, `tax_test.go`, `guardrails_test.go`, orchestration/integration tests that assert full `WhatIfAnalysis` shape | stay in parent `retirement/` |

Test migration happens **incrementally per commit**. When commit 4 introduces
`analysis/sensitivity.go`, commit 4 also moves the relevant assertions into
`analysis/sensitivity_test.go`. By commit 8, every test has already moved;
deletion of legacy files is mechanical.

When a single test file mixes concerns (e.g., `coverage_gaps2_test.go` cuts
across analyses + projection), file-level decision: move the whole file to
where the majority lives. Surgical splitting is a follow-on, not a
prerequisite.

---

## Staging — branch, commits, parity test

Branch: `feat/projection-engine`, off `dev`. Eight commits, each compiling
and passing `go test ./...`:

| # | Commit | Net intent |
|---|--------|-----------|
| 1 | feat(retirement): introduce engine package with Run | New `engine/` with `Engine`, `Input`, `PreparedChainLink`, `Run`. The body of `Calculator.RunProjection` (and its private helpers `CalculateTotalIncome`/`Expenses`/`ExpenseBreakdown`/`calculateHealthcarePV`/`findSteadyStateMonth`) **moves** into `engine/`; `Calculator.RunProjection` becomes a one-line delegator to `engine.New().Run(...)`. Adds parity scaffolding: `parity_test.go`, `parityFixtures`, `compareWhatIfAnalysis`, and a temporary `Calculator.SetMonteCarloSeedForParity` override. |
| 2 | refactor(retirement): extract pure post-projection analyses | New `analysis/rmd.go`, `analysis/sustainability.go`, `analysis/explainability.go` + tests. `Calculator.BuildRMDAnalysis`/etc. become one-line delegators. Test files migrate from `retirement/` to `analysis/`. |
| 3 | refactor(retirement): extract settings-only analyses | New `analysis/budget_fit.go`, `analysis/present_value.go` + tests. Delegation pattern continues. |
| 4 | refactor(retirement): extract sensitivity, failure-points, MC | New `analysis/sensitivity.go`, `analysis/failure_points.go`, `analysis/monte_carlo.go`, `analysis/perturb.go` + tests. MC seed wired through `analysis.MonteCarlo(eng, in, runs, seed)`. The temporary parity-only seed override on `Calculator` now routes to `analysis.MonteCarlo` underneath. |
| 5 | refactor(retirement): extract SS and Backtest | New `analysis/ss.go`, `analysis/backtest.go`, `history/data.go` + tests. Historical return helpers move from the parent package to `history`; temporary parent wrappers may remain until commit 8 if needed for incremental compilation. |
| 6 | feat(retirement): add RunFull orchestrator | New `orchestrator.go` and `eligibility.go`. `Calculator.RunFullAnalysis` becomes a one-line delegator to `RunFull(eng, in)`. Parity test now compares `Calculator.RunFullAnalysis` vs `RunFull` end-to-end. |
| 7 | refactor(handlers): migrate whatif handlers and test fixture | `buildCalculator` → `buildEngineInput`, `getEngine`, three handler call sites + one test fixture updated. Calculator still exists for backwards compat (parity test still uses it). |
| 8 | refactor(retirement): delete Calculator | Delete `calculator.go`, `rmd.go`, `social_security.go`, `backtest.go`, `parity_test.go`, plus the temporary `SetMonteCarloSeedForParity` and `runFullForParity` helpers. Remaining engine/analysis tests stand alone. |

### Parity test (`internal/services/retirement/parity_test.go`)

Added in commit 1, deleted in commit 8. Both the test, the fixture set, and
the comparison helper are new — none exists today.

```go
//go:build !short

package retirement

import (
    "math"
    "testing"

    "budget2/internal/models"
    "budget2/internal/services/retirement/engine"
    "budget2/internal/services/retirement/prepare"
)

// parityFixtures returns a small representative set of *models.WhatIfSettings
// covering the projection paths most likely to drift: baseline solo,
// married-filing-jointly with SS, chain-linked scenarios, RMD-active,
// guardrails on/off, taxable+tax-deferred mix. Each fixture is built inline
// from defaultWhatIfSettings() with targeted overrides.
func parityFixtures(t testing.TB) []parityFixture { /* … */ }

type parityFixture struct {
    Name     string
    Settings *models.WhatIfSettings
    // For Cat-C analyses we use a fixed non-zero MC seed on both sides;
    // commit 4 adds a temporary "MC seed override" handle to Calculator
    // so the old path runs deterministically too. The override is deleted
    // with Calculator in commit 8.
    MCSeed int64
}

func TestParity_FullAnalysis_AcrossFixtures(t *testing.T) {
    for _, f := range parityFixtures(t) {
        t.Run(f.Name, func(t *testing.T) {
            calc := NewCalculator(prepare.MustFrom(t, f.Settings))
            calc.SetMonteCarloSeedForParity(f.MCSeed) // temporary, removed in commit 8
            old := calc.RunFullAnalysis()

            in := engine.Input{Prepared: prepare.MustFrom(t, f.Settings)}
            // RunFull's MC path will accept seed via a parity-only override
            // until commit 6, when MonteCarloSeed becomes the orchestrator
            // constant. For commits 1-5, we call analysis.MonteCarlo and
            // assemble WhatIfAnalysis manually in the test.
            new := runFullForParity(engine.New(), in, f.MCSeed)

            if diff := compareWhatIfAnalysis(old, new); diff != "" {
                t.Fatalf("parity diff:\n%s", diff)
            }
        })
    }
}

// compareWhatIfAnalysis returns "" on tolerant deep-equality, else a diff
// describing the first mismatch.
func compareWhatIfAnalysis(a, b *models.WhatIfAnalysis) string { /* … */ }
```

Comparison rules in `compareWhatIfAnalysis`:

- Exact equality for ints, bools, strings, time.Time.
- `math.Abs(x-y) < 1e-9` for floats; relative tolerance
  `math.Abs(x-y)/max(|x|,|y|) < 1e-12` if magnitudes are large.
- For RNG-derived fields (`MonteCarlo.Results`, `MonteCarlo.Distribution`),
  determinism comes from the fixed MC seed both sides use during the
  migration window. After commit 4 wires seed into `analysis.MonteCarlo`,
  the temporary `Calculator.SetMonteCarloSeedForParity` override is what
  makes the old path deterministic; both override and helper are deleted
  with Calculator in commit 8.

---

## Risks and mitigations

1. **MC determinism gap.** Today `RunMonteCarloSimulation` seeds from
   `time.Now()` if no seed is set. Wiring seed through is a behavior change
   for `RunFull`. **Mitigation:** orchestrator passes `seed=0` →
   `analysis.MonteCarlo` interprets 0 as auto-seed-from-time (matches
   today). Parity test uses a fixed non-zero seed on both sides.

2. **Hidden internal callers.** `calculator.go` calls its own helpers in
   non-obvious ways (e.g., `c.calculateHealthcarePV` from
   `CalculatePresentValueAnalysis`). **Mitigation:** when extracting an
   analysis, keep its private helpers in the same `analysis/<name>.go` file.
   Cross-analysis helpers live in `analysis/perturb.go` or
   `analysis/internal.go`.

3. **Test-file overlap.** Some test files cross-cut analyses + projection.
   **Mitigation:** file-level placement decision (move to where the majority
   lives); surgical splitting is a follow-on.

4. **Handler test churn.** `handlers_test.go` (8.3k LOC) has one direct
   `retirement.NewCalculator(...)` reference (line 3059) plus three
   `buildCalculator` helper tests. **Mitigation:** rewrite the direct fixture
   to use `engine.Input`, and keep the helper tests focused on hash/error
   behavior plus the returned input shape.

5. **Cycle risk on chain types.** `chain.go` lives in parent package but
   produces engine inputs. **Mitigation:** canonical `PreparedChainLink`
   type lives in `engine`. Parent's `chain.go` outputs
   `[]engine.PreparedChainLink`. Engine never imports parent.

6. **Cycle risk on historical data.** Backtest needs historical return data,
   and the parent orchestrator needs to call backtest. If the data type stays
   in the parent package, `analysis` would have to import `retirement`, while
   `retirement` imports `analysis`. **Mitigation:** move historical data and
   helper functions to `retirement/history`; both parent and analysis import
   that leaf package.

---

## Out of scope (deferred to follow-on candidates)

- **Engine-level caching, parallel projection runs (Candidate #3).** The new
  engine has the right shape; analyses still call `eng.Run(...)`
  independently. Engine-level reuse is a future optimization.
- **AnalysisService extraction (Candidate #4).** The handler-side `cache`
  global, `getEngine()`, and `RunFull` consolidate into an `AnalysisService`
  package later.
- **Performance work, parallel MC, anything beyond byte-equal output.**
- **Template / view-handler refactoring beyond the 4 named call sites.**
- **Renames of analysis types in `models/`.**

---

## Acceptance criteria

- `go build ./...` passes after each commit.
- `go test ./...` passes after each commit.
- `go vet ./...` and existing pre-commit hooks pass after each commit.
- Parity test (`TestParity_FullAnalysis_AcrossFixtures`) passes from commit 6
  through commit 7 (deleted in commit 8).
- After commit 8: `grep -r "retirement\.NewCalculator\|retirement\.Calculator\b"
  --include="*.go"` returns no matches.
- After commit 8: `internal/services/retirement/calculator.go`,
  `rmd.go`, `social_security.go`, `backtest.go`, and direct historical data
  globals in the parent package do not exist.
- Net LOC change: roughly -1,400 across the retirement subsystem.
- Tracker `2026-05-08-architecture-deepening.md` Candidate #1 marked
  **Landed** with the merge commit SHA.
