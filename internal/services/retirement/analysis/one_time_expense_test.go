package analysis

import (
	"math/rand"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/history"
)

// oneTimeExpenseRegressionSettings builds a scenario identical across calls
// except for OneTimeExpenses, mirroring propertyTaxRegressionSettings: a
// $50k one-time expense in year 3, inflation-free so the expected size is
// exact.
func oneTimeExpenseRegressionSettings(oneTime []models.OneTimeExpense) *models.WhatIfSettings {
	return &models.WhatIfSettings{
		StartDate: "2026-01",
		Persons: []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge("2026-01", 65)},
		},
		PortfolioValue:        1_000_000,
		MonthlyLivingExpenses: 3_500,
		CurrentAge:            65,
		ProjectionYears:       10,
		InvestmentReturn:      6.0,
		InflationRate:         0,
		TaxDeferredPercent:    60.0,
		RothPercent:           10.0,
		OneTimeExpenses:       oneTime,
	}
}

// TestOneTimeExpense_HistoricalBacktestPicksItUp verifies the historical
// backtest loop — which shares StepMonth with the canonical loop and Monte
// Carlo — folds a one-time expense into its year rather than dropping it,
// mirroring TestHistoricalBacktest_IncludesPropertyTax.
func TestOneTimeExpense_HistoricalBacktestPicksItUp(t *testing.T) {
	withIn := engineInput(t, oneTimeExpenseRegressionSettings([]models.OneTimeExpense{
		{Description: "roof", Year: 3, Amount: 50_000},
	}))
	withoutIn := engineInput(t, oneTimeExpenseRegressionSettings(nil))

	with := HistoricalBacktest(withIn, history.DefaultData())
	without := HistoricalBacktest(withoutIn, history.DefaultData())

	if with == nil || without == nil {
		t.Fatal("HistoricalBacktest returned nil")
	}
	if with.TotalSequences == 0 || with.TotalSequences != without.TotalSequences {
		t.Fatalf("expected matching non-zero sequence counts, got with=%d without=%d",
			with.TotalSequences, without.TotalSequences)
	}

	// In this scenario every sequence survives in both arms (SurvivedCount
	// is identical), so a SurvivedCount comparison is unreachable for any
	// implementation, including one with the one-time expense deleted from
	// the engine entirely — it is not a real assertion. The mean final
	// balance across a.Results is where the signal actually lives: a $50k
	// one-time expense in year 3, compounded by the remaining projection
	// years, must depress the mean by well over half its nominal amount.
	withMean := meanFinalBalance(t, with.Results)
	withoutMean := meanFinalBalance(t, without.Results)
	const nominalExpense = 50_000.0
	if diff := withoutMean - withMean; diff < nominalExpense/2 {
		t.Errorf("expected mean final balance to drop by at least %.2f (half the $50k one-time expense) "+
			"with the expense present; got with=%.2f without=%.2f (diff=%.2f) "+
			"— one-time expense may have been dropped from the backtest loop",
			nominalExpense/2, withMean, withoutMean, diff)
	}
}

// meanFinalBalance averages FinalBalance across backtest sequence results.
func meanFinalBalance(t *testing.T, results []models.HistoricalBacktestResult) float64 {
	t.Helper()
	if len(results) == 0 {
		t.Fatal("meanFinalBalance: no results")
	}
	var sum float64
	for _, r := range results {
		sum += r.FinalBalance
	}
	return sum / float64(len(results))
}

// TestOneTimeExpense_MonteCarloPicksItUp verifies the Monte Carlo loop
// includes the one-time expense, mirroring TestMonteCarlo_IncludesPropertyTax:
// same-seed runs must diverge by roughly the expense amount once it is
// present.
func TestOneTimeExpense_MonteCarloPicksItUp(t *testing.T) {
	const seed int64 = 42
	const runs = 20

	run := func(oneTime []models.OneTimeExpense) float64 {
		t.Helper()
		in := engineInput(t, oneTimeExpenseRegressionSettings(oneTime))
		rng := rand.New(rand.NewSource(seed))
		config := DefaultMonteCarloConfig()

		var sum float64
		for i := 0; i < runs; i++ {
			sum += RunSingleMonteCarloSimulation(in, rng, config).FinalBalance
		}
		return sum / float64(runs)
	}

	withMean := run([]models.OneTimeExpense{{Description: "roof", Year: 3, Amount: 50_000}})
	withoutMean := run(nil)

	// Same seed → identical random draws consumed elsewhere, so the only
	// legitimate source of a difference is the one-time expense itself.
	if withMean >= withoutMean {
		t.Errorf("expected mean final balance to drop with a $50k one-time expense; got with=%.2f without=%.2f "+
			"(one-time expense silently dropped from the MC accumulator?)", withMean, withoutMean)
	}
}
