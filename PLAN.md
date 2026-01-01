# Budget App Conversion: Python → Go + HTMX

## Status: Phase 3 Complete

**Started:** 2025-12-31
**Go Version:** 1.25rc1 (installed at ~/go-sdk/go/bin/go)

## Decisions Made

| Choice | Selection |
|--------|-----------|
| Framework | Chi router |
| Storage | File-based (CSV/JSON) |
| Scope | Full feature parity |
| LLM | Deferred to later phase |
| Charts | Plotly.js (client-side) |
| CSS | Tailwind via CDN |

## Phase 1: Foundation ✅ COMPLETE

All items completed:
- [x] Go module initialized (`go mod init budget2`)
- [x] Project directory structure created
- [x] Makefile with build/run/dev commands
- [x] Transaction model with 20+ filtering/aggregation methods
- [x] Dashboard metrics model (KPIs, trends, comparisons)
- [x] Income source model for retirement planning
- [x] User settings model
- [x] CSV data loader with date parsing, deduplication
- [x] Income/outflow classifier (ported from Python)
- [x] Configuration management (env vars)
- [x] Template renderer with helper functions
- [x] Base layout with navigation (HTMX + Tailwind)
- [x] Dashboard page with KPIs and chart placeholders
- [x] Chart data API endpoints (monthly, category, cashflow, merchants)
- [x] HTMX and Plotly.js vendor files downloaded
- [x] Sample CSV data copied from Python app
- [x] Successful build and test run

## Phase 2: Dashboard Enhancements ✅ COMPLETE

- [x] Period comparison (previous period, same period last year)
- [x] Sparkline charts in KPI cards
- [x] More chart types (weekly pattern, cumulative balance)
- [x] Spending alerts panel (unusual day detection, large transactions)
- [x] Category drilldown (click pie slice to see transactions)

## Phase 3: Data Explorer - COMPLETE

- [x] Transaction table with server-side pagination
- [x] HTMX-powered filtering (category, search, date)
- [x] Column sorting
- [x] CSV file upload handling
- [x] File manager (enable/disable files)
- [x] Transaction details modal

## Phase 4: Insights - PENDING

- [ ] Unusual spending day detection (mean + 2σ threshold)
- [ ] Recurring payment detection (frequency classification)
- [ ] Spending alerts system
- [ ] Category analysis
- [ ] Insights page assembly

## Phase 5: Retirement/What-If - PENDING

- [ ] Present value calculations (port from Python)
- [ ] Income source CRUD (fixed, temporary, delayed, variable)
- [ ] Expense source management
- [ ] Month-by-month projection engine
- [ ] Portfolio balance chart
- [ ] Sensitivity analysis
- [ ] Settings persistence (JSON)

## Phase 6: Export & Polish - PENDING

- [ ] Excel export with excelize (multiple sheets)
- [ ] CSV summary export
- [ ] Budget category management
- [ ] Savings goals tracking
- [ ] Comprehensive error handling
- [ ] Loading states and error messages

## Project Structure (Current)

```
budget2/
├── cmd/server/main.go              # Entry point, handlers, chart APIs
├── internal/
│   ├── config/config.go            # Env-based configuration
│   ├── models/
│   │   ├── transaction.go          # Transaction + TransactionSet
│   │   ├── dashboard.go            # Metrics, charts, alerts
│   │   ├── income_source.go        # Retirement income modeling
│   │   └── user_profile.go         # User settings, budgets, goals
│   ├── services/
│   │   ├── dataloader/loader.go    # CSV parsing, deduplication
│   │   └── classifier/classifier.go # Income/outflow classification
│   └── templates/render.go         # Template helpers
├── web/
│   ├── templates/
│   │   ├── layouts/base.html       # Nav, HTMX, Tailwind
│   │   └── pages/
│   │       ├── dashboard.html      # Dashboard with KPIs
│   │       ├── explorer.html       # Data Explorer with table, filters
│   │       ├── whatif.html         # What-If placeholder
│   │       └── insights.html       # Insights placeholder
│   └── static/
│       ├── css/styles.css
│       ├── js/charts.js            # Plotly rendering
│       └── vendor/
│           ├── htmx.min.js
│           └── plotly.min.js
├── data/
│   ├── banking24-25.csv
│   ├── creditCard24-25.csv
│   ├── settings/
│   └── uploads/
├── go.mod
├── go.sum
├── Makefile
└── budget2                         # Compiled binary
```

## How to Run

```bash
# Build and run
make run

# Development (no binary)
make dev

# Just build
make build
```

Server runs at http://localhost:8080

## Key Files Reference

| File | Purpose |
|------|---------|
| `cmd/server/main.go` | All handlers, chart data builders |
| `internal/models/transaction.go` | Core data model with filtering |
| `internal/services/dataloader/loader.go` | CSV loading logic |
| `internal/services/classifier/classifier.go` | Income detection keywords |
| `web/templates/pages/dashboard.html` | Dashboard UI + KPIs template |

## Source Python Files (../budget)

| Python File | Go Equivalent | Status |
|-------------|---------------|--------|
| `data_loader.py` | `services/dataloader/loader.go` | ✅ Ported |
| `app.py` (UI) | `web/templates/` | 🔄 Partial |
| `visualizations.py` | `cmd/server/main.go` (chart builders) | 🔄 Partial |
| `insights_analyzer.py` | Not yet created | ❌ Pending |
| `retirement_calculator.py` | Not yet created | ❌ Pending |
| `export_manager.py` | Not yet created | ❌ Pending |

## Notes

- Income keywords and transfer patterns ported from Python
- TransactionSet provides Pandas-like filtering (FilterByType, FilterByDateRange, GroupByMonth, etc.)
- HTMX partials use `hx-get`, `hx-target`, `hx-trigger="change"`
- Chart data returned as JSON, rendered client-side with Plotly.js
