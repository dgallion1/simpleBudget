package retirement

import (
	"math/rand"
	"testing"

	"budget2/internal/models"
)

func taxableOnlySimulationSettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 1_000_000
	s.MonthlyLivingExpenses = 2_000
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.ExpenseSources = nil
	s.IncomeSources = nil
	s.CurrentAge = 65
	s.ProjectionYears = 20
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.TaxDeferredPercent = 0
	s.RothPercent = 0
	s.StockPercent = 100
	s.CashPercent = 0
	s.TaxableStockPercent = 100
	s.TaxableCashPercent = 0
	s.TaxableDividendYield = 0
	s.TaxableQualifiedDividendPercent = 0
	s.TaxableCapitalGainsDistributionRate = 0
	return s
}

func TestHistoricalSequence_HighTaxableDividendYieldReducesFinalBalance(t *testing.T) {
	base := taxableOnlySimulationSettings()
	highDividend := *base
	highDividend.TaxableDividendYield = 4.0

	baseResult := NewCalculator(base).runSingleHistoricalSequence(1982)
	highDividendResult := NewCalculator(&highDividend).runSingleHistoricalSequence(1982)

	if highDividendResult.FinalBalance >= baseResult.FinalBalance {
		t.Fatalf("expected taxable dividends to reduce historical final balance, got base=%.2f high-div=%.2f",
			baseResult.FinalBalance, highDividendResult.FinalBalance)
	}
}

func TestMonteCarlo_HighTaxableDividendYieldReducesFinalBalance(t *testing.T) {
	base := taxableOnlySimulationSettings()
	highDividend := *base
	highDividend.TaxableDividendYield = 4.0

	config := &MonteCarloConfig{
		ReturnVolatility:        0,
		CrashProbability:        0,
		CrashSeverity:           -30,
		RecoveryBoost:           0,
		SpendingShockProb:       0,
		SpendingShockMin:        0,
		SpendingShockMax:        0,
		HealthShockProb:         0,
		HealthShockMin:          0,
		HealthShockMax:          0,
		LongevityVariation:      0,
		AdaptiveSpending:        false,
		DiscretionaryCutPercent: 0,
		AdaptationRecoveryYears: 0,
	}

	baseResult := NewCalculator(base).runSingleMonteCarloSimulation(rand.New(rand.NewSource(42)), config)
	highDividendResult := NewCalculator(&highDividend).runSingleMonteCarloSimulation(rand.New(rand.NewSource(42)), config)

	if highDividendResult.FinalBalance >= baseResult.FinalBalance {
		t.Fatalf("expected taxable dividends to reduce Monte Carlo final balance, got base=%.2f high-div=%.2f",
			baseResult.FinalBalance, highDividendResult.FinalBalance)
	}
}
