package analysis

import (
	"math/rand"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/history"
)

// buildLoopParityAnalysisScenario mirrors retirement.buildLoopParityScenario
// but lives in the analysis package to avoid an import cycle (analysis cannot
// import its retirement parent).
//
// Settings: 100% Roth, $100k, 6% return, $2k/month expenses,
// clock starts 2026. At ~month 50 the cumulative withdrawals exceed the
// initial $100k basis, surfacing taxable earnings.
func buildLoopParityAnalysisScenario(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.CurrentAge = 65
	s.Persons = []models.Person{
		{
			ID:         "p1",
			Name:       "You",
			Role:       models.PersonRolePrimary,
			BirthMonth: models.BirthMonthForAge("2026-01", 65),
		},
	}
	s.PortfolioValue = 100_000
	s.TaxDeferredPercent = 0
	s.RothPercent = 100
	// Taxable implied = 0%
	s.MonthlyLivingExpenses = 2_000
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.ExpenseSources = nil
	s.IncomeSources = nil
	s.InvestmentReturn = 6.0
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.ProjectionYears = 10
	s.RothFirstFundedYear = 2026 // clock unsatisfied throughout projection
	return s
}

// TestProjectionLoops_RothStateParity_MCAndBacktest is the MC and backtest
// half of the 3-loop parity test. The deterministic half lives in the
// retirement package (TestProjectionLoops_RothStateParity_Deterministic)
// because it uses the Calculator directly.
//
// These sub-tests verify that the MC and backtest loops:
//   - Do not panic when Roth 5-year state is present.
//   - Produce structurally valid (non-trivial) results.
func TestProjectionLoops_RothStateParity_MCAndBacktest(t *testing.T) {
	t.Run("monte carlo loop does not panic", func(t *testing.T) {
		in := engineInput(t, buildLoopParityAnalysisScenario(t))
		rng := rand.New(rand.NewSource(1))
		cfg := DefaultMonteCarloConfig()

		result := RunSingleMonteCarloSimulation(in, rng, cfg)

		if result.DepletionYear < 0 {
			t.Fatalf("unexpected negative DepletionYear: %v", result.DepletionYear)
		}
		if result.ProjectionYears <= 0 {
			t.Fatalf("expected ProjectionYears > 0, got %d", result.ProjectionYears)
		}
	})

	t.Run("historical backtest loop does not panic", func(t *testing.T) {
		in := engineInput(t, buildLoopParityAnalysisScenario(t))

		result := runSingleHistoricalSequence(in, history.DefaultData(), 1990)

		if result.StartYear != 1990 {
			t.Fatalf("expected StartYear=1990, got %d", result.StartYear)
		}
		if result.FinalBalance < 0 {
			t.Fatalf("expected FinalBalance >= 0, got %.2f", result.FinalBalance)
		}
	})
}
