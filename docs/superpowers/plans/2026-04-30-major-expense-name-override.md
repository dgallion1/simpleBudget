# Major Expense Names Override Bank Descriptions — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make user-curated Major Expense names override bank-given transaction descriptions across all displays, aggregations, search, and transaction-level exports — with the per-transaction `DisplayName` alias still winning when set.

**Architecture:** Add a derived field `MajorExpenseName` to `Transaction`, stamped at load time by a new `applyMajorExpenseNames` step in the dataloader (analogous to existing `applyAliases`). Add a `Label()` method that returns `DisplayName → MajorExpenseName → Description`. Templates, search, and aggregations switch from `Description` to `Label()`. Keep the engine's matching logic single-sourced by exporting it.

**Tech Stack:** Go 1.x, html/template, chi router, file-based JSON storage. Tests use the standard `testing` package and the existing `setupTestDir` helper in `internal/services/dataloader/coverage_test.go`.

---

## File Structure

| File | Role | Status |
|---|---|---|
| `internal/models/transaction.go` | New `MajorExpenseName` field + `Label()` method; `FilterBySearch` includes label | Modify |
| `internal/models/transaction_test.go` | Unit tests for `Label()` and `FilterBySearch` updates | Modify |
| `internal/services/majorexpenses/engine.go` | Export `matchTransaction` as `MatchTransaction` | Modify |
| `internal/services/dataloader/major_expense_names.go` | New `applyMajorExpenseNames` function | Create |
| `internal/services/dataloader/major_expense_names_test.go` | Unit tests for the new function | Create |
| `internal/services/dataloader/loader.go` | Wire new step into `LoadData()` | Modify |
| `internal/handlers/dashboard/handlers.go:951` | Top-merchants aggregation by `Label()` | Modify |
| `web/templates/pages/explorer.html` | Description column uses `Label`; bank text in parens | Modify |
| `web/templates/pages/major-expenses.html` | 3 sites: matched table, exception rows, anomaly rows | Modify |
| `web/templates/pages/insights.html` | Subscription/recurring/top-merchants/top-income lists | Modify |
| `web/templates/components/category-drilldown.html` | Description column | Modify |

---

## Task 1: Add `MajorExpenseName` field and `Label()` method

**Files:**
- Modify: `internal/models/transaction.go`
- Test: `internal/models/transaction_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/models/transaction_test.go`:

```go
func TestTransaction_Label(t *testing.T) {
	tests := []struct {
		name string
		txn  Transaction
		want string
	}{
		{
			name: "DisplayName wins over both",
			txn:  Transaction{Description: "BANK", MajorExpenseName: "Mortgage", DisplayName: "Mort."},
			want: "Mort.",
		},
		{
			name: "MajorExpenseName beats Description",
			txn:  Transaction{Description: "BANK", MajorExpenseName: "Mortgage"},
			want: "Mortgage",
		},
		{
			name: "Description fallback when nothing else",
			txn:  Transaction{Description: "BANK"},
			want: "BANK",
		},
		{
			name: "all empty returns empty",
			txn:  Transaction{},
			want: "",
		},
		{
			name: "DisplayName wins even when only it is set",
			txn:  Transaction{DisplayName: "Friendly"},
			want: "Friendly",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.txn.Label(); got != tc.want {
				t.Errorf("Label() = %q, want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/models -run TestTransaction_Label -v`
Expected: FAIL — `Transaction has no field or method Label`

- [ ] **Step 3: Add the field and method**

In `internal/models/transaction.go`, add `MajorExpenseName` to the `Transaction` struct (immediately after `DisplayName`):

```go
DisplayName       string `json:"display_name,omitempty"`        // User-assigned alias
MajorExpenseName  string `json:"major_expense_name,omitempty"`  // Derived; stamped at load time, not persisted to source CSVs
```

Then add the `Label` method (place it near `AbsAmount`):

```go
// Label returns the user-facing name for a transaction.
// Precedence: DisplayName (per-txn alias) -> MajorExpenseName (group name)
// -> Description (bank text). Set on display, search, aggregation, and
// transaction-level export sites so the user-curated name always wins
// over the bank's text when one is available.
func (t Transaction) Label() string {
	switch {
	case t.DisplayName != "":
		return t.DisplayName
	case t.MajorExpenseName != "":
		return t.MajorExpenseName
	default:
		return t.Description
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/models -run TestTransaction_Label -v`
Expected: PASS (5 subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/models/transaction.go internal/models/transaction_test.go
git commit -m "feat(models): add Transaction.Label() and MajorExpenseName field"
```

---

## Task 2: Extend `FilterBySearch` to match `MajorExpenseName`

**Files:**
- Modify: `internal/models/transaction.go:122-132`
- Test: `internal/models/transaction_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/models/transaction_test.go`:

```go
func TestTransactionSet_FilterBySearch_MatchesMajorExpenseName(t *testing.T) {
	ts := NewTransactionSet([]Transaction{
		{Description: "BOFA HOMELOANS 0123", MajorExpenseName: "Mortgage"},
		{Description: "Whole Foods", MajorExpenseName: "Groceries"},
		{Description: "Starbucks"},
	})
	got := ts.FilterBySearch("mortgage")
	if got.Len() != 1 {
		t.Fatalf("expected 1 match, got %d", got.Len())
	}
	if got.Transactions[0].Description != "BOFA HOMELOANS 0123" {
		t.Errorf("wrong row matched: %q", got.Transactions[0].Description)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/models -run TestTransactionSet_FilterBySearch_MatchesMajorExpenseName -v`
Expected: FAIL — search currently only checks `Description` and `DisplayName`.

- [ ] **Step 3: Update `FilterBySearch`**

Replace the body of `FilterBySearch` in `internal/models/transaction.go`:

```go
// FilterBySearch returns transactions matching the search term in
// description, display name, or major-expense name.
func (ts *TransactionSet) FilterBySearch(search string) *TransactionSet {
	result := &TransactionSet{}
	searchLower := strings.ToLower(search)
	for _, t := range ts.Transactions {
		if strings.Contains(strings.ToLower(t.Description), searchLower) ||
			(t.DisplayName != "" && strings.Contains(strings.ToLower(t.DisplayName), searchLower)) ||
			(t.MajorExpenseName != "" && strings.Contains(strings.ToLower(t.MajorExpenseName), searchLower)) {
			result.Transactions = append(result.Transactions, t)
		}
	}
	return result
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/models -run TestTransactionSet_FilterBySearch -v`
Expected: PASS (existing search tests still pass + new one passes).

- [ ] **Step 5: Commit**

```bash
git add internal/models/transaction.go internal/models/transaction_test.go
git commit -m "feat(models): FilterBySearch matches MajorExpenseName"
```

---

## Task 3: Export `matchTransaction` from majorexpenses package

**Files:**
- Modify: `internal/services/majorexpenses/engine.go`
- Modify: `internal/services/majorexpenses/engine_test.go` (only if it references the unexported name)

- [ ] **Step 1: Rename the function and update internal call sites**

In `internal/services/majorexpenses/engine.go`:

1. Rename `matchTransaction` to `MatchTransaction` (capital M) at its definition (line ~154).
2. Update the doc comment's first word from `matchTransaction` to `MatchTransaction`.
3. Update both internal call sites: `Match` (line 73) and `AnnotateRecurringPayments` (line 121).

```go
// Inside Match:
if id, ok := MatchTransaction(t, defs); ok {

// Inside AnnotateRecurringPayments:
if id, ok := MatchTransaction(first, defs); ok {
```

- [ ] **Step 2: Update tests that reference the old name (if any)**

Run: `grep -rn "matchTransaction" /home/darrell/bin/ai/budget2/internal --include="*.go"`
Expected: only mentions in updated comments, no callers. If any test file uses `matchTransaction`, rename to `MatchTransaction`.

- [ ] **Step 3: Run the package tests**

Run: `go test ./internal/services/majorexpenses/... -v`
Expected: all pass (pure rename, behavior unchanged).

- [ ] **Step 4: Commit**

```bash
git add internal/services/majorexpenses
git commit -m "refactor(majorexpenses): export MatchTransaction"
```

---

## Task 4: Add `applyMajorExpenseNames` in dataloader

**Files:**
- Create: `internal/services/dataloader/major_expense_names.go`
- Test: `internal/services/dataloader/major_expense_names_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/services/dataloader/major_expense_names_test.go`:

```go
package dataloader

import (
	"encoding/json"
	"testing"

	"budget2/internal/models"
)

func TestApplyMajorExpenseNames_NoFiles(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, nil)
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "BOFA HOMELOANS 0123", Amount: -1500},
	}
	got := loader.applyMajorExpenseNames(txns)
	if got[0].MajorExpenseName != "" {
		t.Errorf("expected empty MajorExpenseName when no defs file; got %q", got[0].MajorExpenseName)
	}
}

func TestApplyMajorExpenseNames_KeywordMatch(t *testing.T) {
	defs := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{ID: "me1", Name: "Mortgage", Keywords: []string{"homeloans"}},
	}}
	defsJSON, _ := json.Marshal(defs)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json": string(defsJSON),
	})
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "BOFA HOMELOANS 0123", Amount: -1500},
		{Hash: "h2", Description: "Whole Foods", Amount: -42},
	}
	got := loader.applyMajorExpenseNames(txns)
	if got[0].MajorExpenseName != "Mortgage" {
		t.Errorf("expected MajorExpenseName 'Mortgage'; got %q", got[0].MajorExpenseName)
	}
	if got[1].MajorExpenseName != "" {
		t.Errorf("expected empty MajorExpenseName for unmatched; got %q", got[1].MajorExpenseName)
	}
}

func TestApplyMajorExpenseNames_PinOverridesMatching(t *testing.T) {
	defs := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"homeloans"}},
		{ID: "me-rare", Name: "Rare Pin Target"},
	}}
	defsJSON, _ := json.Marshal(defs)
	pins := map[string]string{"h1": "me-rare"}
	pinsJSON, _ := json.Marshal(pins)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json":   string(defsJSON),
		"transaction_pins.json": string(pinsJSON),
	})
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "BOFA HOMELOANS 0123", Amount: -1500},
	}
	got := loader.applyMajorExpenseNames(txns)
	if got[0].MajorExpenseName != "Rare Pin Target" {
		t.Errorf("pin should override keyword match; got %q", got[0].MajorExpenseName)
	}
}

func TestApplyMajorExpenseNames_PinToDeletedExpenseFallsThrough(t *testing.T) {
	defs := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{ID: "me-mortgage", Name: "Mortgage", Keywords: []string{"homeloans"}},
	}}
	defsJSON, _ := json.Marshal(defs)
	pins := map[string]string{"h1": "deleted-id"}
	pinsJSON, _ := json.Marshal(pins)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json":   string(defsJSON),
		"transaction_pins.json": string(pinsJSON),
	})
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "BOFA HOMELOANS 0123", Amount: -1500},
	}
	got := loader.applyMajorExpenseNames(txns)
	if got[0].MajorExpenseName != "Mortgage" {
		t.Errorf("expected fall-through to keyword match 'Mortgage'; got %q", got[0].MajorExpenseName)
	}
}

func TestApplyMajorExpenseNames_RangeAmount(t *testing.T) {
	defs := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{ID: "me-rent", Name: "Rent", ExpectedMin: 1000, ExpectedMax: 2000},
	}}
	defsJSON, _ := json.Marshal(defs)

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json": string(defsJSON),
	})
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "Anything", Amount: -1500},
		{Hash: "h2", Description: "Anything", Amount: -50},
	}
	got := loader.applyMajorExpenseNames(txns)
	if got[0].MajorExpenseName != "Rent" {
		t.Errorf("expected 'Rent' for in-range amount; got %q", got[0].MajorExpenseName)
	}
	if got[1].MajorExpenseName != "" {
		t.Errorf("expected empty for out-of-range amount; got %q", got[1].MajorExpenseName)
	}
}

func TestApplyMajorExpenseNames_InvalidJSONFallsBack(t *testing.T) {
	_, loader, cleanup := setupTestDir(t, map[string]string{
		"major_expenses.json": "broken json{{{",
	})
	defer cleanup()

	txns := []models.Transaction{
		{Hash: "h1", Description: "Whatever", Amount: -10},
	}
	got := loader.applyMajorExpenseNames(txns)
	if len(got) != 1 || got[0].MajorExpenseName != "" {
		t.Error("expected empty MajorExpenseName when defs file is corrupt (graceful no-op)")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/services/dataloader -run TestApplyMajorExpenseNames -v`
Expected: FAIL — `loader.applyMajorExpenseNames undefined`.

- [ ] **Step 3: Implement `applyMajorExpenseNames`**

Create `internal/services/dataloader/major_expense_names.go`:

```go
package dataloader

import (
	"log"

	"budget2/internal/models"
	"budget2/internal/services/majorexpenses"
)

// applyMajorExpenseNames stamps Transaction.MajorExpenseName on every
// transaction that maps to a declared major expense — either via an
// explicit user pin or by keyword/amount matching. Mirrors the logic
// used by services/majorexpenses.Match so the per-transaction label
// matches the group the Major Expenses page would put the row in.
//
// Failure modes are non-fatal: if either the defs file or the pins file
// is missing or unreadable, we log and return the input unchanged. This
// makes the feature opt-in: users who haven't configured Major Expenses
// see no behavior change.
func (dl *DataLoader) applyMajorExpenseNames(transactions []models.Transaction) []models.Transaction {
	defs, err := dl.LoadMajorExpenses()
	if err != nil {
		log.Printf("Warning: could not load major expenses for label stamping: %v", err)
		return transactions
	}
	if len(defs) == 0 {
		return transactions
	}

	pins, err := dl.LoadTransactionPins()
	if err != nil {
		log.Printf("Warning: could not load transaction pins for label stamping: %v", err)
		pins = nil
	}

	nameByID := make(map[string]string, len(defs))
	validIDs := make(map[string]bool, len(defs))
	for _, d := range defs {
		nameByID[d.ID] = d.Name
		validIDs[d.ID] = true
	}

	for i := range transactions {
		t := transactions[i]
		// Pin wins when it points to an existing expense.
		if pins != nil && t.Hash != "" {
			if id, ok := pins[t.Hash]; ok && validIDs[id] {
				transactions[i].MajorExpenseName = nameByID[id]
				continue
			}
		}
		// Otherwise fall back to keyword/amount matching.
		if id, ok := majorexpenses.MatchTransaction(t, defs); ok {
			transactions[i].MajorExpenseName = nameByID[id]
		}
	}
	return transactions
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/services/dataloader -run TestApplyMajorExpenseNames -v`
Expected: PASS (6 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/services/dataloader/major_expense_names.go internal/services/dataloader/major_expense_names_test.go
git commit -m "feat(dataloader): applyMajorExpenseNames stamps labels at load time"
```

---

## Task 5: Wire `applyMajorExpenseNames` into `LoadData()`

**Files:**
- Modify: `internal/services/dataloader/loader.go:165-173`
- Test: `internal/services/dataloader/major_expense_names_test.go`

- [ ] **Step 1: Write the failing integration test**

Append to `internal/services/dataloader/major_expense_names_test.go`:

```go
func TestLoadData_StampsMajorExpenseNames(t *testing.T) {
	defs := models.MajorExpenseStore{Expenses: []models.MajorExpense{
		{ID: "me1", Name: "Mortgage", Keywords: []string{"homeloans"}},
	}}
	defsJSON, _ := json.Marshal(defs)

	csv := "Date,Description,Amount\n2024-01-15,BOFA HOMELOANS 0123,-1500\n2024-01-16,Whole Foods,-42\n"

	_, loader, cleanup := setupTestDir(t, map[string]string{
		"transactions.csv":    csv,
		"major_expenses.json": string(defsJSON),
	})
	defer cleanup()

	ts, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData failed: %v", err)
	}
	if ts.Len() != 2 {
		t.Fatalf("expected 2 transactions, got %d", ts.Len())
	}

	var mortgage, groceries models.Transaction
	for _, txn := range ts.Transactions {
		switch txn.Description {
		case "BOFA HOMELOANS 0123":
			mortgage = txn
		case "Whole Foods":
			groceries = txn
		}
	}
	if mortgage.MajorExpenseName != "Mortgage" {
		t.Errorf("mortgage row: got MajorExpenseName=%q, want %q", mortgage.MajorExpenseName, "Mortgage")
	}
	if groceries.MajorExpenseName != "" {
		t.Errorf("unmatched row: got MajorExpenseName=%q, want empty", groceries.MajorExpenseName)
	}
	if mortgage.Label() != "Mortgage" {
		t.Errorf("mortgage Label(): got %q, want %q", mortgage.Label(), "Mortgage")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/services/dataloader -run TestLoadData_StampsMajorExpenseNames -v`
Expected: FAIL — `MajorExpenseName` is empty on the matched row because the load pipeline doesn't yet call `applyMajorExpenseNames`.

- [ ] **Step 3: Wire the new step into `LoadData`**

In `internal/services/dataloader/loader.go`, locate this block (around line 167-173):

```go
	// Apply user-assigned aliases
	allTransactions = dl.applyAliases(allTransactions)

	// Compute derived fields
	for i := range allTransactions {
		allTransactions[i].ComputeDerivedFields()
	}
```

Insert the new step between `applyAliases` and the derived-field loop:

```go
	// Apply user-assigned aliases
	allTransactions = dl.applyAliases(allTransactions)

	// Stamp MajorExpenseName based on user-defined major expenses + pins
	allTransactions = dl.applyMajorExpenseNames(allTransactions)

	// Compute derived fields
	for i := range allTransactions {
		allTransactions[i].ComputeDerivedFields()
	}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/services/dataloader -run TestLoadData_StampsMajorExpenseNames -v`
Expected: PASS.

- [ ] **Step 5: Run the full dataloader suite to catch regressions**

Run: `go test ./internal/services/dataloader/...`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/services/dataloader/loader.go internal/services/dataloader/major_expense_names_test.go
git commit -m "feat(dataloader): wire MajorExpenseName stamping into LoadData"
```

---

## Task 6: Update Explorer transaction-row template to use `Label`

**Files:**
- Modify: `web/templates/pages/explorer.html:618-619`

- [ ] **Step 1: Read the current row markup**

Lines 618-619 currently render:

```html
<td class="p-3 text-sm text-gray-800 dark:text-gray-200 truncate" data-hash="{{.Hash}}" data-description="{{.Description}}" data-display-name="{{.DisplayName}}">
    <span class="alias-display cursor-pointer hover:text-indigo-600 dark:hover:text-indigo-400" onclick="filterByDescription('{{js (or .DisplayName .Description)}}')" title="Click to filter, double-click to rename">{{if .DisplayName}}<span class="font-medium">{{.DisplayName}}</span> <span class="text-xs text-gray-400 dark:text-gray-500">({{.Description}})</span>{{else}}{{.Description}}{{end}}</span>
</td>
```

- [ ] **Step 2: Replace with `Label`-aware markup**

Replace the entire `<td>` block at lines 618-619 with:

```html
<td class="p-3 text-sm text-gray-800 dark:text-gray-200 truncate" data-hash="{{.Hash}}" data-description="{{.Description}}" data-display-name="{{.DisplayName}}" data-major-expense-name="{{.MajorExpenseName}}">
    <span class="alias-display cursor-pointer hover:text-indigo-600 dark:hover:text-indigo-400" onclick="filterByDescription('{{js .Label}}')" title="Click to filter, double-click to rename">{{if ne .Label .Description}}<span class="font-medium">{{.Label}}</span> <span class="text-xs text-gray-400 dark:text-gray-500">({{.Description}})</span>{{else}}{{.Description}}{{end}}</span>
</td>
```

This shows `Label (Description)` whenever the resolved label differs from the bank text — covering both `DisplayName` aliases and Major Expense matches in one branch.

- [ ] **Step 3: Render-time smoke test**

Run: `go test ./internal/templates/...`
Expected: pass. If a render test asserts on the old "DisplayName-only or Description" markup, update its expected substring to match the new branch.

- [ ] **Step 4: Run the server, eyeball the explorer**

Run: `go run ./cmd/server` in a terminal, open `http://localhost:8080/explorer`. Verify:
- Rows that match a Major Expense show `MajorName (BANK TEXT)`.
- Rows with a DisplayName alias still show `Alias (BANK TEXT)` (DisplayName wins).
- Rows with neither show plain bank text.

- [ ] **Step 5: Commit**

```bash
git add web/templates/pages/explorer.html
git commit -m "feat(explorer): description column uses Label() with bank text in parens"
```

---

## Task 7: Update Major Expenses page templates (3 sites)

**Files:**
- Modify: `web/templates/pages/major-expenses.html` lines 300, 461, 503, 553

- [ ] **Step 1: Read the four sites**

Each is a slightly different snippet of the `DisplayName ?? Description` pattern. Locate them with:

Run: `grep -n 'DisplayName' /home/darrell/bin/ai/budget2/web/templates/pages/major-expenses.html`
Expected output: four matches at lines 300, 461, 503, 553.

- [ ] **Step 2: Replace each site with `.Label` (or `.Transaction.Label` where applicable)**

Line 300 (matched-transactions table row, inside `range .Transactions`):

```
{{if and $pinned (index $pinned .Hash)}}<span class="text-amber-600 dark:text-amber-400" title="Pinned manually">📌</span> {{end}}{{.Label}}
```

Line 461 (exception row — note the `.Transaction.` prefix because we're inside an exception struct):

```html
{{$label := .Transaction.Label}}
```
Replace the existing `{{$label := .Transaction.Description}}{{if .Transaction.DisplayName}}{{$label = .Transaction.DisplayName}}{{end}}` two-statement init with the single `{{$label := .Transaction.Label}}`.

Line 503 (anomaly row): same pattern — replace `{{$desc := .Transaction.Description}}{{if .Transaction.DisplayName}}{{$desc = .Transaction.DisplayName}}{{end}}` with `{{$desc := .Transaction.Label}}`.

Line 553 (whichever third use): same `{{$label := .Transaction.Label}}` substitution.

- [ ] **Step 3: Verify only `Label`-style references remain in the file**

Run: `grep -n 'DisplayName\|\.Description' /home/darrell/bin/ai/budget2/web/templates/pages/major-expenses.html | grep -v 'data-description'`
Expected: only references that are intentionally still showing the raw bank text (e.g., a "(bank text)" parenthetical). If any of the four removed sites still appear, repeat Step 2.

- [ ] **Step 4: Run the major-expenses render tests**

Run: `go test ./internal/templates -run TestRenderMajorExpenses -v`
Expected: PASS. If assertions reference the old strings, update the expected substrings to use the new label.

- [ ] **Step 5: Commit**

```bash
git add web/templates/pages/major-expenses.html internal/templates
git commit -m "feat(major-expenses): use Label() for matched/exception/anomaly rows"
```

---

## Task 8: Update Insights templates (subscriptions, recurring, top merchants, top income)

**Files:**
- Modify: `web/templates/pages/insights.html` lines 173-180, 252, 316, 627, 719

- [ ] **Step 1: Distinguish Transaction-shaped sites from RecurringPayment/IncomePattern-shaped sites**

The `.Description` references in `insights.html` come from two different data shapes:

1. `RecurringPayment` / `IncomePattern` structs — these have a `.MajorExpenseName` field (annotated by `engine.AnnotateRecurringPayments`) and a `.Description`. They do **not** have `.Label` because they're not Transactions.
2. `Transaction` shapes (e.g., line 627 inside the "missed merchants" block) — these get `.Label`.

For shape 1, render `MajorExpenseName ?? Description` directly in the template.

For shape 2, use `.Label`.

- [ ] **Step 2: Update RecurringPayment / IncomePattern rows (lines 173-180, 252, 316, 719)**

For each of those rows, replace `{{.Description}}` (where it's the primary visible label) with the conditional:

```html
{{if .MajorExpenseName}}{{.MajorExpenseName}}{{else if .Description}}{{.Description}}{{else}}Unlabeled subscription{{end}}
```

Specifically:

- Line 180 (the `<p>` with subscription title): replace
  `{{if .Description}}{{.Description}}{{else}}Unlabeled subscription{{end}}`
  with the three-way conditional above.
- Line 252 (recurring outflow `<span>`): replace `{{.Description}}` with `{{if .MajorExpenseName}}{{.MajorExpenseName}}{{else}}{{.Description}}{{end}}`. Keep the existing `title="{{.Description}}"` so the bank text shows on hover.
- Line 316 (top income row): same pattern as line 252.
- Line 719: same pattern as line 252.

Keep the existing `{{if .MajorExpenseName}}<span>{{.MajorExpenseName}}</span>{{end}}` chip after the description on lines 253 and 628 — but since the chip would now duplicate the primary label, **remove the chip in those two spots**.

- [ ] **Step 3: Update Transaction-shape sites (line 627)**

Line 627 is inside a list of raw transactions ("missed/anomaly merchants"). Replace `{{.Description}}` with `{{.Label}}`.

- [ ] **Step 4: Search/filter URL parameters keep using Description**

The `onclick="...search={{urlquery .Description}}..."` patterns navigate to Explorer with a search query. After Task 2, `FilterBySearch` matches `MajorExpenseName` too, so passing the displayed label works. But for RecurringPayment-shaped rows we now display `MajorExpenseName ?? Description` — pass the same value to the search:

In each `onclick` containing `urlquery .Description`, change to:

```html
onclick="window.location.href='/explorer?search={{urlquery (or .MajorExpenseName .Description)}}&type=Outflow'"
```

(Use `Outflow` or `Income` to match the existing param.)

- [ ] **Step 5: Run insights tests**

Run: `go test ./internal/handlers/insights/... ./internal/templates/...`
Expected: PASS. Update render-test fixtures if they assert on the old strings.

- [ ] **Step 6: Commit**

```bash
git add web/templates/pages/insights.html internal/templates
git commit -m "feat(insights): use Major Expense names for subscriptions, recurring, top merchants"
```

---

## Task 9: Update Dashboard top-merchants aggregation

**Files:**
- Modify: `internal/handlers/dashboard/handlers.go:945-970`
- Test: `internal/handlers/dashboard/handlers_http_test.go` (add)

- [ ] **Step 1: Write the failing test**

Append to `internal/handlers/dashboard/handlers_http_test.go`:

```go
func TestBuildMerchantsChartData_AggregatesByLabel(t *testing.T) {
	ts := models.NewTransactionSet([]models.Transaction{
		{Description: "BOFA HOMELOANS 0123", MajorExpenseName: "Mortgage", Amount: -1500, TransactionType: models.Outflow},
		{Description: "BOFA HOMELOANS 0124", MajorExpenseName: "Mortgage", Amount: -1500, TransactionType: models.Outflow},
		{Description: "Whole Foods", Amount: -42, TransactionType: models.Outflow},
	})

	chart := buildMerchantsChartData(ts)
	labels, ok := chart["labels"].([]string)
	if !ok {
		t.Fatalf("chart[labels] not []string: %T", chart["labels"])
	}

	var sawMortgage bool
	mortgageCount := 0
	for _, l := range labels {
		if l == "Mortgage" {
			sawMortgage = true
			mortgageCount++
		}
	}
	if !sawMortgage {
		t.Errorf("expected 'Mortgage' label, got %v", labels)
	}
	if mortgageCount > 1 {
		t.Errorf("expected 'Mortgage' to appear once (rolled up), saw %d times", mortgageCount)
	}
}
```

(If `buildMerchantsChartData` returns the chart in a different shape — e.g., a struct instead of a map — adapt the assertions to that shape; the spirit of the test is "two BOFA HOMELOANS rows roll up into one Mortgage entry".)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/handlers/dashboard -run TestBuildMerchantsChartData_AggregatesByLabel -v`
Expected: FAIL — both BOFA rows currently appear as separate keys.

- [ ] **Step 3: Update the aggregation key**

In `internal/handlers/dashboard/handlers.go` around line 951, change:

```go
merchantTotals[t.Description] += math.Abs(t.Amount)
```

to:

```go
merchantTotals[t.Label()] += math.Abs(t.Amount)
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/handlers/dashboard -run TestBuildMerchantsChartData_AggregatesByLabel -v`
Expected: PASS.

- [ ] **Step 5: Run the dashboard suite**

Run: `go test ./internal/handlers/dashboard/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/handlers/dashboard/handlers.go internal/handlers/dashboard/handlers_http_test.go
git commit -m "feat(dashboard): top-merchants rolls up by Label"
```

---

## Task 10: Update Category Drilldown component

**Files:**
- Modify: `web/templates/components/category-drilldown.html:44`

- [ ] **Step 1: Replace the description cell**

Line 44 currently is:

```html
<td class="p-3 text-sm text-gray-800 dark:text-gray-200">{{.Description}}</td>
```

Replace with:

```html
<td class="p-3 text-sm text-gray-800 dark:text-gray-200" title="{{.Description}}">{{.Label}}</td>
```

- [ ] **Step 2: Run template tests**

Run: `go test ./internal/templates/...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add web/templates/components/category-drilldown.html
git commit -m "feat(category-drilldown): description cell uses Label"
```

---

## Task 11: Final verification

**Files:** none — this task is verification only.

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: all packages PASS.

- [ ] **Step 2: Run vet and staticcheck**

Run: `go vet ./... && staticcheck ./...`
Expected: clean.

- [ ] **Step 3: Manual UI walk-through**

Run: `go run ./cmd/server`, open `http://localhost:8080`. Click through:

- **Dashboard** → Top Merchants chart shows curated names, multiple bank-text variants of the same merchant collapse into one bar.
- **Explorer** → description column shows `MajorName (BANK TEXT)` for matched rows; `Alias (BANK TEXT)` for renamed rows; bare bank text for unmatched.
- **Major Expenses** → matched-transactions sub-table, exception rows, and anomaly rows all show curated names.
- **Insights** → subscriptions and recurring lists show curated names; top merchants list shows curated names.
- **Search** in Explorer → typing a Major Expense name (e.g., "Mortgage") finds rows whose bank text doesn't contain the word.

- [ ] **Step 4: Commit any final tweaks**

If the manual walk-through surfaces anything, fix and commit. Otherwise this task is done.

---

## Self-Review (already run)

**Spec coverage:** every spec section has at least one task (Label resolution = Task 1; load-time stamping = Tasks 4-5; templates = Tasks 6-8 + 10; aggregation = Task 9; search = Task 2; export policy = no code, in spec). ✅

**Placeholder scan:** no TBD/TODO. Every code step shows the actual code. ✅

**Type consistency:** field name `MajorExpenseName` and method `Label()` used identically in all tasks. Exported function `MatchTransaction` used by the new dataloader code in Task 4 matches what Task 3 produces. ✅
