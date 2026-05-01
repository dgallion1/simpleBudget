# Major Expenses — Collapsible Table & Compact Add Affordance

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
- No change to mutation endpoints, response shapes, or the `ExpenseSummary` type.
- No new client-side framework or modal/dialog library.

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
- The `<summary>` is `display: none` (or `hidden`); the `<details>` `open` state is toggled by clicking the `[+]` icon in the header. Use a tiny inline script that flips `panel.open` on icon click — same pattern already used elsewhere in this codebase for click-driven `<details>` (no new lib).
- When closed (default): zero visual footprint apart from a thin border below the header row.
- After successful submission, the form auto-resets via the existing `hx-on::after-request="if(event.detail.successful) this.reset()"`. Add: also collapse the panel on successful submit.
- Form fields, validation, and HTMX wiring remain identical to today.

### Table

- Rendered as a real `<table id="major-expenses-list">` for semantics and screen-reader behavior. **Each expense gets its own `<tbody data-expense-id="{ID}" data-open="false">`** containing exactly two `<tr>` elements: a compact summary row (always visible) and a detail row (`<tr class="major-expense-detail-row">` with a single `<td colspan="5">` whose visibility toggles). One `<tbody>` per expense lets the `data-open` attribute scope to one row without sibling-selector gymnastics.
- We avoid `<details>`/`<summary>` for the *row* level because they cannot be table descendants. Instead use a chevron `<button aria-expanded="false" aria-controls="major-expense-detail-{ID}">` in the first cell of the summary row that toggles `data-open` on its parent `<tbody>`. CSS: `tbody[data-open="false"] tr.major-expense-detail-row { display: none; }`. Initial state: closed for all rows.
- Columns (collapsed):
  1. Chevron (▸/▾) — 1ch
  2. Name — flex/truncate
  3. Matched count — right-aligned, with the pinned-count badge inline (`📌 N pinned` when `PinnedCount > 0`), preserving today's amber styling
  4. Total — right-aligned, font-mono
  5. Delete (✕) — right-aligned, 1ch
- Empty state (no expenses declared): `<p>` says "No major expenses declared yet — click the **+** above to add your first one." (replaces today's "Add your first one below.").

### Expanded row content

- Single `<td colspan="5">` containing two stacked sections, no nested toggle:
  1. The current edit `<form id="major-expense-item-{ID}">` body (name, keywords, min, max, notes, htmx PUT on change). Keep existing search-bait `data-search` attribute on the **summary `<tr>`** so the search filter still works.
  2. The matched-transactions `<table>` from today's `<details>` block — but flat, no `<details>`/`<summary>`. Header reads: "Matched transactions ({{.Count}}{{if .PinnedCount}}, {{.PinnedCount}} pinned{{end}})".
- All inner mutation hooks, hx-include, urls, and class names on inner elements stay identical.

### Search interaction

- Today's `applyUnifiedFilter()` JS toggles `display` on `.major-expense-item-row` (the per-expense `<form>`). With the new layout, the summary `<tr class="major-expense-item-row">` keeps that class and the same `data-search` attribute. The expansion `<tr>` is hidden by default — search does NOT need to force-open it; nested matched-txn rows still get their `style.display` toggled but are simply invisible until the user clicks the chevron. This matches today's behavior for the right-card exceptions panel (closed buckets stay closed under search).
- Exception: when the search **does** match a nested matched-txn, force-open the row (set the chevron `aria-expanded="true"` and `tbody` `data-open="true"` for that row). This preserves today's "force open `<details>` containing matched txn" behavior.

### Persisted open-state across HTMX swaps

- Today's `htmx:beforeSwap` snapshots `<details id>` open state via the selector `#major-expenses-results details[id], #major-expenses-list-card details[id]`. The new layout no longer uses row-level `<details>`, so the JS gains a parallel snapshot/restore of `data-open` on `tbody[data-expense-id]`:
  - Before swap: collect the set of `data-expense-id` values from `tbody[data-open="true"]`.
  - After swap: re-apply `data-open="true"` and update `aria-expanded` on the matching chevron buttons.
- The add panel's `<details id="major-expenses-add-panel">` uses the existing details-snapshot path, so its open state survives swaps automatically.

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

That's the only handler change. No new endpoints, no shape changes.

## Affected Files

- `web/templates/pages/major-expenses.html` — template restructure: header row, add panel, table, expanded row, JS for chevron toggle and add-icon toggle, snapshot/restore for `data-open`.
- `internal/handlers/majorexpenses/handlers.go` — add `TotalDeclared` to context.
- `internal/templates/render_major_expenses_test.go` — update existing render assertions for new structure; add assertions for total + chevron + collapsed-by-default expand row.
- `internal/handlers/majorexpenses/handlers_test.go` — add a test that the `TotalDeclared` context key equals the sum of `Summaries[].Total`.

## Testing

- Existing handler tests continue to pass without semantic changes (mutation responses still use the same templates and OOB swaps).
- New unit test: `TotalDeclared` matches `sum(Summaries[].Total)` for a fixture set with mixed match counts.
- Updated render test: rendered HTML contains the total, the `[+]` icon button, and a `<tr class="major-expense-item-row">` with a chevron button per declared expense; expanded row exists in markup but its `<tbody>` has `data-open="false"` initially.
- Manual verification (per CLAUDE.md UI guidance): with `make dev` running, confirm
  1. Default state — table compact, total visible, add panel hidden.
  2. Click `[+]` — add form expands; submit creates expense; panel collapses on success.
  3. Click chevron — single row expands with edit form + matched-txn table flat below.
  4. Edit a field — htmx PUT fires after 500ms and the right card re-renders, the row stays open.
  5. Search filters rows by name/keywords/notes; matching matched-txns force-open their row.
  6. Date-range change — list re-renders with rows preserved as collapsed (no row was open, none restored).
  7. Open one row → change date range → row stays open after swap.

## Risks & Trade-offs

- **`<table>` with toggleable detail rows requires JS** (chevron `aria-expanded` + `tbody[data-open]`). Today's per-item `<details>` is JS-free for the row toggle itself, but JS is already mandatory for search and HTMX persistence — net cognitive cost is small.
- **Loss of native disclosure semantics** — the chevron button + `aria-expanded` is the standard accessible pattern; documented in WAI-ARIA Authoring Practices for disclosure widgets.
- **Search-doesn't-force-open is a behavior change** for the row itself (today the form is always visible). Forcing matched-txn matches to open the row preserves the only case where search needed disclosure-state changes.
- **Add panel collapses on success** is a small behavior shift from today's "form remains visible after add". Acceptable: the icon makes re-opening cheap, and the user typically adds one expense at a time.

## Out-of-Scope Improvements (deliberately deferred)

- Sorting columns (by name/total/count) — table layout enables it but no current ask.
- Inline rename without expanding — keep edit-on-expand simple.
- Bulk-delete — not requested.
