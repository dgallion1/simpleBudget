# SS Portfolio-Aware Claiming Age Optimizer

> **Implementation note (2026-04-09):** This spec was written with an async/polling architecture. The actual implementation went **synchronous** — no background jobs, no polling endpoint, no `SSPortfolioStore`. Eligibility was also relaxed: the feature works when *either* person (not both) has a valid claim age. See `docs/superpowers/plans/2026-04-09-ss-portfolio-optimizer.md` for the implemented design.

## Problem

The current SS optimizer ranks claiming ages by cumulative benefits at age 85. That remains useful as a quick benefit comparison, but it is no longer the most decision-relevant metric once Social Security claiming ages feed the full retirement projection. The better household strategy may be the one that improves portfolio survival, not the one that maximizes cumulative SS checks in isolation.

## Scope

This feature adds a second, portfolio-aware recommendation layer on top of the existing SS comparison tables.

Phase 1 does **not** replace the current `RunSSAnalysis()` tables, breakeven chips, or "best cumulative by age 85" star. Those remain synchronous and unchanged for immediate feedback. The new work is:

1. Run an async Monte Carlo grid for the selected SS scenario.
2. Evaluate all valid primary/spouse claiming-age combinations for the current household.
3. Surface the portfolio-optimal pair and per-age portfolio impact tables.

## Non-Goals

1. Do not rewrite the existing synchronous SS comparison analysis.
2. Do not persist portfolio-grid jobs to disk.
3. Do not add multi-user job isolation; this remains a personal single-user app.
4. Do not extend the Monte Carlo engine just to expose raw per-run results in Phase 1.

## Eligibility

Show the portfolio-aware panel only when all of the following are true:

1. `settings.HasSpouse()` is true.
2. `settings.SocialSecurity != nil`.
3. `settings.SocialSecurity.FRABenefit > 0`.
4. `settings.SocialSecurity.SpouseFRABenefit > 0`.
5. `ClaimAge` is between 62 and 70.
6. `SpouseClaimAge` is between 62 and 70.
7. `settings.CurrentAge > 0` and `settings.SpouseAge > 0`.

If any condition fails, keep the existing SS comparison UI only.

Because ages below the current age are invalid, the grid is **up to 81 combinations**, not always exactly 81.

## Design Decisions

### Keep Existing Tables, Add Async Portfolio Panel

The current SS comparison table stays as the fast, deterministic benefit table. The portfolio-aware recommendation is rendered as a separate section below the existing SS comparison content.

This avoids two problems in one step:

1. The current synchronous request path stays responsive.
2. The app does not silently redefine the meaning of the existing "best age" star.

### Reuse Existing Settings Hash and Chain-Aware Calculator Logic

The app already has:

1. `getSettingsHash(settings)` in `internal/handlers/whatif/handlers.go`
2. `buildCalculator(settings)` in the same file
3. `runAnalysisWithCache(settings)` for the normal what-if response path

The portfolio job must reuse the same settings hash logic rather than defining a second cache key scheme.

If the async worker needs pre-resolved scenario-chain context, factor that out of `buildCalculator()` into a reusable helper instead of reloading chained scenarios 81 times.

### Single Active Job Store, Not a Hash Map

The earlier map-based store is unnecessary for this app and introduces stale-result problems. The polling endpoint always reads the currently saved scenario, so the store only needs to track the latest job/result.

Use a single in-memory job record with a generation token:

```go
type SSPortfolioStore struct {
    mu         sync.RWMutex
    generation uint64
    hash       string
    startedAt  time.Time
    readyAt    time.Time
    running    bool
    result     *models.SSPortfolioAnalysis
}
```

Rules:

1. Starting a new eligible save increments `generation`, replaces `hash`, clears the old result, and marks `running=true`.
2. The worker captures `(generation, hash)` at launch.
3. When the worker finishes, it stores the result only if both still match.
4. If a newer save happened while the worker was running, the stale worker exits without overwriting the latest job state.
5. Expire finished results after 5 minutes, matching the existing what-if cache horizon.

This is the key concurrency guard. Without it, an old goroutine can finish late and overwrite a newer scenario's result.

## Data Model

### `internal/models/whatif.go`

Add portfolio-aware option/result types:

```go
type SSPortfolioOption struct {
    ClaimAge            int     `json:"claim_age"`
    MonthlyBenefit      float64 `json:"monthly_benefit"`
    SurvivalRate        float64 `json:"survival_rate"`
    MedianEndingBalance float64 `json:"median_ending_balance"`
    P10EndingBalance    float64 `json:"p10_ending_balance"`
    P90EndingBalance    float64 `json:"p90_ending_balance"`
    DeltaSurvivalRate   float64 `json:"delta_survival_rate"`
}

type SSPortfolioAnalysis struct {
    PrimaryOptions       []SSPortfolioOption `json:"primary_options"`
    SpouseOptions        []SSPortfolioOption `json:"spouse_options"`
    OptimalPrimaryAge    int                 `json:"optimal_primary_age"`
    OptimalSpouseAge     int                 `json:"optimal_spouse_age"`
    OptimalSurvivalRate  float64             `json:"optimal_survival_rate"`
    BaselineSurvivalRate float64             `json:"baseline_survival_rate"`
    Ready                bool                `json:"ready"`
    Error                string              `json:"error,omitempty"`
}
```

Attach it to the existing SS analysis model so templates can stay under the current social-security component:

```go
type SSComparisonAnalysis struct {
    ...
    Portfolio *SSPortfolioAnalysis `json:"portfolio,omitempty"`
}
```

Phase 1 intentionally does **not** include "median depletion age". The current `RunMonteCarloSimulation()` API exposes aggregate stats and not the raw sorted depletion-year distribution needed for a true median. If depletion-age display is desired later, extend the MC result shape in a separate change.

## Computation

### Grid Shape

For the current saved household:

1. Primary ages: `max(62, settings.CurrentAge)` through `70`
2. Spouse ages: `max(62, settings.SpouseAge)` through `70`

Each cell represents one `(primaryAge, spouseAge)` pair.

### Per Cell

For each valid pair:

1. Clone the saved `WhatIfSettings`.
2. Override `SocialSecurity.ClaimAge`.
3. Override `SocialSecurity.SpouseClaimAge`.
4. Build a fresh calculator using the same chain-aware logic as the normal what-if path.
5. Run `RunMonteCarloSimulation(ssPortfolioMonteCarloRuns)`.
6. Reuse MC aggregate stats directly:
   - `SuccessRate`
   - `MedianBalance`
   - `Percentile10`
   - `Percentile90`
7. Reuse the existing SS analysis output for the matching pair's displayed monthly benefits rather than re-deriving template-only values separately.

Use a named constant for the grid run count, for example:

```go
const ssPortfolioMonteCarloRuns = 250
```

This should be independent from the main `RunFullAnalysis()` Monte Carlo count. Running `81 x 1000` extra simulations on every SS form save is avoidable cost for Phase 1, and the sample size should remain easy to tune after timing the feature in practice.

### Baseline and Optimal Pair

The baseline pair is the currently selected `(ClaimAge, SpouseClaimAge)`.

For each cell store:

1. survival rate
2. median ending balance
3. p10 ending balance
4. p90 ending balance

Select the optimal pair by:

1. Highest survival rate
2. Tiebreak: highest median ending balance
3. Final tiebreak: younger combined claiming age total, so ties are deterministic

### Table Construction

Build two display tables from the computed grid:

1. Primary table: vary primary age while holding spouse age at the selected spouse age.
2. Spouse table: vary spouse age while holding primary age at the selected primary age.

For each row:

1. `MonthlyBenefit` comes from the SS analysis row for that claim age under the relevant held-constant pair.
2. `DeltaSurvivalRate = row.SurvivalRate - baseline.SurvivalRate`

Do not compute ages below the person's current age and then filter later. Skip them when building the grid.

## Handler Flow

### `POST /whatif/social-security`

Update `handleWhatIfSocialSecurity()` in `internal/handlers/whatif/handlers.go`:

1. Parse and save settings exactly as today.
2. Build or resolve the dependency hash using the existing hash logic.
3. Run the normal response path with `runAnalysisWithCache(settings)` instead of calling `calc.RunFullAnalysis()` directly.
4. If the SS portfolio feature is eligible:
   - start or refresh the async portfolio job for the current hash
   - do not block the response on job completion
5. If the feature is not eligible:
   - invalidate any existing portfolio job/result for the current store
6. Render the normal `whatif-results` partial immediately.

Important: the async SS portfolio result must **not** be stored inside the existing package-level `analysisCache`. That cache is for the normal request/response analysis object, not job lifecycle state.

### New Polling Endpoint

Add:

- `GET /whatif/ss-portfolio-status`

Behavior:

1. Load current saved settings.
2. If the feature is not eligible, return an empty `div` with the panel id.
3. Compute the current dependency hash using the same logic as the save path.
4. Ask the portfolio store for the latest state for that hash.
5. Return one of:
   - placeholder panel with polling still enabled
   - completed portfolio panel with polling removed
   - error panel with polling removed

The endpoint should not accept query parameters. It should always reflect the currently saved scenario, matching the rest of the what-if flow.

## Templates and UI

### File

Update `web/templates/components/whatif/social-security.html`.

### Placement

Render the portfolio-aware panel below the existing SS comparison content in the same card.

### Placeholder

```html
<div id="ss-portfolio-impact"
     hx-get="/whatif/ss-portfolio-status"
     hx-trigger="every 2s"
     hx-swap="outerHTML">
    <div class="flex items-center gap-2 text-sm text-gray-500">
        <svg class="animate-spin h-4 w-4">...</svg>
        Analyzing portfolio impact across claiming-age combinations...
    </div>
</div>
```

### Completed Panel

Show:

1. Banner:
   `Portfolio-optimal combination: You at 67, spouse at 65 - 94.2% survival vs 88.1% at the current selection`
2. Primary table:
   `Claim Age | Monthly Benefit | Survival Rate | Median Ending Balance | 10th/90th Percentile | Delta vs Baseline`
3. Spouse table:
   same columns

Highlight:

1. the row matching the global optimal pair in each table slice
2. positive `DeltaSurvivalRate` in green
3. negative `DeltaSurvivalRate` in amber/red

### Error Panel

If the worker fails, show a compact warning panel in place of the spinner:

`Couldn't finish portfolio SS analysis. Existing claiming tables are still available.`

Stop polling on error.

### Copy Notes

The UI should be explicit that:

1. the existing top tables are still cumulative-benefit comparisons
2. the new panel is portfolio survival analysis

That distinction matters because the two recommendations may disagree.

## Implementation Notes

1. Reuse the current SS claim-age validation helpers.
2. Reuse the current spouse-benefit logic; do not create a second spousal-top-up formula for the portfolio grid.
3. Reuse the current dark-mode table styling and green-highlight conventions.
4. Keep the portfolio panel absent when the spouse configuration is incomplete.
5. If repeated saves produce the same hash and a matching job is already running or ready, do not launch a duplicate worker.

## Testing

### Unit Tests

Add tests covering:

1. eligibility rules
2. grid dimension calculation when current ages are above 62
3. optimal-pair selection and deterministic tiebreaks
4. row delta calculation vs baseline
5. stale worker completion is discarded when generation/hash no longer match
6. same-hash saves do not launch duplicate work
7. store expiry after TTL

### Integration Tests

Add or update tests in `internal/handlers/whatif/handlers_test.go` for:

1. `POST /whatif/social-security` starts a portfolio job only when eligible
2. ineligible saves clear the portfolio state
3. `GET /whatif/ss-portfolio-status` returns placeholder while running
4. `GET /whatif/ss-portfolio-status` returns completed content when ready
5. `GET /whatif/ss-portfolio-status` returns empty content when the spouse configuration is incomplete
6. a late completion from an older save does not overwrite a newer scenario's result

### Regression Coverage

Verify that:

1. existing SS comparison tests still pass unchanged
2. existing projection integration behavior still uses the selected claim ages
3. the current what-if cache behavior is unchanged for non-SS requests
