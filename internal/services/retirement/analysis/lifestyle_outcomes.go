package analysis

import (
	"math"

	"budget2/internal/models"
)

const (
	guardrailCutTolerance = 1e-9
	fundingGapTolerance   = 1e-7
)

// guardrailImpactTracker measures only the drop/rise spending guardrail.
// Adaptive Monte Carlo sub-runs reduce discretionary expense sources through
// DiscretionaryMultiplier and therefore do not register as guardrail cuts.
// Task 4 aggregates this metadata from the main results slice, never from
// adaptiveResults.
type guardrailImpactTracker struct {
	impact                 models.MonteCarloGuardrailImpact
	currentBelowPlanMonths int
}

func (t *guardrailImpactTracker) observe(plannedLiving, multiplier, cumulativeInflation float64) {
	if !(cumulativeInflation > 0) {
		panic("guardrail impact requires positive cumulative inflation")
	}
	if t.impact.MonthsObserved == 0 {
		t.impact.MinLivingSpendingMultiplier = 1
	}
	t.impact.MonthsObserved++

	belowPlan := plannedLiving > 0 && multiplier < 1-guardrailCutTolerance
	t.impact.BelowPlanAtEnd = belowPlan
	if !belowPlan {
		t.currentBelowPlanMonths = 0
		return
	}

	if t.currentBelowPlanMonths == 0 {
		t.impact.CutEpisodes++
	}
	t.currentBelowPlanMonths++
	t.impact.MonthsBelowPlan++
	if t.currentBelowPlanMonths > t.impact.LongestBelowPlanMonths {
		t.impact.LongestBelowPlanMonths = t.currentBelowPlanMonths
	}
	if multiplier < t.impact.MinLivingSpendingMultiplier {
		t.impact.MinLivingSpendingMultiplier = multiplier
	}
	realCut := plannedLiving * math.Max(0, 1-multiplier) / cumulativeInflation
	if realCut > t.impact.MaxMonthlyLivingCutReal {
		t.impact.MaxMonthlyLivingCutReal = realCut
	}
}

func (t *guardrailImpactTracker) observeFundingGap(shortfall float64) {
	if shortfall > fundingGapTolerance {
		t.impact.FundingGapMonths++
	}
}

func (t *guardrailImpactTracker) result() models.MonteCarloGuardrailImpact {
	result := t.impact
	if result.MonthsObserved == 0 {
		result.MinLivingSpendingMultiplier = 1
	}
	return result
}

// aggregateLifestyleOutcomes classifies fully observed Monte Carlo runs and
// summarizes spending-cut burden. Legacy results without impact metadata are
// not treated as evidence that the full lifestyle was funded.
func aggregateLifestyleOutcomes(results []models.MonteCarloResult) *models.LifestyleOutcomeStats {
	if len(results) == 0 {
		return nil
	}
	for _, result := range results {
		if result.GuardrailImpact == nil {
			return nil
		}
	}

	stats := &models.LifestyleOutcomeStats{Runs: len(results)}
	cutMonths := make([]float64, 0, len(results))
	longestCutMonths := make([]float64, 0, len(results))
	maxCutPercentages := make([]float64, 0, len(results))
	maxMonthlyRealCuts := make([]float64, 0, len(results))

	for _, result := range results {
		impact := result.GuardrailImpact
		hasCuts := impact.MonthsBelowPlan > 0

		switch {
		case impact.FundingGapMonths > 0 || !result.Survives:
			stats.Shortfall++
		case hasCuts:
			stats.FundedWithCuts++
		default:
			stats.FundedWithoutCuts++
		}

		if !hasCuts {
			continue
		}
		stats.RunsWithCuts++
		if impact.BelowPlanAtEnd {
			stats.CutRunsEndingBelowPlan++
		}
		cutMonths = append(cutMonths, float64(impact.MonthsBelowPlan))
		longestCutMonths = append(longestCutMonths, float64(impact.LongestBelowPlanMonths))
		maxCutPercentages = append(maxCutPercentages, math.Max(0, 1-impact.MinLivingSpendingMultiplier)*100)
		maxMonthlyRealCuts = append(maxMonthlyRealCuts, impact.MaxMonthlyLivingCutReal)
	}

	if stats.RunsWithCuts == 0 {
		return stats
	}
	stats.MedianCutMonths = lifestyleMedian(cutMonths)
	stats.P90CutMonths = lifestyleP90(cutMonths)
	stats.P90LongestCutMonths = lifestyleP90(longestCutMonths)
	stats.MedianMaxLivingCutPct = lifestyleMedian(maxCutPercentages)
	stats.P90MaxLivingCutPct = lifestyleP90(maxCutPercentages)
	stats.P90MaxMonthlyLivingCutReal = lifestyleP90(maxMonthlyRealCuts)
	return stats
}

func lifestyleMedian(values []float64) float64 {
	sortFloat64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

func lifestyleP90(values []float64) float64 {
	sortFloat64s(values)
	return values[int(math.Ceil(.9*float64(len(values))))-1]
}
