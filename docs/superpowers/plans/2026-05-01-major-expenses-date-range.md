# Major Expenses — Date Range Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a server-side date range filter to `/major-expenses` so the user can scope per-expense rollups and exception buckets to a window — mirroring Explorer's date-range pattern (two `<input type="date">` controls + `[←] 3M | 6M | 12M | All [→]` quick-buttons + sessionStorage persistence).

**Architecture:** `buildPageData` in `internal/handlers/majorexpenses/handlers.go` takes `*http.Request`, resolves `start`/`end` against the full-data MinDate/MaxDate, and applies `FilterByDateRange` before `Match()`. The matching engine is untouched; it just receives a smaller transaction set. The page-level handler detects HTMX target headers and renders either the full base layout or just the results wrapper partial. Mutation handlers (add/update/delete/pin/unpin/bulk-pin) thread the active window through `hx-include` so post-mutation re-renders preserve the user's filter.

**Tech Stack:** Go (`net/http`, `chi/v5`), `html/template`, HTMX, plain ES5 JS, Tailwind. Tests in Go (`internal/handlers/majorexpenses/handlers_test.go`).

**Spec:** `docs/superpowers/specs/2026-05-01-major-expenses-date-range-design.md`

---

## File Map

- **Modify** `internal/handlers/majorexpenses/handlers.go` — add `parseRangeFromRequest`, `formatDateInputValue` helpers; change `buildPageData` signature to accept `*http.Request`; add HTMX target detection in `handleMajorExpensesPage`; thread the window through every handler.
- **Modify** `internal/handlers/majorexpenses/handlers_test.go` — update existing `TestBuildPageData_IncomeNotIncludedInGroups` (one call site to fix), add 9 new tests, add `setupTestEnvWithRenderer` helper.
- **Modify** `web/templates/pages/major-expenses.html` — add filter card markup; add `major-expenses-results-wrapper` template definition that composes both cards; port `setDateRange` / `stepDateRange` / `detectSelectedDateRange` JS; add sessionStorage persistence; add `hx-include="#major-expenses-filter-form"` on every mutation form/button; update bulk-pin `htmx.ajax` call to forward `start`/`end`.

No other files touched. No new packages. No new endpoints — this rides existing routes.

---

## Task 1: Add `parseRangeFromRequest` helper (TDD)

**Files:**
- Modify: `internal/handlers/majorexpenses/handlers.go` (add helper at bottom of file, before `renderError`)
- Test: `internal/handlers/majorexpenses/handlers_test.go` (add `TestParseRangeFromRequest`)

The helper resolves the active window from a request: URL query params first, form values second, fallback to `txns.MinDate()` / `txns.MaxDate()` when missing or unparseable.

- [ ] **Step 1: Write the failing test**

Append to `internal/handlers/majorexpenses/handlers_test.go`:

```go
func TestParseRangeFromRequest(t *testing.T) {
	// Build a small TransactionSet so MinDate/MaxDate are deterministic.
	txns := &models.TransactionSet{Transactions: []models.Transaction{
		{Date: time.Date(2023, 1, 15, 0, 0, 0, 0, time.UTC), Amount: -10, TransactionType: models.Outflow},
		{Date: time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC), Amount: -20, TransactionType: models.Outflow},
	}}
	min := txns.MinDate()
	max := txns.MaxDate()
	mustParse := func(s string) time.Time {
		t.Helper()
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return v
	}

	cases := []struct {
		name             string
		req              *http.Request
		wantStart, wantEnd time.Time
	}{
		{
			name:      "url query parses both",
			req:       httptest.NewRequest("GET", "/major-expenses?start=2024-01-01&end=2024-12-31", nil),
			wantStart: mustParse("2024-01-01"),
			wantEnd:   mustParse("2024-12-31"),
		},
		{
			name:      "missing both falls back to MinDate/MaxDate",
			req:       httptest.NewRequest("GET", "/major-expenses", nil),
			wantStart: min,
			wantEnd:   max,
		},
		{
			name:      "unparseable both falls back to MinDate/MaxDate",
			req:       httptest.NewRequest("GET", "/major-expenses?start=garbage&end=also-garbage", nil),
			wantStart: min,
			wantEnd:   max,
		},
		{
			name: "form values used when query missing",
			req: func() *http.Request {
				form := url.Values{"start": {"2024-03-01"}, "end": {"2024-03-31"}}
				r := httptest.NewRequest("POST", "/major-expenses", strings.NewReader(form.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return r
			}(),
			wantStart: mustParse("2024-03-01"),
			wantEnd:   mustParse("2024-03-31"),
		},
		{
			name:      "only start parses; end falls back",
			req:       httptest.NewRequest("GET", "/major-expenses?start=2024-05-01", nil),
			wantStart: mustParse("2024-05-01"),
			wantEnd:   max,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStart, gotEnd := parseRangeFromRequest(tc.req, txns)
			if !gotStart.Equal(tc.wantStart) {
				t.Errorf("start = %v, want %v", gotStart, tc.wantStart)
			}
			if !gotEnd.Equal(tc.wantEnd) {
				t.Errorf("end = %v, want %v", gotEnd, tc.wantEnd)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/handlers/majorexpenses/ -run TestParseRangeFromRequest -v`
Expected: compile error — `undefined: parseRangeFromRequest`.

- [ ] **Step 3: Implement the helper**

Append to `internal/handlers/majorexpenses/handlers.go` (just above `renderError`):

```go
// parseRangeFromRequest resolves the active date window for a request.
// Order of resolution per side: URL query → form value → fallback to the
// loaded data's MinDate / MaxDate. Unparseable values silently fall back —
// query params are a UX convenience, not a strict API contract.
func parseRangeFromRequest(r *http.Request, txns *models.TransactionSet) (start, end time.Time) {
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	if startStr == "" || endStr == "" {
		// Form values cover POST/PUT/DELETE bodies (mutation handlers).
		// ParseForm is idempotent and cheap — safe to call even on GET.
		_ = r.ParseForm()
		if startStr == "" {
			startStr = r.PostForm.Get("start")
		}
		if endStr == "" {
			endStr = r.PostForm.Get("end")
		}
	}
	start = txns.MinDate()
	end = txns.MaxDate()
	if startStr != "" {
		if t, err := time.Parse("2006-01-02", startStr); err == nil {
			start = t
		}
	}
	if endStr != "" {
		if t, err := time.Parse("2006-01-02", endStr); err == nil {
			end = t
		}
	}
	return start, end
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/handlers/majorexpenses/ -run TestParseRangeFromRequest -v`
Expected: PASS for all 5 sub-tests.

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/majorexpenses/handlers.go internal/handlers/majorexpenses/handlers_test.go
git commit -m "feat(major-expenses): add parseRangeFromRequest helper

Resolves the active date window from a request — URL query first,
form values second, fallback to MinDate/MaxDate. Unparseable values
silently fall back; the query params are a UX convenience, not a
strict API."
```

---

## Task 2: Thread date range through `buildPageData` (TDD)

**Files:**
- Modify: `internal/handlers/majorexpenses/handlers.go` (`buildPageData` signature + body, add `formatDateInputValue` helper)
- Modify: `internal/handlers/majorexpenses/handlers_test.go` (update existing `TestBuildPageData_IncomeNotIncludedInGroups` to pass a request; add `TestBuildPageData_DateRangeFiltersTransactions`)

`buildPageData()` becomes `buildPageData(r *http.Request)`. It loads data, resolves the window via `parseRangeFromRequest`, applies `FilterByDateRange` before the existing outflow-filter-and-match pipeline, and returns the data map enriched with `StartDate`/`EndDate`/`MinDate`/`MaxDate` strings.

- [ ] **Step 1: Write the new failing test**

Append to `internal/handlers/majorexpenses/handlers_test.go`:

```go
func TestBuildPageData_DateRangeFiltersTransactions(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// Replace the default fixture with one that spans 3 calendar years
	// so we can prove a windowed call only sees the in-window subset.
	csvDir := t.TempDir()
	csvPath := filepath.Join(csvDir, "test.csv")
	csvContent := "Date,Description,Amount,Type,Category\n" +
		"2023-06-15,Landlord LLC,-1500,Outflow,Housing\n" +
		"2024-06-15,Landlord LLC,-1700,Outflow,Housing\n" +
		"2025-06-15,Landlord LLC,-1900,Outflow,Housing\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	store, _ := storage.New(csvDir)
	dl2 := dataloader.New(csvDir, store)
	Initialize(dl2, nil)
	defer Initialize(dl, nil)

	if _, err := dl2.AddMajorExpense(makeExpense("rent", "Rent", []string{"landlord"}, 0, 0)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 2024-only window — only the middle row should match.
	req := httptest.NewRequest("GET", "/major-expenses?start=2024-01-01&end=2024-12-31", nil)
	data, err := buildPageData(req)
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}

	// Map keys are echoed for the template inputs.
	if got := data["StartDate"]; got != "2024-01-01" {
		t.Errorf("StartDate = %v, want 2024-01-01", got)
	}
	if got := data["EndDate"]; got != "2024-12-31" {
		t.Errorf("EndDate = %v, want 2024-12-31", got)
	}
	if got := data["MinDate"]; got != "2023-06-15" {
		t.Errorf("MinDate = %v, want 2023-06-15 (full-data min)", got)
	}
	if got := data["MaxDate"]; got != "2025-06-15" {
		t.Errorf("MaxDate = %v, want 2025-06-15 (full-data max)", got)
	}

	// Per-expense rollup reflects only the 2024 transaction.
	body, _ := json.Marshal(data["Summaries"])
	var raw []map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode summaries: %v\n%s", err, body)
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(raw))
	}
	if got := int(raw[0]["Count"].(float64)); got != 1 {
		t.Errorf("Count = %d, want 1 (only the 2024 txn is in window)", got)
	}
	if got := raw[0]["Total"].(float64); got != 1700 {
		t.Errorf("Total = %v, want 1700 (only the 2024 txn is in window)", got)
	}
}

// makeExpense is a small constructor for tests so we don't repeat the
// MajorExpense literal everywhere.
func makeExpense(id, name string, keywords []string, min, max float64) models.MajorExpense {
	return models.MajorExpense{
		ID:          id,
		Name:        name,
		Keywords:    keywords,
		ExpectedMin: min,
		ExpectedMax: max,
	}
}
```

- [ ] **Step 2: Update the existing test that calls `buildPageData()` with no args**

In `internal/handlers/majorexpenses/handlers_test.go`, find the line in `TestBuildPageData_IncomeNotIncludedInGroups`:

```go
	data, err := buildPageData()
```

Replace with:

```go
	data, err := buildPageData(httptest.NewRequest("GET", "/major-expenses", nil))
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/handlers/majorexpenses/ -run "TestBuildPageData" -v`
Expected: compile error — `buildPageData` argument count mismatch and `formatDateInputValue` undefined (we'll need it shortly).

- [ ] **Step 4: Implement the `formatDateInputValue` helper**

Append to `internal/handlers/majorexpenses/handlers.go` (just above the new `parseRangeFromRequest`):

```go
// formatDateInputValue formats a date for use as an <input type="date">
// value attribute. Returns "" for the zero time so empty data sets render
// blank inputs rather than "0001-01-01".
func formatDateInputValue(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}
```

- [ ] **Step 5: Modify `buildPageData` to accept the request and apply the window**

Replace the entire `buildPageData` function in `internal/handlers/majorexpenses/handlers.go` with:

```go
// buildPageData loads expenses + transactions, applies the active date
// window from the request, runs Match, and produces the dual-card page
// data. It is the single source of truth for both full-page and partial
// rendering.
func buildPageData(r *http.Request) (map[string]interface{}, error) {
	expenses, err := loader.LoadMajorExpenses()
	if err != nil {
		return nil, fmt.Errorf("load major expenses: %w", err)
	}
	if expenses == nil {
		expenses = []models.MajorExpense{}
	}

	txns, err := loader.LoadData()
	if err != nil {
		return nil, fmt.Errorf("load transactions: %w", err)
	}

	pins, err := loader.LoadTransactionPins()
	if err != nil {
		return nil, fmt.Errorf("load transaction pins: %w", err)
	}

	// Resolve the active window from the request, then narrow the
	// transaction set BEFORE the outflow filter / matcher pipeline so
	// per-expense rollups AND exception buckets reflect only in-window
	// transactions.
	minDate := txns.MinDate()
	maxDate := txns.MaxDate()
	startDate, endDate := parseRangeFromRequest(r, txns)
	windowed := txns.FilterByDateRange(startDate, endDate)

	// Major Expenses is an expense-tracking page — filter to outflows
	// BEFORE matching so income (paychecks, refunds, transfers) can't
	// inflate "matched" counts/totals when its description happens to
	// contain a keyword.
	outflows := windowed.FilterByType(models.Outflow)

	match := majorexpenseengine.Match(outflows, expenses, majorexpenseengine.MatchOptions{
		UnknownLargeThreshold: defaultUnknownThreshold,
		NewMerchantWindow:     time.Duration(defaultNewWindowDays) * 24 * time.Hour,
		Pins:                  pins,
	})

	type ExpenseSummary struct {
		Expense      models.MajorExpense
		Count        int
		PinnedCount  int
		Total        float64
		Transactions []models.Transaction
		PinnedHashes map[string]bool
	}
	summaries := make([]ExpenseSummary, 0, len(expenses))
	for _, e := range expenses {
		var total float64
		txns := append([]models.Transaction(nil), match.Groups[e.ID]...)
		for _, t := range txns {
			total += t.AbsAmount()
		}
		sort.Slice(txns, func(i, j int) bool { return txns[i].Date.After(txns[j].Date) })

		pinnedForExpense := make(map[string]bool)
		for _, t := range txns {
			if match.PinnedHashes[t.Hash] {
				pinnedForExpense[t.Hash] = true
			}
		}

		summaries = append(summaries, ExpenseSummary{
			Expense:      e,
			Count:        len(txns),
			PinnedCount:  len(pinnedForExpense),
			Total:        total,
			Transactions: txns,
			PinnedHashes: pinnedForExpense,
		})
	}

	return map[string]interface{}{
		"Title":          "Major Expenses",
		"ActiveTab":      "major-expenses",
		"Expenses":       expenses,
		"ExpenseOptions": buildExpenseOptions(expenses),
		"Summaries":      summaries,
		"Match":          match,
		"PinnedHashes":   match.PinnedHashes,
		"Threshold":      defaultUnknownThreshold,
		"WindowDays":     defaultNewWindowDays,
		"StartDate":      formatDateInputValue(startDate),
		"EndDate":        formatDateInputValue(endDate),
		"MinDate":        formatDateInputValue(minDate),
		"MaxDate":        formatDateInputValue(maxDate),
	}, nil
}
```

- [ ] **Step 6: Update every call site of `buildPageData` in `handlers.go`**

There are three call sites in `internal/handlers/majorexpenses/handlers.go`. Update each:

In `handleMajorExpensesPage`:
```go
	data, err := buildPageData()
```
becomes:
```go
	data, err := buildPageData(r)
```

In `handleExceptions`:
```go
	data, err := buildPageData()
```
becomes:
```go
	data, err := buildPageData(r)
```

In `renderResults` — change the function signature and body. Replace the entire `renderResults` function with:

```go
// renderResults sends the combined dual-column partial used by every
// mutation handler. Threads the active window through so post-mutation
// re-renders preserve the user's filter.
func renderResults(w http.ResponseWriter, r *http.Request) {
	data, err := buildPageData(r)
	if err != nil {
		renderError(w, "Failed to refresh page: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if renderer != nil {
		renderer.RenderPartial(w, "major-expenses-results", data)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
```

Then update every caller of `renderResults(w)` in `handlers.go` to `renderResults(w, r)`. There are six callers: `handleAdd`, `handleUpdate`, `handleDelete`, `handlePin`, `handleBulkPin`, `handleUnpin`.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/handlers/majorexpenses/ -v`
Expected: all existing tests + the new `TestBuildPageData_DateRangeFiltersTransactions` PASS.

- [ ] **Step 8: Run full test suite to catch any cross-package fallout**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/handlers/majorexpenses/handlers.go internal/handlers/majorexpenses/handlers_test.go
git commit -m "feat(major-expenses): apply date range in buildPageData

buildPageData now takes *http.Request, resolves the active window via
parseRangeFromRequest, and applies FilterByDateRange before the existing
outflow-filter-and-match pipeline. Per-expense rollups AND exception
buckets reflect only in-window transactions. Map keys gain StartDate /
EndDate / MinDate / MaxDate strings for the upcoming filter inputs."
```

---

## Task 3: Page-handler tests for default / explicit / unparseable / inverted ranges (TDD)

**Files:**
- Modify: `internal/handlers/majorexpenses/handlers_test.go` (4 new tests)

These confirm the GET endpoint surfaces the right data-map values for each range scenario. They run against the `renderer = nil` JSON path, so they are pure data-shape assertions.

- [ ] **Step 1: Write four failing tests**

Append to `internal/handlers/majorexpenses/handlers_test.go`:

```go
func TestHandleMajorExpensesPage_NoQueryParamsDefaultsToAllTime(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/major-expenses", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	// MinDate/MaxDate are populated by the fixture (now-2mo .. now-1mo).
	// Default window equals the full range when no params are provided.
	if body["StartDate"] != body["MinDate"] {
		t.Errorf("StartDate = %v, want = MinDate %v", body["StartDate"], body["MinDate"])
	}
	if body["EndDate"] != body["MaxDate"] {
		t.Errorf("EndDate = %v, want = MaxDate %v", body["EndDate"], body["MaxDate"])
	}
}

func TestHandleMajorExpensesPage_StartEndQueryParamsEchoed(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/major-expenses?start=2024-01-01&end=2024-12-31", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	if body["StartDate"] != "2024-01-01" {
		t.Errorf("StartDate = %v, want 2024-01-01", body["StartDate"])
	}
	if body["EndDate"] != "2024-12-31" {
		t.Errorf("EndDate = %v, want 2024-12-31", body["EndDate"])
	}
}

func TestHandleMajorExpensesPage_UnparseableDatesFallBackToAllTime(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/major-expenses?start=garbage&end=also-garbage", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	if body["StartDate"] != body["MinDate"] {
		t.Errorf("unparseable start should fall back to MinDate; StartDate=%v MinDate=%v", body["StartDate"], body["MinDate"])
	}
	if body["EndDate"] != body["MaxDate"] {
		t.Errorf("unparseable end should fall back to MaxDate; EndDate=%v MaxDate=%v", body["EndDate"], body["MaxDate"])
	}
}

func TestHandleMajorExpensesPage_StartAfterEndReturnsEmptyWindow(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// Seed a defined expense so we can confirm it still appears with
	// zero counts/totals when the window collapses.
	if _, err := dl.AddMajorExpense(makeExpense("rent", "Rent", []string{"landlord"}, 0, 0)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := httptest.NewRequest("GET", "/major-expenses?start=2099-01-01&end=2024-01-01", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())

	// The defined expense is still listed (left card always shows every
	// definition) — but with zero count and zero total because the
	// window is empty.
	rawSummaries, _ := json.Marshal(body["Summaries"])
	var summaries []map[string]interface{}
	if err := json.Unmarshal(rawSummaries, &summaries); err != nil {
		t.Fatalf("decode summaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary (defined expense), got %d", len(summaries))
	}
	if got := int(summaries[0]["Count"].(float64)); got != 0 {
		t.Errorf("Count = %d, want 0 (empty window)", got)
	}
	if got := summaries[0]["Total"].(float64); got != 0 {
		t.Errorf("Total = %v, want 0 (empty window)", got)
	}
}
```

- [ ] **Step 2: Run the new tests**

Run: `go test ./internal/handlers/majorexpenses/ -run "TestHandleMajorExpensesPage_(NoQueryParams|StartEndQueryParams|UnparseableDates|StartAfterEnd)" -v`
Expected: all four PASS without further code changes — Task 2 already wired the data through.

If any fail, double-check the changes from Task 2 are saved (the data map must include `StartDate`/`EndDate`/`MinDate`/`MaxDate`).

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/majorexpenses/handlers_test.go
git commit -m "test(major-expenses): cover default / explicit / unparseable / inverted date ranges"
```

---

## Task 4: HTMX target detection in `handleMajorExpensesPage` (TDD)

**Files:**
- Modify: `internal/handlers/majorexpenses/handlers.go` (`handleMajorExpensesPage`)
- Modify: `internal/handlers/majorexpenses/handlers_test.go` (add `setupTestEnvWithRenderer` helper + `TestHandleMajorExpensesPage_HTMXFilterReturnsWrapperOnly`)

When the request has the HTMX target header pointing at the wrapper, the handler must render only the `major-expenses-results-wrapper` partial. Otherwise it renders the full base layout. Without this, an HTMX swap of the wrapper would inject a complete page inside the wrapper.

- [ ] **Step 1: Add a helper that builds a real renderer for tests**

Append to `internal/handlers/majorexpenses/handlers_test.go`:

```go
// setupTestEnvWithRenderer wires the package up with a real templates.Renderer
// pulling from the embedded FS, so tests can assert on rendered HTML rather
// than the JSON fallback. Mirrors setupTestEnv otherwise.
func setupTestEnvWithRenderer(t *testing.T) (*dataloader.DataLoader, func()) {
	t.Helper()
	dl, cleanup := setupTestEnv(t)

	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	rend, err := templates.NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}
	Initialize(dl, rend)
	prevCleanup := cleanup
	return dl, func() {
		prevCleanup()
		Initialize(dl, nil) // restore JSON-mode for any tests that follow
	}
}
```

This needs new imports — at the top of `handlers_test.go`, add to the import block:

```go
	"io/fs"

	"budget2/internal/templates"
	"budget2/web"
```

- [ ] **Step 2: Write the failing test**

Append to `internal/handlers/majorexpenses/handlers_test.go`:

```go
func TestHandleMajorExpensesPage_HTMXFilterReturnsWrapperOnly(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/major-expenses?start=2024-01-01&end=2024-12-31", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "major-expenses-results-wrapper")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	// Wrapper marker is present.
	if !strings.Contains(body, `id="major-expenses-results-wrapper"`) {
		t.Errorf("expected wrapper id in HTMX response; got:\n%s", body)
	}
	// Base-layout markers are absent — otherwise the swap nests a full
	// page inside the wrapper.
	if strings.Contains(strings.ToLower(body), "<!doctype") {
		t.Errorf("HTMX response must NOT include the base layout; found <!doctype")
	}
	if strings.Contains(body, "<html") {
		t.Errorf("HTMX response must NOT include the base layout; found <html>")
	}
}

func TestHandleMajorExpensesPage_NonHTMXReturnsBaseLayout(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/major-expenses", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := strings.ToLower(w.Body.String())
	if !strings.Contains(body, "<!doctype") && !strings.Contains(body, "<html") {
		t.Errorf("non-HTMX response must include the base layout; got:\n%s", body)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/handlers/majorexpenses/ -run "TestHandleMajorExpensesPage_(HTMXFilterReturnsWrapperOnly|NonHTMXReturnsBaseLayout)" -v`
Expected:
- `HTMXFilterReturnsWrapperOnly` FAILS — handler always renders `base`, so the response contains `<!doctype` and `<html>`.
- `NonHTMXReturnsBaseLayout` PASSES already — but template `major-expenses-results-wrapper` doesn't exist yet, so this might fail at the wrapper-id assertion in the previous test depending on order. We'll add the template in Task 7. For now both tests will fail; that's expected.

- [ ] **Step 4: Implement HTMX target detection in `handleMajorExpensesPage`**

In `internal/handlers/majorexpenses/handlers.go`, replace the body of `handleMajorExpensesPage` with:

```go
func handleMajorExpensesPage(w http.ResponseWriter, r *http.Request) {
	data, err := buildPageData(r)
	if err != nil {
		renderError(w, "Failed to build page: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if renderer != nil {
		// HTMX requests targeting the results wrapper get only the
		// wrapper partial. Returning the full base layout into the
		// wrapper would nest a complete page inside the results area.
		if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Target") == "major-expenses-results-wrapper" {
			renderer.RenderPartial(w, "major-expenses-results-wrapper", data)
			return
		}
		renderer.Render(w, "base", data)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
```

- [ ] **Step 5: Run the tests again — `HTMXFilterReturnsWrapperOnly` will still FAIL because the partial doesn't exist yet**

Run: `go test ./internal/handlers/majorexpenses/ -run "TestHandleMajorExpensesPage_(HTMXFilterReturnsWrapperOnly|NonHTMXReturnsBaseLayout)" -v`
Expected: `NonHTMXReturnsBaseLayout` PASSES; `HTMXFilterReturnsWrapperOnly` still FAILS — the renderer can't find `major-expenses-results-wrapper`. We add it in Task 7. Don't commit yet.

**Deferred to Task 7**: this test passing requires the new template partial. Leave the test in place; it acts as a forcing function for Task 7.

- [ ] **Step 6: Commit just the handler change**

```bash
git add internal/handlers/majorexpenses/handlers.go internal/handlers/majorexpenses/handlers_test.go
git commit -m "feat(major-expenses): HTMX target detection in page handler

When HX-Target is the results wrapper, render only the wrapper partial
instead of the full base layout. Otherwise an HTMX swap would inject a
complete page inside the swap target.

The wrapper template partial lands in a follow-up commit; the
HTMXFilterReturnsWrapperOnly test stays red until then."
```

---

## Task 5: `handleExceptions` honors the active window (TDD)

**Files:**
- Modify: `internal/handlers/majorexpenses/handlers.go` (`handleExceptions`)
- Modify: `internal/handlers/majorexpenses/handlers_test.go` (`TestHandleExceptions_StartEndQueryParams`)

`handleExceptions` already calls `buildPageData`. After Task 2, the new `buildPageData(r)` reads from `r` directly — so the call already gets the window. We add a test to lock in this behavior.

- [ ] **Step 1: Verify the call site is `buildPageData(r)`**

Open `internal/handlers/majorexpenses/handlers.go` and confirm `handleExceptions` calls `buildPageData(r)` (it should already after Task 2 step 6).

- [ ] **Step 2: Write the failing test**

Append to `internal/handlers/majorexpenses/handlers_test.go`:

```go
func TestHandleExceptions_StartEndQueryParams(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// Seed an expense definition + a multi-year fixture.
	csvDir := t.TempDir()
	csvPath := filepath.Join(csvDir, "test.csv")
	csvContent := "Date,Description,Amount,Type,Category\n" +
		"2023-06-15,Random Big Purchase A,-450,Outflow,Misc\n" +
		"2024-06-15,Random Big Purchase B,-450,Outflow,Misc\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	store, _ := storage.New(csvDir)
	dl2 := dataloader.New(csvDir, store)
	Initialize(dl2, nil)
	defer Initialize(dl, nil)

	req := httptest.NewRequest("GET", "/major-expenses/exceptions?start=2024-01-01&end=2024-12-31", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())

	// Defaults are not "all-time" here — the query params should win.
	if body["StartDate"] != "2024-01-01" {
		t.Errorf("StartDate = %v, want 2024-01-01", body["StartDate"])
	}
	if body["EndDate"] != "2024-12-31" {
		t.Errorf("EndDate = %v, want 2024-12-31", body["EndDate"])
	}
}
```

- [ ] **Step 3: Run the test**

Run: `go test ./internal/handlers/majorexpenses/ -run TestHandleExceptions_StartEndQueryParams -v`
Expected: PASS without further code changes (Task 2 already threaded `r` through `handleExceptions`).

- [ ] **Step 4: Commit**

```bash
git add internal/handlers/majorexpenses/handlers_test.go
git commit -m "test(major-expenses): handleExceptions honors start/end query params"
```

---

## Task 6: Mutation handlers preserve the active window (TDD)

**Files:**
- Modify: `internal/handlers/majorexpenses/handlers_test.go` (2 new tests)

After Task 2, `renderResults(w, r)` already passes the request through, and every mutation handler already calls `renderResults(w, r)`. We add tests that submit mutation forms with `start`/`end` form values and assert the response data map echoes them.

- [ ] **Step 1: Write the failing tests**

Append to `internal/handlers/majorexpenses/handlers_test.go`:

```go
func TestHandleAdd_PreservesDateRange(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{}
	form.Set("name", "Rent")
	form.Set("keywords", "landlord")
	form.Set("expected_min", "1500")
	form.Set("expected_max", "2000")
	form.Set("start", "2024-01-01")
	form.Set("end", "2024-12-31")

	req := httptest.NewRequest("POST", "/major-expenses", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	if body["StartDate"] != "2024-01-01" {
		t.Errorf("StartDate = %v, want 2024-01-01 (mutation must preserve window)", body["StartDate"])
	}
	if body["EndDate"] != "2024-12-31" {
		t.Errorf("EndDate = %v, want 2024-12-31 (mutation must preserve window)", body["EndDate"])
	}
}

func TestHandleBulkPin_PreservesDateRange(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	exp, err := dl.AddMajorExpense(makeExpense("rent", "Rent", []string{"landlord"}, 0, 0))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	form := url.Values{}
	form.Set("expense_id", exp.ID)
	form.Add("hashes", "deadbeef")
	form.Set("start", "2024-01-01")
	form.Set("end", "2024-12-31")

	req := httptest.NewRequest("POST", "/major-expenses/pins/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	if body["StartDate"] != "2024-01-01" {
		t.Errorf("StartDate = %v, want 2024-01-01 (bulk-pin must preserve window)", body["StartDate"])
	}
	if body["EndDate"] != "2024-12-31" {
		t.Errorf("EndDate = %v, want 2024-12-31 (bulk-pin must preserve window)", body["EndDate"])
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/handlers/majorexpenses/ -run "TestHandleAdd_PreservesDateRange|TestHandleBulkPin_PreservesDateRange" -v`
Expected: PASS without further code changes.

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/majorexpenses/handlers_test.go
git commit -m "test(major-expenses): mutations preserve active date window via form values"
```

---

## Task 7: Add `major-expenses-results-wrapper` template + the filter-card UI

**Files:**
- Modify: `web/templates/pages/major-expenses.html`

This is the largest single file change. We add three things at once because they belong together: the new wrapper partial, the filter card markup, and the JS helpers + sessionStorage. Splitting them would create intermediate states where the page is half-broken (filter card with no submit target, etc.).

- [ ] **Step 1: Replace the body of `major-expenses-content` with the new structure**

Open `web/templates/pages/major-expenses.html`. Replace the block starting `{{define "major-expenses-content"}}` and ending at the matching `{{end}}` (line 1 through line 33 in the existing file) with:

```html
{{define "major-expenses-content"}}
<div class="space-y-4">
    <!-- Filter card (NEW) -->
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
        <form id="major-expenses-filter-form"
              hx-get="/major-expenses"
              hx-target="#major-expenses-results-wrapper"
              hx-swap="outerHTML"
              hx-push-url="true"
              hx-trigger="change from:input[type=date], click from:.date-range-btn"
              hx-indicator="#me-loading"
              class="flex flex-wrap items-end gap-3">
            <div>
                <label class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">From</label>
                <input type="date" name="start" value="{{.StartDate}}" min="{{.MinDate}}" max="{{.MaxDate}}"
                    class="border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-md px-3 py-2 text-sm focus:ring-indigo-500 focus:border-indigo-500">
            </div>
            <div>
                <label class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">To</label>
                <input type="date" name="end" value="{{.EndDate}}" min="{{.MinDate}}" max="{{.MaxDate}}"
                    class="border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-md px-3 py-2 text-sm focus:ring-indigo-500 focus:border-indigo-500">
            </div>
            <div class="flex gap-1 items-center" id="me-date-range-buttons">
                <button type="button" onclick="meStepDateRange(-1)" title="Step back" aria-label="Step back"
                    class="px-2 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7"></path></svg>
                </button>
                <button type="button" onclick="meSetDateRange(3)" data-months="3"
                    class="date-range-btn px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">3M</button>
                <button type="button" onclick="meSetDateRange(6)" data-months="6"
                    class="date-range-btn px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">6M</button>
                <button type="button" onclick="meSetDateRange(12)" data-months="12"
                    class="date-range-btn px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">12M</button>
                <button type="button" onclick="meSetDateRange(0)" data-months="0"
                    class="date-range-btn px-3 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">All</button>
                <button type="button" onclick="meStepDateRange(1)" title="Step forward" aria-label="Step forward"
                    class="px-2 py-1 text-sm bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path></svg>
                </button>
            </div>
            <span id="me-loading" class="htmx-indicator">
                <svg class="animate-spin h-5 w-5 text-indigo-600 dark:text-indigo-400" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
            </span>
        </form>
    </div>

    <!-- Existing search/header card -->
    <div class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
        <h1 class="text-2xl font-semibold text-gray-800 dark:text-gray-100">Major Expenses</h1>
        <p class="text-sm text-gray-600 dark:text-gray-400 mt-1">
            Tell the app about expenses you understand. We'll group transactions
            that match your keywords and call out anything that looks unusual.
        </p>
        <div class="mt-3 relative">
            <input type="search" id="major-expenses-search"
                placeholder="Filter expenses, keywords, notes, transactions, amounts, dates…"
                class="w-full text-sm border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 rounded-md py-1.5 px-3 pr-40"
                autocomplete="off">
            <span id="major-expenses-search-status" class="hidden absolute right-9 top-1/2 -translate-y-1/2 text-xs text-gray-500 dark:text-gray-400 pointer-events-none" aria-live="polite"></span>
            <button type="button" id="major-expenses-search-clear"
                class="hidden absolute right-2 top-1/2 -translate-y-1/2 p-1 rounded text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-600"
                aria-label="Clear search">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
                </svg>
            </button>
        </div>
    </div>

    <!-- Wrapper for HTMX swap on date-range change -->
    {{template "major-expenses-results-wrapper" .}}
</div>
```

- [ ] **Step 2: Add the `major-expenses-results-wrapper` template definition**

In the same file, immediately after the `{{define "major-expenses-content"}}…{{end}}` block (after the new content above), add a new template definition:

```html
{{define "major-expenses-results-wrapper"}}
<div id="major-expenses-results-wrapper" class="grid grid-cols-1 lg:grid-cols-2 gap-4">
    <div id="major-expenses-list-card" class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
        {{template "major-expenses-list-card-content" .}}
    </div>
    <div id="major-expenses-results" class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
        {{template "major-expenses-exceptions-panel" .}}
    </div>
</div>
{{end}}
```

- [ ] **Step 3: Add the page-level JS for the filter form — sessionStorage + setDateRange + stepDateRange**

The page-level JS already lives inside the `<script>` block at the bottom of `major-expenses-content`. We add a new block above the existing IIFE — at the top of the `<script>` block, just after the opening `<script>` tag and before the existing `(function () { ... })()`. Insert:

```html
<script>
// Filter-form sessionStorage restore: bare /major-expenses with empty
// query + saved key → redirect once to /major-expenses?<saved>. Mirrors
// the pattern in pages/explorer.html.
(function () {
    const KEY = 'majorExpensesFilters';
    if (window.location.search === '' || window.location.search === '?') {
        const saved = sessionStorage.getItem(KEY);
        if (saved && !sessionStorage.getItem(KEY + '_restoring')) {
            sessionStorage.setItem(KEY + '_restoring', '1');
            window.location.replace('/major-expenses?' + saved);
            return;
        }
        sessionStorage.removeItem(KEY + '_restoring');
    } else {
        sessionStorage.removeItem(KEY + '_restoring');
    }

    // Save outgoing filter params on every htmx:configRequest from the
    // filter form. We do NOT save window.location.search inside this
    // handler — at this moment it still contains the OLD URL because
    // hx-push-url runs after the request settles. Instead pull start/end
    // from the form values so the saved value reflects what's about to
    // be sent.
    document.body.addEventListener('htmx:configRequest', function (evt) {
        const form = document.getElementById('major-expenses-filter-form');
        if (!form || !(evt.detail.elt === form || form.contains(evt.detail.elt))) return;
        const start = form.querySelector('input[name="start"]').value;
        const end = form.querySelector('input[name="end"]').value;
        const params = new URLSearchParams();
        if (start) params.set('start', start);
        if (end) params.set('end', end);
        sessionStorage.setItem(KEY, params.toString());
    });
})();

// Quick-range helpers — page-local copies of Explorer's setDateRange /
// stepDateRange / detectSelectedDateRange, scoped to the major-expenses
// filter form. Naming is `me*` to avoid colliding with Explorer's
// global-scope versions on the rare chance both pages ever co-exist.
function meSetDateRange(months) {
    const form = document.getElementById('major-expenses-filter-form');
    if (!form) return;
    const startInput = form.querySelector('input[name="start"]');
    const endInput = form.querySelector('input[name="end"]');
    const minDate = startInput.getAttribute('min');
    const maxDate = endInput.getAttribute('max');
    if (months === 0) {
        startInput.value = minDate;
        endInput.value = maxDate;
    } else {
        const end = new Date(maxDate);
        const start = new Date(end);
        start.setMonth(start.getMonth() - months);
        const minDateObj = new Date(minDate);
        if (start < minDateObj) start.setTime(minDateObj.getTime());
        startInput.value = start.toISOString().split('T')[0];
        endInput.value = maxDate;
    }
    meUpdateDateRangeButtons(months);
    htmx.trigger(form, 'submit');
}

function meStepDateRange(direction) {
    const form = document.getElementById('major-expenses-filter-form');
    if (!form) return;
    const startInput = form.querySelector('input[name="start"]');
    const endInput = form.querySelector('input[name="end"]');
    const minDate = startInput.getAttribute('min');
    const maxDate = endInput.getAttribute('max');
    const start = new Date(startInput.value);
    const end = new Date(endInput.value);
    const deltaMs = end - start;
    let newStart, newEnd;
    if (direction > 0) {
        newStart = new Date(end.getTime() + 86400000);
        newEnd = new Date(newStart.getTime() + deltaMs);
    } else {
        newEnd = new Date(start.getTime() - 86400000);
        newStart = new Date(newEnd.getTime() - deltaMs);
    }
    const minD = new Date(minDate);
    const maxD = new Date(maxDate);
    if (newStart < minD) newStart = minD;
    if (newEnd > maxD) newEnd = maxD;
    if (newStart > maxD) return;
    if (newEnd < minD) return;
    startInput.value = newStart.toISOString().split('T')[0];
    endInput.value = newEnd.toISOString().split('T')[0];
    meUpdateDateRangeButtons(-1);
    htmx.trigger(form, 'submit');
}

function meUpdateDateRangeButtons(selectedMonths) {
    document.querySelectorAll('#me-date-range-buttons .date-range-btn').forEach(function (btn) {
        const btnMonths = parseInt(btn.getAttribute('data-months'));
        if (btnMonths === selectedMonths) {
            btn.classList.remove('bg-gray-100', 'dark:bg-gray-700', 'text-gray-700', 'dark:text-gray-300');
            btn.classList.add('bg-indigo-600', 'dark:bg-indigo-500', 'text-white', 'dark:text-white');
        } else {
            btn.classList.remove('bg-indigo-600', 'dark:bg-indigo-500', 'text-white', 'dark:text-white');
            btn.classList.add('bg-gray-100', 'dark:bg-gray-700', 'text-gray-700', 'dark:text-gray-300');
        }
    });
}

function meDetectSelectedDateRange() {
    const form = document.getElementById('major-expenses-filter-form');
    if (!form) return;
    const startInput = form.querySelector('input[name="start"]');
    const endInput = form.querySelector('input[name="end"]');
    const minDate = startInput.getAttribute('min');
    const maxDate = endInput.getAttribute('max');
    const startDate = startInput.value;
    const endDate = endInput.value;
    if (endDate !== maxDate) { meUpdateDateRangeButtons(-1); return; }
    if (startDate === minDate) { meUpdateDateRangeButtons(0); return; }
    const end = new Date(maxDate);
    for (const months of [3, 6, 12]) {
        const expectedStart = new Date(end);
        expectedStart.setMonth(expectedStart.getMonth() - months);
        if (startDate === expectedStart.toISOString().split('T')[0]) {
            meUpdateDateRangeButtons(months);
            return;
        }
    }
    meUpdateDateRangeButtons(-1);
}

// Highlight the correct quick-range button on first paint and after
// every HTMX swap of the wrapper (the inputs may have new values).
document.addEventListener('DOMContentLoaded', meDetectSelectedDateRange);
document.body.addEventListener('htmx:afterSwap', meDetectSelectedDateRange);
</script>

<script>
```

Note that we close this `<script>` and immediately open another — the existing IIFE block stays intact below. The text-search re-application (`applyUnifiedFilter`) inside the existing `htmx:afterSwap` listener already covers the requirement that the search filter re-applies after a date-range swap; nothing else to do for that.

- [ ] **Step 4: Run the existing template tests to confirm no regressions**

Run: `go test ./internal/templates/ -run TestRenderMajorExpenses -v`
Expected: PASS — the existing tests don't reference any of the new ids; their assertions on header text and matched-txn rows still hold.

- [ ] **Step 5: Run the handler tests, including the previously-deferred wrapper test**

Run: `go test ./internal/handlers/majorexpenses/ -v`
Expected: every test PASSES, including `TestHandleMajorExpensesPage_HTMXFilterReturnsWrapperOnly` from Task 4 — the partial now exists.

- [ ] **Step 6: Run the full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/templates/pages/major-expenses.html
git commit -m "feat(major-expenses): date-range filter card + wrapper partial

New filter card (date inputs + 3M/6M/12M/All quick-buttons + step
arrows) above the existing search/header card. Submits via HTMX,
swapping a new major-expenses-results-wrapper partial that composes the
list-card and exceptions-panel partials. Page-local meSetDateRange /
meStepDateRange / meDetectSelectedDateRange JS helpers ported from
Explorer. sessionStorage persistence under key majorExpensesFilters
saves the outgoing form values on every htmx:configRequest."
```

---

## Task 8: Wire `hx-include` on existing mutation forms

**Files:**
- Modify: `web/templates/pages/major-expenses.html`

Without `hx-include`, mutation requests (add / update / delete / pin / unpin / bulk-pin) do not carry `start`/`end`, and `parseRangeFromRequest` falls back to all-time — silently widening the user's window after every save. We add `hx-include="#major-expenses-filter-form"` to every mutation control. The bulk-pin path uses `htmx.ajax` from JS, so we update its `values` payload separately.

- [ ] **Step 1: Add `hx-include` to the per-item form**

In `web/templates/pages/major-expenses.html`, find the per-item form (around line 301):

```html
<form id="major-expense-item-{{.Expense.ID}}" hx-put="/major-expenses/{{.Expense.ID}}" hx-target="#major-expenses-results"
    hx-swap="innerHTML" hx-trigger="change delay:500ms"
```

Replace with:

```html
<form id="major-expense-item-{{.Expense.ID}}" hx-put="/major-expenses/{{.Expense.ID}}" hx-target="#major-expenses-results"
    hx-swap="innerHTML" hx-trigger="change delay:500ms"
    hx-include="#major-expenses-filter-form"
```

Then, in the same `major-expense-item` template, find the delete button (around line 309):

```html
<button type="button" hx-delete="/major-expenses/{{.Expense.ID}}"
    hx-target="#major-expenses-results" hx-swap="innerHTML"
```

Replace with:

```html
<button type="button" hx-delete="/major-expenses/{{.Expense.ID}}"
    hx-target="#major-expenses-results" hx-swap="innerHTML"
    hx-include="#major-expenses-filter-form"
```

Then find the per-row "unpin" button inside the matched-txn table (around line 374):

```html
<button type="button"
    hx-delete="/major-expenses/pins/{{.Hash}}"
    hx-target="#major-expenses-results" hx-swap="innerHTML"
```

Replace with:

```html
<button type="button"
    hx-delete="/major-expenses/pins/{{.Hash}}"
    hx-target="#major-expenses-results" hx-swap="innerHTML"
    hx-include="#major-expenses-filter-form"
```

- [ ] **Step 2: Add `hx-include` to the add-form**

Find the add-form (around line 391):

```html
<form id="major-expenses-add-form" hx-post="/major-expenses" hx-target="#major-expenses-results" hx-swap="innerHTML"
    hx-on::after-request="if(event.detail.successful) this.reset()"
```

Replace with:

```html
<form id="major-expenses-add-form" hx-post="/major-expenses" hx-target="#major-expenses-results" hx-swap="innerHTML"
    hx-on::after-request="if(event.detail.successful) this.reset()"
    hx-include="#major-expenses-filter-form"
```

- [ ] **Step 3: Add `hx-include` to the pin-picker**

Find the pin picker form (around line 459):

```html
<form class="inline" hx-post="/major-expenses/pins" hx-target="#major-expenses-results" hx-swap="innerHTML" hx-trigger="change">
```

Replace with:

```html
<form class="inline" hx-post="/major-expenses/pins" hx-target="#major-expenses-results" hx-swap="innerHTML" hx-trigger="change"
    hx-include="#major-expenses-filter-form">
```

- [ ] **Step 4: Update the bulk-pin `htmx.ajax` call to include `start` and `end`**

Find the bulk-pin click handler in the existing `<script>` block (around line 164):

```js
        const fd = new FormData();
        fd.append('expense_id', target.value);
        hashes.forEach(function (h) { fd.append('hashes', h); });
        if (window.htmx && typeof window.htmx.ajax === 'function') {
            window.htmx.ajax('POST', '/major-expenses/pins/bulk', {
                target: '#major-expenses-results',
                swap: 'innerHTML',
                values: fd,
            });
        }
```

Replace with:

```js
        const fd = new FormData();
        fd.append('expense_id', target.value);
        hashes.forEach(function (h) { fd.append('hashes', h); });
        // Carry the active date window so the post-mutation render
        // preserves the user's filter (matches hx-include on the other
        // mutation forms).
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
```

- [ ] **Step 5: Run the full test suite**

Run: `go test ./...`
Expected: PASS — the handler tests aren't affected; this is presentation wiring.

- [ ] **Step 6: Commit**

```bash
git add web/templates/pages/major-expenses.html
git commit -m "feat(major-expenses): mutations include filter form's date window

hx-include the filter form on every mutation control so add / update /
delete / pin / unpin requests carry start and end. Bulk-pin uses
htmx.ajax from JS, so we append the values to its FormData payload
explicitly."
```

---

## Task 9: Smoke check in a live browser

**Files:** none (verification only)

This task confirms the JS quick-buttons, sessionStorage round-trip, and HTMX swap behave correctly end-to-end. The unit tests cover the data plane; this covers the presentation plane. Spec calls for manual smoke instead of Playwright because the JS is a literal port of working Explorer code.

- [ ] **Step 1: Start the server**

Run: `go run ./cmd/server` (or your usual dev startup script).

- [ ] **Step 2: Open the page**

Navigate to `http://localhost:8080/major-expenses` in a browser. Confirm:
1. The new filter card appears above the search header card.
2. The `From` and `To` inputs are populated with the dataset's MinDate / MaxDate.
3. The `All` quick-button shows the indigo highlight (since the page is at the all-time default).

- [ ] **Step 3: Exercise the quick-buttons**

Click `3M`. Confirm:
1. The `From` input shifts to (MaxDate − 3 months); `To` stays at MaxDate.
2. The page swaps via HTMX; the URL changes to `/major-expenses?start=...&end=...`.
3. The `3M` button now has the indigo highlight.
4. Both cards (list + exceptions) re-render and show only in-window matches.

- [ ] **Step 4: Exercise the step arrows**

Click `←` (step back). Confirm the window shifts to the prior 3-month period and the buttons clear their highlight (since no quick-range matches).

- [ ] **Step 5: Verify sessionStorage persistence**

While viewing a non-default range, navigate to `/explorer`, then back to `/major-expenses`. Confirm:
1. The filter form re-populates with the saved range, not all-time.
2. The cards reflect the saved window.

- [ ] **Step 6: Verify mutations preserve the window**

While viewing a non-default range, edit a major-expense definition (change a keyword). Confirm:
1. The page swaps and the date inputs still show the same window.
2. The cards re-render with the same window.

- [ ] **Step 7: Verify text search composes with date filter**

While viewing a non-default range, type a query into the existing search box. Confirm:
1. Rows hide as expected (only window-bound rows existed; query narrows further).
2. Clearing the search restores the full window-bound set.

- [ ] **Step 8: Verify the all-time button restores everything**

Click `All`. Confirm both cards return to the full historical view.

- [ ] **Step 9: Stop the server. Done.**

No commit — this task is verification only.

---

## Self-Review Notes

- **Spec coverage:** All 9 spec tests map to plan tasks (Tasks 2, 3 ×4, 4, 5, 6 ×2). Wrapper-only HTMX rendering (test #9) is locked in across Tasks 4 + 7. Mutation preservation (tests #6/#7) is in Task 6. Edge cases (`start > end`, empty data, unparseable) are in Task 3.
- **Type consistency:** `parseRangeFromRequest` and `formatDateInputValue` are referenced consistently; `buildPageData(r *http.Request)` is the single new signature, used at 3 call sites; `renderResults(w, r)` is the new mutation signature. The HX-Target string `major-expenses-results-wrapper` matches the partial name in Task 7.
- **JS namespacing:** Page-local helpers are prefixed `me*` (`meSetDateRange`, `meStepDateRange`, `meUpdateDateRangeButtons`, `meDetectSelectedDateRange`) so they cannot collide with the global-scope `setDateRange` / `stepDateRange` defined in `pages/explorer.html` if both pages ever load JS into the same global scope (e.g., via a shared layout reload). Inline `onclick="me…"` attributes in the filter card use the `me*` names.
- **No placeholders, TODOs, TBDs:** confirmed via grep.
