# Major-Expense Checkbox Pinning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add multi-select checkboxes to every exception table on `/major-expenses` so a user can cherry-pick a heterogeneous subset of exceptions across buckets and pin them all to one expense in two clicks.

**Architecture:** Pure UI-layer change. The existing `POST /major-expenses/pins/bulk` handler already accepts `expense_id` plus repeated `hashes` form fields and is reused without modification. Selection lives in DOM `:checked` state only — no JS-side store. The existing bulk toolbar (currently filter-driven) is repurposed with two activation modes: "Pin N selected" (checked rows) takes priority over "Pin all M matching" (filter-driven). Shift-click range select is added per-bucket.

**Tech Stack:** Go html/template (`web/templates/pages/major-expenses.html`), HTMX, vanilla JS (no framework), TailwindCSS classes already in use, Go test with `httptest`/`fs.Sub` for render assertions.

**Spec:** `docs/superpowers/specs/2026-05-03-major-expense-checkbox-pinning-design.md`

---

## File Structure

| File | Responsibility | Action |
|------|---------------|--------|
| `web/templates/pages/major-expenses.html` | All template + JS for the page; checkbox column markup, count chip, Clear button, bulk toolbar mode-switch, shift-click | **Modify** |
| `internal/templates/render_major_expenses_test.go` | Render-level assertions for new HTML | **Modify** (add tests) |
| `docs/superpowers/specs/2026-05-03-major-expense-checkbox-pinning-design.md` | Reference design (already committed) | unchanged |

No new files. No backend handler edits.

---

### Task 1: Render test — checkbox column on the comprehensive Unmatched table

**Files:**
- Modify: `internal/templates/render_major_expenses_test.go` — add new test function

This is the TDD-first step. Write a failing render test that asserts the checkbox column markup exists on the comprehensive `AllUnmatched` render path. This locks in the contract before any template edit.

- [ ] **Step 1: Add the failing test**

Append the following test function to `internal/templates/render_major_expenses_test.go` (after the existing `TestRenderMajorExpenses_UnmatchedBucketShowsAllRowsWithDimming` function):

```go
// TestRenderMajorExpenses_ExceptionsHaveCheckboxColumn verifies that every
// exception render path emits a leading checkbox column with the expected
// class, data-hash binding, and propagation-stop wiring so the click-to-
// prefill behavior on the rest of the row is not triggered.
func TestRenderMajorExpenses_ExceptionsHaveCheckboxColumn(t *testing.T) {
	now := time.Now()
	html := renderMajorExpensesContent(t, map[string]any{
		"Title":     "Major Expenses",
		"ActiveTab": "major-expenses",
		"Expenses": []models.MajorExpense{
			{ID: "rent", Name: "Rent"},
		},
		"ExpenseOptions": []struct {
			ID    string
			Label string
		}{{ID: "rent", Label: "Rent"}},
		"Summaries": []struct{}{},
		"Match": struct {
			Exceptions models.ExceptionsReport
		}{
			Exceptions: models.ExceptionsReport{
				Anomalous: []models.ExceptionAnomalousAmount{
					{
						MajorExpenseID:   "rent",
						MajorExpenseName: "Rent",
						Transaction:      models.Transaction{Date: now, Amount: -3500, Description: "My Landlord LLC", Hash: "h-anom"},
						ExpectedMin:      1500,
						ExpectedMax:      2000,
					},
				},
				NewMerchants: []models.ExceptionNewMerchant{
					{Description: "brand new store", FirstSeen: now, Transaction: models.Transaction{Date: now, Amount: -75, Description: "Brand New Store", Hash: "h-new"}},
				},
				Threshold:     100,
				NewWindowDays: 30,
			},
		},
		"AllUnmatched": []models.Transaction{
			{Date: now, Amount: -250, Description: "Big Unknown Charge", Hash: "h-big"},
		},
		"Threshold":     100.0,
		"WindowDays":    30,
		"TotalDeclared": 0.0,
	})

	// Each of the three exception buckets must emit a header checkbox.
	for _, bucket := range []string{
		"major-expenses-pin-check-header-unmatched",
		"major-expenses-pin-check-header-anomalous",
		"major-expenses-pin-check-header-new-merchants",
	} {
		if !strings.Contains(html, `id="`+bucket+`"`) {
			t.Errorf("expected header checkbox id %q on its bucket, got html=%s", bucket, html)
		}
	}

	// Each row carries its own checkbox bound to the transaction hash.
	for _, hash := range []string{"h-big", "h-anom", "h-new"} {
		want := `class="major-expenses-pin-check"` // class on the input
		if !strings.Contains(html, want) {
			t.Fatalf("expected row checkbox class %q in output", want)
		}
		dataAttr := `data-hash="` + hash + `"`
		if !strings.Contains(html, dataAttr) {
			t.Errorf("expected data-hash=%q on the corresponding row, got html=%s", hash, html)
		}
	}

	// Row checkboxes must stop click propagation so row-click prefill
	// is not triggered by interacting with the checkbox.
	if !strings.Contains(html, `class="major-expenses-pin-check-cell"`) {
		t.Errorf("expected wrapping td.major-expenses-pin-check-cell to scope propagation-stop CSS, got html=%s", html)
	}

	// data-bucket on each row checkbox identifies which bucket the row
	// belongs to so shift-click can scope ranges per-bucket.
	for _, bucket := range []string{`data-bucket="unmatched"`, `data-bucket="anomalous"`, `data-bucket="new-merchants"`} {
		if !strings.Contains(html, bucket) {
			t.Errorf("expected row checkbox to expose %s, got html=%s", bucket, html)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/templates/ -run TestRenderMajorExpenses_ExceptionsHaveCheckboxColumn -v`

Expected: FAIL with messages like `expected header checkbox id "major-expenses-pin-check-header-unmatched"…`. The template doesn't render any of this markup yet.

- [ ] **Step 3: Commit the failing test**

```bash
git add internal/templates/render_major_expenses_test.go
git commit -m "test(major-expenses): assert checkbox column on every exception bucket

Failing test that locks in the markup contract for the new multi-select
pinning UI: each bucket has a header checkbox, every row has a per-row
checkbox bound to data-hash, and the wrapping cell carries a class so
click propagation can be scoped via CSS/JS.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

> Note: the pre-commit hook runs `go test ./...` and will fail because this test fails. Use `git commit --no-verify` for this single commit ONLY if `make check` blocks; better: include the implementation in the same commit as a TDD pair. **Decision for this plan:** combine the failing-test commit with the implementation commit (Task 2) to keep the pre-commit hook green. Skip the standalone commit here — proceed straight to Task 2 without committing.

Replace Step 3 with:

- [ ] **Step 3 (revised): Do not commit yet — implementation in Task 2 will close the test and they commit together.**

---

### Task 2: Implement checkbox columns in `web/templates/pages/major-expenses.html`

**Files:**
- Modify: `web/templates/pages/major-expenses.html` — four exception render paths

The template has FOUR places that need a checkbox column added:

1. **Legacy `UnknownLarge` table** (around lines 1072–1102): used by old fixtures that don't set `AllUnmatched`. `data-bucket="unmatched"`.
2. **Comprehensive `AllUnmatched` table** (around lines 1108–1147): used by current production data. `data-bucket="unmatched"`.
3. **Anomalous table** (around lines 1163–1205): `data-bucket="anomalous"`.
4. **New merchants table** (around lines 1219–1265): `data-bucket="new-merchants"`. The `$matchedID` branch already shows a checkmark badge instead of a pin picker — its row STILL gets a checkbox so the user can bulk-pin matched-but-not-pinned rows if they want; the picker column itself is replaced by the badge as today.

Each table gets:
- A new leading `<th>` in the `<thead>` containing a header checkbox.
- A new leading `<td class="major-expenses-pin-check-cell">` in each row containing the row checkbox.

**Header `<th>` template (apply to all four tables):**

For Unmatched (legacy + comprehensive), use:

```html
<th class="px-2 py-1 w-6">
    <input type="checkbox"
           id="major-expenses-pin-check-header-unmatched"
           class="major-expenses-pin-check-header"
           data-bucket="unmatched"
           aria-label="Select visible unmatched exceptions"
           onclick="event.stopPropagation()">
</th>
```

For Anomalous:

```html
<th class="px-2 py-1 w-6">
    <input type="checkbox"
           id="major-expenses-pin-check-header-anomalous"
           class="major-expenses-pin-check-header"
           data-bucket="anomalous"
           aria-label="Select visible anomalous exceptions"
           onclick="event.stopPropagation()">
</th>
```

For New merchants:

```html
<th class="px-2 py-1 w-6">
    <input type="checkbox"
           id="major-expenses-pin-check-header-new-merchants"
           class="major-expenses-pin-check-header"
           data-bucket="new-merchants"
           aria-label="Select visible new-merchant exceptions"
           onclick="event.stopPropagation()">
</th>
```

**Row `<td>` template (apply to all four `<tbody>` row blocks):**

The hash and label vary per render path. Use these expressions:

| Render path | `$hash` | `$label` |
|---|---|---|
| Legacy UnknownLarge | `.Transaction.Hash` | `.Transaction.Label` |
| Comprehensive AllUnmatched | `.Hash` | `.Label` |
| Anomalous | `.Transaction.Hash` | `.Transaction.Label` |
| New merchants | `.Transaction.Hash` | `.Transaction.Label` |

The row cell — substitute the bucket id and the hash/label expressions:

```html
<td class="major-expenses-pin-check-cell px-2 py-1 w-6" onclick="event.stopPropagation()">
    <input type="checkbox"
           class="major-expenses-pin-check"
           data-hash="{{<HASH_EXPR>}}"
           data-bucket="<BUCKET_ID>"
           aria-label="Select {{<LABEL_EXPR>}} for bulk pinning"
           onclick="event.stopPropagation()">
</td>
```

- [ ] **Step 1: Modify the legacy `UnknownLarge` table (the `$unmatchedFromExceptions` branch)**

Find this block (around line 1072):

```html
        <table class="w-full text-xs mt-1">
            <thead class="text-gray-500 dark:text-gray-400">
                <tr>
                    <th class="text-left px-2 py-1">Date</th>
                    <th class="text-left px-2 py-1">Description</th>
                    <th class="text-right px-2 py-1">Amount</th>
                    {{if .Expenses}}<th class="text-left px-2 py-1">Pin to</th>{{end}}
                </tr>
            </thead>
            <tbody>
                {{range .Match.Exceptions.UnknownLarge}}
                {{$label := .Transaction.Label}}{{$rawText := or .Transaction.DisplayName .Transaction.Description}}
                {{$pinnedID := ""}}{{if $.PinMap}}{{$pinnedID = index $.PinMap .Transaction.Hash}}{{end}}
                <tr class="major-expenses-exception-row border-t border-gray-100 dark:border-gray-700 cursor-pointer hover:bg-gray-100 dark:hover:bg-gray-700"
                    data-hash="{{.Transaction.Hash}}"
                    ...>
                    <td class="px-2 py-1 dark:text-gray-300">{{.Transaction.Date.Format "2006-01-02"}}</td>
```

Replace the `<thead>` with the header checkbox prepended:

```html
        <table class="w-full text-xs mt-1">
            <thead class="text-gray-500 dark:text-gray-400">
                <tr>
                    <th class="px-2 py-1 w-6">
                        <input type="checkbox"
                               id="major-expenses-pin-check-header-unmatched"
                               class="major-expenses-pin-check-header"
                               data-bucket="unmatched"
                               aria-label="Select visible unmatched exceptions"
                               onclick="event.stopPropagation()">
                    </th>
                    <th class="text-left px-2 py-1">Date</th>
                    <th class="text-left px-2 py-1">Description</th>
                    <th class="text-right px-2 py-1">Amount</th>
                    {{if .Expenses}}<th class="text-left px-2 py-1">Pin to</th>{{end}}
                </tr>
            </thead>
```

Inside that table's `<tbody> {{range ...}}`, prepend the row checkbox cell as the first `<td>` inside the `<tr>` (before the existing date `<td>`):

```html
                    <td class="major-expenses-pin-check-cell px-2 py-1 w-6" onclick="event.stopPropagation()">
                        <input type="checkbox"
                               class="major-expenses-pin-check"
                               data-hash="{{.Transaction.Hash}}"
                               data-bucket="unmatched"
                               aria-label="Select {{$label}} for bulk pinning"
                               onclick="event.stopPropagation()">
                    </td>
```

- [ ] **Step 2: Modify the comprehensive `AllUnmatched` table (the `$unmatchedRows` branch)**

Find the matching block (around line 1108) — the table with `class="w-full text-xs mt-1 major-expenses-sortable"`.

Prepend a header checkbox `<th>` (NOT sortable; same id and bucket as Step 1 — there is only one Unmatched bucket in either render mode, but only one of the two tables renders at a time, so the duplicate id is safe):

```html
                    <th class="px-2 py-1 w-6">
                        <input type="checkbox"
                               id="major-expenses-pin-check-header-unmatched"
                               class="major-expenses-pin-check-header"
                               data-bucket="unmatched"
                               aria-label="Select visible unmatched exceptions"
                               onclick="event.stopPropagation()">
                    </th>
```

Insert it before the existing Date `<th>`. Then prepend the row checkbox `<td>` inside the `{{range $unmatchedRows}}` block, before the existing Date `<td>`:

```html
                    <td class="major-expenses-pin-check-cell px-2 py-1 w-6" onclick="event.stopPropagation()">
                        <input type="checkbox"
                               class="major-expenses-pin-check"
                               data-hash="{{.Hash}}"
                               data-bucket="unmatched"
                               aria-label="Select {{$label}} for bulk pinning"
                               onclick="event.stopPropagation()">
                    </td>
```

- [ ] **Step 3: Modify the Anomalous table (around line 1163)**

Prepend in the `<thead>` `<tr>` before the existing Date `<th>`:

```html
                <th class="px-2 py-1 w-6">
                    <input type="checkbox"
                           id="major-expenses-pin-check-header-anomalous"
                           class="major-expenses-pin-check-header"
                           data-bucket="anomalous"
                           aria-label="Select visible anomalous exceptions"
                           onclick="event.stopPropagation()">
                </th>
```

Inside `{{range .Match.Exceptions.Anomalous}}`, prepend before the existing Date `<td>`:

```html
                <td class="major-expenses-pin-check-cell px-2 py-1 w-6" onclick="event.stopPropagation()">
                    <input type="checkbox"
                           class="major-expenses-pin-check"
                           data-hash="{{.Transaction.Hash}}"
                           data-bucket="anomalous"
                           aria-label="Select {{$desc}} for bulk pinning"
                           onclick="event.stopPropagation()">
                </td>
```

(`$desc` is already declared by the existing `{{$desc := .Transaction.Label}}` line above the row.)

- [ ] **Step 4: Modify the New merchants table (around line 1219)**

Prepend in the `<thead>` `<tr>` before the existing First-seen `<th>`:

```html
                <th class="px-2 py-1 w-6">
                    <input type="checkbox"
                           id="major-expenses-pin-check-header-new-merchants"
                           class="major-expenses-pin-check-header"
                           data-bucket="new-merchants"
                           aria-label="Select visible new-merchant exceptions"
                           onclick="event.stopPropagation()">
                </th>
```

Inside `{{range .Match.Exceptions.NewMerchants}}`, prepend before the existing First-seen `<td>`. Use `{{$label}}` which is already declared above the row:

```html
                <td class="major-expenses-pin-check-cell px-2 py-1 w-6" onclick="event.stopPropagation()">
                    <input type="checkbox"
                           class="major-expenses-pin-check"
                           data-hash="{{.Transaction.Hash}}"
                           data-bucket="new-merchants"
                           aria-label="Select {{$label}} for bulk pinning"
                           onclick="event.stopPropagation()">
                </td>
```

- [ ] **Step 5: Run the render test from Task 1**

Run: `go test ./internal/templates/ -run TestRenderMajorExpenses_ExceptionsHaveCheckboxColumn -v`

Expected: PASS.

- [ ] **Step 6: Run the full template test suite to catch regressions**

Run: `go test ./internal/templates/ -v`

Expected: every test passes. The existing `TestRenderMajorExpenses_*` tests should still match — the new column is additive, no existing strings are removed.

If `TestRenderMajorExpenses_WithEntriesAndExceptions` fails on a "stopPropagation" count assertion (line 484: `got < 3`), inspect the failure: the new row checkbox cells add three more `event.stopPropagation()` occurrences per fixture, which only INCREASES the count and the assertion is `>=`, so it should still pass. If it doesn't pass, do not weaken the assertion — fix the rendered HTML.

- [ ] **Step 7: Commit**

```bash
git add internal/templates/render_major_expenses_test.go web/templates/pages/major-expenses.html
git commit -m "feat(major-expenses): add checkbox column to every exception bucket

Adds a leading checkbox column to all four exception render paths
(legacy UnknownLarge, comprehensive AllUnmatched, Anomalous, New
merchants). Each row checkbox is bound to its transaction hash and
its bucket id so a follow-up JS pass can drive shift-click ranges
and bulk-pin selection. event.stopPropagation is stamped on the
input, the wrapping <td>, and the bucket header inputs so existing
row-click prefill, sort, and details-toggle behavior is unaffected.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

The pre-commit hook should pass (`make check` runs `go test ./...`).

---

### Task 3: Render test — count chip + Clear button server-side markup

**Files:**
- Modify: `internal/templates/render_major_expenses_test.go`

The count chip and the Clear button live in the rendered HTML so they survive HTMX swaps without JS having to re-create them. Lock in their structure with a render test.

- [ ] **Step 1: Add the failing test**

Append to `internal/templates/render_major_expenses_test.go`:

```go
// TestRenderMajorExpenses_CountChipAndClearButton verifies the
// server-side markup for the bulk-pin selection chip in the panel
// header and the Clear button inside the bulk-pin toolbar. Both
// must render hidden by default; JS reveals them when N > 0.
func TestRenderMajorExpenses_CountChipAndClearButton(t *testing.T) {
	now := time.Now()
	html := renderMajorExpensesContent(t, map[string]any{
		"Title":     "Major Expenses",
		"ActiveTab": "major-expenses",
		"Expenses": []models.MajorExpense{
			{ID: "rent", Name: "Rent"},
		},
		"ExpenseOptions": []struct {
			ID    string
			Label string
		}{{ID: "rent", Label: "Rent"}},
		"Summaries": []struct{}{},
		"Match": struct {
			Exceptions models.ExceptionsReport
		}{
			Exceptions: models.ExceptionsReport{
				Threshold:     100,
				NewWindowDays: 30,
			},
		},
		"AllUnmatched": []models.Transaction{
			{Date: now, Amount: -250, Description: "Big Unknown Charge", Hash: "h-big"},
		},
		"Threshold":     100.0,
		"WindowDays":    30,
		"TotalDeclared": 0.0,
	})

	// Count chip is rendered with the "hidden" class so it is invisible
	// at SSR time. JS toggles visibility on selection change.
	if !strings.Contains(html, `id="major-expenses-pin-count-chip"`) {
		t.Errorf("expected count chip element id in panel header, got: %s", html)
	}
	if !strings.Contains(html, `class="major-expenses-pin-count-chip hidden`) {
		t.Errorf("expected count chip rendered with hidden class, got: %s", html)
	}

	// Clear button lives inside the existing bulk-pin toolbar and is
	// always rendered with a hidden class — JS reveals it in checked
	// mode only.
	if !strings.Contains(html, `id="major-expenses-bulk-pin-clear"`) {
		t.Errorf("expected Clear button id inside bulk-pin toolbar, got: %s", html)
	}
	if !strings.Contains(html, `class="major-expenses-bulk-pin-clear`) {
		t.Errorf("expected Clear button class for JS hooks, got: %s", html)
	}
}

// TestRenderMajorExpenses_CheckboxesRenderWithoutExpenses verifies
// the spec's edge case: row + header checkboxes still render even
// when .ExpenseOptions is empty, but the bulk toolbar is absent.
func TestRenderMajorExpenses_CheckboxesRenderWithoutExpenses(t *testing.T) {
	now := time.Now()
	html := renderMajorExpensesContent(t, map[string]any{
		"Title":     "Major Expenses",
		"ActiveTab": "major-expenses",
		"Expenses":  []models.MajorExpense{},
		// No ExpenseOptions key -> nil/empty.
		"Summaries": []struct{}{},
		"Match": struct {
			Exceptions models.ExceptionsReport
		}{Exceptions: models.ExceptionsReport{Threshold: 100, NewWindowDays: 30}},
		"AllUnmatched": []models.Transaction{
			{Date: now, Amount: -250, Description: "Big Unknown Charge", Hash: "h-big"},
		},
		"Threshold":     100.0,
		"WindowDays":    30,
		"TotalDeclared": 0.0,
	})

	if !strings.Contains(html, `class="major-expenses-pin-check"`) {
		t.Errorf("expected row checkbox even without expenses, got: %s", html)
	}
	if !strings.Contains(html, `id="major-expenses-pin-check-header-unmatched"`) {
		t.Errorf("expected header checkbox even without expenses, got: %s", html)
	}
	if strings.Contains(html, `id="major-expenses-bulk-pin"`) {
		t.Errorf("bulk-pin toolbar must NOT render when ExpenseOptions is empty, got: %s", html)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/templates/ -run "TestRenderMajorExpenses_CountChipAndClearButton|TestRenderMajorExpenses_CheckboxesRenderWithoutExpenses" -v`

Expected: both FAIL — the count chip element doesn't exist and the Clear button isn't in the toolbar yet. (`CheckboxesRenderWithoutExpenses` should already pass for the row/header checks because Task 2 added them unconditionally; it only fails on the bulk-pin assertion if the toolbar IS rendered without expenses, which it shouldn't be — re-read Task 2 to confirm the toolbar is gated by `{{if .ExpenseOptions}}` already.)

- [ ] **Step 3: Do not commit yet — implementation in Task 4 closes the test.**

---

### Task 4: Implement count chip + Clear button in the template

**Files:**
- Modify: `web/templates/pages/major-expenses.html`

- [ ] **Step 1: Add the count chip to the panel header**

Find this block in `major-expenses-exceptions-panel` (around line 1015):

```html
<div class="flex items-center justify-between mb-2">
    <h2 class="text-lg font-medium text-gray-800 dark:text-gray-100">Exceptions</h2>
    {{$total := 0}}
    ...
    <span class="text-xs text-gray-500 dark:text-gray-400">{{$total}} flagged</span>
</div>
```

Replace the trailing `<span>` line with this expanded version that adds the chip:

```html
    <span class="text-xs text-gray-500 dark:text-gray-400">
        {{$total}} flagged
        <span id="major-expenses-pin-count-chip"
              class="major-expenses-pin-count-chip hidden ml-1 text-amber-700 dark:text-amber-300"
              aria-live="polite">
            · <span id="major-expenses-pin-count-chip-num">0</span> selected
        </span>
    </span>
```

- [ ] **Step 2: Add the Clear button inside the existing bulk-pin toolbar**

Find this block (around line 1043):

```html
    <button type="button" id="major-expenses-bulk-pin-apply"
        class="px-2 py-0.5 bg-amber-600 hover:bg-amber-700 text-white rounded text-xs disabled:opacity-50 disabled:cursor-not-allowed"
        disabled>Apply</button>
</div>
```

Replace with the same Apply button followed by the new Clear button (still inside the toolbar `<div>`):

```html
    <button type="button" id="major-expenses-bulk-pin-apply"
        class="px-2 py-0.5 bg-amber-600 hover:bg-amber-700 text-white rounded text-xs disabled:opacity-50 disabled:cursor-not-allowed"
        disabled>Apply</button>
    <button type="button" id="major-expenses-bulk-pin-clear"
        class="major-expenses-bulk-pin-clear hidden px-2 py-0.5 bg-gray-200 hover:bg-gray-300 dark:bg-gray-600 dark:hover:bg-gray-500 text-gray-700 dark:text-gray-100 rounded text-xs"
        aria-label="Clear all selected exception checkboxes">Clear</button>
</div>
```

- [ ] **Step 3: Run the new render tests**

Run: `go test ./internal/templates/ -run "TestRenderMajorExpenses_CountChipAndClearButton|TestRenderMajorExpenses_CheckboxesRenderWithoutExpenses" -v`

Expected: PASS.

- [ ] **Step 4: Run the full template test suite**

Run: `go test ./internal/templates/ -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/templates/render_major_expenses_test.go web/templates/pages/major-expenses.html
git commit -m "feat(major-expenses): add SSR markup for count chip and Clear button

The count chip in the panel header (\"N flagged · M selected\") and
the Clear button inside the bulk-pin toolbar are server-rendered
with a hidden class so they survive HTMX swaps without re-injection.
JS in subsequent commits flips visibility on selection change.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: JS — selection helpers and toolbar mode-switch

**Files:**
- Modify: `web/templates/pages/major-expenses.html` (script IIFE around line 280–950)

This task wires the data layer of the selection model — pure helpers that read DOM state and the rewritten `syncBulkPinToolbar` that branches on checked-vs-filter mode.

- [ ] **Step 1: Add the per-bucket anchor map and helpers**

Find this block in the IIFE near the top (around line 282):

```javascript
(function () {
    function visibleExceptionHashes() {
        return Array.from(
            document.querySelectorAll('tr.major-expenses-exception-row:not([style*="display: none"])')
        ).map(function (tr) {
            return tr.getAttribute('data-hash') || '';
        }).filter(Boolean);
    }
```

Immediately after `visibleExceptionHashes`, add these helper functions (still inside the IIFE):

```javascript
    // ---- Bulk-selection helpers (DOM is the source of truth) ----

    // Anchor for shift-click range select, keyed by bucket id. Reset
    // on every htmx:afterSwap so post-mutation state is clean.
    let lastCheckedByBucket = Object.create(null);

    function rowCheckboxes(bucketID) {
        const sel = bucketID
            ? 'input.major-expenses-pin-check[data-bucket="' + bucketID + '"]'
            : 'input.major-expenses-pin-check';
        return Array.from(document.querySelectorAll(sel));
    }

    function visibleRowCheckboxesInBucket(bucketID) {
        return rowCheckboxes(bucketID).filter(function (cb) {
            const tr = cb.closest('tr.major-expenses-exception-row');
            return tr && tr.style.display !== 'none';
        });
    }

    function checkedRowCheckboxes() {
        return rowCheckboxes().filter(function (cb) { return cb.checked; });
    }

    function countChecked() { return checkedRowCheckboxes().length; }

    function collectCheckedHashes() {
        return checkedRowCheckboxes().map(function (cb) {
            return cb.getAttribute('data-hash') || '';
        }).filter(Boolean);
    }

    function refreshBucketHeader(bucketID) {
        const header = document.getElementById('major-expenses-pin-check-header-' + bucketID);
        if (!header) return;
        const visible = visibleRowCheckboxesInBucket(bucketID);
        if (!visible.length) {
            header.checked = false;
            header.indeterminate = false;
            return;
        }
        const checkedCount = visible.filter(function (cb) { return cb.checked; }).length;
        if (checkedCount === 0) {
            header.checked = false;
            header.indeterminate = false;
        } else if (checkedCount === visible.length) {
            header.checked = true;
            header.indeterminate = false;
        } else {
            header.checked = false;
            header.indeterminate = true;
        }
    }

    function refreshAllBucketHeaders() {
        ['unmatched', 'anomalous', 'new-merchants'].forEach(refreshBucketHeader);
    }

    function refreshCountChip() {
        const chip = document.getElementById('major-expenses-pin-count-chip');
        const num = document.getElementById('major-expenses-pin-count-chip-num');
        if (!chip || !num) return;
        const n = countChecked();
        num.textContent = String(n);
        chip.classList.toggle('hidden', n === 0);
    }
```

- [ ] **Step 2: Rewrite `syncBulkPinToolbar` to branch on checked-vs-filter mode**

Find the existing function (around line 292):

```javascript
    function syncBulkPinToolbar(visibleCount, query) {
        const bar = document.getElementById('major-expenses-bulk-pin');
        if (!bar) return;
        const counter = document.getElementById('major-expenses-bulk-pin-count');
        const apply = document.getElementById('major-expenses-bulk-pin-apply');
        const target = document.getElementById('major-expenses-bulk-pin-target');
        const active = query.trim() !== '' && visibleCount > 0;
        bar.classList.toggle('hidden', !active);
        bar.classList.toggle('flex', active);
        if (counter) counter.textContent = String(visibleCount);
        if (apply) apply.disabled = !active || !target || !target.value;
    }
```

Replace it with this version that handles both modes and the Clear button:

```javascript
    function syncBulkPinToolbar(visibleCount, query) {
        const bar = document.getElementById('major-expenses-bulk-pin');
        if (!bar) return;
        const counter = document.getElementById('major-expenses-bulk-pin-count');
        const apply = document.getElementById('major-expenses-bulk-pin-apply');
        const target = document.getElementById('major-expenses-bulk-pin-target');
        const clear = document.getElementById('major-expenses-bulk-pin-clear');
        const labelLead = bar.querySelector('span.major-expenses-bulk-pin-label-lead');

        const checkedCount = countChecked();
        const filterActive = (query || '').trim() !== '' && visibleCount > 0;

        // Mode priority: checked > filter > hidden.
        let mode = 'hidden';
        let displayCount = 0;
        if (checkedCount > 0) {
            mode = 'checked';
            displayCount = checkedCount;
        } else if (filterActive) {
            mode = 'filter';
            displayCount = visibleCount;
        }

        const visibleBar = mode !== 'hidden';
        bar.classList.toggle('hidden', !visibleBar);
        bar.classList.toggle('flex', visibleBar);

        if (counter) counter.textContent = String(displayCount);

        // Update the lead label between "Pin all" (filter) and
        // "Pin" (checked). The trailing "matching →" / "selected →"
        // is text after the counter; we swap it via labelLead's
        // dataset markers.
        if (labelLead) {
            labelLead.textContent = mode === 'checked' ? 'Pin ' : 'Pin all ';
        }
        const labelTrail = bar.querySelector('span.major-expenses-bulk-pin-label-trail');
        if (labelTrail) {
            labelTrail.textContent = mode === 'checked' ? ' selected →' : ' matching →';
        }

        if (apply) apply.disabled = !visibleBar || !target || !target.value;
        if (clear) clear.classList.toggle('hidden', mode !== 'checked');
    }
```

> Important: this function references `span.major-expenses-bulk-pin-label-lead` and `span.major-expenses-bulk-pin-label-trail`. The current toolbar markup lumps the whole label into one `<span>`. We split it in the next step so the JS can target both halves.

- [ ] **Step 3: Update the toolbar label markup to expose lead and trail spans**

Find this in `major-expenses-exceptions-panel` (around line 1034):

```html
    <span class="text-amber-700 dark:text-amber-300 font-medium">
        Pin all <span id="major-expenses-bulk-pin-count">0</span> matching →
    </span>
```

Replace with:

```html
    <span class="text-amber-700 dark:text-amber-300 font-medium">
        <span class="major-expenses-bulk-pin-label-lead">Pin all </span><span id="major-expenses-bulk-pin-count">0</span><span class="major-expenses-bulk-pin-label-trail"> matching →</span>
    </span>
```

- [ ] **Step 4: Run the template tests to make sure nothing regressed**

Run: `go test ./internal/templates/ -v`

Expected: PASS. The lead/trail span split is additive — existing tests don't assert on the inner text structure of the toolbar.

- [ ] **Step 5: Commit**

```bash
git add web/templates/pages/major-expenses.html
git commit -m "feat(major-expenses): selection helpers and dual-mode bulk toolbar

Adds the DOM-only selection helpers (rowCheckboxes, countChecked,
collectCheckedHashes), per-bucket header sync with indeterminate
state, and the count chip refresher. syncBulkPinToolbar now branches
on a checked > filter > hidden priority and updates lead/trail
label spans so the toolbar reads \"Pin N selected →\" or
\"Pin all M matching →\" depending on mode.

No event wiring yet — the helpers compile but do nothing; subsequent
commits attach them to checkbox change, header click, shift-click,
Clear, and Apply.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: JS — row + header checkbox change handlers

**Files:**
- Modify: `web/templates/pages/major-expenses.html` (script IIFE)

- [ ] **Step 1: Add the row + header change handlers via event delegation**

Find this existing block (around line 443):

```javascript
    document.addEventListener('change', function (e) {
        if (e.target && e.target.id === 'major-expenses-bulk-pin-target') {
            const input = document.getElementById('major-expenses-search');
            applyUnifiedFilter(input ? input.value : '');
        }
    });
```

Immediately AFTER it (still inside the IIFE), add the new delegated change handler:

```javascript
    // Row + header checkbox change. Delegated so HTMX swaps don't
    // need rebinding. Note: shift-click range is handled in the
    // 'click' phase (a separate listener) because by the time
    // 'change' fires the browser has already toggled the box.
    document.addEventListener('change', function (e) {
        const t = e.target;
        if (!t) return;

        // Header checkbox: toggle every visible row in this bucket
        // to the header's new state.
        if (t.classList && t.classList.contains('major-expenses-pin-check-header')) {
            const bucket = t.getAttribute('data-bucket') || '';
            const want = !!t.checked;
            visibleRowCheckboxesInBucket(bucket).forEach(function (cb) {
                cb.checked = want;
            });
            // Header was just clicked; keep its state authoritative
            // (refreshBucketHeader could reset it to indeterminate
            // if any visible row diverges, but right now they all
            // match `want`).
            refreshBucketHeader(bucket);
            refreshCountChip();
            const input = document.getElementById('major-expenses-search');
            applyUnifiedFilter(input ? input.value : '');
            return;
        }

        // Row checkbox: update bucket header and count chip. Anchor
        // is recorded on click (next task) — change does not touch it.
        if (t.classList && t.classList.contains('major-expenses-pin-check')) {
            const bucket = t.getAttribute('data-bucket') || '';
            refreshBucketHeader(bucket);
            refreshCountChip();
            const input = document.getElementById('major-expenses-search');
            applyUnifiedFilter(input ? input.value : '');
            return;
        }
    });
```

- [ ] **Step 2: Hook header refresh into `applyUnifiedFilter`**

Find this block at the end of `applyUnifiedFilter` (around line 371):

```javascript
        syncBulkPinToolbar(visibleExceptions, q);
    }
```

Replace with:

```javascript
        refreshAllBucketHeaders();
        syncBulkPinToolbar(visibleExceptions, q);
    }
```

This guarantees that when a filter changes which rows are visible, every bucket header re-evaluates its checked/indeterminate state against the new visible row set. Without this, hiding all the unchecked rows in a bucket would leave the header in an indeterminate state forever.

- [ ] **Step 3: Manual verification — start the server and exercise the flow**

Build and run the dev server:

```bash
go build ./cmd/server && ./server &
SERVER_PID=$!
sleep 1
```

Open `http://localhost:8080/major-expenses` in a browser. Confirm in the browser console (no errors) and visually:

- Each exception row has a leftmost checkbox.
- Clicking a row's checkbox does not open the add panel (propagation is stopped).
- The count chip "· N selected" appears in the panel header when N ≥ 1, hides when N = 0.
- The bulk-pin toolbar shows "Pin N selected →" with the Clear button visible when ≥ 1 box is checked.
- The bulk-pin toolbar still shows "Pin all M matching →" when nothing is checked but the search is active.
- Clicking the bucket header checkbox toggles every visible row in that bucket.

Stop the server: `kill $SERVER_PID`.

Note: this is a developer smoke check; the formal Playwright run is Task 10.

- [ ] **Step 4: Commit**

```bash
git add web/templates/pages/major-expenses.html
git commit -m "feat(major-expenses): wire row and header checkbox change events

Row checkbox change refreshes the bucket header (indeterminate
when partial, checked when all visible are checked) and updates the
count chip. Header checkbox toggle flips every visible row in its
bucket to the header's new state. applyUnifiedFilter now refreshes
all bucket headers after a filter change so visibility-driven state
stays correct.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: JS — shift-click range select within a bucket

**Files:**
- Modify: `web/templates/pages/major-expenses.html` (script IIFE)

- [ ] **Step 1: Add the shift-click handler**

`change` events fire after the browser has already toggled the box, and `click` events fire before — but shift-click range needs to react to the user's intent at click time and apply the SAME new value (the just-clicked target's state) to the range. We attach a `click` listener on row checkboxes:

Add this immediately after the change handler block from Task 6 (still inside the IIFE):

```javascript
    // Click on a row checkbox. Two responsibilities:
    //   1. Record the bucket anchor (always, on every click).
    //   2. If shift is held AND there is a previous anchor in this
    //      bucket AND it is still visible, toggle the visible-range
    //      between anchor and target to the target's NEW value.
    //
    // Click fires before change; the target's `checked` reflects the
    // pre-click state during the listener. We compute the new state
    // as !target.checked.
    document.addEventListener('click', function (e) {
        const t = e.target;
        if (!t || !t.classList || !t.classList.contains('major-expenses-pin-check')) return;
        const bucket = t.getAttribute('data-bucket') || '';
        const targetHash = t.getAttribute('data-hash') || '';

        const anchorHash = lastCheckedByBucket[bucket] || '';
        const wantApplyRange = e.shiftKey && anchorHash && anchorHash !== targetHash;

        if (wantApplyRange) {
            const visible = visibleRowCheckboxesInBucket(bucket);
            const idxAnchor = visible.findIndex(function (cb) { return cb.getAttribute('data-hash') === anchorHash; });
            const idxTarget = visible.findIndex(function (cb) { return cb.getAttribute('data-hash') === targetHash; });
            if (idxAnchor === -1) {
                // Anchor no longer visible; treat as a fresh click.
                lastCheckedByBucket[bucket] = targetHash;
                return;
            }
            // Browser will toggle target after this listener returns;
            // its NEW state is !current. Apply that to every visible
            // checkbox in [min, max].
            const newState = !t.checked;
            const lo = Math.min(idxAnchor, idxTarget);
            const hi = Math.max(idxAnchor, idxTarget);
            for (let i = lo; i <= hi; i++) {
                visible[i].checked = newState;
            }
            // Refresh state synchronously; the change event for `t`
            // will still fire and trigger another refresh, which is
            // a harmless no-op.
            refreshBucketHeader(bucket);
            refreshCountChip();
            const input = document.getElementById('major-expenses-search');
            applyUnifiedFilter(input ? input.value : '');
        }

        // Always record the anchor on click (shift or not).
        lastCheckedByBucket[bucket] = targetHash;
    });
```

- [ ] **Step 2: Manual verification**

Start the dev server (same way as Task 6, Step 3). In the browser:

- Click row 1 in Unmatched (now checked, anchor set).
- Shift-click row 5 in Unmatched → rows 1–5 all checked.
- Shift-click row 3 (already in range) → rows 1–3 should now be UNCHECKED (the target was checked, so newState = !checked = false applies to the whole range from anchor=1 to target=3).
- Shift-click in Anomalous → no effect on Unmatched, anchor for Anomalous now set.

Stop the server.

- [ ] **Step 3: Commit**

```bash
git add web/templates/pages/major-expenses.html
git commit -m "feat(major-expenses): shift-click range select within a bucket

Anchor is recorded on every row-checkbox click, scoped per bucket
via lastCheckedByBucket. Shift-clicking computes the visible-DOM-
order range between anchor and target, then applies the target's
NEW state (!current) to the entire range. Sorted tables work
naturally because the range is computed from live DOM order.
Cross-bucket shift-clicks re-anchor the new bucket without affecting
the old one.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: JS — Clear button + Apply branching + HTMX swap reset

**Files:**
- Modify: `web/templates/pages/major-expenses.html` (script IIFE)

- [ ] **Step 1: Wire the Clear button**

Add this delegated click listener (place it after the shift-click handler from Task 7, still inside the IIFE):

```javascript
    // Clear button: uncheck every row + every header, reset the
    // anchor map, and re-sync everything visible.
    document.addEventListener('click', function (e) {
        if (!e.target || e.target.id !== 'major-expenses-bulk-pin-clear') return;
        e.preventDefault();
        rowCheckboxes().forEach(function (cb) { cb.checked = false; });
        document.querySelectorAll('input.major-expenses-pin-check-header').forEach(function (h) {
            h.checked = false;
            h.indeterminate = false;
        });
        lastCheckedByBucket = Object.create(null);
        refreshCountChip();
        const input = document.getElementById('major-expenses-search');
        applyUnifiedFilter(input ? input.value : '');
    });
```

- [ ] **Step 2: Branch the existing Apply handler between checked-vs-visible hashes**

Find the existing Apply handler (around line 414):

```javascript
    document.addEventListener('click', function (e) {
        if (!e.target || e.target.id !== 'major-expenses-bulk-pin-apply') return;
        e.preventDefault();
        const target = document.getElementById('major-expenses-bulk-pin-target');
        if (!target || !target.value) return;
        const hashes = visibleExceptionHashes();
        if (!hashes.length) return;
        ...
```

Change `const hashes = visibleExceptionHashes();` to use checked hashes when any are checked, falling back to visible (filter-driven) otherwise:

```javascript
    document.addEventListener('click', function (e) {
        if (!e.target || e.target.id !== 'major-expenses-bulk-pin-apply') return;
        e.preventDefault();
        const target = document.getElementById('major-expenses-bulk-pin-target');
        if (!target || !target.value) return;
        const hashes = countChecked() > 0 ? collectCheckedHashes() : visibleExceptionHashes();
        if (!hashes.length) return;
```

(Leave the rest of the Apply handler — the FormData build, htmx.ajax POST — unchanged.)

- [ ] **Step 3: Reset the anchor map on every HTMX swap**

Find the existing `htmx:afterSwap` handler (around line 395):

```javascript
    document.body.addEventListener('htmx:afterSwap', function () {
        if (savedOpenDetails) {
            ...
        }
        if (savedOpenRows) {
            ...
        }
        const input = document.getElementById('major-expenses-search');
        if (input && input.value) applyUnifiedFilter(input.value);
    });
```

Add a single line clearing the anchor map at the top of the handler. Replace the opening `function () {` body to start with the reset:

```javascript
    document.body.addEventListener('htmx:afterSwap', function () {
        // Incoming markup has fresh, unchecked checkboxes; the prior
        // anchors point at hashes that are no longer in the DOM (or
        // moved buckets). Reset before any sync runs.
        lastCheckedByBucket = Object.create(null);

        if (savedOpenDetails) {
            ...
```

(Keep the rest of the handler exactly as it was.)

Also, add a count-chip + header refresh at the END of that handler so the post-swap UI matches the empty selection state:

```javascript
        const input = document.getElementById('major-expenses-search');
        if (input && input.value) applyUnifiedFilter(input.value);
        refreshAllBucketHeaders();
        refreshCountChip();
    });
```

- [ ] **Step 4: Manual verification**

Start the server. In the browser:

- Check 3 rows (e.g. 2 Unmatched + 1 New merchants). Toolbar shows "Pin 3 selected →" with Clear button visible.
- Click Clear → all unchecked, count chip hides, toolbar collapses (or falls back to "Pin all M matching" if a search filter is active).
- Re-check 2 rows. Choose an expense in the toolbar dropdown. Click Apply.
- After the swap: those 2 rows are gone (they're now matched), no rows are checked, count chip is hidden, the bulk toolbar reflects the empty post-swap state.

Stop the server.

- [ ] **Step 5: Run the full check pipeline**

Run: `make check`

Expected: vet, staticcheck, govulncheck, and `go test ./...` all pass.

- [ ] **Step 6: Commit**

```bash
git add web/templates/pages/major-expenses.html
git commit -m "feat(major-expenses): Clear button + Apply branching + swap reset

Clear button unchecks every row, resets every bucket header, and
clears the anchor map. Apply now collects checked hashes when any
are checked, falling back to visible-row hashes (the legacy filter-
driven path). htmx:afterSwap resets the per-bucket anchor map
before any sync runs and refreshes count chip + bucket headers
afterward so post-mutation UI is clean.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Render test — bulk toolbar lead/trail spans

**Files:**
- Modify: `internal/templates/render_major_expenses_test.go`

A small lock-in test for the toolbar label split — the JS depends on those exact span class names.

- [ ] **Step 1: Add the test**

Append:

```go
// TestRenderMajorExpenses_BulkToolbarLeadTrailSpans verifies the
// toolbar label is split into lead/trail spans so the JS mode-
// switch can rewrite each independently. Without this split JS
// would have to regex the inner text.
func TestRenderMajorExpenses_BulkToolbarLeadTrailSpans(t *testing.T) {
	now := time.Now()
	html := renderMajorExpensesContent(t, map[string]any{
		"Title":     "Major Expenses",
		"ActiveTab": "major-expenses",
		"Expenses": []models.MajorExpense{
			{ID: "rent", Name: "Rent"},
		},
		"ExpenseOptions": []struct {
			ID    string
			Label string
		}{{ID: "rent", Label: "Rent"}},
		"Summaries": []struct{}{},
		"Match": struct {
			Exceptions models.ExceptionsReport
		}{Exceptions: models.ExceptionsReport{Threshold: 100, NewWindowDays: 30}},
		"AllUnmatched": []models.Transaction{
			{Date: now, Amount: -250, Description: "Big Unknown Charge", Hash: "h-big"},
		},
		"Threshold":     100.0,
		"WindowDays":    30,
		"TotalDeclared": 0.0,
	})
	if !strings.Contains(html, `class="major-expenses-bulk-pin-label-lead"`) {
		t.Errorf("expected lead label span class for JS mode-switch, got: %s", html)
	}
	if !strings.Contains(html, `class="major-expenses-bulk-pin-label-trail"`) {
		t.Errorf("expected trail label span class for JS mode-switch, got: %s", html)
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./internal/templates/ -run TestRenderMajorExpenses_BulkToolbarLeadTrailSpans -v`

Expected: PASS (Task 5 already split the spans).

- [ ] **Step 3: Commit**

```bash
git add internal/templates/render_major_expenses_test.go
git commit -m "test(major-expenses): assert bulk-toolbar label spans

The mode-switch JS depends on .major-expenses-bulk-pin-label-lead
and -trail being addressable. This locks the markup in so a future
tidy-up doesn't rejoin them and silently break the toolbar text.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Manual Playwright smoke test + screenshot

**Files:**
- (no code changes; verification only)

Spec section 4 of the test plan calls for a Playwright smoke run. Run it via the `mcp__plugin_playwright_playwright__*` tool surface (or `agent-browser` if the Playwright MCP is unavailable in the executing environment).

- [ ] **Step 1: Start the dev server**

```bash
go build ./cmd/server && ./server > /tmp/major-expenses-smoke.log 2>&1 &
SERVER_PID=$!
sleep 1
```

(The server logs to `/tmp/major-expenses-smoke.log` so any 5xx from a misbehaving template is visible.)

- [ ] **Step 2: Walk through the smoke matrix**

Using the available browser-automation tools, navigate to `http://localhost:8080/major-expenses` and verify:

1. The Exceptions panel renders three buckets (Unmatched, Anomalous, New merchants) — at least one of which has ≥ 2 rows. If the active dataset has none, switch to a date window where exceptions exist (use the date filter at the top of the page).
2. Check 2 rows in Unmatched and 1 in New merchants → header chip reads "· 3 selected" and the toolbar reads "Pin 3 selected →" with a visible "Clear" button.
3. Choose an expense in the toolbar dropdown. The Apply button enables.
4. Click Apply → page swaps; the 3 rows disappear from Exceptions; count chip is hidden; bulk toolbar collapses (or falls back to filter mode if a search is active).
5. Type a query (e.g., "amazon") in the unified search box. With nothing checked, the toolbar shows "Pin all M matching →" (legacy filter path preserved).
6. With the same filter, check one row → toolbar switches to "Pin 1 selected →". The pretend post would only carry that one hash.
7. Clear the filter, then check row 1 in a bucket with ≥ 5 rows. Shift-click row 5 in the same bucket → rows 1–5 are all checked.
8. Sort the Unmatched bucket by Amount (click the Amount column header). Then in the new sorted order, click row 1 and shift-click row 4 → rows 1–4 of the SORTED order are checked.
9. Click Clear → all unchecked, header chip hides, toolbar collapses.

- [ ] **Step 3: Capture a screenshot of the "Pin N selected" state**

Save it to `docs/superpowers/specs/2026-05-03-major-expense-checkbox-pinning-smoke.png` so the PR has visual evidence.

- [ ] **Step 4: Stop the server**

```bash
kill $SERVER_PID
```

- [ ] **Step 5: Commit the screenshot**

```bash
git add docs/superpowers/specs/2026-05-03-major-expense-checkbox-pinning-smoke.png
git commit -m "docs(major-expenses): smoke screenshot for checkbox pinning UI

Visual record of the multi-select bulk-pin flow with N selected,
toolbar in checked mode, count chip in panel header, and Clear
button visible. Exercised every smoke scenario in the spec.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

### Task 11: Final verification + branch wrap-up

**Files:**
- (no code changes; full pipeline + branch close-out)

- [ ] **Step 1: Run the complete check pipeline**

Run: `make check`

Expected: vet, staticcheck, govulncheck, and `go test ./...` all pass.

- [ ] **Step 2: Run the race-detector test (opt-in but recommended before a UI feature lands)**

Run: `make race`

Expected: PASS. (This is opt-in per project convention but cheap insurance for handler concurrency.)

- [ ] **Step 3: Confirm the spec checkboxes are all closed**

Skim `docs/superpowers/specs/2026-05-03-major-expense-checkbox-pinning-design.md` once more and confirm every numbered Design section has been implemented:

- [ ] §1 Checkbox column on every exception table — **Tasks 1–2**
- [ ] §2 Unified bulk toolbar with mode-switch — **Tasks 4–5**
- [ ] §3 Selection state model in DOM only — **Task 5**
- [ ] §4 Shift-click range — **Task 7**
- [ ] §5 Count chip — **Tasks 4 + 5**
- [ ] §6 Per-row dropdown unchanged — verified via existing tests still passing in **Task 2 + Task 4**

Edge cases from the spec:

- [ ] Empty exception list — table simply doesn't render (already covered by the `{{if eq $total 0}}` guard).
- [ ] Bulk endpoint failure — existing handler behavior, unchanged.
- [ ] Click on checkbox `<td>` — `onclick="event.stopPropagation()"` on cell + input.
- [ ] Header checkbox click — input is in `<thead>`, isolated from `<summary>` toggle.
- [ ] Hidden checked rows survive filter — `collectCheckedHashes` reads ALL `:checked` regardless of `style.display`.
- [ ] No expenses (`.ExpenseOptions` empty) — toolbar absent, checkboxes still render, verified by `TestRenderMajorExpenses_CheckboxesRenderWithoutExpenses`.
- [ ] Sort — checkbox state lives on the row, anchor by hash, range by live DOM order.
- [ ] HTMX swap — `lastCheckedByBucket = Object.create(null)` at top of `htmx:afterSwap`.
- [ ] Missing/duplicate hashes — `data-hash` checked for non-empty before collection.

If any item above isn't true, fix it before opening the PR.

- [ ] **Step 4: Push the branch**

```bash
git push -u origin dev
```

- [ ] **Step 5: Open a PR (optional, depending on team workflow)**

If the branch is ready for review:

```bash
gh pr create --title "feat(major-expenses): checkbox-driven bulk pin for exceptions" --body "$(cat <<'EOF'
## Summary
- Adds a leftmost checkbox column to every exception bucket on /major-expenses, replacing the per-row open-each-dropdown chore for multi-pin workflows.
- The existing bulk-pin toolbar gains a checked-rows mode (priority over the legacy filter-driven mode) plus a Clear button. The "N selected" chip in the panel header tracks total selection across buckets.
- Shift-click within a bucket selects a contiguous visible range. Per-row "Pin to…" dropdown and "+ Create new from this…" are untouched.
- Reuses existing `POST /major-expenses/pins/bulk`. No backend changes.

## Test plan
- [x] `make check` passes (vet, staticcheck, govulncheck, `go test ./...`)
- [x] `make race` passes
- [x] Render tests cover: checkbox column on each bucket, count chip element, Clear button, lead/trail label spans, no-expenses edge case
- [x] Manual Playwright smoke matrix from the spec — screenshot attached to docs/

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-Review

Re-checked the plan against the spec:

| Spec section | Plan task |
|---|---|
| §1 Checkbox column on every exception table | Tasks 1–2 (test + implementation across all 4 render paths) |
| §2 Unified bulk toolbar with checked > filter priority | Task 5 (rewrite of `syncBulkPinToolbar` + lead/trail span split) and Task 8 (Apply branching) |
| §3 DOM-only selection state | Task 5 (helpers) — no JS-side store, helpers read `:checked` |
| §4 Shift-click range select | Task 7 |
| §5 Count chip in panel header | Task 4 (markup) + Task 5 (`refreshCountChip`) |
| §6 Per-row dropdown unchanged | Touch is purely additive — verified by existing tests |
| Edge cases | Task 11 Step 3 enumerates and verifies each one |
| Render tests | Tasks 1, 3, 9 |
| JS smoke test | Task 10 |
| Backend unchanged | No tasks edit handler files |

**No placeholders.** Every step has either exact code, an exact command, or a clear binary success criterion.

**Type / name consistency check:**

| Identifier | First defined | Reused in |
|---|---|---|
| `lastCheckedByBucket` | Task 5 Step 1 | Tasks 7, 8 |
| `rowCheckboxes`, `visibleRowCheckboxesInBucket`, `checkedRowCheckboxes`, `countChecked`, `collectCheckedHashes` | Task 5 Step 1 | Tasks 6, 7, 8 |
| `refreshBucketHeader`, `refreshAllBucketHeaders`, `refreshCountChip` | Task 5 Step 1 | Tasks 6, 8 |
| `major-expenses-pin-check`, `major-expenses-pin-check-cell`, `major-expenses-pin-check-header`, `major-expenses-pin-count-chip(-num)`, `major-expenses-bulk-pin-clear`, `major-expenses-bulk-pin-label-lead/-trail` | Tasks 2, 4, 5 | tested in Tasks 1, 3, 9; consumed by JS in Tasks 5, 6, 7, 8 |
| `data-bucket` values: `unmatched`, `anomalous`, `new-merchants` | Task 2 | matched by header IDs in Tasks 5, 6 |

All consistent.

**Scope guard:** the plan adds zero handlers, zero new endpoints, and zero new template files. It is one focused UI feature implementable in roughly half a working day end-to-end.
