# Tax Optimizer (Phase 1)

## Problem

SimpleBudget models taxes accurately (LTCG, NIIT, IRMAA, SS taxation, RMD, penalties, ordinary brackets) and lets users configure a single fixed-amount Roth conversion ladder via `RothConversionConfig`. But it does not *search* for the best tax-optimization strategy. Users must hand-tune Roth conversion amounts, conversion windows, and SS claim ages to find an outcome they like — and the SS Portfolio Optimizer currently ranks SS claim ages without considering Roth conversions, so its "optimal" pair may be wrong once a Roth ladder is in play.

For a portfolio that is heavily tax-deferred (the common case), this leaves substantial real ending-portfolio value on the table.

## Scope

Phase 1 adds a synchronous **Tax Optimizer** that:

1. Takes the top-3 SS claim-age pairs from the existing `SSPortfolioAnalysis` as candidate SS configurations.
2. Enumerates ~45 Roth conversion strategies (mix of fixed-amount ladders and bracket-fill).
3. Runs a deterministic projection for every `(SS pair × Roth strategy)` combination (~135 projections total).
4. Ranks by **real (inflation-adjusted) ending portfolio**.
5. Refines the top-5 with a small Monte Carlo budget for sequence-of-returns robustness.
6. Surfaces the best strategy plus top-5 alternatives in a new collapsible panel on the what-if page.

The optimizer is **read-only**: it never mutates saved settings. The user reviews the recommendation and manually adjusts their scenario.

## Non-Goals

1. **Withdrawal-order strategy.** Adding strategy support to `WithdrawForExpenses` requires parameterizing the engine's hardcoded withdrawal order (RMD → taxable → Roth → tax-deferred discretionary). That refactor is Phase 2 and gets its own design doc.
2. **Survivor-spouse modeling.** The scoring is the joint MFJ projection only. Modeling MFJ→single filing-status transitions on death of one spouse would change rankings (especially for large age gaps), but is a separate modeling change.
3. **One-click apply.** No "apply this strategy" button; no auto-creation of sibling scenarios. The optimizer is informational.
4. **Async / background jobs.** Wall-time budget is small enough for synchronous execution (~15s worst case). No polling endpoint, no progress bar.
5. **State-by-state tax optimization.** State tax is taken as configured in `TaxConfig.StateIncomeTaxRate`. The optimizer does not search across states.
6. **HSA, SEP-IRA, Roth 401(k) in-plan-conversion modeling.** Strategies operate on the existing tax-deferred / Roth / taxable bucket model only.

## Eligibility

Show the Tax Optimizer panel only when all of:

1. `settings.TaxConfig != nil && settings.TaxConfig.FilingStatus != ""`
2. `settings.TaxDeferredPercent * settings.PortfolioValue / 100 >= 100_000`
3. `settings.CurrentAge < 73` (post-RMD-age optimization is degenerate; conversions after RMDs start typically destroy value)
4. `settings.ProjectionYears >= 5`

When ineligible, render a one-line reason message in the panel slot. Do not omit the panel silently — the explanation is part of the UX.

When `SSPortfolioAnalysis == nil` (e.g. single filer, or SS Portfolio ineligibility), fall back to using the scenario's *current* SS settings as the only SS pair. The optimizer still runs; the SS sweep is just degenerate.

## Design Decisions

### Maximize Real Ending Portfolio

Score every candidate by real (inflation-adjusted) ending portfolio at the end of the projection. This implicitly penalizes tax drag (every tax dollar is a non-compounding dollar) without requiring a separate tax-minimization objective. Matches the SS Portfolio Optimizer's existing precedent of ranking by portfolio impact, and matches the user's typical mental model ("what gives me the most money at the end?").

Lifetime tax paid and peak marginal bracket are computed *as additional reporting columns* but are not part of the ranking function.

### Sequential Decomposition With Top-3 SS Pairs

A full joint search over SS × Roth × bracket-fill × withdrawal-order is 50k–500k projections. A pure greedy (best SS pair, then sweep Roth) is ~50 projections but misses the SS × Roth interaction — e.g., a delayed-SS strategy with a large Roth ladder bridging the gap may beat the best SS-only pair.

The compromise is **top-K joint with K=3**:

1. From `SSPortfolioAnalysis`, extract the top-3 SS pairs by survival rate.
2. For each, sweep ~45 Roth strategies.
3. Total ~135 deterministic runs — sync-friendly, captures the dominant SS × Roth interaction at modest cost.

If a future test (`TestTaxOptimizer_PrefersJointOverGreedy`) cannot construct a realistic scenario where the K=3 grid beats greedy, the top-K grid is not earning its complexity and we drop to K=1.

### Deterministic Ranking + Monte Carlo Tiebreak Of Top-5

A 135-candidate Monte Carlo grid (with even 100 paths each) is ~14,000 projections — too slow for sync. A pure deterministic ranking is fast but blind to sequence-of-returns risk on close-ranked candidates.

Hybrid: deterministic ranking selects top-5, then small MC budget (32 paths default, tunable constant `taxOptimizerMonteCarloRuns`) re-scores those 5 by median ending portfolio. Final ordering uses MC median; deterministic score is preserved as a reporting column.

### Bracket-Fill As First-Class Strategy

The current `RothConversionConfig` is one fixed annual amount. Bracket-fill (convert each year up to the top of a target marginal bracket) is a different, popular strategy: the per-year amount varies with other income.

To support this without expensive in-engine recomputation, add a new optional `PerYearOverrides map[int]float64` field to `RothConversionConfig`. When present, the engine uses the per-year override for that calendar year; otherwise falls back to `AnnualAmount`. Backwards compatible (nil map = current behavior).

The optimizer pre-computes the per-year overrides for each bracket-fill candidate using an estimate of non-conversion taxable income (SS + pensions + RMD + dividends). Estimate inaccuracy is second-order on the ranking; if it materially affects rankings in tests, we promote bracket-fill to live engine computation in Phase 1.5.

### Read-Only Recommendation, Not Applied

The optimizer surfaces a recommendation; the user manually edits their scenario. No "apply" button. Three reasons:

1. **No surprise scenario edits.** A what-if scenario is the user's intentional configuration; the optimizer must not silently overwrite it.
2. **Forces understanding.** A user who manually applies a strategy has read and understood it; one-click apply hides the mechanism.
3. **Matches SS Portfolio Optimizer.** That feature stars an optimal age without setting `ClaimAge`; same UX shape.

### Reuse SS Portfolio Eligibility Pattern

The Tax Optimizer mirrors the SS Portfolio Optimizer's eligibility/result struct conventions: `Eligible bool`, `IneligibleReason string`, return value embedded in `FullAnalysis`, rendered by a template partial. Single-user app means we reuse the existing `getSettingsHash` cache without new keys.

## Files In Scope

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/services/retirement/analysis/tax_optimizer.go` | Create | Optimizer orchestration, eligibility, scoring, MC refinement |
| `internal/services/retirement/analysis/tax_optimizer_strategies.go` | Create | Roth strategy enumeration (ladder + bracket-fill) |
| `internal/services/retirement/analysis/tax_optimizer_test.go` | Create | Unit tests |
| `internal/services/retirement/analysis/tax_optimizer_strategies_test.go` | Create | Strategy enumeration tests |
| `internal/services/retirement/analysis/tax_optimizer_integration_test.go` | Create | End-to-end through orchestrator |
| `internal/models/whatif.go` | Modify | Add `TaxOptimizerAnalysis`, `TaxOptimizerCandidate`, `RothOptimizerStrategy`, `RothStrategyKind`; extend `RothConversionConfig` with `PerYearOverrides` |
| `internal/services/retirement/engine/*.go` | Modify | Engine reads `RothConversionConfig.PerYearOverrides` when present, falls back to `AnnualAmount` otherwise |
| `internal/services/retirement/engine/tax_test.go` | Modify | Cover `PerYearOverrides` engine behavior |
| `internal/services/retirement/orchestrator.go` | Modify | Attach `TaxOptimizer` result to `FullAnalysis` after `SSPortfolio` runs |
| `internal/handlers/whatif/handlers.go` | Modify | Pass `TaxOptimizerAnalysis` to template context |
| `internal/handlers/whatif/handlers_test.go` | Modify | Cover panel render and ineligibility branch |
| `web/templates/whatif/_tax_optimizer.html` | Create | Panel template (best card + top-5 table + explainer) |
| `web/templates/whatif/whatif.html` (or equivalent) | Modify | Include `_tax_optimizer.html` partial below SS Portfolio panel |

## Models

```go
// TaxOptimizerAnalysis is the per-scenario recommendation produced by
// analysis.TaxOptimizer. Always non-nil when called via RunFullAnalysis;
// Eligible=false carries IneligibleReason for UI rendering.
type TaxOptimizerAnalysis struct {
    Eligible         bool                    `json:"eligible"`
    IneligibleReason string                  `json:"ineligible_reason,omitempty"`

    Baseline         TaxOptimizerCandidate   `json:"baseline"`             // user's current scenario, scored for delta
    Best             TaxOptimizerCandidate   `json:"best"`                 // top-ranked after MC tiebreak
    Top              []TaxOptimizerCandidate `json:"top"`                  // up to 5, includes Best at index 0

    MonteCarloRuns   int                     `json:"monte_carlo_runs"`     // budget per top-5 candidate
    CandidatesScored int                     `json:"candidates_scored"`    // total deterministic scores computed
}

// TaxOptimizerCandidate is one (SS pair, Roth strategy) configuration
// plus its scored outcome.
type TaxOptimizerCandidate struct {
    PrimaryClaimAge int                   `json:"primary_claim_age"`
    SpouseClaimAge  int                   `json:"spouse_claim_age,omitempty"`
    RothStrategy    RothOptimizerStrategy `json:"roth_strategy"`

    EndingPortfolioReal float64 `json:"ending_portfolio_real"`
    LifetimeTaxReal     float64 `json:"lifetime_tax_real"`
    PeakMarginalBracket float64 `json:"peak_marginal_bracket"`
    TotalRothConverted  float64 `json:"total_roth_converted"`

    // MC-refined; zero-valued for non-top-5 entries.
    MCSurvivalRate     float64 `json:"mc_survival_rate,omitempty"`
    MCMedianEndingReal float64 `json:"mc_median_ending_real,omitempty"`
}

type RothStrategyKind string

const (
    RothStrategyNone        RothStrategyKind = "none"
    RothStrategyLadder      RothStrategyKind = "ladder"
    RothStrategyBracketFill RothStrategyKind = "bracket_fill"
)

type RothOptimizerStrategy struct {
    Kind          RothStrategyKind `json:"kind"`
    AnnualAmount  float64          `json:"annual_amount,omitempty"`  // ladder only
    TargetBracket float64          `json:"target_bracket,omitempty"` // bracket_fill only; e.g. 0.22
    StartAge      int              `json:"start_age"`
    EndAge        int              `json:"end_age"`
    Label         string           `json:"label"` // human-readable, e.g. "$100k/yr to RMD age"
}
```

`RothConversionConfig` gains one new field:

```go
type RothConversionConfig struct {
    Enabled          bool                `json:"enabled"`
    AnnualAmount     float64             `json:"annual_amount"`
    StartYear        int                 `json:"start_year"`
    EndYear          int                 `json:"end_year"`
    PerYearOverrides map[int]float64     `json:"per_year_overrides,omitempty"` // NEW: projection-year offset → override amount; engine prefers this when set
}
```

The `PerYearOverrides` map is keyed by **projection-year offset** (consistent with `StartYear`/`EndYear` in `RothConversionConfig`, which the engine reads as projection-year indices in `RothConversionAmountForYear` at `engine/loop_helpers.go:126`). Empty/nil = pre-existing behavior. The optimizer constructs this map in-memory and never persists it to user-saved scenarios.

Engine modification: `RothConversionAmountForYear` reads `PerYearOverrides[currentYear]` if the map is non-nil, falling back to `AnnualAmount` otherwise. Single read site; no other engine changes.

## Public API

```go
// TaxOptimizer runs the optimizer and returns a recommendation.
// Always synchronous. Eligibility is gated; ineligible scenarios return
// a non-nil result with Eligible=false and IneligibleReason set.
func TaxOptimizer(eng *engine.Engine, in engine.Input, ss *models.SSPortfolioAnalysis) *models.TaxOptimizerAnalysis

// TaxOptimizerWithSeed is TaxOptimizer with a fixed Monte Carlo seed.
// seed == 0 means auto-seed (preserves "default = unpredictable").
func TaxOptimizerWithSeed(eng *engine.Engine, in engine.Input, ss *models.SSPortfolioAnalysis, seed int64) *models.TaxOptimizerAnalysis
```

## Data Flow

### Inside `TaxOptimizer`

1. Read settings from `in.Prepared.Settings()`. If `taxOptimizerEligible` returns `(false, reason)`, return a non-nil result with `Eligible=false` and the reason.
2. Score the **baseline** (user's current SS settings + current Roth config) as one `TaxOptimizerCandidate`. Used for delta columns in the UI.
3. Extract **top-3 SS pairs** from `ssPortfolio.PrimaryOptions` / `ssPortfolio.SpouseOptions` by survival rate. Fall back to `{currentSS}` if `ssPortfolio == nil`.
4. Enumerate **Roth strategy candidates** via `enumerateRothStrategies(settings)` (see below).
5. For each `(ssPair, rothStrategy)`, build a cloned `prepare.PreparedSettings` with the SS pair and Roth overrides applied (following the established `analysis.cloneSettingsWithClaimAges` + `perturbAndPrepare` pattern in `analysis/ss.go`), construct `engine.Input{Prepared: clone, Chain: in.Chain, Hooks: in.Hooks}`, run `eng.Run(cellInput)`, extract score columns. Append to `scored`.
6. Sort `scored` descending by `EndingPortfolioReal`. Take `top5 = scored[:5]`.
7. For each of the 5, run Monte Carlo via the existing `analysis.MonteCarlo(eng, cellInput, taxOptimizerMonteCarloRuns, seed)` entry point. Fill `MCSurvivalRate` (from `Stats.SuccessRate`) and `MCMedianEndingReal` (from `Stats.MedianBalance`, deflated to real if not already).
8. Re-sort `top5` descending by `MCMedianEndingReal`. Set `Best = top5[0]`, `Top = top5`.
9. Return result with `CandidatesScored = len(scored)`, `MonteCarloRuns = taxOptimizerMonteCarloRuns`.

### Strategy Enumeration

`enumerateRothStrategies(settings)` produces ~45 candidates:

**Ladders** (`Kind=ladder`): cross-product of
- Amounts: `{0, 25_000, 50_000, 75_000, 100_000, 150_000, 200_000}` (7 values)
- Windows: anchored on key ages, given `a = CurrentAge`, `s = SocialSecurity.ClaimAge`:
  - `(a, a+5)` — "aggressive early"
  - `(a, s)` — "bridge to SS"
  - `(a, 65)` — "pre-IRMAA"
  - `(a, 73)` — "pre-RMD"
  - `(a+5, a+10)` — "deferred mid"

Total: 7 × 5 = 35 candidates. Dedup any window where end ≤ start (e.g., "pre-IRMAA" when current age already 67). Skip `AnnualAmount = 0` for all-but-one window (one `none` candidate suffices).

**Bracket-fill** (`Kind=bracket_fill`): cross-product of
- Target brackets: `{0.12, 0.22, 0.24}` (3 values)
- Windows: `(a, 65)`, `(a, 73)`, `(a, s)` (3 windows)

Total: 3 × 3 = 9 candidates. For each, pre-compute `PerYearOverrides` by estimating non-conversion taxable income per year (SS + pensions + estimated RMD + estimated taxable-account dividends) and filling to the bracket ceiling.

**Plus one "no conversions" baseline candidate.** Total ≈ 45.

Each candidate carries a stable `Label` for the UI: `"$100k/yr to RMD age"`, `"Fill 22% bracket, 67→72"`, etc.

### Bracket-Fill Pre-Computation

For a bracket-fill candidate spanning ages `[start, end]` with target bracket `B`:

```
for year := projectionYearOf(start); year <= projectionYearOf(end); year++ {
    age := primaryAgeAtProjectionYear(year)
    otherIncome := estimateOtherTaxableIncome(settings, age, year)
    ceiling := bracketTopForFiling(filingStatus, year, B)
    convert := max(0, ceiling - otherIncome)
    PerYearOverrides[year] = convert
}
```

`estimateOtherTaxableIncome` is a closed-form estimate that adds:
- SS benefits (taxable portion) if claimed
- Pensions and ordinary-income sources active that year
- Estimated RMD if `age >= 73`
- Estimated qualified dividends + cap-gains distributions on the taxable balance

This estimate runs once per bracket-fill candidate. It does not need to match the engine's eventual computed values exactly; the optimizer ranks on the engine's *actual* projection results, so any estimate error simply means the bracket-fill candidate may be slightly off-target on conversion amount (not on score).

### Cache

The optimizer result is part of `FullAnalysis` and rides on the existing settings-hash cache (`getSettingsHash`). No new cache keys. The hash already includes `SocialSecurity`, `RothConversion`, `TaxConfig`, `PortfolioValue`, `TaxDeferredPercent`.

## Error Handling

The optimizer never returns an error to its caller and never propagates a panic. Every failure mode degrades gracefully:

| Condition | Behavior |
|-----------|----------|
| Ineligibility (any rule above) | Return `&TaxOptimizerAnalysis{Eligible: false, IneligibleReason: ...}` |
| `eng == nil` or `in.Prepared == nil` | Return `nil` (caller bug; do not render panel) |
| `ssPortfolio == nil` | Fall back to `ssPairs = {currentSS}`, optimizer still runs |
| `ssPortfolio.PrimaryOptions` empty | Same fallback |
| Per-candidate projection produces NaN/Inf | Coerce score to `-math.MaxFloat64`, exclude from `Top`, include in `CandidatesScored` count |
| Bracket-fill estimate yields all zeros | Skip that candidate (would dup the baseline) |
| Bracket-ceiling lookup fails (unknown filing status) | Skip the entire bracket-fill family; emit ladder-only |

No `defer/recover` is added inside `TaxOptimizer` — panics propagate. The existing `RunFullAnalysis` contract is preserved.

## Testing

### Unit Tests

`analysis/tax_optimizer_test.go`:

- **Eligibility table** — every entry in the eligibility section, including boundary cases ($99,999 vs $100,000 tax-deferred; age 72 vs 73; 4 vs 5 projection years; nil vs zero-value `TaxConfig`).
- **Deterministic scoring** — same inputs (seed=42) → same `Best`, same `Top` order, same scores to within 1e-6. Run twice, compare full `TaxOptimizerAnalysis`.
- **SS × Roth interaction** — `TestTaxOptimizer_PrefersJointOverGreedy`: construct a scenario where SS Portfolio's #1 pair is worse than its #2 pair once a large Roth ladder is added. Assert `Best.PrimaryClaimAge` matches the #2 pair. **If this test cannot be constructed with realistic inputs, drop top-K grid to K=1 and remove this test.**
- **Baseline delta** — assert `Baseline.RothStrategy` matches the user's current `RothConversionConfig`, `Baseline.PrimaryClaimAge` matches saved SS settings.
- **`ssPortfolio == nil` fallback** — single-filer scenario: optimizer still runs with one SS pair.
- **NaN/Inf coercion** — synthetic engine input that produces NaN; assert candidate is dropped from `Top` but counted in `CandidatesScored`.

`analysis/tax_optimizer_strategies_test.go`:

- **Exact candidate count** for representative settings (`currentAge=67`, `claim=67` → N candidates, hand-checked).
- **Window respect** — no past-age start, no end past projection horizon.
- **Bracket-fill zero skip** — when target bracket is below estimated baseline income for every year, candidate is excluded.
- **Label stability** — labels match expected strings (golden file or table).
- **Bracket-fill ceiling honored** — for a sample candidate, every `PerYearOverrides[y]` plus estimated other income ≤ bracket ceiling within $1.

`engine/tax_test.go` (additions):

- **`PerYearOverrides` consultation** — engine uses per-year value when set, falls back to `AnnualAmount` when key absent.
- **Backwards compatibility** — `RothConversionConfig{...AnnualAmount: 50000}` with nil `PerYearOverrides` produces identical projection to current behavior.

### Integration Tests

`analysis/tax_optimizer_integration_test.go`:

- **Orchestrator smoke** — `RunFullAnalysis(testdata-scenario)` returns `FullAnalysis` with non-nil `TaxOptimizer` field for an eligible scenario; nil-Eligible result for an ineligible one.
- **Top-5 MC tiebreak** — with seed=42 and a constructed close-ranking scenario, top-5 final order matches a golden expected list. Assert obvious winner stays #1 (deterministic margin > $5k).

### Handler / Template Tests

`handlers/whatif/handlers_test.go` (additions):

- Panel renders when `Eligible=true`: expected columns present (SS pair, strategy label, ending portfolio, lifetime tax, peak bracket, Roth converted, delta vs baseline).
- Ineligibility message renders when `Eligible=false`.
- Delta column shows `+$X` / `-$X` correctly.

### Explicitly Not Tested

- Specific dollar amounts on real user scenarios (drift with engine changes; assert relative ordering instead).
- Wall-clock performance (opt-in `make bench`).

### Coverage Target

95%+ on `tax_optimizer.go` and `tax_optimizer_strategies.go`. Uncoverable patterns (defensive nil guards, log-only branches) documented inline per existing project convention.

## UI

New panel: `web/templates/whatif/_tax_optimizer.html`, rendered below the SS Portfolio panel.

```
┌─ Tax Optimizer ──────────────────────────────────────────────────┐
│ Best strategy (after Monte Carlo refinement):                    │
│   $100k/yr Roth conversion, age 67 → 72                          │
│   SS: primary 70, spouse 62                                      │
│   Ending portfolio (real): $3,847,000  (+$412,000 vs current)    │
│   Lifetime tax paid (real): $548,000                              │
│   Peak marginal bracket: 24%                                      │
│   Total Roth converted: $600,000                                  │
│                                                                   │
│ Top 5 alternatives:                                               │
│ ┌────────────────────┬────────┬───────────┬───────┬──────────┐  │
│ │ Strategy           │ SS     │ End Port. │ Tax   │ Δ Baseln │  │
│ ├────────────────────┼────────┼───────────┼───────┼──────────┤  │
│ │ $100k/yr 67→72     │ 70/62  │ $3,847k   │ $548k │ +$412k   │  │
│ │ Fill 24%, 67→72    │ 70/62  │ $3,803k   │ $562k │ +$368k   │  │
│ │ $150k/yr 67→70     │ 70/62  │ $3,791k   │ $574k │ +$356k   │  │
│ │ Fill 22%, 67→72    │ 67/62  │ $3,762k   │ $531k │ +$327k   │  │
│ │ $75k/yr 67→72      │ 70/62  │ $3,728k   │ $521k │ +$293k   │  │
│ └────────────────────┴────────┴───────────┴───────┴──────────┘  │
│                                                                   │
│ ⓘ Recommendations are read-only. Edit your scenario manually     │
│   to apply. Survivor-spouse filing-status changes are not        │
│   modeled in Phase 1.                                            │
└──────────────────────────────────────────────────────────────────┘
```

Ineligible state renders only the title and the one-line reason.

## Constants

| Constant | Default | Rationale |
|----------|---------|-----------|
| `taxOptimizerEligibilityMinTaxDeferred` | `100_000` | Below this, conversion optimization moves the needle by less than projection noise. |
| `taxOptimizerEligibilityMaxStartAge` | `73` | RMD age; after this, conversions are routinely dominated by RMD distributions. |
| `taxOptimizerEligibilityMinProjectionYears` | `5` | Below this, conversion windows degenerate. |
| `taxOptimizerTopSSPairs` | `3` | Captures dominant SS × Roth interaction at modest cost. |
| `taxOptimizerTopFinalists` | `5` | Number of candidates MC-refined and shown to user. |
| `taxOptimizerMonteCarloRuns` | `32` | Matches SS Portfolio Optimizer's small-budget convention. |
| `taxOptimizerLadderAmounts` | `{0, 25_000, 50_000, 75_000, 100_000, 150_000, 200_000}` | Coverage of practical strategy space. |
| `taxOptimizerBracketFillTargets` | `{0.12, 0.22, 0.24}` | Common advisor targets; skips 32%+ as rarely beneficial pre-RMD. |

All constants live in a single block at the top of `tax_optimizer.go` for easy tuning.

## Established Patterns Reused

- **Settings cloning + override:** `analysis/ss.go` already implements `cloneSettingsWithClaimAges(s, primaryClaimAge, spouseClaimAge)` returning `prepare.PreparedSettings` via `perturbAndPrepare`. The Tax Optimizer follows the same shape with a `cloneSettingsWithSSAndRoth(s, ssPair, rothStrategy)` helper that additionally swaps `RothConversion`.
- **Deterministic projection:** `eng.Run(input) → *models.ProjectionResult`. Pure function. Already used everywhere in the analysis layer.
- **Monte Carlo:** `analysis.MonteCarlo(eng, input, runs, seed) → *models.MonteCarloAnalysis`. Used by SS Portfolio's `runSSPortfolioCellMC`.
- **Engine.Input construction:** `engine.Input{Prepared: clone, Chain: in.Chain, Hooks: in.Hooks}`. Hooks and Chain passed through unchanged from the request input.

## Open Questions Carried Into Implementation Plan

1. **Estimated RMD inside `estimateOtherTaxableIncome`.** The bracket-fill pre-computation needs a closed-form RMD estimate without running a projection. Two viable approaches: (a) reuse the existing RMD helper in `engine/rmd.go` with a year-zero balance assumption, or (b) iterate balance forward via simple compounding. Plan will pick based on accuracy on the golden bracket-fill test; default to (b) if (a) drifts more than $5k on the test scenario.
2. **`ProjectionResult` field for ending real portfolio.** Need to confirm the exact field name and whether deflation is applied; if the result carries nominal only, the optimizer applies inflation deflation using `settings.InflationRate`. Implementation reads existing field names; this is a code-survey item, not a design choice.

## Risks

| Risk | Mitigation |
|------|------------|
| ~15s sync wall-time exceeds user patience | Constants are tunable; can drop MC budget or candidate count. Profile first before any work to reduce. |
| `PerYearOverrides` engine change has subtle hot-path effect | Backwards-compat test asserting nil-map equivalence to current behavior. Engine change is additive, single read site. |
| Top-K=3 grid doesn't beat K=1 in practice | `TestTaxOptimizer_PrefersJointOverGreedy` is the canary. If unconstructable, drop to K=1 and shrink to ~45 deterministic runs. |
| Bracket-fill estimate error materially shifts rankings | Detected by golden-file test on bracket-fill candidate scoring. Phase 1.5: promote bracket-fill to live engine computation. |
| Survivor-spouse modeling absence misleads users with large age gaps | Explicit explainer note in panel; covered in Non-Goals. Phase 2 candidate. |
