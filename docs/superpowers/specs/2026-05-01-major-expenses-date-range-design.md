# Major Expenses — Date Range Filter

**Status:** Ready for implementation
**Date:** 2026-05-01
**Owner:** Darrell

## Summary

Add a server-side date range filter to `/major-expenses` so the user can scope per-expense rollups and exception buckets to a window. Mirrors the existing Explorer date-range pattern: two `<input type="date">` controls with `[←] 3M | 6M | 12M | All [→]` quick-buttons. Submits via HTMX, persists per-page in sessionStorage, defaults to all-time on first visit.

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
| 7 | Submission = HTMX swap of a wrapper containing both result cards (no full page reload). `GET /major-expenses` must return the wrapper partial when the request targets that wrapper; otherwise it keeps returning the full base page. |

## Architecture & data flow

`buildPageData()` in `internal/handlers/majorexpenses/handlers.go` resolves and applies an explicit window:

```
LoadData() → MinDate/MaxDate (full range)
           → resolve start/end from request
           → FilterByDateRange(start, end)    ← NEW
           → FilterByType(Outflow)             (existing)
           → Match()                            (unchanged)
           → per-expense Summaries + exception buckets
```

Helpers/signatures:

- `buildPageData(r *http.Request) (map[string]interface{}, error)` — loads transactions once, resolves the request's date range against the full-data `MinDate` / `MaxDate`, filters the transaction set, and returns the current page data. The map gains keys `StartDate`, `EndDate`, `MinDate`, `MaxDate` as `YYYY-MM-DD` strings. If the data set is empty, these strings are empty rather than `0001-01-01`.
- `parseRangeFromRequest(r *http.Request, txns *models.TransactionSet) (start, end time.Time)` — reads `start`/`end` first from `r.URL.Query()`, falls back to parsed form values (covers POST/PUT/DELETE bodies). Empty or unparseable values fall back to `txns.MinDate()` / `txns.MaxDate()`. Mirrors `internal/handlers/explorer/handlers.go:142-152`, but defaults to all-time instead of YTD.
- `formatDateInputValue(t time.Time) string` — returns `""` for zero time, otherwise `t.Format("2006-01-02")`.
- `renderResults(w http.ResponseWriter, r *http.Request)` — rebuilds page data from the active request window.

Handlers updated:

- `handleMajorExpensesPage` — call `buildPageData(r)`. If the request is an HTMX request targeting `major-expenses-results-wrapper`, render only the `major-expenses-results-wrapper` partial; otherwise render `base`.
- `handleExceptions` — call `buildPageData(r)` and render only the exceptions panel.
- `handleAdd`, `handleUpdate`, `handleDelete`, `handlePin`, `handleBulkPin`, `handleUnpin` — call `renderResults(w, r)` after mutation so post-mutation re-render preserves the active window.

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

Important HTMX detail: `GET /major-expenses` normally renders `base`. For the filter form, `handleMajorExpensesPage` must detect the HTMX target and render `major-expenses-results-wrapper` only. Returning the full base page into `#major-expenses-results-wrapper` would nest a complete page inside the results area.

JS helpers `setDateRange(months)` and `stepDateRange(direction)` are ported page-local from `web/templates/pages/explorer.html:286-340`. They read `document.querySelector('#major-expenses-filter-form input[name="start"]')` etc., set ISO date strings, then dispatch the `change` event so HTMX submits.

sessionStorage persistence mirrors Explorer (`web/templates/pages/explorer.html:148-170`):
- Bare `/major-expenses` with empty query + saved key → redirect to `/major-expenses?<saved>`.
- Every `htmx:configRequest` from the filter form stores the outgoing request parameters (`start`/`end`) in `sessionStorage["majorExpensesFilters"]`. Do not save `window.location.search` inside `htmx:configRequest`; it still contains the old URL until the request completes.

The existing client-side text search (`applyUnifiedFilter`) remains the second-stage filter. Server narrows by date → DOM contains windowed rows → text search hides further. After the date-range HTMX swap, the existing `htmx:afterSwap` hook should call `applyUnifiedFilter(currentQuery)` even when the query is non-empty, because the search input lives outside the swapped wrapper and retains its value.

## Mutation handler behavior

Every mutation must re-render the page with the active window applied — otherwise saving an expense definition jumps the page back to all-time, which is jarring.

Two viable approaches:

| Option | Wiring | Trade-off |
|--------|--------|-----------|
| `hx-include` | Add `hx-include="#major-expenses-filter-form"` to each mutation form/button | One attribute per form. Single source of truth (the filter form). Recommended. |
| Hidden inputs | Render hidden `start`/`end` inputs inside each mutation form | More verbose; risks drift if filter form values change without re-rendering hidden inputs. |

We use `hx-include`. The mutation handlers read `start`/`end` from the request via `parseRangeFromRequest`, no template duplication.

Mutation controls currently target `#major-expenses-results` and rely on the `major-expenses-results` partial plus an OOB swap to refresh the left card. That can stay in place for the mutation path. The date filter is the only flow that needs to replace the outer `major-expenses-results-wrapper`.

**Pin edge case**: pinning a transaction whose date is outside the active window. The pin succeeds (pins are by hash, date-independent). The pinned transaction does not appear in the re-rendered list because it's outside the window. Correct behavior — pins are persistent across windows; views are not.

## Edge cases & validation

- **Empty data store**: `MinDate`/`MaxDate` return zero time. Inputs render with empty `value`/`min`/`max` strings. Cards render empty (existing behavior).
- **Unparseable `start` or `end`**: `time.Parse` errors are silently swallowed; fall back to MinDate/MaxDate respectively. No 4xx.
- **`start > end`**: `FilterByDateRange` returns an empty transaction set. Every defined expense still appears in the left card with count `0` / total `$0.00`; exception buckets are empty. No 4xx or warning — user sees the crossed inputs.
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
5. `TestHandleMajorExpensesPage_StartAfterEndReturnsEmptyWindow` — GET with start > end; assert all defined expenses are still represented with zero counts/totals, exception buckets are empty, status 200.
6. `TestHandleAdd_PreservesDateRange` — POST a new expense with `start`/`end` form values; assert rendered partial reflects the window.
7. `TestHandleBulkPin_PreservesDateRange` — same for bulk pin.
8. `TestHandleExceptions_StartEndQueryParams` — GET `/major-expenses/exceptions?start=...&end=...`; assert windowed buckets.
9. `TestHandleMajorExpensesPage_HTMXFilterReturnsWrapperOnly` — HTMX GET `/major-expenses?start=...&end=...` with `HX-Target: major-expenses-results-wrapper`; assert response contains the wrapper and does not contain the full layout/base page.

JS quick-buttons (`setDateRange` / `stepDateRange`) are a literal port of working Explorer code; smoke-checked manually after wiring rather than covered by Playwright tests.

## Files affected

| File | Change |
|------|--------|
| `internal/handlers/majorexpenses/handlers.go` | `buildPageData` gains window; `parseRangeFromRequest` helper; all GET/POST/PUT/DELETE handlers parse and pass the window. |
| `internal/handlers/majorexpenses/handlers_test.go` | 9 new tests above. |
| `web/templates/pages/major-expenses.html` | New filter card; new `major-expenses-results-wrapper` partial; ported `setDateRange`/`stepDateRange` JS; sessionStorage persistence. |
| Existing partials (`major-expenses-list-card-content`, `major-expenses-exceptions-panel`) | Composed inside the new wrapper; mutation forms gain `hx-include="#major-expenses-filter-form"`. |

## Risks / open items

- The existing search bar's row-hide logic (`applyUnifiedFilter`) runs against the rendered DOM. After an HTMX swap of the wrapper, the search input value is preserved because it lives outside the swap target. The active search must be re-applied in `htmx:afterSwap` so a date change does not briefly show unfiltered rows under a non-empty search input.
- New Merchants window (`defaultNewWindowDays = 30`) uses input-relative semantics — "new" means "first seen within the last 30 days of the loaded data". Under windowing, this becomes "first seen within the last 30 days of the in-window data", which is intuitively correct (looking at 2024 → "new merchants in late 2024"). No change needed; flag for awareness.
