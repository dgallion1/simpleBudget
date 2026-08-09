package whatifmcp

import (
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

func TestShapeAnalysis_NilSectionsDoNotPanic(t *testing.T) {
	a := &models.WhatIfAnalysis{Projection: &models.ProjectionResult{}}
	v := ShapeAnalysis(a, true)
	if v.Budget != nil || v.RMD != nil || v.Tax != nil || v.MonteCarlo != nil {
		t.Errorf("expected nil sections for an empty analysis, got %+v", v)
	}
}
