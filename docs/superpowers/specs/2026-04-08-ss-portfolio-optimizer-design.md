# SS Portfolio-Aware Claiming Age Optimizer

## Overview

Replace the current SS claiming age optimizer's "max cumulative benefit at 85" logic with a portfolio-aware analysis that runs Monte Carlo simulations across all 81 combinations of primary (62-70) and spouse (62-70) claiming ages. The analysis runs asynchronously via a goroutine with HTMX polling, keeping the UI responsive while computing results.

## Goals

- Determine which claiming age combination maximizes portfolio survival probability
- Show per-age portfolio impact (survival rate, ending balance distribution) for both primary and spouse
- Surface the optimal pair prominently
- Run asynchronously so the existing instant comparison tables remain responsive

## Data Model

### SSPortfolioOption

One row in a portfolio impact table:

```go
type SSPortfolioOption struct {
    ClaimAge            int
    MonthlyBenefit      float64
    SurvivalRate        float64  // % of MC runs that survive
    MedianEndingBalance float64
    P10EndingBalance    float64  // 10th percentile ending balance
    P90EndingBalance    float64  // 90th percentile ending balance
    MedianDepletionAge  float64  // median age at depletion (0 if >50% survive)
    DeltaSurvivalRate   float64  // vs currently selected baseline pair
}
```

### SSPortfolioAnalysis

Full result of the async computation:

```go
type SSPortfolioAnalysis struct {
    PrimaryOptions       []SSPortfolioOption // 9 rows, spouse held at selected age
    SpouseOptions        []SSPortfolioOption // 9 rows, primary held at selected age
    OptimalPrimaryAge    int
    OptimalSpouseAge     int
    OptimalSurvivalRate  float64
    BaselineSurvivalRate float64 // survival rate at currently selected ages
    Ready                bool
    Error                string
}
```

The 81-grid is computed internally but not stored in full. The optimal pair is extracted by scanning all 81 cells. The two display tables are sliced from the grid along the selected spouse/primary age axes.

## Async Engine

### SSPortfolioStore

In-memory concurrent store for the most recent analysis result:

```go
type SSPortfolioStore struct {
    mu      sync.RWMutex
    results map[string]*SSPortfolioAnalysis // keyed by settings hash
}
```

- Created once at app startup, injected into the handler
- Single active computation at a time. Map allows hash-based lookup, but old entries are deleted when a new computation starts. No multi-user support needed (personal app)
- Results evicted on invalidation or after 5-minute TTL

### Settings Hash

Deterministic hash of all fields that affect the projection: portfolio value, expenses, income sources, SS config, tax config, investment returns, etc. Any setting change invalidates the old result.

### Flow

1. User saves SS form -> handler calls `RunFullAnalysis()` synchronously -> renders page with existing cumulative tables + placeholder div
2. If both primary and spouse claim ages are set (non-zero), handler invalidates old result and launches `go store.RunSSPortfolioGrid(calculator, hash)`
3. Goroutine iterates all 81 combinations (primaryAge 62-70 x spouseAge 62-70):
   - Deep-copy WhatIfSettings
   - Override ClaimAge and SpouseClaimAge
   - Build fresh Calculator from cloned settings
   - Run RunMonteCarloSimulation(1000)
   - Extract: survival rate, ending balance slice (for percentiles), depletion year slice
4. Find optimal pair: highest survival rate, tiebreak on median ending balance
5. Build two display tables by slicing grid at selected ages
6. Compute delta (each row's survival rate minus baseline pair's survival rate)
7. Store result, mark Ready: true
8. Concurrency: goroutine clones settings and builds fresh calculators per combination — no shared mutable state with the main request path

### Age Filtering

Ages below the person's current age are excluded from display tables, matching the existing comparison table behavior.

## API Endpoints

### Modified

- `POST /whatif/social-security` — existing behavior unchanged, plus:
  - Computes settings hash
  - If both claim ages set: invalidates old result, launches goroutine
  - Renders response with portfolio impact placeholder div

### New

- `GET /whatif/ss-portfolio-status` — polling endpoint
  - Reads settings hash from current saved settings (no query params)
  - Returns either:
    - Placeholder HTML with spinner + `hx-get` + `hx-trigger="every 2s"` (still computing)
    - Full portfolio impact panel HTML with no polling trigger (done)
    - Empty div if conditions not met (claim ages not set)
  - If user changes settings between polls, hash mismatch causes placeholder return (correct, since new form save launches new goroutine)

## Template & UI

### Guard Condition

Portfolio impact panel only appears when BOTH primary and spouse claim ages are set (non-zero). If either is "Analysis only", only the existing instant cumulative tables show.

### Placeholder (immediate, on form save)

Shown below the existing cumulative comparison tables, above the breakeven section:

```html
<div id="ss-portfolio-impact"
     hx-get="/whatif/ss-portfolio-status"
     hx-trigger="every 2s"
     hx-swap="outerHTML">
    <div class="flex items-center gap-2 text-sm text-gray-500">
        <svg class="animate-spin h-4 w-4">...</svg>
        Analyzing portfolio impact across 81 claiming age combinations...
    </div>
</div>
```

### Completed Panel (replaces placeholder)

1. **Optimal combination banner** — green highlight:
   "Optimal combination: You at 67, Spouse at 65 — 94.2% survival rate (vs 88.1% at current selection)"

2. **Primary portfolio impact table** (9 rows, spouse held at selected age):
   - Columns: Claim Age | Monthly Benefit | Survival Rate | Median Ending Balance | 10th/90th Pctl | Median Depletion Age* | Delta vs Baseline
   - Best row highlighted with green star (matching existing table style)
   - *Median Depletion Age column only shown if any scenario depletes

3. **Spouse portfolio impact table** (9 rows, primary held at selected age):
   - Same columns as primary table
   - Best row highlighted

### Styling

- Follows existing dark mode / Tailwind patterns
- Best rows use green highlight + star marker (same as existing comparison table)
- Consistent with existing whatif section styling

## Computation Details

### Per Grid Cell

For each (primaryAge, spouseAge) pair:
1. Deep-copy WhatIfSettings
2. Override SocialSecurity.ClaimAge = primaryAge
3. Override SocialSecurity.SpouseClaimAge = spouseAge
4. Build Calculator
5. RunMonteCarloSimulation(1000)
6. From results: survival rate = SuccessRate, sort FinalBalance values for percentiles, collect DepletionYear values for median depletion age

### Percentile Calculation

Sort the ending balance slice from MC results. P10 = value at index len*0.10, P50 (median) = index len*0.50, P90 = index len*0.90.

### Optimal Pair Selection

Scan all 81 cells. Primary sort: highest survival rate. Tiebreak: highest median ending balance.

### Display Table Construction

- Primary table: from the 81-grid, select the 9 cells where spouseAge == selected spouse age
- Spouse table: from the 81-grid, select the 9 cells where primaryAge == selected primary age
- Both tables: filter out ages below the person's current age

## Testing Strategy

### Unit Tests

1. **SSPortfolioStore** — concurrent access: launch goroutine, verify Ready transitions false->true, verify hash invalidation clears old results
2. **Grid extraction** — given mock 81-cell grid, verify primary table slices correctly at given spouse age, spouse table slices at given primary age
3. **Optimal pair selection** — verify highest survival rate wins, verify tiebreak on median ending balance
4. **Delta calculation** — verify each row's delta computed against baseline pair
5. **Percentile extraction** — verify 10th/50th/90th from known distribution
6. **Age filtering** — verify ages below current age excluded from display tables
7. **Guard conditions** — verify no goroutine launched when either claim age is "Analysis only" or spouse benefit is zero

### Integration Tests

8. **Polling endpoint** — verify returns placeholder when not ready, full panel when ready
9. **Settings change invalidation** — save form, verify goroutine starts; change setting, verify old result invalidated and new goroutine starts
10. **End-to-end** — configure both claim ages, verify polling returns completed panel with correct optimal pair

### MC Run Count in Tests

Tests use low MC run counts (e.g., 10) for speed. Enough to verify wiring and data flow without testing MC statistical quality.
