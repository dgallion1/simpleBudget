# Major Expenses — Unified Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the right-card-only exception search on the Major Expenses page with a single page-level search bar that filters both the Major Expenses list (incl. each item's matched transactions) and the Exceptions panel.

**Architecture:** All filtering is client-side JS against `data-search` attributes rendered into the markup. Templates gain `data-search` on each Major Expense `<form>` and on each matched-txn `<tr>`; the existing per-card exception input is removed and replaced with a unified input in the page header card. JS replaces `applyExceptionFilter` with `applyUnifiedFilter` that walks both cards.

**Tech Stack:** Go html/template, HTMX, plain ES5 JS, Tailwind. Tests in Go (`internal/templates/render_major_expenses_test.go`).

**Spec:** `docs/superpowers/specs/2026-05-01-major-expenses-search-design.md`

---

## File Map

- **Modify** `web/templates/pages/major-expenses.html` — single template file, contains all template defs and the JS; all changes happen here.
- **Modify** `internal/templates/render_major_expenses_test.go` — update the assertions that reference the old search input id; add new assertions for the new id, the page-level placement, and `data-search` on left-card rows.

No new files. No handler/Go changes.

---

## Task 1: Update template tests for the new search input id

**Files:**
- Modify: `internal/templates/render_major_expenses_test.go:184` (assert new id) and `:187-198` (keep existing exception-row data-search assertions intact)

The `TestRenderMajorExpenses_WithEntriesAndExceptions` test currently asserts the OLD input id exists. We flip the assertion to the NEW id, and add assertions for left-card `data-search` wiring. Doing this **first** drives the template change via TDD.

- [ ] **Step 1: Edit the existing assertion (around line 184)** — replace the old id check, add new ones

Find this block in `internal/templates/render_major_expenses_test.go`:

```go
	// Search input + per-row data-search
	if !strings.Contains(html, `id="major-expenses-exception-search"`) {
		t.Errorf("expected exception search input")
	}
	if !strings.Contains(html, `class="major-expenses-exception-row`) {
		t.Errorf("expected rows to carry the exception-row class targeted by the filter script")
	}
```

Replace with:

```go
	// Unified search input (was per-card exception search; now page-level).
	if !strings.Contains(html, `id="major-expenses-search"`) {
		t.Errorf("expected unified search input id=major-expenses-search")
	}
	if strings.Contains(html, `id="major-expenses-exception-search"`) {
		t.Errorf("old per-card exception search input must be removed once the unified bar lands")
	}
	if !strings.Contains(html, `class="major-expenses-exception-row`) {
		t.Errorf("expected rows to carry the exception-row class targeted by the filter script")
	}
	// Each Major Expense item exposes a data-search built from name +
	// keywords + notes so the unified filter can match the item itself.
	if !strings.Contains(html, `data-search="Rent landlord`) {
		t.Errorf("expected expense item data-search to include name and keywords, got html=%s", html)
	}
	// Matched-txn rows inside an item carry their own data-search so the
	// unified filter can locate the transaction inside the item.
	if !strings.Contains(html, `data-search="Landlord LLC $1700.00 `) {
		t.Errorf("expected matched-txn row data-search to include label + amount + date")
	}
	// Each item form carries the row class so JS can select it.
	if !strings.Contains(html, `class="major-expense-item-row`) {
		t.Errorf("expected expense-item form to carry major-expense-item-row class")
	}
	// Each matched-txn <tr> carries the row class so JS can select it.
	if !strings.Contains(html, `class="major-expense-matched-row`) {
		t.Errorf("expected matched-txn row to carry major-expense-matched-row class")
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/templates/ -run TestRenderMajorExpenses_WithEntriesAndExceptions -v`
Expected: FAIL — `id="major-expenses-search"` not present, `data-search="Rent landlord"` not present, etc.

- [ ] **Step 3: Commit the failing test**

```bash
git add internal/templates/render_major_expenses_test.go
git commit -m "test(major-expenses): assert unified search input + left-card data-search"
```

---

## Task 2: Add `data-search` and row classes to left-card templates

**Files:**
- Modify: `web/templates/pages/major-expenses.html` — `major-expense-item` template (~line 237) and the matched-txn `<tr>` inside it (~line 297)

This makes the failing assertions from Task 1 about left-card markup pass. We're not adding the input yet — that's Task 3.

- [ ] **Step 1: Edit the `<form>` opening tag in `major-expense-item`**

Find (around line 238):

```html
<form id="major-expense-item-{{.Expense.ID}}" hx-put="/major-expenses/{{.Expense.ID}}" hx-target="#major-expenses-results"
    hx-swap="innerHTML" hx-trigger="change delay:500ms"
    class="p-3 bg-gray-50 dark:bg-gray-700 rounded text-sm scroll-mt-4 transition-shadow">
```

Replace with:

```html
<form id="major-expense-item-{{.Expense.ID}}" hx-put="/major-expenses/{{.Expense.ID}}" hx-target="#major-expenses-results"
    hx-swap="innerHTML" hx-trigger="change delay:500ms"
    data-search="{{.Expense.Name}}{{range .Expense.Keywords}} {{.}}{{end}}{{if .Expense.Notes}} {{.Expense.Notes}}{{end}}"
    class="major-expense-item-row p-3 bg-gray-50 dark:bg-gray-700 rounded text-sm scroll-mt-4 transition-shadow">
```

- [ ] **Step 2: Edit the matched-txn `<tr>` inside the same template**

Find (around line 297):

```html
                {{range .Transactions}}
                <tr class="border-t border-gray-200 dark:border-gray-600">
                    <td class="py-0.5 dark:text-gray-300">{{.Date.Format "2006-01-02"}}</td>
                    <td class="py-0.5 dark:text-gray-200">
                        {{if and $pinned (index $pinned .Hash)}}<span class="text-amber-600 dark:text-amber-400" title="Pinned manually">📌</span> {{end}}{{.Label}}
                    </td>
```

Replace the `<tr>` opening line with:

```html
                {{range .Transactions}}
                <tr class="major-expense-matched-row border-t border-gray-200 dark:border-gray-600"
                    data-search="{{.Label}} ${{printf "%.2f" .AbsAmount}} {{.Date.Format "2006-01-02"}}">
                    <td class="py-0.5 dark:text-gray-300">{{.Date.Format "2006-01-02"}}</td>
                    <td class="py-0.5 dark:text-gray-200">
                        {{if and $pinned (index $pinned .Hash)}}<span class="text-amber-600 dark:text-amber-400" title="Pinned manually">📌</span> {{end}}{{.Label}}
                    </td>
```

- [ ] **Step 3: Run the test — expect it to still fail (unified-input id not yet present)**

Run: `go test ./internal/templates/ -run TestRenderMajorExpenses_WithEntriesAndExceptions -v`
Expected: still FAIL but only on `id="major-expenses-search"` — the four left-card assertions should now pass. Confirm the failure message lists ONLY the unified-search-input assertion.

- [ ] **Step 4: Commit**

```bash
git add web/templates/pages/major-expenses.html
git commit -m "feat(major-expenses): expose data-search on items and matched-txn rows"
```

---

## Task 3: Add the unified search input; remove the right-card exception input

**Files:**
- Modify: `web/templates/pages/major-expenses.html` — header card (~lines 2-9), exceptions panel (~lines 418-424)

- [ ] **Step 1: Add the input to the page header card**

Find (around line 2):

```html
<div class="space-y-4">
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
        <h1 class="text-2xl font-semibold text-gray-800 dark:text-gray-100">Major Expenses</h1>
        <p class="text-sm text-gray-600 dark:text-gray-400 mt-1">
            Tell the app about expenses you understand. We'll group transactions
            that match your keywords and call out anything that looks unusual.
        </p>
    </div>
```

Replace with:

```html
<div class="space-y-4">
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
        <h1 class="text-2xl font-semibold text-gray-800 dark:text-gray-100">Major Expenses</h1>
        <p class="text-sm text-gray-600 dark:text-gray-400 mt-1">
            Tell the app about expenses you understand. We'll group transactions
            that match your keywords and call out anything that looks unusual.
        </p>
        <div class="mt-3 relative">
            <input type="search" id="major-expenses-search"
                placeholder="Filter expenses, keywords, notes, transactions, amounts, dates…"
                class="w-full text-sm border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-md py-1.5 px-3 pr-32"
                autocomplete="off">
            <span id="major-expenses-search-status" class="hidden absolute right-2 top-1/2 -translate-y-1/2 text-xs text-gray-500 dark:text-gray-400" aria-live="polite"></span>
        </div>
    </div>
```

- [ ] **Step 2: Remove the per-card exception search input**

Find (around lines 418-424, inside `major-expenses-exceptions-panel`):

```html
<div class="mb-3 relative">
    <input type="search" id="major-expenses-exception-search"
        placeholder="Filter by description, name, amount (625), or date (2025-12)…"
        class="w-full text-sm border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-md py-1.5 px-3 pr-8"
        autocomplete="off">
    <span id="major-expenses-exception-search-status" class="hidden absolute right-2 top-1/2 -translate-y-1/2 text-xs text-gray-500 dark:text-gray-400" aria-live="polite"></span>
</div>
```

Delete that entire block.

- [ ] **Step 3: Run the test — expect PASS**

Run: `go test ./internal/templates/ -run TestRenderMajorExpenses_WithEntriesAndExceptions -v`
Expected: PASS

- [ ] **Step 4: Run the full template test suite**

Run: `go test ./internal/templates/ -v`
Expected: PASS — `TestRenderMajorExpenses_MultipleEntriesAllRender`, `TestRenderMajorExpenses_EmptyState`, `TestRenderMajorExpenses_WithEntriesAndExceptions`, `TestRenderMajorExpensesResults_IncludesOOBSwap` all green.

- [ ] **Step 5: Commit**

```bash
git add web/templates/pages/major-expenses.html
git commit -m "feat(major-expenses): unified page-level search bar replaces per-card filter"
```

---

## Task 4: Replace `applyExceptionFilter` with `applyUnifiedFilter`

**Files:**
- Modify: `web/templates/pages/major-expenses.html` — JS block at lines 21-209

The status-id change (now `major-expenses-search-status`) and the new input id (`major-expenses-search`) flow through here, plus we add the left-card pass.

- [ ] **Step 1: Replace the `applyExceptionFilter` function**

Find (around line 49):

```js
    function applyExceptionFilter(query) {
        const q = (query || '').toLowerCase().trim();
        const rows = document.querySelectorAll('tr.major-expenses-exception-row');
        let visible = 0;
        rows.forEach(function (tr) {
            const text = (tr.getAttribute('data-search') || '').toLowerCase();
            const match = q === '' || text.includes(q);
            tr.style.display = match ? '' : 'none';
            if (match) visible++;
        });
        const status = document.getElementById('major-expenses-exception-search-status');
        if (status) {
            if (q === '') {
                status.classList.add('hidden');
                status.textContent = '';
            } else {
                status.classList.remove('hidden');
                status.textContent = visible + ' of ' + rows.length;
            }
        }
        // Auto-open every <details> that contains a visible match so the
        // user doesn't have to expand each bucket manually.
        document.querySelectorAll('#major-expenses-results details').forEach(function (d) {
            if (q === '') return;
            const hasMatch = d.querySelector('tr.major-expenses-exception-row:not([style*="display: none"])');
            if (hasMatch) d.open = true;
        });
        syncBulkPinToolbar(visible, q);
    }
```

Replace with:

```js
    function applyUnifiedFilter(query) {
        const q = (query || '').toLowerCase().trim();

        // ---- LEFT CARD: Major Expense items + their matched transactions ----
        const items = document.querySelectorAll('.major-expense-item-row');
        let visibleExpenses = 0;
        items.forEach(function (item) {
            const itemHay = (item.getAttribute('data-search') || '').toLowerCase();
            const itemMatch = q === '' || itemHay.includes(q);

            // Per-row matched-txn filter inside this item.
            const txnRows = item.querySelectorAll('.major-expense-matched-row');
            let txnHadMatch = false;
            txnRows.forEach(function (tr) {
                const hay = (tr.getAttribute('data-search') || '').toLowerCase();
                const match = q === '' || hay.includes(q);
                tr.style.display = match ? '' : 'none';
                if (match && q !== '') txnHadMatch = true;
            });

            const show = q === '' || itemMatch || txnHadMatch;
            item.style.display = show ? '' : 'none';
            if (show) visibleExpenses++;

            // Force-open <details> when the user matched a contained txn.
            // We never force-close, so the server-rendered open-when-pinned
            // default still wins on empty queries.
            if (q !== '' && txnHadMatch) {
                const details = item.querySelector('details');
                if (details) details.open = true;
            }
        });

        // ---- RIGHT CARD: Exceptions ----
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

        // ---- Status badge ----
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

- [ ] **Step 2: Update the input listener to read the new id**

Find (around line 141):

```js
    // Search input — event delegation so it survives HTMX swaps.
    document.addEventListener('input', function (e) {
        if (e.target && e.target.id === 'major-expenses-exception-search') {
            applyExceptionFilter(e.target.value);
        }
    });
```

Replace with:

```js
    // Search input — event delegation so it survives HTMX swaps.
    document.addEventListener('input', function (e) {
        if (e.target && e.target.id === 'major-expenses-search') {
            applyUnifiedFilter(e.target.value);
        }
    });
```

- [ ] **Step 3: Update the `htmx:afterSwap` re-apply hook**

Find (around line 105):

```js
        // Reapply the active search after every HTMX swap. Without this,
        // a single-pin POST refreshes the panel and the user's filter
        // input still shows the search text but every row is visible.
        const input = document.getElementById('major-expenses-exception-search');
        if (input && input.value) applyExceptionFilter(input.value);
```

Replace with:

```js
        // Reapply the active search after every HTMX swap. Without this,
        // a single-pin POST refreshes the panel and the user's filter
        // input still shows the search text but every row is visible.
        const input = document.getElementById('major-expenses-search');
        if (input && input.value) applyUnifiedFilter(input.value);
```

- [ ] **Step 4: Update the bulk-pin target-change re-apply hook**

Find (around line 134):

```js
    // Re-enable the Apply button when the target dropdown changes.
    document.addEventListener('change', function (e) {
        if (e.target && e.target.id === 'major-expenses-bulk-pin-target') {
            const input = document.getElementById('major-expenses-exception-search');
            applyExceptionFilter(input ? input.value : '');
        }
    });
```

Replace with:

```js
    // Re-enable the Apply button when the target dropdown changes.
    document.addEventListener('change', function (e) {
        if (e.target && e.target.id === 'major-expenses-bulk-pin-target') {
            const input = document.getElementById('major-expenses-search');
            applyUnifiedFilter(input ? input.value : '');
        }
    });
```

- [ ] **Step 5: Persist the unified search input across HTMX swaps**

The existing `htmx:beforeSwap`/`afterSwap` handlers persist `<details>` open state but don't snapshot the search input. Since the input now lives at page level (outside the swap target), it's not destroyed by swaps — but the left-card list IS rebuilt on item-edit OOB swaps. The existing `afterSwap` re-apply hook (Step 3) handles this correctly. **No additional change needed** — confirm by reading the surrounding `htmx:afterSwap` body and verifying `applyUnifiedFilter` runs on every swap.

- [ ] **Step 6: Sanity build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 7: Run the full test suite**

Run: `go test ./...`
Expected: all green.

- [ ] **Step 8: Commit**

```bash
git add web/templates/pages/major-expenses.html
git commit -m "feat(major-expenses): unified filter walks both cards and matched txns"
```

---

## Task 5: Manual smoke test in the browser

**Files:** none (verification only)

Spec requires verifying the behavior end-to-end with a real dataset.

- [ ] **Step 1: Start the server**

Run: `go run ./cmd/server` (or whatever the project's dev start command is — check `Makefile` if unsure)

- [ ] **Step 2: Open the Major Expenses page**

Browse to `http://localhost:<port>/major-expenses` (use whatever port the server logs on startup).

- [ ] **Step 3: Test cases**

Verify each of the following with the unified search input:

| Query | Expected |
|-------|----------|
| (empty) | All Major Expense items visible, all matched-txn `<details>` honor server defaults, all exception rows visible. Status badge hidden. |
| Partial Major Expense name (e.g. "Rent") | Only that expense visible on the left; right card filters too if any exception matches. `<details>` NOT auto-opened (matched the metadata, not a txn). Status reads "1 expenses · 0 exceptions" or similar. |
| Description from a matched txn (e.g. "Landlord") | Left: parent expense visible with `<details>` open and only matching txn rows shown. Right: any exceptions whose description contains "Landlord" visible. |
| Amount (e.g. "1700") | Both cards filter to rows containing $1700. `<details>` open where matches exist. |
| String matching nothing | Both cards empty, status reads "0 expenses · 0 exceptions". |

- [ ] **Step 4: HTMX swap regression**

With a non-empty query active (e.g. "Rent"), edit an expense's notes and tab away to fire the inline `hx-put`. After the OOB swap, verify:
- search input still shows "Rent"
- left card still filtered to "Rent" only
- right card still filtered
- `<details>` for any matching expense still respects the open-state persistence

- [ ] **Step 5: Bulk-pin toolbar regression**

With a query that produces visible exceptions (e.g. an exception description), verify the bulk-pin toolbar appears with the correct count. Pick an expense from the dropdown and click Apply; verify the swap completes and the search re-applies.

- [ ] **Step 6: Stop the server, no commit needed**

This is verification only.

---

## Self-Review

**Spec coverage:**
- ✅ Single unified search input at page level → Task 3 Step 1
- ✅ Remove existing right-card exception search → Task 3 Step 2
- ✅ `data-search` on Major Expense items → Task 2 Step 1
- ✅ `data-search` on matched-txn rows → Task 2 Step 2
- ✅ Filter both cards via JS, hide non-matching items, force-open `<details>` only when contained-txn matches → Task 4 Step 1
- ✅ Empty-query reverts both cards → covered by `q === ''` branches in `applyUnifiedFilter`
- ✅ Status badge "N expenses · M exceptions" → Task 4 Step 1 status block
- ✅ Persist across HTMX swaps → Task 4 Steps 3 & 5
- ✅ Bulk-pin toolbar still keys on visible-exception count + non-empty query → Task 4 Step 1 (`syncBulkPinToolbar(visibleExceptions, q)`)
- ✅ Template snapshot tests → Task 1 + Task 3 Step 4
- ✅ Manual smoke covering all five spec scenarios → Task 5

**Placeholder scan:** None — every step has either exact code or an exact command + expected outcome.

**Type/name consistency:**
- Input id: `major-expenses-search` (used in template, JS input listener, JS afterSwap hook, JS bulk-pin change hook, test) ✅
- Status id: `major-expenses-search-status` (used in template + JS) ✅
- Item class: `major-expense-item-row` (used in template + JS + test) ✅
- Matched-txn row class: `major-expense-matched-row` (used in template + JS + test) ✅
- Function name: `applyUnifiedFilter` (defined once, called from input listener, afterSwap, bulk-pin-target change) ✅
