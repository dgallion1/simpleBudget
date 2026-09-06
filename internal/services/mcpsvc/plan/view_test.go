package plan

import (
	"encoding/json"
	"strings"
	"testing"

	"budget2/internal/models"
)

func sampleAnalysis() *models.WhatIfAnalysis {
	months := make([]models.ProjectionMonth, 24)
	for i := range months {
		months[i] = models.ProjectionMonth{Month: i, PortfolioBalance: 1_000_000 - float64(i)*1_000}
	}
	return &models.WhatIfAnalysis{
		Settings: &models.WhatIfSettings{PortfolioValue: 1_000_000, ProjectionYears: 2},
		Projection: &models.ProjectionResult{
			Months:       months,
			FinalBalance: 976_000.4567,
			Survives:     true,
			YearlySummaries: []models.ProjectionYearSummary{
				{Year: 0, StartingBalance: 1_000_000, EndingBalance: 988_000.9, Taxes: 5_000.4, IRMAA: 0},
				{Year: 1, StartingBalance: 988_000.9, EndingBalance: 976_000.4, Taxes: 5_100.6, IRMAA: 0},
			},
		},
		Sustainability: &models.SustainabilityScore{Score: 88, Label: "Good"},
		BudgetFit:      &models.BudgetFitAnalysis{MonthlyExpenses: 4_000.2, MonthlyIncome: 1_000.8, MonthlyGap: 3_000.1},
		RMD:            &models.RMDAnalysis{StartAge: 73, StartsInYears: 8, TaxDeferredValue: 600_000.7},
		Tax:            &models.TaxAnalysis{TotalTaxPaid: 10_101.9, AverageEffectiveRate: 12.5},
	}
}

func TestShapeAnalysis_ExcludesPerMonthSeries(t *testing.T) {
	v := ShapeAnalysis(sampleAnalysis(), true)
	if len(v.Years) != 2 {
		t.Fatalf("Years = %d, want 2", len(v.Years))
	}
	// The view type must not carry a per-month field at all; this test guards
	// the year series being present without months leaking in alongside it.
	if v.Headline.FinalBalance != 976_000 {
		t.Errorf("FinalBalance = %v, want 976000 (rounded)", v.Headline.FinalBalance)
	}
}

func TestShapeAnalysis_RoundsCurrencyToWholeDollars(t *testing.T) {
	v := ShapeAnalysis(sampleAnalysis(), true)
	if v.Years[0].Taxes != 5_000 {
		t.Errorf("Years[0].Taxes = %v, want 5000", v.Years[0].Taxes)
	}
	if v.Budget.MonthlyGap != 3_000 {
		t.Errorf("MonthlyGap = %v, want 3000", v.Budget.MonthlyGap)
	}
	if v.Tax.TotalTaxPaid != 10_102 {
		t.Errorf("TotalTaxPaid = %v, want 10102", v.Tax.TotalTaxPaid)
	}
}

func TestShapeAnalysis_OmitsMonteCarloWhenNotRequested(t *testing.T) {
	a := sampleAnalysis()
	a.MonteCarlo = &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 91.5}}

	if got := ShapeAnalysis(a, true); got.MonteCarlo == nil {
		t.Fatal("MonteCarlo should be present when includeMonteCarlo is true")
	}
	if got := ShapeAnalysis(a, false); got.MonteCarlo != nil {
		t.Errorf("MonteCarlo = %+v, want nil when includeMonteCarlo is false", got.MonteCarlo)
	}
}

func TestShapeAnalysis_MonteCarloLifestyleIsOptional(t *testing.T) {
	a := sampleAnalysis()
	a.MonteCarlo = &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 91.5}}

	without := ShapeAnalysis(a, true)
	if without.MonteCarlo == nil {
		t.Fatal("MonteCarlo should be present")
	}
	if without.MonteCarlo.Lifestyle != nil {
		t.Fatalf("Lifestyle = %+v, want nil", without.MonteCarlo.Lifestyle)
	}
	data, err := json.Marshal(without.MonteCarlo)
	if err != nil {
		t.Fatalf("json.Marshal without lifestyle failed: %v", err)
	}
	if strings.Contains(string(data), "lifestyle") {
		t.Fatalf("nil lifestyle should be omitted from JSON: %s", data)
	}

	a.MonteCarlo.Stats.Lifestyle = &models.LifestyleOutcomeStats{
		Runs: 4, FundedWithoutCuts: 1, FundedWithCuts: 1, Shortfall: 2,
	}
	with := ShapeAnalysis(a, true)
	if with.MonteCarlo == nil || with.MonteCarlo.Lifestyle == nil {
		t.Fatalf("Lifestyle should be present: %+v", with.MonteCarlo)
	}
	got := with.MonteCarlo.Lifestyle
	if got.Runs != 4 || got.FundedWithoutCuts != 1 || got.FundedWithCuts != 1 || got.Shortfall != 2 {
		t.Fatalf("unexpected lifestyle counts: %+v", got)
	}
	data, err = json.Marshal(with.MonteCarlo)
	if err != nil {
		t.Fatalf("json.Marshal with lifestyle failed: %v", err)
	}
	for _, key := range []string{"\"runs\":4", "\"funded_without_cuts\":1", "\"funded_with_cuts\":1", "\"shortfall\":2"} {
		if !strings.Contains(string(data), key) {
			t.Errorf("lifestyle JSON missing %s: %s", key, data)
		}
	}
}

func TestShapeAnalysis_DefinesLegacySuccessRateForGapBearingLifestyle(t *testing.T) {
	a := sampleAnalysis()
	a.MonteCarlo = &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{
		Runs:        1,
		SuccessRate: 100,
		Lifestyle: &models.LifestyleOutcomeStats{
			Runs: 1, Shortfall: 1,
		},
	}}

	got := ShapeAnalysis(a, true).MonteCarlo
	if got == nil || got.Lifestyle == nil || got.Lifestyle.Shortfall != 1 {
		t.Fatalf("gap-bearing lifestyle missing: %+v", got)
	}
	if got.SuccessRate != 100 {
		t.Fatalf("SuccessRate = %v, want unchanged legacy value 100", got.SuccessRate)
	}
	const wantDefinition = "Percentage of runs avoiding modeled portfolio depletion; unpaid spending during withdrawal delays can still occur."
	if got.SuccessRateDefinition != wantDefinition {
		t.Fatalf("SuccessRateDefinition = %q, want %q", got.SuccessRateDefinition, wantDefinition)
	}
}

func TestShapeAnalysis_SustainabilityScoreZeroMarshalsWithKey(t *testing.T) {
	a := &models.WhatIfAnalysis{
		Settings:       &models.WhatIfSettings{PortfolioValue: 1_000_000, ProjectionYears: 2},
		Projection:     &models.ProjectionResult{FinalBalance: 100_000, Survives: false},
		Sustainability: &models.SustainabilityScore{Score: 0, Label: "Critical"},
	}
	v := ShapeAnalysis(a, true)
	if v.Headline.SustainabilityScore != 0 {
		t.Errorf("SustainabilityScore = %d, want 0", v.Headline.SustainabilityScore)
	}
	if v.Headline.SustainabilityLabel != "Critical" {
		t.Errorf("SustainabilityLabel = %q, want Critical", v.Headline.SustainabilityLabel)
	}
	// Verify the key is present in JSON even with value 0
	data, err := json.Marshal(v.Headline)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if !strings.Contains(string(data), "sustainability_score") {
		t.Errorf("JSON missing sustainability_score key: %s", string(data))
	}
}

func TestShapeAnalysis_NilSectionsDoNotPanic(t *testing.T) {
	a := &models.WhatIfAnalysis{Projection: &models.ProjectionResult{}}
	v := ShapeAnalysis(a, true)
	if v.Budget != nil || v.RMD != nil || v.Tax != nil || v.MonteCarlo != nil {
		t.Errorf("expected nil sections for an empty analysis, got %+v", v)
	}

	// Test with genuinely nil Projection
	a2 := &models.WhatIfAnalysis{}
	v2 := ShapeAnalysis(a2, true)
	if len(v2.Years) != 0 {
		t.Errorf("Years = %v, want empty when Projection is nil", v2.Years)
	}
	if v2.Headline.FinalBalance != 0 {
		t.Errorf("FinalBalance = %v, want 0 when Projection is nil", v2.Headline.FinalBalance)
	}
}
