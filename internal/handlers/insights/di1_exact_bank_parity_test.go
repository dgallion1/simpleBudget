package insights

import (
	"budget2/internal/models"
	"budget2/internal/services/mcpsvc/spend"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
	"time"
)

func TestDI1AdversarialExactBankUIAndMCP(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	var tx []models.Transaction
	for _, row := range []struct{ display, original, category string }{
		{"Cloud Storage", "CLOUDBOX*STORAGE PLAN CLOUDBOX.COM CA", "Electronics & Software"},
		{"AutoLoan Finance", "AUTOLOAN FINANCE BILL PMT", "Auto Payment"},
	} {
		for i := 0; i < 3; i++ {
			tx = append(tx, models.Transaction{Description: row.display, OriginalDescription: row.original, Category: row.category, Amount: -25, Date: now.AddDate(0, -i, 0), TransactionType: models.Outflow})
		}
	}
	ts := models.NewTransactionSet(tx)
	ui := calculateInsights(ts, ts, ts.MinDate(), now)
	t.Logf("UI subscriptions=%d bills=%d other=%d monthlySubscriptions=%.2f totalAnnual=%.2f", len(ui.Subscriptions), len(ui.RecurringPayments), len(ui.OtherRecurring), ui.MonthlySubscriptions, ui.TotalRecurring)
	if len(ui.Subscriptions) != 1 || len(ui.RecurringPayments) != 1 || len(ui.OtherRecurring) != 0 || ui.MonthlySubscriptions != 25 {
		t.Error("UI classification differs from required subscription/bill grouping")
	}
	ctx := context.Background()
	srv := mcp.NewServer(&mcp.Implementation{Name: "synthetic-di1-second", Version: "test"}, nil)
	spend.Register(srv, spend.Deps{Transactions: di1Transactions{ts}})
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "synthetic-di1-second", Version: "test"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, only := range []bool{false, true} {
		result, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_recurring", Arguments: map[string]any{"reference_date": "2026-08-15", "subscriptions_only": only}})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("MCP error: %+v", result.Content)
		}
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("MCP subscriptions_only=%v %s", only, raw)
		var out struct {
			Count    int
			Payments []struct{ Description, Classification string }
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		if only {
			if out.Count != 1 {
				t.Errorf("subscription-filtered MCP count=%d want 1", out.Count)
			}
		} else {
			for _, p := range out.Payments {
				want := "bill"
				if p.Description == "cloud storage" {
					want = "subscription"
				}
				if p.Classification != want {
					t.Errorf("MCP %q classification=%s want %s", p.Description, p.Classification, want)
				}
			}
		}
	}
}
