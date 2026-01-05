package retirement

import (
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
