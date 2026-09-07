package spend

import (
	"budget2/internal/models"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"math"
	"testing"
	"time"
)

func TestDI3SummaryReportingRounding(t *testing.T) {
	for _, tc := range []struct {
		name                                                  string
		income, spend, refund, wantIncome, wantSpend, wantNet float64
	}{
		{"fractional", 10.006, .004, 0, 10.01, 0, 10.01},
		{"boundary", 2.675, .004, 0, 2.67, 0, 2.67},
		{"refund", 10, 1, 3, 10, -2, 12},
		{"zero", 10.001, 10.004, 0, 10, 10, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			date := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
			ts := models.NewTransactionSet([]models.Transaction{
				{Date: date, Amount: tc.income, TransactionType: models.Income},
				{Date: date, Amount: -tc.spend, TransactionType: models.Outflow},
				{Date: date, Amount: tc.refund, TransactionType: models.Outflow},
			})
			cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}})
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "summarize_spending", Arguments: map[string]any{}})
			if err != nil || res.IsError {
				t.Fatalf("tool: %v %v", res, err)
			}
			var out summaryOutput
			if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
				t.Fatal(err)
			}
			if out.TotalIncome != tc.wantIncome || out.TotalExpenses != tc.wantSpend || out.NetSavings != tc.wantNet {
				t.Errorf("got income=%v spend=%v net=%v", out.TotalIncome, out.TotalExpenses, out.NetSavings)
			}
			if out.NetSavings == 0 && math.Signbit(out.NetSavings) {
				t.Error("negative zero")
			}
		})
	}
}

func TestDI3SummaryMonthlyAdjustment(t *testing.T) {
	ts := models.NewTransactionSet([]models.Transaction{
		{Date: time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC), Amount: -.004, TransactionType: models.Outflow},
		{Date: time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC), Amount: -.004, TransactionType: models.Outflow},
	})
	cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "summarize_spending", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("tool: %v %v", res, err)
	}
	var out struct {
		Total      float64       `json:"total_expenses"`
		Rows       []namedAmount `json:"by_month"`
		Adjustment *float64      `json:"monthly_rounding_adjustment"`
	}
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &out); err != nil {
		t.Fatal(err)
	}
	if out.Total != .01 || len(out.Rows) != 2 || out.Rows[0].Amount != 0 || out.Rows[1].Amount != 0 || out.Adjustment == nil || *out.Adjustment != .01 {
		t.Fatalf("got %+v", out)
	}
}
