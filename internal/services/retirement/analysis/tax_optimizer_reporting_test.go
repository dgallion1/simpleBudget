package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// PeakMarginalBracket must be the MAX across projection years, not the first
// year, the last year, the mean, or the sum. The peak is the number a user acts
// on ("does this plan ever push me into 32%?"), so a candidate whose worst year
// is buried mid-projection must still report it.
func TestProjectionToCandidate_PeakMarginalBracketTakesMax(t *testing.T) {
	proj := &models.ProjectionResult{
		YearlySummaries: []models.ProjectionYearSummary{
			{MarginalRate: 22, CumulativeInflation: 1, EndingBalanceReal: 1_000_000},
			{MarginalRate: 32, CumulativeInflation: 1, EndingBalanceReal: 1_000_000}, // peak, mid-projection
			{MarginalRate: 24, CumulativeInflation: 1, EndingBalanceReal: 1_000_000},
		},
	}
	cand := projectionToCandidate(proj, 67, 62, models.RothOptimizerStrategy{})
	if cand.PeakMarginalBracket != 32 {
		t.Errorf("PeakMarginalBracket = %v, want 32 (max); "+
			"22 would mean first-year, 24 last-year, 26 mean, 78 sum",
			cand.PeakMarginalBracket)
	}
}

// A single non-finite rate must not poison the column. Propagating NaN would
// make the max NaN for that candidate and render the whole comparison table
// unreadable, so a bad year is skipped and the finite peak still reports.
func TestProjectionToCandidate_PeakMarginalBracketSkipsNonFinite(t *testing.T) {
	for _, tc := range []struct {
		name string
		bad  float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := &models.ProjectionResult{
				YearlySummaries: []models.ProjectionYearSummary{
					{MarginalRate: 24, CumulativeInflation: 1, EndingBalanceReal: 1_000_000},
					{MarginalRate: tc.bad, CumulativeInflation: 1, EndingBalanceReal: 1_000_000},
				},
			}
			cand := projectionToCandidate(proj, 67, 62, models.RothOptimizerStrategy{})
			if cand.PeakMarginalBracket != 24 {
				t.Errorf("PeakMarginalBracket = %v, want 24 (bad year skipped)", cand.PeakMarginalBracket)
			}
		})
	}
}

// TotalRothConverted must sum the engine's REALIZED per-month conversions.
// Reading the planned PerYearConversions instead would overstate any strategy
// the engine could not execute in full.
func TestProjectionToCandidate_TotalRothConvertedSumsRealizedMonths(t *testing.T) {
	proj := &models.ProjectionResult{
		YearlySummaries: []models.ProjectionYearSummary{
			{CumulativeInflation: 1, EndingBalanceReal: 1_000_000},
		},
		Months: []models.ProjectionMonth{
			{RothConversions: 1_000},
			{RothConversions: 2_500},
			{RothConversions: 0},
			{RothConversions: 500},
		},
	}
	cand := projectionToCandidate(proj, 67, 62, models.RothOptimizerStrategy{})
	if cand.TotalRothConverted != 4_000 {
		t.Errorf("TotalRothConverted = %v, want 4000 (sum of realized months)", cand.TotalRothConverted)
	}
}

// A candidate that never converts must report zero, not a stale or planned
// figure — the UI renders "—" for zero.
func TestProjectionToCandidate_TotalRothConvertedZeroWhenNoConversions(t *testing.T) {
	proj := &models.ProjectionResult{
		YearlySummaries: []models.ProjectionYearSummary{
			{MarginalRate: 22, CumulativeInflation: 1, EndingBalanceReal: 1_000_000},
		},
		Months: []models.ProjectionMonth{{RothConversions: 0}, {RothConversions: 0}},
	}
	cand := projectionToCandidate(proj, 67, 62, models.RothOptimizerStrategy{})
	if cand.TotalRothConverted != 0 {
		t.Errorf("TotalRothConverted = %v, want 0", cand.TotalRothConverted)
	}
}

// End-to-end plumbing. Without this, a break anywhere upstream — the engine no
// longer recording MarginalRate or RothConversions — would silently return both
// columns to the zero they used to be while every unit test above still passed,
// because those tests hand-build their projections.
func TestProjectionToCandidate_EndToEndPopulatesBothFields(t *testing.T) {
	s := adversarialOverlapSettings(t)
	overrides := map[int]float64{2: 40_000, 3: 40_000, 4: 40_000}
	proj := runWithOverrides(t, s, overrides)

	cand := projectionToCandidate(proj, 70, 0, models.RothOptimizerStrategy{})

	if cand.TotalRothConverted <= 0 {
		t.Fatalf("TotalRothConverted = %v; engine ran %d months with conversions planned — "+
			"zero means the field is not wired to the projection",
			cand.TotalRothConverted, len(proj.Months))
	}
	const planned = 120_000
	if math.Abs(cand.TotalRothConverted-planned) > 1 {
		t.Errorf("TotalRothConverted = %.2f, want ~%d (the overrides the engine executed)",
			cand.TotalRothConverted, planned)
	}

	if cand.PeakMarginalBracket <= 0 {
		t.Fatalf("PeakMarginalBracket = %v; zero means the field is not wired to "+
			"ProjectionYearSummary.MarginalRate", cand.PeakMarginalBracket)
	}
	var wantPeak float64
	for _, ys := range proj.YearlySummaries {
		if ys.MarginalRate > wantPeak {
			wantPeak = ys.MarginalRate
		}
	}
	if cand.PeakMarginalBracket != wantPeak {
		t.Errorf("PeakMarginalBracket = %v, want %v (max over the projection's years)",
			cand.PeakMarginalBracket, wantPeak)
	}
	// A real conversion-era projection must land on a genuine statutory-ish rate,
	// not the constant 10% floor the pre-#34 bracket lookup collapsed to.
	if cand.PeakMarginalBracket <= 10 {
		t.Errorf("PeakMarginalBracket = %v; a $40k/yr conversion on a single filer "+
			"should exceed the 10%% bracket floor", cand.PeakMarginalBracket)
	}
}
