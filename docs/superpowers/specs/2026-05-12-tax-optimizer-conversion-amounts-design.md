# Tax Optimizer: Per-Row Conversion Amounts View

**Status:** Reviewed Draft
**Date:** 2026-05-12
**Builds on:** `2026-05-12-tax-optimizer-design.md`

## Problem

The Tax Optimizer panel recommends strategies like "Fill 24% bracket, 67→73"
but the existing Roth conversion form only accepts a single fixed annual
amount. Users can't see what per-year amounts the optimizer is actually
using internally, so they can't translate a recommendation into the form
inputs without guesswork.

For bracket-fill strategies the amount varies each year (sized to fill
to the bracket ceiling minus that year's other taxable income). For
ladder strategies it's flat, but the total conversion volume isn't
shown anywhere either.

## Goal

Expose the per-year conversion amounts the optimizer computed for each
Top-5 row so the user can:

1. Pick a single representative number to plug into the Roth conversion
   form (e.g. average, or the smallest per-year value for the most
   conservative approximation).
2. Sanity-check the total dollar volume of conversions a strategy would
   produce.
3. See how the bracket-fill amounts drift year-to-year as other income
   (SS COLAs, dividends) grows into the bracket.

## Non-Goals

- No new "Fill bracket" mode in the scenario form. That's a larger
  effort and is out of scope for this change.
- No new HTMX endpoint or interactive recompute — amounts are computed
  once at scoring time and rendered statically.
- No comparison view across strategies. Each row exposes its own amounts
  independently; the user does the comparison by toggling rows.
- No attempt to make `TotalRothConverted` accurate in Phase 1. That
  field remains tied to future engine explainability work; this change
  adds a UI-facing conversion plan summary instead.

## UX

Each Top-5 row in the optimizer table gets a sibling sub-row containing
a native HTML `<details>` element. Closed by default:

```
▸ Show conversion amounts
```

Open state:

```
Age   Conversion
 67    $320,400
 68    $310,200
 69    $300,600
 70    $291,500
 71    $283,000
 72    $275,100

Avg $296,800  ·  Min $275,100  ·  Max $320,400  ·  Total $1.78M over 6 years
```

The "No conversion" baseline row has no toggle (empty
`PerYearConversions` → template skips rendering).

Both ladder and bracket-fill strategies show the toggle. Ladder rows
will show a flat amount across every year — the per-year table is
slightly redundant with the strategy label, but the summary line's
**Total converted** value is information not available elsewhere, and
inconsistent behavior across rows would be more confusing than a
slightly-redundant table.

## Data Model

Add to `internal/models/whatif.go`:

```go
// YearlyConversion is one year's planned Roth conversion as part of an
// optimizer strategy. Age is the primary's age in that year; Amount is
// the dollar amount converted in that year, in nominal dollars.
type YearlyConversion struct {
    Age    int     `json:"age"`
    Amount float64 `json:"amount"`
}
```

Extend `TaxOptimizerCandidate`:

```go
type TaxOptimizerCandidate struct {
    // ...existing fields...

    // PerYearConversions is the year-by-year conversion plan implied
    // by RothStrategy. Empty when RothStrategy.Kind is RothStrategyNone
    // or when the strategy is a zero-amount ladder (the no-conversion
    // baseline). Otherwise contains one entry per year of the strategy
    // window. For ladder strategies all entries have the same Amount;
    // for bracket-fill strategies each entry's Amount equals the
    // bracket ceiling minus the optimizer's estimate of other taxable
    // income for that year.
    PerYearConversions []YearlyConversion `json:"per_year_conversions,omitempty"`
}
```

Placement note: put `YearlyConversion` near `RothOptimizerStrategy` /
`TaxOptimizerCandidate`, not near persisted Roth settings. This is an
analysis-output shape, not saved user configuration.

## Computation

Factor the per-year math out of `rothStrategyToConfig` into a small
helper that both `rothStrategyToConfig` and the new view use, so the
two never drift:

```go
// strategyYearlyConversions returns the per-year conversion amounts
// implied by strat. Returns nil for the no-conversion baseline.
// Ladder strategies produce uniform Amount across the window;
// bracket-fill strategies size each year to (ceiling − other income).
func strategyYearlyConversions(
    s *models.WhatIfSettings,
    strat models.RothOptimizerStrategy,
) []models.YearlyConversion
```

`rothStrategyToConfig` already loops over the same range and computes
the same numbers for bracket-fill; refactor so its
`PerYearOverrides` population goes through the new helper.

Call sites:

- `scoreCandidate` (in `analysis/tax_optimizer.go`): after
  `projectionToCandidate` returns, populate
  `cand.PerYearConversions = strategyYearlyConversions(in.Prepared.Settings(), strat)`
  on the candidate before returning. Do this before returning the
  candidate so all top-row candidates carry the same display data that
  was used to build the Roth config.
- The baseline candidate built in `TaxOptimizerWithSeed` should get the
  same treatment after `projectionToCandidate`. Since the baseline is
  intentionally scored by running the saved scenario directly, populate
  it from `currentRoth` and the original `settings`, not by routing it
  through `scoreCandidate`. It will be empty for no conversions and
  non-empty for the user's fixed annual Roth conversion form values.

Performance: one extra loop per candidate over the strategy window
(≤ ~30 ages). Negligible compared to the engine projection.

## Template

`web/templates/components/whatif/tax-optimizer.html` — inside the
`<tbody>` loop, after each strategy `<tr>` close, render:

```html
{{if $c.PerYearConversions}}
<tr class="border-b dark:border-gray-700">
  <td colspan="{{$colspan}}" class="px-2 pb-2">
    <details class="text-xs text-gray-600 dark:text-gray-300">
      <summary class="cursor-pointer hover:text-indigo-600 dark:hover:text-indigo-400">
        Show conversion amounts
      </summary>
      <div class="mt-2 pl-4">
        <table class="text-xs">
          <thead>
            <tr>
              <th class="text-left pr-4 font-medium">Age</th>
              <th class="text-right font-medium">Conversion</th>
            </tr>
          </thead>
          <tbody>
            {{range $c.PerYearConversions}}
            <tr>
              <td class="pr-4">{{.Age}}</td>
              <td class="text-right">{{formatMoney .Amount}}</td>
            </tr>
            {{end}}
          </tbody>
        </table>
        <p class="mt-2 text-gray-500 dark:text-gray-400">
          {{conversionSummary $c.PerYearConversions}}
        </p>
      </div>
    </details>
  </td>
</tr>
{{end}}
```

`$colspan` is set once at the top of the `<tbody>` based on whether
Monte Carlo ran (existing template already uses
`{{if gt $to.MonteCarloRuns 0}}` conditionals for two of the columns).
Count: 5 columns without MC, 7 with MC.

New template helper `conversionSummary(items []models.YearlyConversion) string`
registered in `internal/templates/render.go` alongside `formatMoney`,
returning the formatted line:

```
Avg $296,800  ·  Min $275,100  ·  Max $320,400  ·  Total $1.78M over 6 years
```

**Format rules** (intentionally different from `formatMoney`, which
includes cents):
- Avg / Min / Max: whole-dollar, comma-separated, no cents
  (e.g. `$296,800`). Conversion amounts are inherently approximate
  at the whole-dollar level — cents add noise.
- Total: M-abbreviated for ≥ $1M (e.g. `$1.78M`, 2 decimals),
  K-abbreviated for ≥ $10K (e.g. `$250K`, no decimals), otherwise
  whole-dollar (e.g. `$5,400`). Total volumes are typically large
  multi-year sums — abbreviation aids scanning.
- "over N years" reflects the count of entries in the slice.

Returns an empty string if the slice is empty (defensive — the
template already gates with `{{if $c.PerYearConversions}}`).

Implementation signature in `internal/templates/render.go`:

```go
func conversionSummary(items []models.YearlyConversion) string
```

This requires importing `budget2/internal/models` into the templates
package. Register the helper as `"conversionSummary"` in `getFuncMap`.

## Tests

### Unit (`tax_optimizer_strategies_test.go`)
- `TestStrategyYearlyConversions_Ladder` — uniform amount across window.
- `TestStrategyYearlyConversions_BracketFill_MFJ` — amounts equal
  ceiling minus `estimateOtherTaxableIncome` for each year.
- `TestStrategyYearlyConversions_NoConversion` — nil result.
- `TestStrategyYearlyConversions_ZeroAmountLadder` — nil result
  (matches baseline behavior).
- `TestRothStrategyToConfig_MatchesYearlyConversions` — for a
  bracket-fill strategy, the per-year amounts produced by
  `strategyYearlyConversions` match the `PerYearOverrides` produced
  by `rothStrategyToConfig`, year by year. This locks the shared-math
  invariant.

### Unit (`tax_optimizer_test.go`)
- `TestScoreCandidate_PopulatesPerYearConversions` — the public path
  through `scoreCandidate` attaches the slice.
- `TestTaxOptimizerWithSeed_PopulatesBaselinePerYearConversions` —
  when the saved scenario has enabled fixed annual Roth conversions,
  `Baseline.PerYearConversions` is populated even though the baseline
  is scored directly from the original input.

### Template / handler (`handlers_test.go`)
Extend `TestTaxOptimizerPanel_EligibleRendersStrategyAndTable`:
- Assert "Show conversion amounts" appears in the rendered HTML for
  non-baseline rows.
- Assert at least one age/amount row appears.
- Assert the summary line appears with formatted Avg/Min/Max/Total.
- Seed the template fixture's `Top` candidates with
  `PerYearConversions`; otherwise the template will correctly skip the
  disclosure and the assertions will be meaningless.

### Template helper (`render_helpers_test.go`)
- Empty slice returns `""`.
- Single-entry and multi-entry slices format Avg / Min / Max with
  whole-dollar comma formatting.
- Totals use whole-dollar / K / M abbreviations at the specified
  thresholds.
- `getFuncMap()` contains `"conversionSummary"`.

## Risk / Blast Radius

Per CLAUDE.md, run `gitnexus_impact` before editing each symbol:

- `TaxOptimizerCandidate` — adding a field is additive. Risk: low.
  All consumers either marshal it (JSON output adds an optional field)
  or read specific fields (won't break).
- `scoreCandidate` — adding a final write to the returned candidate.
  Risk: low. No callers depend on the absence of the new field.
- `rothStrategyToConfig` — refactoring its per-year loop to call the
  new helper. Behavior preserved by drift-guard test. Risk: low.
- `getFuncMap` / `conversionSummary` — additive template helper. Risk:
  low. Failure mode is template parse/runtime failure, covered by
  renderer and handler tests.

No data migration. Stored scenarios are unaffected (the new field is
on the in-memory analysis result, not on persisted settings).

## Files Changed

- `internal/models/whatif.go` — add `YearlyConversion`, extend candidate.
- `internal/services/retirement/analysis/tax_optimizer_strategies.go` —
  factor shared per-year math into `strategyYearlyConversions`.
- `internal/services/retirement/analysis/tax_optimizer.go` — attach
  `PerYearConversions` in `scoreCandidate` and the baseline.
- `internal/services/retirement/analysis/tax_optimizer_strategies_test.go` —
  unit tests for the helper and the `rothStrategyToConfig` drift guard.
- `internal/services/retirement/analysis/tax_optimizer_test.go` —
  `scoreCandidate` integration and baseline population tests.
- `web/templates/components/whatif/tax-optimizer.html` — render the
  disclosure per row.
- `internal/templates/render.go` — register `conversionSummary`
  alongside `formatMoney` in the FuncMap.
- `internal/templates/render_helpers_test.go` — unit tests for the
  new helper covering empty / single-entry / multi-entry / large-total
  cases, plus the funcMap presence assertion.
- `internal/handlers/whatif/handlers_test.go` — extend panel-render test.

## Open Questions

None. All design decisions are settled.
