package dashboard

import (
	"budget2/internal/models"
	"budget2/internal/services/metrics"
	"budget2/internal/templates"
	"budget2/web"
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDI2SecondYearCivilHistory(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC)
	req := httptest.NewRequest("GET", "/dashboard/kpis?start=2026-02-01&end=2026-02-28&comparison=year", nil)
	for _, zone := range []*time.Location{time.UTC, time.FixedZone("west", -5*3600), time.FixedZone("east", 9*3600)} {
		for _, hour := range []int{0, 12} {
			t.Run(zone.String()+time.Date(2000, 1, 1, hour, 0, 0, 0, time.UTC).Format("15"), func(t *testing.T) {
				ts := models.NewTransactionSet([]models.Transaction{
					{Date: time.Date(2025, 2, 1, hour, 0, 0, 0, zone), Amount: -100, TransactionType: models.Outflow},
					{Date: time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC), Amount: -200, TransactionType: models.Outflow},
				})
				p := dashboardPeriod(ts, req, now)
				comparison := dashboardPeriodComparison(ts, p, "year", nil, nil)
				legacy := metrics.Comparison(ts, p.SelectedStart, p.SelectedEnd, "year", nil, nil)
				if zone == time.UTC && (!legacy.HasData || legacy.Previous.TotalExpenses != 100) {
					t.Fatalf("baseline premise false: %+v", legacy)
				}
				t.Logf("legacy HasData=%v", legacy.HasData)
				t.Logf("first=%s prior=%s..%s history=%v comparison.HasData=%v", ts.MinDate(), p.PreviousStart.Format("2006-01-02"), p.PreviousEnd.Format("2006-01-02"), p.HistoryAvailable, comparison.HasData)
				if !p.HistoryAvailable || !comparison.HasData {
					t.Error("same civil-day history was suppressed; expected prior expense 100")
				}
				if comparison.Previous != nil && comparison.Previous.TotalExpenses != 100 {
					t.Errorf("prior expense=%v want 100", comparison.Previous.TotalExpenses)
				}
			})
		}
	}
}

func TestDI2SecondYearHistoryRendered(t *testing.T) {
	ts := models.NewTransactionSet([]models.Transaction{
		{Date: time.Date(2025, 2, 1, 12, 0, 0, 0, time.UTC), Amount: -100, TransactionType: models.Outflow},
		{Date: time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC), Amount: -200, TransactionType: models.Outflow},
	})
	req := httptest.NewRequest("GET", "/dashboard?start=2026-02-01&end=2026-02-28&comparison=year", nil)
	p := dashboardPeriod(ts, req, time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC))
	fsys, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := templates.NewFromFS(fsys, false)
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderer.RenderToString("shared/period-context", map[string]any{"Period": p})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Not enough history to compare") {
		t.Error("rendered false no-history statement for covered 2025-02-01..2025-02-28")
	}
	// Counterfactual: retain dates, amounts, timestamps and year bounds; repair
	// only the availability metadata. Real comparison then recovers the $100.
	p.HistoryAvailable = true
	p.HistoryReason = ""
	c := dashboardPeriodComparison(ts, p, "year", nil, nil)
	if !c.HasData || c.Previous.TotalExpenses != 100 {
		t.Fatalf("availability remedy fails: %+v", c)
	}
	out, err = renderer.RenderToString("shared/period-context", map[string]any{"Period": p})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Not enough history to compare") {
		t.Fatal("counterfactual still reports absent history")
	}
	t.Log("metadata-only counterfactual restores prior $100 and removes false unavailable statement")
}
