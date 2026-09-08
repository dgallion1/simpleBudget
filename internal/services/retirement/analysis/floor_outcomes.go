package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"math"
)

// floorOutcomeTracker measures actual funded living, including months after
// legacy depletion. Incomplete trailing years contribute spending, not cuts.
type floorOutcomeTracker struct {
	floor                                        float64
	outcome                                      models.MonteCarloFloorOutcome
	annualCents, previousAnnualCents, totalCents float64
}

func (t *floorOutcomeTracker) observe(funded, cpi float64) {
	real := engine.RoundLivingCents(funded / cpi)
	if real < engine.RoundLivingCents(t.floor) {
		t.outcome.FloorFailed = true
	}
	cents := math.Round(real * 100)
	t.totalCents += cents
	t.annualCents += cents
	t.outcome.MonthsObserved++
	if t.outcome.MonthsObserved%12 == 0 {
		if t.previousAnnualCents > 0 && t.annualCents < t.previousAnnualCents {
			cut := (1 - t.annualCents/t.previousAnnualCents) * 100
			t.outcome.AnnualCutCount++
			t.outcome.WorstAnnualCutPct = math.Max(t.outcome.WorstAnnualCutPct, cut)
		}
		t.previousAnnualCents = t.annualCents
		t.annualCents = 0
	}
}
func (t *floorOutcomeTracker) result(balance, cpi float64) *models.MonteCarloFloorOutcome {
	if t == nil {
		return nil
	}
	out := t.outcome
	out.TotalFundedLivingReal = t.totalCents / 100
	out.FinalBalanceReal = balance / cpi
	return &out
}
