# Gross Withdrawal Line on Budget Analysis Panel

**Date:** 2026-05-12
**Status:** Draft
**Owner:** Darrell

## Motivation

The WhatIf "Monthly Budget Analysis" panel computes a **Monthly Gap** that
represents the after-tax shortfall between expenses and net income. When a
user funds that gap from a tax-deferred account (401k, Traditional IRA), the
gross withdrawal required is meaningfully larger than the gap itself, because
the withdrawal is taxed as ordinary income. A user reading the current panel
will undercount how fast their portfolio depletes.

The fix is to show, alongside the gap, the **gross withdrawal needed** to net
the gap, broken out by funding source.

## Goals

- Show the user how much they must *actually* withdraw to net the shortfall.
- Distinguish funding sources (tax-deferred / taxable / Roth) so the user can
  see which bucket is cheapest to draw from.
- Capture real-world tax effects (bracket boundaries, NIIT, Social Security
  taxability, IRMAA cliffs) — not a flat assumed rate.

## Non-Goals

- Recommend an optimal withdrawal sequence (Roth conversion / withdrawal
  ladder strategy lives in the Tax Optimizer panel).
- Change the existing `MonthlyGap` / `SteadyStateGap` / `RequiredRate`
  numbers — those remain the after-tax shortfall.
- Project the multi-year impact on portfolio depletion (the existing
  projection chart already reflects taxed withdrawals).

## UI Changes

### Placement

Under the existing two-column **Monthly Gap / Required Rate** grid in
`web/templates/components/whatif/budget-analysis.html`, in both:

- `whatif-budget-analysis` (Current/Today section)
- `whatif-budget-steady-state` (At Year N section)

### Rendering

When `MonthlyGap > 0` (Current) or `SteadyStateGap > 0` (Steady State):

```
GROSS WITHDRAWAL NEEDED TO CLOSE GAP
From Tax-Deferred    $8,164/mo    (27% marginal)
From Taxable         $6,180/mo    (~4% LTCG-equiv)
From Roth            $5,960/mo    (no tax)
```

Footnote (italicized small text):

> *Tax-deferred grosses up to cover income tax on the withdrawal. Taxable
> applies LTCG on the gain portion of the sale. Roth is a 1:1 withdrawal. A
> larger gross withdrawal depletes the account faster than the gap alone
> suggests.*

When `MonthlyGap <= 0` (surplus), the section does not render at all.

### Styling

- Tax-deferred row: dollar amount in **red** (largest cost, matches existing
  IRMAA/Taxes styling per `feedback_ui_cost_styling`).
- Taxable row: dollar amount in **amber** (intermediate).
- Roth row: dollar amount in **green** (no extra cost beyond the gap).
- Rate columns: small grey muted text.

## Computation

### Tax-Deferred

Reuse the existing `estimateTaxSnapshot` closure in `budget_fit.go`. For the
Current section:

```go
extraSnapshot := estimateTaxSnapshot(
    0,
    taxableCashFlow,
    monthlyRMD + monthlyGap,   // <-- adds gap to taxable RMD-equivalent withdrawal
    rothConversionThisMonth,
    &currentIRMALookbackMAGI,
)
extraTax := extraSnapshot.MonthlyTax - currentSnapshot.MonthlyTax
marginalRate := extraTax / monthlyGap
grossWithdrawalTD := monthlyGap / (1 - marginalRate)
```

Steady-state uses `steadyStateSnapshot` as baseline and adds
`SteadyStateGap` to the RMD parameter passed to a second
`estimateTaxSnapshot` call.

Guard rails:

- If `marginalRate >= 0.95` (degenerate, shouldn't happen but defensive),
  cap at 0.95 to keep the gross-up finite.
- If `monthlyGap <= 0`, skip computation entirely.

### Taxable

`gainFraction` represents the portion of a withdrawal that is capital gain
(taxed) vs. return of basis (untaxed).

- **Current section** — Engine line `internal/services/retirement/engine/taxable.go:48`
  initializes `CostBasis = marketValue` at month 0, so the gain fraction is
  effectively 0. Hardcode `gainFraction = 0.0`. Gross withdrawal = gap, rate
  shown as `(0% — basis = market)`.

- **Steady-state section** — Use the smooth approximation:

  ```go
  gainFraction := 1.0 - math.Pow(1.0 + taxableAnnualReturn/100.0, -yearsToSteadyState)
  ```

  Then simulate via `estimateTaxSnapshot` with
  `taxableCashFlow.CapitalGainsDistributions += monthlyGap * gainFraction`.
  Diff the resulting `MonthlyTax` vs. baseline to derive the effective rate
  and gross-up:

  ```go
  extraTax := extraSnap.MonthlyTax - steadyStateSnapshot.MonthlyTax
  effectiveRate := extraTax / monthlyGap
  grossWithdrawalTaxable := monthlyGap / (1 - effectiveRate)
  ```

### Roth

```go
grossWithdrawalRoth := monthlyGap
// rate is implicit 0%
```

No simulation needed.

## Data Model

Add to `models.BudgetFitAnalysis` in `internal/models/whatif.go`:

```go
// Current section
GrossWithdrawalTaxDeferred float64 `json:"gross_withdrawal_tax_deferred"`
MarginalRateTaxDeferred    float64 `json:"marginal_rate_tax_deferred"` // 0-100
GrossWithdrawalTaxable     float64 `json:"gross_withdrawal_taxable"`
EffectiveRateTaxable       float64 `json:"effective_rate_taxable"`     // 0-100
GrossWithdrawalRoth        float64 `json:"gross_withdrawal_roth"`

// Steady-state section
SteadyStateGrossWithdrawalTaxDeferred float64 `json:"steady_state_gross_withdrawal_tax_deferred"`
SteadyStateMarginalRateTaxDeferred    float64 `json:"steady_state_marginal_rate_tax_deferred"`
SteadyStateGrossWithdrawalTaxable     float64 `json:"steady_state_gross_withdrawal_taxable"`
SteadyStateEffectiveRateTaxable       float64 `json:"steady_state_effective_rate_taxable"`
SteadyStateGrossWithdrawalRoth        float64 `json:"steady_state_gross_withdrawal_roth"`
```

All fields stay at zero when the section's gap is ≤ 0.

## Tests

In `internal/services/retirement/analysis/budget_fit_test.go`:

1. **Zero gap → all fields zero.** Build a scenario where income covers
   expenses; assert all `GrossWithdrawal*` and rate fields = 0.
2. **Tax-deferred-only portfolio, modest income, real shortfall** — assert
   `MarginalRateTaxDeferred` is within 1 percentage point of the expected
   federal bracket (e.g., 22% bracket scenario → marginal in [21, 27]
   accounting for state).
3. **Roth gross equals gap exactly** for any positive gap.
4. **Taxable at year 0** — `EffectiveRateTaxable ≈ 0`, gross ≈ gap.
5. **Steady-state at year 20 with 7% taxable return** — assert
   `gainFraction ≈ 0.74` and `EffectiveRateTaxable` is positive but well
   below the tax-deferred marginal rate.
6. **Surplus scenario** (negative gap from RMD-driven excess) — assert all
   gross-withdrawal fields = 0.

In `internal/handlers/whatif/`:

- Template render smoke test: rendered HTML contains "Gross Withdrawal" when
  gap > 0, does not contain it when gap ≤ 0.

## Files Touched

- `internal/models/whatif.go` — new fields on `BudgetFitAnalysis`
- `internal/services/retirement/analysis/budget_fit.go` — compute new fields
- `internal/services/retirement/analysis/budget_fit_test.go` — unit tests
- `web/templates/components/whatif/budget-analysis.html` — render new section
- `internal/handlers/whatif/completeness_render_test.go` (or sibling) —
  template smoke test

## Risk / Impact

- `budget_fit.go` is read by every WhatIf submission. CLAUDE.md mandates
  `gitnexus_impact` before editing — must run and report blast radius.
- Two additional `estimateTaxSnapshot` calls per BudgetFit run (one per
  section, per source needing simulation). Each call constructs a fresh
  `TaxCalculator` and `ProjectionTaxAccumulator`; negligible cost relative
  to the rest of the WhatIf compute path.
- No change to existing `MonthlyGap` / `SteadyStateGap` / `RequiredRate`
  outputs; purely additive. Cannot regress current rendering or numbers.

## Open Questions

None — design fully approved (placement below Required Rate, hide on
surplus, smooth approximation for taxable gain fraction).
