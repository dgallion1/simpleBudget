# Major Expenses — Unified Search & Filter

**Date:** 2026-05-01
**Branch:** data-storage-improvements
**Status:** Design approved

## Problem

The Major Expenses page has two side-by-side cards:

- **Left card** "Your Major Expenses" — declared expenses, each with an
  expandable matched-transactions table.
- **Right card** "Exceptions" — three buckets (unmatched-over-threshold,
  anomalous, new merchants). Has a client-side search input that filters
  these rows by description / expense-name / amount / date.

The existing search only covers the right card. Users can't:

1. Search for a transaction and discover **which Major Expense
   contains it** (left-card matched txns are not searchable today).
2. Search Major Expenses themselves by name, keywords, notes, or by
   the descriptions of the matched transactions inside them.

## Solution

Replace the right-card-only search with a **single unified search input
at the page level** (above the two-card grid) that filters both cards
client-side using the data already on the page.

## UI

```
┌─────────────────────────────────────────────────────┐
│ Major Expenses                                      │
│ [🔍 Filter expenses, txns, amounts, dates…]  3·2    │  ← new
├──────────────────────────┬──────────────────────────┤
│ Your Major Expenses      │ Exceptions               │
│ (filtered)               │ (filtered)               │
└──────────────────────────┴──────────────────────────┘
```

- Search input lives in the page header card (same card the H1 is in),
  full-width below the description text.
- A small status badge (`"3 expenses · 2 exceptions"`) appears next to
  or below the input when a query is active. Hidden when query is empty.
- The existing exception-only search input inside the right card is
  removed. Its bulk-pin toolbar logic is unchanged — that toolbar keys
  off "visible exception count + non-empty query" and continues to
  function with the unified bar driving the same state.

## Search behavior

All filtering is **client-side** against `data-search` attributes
rendered into the markup by the templates. No server round-trips.

**Left card — for each Major Expense `<form>` item:**

1. Compute a haystack from the expense's name + each keyword + notes,
   joined by spaces (rendered into a `data-search` attribute on the
   `<form>`).
2. Each matched-transaction `<tr>` in the item's `<details>` table also
   carries a `data-search` attribute (label + amount + date).
3. Match logic for an item:
   - **No query** → show item normally; let the server-rendered
     `<details open>` rule (open when PinnedCount > 0) stand.
   - **Query matches the item's own haystack** → show item; do not
     force-open the `<details>` (the user matched on metadata, they
     don't necessarily want the txn list expanded).
   - **Query matches one or more contained txn rows** → show item,
     force-open the `<details>`, and hide non-matching `<tr>` rows so
     only matching txns are visible inside.
   - **No match anywhere** → hide the entire item via inline `display:none`.

**Right card — Exceptions:** identical logic to today's
`applyExceptionFilter`, just driven by the same unified query. Buckets
auto-open when they contain a visible row.

**Empty query (input cleared):** Both cards revert fully:

- Every Major Expense item visible.
- Every matched-txn `<tr>` visible.
- `<details>` `open` state goes back to whatever the server rendered
  (we never set `open=false`, only `open=true`, so server defaults
  survive).
- Status badge hidden.

## Implementation

### Template changes (`web/templates/pages/major-expenses.html`)

1. **Add unified search input** to the top header card (around line 9,
   inside the existing `<div class="bg-white ... rounded-lg shadow p-4">`
   that holds the H1 and intro paragraph).
2. **Remove the right-card search input** (the
   `<input id="major-expenses-exception-search">` block at lines
   418-424) — its job is replaced.
3. **Add `data-search` to each Major Expense `<form>`** in the
   `major-expense-item` template:
   ```html
   data-search="{{.Expense.Name}} {{range .Expense.Keywords}}{{.}} {{end}}{{.Expense.Notes}}"
   ```
4. **Add `data-search` to each matched-txn `<tr>`** in the matched-txns
   table inside `major-expense-item`:
   ```html
   data-search="{{.Label}} ${{printf "%.2f" .AbsAmount}} {{.Date.Format "2006-01-02"}}"
   ```
5. **Class names:** add `major-expense-item-row` to the `<form>` and
   `major-expense-matched-row` to the matched txn `<tr>` so the JS can
   select them deterministically.

### JS changes (same `<script>` block in major-expenses.html)

Replace `applyExceptionFilter(query)` with `applyUnifiedFilter(query)`:

```js
function applyUnifiedFilter(query) {
    const q = (query || '').toLowerCase().trim();

    // ---- LEFT CARD ----
    const items = document.querySelectorAll('.major-expense-item-row');
    let visibleExpenses = 0;
    items.forEach(function (item) {
        const itemHay = (item.getAttribute('data-search') || '').toLowerCase();
        const itemMatch = q === '' || itemHay.includes(q);

        // Per-row matched-txn filter
        const txnRows = item.querySelectorAll('.major-expense-matched-row');
        let txnVisible = 0;
        let txnHadMatch = false;
        txnRows.forEach(function (tr) {
            const hay = (tr.getAttribute('data-search') || '').toLowerCase();
            const match = q === '' || hay.includes(q);
            tr.style.display = match ? '' : 'none';
            if (match && q !== '') txnHadMatch = true;
            if (match) txnVisible++;
        });

        const show = q === '' || itemMatch || txnHadMatch;
        item.style.display = show ? '' : 'none';
        if (show) visibleExpenses++;

        // Force-open <details> when the match is on a contained txn
        // (not when match is on item metadata only).
        if (q !== '' && txnHadMatch) {
            const details = item.querySelector('details');
            if (details) details.open = true;
        }
    });

    // ---- RIGHT CARD (existing exception logic, lifted) ----
    const exRows = document.querySelectorAll('tr.major-expenses-exception-row');
    let visibleExceptions = 0;
    exRows.forEach(function (tr) {
        const text = (tr.getAttribute('data-search') || '').toLowerCase();
        const match = q === '' || text.includes(q);
        tr.style.display = match ? '' : 'none';
        if (match) visibleExceptions++;
    });
    document.querySelectorAll('#major-expenses-results details').forEach(function (d) {
        if (q === '') return;
        const hasMatch = d.querySelector('tr.major-expenses-exception-row:not([style*="display: none"])');
        if (hasMatch) d.open = true;
    });

    // ---- STATUS BADGE ----
    const status = document.getElementById('major-expenses-search-status');
    if (status) {
        if (q === '') {
            status.classList.add('hidden');
            status.textContent = '';
        } else {
            status.classList.remove('hidden');
            status.textContent = visibleExpenses + ' expenses · ' + visibleExceptions + ' exceptions';
        }
    }

    syncBulkPinToolbar(visibleExceptions, q);
}
```

- Update the input listener (event delegation) to listen for the new
  input id `major-expenses-search` instead of
  `major-expenses-exception-search`.
- Update `htmx:afterSwap` re-apply hook to read the new input id.
- Update `syncBulkPinToolbar` only if the input id reference inside it
  needs a touch (it currently doesn't — it's driven by parameters).

### What does NOT change

- Server-side handlers (no API changes).
- Pin / unpin / bulk-pin behavior, including the bulk-pin toolbar gate
  (still requires non-empty query + ≥1 visible exception).
- The `<details>` open-state persistence across HTMX swaps (already
  implemented; new search re-applies on `afterSwap` via the existing
  hook).
- The "click an exception row to pre-fill the add form" behavior.
- The "click an expense name in an anomalous row to jump to its item"
  behavior.

## Testing

1. **Template snapshot test** in
   `internal/templates/render_major_expenses_test.go`:
   - assert `data-search` attribute appears on matched-txn rows and on
     expense items
   - assert the unified search input id is present
   - assert the old `major-expenses-exception-search` id is gone
2. **Manual smoke** (chrome-mcp):
   - empty query → everything visible
   - query matching only an expense name → that expense visible, others
     hidden, `<details>` not auto-opened
   - query matching only a matched txn description → its parent expense
     visible with `<details>` open and only matching txn shown
   - query matching only an exception → left card empty, right card
     filtered
   - query matching all three at once → status reads "N expenses · M
     exceptions" with both cards filtered
3. **HTMX swap regression**: edit an expense via the inline form, verify
   the active query re-applies after the swap and bulk-pin toolbar
   remains visible.

## YAGNI / out of scope

- Server-side search endpoint — current dataset fits easily client-side.
- Persisted-across-reload search (URL query param) — the existing
  exception search isn't persisted either; matching its behavior keeps
  this change tight.
- Highlighting matched substrings inside cells — visible context is
  enough.
- Searching pinned-only sub-buckets separately — they're matched txns
  too, so they're included automatically.
