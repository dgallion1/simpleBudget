# Phase 5: What-If/Retirement Planning Implementation

## Summary

Port the Python retirement planning functionality to Go + HTMX with full feature parity. This includes portfolio projections, income/expense source management, sustainability scoring, and sensitivity analysis.

## Files to Create

| File | Purpose |
|------|---------|
| `internal/models/whatif.go` | Data structures for settings, projections, analysis results |
| `internal/services/retirement/calculator.go` | Core retirement calculations (PV, projections, sustainability) |
| `internal/services/retirement/settings.go` | JSON persistence for whatif.json |

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/server/main.go` | Add 10 HTTP handlers for what-if routes |
| `web/templates/pages/whatif.html` | Replace placeholder with full two-column layout |
| `PLAN.md` | Update Phase 4 to complete, Phase 5 in progress |

## Implementation Steps

### Step 1: Models (`internal/models/whatif.go`)

Create data structures:
- `WhatIfSettings` - All user parameters (portfolio, rates, sources)
- `ProjectionMonth` - Single month in projection
- `ProjectionResult` - Complete projection with longevity, final balance
- `BudgetFitAnalysis` - Monthly gap analysis
- `PresentValueAnalysis` - PV of expenses/income with coverage ratio
- `SustainabilityScore` - 0-100 score with label/color
- `SensitivityResult` - Scenario analysis results
- `WhatIfAnalysis` - Complete analysis container

### Step 2: Calculator (`internal/services/retirement/calculator.go`)

Implement calculation functions:
```go
func PresentValue(futureValue, annualRate float64, periods int) float64
func PresentValueAnnuity(payment, discountRate, growthRate float64, startMonth, endMonth int) float64
func (c *Calculator) CalculateTotalIncome(month int) float64
func (c *Calculator) CalculateTotalExpenses(month int) float64
func (c *Calculator) RunProjection() *ProjectionResult
func (c *Calculator) CalculateBudgetFit() *BudgetFitAnalysis
func (c *Calculator) CalculatePresentValueAnalysis() *PresentValueAnalysis
func (c *Calculator) CalculateSustainabilityScore(projection *ProjectionResult) *SustainabilityScore
func (c *Calculator) CalculateSensitivity() []SensitivityResult
func (c *Calculator) RunFullAnalysis() *WhatIfAnalysis
```

Key formulas:
- PV: `FV / (1 + r)^n`
- Monthly projection: expenses - income = gap, portfolio += growth - withdrawal
- Sustainability: score 0-100 based on withdrawal rate thresholds (≤3%=100, ≤4%=90, etc.)

### Step 3: Settings Persistence (`internal/services/retirement/settings.go`)

```go
func NewSettingsManager(settingsDir string) *SettingsManager
func (sm *SettingsManager) Load() (*WhatIfSettings, error)
func (sm *SettingsManager) Save(settings *WhatIfSettings) error
```

Save to `data/settings/whatif.json`

### Step 4: HTTP Handlers (`cmd/server/main.go`)

Add routes:
```go
r.Get("/whatif", handleWhatIf)
r.Post("/whatif/calculate", handleWhatIfCalculate)
r.Post("/whatif/income", handleWhatIfIncome)
r.Delete("/whatif/income/{id}", handleWhatIfIncomeDelete)
r.Post("/whatif/expense", handleWhatIfExpense)
r.Delete("/whatif/expense/{id}", handleWhatIfExpenseDelete)
r.Get("/whatif/chart/projection", handleWhatIfProjectionChart)
r.Post("/whatif/sync", handleWhatIfSync)
```

HTMX patterns:
- Parameter changes: `hx-trigger="change delay:500ms"` for debounced auto-recalculate
- Income/expense CRUD: `hx-target="#income-sources-list"` for partial swaps
- Chart updates: JSON endpoint for Plotly, triggered after calculations

### Step 5: Template (`web/templates/pages/whatif.html`)

Two-column layout:

**Left Column (1/3 width):**
- Portfolio & Expenses card (portfolio value, living expenses, healthcare)
- "Sync from Dashboard" button
- Rate Assumptions card (sliders for withdrawal, inflation, returns, etc.)
- Income Sources card (dynamic CRUD list)
- Expense Sources card (dynamic CRUD list)

**Right Column (2/3 width):**
- Monthly Budget Fit (3-metric grid: net gap, required rate, max rate)
- Present Value Analysis (PV expenses, PV income, coverage ratio)
- Portfolio Longevity Chart (Plotly area chart)
- Sustainability Gauge (SVG arc 0-100 with color coding)
- Sensitivity Table (scenarios with longevity/balance/score)

### Step 6: Chart Integration

Projection chart data structure (Plotly area chart):
```json
{
  "data": [{
    "x": [0, 0.083, 0.167, ...],  // Years
    "y": [500000, 499500, ...],   // Portfolio balance
    "fill": "tozeroy",
    "name": "Portfolio Balance"
  }],
  "layout": {
    "title": "Portfolio Projection",
    "xaxis": {"title": "Years"},
    "yaxis": {"title": "Balance ($)"}
  }
}
```

## UI Parameters (from Python)

| Parameter | Range | Default |
|-----------|-------|---------|
| Portfolio Value | $0+ | $500,000 |
| Monthly Living Expenses | $0+ | $4,000 |
| Monthly Healthcare | $0+ | $500 |
| Healthcare Start Years | 0-30 | 0 |
| Max Withdrawal Rate | 0-20% | 4% |
| Inflation Rate | 0-10% | 3% |
| Healthcare Inflation | 0-15% | 6% |
| Spending Decline Rate | 0-5% | 1% |
| Investment Return | 0-15% | 6% |
| Projection Years | 5-40 | 30 |

## Income Source Fields
- Name (string)
- Monthly Amount ($)
- Start Year (0 = immediate)
- End Year (0 = perpetual)
- COLA checkbox (apply inflation adjustment)

## Expense Source Fields
- Name (string)
- Monthly Amount ($)
- Start Year (0 = immediate)
- End Year (0 = perpetual)
- Apply Inflation checkbox

## Sensitivity Analysis Scenarios
1. Investment return ±2%
2. Inflation rate ±1%
3. Spending +10%
4. Healthcare +50%

## Reference Files

- Python calculator: `/home/darrell/bin/ai/budget/src/budget/retirement_calculator.py`
- Python UI: `/home/darrell/bin/ai/budget/src/budget/app.py` (lines 1666-2400)
- Existing Go models: `/home/darrell/bin/ai/budget2/internal/models/income_source.go`
- Template patterns: `/home/darrell/bin/ai/budget2/web/templates/pages/insights.html`

## Quick Start Command

After clearing context, run:
```
implement phase 5 what-if retirement planning per PHASE5_PLAN.md
```
