package templates

import (
	"io/fs"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/insights"
	"budget2/web"
)

func TestDI2RenderedForecastAvailability(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatal(err)
	}
	day := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ts := models.NewTransactionSet([]models.Transaction{{Date: day("2026-09-07"), Amount: 70, TransactionType: models.Outflow}})
	for _, end := range []string{"2026-09-07", "2026-09-06"} {
		p := insights.BuildPeriodContext(ts, day("2026-09-01"), day(end), day("2026-09-07"))
		v := insights.SpendingVelocityForPeriod(ts, p)
		out, err := renderer.RenderToString("spending-velocity", map[string]any{"Period": p, "Velocity": v})
		if err != nil {
			t.Fatal(err)
		}
		if end == "2026-09-07" {
			for _, want := range []string{"Estimated September 2026 total through 2026-09-07", "-$300.00", "Net refund estimate"} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in %s", want, out)
				}
			}
		} else if !strings.Contains(out, "Forecast unavailable") || strings.Contains(out, "Estimated September") || strings.Contains(out, "-$300.00") {
			t.Fatalf("historical subrange shows forecast: %s", out)
		}
		if strings.Contains(out, "0.0%") || !strings.Contains(out, "Not enough history to compare") {
			t.Fatal("unavailable history misrepresented")
		}
	}
}
