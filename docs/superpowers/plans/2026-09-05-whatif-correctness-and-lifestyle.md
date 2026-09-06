# What-if Correctness and Full-Lifestyle Outcomes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking. Execution authorized by the user: update the plan and start building. No commits or publishing authorized.

**Goal:** Correct understated retirement risks and make the what-if page explain whether the full planned lifestyle holds, requires circuit-breaker cuts, or still falls short.

**Architecture:** Keep the canonical engine and existing simplified guardrail policy. Add small outcome summaries to existing Monte Carlo results, aggregate them in the analysis layer, and render explicit base-projection and simulation outcomes in the handler/UI. Deliver calculation, outcome-reporting, and presentation phases separately.

**Tech Stack:** Go 1.26, Go tests, html/template, HTMX, existing vanilla JavaScript/Plotly, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-05-whatif-correctness-and-lifestyle-design.md` — read alongside this plan.

**Status:** Implemented and independently verified on 2026-09-06 in `/tmp/budget2-lifestyle-build`, branch `codex/whatif-lifestyle`, based on `3f20055`. All eight task gates passed; no unresolved flags. No commit, push or deployment. The original design and pre-build evidence below are retained as history; completed checklists and `docs/superpowers/reviews/2026-09-05-whatif-fixes-verification.md` record execution.

## Review evidence (2026-09-05)

The reviewer ran Task 1's fixture against master and again with the two-line fix applied on a throwaway copy (restored afterwards). Both ends of the oracle validate:

| Case (seed 123, all-Roth cash, 1 year) | Ending-balance delta |
|---|---|
| Master, either event type | $1,031.78 |
| Fixed, one event | $12,381.33 |
| Fixed, both event types in one year | $24,762.67 |
| Fixed, one event per year over two years | $25,153.49 |
| Fixed, one event, guardrails enabled | $12,381.33 |

Two facts change the plan:

- **The reviewer observed no failures in the existing suite after the fix.** `go test ./internal/services/retirement/...` is green with the fix applied. The new regression fixture detects the undercharge. Existing seed/reuse checks remain required during integration.
- **Guardrails do not clip shocks** (`ExtraExpenses` is added after the multiplier, stepper.go:326), but **the in-year tax estimator does annualize them**: `ExtraExpenses` is not in the one-time-income carve-out of `AnnualizedInputs` (engine/projtax.go:97), and the boundary month is month 0 of its year, so a full-dollar tax-deferred-funded shock is extrapolated ×12 for that month's tax estimate. Task 1 carries a tax-deferred fixture; the bounded one-time-expense metadata remedy below was authorized and implemented.

Other verified facts folded into tasks: the buffer panel renders only when `SequenceRisk` is non-nil (≥100 runs) and `SequenceRiskImpact > 5` (Task 2); `successRateTextClass`/`successRateBarClass` in `internal/templates/render.go` tint the same success figure at 90/80/70 on three surfaces (Task 5); the MCP plan view exposes only `success_rate` (Task 4); the repo has no Playwright suite (Task 7); `ProjectionYearSummary.Withdrawals` is accumulated in two places (Task 5).

## Global constraints

- Full planned lifestyle is the target; spending circuit breakers are the fallback.
- No tax-law overhaul, full Guyton–Klinger implementation, reserve optimizer, new essentials classification, redesign, publishing, or automatic commits.
- Keep current guardrail evaluation semantics unchanged.
- Missing/pending metrics are never zero risk.
- Verify using copied data only. Do not alter household settings, guardrail thresholds, real ledger files, or real backups.
- Read applicable AGENTS.md and RTK.md before execution. Prefix shell commands with `rtk`; use `rtk proxy` when exact output and exit status matter.
- Before modifying existing Go functions/methods/types, use LSP incomingCalls; use findReferences for fields, variables, constants, and interface changes. Report direct callers/files and chase broad callers transitively. No LSP tool was available during the review; that is not permission to skip this gate during implementation.
- Warn before touching the engine package or exported models. Shared Monte Carlo changes affect main risk results, Social Security comparisons, and tax-strategy evaluation. Inspect semantic references before changing signatures or serialized fields.
- Use `prepare.MustFrom` through `engineInput`/`runProj` in analysis tests. Set ages through `Persons[].BirthMonth`, because preparation overrides derived ages. Never import the parent retirement package from analysis.
- Keep new outcome fields in computed results, not saved household settings. Preserve existing JSON fields for other consumers.
- Run focused red/green tests per task. Before any separately authorized commit: green build, vet, full tests, staticcheck, and inspected diff; invoke the repository ship skill.

## Delivery map

| Phase | Tasks | Independently reviewable result |
|---|---|---|
| 1. Calculation integrity | 1–2 | Full emergency charges; buffer claims accurately limited |
| 2. Lifestyle and fallback | 3–5 | Cut statistics and interpretable outcome/withdrawal summary |
| 3. Presentation and verification | 6–8 | Honest thresholds, usable controls, end-to-end evidence |

Do not block the emergency fix on later UI work. The phase-2 summary depends on corrected simulations. Phase 3 can be developed independently once its affected interfaces are stable.

## Verification tiers (agents2 constitution)

This plan is written in superpowers form; executing it under the agents2 swarm rules needs a ledger row per task. Run prefix **L** (lifestyle) — task IDs are a namespace, never reuse another run's. The existing critical engine glob requires Tier 3 for L1 and L3, even though these changes are reversible. Money, rendered percentages, and split classification carry the `second` lane per the 2026-08-31 lean rules.

| Ledger ID | Plan task | Tier | `checks` | Why this tier/lane |
|---|---|---|---|---|
| L1 | Task 1 — full emergency charges | 3 | `tests,second` | Money figure in every simulation; seeded outputs change; tax-annualization gate needs an adversarial reader |
| L2 | Task 2 — buffer illustration copy | 2 | `tests,content,a11y` | Copy replacement with exact-string oracle; vacuous-pass hazard (fixture gate) |
| L3 | Task 3 — per-run cut observation | 3 | `tests,second` | Real-dollar arithmetic over engine fields; censoring semantics |
| L4 | Task 4 — lifestyle aggregation | 2 | `tests,second` | Percentages, quantiles, denominators; shared stats path feeds SS grid and tax optimizer |
| L5 | Task 5 — verdict and outcome rendering | 3 | `tests,second,a11y` | Split-classification surface (success-rate tint on three surfaces); rendered-string percentages; new component |
| L6 | Task 6 — searched bounds vs transitions | 2 | `tests,second,content,a11y` | Additive fields plus exact wording; rounding-onto-failure case |
| L7 | Task 7 — tab semantics and chart header | 2 | `a11y` | Markup/JS/keyboard; verified against ACCESSIBILITY.md, not screenshots |
| L8 | Task 8 — end-to-end evidence | 1 | `tests,content` | Verification record plus durable test-only scenario fixtures; the gate for the run is `swarm/gate.sh done` |

Phase 1 (L1, L2) may be dispatched alone once L1's tax-deferred and delay-window fixtures exist. L5 depends on L3 and L4; L6 and L7 are independent of phase 2.

## File responsibilities

Existing implementation:

- `internal/services/retirement/analysis/monte_carlo.go`: event charges, per-run observation, shared aggregation; retain public entry signatures.
- `internal/services/retirement/analysis/failure_points.go`: searched bounds and bracketed transition results.
- `internal/services/retirement/engine/stepper.go`: existing MonthOutcome provides LivingExpenses, GuardrailMultiplier, expenses and portfolio state. Prefer consuming these fields without changing engine behavior.
- `internal/services/retirement/engine/guardrails.go`: reference only; current drop/rise policy is not being replaced.
- `internal/models/whatif.go`: additive computed-result fields, failure-bound metadata.
- `internal/services/retirement/orchestrator.go`: integration/reference; preserve shared seed and main-run reuse.
- `internal/handlers/whatif/verdict.go`: conditional base-projection summary and separate present/future cash-flow semantics.
- `web/templates/components/whatif/{verdict-bar,budget-analysis,monte-carlo,guardrails,failure-points,projection-chart,historical-backtest}.html`: visible labels and outcome explanation.
- `web/templates/pages/whatif.html`, `web/static/js/whatif-tabs.js`: tab/panel relationships and collapse state.

New focused units:

- `analysis/monte_carlo_shocks_test.go`: full-dollar emergency regression tests.
- `analysis/lifestyle_outcomes.go` and `_test.go`: pure cut observation and aggregation helpers.
- `analysis/failure_bounds_test.go`: tested-bound versus failure-transition oracles.
- `internal/handlers/whatif/lifestyle_render_test.go`, `risk_language_render_test.go`, `failure_bounds_render_test.go`: fixture-driven presentation tests.
- `web/templates/components/whatif/lifestyle-outcomes.html`: compact outcome distribution and fallback burden explanation.

All abbreviated `analysis/` paths above mean `internal/services/retirement/analysis/`.

## Task 1 — Charge emergency events in full

**Files:** modify `analysis/monte_carlo.go`; create `analysis/monte_carlo_shocks_test.go`.
**Consumes:** existing `RunSingleMonteCarloSimulation(engine.Input, *rand.Rand, *MonteCarloConfig) models.MonteCarloResult`.
**Produces:** unchanged interface, corrected spending/health event cash flows.

- [x] Run semantic caller checks for the simulation function and shared wrappers. Record effects on main Monte Carlo, Social Security and tax optimizer consumers.
- [x] Add the following regression fixture (package `analysis`; imports `math/rand`, `testing`, `budget2/internal/models`). It compares identical random streams, so the emergency is the only dollar difference.

```go
func TestMonteCarloEmergencyChargedInFull(t *testing.T) {
    for _, health := range []bool{false, true} {
        s := models.DefaultWhatIfSettings()
        s.StartDate = "2026-01"
        s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
        s.PortfolioValue = 1_000_000
        s.TaxDeferredPercent, s.RothPercent = 0, 100
        s.RothStockPercent, s.RothCashPercent = 0, 100
        s.ProjectionYears = 1
        in := engineInput(t, s)
        c := DefaultMonteCarloConfig()
        c.LongevityVariation, c.CrashProbability = 0, 0
        c.SpendingShockProb, c.HealthShockProb = 1, 0
        if health { c.SpendingShockProb, c.HealthShockProb = 0, 1 }
        c.SpendingShockMin, c.SpendingShockMax = 0, 0
        c.HealthShockMin, c.HealthShockMax = 0, 0
        run := func() models.MonteCarloResult {
            return RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(123)), c)
        }
        baseline := run()
        if health { c.HealthShockMin, c.HealthShockMax = 12_000, 12_000
        } else { c.SpendingShockMin, c.SpendingShockMax = 12_000, 12_000 }
        shocked := run()
        delta := baseline.FinalBalance - shocked.FinalBalance
        // This simulator clamps cash returns to [0,15] percent annually.
        // One full event plus lost growth must lie in this interval.
        if delta < 12_000 || delta > 13_800 {
            t.Fatalf("health=%v: emergency cost %.2f, want [12000,13800]", health, delta)
        }
        if shocked.SpendingShocks+shocked.HealthShocks != 1 {
            t.Fatal("expected exactly one emergency event")
        }
    }
}
```

- [x] Run `rtk proxy go test ./internal/services/retirement/analysis -run TestMonteCarloEmergencyChargedInFull -count=1 -v`. Expected current failure: approximately $1,031.78 for each type, rather than a full $12,000 event plus lost growth.
- [x] Change only the event charge expressions, retaining the month reset and RNG draw order:

```go
shockExpenses += shockAmount
shockExpenses += healthShockAmount
```

- [x] Add three cases using the same fixture, each with an explicit upper bound derived from the 15% cash-return cap so that twelve repeated full charges (and even two) are rejected:

| Case | Events expected | Delta lower bound | Delta upper bound | Measured with fix |
|---|---|---|---|---|
| Both types, one year | 1 spending + 1 health | 24,000 | 24,000 × 1.15 = 27,600 | 24,762.67 |
| One spending event per year, two years | 2 spending | 24,000 | 12,000 × 1.15² + 12,000 × 1.15 = 29,670 | 25,153.49 |
| Zero probabilities | 0 | 0 | 0 | 0 |

- [x] **Tax-annualization fixtures.** Compare a full-dollar $50,000 shock with a same-dollar planned OneTimeExpense at the same month in tax-deferred cash. Preserve RNG draws in the control by keeping the event probability and setting its amount to zero; change only the planned expense. Assert ending-balance parity, but do not treat it as sufficient proof. Add an independent month-zero tax-estimator oracle: $50,000 taxable withdrawals classified as OneTimeIncome.TaxDeferredWithdrawals must contribute $50,000, not $600,000, to AnnualInputs.TaxableWithdrawals. Inspect the actual StepMonth shock snapshot as well as the helper, so classification wiring is covered. Include recurring withdrawals alongside the discrete event and a later-month case.
- [x] **Bounded engine remedy is in scope.** Reuse PortfolioMonthInput.OneTimeExpense to classify the shock already included in TotalExpenses. It is metadata for tax treatment, not an additional charge. First inspect all ExtraExpenses callers and prove that existing callers represent discrete shocks. Extend the existing classification in the shared stepper, with no new MonthReturns field unless evidence requires one. Caller checks, a blast-radius warning, and tax/IRMAA regression checks remain mandatory. User authorization to execute includes this bounded fix; another permission question is not required merely because the file is in engine. Escalate only a genuinely different policy or architectural decision.
- [x] **Withdrawal-delay diagnostic, separate policy finding.** Use a paired two-year fixture with spending probability 1, health probability 0, exactly two events, TaxDeferredDelayYears=1, and zero event amounts in the control while retaining RNG draws. Observe funded withdrawals and unpaid shortfall by month; do not infer year-zero payment from an ending-balance difference that includes year-one events. Characterize whether unpaid need is carried, forgiven, or classified as temporary. Record any lost need separately; do not introduce debt carry-forward or change the depletion policy in Task 1. Phase-2 reporting must not call an observed funding gap full-lifestyle maintenance. If necessary, add an explicit unsupported/unknown outcome rather than silently promise coverage. Record the policy decision before any wider behavior change.
- [x] Rerun the focused test and `rtk proxy go test -count=1 ./internal/services/retirement/...`. The reviewer reported the retirement suite green with the fix applied; rerun against the execution checkout and investigate every new failure without assuming its cause. Do not update expected outputs simply to hide a discrepancy.

**Acceptance:** full event costs within the bounded table, exact event counts, no repeated monthly full charge, unchanged no-shock behavior, the shock month's tax classification passes an independent annualized-income oracle and matches a planned expense control. Withdrawal-delay behavior is documented separately without an unapproved accounting-policy change. The four measured deltas in "Review evidence" are the corrected seeded baseline; the $1,031.78 figure is the buggy one.

## Task 2 — Remove unsupported buffer promises

**Files:** `analysis/monte_carlo.go`, `web/templates/components/whatif/monte-carlo.html`, new `internal/handlers/whatif/risk_language_render_test.go`.
**Consumes:** existing `SequenceRiskBreakdown`; no new engine inputs.
**Produces:** honest illustration; legacy result field names remain compatible.

- [x] **Fixture gate.** The buffer panel lives inside `whatif-sequence-risk-details`, which renders only when `Stats.SequenceRisk` is non-nil (the analysis returns nil below 100 runs) AND `Stats.SequenceRiskImpact > 5.0`. A fixture that misses either gate renders no buffer text at all, and every "does not contain" assertion below passes vacuously. Build the fixture with `Runs: 1000`, `SequenceRiskImpact: 12`, and a populated `SequenceRisk` with nonzero `BufferAmount`, `AnnualShortfall`, and `AdjustedSpending`, then assert the OLD strings (`Recommended Buffer`, `Safe 3% withdrawal`) ARE present on master before any copy change. That red run proves the panel is rendering.
- [x] Add the render fixture above; assert the following visible wording and absence of the old promises:

```text
Buffer illustration — not simulation-tested
Illustrative withdrawal at 3%
Illustrative monthly withdrawal at 4%
This illustration uses total expenses and fixed withdrawal assumptions.
It does not account for outside income, withdrawal taxes, or a separate cash allocation.
```

- [x] Assert the rendered panel does not contain `Recommended Buffer`, `Safe 3%`, `Safe monthly spending`, `is sufficient`, or `provides good protection`.
- [x] Run `rtk proxy go test ./internal/handlers/whatif -run BufferIllustration -count=1`; confirm failure before changing copy.
- [x] Put the numeric arithmetic inside a native `<details>` titled “Buffer illustration — not simulation-tested”. Replace recommendation/safety language in the view and generated BufferRationale strings. Keep the arithmetic unchanged and clearly identified so this task is not an unreviewed optimizer rewrite.
- [x] Run handler render tests and analysis tests. The only non-template consumer of `SequenceRiskBreakdown` outside the analysis package is `internal/services/retirement/orchestrator.go`; the MCP plan view (`internal/services/mcpsvc/plan/view.go`) exposes none of the buffer fields. Confirm with `grep -rn 'BufferRationale\|RecommendedBuffer\|BufferAmount' --include='*.go' internal | grep -v _test` and record the result in the manifest; legacy fields keep serializing.

**Acceptance:** the fixture's red run shows the old copy rendering; the green run shows the new copy and none of the old promises. No tested or safe reserve recommendation is implied. This is the proposed first-release correction, not a claim that the reserve calculation has become financially complete.

## Task 3 — Observe circuit-breaker cuts in each simulation

**Files:** additive models in `internal/models/whatif.go`; new `analysis/lifestyle_outcomes.go` and `_test.go`; small integration in `analysis/monte_carlo.go`.
**Consumes:** existing `engine.MonthOutcome`, `st.CumulativeInflation`, and `models.MonteCarloResult`.
**Produces:** proposed additive field `MonteCarloResult.GuardrailImpact *MonteCarloGuardrailImpact`:

```go
type MonteCarloGuardrailImpact struct {
    FundingGapMonths int `json:"funding_gap_months"`
    MonthsObserved int `json:"months_observed"`
    MonthsBelowPlan int `json:"months_below_plan"`
    LongestBelowPlanMonths int `json:"longest_below_plan_months"`
    CutEpisodes int `json:"cut_episodes"`
    MinLivingSpendingMultiplier float64 `json:"min_living_spending_multiplier"`
    MaxMonthlyLivingCutReal float64 `json:"max_monthly_living_cut_real"`
    BelowPlanAtEnd bool `json:"below_plan_at_end"`
}
```

A non-nil impact means metrics were observed; nil means unavailable, not no cuts. All new runs get a non-nil record even with guardrails disabled.

- [x] Check model references, JSON consumers, SS reuse and optimizer tests before adding fields.
- [x] Introduce an unexported accumulator `guardrailImpactTracker` with `observe(plannedLiving, multiplier, cumulativeInflation float64)` and `result() models.MonteCarloGuardrailImpact`. Keep longest-current-streak state private.
- [x] Test the helper before integration with this exact oracle:

```go
func TestGuardrailImpactTracksBelowPlanMonths(t *testing.T) {
    var tracker guardrailImpactTracker
    for _, mult := range []float64{1, .9, .8, 1, 1.1, .9} {
        tracker.observe(1000, mult, 2)
    }
    got := tracker.result()
    if got.MonthsObserved != 6 || got.MonthsBelowPlan != 3 ||
        got.LongestBelowPlanMonths != 2 || got.CutEpisodes != 2 ||
        !got.BelowPlanAtEnd || math.Abs(got.MinLivingSpendingMultiplier-.8) > 1e-9 ||
        math.Abs(got.MaxMonthlyLivingCutReal-100) > 1e-9 {
        t.Fatalf("unexpected impact: %+v", got)
    }
}
```

- [x] Run `rtk proxy go test ./internal/services/retirement/analysis -run GuardrailImpact -count=1`; expect failure until helper exists.
- [x] Count a cut month only if plannedLiving > 0 and multiplier < 1−1e−9. Increment MonthsObserved on every observed month. Start MinLivingSpendingMultiplier at 1; do not classify a raise as a cut. Real dollar cut is `plannedLiving * max(0, 1-multiplier) / cumulativeInflation`. Engine inflation must be positive; test invalid helper inputs explicitly rather than silently divide by zero.
- [x] Add helper cases: disabled/no cuts; all months cut; recovery then relapse; reduced raise at 1.1; zero living expenses; phase-reduced plannedLiving with multiplier 1; observed path ending while cut. Verify longest streak and episode counts, not just totals.
- [x] Preserve observed funding gaps separately from legacy depletion policy: increment FundingGapMonths for `out.Result.Shortfall > 1e-7`. Add `FundingShortfall float64` (`json:"funding_shortfall,omitempty"`) to ProjectionMonth and populate from the canonical StepMonth result, without changing withdrawals or debt policy. Verify delay-window fixtures retain the actual unpaid need. This additive engine/month.go instrumentation is a Tier 3 surface and requires caller checks plus calibrated oracle.
- [x] Immediately after each existing StepMonth, observe `out.LivingExpenses`, `out.GuardrailMultiplier`, and `st.CumulativeInflation`; store the final tracker result before returning MonteCarloResult. Include the depletion month once, then stop observing at the existing stop point.
- [x] Compare seeded no-shock outputs before/after instrumentation: balances, tax totals, event counts and survival must be identical. Only new metadata changes. Guardrail policy and RNG draws must be unchanged.
- [x] **Adaptive sub-run is not a guardrail cut.** `aggregateMonteCarlo` reruns `runSimulations` with `AdaptiveSpending=true` (fixed seed 42) to compute `EarlyCrashSurvivalAdapted`; that path reduces discretionary expense sources through `DiscretionaryMultiplier`, which never touches `GuardrailMultiplier`. The tracker will correctly report no cuts for those runs. Document in `lifestyle_outcomes.go` that `GuardrailImpact` measures the drop/rise guardrail only, and that Task 4 aggregates from the MAIN results slice, never from `adaptiveResults`.

**Acceptance:** cut metadata reflects actual living-expense reductions. Duration ends at depletion or simulated horizon; no implied recovery afterward. Do not change `engine.GuardrailState.Evaluate`.

## Task 4 — Aggregate full-lifestyle and fallback outcomes

**Files:** `internal/models/whatif.go`, `analysis/lifestyle_outcomes.go` and `_test.go`, shared aggregation in `analysis/monte_carlo.go`, additive MCP view in `internal/services/mcpsvc/plan/view.go` and its test.
**Consumes:** `[]models.MonteCarloResult` with the Task 3 metadata.
**Produces:** proposed `MonteCarloStats.Lifestyle *LifestyleOutcomeStats`:

```go
type LifestyleOutcomeStats struct {
    Runs int `json:"runs"`
    FundedWithoutCuts int `json:"funded_without_cuts"`
    FundedWithCuts int `json:"funded_with_cuts"`
    Shortfall int `json:"shortfall"`
    RunsWithCuts int `json:"runs_with_cuts"`
    MedianCutMonths float64 `json:"median_cut_months"`
    P90CutMonths float64 `json:"p90_cut_months"`
    P90LongestCutMonths float64 `json:"p90_longest_cut_months"`
    MedianMaxLivingCutPct float64 `json:"median_max_living_cut_pct"`
    P90MaxLivingCutPct float64 `json:"p90_max_living_cut_pct"`
    P90MaxMonthlyLivingCutReal float64 `json:"p90_max_monthly_living_cut_real"`
    CutRunsEndingBelowPlan int `json:"cut_runs_ending_below_plan"`
}
```

- [x] Add pure `aggregateLifestyleOutcomes(results []models.MonteCarloResult) *models.LifestyleOutcomeStats`. Return nil for empty input or any missing GuardrailImpact record; never misclassify legacy missing data as full lifestyle maintained.
- [x] Use this four-run oracle: survived/no cuts; survived/12 cut months; failed/24 cut months; failed/no cuts. Expect category counts 1/1/2, RunsWithCuts=2, median duration=18, P90 duration=24. All three category counts must sum to Runs. Classify any run with FundingGapMonths > 0 as Shortfall even when legacy Survives is true. FundedWithoutCuts+FundedWithCuts equals the existing survival count only when no surviving run has an observed gap; add an explicit surviving-but-unfunded delay fixture proving the exception. Keep the legacy survival statistic unchanged and label it as avoiding modeled depletion, not funding every month.
- [x] Define median as the middle value or mean of two middle values; P90 uses nearest rank `ceil(.9*n)-1`. Restrict depth/duration distributions to runs with cuts, including failed runs; label them accordingly. Test a one-run distribution and an even-sized distribution.
- [x] Store the aggregate in `monteCarloCoreStats` (monte_carlo.go:205), the path shared by `aggregateMonteCarlo` (main run) and `aggregateMonteCarloStats` (SS baseline reuse, SS grid cells, tax-optimizer finalists). Consequences to state in the manifest: every SS cell and optimizer finalist now carries `Lifestyle` too (cheap, additive); `TestSSPortfolioBaselineFromMainMCMatchesResimulatedCell` compares the two paths with `reflect.DeepEqual` and must stay green without edits, which is the proof the paths agree.
- [x] **MCP consumer.** `internal/services/mcpsvc/plan/view.go` shapes `MonteCarloView{SuccessRate}` only, so an MCP client still sees the single number the spec is retiring. Add an additive optional block, `Lifestyle *LifestyleView `json:"lifestyle,omitempty"`` with `Runs`, `FundedWithoutCuts`, `FundedWithCuts`, `Shortfall` (ints, JSON snake_case), populated only when `Stats.Lifestyle` is non-nil. Add a test in that package asserting the block is absent for nil stats and present with the four counts otherwise. Also expose additive `success_rate_definition` text explaining that legacy success means avoiding modeled depletion and may include unpaid spending during withdrawal delays; preserve the existing number and test the definition alongside a gap-bearing outcome. `run_scenario` already omits Monte Carlo; leave that unchanged.
- [x] Run `rtk proxy go test -count=1 ./internal/services/retirement/analysis -run 'Lifestyle|MonteCarlo|SSBaseline'`, the complete analysis package, and `./internal/services/mcpsvc/...`.

**Acceptance:** visible percentages use all runs as denominator; fallback-burden quantiles use cut runs. These results support “funded with cuts,” not the causal claim “cuts saved this plan.” Existing no-guardrail chart comparison remains deterministic; do not label it a paired Monte Carlo test.

## Task 5 — Put lifestyle outcomes and present funding needs in the verdict

**Files:** `internal/handlers/whatif/verdict.go`, existing verdict tests, new `lifestyle_render_test.go`, `internal/templates/render.go` (success-rate class helpers), `verdict-bar.html`, `budget-analysis.html`, `guardrails.html`, new `lifestyle-outcomes.html`, `web/templates/pages/whatif.html`.
**Consumes:** corrected projection, BudgetFit, and MonteCarloStats.Lifestyle. No second cash-flow calculator.
**Produces:** explicit, conditional summary and a compact outcome component directly below it.

- [x] Add fixture tests covering each base outcome before changing rendering:

| Projection fixture | Required meaning |
|---|---|
| Survives, no below-plan living cuts | Base projection funds planned lifestyle through the actual end month |
| Survives, some below-plan living cuts | Base projection funds spending with circuit-breaker cuts |
| Depletes, guardrails enabled | Base projection has a funding shortfall despite configured guardrails |
| Depletes, guardrails disabled | Base projection has a funding shortfall |
| Survives but any FundingShortfall > 1e-7 | Base projection has an unpaid funding gap; do not claim planned lifestyle is funded |
| Missing projection | Projection unavailable; no fabricated depletion date |
| Risk pending/missing | Risk results pending/unavailable; no zero-failure implication |

- [x] Derive base cuts from observed projection months with positive planned living expenses and GuardrailMultiplier < 1−1e−9. Do not use event type alone: lowering a prior raise to 1.1 remains above plan. Use a nil-safe helper in verdict.go and test legacy zero multiplier separately.
- [x] Derive calendar endpoints from parsed StartDate and month offsets (`start.AddDate(0, offset, 0)`), not integer year addition. Last funded month is ProjectionYears*12−1; depletion month uses DepletionMonth. Test January and April starts plus a depletion crossing New Year.
- [x] **This changes an existing tested headline; say so in the manifest.** Today `BuildVerdict` renders `startYear + ProjectionYears`, so a 2026-01 start with 38 years reads "Funded through 2064" (verdict_test.go, "funded full horizon with strong MC is green"). The last funded month is 2063-12. New format is month-and-year, `Jan 2006`-style: "Funded through Dec 2063"; depletion at month 72 from 2026-01 reads "Funds run out in Jan 2032". Rewrite the four `TestBuildVerdict` subtests and `TestVerdictBar_Render` to the new strings in the same commit, and list them in the manifest so a checker reads the change as intended, not as a regression.
- [x] **Split classification (the W4 class) — enumerate every surface that tints the success rate before changing any.** `successRateTextClass` (render.go:787) and `successRateBarClass` (render.go:858) band the same figure at 90/80/70 and are used on four templates: `verdict-bar.html`, `monte-carlo.html` (text and bar), `historical-backtest.html` (twice), and `tax-optimizer.html`. Neutralizing only the verdict's 70% rule while those keep tinting green at ≥90 leaves the classification split across surfaces. Decision for this task: make the two helpers return the neutral classes (`text-gray-800 dark:text-gray-200` and `bg-accent-strong`) for every value, keep the helper as the single call site so a future risk target lands in one place, and add a render test that greps every rendered surface for `text-green`, `text-lime`, and `text-red` next to a success percentage and finds none. The `second` lane's job is to find a surface this enumeration missed (charts.js, the MCP view, the tax-optimizer table).
- [x] Retain current and selected-future gap fields separately in VerdictView; current gap comes directly from BudgetFit.MonthlyGap. Label current gap “Needed from portfolio after estimated taxes and RMDs”; explain positive shortfall consistently across cards. Label the existing RequiredRate “Additional withdrawal rate after RMDs”; never present it as total portfolio draw.
- [x] Add a distinct first-projection-year outflow fact using the existing ProjectionYearSummary.Withdrawals field, also rendered as Portfolio Out in projection-breakdown.html. Two accumulators produce that field: `engine/month.go:128` sums `cashFlow.ActualWithdrawal`, and `analysis/explainability.go:53` sums `month.NetWithdrawal`, which month.go:177 sets from the same `ActualWithdrawal`. The template iterates `.YearlySummaries` (projection-breakdown.html:32) in the scope it is invoked with at whatif.html:201; both `models.ProjectionResult` (engine-built, whatif.go:937) and `models.ProjectionExplainability` (analysis-built, whatif.go:1013) carry a field of that name. Confirm from the enclosing `{{with}}` which one is in scope, read the verdict fact from `Projection.YearlySummaries[0].Withdrawals` (the engine copy, the same source the sweep and trajectory handlers already use), and add a test asserting the two accumulators agree for the first year on a fixture with an RMD so a future divergence is caught. Display its annual dollar amount; if a percentage is shown, divide by that same year's starting portfolio and label “Net portfolio outflow / starting balance.” Do not gross up or add RMDs a second time. Omit a percentage at zero starting balance.
- [x] Render the three simulation outcome shares from the counts in Task 4, with count/denominator and a one-line conditional-model explanation. Example fixture: 600 no-cut funded + 300 cut-funded + 100 shortfall of 1000 must display 60%, 30%, 10%. Do not replace this with a single “90% full lifestyle success” figure.
- [x] Render “Among runs with cuts” above depth/duration summaries; convert months to years only for display and state that cuts still ongoing at the stopping point have incomplete observed durations. Show the living-budget percentage and today's-dollar impact together.
- [x] Use neutral aggregate risk styling until a target is chosen. Base projection with cuts is amber; base shortfall is red. Replace the hidden 70% green success policy and “median path” wording. Never color missing simulations as evidence of safety.
- [x] Explain beside guardrail controls: “These rules adjust your living-expense budget. Healthcare, property tax, and other expense lines are not automatically cut. This does not separately protect essential expenses.” Show current rules as unchanged user inputs.
- [x] Run `rtk proxy go test -count=1 ./internal/handlers/whatif` and focused result assembly tests. Check fast partials, full results, and settings refresh, not just a full initial page.

**Acceptance:** a user can distinguish full lifestyle, spending reductions and shortfall; current funding needs remain visible while moving the future-year slider. No risk threshold or household policy is invented.

## Task 6 — Separate searched bounds from approximate failure transitions

**Files:** `internal/models/whatif.go`, `analysis/failure_points.go`, new `analysis/failure_bounds_test.go`, `failure-points.html`, `monte-carlo.html`, `historical-backtest.html`, new `failure_bounds_render_test.go`.
**Consumes:** existing one-variable threshold searches.
**Produces:** additive FailurePoint fields `SearchMin float64`, `SearchMax float64`, `ThresholdFound bool` with JSON names `search_min`, `search_max`, `threshold_found`.

- [x] Add tests for survival at each outer search bound: no failure threshold found within that range. Add bracketed failure cases confirming ThresholdFound=true, and a baseline-depleted case retaining its existing special message. Check all four parameter branches, not just inflation.
- [x] Populate actual search endpoints and set ThresholdFound=false for boundary-survival returns. Preserve legacy numeric Threshold for consumers but require the flag in the view. If a bound is not searched (for example allocation-based return sentinel), do not invent a range.
- [x] Render these mutually exclusive strings:

```text
Approximate failure thresholds
One assumption changes at a time; all other settings stay fixed.
No depletion found within the tested range.
Approximate transition near …
Tested range: … to …
Lowest simulated ending balance
Ending balances are nominal dollars at each run's simulated endpoint.
```

- [x] Replace “Safe” with a bounded interpretation such as “Larger modeled margin”; replace absolute robustness claims. Verify rounded values are described as approximate, not exact.
- [x] **Rounding can land on the failing side.** The searches round the last SURVIVING value (`math.Round(low*10)/10` for inflation, nearest $50 / $1,000 for expenses and portfolio), so a bracket of [7.96, 8.04] renders 8.0, which may itself fail. Add one test per rounding branch that constructs such a bracket (drive the search with a fixture whose true transition sits just above a rounding boundary, found by bisection in the test itself) and asserts the view says "Approximate transition near 8.0%" and never "Fails if above 8.0%". This is what makes the "approximate" wording a tested claim rather than decoration.
- [x] Explain that success means meeting modeled funding needs to each run's horizon; include the existing longevity variation range and guardrail-policy context. Describe differences between survival rates as percentage points, not relative percentages. Preserve historical versus simulated model distinctions.
- [x] Run `rtk proxy go test -count=1 ./internal/services/retirement/analysis ./internal/handlers/whatif`.

**Acceptance:** survival at 15% inflation never renders “fails above 15%.” Sample minima are never promised as worst possible outcomes. Labels distinguish nominal dollars and varying horizons.

## Task 7 — Make chart controls and navigation usable

**Files:** `projection-chart.html`, `web/templates/pages/whatif.html`, `web/static/js/whatif-tabs.js`, existing tabs render tests.
**Consumes:** existing data-wf-tab, data-wf-panel, data-wf-collapse attributes and HTMX wiring.
**Produces:** accessible behavior without changing persistence keys or chart endpoints.

- [x] Add static render assertions that each tab has a unique ID and aria-controls, and each panel has role=tabpanel and aria-labelledby matching its tab. Implement the overview pair as:

```html
<button id="wf-tab-overview" role="tab" aria-controls="wf-panel-overview"
        data-wf-tab="overview" type="button">Overview</button>
<div id="wf-panel-overview" role="tabpanel" aria-labelledby="wf-tab-overview"
     data-wf-panel="overview"></div>
```

- [x] Set tabindex=0 only on the selected tab, −1 on others. Add Left/Right wraparound and Home/End selection, moving focus to the selected tab. Do not steal focus during automatic partial refresh. Retain click behavior and fallback to Overview for unknown saved tab names.
- [x] Set aria-expanded and aria-controls on each existing collapse button; give its body a stable ID. Both restored state and click toggles must update these attributes.
- [x] Stack the chart header on mobile and allow the control group to wrap: outer `flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between`; inner `flex flex-wrap items-center gap-2`; dollar-toggle group `shrink-0`. Preserve the existing style and pressed states.
- [x] **No Playwright suite exists in this repo** (only plan documents mention it). The viewport and keyboard checks below run through the session's Playwright MCP browser against the verify server from Task 8, and the only durable evidence is the record written in Task 8. Each check must be logged there with viewport, action, and observed result; a `checker-a11y` verdict that cites only "verified in browser" without that record is not evidence.
- [x] Verify in the Playwright MCP browser at 390×844, 800×600, and 1440×1000, with 200% zoom-equivalent reflow. “Today's Dollars” must be fully readable, both dollar controls clickable, and comparison controls reachable. No page-level horizontal overflow; tables may scroll within their own containers.
- [x] Browser evidence found the shared desktop nav needs about1190px but appears at768px. In layouts/base.html keep mobile navigation until the xl (1280px) breakpoint, rebuild static CSS, and verify menu reachability. This narrow dependency is required to meet the800px no-overflow check.
- [x] Keyboard-test all tabs, collapse buttons and chart controls. Change a setting on Cash Flow and verify tab/scenario persistence after both fast and full refresh. Check focus remains sensible and no control silently resets the underlying scenario.

**Acceptance:** readable mobile controls and working keyboard semantics, independently verified after the UI change. A screenshot alone does not certify accessibility.

## Task 8 — End-to-end evidence and release checks

**Files:** implementation/test files from Tasks 1–7; create `docs/superpowers/reviews/2026-09-05-whatif-fixes-verification.md` during execution.
**Consumes:** completed changes. **Produces:** verification record and a scoped, reviewable diff.

- [x] Launch only through `rtk proxy scripts/whatif-verify.sh start`; use `http://localhost:8099/whatif`. Stop with the same script's `stop` command. If this environment kills detached children when the launcher exits, keep the launcher shell session open rather than hand-rolling another server or using real data.
- [x] Exercise disposable fixtures: no-cut funded plan; cut-funded plan; shortfall despite guardrails; guardrails disabled; zero living expenses; scheduled spending phases; healthcare/property-tax-heavy plan; absent/pending analysis; January and non-January starts.
- [x] For fixed seeds, record no-cut funded/cut-funded/shortfall counts, cut quantiles, and survival before versus after the emergency correction. Assert count identities and explain expected changes. Do not set pass criteria requiring a particular personal success probability.
- [x] Spot-check displayed sums from rendered strings: expenses + taxes − income − applicable RMD cash flow equals labeled gap at displayed precision; actual outflow is not counted twice. Verify populated rose tax costs and correct NIIT/IRMAA treatment in their existing views.
- [x] Record browser console errors, mobile captures, keyboard results, and tab/scenario refresh behavior. Run the UI detector once over changed templates, investigate actionable findings, and do not treat heuristic style counts as proven accessibility failures.
- [x] Run the complete repository gate without filtering test output:

```bash
rtk proxy go build ./...
rtk proxy go vet ./...
rtk proxy go test ./...
rtk proxy staticcheck ./...
rtk git diff --check
rtk git diff --stat
rtk git diff
```

- [x] Stop the verification server and detector. Confirm only intended code/tests/docs changed, with no household data or backup modifications. Report remaining failures honestly. Commit/push only if separately requested, using the ship skill.

## Review checklist — design review, implementation evidence still required

| Question | Resolution |
|---|---|
| Does Task 1's oracle catch one-twelfth charges, repeated monthly charges, both shock types, and multi-year recurrence? | Yes, measured at both ends (Review evidence). Two gaps added: tax annualization of the shock month and the withdrawal-delay window. |
| Are the cash-buffer limitations sufficient for a first release? | Yes as an illustration, provided the fixture clears the render gate so the negative assertions cannot pass vacuously. |
| Does full-lifestyle classification respect phases, raises, zero living expense and depletion censoring? | Yes; `LivingExpenses` is the pre-guardrail base after phase multiplier and inflation. Adaptive sub-run clarified as not a guardrail cut. |
| Are denominators and quantiles unambiguous? Does missing metadata stay unknown? | Yes; nil `GuardrailImpact` yields nil `Lifestyle`. |
| Does the plan avoid claiming cuts caused survival? | Yes; wording is "funded with cuts". |
| Does it preserve guardrail scope? | Yes; copy beside controls states living-expense-only scope. |
| Are tax-aware cash flows reused in the headline? | Yes; `BudgetFit.MonthlyGap` is expenses minus (income + RMD − taxes), verified in budget_fit.go. |
| Do SS/optimizer seeded comparisons survive? | Implemented through monteCarloCoreStats; the unchanged DeepEqual reuse test and independent L4 reviews pass. |
| Are pending states, dates, mobile, keyboard covered? | Yes, with the headline-date test rewrite made explicit and the absence of a Playwright suite acknowledged. |
| Are phases independently reviewable? | Yes; phase 1 is dispatchable alone once L1's two extra fixtures exist. |

## Execution

The user authorized plan corrections and implementation. The corrected tier table applies; no repeat sign-off is required. Under the agents2 constitution: create the ledger rows L1–L8 with the `checks` column as tabled, dispatch Tier 2 tasks to `worker-coder` (the lead may take L2 or L8 directly per the lean exception), run the named checkers, and accept only on `swarm/gate.sh check <task>` exit 0. Nothing in this planning work changes the live retirement scenario.

## Execution correction: observed gaps and critical engine checks

The delay diagnostic found year-zero unpaid need is not carried forward: a $12,000 event adds $12,000 shortfall and $0 funded withdrawals while tax-deferred withdrawals are blocked. Year-one events can still change ending balance, so ending-balance parity cannot prove coverage. Tasks 3–5 therefore preserve actual shortfall metadata and classify observed unpaid spending as shortfall without changing debt/depletion policy.

The repository already has critical engine globs and an accessibility standard. Preserve those files and prior ledger rows. L1 and the additive canonical month metadata in L3 require Tier 3 executable acceptance checks; the tier table above has been corrected. The L1 test oracle was calibrated red before production edits; the executable wrapper was added after that calibration when the lead corrected the tier oversight.

## L5 execution contract correction (2026-09-06)

L5 attempt2 second FAIL conceded: the test selected a nested tax row and ignored additive-looking IRMAA, missing visible double presentation; surplus aria said funding need. Mechanism: second checker complete-visible-row oracle. Gate escalates L5 to Tier3. This is a lead/spec defect: attempt3 contract must parse prominent current totals and every future additive-looking row, treating NIIT/IRMAA as included only when visibly labeled. Current tax total excludes IRMAA, both IRMAA rows say included in Monthly Expenses, and signed adjustment uses neutral displayed cash-flow total wording. No engine/model/accounting change. Calibrate executable whole-L5 oracle red on actual attempt2 and green on disposable prototype BEFORE dispatch. This is the last attempt before the three-failure hard stop.
