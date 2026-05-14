package analysis

import (
	"testing"

	"budget2/internal/services/retirement/history"
)

// TestRunSingleHistoricalSequence_RothBasisCarried is a smoke test that
// confirms runSingleHistoricalSequence does not panic and produces a
// structurally valid result when the Roth 5-year clock is set to the
// projection start year (clock starts unsatisfied, matures mid-run).
func TestRunSingleHistoricalSequence_RothBasisCarried(t *testing.T) {
	settings := buildRothFiveYearAnalysisScenario(t)
	// Clock starts unsatisfied; matures in 2031.
	settings.RothFirstFundedYear = 2026

	in := engineInput(t, settings)
	data := history.DefaultData()

	result := runSingleHistoricalSequence(in, data, 1990)

	// StartYear must be echoed back correctly.
	if result.StartYear != 1990 {
		t.Fatalf("expected StartYear=1990, got %d", result.StartYear)
	}

	// FinalBalance must be non-negative (zero is valid if portfolio depleted).
	if result.FinalBalance < 0 {
		t.Fatalf("expected FinalBalance >= 0, got %.2f", result.FinalBalance)
	}
}
