package retirement

import (
	"math"
	"math/rand"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
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

func highIncomeMedicareSimulationSettings(age int) *models.WhatIfSettings {
	s := taxableOnlySimulationSettings()
	s.PortfolioValue = 1_000_000
	s.CurrentAge = age
	s.MonthlyLivingExpenses = 0
	s.IncomeSources = []models.IncomeSource{
		{ID: "pension", Name: "Pension", Amount: 25000, StartMonth: 0},
	}
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	return s
}

func TestHistoricalSequence_HighTaxableDividendYieldReducesFinalBalance(t *testing.T) {
	base := taxableOnlySimulationSettings()
	highDividend := *base
	highDividend.TaxableDividendYield = 4.0

	baseResult := newTestCalc(t, base).runSingleHistoricalSequence(1982)
	highDividendResult := newTestCalc(t, &highDividend).runSingleHistoricalSequence(1982)

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

	baseResult := newTestCalc(t, base).runSingleMonteCarloSimulation(rand.New(rand.NewSource(42)), config)
	highDividendResult := newTestCalc(t, &highDividend).runSingleMonteCarloSimulation(rand.New(rand.NewSource(42)), config)

	if highDividendResult.FinalBalance >= baseResult.FinalBalance {
		t.Fatalf("expected taxable dividends to reduce Monte Carlo final balance, got base=%.2f high-div=%.2f",
			baseResult.FinalBalance, highDividendResult.FinalBalance)
	}
}

func TestHistoricalSequence_MedicareAgeDoesNotImmediatelyApplyIRMALaggedPremiums(t *testing.T) {
	preMedicare := highIncomeMedicareSimulationSettings(64)
	medicare := highIncomeMedicareSimulationSettings(65)

	preResult := newTestCalc(t, preMedicare).runSingleHistoricalSequence(1982)
	medicareResult := newTestCalc(t, medicare).runSingleHistoricalSequence(1982)

	if math.Abs(medicareResult.FinalBalance-preResult.FinalBalance) > 0.01 {
		t.Fatalf("expected no immediate IRMAA drag without lookback history, got pre=%.2f medicare=%.2f",
			preResult.FinalBalance, medicareResult.FinalBalance)
	}
}

// F-049 + F-073 + F-1: engine.ReinvestRequiredRMDToTaxableState moves the gross
// distribution into the taxable account with matching cost basis (F-049's goal:
// basis equals what was deposited, so later LTCG is right), and returns that
// gross so callers report it as taxable income (F-073). It does not withhold
// tax — the month's tax model already levies it on the gross and funds it as a
// separate cash outflow, so withholding here charged it twice (F-1).
func TestReinvestRequiredRMD_DepositsGrossWithGrossBasis(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := engine.NewTaxableAccountState(s, 0)
	taxDeferred := 100000.0
	rmd := 10000.0

	gross := engine.ReinvestRequiredRMDToTaxableState(rmd, &taxDeferred, &taxable)
	wantGross := 10000.0

	if math.Abs(gross-wantGross) > 0.01 {
		t.Errorf("gross = %.2f; want %.2f", gross, wantGross)
	}
	if math.Abs(taxable.MarketValue-wantGross) > 0.01 {
		t.Errorf("taxable.MarketValue = %.2f; want %.2f", taxable.MarketValue, wantGross)
	}
	if math.Abs(taxable.CostBasis-wantGross) > 0.01 {
		t.Errorf("taxable.CostBasis = %.2f; want %.2f", taxable.CostBasis, wantGross)
	}
	if math.Abs(taxDeferred-90000.0) > 0.01 {
		t.Errorf("taxDeferred remaining = %.2f; want 90000", taxDeferred)
	}
}

func TestReinvestRequiredRMD_ClampedToAvailableBalance(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := engine.NewTaxableAccountState(s, 0)
	taxDeferred := 4000.0
	gross := engine.ReinvestRequiredRMDToTaxableState(10000, &taxDeferred, &taxable)
	if math.Abs(gross-4000.0) > 0.01 {
		t.Errorf("gross = %.2f; want 4000 (clamped to the remaining balance)", gross)
	}
	if math.Abs(taxDeferred) > 0.01 {
		t.Errorf("taxDeferred = %.2f; want 0", taxDeferred)
	}
	if math.Abs(taxable.MarketValue-4000.0) > 0.01 {
		t.Errorf("taxable.MarketValue = %.2f; want 4000", taxable.MarketValue)
	}
}

func TestReinvestRequiredRMD_NoopWhenNothingToDistribute(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := engine.NewTaxableAccountState(s, 0)
	taxDeferred := 100000.0
	if gross := engine.ReinvestRequiredRMDToTaxableState(0, &taxDeferred, &taxable); gross != 0 {
		t.Errorf("zero-RMD gross = %.2f; want 0", gross)
	}
	empty := 0.0
	if gross := engine.ReinvestRequiredRMDToTaxableState(10000, &empty, &taxable); gross != 0 {
		t.Errorf("empty-account gross = %.2f; want 0", gross)
	}
	if taxable.MarketValue != 0 {
		t.Errorf("taxable.MarketValue = %.2f; want 0 (nothing was distributed)", taxable.MarketValue)
	}
}

func TestMonteCarlo_MedicareAgeDoesNotImmediatelyApplyIRMALaggedPremiums(t *testing.T) {
	preMedicare := highIncomeMedicareSimulationSettings(64)
	medicare := highIncomeMedicareSimulationSettings(65)

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

	preResult := newTestCalc(t, preMedicare).runSingleMonteCarloSimulation(rand.New(rand.NewSource(42)), config)
	medicareResult := newTestCalc(t, medicare).runSingleMonteCarloSimulation(rand.New(rand.NewSource(42)), config)

	if math.Abs(medicareResult.FinalBalance-preResult.FinalBalance) > 0.01 {
		t.Fatalf("expected no immediate IRMAA drag without lookback history, got pre=%.2f medicare=%.2f",
			preResult.FinalBalance, medicareResult.FinalBalance)
	}
}
