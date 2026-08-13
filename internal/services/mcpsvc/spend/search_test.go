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
