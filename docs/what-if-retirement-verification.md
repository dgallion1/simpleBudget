# What-If Retirement Verification

This note records the verification pass for the live What-If planner values on
2026-04-07.

## Scope

Verified the live `/whatif` page against the calculator output loaded from
`data/settings/whatif.json`. The check covered:

- Monthly Budget Analysis current and steady-state values
- Present Value Analysis values
- Portfolio Longevity final nominal and real balances
- Projection explainability percentages for taxes and inflation

## Scenario Inputs

The saved Current Plan scenario used these key inputs:

- Portfolio value: `$1,900,000`
- Monthly living expenses: `$9,800`
- Projection start month: `2026-04`
- Projection length: `31` years
- Projection timing: `end_of_month`
- Discount rate: `5%`
- Inflation rate: `2.7%`
- Tax-deferred allocation: `80%`
- Roth allocation: `0%`
- Per-account stock allocation: `80%` tax-deferred, `20%` taxable
- Healthcare: Darrell Gallion on Medicare at `$550/mo`, Christine on ACA at `$1,150/mo`
- Income sources: Darrell SSI `$4,000/mo` from month 0, Christine job `$2,000/mo` through month 48, Christine pension `$1,200/mo` from month 48, Christine SSI `$2,000/mo` from month 156
- Spending phases enabled with `Go-Go` at `100%`, `Slow-Go` at `85%` from age 75, and `No-Go` at `85%` from age 85
- Steady-state override year: `13`

Settings load normalizes `start_date`, derives working ages from `persons`, and
recomputes linked healthcare ages before running the planner. For this scenario,
the normalized planning ages are primary `67` and spouse `54`.

## Verified Values

The live page matched the calculator output exactly after UI rounding:

| Panel | Value | Verified output |
| --- | --- | --- |
| Current budget | Monthly expenses | `$11,500.00` |
| Current budget | Monthly income | `$6,000.00` |
| Current budget | Estimated taxes | `$35.00` |
| Current budget | Taxable Social Security | `20%` |
| Current budget | Monthly gap | `$5,535.00` shortfall |
| Current budget | Required rate | `3.5%` |
| Steady state | Year | `13` |
| Steady state | Monthly expenses | `$13,617.70` |
| Steady state | Monthly income | `$8,608.54` |
| Steady state | Estimated RMD | `$13,374.80` |
| Steady state | Estimated taxes | `$2,676.92` |
| Steady state | Taxable Social Security | `85%` |
| Steady state | Monthly gap | `+$5,688.72` surplus |
| Steady state | Required rate | `0.0%` |
| Present value | Total resources | `$3,345,806.64` |
| Present value | PV income | `$1,445,806.64` |
| Present value | PV expenses | `$3,058,923.29` |
| Present value | Coverage ratio | `1.1x` |
| Present value | Surplus / deficit | `+$286,883.35` |
| Portfolio longevity | Longevity | `31+ years` |
| Portfolio longevity | Final balance, nominal | `$3,052,007.82` |
| Portfolio longevity | Final balance, real | `$1,339,263.69` |
| Projection explainability | Taxes consumed | `9.7%` |
| Projection explainability | Cumulative inflation | `127.9%` |
| Projection explainability | Inflation distortion removed in real mode | `56.1%` |

The steady-state gap is negative in the calculator (`-5688.72`), meaning a
surplus. The UI intentionally formats that as `+$5,688.72` with the `Surplus`
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
