# Architecture Deepening — Tracker

**Created:** 2026-05-08
**Status:** Active
**Branch context:** Created on `dev`. Each candidate gets its own feature branch.

This tracker records architectural deepening opportunities surfaced from a friction
review of the retirement / what-if subsystem. Vocabulary follows the
`improve-codebase-architecture` skill's `LANGUAGE.md` (module, interface, seam,
depth, locality, leverage, deletion test).

Each candidate is a sketch — designs sharpen during the grilling loop and land in
`docs/superpowers/plans/` before execution. Do not treat the "Solution" lines
below as committed designs.

---

## Sequencing

Proposed order: **#2 → #1 → #3 → #4**.

- #2 unlocks a clean engine input type, reducing churn in #1.
- #1 then becomes the central deepening (one Projection Engine, analyses on top).
- #3 follows naturally from #1 (reusable `Run(prepared)` primitive).
- #4 sits on top of #1+#3 (AnalysisService extracted from handler globals).

**Scheduling note:** plan `2026-05-06-rmd-from-projection.md` (F-072) has
already landed — `BuildRMDAnalysis(projection)` is the live signature and
F-078 follow-on work has merged on top. No conflict for #1.

---

## Candidate #1 — Calculator-as-orchestrator vs. analyses

**Status:** Proposed.
**Files:** `internal/services/retirement/calculator.go` (3,141 LOC),
`rmd.go`, `social_security.go`, `backtest.go`, `guardrails.go`.
Downstream symptom: `internal/handlers/whatif/handlers_test.go` (8,382 LOC).

**Problem.** `Calculator` hosts ten distinct analyses as receiver methods —
`RunProjection`, `CalculateSensitivity`, `CalculateFailurePoints`,
`RunMonteCarloSimulation`, `BuildRMDAnalysis`, `RunSSAnalysis`,
`RunSSPortfolioAnalysis`, `RunHistoricalBacktest`,
`CalculatePresentValueAnalysis`, `CalculateBudgetFit`. `RunFullAnalysis`
(calculator.go:3101) is a 40-line fan-out. Every analysis is glued to one
struct because they all need `Settings` and the projection engine. The
interface every caller faces is the entire `*Calculator`. Tests can only
reach analyses through this surface — that's part of why the whatif handler
test file is 8.3k lines.

**Direction.** Extract a **Projection Engine** as a deep module behind a small
interface (`Run(PreparedSettings) ProjectionResult`). Each analysis becomes
its own module that consumes the engine. `RunFullAnalysis` either becomes a
thin orchestrator or is replaced by the AnalysisService in #4.

**Deletion test.** Removing `Calculator` would concentrate the projection
engine into one deep module and make the analyses visibly independent —
complexity concentrates, doesn't disperse.

---

## Candidate #2 — `WhatIfSettings` conflates config with derived state

**Status:** Landed on `feat/prepared-settings` (Tasks 1-3). Three commits:
`8437552` (introduce prepare package), `ee6582e` (engine takes
PreparedSettings), `698534e` (move load-time prep out of WhatIfSettings).
Plan: `docs/superpowers/plans/2026-05-08-prepared-settings.md`.
**Files:** `internal/models/whatif.go` (1,433 LOC). Normalization callers:
`settings.go:275/317/503`, `chain.go:55-56`, `whatif.go:848`.

**Problem.** `WhatIfSettings` mixes user-entered config (~60 form fields) with
derived state (`CurrentAge`, `SpouseAge`, normalized phase reference) that
must be recomputed via `ComputeAges()` + `NormalizePhaseAgeReference()` after
any mutation. Today the discipline is preserved only because
`retirementMgr.Load/Save` are gatekeepers. The contract "Calculator expects
normalized settings" is undocumented and unenforced. The shallow-copy
pattern at `handlers.go:652` (`clone := *settings; clone.Guardrails = nil`)
already bypasses normalization.

**Direction.** Split into (a) `WhatIfConfig` — the persisted, user-facing
struct, no methods — and (b) `PreparedSettings` — what the engine consumes,
returned by a `Prepare(WhatIfConfig) PreparedSettings` step. Calculator,
chain builder, and analyses take `PreparedSettings`. Form handlers and
persistence stay on `WhatIfConfig`.

**Deletion test.** Collapsing the model to a pure config struct forces every
entry point to call `Prepare` explicitly. The implicit-normalization footgun
disappears — concentration, not dispersion.

---

## Candidate #3 — Repeated full-projection runs as the only computation primitive

**Status:** Proposed.
**Files:** `calculator.go:1779-2103` (sensitivity + 4 failure-point binary
searches), `social_security.go:419+/527+`, `backtest.go`, `handlers.go:642`
(no-guardrails chart bypass).

**Problem.** Every "what if X were different" analysis builds a modified
`Settings`, instantiates a new `Calculator`, and calls `RunProjection()`.
Sensitivity does this 4×, failure-points ~80× (4 params × ~20 binary-search
iterations), Monte Carlo 1,000×, the no-guardrails chart re-runs on every
render, and `RunSSAnalysis` carries a comment apologizing for re-running the
baseline (`social_security.go:526`). The handler-side `runAnalysisWithCache`
exists because the engine has no notion of a reusable projection request.

**Direction.** Give the Projection Engine an explicit, reusable
`Run(PreparedSettings) ProjectionResult` entry point. Orchestrators hand it
perturbed `PreparedSettings` instead of constructing new Calculators.
Caching/parallelism become engine-level properties.

**Deletion test.** Force one shared engine instance and the redundant work
surfaces as one cache or one explicit re-entry primitive — concentrated.

---

## Candidate #4 — `runAnalysisWithCache` + per-view fetchers as a hidden seam

**Status:** Proposed.
**Files:** `internal/handlers/whatif/handlers.go:31-133` (global `cache`
var, `buildCalculator`, `runAnalysisWithCache`), and ~12 view handlers
across `handlers.go`, `handlers_healthcare.go`, `handlers_income_expense.go`,
`handlers_rates.go`.

**Problem.** A de facto AnalysisService already lives in the handler package
— global `cache`, SHA-256-of-JSON keying, 5-minute TTL, chain-aware hash
composition — but it's a free function and a package-level mutable global,
not a module. One consumer
(`handleWhatIfProjectionChartNoGuardrails`) bypasses it entirely. View
handlers all do `Load → runAnalysisWithCache → pluck → render`.

**Direction.** Promote into an `AnalysisService` (own package) with a real
interface (`Get(ctx, config) (*Analysis, error)` plus targeted entry points
like `Project(ctx, prepared) ProjectionResult`). Handlers depend on the
interface; the cache global goes away.

**Deletion test.** Removing the cache forces every handler to call
`RunFullAnalysis()` and the duplicated orchestration becomes visible.

---

## Status legend

- **Proposed** — sketch only, not yet grilled.
- **Grilling** — design tree being walked with the user.
- **Planned** — design committed to `docs/superpowers/plans/YYYY-MM-DD-*.md`.
- **In progress** — branch active, plan executing.
- **Landed** — merged, tracker entry updated with merge commit.
- **Withdrawn** — design rejected; ADR recorded if reason is load-bearing.
