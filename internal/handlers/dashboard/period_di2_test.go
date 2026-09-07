package dashboard

import (
	"budget2/internal/models"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDI2DashboardPriorCalendarMonth(t *testing.T) {
	router, cleanup := setupTestEnv(t, [][]string{
		{"2025-12-01", "PAYCHECK", "1000", "Income"},
		{"2026-01-01", "HOME PURCHASE", "-100", "Home"},
		{"2026-01-15", "PAYCHECK", "1000", "Income"},
		{"2026-02-10", "HOME PURCHASE", "-200", "Home"},
	})
	defer cleanup()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/dashboard/kpis?start=2026-02-01&end=2026-02-28&comparison=previous", nil))
	var got struct{ PeriodComparison *models.PeriodComparison }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.PeriodComparison == nil || got.PeriodComparison.Previous == nil {
		t.Fatalf("missing comparison: %s", w.Body.String())
	}
	if got.PeriodComparison.Previous.TotalExpenses != 100 {
		t.Fatalf("prior-calendar-month spending=%v want 100", got.PeriodComparison.Previous.TotalExpenses)
	}
}

func TestDI2DashboardPeriodFullAndHTMX(t *testing.T) {
	router, cleanup := setupTestEnvWithRenderer(t, [][]string{{"2026-08-28", "Home purchase", "-100", "Home"}})
	defer cleanup()
	old := dashboardNow
	dashboardNow = func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	defer func() { dashboardNow = old }()
	for _, path := range []string{"/dashboard", "/dashboard/kpis"} {
		r := httptest.NewRequest("GET", path+"?start=2026-08-01&end=2026-08-31", nil)
		if path == "/dashboard/kpis" {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		for _, want := range []string{"Latest transaction: 2026-08-28", "Not enough history to compare", "evidence bounds"} {
			if !strings.Contains(w.Body.String(), want) {
				t.Errorf("%s missing %q", path, want)
			}
		}
	}
}
