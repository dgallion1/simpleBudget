# Exception-row description → Explorer link

**Date:** 2026-05-02
**Status:** Approved (pending implementation)
**Owner:** Darrell

## Problem

On the Major Expenses page, the **Exceptions** panel lists transactions that need user attention across three buckets: **Unmatched**, **Anomalous**, and **New Merchants**. Each row shows date, description, amount, and a "Pin to…" control.

Users cannot trace an exception back to the underlying bank transaction in the Explorer. The description column is plain text, and the only interactive elements (the row-level click handler that pre-fills the add form, the "Pin to…" select, and — in NewMerchants/Anomalous — a same-page anchor to the *matched expense bucket*) all keep the user inside the Major Expenses page.

The existing matched-rows section (rendered inside each major-expense bucket, template `major-expenses-list-card-content`, line ~752) already wraps each transaction's description in an anchor pointing to `/explorer?search=<desc>&type=Outflow`. The exception rows lack that affordance even though the underlying need — "show me this transaction in context" — is the same.

User report (verbatim): *"Shows links that don't help me. See 'home support' → 'home support' … I can't trace the expense back to the original expense from the bank."*

## Solution

Add a description-column hyperlink to **every exception row** in all three buckets, using the same `/explorer?search=…&type=Outflow` pattern already used for matched rows. Display text remains the cleaned `Label`; the search query uses the raw `DisplayName` (with fallback to `Description`) — identical convention to the matched-row link.

### Anchor pattern

```html
<a href="/explorer?search={{urlquery $rawText}}&type=Outflow"
   class="text-blue-600 dark:text-blue-400 hover:underline"
   title="Show this transaction in the Explorer"
   onclick="event.stopPropagation()">{{$label}}</a>
```

`event.stopPropagation()` is required because each exception `<tr>` has a row-level click handler that pre-fills the add form (`data-fill-name`, `data-fill-keyword`, `data-fill-amount`). Without it, clicking the link would both navigate *and* mutate the add form — unintended.

### Locations (all in `web/templates/pages/major-expenses.html`)

| # | Bucket | Line | Current cell | Variable for `$label` | Variable for `$rawText` |
|---|--------|------|--------------|-----------------------|-------------------------|
| 1 | UnknownLarge legacy fallback | 954 | `<td class="px-2 py-1 dark:text-gray-200">{{$label}}</td>` | already set line 946 | already set line 946 |
| 2 | AllUnmatched (current Unmatched bucket) | 988 | `<td class="px-2 py-1 dark:text-gray-200">{{$label}}</td>` | already set line 979 | already set line 979 |
| 3 | Anomalous (Description column) | 1036 | `<td class="px-2 py-1 dark:text-gray-300">{{$desc}}</td>` | `$desc` (line 1024) | `$rawText` (line 1024) |
| 4 | NewMerchants | 1084 | `<td class="px-2 py-1 dark:text-gray-200">{{$label}}</td>` | already set line 1075 | already set line 1075 |

In location 3 the Anomalous bucket already exposes `$desc` for display and `$rawText` for the raw form; the new anchor uses `$rawText` for the query and `$desc` for the visible text.

The dark-text utility class on each `<td>` (`dark:text-gray-200` / `dark:text-gray-300`) stays on the `<td>`. The anchor's own `text-blue-600 dark:text-blue-400` overrides it for the link text — matching the matched-row link's visual treatment.

## Out of scope

Intentionally **not** changing:

- The "Pin to…" select and its pinned-state display — separate concern.
- The matched-row link in `major-expenses-list-card-content` — already correct, unchanged.
- The same-page `#major-expense-item-<id>` anchor in NewMerchants (line 1095-1096) and Anomalous (line 1032-1034) — these jump to the *bucket* on the same page and are complementary to the new Explorer link, not redundant.
- The row-level click handler that pre-fills the add form — preserved; clicking row whitespace still pre-fills.
- The row's `title` attribute — preserved.
- Engine, handler, or model logic. This is a presentation-only change.

## Testing

Extend `internal/templates/render_major_expenses_test.go` (or the analogous existing test) with a fixture that produces at least one row in each of the three exception buckets, and assert:

1. The rendered HTML contains an `<a href="/explorer?search=…&type=Outflow">` anchor for each exception bucket's description cell.
2. The anchor's `href` is properly URL-encoded (i.e. `urlquery` did its job — descriptions with `&`, spaces, or `#` round-trip safely).
3. The anchor includes `onclick="event.stopPropagation()"`.
4. The visible text matches the cleaned label (`Transaction.Label`), not the raw description, when those differ.

No new engine tests are needed — the engine is unchanged.

## Acceptance criteria

- All four template locations carry the anchor.
- Clicking the description on any exception row in any of the three buckets opens the Explorer pre-filtered to outflows matching that description.
- Clicking the description does **not** trigger the add-form pre-fill (verified manually or via a Playwright check).
- The pinned-state display in the "Pin to…" column is unaffected.
- All existing render tests still pass; new render-test assertions added per the Testing section pass.
