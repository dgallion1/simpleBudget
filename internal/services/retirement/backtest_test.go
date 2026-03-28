package retirement

import (
	"sort"
	"testing"

	"budget2/internal/models"
)

func TestHistoricalDataExists(t *testing.T) {
	data := GetHistoricalReturns()

	if len(data) < 90 {
		t.Errorf("Expected at least 90 years of data, got %d", len(data))
	}

	// Check first year
	if data[0].Year != 1928 {
		t.Errorf("Expected first year 1928, got %d", data[0].Year)
	}

	// Check data is sequential
	for i := 1; i < len(data); i++ {
		if data[i].Year != data[i-1].Year+1 {
			t.Errorf("Data not sequential at index %d: %d followed by %d", i-1, data[i-1].Year, data[i].Year)
		}
	}
}

func TestGetHistoricalSequence(t *testing.T) {
	// Valid sequence
	seq := GetHistoricalSequence(1950, 30)
	if seq == nil {
		t.Fatal("Expected valid sequence for 1950")
	}
	if len(seq) != 30 {
		t.Errorf("Expected 30 years, got %d", len(seq))
	}
	if seq[0].Year != 1950 {
		t.Errorf("Expected first year 1950, got %d", seq[0].Year)
	}

	// Invalid start year (before data)
	seq = GetHistoricalSequence(1900, 30)
	if seq != nil {
		t.Error("Expected nil for year before data")
	}

	// Not enough data remaining
	seq = GetHistoricalSequence(2020, 30)
	if seq != nil {
		t.Error("Expected nil when not enough data remaining")
	}
}

func TestGetAvailableStartYears(t *testing.T) {
	years := GetAvailableStartYears(30)

	if len(years) == 0 {
		t.Fatal("Expected some available years")
	}

	// First available year should be 1928
	if years[0] != 1928 {
		t.Errorf("First year should be 1928, got %d", years[0])
	}

	// Should not include years that don't have 30 years of remaining data
	lastYear := years[len(years)-1]
	if lastYear > GetHistoricalReturns()[len(GetHistoricalReturns())-1].Year-29 {
		t.Errorf("Last available year %d is too recent", lastYear)
	}
}

func TestGetHistoricalStats(t *testing.T) {
	avgStock, avgBond, avgCash, avgInflation, stockStdDev, bondStdDev := GetHistoricalStats()

	// Stock average should be positive (historically ~10%)
	if avgStock < 5 || avgStock > 15 {
		t.Errorf("Stock average %f outside reasonable range [5, 15]", avgStock)
	}

	// Bond average should be positive (historically ~5%)
	if avgBond < 1 || avgBond > 10 {
		t.Errorf("Bond average %f outside reasonable range [1, 10]", avgBond)
	}

	// Cash average should be positive (historically ~3%)
	if avgCash < 1 || avgCash > 6 {
		t.Errorf("Cash average %f outside reasonable range [1, 6]", avgCash)
	}

	// Inflation average should be positive (historically ~3%)
	if avgInflation < 1 || avgInflation > 6 {
		t.Errorf("Inflation average %f outside reasonable range [1, 6]", avgInflation)
	}

	// Stock standard deviation should be meaningful
	if stockStdDev < 10 || stockStdDev > 25 {
		t.Errorf("Stock std dev %f outside reasonable range [10, 25]", stockStdDev)
	}

	// Bond standard deviation should be meaningful
	if bondStdDev < 5 || bondStdDev > 15 {
		t.Errorf("Bond std dev %f outside reasonable range [5, 15]", bondStdDev)
	}
}

func TestRunHistoricalBacktest(t *testing.T) {
	settings := &models.WhatIfSettings{
		PortfolioValue:        1000000,
		MonthlyLivingExpenses: 3500,
		CurrentAge:            65,
		ProjectionYears:       30,
		InvestmentReturn:      6.0,
		InflationRate:         2.5,
		TaxDeferredPercent:    60.0,
		RothPercent:           10.0,
	}

	calc := NewCalculator(settings)
	result := calc.RunHistoricalBacktest()

	if result == nil {
		t.Fatal("Expected backtest result")
	}

	if result.TotalSequences == 0 {
		t.Error("Expected some sequences")
	}

	// Success rate should be between 0 and 100
	if result.SuccessRate < 0 || result.SuccessRate > 100 {
		t.Errorf("Success rate %f out of range [0, 100]", result.SuccessRate)
	}

	// Should have worst and best years
	if len(result.WorstStartYears) == 0 {
		t.Error("Expected worst start years")
	}
	if len(result.BestStartYears) == 0 {
		t.Error("Expected best start years")
	}

	// Data years should match our data
	if result.DataStartYear != 1928 {
		t.Errorf("Expected data start year 1928, got %d", result.DataStartYear)
	}
}

func TestYearsUntilDepletion_UsesRelativeFailureTiming(t *testing.T) {
	result := HistoricalSequenceResult{
		StartYear:     1982,
		DepletionYear: 1990,
	}

	if got := yearsUntilDepletion(result); got != 8 {
		t.Fatalf("yearsUntilDepletion() = %d, want 8", got)
	}
}

func TestYearsUntilDepletion_SameYearFailureReturnsZero(t *testing.T) {
	result := HistoricalSequenceResult{
		StartYear:     1980,
		DepletionYear: 1980,
	}

	if got := yearsUntilDepletion(result); got != 0 {
		t.Fatalf("yearsUntilDepletion() = %d, want 0", got)
	}
}

func TestHistoricalFailureOrderingUsesRelativeTiming(t *testing.T) {
	results := []HistoricalSequenceResult{
		{
			StartYear:     1928,
			Survives:      false,
			DepletionYear: 1938, // fails after 10 years
		},
		{
			StartYear:     1980,
			Survives:      false,
			DepletionYear: 1985, // fails after 5 years
		},
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Survives != results[j].Survives {
			return !results[i].Survives
		}
		if !results[i].Survives {
			return yearsUntilDepletion(results[i]) < yearsUntilDepletion(results[j])
		}
		return results[i].FinalBalance < results[j].FinalBalance
	})

	if results[0].StartYear != 1980 {
		t.Fatalf("expected earlier relative failure to rank worse, got start year %d first", results[0].StartYear)
	}
}

func TestRunSingleHistoricalSequence(t *testing.T) {
	settings := &models.WhatIfSettings{
		PortfolioValue:        1000000,
		MonthlyLivingExpenses: 3000,
		CurrentAge:            65,
		ProjectionYears:       30,
		InvestmentReturn:      6.0,
		InflationRate:         2.5,
		TaxDeferredPercent:    60.0,
		RothPercent:           10.0,
	}

	calc := NewCalculator(settings)

	// Test a known good period (1982 bull market start)
	goodResult := calc.runSingleHistoricalSequence(1982)
	if !goodResult.Survives {
		t.Error("1982 sequence should typically survive with reasonable withdrawal")
	}

	// Test starting before Great Depression
	depressionResult := calc.runSingleHistoricalSequence(1929)
	// We just verify it runs without error - survival depends on withdrawal rate
	if depressionResult.StartYear != 1929 {
		t.Errorf("Expected start year 1929, got %d", depressionResult.StartYear)
	}
}

func TestHistoricalBacktestHonorsProjectionTiming(t *testing.T) {
	base := models.DefaultWhatIfSettings()
	base.PortfolioValue = 1_000_000
	base.MonthlyLivingExpenses = 3_000
	base.MonthlyHealthcare = 0
	base.HealthcarePersons = nil
	base.ExpenseSources = nil
	base.IncomeSources = nil
	base.InflationRate = 0
	base.SpendingDeclineRate = 0
	base.ProjectionYears = 20
	base.InvestmentReturn = 0
	base.TaxDeferredPercent = 0
	base.RothPercent = 0
	base.StockPercent = 100
	base.CashPercent = 0
	base.TaxableStockPercent = 100
	base.TaxableCashPercent = 0

	startSettings := *base
	startSettings.ProjectionTiming = models.ProjectionTimingStartOfMonth
	endSettings := *base
	endSettings.ProjectionTiming = models.ProjectionTimingEndOfMonth

	startResult := NewCalculator(&startSettings).runSingleHistoricalSequence(1990)
	endResult := NewCalculator(&endSettings).runSingleHistoricalSequence(1990)

	if startResult.FinalBalance >= endResult.FinalBalance {
		t.Fatalf("expected start-of-month backtest balance below end-of-month, got start=%.2f end=%.2f",
			startResult.FinalBalance, endResult.FinalBalance)
	}
}

func TestBacktestWithBigTicketItems(t *testing.T) {
	settings := &models.WhatIfSettings{
		PortfolioValue:        1000000,
		MonthlyLivingExpenses: 3000,
		CurrentAge:            65,
		ProjectionYears:       20,
		InvestmentReturn:      6.0,
		InflationRate:         2.5,
		TaxDeferredPercent:    60.0,
		RothPercent:           10.0,
		BigTicketItems: []models.BigTicketItem{
			{
				ID:     "test1",
				Name:   "Home Sale",
				Amount: 200000,
				Year:   5,
				Type:   models.BigTicketIncome,
			},
		},
	}

	calc := NewCalculator(settings)
	result := calc.runSingleHistoricalSequence(1990)

	// With a big income item, should likely survive
	if result.StartYear != 1990 {
		t.Errorf("Expected start year 1990, got %d", result.StartYear)
	}
	// Result should have positive final balance with the extra income
}

func TestBacktestWithRothConversion(t *testing.T) {
	settings := &models.WhatIfSettings{
		PortfolioValue:        1000000,
		MonthlyLivingExpenses: 3000,
		CurrentAge:            65,
		ProjectionYears:       20,
		InvestmentReturn:      6.0,
		InflationRate:         2.5,
		TaxDeferredPercent:    80.0,
		RothPercent:           0.0,
		RothConversion: &models.RothConversionConfig{
			Enabled:      true,
			AnnualAmount: 50000,
			StartYear:    0,
			EndYear:      10,
		},
	}

	calc := NewCalculator(settings)
	result := calc.runSingleHistoricalSequence(1990)

	// Roth conversion should move money but not affect survival
	if result.StartYear != 1990 {
		t.Errorf("Expected start year 1990, got %d", result.StartYear)
	}
}

func TestFinalBalanceRealCalculation(t *testing.T) {
	// Test that FinalBalanceReal = FinalBalance / CumulativeInflation
	settings := &models.WhatIfSettings{
		PortfolioValue:        1000000,
		MonthlyLivingExpenses: 2500, // Low withdrawal to ensure survival
		CurrentAge:            65,
		ProjectionYears:       30,
		InvestmentReturn:      6.0,
		InflationRate:         2.5,
		TaxDeferredPercent:    50.0,
		RothPercent:           20.0,
		StockPercent:          60.0,
		CashPercent:           10.0,
	}

	calc := NewCalculator(settings)
	result := calc.runSingleHistoricalSequence(1982) // 1982 was a good starting year

	// If portfolio survives, verify the real balance calculation
	if result.Survives && result.FinalBalance > 0 && result.CumulativeInflation > 0 {
		expectedReal := result.FinalBalance / result.CumulativeInflation
		tolerance := 0.01 // Allow 1% tolerance for floating point

		diff := (result.FinalBalanceReal - expectedReal) / expectedReal
		if diff < -tolerance || diff > tolerance {
			t.Errorf("FinalBalanceReal calculation incorrect: got %.2f, expected %.2f (FinalBalance=%.2f, CumulativeInflation=%.4f)",
				result.FinalBalanceReal, expectedReal, result.FinalBalance, result.CumulativeInflation)
		}
	}

	// Also verify cumulative inflation is reasonable for 30 years
	// Historical inflation averaged ~3%, so 30 years should give roughly 1.03^30 ≈ 2.43
	// But it varies widely, so just ensure it's > 1.0
	if result.CumulativeInflation <= 1.0 {
		t.Errorf("CumulativeInflation should be > 1.0 for 30 years, got %.4f", result.CumulativeInflation)
	}
}

func TestAssetAllocationDefaults(t *testing.T) {
	// Test that the GetEffectiveAssetAllocation method works correctly

	// Case 1: No allocation set (both zero) - should default to 60/40/0
	settings := &models.WhatIfSettings{
		StockPercent: 0,
		CashPercent:  0,
	}
	stock, bond, cash := settings.GetEffectiveAssetAllocation()
	if stock != 60.0 || bond != 40.0 || cash != 0.0 {
		t.Errorf("Default allocation should be 60/40/0, got %.0f/%.0f/%.0f", stock, bond, cash)
	}

	// Case 2: Custom allocation set - should be used as-is
	settings2 := &models.WhatIfSettings{
		StockPercent: 80.0,
		CashPercent:  5.0,
	}
	stock2, bond2, cash2 := settings2.GetEffectiveAssetAllocation()
	if stock2 != 80.0 || bond2 != 15.0 || cash2 != 5.0 {
		t.Errorf("Custom allocation should be 80/15/5, got %.0f/%.0f/%.0f", stock2, bond2, cash2)
	}

	// Case 3: 100% bonds (0 stocks, 0 cash but intentionally set)
	// This is the edge case - user would need to set cash to a tiny value to indicate intent
	// With current design, if stock=0 and cash=0, we default to 60/40/0
	// This is documented behavior - users wanting 100% bonds should set cash to 0.01
	settings3 := &models.WhatIfSettings{
		StockPercent: 0,
		CashPercent:  0.01, // Tiny cash value indicates intentional allocation
	}
	stock3, bond3, _ := settings3.GetEffectiveAssetAllocation()
	if stock3 != 0.0 {
		t.Errorf("With intentional 0%% stocks and 0.01%% cash, stocks should be 0, got %.2f", stock3)
	}
	if bond3 < 99.9 {
		t.Errorf("With 0%% stocks and 0.01%% cash, bonds should be ~99.99%%, got %.2f", bond3)
	}
}

func TestHistoricalBacktest_ChainTransition(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 30
	primary.PortfolioValue = 500000
	primary.MonthlyLivingExpenses = 2000
	primary.TaxDeferredPercent = 50
	primary.RothPercent = 25

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 8000

	chainCalc := NewCalculatorWithChain(primary, []ResolvedScenarioChainLink{
		{TransitionAge: 70, Settings: linked},
	})
	noChainCalc := NewCalculator(primary)

	chainBT := chainCalc.RunHistoricalBacktest()
	noChainBT := noChainCalc.RunHistoricalBacktest()

	if chainBT == nil || noChainBT == nil {
		t.Fatal("expected non-nil backtest results")
	}

	if chainBT.SuccessRate >= noChainBT.SuccessRate {
		t.Errorf("chained backtest success (%f) should be lower than no-chain (%f)",
			chainBT.SuccessRate, noChainBT.SuccessRate)
	}
}
