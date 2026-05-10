package analysis

import (
	"math/rand"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
)

// propertyTaxRegressionSettings builds two scenarios identical except for
// MonthlyPropertyTax so we can assert the analysis-package per-month
// expense accumulators actually include property tax. Without the fix in
// monte_carlo.go and backtest.go, the simulation loops here drop property
// tax silently and both scenarios produce identical outcomes.
func propertyTaxRegressionSettings(monthlyPropertyTax float64) *models.WhatIfSettings {
	return &models.WhatIfSettings{
		StartDate: "2026-01",
		Persons: []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge("2026-01", 65)},
		},
		PortfolioValue:        1_000_000,
		MonthlyLivingExpenses: 3_500,
		MonthlyPropertyTax:    monthlyPropertyTax,
		PropertyTaxInflation:  4.0,
		CurrentAge:            65,
		ProjectionYears:       30,
		InvestmentReturn:      6.0,
		InflationRate:         2.5,
		TaxDeferredPercent:    60.0,
		RothPercent:           10.0,
	}
}

func TestHistoricalBacktest_IncludesPropertyTax(t *testing.T) {
	withTaxIn := engineInput(t, propertyTaxRegressionSettings(2_000))
	noTaxIn := engineInput(t, propertyTaxRegressionSettings(0))

	withTax := HistoricalBacktest(withTaxIn, history.DefaultData())
	noTax := HistoricalBacktest(noTaxIn, history.DefaultData())

	if withTax == nil || noTax == nil {
		t.Fatal("HistoricalBacktest returned nil")
	}
	if withTax.TotalSequences == 0 || withTax.TotalSequences != noTax.TotalSequences {
		t.Fatalf("expected matching non-zero sequence counts, got with=%d without=%d",
			withTax.TotalSequences, noTax.TotalSequences)
	}

	// A $2,000/mo property tax ($24k/yr inflation-adjusted) must push some
	// surviving sequences into depletion across 30-year backtests.
	if withTax.SurvivedCount >= noTax.SurvivedCount {
		t.Errorf("expected SurvivedCount to drop with $2k/mo property tax; got with=%d without=%d "+
			"(property tax silently dropped from the backtest accumulator?)",
			withTax.SurvivedCount, noTax.SurvivedCount)
	}
}

func TestMonteCarlo_IncludesPropertyTax(t *testing.T) {
	const seed int64 = 42
	const runs = 20

	run := func(monthlyPropertyTax float64) *models.MonteCarloAnalysis {
		t.Helper()
		in := engineInput(t, propertyTaxRegressionSettings(monthlyPropertyTax))
		rng := rand.New(rand.NewSource(seed))
		config := DefaultMonteCarloConfig()

		results := make([]models.MonteCarloResult, runs)
		for i := range results {
			results[i] = RunSingleMonteCarloSimulation(in, rng, config)
		}

		var sum float64
		for _, r := range results {
			sum += r.FinalBalance
		}
		return &models.MonteCarloAnalysis{
			Stats: &models.MonteCarloStats{MeanBalance: sum / float64(runs)},
		}
	}

	withTax := run(2_000)
	noTax := run(0)

	// Same seed → noTax must produce a higher mean balance because the
	// noTax scenario isn't paying $24k/yr (inflation-adjusted) in property
	// tax. If they're equal, property tax is missing from the MC loop.
	if withTax.Stats.MeanBalance >= noTax.Stats.MeanBalance {
		t.Errorf("expected mean final balance to drop with $2k/mo property tax; got with=%.2f without=%.2f "+
			"(property tax silently dropped from the MC accumulator?)",
			withTax.Stats.MeanBalance, noTax.Stats.MeanBalance)
	}
}

// TestPropertyTaxAtMonth_Exported confirms the symbol is reachable from
// the analysis package — the original bug was that propertyTaxAtMonth was
// unexported and the analysis package couldn't call it.
func TestPropertyTaxAtMonth_Exported(t *testing.T) {
	s := &models.WhatIfSettings{MonthlyPropertyTax: 500, PropertyTaxInflation: 0}
	if got := engine.PropertyTaxAtMonth(s, 0); got != 500 {
		t.Errorf("PropertyTaxAtMonth(month=0) = %v, want 500", got)
	}
}
