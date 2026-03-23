# Code Review: Financial Concepts

Date: 2026-03-23

## Findings

1. High: `internal/services/retirement/calculator.go:182` only withdraws `min(monthlyRMD, remaining need)` when expenses are below the scheduled RMD, and `internal/services/retirement/calculator.go:477` only reinvests the full RMD when `neededFromPortfolio <= 0`. In months with a small positive spending gap, the model under-withdraws the legally required RMD, which overstates remaining tax-deferred balances and understates taxable income.

2. High: `internal/services/retirement/calculator.go:172` makes early tax-deferred access purely a user-controlled delay toggle; there is no modeling of the age-59.5 early-distribution penalty anywhere in the retirement engine. That means scenarios that tap traditional IRA/401(k) money before 59.5 can look materially better than reality unless the user manually compensates.

3. Medium: `internal/services/retirement/backtest.go:266` drops surplus income in the historical path. Unlike the main projection at `internal/services/retirement/calculator.go:473`, the backtest never deposits `abs(neededFromPortfolio)` into taxable when income exceeds expenses, so historical success rates and ending balances are biased downward.

4. Medium: The steady-state panel is inconsistent with the main projection in allocation mode. Main projections correctly fall back to allocation-derived returns at `internal/services/retirement/calculator.go:428`, but steady-state RMD and required-rate estimates at `internal/services/retirement/calculator.go:698` and `internal/services/retirement/calculator.go:711` compound with `InvestmentReturn` directly, which is `0` by default. That can materially understate future balances and distort the steady-state gap.

## Assumptions

- I treated this as a tax-aware planner because it models RMDs, Roth conversions, filing status, and state tax rate. If the projection is intended to be explicitly pre-tax, findings 1 and 2 still need UI disclosure, but they become modeling limitations rather than pure bugs.
- The hardcoded withdrawal order `taxable -> Roth -> tax-deferred` at `internal/services/retirement/calculator.go:192` and `internal/services/retirement/calculator.go:200` is a planning choice, not necessarily a defect, but it is not the standard "traditional" sequence many planners use.

## Financial Concept Check

- RMD start age 73 is aligned with current IRS rules.
- The Uniform Lifetime Table factors used in the RMD logic are directionally correct for standard owner calculations.
- The monthly return conversion uses a geometric formula instead of simple division, which is financially sound.
- The biggest conceptual gaps are incomplete RMD enforcement, missing pre-59.5 penalty treatment, and inconsistent handling across projection vs. backtest vs. steady-state views.

## Resolution

All four findings were addressed on 2026-03-23:

1. **RMD enforcement (Fixed):** After `withdrawForExpenses`, any unmet RMD (i.e., `monthlyRMD - rmdUsed`) is now withdrawn from tax-deferred and reinvested into taxable via `reinvestRequiredRMD`. Applied to all three projection paths: main projection, Monte Carlo, and historical backtest.

2. **Early distribution penalty (Fixed):** Added `earlyWithdrawalPenaltyRate()` which returns 10% when `currentAge + currentYear < 60` (approximation for the IRS age-59½ threshold). The penalty is threaded through `withdrawForExpenses` and `applyBigTicketExpense` — non-RMD tax-deferred withdrawals before 59½ reduce spending power by 10%, requiring a larger gross withdrawal to meet the same spending need. The `EarlyPenaltyPaid` field was added to `withdrawalBreakdown` for tracking.

3. **Backtest surplus reinvestment (Fixed):** Added `taxableBalance += math.Abs(neededFromPortfolio)` to `backtest.go` when income exceeds expenses, matching the main projection's behavior.

4. **Steady-state rate (Fixed):** The steady-state panel now falls back to `s.GetExpectedReturnFromAllocation()` when `InvestmentReturn` is 0 (allocation mode), ensuring the estimated tax-deferred balance and portfolio value compound at the same blended rate used by the main projection.

## Verification

- `go test ./...` passed after all fixes were applied.
- Test coverage for `internal/services/retirement/` improved from 55.3% to 95.0% (6 new test files, ~120 test cases).

## Sources

- IRS Publication 590-B: https://www.irs.gov/publications/p590b
- IRS Traditional and Roth IRAs: https://www.irs.gov/retirement-plans/traditional-and-roth-iras
- Fidelity normal IRA withdrawals: https://www.fidelity.com/retirement-ira/ira-normal-withdrawal
- Fidelity tax-savvy withdrawals: https://www.fidelity.com/viewpoints/retirement/tax-savvy-withdrawals
