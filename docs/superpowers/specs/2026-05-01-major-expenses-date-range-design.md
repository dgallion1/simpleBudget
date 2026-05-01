# Major Expenses — Date Range Filter

**Status:** Approved (pending user review of this doc)
**Date:** 2026-05-01
**Owner:** Darrell

## Summary

Add a server-side date range filter to `/major-expenses` so the user can scope per-expense rollups and exception buckets to a window. Mirrors the existing Explorer/Insights/Dashboard date-range pattern: two `<input type="date">` controls with `[←] 3M | 6M | 12M | All [→]` quick-buttons. Submits via HTMX, persists per-page in sessionStorage, defaults to all-time on first visit.

## Problem

`/major-expenses` today loads all outflow transactions, runs `majorexpenseengine.Match`, and renders two cards: per-expense rollups + matched transactions on the left, exception buckets (Unknown Large, New Merchants, Out-of-Range, Cluster) on the right. There is no way to scope the page to a window — totals always reflect the full transaction history. The user wants to be able to ask "what did this look like last quarter?" or "compare 2024 vs 2025" without exporting data to Explorer.

## Goals

- Per-expense `Count` / `Total` / `Transactions` reflect only transactions within the window.
- Exception buckets reflect only transactions within the window (so "Unknown Large" lists only window-bound anomalies).
- The matching engine itself is untouched — it just receives a smaller transaction set.
- UX is consistent with Explorer's filter (muscle memory, shared JS helpers).
- Default behavior preserved: bare `/major-expenses` with no query string and no saved state shows all-time.

## Non-goals

- Filtering by category, type, or other fields. Date range only.
- Sharing date-range state with Explorer/Insights/Dashboard. Major Expenses persists its own range.
- Restricting which expense definitions are visible. The left card always lists every defined expense; the window only changes the matched-transactions / counts / totals shown for each.
- Server-side caching of windowed match results. Match is fast enough; no premature optimization.

## Behavioral decisions (user-confirmed)

| # | Decision |
|---|----------|
| 1 | Date range constrains the transaction set fed into `Match()`. Per-expense rollups AND exception buckets reflect only in-window transactions. |
| 2 | Server-side filter via `?start=YYYY-MM-DD&end=YYYY-MM-DD` query params. |
| 3 | Default = all-time (start=MinDate, end=MaxDate) on first visit. |
| 4 | Persistence = per-page sessionStorage under key `majorExpensesFilters`, mirroring Explorer's pattern. |
| 5 | Quick-button set = `[←] 3M | 6M | 12M | All [→]` exactly matching Explorer (re-uses `setDateRange` / `stepDateRange` JS helpers, ported page-local). |
| 6 | UI placement = new filter card above the existing search/header card. |
| 7 | Submission = HTMX swap of a wrapper containing both result cards (no full page reload). |

## Architecture & data flow

`buildPageData()` in `internal/handlers/majorexpenses/handlers.go` gains an explicit window:

```
LoadData() → MinDate/MaxDate (full range)
           → FilterByDateRange(start, end)   ← NEW
           → FilterByType(Outflow)            (existing)
           → Match()                           (unchanged)
           → per-expense Summaries + exception buckets
```

Helpers/signatures:

- `buildPageData(start, end time.Time) (map[string]interface{}, error)` — gains the window. The map gains keys `StartDate`, `EndDate`, `MinDate`, `MaxDate` (all `YYYY-MM-DD` strings).
- `parseRangeFromRequest(r *http.Request, txns *models.TransactionSet) (start, end time.Time)` — reads `start`/`end` first from `r.URL.Query()`, falls back to `r.FormValue` (covers POST/PUT/DELETE bodies). Empty or unparseable values fall back to `txns.MinDate()` / `txns.MaxDate()`. Mirrors `internal/handlers/explorer/handlers.go:142-152`.
- `renderResults(w http.ResponseWriter, start, end time.Time)` — passes the window through.

Handlers updated:

- `handleMajorExpensesPage` — parse range, pass to `buildPageData`.
- `handleExceptions` — parse range, pass through (HTMX partial endpoint that re-renders only the exceptions panel).
- `handleAdd`, `handleUpdate`, `handleDelete`, `handlePin`, `handleBulkPin`, `handleUnpin` — parse range from form/query, pass to `renderResults` so post-mutation re-render preserves the active window.

## UI / form layout

A new filter card is inserted **above** the existing search/header card in `web/templates/pages/major-expenses.html`:

```
┌────────────────────────────────────────────────┐
│ From [date] To [date]  [←] 3M 6M 12M All [→] │  ← NEW filter card
└────────────────────────────────────────────────┘
┌────────────────────────────────────────────────┐
│ Major Expenses    [search…           ✕]        │  ← existing header
└────────────────────────────────────────────────┘
┌─────────────────────┬──────────────────────────┐
│ Expense list (left) │ Exception buckets (right)│  ← existing
└─────────────────────┴──────────────────────────┘
```

Form skeleton:

```html
<form id="major-expenses-filter-form"
      hx-get="/major-expenses"
      hx-target="#major-expenses-results-wrapper"
      hx-swap="outerHTML"
      hx-push-url="true"
      hx-trigger="change from:input[type=date], click from:.date-range-btn"
      hx-indicator="#me-loading">
  <input type="date" name="start" value="{{.StartDate}}" min="{{.MinDate}}" max="{{.MaxDate}}">
  <input type="date" name="end"   value="{{.EndDate}}"   min="{{.MinDate}}" max="{{.MaxDate}}">
  <!-- [←] 3M 6M 12M All [→] buttons, identical to Explorer -->
</form>
```

The two cards become wrapped in a single `<div id="major-expenses-results-wrapper">` rendered by a new `major-expenses-results-wrapper` partial. The wrapper composes the existing partials (`major-expenses-list-card-content` + `major-expenses-exceptions-panel`) — those don't change.

JS helpers `setDateRange(months)` and `stepDateRange(direction)` are ported page-local from `web/templates/pages/explorer.html:286-340`. They read `document.querySelector('#major-expenses-filter-form input[name="start"]')` etc., set ISO date strings, then dispatch the `change` event so HTMX submits.

sessionStorage persistence mirrors Explorer (`web/templates/pages/explorer.html:148-170`):
- Bare `/major-expenses` with empty query + saved key → redirect to `/major-expenses?<saved>`.
- Every htmx:configRequest from the filter form re-saves `window.location.search.substring(1)` to `sessionStorage["majorExpensesFilters"]`.

The existing client-side text search (`applyUnifiedFilter`) is unchanged. Server narrows by date → DOM contains windowed rows → text search hides further. Composes correctly because text search reads `data-search` attributes from rendered rows.

## Mutation handler behavior

Every mutation must re-render the page with the active window applied — otherwise saving an expense definition jumps the page back to all-time, which is jarring.

Two viable approaches:

| Option | Wiring | Trade-off |
|--------|--------|-----------|
| `hx-include` | Add `hx-include="#major-expenses-filter-form"` to each mutation form/button | One attribute per form. Single source of truth (the filter form). Recommended. |
| Hidden inputs | Render hidden `start`/`end` inputs inside each mutation form | More verbose; risks drift if filter form values change without re-rendering hidden inputs. |

We use `hx-include`. The mutation handlers read `start`/`end` from the request via `parseRangeFromRequest`, no template duplication.

**Pin edge case**: pinning a transaction whose date is outside the active window. The pin succeeds (pins are by hash, date-independent). The pinned transaction does not appear in the re-rendered list because it's outside the window. Correct behavior — pins are persistent across windows; views are not.

## Edge cases & validation

- **Empty data store**: `MinDate`/`MaxDate` return zero time. Inputs render with empty `min`/`max`. Cards render empty (existing behavior).
- **Unparseable `start` or `end`**: `time.Parse` errors are silently swallowed; fall back to MinDate/MaxDate respectively. No 4xx.
- **`start > end`**: `FilterByDateRange` returns an empty set. Cards render "no matches in window". No swap or warning — user sees the crossed inputs.
- **`start` before MinDate or `end` after MaxDate**: implicitly clamped by `FilterByDateRange` (inclusion-only filter). HTML `min`/`max` attributes nudge the browser picker; server-side filter is authoritative.
- **HTMX mutation request with no `start`/`end`** (e.g., a button missing `hx-include`): falls back to all-time via `parseRangeFromRequest`. Acceptable degradation; tests cover the happy path.
- **sessionStorage stale dates after data import**: saved range may fall outside new MinDate/MaxDate; cards render empty. User sees the empty result and adjusts. No clamp on restore — gold-plating.
- **Pin-only major expenses** (no keywords, no amount range): still appear in the left card with a window-scoped count. No special handling needed.

## Testing

New tests in `internal/handlers/majorexpenses/handlers_test.go` (existing file, existing `setupTestHandlers`-style fixtures):

1. `TestBuildPageData_DateRangeFiltersTransactions` — fixture spans multiple years; assert per-expense `Count`/`Total` reflect only in-window txns and exception buckets contain only in-window txns.
2. `TestHandleMajorExpensesPage_StartEndQueryParams` — GET `/major-expenses?start=2024-01-01&end=2024-12-31`; assert `StartDate`/`EndDate` echoed in the data map and rollups scoped.
3. `TestHandleMajorExpensesPage_NoQueryParamsDefaultsToAllTime` — GET `/major-expenses`; assert `StartDate==MinDate`, `EndDate==MaxDate`.
4. `TestHandleMajorExpensesPage_UnparseableDatesFallBackToAllTime` — GET `/major-expenses?start=garbage&end=also-garbage`; assert no error, all-time result.
5. `TestHandleMajorExpensesPage_StartAfterEndReturnsEmpty` — GET with start > end; assert empty Summaries and exception buckets, status 200.
6. `TestHandleAdd_PreservesDateRange` — POST a new expense with `start`/`end` form values; assert rendered partial reflects the window.
7. `TestHandleBulkPin_PreservesDateRange` — same for bulk pin.
8. `TestHandleExceptions_StartEndQueryParams` — GET `/major-expenses/exceptions?start=...&end=...`; assert windowed buckets.

JS quick-buttons (`setDateRange` / `stepDateRange`) are a literal port of working Explorer code; smoke-checked manually after wiring rather than covered by Playwright tests.

## Files affected

| File | Change |
|------|--------|
| `internal/handlers/majorexpenses/handlers.go` | `buildPageData` gains window; `parseRangeFromRequest` helper; all GET/POST/PUT/DELETE handlers parse and pass the window. |
| `internal/handlers/majorexpenses/handlers_test.go` | 8 new tests above. |
| `web/templates/pages/major-expenses.html` | New filter card; new `major-expenses-results-wrapper` partial; ported `setDateRange`/`stepDateRange` JS; sessionStorage persistence. |
| Existing partials (`major-expenses-list-card-content`, `major-expenses-exceptions-panel`) | Composed inside the new wrapper; mutation forms gain `hx-include="#major-expenses-filter-form"`. |

## Risks / open items

- The existing search bar's row-hide logic (`applyUnifiedFilter`) runs against the rendered DOM. After an HTMX swap of the wrapper, the search input value is preserved (it lives in a separate card outside the swap target). The filter logic re-runs on next keystroke; if the user wants the swap to immediately reapply the active text filter, an `htmx:afterSwap` hook can call `applyUnifiedFilter(currentQuery)`. Decide during implementation; not a blocker.
- New Merchants window (`defaultNewWindowDays = 30`) uses input-relative semantics — "new" means "first seen within the last 30 days of the loaded data". Under windowing, this becomes "first seen within the last 30 days of the in-window data", which is intuitively correct (looking at 2024 → "new merchants in late 2024"). No change needed; flag for awareness.
