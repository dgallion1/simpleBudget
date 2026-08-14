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

// TestSearchTransactionsFiltersByAmountRange exercises filterByAbsAmount's
// actual comparison loop: min_amount 50 and max_amount 250 bracket only
// SAFEWAY (204.10), excluding both the 15.99 NETFLIX charges (below min)
// and the 5000 PAYCHECK (above max). Without this test neither the
// min>0 nor the max>0 branch in filterByAbsAmount had ever executed.
func TestSearchTransactionsFiltersByAmountRange(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_transactions",
		Arguments: map[string]any{"min_amount": 50, "max_amount": 250},
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
	if out.Transactions[0].Description != "SAFEWAY" {
		t.Errorf("description = %q, want SAFEWAY", out.Transactions[0].Description)
	}
	if out.Transactions[0].Amount != -204.10 {
		t.Errorf("amount = %v, want -204.10", out.Transactions[0].Amount)
	}
}

// TestSearchTransactionsFiltersByType covers both the "income" and
// "outflow" branches of the type switch, plus "expense" as the documented
// alias for "outflow".
func TestSearchTransactionsFiltersByType(t *testing.T) {
	cases := []struct {
		name      string
		typeParam string
		wantTotal int
	}{
		{"income", "income", 1},
		{"outflow", "outflow", 3},
		{"expense alias for outflow", "expense", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      "search_transactions",
				Arguments: map[string]any{"type": tc.typeParam},
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
			if out.Total != tc.wantTotal {
				t.Errorf("total = %d, want %d: %+v", out.Total, tc.wantTotal, out.Transactions)
			}
		})
	}
}

// TestSearchTransactionsRejectsAnUnrecognizedType covers the switch's
// default branch: an unrecognized type value must fail as a tool error
// rather than silently matching everything.
func TestSearchTransactionsRejectsAnUnrecognizedType(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_transactions",
		Arguments: map[string]any{"type": "refund"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("an unrecognized type should be a tool error, not a silent no-op filter")
	}
}

// TestSearchTransactionsFiltersBySearchTerm exercises the search substring
// filter, which no prior test invoked.
func TestSearchTransactionsFiltersBySearchTerm(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixture()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_transactions",
		Arguments: map[string]any{"search": "paycheck"},
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
	if out.Transactions[0].Description != "PAYCHECK" {
		t.Errorf("description = %q, want PAYCHECK", out.Transactions[0].Description)
	}
}

// TestSearchTransactionsSumAmountCoversAllMatchesNotJustThePage pins the
// sharpest part of the tool's documented contract: sum_amount is the signed
// sum over every matching row, not just the page returned. A future change
// that moved the sum computation after Paginate would pass every other
// search test while silently breaking this promise, so the fixture and
// page size here are chosen so the two sums provably differ:
//   - full match sum: -15.99 (netflix) + -204.10 (safeway) + -15.99 (netflix) + 5000 (paycheck) = 4763.92
//   - page-1 (2 rows, newest first) sum:  -15.99 (netflix 02-05) + 5000 (paycheck 02-01) = 4984.01
func TestSearchTransactionsSumAmountCoversAllMatchesNotJustThePage(t *testing.T) {
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

	const wantFullSum = 4763.92
	const pageOnlySum = 4984.01
	if out.SumAmount == pageOnlySum {
		t.Fatalf("sum_amount = %v matches the PAGE sum, not the full match sum -- sum_amount must be computed before Paginate", out.SumAmount)
	}
	if out.SumAmount != wantFullSum {
		t.Errorf("sum_amount = %v, want %v (the signed sum over all 4 matches, not just the 2 returned rows)", out.SumAmount, wantFullSum)
	}
}

// TestSearchTransactionsExcludesSuppressedTransactions guards the fix for a
// review finding: every other aggregate in the app (dashboard,
// get_anomalies, get_price_creep, summarize_spending) excludes rows the user
// has already marked as a resolved duplicate, so search_transactions must
// too -- otherwise a model summing search results would silently disagree
// with summarize_spending's totals for the same window.
func TestSearchTransactionsExcludesSuppressedTransactions(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	ts := models.NewTransactionSet([]models.Transaction{
		{Date: day("2026-01-05"), Description: "NETFLIX", Category: "Entertainment", Amount: -15.99, TransactionType: models.Outflow},
		{Date: day("2026-01-05"), Description: "NETFLIX", Category: "Entertainment", Amount: -15.99, TransactionType: models.Outflow, Suppressed: true},
	})
	cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_transactions",
		Arguments: map[string]any{},
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
		t.Fatalf("total = %d, want 1 (the suppressed duplicate must be excluded)", out.Total)
	}
}

// TestSearchReturnsTheTransactionHash pins the field pin_transactions uses to
// address a row. Without it, "find this charge, then pin it" cannot be done
// with these tools at all.
func TestSearchReturnsTheTransactionHash(t *testing.T) {
	txn := models.Transaction{
		Date:            time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC),
		Description:     "CITY WATER DEPT",
		Category:        "Utilities",
		Amount:          -88.10,
		TransactionType: models.Outflow,
	}
	txn.Hash = txn.ComputeHash()

	cs := connect(t, Deps{Transactions: stubTransactions{
		ts: models.NewTransactionSet([]models.Transaction{txn}),
	}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_transactions",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	out := decodeToolResult[searchOutput](t, res)
	if len(out.Transactions) != 1 {
		t.Fatalf("got %d rows, want 1", len(out.Transactions))
	}
	if out.Transactions[0].Hash != txn.Hash {
		t.Errorf("hash = %q, want %q", out.Transactions[0].Hash, txn.Hash)
	}
}
