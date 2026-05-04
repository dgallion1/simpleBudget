---
title: Budget vs Actual Tracking on Dashboard
date: 2026-05-04
status: approved-pending-implementation
---

# Budget vs Actual Tracking — Design

## Problem

The dashboard's KPI cards show three budget-aware metrics — Monthly Living Expenses, Monthly Healthcare, and the combined Budget composite — but a user looking at the cards cannot answer *"how am I tracking against budget over time?"* at a glance. Two specific gaps:

1. **The small sparklines on each card show actual values only, with no target reference.** A line going up tells you spending rose, but not whether it crossed the budget line.
2. **There is no cumulative-variance series anywhere on the dashboard.** The Budget card text says "$16,618.41 under over 2.0 mo" but the user has no way to see whether that under-spending built up evenly or in bursts.

The existing "Monthly Variance vs Budget" chart in the chart grid shows per-month signed delta bars (red over / green under), but it does not show absolute actual-vs-target levels and does not show the cumulative running total.

## Goal

Make budget vs actual readable two ways on the dashboard:

- **Glanceable** — every card with a target visually shows where actuals sit relative to that target across time.
- **Diagnostic** — a full-width chart shows monthly Living + Healthcare actuals stacked against the combined target line, plus a running cumulative variance line that ends at the same number printed on the Budget card.

## Non-Goals

- Per-category drilldown beyond Living and Healthcare. The chart treats them as the two components of a single budget bucket because they draw from the same account.
- Phase-aware monthly target lines. The active configured target is treated as a single horizontal value across the date range. If targets change by spending phase mid-range, the chart shows the currently-effective target — not a stepped line. This matches existing card behavior.
- Editing budgets from the chart. Users continue to set targets in What-If; the dashboard is read-only.
- Drilling into a clicked month's transactions from the new chart. Out of scope for this iteration.

## Architecture

```
                                    ┌─────────────────────────┐
                                    │   What-If settings      │
                                    │  (BudgetTarget +        │
                                    │   HealthcareTarget)     │
                                    └────────────┬────────────┘
                                                 │
                                                 ▼
┌─────────────────────────────┐    ┌────────────────────────────────────┐
│ TransactionSet (date range) │───▶│ calculateMetrics                   │
└─────────────────────────────┘    │ (handlers.go)                      │
                                   │                                    │
                                   │ • LivingExpensesTrend   (existing) │
                                   │ • HealthcareTrend       (existing) │
                                   │ • TrendLabels           (existing) │
                                   │ • CombinedCumulativeTrend  (NEW)   │
                                   └────────────┬───────────────────────┘
                                                │
              ┌─────────────────────────────────┼─────────────────────────────────┐
              ▼                                 ▼                                 ▼
   ┌─────────────────────┐           ┌─────────────────────┐           ┌──────────────────────┐
   │ kpis.html partial   │           │ budget-vs-actual    │           │ kpi-detail modal     │
   │  (sparkline-monthly │           │ partial + endpoint  │           │ (no change)          │
   │   sparkline-health  │           │ /dashboard/charts/  │           │                      │
   │   sparkline-budget) │           │  data/budget-vs-    │           │                      │
   │                     │           │  actual             │           │                      │
   └──────────┬──────────┘           └──────────┬──────────┘           └──────────────────────┘
              │                                 │
              ▼                                 ▼
   ┌──────────────────────┐         ┌──────────────────────┐
   │ charts.js            │         │ charts.js loadChart  │
   │ renderSparkline w/   │         │ → Plotly subplots    │
   │ optional target +    │         │ (stacked bars + line │
   │ variance-mode flag   │         │  on shared x-axis)   │
   └──────────────────────┘         └──────────────────────┘
```

### Components

**`internal/handlers/dashboard/handlers.go`**
- Compute `combinedCumulativeTrend []float64` inside the existing month-by-month loop in `calculateMetrics`. For each month `i`:
  ```
  combinedCumulativeTrend[i] = combinedCumulativeTrend[i-1]
                             + (livingTrend[i] + healthcareTrend[i])
                             - combinedTarget
  ```
  When `combinedTarget == 0` (no budget configured), the series is left `nil` so the template can branch on `len() > 0`.
- Add a new chart-data builder `buildBudgetVsActualChartData(ts, livingTarget, healthcareTarget)` returning a Plotly subplot payload (top: stacked bars + target line; bottom: cumulative variance line with green/red area fill against zero).
- Wire a new chart type `case "budget-vs-actual"` into `handleChartData` so the existing `/dashboard/charts/data/{type}` route serves it. The route already date-filters via the same code path used by `monthly`, `spending-trend`, etc.

**`internal/models/dashboard.go`**
- Add a `CombinedCumulativeTrend []float64` field to `DashboardMetrics` with the JSON tag `combined_cumulative_trend`. Same length as `TrendLabels` when populated; `nil` otherwise.

**`web/templates/components/kpis.html`**
- Replace the inline `data-values`/`data-color` setup on `sparkline-monthly` and `sparkline-healthcare` with a new triple of attributes that includes the target:
  - `data-values`, `data-color`, `data-target` (number; omitted if no target).
- Add a new `<div id="sparkline-budget">` to the Budget card with `data-values="{{toJSON .Metrics.CombinedCumulativeTrend}}"` and a `data-mode="variance"` attribute that tells the JS to color above-zero red and below-zero green against a dashed zero baseline.

**`web/templates/components/budget-vs-actual.html`** (new)
- Card shell with title "Budget vs Actual Over Time" and an empty-state branch when `HasCombinedTarget` is false (renders a "Set a budget in What-If →" link, mirroring the Budget KPI card).
- One chart container `<div id="chart-budget-vs-actual" data-chart-url="/dashboard/charts/data/budget-vs-actual">` that the existing `loadAllCharts` flow auto-populates and re-loads on date filter changes.

**`web/templates/pages/dashboard.html`**
- Insert the new `{{template "budget-vs-actual" .}}` block between the KPI row and the chart grid.
- Remove the existing "Monthly Variance vs Budget" card from the chart grid (its data is fully represented by the new chart's top panel — see "Replacing the existing variance chart" below).

**`web/static/js/charts.js`**
- Extend `renderSparkline(containerId, values, color, options)` with an optional fourth parameter:
  - `options.target` — number; when present, draws a dashed horizontal line at that y value, fills above red @ 30% / below green @ 30%.
  - `options.mode === "variance"` — special handling for the Budget card: zero baseline is the reference, fill above zero red / below zero green; no separate target line.
- Update `initSparklines` to read `data-target` and `data-mode` from each element and pass them through.
- No new chart-rendering function for the full-width chart — the existing `loadChart` + `renderChart` JSON pipeline handles Plotly subplots if the server returns a layout with `grid` / `xaxis2` / `yaxis2` (verify during implementation; fall back to a custom render function only if needed).

### Replacing the existing variance chart

The existing `Monthly Variance vs Budget` chart in the chart grid (delta bars per month) is subsumed by the top panel of the new chart. Removing it avoids redundancy and frees grid space. The handler endpoint (`case "monthly"` in `handleChartData`) and the builder `buildMonthlyVarianceChartData` should be deleted along with the chart container in `dashboard.html`.

### Data flow

1. User loads `/dashboard` or changes the date filter.
2. KPI partial reload via `hx-get="/dashboard/kpis"` re-renders cards with up-to-date `LivingExpensesTrend`, `HealthcareTrend`, `CombinedCumulativeTrend`, `BudgetTarget`, `HealthcareTarget`.
3. `htmx:afterSwap` triggers `initSparklines()`, which now reads target and mode attributes alongside values.
4. Independently, the budget-vs-actual chart container is picked up by `loadAllCharts`, which fetches `/dashboard/charts/data/budget-vs-actual?start=…&end=…` and hands the JSON to Plotly.

## Error handling

- **No budget configured (`HasCombinedTarget == false`)**: KPI sparklines for Living/Healthcare fall back to existing line-only rendering (no target overlay). Budget card shows "Not set" text and no sparkline. The new full-width chart renders the empty-state link, not the chart.
- **No transactions in range**: `TrendLabels` is empty. KPI sparklines render nothing (existing behavior). The full-width chart shows the same "Loading chart..." placeholder content already used elsewhere, replaced by an empty plot when the JSON returns empty arrays.
- **One month of data**: a single bar in the top panel and a single point in the bottom panel render correctly.
- **Plotly subplot rendering edge case**: if the existing `renderChart` does not handle a subplot layout, the implementation adds a thin `renderSubplotChart` wrapper. This is a contingency, not a primary path.
- **Date filter race**: existing `loadAllCharts` already refetches on `htmx:afterSettle` for the kpi container; new chart obeys the same pattern via its `data-chart-url`.

## Testing

**Unit tests (`internal/handlers/dashboard/handlers_test.go`)**
- `calculateMetrics` returns `CombinedCumulativeTrend` of expected length and values for: zero target, target with no transactions, target with mixed over/under months. Verify the last element equals the existing `CombinedCumulativeDelta` (invariant — both numbers must agree).
- `buildBudgetVsActualChartData` returns:
  - Two traces in the top panel (Living bar + Healthcare bar, stacked) and one in the bottom (cumulative line).
  - A `shapes` entry for the horizontal target line at `combinedTarget`.
  - Empty-state when `combinedTarget == 0`: returns a payload with `nil` data so the front end can branch on emptiness.

**HTTP tests (`internal/handlers/dashboard/handlers_http_test.go`)**
- `GET /dashboard/charts/data/budget-vs-actual` returns 200 with the expected JSON structure for a fixture with two months of data.
- `GET /dashboard/kpis` HTML contains `id="sparkline-budget"` and `data-mode="variance"` on the Budget card.

**Coverage discipline**: per project memory `feedback_subagent_pattern_works.md` and `project_test_coverage.md`, dispatch the implementation as a prescriptive subagent task with literal code so coverage targets are met first-try.

## Risks & mitigations

| Risk | Mitigation |
|------|-----------|
| Cumulative variance number disagrees with KPI card text after refactor | Test asserting `last(CombinedCumulativeTrend) ≈ CombinedCumulativeDelta` |
| Plotly subplot rendering breaks date-filter refresh | Reuse existing `loadChart` plumbing; verify on first render |
| Removing existing variance chart breaks a deep-link/test | Search for `chart-monthly` and `data/monthly` references; update or remove |
| Sparkline visual clutter at h-10 | Keep target line dashed at 1px and fills at 30% opacity to preserve readability |

## Open questions

None at design time. Defer implementation specifics (e.g., exact Plotly subplot layout JSON) to the writing-plans phase.
