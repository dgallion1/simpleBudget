# What-If Retirement Verification

This note records the verification pass for the live What-If planner values.

**Last refreshed:** 2026-05-06 — post-`b978aa9` compounding fix and
post-`feat/whatif-fixes` PRs 1-8 (F-001, F-018, F-026, F-029, F-049,
F-057, F-063, F-065 closed).

---

## Scope

Verified the live `/whatif` page against the calculator output loaded from
`data/settings/whatif.json`. The check covered:

- Monthly Budget Analysis current and steady-state values
- Present Value Analysis values
- Portfolio Longevity final nominal and real balances
- Projection explainability percentages for taxes and inflation

## Scenario Inputs

The saved Current Plan scenario used these key inputs:

- Portfolio value: `$2,000,000`
- Monthly living expenses: `$8,100` (base)
- Projection start month: `2026-04`
- Projection length: `31` years
- Projection timing: `end_of_month`
- Discount rate: `5%`
- Inflation rate: `4.2%`
- Investment return: `6%`
- Tax-deferred allocation: `80%`
- Roth allocation: `0%`
- Per-account stock allocation: `80%` tax-deferred, `20%` taxable
- Healthcare: Darrell Gallion on Medicare (`$650/mo` current, `$550/mo` Medicare), Christine on ACA (`$1,600/mo` current)
- Income sources: Christine Pension `$1,200/mo` from month 48 (all other sources removed)
- Social Security: Darrell FRA benefit `$4,100/mo` claimed at 67, spouse benefit `$1,500/mo` claimed at 62
- Spending phases enabled with `Go-Go` at `1.05x`, `Slow-Go` at `0.95x` from age 73, and `No-Go` at `0.85x` from age 85
- Steady-state override year: `4`

Settings load normalizes `start_date`, derives working ages from `persons`, and
recomputes linked healthcare ages before running the planner. For this scenario,
the normalized planning ages are primary `67` and spouse `54`.

## Verified Values

The live page matched the calculator output exactly after UI rounding:

| Panel | Value | Verified output |
| --- | --- | --- |
| Current budget | Monthly expenses | `$10,755.00` |
| Current budget | Monthly income | `$4,100.00` |
| Current budget | Estimated taxes | `$0.00` |
| Current budget | Taxable Social Security | `0%` |
| Current budget | Monthly gap | `$6,655.00` shortfall |
| Current budget | Required rate | `4.0%` |
| Steady state | Year | `4` |
| Steady state | Monthly expenses | `$14,367.71` |
| Steady state | Monthly income | `$7,336.52` |
| Steady state | Estimated RMD | `$8,638.74` |
| Steady state | Estimated taxes | `$1,425.47` |
| Steady state | Taxable Social Security | `85%` |
| Steady state | Monthly gap | `-$182.08` surplus |
| Steady state | Required rate | `0.0%` |
| Present value | Total resources | `$2,221,209.18` |
| Present value | PV income | `$221,209.18` |
| Present value | PV expenses | `$3,231,862.12` |
| Present value | Coverage ratio | `0.7x` |
| Present value | Surplus / deficit | `-$1,010,652.94` |
| Portfolio longevity | Longevity | `31+ years` |
| Portfolio longevity | Final balance, nominal | `$2,014,811.97` |
| Portfolio longevity | Final balance, real | `$564,708.35` |
| Projection explainability | Taxes consumed | `7.3%` |
| Projection explainability | Cumulative inflation | `3.6%` |
| Projection explainability | Inflation distortion removed in real mode | `72.0%` |

The steady-state gap is negative in the calculator (`-182.08`), meaning a
surplus. The UI intentionally formats that as `+$182.08` with the `Surplus`
label.

## Verification Method

1. Fetched the live page from `http://127.0.0.1:8080/whatif`.
2. Extracted the rendered values from the Monthly Budget, Present Value, and
   Portfolio Longevity panels.
3. Loaded `data/settings/whatif.json` through `retirement.SettingsManager` so
   the same normalization path ran as the application uses.
4. Ran `retirement.NewCalculator(settings).RunFullAnalysis()`.
5. Compared `BudgetFit`, `PresentValue`, `Projection`, and
   `ProjectionExplainability` values against the live page.
6. Ran focused tests:

```bash
go test ./internal/services/retirement ./internal/handlers/whatif
```

## Calculator Paths

The verified page values are sourced from:

- `CalculateBudgetFit()` in `internal/services/retirement/calculator.go`
- `CalculatePresentValueAnalysis()` in `internal/services/retirement/calculator.go`
- `RunProjection()` plus `buildProjectionExplainability()` in
  `internal/services/retirement/calculator.go`
- Settings load normalization in `internal/services/retirement/settings.go`
- Age derivation in `internal/models/whatif.go`

No display/calculation mismatch was found in this verification pass.
