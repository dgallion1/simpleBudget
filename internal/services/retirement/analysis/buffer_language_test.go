package analysis

import (
	"budget2/internal/models"
	"strings"
	"testing"
)

func TestBufferRationaleIsAnIllustration(t *testing.T) {
	for _, survived := range []int{50, 47, 42, 37, 32} {
		runs := make([]models.MonteCarloResult, 100)
		for i := 0; i < 50; i++ {
			runs[i].Survives = true
		}
		for i := 50; i < 100; i++ {
			runs[i].MarketCrashes = 1
			runs[i].EarlyCrashes = 1
			runs[i].FirstCrashYear = 1
			runs[i].Survives = i-50 < survived
		}
		got := calculateSequenceRiskBreakdown(models.DefaultWhatIfSettings(), runs, 60000, 1000000)
		if got == nil || !strings.Contains(got.BufferRationale, "Illustration uses") {
			t.Fatalf("missing illustration for %d survivors: %+v", survived, got)
		}
		for _, promise := range []string{"recommended", "safe", "sufficient", "good protection", "weather early crashes"} {
			if strings.Contains(got.BufferRationale, promise) {
				t.Errorf("unsupported rationale promise %q: %s", promise, got.BufferRationale)
			}
		}
	}
}
