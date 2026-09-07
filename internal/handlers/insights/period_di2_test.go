package insights

import (
	"budget2/internal/models"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDI2HistoricalSelectionHasNoForecast(t *testing.T) {
	ts := models.NewTransactionSet([]models.Transaction{{Date: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC), Amount: -100, TransactionType: models.Outflow}})
	got := calculateInsightsAt(ts, ts, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC))
	if got.Velocity.MonthProjection != 0 {
		t.Fatalf("historical August produced a current-month forecast: %v", got.Velocity.MonthProjection)
	}
}

func TestDI2ChartUsesSharedBounds(t *testing.T) {
	cleanup := setupTestLoader(t, "Date,Description,Amount,Category\n2026-07-01,Home purchase,-100,Home\n2026-08-28,Home purchase,-200,Home\n")
	defer cleanup()
	w := httptest.NewRecorder()
	handleTrendsChartData(w, httptest.NewRequest("GET", "/insights/trends/chart?start=2026-08-01&end=2026-08-31", nil))
	var got struct {
		Period models.PeriodContext `json:"period"`
		Data   []struct {
			Name string
			Y    []float64
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Period.HistoryAvailable || len(got.Data) != 2 || got.Data[1].Name != "2026-07-01 to 2026-07-31" || len(got.Data[0].Y) != 1 || got.Data[0].Y[0] != 200 || got.Data[1].Y[0] != 100 {
		t.Fatalf("chart period/value mismatch: %s", w.Body.String())
	}
}

func TestDI2InsightsPeriodFullAndPartials(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, "Date,Description,Amount,Category\n2026-07-01,Home purchase,-100,Home\n2026-08-28,Home purchase,-200,Home\n")
	defer cleanup()
	old := insightsNow
	insightsNow = func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	defer func() { insightsNow = old }()
	for _, path := range []string{"/insights", "/insights/trends", "/insights/velocity"} {
		for _, hx := range []bool{false, true} {
			r := httptest.NewRequest("GET", path+"?start=2026-08-01&end=2026-08-31", nil)
			if hx {
				r.Header.Set("HX-Request", "true")
			}
			w := httptest.NewRecorder()
			switch path {
			case "/insights":
				handleInsights(w, r)
			case "/insights/trends":
				handleTrendsPartial(w, r)
			case "/insights/velocity":
				handleVelocityPartial(w, r)
			}
			out := w.Body.String()
			for _, want := range []string{"Latest transaction: 2026-08-28", "2026-07-01", "evidence bounds"} {
				if !strings.Contains(out, want) {
					t.Errorf("%s HX=%v missing %q", path, hx, want)
				}
			}
			if path != "/insights/trends" && !strings.Contains(out, "Forecast unavailable") {
				t.Errorf("%s HX=%v missing unavailable forecast", path, hx)
			}
		}
	}
}
