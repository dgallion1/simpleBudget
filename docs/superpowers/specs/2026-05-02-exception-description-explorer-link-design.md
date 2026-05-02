# Exception-row description → Explorer link

**Date:** 2026-05-02
**Status:** Approved (reviewed against current template; pending implementation)
**Owner:** Darrell

## Problem

On the Major Expenses page, the **Exceptions** panel lists transactions that need user attention across three buckets: **Unmatched**, **Anomalous**, and **New Merchants**. The Unmatched bucket has two render paths: the current comprehensive `.AllUnmatched` list and a legacy `.Match.Exceptions.UnknownLarge` fallback used by older fixtures. Each row shows date, description, amount, and a "Pin to…" / "Move to" control when expense options exist.

Users cannot trace an exception back to the underlying bank transaction in the Explorer. The description column is plain text, and the only interactive elements (the row-level click handler that pre-fills the add form, the "Pin to…" select, and — in NewMerchants/Anomalous — a same-page anchor to the *matched expense bucket*) all keep the user inside the Major Expenses page.

The existing matched-rows section (rendered inside each major-expense bucket, template `major-expenses-list-card-content`, line ~752) already wraps each transaction's description in an anchor pointing to `/explorer?search=<desc>&type=Outflow`. The exception rows lack that affordance even though the underlying need — "show me this transaction in context" — is the same.

User report (verbatim): *"Shows links that don't help me. See 'home support' → 'home support' … I can't trace the expense back to the original expense from the bank."*

## Solution

Add a description-column hyperlink to **every exception row** in all three buckets, covering both Unmatched render paths, using the same `/explorer?search=…&type=Outflow` route and visual treatment already used for matched rows. Display text remains the cleaned `Label`; the search query uses the raw `DisplayName` with fallback to `Description`.

This intentionally differs slightly from the current matched-row implementation, which displays `$bankText` directly. For exception rows, keep the existing cleaned display text so this change only adds navigation and does not alter the row labels.

### Anchor pattern

```html
<a href="/explorer?search={{urlquery $rawText}}&type=Outflow"
   class="text-blue-600 dark:text-blue-400 hover:underline"
   title="Show this transaction in the Explorer"
   onclick="event.stopPropagation()">{{$label}}</a>
```

`event.stopPropagation()` is required because each exception `<tr>` has a row-level click handler that pre-fills the add form (`data-fill-name`, `data-fill-keyword`, `data-fill-amount`). Without it, the delegated document click handler would also see the click as an exception-row click and mutate the add form before navigation.

### Locations (all in `web/templates/pages/major-expenses.html`)

Reviewed against the current `web/templates/pages/major-expenses.html` on 2026-05-02:

| # | Bucket / render path | Approx. line | Current cell | Variable for `$label` | Variable for `$rawText` |
|---|--------|------|--------------|-----------------------|-------------------------|
| 1 | Unmatched legacy fallback (`UnknownLarge`) | ~954 | `<td class="px-2 py-1 dark:text-gray-200">{{$label}}</td>` | already set near line 946 | already set near line 946 |
| 2 | Unmatched current list (`AllUnmatched`) | ~988 | `<td class="px-2 py-1 dark:text-gray-200">{{$label}}</td>` | already set near line 979 | already set near line 979 |
| 3 | Anomalous description column | ~1036 | `<td class="px-2 py-1 dark:text-gray-300">{{$desc}}</td>` | `$desc`, set near line 1024 | `$rawText`, set near line 1024 |
| 4 | New Merchants | ~1084 | `<td class="px-2 py-1 dark:text-gray-200">{{$label}}</td>` | already set near line 1075 | already set near line 1075 |

In location 3 the Anomalous bucket already exposes `$desc` for display and `$rawText` for the raw form; the new anchor uses `$rawText` for the query and `$desc` for the visible text.

The dark-text utility class on each `<td>` (`dark:text-gray-200` / `dark:text-gray-300`) stays on the `<td>`. The anchor's own `text-blue-600 dark:text-blue-400` overrides it for the link text — matching the matched-row link's visual treatment.

## Out of scope

Intentionally **not** changing:

- The "Pin to…" select and its pinned-state display — separate concern.
- The matched-row link in `major-expenses-list-card-content` — already correct, unchanged.
- The same-page `#major-expense-item-<id>` anchor in NewMerchants (line ~1095) and Anomalous (line ~1032) — these jump to the matched *major expense* on the same page and are complementary to the new Explorer link, not redundant.
- The row-level click handler that pre-fills the add form — preserved; clicking row whitespace still pre-fills.
- The row's `title` attribute — preserved.
- Engine, handler, or model logic. This is a presentation-only change.

## Testing

Extend `internal/templates/render_major_expenses_test.go`. The existing `TestRenderMajorExpenses_WithEntriesAndExceptions` fixture already produces `UnknownLarge`, `Anomalous`, and `NewMerchants` rows; add assertions there for those render paths. Add or extend a separate `AllUnmatched` fixture for the current Unmatched path because that path suppresses the legacy `UnknownLarge` branch when present.

Assert:

1. The rendered HTML contains an `<a href="/explorer?search=…&type=Outflow">` anchor for each exception description cell.
2. The anchor's `href` is properly URL-encoded with `urlquery`. In raw `html/template` output, spaces may appear as `&#43;` because the URL-encoded `+` is HTML-escaped in attribute context; tests can either assert that encoded string or parse the href before checking query values.
3. The anchor includes `onclick="event.stopPropagation()"`.
4. The visible text matches the cleaned label (`Transaction.Label`), not the raw description, when those differ.

No new engine tests are needed — the engine is unchanged.

## Acceptance criteria

- All four exception description render paths carry the anchor.
- Clicking the description on any exception row in any of the three buckets opens the Explorer pre-filtered to outflows matching that description.
- Clicking the description does **not** trigger the add-form pre-fill (verified manually or via a Playwright check).
- The pinned-state display in the "Pin to…" column is unaffected.
- All existing render tests still pass; new render-test assertions added per the Testing section pass.
