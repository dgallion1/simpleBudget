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
