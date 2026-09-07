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

func TestDI5GroupingInvariant(t *testing.T) {
	for _, split := range []bool{false, true} {
		for _, top := range []int{1, 10} {
			for _, expense := range []float64{.004, .006, -.004, -.006} {
				ts := models.NewTransactionSet([]models.Transaction{
					{Date: time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC), Amount: 10.006, TransactionType: models.Income},
					{Date: time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC), Description: "Alder", Category: "Home", Amount: -expense, TransactionType: models.Outflow},
					{Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), Description: "Alder", Category: "Home", Amount: -expense, TransactionType: models.Outflow},
				})
				if split {
					ts.Transactions[2].Date = time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
					ts.Transactions[2].Description = "Birch"
					ts.Transactions[2].Category = "Shopping"
				}
				cs := connect(t, Deps{Transactions: stubTransactions{ts: ts}})
				result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "summarize_spending", Arguments: map[string]any{"start_date": "2026-01-01", "end_date": "2026-02-28", "top_n": top}})
				if err != nil || result.IsError {
					t.Fatalf("tool=%v error=%v", result, err)
				}
				var out summaryOutput
				if err := json.Unmarshal(mustJSON(t, result.StructuredContent), &out); err != nil {
					t.Fatal(err)
				}
				wantSpend := .01
				if expense < 0 {
					wantSpend = -.01
				}
				wantNet := 10.0
				if expense < 0 {
					wantNet = 10.02
				}
				if out.TotalIncome != 10.01 || out.TotalExpenses != wantSpend || out.NetSavings != wantNet {
					t.Fatalf("grouping changed anchors split=%v top=%d expense=%v: %+v", split, top, expense, out)
				}
				var monthCents int64
				for _, r := range out.ByMonth {
					monthCents += int64(math.Round(r.Amount * 100))
					if r.Amount == 0 && math.Signbit(r.Amount) {
						t.Fatal("negative zero month")
					}
				}
				if monthCents+int64(math.Round(out.MonthlyRoundingAdjustment*100)) != int64(math.Round(out.TotalExpenses*100)) {
					t.Fatalf("monthly reconciliation failed: %+v", out)
				}
				if out.MonthlyRoundingAdjustment == 0 && math.Signbit(out.MonthlyRoundingAdjustment) {
					t.Fatal("negative zero adjustment")
				}
				t.Logf("split=%v top=%d expense=%v expenseTotal=%v net=%v adjustment=%v", split, top, expense, out.TotalExpenses, out.NetSavings, out.MonthlyRoundingAdjustment)
			}
		}
	}
}
