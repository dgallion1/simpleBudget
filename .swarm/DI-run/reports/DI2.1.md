# DI2 attempt 1 implementation evidence

## Scope and isolation
Implemented DI2 only in the dashboard-insights worktree. Source is frozen for independent review after this handoff. No git-state changes, commits, subagents, ledger/spec/verdict edits, live data, server starts, tree copies, JS edits, or CSS builds. Approved task-2 brief, plan GlobalConstraints, design period semantics and ACCESSIBILITY.md governed this work.

The lead's immutable /tmp/budget2-DI2-base.USPhZp baseline was read only. Commands:
- `rtk proxy git diff --no-index --stat /tmp/budget2-DI2-base.USPhZp/internal internal`: exit 1 (expected differences), exactly 14 DI2 Go files.
- `rtk proxy git diff --no-index --stat /tmp/budget2-DI2-base.USPhZp/web web`: exit 1 (expected differences), exactly three DI2 templates.
All 17 source/test/template differences are in the manifest. Accepted DI1 recurring/classifier and DI6 source/CSS have no differences against that baseline. Report and manifest are additional evidence paths.

## Semantic references before edits
No callable LSP tool was available; used gopls CLI references plus rg enumeration. This is CLI semantic evidence, not interactive LSP validation. Successful pre-edit reference locations included:
- services/insights/trends.go:110:6 MajorExpenseTrends, :198:6 CategoryTrends, :348:6 SpendingVelocity: UI/MCP producers and tests.
- models/insights.go:89:6 SpendingVelocity and :98:6 InsightsData: verdict, handlers, template fixtures, MCP.
- handlers/insights/handlers.go:151:6 calculateInsights, :205:6 loadAndAnalyzeTrends, :219:6 full page, :314:6 trends partial, :348:6 trends chart, :414:6 velocity partial.
- handlers/dashboard/handlers.go:138:6 full page, :223:6 KPIs, :274:6 charts, :335:6 major drilldown, :445:6 KPI detail, :723:6 month detail, :850:6 export, :985:6 resolveDateRange.
- mcpsvc/spend/trends.go:179:15 majorExpenseTrendRows, :192:6 registerTrends, :92:6 trendsOutput, :58:6 velocityRow.
The command form was `rtk proxy gopls references <repository-relative-file>:<line>:<column>`. A few initial stale coordinates were rejected and corrected before edits. rg and template/JS reading supplied non-Go consumers that gopls cannot enumerate.

## Red and green commands
All commands ran in /home/darrell/bin/ai/budget2/.worktrees/dashboard-insights using synthetic fixtures only.
1. `rtk proxy go test ./internal/handlers/insights ./internal/services/mcpsvc/spend -run '^TestDI2' -count=1`: exit 1 before implementation. Historical August selection incorrectly projected 2400; February MCP comparison began January 4 instead of January 1.
2. `rtk proxy go test ./internal/services/insights -run '^TestDI2' -count=1`: exit 1 for missing new exported APIs (contract red, separately from behavioral failures above); then exit 0 after producer implementation.
3. `rtk proxy go test ./internal/handlers/dashboard -run '^TestDI2' -count=1`: exit 1, prior-calendar expenses 0 instead of 100; exit 0 after adapter.
4. `rtk proxy go test ./internal/handlers/insights ./internal/handlers/dashboard -run '^TestDI2.*(Full|Partials)' -count=1`: exit 1 before bindings, missing visible context/forecast text and the new wrapper template.
5. `rtk proxy go test ./internal/handlers/insights ./internal/handlers/dashboard ./internal/templates -run '^TestDI2|TestRenderInsights' -count=1`: exit 0 after bindings.
6. Existing MCP prior-bound assertion was updated from January 4 to January 1 under the approved calendar-month contract; its old equal-length explanatory comments were updated too.
7. Scoped gofmt applied to the 14 manifest Go paths.
8. Final `rtk proxy go test ./internal/services/insights ./internal/handlers/insights ./internal/handlers/dashboard ./internal/services/mcpsvc/spend ./internal/templates -count=1`: exit 0; five packages succeeded (0.012s, 0.326s, 0.922s, 0.741s, 1.231s).
9. Final `rtk proxy go build ./...`: exit 0.

Fixtures cover full calendar months, MTD clamping including leap February, equal inclusive ranges, DST civil dates, historical August/Sep6 demo, seven-day freshness and elapsed boundaries, missing history versus observed zero, signed refunds, fractional cents, transfers, suppressed rows, exact MCP calendar bounds, full/HTMX rendering, chart labels, and earlier-this-month forecast suppression.

## Interfaces for DI3/DI4
All exported functions below are in internal/services/insights:
- `BuildPeriodContext(allData *models.TransactionSet, start, end, now time.Time) models.PeriodContext`
- `TransactionsForPeriod(ts *models.TransactionSet, start, end time.Time) *models.TransactionSet`
- `SpendingVelocityForPeriod(allData *models.TransactionSet, p models.PeriodContext) *models.SpendingVelocity`
- `CategoryTrendsForPeriod(ts *models.TransactionSet, p models.PeriodContext) []models.CategoryTrend`
- `MajorExpenseTrendsForPeriod(ts *models.TransactionSet, defs []models.MajorExpense, pins map[string]string, p models.PeriodContext) []models.CategoryTrend`

PeriodContext exposes Valid, SelectedStart/End/Days, PreviousStart/End/Days, ComparisonKind, ComparisonClamped, HasData, LatestTransaction, DataAgeDays, Stale, HistoryAvailable/Reason, Today, ReferenceDate/Month, ElapsedDays, DaysInMonth, ForecastAvailable/Reason/Amount. InsightsData and SpendingVelocity have additive Period pointers. Consult declarations for JSON tags.

Dates are inclusive civil Y-M-D, normalized to UTC midnight without shifting imported date-only records. Injected now uses its supplied local civil date. ReferenceDate=min(selected end,today); ReferenceMonth/elapsed/monthlength follow that date. Completed calendar month compares prior calendar month; current MTD compares prior MTD clamped with explicit label; other selections compare the immediately preceding equal inclusive day count.

HistoryAvailable requires full-active first/last evidence bounds to enclose the prior range. This does NOT prove import completeness. Every context rendering explicitly states that limitation. No-history trend rows are unavailable/absent, not fabricated zero. MCP historical_daily and burn_rate_change are omitted when unavailable, but observed zero is retained. Consumers must use availability flags rather than numeric zero to infer absence.

Forecast requires selected range to include current month start THROUGH TODAY, latest full-active transaction in current month not future and <=7 civil days old, and >=7 elapsed days. Earlier-this-month selections are historical and cannot borrow later transactions, as expressly approved by lead; regression tested. Amount is rounded SignedNet current-month outflows / elapsed days * month days. Unavailable amount may be zero internally; availability/reason is authoritative. Available zero has ForecastAvailable=true even where JSON omitempty omits the numeric zero.

Explicit category/major comparisons are uncapped. Major-expense pins precede matching; unmatched rows remain outside major definitions. ChangeDisplay remains the single ChangeCell producer; signed refunds and rounded negative-zero behavior are preserved. Legacy MajorExpenseTrends, CategoryTrends, SpendingVelocity and existing compatibility wrappers remain available; production period consumers use the new APIs.

## Consumer inventory
- Insights full page: same explicit clock/context for visible period, trends, pace and forecast.
- Insights category-trends HTMX partial and trends chart JSON: same defaults/context; chart trace labels contain exact prior/selected dates, empty arrays on unavailable history.
- Insights spending-velocity HTMX partial: context, prior availability and forecast bindings shared with full page.
- Existing insights.js chart consumer receives unchanged trace shape plus additive period metadata; no JS edits.
- Insights recurring partial/classification remains DI1-owned; income-pattern partial and whole-history income analysis retain their unrelated semantics.
- Dashboard full page and KPI HTMX refresh: shared dashboard-period-kpis wrapper retains visible bounds/latest/history caveat on refresh.
- Dashboard charts (major expense, spending trend, merchants, cumulative, budget-vs-actual), major-expense drilldown, KPI detail, KPI month detail and CSV export: same selected bounds/default adapter and civil-date filter.
- Dashboard previous comparison uses explicit period windows; existing optional year comparison retains prior-year AddDate compatibility in its adapter.
- Account card as-of behavior and retirement/metrics/storage implementations remain unchanged.
- MCP get_trends: explicit context for category/major/velocity output, accurate tool description; existing income history/cap remains separate.
- Shared period-context.html defines period context, forecast, historical daily, days remaining, and dashboard KPI wrapper. Minimal page bindings only; DI4 still owns final hierarchy/layout.
- Existing insights verdict bar is gated to valid history plus available forecast because its legacy body assumes a forecast. No out-of-scope component redesign.

## Accessibility, rendering and concerns
Real template renderer tests exercise full-page, HTMX partial and forecast/refund/unavailable output. Visible text carries status; no hidden text, aria-hidden tricks, live-announcement churn or inaccessible dismissal control. Stale details use native keyboard-operable details/summary. No live browser or server was used.

No new CSS utilities were intentionally introduced: template classes reuse existing utilities. tailwind.css is byte-unchanged against the accepted DI6 baseline. CSS generation/freshness was NOT run, per ownership restriction; lead may coordinate a later generated-delta check if desired. No DI3/DI4 layout work is included. History remains evidence-envelope availability, not completeness assurance; that limitation is rendered. Independent review and mechanical acceptance belong to lead, not this report.

## Files
Exact cumulative DI2 paths relative to the accepted baseline are recorded in .swarm/DI-run/manifests/DI2.1.files, including this report and that manifest.

