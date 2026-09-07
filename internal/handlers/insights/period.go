package insights

import (
	"net/http"
	"time"

	"budget2/internal/models"
	insightssvc "budget2/internal/services/insights"
)

var insightsNow = time.Now

// All Insights entry points resolve the same selected period and defaults.
func insightsPeriod(ts *models.TransactionSet, r *http.Request, now time.Time) models.PeriodContext {
	active := ts.Active()
	end := active.MaxDate()
	if end.IsZero() {
		end = now
	}
	start := end.AddDate(0, -12, 0)
	if first := active.MinDate(); !first.IsZero() && start.Before(first) {
		start = first
	}
	if value, err := time.Parse("2006-01-02", r.URL.Query().Get("start")); err == nil {
		start = value
	}
	if value, err := time.Parse("2006-01-02", r.URL.Query().Get("end")); err == nil {
		end = value
	}
	return insightssvc.BuildPeriodContext(active, start, end, now)
}

func loadAndAnalyzeTrendsForPeriod(ts *models.TransactionSet, p models.PeriodContext) []models.CategoryTrend {
	if loader != nil {
		defs, err := loader.LoadMajorExpenses()
		if err == nil && len(defs) > 0 {
			pins, _ := loader.LoadTransactionPins()
			return insightssvc.MajorExpenseTrendsForPeriod(ts, defs, pins, p)
		}
	}
	return insightssvc.CategoryTrendsForPeriod(ts, p)
}
