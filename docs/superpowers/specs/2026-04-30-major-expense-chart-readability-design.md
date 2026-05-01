# Major Expense Chart Readability — Design

**Date:** 2026-04-30
**Branch:** data-storage-improvements
**Status:** Ready for implementation

## Problem

The dashboard's "Spending by Major Expense" donut chart becomes unreadable when
many small buckets exist. With ~13 buckets — 9 of them under 3% — Plotly's
auto-placed leader-line labels stack on top of each other on the right side, the
horizontal legend wraps awkwardly, and sub-1% percentages are unhelpful at the
chart's render size.

## Approach

Hybrid donut + text breakdown:

- The donut shows the largest 8 buckets as individual wedges, plus a single
  rolled-up "Other" wedge representing everything beyond the top 8.
- A small two-column text list below the donut breaks down what's inside
  "Other": item name, dollar amount, percent of total — sorted descending.
- The "Unmatched" bucket continues to appear as its own wedge (it is a
  meaningful category, not long-tail noise).

This preserves the at-a-glance "share of total" feel of a donut while making
the long tail readable.

## Server changes

**File:** `internal/handlers/dashboard/handlers.go`, function
`buildMajorExpenseChartData` (currently lines 744–813).

Today the function builds `buckets` from matched major-expense groups, sorts
them by value descending, then appends a single `Unmatched` bucket if non-zero.

After this design:

1. After the descending sort, partition the matched buckets:
   - Indices `0..7` → keep as-is (the donut's individual wedges).
   - Indices `8..` → sum their values into a single `Other` wedge appended at
     the end of the matched buckets, **before** `Unmatched` is appended.
2. Build a `smaller` slice from indices `8..`, with each entry containing:
   - `name` — the bucket name
   - `amount` — the bucket value (matches the existing sum)
   - `percent` — the bucket's share of the total of all returned wedges
     (matched buckets + `Other` + `Unmatched`), rounded to one decimal place
     for values ≥ 1%, or two decimal places for values < 1% (so a 0.45% slice
     does not collapse to 0%).
3. Return shape grows from:
   ```json
   { "data": [{ "type": "pie", "labels": [...], "values": [...], "hole": 0.4 }] }
   ```
   to:
   ```json
   {
     "data": [{ "type": "pie", "labels": [...], "values": [...], "hole": 0.4 }],
     "smaller": [
       { "name": "Subscription",   "amount": 213.45, "percent": 2.1 },
       { "name": "vacation FL #1", "amount": 182.10, "percent": 1.8 }
     ]
   }
   ```
4. When there are 8 or fewer matched buckets, no `Other` wedge is added and
   `smaller` is omitted from the response (so the client clears/hides its
   breakdown sibling).
5. When there are exactly 9 matched buckets, the `Other` wedge represents a
   single bucket (and `smaller` has one entry). This is intentional — the
   rule "top 8 + Other" is applied uniformly without a special case for
   N=9.

The threshold (`8`) is a constant in the handler file. No config plumbing.

## Template change

**File:** `web/templates/pages/dashboard.html` (around line 132–140).

Add a sibling div directly after `#chart-major-expense`:

```html
<div id="chart-major-expense" class="chart-container" data-chart-url="..."> ... </div>
<div id="chart-major-expense-breakdown"
     class="mt-3 text-sm text-gray-600 dark:text-gray-300"></div>
```

The chart loader (`refreshCharts` in `web/static/js/dashboard.js`) keys off
`.chart-container[data-chart-url]` and is unchanged. The new sibling lives
inside the same dashboard tile.

## Client change

**File:** `web/static/js/charts.js`, function `renderChart`.

After the existing `Plotly.newPlot(...)` call, add a per-chart hook for
`containerId === 'chart-major-expense'` (parallel to the existing
`chart-category` click handler):

- Locate `#chart-major-expense-breakdown`.
- If `data.smaller` is a non-empty array, render a small two-column list with
  a header (`Other categories`) and one row per item showing name, dollar
  amount (formatted as `$1,234.56`), and percent.
- If `data.smaller` is missing or empty, clear the breakdown div so the tile
  does not reserve vertical space.

The breakdown rows should be readable on dark and light themes — use existing
`text-gray-*` Tailwind utilities consistent with neighboring chart tiles.

## Testing

**Server (`internal/handlers/dashboard/handlers_test.go` or its existing
neighbor):**

Extend coverage of `buildMajorExpenseChartData` for these cases:

1. **Fewer than threshold + no unmatched** — N ≤ 8 buckets, all matched →
   no `Other` wedge, `smaller` omitted, labels match input.
2. **Exactly threshold** — N = 8 buckets → no `Other` wedge, `smaller` omitted.
3. **Above threshold** — N = 11 buckets → 8 individual wedges plus one
   `Other` wedge whose value equals the sum of the bottom 3; `smaller`
   contains those 3 with correct percents summing to the `Other` wedge's
   share.
4. **Above threshold + unmatched** — verify `Unmatched` wedge is appended
   *after* `Other`, and is excluded from `smaller`.
5. **Percent precision** — a sub-1% bucket retains two decimal places so it
   doesn't read as `0.0%`.

**Client:** manually verify in the dev server browser that the breakdown
appears with > 8 buckets, disappears with ≤ 8 buckets, and renders cleanly in
both light and dark themes. No automated UI test required.

## Out of scope

- The duplicate `Dog` / `dog` buckets visible in the screenshot — likely two
  major-expense entries with case-different names. Flagged here; not fixed in
  this work.
- Click-to-expand on the `Other` wedge (could be a follow-up).
- Threshold tuning beyond `8`.
