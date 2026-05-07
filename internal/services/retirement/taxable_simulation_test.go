package retirement

import (
	"math"
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

func TestHistoricalSequence_MedicareAgeDoesNotImmediatelyApplyIRMALaggedPremiums(t *testing.T) {
	preMedicare := highIncomeMedicareSimulationSettings(64)
	medicare := highIncomeMedicareSimulationSettings(65)

	preResult := NewCalculator(preMedicare).runSingleHistoricalSequence(1982)
	medicareResult := NewCalculator(medicare).runSingleHistoricalSequence(1982)

	if math.Abs(medicareResult.FinalBalance-preResult.FinalBalance) > 0.01 {
		t.Fatalf("expected no immediate IRMAA drag without lookback history, got pre=%.2f medicare=%.2f",
			preResult.FinalBalance, medicareResult.FinalBalance)
	}
}

// F-049 + F-073: reinvestRequiredRMDToTaxableState reinvests the after-tax
// portion as basis (F-049) and returns the gross distribution amount that
// callers report as taxable income (F-073).
func TestReinvestRequiredRMD_F049_BasisIsAfterTax(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := newTaxableAccountState(s, 0)
	taxDeferred := 100000.0
	rmd := 10000.0
	marginalRate := 0.22 // 22%

	gross, net := reinvestRequiredRMDToTaxableState(rmd, marginalRate, &taxDeferred, &taxable)
	wantGross := 10000.0
	wantNet := 7800.0

	if math.Abs(gross-wantGross) > 0.01 {
		t.Errorf("gross = %.2f; want %.2f", gross, wantGross)
	}
	if math.Abs(net-wantNet) > 0.01 {
		t.Errorf("net = %.2f; want %.2f", net, wantNet)
	}
	if math.Abs(taxable.MarketValue-wantNet) > 0.01 {
		t.Errorf("taxable.MarketValue = %.2f; want %.2f", taxable.MarketValue, wantNet)
	}
	if math.Abs(taxable.CostBasis-wantNet) > 0.01 {
		t.Errorf("taxable.CostBasis = %.2f; want %.2f", taxable.CostBasis, wantNet)
	}
	if math.Abs(taxDeferred-90000.0) > 0.01 {
		t.Errorf("taxDeferred remaining = %.2f; want 90000", taxDeferred)
	}
}

func TestReinvestRequiredRMD_F049_ZeroMarginalRate(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := newTaxableAccountState(s, 0)
	taxDeferred := 100000.0
	gross, net := reinvestRequiredRMDToTaxableState(10000, 0.0, &taxDeferred, &taxable)
	if math.Abs(gross-10000.0) > 0.01 {
		t.Errorf("zero-marginal gross = %.2f; want 10000", gross)
	}
	if math.Abs(net-10000.0) > 0.01 {
		t.Errorf("zero-marginal net = %.2f; want 10000", net)
	}
}

func TestReinvestRequiredRMD_F049_MarginalRateClamped(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := newTaxableAccountState(s, 0)
	taxDeferred := 100000.0
	gross, net := reinvestRequiredRMDToTaxableState(10000, 1.5, &taxDeferred, &taxable)
	if math.Abs(gross-10000.0) > 0.01 {
		t.Errorf("marginal>1 gross = %.2f; want 10000 (gross unchanged by rate clamp)", gross)
	}
	if math.Abs(net-0.0) > 0.01 {
		t.Errorf("marginal>1 net = %.2f; want 0", net)
	}
	taxDeferred = 100000.0
	taxable = newTaxableAccountState(s, 0)
	gross, net = reinvestRequiredRMDToTaxableState(10000, -0.5, &taxDeferred, &taxable)
	if math.Abs(gross-10000.0) > 0.01 {
		t.Errorf("marginal<0 gross = %.2f; want 10000", gross)
	}
	if math.Abs(net-10000.0) > 0.01 {
		t.Errorf("marginal<0 net = %.2f; want 10000", net)
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

	preResult := NewCalculator(preMedicare).runSingleMonteCarloSimulation(rand.New(rand.NewSource(42)), config)
	medicareResult := NewCalculator(medicare).runSingleMonteCarloSimulation(rand.New(rand.NewSource(42)), config)

	if math.Abs(medicareResult.FinalBalance-preResult.FinalBalance) > 0.01 {
		t.Fatalf("expected no immediate IRMAA drag without lookback history, got pre=%.2f medicare=%.2f",
			preResult.FinalBalance, medicareResult.FinalBalance)
	}
}
