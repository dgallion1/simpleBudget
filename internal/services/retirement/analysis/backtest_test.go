package analysis

import (
	"sort"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/history"
)

func TestRunHistoricalBacktest(t *testing.T) {
	settings := &models.WhatIfSettings{
		StartDate: "2026-01",
		Persons: []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge("2026-01", 65)},
		},
		PortfolioValue:        1000000,
		MonthlyLivingExpenses: 3500,
		CurrentAge:            65,
		ProjectionYears:       30,
		InvestmentReturn:      6.0,
		InflationRate:         2.5,
		TaxDeferredPercent:    60.0,
		RothPercent:           10.0,
	}

	in := engineInput(t, settings)
	result := HistoricalBacktest(in, history.DefaultData())

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

	// SurvivedCount must agree with SuccessRate.
	expectedSurvived := int(result.SuccessRate / 100.0 * float64(result.TotalSequences))
	if result.SurvivedCount < expectedSurvived-1 || result.SurvivedCount > expectedSurvived+1 {
		t.Errorf("SurvivedCount=%d not consistent with SuccessRate=%.2f over %d sequences (expected ~%d)",
			result.SurvivedCount, result.SuccessRate, result.TotalSequences, expectedSurvived)
	}

	// Results table must be sorted with failures first (so users see why
	// the success rate isn't 100% without scrolling past survivors).
	seenSurvivor := false
	for i, r := range result.Results {
		if r.Survives {
			seenSurvivor = true
		} else if seenSurvivor {
			t.Errorf("Results[%d] is a failure that appears AFTER a survivor — failures must be sorted first", i)
			break
		}
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
		StartDate: "2026-01",
		Persons: []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge("2026-01", 65)},
		},
		PortfolioValue:        1000000,
		MonthlyLivingExpenses: 3000,
		CurrentAge:            65,
		ProjectionYears:       30,
		InvestmentReturn:      6.0,
		InflationRate:         2.5,
		TaxDeferredPercent:    60.0,
		RothPercent:           10.0,
	}

	in := engineInput(t, settings)
	data := history.DefaultData()

	// Test a known good period (1982 bull market start)
	goodResult := runSingleHistoricalSequence(in, data, 1982)
	if !goodResult.Survives {
		t.Error("1982 sequence should typically survive with reasonable withdrawal")
	}

	// Test starting before Great Depression
	depressionResult := runSingleHistoricalSequence(in, data, 1929)
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

	data := history.DefaultData()
	startResult := runSingleHistoricalSequence(engineInput(t, &startSettings), data, 1990)
	endResult := runSingleHistoricalSequence(engineInput(t, &endSettings), data, 1990)

	if startResult.FinalBalance >= endResult.FinalBalance {
		t.Fatalf("expected start-of-month backtest balance below end-of-month, got start=%.2f end=%.2f",
			startResult.FinalBalance, endResult.FinalBalance)
	}
}

func TestBacktestWithBigTicketItems(t *testing.T) {
	settings := &models.WhatIfSettings{
		StartDate: "2026-01",
		Persons: []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge("2026-01", 65)},
		},
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

	result := runSingleHistoricalSequence(engineInput(t, settings), history.DefaultData(), 1990)

	// With a big income item, should likely survive
	if result.StartYear != 1990 {
		t.Errorf("Expected start year 1990, got %d", result.StartYear)
	}
	// Result should have positive final balance with the extra income
}

func TestBacktestWithRothConversion(t *testing.T) {
	settings := &models.WhatIfSettings{
		StartDate: "2026-01",
		Persons: []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge("2026-01", 65)},
		},
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

	result := runSingleHistoricalSequence(engineInput(t, settings), history.DefaultData(), 1990)

	// Roth conversion should move money but not affect survival
	if result.StartYear != 1990 {
		t.Errorf("Expected start year 1990, got %d", result.StartYear)
	}
}

func TestFinalBalanceRealCalculation(t *testing.T) {
	// Test that FinalBalanceReal = FinalBalance / CumulativeInflation
	settings := &models.WhatIfSettings{
		StartDate: "2026-01",
		Persons: []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge("2026-01", 65)},
		},
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

	result := runSingleHistoricalSequence(engineInput(t, settings), history.DefaultData(), 1982) // 1982 was a good starting year

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

// TestHistoricalBacktest_ChainTransition lives in the retirement
// package (see backtest_chain_test.go) because chain transitions
// require engine.NextChainTransitionHook to be wired by retirement's
// init(). Analysis-package tests can't trigger that wiring without
// importing retirement (cycle). The retirement-side test exercises
// the same path through Calculator.RunHistoricalBacktest, which
// delegates here.
