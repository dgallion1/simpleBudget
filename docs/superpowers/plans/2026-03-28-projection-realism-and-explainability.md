# Portfolio Projection Realism And Explainability Plan

> **For agentic workers:** Use checkbox progress updates in this file. Prefer implementing this plan in order. Each phase should leave the app in a releasable state with tests passing.

**Goal:** Make the What-If portfolio projection more realistic, easier to trust, and easier to explain by improving tax treatment, monthly timing, taxable-account modeling, and UI transparency.

**Current baseline:** `RunProjection()` in `internal/services/retirement/calculator.go` runs a deterministic month-by-month nominal projection. The loop itself iterates monthly, but inflation, COLA, and expense growth stair-step at year boundaries because the helpers use integer division (`month / 12`). Taxes are computed post-hoc via `TaxAnalysis` but are **not** deducted from cash flow during the projection — the curve is effectively pre-tax. Taxable assets are modeled as a simple balance bucket with no cost-basis tracking. The chart UI shows only nominal values and does not explain what is driving the curve. `HistoricalBacktestResult` already carries `FinalBalanceReal` and `CumulativeInflation`, but `ProjectionMonth` has no real-dollar fields.

**Primary files:**
- `internal/services/retirement/calculator.go`
- `internal/services/retirement/tax.go`
- `internal/models/whatif.go`
- `internal/handlers/whatif/handlers.go`
- `web/templates/components/whatif/projection-chart.html`
- `web/templates/components/whatif/budget-analysis.html`
- `web/templates/components/whatif/spending-phases.html`
- `internal/services/retirement/*_test.go`

**Non-goals for the first pass:**
- Perfect tax-law fidelity for every filing status edge case
- Full brokerage-lot accounting
- Intramonth market simulation in the deterministic baseline

---

## Phase 1: Make The Deterministic Projection Internally Consistent At Monthly Resolution

**Why first:** This improves realism without changing the overall structure of the calculator too much, and it creates the foundation needed for tax-aware and real-dollar reporting.

- [x] Convert year-boundary stair-stepping to continuous monthly compounding
  - The loop is already monthly. The fix is in the helpers that use integer division (`month / 12`) — switch to `float64(month) / 12.0` for smooth compounding.
  - Applies to: living-expense inflation, COLA, expense-source inflation, and healthcare growth.
  - Spending-phase transitions should remain age-threshold based and discrete. Only the inflation/compounding within a phase should become monthly-smooth.

- [x] Refactor the main `RunProjection()` loop in `internal/services/retirement/calculator.go`
  - The displayed chart comes from `RunProjection()`, and that loop currently updates `currentLivingExpenses` only inside the annual `if m%12 == 0` block.
  - Move living-expense escalation to month-level logic so the core projection path itself no longer stair-steps.
  - Keep annual-only events annual: RMD recalculation, Roth conversions, big-ticket items, and chain-transition checks can remain at year boundaries unless explicitly reworked later.

- [x] Update `IncomeSource.GetAdjustedAmount()` in `internal/models/income_source.go`
  - Currently uses `yearsActive := monthsActive / 12` (integer division) → stair-steps COLA annually.
  - Change to `math.Pow(1+is.COLARate, float64(monthsActive)/12.0)` for smooth monthly compounding.

- [x] Update `ExpenseSource.GetAdjustedAmount()` in `internal/models/income_source.go`
  - Currently uses `yearsSinceStart := (month - startMonth) / 12` (integer division) → stair-steps annually.
  - Change to `float64(month - startMonth) / 12.0` for smooth monthly compounding.

- [x] Update healthcare growth in `internal/models/healthcare.go`
  - `HealthcarePerson.GetMonthlyCost()` currently uses `yearsElapsed := month / 12` and therefore stair-steps pre/post-Medicare inflation annually.
  - Convert to continuous monthly compounding while preserving Medicare transition ages.

- [x] Update living-expense inflation in `CalculateTotalExpenses()` and `CalculateExpenseBreakdown()`
  - Both use `years := month / 12` (integer division) then `math.Pow(1+rate, float64(years))`.
  - Change to `float64(month) / 12.0` for continuous compounding.
  - Preserve spending-phase multiplier semantics: phase transitions remain age-based, but inflation within each active phase compounds monthly.

- [x] Add real-dollar fields to `ProjectionMonth` in `internal/models/whatif.go`
  - Add `CumulativeInflation`, `PortfolioBalanceReal`, `TotalExpensesReal`, `TotalIncomeReal`.
  - Follow the precedent set by `HistoricalBacktestResult` which already has `FinalBalanceReal` and `CumulativeInflation`.
  - Populate in `RunProjection()` each month: `realValue = nominalValue / cumulativeInflationFactor`.

- [x] Apply the same monthly-resolution semantics to Monte Carlo and historical backtest in this phase
  - `runSingleMonteCarloSimulation()` and `internal/services/retirement/backtest.go` currently mirror the same annual-boundary inflation/spending updates.
  - Do not leave deterministic projections monthly-smooth while Monte Carlo/backtest remain annual-step; that would create contradictory answers for the same settings.
  - If exact monthly smoothing is not feasible for stochastic inflation in Monte Carlo, document the approximation explicitly and keep deterministic/backtest/Monte Carlo as close as possible.

- [x] Add/extend focused tests (these files already exist)
  - `calculator_expense_test.go`: verify living-expense inflation no longer stair-steps at year boundaries
  - `calculator_test.go`: verify COLA and expense-source inflation compound monthly
  - Add model-level tests for healthcare monthly compounding across Medicare transition
  - Add parity tests or assertions for Monte Carlo/backtest month-resolution assumptions where practical

**Acceptance criteria:**
- Inflation-driven values change smoothly month-to-month instead of jumping at year boundaries.
- New real-dollar fields are available on each `ProjectionMonth`.
- Deterministic, Monte Carlo, and historical backtest paths no longer disagree merely because one path compounds monthly and another still stair-steps annually.
- Existing tests still pass (values will shift slightly due to continuous vs. discrete compounding).

---

## Phase 2: Add Tax-Aware Cash Flow To The Base Projection

**Why second:** This is the biggest realism gap in the current projection. The chart currently behaves like most cash flows are fully spendable.

- [x] Define the tax policy for the base projection in a short design note at the top of `internal/services/retirement/calculator.go`
  - Treat pension and tax-deferred withdrawals as ordinary income.
  - Treat Social Security using the existing tax engine assumptions.
  - Treat Roth withdrawals as non-taxable.
  - Interim Phase 2 assumption: taxable-account sales remain modeled as return-of-principal cash withdrawals with no capital-gains realization until Phase 4 introduces basis tracking.
  - Taxable-account dividends/realized gains become first-class taxable events in Phase 4.
  - Keep Roth conversion tax visible and deducted from available cash unless explicitly configured otherwise.

- [x] Extend the existing tax infrastructure for in-loop use
  - `TaxCalculator`, `TaxAnalysis`, and `YearlyTaxSummary` already exist in `tax.go` and `whatif.go`.
  - Add a year-to-date accumulator struct that tracks ordinary income, SS income, Roth conversions, and taxes paid — resetting at each year boundary.
  - The existing `CalculateTotalTax()` can be called at year boundaries or allocated monthly.
  - Be explicit that Phase 2 taxes ordinary income, Social Security, RMDs, and Roth conversions; it does not yet tax capital gains created by taxable-account sales.

- [x] Add a tax-aware monthly cash-flow function
  - Input: gross income sources, withdrawals, RMD, conversion activity, tax config, year offset.
  - Output: estimated taxes due this month (annualized estimate / 12).

- [x] Integrate taxes into `RunProjection()` loop
  - Currently taxes are only computed post-hoc (`// Note: Tax impact tracked separately via TaxAnalysis` at line ~445).
  - Subtract taxes from available net income before deciding how much needs to come from the portfolio.
  - Ensure RMDs still behave as forced gross withdrawals.
  - Ensure Roth conversions trigger tax cost in the same year.

- [x] Extend `ProjectionMonth` and summary models in `internal/models/whatif.go`
  - Add fields like `TaxesPaid`, `GrossIncome`, `NetIncome`, `TaxableWithdrawals`, `RothConversions`.

- [x] Update the budget analysis path in `CalculateBudgetFit()`
  - Add a visible distinction between pre-tax and after-tax monthly gap.
  - Prefer after-tax as the default displayed gap once the new logic lands.

- [x] Add/extend tests (these files already exist)
  - `tax_test.go`: monthly allocation of annual tax burden
  - `rmd_tax_test.go`: RMD tax handling with in-loop deduction
  - `calculator_coverage_test.go`: Roth conversion tax reduces available spending cash
  - `calculator_test.go`: after-tax projection depletes portfolio sooner than pre-tax for the same scenario

**Acceptance criteria:**
- The chart and final balance reflect estimated taxes.
- Budget-analysis shortfall numbers reconcile with the tax-aware projection.
- The Phase 2 implementation is explicit about what remains deferred until Phase 4, so taxable-account treatment is incomplete by design rather than ambiguous.

---

## Phase 3: Make Cash-Flow Timing Explicit

**Why third:** The current engine implicitly applies growth before withdrawals, which is favorable. This should be explicit and configurable.

- [x] Add a projection timing setting to `WhatIfSettings` in `internal/models/whatif.go`
  - Suggested enum values: `start_of_month`, `mid_month`, `end_of_month`
  - Default to `end_of_month` for backward compatibility or `mid_month` if a more neutral default is preferred after review.

- [x] Refactor `RunProjection()` ordering logic
  - `start_of_month`: withdraw/deposit cash flows, then apply growth
  - `end_of_month`: apply growth, then withdraw/deposit cash flows
  - `mid_month`: apply half growth, cash flow, then remaining half growth

- [x] Mirror the same ordering in Monte Carlo and historical backtest paths
  - `runSingleMonteCarloSimulation()` (calculator.go:1438)
  - Historical backtest flow in `internal/services/retirement/backtest.go`
  - All three paths must use the same timing convention to avoid contradictory results.

- [x] Expose the timing setting in the What-If UI
  - Add a small selector in `web/templates/components/whatif/portfolio-settings.html` or `rate-assumptions.html`

- [x] Add tests
  - Deterministic test asserting `start_of_month` produces a lower final balance than `end_of_month` in a withdrawal-heavy scenario
  - Monte Carlo/backtest parity tests to ensure the selected timing policy is honored consistently

**Acceptance criteria:**
- Timing is no longer implicit.
- Users can choose a conservative timing convention.

---

## Phase 4: Improve Taxable-Account Realism

**Why fourth:** Once taxes exist in the base projection, the taxable account should stop acting like a generic cash bucket.

- [x] Introduce a taxable-account state struct in `internal/services/retirement/calculator.go`
  - Currently the taxable account is a plain `float64` balance (`taxableBalance`) with no basis tracking.
  - New struct should include at least `MarketValue`, `CostBasis`, `UnrealizedGains`, `QualifiedDividendYield`, `NonQualifiedDividendYield`, and `RealizedGainsYTD`.

- [x] Extend settings in `internal/models/whatif.go`
  - Add optional taxable-account assumptions:
    - dividend yield
    - qualified dividend share
    - capital-gains distribution rate
    - optional turnover / realization assumption

- [x] Update taxable-account growth handling in `RunProjection()`
  - Split taxable return into:
    - unrealized appreciation
    - dividends distributed during the year
    - capital-gain realization triggered by sales
  - Feed taxable dividends and realized gains into the new tax-aware cash-flow layer.

- [x] Update withdrawal logic
  - Currently `*taxableBalance -= fromTaxable` with no tax consequence.
  - Selling from taxable should reduce market value and proportionally consume basis/gains (average-cost method).
  - Only the realized gain portion should be taxed as capital gain via the Phase 2 tax layer.

- [x] Keep the first version intentionally approximate
  - Average-cost basis is acceptable.
  - Per-lot accounting should remain out of scope unless later required.

- [x] Add tests
  - Selling only basis should not create capital-gains tax.
  - Selling appreciated taxable assets should realize proportional gains.
  - High dividend yield should reduce final balance relative to the old simple-bucket model.

**Acceptance criteria:**
- Taxable-account tax drag is reflected in the projection.
- Realized gains and dividends are visible in tests and month-level projection data.

---

## Phase 5: Improve Projection Explainability In The UI

**Why fifth:** Even correct math is hard to trust if users cannot see the drivers.

- [x] Add a nominal vs real toggle to the projection card
  - Update `web/templates/components/whatif/projection-chart.html` (currently a simple card with no controls)
  - Update `handleWhatIfProjectionChart` (handlers.go:1007) to accept a query param like `display_dollars=nominal|real`
  - When `real` is selected, use the new `PortfolioBalanceReal` field from Phase 1

- [x] Add year markers and optional event markers to the chart payload
  - Include scenario-chain transitions, Social Security start, pension start, Medicare transitions, and RMD start when available.
  - Keep the initial rendering simple if Plotly annotation density becomes too high.

- [x] Add a year-by-year projection explainer table/card
  - Suggested new partial: `web/templates/components/whatif/projection-breakdown.html`
  - Show for each year:
    - starting balance
    - growth
    - gross income
    - taxes
    - expenses
    - withdrawals
    - ending balance

- [x] Add reconciliation messaging to the card
  - Example: "Final balance is lower in real dollars because inflation over 31 years was X% cumulative."
  - Example: "Taxes consumed Y% of gross retirement cash flow."

- [x] Make the budget panel and projection panel use the same wording
  - If the chart is after-tax, the budget gap card must also say after-tax.
  - If real-dollar mode is active, labels should say "today's dollars".

- [x] Add UI tests or render tests where practical
  - `internal/templates/render_test.go` (already exists) for new partial rendering
  - Extend handler tests in `internal/handlers/whatif` for chart payload shape with nominal/real variants

**Acceptance criteria:**
- Users can see both nominal and inflation-adjusted paths.
- Users can inspect a per-year reconciliation of how the portfolio changed.

---

## Recommended Delivery Order

- [x] Slice 1: Monthly compounding + real-dollar fields
- [x] Slice 2: Tax-aware base projection
- [x] Slice 3: Nominal/real toggle + yearly explainer table
- [x] Slice 4: Timing selector
- [x] Slice 5: Taxable-account realism

This order gets the biggest trust improvements into the app earlier, while postponing the most invasive taxable-basis work until the tax-aware projection is already in place.

---

## Verification Checklist

- [x] `go test ./internal/services/retirement ./internal/models ./internal/handlers/whatif ./internal/templates`
- [ ] Manually verify the current live scenario in `data/settings/whatif.json` still renders and that the chart matches the backend data series.
- [ ] Verify that nominal and real chart series reconcile using `PortfolioBalanceReal = PortfolioBalance / CumulativeInflation`.
- [ ] Verify that enabling taxes lowers or leaves unchanged the final balance for scenarios with taxable income.
- [ ] Verify that `start_of_month` timing is never more optimistic than `end_of_month` in a pure-withdrawal scenario.

---

## Notes For Implementation

- Keep `RunProjection()`, Monte Carlo, and historical backtest semantics aligned. If one path uses tax-aware monthly timing and another does not, users will get contradictory answers.
- Avoid a flag day rewrite. Add fields and helper functions first, then switch the projection loop once tests cover the new behavior.
- Preserve backward compatibility in JSON where practical by using zero-value defaults and `omitempty`.
- Prefer small, test-backed slices over one large refactor. The projection engine already has broad test coverage; use that structure rather than bypassing it.
