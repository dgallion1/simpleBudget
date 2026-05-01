# Major Expenses — Collapsible Table & Compact Add Affordance — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the always-expanded list of edit forms in the "Your Major Expenses" card with a compact collapsible table, an icon-toggled add panel, and a sum-of-totals header. Server, template, JS, and tests all change in lockstep.

**Architecture:** One handler-context addition (`TotalDeclared`). One template rewrite (`web/templates/pages/major-expenses.html`) that converts the list-of-forms into a `<table>` with one `<tbody data-expense-id="…" data-open="false">` per expense (summary `<tr>` + detail `<tr>` with the edit form + matched-txns table flat inside). Inline JS rewires chevron toggling, add-panel toggling, group-based search, jump-to-existing, exception prefill, and `data-open` persistence across HTMX swaps. Mutation routing and OOB swap pipeline stay identical.

**Tech Stack:** Go html/template, HTMX, Tailwind utility classes, vanilla JS (delegated event listeners on `document`).

**Spec:** `docs/superpowers/specs/2026-05-01-major-expenses-collapsible-table-design.md`

---

## Pre-flight

### Task 0: GitNexus impact analysis (read-only)

**Files:** none — diagnostic only.

- [ ] **Step 1: Run upstream impact for `buildPageData`**

```bash
# Run from a Claude session that has the GitNexus MCP available.
# In MCP-tool form: gitnexus_impact({target: "buildPageData", direction: "upstream"})
```

Expected: lists every handler that calls `buildPageData` (page render + HTMX wrapper render). Risk should be MEDIUM — only one new context key is added; no existing key is renamed or removed.

- [ ] **Step 2: Run upstream impact for affected templates**

```bash
# gitnexus_impact({target: "major-expenses-content", direction: "upstream"})
# gitnexus_impact({target: "major-expenses-list-card-content", direction: "upstream"})
# gitnexus_impact({target: "major-expenses-results", direction: "upstream"})
```

Expected: list of handlers whose responses include these blocks (page handler, mutation handlers, OOB swap handler). Confirms which test files must be updated.

- [ ] **Step 3: Halt and surface to user if risk is HIGH or CRITICAL**

If GitNexus reports HIGH/CRITICAL risk for any of these, stop and post the impact summary to the user before continuing. Otherwise proceed.

---

## Task 1: Add `TotalDeclared` to handler context (TDD)

**Files:**
- Modify: `internal/handlers/majorexpenses/handlers.go` — context map at end of `buildPageData`
- Test: `internal/handlers/majorexpenses/handlers_test.go` — new `TestBuildPageData_TotalDeclared`

- [ ] **Step 1: Write the failing test**

Append after the existing `TestBuildPageData_DateRangeFiltersTransactions` (line ~683):

```go
func TestBuildPageData_TotalDeclared(t *testing.T) {
    dl, cleanup := setupTestEnv(t)
    defer cleanup()

    // Seed two outflows that match two different expenses. Total must
    // equal the sum of their absolute amounts.
    csvDir := t.TempDir()
    csvPath := filepath.Join(csvDir, "test.csv")
    today := time.Now().Format("2006-01-02")
    csvContent := "Date,Description,Amount,Type,Category\n" +
        today + ",Anthropic Subscription,-108,Outflow,Software\n" +
        today + ",Verizon Wireless,-92,Outflow,Utilities\n"
    if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
        t.Fatalf("write csv: %v", err)
    }
    store, _ := storage.New(csvDir)
    dl2 := dataloader.New(csvDir, store)
    Initialize(dl2, nil)
    defer Initialize(dl, nil)

    if _, err := dl2.AddMajorExpense(makeExpense("anthropic", "Anthropic", []string{"anthropic"}, 0, 0)); err != nil {
        t.Fatalf("seed anthropic: %v", err)
    }
    if _, err := dl2.AddMajorExpense(makeExpense("verizon", "Verizon", []string{"verizon"}, 0, 0)); err != nil {
        t.Fatalf("seed verizon: %v", err)
    }

    data, err := buildPageData(httptest.NewRequest("GET", "/major-expenses", nil))
    if err != nil {
        t.Fatalf("buildPageData: %v", err)
    }

    total, ok := data["TotalDeclared"].(float64)
    if !ok {
        t.Fatalf("expected TotalDeclared float64 in context, got %T (%v)", data["TotalDeclared"], data["TotalDeclared"])
    }
    if total != 200 {
        t.Errorf("TotalDeclared = %v, want 200 (108 + 92)", total)
    }

    // Cross-check: TotalDeclared must equal the sum of Summaries[].Total.
    body, _ := json.Marshal(data["Summaries"])
    var raw []map[string]interface{}
    if err := json.Unmarshal(body, &raw); err != nil {
        t.Fatalf("decode summaries: %v", err)
    }
    var sum float64
    for _, s := range raw {
        sum += s["Total"].(float64)
    }
    if sum != total {
        t.Errorf("TotalDeclared (%v) != sum of Summaries[].Total (%v)", total, sum)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/darrell/bin/ai/budget2
go test ./internal/handlers/majorexpenses/ -run TestBuildPageData_TotalDeclared -v
```

Expected: FAIL with `expected TotalDeclared float64 in context, got <nil> (<nil>)`.

- [ ] **Step 3: Add `TotalDeclared` to the handler context**

Open `internal/handlers/majorexpenses/handlers.go`. After the `summaries` slice is fully populated and before `return map[string]interface{}{...}` (around line 354), insert:

```go
    var totalDeclared float64
    for _, s := range summaries {
        totalDeclared += s.Total
    }
```

Then add the new key to the returned map (after `"Summaries": summaries,`):

```go
    "Summaries":     summaries,
    "TotalDeclared": totalDeclared,
```

- [ ] **Step 4: Run the new test plus the package suite**

```bash
go test ./internal/handlers/majorexpenses/ -run TestBuildPageData_TotalDeclared -v
go test ./internal/handlers/majorexpenses/...
```

Expected: new test PASS; all existing tests still PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/majorexpenses/handlers.go internal/handlers/majorexpenses/handlers_test.go
git commit -m "$(cat <<'EOF'
feat(major-expenses): add TotalDeclared sum to page context

New context key TotalDeclared = sum(Summaries[].Total). Used by the
upcoming compact-table header to show "Total: $X" next to the add
icon. No renames, no existing keys removed.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Restructure template + update render tests

**Files:**
- Modify: `web/templates/pages/major-expenses.html` — `major-expenses-list-card-content`, `major-expenses-list`, `major-expense-item`, `major-expenses-add-form` blocks
- Modify: `internal/templates/render_major_expenses_test.go` — assertions for new structure

This task replaces the list card's always-expanded forms with the table-of-rows layout and the icon-toggled add panel. Inline JS is *not* rewired in this task — the render tests do not depend on JS, and the next task replaces the JS in one shot.

- [ ] **Step 1: Update render tests to assert the new structure**

These assertion changes will make the test suite fail until the template lands in step 2.

In `internal/templates/render_major_expenses_test.go`, find `TestRenderMajorExpenses_WithEntriesAndExceptions`. Replace the existing per-item structural assertions (lines ~177-216) with the block below, and update `TestRenderMajorExpenses_EmptyState` and the multi-entry test as noted.

A) In `TestRenderMajorExpenses_WithEntriesAndExceptions`, replace the block from `if !strings.Contains(html, \`id="major-expense-item-rent"\`)` through `if !strings.Contains(html, \`class="major-expense-matched-row\`)` with:

```go
    // Summary row keeps the stable jump target id.
    if !strings.Contains(html, `id="major-expense-item-rent"`) {
        t.Errorf("expected summary row to keep id used by jump links")
    }
    // Edit form moved out of the row form-wrapping into the detail cell
    // and got its own id.
    if !strings.Contains(html, `id="major-expense-edit-rent"`) {
        t.Errorf("expected edit form id=major-expense-edit-rent inside detail row")
    }
    // Each expense renders one tbody[data-expense-id] containing a
    // summary tr + detail tr.
    if !strings.Contains(html, `data-expense-id="rent"`) {
        t.Errorf("expected one tbody[data-expense-id=rent] per declared expense")
    }
    if !strings.Contains(html, `data-open="false"`) {
        t.Errorf("expected initial collapsed state data-open=\"false\" on tbody")
    }
    // Detail row carries id used by aria-controls and the JS toggle
    // selector.
    if !strings.Contains(html, `id="major-expense-detail-rent"`) {
        t.Errorf("expected detail row id used by aria-controls")
    }
    if !strings.Contains(html, `class="major-expense-detail-row`) {
        t.Errorf("expected detail row class used by CSS toggle")
    }
    // Chevron button must expose aria-expanded so accessibility tests
    // and the JS toggle can find it.
    if !strings.Contains(html, `aria-expanded="false"`) {
        t.Errorf("expected chevron button with aria-expanded=false")
    }
    if !strings.Contains(html, `aria-controls="major-expense-detail-rent"`) {
        t.Errorf("expected chevron aria-controls referencing detail row id")
    }
    // Add form is wrapped in a details panel toggled by the [+] icon.
    if !strings.Contains(html, `id="major-expenses-add-panel"`) {
        t.Errorf("expected add form to be wrapped in details#major-expenses-add-panel")
    }
    if !strings.Contains(html, `id="major-expenses-add-form"`) {
        t.Errorf("expected add form to keep id used by click-to-prefill handler")
    }
    if !strings.Contains(html, `id="major-expenses-add-toggle"`) {
        t.Errorf("expected [+] toggle button id used by header click handler")
    }
    // The summary row carries the row class so the unified search JS
    // can iterate it (the form no longer wraps the row in this layout).
    if !strings.Contains(html, `class="major-expense-item-row`) {
        t.Errorf("expected summary tr to carry major-expense-item-row class")
    }
    // Matched-txn rows still carry the row class so the unified search
    // JS can match them inside the same tbody group.
    if !strings.Contains(html, `class="major-expense-matched-row`) {
        t.Errorf("expected matched-txn row to carry major-expense-matched-row class")
    }
    // Header surfaces the total declared. Use a stable data attribute
    // so the assertion does not bind to formatted-money output rules.
    if !strings.Contains(html, `data-total-declared`) {
        t.Errorf("expected header to expose data-total-declared for the running sum")
    }
```

B) In the same test, also seed the `TotalDeclared` key in the data map so the new header renders:

```go
    "Threshold":     100.0,
    "WindowDays":    30,
    "TotalDeclared": 4800.0, // matches Rent's Summary Total
```

C) Remove the assertion about the matched-txn `<details id="major-expense-matched-rent">` (it no longer exists — matched txns are flat inside the detail cell). Find this block in the test:

```go
        `id="major-expense-matched-rent"`,
```

Delete that string from the slice; the loop currently asserts these IDs exist for "HTMX-swap open-state persistence" — the matched-txn nested disclosure is gone.

D) In `TestRenderMajorExpenses_EmptyState`, update the empty-state assertion to match the new copy:

Replace:

```go
    if !strings.Contains(html, "No major expenses declared yet") {
        t.Errorf("expected empty-state copy, got: %s", html)
    }
```

with:

```go
    if !strings.Contains(html, "No major expenses declared yet") {
        t.Errorf("expected empty-state copy, got: %s", html)
    }
    if !strings.Contains(html, "Click the + above") {
        t.Errorf("expected empty-state to point at the new add icon, got: %s", html)
    }
```

Also seed `TotalDeclared` in the empty-state data map:

```go
    "Threshold":     100.0,
    "WindowDays":    30,
    "TotalDeclared": 0.0,
```

E) In `TestRenderMajorExpenses_MultipleEntriesAllRender`, seed `TotalDeclared` (any non-zero value works because the test does not assert on it):

```go
        "Threshold":     100.0,
        "WindowDays":    30,
        "TotalDeclared": 300.0,
```

F) In `TestRenderMajorExpenses_MultipleEntriesAllRender`, update the pinned-marker assertion. The matched-txns table is now always flat inside the detail row — the marker is still present in the rendered output (the detail row exists in HTML even when collapsed), so the assertion still holds. Also adjust the per-entry id assertion: ids stay on the summary row.

```go
    for _, name := range []string{"Lucid", "Hyundai", "Wegmans"} {
        if !strings.Contains(html, `id="major-expense-item-`+name+`"`) {
            t.Errorf("expected summary row id %q to render", name)
        }
        if !strings.Contains(html, `data-expense-id="`+name+`"`) {
            t.Errorf("expected tbody for %q", name)
        }
    }
```

G) In `TestRenderMajorExpensesResults_IncludesOOBSwap`, seed `TotalDeclared` in the data map:

```go
        "Threshold":     100.0,
        "WindowDays":    30,
        "TotalDeclared": 0.0,
```

- [ ] **Step 2: Run render tests to verify they fail**

```bash
go test ./internal/templates/ -run TestRenderMajorExpenses -v
```

Expected: every assertion that depends on the new template structure FAILS (id missing, tbody missing, data-open missing, etc.). This confirms the assertions are wired correctly.

- [ ] **Step 3: Rewrite the list-card-content template block**

Open `web/templates/pages/major-expenses.html`. Replace the `{{define "major-expenses-list-card-content"}}` block (lines ~474-483) with:

```html
{{define "major-expenses-list-card-content"}}
<div class="flex items-center justify-between mb-3 gap-3">
    <h2 class="text-lg font-medium text-gray-800 dark:text-gray-100">Your Major Expenses</h2>
    <div class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200">
        <span data-total-declared="{{printf "%.2f" .TotalDeclared}}">
            Total: <span class="font-mono">${{printf "%.0f" .TotalDeclared}}</span>
        </span>
        <span class="text-gray-400 dark:text-gray-500">·</span>
        <span class="text-xs text-gray-500 dark:text-gray-400">{{len .Summaries}} declared</span>
        <button type="button" id="major-expenses-add-toggle"
            aria-label="Add a major expense" aria-expanded="false"
            aria-controls="major-expenses-add-panel"
            class="ml-1 p-1.5 rounded-md bg-indigo-600 text-white hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-400">
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4"></path>
            </svg>
        </button>
    </div>
</div>

<details id="major-expenses-add-panel" class="mb-3">
    <summary class="sr-only">Add a major expense</summary>
    {{template "major-expenses-add-form" .}}
</details>

{{template "major-expenses-list" .}}
{{end}}
```

- [ ] **Step 4: Rewrite the list block as a `<table>`**

Replace the `{{define "major-expenses-list"}}` block (lines ~485-495) with:

```html
{{define "major-expenses-list"}}
{{if .Summaries}}
<style>
    #major-expenses-list tbody[data-open="false"] > tr.major-expense-detail-row { display: none; }
</style>
<table id="major-expenses-list" class="w-full text-sm">
    <thead class="text-xs text-gray-500 dark:text-gray-400">
        <tr>
            <th class="w-6"></th>
            <th class="text-left py-1 px-1 font-normal">Name</th>
            <th class="text-right py-1 px-1 font-normal">Matched</th>
            <th class="text-right py-1 px-1 font-normal">Total</th>
            <th class="w-6"></th>
        </tr>
    </thead>
    {{range .Summaries}}
    {{template "major-expense-item" .}}
    {{end}}
</table>
{{else}}
<p class="text-sm text-gray-500 dark:text-gray-300 italic">
    No major expenses declared yet. Click the + above to add your first one.
</p>
{{end}}
{{end}}
```

- [ ] **Step 5: Rewrite the per-expense item as a `<tbody>`**

Replace the entire `{{define "major-expense-item"}}` block (lines ~498-589) with:

```html
{{define "major-expense-item"}}
<tbody data-expense-id="{{.Expense.ID}}" data-open="false" class="border-t border-gray-100 dark:border-gray-700">
    <tr id="major-expense-item-{{.Expense.ID}}"
        class="major-expense-item-row scroll-mt-4"
        data-search="{{.Expense.Name}}{{range .Expense.Keywords}} {{.}}{{end}}{{if .Expense.Notes}} {{.Expense.Notes}}{{end}}">
        <td class="py-1.5 px-1 align-middle">
            <button type="button" class="major-expense-row-toggle text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
                aria-expanded="false" aria-controls="major-expense-detail-{{.Expense.ID}}"
                aria-label="Show details for {{.Expense.Name}}">
                <svg class="w-3.5 h-3.5 transition-transform" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path>
                </svg>
            </button>
        </td>
        <td class="py-1.5 px-1 dark:text-gray-100 font-medium truncate">{{.Expense.Name}}</td>
        <td class="py-1.5 px-1 text-right text-gray-600 dark:text-gray-300">
            {{.Count}} matched{{if .PinnedCount}} <span class="text-amber-600 dark:text-amber-400" title="{{.PinnedCount}} manually pinned">· 📌 {{.PinnedCount}}</span>{{end}}
        </td>
        <td class="py-1.5 px-1 text-right font-mono dark:text-gray-100">${{printf "%.0f" .Total}}</td>
        <td class="py-1.5 px-1 text-right align-middle">
            <button type="button" hx-delete="/major-expenses/{{.Expense.ID}}"
                hx-target="#major-expenses-results" hx-swap="innerHTML"
                hx-include="#major-expenses-filter-form"
                class="text-red-500 hover:text-red-700"
                aria-label="Delete major expense {{.Expense.Name}}">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
                </svg>
            </button>
        </td>
    </tr>
    <tr id="major-expense-detail-{{.Expense.ID}}" class="major-expense-detail-row">
        <td colspan="5" class="px-1 pb-3 pt-1 bg-gray-50 dark:bg-gray-700/50">
            <form id="major-expense-edit-{{.Expense.ID}}"
                hx-put="/major-expenses/{{.Expense.ID}}" hx-target="#major-expenses-results"
                hx-swap="innerHTML" hx-trigger="change delay:500ms"
                hx-include="#major-expenses-filter-form"
                class="grid grid-cols-1 gap-2 text-xs p-2">
                <label class="block">
                    <span class="text-gray-600 dark:text-gray-300">Name</span>
                    <input type="text" name="name" value="{{.Expense.Name}}" required
                        class="mt-0.5 w-full px-2 py-1 border border-gray-300 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 rounded"
                        aria-label="Expense name">
                </label>
                <label class="block">
                    <span class="text-gray-600 dark:text-gray-300">Keywords (comma-separated, blank = match by amount)</span>
                    <input type="text" name="keywords"
                        value="{{range $i, $k := .Expense.Keywords}}{{if $i}}, {{end}}{{$k}}{{end}}"
                        class="mt-0.5 w-full px-2 py-1 border border-gray-300 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 rounded">
                </label>
                <div class="grid grid-cols-2 gap-2">
                    <label>
                        <span class="text-gray-600 dark:text-gray-300">Min $ (0 = no min)</span>
                        <input type="number" name="expected_min" value="{{printf "%.2f" .Expense.ExpectedMin}}" min="0" step="0.01"
                            class="mt-0.5 w-full px-2 py-1 border border-gray-300 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 rounded">
                    </label>
                    <label>
                        <span class="text-gray-600 dark:text-gray-300">Max $ (0 = no max)</span>
                        <input type="number" name="expected_max" value="{{printf "%.2f" .Expense.ExpectedMax}}" min="0" step="0.01"
                            class="mt-0.5 w-full px-2 py-1 border border-gray-300 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 rounded">
                    </label>
                </div>
                <label class="block">
                    <span class="text-gray-600 dark:text-gray-300">Notes (optional)</span>
                    <input type="text" name="notes" value="{{.Expense.Notes}}"
                        class="mt-0.5 w-full px-2 py-1 border border-gray-300 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 rounded">
                </label>
            </form>
            {{if .Transactions}}
            <div class="mt-2 text-xs">
                <p class="text-gray-500 dark:text-gray-400 px-2 py-0.5">
                    Matched transactions ({{.Count}}{{if .PinnedCount}}, {{.PinnedCount}} pinned{{end}})
                </p>
                <table class="w-full">
                    <thead class="text-gray-400 dark:text-gray-500">
                        <tr><th class="text-left py-0.5 px-2">Date</th><th class="text-left py-0.5 px-2">Description</th><th class="text-right py-0.5 px-2">Amount</th><th></th></tr>
                    </thead>
                    <tbody>
                        {{$pinned := .PinnedHashes}}
                        {{range .Transactions}}
                        {{$bankText := or .DisplayName .Description}}
                        <tr class="major-expense-matched-row border-t border-gray-200 dark:border-gray-600"
                            data-search="{{$bankText}} ${{printf "%.2f" .AbsAmount}} {{.Date.Format "2006-01-02"}}">
                            <td class="py-0.5 px-2 dark:text-gray-300">{{.Date.Format "2006-01-02"}}</td>
                            <td class="py-0.5 px-2 dark:text-gray-200">
                                {{if and $pinned (index $pinned .Hash)}}<span class="text-amber-600 dark:text-amber-400" title="Pinned manually">📌</span> {{end}}<a href="/explorer?search={{urlquery $bankText}}&type=Outflow"
                                    class="text-blue-600 dark:text-blue-400 hover:underline"
                                    title="Show all transactions matching “{{$bankText}}” in the Explorer">{{$bankText}}</a>
                            </td>
                            <td class="py-0.5 px-2 text-right font-mono text-red-600 dark:text-red-400">${{printf "%.2f" .AbsAmount}}</td>
                            <td class="py-0.5 px-2 text-right">
                                {{if and $pinned (index $pinned .Hash)}}
                                <button type="button"
                                    hx-delete="/major-expenses/pins/{{.Hash}}"
                                    hx-target="#major-expenses-results" hx-swap="innerHTML"
                                    hx-include="#major-expenses-filter-form"
                                    class="text-xs text-gray-500 hover:text-red-600 dark:text-gray-400 dark:hover:text-red-400"
                                    aria-label="Unpin transaction">unpin</button>
                                {{end}}
                            </td>
                        </tr>
                        {{end}}
                    </tbody>
                </table>
            </div>
            {{end}}
        </td>
    </tr>
</tbody>
{{end}}
```

Notes embedded in the markup above:
- The summary row keeps `id="major-expense-item-{ID}"` (jump target).
- The edit form is `id="major-expense-edit-{ID}"` (avoids colliding with the row id).
- The Name field stays editable, but moves into the detail-row form (no longer inline in the row). This preserves rename + satisfies the existing PUT validation. The summary row shows the name as plain text for scanability.
- `<style>` is inline to keep the change scoped to this template; the codebase has no global CSS file, so inline `<style>` is the existing pattern when behavior-specific CSS is needed.

- [ ] **Step 6: Update the add-form template — keep id, drop the now-redundant intro paragraph**

Replace the `{{define "major-expenses-add-form"}}` block (lines ~591-638). The form keeps every name attribute and HTMX wiring; only its surrounding `<details>` and the auto-collapse hook change. Replace with:

```html
{{define "major-expenses-add-form"}}
<form id="major-expenses-add-form" hx-post="/major-expenses" hx-target="#major-expenses-results" hx-swap="innerHTML"
    hx-on::after-request="if(event.detail.successful){ this.reset(); var p=document.getElementById('major-expenses-add-panel'); if(p){ p.open=false; var t=document.getElementById('major-expenses-add-toggle'); if(t){ t.setAttribute('aria-expanded','false'); } } }"
    hx-include="#major-expenses-filter-form"
    class="space-y-2 pb-3 mb-3 border-b dark:border-gray-700">
    <h3 class="text-sm font-medium text-gray-700 dark:text-gray-200">Add a major expense</h3>
    <p class="text-xs text-gray-500 dark:text-gray-400">Tip: click an exception row on the right to pre-fill this form.</p>
    <label class="block">
        <span class="text-xs text-gray-600 dark:text-gray-300">Name <span class="text-red-500">*</span></span>
        <input type="text" name="name" placeholder="What is this? e.g., Car Payment, Rent, Groceries" required
            class="mt-0.5 w-full text-sm border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-md py-1.5 px-2">
    </label>
    <label class="block">
        <span class="text-xs text-gray-600 dark:text-gray-300">Keywords <span class="text-gray-400">(match by description)</span></span>
        <input type="text" name="keywords" placeholder="e.g., landlord — leave blank for amount-only or pin-only"
            class="mt-0.5 w-full text-sm border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-md py-1.5 px-2">
    </label>
    <div class="grid grid-cols-2 gap-2">
        <label class="block">
            <span class="text-xs text-gray-600 dark:text-gray-300">Min $</span>
            <input type="number" name="expected_min" placeholder="0 = no min" min="0" step="0.01"
                class="mt-0.5 w-full text-sm border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-md py-1.5 px-2">
        </label>
        <label class="block">
            <span class="text-xs text-gray-600 dark:text-gray-300">Max $</span>
            <input type="number" name="expected_max" placeholder="0 = no max" min="0" step="0.01"
                class="mt-0.5 w-full text-sm border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-md py-1.5 px-2">
        </label>
    </div>
    <details class="text-xs text-gray-500 dark:text-gray-400">
        <summary class="cursor-pointer hover:text-gray-700 dark:hover:text-gray-200">How matching works</summary>
        <ul class="mt-1 ml-4 list-disc space-y-1">
            <li><strong>Keyword only</strong> — matches anything whose description contains the keyword (case-insensitive). A range, if set, is used for anomaly detection.</li>
            <li><strong>Min = Max only</strong> (no keyword) — matches every transaction of that exact amount. Useful when descriptions vary (checks).</li>
            <li><strong>Keyword + Min = Max</strong> — both must match. Use this when several expenses share a generic keyword like "check": e.g. Lucid keyword=<code>check</code> Min=Max=<code>1580.00</code>, Car keyword=<code>check</code> Min=Max=<code>626.00</code>.</li>
            <li><strong>Min &lt; Max</strong> (range) without a keyword matches anything in that range; with a keyword, the range is anomaly-only.</li>
            <li><strong>Pin-only</strong> (no keyword, no Min/Max) — auto-matches nothing. Use this for sub-buckets like "Amazon — Books" where no keyword reliably distinguishes them; assign transactions one-by-one via the "Pin to…" dropdown on each exception row, or bulk-pin filtered exceptions.</li>
        </ul>
    </details>
    <input type="text" name="notes" placeholder="Notes (optional)"
        class="w-full text-sm border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-md py-1.5 px-2">
    <div class="flex justify-end">
        <button type="submit" class="px-3 py-1.5 bg-indigo-600 text-white text-sm rounded hover:bg-indigo-700">
            Add expense
        </button>
    </div>
</form>
{{end}}
```

- [ ] **Step 7: Run the render tests**

```bash
go test ./internal/templates/ -run TestRenderMajorExpenses -v
```

Expected: all four render tests PASS.

- [ ] **Step 8: Run the handler suite to confirm no regression**

```bash
go test ./internal/handlers/majorexpenses/...
```

Expected: PASS — handlers don't depend on the template internals.

- [ ] **Step 9: Run the full check before commit**

```bash
go test ./...
```

Expected: full suite PASS. (The pre-commit hook runs `go vet`, `staticcheck`, `govulncheck`, then `go test ./...` — same gates.)

- [ ] **Step 10: Commit**

```bash
git add web/templates/pages/major-expenses.html internal/templates/render_major_expenses_test.go
git commit -m "$(cat <<'EOF'
feat(major-expenses): collapsible table + icon-toggled add panel

Convert the "Your Major Expenses" list card from a stack of
always-expanded edit forms into a compact <table> with one
<tbody data-expense-id="..." data-open="false"> per expense
(summary row + detail row carrying the edit form and matched-txn
table). Header now shows Total: $X next to a [+] icon that
toggles a collapsed <details id="major-expenses-add-panel">
wrapping the existing add form. Empty-state copy updated to
point at the new icon. JS rewiring lands in the next commit.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Rewire inline JS — chevron toggle, add-panel toggle, group-based search, jump, prefill, swap persistence

**Files:**
- Modify: `web/templates/pages/major-expenses.html` — second `<script>` block (the IIFE starting `(function () { function visibleExceptionHashes() {...`).

This task replaces the inline JS that the previous task left in a stale state (selectors that no longer match the new DOM). After this task, every interactive behavior listed in the spec works end-to-end.

- [ ] **Step 1: Replace the second `<script>` IIFE wholesale**

Open `web/templates/pages/major-expenses.html`. Locate the `<script>` block that starts at line ~212 (`(function () { function visibleExceptionHashes() {...`) and ends at line ~459. Replace its **entire body** (everything between `<script>` and `</script>`) with:

```javascript
(function () {
    function visibleExceptionHashes() {
        return Array.from(
            document.querySelectorAll('tr.major-expenses-exception-row:not([style*="display: none"])')
        ).map(function (tr) {
            return tr.getAttribute('data-hash') || '';
        }).filter(Boolean);
    }

    // Show/hide the bulk-pin toolbar and update its row count based on
    // the current visible-exception set.
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

    function setRowOpen(tbody, open) {
        if (!tbody) return;
        tbody.setAttribute('data-open', open ? 'true' : 'false');
        const btn = tbody.querySelector('button.major-expense-row-toggle');
        if (btn) btn.setAttribute('aria-expanded', open ? 'true' : 'false');
    }

    function applyUnifiedFilter(query) {
        const q = (query || '').toLowerCase().trim();

        // ---- LEFT CARD: iterate by tbody group, not by row. ----
        const groups = document.querySelectorAll('#major-expenses-list tbody[data-expense-id]');
        let visibleExpenses = 0;
        groups.forEach(function (group) {
            const summary = group.querySelector('tr.major-expense-item-row');
            const itemHay = (summary && summary.getAttribute('data-search') || '').toLowerCase();
            const itemMatch = q === '' || itemHay.includes(q);

            const txnRows = group.querySelectorAll('tr.major-expense-matched-row');
            let txnHadMatch = false;
            txnRows.forEach(function (tr) {
                const hay = (tr.getAttribute('data-search') || '').toLowerCase();
                const match = q === '' || hay.includes(q);
                tr.style.display = match ? '' : 'none';
                if (match && q !== '') txnHadMatch = true;
            });

            const show = q === '' || itemMatch || txnHadMatch;
            group.style.display = show ? '' : 'none';
            if (show) visibleExpenses++;

            // Force-open the row when the user matched a contained txn.
            // Empty queries leave open-state untouched so collapse
            // defaults survive.
            if (q !== '' && txnHadMatch) setRowOpen(group, true);
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
        const clear = document.getElementById('major-expenses-search-clear');
        if (clear) clear.classList.toggle('hidden', q === '');

        syncBulkPinToolbar(visibleExceptions, q);
    }

    // Persist <details> open state AND tbody[data-open] across HTMX
    // swaps. Each swap replaces DOM nodes wholesale, so we snapshot
    // before, restore after.
    let savedOpenDetails = null;
    let savedOpenRows = null;
    function persistedDetailsSelector() {
        return '#major-expenses-results details[id], #major-expenses-list-card details[id]';
    }
    document.body.addEventListener('htmx:beforeSwap', function () {
        const open = new Set();
        document.querySelectorAll(persistedDetailsSelector()).forEach(function (d) {
            if (d.open) open.add(d.id);
        });
        savedOpenDetails = open;

        const rows = new Set();
        document.querySelectorAll('#major-expenses-list tbody[data-expense-id][data-open="true"]').forEach(function (tb) {
            rows.add(tb.getAttribute('data-expense-id'));
        });
        savedOpenRows = rows;
    });
    document.body.addEventListener('htmx:afterSwap', function () {
        if (savedOpenDetails) {
            document.querySelectorAll(persistedDetailsSelector()).forEach(function (d) {
                if (savedOpenDetails.has(d.id)) d.open = true;
            });
            savedOpenDetails = null;
        }
        if (savedOpenRows) {
            savedOpenRows.forEach(function (id) {
                const tb = document.querySelector('#major-expenses-list tbody[data-expense-id="' + CSS.escape(id) + '"]');
                if (tb) setRowOpen(tb, true);
            });
            savedOpenRows = null;
        }
        const input = document.getElementById('major-expenses-search');
        if (input && input.value) applyUnifiedFilter(input.value);
    });

    // Bulk-pin Apply.
    document.addEventListener('click', function (e) {
        if (!e.target || e.target.id !== 'major-expenses-bulk-pin-apply') return;
        e.preventDefault();
        const target = document.getElementById('major-expenses-bulk-pin-target');
        if (!target || !target.value) return;
        const hashes = visibleExceptionHashes();
        if (!hashes.length) return;
        const fd = new FormData();
        fd.append('expense_id', target.value);
        hashes.forEach(function (h) { fd.append('hashes', h); });
        const filterForm = document.getElementById('major-expenses-filter-form');
        if (filterForm) {
            const startVal = filterForm.querySelector('input[name="start"]').value;
            const endVal = filterForm.querySelector('input[name="end"]').value;
            if (startVal) fd.append('start', startVal);
            if (endVal) fd.append('end', endVal);
        }
        if (window.htmx && typeof window.htmx.ajax === 'function') {
            window.htmx.ajax('POST', '/major-expenses/pins/bulk', {
                target: '#major-expenses-results',
                swap: 'innerHTML',
                values: fd,
            });
        }
    });

    document.addEventListener('change', function (e) {
        if (e.target && e.target.id === 'major-expenses-bulk-pin-target') {
            const input = document.getElementById('major-expenses-search');
            applyUnifiedFilter(input ? input.value : '');
        }
    });

    // Search input (delegated; survives HTMX swaps).
    document.addEventListener('input', function (e) {
        if (e.target && e.target.id === 'major-expenses-search') {
            applyUnifiedFilter(e.target.value);
        }
    });

    document.addEventListener('click', function (e) {
        const clear = e.target && e.target.closest && e.target.closest('#major-expenses-search-clear');
        if (!clear) return;
        e.preventDefault();
        const input = document.getElementById('major-expenses-search');
        if (!input) return;
        input.value = '';
        applyUnifiedFilter('');
        input.focus();
    });

    // Add-panel toggle: [+] icon flips <details> open state and aria.
    document.addEventListener('click', function (e) {
        const toggle = e.target && e.target.closest && e.target.closest('#major-expenses-add-toggle');
        if (!toggle) return;
        e.preventDefault();
        const panel = document.getElementById('major-expenses-add-panel');
        if (!panel) return;
        const willOpen = !panel.open;
        panel.open = willOpen;
        toggle.setAttribute('aria-expanded', willOpen ? 'true' : 'false');
        if (willOpen) {
            const name = panel.querySelector('input[name="name"]');
            if (name) name.focus();
        }
    });

    // Chevron toggle: flips data-open on the parent tbody.
    document.addEventListener('click', function (e) {
        const btn = e.target && e.target.closest && e.target.closest('button.major-expense-row-toggle');
        if (!btn) return;
        e.preventDefault();
        const tbody = btn.closest('tbody[data-expense-id]');
        if (!tbody) return;
        const open = tbody.getAttribute('data-open') !== 'true';
        setRowOpen(tbody, open);
    });

    // Click delegation for jump-to-existing AND exception-row prefill.
    document.addEventListener('click', function (e) {
        const tag = (e.target.tagName || '').toLowerCase();
        if (tag === 'input' || tag === 'button' || tag === 'svg' || tag === 'path' ||
            tag === 'select' || tag === 'option') return;

        // Jump-to-existing: open the target row first.
        const jumpEl = e.target.closest('[data-jump-expense]');
        if (jumpEl) {
            e.preventDefault();
            const id = jumpEl.getAttribute('data-jump-expense');
            const summary = document.getElementById('major-expense-item-' + id);
            const tbody = summary && summary.closest('tbody[data-expense-id]');
            if (!summary) return;
            setRowOpen(tbody, true);
            summary.scrollIntoView({ behavior: 'smooth', block: 'center' });
            summary.classList.add('ring-2', 'ring-amber-400');
            setTimeout(function () { summary.classList.remove('ring-2', 'ring-amber-400'); }, 1500);
            return;
        }

        // Exception-row prefill: open the add panel before filling.
        const fillRow = e.target.closest('tr[data-fill-name]');
        if (fillRow) {
            const form = document.getElementById('major-expenses-add-form');
            if (!form) return;
            const panel = document.getElementById('major-expenses-add-panel');
            const toggle = document.getElementById('major-expenses-add-toggle');
            if (panel && !panel.open) {
                panel.open = true;
                if (toggle) toggle.setAttribute('aria-expanded', 'true');
            }
            const desc = fillRow.getAttribute('data-fill-name') || '';
            const amount = parseFloat(fillRow.getAttribute('data-fill-amount') || '0');
            const isCheckLike = /\bcheck\b|^\s*#?\d{3,}\s*$/i.test(desc);

            form.querySelector('input[name="name"]').value = '';

            if (isCheckLike) {
                form.querySelector('input[name="keywords"]').value = '';
                if (amount > 0) {
                    const exact = amount.toFixed(2);
                    form.querySelector('input[name="expected_min"]').value = exact;
                    form.querySelector('input[name="expected_max"]').value = exact;
                }
            } else {
                form.querySelector('input[name="keywords"]').value = desc;
                form.querySelector('input[name="expected_min"]').value = '';
                form.querySelector('input[name="expected_max"]').value = '';
            }

            form.querySelector('input[name="name"]').focus();
            form.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
            return;
        }
    });
})();
```

- [ ] **Step 2: Verify no template render regressions**

```bash
go test ./internal/templates/...
go test ./internal/handlers/majorexpenses/...
```

Expected: all PASS — JS changes don't affect server-rendered HTML.

- [ ] **Step 3: Run the full check**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add web/templates/pages/major-expenses.html
git commit -m "$(cat <<'EOF'
feat(major-expenses): rewire inline JS for collapsible table

Replace the per-row form-based search loop with a tbody-group
loop, add the chevron toggle and [+] add-panel toggle, snapshot
and restore tbody[data-open] across HTMX swaps alongside the
existing <details> snapshot, and update jump-to-existing /
exception-row prefill to open their target panels first. Mutation
routing (#major-expenses-results target + OOB swap of
#major-expenses-list-card) is unchanged.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Manual browser verification

**Files:** none — runtime check.

This task is a checklist run, not a code change. Per CLAUDE.md, UI changes require manual verification before declaring done.

- [ ] **Step 1: Start the dev server**

```bash
cd /home/darrell/bin/ai/budget2
make dev
```

Wait for "listening on …" log, then open `http://localhost:8080/major-expenses` in a browser.

- [ ] **Step 2: Walk the spec checklist**

Confirm each item from the spec's Testing → Manual verification block:

1. Default state — table compact, total visible (e.g. `Total: $4,231 · 12 declared · [+]`), add panel hidden.
2. Click `[+]` — add form expands inline; submit creates expense; panel collapses on success and the table now shows the new row.
3. Click chevron on any row — single row expands with edit form + matched-txn table together (no nested toggle).
4. Edit a field (keywords or min/max) — htmx PUT fires after 500ms (network tab), the right card refreshes, the row stays open.
5. Type in the search box — rows whose name/keywords/notes match stay visible; matching matched-txns force-open their row.
6. Type a search that matches an exception — right card filters and the bulk-pin toolbar appears.
7. Change the date range — list re-renders, active search reapplies, rows that were closed stay closed.
8. Open one row → change date range → that row stays open after swap (when the expense still exists in the new window).
9. Click an anomalous-exception expense-name link — its row opens, scrolls into view, briefly highlights amber.
10. Click an exception row to prefill the add form while the add panel is closed — panel opens, fields fill, name input is focused.

- [ ] **Step 3: Confirm dark mode parity**

Toggle dark mode and re-verify items 1, 3, and 5 — total, chevron contrast, and search status should all read clearly.

- [ ] **Step 4: GitNexus post-flight**

```bash
# gitnexus_detect_changes()
```

Verify the affected-symbol set matches expectation (`buildPageData` + the renamed/new template blocks, plus the JS IIFE). No surprise upstream impact.

- [ ] **Step 5: Note any issues, fix in a follow-up commit, then declare done**

If anything fails, file the regression as a discrete fix commit before merging. Otherwise the branch is ready for the user to review.

---

## Self-Review

- **Spec coverage:**
  - Card structure → Task 2 step 3 (header) + step 4 (table) + step 5 (item).
  - Header total/count/icon → Task 2 step 3.
  - Add panel via `<details>` with hidden `<summary>` (sr-only) → Task 2 step 3.
  - `<table id="major-expenses-list">` with `<tbody data-expense-id>` per row → Task 2 step 4 + 5.
  - Chevron `aria-expanded` + `aria-controls` → Task 2 step 5.
  - Edit form id rename to `major-expense-edit-{ID}` → Task 2 step 5.
  - Matched-txns flat in detail cell → Task 2 step 5.
  - Group-based search → Task 3.
  - Force-open on nested-txn match → Task 3 (`setRowOpen(group, true)` when `txnHadMatch`).
  - Persistence of `data-open` across HTMX swaps → Task 3 (`savedOpenRows`).
  - Jump-to-existing opens row first → Task 3.
  - Exception prefill opens add panel first → Task 3.
  - `TotalDeclared` server-side → Task 1.
  - Render-test coverage of new structure → Task 2 step 1.
  - Manual verification → Task 4.
  - GitNexus pre + post → Task 0 + Task 4.

- **Placeholder scan:** No "TBD"/"TODO"/"add appropriate error handling"/"similar to Task N" in the plan. Every code step shows the exact code.

- **Type/name consistency:** Verified — `setRowOpen` is defined once and used by chevron, search-force-open, jump, and swap-restore. `data-expense-id` is consistent across template and JS. `major-expense-edit-{ID}` is used as form id. `major-expense-detail-{ID}` is used both as `<tr id>` and chevron `aria-controls`. `data-total-declared` matches the render test assertion. Add-panel id `major-expenses-add-panel` and toggle id `major-expenses-add-toggle` are consistent across template, JS, and tests. Name editing moved from inline-row to detail-row form so the existing PUT validation still passes and rename is preserved.
