# Dashboard Budget Tracking — Design Spec

**Date:** 2026-05-03
**Status:** Approved (verbal)
**Author:** Darrell + Claude

## Problem

The dashboard surfaces total income, total expenses, net savings, and savings
rate for the selected date range, but it does not show the single number Darrell
tracks most closely: **monthly living expenses**. That number is the input the
What-If retirement scenario consumes (`Settings.MonthlyLivingExpenses`) to
project long-term spending, so being able to see how close actuals are to that
budget — for any chosen date range — is the missing feedback loop.

Today, comparing the dashboard total (e.g., `$6,351.57` over a few months)
against a what-if budget (e.g., `$5,000/mo`) requires mental math and a tab
switch. We want it on the dashboard.

## Goals

1. Show the **average monthly outflow** for the selected dashboard date range.
2. Show the **what-if `MonthlyLivingExpenses` target** as the comparison point.
3. Show the **cumulative over/under variance** for the selected period
   (`Actual − Target × months`), so it is obvious how much budget has been
   eaten or banked.
4. Drop the existing **Savings Rate** KPI (not useful for this user).
5. Repurpose the **Net Savings** KPI slot into a **Budget** KPI that displays
   the cumulative over/under figure.

## Non-Goals

- Editing the what-if budget from the dashboard (no inline edit, no sync
  button). The what-if page remains the single source of truth.
- New drilldown modals; reuse the existing expenses drilldown for both new
  cards.
- Per-category budget tracking — this is one global monthly-expense target,
  not a category-by-category budget tool.
- Changes to chart panels below the KPI row.

## KPI Row — Final Shape

The four KPI cards become:

| Position | Card | Source | Notes |
|---|---|---|---|
| 1 | **Total Income** | sum of inflows in date range | unchanged |
| 2 | **Total Expenses** | sum of outflows in date range (already nets refunds, per `12c5ef9`) | unchanged |
| 3 | **Monthly Living Expenses** *(new)* | `TotalExpenses ÷ months` | shows target underneath, and per-month variance |
| 4 | **Budget** *(replaces Net Savings slot)* | `Actual − Target × months` for the period | green if under, red if over |

**Removed:** Savings Rate card. (It has no sparkline, just a transaction-count
sub-line, so removal is a straight delete of the card block.)

## Data Model

### Months in date range

```
months = (EndDate.Sub(StartDate).Hours() / 24 + 1) / 30.4375
```

- `30.4375` = average days per month (`365.25 / 12`).
- `+1` so a single-day range is `1/30.4375 ≈ 0.033 mo`, not `0`.
- `months` is a `float64`, never zero (guarded — see edge cases).

### Target

`Target = settings.MonthlyLivingExpenses` from the active what-if scenario file
(the same value the what-if page edits). The dashboard already loads scenario
settings via the existing what-if store; no new persistence layer.

### Derived values

```
ActualMonthly  = TotalExpenses / months           // card 3 headline
PerMonthDelta  = ActualMonthly - Target            // card 3 sub-line
CumulativeDelta = TotalExpenses - Target * months  // card 4 headline
                = PerMonthDelta * months           // equivalent
```

Sign convention: **positive = over budget (bad)**, **negative = under (good)**.
Display always shows absolute value with a "over" / "under" label rather than
a raw +/- sign.

### Period comparison

The existing comparison dropdown (Previous period / Same period last year)
continues to drive Income and Expenses comparisons. For the two new cards:

- **Monthly Living Expenses card:** comparison shows `±$X/mo vs prior` if
  `PeriodComparison.HasData`.
- **Budget card:** comparison shows the change in cumulative variance
  (`current.CumulativeDelta − previous.CumulativeDelta`), labelled as
  "vs prior period".

If the prior period has no transactions, the comparison line is omitted (same
behavior as the existing KPIs).

## UI Layout

The KPI grid stays at `grid-cols-1 md:grid-cols-2 lg:grid-cols-4`. Card
sizing and chrome match the existing KPI cards (icon, headline number,
optional comparison line, sparkline if applicable).

### Card 3 — Monthly Living Expenses

```
┌─────────────────────────────────────┐
│ Monthly Living Expenses        [$]  │
│ $5,847                              │  ← actual/mo (gray-900 / dark gray-100)
│ Target $5,000 · +$847/mo over       │  ← sub-line, red if over, green if under
│ ▁▂▃▅▆▇  (sparkline of monthly)     │
└─────────────────────────────────────┘
```

- Icon: dollar / wallet glyph (reuse one of the existing Heroicons — pick
  whichever is not already on a sibling card).
- Headline color: neutral (gray-900 dark:gray-100). The headline is a
  diagnostic, not a value judgement; coloring lives on the variance line.
- Sub-line text: `Target ${{target}} · {{abs delta}}/mo {{over|under}}`.
  - Red (`text-red-600 dark:text-red-400`) when over.
  - Green (`text-green-600 dark:text-green-400`) when under.
- Sparkline: reuses `ExpensesTrend` data (no new field needed); same color
  as the Total Expenses sparkline (`#ef4444`).
- Click → opens the existing expenses drilldown modal
  (`openKPIDetail('expenses')`).

### Card 4 — Budget

```
┌─────────────────────────────────────┐
│ Budget                          [✓] │
│ $3,477 over                         │  ← cumulative; red if over, green if under
│ 4.1 mo @ +$847/mo                   │  ← sub-line, neutral gray
│                                     │  (no sparkline)
└─────────────────────────────────────┘
```

- Icon: scale / target glyph; tint matches the headline state (red bg /
  green bg, mirroring the existing Net Savings card pattern).
- Headline format: `${{abs delta}} {{over|under}}`. If `|delta| < $1`,
  show `On budget` instead.
- Sub-line: `{{months}} mo @ {{sign}}${{per-month abs}}/mo` so the math
  is transparent.
- Click → opens the existing expenses drilldown
  (`openKPIDetail('expenses')`).

### Card 4 — No Target Fallback

When `Target == 0` (what-if not yet configured):

```
┌─────────────────────────────────────┐
│ Budget                          [⚙] │
│ Not set                             │  ← gray-500 italic
│ Set a budget in What-If →           │  ← link to /whatif
└─────────────────────────────────────┘
```

Card 3 still displays the actual `ActualMonthly` number with no sub-line in
this case — knowing the actual is useful even without a target.

## Backend Changes

### `internal/models/dashboard.go`

(`SecondaryMetrics` exists in this file but is currently unreferenced
anywhere else in the codebase — ignore it for this work.)

Add to `DashboardMetrics`:

```go
MonthsInRange      float64 // average-calendar-month count for the date range
ActualMonthly      float64 // TotalExpenses / MonthsInRange
BudgetTarget       float64 // from what-if MonthlyLivingExpenses
PerMonthDelta      float64 // ActualMonthly - BudgetTarget; signed
CumulativeDelta    float64 // TotalExpenses - BudgetTarget*MonthsInRange; signed
HasBudgetTarget    bool    // BudgetTarget > 0
```

Keep `SavingsRate` on the struct for backward compatibility with anything
that might consume the JSON externally; just stop rendering it on the
dashboard. (If grep shows no external consumers, delete it.)

### `calculateMetrics` in `internal/handlers/dashboard/handlers.go`

After computing `totalExpenses`:

1. Compute `monthsInRange` from the filtered set's date span. Use the
   selected dashboard date range (`StartDate` / `EndDate`) when available,
   otherwise fall back to `ts.MinDate()` / `ts.MaxDate()`.
2. Guard: if `monthsInRange < 1/30.4375` (less than a day), set to that
   floor so we never divide by zero.
3. Load `target` via `retirement.SettingsManager.Load()` — the same
   accessor the what-if handlers use. Inject it as a function argument so
   `calculateMetrics` stays unit-testable without touching disk.
4. Populate the new fields. Set `HasBudgetTarget = target > 0`.

Refactor signature:

```go
func calculateMetrics(ts *models.TransactionSet, rangeStart, rangeEnd time.Time, target float64) *models.DashboardMetrics
```

The two callers (`Index` and `KPIs`) already have the date range; both pass
it through. Both load the target from the what-if store at the handler
level and pass it in.

### Period comparison

`calculateComparison` returns the same `PeriodComparison` struct, with
two new fields:

```go
ActualMonthlyChange   float64 // current.ActualMonthly - previous.ActualMonthly
CumulativeDeltaChange float64 // current.CumulativeDelta - previous.CumulativeDelta
```

`SavingsRateChange` stays on the struct for now (same backward-compat
reasoning as above); just stop rendering it.

## Template Changes

### `web/templates/components/kpis.html`

- **Delete** the Savings Rate card block (lines ~85–112).
- **Replace** the Net Savings card with the **Budget** card per the layout
  above. Keep the same grid slot.
- **Insert** the **Monthly Living Expenses** card in slot 3 (after Total
  Expenses, before Budget).
- All formatting uses existing template helpers: `formatMoney`, `abs`,
  `colorClass`. May need a small `signedDelta` helper or inline `if` for
  the over/under label.

### `web/templates/components/kpi-detail.html`

No change required. Both new cards open the existing expenses drilldown.

## Testing

### Unit tests (`internal/handlers/dashboard/handlers_test.go`)

Add cases to `TestCalculateMetrics` (or create one if it doesn't exist):

1. **Happy path:** 3-month range, `TotalExpenses = $18,000`, target =
   `$5,000`. Expect `ActualMonthly ≈ $6,000`, `PerMonthDelta ≈ +$1,000`,
   `CumulativeDelta ≈ +$3,000`, `HasBudgetTarget = true`.
2. **Under budget:** `TotalExpenses = $12,000`, target `$5,000`,
   3-month range. Expect `CumulativeDelta ≈ −$3,000`.
3. **No target:** target `0`. Expect `HasBudgetTarget = false`,
   `ActualMonthly` still computed, `PerMonthDelta = ActualMonthly`,
   `CumulativeDelta = TotalExpenses`.
4. **Single-day range:** `monthsInRange ≈ 0.033`, no divide-by-zero panic.
5. **Empty range:** zero transactions → all derived fields are `0`,
   `HasBudgetTarget` reflects target only.

### HTTP tests (`internal/handlers/dashboard/handlers_http_test.go`)

1. `/dashboard` renders the Monthly Living Expenses card with the right
   number and target string.
2. `/dashboard/kpis` (the htmx fragment) renders the Budget card with the
   right "over" / "under" label and color class.
3. With `target = 0`, the Budget card shows the "Set a budget in What-If"
   fallback link.
4. The Savings Rate card is **not** present in either rendered page.

### Coverage ceiling

Per `project_test_coverage.md`, dashboard handlers have a coverage ceiling.
This change must keep coverage at or above the existing ceiling. If the new
target-loading branch trips it, add a small test fixture exercising both
the "loaded target" and "missing/zero target" paths.

## Risks

- **GitNexus impact:** `calculateMetrics` is called from multiple handlers
  and likely participates in dashboard execution flows. Run
  `gitnexus_impact({target: "calculateMetrics", direction: "upstream"})`
  before editing and report blast radius.
- **Backward compat:** Removing `SavingsRate` from the JSON could break
  external consumers (none expected — single-user, file-based app). Keep
  the field on the struct, just stop rendering. Easy to delete later.
- **Date range edge cases:** "All" preset can pull a multi-year range,
  which means a small per-month delta could blow up to a giant cumulative
  number. Acceptable — that's the truth of the data.

## Out of Scope (Explicit)

- Editing the what-if budget from the dashboard.
- Multiple budgets / category budgets.
- Budget history or trend charts.
- Updates to the what-if page itself (other than the existing budget
  field already serving as the source).

## Acceptance Criteria

1. Dashboard KPI row shows four cards: Total Income, Total Expenses,
   Monthly Living Expenses, Budget. Savings Rate is gone.
2. Monthly Living Expenses card matches `TotalExpenses ÷ monthsInRange` to
   the cent, and shows the target plus per-month delta.
3. Budget card shows the cumulative over/under figure with correct sign,
   color, and "over" / "under" / "On budget" label.
4. With no target set, the Budget card shows the "Set a budget in What-If"
   fallback and links to `/whatif`.
5. Date filter changes refresh both new cards via the existing
   `/dashboard/kpis` htmx swap.
6. Period comparison dropdown produces meaningful comparison lines on
   both new cards when the prior period has data.
7. All existing dashboard tests still pass; new tests cover the new
   logic; coverage ceiling held.
