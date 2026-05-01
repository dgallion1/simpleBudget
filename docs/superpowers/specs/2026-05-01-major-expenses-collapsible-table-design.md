# Major Expenses — Collapsible Table & Compact Add Affordance

**Date:** 2026-05-01
**Branch:** data-storage-improvements
**Status:** Ready for implementation

## Problem

The "Your Major Expenses" list card on `/major-expenses` shows every declared expense as an always-expanded edit form, plus a separate always-visible add form above the list. With 10+ expenses, the card becomes a long stack of identical-looking forms; the user has to scroll past edit fields just to skim names and totals. The add form alone occupies ~40% of the card vertical space even when not in use.

## Goals

1. Compact, scannable list of expenses by default.
2. Reach edit + matched-transactions for any expense in one click.
3. Keep the add affordance discoverable without permanently consuming space.
4. Surface the **sum of declared-expense totals** for the active date window prominently.
5. Preserve all existing behavior: keyword/amount/notes editing, deletion, matched-txn drill-down with pin/unpin, search filtering, htmx-driven mutations, OOB swaps, sessionStorage filter restore, persisted-open-state across swaps.

## Non-Goals

- No change to the right-hand Exceptions panel.
- No change to the Major Expenses filter card (date range, quick-range buttons).
- No change to mutation endpoints, request routing, or the `ExpenseSummary` type.
- No new client-side framework or modal/dialog library.

## Current Implementation Constraints

- Mutations currently target `#major-expenses-results`. The `major-expenses-results` partial refreshes the right Exceptions panel and OOB-swaps `#major-expenses-list-card`. Preserve this flow; do not retarget every mutation to the wrapper.
- The unified search input lives outside the HTMX-swapped wrapper. The existing `htmx:afterSwap` hook must continue to re-apply the active query after date-range swaps and mutation swaps.
- Exception-row click-to-prefill currently assumes `#major-expenses-add-form` is visible. After the add form moves into a collapsed panel, that click path must open the add panel before filling fields and focusing the name input.
- Anomalous exception links currently scroll to `#major-expense-item-{ID}`. Keep a stable target id on the summary row and update the click handler to open the corresponding table body before highlighting.

## Design

### Card structure (replaces today's list card)

```
┌─ Your Major Expenses ──── Total: $4,231 · 12 declared ── [+] ─┐
│ ▸ Car Payment             3 matched          $1,878    ✕      │
│ ▸ Lucid Loan              3 matched          $4,740    ✕      │
│ ▾ Cellphone (Verizon)     6 matched            $588    ✕      │
│ ┌──────────────────────────────────────────────────────────┐ │
│ │ Edit form (name, keywords, min, max, notes)              │ │
│ │ Matched transactions table (date, desc, amount, unpin)   │ │
│ └──────────────────────────────────────────────────────────┘ │
│ ▸ Subscription            14 matched          $668    ✕      │
│ …                                                            │
└──────────────────────────────────────────────────────────────┘
```

### Header row

- `<h2>Your Major Expenses</h2>` on the left (unchanged text).
- Right side, in order: **Total** (`$X,XXX` from sum across `Summaries[].Total`), separator `·`, **count** (`N declared`), separator `·`, **add icon** — a `<button>` with a plus-sign SVG that toggles the add form.
- Total uses muted-but-readable styling (e.g. `text-sm font-medium text-gray-700 dark:text-gray-200`) — it's a status, not a number to action on. No color emphasis (it isn't a "cost styling" item; it's just a sum the user already chose to track).

### Add affordance (Choice A — inline expanding panel)

- The existing `<form id="major-expenses-add-form">` is wrapped in a `<details id="major-expenses-add-panel">` element placed **between the header row and the table**.
- Include a valid first-child `<summary>` for HTML semantics, visually hidden with the existing screen-reader utility pattern rather than `display: none`. The `<details>` `open` state is toggled by clicking the `[+]` icon in the header. Use a tiny inline script that flips `panel.open`, mirrors `aria-expanded`, and focuses the name field when opening.
- When closed (default): zero visual footprint apart from a thin border below the header row.
- After successful submission, the form auto-resets via the existing `hx-on::after-request="if(event.detail.successful) this.reset()"`. Add: also collapse the panel on successful submit.
- Form fields, validation, and HTMX wiring remain identical to today.
- Exception-row click-to-prefill must open this panel before filling and scrolling to the form.

### Table

- Rendered as a real `<table id="major-expenses-list">` for semantics and screen-reader behavior. **Each expense gets its own `<tbody data-expense-id="{ID}" data-open="false">`** containing exactly two `<tr>` elements: a compact summary row (always visible) and a detail row (`<tr id="major-expense-detail-{ID}" class="major-expense-detail-row">` with a single `<td colspan="5">` whose visibility toggles). One `<tbody>` per expense lets the `data-open` attribute scope to one row without sibling-selector gymnastics.
- The compact row keeps the stable jump target: `<tr id="major-expense-item-{ID}" class="major-expense-item-row" data-search="...">`.
- We avoid `<details>`/`<summary>` for the *row* level because they cannot be table descendants. Instead use a chevron `<button type="button" class="major-expense-row-toggle" aria-expanded="false" aria-controls="major-expense-detail-{ID}">` in the first cell of the summary row that toggles `data-open` on its parent `<tbody>`. CSS: `#major-expenses-list tbody[data-open="false"] > tr.major-expense-detail-row { display: none; }`. Initial state: closed for all rows.
- The edit form moves into the detail cell as `<form id="major-expense-edit-{ID}">`; do not wrap a `<tr>` in a `<form>`.
- Columns (collapsed):
  1. Chevron (▸/▾) — 1ch
  2. Name — flex/truncate
  3. Matched count — right-aligned, with the pinned-count badge inline (`📌 N pinned` when `PinnedCount > 0`), preserving today's amber styling
  4. Total — right-aligned, font-mono
  5. Delete (✕) — right-aligned, 1ch
- Empty state (no expenses declared): `<p>` says "No major expenses declared yet. Click the + above to add your first one." (replaces today's "Add your first one below.").

### Expanded row content

- Single `<td colspan="5">` containing two stacked sections, no nested toggle:
  1. The current edit form body (name, keywords, min, max, notes), now in `<form id="major-expense-edit-{ID}">`, with the same htmx PUT-on-change behavior. Keep existing search-bait `data-search` attribute on the **summary `<tr>`** so the search filter still works.
  2. The matched-transactions `<table>` from today's `<details>` block — but flat, no `<details>`/`<summary>`. Header reads: "Matched transactions ({{.Count}}{{if .PinnedCount}}, {{.PinnedCount}} pinned{{end}})".
- Keep `major-expense-matched-row`, per-row `data-search`, Explorer description links, pinned markers, unpin buttons, `hx-include="#major-expenses-filter-form"`, and all mutation URLs intact.

### Search interaction

- Today's `applyUnifiedFilter()` JS toggles `display` on `.major-expense-item-row` and searches matched rows via `item.querySelectorAll('.major-expense-matched-row')`. With the new layout, matched rows are no longer descendants of the summary row; they live in the sibling detail row inside the same `<tbody>`.
- Update the left-card search loop to iterate by expense group: `#major-expenses-list tbody[data-expense-id]`. For each group, read the summary row's `data-search`, find `.major-expense-matched-row` inside the same group, and set `group.style.display`.
- Empty query: every expense group visible, every matched transaction row visible, and no row is force-opened.
- Query matches summary metadata only: show the group, but do not force-open the detail row.
- Query matches one or more nested matched transactions: show the group, hide non-matching matched transaction rows, and force-open the row by setting `tbody[data-open="true"]` plus `aria-expanded="true"` on the chevron.
- No match: hide the entire `<tbody>`.
- The status badge continues to count visible expense groups and visible exception rows.

### Persisted open-state across HTMX swaps

- Today's `htmx:beforeSwap` snapshots `<details id>` open state via the selector `#major-expenses-results details[id], #major-expenses-list-card details[id]`. The new layout no longer uses row-level `<details>`, so the JS gains a parallel snapshot/restore of `data-open` on `tbody[data-expense-id]`:
  - Before swap: collect the set of `data-expense-id` values from `tbody[data-open="true"]`.
  - After swap: re-apply `data-open="true"` and update `aria-expanded` on the matching chevron buttons.
- The add panel's `<details id="major-expenses-add-panel">` uses the existing details-snapshot path, so its open state survives swaps automatically.
- Continue to re-apply active search after swap. Search can also force-open rows that matched nested transactions; restore logic should not force-close anything.

### Jump-to-existing behavior

- Keep `id="major-expense-item-{ID}"` on the summary row so anomalous exception links remain targetable.
- When `[data-jump-expense]` is clicked, scroll the summary row into view, open its containing `<tbody>`, update the chevron `aria-expanded`, and briefly highlight the summary row or containing `<tbody>`.
- Do not rely on the edit form carrying `id="major-expense-item-{ID}"`; the edit form id is now `major-expense-edit-{ID}`.

### Server-side change

In `internal/handlers/majorexpenses/handlers.go`, where the response context is built:

```go
var totalDeclared float64
for _, s := range summaries {
    totalDeclared += s.Total
}
return map[string]interface{}{
    // …
    "Summaries":     summaries,
    "TotalDeclared": totalDeclared,  // NEW
    // …
}
```

That's the only handler change. No new endpoints and no existing context fields are removed; the nil-renderer JSON test path will include this one additional key.

Before implementation, run GitNexus impact analysis for symbols that will be edited, especially `buildPageData` and any named template/JS helpers touched by the change. Expected risk is medium because one template/JS change affects add, update, delete, pin, unpin, bulk pin, date-range swaps, OOB refresh, and search reapplication.

## Affected Files

- `web/templates/pages/major-expenses.html` — template restructure: header row, add panel, table, expanded row, JS for chevron/add toggles, group-based search, jump/prefill behavior, and snapshot/restore for `data-open`.
- `internal/handlers/majorexpenses/handlers.go` — add `TotalDeclared` to context.
- `internal/templates/render_major_expenses_test.go` — update existing render assertions for new structure; add assertions for total, add toggle, table groups, chevron, detail row, and collapsed-by-default state.
- `internal/handlers/majorexpenses/handlers_test.go` — add a test that the `TotalDeclared` context key equals the sum of `Summaries[].Total`.

## Testing

- Existing handler tests continue to pass without semantic changes (mutation responses still use the same templates and OOB swaps).
- New unit test: `TotalDeclared` matches `sum(Summaries[].Total)` for a fixture set with mixed match counts.
- Updated render test: rendered HTML contains the total, the `[+]` icon button, and a `<tr class="major-expense-item-row">` with a chevron button per declared expense; expanded row exists in markup but its `<tbody>` has `data-open="false"` initially.
- Updated render test: `#major-expenses-list` is a table, each expense has one `tbody[data-expense-id]`, the edit form id is `major-expense-edit-{ID}`, and old row-level matched `<details id="major-expense-matched-{ID}">` is gone.
- Keep the existing OOB swap test: mutation responses must still include `hx-swap-oob="innerHTML"` for `#major-expenses-list-card`.
- Manual verification (per CLAUDE.md UI guidance): with `make dev` running, confirm
  1. Default state — table compact, total visible, add panel hidden.
  2. Click `[+]` — add form expands; submit creates expense; panel collapses on success.
  3. Click chevron — single row expands with edit form + matched-txn table flat below.
  4. Edit a field — htmx PUT fires after 500ms and the right card re-renders, the row stays open.
  5. Search filters rows by name/keywords/notes; matching matched-txns force-open their row.
  6. Search by exception still filters the right card and enables bulk pin.
  7. Date-range change — list re-renders, active search reapplies, and no row opens unless restored or forced by search.
  8. Open one row → change date range → row stays open after swap when the expense id still exists.
  9. Click an anomalous exception expense link — target row opens, scrolls into view, and highlights.
  10. Click an exception row to prefill the add form while the add panel is closed — panel opens, fields fill, and name receives focus.

## Risks & Trade-offs

- **`<table>` with toggleable detail rows requires JS** (chevron `aria-expanded` + `tbody[data-open]`). Today's per-item `<details>` is JS-free for the row toggle itself, but JS is already mandatory for search and HTMX persistence — net cognitive cost is small.
- **Loss of native disclosure semantics** — the chevron button + `aria-expanded` is the standard accessible pattern; documented in WAI-ARIA Authoring Practices for disclosure widgets.
- **Search selector regression risk** — moving matched rows into a sibling detail row requires changing search from row-based to `tbody`-group-based.
- **OOB swap regression risk** — mutation controls should keep targeting `#major-expenses-results`; the left card should continue refreshing through the existing OOB template block.
- **Search-doesn't-force-open is a behavior change** for metadata-only matches (today the form is always visible). Forcing matched-txn matches to open the row preserves the case where search needs disclosure-state changes.
- **Add panel collapses on success** is a small behavior shift from today's "form remains visible after add". Acceptable: the icon makes re-opening cheap, and the user typically adds one expense at a time.

## Out-of-Scope Improvements (deliberately deferred)

- Sorting columns (by name/total/count) — table layout enables it but no current ask.
- Inline rename without expanding — keep edit-on-expand simple.
- Bulk-delete — not requested.
