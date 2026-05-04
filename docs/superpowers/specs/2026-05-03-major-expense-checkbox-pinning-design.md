# Major-Expense Checkbox Pinning — Design

**Date:** 2026-05-03
**Status:** Reviewed design — ready for implementation plan
**Scope:** UI-layer enhancement to the Exceptions panel on `/major-expenses`. No backend changes.

## Problem

Pinning N exception transactions to the same major expense currently requires opening the per-row "Pin to…" dropdown N times. The existing **filter-driven bulk-pin toolbar** (visible only when the global search box is non-empty) helps when an exact subset can be expressed as a single search, but it offers no way to cherry-pick a heterogeneous subset of exceptions.

## Goals

- Cherry-pick any subset of exception rows (across buckets) and pin them all to one expense without reopening the per-row dropdown for each transaction.
- Preserve every existing single-row workflow: per-row "Pin to…" dropdown, "+ Create new from this…", row-click prefill of the add form.
- Preserve the filter-driven bulk-pin toolbar.

## Non-goals

- Bulk **unpin**. Out of scope for v1.
- New backend endpoints. The existing `POST /major-expenses/pins/bulk` already accepts `expense_id` plus repeated `hashes` form fields and is sufficient.
- Touching the matched-transactions side of the page (left card).

## Design

### 1. New leftmost column on every exception table

A checkbox column is added to all three exception tables (Unmatched / Anomalous / New merchants) and to the legacy `UnknownLarge` render path used by older fixtures.

- **Header cell:** a "select all visible in this bucket" checkbox. "Visible" respects the active search filter — checking the header selects only rows currently shown after filtering, never hidden rows.
- **Body cells:** one checkbox per row, rendered as `input[type=checkbox].major-expenses-pin-check` with `data-hash="<transaction hash>"` and `data-bucket="<stable bucket id>"`. The checkbox `td` and input both stop click propagation so the existing row-click-to-prefill behavior still works on the rest of the row.
- **Indeterminate state:** the header checkbox shows the indeterminate visual when 1 ≤ N < visible-count rows are checked in that bucket.
- **Accessible labels:** header checkboxes use labels like `Select visible unmatched exceptions`; row checkboxes use labels like `Select Big Unknown Charge for bulk pinning`.

The checkbox column is rendered whenever a bucket table renders. It is independent of `.Expenses` / `.ExpenseOptions`; selecting rows without any existing expense is harmless and keeps the layout stable. The bulk toolbar itself remains gated by `.ExpenseOptions`, matching the current template's behavior: if there is no destination expense, there is no bulk Apply UI.

### 2. Unified bulk toolbar

The existing `#major-expenses-bulk-pin` toolbar is reused with two activation modes:

| Trigger                       | Toolbar text                | Hashes posted                       |
|-------------------------------|-----------------------------|-------------------------------------|
| ≥ 1 checkbox checked          | `Pin N selected →`          | the explicitly-checked hashes       |
| 0 checked AND search filter active AND ≥ 1 visible exception | `Pin all M matching →`      | every currently-visible row         |
| Both conditions               | **Checked wins** — toolbar shows `Pin N selected →` and posts checked hashes only |

The toolbar visibility logic is the OR of both triggers. The selection-mode label and post payload come from whichever trigger fires first in priority order (checked > filtered).

A `[Clear]` button is added to the toolbar — visible only in checked-mode — that unchecks every row checkbox, resets every header checkbox/indeterminate state, updates the count chip, and re-runs the toolbar sync.

If there are checked rows but no `#major-expenses-bulk-pin` element because `.ExpenseOptions` is empty, the count chip still reflects the selection and the toolbar simply remains absent. This avoids adding a disabled toolbar with no actionable destination.

### 3. Selection state model

Selection is stored in **the DOM only** (the `:checked` state of the checkboxes). No JS-side `Set` or array. Rationale:

- The exceptions panel is the only consumer.
- HTMX swaps replace the panel wholesale; checkboxes in incoming HTML start unchecked, which is the correct post-mutation state.
- A single `querySelectorAll('tr.major-expenses-exception-row input.major-expenses-pin-check:checked')` on Apply gives the authoritative list.

**HTMX swap behavior:** after a successful bulk-pin, pinned rows disappear (they're now matched). The newly rendered exceptions panel has zero checked rows, the count chip resets to 0, and the toolbar collapses or falls back to filter-driven mode if a search is active. No explicit save/restore needed.

**Search-filter behavior:** filtering hides rows but does not uncheck them. The count chip shows `N selected` (total checked) — hidden checked rows are still part of the bulk-pin payload. This is intentional: a user may filter to find more candidates, check them, then clear the filter; previously checked rows from the wider set must survive. After every filter pass, all bucket header checkboxes must be refreshed against the new visible row set so checked/indeterminate state never reflects stale visibility.

### 4. Shift-click range select

Within a single bucket, shift-clicking a checkbox toggles every **visible** row from the **last-clicked checkbox in that bucket** to the shift-clicked one, applying the shift-clicked target's new value (checked or unchecked) to all rows in between.

- The anchor is stored as the last-clicked row's transaction hash per bucket in a module-local object, e.g. `lastCheckedByBucket[bucketID] = hash`. Avoid `WeakRef`; it is unnecessary and makes the code harder to reason about.
- At shift-click time, the range is computed from the live DOM order of visible rows in that bucket. Sorting (which reorders rows but keeps them visible) automatically yields the correct post-sort range.
- Hidden rows (filtered out) are not in the range and are not toggled.
- A non-shift click always re-anchors that bucket, regardless of whether it checked or unchecked.
- No cross-bucket ranges. Shift-clicking in a different bucket re-anchors that bucket and does not affect any other bucket.
- If the stored anchor hash is no longer visible or no longer present after a filter/swap, treat the shift-click as a normal click and replace the anchor.

### 5. Count chip

A small `· N selected` segment is appended to the existing `N flagged` line in the Exceptions panel header (`major-expenses-exceptions-panel` template):

```
Exceptions                                    14 flagged · 4 selected
```

- Hidden when N = 0.
- Updates whenever any checkbox changes, the Clear button is clicked, the search filter changes visibility, or the panel is re-rendered.
- Read-only display. Selection is cleared via the toolbar's `Clear` button only — keeps the chip's affordance unambiguous.

### 6. Per-row dropdown unchanged

The `major-expenses-pin-picker` template emits the same `<select>` it does today. Single-row pinning, including `+ Create new from this…`, works identically. The bulk path operates **alongside**, not instead of, the per-row dropdown.

## Architecture

### Files touched

| File | Change |
|------|--------|
| `web/templates/pages/major-expenses.html` | Add checkbox column to all three exception tables (Unmatched legacy, Unmatched comprehensive, Anomalous, New merchants). Add `[Clear]` button + dynamic label to existing bulk toolbar. Add `· N selected` segment to panel header. Extend the `<script>` IIFE with: row checkbox listener, header checkbox listener, shift-click range, toolbar sync mode-switch, Clear handler. |
| `internal/templates/render_major_expenses_test.go` | Add render-level assertion for the new checkbox column and header cells (presence + `aria-label` text). |
| `cmd/server/main_test.go` | Review for brittle rendered-table assertions; update only if existing tests assume column counts. Current likely change set is template render tests only. |

No Go handler changes. No new endpoint. No new template helper.

### JS structure

The existing `major-expenses.html` already contains an IIFE with `applyUnifiedFilter`, `syncBulkPinToolbar`, and a click delegator. New responsibilities go in the same IIFE:

```
syncBulkPinToolbar(visibleCount, query)
  ├── (existing) read visibleCount, query
  ├── (new) checkedCount = countChecked()
  ├── (new) mode = checkedCount > 0 ? 'checked' : (visibleCount > 0 && query ? 'filter' : 'hidden')
  ├── show/hide bar
  ├── update label per mode
  └── show/hide [Clear]

countChecked() → number
collectCheckedHashes() → string[]
collectVisibleHashes() → string[]   (existing visibleExceptionHashes, kept)

handleRowCheckboxChange(e)
  ├── update bucket anchor (lastCheckedByBucket[bucket] = hash)
  ├── refresh header indeterminate / checked state for this bucket
  ├── update count chip
  └── syncBulkPinToolbar(...)

handleHeaderCheckboxChange(e)
  ├── flip every visible row checkbox in this bucket to header state
  ├── refresh count chip
  └── syncBulkPinToolbar(...)

handleShiftClick(e)
  ├── if !e.shiftKey: just record anchor hash
  ├── else: span anchor..current visible rows, apply target's new state
  └── handleRowCheckboxChange flow

handleClearButton()
  ├── uncheck every row + every header
  ├── clear every header indeterminate state
  ├── clear lastCheckedByBucket
  └── syncBulkPinToolbar(...)

handleApply()                       (existing path, branched)
  ├── hashes = checkedCount > 0 ? collectCheckedHashes() : collectVisibleHashes()
  └── (existing) htmx.ajax POST /major-expenses/pins/bulk with hashes
```

All listeners are attached via event delegation on `document` (matches existing style) so they survive HTMX swaps without rebinding.

`applyUnifiedFilter()` must call a new `refreshAllBucketCheckboxHeaders()` before `syncBulkPinToolbar(...)`; otherwise a filter can hide checked rows and leave a bucket header checked/indeterminate based on rows that are no longer visible.

## Edge cases

- **Empty exception list:** no rows = no checkboxes. Header checkbox in an empty bucket is hidden (already the case — the table doesn't render).
- **Bulk endpoint failure:** existing handler returns the panel re-rendered with an error toast; checkbox state is lost (incoming HTML has fresh checkboxes). Acceptable — failure is rare and the user re-selects.
- **Click on the checkbox `<td>` (not the input itself):** stop propagation; do not trigger row-click prefill.
- **Click on the header checkbox area:** never collapses the `<details>` bucket. The checkbox is in the table head, the `<summary>` toggle is separate — already isolated by DOM structure.
- **Rows hidden by filter then header-checkbox clicked:** only visible rows in that bucket toggle. Hidden checked rows keep their state.
- **No expenses exist (`.ExpenseOptions` is empty):** "Pin to" column header and the bulk toolbar are suppressed today. Checkbox columns still render, and selection/count behavior still works, but there is no Apply control until an expense exists.
- **Sort order changes (existing sortable tables):** sorting reorders rows but checkbox state is per-row DOM, so it travels with the row. Because the shift-click anchor is stored by row hash and the range is computed from live DOM order, the next shift-click uses the sorted order correctly.
- **HTMX swap while selection exists:** incoming rows are fresh and unchecked. Clear `lastCheckedByBucket` in the `htmx:afterSwap` path before syncing count/toolbar/header state.
- **Duplicate or missing hashes:** transaction hashes are expected to be unique. If a row lacks `data-hash`, do not render a checkbox for it and exclude it from bulk collection.

## Testing

### Render tests (Go)

- `internal/templates/render_major_expenses_test.go`: assert the rendered HTML contains the checkbox column header and one `<input type="checkbox" class="major-expenses-pin-check">` per exception row, in each of the four render paths (legacy `UnknownLarge`, comprehensive Unmatched, Anomalous, New merchants).
- Assert the `· N selected` chip element exists with `hidden` class when no rows are pre-selected (always, server-side).
- Assert checkbox markup renders even when `.Expenses` / `.ExpenseOptions` is empty, while the existing bulk toolbar remains absent in that state.
- Assert row checkbox cells include `onclick="event.stopPropagation()"` or equivalent propagation protection so row-click prefill does not fire from checkbox interaction.

### JS behavior tests

The repo currently has no JS unit-test infrastructure for `major-expenses.html`. We will rely on a single Playwright MCP smoke test (manual, captured as a screenshot in the PR) for v1:

1. Open `/major-expenses`.
2. Check 2 rows in Unmatched, 1 in New merchants → count chip shows `3 selected` and toolbar shows `Pin 3 selected →`.
3. Choose an expense → Apply → expect 3 fewer exceptions and a fresh panel with zero checked rows.
4. Filter by "amazon" with no checks → toolbar shows `Pin all M matching →`.
5. With same filter, check one row → toolbar switches to `Pin 1 selected →` and posts only the checked row.
6. With a checked row hidden by the active filter, verify the count chip still includes it and Apply includes it.
7. Shift-click test: check row 1, shift-click row 5 in the same bucket → visible rows 1–5 checked.
8. Sort the Unmatched bucket, then shift-click within it → range follows the new visible DOM order.
9. Clear button → all unchecked, header checkboxes reset, count chip hides, toolbar collapses or falls back to filter-driven mode if a query is still active.

### Backend

`internal/handlers/majorexpenses/handlers_test.go::TestHandleBulkPin` already exercises the endpoint. No new server-side test needed.

## Rollout

Single PR. No flag needed — pure additive UI change with the new bulk path opt-in (you have to check something to use it). Filter-driven bulk continues to work as a fallback.

## Out of scope (future work)

- Bulk **unpin** for already-matched transactions on the left card.
- Saved selection sets ("pin these 12 every month").
- Keyboard shortcut for "select all across all buckets" (e.g., `Ctrl+A` while focused on the panel).
- Cross-bucket shift-click ranges.
