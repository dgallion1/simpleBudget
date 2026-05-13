# WhatIf Steady-State Slider + Withdrawal Mix Refactor

**Date:** 2026-05-13
**Status:** Shipped
**Supersedes (in part):** `docs/superpowers/specs/2026-05-12-gross-withdrawal-line-design.md`

## Motivation

Two user-reported issues against the WhatIf "Monthly Budget Analysis" panel:

1. **The steady-state slider could not reach year 0.** When `MinSteadyStateYear ≤ 0.5` the template floored the slider at 1; when `MinSteadyStateYear > 0.5` (delayed income) it floored at the auto-detected year. Either way the user could not drag the thumb back to year 0 to view today's values in the steady-state panel.
2. **The "Gross Withdrawal Needed to Close Gap" rows looked additive.** They were presented as three independent "if you funded the entire gap from this bucket" alternatives, which most users summed mentally and concluded the math was wrong (the sum greatly exceeds the gap because each row covers the whole gap).

## Changes

### 1. Slider can reach year 0 unconditionally

**Template** — `web/templates/components/whatif/budget-analysis.html`

```html
<input type="range" name="steady_state_override_year"
       id="steady-state-slider"
       value="..."
       min="0"
       max="{{.Settings.ProjectionYears}}"
       step="1"
       ...>
```

The previous `MinSteadyStateYear`-dependent floor is gone.

### 2. Year-0 panel mirrors Current (Today) values

**Calculator** — `internal/services/retirement/analysis/budget_fit.go`

When the resolved `steadyStateMonth == 0`, the steady-state result fields are copied from the month-0 (Current) fields rather than left at zero. Mirrored fields:

```
SteadyStateExpenses                      ← MonthlyExpenses
SteadyStateIncome                        ← MonthlyIncome
SteadyStateGrossIncome                   ← GrossIncome
SteadyStateNetIncome                     ← NetIncome
SteadyStateTaxes                         ← MonthlyTaxes
SteadyStateStateTax                      ← MonthlyStateTax
SteadyStateNIIT                          ← MonthlyNIIT
SteadyStateIRMAA                         ← MonthlyIRMAA
SteadyStateTaxableSocialSecurityPct      ← TaxableSocialSecurityPct
SteadyStateRMD                           ← MonthlyRMD
SteadyStateGap                           ← MonthlyGap
SteadyStateRate                          ← RequiredRate
SteadyStateGrossWithdrawalTaxDeferred    ← GrossWithdrawalTaxDeferred
SteadyStateNetWithdrawalTaxDeferred      ← NetWithdrawalTaxDeferred
SteadyStateMarginalRateTaxDeferred       ← MarginalRateTaxDeferred
SteadyStateGrossWithdrawalTaxable        ← GrossWithdrawalTaxable
SteadyStateNetWithdrawalTaxable          ← NetWithdrawalTaxable
SteadyStateEffectiveRateTaxable          ← EffectiveRateTaxable
SteadyStateGrossWithdrawalRoth           ← GrossWithdrawalRoth
SteadyStateNetWithdrawalRoth             ← NetWithdrawalRoth
```

### 3. `SteadyStateOverrideYear` semantics: any value ≥ 0 is honored

**Calculator** — same file

```go
// Override always wins when non-negative. The slider posts its current
// value (0..ProjectionYears) and the user expects to view exactly that
// year — including year 0 (Current values) and years below the
// auto-calculated steady-state when income is still ramping in.
steadyStateYear := minSteadyStateYear
if s.SteadyStateOverrideYear >= 0 {
    steadyStateYear = s.SteadyStateOverrideYear
}
```

**Behavior change.** Previously `SteadyStateOverrideYear == 0` was the "auto" sentinel and meant *use `MinSteadyStateYear`*. Now it means *show year 0*. New scenarios (default `SteadyStateOverrideYear == 0`) render the steady-state panel at year 0 on first paint; users slide forward to see future projections. The auto-jump-to-`MinSteadyStateYear` behavior is gone.

`MinSteadyStateYear` is still computed and exposed as a separate field — it just no longer drives the default display.

### 4. Withdrawal panel is a proportional mix, not three alternatives

**Calculator** — same file. New helper:

```go
// withdrawalMixShares returns the proportional split of a gap-closing
// withdrawal across (tax-deferred, taxable, Roth) buckets, derived from
// the user's portfolio allocation. The three values sum to 1.
func withdrawalMixShares(s *models.WhatIfSettings) (pTD, pTX, pR float64) {
    pTD = s.TaxDeferredPercent / 100
    pR  = s.RothPercent / 100
    pTX = 1 - pTD - pR
    if pTX < 0 { pTX = 0 }
    return
}
```

For both the Current section and the steady-state section, when the relevant gap is positive:

| Field | Formula |
|---|---|
| `NetWithdrawalTaxDeferred` | `gap × pTD` |
| `NetWithdrawalTaxable` | `gap × pTX` |
| `NetWithdrawalRoth` | `gap × pR` |
| `GrossWithdrawalTaxDeferred` | `NetWithdrawalTaxDeferred / (1 − marginal_TD)` |
| `GrossWithdrawalTaxable` (year 0) | `NetWithdrawalTaxable` (basis = market, LTCG = 0) |
| `GrossWithdrawalTaxable` (steady state) | `NetWithdrawalTaxable / (1 − effective_LTCG)` |
| `GrossWithdrawalRoth` | `NetWithdrawalRoth` (no tax) |

The `Net*` values sum to the gap by construction. The `Gross*` values may exceed `Net*` whenever there is tax overhead (Tax-Deferred at any time; Taxable once gains have accrued).

The marginal/effective rates are computed via `estimateTaxSnapshot` against the bucket's share — not the full gap — so the bracket is more accurate for users with small TD shares.

### 5. Model fields

**Added** to `models.BudgetFitAnalysis`:

```go
NetWithdrawalTaxDeferred              float64 `json:"net_withdrawal_tax_deferred,omitempty"`
NetWithdrawalTaxable                  float64 `json:"net_withdrawal_taxable,omitempty"`
NetWithdrawalRoth                     float64 `json:"net_withdrawal_roth,omitempty"`
SteadyStateNetWithdrawalTaxDeferred   float64 `json:"steady_state_net_withdrawal_tax_deferred,omitempty"`
SteadyStateNetWithdrawalTaxable       float64 `json:"steady_state_net_withdrawal_taxable,omitempty"`
SteadyStateNetWithdrawalRoth          float64 `json:"steady_state_net_withdrawal_roth,omitempty"`
```

Existing `GrossWithdrawal*` / `SteadyStateGrossWithdrawal*` field names are preserved but their semantics have shifted from "if entire gap from this bucket" to "gross needed for this bucket's share of the mix".

### 6. Template rendering

The section heading flips from **"Gross Withdrawal Needed to Close Gap"** to **"Suggested Withdrawal Mix"**. Each row shows the `Net*` value (the after-tax contribution) prominently. When `Gross* > Net*` the row appends a parenthetical such as `(withdraw $5,361 at 27% marginal)` so the actual amount to pull is still visible. A final `Total (closes gap)` row displays the gap itself, making the "rows sum to this" relationship explicit.

The explanatory footer was rewritten:

> Each row is the after-tax contribution from that bucket, split proportionally to your portfolio allocation. The three rows sum to the gap. Tax-Deferred shows the gross withdrawal needed because part is lost to income tax; Taxable shows the gross at steady state when capital gains accrue.

## Tests

- `internal/services/retirement/calculator_test.go`
  - `TestSteadyStateBudgetFit/override=0_mirrors_current_values_when_min_steady_state_is_0` — new — pins the year-0 mirror behavior.
  - `TestSteadyStateBudgetFit/detects_steady_state_with_delayed_income` — rewritten to assert against `MinSteadyStateYear` (auto-detection) rather than `SteadyStateMonth`/`SteadyStateYear` (which now reflect the override).
  - `TestSteadyStateBudgetFit/finds_latest_starting_income_source` — same rewrite.
  - `TestSteadyStateBudgetFit/steady_state_income_higher_than_current` and `/steady_state_gap_smaller_than_current_gap` — set `SteadyStateOverrideYear` explicitly so the assertions still target a future-year view.

- `internal/services/retirement/calculator_expense_test.go`
  - `TestCalculateBudgetFit/withdrawal_mix_sums_to_gap_(proportional_to_allocation)` — new — pins the sum-to-gap invariant for the Current section.
  - `TestCalculateBudgetFit/steady-state_withdrawal_mix_sums_to_gap_with_tax_overhead_on_TD/TX` — rewritten from `steady-state_gross_withdrawal_mirrors_compute` to use a mixed allocation and the new semantics (net sum to gap, gross > net for TD/TX, gross == net for Roth, taxable cheaper than tax-deferred per dollar of net).
  - `TestCalculateBudgetFitIncomeStartMonth/deferred_income_excluded_from_month-0_breakdown_but_affects_steady_state` — sets override explicitly.
  - `TestFindSteadyStateMonthMultipleSources/*` and `TestFindSteadyStateMonth_ProjectedSocialSecurity` — switched to assert `MinSteadyStateYear` (the thing actually being tested).

- `internal/services/retirement/coverage_gaps_test.go`
  - `TestCalculateBudgetFit_SteadyStateRMD`, `TestCalculateBudgetFit_IncomeSourceFutureStart` — set `SteadyStateOverrideYear` explicitly.

- `internal/handlers/whatif/gross_withdrawal_render_test.go`
  - Heading assertions updated to `"Suggested Withdrawal Mix"`.
  - Fixture data populates the new `Net*` fields so the prominent display amounts are non-zero.

## Files Touched

- `internal/models/whatif.go` — added `NetWithdrawal*` and `SteadyStateNetWithdrawal*` fields.
- `internal/services/retirement/analysis/budget_fit.go` — new helper, rewritten gross-withdrawal blocks, override-honoring condition, year-0 mirror branch.
- `web/templates/components/whatif/budget-analysis.html` — slider `min="0"`, heading change, Net*-prominent rendering, total row.
- `internal/services/retirement/calculator_test.go`
- `internal/services/retirement/calculator_expense_test.go`
- `internal/services/retirement/coverage_gaps_test.go`
- `internal/handlers/whatif/gross_withdrawal_render_test.go`

## Risk / Impact

- **Existing scenarios with `SteadyStateOverrideYear == 0` and `MinSteadyStateYear > 0`** previously auto-displayed the future-year steady state. They will now display year 0 (mirroring Current) on first paint. Users see the slider at year 0 and must drag forward to view the future projection. This was an accepted trade-off — the user explicitly chose "simplest" over "preserve auto via a sentinel flag."
- **JSON shape grew** by six fields. All new fields use `omitempty`. Consumers that ignore unknown fields are unaffected.
- **`Score()` only uses `RequiredRate`** (Current side) — sustainability scoring is unaffected by the steady-state and mix changes.

## Follow-Ons

- The chosen split is *proportional to allocation*. A more sophisticated split (tax-efficient sequence, bracket-aware optimization, RMD-first) could replace it later — `withdrawalMixShares` is a single function and the model fields are stable enough to swap the strategy without touching the template.
- The edge-case test coverage outlined in `2026-05-13-gross-withdrawal-edge-cases-followon.md` (bracket crossings, IRMAA cliffs, NIIT thresholds, Social Security taxability) still applies to the new proportional-split math; tests should be added against the share-sized simulation rather than the full-gap simulation.
