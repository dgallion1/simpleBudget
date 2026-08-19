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

// searchFixtureWithTransfer is the spec's defect fixture: one income, one
// outflow, and one transfer. With no type filter the pre-fix code summed all
// three (5000 + -1200 + -3000 = 800), netting the $3,000 transfer into a
// spending total; the correct sum (income + outflow, matching
// summarize_spending's net_savings for the same window) is 3800.
func searchFixtureWithTransfer() *models.TransactionSet {
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			panic(err)
		}
		return d
	}
	return models.NewTransactionSet([]models.Transaction{
		{Date: day("2026-01-05"), Description: "PAYCHECK", Category: "Income", Amount: 5000, TransactionType: models.Income},
		{Date: day("2026-01-10"), Description: "RENT", Category: "Housing", Amount: -1200, TransactionType: models.Outflow},
		{Date: day("2026-01-15"), Description: "TRANSFER TO BROKERAGE", Category: "Transfer", Amount: -3000, TransactionType: models.Transfer},
	})
}

// TestSearchTransactionsDefaultSumExcludesTransfers pins the invariant
// server.go promises ("a Transfer is excluded from both totals by type
// filter"): on the default path (no type filter) sum_amount must NOT net a
// Transfer into the spending total. The transfer row stays listed (the
// ledger remains visible), but the sum covers Income + Outflow only, so it
// agrees with summarize_spending's net_savings for the same window.
//
// Against the pre-fix code this fails: sum_amount comes back 800 (the
// transfer netted in) rather than 3800.
func TestSearchTransactionsDefaultSumExcludesTransfers(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixtureWithTransfer()}})

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

	// The transfer row stays listed -- the ledger remains visible.
	if out.Total != 3 {
		t.Fatalf("total = %d, want 3 (the transfer row must still be listed): %+v", out.Total, out.Transactions)
	}
	// But the sum must not net it in. 5000 + -1200 = 3800; the -3000 transfer
	// is neither income nor expense and must be excluded.
	const wantSum = 3800.0
	if out.SumAmount == 800.0 {
		t.Fatalf("sum_amount = %v matches the NETTED-IN transfer total (5000 + -1200 + -3000); the transfer must be excluded from the default sum", out.SumAmount)
	}
	if out.SumAmount != wantSum {
		t.Errorf("sum_amount = %v, want %v (Income + Outflow only; the Transfer is listed but not summed)", out.SumAmount, wantSum)
	}
}

// TestSearchTransactionsFiltersByTypeTransfer covers the missing "transfer"
// branch of the type switch: a model must be able to ask "show me my
// transfers", matching what the explorer UI already accepts. Against the
// pre-fix code this fails because type:"transfer" hits the switch's default
// branch and returns a tool error ("not recognized").
func TestSearchTransactionsFiltersByTypeTransfer(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: searchFixtureWithTransfer()}})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_transactions",
		Arguments: map[string]any{"type": "transfer"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("type \"transfer\" must be a valid filter, not an error: %+v", res.Content)
	}

	var out searchOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	if out.Total != 1 {
		t.Fatalf("total = %d, want 1 (the single Transfer row): %+v", out.Total, out.Transactions)
	}
	if out.Transactions[0].Type != "Transfer" {
		t.Errorf("row type = %q, want \"Transfer\"", out.Transactions[0].Type)
	}
	if out.Transactions[0].Amount != -3000 {
		t.Errorf("amount = %v, want -3000", out.Transactions[0].Amount)
	}
	// With an explicit type=transfer filter, sum_amount is the signed sum over
	// the transfer rows alone.
	if out.SumAmount != -3000 {
		t.Errorf("sum_amount = %v, want -3000 (the transfer sum)", out.SumAmount)
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
