package spend

import (
	"budget2/internal/models"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
	"time"
)

func TestDI2CalendarMonthComparison(t *testing.T) {
	cs := connect(t, Deps{Transactions: stubTransactions{ts: trendsFixture()}})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_trends", Arguments: map[string]any{"start_date": "2026-02-01", "end_date": "2026-02-28"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}
	var got trendsOutput
	if err := json.Unmarshal(mustJSON(t, res.StructuredContent), &got); err != nil {
		t.Fatal(err)
	}
	if got.PreviousStart != "2026-01-01" || got.PreviousEnd != "2026-01-31" {
		t.Fatalf("February previous range %s..%s; want January 1..31", got.PreviousStart, got.PreviousEnd)
	}
}

func TestDI2MCPExplicitClockAndUnknownHistory(t *testing.T) {
	day := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ts := models.NewTransactionSet([]models.Transaction{{Date: day("2026-09-07"), Amount: -70, Category: "Home", TransactionType: models.Outflow}})
	out := trendsForWindow(Deps{}, ts, day("2026-09-01"), day("2026-09-07"), day("2026-09-07"))
	if !out.Period.ForecastAvailable || out.Period.ForecastAmount != 300 || out.Velocity.DailyAverage != 10 || out.Period.HistoryAvailable || out.Velocity.HistoricalDaily != nil || out.Velocity.BurnRateChange != nil || len(out.CategoryTrends) != 0 {
		t.Fatalf("unsupported history or wrong forecast: %+v", out)
	}
	ts.Transactions = append(ts.Transactions, models.Transaction{Date: day("2026-08-01"), Amount: 100, TransactionType: models.Income})
	out = trendsForWindow(Deps{}, ts, day("2026-09-01"), day("2026-09-07"), day("2026-09-07"))
	if !out.Period.HistoryAvailable || out.Velocity.HistoricalDaily == nil || *out.Velocity.HistoricalDaily != 0 || len(out.CategoryTrends) != 1 {
		t.Fatalf("observed zero hidden: %+v", out)
	}
	out = trendsForWindow(Deps{}, ts, day("2026-09-01"), day("2026-09-06"), day("2026-09-07"))
	if out.Period.ForecastAvailable || out.Period.ForecastReason == "" {
		t.Fatal("earlier current-month subrange borrowed later records")
	}
}
