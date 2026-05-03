# Major-Expense Checkbox Pinning — Smoke Verification

**Branch:** `dev`
**Date:** 2026-05-03
**Plan:** `docs/superpowers/plans/2026-05-03-major-expense-checkbox-pinning.md`

The plan called for a Playwright-driven interactive smoke matrix and a screenshot of the "Pin N selected" state. Both browser-automation MCPs (`mcp__plugin_playwright_playwright__*` and `mcp__claude-in-chrome__*`) were unavailable in the executing environment, so this run substitutes a static-rendered + JS-syntax smoke check that covers each scenario the interactive matrix would have. The interactive matrix (Section 3 below) is left for the user or a future run.

## 1. Static rendered-HTML smoke (executed)

Rendered `/major-expenses` against the live local dataset and grepped for every new identifier the feature introduces.

| Marker | Expected | Observed |
|---|---|---|
| `class="major-expenses-pin-check"` (row inputs) | one per exception row | 294 |
| `class="major-expenses-pin-check-cell"` (wrapping `<td>`) | one per row | 294 |
| `id="major-expenses-pin-check-header-unmatched"` | 1 | 1 |
| `id="major-expenses-pin-check-header-anomalous"` | 1 | 1 |
| `id="major-expenses-pin-check-header-new-merchants"` | 1 | 1 |
| `data-bucket="unmatched"` rows | matches Unmatched count | 262 |
| `data-bucket="anomalous"` rows | matches Anomalous count | 3 |
| `data-bucket="new-merchants"` rows | matches NewMerchants count | 32 |
| `id="major-expenses-pin-count-chip"` | 1 | 1 |
| `id="major-expenses-pin-count-chip-num"` | 1 | 1 |
| `class="major-expenses-pin-count-chip hidden` (chip default-hidden) | 1 | 1 |
| `aria-live="polite"` on outer chip wrapper (a11y fix) | ≥ 1 | 2 |
| `id="major-expenses-bulk-pin"` (toolbar) | 1 | 1 |
| `id="major-expenses-bulk-pin-apply"` | 1 | 1 |
| `id="major-expenses-bulk-pin-clear"` | 1 | 1 |
| `class="major-expenses-bulk-pin-clear hidden` (Clear default-hidden) | 1 | 1 |
| `class="major-expenses-bulk-pin-label-lead"` | 1 (per rendered toolbar) | 2 |
| `class="major-expenses-bulk-pin-label-trail"` | 1 (per rendered toolbar) | 2 |

## 2. JS-syntax smoke (executed)

Extracted the page-level IIFE (22 KB of JS) and validated with `node --check`.

| Function | Defined |
|---|---|
| `rowCheckboxes` | yes |
| `visibleRowCheckboxesInBucket` | yes |
| `countChecked` | yes |
| `collectCheckedHashes` | yes |
| `refreshBucketHeader` | yes |
| `refreshAllBucketHeaders` | yes |
| `refreshCountChip` | yes |
| `syncBulkPinToolbar` | yes |

`node --check /tmp/major-expenses-iife.js` → exit 0. No parse errors.

## 3. Interactive matrix (deferred to user / next session)

Left for the user to walk through after the branch is merged or in a session with working browser MCP:

1. Open `/major-expenses`.
2. Check 2 rows in Unmatched + 1 in NewMerchants → count chip "· 3 selected" + toolbar "Pin 3 selected →" + Clear button visible.
3. Choose an expense in the toolbar dropdown → Apply enables → click → 3 rows disappear → fresh panel with empty selection.
4. With nothing checked, type "amazon" in unified search → toolbar shows "Pin all M matching →" (legacy filter path).
5. With same filter, check one row → toolbar switches to "Pin 1 selected →".
6. Check one row, then filter to hide it → count chip still includes it; Apply still posts that hash.
7. Check row 1 in a bucket with ≥ 5 rows, shift-click row 5 → rows 1–5 all checked.
8. Sort the Unmatched bucket by Amount, then shift-click within → range follows new sorted order.
9. Click Clear → all unchecked, chip hides, toolbar collapses (or falls back to filter mode).

## 4. Verification summary

- Server-side rendering of every new element and class: ✅
- All four render paths emitting checkbox markup: ✅
- Default-hidden state on count chip + Clear button: ✅
- `aria-live` accessibility fix in place on outer wrapper: ✅
- Backend endpoint unchanged; existing `TestHandleBulkPin` covers the API: ✅
- JS parses cleanly: ✅
- Render tests in `internal/templates/render_major_expenses_test.go` cover: checkbox column on each bucket (comprehensive + legacy paths), count chip element, Clear button + hidden class, no-expenses edge case, lead/trail label spans: ✅
- Interactive click/shift-click/Apply behavior: deferred (Section 3).
