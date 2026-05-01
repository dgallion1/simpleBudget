# Major Expense Chart Readability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the dashboard's "Spending by Major Expense" donut readable when there are many small buckets — keep the top 8 as wedges, roll the rest into a single "Other" wedge, and list the rolled-up items in a small text breakdown below the chart.

**Architecture:** Server-side rollup of buckets past the top-8 limit (mirroring the existing `buildCategoryChartData` "top 10 + Other" pattern) plus a new `smaller` field in the JSON response. Template adds a sibling div under the chart. Client adds a per-chart hook in `renderChart` that builds the breakdown using DOM APIs (no innerHTML, no string templating) when `data.smaller` is present.

**Tech Stack:** Go (backend), Plotly.js + vanilla JS (charts), Tailwind classes (styling), standard `go test` for server tests, manual browser smoke test for the UI piece.

---

## File Structure

- **Modify:** `internal/handlers/dashboard/handlers.go` — extend `buildMajorExpenseChartData` to roll up beyond top 8 and emit `smaller`.
- **Modify:** `internal/handlers/dashboard/handlers_http_test.go` — add server-side test cases for the new rollup + percent behavior.
- **Modify:** `web/templates/pages/dashboard.html` — add `#chart-major-expense-breakdown` sibling div.
- **Modify:** `web/static/js/charts.js` — add a per-chart hook in `renderChart` that fills the breakdown div when `data.smaller` is non-empty.

No new files. Each modification is bounded and self-contained.

---

## Task 1: Server bucketing — top 8 + Other rollup with `smaller` payload

**Files:**
- Modify: `internal/handlers/dashboard/handlers.go:744-813` (`buildMajorExpenseChartData`)
- Test: `internal/handlers/dashboard/handlers_http_test.go` (new tests near existing `TestBuildMajorExpenseChartData_*`)

- [ ] **Step 1: Run impact analysis on the function being changed**

Run via the GitNexus MCP tool:
`gitnexus_impact({target: "buildMajorExpenseChartData", direction: "upstream"})`

Expected: low risk — the function is called only from `handleChartData` in `handlers.go:162`. Report blast radius back to the user before proceeding. Stop and warn if HIGH/CRITICAL.

- [ ] **Step 2: Write the first failing test — fewer than threshold, no rollup**

Add this test to `internal/handlers/dashboard/handlers_http_test.go` directly after `TestBuildMajorExpenseChartData_AllUnmatched`:

```go
// Helper: install a loader with the given major expenses for the duration
// of the test. The loader is a package-level var; restore it on cleanup.
func withMajorExpenses(t *testing.T, expenses []models.MajorExpense) {
	t.Helper()
	tmpDir, dl, cleanup := writeTempCSV(t, [][]string{})
	t.Cleanup(cleanup)
	_ = tmpDir
	if err := dl.SaveMajorExpenses(expenses); err != nil {
		t.Fatalf("SaveMajorExpenses: %v", err)
	}
	prev := loader
	loader = dl
	t.Cleanup(func() { loader = prev })
}

func TestBuildMajorExpenseChartData_FewerThanThreshold(t *testing.T) {
	// 5 distinct major expenses, all matched → no "Other" wedge, no smaller.
	var expenses []models.MajorExpense
	var txns []models.Transaction
	for i := 0; i < 5; i++ {
		name := string(rune('A' + i))
		expenses = append(expenses, models.MajorExpense{
			ID: name, Name: name + "-bucket", Keywords: []string{name + "-kw"},
		})
		txns = append(txns, makeTransaction(name+"-kw payment", -float64((5-i)*100),
			time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"))
	}
	withMajorExpenses(t, expenses)

	ts := makeTransactionSet(txns...)
	result := buildMajorExpenseChartData(ts)

	data := result["data"].([]map[string]interface{})
	labels := data[0]["labels"].([]string)
	for _, l := range labels {
		if l == "Other" {
			t.Errorf("expected no 'Other' wedge with 5 buckets, got labels=%v", labels)
		}
	}
	if _, ok := result["smaller"]; ok {
		t.Errorf("expected no 'smaller' field with 5 buckets, got %v", result["smaller"])
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/handlers/dashboard/ -run TestBuildMajorExpenseChartData_FewerThanThreshold -v`

Expected: FAIL — either compilation error (`withMajorExpenses` is new and may already pass since current code has no Other concept and no smaller key, so likely PASS). If it PASSES on the first run because the current implementation never adds "Other" or `smaller`, that's fine — this is a regression-prevention test. Note "PASS" as the actual result and continue.

- [ ] **Step 4: Write the second failing test — exactly at threshold**

Add to the same test file:

```go
func TestBuildMajorExpenseChartData_ExactlyAtThreshold(t *testing.T) {
	// 8 distinct major expenses → no "Other" wedge, no smaller.
	var expenses []models.MajorExpense
	var txns []models.Transaction
	for i := 0; i < 8; i++ {
		name := string(rune('A' + i))
		expenses = append(expenses, models.MajorExpense{
			ID: name, Name: name + "-bucket", Keywords: []string{name + "-kw"},
		})
		txns = append(txns, makeTransaction(name+"-kw payment", -float64((8-i)*100),
			time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"))
	}
	withMajorExpenses(t, expenses)

	ts := makeTransactionSet(txns...)
	result := buildMajorExpenseChartData(ts)

	labels := result["data"].([]map[string]interface{})[0]["labels"].([]string)
	if len(labels) != 8 {
		t.Errorf("expected 8 labels (no Other), got %d: %v", len(labels), labels)
	}
	for _, l := range labels {
		if l == "Other" {
			t.Errorf("expected no 'Other' wedge at exactly 8 buckets, got labels=%v", labels)
		}
	}
	if _, ok := result["smaller"]; ok {
		t.Errorf("expected no 'smaller' field at exactly 8 buckets")
	}
}
```

- [ ] **Step 5: Write the third failing test — above threshold, rollup occurs**

```go
func TestBuildMajorExpenseChartData_AboveThresholdRollup(t *testing.T) {
	// 11 distinct major expenses → top 8 + Other (rollup of 3) on the donut,
	// smaller has 3 entries with descending amounts.
	var expenses []models.MajorExpense
	var txns []models.Transaction
	for i := 0; i < 11; i++ {
		name := string(rune('A' + i))
		expenses = append(expenses, models.MajorExpense{
			ID: name, Name: name + "-bucket", Keywords: []string{name + "-kw"},
		})
		// Amounts: 1100, 1000, 900, ..., 100 (descending)
		txns = append(txns, makeTransaction(name+"-kw payment", -float64((11-i)*100),
			time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"))
	}
	withMajorExpenses(t, expenses)

	ts := makeTransactionSet(txns...)
	result := buildMajorExpenseChartData(ts)

	labels := result["data"].([]map[string]interface{})[0]["labels"].([]string)
	values := result["data"].([]map[string]interface{})[0]["values"].([]float64)

	if len(labels) != 9 {
		t.Fatalf("expected 9 wedges (top 8 + Other), got %d: %v", len(labels), labels)
	}
	if labels[8] != "Other" {
		t.Errorf("expected last wedge to be 'Other', got %q", labels[8])
	}

	// Sum of bottom 3 buckets (300+200+100 = 600).
	if !floatEqual(values[8], 600) {
		t.Errorf("Other value = %v, want 600", values[8])
	}

	smaller, ok := result["smaller"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected smaller []map[string]interface{}, got %T", result["smaller"])
	}
	if len(smaller) != 3 {
		t.Fatalf("expected 3 smaller entries, got %d: %v", len(smaller), smaller)
	}
	// Sorted descending — biggest of the rolled-up items first.
	if smaller[0]["name"] != "I-bucket" {
		t.Errorf("smaller[0].name = %v, want I-bucket", smaller[0]["name"])
	}
	if a, _ := smaller[0]["amount"].(float64); !floatEqual(a, 300) {
		t.Errorf("smaller[0].amount = %v, want 300", smaller[0]["amount"])
	}
	// Total = 6600, smaller[0] = 300 → 4.55%, rounded to 1 decimal = 4.5.
	if p, _ := smaller[0]["percent"].(float64); !floatEqual(p, 4.5) && !floatEqual(p, 4.55) {
		t.Errorf("smaller[0].percent = %v, want ~4.5", smaller[0]["percent"])
	}
}
```

- [ ] **Step 6: Write the fourth failing test — Unmatched stays last after Other**

```go
func TestBuildMajorExpenseChartData_RollupWithUnmatched(t *testing.T) {
	// 11 matched + some unmatched → wedge order: top 8 matched, Other,
	// then Unmatched last. smaller excludes Unmatched.
	var expenses []models.MajorExpense
	var txns []models.Transaction
	for i := 0; i < 11; i++ {
		name := string(rune('A' + i))
		expenses = append(expenses, models.MajorExpense{
			ID: name, Name: name + "-bucket", Keywords: []string{name + "-kw"},
		})
		txns = append(txns, makeTransaction(name+"-kw payment", -float64((11-i)*100),
			time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"))
	}
	// Add an outflow that matches no keyword → goes to Unmatched.
	txns = append(txns, makeTransaction("mystery", -2000,
		time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Misc"))
	withMajorExpenses(t, expenses)

	ts := makeTransactionSet(txns...)
	result := buildMajorExpenseChartData(ts)

	labels := result["data"].([]map[string]interface{})[0]["labels"].([]string)
	if len(labels) != 10 {
		t.Fatalf("expected 10 wedges (top 8 + Other + Unmatched), got %d: %v", len(labels), labels)
	}
	if labels[8] != "Other" {
		t.Errorf("expected wedge 8 = Other, got %q", labels[8])
	}
	if labels[9] != "Unmatched" {
		t.Errorf("expected wedge 9 = Unmatched (last), got %q", labels[9])
	}

	smaller := result["smaller"].([]map[string]interface{})
	for _, item := range smaller {
		if item["name"] == "Unmatched" {
			t.Errorf("smaller must not contain Unmatched, got %v", smaller)
		}
	}
}
```

- [ ] **Step 7: Write the fifth failing test — sub-1% percent precision**

```go
func TestBuildMajorExpenseChartData_SubOnePercentPrecision(t *testing.T) {
	// One huge bucket plus 10 tiny ones → tail items have sub-1% shares
	// and must be returned with two-decimal precision (not 0.0).
	var expenses []models.MajorExpense
	var txns []models.Transaction

	// Big bucket: 99000.
	expenses = append(expenses, models.MajorExpense{
		ID: "big", Name: "Big", Keywords: []string{"big-kw"},
	})
	txns = append(txns, makeTransaction("big-kw rent", -99000,
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"))

	// 10 tiny buckets at $50 each → total = 500. Grand total = 99500.
	// Each tiny share = 50/99500 ≈ 0.0503% → must NOT round to 0.0.
	for i := 0; i < 10; i++ {
		name := "tiny" + string(rune('A'+i))
		expenses = append(expenses, models.MajorExpense{
			ID: name, Name: name, Keywords: []string{name + "-kw"},
		})
		txns = append(txns, makeTransaction(name+"-kw small", -50,
			time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Misc"))
	}

	withMajorExpenses(t, expenses)
	ts := makeTransactionSet(txns...)
	result := buildMajorExpenseChartData(ts)

	smaller, ok := result["smaller"].([]map[string]interface{})
	if !ok || len(smaller) == 0 {
		t.Fatalf("expected smaller entries, got %v", result["smaller"])
	}
	for _, item := range smaller {
		p, _ := item["percent"].(float64)
		if p == 0 {
			t.Errorf("sub-1%% bucket rounded to 0%%; need 2-decimal precision: %v", item)
		}
	}
}
```

- [ ] **Step 8: Run all five new tests to confirm they fail (or surface the gap)**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/handlers/dashboard/ -run TestBuildMajorExpenseChartData -v`

Expected:
- `_EmptyData` and `_AllUnmatched` continue to PASS.
- `_FewerThanThreshold` and `_ExactlyAtThreshold` may PASS (current code never adds Other or smaller — those assertions hold).
- `_AboveThresholdRollup`, `_RollupWithUnmatched`, `_SubOnePercentPrecision` FAIL (current code returns 11/12 wedges, no Other, no smaller).

If a test that "should pass" fails (e.g., compile errors), fix the test before continuing.

- [ ] **Step 9: Implement the rollup in `buildMajorExpenseChartData`**

Replace the body of `buildMajorExpenseChartData` in `internal/handlers/dashboard/handlers.go` (lines 744–813) with:

```go
// buildMajorExpenseChartData renders a pie chart of outflow spending
// grouped by user-declared major expense for the date-filtered window.
// Transactions that don't match any major expense are bucketed under
// "Unmatched" so the totals add up to the period's total outflows.
//
// To keep the donut readable when many small buckets exist, only the
// top majorExpenseDonutLimit matched buckets are kept as individual
// wedges; the rest are rolled into a single "Other" wedge. The list of
// rolled-up items is returned alongside the chart in the "smaller"
// field so the client can render a text breakdown.
func buildMajorExpenseChartData(ts *models.TransactionSet) map[string]interface{} {
	const majorExpenseDonutLimit = 8

	outflows := ts.FilterByType(models.Outflow)

	// Best-effort load — empty config (or no loader during unit tests)
	// just means everything goes in "Unmatched", which is a perfectly
	// fine empty state.
	var expenses []models.MajorExpense
	var pins map[string]string
	if loader != nil {
		expenses, _ = loader.LoadMajorExpenses()
		pins, _ = loader.LoadTransactionPins()
	}

	match := majorexpenses.Match(outflows, expenses, majorexpenses.MatchOptions{Pins: pins})

	expenseByID := make(map[string]models.MajorExpense, len(expenses))
	for _, e := range expenses {
		expenseByID[e.ID] = e
	}

	type bucket struct {
		name string
		val  float64
	}
	var buckets []bucket
	for id, txns := range match.Groups {
		name := expenseByID[id].Name
		if name == "" {
			continue // expense was deleted between Match and lookup; skip
		}
		var total float64
		for _, t := range txns {
			total += t.AbsAmount()
		}
		if total > 0 {
			buckets = append(buckets, bucket{name, total})
		}
	}
	var unmatchedTotal float64
	for _, t := range match.Unmatched {
		unmatchedTotal += t.AbsAmount()
	}

	sort.Slice(buckets, func(i, j int) bool { return buckets[i].val > buckets[j].val })

	// Roll up the long tail past the donut limit into a single "Other"
	// bucket and remember which entries went into it for the breakdown.
	var rolledUp []bucket
	if len(buckets) > majorExpenseDonutLimit {
		// Copy out the rolled-up entries before mutating the head slice;
		// append-in-place can otherwise overwrite the tail's storage.
		rolledUp = append([]bucket(nil), buckets[majorExpenseDonutLimit:]...)
		var otherTotal float64
		for _, b := range rolledUp {
			otherTotal += b.val
		}
		buckets = append(buckets[:majorExpenseDonutLimit], bucket{"Other", otherTotal})
	}

	if unmatchedTotal > 0 {
		buckets = append(buckets, bucket{"Unmatched", unmatchedTotal})
	}

	labels := make([]string, len(buckets))
	values := make([]float64, len(buckets))
	var grandTotal float64
	for i, b := range buckets {
		labels[i] = b.name
		values[i] = b.val
		grandTotal += b.val
	}

	out := map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type":   "pie",
				"labels": labels,
				"values": values,
				"hole":   0.4,
			},
		},
	}

	if len(rolledUp) > 0 {
		smaller := make([]map[string]interface{}, 0, len(rolledUp))
		for _, b := range rolledUp {
			pct := 0.0
			if grandTotal > 0 {
				pct = b.val / grandTotal * 100
			}
			// One decimal for ≥ 1%, two decimals for < 1% so that a
			// 0.45% slice does not display as "0%".
			if pct < 1 {
				pct = math.Round(pct*100) / 100
			} else {
				pct = math.Round(pct*10) / 10
			}
			smaller = append(smaller, map[string]interface{}{
				"name":    b.name,
				"amount":  b.val,
				"percent": pct,
			})
		}
		out["smaller"] = smaller
	}

	return out
}
```

If `math` is not already imported in this file, add it to the import block.

- [ ] **Step 10: Run all dashboard tests to verify the new tests pass and nothing regresses**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/handlers/dashboard/ -v -count=1`

Expected: all tests PASS, including the five new `TestBuildMajorExpenseChartData_*` cases.

If anything fails, fix the implementation; do not modify the tests to make them green.

- [ ] **Step 11: Run impact detection before commit**

Run via the GitNexus MCP tool: `gitnexus_detect_changes()`
Expected: only `buildMajorExpenseChartData` (and adjacent test additions) appear as changed scope.

- [ ] **Step 12: Commit**

```bash
git add internal/handlers/dashboard/handlers.go internal/handlers/dashboard/handlers_http_test.go
git commit -m "$(cat <<'EOF'
feat(dashboard): roll up tail buckets into "Other" on major-expense donut

Keep the top 8 matched major-expense buckets as individual wedges and
collapse the rest into a single "Other" wedge so the donut stays
readable when many small buckets exist. Return the rolled-up items as
a new "smaller" field (name + amount + percent, sorted descending) for
the client to render as a text breakdown. Unmatched continues to be
appended last and is excluded from the breakdown.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Template — add breakdown sibling div

**Files:**
- Modify: `web/templates/pages/dashboard.html:132-140`

- [ ] **Step 1: Add the sibling div under the chart container**

Locate the existing block (lines 132–140 in `web/templates/pages/dashboard.html`):

```html
<!-- Spending by Major Expense -->
<div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
    <h3 class="text-lg font-semibold text-gray-800 dark:text-gray-100 mb-4">Spending by Major Expense</h3>
    <div id="chart-major-expense" class="chart-container" data-chart-url="/dashboard/charts/data/major-expense">
        <div class="flex items-center justify-center h-64 text-gray-400 dark:text-gray-500">
            Loading chart...
        </div>
    </div>
</div>
```

Replace with:

```html
<!-- Spending by Major Expense -->
<div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
    <h3 class="text-lg font-semibold text-gray-800 dark:text-gray-100 mb-4">Spending by Major Expense</h3>
    <div id="chart-major-expense" class="chart-container" data-chart-url="/dashboard/charts/data/major-expense">
        <div class="flex items-center justify-center h-64 text-gray-400 dark:text-gray-500">
            Loading chart...
        </div>
    </div>
    <div id="chart-major-expense-breakdown" class="mt-3 text-sm text-gray-600 dark:text-gray-300"></div>
</div>
```

The new sibling lives inside the same dashboard tile so it reads as part of the chart card, but outside `#chart-major-expense` so Plotly's takeover of that div does not clobber it.

- [ ] **Step 2: Run server tests to make sure templates still parse**

Run: `cd /home/darrell/bin/ai/budget2 && go test ./internal/templates/ ./internal/handlers/dashboard/ -count=1`

Expected: PASS. The template parser exercises this file via existing renderer tests; a malformed template would surface here.

- [ ] **Step 3: Commit**

```bash
git add web/templates/pages/dashboard.html
git commit -m "$(cat <<'EOF'
feat(dashboard): add breakdown slot under major-expense donut

Add a sibling div #chart-major-expense-breakdown next to the chart
container so the upcoming JS hook can render a text list of rolled-up
"Other" items below the donut without fighting Plotly for the chart
container's DOM.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Client — render the breakdown when `data.smaller` is present

**Files:**
- Modify: `web/static/js/charts.js:95-108` (extend post-render hook section in `renderChart`)

The breakdown is built using DOM APIs (`createElement` + `textContent`) — no `innerHTML`, no string templating. All untrusted text (the major-expense name) flows through `textContent` so HTML in a name renders harmlessly as text.

- [ ] **Step 1: Add the breakdown renderer hook**

In `web/static/js/charts.js`, locate the existing per-chart hook block:

```javascript
    // Render
    Plotly.newPlot(containerId, data.data, layout, config);

    // Add click handler for category pie chart
    if (containerId === 'chart-category') {
        const container = document.getElementById(containerId);
        container.on('plotly_click', function(eventData) {
            if (eventData.points && eventData.points.length > 0) {
                const category = eventData.points[0].label;
                if (category && typeof openCategoryDrilldown === 'function') {
                    openCategoryDrilldown(category);
                }
            }
        });
    }
}
```

Insert a sibling hook for `chart-major-expense` directly after the existing `chart-category` hook (still before the closing brace of `renderChart`):

```javascript
    // Render the "Other" breakdown for the major-expense donut.
    if (containerId === 'chart-major-expense') {
        renderMajorExpenseBreakdown(data.smaller);
    }
}
```

Then add the helper function below `renderChart` (above `renderSparkline`):

```javascript
/**
 * Render the text breakdown of rolled-up "Other" major-expense items
 * into the breakdown sibling div. Clears the div when no items.
 * Built with DOM APIs and textContent — never innerHTML — so any HTML
 * inside a major-expense name renders as plain text.
 * @param {Array<{name: string, amount: number, percent: number}>} items
 */
function renderMajorExpenseBreakdown(items) {
    const target = document.getElementById('chart-major-expense-breakdown');
    if (!target) return;

    // Always clear first so the empty-state path also resets prior content.
    while (target.firstChild) {
        target.removeChild(target.firstChild);
    }

    if (!items || items.length === 0) {
        return;
    }

    const fmtMoney = new Intl.NumberFormat('en-US', {
        style: 'currency',
        currency: 'USD',
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
    });

    const header = document.createElement('div');
    header.className = 'font-medium text-gray-700 dark:text-gray-200 mb-1';
    header.textContent = 'Other categories';
    target.appendChild(header);

    items.forEach(function(it) {
        const row = document.createElement('div');
        row.className = 'flex justify-between gap-4 py-0.5';

        const nameEl = document.createElement('span');
        nameEl.className = 'truncate';
        nameEl.textContent = String(it.name);
        row.appendChild(nameEl);

        const valEl = document.createElement('span');
        valEl.className = 'tabular-nums whitespace-nowrap';
        // Amount text node + a small spacer + a styled percent span.
        valEl.appendChild(document.createTextNode(fmtMoney.format(it.amount) + '  '));

        const pctEl = document.createElement('span');
        pctEl.className = 'text-gray-500 dark:text-gray-400';
        const pctNum = Number(it.percent);
        const pctText = (pctNum < 1 ? pctNum.toFixed(2) : pctNum.toFixed(1)) + '%';
        pctEl.textContent = pctText;
        valEl.appendChild(pctEl);

        row.appendChild(valEl);
        target.appendChild(row);
    });
}
```

- [ ] **Step 2: Build/start the dev server**

Run: `cd /home/darrell/bin/ai/budget2 && make run` (or whichever command starts the dev server in this repo — check the Makefile if unsure with `grep -E '^run:|^dev:' Makefile`).

Expected: server starts on its usual port without errors.

- [ ] **Step 3: Manually verify the chart in the browser**

Open the dashboard in a browser. Confirm:

1. With the user's real data (which currently has > 8 major-expense buckets), the donut shows 8 main wedges plus an "Other" wedge.
2. A text list with the header "Other categories" appears directly below the donut, listing the rolled-up items with name, formatted amount (e.g. `$213.45`), and percent.
3. Items are sorted descending by amount.
4. The breakdown reads clearly in both light and dark themes (toggle via the existing theme switch).
5. Resize the window to a narrow width and confirm rows truncate cleanly without breaking the card layout.

If the user's dataset has ≤ 8 buckets in the current date filter, narrow the date range so > 8 buckets are matched (or temporarily widen it to "All"). To verify the empty-state path, narrow the filter further until ≤ 8 buckets remain — the breakdown div must be visually empty (no header, no rows).

- [ ] **Step 4: Commit**

```bash
git add web/static/js/charts.js
git commit -m "$(cat <<'EOF'
feat(dashboard): render "Other" breakdown under major-expense donut

When the major-expense chart payload includes a "smaller" array, render
a small two-column list (name | amount + percent) below the donut into
#chart-major-expense-breakdown. Clears the slot when smaller is empty
or absent. Built with DOM APIs + textContent only — never innerHTML —
so a major-expense name containing HTML renders as plain text.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Verification before completion

After Task 3 is committed:

- [ ] **Step 1: Run the full check target**

Run: `cd /home/darrell/bin/ai/budget2 && make check`

Expected: PASS (vet, staticcheck, govulncheck, full test suite).

- [ ] **Step 2: Confirm scope of changes**

Run via the GitNexus MCP tool: `gitnexus_detect_changes()`
Expected: changes affect only `buildMajorExpenseChartData`, the dashboard template, the charts JS, and the new tests. Anything else is a surprise — investigate before claiming done.

- [ ] **Step 3: Report back to the user**

Summarize: what shipped (rollup + breakdown), what was verified (5 new server tests + browser smoke test), and what was deliberately left alone (the `Dog`/`dog` duplicate-bucket data issue called out in the spec as out of scope).
