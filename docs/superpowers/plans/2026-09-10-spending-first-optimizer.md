# Spending First Optimizer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. This document authorizes planning only; execute when the user requests implementation.

**Goal:** Find spending options that fund more living expenses now, expose possible future cuts, and maintain a minimum comfortable lifestyle in every final checked future.

**Architecture:** Add a spending-first search that varies both the starting living budget and existing guardrail policies, using the current simulation engine and a new observation-only spending tracker. Add a timed, cuttable living boost to the shared living schedule and an income/withdrawal timeline from existing base-case outputs. Keep previews immutable and save the selected base budget, boost schedule, and policy together through the existing revision-aware settings manager. Replace the primary optimizer surface while retaining existing manual controls and legacy routes during migration.

**Tech Stack:** Go 1.26, existing `prepare`/retirement engine/analysis packages, `html/template`, HTMX, vanilla JavaScript, Plotly, existing Go and browser test harnesses.

**Spec:** [Spending First Optimizer — Design](../specs/2026-09-10-spending-first-optimizer-design.md). Read it before executing; proposed defaults are not user-confirmed preferences.

## Global Constraints

The following requirements are copied verbatim from the spec:

- The minimum includes basic living costs plus a fun allowance, expressed in today's dollars.
- Healthcare, taxes, property tax, major expenses, and other configured obligations remain additional costs; the living minimum must not double-count them.
- Never lower the user's minimum, relax the acceptance rule, shorten the modeled horizon, or discard failed futures to produce a recommendation.
- An accepted candidate has zero minimum-shortfall months and zero unpaid-obligation months across every final validation path. At real-cent precision, unpaid other obligations equal max(0, shortfall - adjusted living), following `engine.FundedLiving`'s obligations-first attribution. A shortfall absorbed by living reduces the adjusted living request; it is a below-plan cut only when funded living is below that month's planned living, and a minimum failure only when below the minimum. Unpaid other obligations imply zero funded living and therefore also a minimum failure for the positive floor. These outcome counts overlap; they are not disjoint categories.
- Portfolio depletion is reported separately; dependable income may still fund all obligations after depletion.
- Rank spending by funded living in the first five years by default; requested starting spending and lifetime totals are separate measures.
- Scheduled spending phases and decline assumptions remain in force. A requested early-spending boost is added separately; its scheduled expiry and existing phase reductions are disclosed separately from below-plan cuts.
- Show observed failures and sample sizes. Never label simulation results “guaranteed,” “risk-free,” or an exhaustive worst case.
- Preview, graph inspection, cancellation, and failed requests never change saved settings.
- Apply saves the selected base living-expense setting, optional timed living-spending boost, and complete guardrail configuration atomically against the preview's scenario and revision.
- Keep the existing tax, inflation, withdrawal, and guardrail engines authoritative; do not introduce parallel financial formulas. The timed boost extends the shared planned-living schedule before existing guardrails and the floor are applied.
- Preserve input hooks, reproducible scenario draws, full-horizon observations after depletion, and independent final validation.
- “Follow planned spending” additionally requires zero below-plan months in every final validation path. “More spending with flexibility” permits below-plan months above the minimum. Depletion by itself disqualifies neither class: if all planned living remains funded, the planned class may still pass. Without the zero-cut rule, the planned class could accept arbitrarily high requests that were never actually funded whenever post-depletion resources cover the minimum.
- Every budget measured at selection scale is re-measured on the final validation stream and shown as a frontier row, one table per option class, in ascending budget order. The headline options are the highest-ranked qualifying rows. Discovery-only results are never displayed. Every qualifying final frontier row can be inspected and applied; the two headlines highlight choices without restricting the user to them.
- Selection and final validation each use 1,000 paths to assess the same class-specific acceptance rule. All selection-measured candidates, including selection failures, advance to final measurement so the complete tested frontier remains visible. Selection feasibility guides refinement; final counts determine displayed qualification. A candidate can change status in either direction on fresh draws. Freeze its base budget, boost schedule, and policy before final measurement and never re-tune on final-stage results.
- The optional boost is a monthly living amount in today's dollars with a fixed calendar stop month. It is cuttable living, included in the objective and subject to the same minimum; it is not an additional protected obligation. Its amount and stop month are fixed across a search, while the actual current-plan baseline retains its saved schedule.
- Show a separately labeled base-case timeline of Social Security, other configured income including pensions, portfolio withdrawals, taxes, and funded living. Preserve the engine's accounting and observed date range; do not double-count transfers or taxes, stitch simulated percentiles into cash flow, or imply dependable-income coverage from this illustration.
- Describe the implemented rules as portfolio-trigger spending rules. Full Guyton–Klinger is a separate strategy requiring its own implementation and validation; advertised withdrawal rates, portfolio examples, and generic healthcare estimates are not optimizer defaults or acceptance thresholds.

Repository requirements also apply:

- Before modifying any existing Go function, method, type, variable, or constant, obtain the semantic call/reference analysis required by `AGENTS.md`. Report callers and files, including transitive callers for broad changes. No LSP tool was exposed during planning, and no Go symbols were edited. Restore access to semantic analysis at implementation preflight; text search is not a substitute for the mandated impact check.
- Changes to shared Monte Carlo types and `runSingleMonteCarloSimulation` cross packages. Warn about that blast radius before editing. Task 0 also changes the shared living schedule in `engine.StepMonth` and deterministic expense helpers, affecting canonical projection, Monte Carlo, backtest, and expense analyses. Obtain transitive caller evidence and warn before that work; tax and guardrail decision formulas remain unchanged.
- Prefix shell commands with `rtk`; use `rtk proxy` for unfiltered test output. Never pipe test output through a filter that hides its exit status.
- Analysis tests use `engineInput(t, s)`/`runProj(t, s)` and prepared settings. Set fixture ages through `Person.BirthMonth`, not overwritten age fields. Use `os.Chtimes` for timestamp tests.
- Run `go build ./...`, `go vet ./...`, `go test ./...`, and `staticcheck ./...` before any implementation commit; inspect the diff. Use the `ship` skill when committing or pushing is requested. This plan does not request commits or a deployment.
- Use `whatif-verify` and `scripts/whatif-verify.sh` for browser testing against copied data. Follow `ACCESSIBILITY.md` and WCAG 2.2 AA.

---

## Implementation map and order

| Task | Responsibility | Main files |
|---|---|---|
| 0 | Optional timed living schedule with shared engine parity | New boost model/helper; settings/preparation/stepper integration |
| 1 | Candidate contract and consistent preparation | New models and candidate-preparation helper |
| 2 | Monthly spending experience and risk metrics | New observer/aggregator; opt-in Monte Carlo wiring |
| 3 | Discovery screen, selection at validation scale, validated frontier | New search and scenario-runner files |
| 4 | Preview, graph, cancel, atomic Apply | New spending handlers and route registration |
| 4A | Base-case income and withdrawal timeline | New timeline adapter and accounting fixtures |
| 5 | Spending-led form, comparisons, and graphs | New templates/JS; replace primary inclusion |
| 6 | Financial, browser, performance, and regression verification | Integration tests, browser fixtures, recorded evidence |

Execute Task 0 before Task 1, and Task 4A after Task 4. Tasks 0–4A must pass before connecting the production UI. Tasks 5–6 complete the feature. Do not ship a renamed legacy optimizer as this feature. Independent review can reject each task on its stated oracle; no task passes solely because its diff appears reasonable.

## Task 0: Add an optional, timed living-spending boost to the shared schedule

**Files:**
- Create `internal/models/living_spending_boost.go` and `living_spending_boost_test.go`.
- Modify `internal/models/whatif.go` (`WhatIfSettings`), and the relevant validation/clone paths in `internal/services/retirement/prepare/prepare.go` after semantic impact checks.
- Create `internal/services/retirement/engine/spending_boost.go` and `spending_boost_test.go`.
- Modify `internal/services/retirement/engine/stepper.go` and `expense.go`; inspect `loop_helpers.go` and any live compatibility expense calculator identified by LSP for parity.
- Add canonical/Monte Carlo/backtest parity fixtures in their existing test packages. No separate tax or guardrail algorithm is introduced.

**Interfaces:**

```go
// models/living_spending_boost.go; use explicit snake_case JSON tags.
type LivingSpendingBoost struct {
    MonthlyReal float64
    StopMonth string // YYYY-MM; first calendar month WITHOUT the boost
}
func CloneLivingSpendingBoost(*LivingSpendingBoost) *LivingSpendingBoost

// Add to WhatIfSettings; nil is disabled and omitted from JSON.
LivingSpendingBoost *LivingSpendingBoost `json:"living_spending_boost,omitempty"`

// engine/spending_boost.go; prepared settings have already validated the date.
// calendarMonth is the actual projection month, not a linked scenario's relative start.
func LivingSpendingBoostAtMonth(boost *models.LivingSpendingBoost, calendarMonth time.Time, cpi float64) float64
```

- [ ] **Establish impact and write the schedule oracle first.** Run the mandated incoming-call/reference checks before modifying existing symbols. Use start September 2026, base $10,000, constant phase multiplier 1.1, and boost $1,000 stopping September 2036. With CPI=1, planned living is $12,000 at months 0 and 119, and $11,000 at month 120. With CPI=2 at month 119, the same real $12,000 is nominal $24,000. No boost or an already expired saved boost leaves the original living schedule unchanged. Moving StartDate forward retains September 2036 as the end; it must not restart ten years.
- [ ] **Specify validation and persistence.** Nil disables the boost. A non-nil schedule requires finite positive amount at real-cent precision and a valid `models.ParseYearMonth` stop month. Existing settings may contain an expired schedule, which is inactive; creating a new active optimizer request requires a stop after the current start. Let a stop after the modeled horizon remain valid and disclose that its expiry is outside the horizon. Deep clones, JSON round trips, scenario switching, and ordinary form saves must retain the schedule. Clearing it requires the explicit disabled choice in a new preview, followed by Apply.
- [ ] **Run the failing schedule tests.** `rtk proxy go test ./internal/models ./internal/services/retirement/prepare ./internal/services/retirement/engine -run 'TestLivingSpendingBoost' -count=1`.
- [ ] **Implement one schedule calculation.** Return zero for nil schedules and `MonthlyReal*cpi` only while the supplied projection calendar month is before `StopMonth`; use existing date parsing and supplied path CPI. Derive that calendar month from the projection's time axis, including when active settings come from a chain link; do not restart timing from the linked scenario's StartDate. Add this amount after the base's phase/decline calculation, before guardrails and the absolute floor. In `StepMonth`, retain `CurrentLivingExpenses` as the evolving base alone and use a local total planned-living value for guardrail event amounts, floor application, planned totals, and `MonthOutcome.LivingExpenses`. Never accumulate the boost back into base state. Factor the existing base calculation into a private base-only helper, and use it for `NewProjectionState` and any chain-transition base initialization: those paths must not initialize `CurrentLivingExpenses` from the now boost-inclusive total helper. Add the boost only at the final planned-living boundary. The deterministic expense helper calls the same boost helper using ordinary inflation CPI, not net inflation after spending decline.
- [ ] **Prove all engine paths agree.** Verify canonical projection, Monte Carlo, and backtest receive the boost exactly once through shared stepping. Cover first-month initialization, phase and no-phase/decline branches, stochastic CPI, year boundaries, boost expiry, replay from prepared inputs, and existing chain transitions using the original projection calendar. Optimizer requests still reject chains, but saved settings must not break ordinary engine chaining. An `ExpenseSource` is not a substitute: it is outside living guardrail cuts. Confirm a guardrail multiplier of 0.9 reduces a real $12,000 combined request to $10,800; a real floor of $11,000 then clamps it to $11,000. With no shortfall, expiry from $12,000 to $11,000 causes no below-plan month. Retain existing engine regression results for nil schedules.
- [ ] **Re-run the scoped tests and inspect the diff.** Subsequent Tasks 1–6 must consume this same schedule. Do not proceed with a boost implemented only in the optimizer or a graph.

## Task 1: Define candidates and prepare exactly the plan that will be saved

**Files:**
- Create `internal/models/spending_optimizer.go`.
- Create `internal/services/retirement/analysis/spending_candidate.go`.
- Create `internal/services/retirement/analysis/spending_candidate_test.go`.
- Read `internal/models/whatif.go`, `internal/services/retirement/prepare/prepare.go`, and `internal/services/retirement/engine/loop_helpers.go`.

**Interfaces:** Add these types in `models`, with explicit snake_case JSON tags for every exported field. Monetary fields are dollars; step construction and comparisons use integer cents.

```go
type SpendingOptimizerRequest struct {
    FloorMonthlyReal float64
    NearTermYears int // default 5; accepted range 1–10
    LivingSpendingBoost *LivingSpendingBoost // fixed request schedule; nil disables
    SearchMinMonthlyReal float64 // actual starting living budget
    SearchMaxMonthlyReal float64
    SearchStepMonthlyReal float64
    Seed int64
}

type SpendingCandidate struct {
    ID string
    Kind string // current, planned, flexible
    BaseMonthlyLivingExpenses float64 // value saved to settings
    StartingMonthlyLivingReal float64 // verified canonical first month
    Guardrails *GuardrailConfig
    LivingSpendingBoost *LivingSpendingBoost // exact schedule saved on Apply
    Baseline bool
    Qualifies bool
    Metrics *SpendingRiskMetrics // defined in Task 2
    SimulationYears []GuardrailSimulationYear
    WorstPathYears []GuardrailPathYear
    WorstPathIndex int // zero-based index in final stream
}
```

The task introduces the `SpendingRiskMetrics` definition from Task 2's interface block at the same time, so this task compiles independently; it does not implement aggregation yet.

In `analysis` expose:

```go
func PrepareSpendingCandidate(in engine.Input, c models.SpendingCandidate) (engine.Input, error)
func spendingCandidateForStart(in engine.Input, start, floor float64,
    boost *models.LivingSpendingBoost, policy *models.GuardrailConfig,
    kind, id string) (models.SpendingCandidate, error)
```

- [ ] **Write the independent phase-mapping and preservation oracle.** Use default settings with `MonthlyLivingExpenses=8000`, a single enabled spending phase with `StartAge=0, Multiplier=1.1`, and a $7,000 floor. A requested actual start of $11,000 must map to base $10,000 and canonical month-zero planned living of $11,000. Keep tax settings, income sources, phase configuration, withdrawal strategy, and all hooks unchanged. Add non-unit and below-one phase multipliers, current spending below the desired floor, and non-finite/zero phase factors. With the same phase and an active $1,000 boost, requested start $12,000 must still map to base $10,000. Derive the unit-base factor with the boost disabled, then subtract the month-zero boost before division; a requested start that leaves a non-positive base is invalid. Preserve an unchanged current baseline with its original saved boost, even when the request changes or removes it.

```go
func TestSpendingCandidateMapsPhaseAdjustedStart(t *testing.T) {
    s := models.DefaultWhatIfSettings()
    s.MonthlyLivingExpenses = 8000
    s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true,
        Phases: []models.SpendingPhase{{Name: "Go-Go", StartAge: 0, Multiplier: 1.1}}}
    in := engineInput(t, s)
    g := &models.GuardrailConfig{Enabled: true, MinSpendingPct: 100,
        MaxSpendingPct: 100, MinMonthlySpendingReal: 7000}
    c, err := spendingCandidateForStart(in, 11000, 7000, nil, g, "planned", "p-11000")
    if err != nil { t.Fatal(err) }
    out, err := PrepareSpendingCandidate(in, c)
    if err != nil { t.Fatal(err) }
    if math.Abs(out.Prepared.Settings().MonthlyLivingExpenses-10000) > .005 {
        t.Fatal("candidate did not invert the active phase multiplier")
    }
    if math.Abs(engine.LivingExpensesAtMonth(out.Prepared.Settings(), 0)-11000) > .005 {
        t.Fatal("displayed starting budget differs from prepared candidate")
    }
    if in.Prepared.Settings().MonthlyLivingExpenses != 8000 {
        t.Fatal("candidate preparation mutated the original plan")
    }
}
```

- [ ] **Run the failing oracle.** `rtk proxy go test ./internal/services/retirement/analysis -run '^TestSpendingCandidate' -count=1` must fail because the new contract is absent.
- [ ] **Implement candidate preparation.** Deep-copy using `prepare.Clone`, set only `MonthlyLivingExpenses`, a copied `LivingSpendingBoost`, and a copied `GuardrailConfig`, prepare through `prepare.From`, and retain the full `engine.Input` by assigning only its `Prepared` field. Reject zero prepared input and chained scenarios. `spendingCandidateForStart` derives the active factor from `engine.LivingExpensesAtMonth` on a cloned, prepared unit-base setting with its boost disabled; reject non-positive/non-finite factors. Map `base=(start-activeBoostAtMonthZero)/factor` and validate the resulting positive base. Retain full base precision for an exact displayed cent value, and verify it against the canonical projection. Do not confuse nominal year-one averages with month-zero living dollars.

```go
cloned, err := prepare.Clone(in.Prepared.Settings())
if err != nil { return engine.Input{}, err }
cloned.MonthlyLivingExpenses = c.BaseMonthlyLivingExpenses
cloned.Guardrails = guardrailOptimizerCloneConfig(c.Guardrails)
cloned.LivingSpendingBoost = models.CloneLivingSpendingBoost(c.LivingSpendingBoost)
prepared, err := prepare.From(cloned)
if err != nil { return engine.Input{}, err }
out := in
out.Prepared = prepared
return out, nil
```

- [ ] **Verify the no-extra-cuts policy.** Set `Enabled=true`, `FloorCutPct=0`, `CeilingRaisePct=0`, `MinSpendingPct=100`, `MaxSpendingPct=100`, and the absolute floor. Retain valid positive trigger thresholds. Seeded projections must never cut below their planned schedule, while a phase schedule that crosses the floor is clamped by the existing engine. Original/current-plan baselines remain entirely unchanged; their selected minimum is observational only.
- [ ] **Re-run the task tests and inspect the diff.** Success means exact phase-aware mapping and immutable inputs, including hook preservation. JSON round trips must preserve the complete candidate base budget, boost amount/stop month, and policy.

## Task 2: Measure funded spending, cuts, and minimum failures month by month

**Files:**
- Create `internal/services/retirement/analysis/spending_experience.go` and `spending_experience_test.go`.
- Create `internal/services/retirement/analysis/spending_risk.go` and `spending_risk_test.go`.
- Modify `internal/models/spending_optimizer.go`, `internal/models/whatif.go` (`MonteCarloResult` only), and `internal/services/retirement/analysis/monte_carlo.go`.
- Read `floor_outcomes.go`, `lifestyle_outcomes.go`, and `engine/spending_floor.go`; preserve their existing semantics.

**Interfaces:**

```go
// models/spending_optimizer.go
type SpendingPathOutcome struct {
    MonthsObserved, NearTermMonths int
    NearTermFundedLivingReal float64 // sum, not average
    MinFundedMonthlyReal float64
    MinFundedMonth int // one-based
    FloorShortfallMonths, LongestFloorShortfallMonths int
    LargestFloorGapReal float64
    UnpaidObligationMonths int // shortfall exceeded adjusted living; obligations unpaid
    DepletionMonth int // one-based first legacy depletion event; 0 when never recorded
    FloorShortfallMonthsAfterDepletion int
    FirstCutMonth, MonthsBelowPlan, LongestBelowPlanMonths int
    MaxCutReal, MaxCutPct float64
    BelowPlanAtEnd bool
}

type SpendingRiskMetrics struct {
    Runs, CutPaths, EarlyCutPaths, FloorShortfallPaths, UnpaidObligationPaths int
    DepletionPaths, DepletionFloorFundedPaths, CutPathsStillBelowPlan int // DepletionFloorFundedPaths: minimum funded from first legacy depletion event through horizon, including event month; no income-only attribution
    MedianNearTermMonthlyReal, P10NearTermMonthlyReal float64
    MedianFirstCutMonth *float64 // nil when no paths have cuts
    MedianMaxCutReal, P95MaxCutReal, MedianMaxCutPct, P95MaxCutPct *float64
    MedianMonthsBelowPlan, P95LongestBelowPlanMonths *float64
    LowestObservedMonthlyReal, LargestFloorGapReal float64
    LowestObservedMonth, WorstPathIndex, LongestFloorShortfallMonths int
    FloorEvidence GuardrailOptimizerMetrics // existing counts, intervals, lifetime totals
}

// Add to MonteCarloResult; omitted unless observation was requested.
SpendingOutcome *SpendingPathOutcome `json:"spending_outcome,omitempty"`

// Add to MonteCarloConfig; zero preserves existing behavior/results.
SpendingExperienceYears int

// analysis/spending_experience.go
type spendingMonthObservation struct {
    Planned, Adjusted, Funded, Shortfall, CPI float64 // nominal except CPI
    Depleted bool // sticky legacy flag; not necessarily zero assets thereafter
}
func newSpendingExperienceTracker(floor float64, nearYears int) *spendingExperienceTracker
func (t *spendingExperienceTracker) observe(m spendingMonthObservation)
func (t *spendingExperienceTracker) result() *models.SpendingPathOutcome

// analysis/spending_risk.go
func summarizeSpendingRisk(rows []models.MonteCarloResult, nearYears int) (*models.SpendingRiskMetrics, error)
```

- [ ] **Write a hand-calculated monthly oracle before wiring Monte Carlo.** Use floor $90 and seven monthly nominal observations with CPI fixed at 1 and planned $100 each month. Adjusted/funded/shortfall per month: `(100,100,0) (100,100,0) (80,80,0) (80,80,0) (100,100,0) (71,70,1) (100,0,150)`, with the engine depletion flag set from month 6 on. Month 6 is a shortfall absorbed by living: below plan and below the minimum, but obligations were paid, so it is not an unpaid obligation. Month 7 is a shortfall exceeding living: funded living is zero and obligations went unpaid. Expected: funded sum $530; minimum $0 in month 7; first cut month 3; four below-plan months with a longest run of two; four below-minimum months with a longest run of two; maximum cut $100 and 100%; largest minimum gap $90; exactly one unpaid-obligation month; depletion month 6 with two below-minimum months from it on; below plan at the end. Repeat the same real amounts with nominal amounts and CPI both doubled. Add a second fixture with the flag set from month 3 and the minimum funded in every month from month 3 through the horizon. Also test planned $100, adjusted $120, shortfall $10, funded $110, and floor $90: no below-plan cut, minimum failure, or unpaid other obligation, despite the reduced adjusted request.

```go
func TestSpendingExperienceHandCalculatedMonths(t *testing.T) {
    tr := newSpendingExperienceTracker(90, 1)
    type obs struct{ adjusted, funded, shortfall float64; depleted bool }
    months := []obs{{100, 100, 0, false}, {100, 100, 0, false}, {80, 80, 0, false},
        {80, 80, 0, false}, {100, 100, 0, false}, {71, 70, 1, true}, {100, 0, 150, true}}
    for _, m := range months {
        tr.observe(spendingMonthObservation{Planned: 100, Adjusted: m.adjusted,
            Funded: m.funded, Shortfall: m.shortfall, CPI: 1, Depleted: m.depleted})
    }
    got := tr.result()
    if got.NearTermFundedLivingReal != 530 || got.MinFundedMonthlyReal != 0 ||
        got.MinFundedMonth != 7 || got.FirstCutMonth != 3 ||
        got.MonthsBelowPlan != 4 || got.LongestBelowPlanMonths != 2 ||
        got.FloorShortfallMonths != 4 || got.LongestFloorShortfallMonths != 2 ||
        got.MaxCutReal != 100 || got.MaxCutPct != 100 || got.LargestFloorGapReal != 90 ||
        got.UnpaidObligationMonths != 1 || got.DepletionMonth != 6 ||
        got.FloorShortfallMonthsAfterDepletion != 2 || !got.BelowPlanAtEnd {
        t.Fatalf("monthly oracle mismatch: %+v", got)
    }
}
```

- [ ] **Run failing observer tests.** `rtk proxy go test ./internal/services/retirement/analysis -run '^TestSpendingExperience' -count=1`.
- [ ] **Implement the independent observer.** Round each real living amount with `engine.RoundLivingCents`. Compare funded living with that month's pre-policy `planned/CPI` to identify a month below its planned schedule; a difference of at least one real cent counts. This excludes scheduled phase/decline reductions while including actual unfunded reductions. Do not raise this comparison reference to the selected floor: an unchanged baseline that plans $80 and funds $80 against a $90 minimum has a minimum failure, but no below-plan cut. Separately count below-floor months. An unpaid-obligation month is one whose shortfall exceeds adjusted living at real-cent precision, following `engine.FundedLiving`'s obligations-first attribution. An absorbed shortfall counts as a cut only when funded living falls below planned living; a raised budget may remain above plan. Unpaid other obligations also cause a minimum failure for the positive floor. Record the first depleted month and count floor-shortfall months from that month on. Sum only the first `nearYears*12` funded observations for the objective, but observe the full horizon. Set first/minimum month indices once, keep earliest ties, and maintain consecutive-run counters.

```go
fundedReal := engine.RoundLivingCents(m.Funded / m.CPI)
plannedReal := engine.RoundLivingCents(m.Planned / m.CPI)
belowPlan := fundedReal < plannedReal
belowFloor := fundedReal < engine.RoundLivingCents(t.floor)
unpaid := engine.RoundLivingCents((math.Max(0, m.Shortfall)-m.Adjusted)/m.CPI) > 0 // obligations unpaid only past living
// on the first observation with m.Depleted, record the one-based month as DepletionMonth
```

`spendingExperienceTracker` owns `floor`, `nearYears`, current consecutive counters, and a `models.SpendingPathOutcome` value. Invalid/non-finite values must make the result invalid for aggregation; never silently turn them into successful observations. Preserve errors in an internal validity flag and return nil when invalid.

- [ ] **Wire observation into the existing monthly loop.** After impact analysis, create the tracker only for positive `SpendingExperienceYears` and a positive floor. Feed it `out.LivingExpenses` (pre-policy planned), `out.AdjustedLivingExpenses`, `out.FundedLivingExpenses`, `out.Result.Shortfall`, `st.CumulativeInflation`, and the loop's `depleted` flag; call the tracker after the loop's depletion check so the flag describes the current month. These fields are returned by `StepMonth` in `engine/stepper.go` and mapped to projection output in `engine/month.go`. Do not change RNG draw order, `StepMonth`, tax inputs, adaptation semantics, or depletion stopping conditions. The new scenario runner always enables the existing floor observer, which already keeps depleted paths running.
- [ ] **Add aggregation tests with explicit outcomes.** Four complete 12-month paths with funded averages `[100,200,300,400]` yield median $250 and P10 $130 using linear interpolation. Include two minimum-failure paths, one of which also has an unpaid other obligation; require floor-path count 2 and unpaid-path count 1. Reject observations asserting an unpaid other obligation without a minimum failure for the positive floor. A shortfall reducing living below plan but leaving it above the floor is a cut only. In a separate full-length fixture, a first depletion event at month 100 with the minimum funded from month 100 through the horizon counts in both `DepletionPaths` and `DepletionFloorFundedPaths`. Failure in month 100 excludes the latter even if subsequent months recover; do not insert month 100 into the four 12-month quantile paths. Two cut paths with first-cut months `[3,9]` yield conditional median month 6, denominator 2/4. With zero cut paths, all conditional metrics are nil rather than 0 or NaN. Reject a 6-month capture when a full 12-month near-term period is required.
- [ ] **Implement aggregation and cross-checks.** `summarizeSpendingRisk` requires every result to have full spending and floor observations through `ProjectionYears*12` months. Cross-check its floor-path count with `guardrailOptimizerMetrics`. Use integer counts for qualification, preserve the existing Wilson interval as evidence, and rank worst path by minimum funded month, then greatest total below-floor months, then original result index. Keep percent and dollar distributions independent and labeled.
- [ ] **Run observer, risk, and compatibility checks.** `rtk proxy go test ./internal/services/retirement/analysis -run 'Test(SpendingExperience|SpendingRisk|Guardrail|Lifestyle)' -count=1`. Add seeded on/off comparisons proving that observation does not change balance, taxes, shocks, guardrail results, or annual funded values. A depleted-but-floor-funded fixture must not fail the minimum solely because `Survives=false`, nor count as an unpaid obligation. Preserve the sticky legacy flag if balances recover; do not interpret it as evidence that income alone paid later spending.

- [ ] **Keep scheduled expiry separate from cut experience.** Feed the observer the month's complete planned living after the boost schedule and before guardrails. In a zero-inflation fixture, funded/planned values $12,000/$12,000 before expiry and $11,000/$11,000 after expiry produce zero cut months; $10,500/$11,000 after expiry produces a $500 below-plan cut. Test a phase change at the same boundary. The minimum stays fixed in real dollars across all these months.

## Task 3: Search spending and rules together, then independently validate

**Files:**
- Create `internal/services/retirement/analysis/spending_optimizer.go` and `spending_optimizer_test.go`.
- Create `internal/services/retirement/analysis/spending_optimizer_runner.go` and `spending_optimizer_runner_test.go`.
- Extend `internal/models/spending_optimizer.go` with the result contract.
- Reuse, without changing legacy behavior, `guardrailOptimizerPolicies`, `guardrailOptimizerCloneConfig`, `guardrailOptimizerMetrics`, and `SummarizeGuardrailSimulationYears`.

**Interfaces:**

```go
type SpendingOptimizerResult struct { // package models
    Request SpendingOptimizerRequest
    SearchSeed, SelectionSeed, ValidationSeed int64
    SearchRuns, SelectionRuns, ValidationRuns int
    EvaluatedCandidates int
    EffectiveMinMonthlyReal, EffectiveMaxMonthlyReal, ResolutionMonthlyReal float64
    RangeLimited bool
    HorizonMinYears, HorizonMaxYears int
    Candidates []SpendingCandidate // every final-validated frontier row, ascending budget within Kind; discovery-only results are never returned
    RecommendationIDs []string
}

type spendingScenarioRunner func(context.Context, engine.Input, int64, int,
    models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error)
func OptimizeSpending(context.Context, engine.Input, models.SpendingOptimizerRequest) (*models.SpendingOptimizerResult, error)
func optimizeSpendingWithRunner(context.Context, engine.Input, models.SpendingOptimizerRequest,
    spendingScenarioRunner) (*models.SpendingOptimizerResult, error)
func runSpendingScenarios(context.Context, engine.Input, int64, int,
    models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error)
func spendingCandidateQualifies(c models.SpendingCandidate) bool
```

- [ ] **Build an injected-runner oracle for the search.** The runner returns complete, internally consistent floor/spending outcomes. Use a request with range $7,000–$15,000 and $1,000 steps so all nine discovery positions are known. Set a fixture so planned spending above $8,000 is not followed in full (at least one below-plan path), flexible spending up to $9,000 funds the minimum with cuts, and every amount above $9,000 breaches the minimum. Add a fixture with recorded depletion followed by living funded below plan but above the minimum: flexible qualifies, planned fails on cuts, and both depletion and post-event minimum-funded counts are reported. Add a zero-portfolio fixture that funds full planned living and other obligations: both classes qualify, since depletion alone does not reject a plan. Use near-term means that increase through these boundaries. Assert the service varies both budget and guardrail settings, returns the feasible $8,000 planned and $9,000 flexible levels, returns every selection-measured budget as a final-validated frontier row, and preserves the exact current plan baseline.
- [ ] **Add a second objective oracle.** A nominal $12,000 start followed by cuts produces first-five-year funded average $8,000; a $10,000 start produces $9,000. Assert the $10,000 candidate ranks first even if the other has more lifetime spending. A fixture where every candidate breaches the floor must return no recommendation without lowering the floor.

```go
func TestSpendingCandidateQualificationUsesCounts(t *testing.T) {
    c := models.SpendingCandidate{Kind: "flexible", Metrics: &models.SpendingRiskMetrics{Runs: 1000}}
    if !spendingCandidateQualifies(c) { t.Fatal("complete zero-failure evidence rejected") }
    c.Metrics.FloorShortfallPaths = 1
    if spendingCandidateQualifies(c) { t.Fatal("one failed future rounded away") }
    c.Metrics.FloorShortfallPaths = 0
    c.Metrics.UnpaidObligationPaths = 1
    if spendingCandidateQualifies(c) { t.Fatal("unpaid obligations treated as funded spending") }
    c.Metrics.UnpaidObligationPaths = 0
    c.Metrics.CutPaths = 1 // below plan in one future; minimum and obligations intact
    if !spendingCandidateQualifies(c) { t.Fatal("flexible option must tolerate cuts above the minimum") }
    c.Kind = "planned"
    if spendingCandidateQualifies(c) { t.Fatal("steady option qualified although the plan was not followed in one future") }
}
```

This function tests count acceptance after the aggregator has established completeness; baseline status controls Apply separately.

- [ ] **Run failing search tests.** `rtk proxy go test ./internal/services/retirement/analysis -run '^TestSpending(Optimizer|CandidateQualification)' -count=1`.
- [ ] **Implement request normalization and bounded grid generation.** Require finite positive range/step and a floor of at least $0.01, near-term years 1–10 and no longer than the shortest simulated horizon, `max>=min`, and a minimum search amount at least the floor that leaves a positive nonboost base. Validate boost amount/stop month server-side and preserve nil as disabled. Unlike the legacy optimizer, allow a desired floor above the current spending budget. The total starting request includes the active boost. Hold its amount and stop month fixed for every alternative. Defaults: search min=`max(floor, active month-zero boost + $0.01)` and max=`max(2*current actual start, 2*effective min)`; start with a $100 step and increase in whole $100 increments when needed to keep at most 201 regular lattice positions. Display the resulting step before submission and in results. Include exact endpoints; the unchanged current plan is always a separate comparison, even outside the range. Construct cents-based grid positions and evenly choose up to nine positions including endpoints. If an explicitly entered step would exceed the 201-position bound, return a request error asking for a coarser step or smaller range rather than silently truncating it.
- [ ] **Implement the discovery screen.** Measure at most nine evenly distributed lattice positions against all existing distinct enabled guardrail policies plus the fixed-multiplier planned policy, 32 shared discovery futures per pair. Discovery has one job: choose, per budget, the flexible policy with the highest near-term median among policies with zero floor and unpaid-obligation failures on the screen, falling back to fewest failures. It never establishes feasibility, and its metrics are never returned or displayed.
- [ ] **Implement selection at validation scale.** Run 1,000 fresh shared selection futures for each planned and chosen-flexible candidate (up to 18). Then refine each class between its highest passing selected budget and the next higher failing selected position. Evaluate at most two untested interior midpoints aligned to `req.SearchStepMonthlyReal` on the selection stream, updating the bracket after each measurement. Skip when no bracket or untested interior point exists. A flexible midpoint inherits the lower passing endpoint's guardrails and uses `spendingCandidateForStart` to map its new budget. At most four new candidates are measured; no new policy screen is launched. This is a bounded heuristic, not an exhaustive lattice search or a monotonicity proof. Equal sample sizes mean a candidate is not systematically failed by a larger final sample (spec requirement 15).
- [ ] **Freeze the frontier before validation.** The frontier is every selection-measured candidate (at most 22), including selection failures, plus the actual current plan. Final-stage class-specific counts determine qualification even when status changes from selection. Freeze each base budget, boost schedule, and policy, then measure every member with the same fresh 1,000 final futures. A member that fails at the final stage stays in the result as an inspection-only frontier row with its counts; the headline options are the highest-ranked frontier rows that pass, per class, using the funded near-term objective and the spec's tie-breaks. A row changing status on fresh draws is expected, not an error; do not promise that failures or budget movement will be small. Never change spending or policy based on final failures and retest on the same final sample.
- [ ] **Implement the scenario runner.** Derive per-run seeds in stable index order from the stage master seed, set `MinMonthlySpendingReal`, `CaptureSpendingYears=true`, `SpendingExperienceYears=req.NearTermYears`, and `AdaptiveSpending=false`. Reuse `RunSingleMonteCarloSimulation`, preserving hooks and stochastic assumptions. Bound worker count to `min(8, runtime.GOMAXPROCS(0))`, retain results in index order, check cancellation between jobs, and reject partial arrays. Reuse one recorded nonzero master seed with distinct domain-separated seeds for selection and final validation; expose the seeds as decimal strings in browser JSON to avoid JavaScript integer truncation.

```go
cfg := DefaultMonteCarloConfig()
cfg.MinMonthlySpendingReal = req.FloorMonthlyReal
cfg.CaptureSpendingYears = true
cfg.SpendingExperienceYears = req.NearTermYears
cfg.AdaptiveSpending = false
```

- [ ] **Prove independent evidence and exact baseline behavior.** Instrument the injected runner with `(candidateID, seed, runs, budget, boost, policy)` records. Assert no final-stage result generates a new candidate, same-stage seeds match across options, each baseline uses the unchanged input settings, all final candidates have 1,000 observations, and a seed replay returns identical results. Missing or incomplete data must return an error. Check early-crash, inflation, healthcare-shock, phase-change, and late-depletion real-engine fixtures.
- [ ] **Attach coherent evidence.** Derive annual summaries from final rows with `SummarizeGuardrailSimulationYears`; retain the worst row's `FloorOutcome.Years`, its `SpendingOutcome` minimum month, and its original index. Keep median lifetime spending in supporting details. Set `RangeLimited` when a displayed leader is on a search boundary; always expose actual tested range and resolution even when false.
- [ ] **Run search/runner tests and a benchmark.** `rtk proxy go test ./internal/services/retirement/analysis -run '^TestSpending' -count=1`. Add `BenchmarkSpendingOptimizer` with a representative 35-year fixture. The bound is approximately 55,000 full path runs (9×34×32 + 18×1000 + 4×1000 + 23×1000 = 54,792), depending on deduplication. The fixed boost schedule adds no search dimension or extra paths. The review reports 3.6 ms per surviving 35-year path and 0.3 ms per early-depleted path, but no benchmark command, fixture, or raw output is attached here. Treat these as sizing inputs until reproduced with full-horizon observation, hooks, and account structure. Measure worker scaling instead of assuming linear speedup. Confirm cancellation stops queued work and the worker limit holds. Record time and peak memory; do not claim the new search is as fast as the old one without evidence.

## Task 4: Retain complete previews and save base budget, boost, and policy atomically

**Files:**
- Create `internal/handlers/whatif/handlers_spending_optimizer.go` and `spending_optimizer_test.go`.
- Create `internal/handlers/whatif/handlers_spending_graph.go` and `spending_graph_test.go`.
- Modify `internal/handlers/whatif/handlers.go` for routes and new form data only.
- Read `handlers_guardrail_optimizer.go`, `handlers_guardrail_graph.go`, and `sync_test.go` for stale-preview and race contracts.

**Interfaces:**

```go
func handleSpendingOptimizer(http.ResponseWriter, *http.Request)
func handleCancelSpendingOptimizer(http.ResponseWriter, *http.Request)
func handleApplySpendingOptimizer(http.ResponseWriter, *http.Request)
func handleSpendingOptimizerGraph(http.ResponseWriter, *http.Request)
```

Register POST routes `/whatif/spending/optimize`, `/whatif/spending/optimize/cancel`, `/whatif/spending/optimize/apply`, and `/whatif/spending/optimize/graph`. Preserve existing guardrail routes.

Implementation clarification: also add read-only POST `/whatif/spending/optimize/prepare` with a thin `analysis.NormalizeSpendingRequest` wrapper in `spending_request.go`. This returns normalized min/max/step before comparison, with no simulation, preview tokens, RNG selection, or save. Task 5 uses it for the required pre-submission resolution display so client code does not duplicate normalization.

Define a private `spendingPreview` record with manager pointer, scenario filename, revision, full-settings SHA-256 fingerprint, expiry, cancel function, request, complete result, and separate graph/apply token maps whose values are copied `models.SpendingCandidate` values. Use a private mutex-protected registry. Expiry is 15 minutes and maximum retained previews 128, matching the existing contract; cap active searches to one per manager/scenario and reject concurrent work with HTTP 409.

- [ ] **Write a complete save oracle.** Start with base living $8,000 and existing guardrails. Retain an accepted candidate for base $10,000 with a $1,000 boost stopping September 2036 and a different absolute floor and policy. Submit only its opaque Apply token and request ID. Reload settings and assert all three settings match the candidate in one revision increment; every other serialized setting matches before/after. Also test removing an existing boost through an accepted nil-schedule candidate. Tampering with posted budget, boost, or policy values cannot change the retained candidate. Every qualifying final frontier candidate receives an Apply token, including non-headline choices. A failed candidate and the actual current baseline receive graph authorization only.

```go
form := url.Values{"request_id": {requestID}, "recommendation": {applyToken},
    "monthly_living_expenses": {"999999"}}
req := httptest.NewRequest(http.MethodPost, "/whatif/spending/optimize/apply",
    strings.NewReader(form.Encode()))
req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
w := httptest.NewRecorder()
handleApplySpendingOptimizer(w, req)
// Assert base $10,000, the exact boost amount/stop month, and full policy were saved,
// unrelated settings match, revision advanced once, and token replay is 409.
```

The fixture must create the preview through the real handler or seed the private registry using the exact retained candidate; never bypass `SaveWithRevisionIfScenario` in the handler under test.

- [ ] **Write rejection oracles.** Expiry, cancellation, changed revision, changed fingerprint at equal revision, changed active scenario, changed manager, replayed token, unknown token, non-finite form values, and chained scenarios all leave stored settings unchanged. Test a scenario switch between reading and saving using the existing race-test patterns.
- [ ] **Run failing handler tests.** `rtk proxy go test ./internal/handlers/whatif -run '^TestSpending(Optimizer|Apply|Graph)' -count=1`.
- [ ] **Implement preview lifecycle.** Parse and validate the request server-side; build input with `buildEngineInput` and `retirement.DefaultHooks()`. Retain a snapshot identity before search. Release the registry lock during analysis; re-lock and recheck the active entry and context before storing results and tokens. Use `context.WithTimeout` with an initial three-minute budget, measured in Task 6; timeout returns an incomplete-analysis error and no Apply tokens. Never leave a canceled result publishable. Error envelopes use 400 for invalid input, 409 for stale/canceled/busy state, and 500 for internal failure; cache headers are `no-store`.
- [ ] **Implement Apply with one settings-manager write.** Recheck manager, scenario, revision, fingerprint, expiry, and token; load current settings; assign the retained base budget, deep-copied boost schedule, and deep-copied full guardrail config; call `SaveWithRevisionIfScenario` once. Consume the preview after success, retain it only on a retryable save failure. Redirect with `HX-Redirect` to `/whatif?spending_applied=<revision>#spending-optimizer` and announce the saved spending plan, including any scheduled end of extra spending, on arrival.

```go
s.MonthlyLivingExpenses = candidate.BaseMonthlyLivingExpenses
s.LivingSpendingBoost = models.CloneLivingSpendingBoost(candidate.LivingSpendingBoost)
cfg := *candidate.Guardrails
s.Guardrails = &cfg
revision, err := p.manager.SaveWithRevisionIfScenario(s, p.scenario, p.revision)
```

Apply candidates always have a non-nil enabled guardrail config; reject a missing one before this block. Lock only long enough to reserve/consume a token so concurrent Apply requests cannot both succeed; the settings-manager revision check remains the final authority.

- [ ] **Implement graph parity.** Load a valid retained candidate, build the same input with `analysis.PrepareSpendingCandidate`, and run the canonical projection for the separately labeled base case. Simulated and worst-path views come from retained final evidence, without fresh RNG draws. Include floor, risk metrics, actual and base starting values, the boost amount/stop month, seed strings, annual path counts, and display-dollar mode. Task 4A adds the base-case income/withdrawal timeline to this same candidate graph response. Release registry locks while preparing/rendering; recheck preview validity before responding. Real and nominal charts must not mix units; the minimum in nominal mode grows with that path's inflation, or omit that line with an explicit label if inflation is not retained. Default to real dollars.
- [ ] **Re-run handler and existing guardrail preview tests.** Verify search, graph, and cancel do not save; a canceled slow search never overwrites a newer result. The save oracle must prove the post-Apply canonical budget equals the preview's budget, including a non-unit starting phase, active boost, and the month before/at its expiry.

## Task 4A: Explain income and withdrawals across the selected base case

**Files:**
- Create `internal/services/retirement/analysis/spending_funding.go` and `spending_funding_test.go`.
- Extend `internal/models/spending_optimizer.go` with timeline types and `internal/handlers/whatif/handlers_spending_graph.go`/`spending_graph_test.go` with the new graph payload.
- Read `engine/month.go`, `engine.CalculateMonthlyIncomeBreakdown`, and `models.ProjectionMonth` for field semantics. Do not change tax or cash-flow execution to populate a display.

**Interfaces:**

```go
// models; all monetary fields below are monthly amounts in today's dollars.
type SpendingFundingMonth struct {
    Month int // zero-based, matching ProjectionMonth
    SocialSecurityReal, OtherConfiguredIncomeReal float64
    TaxDeferredWithdrawalReal, TaxableWithdrawalReal, RothWithdrawalReal float64
    TaxesPaidReal, PlannedLivingReal, FundedLivingReal, HealthcareReal float64
}
type SpendingFundingTimeline struct {
    Months []SpendingFundingMonth
    ExpectedMonths int
    Complete bool
    EndReason string // horizon or base_case_projection_ended
}
// analysis; caller supplies the exact candidate and its canonical projection.
func BuildSpendingFundingTimeline(in engine.Input, projection *models.ProjectionResult) (*models.SpendingFundingTimeline, error)
```

- [ ] **Write a source-accounting oracle.** A month with CPI=2, Social Security $2,000, other configured income $1,000, tax-deferred/taxable/Roth withdrawals $3,000/$400/$100, taxes $800 including state tax $100, planned/funded living $5,000/$4,500, and healthcare $600 must display respectively $1,000/$500, $1,500/$200/$50, $400, $2,500/$2,250, and $300 real. These are labeled model measures, not an asserted balance equation. Add an RMD subset and a Roth conversion to the fixture; neither adds another income source. Set the legacy `TaxableWithdrawals` alias and assert it is not added to the real account withdrawals.
- [ ] **Test benefit timing and missing evidence.** Use an actual prepared income configuration with one benefit beginning midyear, a pension/other-income source, and the Social Security hook replacing manual Social Security entries. Require zero Social Security before the claim month and correct income exactly once afterward. Test CPI changes month by month, a partial final year, and a canonical result ending at depletion before benefits or the boost end. Reject invalid/nonfinite CPI or mismatched month order. Never fill omitted months with zeros or mark a truncated timeline complete.
- [ ] **Run failing timeline tests.** `rtk proxy go test ./internal/services/retirement/analysis -run '^TestSpendingFunding' -count=1`.
- [ ] **Build a presentation adapter over existing calculations.** Use `ProjectionMonth.SocialSecurityIncome` and the existing `engine.CalculateMonthlyIncomeBreakdown(in.Hooks, preparedSettings, month).OrdinaryIncome` for other configured income; do not label all non-Social-Security `TotalIncome` as pensions because that field also includes investment tax income. Use the three explicit `WithdrawalFrom...` fields, `TaxesPaid`, `PlannedLivingExpenses`, `FundedLivingExpenses`, `HealthcareExpense`, and the month's `CumulativeInflation`. Do not add `StateTaxPaid` again. RMDs are subsets of account movements, Roth conversions are transfers, and taxable gain amounts are not additional consumption income. Leave investment-income details in their existing accounting view unless their cash meaning is separately verified.
- [ ] **Label scope and aggregation honestly.** Title the view “Income and withdrawals over time — base case”; separate income, account withdrawals, taxes, and spending instead of presenting a stacked complete cash-flow waterfall. Deflate each observed month before annual aggregation. An annual average divides by the observed month count and labels partial years; do not divide incomplete years by 12. Keep monthly values available so claim timing is visible. Mark the boost's stop and benefit starts within the observed range. If canonical projection ended early, show the end date/reason and unavailable later months, while explaining that the separate Monte Carlo minimum evidence still covers complete horizons. This view does not certify dependable-income coverage.
- [ ] **Integrate and verify graph parity.** Construct the timeline from the same retained candidate input and canonical result used by Task 4. Recheck preview validity before returning it. Test current versus alternative schedules, graph switching, cancellation, and post-Apply replay; the timeline must not use current saved settings for a previewed alternative. Re-run scoped analysis and handler tests.

## Task 5: Present the spending decision and explain risk in living dollars

**Files:**
- Create `web/templates/components/whatif/spending_optimizer.html`.
- Create `web/templates/components/whatif/spending_optimizer_results.html`.
- Create `web/static/js/whatif-spending-optimizer.js`.
- Modify `web/templates/components/whatif/guardrails.html` to include `whatif-spending-optimizer` in place of the primary legacy optimizer.
- Create `internal/handlers/whatif/spending_optimizer_render_test.go` and `internal/handlers/whatif/testdata/spending_optimizer_browser.cjs`.
- Read `ACCESSIBILITY.md`, the current templates, `charts.js`, and the Impeccable craft-floor reference before UI edits.

**Interfaces:** Templates receive the normalized request, retained candidate rows, graph/apply tokens, current-plan values, and server-computed comparison text. The JS controller consumes the routes in Task 4 and the exact model JSON fields; it formats values but does not independently calculate financial acceptance or recombine percentile outcomes.

- [ ] **Write render tests around decisions.** Require “How much can I spend?”, “Minimum comfortable monthly living budget”, and “Compare spending options”. Require an empty minimum when no floor exists and the saved absolute floor when one exists. The main view must show monthly near-term funded spending, cuts, and minimum failures; lifetime totals and policy thresholds belong in a disclosure. Require a frontier table per option class with one row per final-validated budget, its sample count, and the minimum-funded-after-depletion count. Prohibit a default $7,500 value, “guaranteed”, and “risk-free” claims in the new surface. Rendering a nil metric must produce “Unavailable”, never 0%.

```html
<h3 id="spending-optimizer-heading" tabindex="-1">How much can I spend?</h3>
<label for="spending-minimum">Minimum comfortable monthly living budget</label>
<p id="spending-minimum-help">Include basic living costs and a fun allowance.
Healthcare, taxes, and other separately entered expenses are additional.</p>
<p>Recommendations must fund this minimum in every checked future.</p>
<button type="submit">Compare spending options</button>
```

- [ ] **Run failing render tests.** `rtk proxy go test ./internal/handlers/whatif -run '^TestSpendingOptimizerRender' -count=1`.
- [ ] **Keep manual controls compatible with saved optimizer plans.** Allow zero cut/raise percentages and finite nonnegative absolute floors without the old raw-base cap in the manual form and `handleWhatIfGuardrails`. This preserves fixed-multiplier planned policies and floors above a phase/boost-adjusted base, including after expiry. Keep trigger bounds and legacy optimizer behavior. Test exact unchanged-policy saves and floor/boost preservation after semantic caller checks.

- [ ] **Build the form and results using the incumbent design.** One required floor input, an optional “Extra spending early” disclosure with a monthly real amount and calendar stop month, plus an advanced disclosure for near-term horizon and range/step. Initialize the boost from saved settings; otherwise leave it disabled. Show the exact date range, explain that the stop month is the first month without extra spending, and distinguish it from the scoring horizon. Include active boost amounts in displayed starting totals. Associate labels/help/errors; keep a polite live status region. Render current plan plus up to two alternatives, then the frontier: one table per class, ascending budget, qualifying rows marked, wide tables scrolling inside their own container. Show the base slider value in supporting details when phases or an active boost make it differ from the actual starting budget. If the new minimum is above the current budget, explain that the search will evaluate higher budgets rather than rejecting it as the old optimizer does.
- [ ] **Make comparisons evidence-based.** Server-side text compares `MedianNearTermMonthlyReal`, not raw requested base settings. Emit an uplift only when both reference and alternative exist; no positive “more” claim for a negative/zero delta. Put conditional cut statistics next to their denominator. Use “No cuts observed” only for complete zero-cut evidence. Show “Below the minimum in X of N futures” and worst gap/duration for failures. Show “The model recorded depletion in X of N futures; in Y of those, your minimum stayed funded from that event through the end,” using final-validation counts. Explain the legacy event without attributing funding to income alone. Every qualifying frontier row has an Apply action. Show planned reductions from phases and boost expiry separately from below-plan cuts. A floor-clamped planned reduction is not a market-risk failure. Label the strategy as portfolio-trigger spending rules; do not claim this feature implements Guyton–Klinger or suggest a universal initial withdrawal rate.
- [ ] **Implement the controller with stale-response protection.** Initialize idempotently on initial load and relevant HTMX swaps; attach one set of handlers per root. Track request and graph generations with `AbortController`. Editing any input cancels/invalidates previews and their Apply buttons, clears stale graphs, and updates matching accessibility/live-region state. Cancel calls the server and aborts fetch. After HTMX replacement, focus the outcome heading or first validation error. Theme changes re-theme charts; clear Plotly instances on replacement and honor reduced motion.

```js
const responseGeneration = ++generation;
controller?.abort();
controller = new AbortController();
const response = await fetch('/whatif/spending/optimize', {
  method: 'POST', body: new URLSearchParams(new FormData(form)), signal: controller.signal
});
if (responseGeneration !== generation || !root.isConnected) return;
```

Wrap fetch/error/finally handling so canceled or stale responses cannot publish results or restore Apply. Do not rely on client invalidation alone; Task 4 owns server authorization.

- [ ] **Build the spending graph first.** Reuse Plotly theme helpers for real-dollar median/P10/P90 annual spending and a constant real minimum. Put the selected coherent worst path behind a clearly labeled view control, with its exact minimum month and amount next to the plot. Keep portfolio and base case as secondary views. Add Task 4A’s income/withdrawal timeline with benefit-start and boost-end markers, gross-versus-tax labels, accessible monthly values, partial-year labels, and a visible stop when the base case ends early. Every chart has a matching accessible values table, sample counts, units, and text explaining annual averages versus monthly floor checks. Do not use a stitched pointwise lower band as a single worst future.
- [ ] **Add browser arithmetic and interaction oracles.** Use a handler-produced fixture with current near-term spending $8,000 and alternative $9,000, 250/1,000 cut paths, and median first cut month 36. Require a $1,000 uplift, 25% cut chance, and year 3 timing with the 250-path denominator. With 120 depletion-event paths of which 90 maintain the minimum from the event month through the horizon, require that sentence with exact counts and no income-only claim. Test the optional boost controls, preview invalidation on amount/end edits, a planned expiry that is not labeled a cut, benefit-start timing, and a truncated income timeline. Test Apply on a qualifying non-headline row. Verify zero-cut/nil-stat cases, failed minimum, no recommendation, exhausted range, cancel, stale result, graph switching, theme switching during load, keyboard-only Apply, and mobile overflow. No independently rounded values may contradict counts or acceptance.
- [ ] **Run render/browser verification and accessibility review.** Use the copied-data server in Task 6. Audit against all applicable numbered requirements in `ACCESSIBILITY.md`, including chart alternatives and complete clearing of hidden/live status after invalidation. Run the Impeccable mechanical detector once after completing the UI. Fix the batched findings and confirm once; no open-ended polish loop.

## Task 6: Prove the end-to-end financial and UI contract

**Files:**
- Create `internal/services/retirement/analysis/spending_optimizer_integration_test.go`.
- Extend the new handler/browser tests from Tasks 4–5.
- Record findings in `docs/superpowers/plans/2026-09-10-spending-first-optimizer-verification.md` during execution, with actual command results and observed timings.

**Interfaces:** Consume the completed public `OptimizeSpending`, preview routes, canonical projection, and saved-settings reload. Produce reproducible evidence for all release criteria below.

- [ ] **Run independent financial fixtures.** Test steady returns, early crash, high inflation, healthcare shock, phase decline crossing the minimum, and a long modeled life. Include timed-boost expiry before/after benefit claiming, simultaneous phase/boost changes, stochastic inflation, and a planned boost that becomes unaffordable but may be cut without breaching the floor. Keep configured healthcare/care obligations in the fixture; do not substitute the video’s generic dollar examples. Use planned/adjusted living $100, floor $90, other obligations $20, and zero portfolio in a hand-calculated monthly fixture. Net available income $115 funds living $95: flexible qualifies, planned fails on cuts. Income $105 funds $85: both fail the minimum with no unpaid other obligation. Income $15 funds living $0 and leaves $5 unpaid other obligations: both counters increment on the same path. Income $120 funds full planned living and obligations: both classes may qualify despite zero portfolio. Extend each into a full-horizon engine fixture preserving these accounting expectations. A single failed cent/month/path prevents recommendation. Use prepared settings for oracles and compare hand-computed observer fixtures with actual simulated captures.
- [ ] **Verify the original goal with a deterministic scenario-runner fixture.** Demonstrate a flexible candidate with higher funded near-term spending than the planned option, visible cuts later, and zero floor/unpaid-obligation failures. The test is not allowed to pass merely by showing higher requested spending or by shortening observation. Also demonstrate an infeasible minimum with visible failures and no Apply action.
- [ ] **Verify full preview-to-Apply equivalence.** Produce a real preview on a copied scenario, inspect its graph, apply it, reload persisted settings, and run the canonical projection again. Base and actual starting spending, boost amount and fixed stop month, guardrails, and floor must match the preview to cent precision. The canonical income/withdrawal timeline must replay identically over its observed range, and scheduled expiry must be distinct from market cuts. Re-run final simulations with the recorded seed to reproduce published risk counts. A later ordinary form save must preserve the saved absolute floor and boost schedule; advancing the plan start must not move the boost stop date.
- [ ] **Measure runtime and resource limits.** Record representative 10-, 35-, and 45-year runs, default-grid candidate counts, memory use, and cancellation latency. Confirm the eight-worker bound, one active search per manager/scenario, and bounded preview storage. The initial three-minute timeout must be sufficient for the representative 35-year plan on the supported host; if it is not, report the evidence and adjust search engineering before release. Do not reduce final 1,000-path evidence or return an unlabeled partial optimum to pass a speed check.
- [ ] **Launch the browser environment with the repository skill.**

```bash
rtk proxy scripts/whatif-verify.sh start
```

Open `http://localhost:8099/whatif` through the available browser skill. Check desktop and narrow mobile widths in both themes, complete the new flow, and cover `whatif-verify` regressions: populated taxes tile, red/rose cost items, and scenario/tab persistence after refresh. Test against copied data only.

- [ ] **Run full verification without filtered pipelines.** Execute sequentially, stopping and investigating any failure:

```bash
rtk proxy go build ./...
rtk proxy go vet ./...
rtk proxy go test ./...
rtk proxy staticcheck ./...
rtk proxy go test -race ./internal/services/retirement/analysis ./internal/handlers/whatif
rtk git diff --check
rtk git diff --stat
rtk git diff
```

The race check is warranted by bounded workers and preview/token lifecycle changes. Do not repeat the full suite after it passes unless subsequent changes or unresolved evidence require it.

- [ ] **Stop the copied-data server and record evidence.**

```bash
rtk proxy scripts/whatif-verify.sh stop
```

Record test commands and actual exit results, seeded fixture identities, measured runtime, desktop/mobile/theme evidence, accessibility findings, final file scope, and any unresolved limitation. Keep the legacy optimizer regressions passing during migration; do not delete tests merely because the new surface uses different labels.

## Release acceptance checklist

- [ ] Starting budget and guardrail policy are both searched, and actual funded near-term spending determines ranking.
- [ ] The comfortable minimum is explicit, inflation-adjusted, and never silently relaxed.
- [ ] More spending is quantified against a real measured comparison, with no uplift invented when the comparison is unavailable.
- [ ] Cut likelihood, timing, depth, duration, minimum failures, and unpaid obligations are visible with correct denominators.
- [ ] The minimum is checked monthly through each full horizon, including after depletion; a percentile chart is not used as the acceptance oracle.
- [ ] Discovery/selection and final evidence are separate; finalist settings are frozen before final validation.
- [ ] The steady option is followed in full in every checked future; unpaid obligation means shortfall exceeding living, never any shortfall.
- [ ] Every frontier row comes from the final stream at the same sample size, and selection ran at validation scale.
- [ ] Phase/boost-adjusted displayed starting dollars, preview graphs, and persisted base settings reconcile.
- [ ] The optional boost is cuttable, CPI-adjusted, saved with a fixed stop month, and included exactly once across engines; its expiry is a planned reduction, not a risk cut.
- [ ] The base-case income/withdrawal timeline shows benefit timing and taxes without double-counting, and labels unavailable months after an early end.
- [ ] Current rules are identified accurately; full Guyton–Klinger and advertised withdrawal-rate assumptions have not slipped into scope.
- [ ] Apply changes the complete selected spending plan atomically and rejects stale, canceled, replayed, or tampered previews.
- [ ] No result is advertised as a global maximum or a guarantee; “worst” refers to the identified observed path/sample.
- [ ] Required Go, race, browser, and accessibility checks pass, with performance evidence within the declared bound.

## Plan review notes

Coverage mapping: timed living schedule and engine parity → Task 0; spec inputs and defaults → Tasks 1/3/5; full-horizon minimum and cut evidence → Task 2; joint search/independence → Task 3; immutable preview and complete Apply → Task 4; base-case income/withdrawal accounting → Task 4A; graphs/copy/accessibility → Task 5; engine parity/performance/regressions → Task 6.

The deliberate scope decision is that “worst case” means every tested future, not dependable-income coverage under all conceivable conditions. That assumption is visible in both documents and in the planned UI. No financial engine changes, application implementation, commits, or deployment were performed while writing this plan.

Revision 2026-09-10, after review against engine semantics: unpaid obligation redefined as shortfall exceeding adjusted living, because `engine.FundedLiving` funds obligations first and any-shortfall counting rejected the depleted-but-income-funded outcome this feature exists to explore; the steady class now requires zero below-plan paths and every selection-measured budget is validated and shown as a frontier row, because otherwise the steady option is unbounded whenever income covers the minimum; selection runs at the 1,000-path validation size, with runtime still to be established by a reproducible full-horizon benchmark.


Follow-up review: corrected overlapping minimum/unpaid outcomes and impossible fixtures; distinguished legacy depletion events from zero assets and income-only funding; specified final-stage frontier qualification, midpoint policy inheritance, custom-step alignment, and Apply for qualifying non-headline choices. Aggregate count equality cannot establish an income-coverage regime. Fresh-seed budget variation is not known to be one grid step. No application code changed.

Video follow-up, 2026-09-10: added a fixed-calendar early-spending boost to the shared living schedule (Task 0), carried it through search/previews/atomic Apply, and added the base-case income/withdrawal timeline (Task 4A). Scheduled expiry is distinct from a below-plan cut. The minimum, full-horizon Monte Carlo evidence, matched sample sizes, and all qualifying frontier Apply actions remain intact. Full Guyton–Klinger and withdrawal-rate examples remain outside this implementation. This revision updates planning documents only.
