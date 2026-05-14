package analysis

import (
	"math/rand"
	"testing"

	"budget2/internal/models"
)

// buildRothFiveYearAnalysisScenario mirrors the engine-package helper but
// lives in the analysis package so it can be used by the MC and backtest
// smoke tests without importing retirement (which would create a cycle).
//
// See retirement.buildRothEarningsScenario for the design rationale.
func buildRothFiveYearAnalysisScenario(t *testing.T) *models.WhatIfSettings {
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
	// Taxable = 0%
	s.MonthlyLivingExpenses = 2_000
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.ExpenseSources = nil
	s.IncomeSources = nil
	s.InvestmentReturn = 6.0
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.ProjectionYears = 10
	return s
}

// TestRunSingleMonteCarloSimulation_RothBasisCarried is a smoke test that
// confirms runSingleMonteCarloSimulation does not panic and produces a
// structurally valid result when the Roth 5-year clock is set to the
// projection start year (clock starts unsatisfied, matures mid-run).
func TestRunSingleMonteCarloSimulation_RothBasisCarried(t *testing.T) {
	settings := buildRothFiveYearAnalysisScenario(t)
	// Clock starts unsatisfied for this run; matures in year 5.
	settings.RothFirstFundedYear = 2026

	in := engineInput(t, settings)
	rng := rand.New(rand.NewSource(42))
	cfg := DefaultMonteCarloConfig()

	result := RunSingleMonteCarloSimulation(in, rng, cfg)

	// DepletionYear == 0 means the portfolio survived; values < 0 are invalid.
	if result.DepletionYear < 0 {
		t.Fatalf("unexpected negative DepletionYear: %v", result.DepletionYear)
	}

	// ProjectionYears must be positive: the loop always runs at least the
	// base number of projection years (modulo longevity variation).
	if result.ProjectionYears <= 0 {
		t.Fatalf("expected ProjectionYears > 0, got %d", result.ProjectionYears)
	}

	// FinalBalance must be non-negative.
	if result.FinalBalance < 0 {
		t.Fatalf("expected FinalBalance >= 0, got %.2f", result.FinalBalance)
	}
}
