# App-wide MCP — Phase 2 (spend) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the MCP server four read-only spending tools — `search_transactions`, `summarize_spending`, `get_recurring`, `get_trends` — by extracting the analysis logic they need out of the handler packages into services.

**Architecture:** The tools live in `internal/services/mcpsvc/spend`, alongside `get_anomalies` and `get_price_creep` from Phase 1. The work is mostly extraction: recurring detection, trend analysis, income patterns, and spending velocity currently sit unexported inside `internal/handlers/insights`, and the live budget/KPI math sits unexported inside `internal/handlers/dashboard`. Each task extracts only what its tool needs, moves that logic's tests with it, and leaves the handler calling the new service.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk` v1.7.0, chi v5.

**Spec:** `docs/superpowers/specs/2026-08-12-app-wide-mcp-design.md` (Phase 2)
**Builds on:** `docs/superpowers/plans/2026-08-12-app-wide-mcp-phase-1.md` (branch `feat/mcp-in-server`)

## Global Constraints

- Go 1.26. No new module dependencies.
- Every tool handler's first line is `defer recoverToError("<tool_name>", &err)`. The go-sdk dispatches each tool call on its own goroutine with no recover of its own, and `middleware.Recoverer` runs on the HTTP request goroutine — a missing defer takes down the user's web server, not one call.
- Tool `Description` strings are the consuming model's only documentation. Write them to be read by a model with no other context: say what the numbers mean, what window they cover, and what they exclude.
- Extraction is a **move**, not a rewrite. Extracted functions keep their behavior, their comments, and their tests. If a function looks wrong while you are moving it, report it — do not fix it in the same commit.
- The dependency direction from Phase 1 holds: `mcpsvc` imports `plan` and `spend`; neither imports `mcpsvc`. Service packages (`insights`, `metrics`) must not import any `handlers` package.
- Per this repo's `CLAUDE.md`: before editing a function/method/type, check callers with the `LSP` tool (`incomingCalls` / `findReferences`). Never rename a symbol with find-and-replace.
- Verify with `go build ./... && go vet ./... && go test ./... && staticcheck ./...`. Run tests **bare** — never pipe through `grep`/`head` without `set -o pipefail`.
- Pre-commit runs `make check`; never bypass with `--no-verify`.
- **No coverage loss.** Every extracted function keeps its tests. Before and after each extraction task, record `go tool cover -func` for the packages involved and put both numbers in the report. Phase 1 lost an 841-line test file to a wholesale delete; that must not repeat.

## File Structure

**Created:**
- `internal/services/insights/recurring.go` + `recurring_test.go` — recurring-payment detection.
- `internal/services/insights/trends.go` + `trends_test.go` — category/major-expense trends, income patterns, spending velocity.
- `internal/services/mcpsvc/spend/search.go` — `search_transactions` types and registration.
- `internal/services/mcpsvc/spend/summary.go` — `summarize_spending`.
- `internal/services/mcpsvc/spend/recurring.go` — `get_recurring`.
- `internal/services/mcpsvc/spend/trends.go` — `get_trends`.

**Modified:**
- `internal/services/metrics/metrics.go` — dead code replaced by the extracted live implementation.
- `internal/handlers/insights/handlers.go` — delegates to `internal/services/insights`.
- `internal/handlers/dashboard/handlers.go` — delegates to `internal/services/metrics`.
- `internal/services/mcpsvc/spend/register.go` — `Deps` grows `Settings`; `Register` calls the new per-tool registrars.
- `internal/services/mcpsvc/server.go` — passes `Settings` into `spend.Deps`.

**Note on `spend/register.go`:** it currently holds `Deps`, `recoverToError`, and both Phase 1 tools. As four more tools arrive it would become the package's junk drawer. Each new tool gets its own file with its own `registerX(s *mcp.Server, deps Deps)` function; `Register` becomes a six-line dispatcher. Move the two Phase 1 tools into `insights_tools.go` as part of Task 1 so the pattern is uniform from the start.

---

### Task 1: `search_transactions`

No extraction — `models.TransactionSet` already has every filter this needs. This task also establishes the per-tool file split the rest of the phase follows.

**Files:**
- Create: `internal/services/mcpsvc/spend/search.go`
- Create: `internal/services/mcpsvc/spend/search_test.go`
- Create: `internal/services/mcpsvc/spend/insights_tools.go` (receives the two Phase 1 tools)
- Modify: `internal/services/mcpsvc/spend/register.go`

**Interfaces:**
- Consumes: `spend.Deps{Transactions TransactionSource; Store *storage.Storage}`, `(Deps).load()`, `recoverToError` — all from Phase 1.
- Produces:
  - `registerSearch(s *mcp.Server, deps Deps)`
  - `registerAnomalies(s *mcp.Server, deps Deps)` and `registerPriceCreep(s *mcp.Server, deps Deps)` (the Phase 1 tools, relocated unchanged)
  - Tool `search_transactions`

- [ ] **Step 1: Split the existing registrations**

Move the `get_anomalies` registration from `register.go` into a new `insights_tools.go` as `func registerAnomalies(s *mcp.Server, deps Deps)`, and `get_price_creep` as `func registerPriceCreep(s *mcp.Server, deps Deps)`. Copy the bodies and `Description` strings verbatim — this is a cut and paste, nothing else changes. `register.go` keeps `Deps`, `TransactionSource`, `recoverToError`, `load()`, and becomes:

```go
// Register adds the spending tools to s.
func Register(s *mcp.Server, deps Deps) {
	registerSearch(s, deps)
	registerAnomalies(s, deps)
	registerPriceCreep(s, deps)
}
```

The anomaly/price-creep input and output types stay in `insights.go` where they already live.

- [ ] **Step 2: Run the existing tests to confirm the split changed nothing**

Run: `go test ./internal/services/mcpsvc/spend/ -v`
Expected: PASS, same tests as before the split.

- [ ] **Step 3: Write the failing test**

Create `internal/services/mcpsvc/spend/search_test.go`:

```go
package spend

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// searchFixture is three outflows and one income across two months, with
// distinct amounts so every filter can be told apart by the rows it returns.
func searchFixture() *models.TransactionSet {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	return models.NewTransactionSet([]models.Transaction{
		{Date: day("2026-01-05"), Description: "NETFLIX", Category: "Entertainment", Amount: -15.99, TransactionType: models.Outflow},
		{Date: day("2026-01-20"), Description: "SAFEWAY", Category: "Groceries", Amount: -204.10, TransactionType: models.Outflow},
		{Date: day("2026-02-05"), Description: "NETFLIX", Category: "Entertainment", Amount: -15.99, TransactionType: models.Outflow},
		{Date: day("2026-02-01"), Description: "PAYCHECK", Category: "Income", Amount: 5000, TransactionType: models.Income},
	})
}

func TestSearchTransactionsFiltersByCategoryAndWindow(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "search_transactions",
		Arguments: map[string]any{
			"category":   "Entertainment",
			"start_date": "2026-01-01",
			"end_date":   "2026-01-31",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("search_transactions returned an error: %+v", res.Content)
	}

	var out searchOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Total != 1 {
		t.Fatalf("total = %d, want 1: %+v", out.Total, out.Transactions)
	}
	if out.Transactions[0].Description != "NETFLIX" {
		t.Errorf("description = %q, want NETFLIX", out.Transactions[0].Description)
	}
	if out.Transactions[0].Amount != -15.99 {
		t.Errorf("amount = %v, want -15.99 (signed, expenses negative)", out.Transactions[0].Amount)
	}
}

func TestSearchTransactionsPaginatesAndReportsTheFullTotal(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_transactions",
		Arguments: map[string]any{"per_page": 2, "page": 1},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("search_transactions returned an error: %+v", res.Content)
	}

	var out searchOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if len(out.Transactions) != 2 {
		t.Errorf("returned %d rows, want 2", len(out.Transactions))
	}
	// Total must describe the whole match set, not the page -- a model that
	// sees total == len(rows) will conclude it has everything.
	if out.Total != 4 {
		t.Errorf("total = %d, want 4 (the full match count, not the page size)", out.Total)
	}
	if out.TotalPages != 2 {
		t.Errorf("total_pages = %d, want 2", out.TotalPages)
	}
}

func TestSearchTransactionsRejectsAnInvalidDate(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_transactions",
		Arguments: map[string]any{"start_date": "01/05/2026"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("an unparseable start_date should be a tool error, not a silent full-history search")
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/services/mcpsvc/spend/ -run TestSearchTransactions -v`
Expected: FAIL — `undefined: searchOutput`, and no `search_transactions` tool is registered.

- [ ] **Step 5: Implement `search.go`**

```go
package spend

import (
	"context"
	"fmt"
	"strings"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultSearchPerPage = 50
	maxSearchPerPage     = 200
)

type searchInput struct {
	StartDate string  `json:"start_date,omitempty" jsonschema:"earliest date to include, inclusive, YYYY-MM-DD"`
	EndDate   string  `json:"end_date,omitempty" jsonschema:"latest date to include, inclusive, YYYY-MM-DD"`
	Category  string  `json:"category,omitempty" jsonschema:"exact category name; omit for all categories"`
	Search    string  `json:"search,omitempty" jsonschema:"case-insensitive substring matched against the description"`
	Type      string  `json:"type,omitempty" jsonschema:"income or outflow; omit for both"`
	MinAmount float64 `json:"min_amount,omitempty" jsonschema:"smallest absolute dollar amount to include"`
	MaxAmount float64 `json:"max_amount,omitempty" jsonschema:"largest absolute dollar amount to include"`
	Page      int     `json:"page,omitempty" jsonschema:"1-based page number; defaults to 1"`
	PerPage   int     `json:"per_page,omitempty" jsonschema:"rows per page, default 50, maximum 200"`
}

type transactionRow struct {
	Date        string  `json:"date"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	Amount      float64 `json:"amount"`
	Type        string  `json:"type"`
}

type searchOutput struct {
	Total        int              `json:"total"`
	Page         int              `json:"page"`
	PerPage      int              `json:"per_page"`
	TotalPages   int              `json:"total_pages"`
	SumAmount    float64          `json:"sum_amount"`
	Transactions []transactionRow `json:"transactions"`
}

// filterByAbsAmount keeps rows whose absolute amount is within [min, max].
// A zero bound is "unset": amounts are dollars and a zero-dollar row carries
// no information, so treating 0 as unset costs nothing and lets both bounds
// be optional in the schema.
func filterByAbsAmount(ts *models.TransactionSet, min, max float64) *models.TransactionSet {
	if min == 0 && max == 0 {
		return ts
	}
	kept := make([]models.Transaction, 0, ts.Len())
	for _, t := range ts.Transactions {
		amt := t.Amount
		if amt < 0 {
			amt = -amt
		}
		if min > 0 && amt < min {
			continue
		}
		if max > 0 && amt > max {
			continue
		}
		kept = append(kept, t)
	}
	return models.NewTransactionSet(kept)
}

func registerSearch(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "search_transactions",
		Description: "Search the transaction history. Every filter is optional and they combine with AND: " +
			"start_date/end_date (inclusive, YYYY-MM-DD), category (exact), search (case-insensitive substring " +
			"of the description), type (income or outflow), and min_amount/max_amount (compared against the " +
			"ABSOLUTE dollar amount, so min_amount 100 matches a -150.00 expense). Amounts in the result are " +
			"SIGNED — expenses are negative. Results are newest-first and paginated: `total` is the full number " +
			"of matching rows, not the number returned, so check it against the rows you received before " +
			"concluding you have seen everything. Default 50 rows per page, maximum 200. sum_amount is the " +
			"signed sum over ALL matches, not just this page.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (res *mcp.CallToolResult, out searchOutput, err error) {
		defer recoverToError("search_transactions", &err)

		start, err := parseWindowDate("start_date", in.StartDate)
		if err != nil {
			return nil, searchOutput{}, err
		}
		end, err := parseWindowDate("end_date", in.EndDate)
		if err != nil {
			return nil, searchOutput{}, err
		}

		ts, err := deps.load()
		if err != nil {
			return nil, searchOutput{}, err
		}

		if !start.IsZero() || !end.IsZero() {
			from, to := start, end
			if from.IsZero() {
				from = ts.MinDate()
			}
			if to.IsZero() {
				to = ts.MaxDate()
			}
			ts = ts.FilterByDateRange(from, to)
		}
		if in.Category != "" {
			ts = ts.FilterByCategory(in.Category)
		}
		if in.Search != "" {
			ts = ts.FilterBySearch(in.Search)
		}
		switch strings.ToLower(in.Type) {
		case "":
		case "income":
			ts = ts.FilterByType(models.Income)
		case "outflow", "expense":
			ts = ts.FilterByType(models.Outflow)
		default:
			return nil, searchOutput{}, fmt.Errorf("type %q is not recognized; use \"income\" or \"outflow\"", in.Type)
		}
		ts = filterByAbsAmount(ts, in.MinAmount, in.MaxAmount)

		total := ts.Len()
		sum := ts.SumAmount()

		perPage := in.PerPage
		if perPage <= 0 {
			perPage = defaultSearchPerPage
		}
		if perPage > maxSearchPerPage {
			perPage = maxSearchPerPage
		}
		page := in.Page
		if page <= 0 {
			page = 1
		}

		sorted := ts.SortByDateDesc()
		totalPages := sorted.TotalPages(perPage)
		paged := sorted.Paginate(page, perPage)

		rows := make([]transactionRow, 0, paged.Len())
		for _, t := range paged.Transactions {
			rows = append(rows, transactionRow{
				Date:        t.Date.Format("2006-01-02"),
				Description: t.Label(),
				Category:    t.Category,
				Amount:      t.Amount,
				Type:        string(t.TransactionType),
			})
		}

		return nil, searchOutput{
			Total:        total,
			Page:         page,
			PerPage:      perPage,
			TotalPages:   totalPages,
			SumAmount:    round0(sum*100) / 100,
			Transactions: rows,
		}, nil
	})
}
```

Note `t.Label()` rather than `t.Description`: it is what the pages display, and it carries the Amazon enrichment label when one exists.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/services/mcpsvc/spend/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/services/mcpsvc/spend
git commit -m "feat(mcp): add search_transactions and split spend tools per file"
```

---

### Task 2: Extract dashboard metrics, add `summarize_spending`

**Files:**
- Modify: `internal/services/metrics/metrics.go`
- Create: `internal/services/metrics/metrics_test.go` (receives the moved dashboard tests)
- Modify: `internal/handlers/dashboard/handlers.go`
- Modify: `internal/handlers/dashboard/handlers_test.go` (the moved tests leave)
- Create: `internal/services/mcpsvc/spend/summary.go` + `summary_test.go`
- Modify: `internal/services/mcpsvc/spend/register.go`, `internal/services/mcpsvc/server.go`

**Read this before starting.** `internal/services/metrics` is **dead code**: `grep -rn "metrics\.New()\|metrics\.Service"` outside the package returns nothing. Its `CalculateMetrics` is an older, poorer version of the live one in `internal/handlers/dashboard/handlers.go:652` — it has no budget target, no healthcare split, and derives months from transaction min/max rather than the selected range. Do not try to reconcile the two implementations or keep both. The live handler implementation wins; the dead one is replaced.

**Interfaces:**
- Consumes: `spend.Deps` (Phase 1), `(Deps).load()`.
- Produces:
  - `metrics.Calculate(ts *models.TransactionSet, rangeStart, rangeEnd time.Time, budgetTarget, healthcareTarget float64) *models.DashboardMetrics`
  - `metrics.Comparison(data *models.TransactionSet, start, end time.Time, compType string, settings *models.WhatIfSettings) *models.PeriodComparison`
  - `metrics.PercentChange(current, previous float64) float64`
  - `metrics.MonthsBetween(start, end time.Time) float64`
  - `metrics.BudgetTargets(s *models.WhatIfSettings, rangeStart, rangeEnd time.Time) (living, healthcare float64)`
  - `spend.Deps` gains `Settings *retirement.SettingsManager`
  - Tool `summarize_spending`

- [ ] **Step 1: Replace the dead metrics implementation**

Delete the entire current body of `internal/services/metrics/metrics.go` below the package doc comment — `Service`, `New`, `CalculateMetrics`, `CalculateComparison`, `PercentChange`. Nothing calls them.

Then move these from `internal/handlers/dashboard/handlers.go` into `metrics.go`, renaming only as shown and changing nothing else:

| From (dashboard, unexported) | To (metrics, exported) |
|---|---|
| `calculateMetrics` (line 652) | `Calculate` |
| `calculateComparison` (line 797) | `Comparison` |
| `percentChange` (line 848) | `PercentChange` |
| `monthsBetween` (line 644) | `MonthsBetween` |
| `currentHealthcareTarget` (line 67) + `phaseAdjustedMonthlyTarget` (line 84) | fold into `BudgetTargets` (below) |

Move the `healthInsuranceCategory` constant with them. `Comparison` takes `settings *models.WhatIfSettings` — it must not reach for a package global.

`BudgetTargets` is new, wrapping the two target helpers so callers ask one question:

```go
// BudgetTargets returns the monthly living-expense and healthcare targets a
// plan implies over the given window. Both are zero when settings is nil,
// which callers read as "no target set" -- the same meaning the dashboard's
// hasBudgetTarget/hasHealthcareTarget flags carry.
func BudgetTargets(s *models.WhatIfSettings, rangeStart, rangeEnd time.Time) (living, healthcare float64) {
	if s == nil {
		return 0, 0
	}
	return phaseAdjustedMonthlyTarget(s, rangeStart, rangeEnd), currentHealthcareTarget(s)
}
```

Keep `phaseAdjustedMonthlyTarget` and `currentHealthcareTarget` unexported inside `metrics`.

`currentBudgetSettings` (dashboard line 46) **stays in the handler** — it reads the `retirementMgr` package global, which is handler wiring, not analysis.

- [ ] **Step 2: Point the handler at the service**

In `internal/handlers/dashboard/handlers.go`, replace each call site with the `metrics.` equivalent. Do not leave unexported forwarding wrappers behind — call `metrics.Calculate(...)` directly at the call sites. Use `LSP` `findReferences` on each moved function first to be sure you have them all.

- [ ] **Step 3: Move the covering tests**

Move these test functions from `internal/handlers/dashboard/handlers_test.go` into a new `internal/services/metrics/metrics_test.go`, changing only the package clause and the call from `calculateMetrics(` to `metrics.Calculate(` (etc.):

`TestCalculateComparison_PreviousPeriod`, `TestCalculateComparison_YearOverYear`, `TestCalculateComparison_NoComparisonData`, `TestCalculateComparison_InvalidType`, `TestCalculateMetrics_BasicTotals`, `TestCalculateMetrics_ZeroIncome`, `TestCalculateMetrics_TrendsLimitedToSixMonths`, `TestCalculateMetrics_MonthsInRange_ApproxFromDates`, `TestCalculateMetrics_ActualMonthly_DividesExpensesByMonths`, `TestCalculateMetrics_BudgetOverTarget`, `TestCalculateMetrics_BudgetUnderTarget`, `TestCalculateMetrics_NoBudgetTarget`, `TestCalculateMetrics_HealthcareUnderTarget`, `TestCalculateMetrics_HealthcareOverTarget`, `TestCalculateMetrics_HealthcareIgnoresOtherCategories`, `TestCalculateMetrics_HealthcareCategoryCaseInsensitive`, `TestCalculateMetrics_NoHealthcareTarget`, `TestCalculateMetrics_LivingExpensesExcludeHealthInsurance`, `TestCalculateMetrics_LivingExpensesTrendExcludesHealthcare`, `TestCalculateMetrics_HealthcareTrendPopulated`.

Move any helper they use. If a test also exercises handler-level behavior, leave it in the handler package and note it in your report rather than splitting it.

- [ ] **Step 4: Verify the extraction changed nothing**

Run: `go test ./internal/services/metrics/ ./internal/handlers/dashboard/ -v`
Expected: PASS. Record `go tool cover -func` for both packages before and after — no function may lose coverage.

- [ ] **Step 5: Commit the extraction on its own**

```bash
git add internal/services/metrics internal/handlers/dashboard
git commit -m "refactor(metrics): extract live dashboard metrics out of the handler"
```

- [ ] **Step 6: Write the failing tool test**

Create `internal/services/mcpsvc/spend/summary_test.go`:

```go
package spend

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSummarizeSpendingTotalsByCategoryAndMonth(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "summarize_spending",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("summarize_spending returned an error: %+v", res.Content)
	}

	var out summaryOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.TotalIncome != 5000 {
		t.Errorf("total_income = %v, want 5000", out.TotalIncome)
	}
	// 15.99 + 204.10 + 15.99, reported as a positive figure.
	if out.TotalExpenses != 236.08 {
		t.Errorf("total_expenses = %v, want 236.08", out.TotalExpenses)
	}

	byCat := map[string]float64{}
	for _, c := range out.ByCategory {
		byCat[c.Category] = c.Amount
	}
	if byCat["Entertainment"] != 31.98 {
		t.Errorf("Entertainment = %v, want 31.98", byCat["Entertainment"])
	}
	if len(out.ByMonth) != 2 {
		t.Errorf("by_month has %d entries, want 2: %+v", len(out.ByMonth), out.ByMonth)
	}
}

// With no settings manager there is no plan to compare against; the tool must
// still answer, omitting the budget block rather than failing or reporting a
// zero target as if it were real.
func TestSummarizeSpendingOmitsBudgetWhenNoSettingsAreWired(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "summarize_spending",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("summarize_spending returned an error: %+v", res.Content)
	}

	var out summaryOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Budget != nil {
		t.Errorf("budget = %+v, want nil when no settings manager is wired", out.Budget)
	}
}
```

- [ ] **Step 7: Run it to verify it fails**

Run: `go test ./internal/services/mcpsvc/spend/ -run TestSummarizeSpending -v`
Expected: FAIL — `undefined: summaryOutput`.

- [ ] **Step 8: Implement `summary.go`**

Add `Settings *retirement.SettingsManager` to `spend.Deps` (documented as optional: when nil, `summarize_spending` omits the budget comparison). Then:

```go
type summaryInput struct {
	StartDate string `json:"start_date,omitempty" jsonschema:"earliest date to include, inclusive, YYYY-MM-DD"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"latest date to include, inclusive, YYYY-MM-DD"`
	TopN      int    `json:"top_n,omitempty" jsonschema:"how many categories and merchants to return, default 10"`
}

type namedAmount struct {
	Category string  `json:"category,omitempty"`
	Merchant string  `json:"merchant,omitempty"`
	Month    string  `json:"month,omitempty"`
	Amount   float64 `json:"amount"`
	Count    int     `json:"count,omitempty"`
}

type budgetView struct {
	LivingTarget       float64 `json:"living_monthly_target"`
	LivingActual       float64 `json:"living_monthly_actual"`
	LivingDelta        float64 `json:"living_monthly_delta"`
	HealthcareTarget   float64 `json:"healthcare_monthly_target"`
	HealthcareActual   float64 `json:"healthcare_monthly_actual"`
	HealthcareDelta    float64 `json:"healthcare_monthly_delta"`
	MonthsInRange      float64 `json:"months_in_range"`
	CumulativeDelta    float64 `json:"cumulative_delta"`
}

type summaryOutput struct {
	Start         string        `json:"start"`
	End           string        `json:"end"`
	TotalIncome   float64       `json:"total_income"`
	TotalExpenses float64       `json:"total_expenses"`
	NetSavings    float64       `json:"net_savings"`
	SavingsRate   float64       `json:"savings_rate"`
	ByCategory    []namedAmount `json:"by_category"`
	ByMerchant    []namedAmount `json:"by_merchant"`
	ByMonth       []namedAmount `json:"by_month"`
	Budget        *budgetView   `json:"budget,omitempty"`
}
```

The handler resolves the window (defaulting to the ledger's own min/max), calls `metrics.Calculate` for the headline and budget figures, builds `ByCategory` from `CategoryTotals()` and `ByMonth` from `MonthlyTotals()` (both sorted, expenses reported positive), and builds `ByMerchant` with `merchants.GroupTransactions` + `merchants.DisplayLabel` — the same fuzzy grouping `get_anomalies` and `get_price_creep` already use, so the tools agree with each other about what one merchant is. `TopN` defaults to 10 and truncates `ByCategory`/`ByMerchant` only; `ByMonth` is never truncated.

Budget block: when `deps.Settings` is nil, leave `Budget` nil. Otherwise load the active scenario's settings, call `metrics.BudgetTargets`, and populate from the `metrics.Calculate` result. When both targets are zero, still leave `Budget` nil — a zero target is "unset", and emitting zeros invites the model to report a 100% overrun.

Description (write it in this spirit, adjusting wording to the final field names):

```
"Totals for the transaction history over an optional date window: income, expenses, net savings and savings rate, plus breakdowns by category, by merchant, and by month. Expense figures here are POSITIVE dollar amounts (unlike search_transactions, which returns signed amounts). Merchants are grouped by the same fuzzy matching used by get_anomalies, so \"SAFEWAY #123\" and \"SAFEWAY #456\" count as one merchant. by_category and by_merchant are limited to top_n entries (default 10) sorted by amount; by_month is always complete. The budget block appears only when a retirement plan with a spending target is configured, and compares actual monthly spending against that plan's target for this window, with healthcare tracked separately from living expenses."
```

- [ ] **Step 9: Wire `Settings` through**

In `internal/services/mcpsvc/server.go`, pass `Settings: deps.Settings` into `spend.Deps`. Extend the tool-inventory test in `mcpsvc/server_test.go` to expect the new tool count.

- [ ] **Step 10: Run the tests to verify they pass**

Run: `go test ./internal/services/mcpsvc/... -v`
Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/services/mcpsvc
git commit -m "feat(mcp): add summarize_spending"
```

---

### Task 3: Extract recurring detection, add `get_recurring`

**Files:**
- Create: `internal/services/insights/recurring.go` + `recurring_test.go`
- Modify: `internal/handlers/insights/handlers.go`, `internal/handlers/insights/handlers_test.go`
- Create: `internal/services/mcpsvc/spend/recurring.go` + `recurring_test.go`
- Modify: `internal/services/mcpsvc/spend/register.go`

**Interfaces:**
- Consumes: `spend.Deps` with `Transactions`, `Store`.
- Produces:
  - `insights.DetectRecurring(ts *models.TransactionSet) []models.RecurringPayment`
  - `insights.DetectRecurringAt(ts *models.TransactionSet, referenceDate time.Time) []models.RecurringPayment`
  - `insights.IsSubscription(rp models.RecurringPayment) bool`
  - `insights.ReferenceDate(ts *models.TransactionSet, referenceDate time.Time) time.Time`
  - `insights.TransactionSetForRecurring(ts *models.TransactionSet, referenceDate time.Time) *models.TransactionSet`
  - Tool `get_recurring`

- [ ] **Step 1: Extract into `internal/services/insights`**

Create the package and move these from `internal/handlers/insights/handlers.go`, exporting the first five and keeping the rest unexported:

| From | To |
|---|---|
| `detectRecurringPayments` (line 355) | `DetectRecurring` |
| `detectRecurringPaymentsAt` (line 359) | `DetectRecurringAt` |
| `isSubscription` (line 81) | `IsSubscription` |
| `recurringReferenceDate` (line 134) | `ReferenceDate` |
| `recurringTransactionSet` (line 146) | `TransactionSetForRecurring` |
| `detectByAmount` (line 600) | `detectByAmount` (unexported) |
| `mergeSimilarGroups` (line 300) | `mergeSimilarGroups` (unexported) |
| `recurringFreshnessWindow` (line 112) | `recurringFreshnessWindow` (unexported) |
| `recurringPaymentIsActive` (line 129) | `recurringPaymentIsActive` (unexported) |

`annotateRecurringWithMajorExpense` (line 157) **stays in the handler** — it reads the `loader` package global. The pure function it delegates to, `majorexpenses.AnnotateRecurringPayments`, is already a service and is what the MCP tool will call directly.

- [ ] **Step 2: Point the handler at the service and move the tests**

Update the handler's call sites (use `LSP` `findReferences` first). Move these tests from `internal/handlers/insights/handlers_test.go` into `internal/services/insights/recurring_test.go`, changing only the package clause and the call names:

`TestMergeSimlarGroups_SubstringMatch`, `TestMergeSimlarGroups_DotComStripping`, `TestMergeSimlarGroups_NoFalsePositives`, `TestMergeSimlarGroups_PreservesCanonicalName`, `TestMergeSimlarGroups_MultipleSuffixes`, `TestMergeSimlarGroups_EmptyInput`, `TestMergeSimlarGroups_SingleGroup`, `TestDetectRecurringPayments_FuzzyMergedVendor`, `TestDetectRecurringPayments_ExactMatchStillWorks`, `TestIsSubscription`, `TestIsSubscription_UnknownFrequency`, `TestIsSubscription_YearlyAndQuarterly`, `TestDetectRecurringPayments_AmountBasedGrouping`, `TestDetectRecurringPayments_AmountBasedSkipsTinyAmounts`, `TestDetectRecurringPayments_AmountBasedNoFalsePositivesDifferentAmounts`, `TestDetectRecurringPayments_AmountBasedSkipsAlreadyMatched`, `TestDetectRecurringPayments_AmountBasedIrregularIntervals`, `TestDetectRecurringPayments_LongHistoryPattern`, `TestDetectRecurringPayments_StrictMatchSkipsExpiredMonthly`, `TestDetectRecurringPayments_StrictMatchKeepsYearlyPaymentsCurrent`, `TestDetectRecurringPayments_UsesDatasetMaxDateForFreshness`, `TestDetectRecurringPaymentsAt_IgnoresTransactionsAfterReferenceDate`, `TestRecurringFreshnessWindow`, `TestRecurringReferenceDate_NilTSZeroRef`, `TestRecurringReferenceDate_ZeroRefUsesMaxDate`, `TestRecurringReferenceDate_ZeroRefEmptyTS`, `TestRecurringTransactionSet_NilTS`, `TestRecurringTransactionSet_ZeroDate`.

The two `TestCalculateInsights_*` tests exercise the handler's own assembly — leave them where they are.

- [ ] **Step 3: Verify the extraction changed nothing**

Run: `go test ./internal/services/insights/ ./internal/handlers/insights/ -v`
Expected: PASS. Record `go tool cover -func` before and after for both packages.

- [ ] **Step 4: Commit the extraction on its own**

```bash
git add internal/services/insights internal/handlers/insights
git commit -m "refactor(insights): extract recurring-payment detection into a service"
```

- [ ] **Step 5: Write the failing tool test**

Create `internal/services/mcpsvc/spend/recurring_test.go`:

```go
package spend

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// recurringFixture is twelve monthly charges from one merchant plus one
// unrelated one-off, anchored to a fixed reference month so freshness does
// not depend on the wall clock.
func recurringFixture() *models.TransactionSet {
	var txns []models.Transaction
	for i := 0; i < 12; i++ {
		txns = append(txns, models.Transaction{
			Date:            time.Date(2025, time.Month(1+i), 5, 0, 0, 0, 0, time.UTC),
			Description:     "NETFLIX",
			Category:        "Entertainment",
			Amount:          -15.99,
			TransactionType: models.Outflow,
		})
	}
	txns = append(txns, models.Transaction{
		Date:            time.Date(2025, 6, 14, 0, 0, 0, 0, time.UTC),
		Description:     "ROOF REPAIR",
		Category:        "Home",
		Amount:          -8400,
		TransactionType: models.Outflow,
	})
	return models.NewTransactionSet(txns)
}

func TestGetRecurringFindsTheMonthlyChargeAndNotTheOneOff(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: recurringFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_recurring",
		Arguments: map[string]any{"reference_date": "2025-12-31"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_recurring returned an error: %+v", res.Content)
	}

	var out recurringOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}

	var netflix *recurringRow
	for i := range out.Payments {
		if out.Payments[i].Description == "NETFLIX" {
			netflix = &out.Payments[i]
		}
		if out.Payments[i].Description == "ROOF REPAIR" {
			t.Errorf("a single one-off charge was reported as recurring: %+v", out.Payments[i])
		}
	}
	if netflix == nil {
		t.Fatalf("NETFLIX not detected as recurring; got %+v", out.Payments)
	}
	// models.RecurringPayment.Frequency is lowercase ("weekly", "monthly",
	// "yearly") -- do not title-case it on the way out.
	if netflix.Frequency != "monthly" {
		t.Errorf("frequency = %q, want monthly", netflix.Frequency)
	}
	if netflix.Occurrences != 12 {
		t.Errorf("occurrences = %d, want 12", netflix.Occurrences)
	}
	if !netflix.IsSubscription {
		t.Error("a recurring 15.99 monthly charge should be flagged as a subscription")
	}
}

// The reference date is the freshness cutoff: a series that stopped long
// before it is no longer active, and must not be reported as current.
func TestGetRecurringHonorsTheReferenceDateAsAFreshnessCutoff(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: recurringFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_recurring",
		Arguments: map[string]any{"reference_date": "2027-06-30"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_recurring returned an error: %+v", res.Content)
	}

	var out recurringOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	for _, p := range out.Payments {
		if p.Description == "NETFLIX" {
			t.Errorf("a series that ended 18 months before the reference date is not active: %+v", p)
		}
	}
}
```

The output types this implies:

```go
type recurringRow struct {
	Description      string  `json:"description"`
	Amount           float64 `json:"amount"`
	Frequency        string  `json:"frequency"`
	IntervalDays     float64 `json:"interval_days,omitempty"`
	LastDate         string  `json:"last_date"`
	NextExpected     string  `json:"next_expected,omitempty"`
	Occurrences      int     `json:"occurrences"`
	AnnualCost       float64 `json:"annual_cost"`
	IsSubscription   bool    `json:"is_subscription"`
	MajorExpenseName string  `json:"major_expense_name,omitempty"`
}

type recurringOutput struct {
	Count         int            `json:"count"`
	ReferenceDate string         `json:"reference_date"`
	Payments      []recurringRow `json:"payments"`
}
```

If the second test's premise turns out not to hold — i.e. `insights.DetectRecurringAt` reports the series as active even 18 months past its last occurrence — STOP and report it rather than deleting the test. `recurringFreshnessWindow` exists precisely to expire stale series, and a failure here is either a real bug or a misunderstanding worth surfacing before it is baked into a tool description.

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./internal/services/mcpsvc/spend/ -run TestGetRecurring -v`
Expected: FAIL — no `get_recurring` tool is registered.

- [ ] **Step 7: Implement `recurring.go`**

The tool takes an optional `reference_date` (defaulting to the ledger's max date, matching the page) and an optional `subscriptions_only` flag. It calls `insights.DetectRecurringAt`, annotates via `majorexpenses.AnnotateRecurringPayments` when `deps.Transactions` can supply definitions and pins, marks each row with `insights.IsSubscription`, and returns merchant, amount, frequency, interval days, last date, count, subscription flag, and major-expense name.

Annotation needs `LoadMajorExpenses()` and `LoadTransactionPins()`, which live on `*dataloader.DataLoader` and are not on the `TransactionSource` interface. Widen the interface rather than reaching for a concrete type:

```go
// MajorExpenseSource supplies the declared major expenses and manual pins used
// to label recurring payments. *dataloader.DataLoader satisfies it.
type MajorExpenseSource interface {
	LoadMajorExpenses() ([]models.MajorExpense, error)
	LoadTransactionPins() (map[string]string, error)
}
```

Add an optional `MajorExpenses MajorExpenseSource` field to `Deps`, wired from the same loader in `mcpsvc/server.go`. When it is nil or either load fails, return the payments unannotated rather than failing the call — the labels are a convenience, not the answer. Confirm both method signatures against `internal/services/dataloader` before writing the interface; if they differ, the real signatures win and you should note the correction in your report.

The Description must state that detection runs over the whole history, that `reference_date` controls the freshness cutoff (a payment is "active" only if seen recently enough for its interval), and that `is_subscription` is a heuristic over frequency and amount rather than a fact about the merchant.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/services/mcpsvc/... -v`
Expected: PASS. Update the tool-inventory test's expected count.

- [ ] **Step 9: Commit**

```bash
git add internal/services/mcpsvc
git commit -m "feat(mcp): add get_recurring"
```

---

### Task 4: Extract trends, income and velocity; add `get_trends`

**Files:**
- Create: `internal/services/insights/trends.go` + `trends_test.go`
- Modify: `internal/handlers/insights/handlers.go`, `internal/handlers/insights/handlers_test.go`
- Create: `internal/services/mcpsvc/spend/trends.go` + `trends_test.go`
- Modify: `internal/services/mcpsvc/spend/register.go`

**Interfaces:**
- Consumes: `spend.Deps` (now with `Transactions`, `Store`, `Settings`, `MajorExpenses`), `insights` package from Task 3.
- Produces:
  - `insights.CategoryTrends(ts *models.TransactionSet, currentStart, currentEnd time.Time) []models.CategoryTrend`
  - `insights.MajorExpenseTrends(ts *models.TransactionSet, defs []models.MajorExpense, pins map[string]string, currentStart, currentEnd time.Time) []models.CategoryTrend`
  - `insights.IncomePatterns(ts *models.TransactionSet) []models.IncomePattern`
  - `insights.SpendingVelocity(currentPeriod, allData *models.TransactionSet) *models.SpendingVelocity`
  - Tool `get_trends`

- [ ] **Step 1: Extract into `internal/services/insights/trends.go`**

Move from `internal/handlers/insights/handlers.go`:

| From | To |
|---|---|
| `analyzeCategoryTrends` (line 828) | `CategoryTrends` |
| `analyzeMajorExpenseTrends` (line 736) | `MajorExpenseTrends` |
| `AnalyzeIncomePatterns` (line 901) | `IncomePatterns` |
| `calculateSpendingVelocity` (line 991) | `SpendingVelocity` |

`AnalyzeIncomePatterns` is already exported from the handler package — run `LSP` `findReferences` on it before moving, and update every caller.

- [ ] **Step 2: Point the handler at the service and move the tests**

Move these into `internal/services/insights/trends_test.go`: `TestAnalyzeCategoryTrends_BasicUpDown`, `TestAnalyzeCategoryTrends_StableCategory`, `TestAnalyzeCategoryTrends_MaxTenCategories`, `TestAnalyzeCategoryTrends_NewCategory`, `TestCalculateSpendingVelocity_BasicCalculation`, `TestCalculateSpendingVelocity_EmptyData`, `TestCalculateSpendingVelocity_RefundReducesDailyAverage`, `TestAnalyzeIncomePatterns_MonthlyIncome`, `TestAnalyzeIncomePatterns_IrregularIncome`, `TestAnalyzeIncomePatterns_MaxTenPatterns`.

Also move any `analyzeMajorExpenseTrends` tests — find them with `grep -n "analyzeMajorExpenseTrends" internal/handlers/insights/handlers_test.go`; the list above was built from test names and may not have caught them.

- [ ] **Step 3: Verify the extraction changed nothing**

Run: `go test ./internal/services/insights/ ./internal/handlers/insights/ -v`
Expected: PASS. Record coverage before and after.

- [ ] **Step 4: Commit the extraction on its own**

```bash
git add internal/services/insights internal/handlers/insights
git commit -m "refactor(insights): extract trend, income and velocity analysis into the service"
```

- [ ] **Step 5: Write the failing tool test**

Create `internal/services/mcpsvc/spend/trends_test.go`:

```go
package spend

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// trendsFixture covers two adjacent equal-length windows: January (prior) and
// February (current). Dining doubles between them; Groceries is flat. A
// monthly paycheck runs through both.
func trendsFixture() *models.TransactionSet {
	day := func(m, d int) time.Time { return time.Date(2026, time.Month(m), d, 0, 0, 0, 0, time.UTC) }
	return models.NewTransactionSet([]models.Transaction{
		{Date: day(1, 10), Description: "BISTRO", Category: "Dining", Amount: -100, TransactionType: models.Outflow},
		{Date: day(2, 10), Description: "BISTRO", Category: "Dining", Amount: -200, TransactionType: models.Outflow},
		{Date: day(1, 12), Description: "SAFEWAY", Category: "Groceries", Amount: -400, TransactionType: models.Outflow},
		{Date: day(2, 12), Description: "SAFEWAY", Category: "Groceries", Amount: -400, TransactionType: models.Outflow},
		{Date: day(1, 1), Description: "PAYCHECK", Category: "Income", Amount: 5000, TransactionType: models.Income},
		{Date: day(2, 1), Description: "PAYCHECK", Category: "Income", Amount: 5000, TransactionType: models.Income},
	})
}

func TestGetTrendsComparesTheWindowAgainstThePrecedingOne(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: trendsFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_trends",
		Arguments: map[string]any{
			"start_date": "2026-02-01",
			"end_date":   "2026-02-28",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_trends returned an error: %+v", res.Content)
	}

	var out trendsOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}

	byCat := map[string]categoryTrendRow{}
	for _, c := range out.CategoryTrends {
		byCat[c.Category] = c
	}
	dining, ok := byCat["Dining"]
	if !ok {
		t.Fatalf("Dining missing from category_trends: %+v", out.CategoryTrends)
	}
	if dining.CurrentAmount != 200 || dining.PreviousAmount != 100 {
		t.Errorf("Dining current/previous = %v/%v, want 200/100", dining.CurrentAmount, dining.PreviousAmount)
	}
	if dining.PercentChange != 100 {
		t.Errorf("Dining percent_change = %v, want 100", dining.PercentChange)
	}
	groceries, ok := byCat["Groceries"]
	if !ok {
		t.Fatalf("Groceries missing from category_trends: %+v", out.CategoryTrends)
	}
	if groceries.PercentChange != 0 {
		t.Errorf("Groceries percent_change = %v, want 0 for flat spending", groceries.PercentChange)
	}
}

func TestGetTrendsSurfacesTheMonthlyPaycheck(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: trendsFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_trends",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_trends returned an error: %+v", res.Content)
	}

	var out trendsOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	found := false
	for _, p := range out.IncomePatterns {
		if p.Source == "PAYCHECK" {
			found = true
		}
	}
	if !found {
		t.Errorf("PAYCHECK not reported in income_patterns: %+v", out.IncomePatterns)
	}
}
```

Read `models.CategoryTrend` and `models.IncomePattern` (`internal/models/insights.go`) before writing the row types — the field names above (`CurrentAmount`, `PreviousAmount`, `PercentChange`, `Source`) are the shape the tool should expose, but the model's own field names are authoritative. If they differ, follow the model and adjust the test to match; note the correction in your report.

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./internal/services/mcpsvc/spend/ -run TestGetTrends -v`
Expected: FAIL — no `get_trends` tool is registered.

- [ ] **Step 7: Implement `trends.go`**

The tool takes an optional window (`start_date`/`end_date`, defaulting to the last full month present in the ledger) and returns four blocks: `category_trends`, `major_expense_trends`, `income_patterns`, and `velocity`. Major-expense trends are omitted when `deps.MajorExpenses` is nil or its loads fail — same rule as `get_recurring`.

The Description must state that trends compare the selected window against the immediately preceding window of equal length, that the comparison is against that prior window and not a long-run average, and that velocity is a pace figure (spend per day so far) rather than a forecast.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/services/mcpsvc/... -v`
Expected: PASS. Update the tool-inventory test's expected count to 12.

- [ ] **Step 9: Commit**

```bash
git add internal/services/mcpsvc
git commit -m "feat(mcp): add get_trends"
```

---

### Task 5: Server instructions and documentation

The Phase 1 plan left this open: `serverInstructions` still describes a retirement-only toolset, which is now wrong in a way the consuming model reads on every connection.

**Files:**
- Modify: `internal/services/mcpsvc/server.go`
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-08-12-app-wide-mcp-design.md`

- [ ] **Step 1: Rewrite `serverInstructions`**

It currently opens "These tools read and re-run a personal retirement projection for one household." Rewrite so it covers both families and keeps every rule the current text carries: ground answers in returned figures rather than recomputing; read `whatif://assumptions` before drawing conclusions from a projection; `apply_changes` writes and `run_scenario` does not; prefer `run_scenario` while exploring.

Add the rules the spending tools need, which the old text never had to say:
- The ledger is what was actually spent; the projection is a model of the future. Do not present one as evidence for the other without saying which is which.
- Expense amounts are signed in `search_transactions` and positive in `summarize_spending` — read the field description rather than assuming.
- Merchant grouping is fuzzy; a "merchant" is a group of similar descriptions, not a verified counterparty.

- [ ] **Step 2: Update the README tool list**

The `## Talking to your plan (MCP)` section lists eight tools. Add the four new ones with a one-line description each, in a "spending tools" paragraph parallel to the existing planner paragraph. Do not touch the security paragraph — it was corrected at the end of Phase 1 and is accurate.

- [ ] **Step 3: Mark Phase 2 done in the design doc**

In `docs/superpowers/specs/2026-08-12-app-wide-mcp-design.md`, update the Phases section to record Phase 2 as implemented, and update the "Where the logic lives" section: it says the insights and dashboard analysis is unexported inside the handler packages, which after this phase is only partly true. Name what moved and what stayed (`annotateRecurringWithMajorExpense`, `currentBudgetSettings`, `calculateInsights`, and the chart builders remain handler-side).

- [ ] **Step 4: Verify and commit**

Run: `go build ./... && go vet ./... && go test ./... && staticcheck ./...`

```bash
git add internal/services/mcpsvc README.md docs/superpowers/specs
git commit -m "docs(mcp): describe the spending tools in the server instructions and README"
```

---

## Definition of done

- `go build ./... && go vet ./... && go test ./... && staticcheck ./...` all green.
- Twelve tools registered and callable: the six planner tools, `get_anomalies`, `get_price_creep`, `search_transactions`, `summarize_spending`, `get_recurring`, `get_trends`.
- The tool-inventory test in `mcpsvc/server_test.go` asserts all twelve by name.
- `internal/services/insights` and `internal/services/metrics` hold the extracted logic; neither imports any `handlers` package.
- **No coverage regression.** Every extraction task's report shows `go tool cover -func` before and after for both the source and destination packages, and no function that had coverage lost it.
- No extracted function changed behavior: the moved tests pass unmodified apart from package clause and call name.

## Follow-ups this phase deliberately leaves open

- `calculateInsights`, the chart builders (`buildSpendingTrendChartData`, `buildMerchantsChartData`, `buildCumulativeChartData`, `buildBudgetVsActualChartData`, `buildMajorExpenseChartData`), and `bucketMajorExpenses` stay in the handler packages. They shape data for templates and Plotly, not for analysis, and no MCP tool needs them.
- `spend` tools reload the ledger through `dataloader.LoadData` on every call, with no caching. Six tools now share that cost; if it becomes hot, cache at the `Deps` level rather than per tool.
- Phase 3 (`curate`) and Phase 4 (`admin`) are unstarted. The `Snapshotter` moves from `plan` up to `mcpsvc` when Phase 3's writes need it.
