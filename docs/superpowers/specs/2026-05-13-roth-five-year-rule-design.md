# Roth IRA Five-Year Rule — Earnings Taxation

**Date:** 2026-05-13
**Scope:** Add accurate modeling of the Roth IRA "forever" 5-year rule for earnings withdrawals in retirement projections.

## Goal

When a user withdraws from their Roth bucket and their Roth account hasn't existed for 5 tax years yet, the **earnings portion** of that withdrawal must be added to ordinary taxable income. Today the engine treats all Roth withdrawals as fully tax-free regardless of account age.

## Non-Goals

- **Conversion 5-year clock (10% penalty)** — out of scope. This rule only bites when the account holder is under 59½. The projection's default `CurrentAge=65` plus the explicit decision to scope to 59½+ scenarios means the conversion penalty clock is never triggered. We may revisit if early-retirement (FIRE) scenarios become a priority.
- **Per-conversion clocks** — same reasoning. With everyone over 59½, all conversions share the single forever clock.
- **Roth 401(k) rules** — separate clock from Roth IRA, but the engine currently lumps both into one `RothPercent` bucket. We'll preserve that simplification and treat the bucket as a single Roth IRA for clock purposes. Documented limitation.
- **Inherited Roth rules, qualified disability/death exceptions** — niche, not modeled.

## Design

### Data model

One new persisted field on `WhatIfSettings`:

```go
// RothOpenedYear is the calendar year of first Roth IRA contribution
// or conversion. Drives the IRS "forever" 5-year rule for earnings
// tax-free treatment. 0 means no Roth has been funded yet — the engine
// treats this as "clock not satisfied" (worst case) for projection
// purposes.
RothOpenedYear int `json:"roth_opened_year,omitempty"`
```

Validation: 0 (blank) or year in `[1998, CurrentCalendarYear + 50]`.

### In-loop state (engine/month.go)

Alongside the existing `rothBalance` float, add `rothBasis`:

```go
rothBasis := s.PortfolioValue * (s.RothPercent / 100)
```

Starting basis equals the starting Roth balance — i.e., we assume the entire pre-projection Roth balance is basis. Rationale: a user with a mature pre-projection Roth has `RothOpenedYear` set to a pre-projection year, so the clock is already satisfied and the split is tax-irrelevant. A user with a freshly-opened Roth has little growth yet, so basis ≈ balance is realistic.

### Clock helper

One central predicate in `engine/loop_helpers.go`:

```go
// RothForeverClockSatisfied reports whether the 5-tax-year aging
// requirement for tax-free Roth earnings withdrawal is met for the
// given calendar year. Returns false when RothOpenedYear is unset
// (worst-case assumption).
func RothForeverClockSatisfied(s *models.WhatIfSettings, currentYear int) bool {
    if s.RothOpenedYear <= 0 {
        return false
    }
    return currentYear >= s.RothOpenedYear+5
}
```

This is the only place the 5-year rule is encoded.

### Conversion handling

`ApplyRothConversionAtYear` (in `engine/loop_helpers.go`) currently moves `conversionAmount` from `taxDeferredBalance` to `rothBalance`. New behavior:

1. Increment `rothBasis` by `conversionAmount` (1:1 with balance).
2. If `s.RothOpenedYear == 0`, set it to `currentYear` (clock starts on first conversion).

Signature gains `rothBasis *float64`. The `RothOpenedYear` mutation is on the settings struct, which is already accessible in the call.

### Roth withdrawal — basis-first ordering

Two call sites pull from Roth: `WithdrawForExpenses` and `ApplyBigTicketExpenseWithTaxableState`, both in `engine/portfolio_month.go`.

Both currently decrement `rothBalance` by a single number. New behavior — they return (and the caller sums) a split:

```go
type RothWithdrawal struct {
    Basis    float64 // Tax-free regardless of clock state
    Earnings float64 // Taxed as ordinary income iff clock not satisfied
}
```

Ordering: pull from basis first (`min(needed, rothBasis)`), spillover comes from earnings (= `rothBalance - rothBasis`). Decrement basis and balance accordingly. Earnings withdrawn = `total - basisWithdrawn`.

### Tax accumulation

`ApplyTaxStateMonth` (in `engine/loop_helpers.go`) currently does not add Roth withdrawals to taxable income. New behavior — when `!RothForeverClockSatisfied(s, currentYear)`, add the **earnings portion only** to the ordinary-income accumulator.

Implementation: surface `EarningsWithdrawnFromRoth` on the `TaxAwarePortfolioMonthResult.CashFlow` struct, plumb it into `ApplyTaxStateMonth`, conditionally include in the income accumulator.

## UI

### Settings form

New input in the portfolio-allocation section of the what-if settings form:
- Label: "Year Roth IRA was first funded"
- Type: integer (year), blank-allowed
- Help text: "IRS 5-year rule for tax-free earnings. Leave blank if you've never had a Roth — we'll assume the rule isn't satisfied (worst-case)."
- Validation: blank, or `1998 ≤ year ≤ currentCalendarYear + 50`

### Projection display

- **Tax breakdown row**: when `taxableRothEarnings > 0` in any year, surface it as a labeled line under federal income tax, using the existing cost-styling (red, per project styling rule). Label: "Roth earnings (5-year rule)".
- **Roth bucket card**: when `RothOpenedYear > 0` and clock unsatisfied, show a small indicator: "5-year clock matures in {RothOpenedYear+5}". Indicator disappears once mature.
- **Existing-balance nudge**: when `RothPercent > 0` and `RothOpenedYear == 0`, show a one-line settings hint: "You have a Roth balance but haven't set the open year. Projections assume the 5-year clock isn't satisfied." Dismissible.

No new dashboards, no new chart series.

## Tests

All in `internal/services/retirement/engine/`, following the existing table-driven style.

1. **`TestRothForeverClockSatisfied`** — table-driven, covers: `RothOpenedYear=0` (false), `currentYear < opened+5` (false), `currentYear == opened+5` (true), `currentYear > opened+5` (true), `currentYear == opened` (false).

2. **`TestApplyRothConversionAtYear_BasisAndClock`** — verifies conversion increments `rothBasis` dollar-for-dollar with balance, sets `RothOpenedYear` when blank, preserves it when already set.

3. **`TestRothWithdrawal_BasisFirstOrdering`** — verifies basis-first split: small withdrawal pulls only basis, large withdrawal exhausts basis then takes earnings, full withdrawal zeroes both.

4. **`TestRothEarnings_TaxedWhenClockUnsatisfied`** — integration scenario: open Roth in year 0, convert, withdraw in year 2 from earnings; assert earnings are added to ordinary income.

5. **`TestRothEarnings_TaxFreeAfterClock`** — same scenario, withdraw in year 6; assert zero tax impact.

6. **`TestRothWithdrawal_NoTaxWhenAllBasis`** — withdraw from basis only (within clock window); assert zero tax impact (basis is always tax-free).

7. **`TestExistingScenario_NoSilentRegression`** — scenario with `RothPercent > 0`, `RothOpenedYear == 0`, **no Roth withdrawals** during projection; assert projection unchanged from pre-feature output.

## Migration / Backwards Compatibility

- Existing scenario files lack `RothOpenedYear`. JSON unmarshal yields zero value (= worst-case, clock unsatisfied).
- Scenarios with `RothPercent > 0` that don't withdraw from Roth see no projection change.
- Scenarios with `RothPercent > 0` that do withdraw from Roth in early projection years see new tax on earnings. The settings-form nudge surfaces this clearly. Users who know their actual Roth open year can correct it; users with mature Roths simply set the field to a pre-projection year.

## Out-of-Scope Follow-ons (documented, not built)

- Conversion 5-year penalty clock for early-retirement (under-59½) scenarios.
- Separate Roth 401(k) bucket with its own clock.
- Educational tooltip in UI explaining the rule (text-only enhancement, no calc impact).
