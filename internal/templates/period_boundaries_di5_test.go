package templates

import (
	"budget2/internal/models"
	"budget2/internal/services/insights"
	"budget2/web"
	"io/fs"
	"strings"
	"testing"
	"time"
)

func TestDI5RenderedBoundaries(t *testing.T) {
	tf, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewFromFS(tf, false)
	if err != nil {
		t.Fatal(err)
	}
	day := func(s string) time.Time {
		d, e := time.Parse("2006-01-02", s)
		if e != nil {
			t.Fatal(e)
		}
		return d
	}
	ts := models.NewTransactionSet([]models.Transaction{
		{Date: day("2026-02-01"), Amount: -30.004, Category: "Home", TransactionType: models.Outflow},
		{Date: day("2026-03-01"), Amount: -100.004, Category: "Home", TransactionType: models.Outflow},
		{Date: day("2026-03-31"), Amount: 150, Category: "Home", TransactionType: models.Outflow},
	})
	p := insights.BuildPeriodContext(ts, day("2026-03-01"), day("2026-03-31"), day("2026-03-31"))
	out, err := r.RenderToString("insights-trends-partial", map[string]any{"Period": p, "CategoryTrends": insights.CategoryTrendsForPeriod(ts, p), "GroupIDs": map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2026-02-01 to 2026-02-28 (28 calendar days)", "clamped to the prior month", "evidence bounds", ">-$50.00</td>", ">$30.00</td>", "-$80.00"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing rendered %q", want)
		}
	}
	if strings.Contains(out, "-$0.00") {
		t.Error("negative zero")
	}
	t.Log("rendered clamp 31 vs 28 days; current=-$50.00 prior=$30.00 delta=-$80.00")
	for _, amount := range []float64{0, 0.0001} {
		zero := models.NewTransactionSet([]models.Transaction{{Date: day("2026-09-07"), Amount: amount, TransactionType: models.Outflow}})
		zp := insights.BuildPeriodContext(zero, day("2026-09-01"), day("2026-09-07"), day("2026-09-07"))
		z, err := r.RenderToString("shared/period-forecast", map[string]any{"Period": zp})
		if err != nil {
			t.Fatal(err)
		}
		if !zp.ForecastAvailable || !strings.Contains(z, ">$0.00</p>") || strings.Contains(z, "-$0.00") || strings.Contains(z, "Forecast unavailable") {
			t.Errorf("available zero: %s", z)
		}
	}
}
