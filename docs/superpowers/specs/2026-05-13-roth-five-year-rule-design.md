# Roth IRA Five-Year Rule -- Earnings Taxation

**Date:** 2026-05-13
**Status:** Draft, reviewed 2026-05-13
**Scope:** Model the Roth IRA qualified-distribution 5-tax-year rule for earnings withdrawals in retirement projections.

## Goal

When a user withdraws from their Roth bucket before the household's Roth IRA qualified-distribution clock is satisfied, the earnings portion of that withdrawal must be included in ordinary taxable income. Today the engine treats all Roth withdrawals as fully tax-free regardless of account age.

The modeled rule is the Roth IRA qualified-distribution rule for earnings:

- The 5-year period starts with the first tax year for which the user made a contribution or conversion contribution to any Roth IRA set up for their benefit.
- A distribution is qualified only after that 5-year period and after one qualifying event. For this feature, the modeled qualifying event is age 59.5+, because the retirement projection defaults to later-life scenarios.
- If the distribution is not qualified, regular contributions and conversion/rollover contribution amounts come out before earnings. Only earnings are taxable income.

Source: IRS Publication 590-B, "What Are Qualified Distributions?" and "Ordering Rules for Distributions" for Roth IRAs: https://www.irs.gov/publications/p590b

## Non-Goals

- **Conversion 5-year penalty clock** -- out of scope. This separate rule is about the 10% early-distribution penalty on conversion or rollover amounts and matters mainly under age 59.5. The projection's default `CurrentAge=65` plus the explicit 59.5+ scope means we should not build per-conversion penalty clocks here.
- **Under-59.5 Roth ordering and penalties** -- out of scope beyond preserving the existing tax-deferred early-withdrawal penalty behavior. Early-retirement/FIRE support needs a separate design.
- **Roth 401(k) rules** -- separate clocks and different plan semantics are out of scope. The engine currently lumps Roth IRA and Roth 401(k) assets into one `RothPercent` bucket. This feature preserves that simplification and treats the bucket as Roth IRA money for clock purposes. Documented limitation.
- **Inherited Roth rules, qualified disability/death exceptions, first-home exception** -- not modeled.

## Design

### Data model

Add one persisted field on `models.WhatIfSettings`:

```go
// RothFirstFundedYear is the calendar tax year of the user's first
// Roth IRA regular contribution or conversion contribution. It drives
// the Roth IRA qualified-distribution 5-tax-year rule for earnings.
// Zero means unknown/unset, not necessarily "no Roth exists."
RothFirstFundedYear int `json:"roth_first_funded_year,omitempty"`
```

Validation:

- Blank/zero is allowed.
- Non-zero value must be in `[1998, ParseStartYear(StartDate)+ProjectionYears]`.
- 1998 is the first tax year Roth IRAs existed.

UI copy may say "Year Roth IRA was first funded", but code and JSON should use "first funded" rather than "opened"; an unfunded opened account does not start the IRS clock.

### Projection-local state

All projection loops that carry `rothBalance` must also carry:

```go
initialRothBalance := s.PortfolioValue * (s.RothPercent / 100)
rothBasis := initialRothBalance
rothFirstFundedYear := s.RothFirstFundedYear
if rothFirstFundedYear == 0 && initialRothBalance > 0 {
    rothFirstFundedYear = ParseStartYear(s.StartDate) // engine package
}
```

This state is projection-local and must not mutate persisted settings. If `RothFirstFundedYear` is blank but the user already has a Roth balance, the conservative assumption is "first funded in the projection start tax year": earnings withdrawn in the first five projection tax years are taxable, then mature. If there is no starting Roth balance and the first modeled Roth funding is a conversion, that conversion year starts the local clock.

Apply this consistently in:

- `internal/services/retirement/engine/month.go` (`runMonthlyLoop`)
- `internal/services/retirement/analysis/monte_carlo.go` (`runSingleMonteCarloSimulation`)
- `internal/services/retirement/analysis/backtest.go` (`runSingleHistoricalSequence`)

### Clock helper

One central predicate in `internal/services/retirement/engine/loop_helpers.go`:

```go
// RothQualifiedDistributionClockSatisfied reports whether the Roth IRA
// 5-tax-year aging requirement is met for the given calendar year.
// firstFundedYear is a calendar tax year, not a projection-year offset.
func RothQualifiedDistributionClockSatisfied(firstFundedYear, calendarYear int) bool {
    if firstFundedYear <= 0 {
        return false
    }
    return calendarYear >= firstFundedYear+5
}
```

The projection year-to-calendar-year conversion belongs at call sites. In `engine` package files, call `ParseStartYear`; in `analysis` package files, call `engine.ParseStartYear`.

```go
calendarYear := ParseStartYear(s.StartDate) + currentYear
```

This avoids mixing the repo's existing `currentYear` projection offset with IRS calendar tax years.

### Conversion handling

`ApplyRothConversionAtYear` (in `engine/loop_helpers.go`) currently moves `conversionAmount` from `taxDeferredBalance` to `rothBalance`. New behavior:

1. Decrement `taxDeferredBalance`.
2. Increment `rothBalance`.
3. Increment `rothBasis` by `conversionAmount`.
4. If the projection-local `rothFirstFundedYear == 0`, set it to the conversion's calendar year.
5. Return the conversion amount as today.

Proposed signature:

```go
func ApplyRothConversionAtYear(
    s *models.WhatIfSettings,
    projectionYear int,
    taxDeferredBalance, rothBalance, rothBasis *float64,
    rothFirstFundedYear *int,
) float64
```

Do not write back to `s.RothFirstFundedYear` from the projection loop. Persisted settings change only through the settings form.

### Roth withdrawal -- basis-first ordering

Two call sites pull from Roth: `WithdrawForExpenses` and `ApplyBigTicketExpenseWithTaxableState`, both in `engine/portfolio_month.go`.

Both should use one small helper so ordering is identical:

```go
type RothWithdrawal struct {
    Total    float64
    Basis    float64 // Tax-free regardless of clock state
    Earnings float64 // Taxed as ordinary income iff clock not satisfied
}

func WithdrawFromRoth(needed float64, rothBalance, rothBasis *float64) RothWithdrawal {
    total := math.Min(needed, *rothBalance)
    basis := math.Min(total, *rothBasis)
    earnings := total - basis

    *rothBalance -= total
    *rothBasis -= basis
    if *rothBasis > *rothBalance {
        *rothBasis = *rothBalance
    }
    if *rothBasis < 0 {
        *rothBasis = 0
    }

    return RothWithdrawal{Total: total, Basis: basis, Earnings: earnings}
}
```

Ordering: pull from basis first, then earnings. Here "basis" is the model's aggregate of regular contributions plus conversion/rollover contribution amounts. The feature intentionally does not split regular contributions from conversion contributions because the under-59.5 conversion penalty clock is out of scope.

### Tax accumulation

`ExecutePortfolioCashFlowWithTaxableState` and `ExecuteTaxAwarePortfolioMonth` need the projection-local Roth basis pointer. `ExecuteTaxAwarePortfolioMonth` also needs the projection-local clock state in `PortfolioMonthInput`:

```go
RothBasis           *float64
RothFirstFundedYear int
TaxableRothEarningsBeforeCashFlow float64
```

`PortfolioCashFlowResult` should surface:

```go
RothBasisWithdrawal    float64
RothEarningsWithdrawal float64
TaxableRothEarnings    float64
```

During the fixed-point month calculation:

1. Roth withdrawals split into basis and earnings as part of cash flow.
2. `TaxableRothEarnings` equals `RothEarningsWithdrawal` only when `!RothQualifiedDistributionClockSatisfied(rothFirstFundedYear, calendarYear)`.
3. Recalculated tax snapshots include `TaxableRothEarningsBeforeCashFlow + TaxableRothEarnings` in ordinary income.

Big-ticket expenses are funded at the year boundary before the normal monthly cash-flow loop. `ApplyBigTicketExpenseWithTaxableState` therefore cannot keep returning only a bare remainder. It should return a structured result:

```go
type BigTicketFundingResult struct {
    UnfundedExpense      float64
    RothBasisWithdrawal  float64
    RothEarningsWithdrawal float64
    TaxableRothEarnings float64
}
```

`ApplyBigTicketItemsForYear` should sum those fields across same-year items. The monthly loop still adds `UnfundedExpense` to `bigTicketExpenseThisMonth`; it also passes the summed `TaxableRothEarnings` into that month's `PortfolioMonthInput.TaxableRothEarningsBeforeCashFlow`.

`ApplyTaxStateMonth` must include both pre-cash-flow and cash-flow taxable Roth earnings in the ordinary-income value passed to `ProjectionTaxAccumulator.ApplyMonth`, so annual summaries, MAGI-sensitive calculations, and year-level tax state agree with the converged monthly snapshot.

## UI

### Settings form

New input in the portfolio-allocation section of the what-if settings form:
- Label: "Year Roth IRA was first funded"
- Type: integer year, blank allowed
- Help text: "Used for the IRS 5-year rule on Roth IRA earnings. Leave blank if you do not know it; if you already have a Roth balance, projections assume the clock started in the projection start year."
- Validation: blank, or `1998 <= year <= projection start year + projection years`

### Projection display

- **Tax breakdown row**: when `TaxableRothEarnings > 0` in any year, surface it as a labeled line under federal income tax, using the existing cost styling. Label: "Roth earnings (5-year rule)".
- **Roth bucket card**: when `RothFirstFundedYear > 0` and the clock is unsatisfied, show a small indicator: "5-year clock matures in {RothFirstFundedYear+5}". Indicator disappears once mature.
- **Existing-balance nudge**: when `RothPercent > 0` and `RothFirstFundedYear == 0`, show a one-line settings hint: "You have a Roth balance but have not set the first-funded year. Projections assume the 5-year clock starts in the projection start year." Dismissible.

No new dashboards, no new chart series.

## Tests

All engine tests should live in `internal/services/retirement/engine/`, following the existing table-driven style unless the scenario is explicitly an integration test.

1. **`TestRothQualifiedDistributionClockSatisfied`** -- covers `firstFundedYear=0` false, `calendarYear < first+5` false, `calendarYear == first+5` true, `calendarYear > first+5` true, and calendar-year vs projection-year naming.

2. **`TestApplyRothConversionAtYear_BasisAndClock`** -- verifies conversion increments `rothBasis` dollar-for-dollar with balance, sets the projection-local `rothFirstFundedYear` when blank, and preserves it when already set.

3. **`TestWithdrawFromRoth_BasisFirstOrdering`** -- verifies small withdrawal pulls only basis, large withdrawal exhausts basis then takes earnings, and full withdrawal zeroes balance without leaving basis above balance.

4. **`TestRothEarnings_TaxedWhenClockUnsatisfied`** -- integration scenario: first funded in projection start year, growth creates earnings, withdrawal before `first+5`; assert earnings are added to ordinary income/taxable annual inputs.

5. **`TestRothEarnings_TaxFreeAfterClock`** -- same scenario, withdrawal in or after `first+5`; assert Roth earnings are not added to ordinary income.

6. **`TestRothWithdrawal_NoTaxWhenAllBasis`** -- withdraw from basis only inside the clock window; assert zero taxable Roth earnings.

7. **`TestBigTicketRothEarnings_FeedTaxState`** -- big-ticket expense funded from Roth earnings before the monthly cash-flow loop; assert taxable Roth earnings flow into that month's tax snapshot and annual accumulator.

8. **`TestExistingRothBlankYear_UsesProjectionStartClock`** -- `RothPercent > 0`, `RothFirstFundedYear == 0`; assert a withdrawal in projection year 0 can tax earnings, while the same withdrawal in projection year 5 is qualified.

9. **`TestRothConversionDoesNotMutateSettings`** -- run a projection with blank persisted `RothFirstFundedYear` and a conversion; assert `s.RothFirstFundedYear` remains zero after the run.

10. **`TestProjectionLoops_RothStateParity`** -- cover deterministic, Monte Carlo single-sequence, and historical backtest paths enough to prove they all initialize and carry `rothBasis`/`rothFirstFundedYear`.

11. **`TestExistingScenario_NoSilentRegression`** -- scenario with `RothPercent > 0`, `RothFirstFundedYear == 0`, and no Roth withdrawals during projection; assert projection outputs are unchanged aside from new zero-valued fields.

## Migration / Backwards Compatibility

- Existing scenario files lack `RothFirstFundedYear`. JSON unmarshal yields zero.
- If `RothPercent > 0` and the field is zero, projection-local state assumes the Roth was first funded in `ParseStartYear(StartDate)`. This taxes earnings withdrawals during the first five projection tax years, then treats later earnings withdrawals as qualified.
- If `RothPercent == 0` and the field is zero, the first modeled Roth conversion starts the projection-local clock. If no conversion occurs, the clock remains unsatisfied but no Roth withdrawal can occur.
- Scenarios with Roth assets but no Roth withdrawals should not change numerically.
- Scenarios with Roth earnings withdrawals in early projection years may show higher taxes. The settings nudge explains the assumption and lets users enter the real first-funded year.

## Impact / Risk

GitNexus impact analysis after refreshing the index on 2026-05-13:

- `WhatIfSettings` -- CRITICAL: 32 impacted nodes, 19 affected processes across Retirement, Whatif, Prepare, and Analysis.
- `ApplyRothConversionAtYear` -- CRITICAL: direct callers are `runMonthlyLoop`, `runSingleMonteCarloSimulation`, and `runSingleHistoricalSequence`; downstream impact includes projection chart handlers, Monte Carlo, historical backtest, sensitivity/failure analysis, tax optimizer scoring, and orchestrator flows.
- `ExecuteTaxAwarePortfolioMonth` -- CRITICAL with the same three projection-loop direct callers and broad downstream WhatIf/analysis impact.
- `ApplyTaxStateMonth` -- CRITICAL: same projection-loop direct callers; annual tax state changes affect MAGI, tax summaries, IRMAA-sensitive calculations, and analysis flows.
- `WithdrawForExpenses` and `ApplyBigTicketExpenseWithTaxableState` -- GitNexus reports LOW, but behavior is financially sensitive because they define bucket ordering and need focused tests.

Implementation should land as one cohesive engine/model/UI change with a broad retirement test run, not as isolated helper edits.

## Out-of-Scope Follow-ons (documented, not built)

- Conversion 5-year penalty clock for early-retirement scenarios under age 59.5.
- Separate Roth IRA, Roth 401(k), Roth SEP, and Roth SIMPLE buckets with distinct clocks.
- User-entered Roth contribution/conversion basis. The first implementation uses modeled aggregate basis only.
- Educational tooltip explaining the rule in more detail.
