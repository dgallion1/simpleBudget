package insights

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestInsightsVerdictBar_Render(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, testCSV())
	defer cleanup()

	t.Run("above usual pace shows red band", func(t *testing.T) {
		out, err := renderer.RenderToString("insights-verdict-bar", map[string]any{
			"PaceVerdict": PaceVerdictView{
				Health: models.HealthRed, HasData: true, IsAbove: true, BurnRateChange: 25,
				DailyAverage: 150, HistoricalDaily: 120, MonthProjection: 4500,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"verdict-red", "above your usual pace", "Daily Avg", `class="num`} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, ptrunc(out, 700))
			}
		}
	})

	t.Run("below usual pace shows green band", func(t *testing.T) {
		out, err := renderer.RenderToString("insights-verdict-bar", map[string]any{
			"PaceVerdict": PaceVerdictView{
				Health: models.HealthGreen, HasData: true, IsBelow: true, BurnRateChange: -20,
				DailyAverage: 80, HistoricalDaily: 100, MonthProjection: 2400,
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{"verdict-green", "below your usual pace"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, ptrunc(out, 700))
			}
		}
	})

	t.Run("no velocity data shows neutral band without a pace headline", func(t *testing.T) {
		out, err := renderer.RenderToString("insights-verdict-bar", map[string]any{
			"PaceVerdict": PaceVerdictView{Health: models.HealthNeutral, HasData: false},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "verdict-neutral") {
			t.Errorf("expected verdict-neutral; got: %s", ptrunc(out, 700))
		}
		if strings.Contains(out, "above your usual pace") || strings.Contains(out, "below your usual pace") {
			t.Errorf("did not expect a pace headline when HasData is false")
		}
	})
}

func ptrunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
