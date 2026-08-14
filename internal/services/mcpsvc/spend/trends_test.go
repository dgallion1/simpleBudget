package spend

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"budget2/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// trendsFixture covers two adjacent equal-length windows: January (prior)
// and February (current). Dining doubles between them; Groceries is flat. A
// monthly paycheck runs through both. It also plants a December 2025 Dining
// charge that predates the ACTUAL previous window CategoryTrends compares
// against -- for a Feb 1-28 (28-day) current window, that window is Jan
// 4-31, not the whole of January or "everything before February" -- so a
// window-honest comparison must exclude it from previous_amount, while a
// bug that compared the current window against ALL prior history would
// wrongly fold it in. Its description deliberately avoids "bistro" so it
// never matches the major-expense keyword tests below.
func trendsFixture() *models.TransactionSet {
	day := func(y, m, d int) time.Time { return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC) }
	return models.NewTransactionSet([]models.Transaction{
		{Date: day(2025, 12, 15), Description: "OLD STEAKHOUSE", Category: "Dining", Amount: -900, TransactionType: models.Outflow},
		{Date: day(2026, 1, 10), Description: "BISTRO", Category: "Dining", Amount: -100, TransactionType: models.Outflow},
		{Date: day(2026, 2, 10), Description: "BISTRO", Category: "Dining", Amount: -200, TransactionType: models.Outflow},
		{Date: day(2026, 1, 12), Description: "SAFEWAY", Category: "Groceries", Amount: -400, TransactionType: models.Outflow},
		{Date: day(2026, 2, 12), Description: "SAFEWAY", Category: "Groceries", Amount: -400, TransactionType: models.Outflow},
		{Date: day(2026, 1, 1), Description: "PAYCHECK", Category: "Income", Amount: 5000, TransactionType: models.Income},
		{Date: day(2026, 2, 1), Description: "PAYCHECK", Category: "Income", Amount: 5000, TransactionType: models.Income},
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

	// Pins the actual comparison window, not just the totals it produces:
	// for a 28-day current window (Feb 1-28 inclusive), CategoryTrends'
	// own "immediately preceding window of equal length" math is Jan 4-31
	// (duration = 27 days; prevStart = currentStart - duration - 1 day),
	// NOT the whole of January and NOT "everything before February". A
	// test that only checked the resulting totals could pass even if the
	// tool secretly compared against all prior history, since (absent the
	// December plant in trendsFixture) there is nothing before Jan 10
	// either way -- see trendsFixture's doc comment.
	if out.PreviousStart != "2026-01-04" || out.PreviousEnd != "2026-01-31" {
		t.Errorf("previous window = [%s, %s], want [2026-01-04, 2026-01-31]", out.PreviousStart, out.PreviousEnd)
	}

	byCat := map[string]categoryTrendRow{}
	for _, c := range out.CategoryTrends {
		byCat[c.Category] = c
	}
	dining, ok := byCat["Dining"]
	if !ok {
		t.Fatalf("Dining missing from category_trends: %+v", out.CategoryTrends)
	}
	// previous_amount must be 100 (the Jan 10 BISTRO charge only) and NOT
	// 1000 (100 + the Dec 15 OLD STEAKHOUSE plant) -- the December charge
	// falls before Jan 4, outside the actual previous window, so an
	// all-prior-history comparison would fail this exact assertion.
	if dining.CurrentAmount != 200 || dining.PreviousAmount != 100 {
		t.Errorf("Dining current/previous = %v/%v, want 200/100", dining.CurrentAmount, dining.PreviousAmount)
	}
	// models.CategoryTrend names this field ChangePercent, not PercentChange.
	if dining.ChangePercent != 100 {
		t.Errorf("Dining change_percent = %v, want 100", dining.ChangePercent)
	}
	groceries, ok := byCat["Groceries"]
	if !ok {
		t.Fatalf("Groceries missing from category_trends: %+v", out.CategoryTrends)
	}
	if groceries.ChangePercent != 0 {
		t.Errorf("Groceries change_percent = %v, want 0 for flat spending", groceries.ChangePercent)
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
		// models.IncomePattern names this field Description, not Source, and
		// IncomePatterns groups by the lower-cased, trimmed description --
		// "PAYCHECK" comes back as "paycheck".
		if p.Description == "paycheck" {
			found = true
			// A test that only checked for the row's presence would still
			// pass if the cadence classifier mis-called every source
			// "irregular" -- assert the cadence fields the row actually
			// carries, not just that a row with this name exists.
			if p.Frequency != "monthly" {
				t.Errorf("paycheck frequency = %q, want monthly", p.Frequency)
			}
			if !p.IsRegular {
				t.Error("paycheck should be flagged is_regular for two evenly-monthly occurrences")
			}
			if p.Occurrences != 2 {
				t.Errorf("paycheck occurrences = %d, want 2", p.Occurrences)
			}
		}
	}
	if !found {
		t.Errorf("paycheck not reported in income_patterns: %+v", out.IncomePatterns)
	}
}

// TestGetTrendsAnnotatesMajorExpenseTrendsWhenWired confirms major_expense_trends
// is populated -- via deps.MajorExpenses, reusing insights.MajorExpenseTrends --
// when both its loads succeed. stubMajorExpenses is defined in recurring_test.go.
func TestGetTrendsAnnotatesMajorExpenseTrendsWhenWired(t *testing.T) {
	cs := connect(t, Deps{
		Transactions: stubTransactions{ts: trendsFixture()},
		MajorExpenses: stubMajorExpenses{
			defs: []models.MajorExpense{
				{ID: "dining-out", Name: "Dining Out", Keywords: []string{"bistro"}},
			},
		},
	})

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
	found := false
	for _, c := range out.MajorExpenseTrends {
		if c.Category == "Dining Out" {
			found = true
			if c.CurrentAmount != 200 || c.PreviousAmount != 100 {
				t.Errorf("Dining Out current/previous = %v/%v, want 200/100", c.CurrentAmount, c.PreviousAmount)
			}
		}
	}
	if !found {
		t.Fatalf("Dining Out not found in major_expense_trends: %+v", out.MajorExpenseTrends)
	}
}

// TestGetTrendsOmitsMajorExpenseTrendsWhenLoadFails confirms a
// LoadMajorExpenses failure degrades to omitting major_expense_trends
// entirely rather than failing the whole call -- the block is a
// convenience, not the answer, matching get_recurring's own
// annotateMajorExpenses degradation.
func TestGetTrendsOmitsMajorExpenseTrendsWhenLoadFails(t *testing.T) {
	cs := connect(t, Deps{
		Transactions:  stubTransactions{ts: trendsFixture()},
		MajorExpenses: stubMajorExpenses{defsErr: errors.New("major_expenses.json is corrupt")},
	})

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
		t.Fatalf("a major-expenses load failure must not fail get_trends: %+v", res.Content)
	}

	var out trendsOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if len(out.MajorExpenseTrends) != 0 {
		t.Errorf("major_expense_trends should be omitted when definitions fail to load, got %+v", out.MajorExpenseTrends)
	}
}

// TestGetTrendsStillComputesMajorExpenseTrendsWhenPinsLoadFails mirrors the
// handler's own annotateRecurringWithMajorExpense (and get_recurring's
// analogous test): a LoadTransactionPins failure alone must not cost the
// definition-derived major_expense_trends, since MajorExpenseTrends accepts
// a nil pins map and pins are only an override on top of keyword/amount
// matching.
func TestGetTrendsStillComputesMajorExpenseTrendsWhenPinsLoadFails(t *testing.T) {
	cs := connect(t, Deps{
		Transactions: stubTransactions{ts: trendsFixture()},
		MajorExpenses: stubMajorExpenses{
			defs:    []models.MajorExpense{{ID: "dining-out", Name: "Dining Out", Keywords: []string{"bistro"}}},
			pinsErr: errors.New("transaction_pins.json is corrupt"),
		},
	})

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
		t.Fatalf("a transaction-pins load failure must not fail get_trends: %+v", res.Content)
	}

	var out trendsOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	found := false
	for _, c := range out.MajorExpenseTrends {
		if c.Category == "Dining Out" {
			found = true
		}
	}
	if !found {
		t.Errorf("Dining Out should still be computed from definitions alone when pins fail to load, got %+v", out.MajorExpenseTrends)
	}
}

// TestGetTrendsRejectsAnInvalidDate mirrors summarize_spending's invalid-date
// handling: a malformed date is a tool error, not a panic or a silently-
// ignored filter.
func TestGetTrendsRejectsAnInvalidDate(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: trendsFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_trends",
		Arguments: map[string]any{"start_date": "01/05/2026"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("get_trends should have reported the invalid date as a tool error")
	}
}

// TestGetTrendsReportsALoadFailureAsAToolError mirrors summarize_spending's
// load-failure handling.
func TestGetTrendsReportsALoadFailureAsAToolError(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{err: errors.New("storage is locked")}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_trends",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("get_trends should have reported the load failure as a tool error")
	}
}

// TestGetTrendsRejectsDefaultingAnEmptyLedger confirms an empty ledger (no
// transactions after suppression) produces a clear tool error rather than
// lastFullMonth silently wrapping the zero time into a nonsensical window
// like "0000-12-01" -- the true answer is "there is nothing to default
// from," not a fabricated date.
func TestGetTrendsRejectsDefaultingAnEmptyLedger(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: &models.TransactionSet{}}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_trends",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("get_trends should have reported an empty ledger as a tool error when defaulting the window")
	}
}

// TestGetTrendsRejectsAStartAfterTheDefaultedEnd confirms that supplying
// only start_date, later than the ledger's last full month, is a tool
// error rather than silently computing a negative-duration window (end
// before start) and a "previous" window that sits after the current one.
func TestGetTrendsRejectsAStartAfterTheDefaultedEnd(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: trendsFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_trends",
		Arguments: map[string]any{"start_date": "2026-06-01"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("get_trends should have reported start_date after the defaulted end_date as a tool error")
	}
}

// TestLastFullMonthWrapsToDecemberOfThePriorYear covers lastFullMonth's
// year-rollover branch: a ledger whose latest transaction falls in a
// partial January must fall back to December of the PRIOR year, not month
// zero of the same year.
func TestLastFullMonthWrapsToDecemberOfThePriorYear(t *testing.T) {
	start, end := lastFullMonth(time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC))

	wantStart := time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2025, time.December, 31, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Errorf("lastFullMonth(Jan 10 2026) = [%v, %v], want [%v, %v]", start, end, wantStart, wantEnd)
	}
}
