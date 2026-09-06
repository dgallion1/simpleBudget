package analysis

import (
	"math"
	"math/rand"
	"testing"

	"budget2/internal/models"
)

func TestAggregateLifestyleOutcomesFourRunOracle(t *testing.T) {
	results := []models.MonteCarloResult{
		{Survives: true, GuardrailImpact: &models.MonteCarloGuardrailImpact{MonthsObserved: 360, MinLivingSpendingMultiplier: 1}},
		{Survives: true, GuardrailImpact: &models.MonteCarloGuardrailImpact{
			MonthsObserved: 360, MonthsBelowPlan: 12, LongestBelowPlanMonths: 8,
			MinLivingSpendingMultiplier: .8, MaxMonthlyLivingCutReal: 200,
		}},
		{Survives: false, GuardrailImpact: &models.MonteCarloGuardrailImpact{
			MonthsObserved: 240, MonthsBelowPlan: 24, LongestBelowPlanMonths: 20,
			MinLivingSpendingMultiplier: .6, MaxMonthlyLivingCutReal: 500, BelowPlanAtEnd: true,
		}},
		{Survives: false, GuardrailImpact: &models.MonteCarloGuardrailImpact{MonthsObserved: 180, MinLivingSpendingMultiplier: 1}},
	}

	got := aggregateLifestyleOutcomes(results)
	if got == nil {
		t.Fatal("aggregateLifestyleOutcomes returned nil")
	}
	if got.Runs != 4 || got.FundedWithoutCuts != 1 || got.FundedWithCuts != 1 || got.Shortfall != 2 {
		t.Fatalf("unexpected categories: %+v", got)
	}
	if got.FundedWithoutCuts+got.FundedWithCuts+got.Shortfall != got.Runs {
		t.Fatalf("categories do not sum to runs: %+v", got)
	}
	if got.RunsWithCuts != 2 || got.MedianCutMonths != 18 || got.P90CutMonths != 24 ||
		got.P90LongestCutMonths != 20 || got.MedianMaxLivingCutPct != 30 ||
		got.P90MaxLivingCutPct != 40 || got.P90MaxMonthlyLivingCutReal != 500 ||
		got.CutRunsEndingBelowPlan != 1 {
		t.Fatalf("unexpected cut-run burden: %+v", got)
	}
}

func TestAggregateLifestyleOutcomesRequiresCompleteMetadata(t *testing.T) {
	if got := aggregateLifestyleOutcomes(nil); got != nil {
		t.Fatalf("empty aggregate = %+v, want nil", got)
	}
	results := []models.MonteCarloResult{
		{Survives: true, GuardrailImpact: &models.MonteCarloGuardrailImpact{MonthsObserved: 12, MinLivingSpendingMultiplier: 1}},
		{Survives: true},
	}
	if got := aggregateLifestyleOutcomes(results); got != nil {
		t.Fatalf("aggregate with missing metadata = %+v, want nil", got)
	}
}

func TestAggregateLifestyleOutcomesFundingGapOverridesLegacySurvival(t *testing.T) {
	results := []models.MonteCarloResult{{
		Survives: true,
		GuardrailImpact: &models.MonteCarloGuardrailImpact{
			MonthsObserved: 12, FundingGapMonths: 12, MinLivingSpendingMultiplier: 1,
		},
	}}

	got := aggregateLifestyleOutcomes(results)
	if got == nil || got.Runs != 1 || got.Shortfall != 1 ||
		got.FundedWithoutCuts != 0 || got.FundedWithCuts != 0 {
		t.Fatalf("surviving delay-window gap was misclassified: %+v", got)
	}
	if legacySuccessCount := 1; got.FundedWithoutCuts+got.FundedWithCuts == legacySuccessCount {
		t.Fatal("funded categories must differ from legacy survival when a surviving run has a funding gap")
	}
}

func TestAggregateLifestyleOutcomesOneCutRunQuantiles(t *testing.T) {
	results := []models.MonteCarloResult{{
		Survives: true,
		GuardrailImpact: &models.MonteCarloGuardrailImpact{
			MonthsObserved: 120, MonthsBelowPlan: 7, LongestBelowPlanMonths: 4,
			MinLivingSpendingMultiplier: .75, MaxMonthlyLivingCutReal: 321, BelowPlanAtEnd: true,
		},
	}}

	got := aggregateLifestyleOutcomes(results)
	if got == nil || got.MedianCutMonths != 7 || got.P90CutMonths != 7 ||
		got.P90LongestCutMonths != 4 || got.MedianMaxLivingCutPct != 25 ||
		got.P90MaxLivingCutPct != 25 || got.P90MaxMonthlyLivingCutReal != 321 {
		t.Fatalf("unexpected one-run quantiles: %+v", got)
	}
}

func TestMonteCarloCoreStatsIncludesLifestyle(t *testing.T) {
	results := []models.MonteCarloResult{{
		Survives:        true,
		FinalBalance:    100,
		GuardrailImpact: &models.MonteCarloGuardrailImpact{MonthsObserved: 12, MinLivingSpendingMultiplier: 1},
	}}
	stats, _ := monteCarloCoreStats(results)
	if stats.Lifestyle == nil || stats.Lifestyle.Runs != 1 || stats.Lifestyle.FundedWithoutCuts != 1 {
		t.Fatalf("core stats missing lifestyle aggregate: %+v", stats.Lifestyle)
	}
}

func TestGuardrailImpactTracksBelowPlanMonths(t *testing.T) {
	var tracker guardrailImpactTracker
	for _, mult := range []float64{1, .9, .8, 1, 1.1, .9} {
		tracker.observe(1000, mult, 2)
	}
	got := tracker.result()
	if got.MonthsObserved != 6 || got.MonthsBelowPlan != 3 ||
		got.LongestBelowPlanMonths != 2 || got.CutEpisodes != 2 ||
		!got.BelowPlanAtEnd || math.Abs(got.MinLivingSpendingMultiplier-.8) > 1e-9 ||
		math.Abs(got.MaxMonthlyLivingCutReal-100) > 1e-9 {
		t.Fatalf("unexpected impact: %+v", got)
	}
}

func TestGuardrailImpactTrackerCases(t *testing.T) {
	type observation struct {
		plannedLiving       float64
		multiplier          float64
		cumulativeInflation float64
	}
	tests := []struct {
		name              string
		observations      []observation
		wantObserved      int
		wantBelow         int
		wantLongest       int
		wantEpisodes      int
		wantMinMultiplier float64
		wantMaxRealCut    float64
		wantBelowAtEnd    bool
	}{
		{
			name: "disabled guardrail has no cuts",
			observations: []observation{
				{plannedLiving: 1000, multiplier: 1, cumulativeInflation: 1},
				{plannedLiving: 1010, multiplier: 1, cumulativeInflation: 1.01},
			},
			wantObserved: 2, wantMinMultiplier: 1,
		},
		{
			name: "all months cut",
			observations: []observation{
				{plannedLiving: 1000, multiplier: .9, cumulativeInflation: 1},
				{plannedLiving: 1000, multiplier: .8, cumulativeInflation: 1},
				{plannedLiving: 1000, multiplier: .7, cumulativeInflation: 1},
			},
			wantObserved: 3, wantBelow: 3, wantLongest: 3, wantEpisodes: 1,
			wantMinMultiplier: .7, wantMaxRealCut: 300, wantBelowAtEnd: true,
		},
		{
			name: "recovery then relapse",
			observations: []observation{
				{plannedLiving: 1000, multiplier: .9, cumulativeInflation: 1},
				{plannedLiving: 1000, multiplier: 1, cumulativeInflation: 1},
				{plannedLiving: 1000, multiplier: .8, cumulativeInflation: 1},
				{plannedLiving: 1000, multiplier: 1, cumulativeInflation: 1},
			},
			wantObserved: 4, wantBelow: 2, wantLongest: 1, wantEpisodes: 2,
			wantMinMultiplier: .8, wantMaxRealCut: 200,
		},
		{
			name: "raise does not count as a cut",
			observations: []observation{
				{plannedLiving: 1000, multiplier: 1.1, cumulativeInflation: 1},
			},
			wantObserved: 1, wantMinMultiplier: 1,
		},
		{
			name: "zero living expenses cannot be cut",
			observations: []observation{
				{plannedLiving: 0, multiplier: .5, cumulativeInflation: 1},
			},
			wantObserved: 1, wantMinMultiplier: 1,
		},
		{
			name: "phase reduced planned living is not a guardrail cut",
			observations: []observation{
				{plannedLiving: 600, multiplier: 1, cumulativeInflation: 1},
			},
			wantObserved: 1, wantMinMultiplier: 1,
		},
		{
			name: "observed path ends while cut",
			observations: []observation{
				{plannedLiving: 1000, multiplier: 1, cumulativeInflation: 1},
				{plannedLiving: 1000, multiplier: .9, cumulativeInflation: 1},
				{plannedLiving: 1000, multiplier: .8, cumulativeInflation: 1},
			},
			wantObserved: 3, wantBelow: 2, wantLongest: 2, wantEpisodes: 1,
			wantMinMultiplier: .8, wantMaxRealCut: 200, wantBelowAtEnd: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var tracker guardrailImpactTracker
			for _, observation := range tc.observations {
				tracker.observe(observation.plannedLiving, observation.multiplier, observation.cumulativeInflation)
			}
			got := tracker.result()
			if got.MonthsObserved != tc.wantObserved || got.MonthsBelowPlan != tc.wantBelow ||
				got.LongestBelowPlanMonths != tc.wantLongest || got.CutEpisodes != tc.wantEpisodes ||
				got.BelowPlanAtEnd != tc.wantBelowAtEnd ||
				math.Abs(got.MinLivingSpendingMultiplier-tc.wantMinMultiplier) > 1e-9 ||
				math.Abs(got.MaxMonthlyLivingCutReal-tc.wantMaxRealCut) > 1e-9 {
				t.Fatalf("unexpected impact: %+v", got)
			}
		})
	}
}

func TestGuardrailImpactRejectsInvalidInflation(t *testing.T) {
	for name, cumulativeInflation := range map[string]float64{
		"zero":     0,
		"negative": -1,
		"nan":      math.NaN(),
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("observe must reject non-positive cumulative inflation")
				}
			}()
			var tracker guardrailImpactTracker
			tracker.observe(1000, .9, cumulativeInflation)
		})
	}
}

func TestGuardrailImpactCountsActualFundingGaps(t *testing.T) {
	var tracker guardrailImpactTracker
	for _, shortfall := range []float64{0, 1e-7, 1.0001e-7, 500} {
		tracker.observeFundingGap(shortfall)
	}
	if got := tracker.result().FundingGapMonths; got != 2 {
		t.Fatalf("FundingGapMonths = %d, want 2", got)
	}
}

func TestL3SeededNoShockInstrumentationParity(t *testing.T) {
	s := monteCarloShockSettings(2)
	s.MonthlyLivingExpenses = 1000
	s.Guardrails = nil
	s.TaxDeferredPercent, s.RothPercent = 100, 0
	s.TaxDeferredStockPercent, s.TaxDeferredCashPercent = 0, 100
	s.TaxDeferredDelayYears = 1
	in := engineInput(t, s)
	c := deterministicShockConfig()
	c.SpendingShockProb, c.HealthShockProb = 0, 0

	got := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(123)), c)
	if math.Abs(got.FinalBalance-1049524.798651163) > 1e-7 || got.TotalIRMAA != 0 ||
		got.MarketCrashes != 0 || got.SpendingShocks != 0 || got.HealthShocks != 0 ||
		!got.Survives || got.ProjectionYears != 2 {
		t.Fatalf("seeded no-shock scalar outcome changed: %+v", got)
	}
	if got.GuardrailImpact == nil {
		t.Fatal("missing observed guardrail impact")
	}
	if got.GuardrailImpact.MonthsObserved != 24 || got.GuardrailImpact.FundingGapMonths != 12 {
		t.Fatalf("unexpected observation metadata: %+v", *got.GuardrailImpact)
	}
}
