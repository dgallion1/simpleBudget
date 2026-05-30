package analysis

import (
	"testing"

	"budget2/internal/models"
)

// A Medicare-eligible retiree with high MAGI must incur IRMAA in the first
// projection years, not $0. The IRMAA two-year MAGI lookback has no history
// for years 0-1, so the main projection loop must seed an assumed
// pre-projection MAGI (its own year-0 MAGI). Without the seed, years 0-1
// silently show $0 IRMAA even for a high-income 65+ household.
func TestProjection_IRMAAAppliesInEarlyYearsForHighMAGI(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 70 // Medicare-eligible from year 0
	s.SpouseAge = 0
	s.ProjectionYears = 5
	s.SocialSecurity = nil
	s.IncomeSources = []models.IncomeSource{
		// ~$300k/yr ordinary income → MAGI far above any IRMAA tier-1 threshold.
		{ID: "pension", Name: "Pension", Amount: 25000, StartMonth: 0},
	}

	proj, _ := runProj(t, s)

	if len(proj.YearlySummaries) < 2 {
		t.Fatalf("expected at least 2 yearly summaries, got %d", len(proj.YearlySummaries))
	}
	if proj.YearlySummaries[0].IRMAA <= 0 {
		t.Errorf("year 0 IRMAA = %.2f, want > 0 (high-MAGI Medicare-eligible retiree should incur IRMAA in the first year)",
			proj.YearlySummaries[0].IRMAA)
	}
	if proj.YearlySummaries[1].IRMAA <= 0 {
		t.Errorf("year 1 IRMAA = %.2f, want > 0", proj.YearlySummaries[1].IRMAA)
	}
}
