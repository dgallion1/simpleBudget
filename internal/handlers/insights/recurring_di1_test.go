package insights

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/mcpsvc/spend"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type di1Transactions struct{ ts *models.TransactionSet }

func (s di1Transactions) LoadData() (*models.TransactionSet, error) { return s.ts, nil }

func TestDI1UncappedTotals(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	var txns []models.Transaction
	for _, name := range []string{"Alder", "Birch", "Cedar", "Dogwood", "Elm", "Fir", "Ginkgo", "Hazel", "Ivy", "Juniper", "Kapok", "Larch", "Maple", "Nutmeg", "Oak", "Pine", "Quince", "Rowan", "Spruce", "Teak", "Electric Company", "Netflix"} {
		amount := -100.0
		if name == "Netflix" {
			amount = -10
		}
		for i := 0; i < 3; i++ {
			txns = append(txns, models.Transaction{Description: name, Amount: amount, Date: now.AddDate(0, -i, 0), TransactionType: models.Outflow})
		}
	}
	ts := models.NewTransactionSet(txns)
	got := calculateInsights(ts, ts, ts.MinDate(), now)
	if got.TotalRecurring != 25320 || got.MonthlyRecurring != 2110 || got.MonthlySubscriptions != 10 {
		t.Fatalf("totals annual=%v monthly=%v subscriptions=%v; want 25320, 2110, 10", got.TotalRecurring, got.MonthlyRecurring, got.MonthlySubscriptions)
	}
	if len(got.Subscriptions) != 1 || len(got.RecurringPayments) != 1 || len(got.OtherRecurring) != 20 {
		t.Fatalf("UI grouping: %+v", got)
	}
	ui := map[string]models.RecurringPayment{}
	for _, group := range [][]models.RecurringPayment{got.Subscriptions, got.RecurringPayments, got.OtherRecurring} {
		for _, p := range group {
			ui[p.Description] = p
		}
	}
	ctx := context.Background()
	srv := mcp.NewServer(&mcp.Implementation{Name: "di1-synthetic", Version: "test"}, nil)
	spend.Register(srv, spend.Deps{Transactions: di1Transactions{ts}})
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "di1-test", Version: "test"}, nil)
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
			t.Fatalf("tool error: %+v", result.Content)
		}
		encoded, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Count    int
			Payments []struct {
				Description    string
				Classification string
				Reason         string  `json:"classification_reason"`
				AnnualCost     float64 `json:"annual_cost"`
				IsSubscription bool    `json:"is_subscription"`
			}
		}
		if err := json.Unmarshal(encoded, &out); err != nil {
			t.Fatal(err)
		}
		wantCount, wantAnnual := 22, 25320.0
		if only {
			wantCount, wantAnnual = 1, 120
		}
		if out.Count != wantCount || len(out.Payments) != wantCount {
			t.Fatalf("subscriptions_only=%v: count=%d rows=%d want %d", only, out.Count, len(out.Payments), wantCount)
		}
		total := 0.0
		seen := map[string]bool{}
		for _, p := range out.Payments {
			q, ok := ui[p.Description]
			if !ok || seen[p.Description] {
				t.Errorf("unexpected or duplicate MCP merchant %q", p.Description)
			}
			seen[p.Description] = true
			if p.Classification != q.Classification || p.Reason == "" || p.Reason != q.ClassificationReason || p.AnnualCost != q.AnnualCost || p.IsSubscription != (q.Classification == models.RecurringSubscription) {
				t.Errorf("UI/MCP mismatch: %+v versus %+v", q, p)
			}
			total += p.AnnualCost
		}
		if total != wantAnnual {
			t.Errorf("MCP annual total=%v want %v", total, wantAnnual)
		}
	}
}
