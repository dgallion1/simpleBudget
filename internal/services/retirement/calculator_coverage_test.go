package retirement

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

func defaultSettingsForTest() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 1_000_000
	s.ProjectionYears = 10
	s.CurrentAge = 65
	s.MonthlyLivingExpenses = 3000
	s.InvestmentReturn = 6.0
	return s
}

// --- runSingleHistoricalSequence tests ---

func TestRunSingleHistoricalSequence_RothConversion(t *testing.T) {
	s := defaultSettingsForTest()
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 50000,
		StartYear:    0,
		EndYear:      5,
	}
	s.TaxDeferredPercent = 80
	s.RothPercent = 10

	c := newTestCalc(t, s)
	result := c.runSingleHistoricalSequence(1990)

	if !result.Survives {
		t.Fatal("expected portfolio to survive with Roth conversion enabled")
	}
	if result.FinalBalance <= 0 {
		t.Error("expected positive final balance")
	}
}

func TestRunSingleHistoricalSequence_BigTicketItems(t *testing.T) {
	t.Run("expense", func(t *testing.T) {
		s := defaultSettingsForTest()
		s.BigTicketItems = []models.BigTicketItem{
			{ID: "1", Name: "Roof", Amount: 30000, Year: 2, Type: models.BigTicketExpense},
		}
		c := newTestCalc(t, s)
		result := c.runSingleHistoricalSequence(1990)

		if !result.Survives {
			t.Fatal("expected portfolio to survive with big ticket expense")
		}
	})

	t.Run("income", func(t *testing.T) {
		s := defaultSettingsForTest()
		s.BigTicketItems = []models.BigTicketItem{
			{ID: "2", Name: "Inheritance", Amount: 100000, Year: 3, Type: models.BigTicketIncome},
		}
		c := newTestCalc(t, s)
		result := c.runSingleHistoricalSequence(1990)

		if !result.Survives {
			t.Fatal("expected portfolio to survive with big ticket income")
		}
		if result.FinalBalance <= 0 {
			t.Error("expected positive final balance with inheritance")
		}
	})
}

func TestRunSingleHistoricalSequence_SpendingPhases(t *testing.T) {
	s := defaultSettingsForTest()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  models.DefaultSpendingPhases(),
	}

	c := newTestCalc(t, s)
	result := c.runSingleHistoricalSequence(1990)

	if !result.Survives {
		t.Fatal("expected portfolio to survive with spending phases enabled")
	}
}

// --- RunProjection tests ---

func TestRunProjection_RothConversion(t *testing.T) {
	s := defaultSettingsForTest()
	s.TaxDeferredPercent = 70
	s.RothPercent = 10
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 40000,
		StartYear:    0,
		EndYear:      5,
	}

	c := newTestCalc(t, s)
	result := c.RunProjection()

	if result == nil {
		t.Fatal("expected non-nil projection result")
	}
	if len(result.Months) == 0 {
		t.Fatal("expected non-empty projection")
	}

	// After Roth conversions, Roth balance should have grown relative to start
	last := result.Months[len(result.Months)-1]
	initialRoth := s.PortfolioValue * (s.RothPercent / 100)
	if last.RothBalance <= initialRoth {
		t.Errorf("expected Roth balance to grow from conversions, got %f vs initial %f", last.RothBalance, initialRoth)
	}
}

func TestRunProjection_BigTicketItems(t *testing.T) {
	s := defaultSettingsForTest()
	s.BigTicketItems = []models.BigTicketItem{
		{ID: "1", Name: "New Car", Amount: 40000, Year: 1, Type: models.BigTicketExpense},
		{ID: "2", Name: "Home Sale", Amount: 200000, Year: 3, Type: models.BigTicketIncome},
	}

	c := newTestCalc(t, s)
	result := c.RunProjection()

	if result == nil {
		t.Fatal("expected non-nil projection result")
	}
	if len(result.Months) == 0 {
		t.Fatal("expected non-empty projection")
	}
}

func TestRunProjection_SpendingPhasesWithDiscretionary(t *testing.T) {
	s := defaultSettingsForTest()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  models.DefaultSpendingPhases(),
	}
	s.ExpenseSources = []models.ExpenseSource{
		{ID: "1", Name: "Dining", Amount: 300, StartYear: 0, EndYear: 0, Inflation: true, Discretionary: true},
		{ID: "2", Name: "Insurance", Amount: 200, StartYear: 0, EndYear: 0, Inflation: true, Discretionary: false},
	}

	c := newTestCalc(t, s)
	result := c.RunProjection()

	if result == nil {
		t.Fatal("expected non-nil projection result")
	}
	if len(result.Months) == 0 {
		t.Fatal("expected non-empty projection")
	}
}

// --- engine.GetLifeExpectancyFactor tests ---

func TestGetLifeExpectancyFactor(t *testing.T) {
	t.Run("age below 72", func(t *testing.T) {
		if f := engine.GetLifeExpectancyFactor(60); f != 0 {
			t.Errorf("expected 0 for age 60, got %f", f)
		}
	})

	t.Run("age 73", func(t *testing.T) {
		if f := engine.GetLifeExpectancyFactor(73); f != 26.5 {
			t.Errorf("expected 26.5 for age 73, got %f", f)
		}
	})

	t.Run("age above 120", func(t *testing.T) {
		if f := engine.GetLifeExpectancyFactor(125); f != 2.0 {
			t.Errorf("expected 2.0 for age 125, got %f", f)
		}
	})
}

// --- engine.CalculateRMD tests ---

func TestCalculateRMD_BelowStartAge(t *testing.T) {
	amount, pct := engine.CalculateRMD(500000, 65)
	if amount != 0 || pct != 0 {
		t.Errorf("expected (0, 0) for age 65, got (%f, %f)", amount, pct)
	}
}

// --- GetAvailableStartYears tests ---

func TestGetAvailableStartYears_EdgeCases(t *testing.T) {
	years := GetAvailableStartYears(30)
	if len(years) == 0 {
		t.Fatal("expected available start years for 30-year projection")
	}
	if years[0] != HistoricalReturns[0].Year {
		t.Errorf("expected first year %d, got %d", HistoricalReturns[0].Year, years[0])
	}
	// Each start year must allow a full 30-year sequence.
	// F-057: last viable start year is lastDataYear - N + 1 (inclusive).
	lastAllowed := HistoricalReturns[len(HistoricalReturns)-1].Year - 30 + 1
	lastReturned := years[len(years)-1]
	if lastReturned > lastAllowed {
		t.Errorf("last start year %d exceeds allowed %d", lastReturned, lastAllowed)
	}

	// Requesting more years than available data returns nil
	tooMany := GetAvailableStartYears(len(HistoricalReturns) + 1)
	if tooMany != nil {
		t.Error("expected nil for projection longer than available data")
	}
}

// The sqrt helper that previously lived in historical_data.go is
// gone — math.Sqrt is now used directly inside the history package.
// The retirement-side sqrt-coverage tests were retired with the move.

// --- Tax calculator tests ---

func TestGetAdjustedStandardDeduction_MarriedJoint(t *testing.T) {
	tc := engine.NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingMarriedJoint,
		StateIncomeTaxRate: models.FloatPtr(5.0),
	}, 3.0)

	deduction := tc.GetAdjustedStandardDeduction(0)
	expected := 29200.0 // 2024 married joint standard deduction
	if deduction != expected {
		t.Errorf("expected %f, got %f", expected, deduction)
	}

	// With inflation adjustment
	adjusted := tc.GetAdjustedStandardDeduction(5)
	if adjusted <= expected {
		t.Errorf("expected inflation-adjusted deduction > %f, got %f", expected, adjusted)
	}
}

func TestGetBracketRate_ZeroIncome(t *testing.T) {
	tc := engine.NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: models.FloatPtr(0),
	}, 3.0)

	rate := tc.GetBracketRate(0, 0)
	if rate != 10 {
		t.Errorf("expected 10%% marginal rate for zero income, got %f", rate)
	}
}

func TestGetAdjustedStandardDeduction_UnknownFiling(t *testing.T) {
	tc := engine.NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       "unknown_status",
		StateIncomeTaxRate: models.FloatPtr(0),
	}, 3.0)

	// Should fall back to married_joint deduction
	deduction := tc.GetAdjustedStandardDeduction(0)
	if deduction != 29200 {
		t.Errorf("expected fallback to married_joint deduction 29200, got %f", deduction)
	}
}

func TestFindSteadyStateMonth_MultipleIncomeSources(t *testing.T) {
	s := defaultSettingsForTest()
	endMonth := 60
	s.IncomeSources = []models.IncomeSource{
		{ID: "1", Name: "SS", Amount: 2000, StartMonth: 24},
		{ID: "2", Name: "Pension", Amount: 1500, StartMonth: 36},
		{ID: "3", Name: "Short-term", Amount: 500, StartMonth: 48, EndMonth: &endMonth},
	}

	c := newTestCalc(t, s)
	month := c.findSteadyStateMonth()

	// Should be max of valid start months: 48 (short-term ends at 60 > 48, so valid)
	if month != 48 {
		t.Errorf("expected steady state month 48, got %d", month)
	}
}

func TestFindSteadyStateMonth_SourceAlreadyEnded(t *testing.T) {
	s := defaultSettingsForTest()
	endMonth := 12 // ends at month 12
	s.IncomeSources = []models.IncomeSource{
		{ID: "1", Name: "Temp", Amount: 500, StartMonth: 24, EndMonth: &endMonth},
		{ID: "2", Name: "Pension", Amount: 2000, StartMonth: 12},
	}

	c := newTestCalc(t, s)
	month := c.findSteadyStateMonth()

	// Source "Temp" starts at 24 but ends at 12 (EndMonth <= StartMonth), so not valid
	// "Pension" starts at 12
	if month != 12 {
		t.Errorf("expected steady state month 12, got %d", month)
	}
}

func TestFindSteadyStateMonth_BeyondProjection(t *testing.T) {
	s := defaultSettingsForTest()
	s.ProjectionYears = 5
	s.IncomeSources = []models.IncomeSource{
		{ID: "1", Name: "SS", Amount: 2000, StartMonth: 120}, // 10 years, beyond 5yr projection
	}

	c := newTestCalc(t, s)
	month := c.findSteadyStateMonth()

	// Should be capped at projection length (60 months)
	if month != 60 {
		t.Errorf("expected 60 (capped at projection), got %d", month)
	}
}

func TestGetBracketRate_TopBracket(t *testing.T) {
	tc := engine.NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: models.FloatPtr(0),
	}, 0)

	// Very high income should hit top bracket (37%)
	rate := tc.GetBracketRate(1_000_000, 0)
	if rate != 37 {
		t.Errorf("expected 37%% for very high income, got %f", rate)
	}
}

func TestRunSingleHistoricalSequence_Depletion(t *testing.T) {
	s := defaultSettingsForTest()
	s.PortfolioValue = 50_000
	s.MonthlyLivingExpenses = 10_000
	s.InvestmentReturn = 2.0
	s.ProjectionYears = 10

	c := newTestCalc(t, s)
	result := c.runSingleHistoricalSequence(1990)

	if result.Survives {
		t.Fatal("expected portfolio to deplete with high expenses")
	}
	if result.FinalBalance != 0 {
		t.Errorf("expected 0 final balance on depletion, got %f", result.FinalBalance)
	}
}

func TestRunSingleHistoricalSequence_WithSurplusIncome(t *testing.T) {
	s := defaultSettingsForTest()
	s.PortfolioValue = 500_000
	s.MonthlyLivingExpenses = 1000
	s.InvestmentReturn = 5.0
	s.ProjectionYears = 5
	s.IncomeSources = []models.IncomeSource{
		{ID: "1", Name: "Pension", Amount: 5000, StartMonth: 0, COLARate: 0.02},
	}

	c := newTestCalc(t, s)
	result := c.runSingleHistoricalSequence(1990)

	if !result.Survives {
		t.Fatal("expected to survive with surplus income")
	}
	// With surplus income reinvested, final balance should exceed initial
	if result.FinalBalance <= s.PortfolioValue {
		t.Error("expected final balance to exceed initial with surplus income")
	}
}

func TestRunProjection_MonteCarloWithDiscretionary(t *testing.T) {
	s := defaultSettingsForTest()
	s.InvestmentReturn = 7.0
	s.ProjectionYears = 5
	s.ExpenseSources = []models.ExpenseSource{
		{ID: "1", Name: "Travel", Amount: 500, Discretionary: true},
	}

	c := newTestCalc(t, s)
	result := c.RunMonteCarloSimulation(50)

	if result == nil || result.Stats == nil {
		t.Fatal("expected non-nil Monte Carlo result")
	}
	if result.Stats.Runs != 50 {
		t.Errorf("expected 50 runs, got %d", result.Stats.Runs)
	}
}

// TestFullyTaxableAccount verifies that a portfolio with 0% tax-deferred and
// 0% Roth (100% taxable) works correctly across projection, backtest, budget
// fit, Monte Carlo, and steady-state. No RMDs should fire, no early withdrawal
// penalty should apply, and all withdrawals should come from taxable.
func TestFullyTaxableAccount(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 0
	s.RothPercent = 0
	s.MonthlyLivingExpenses = 4000
	s.InvestmentReturn = 6.0
	s.ProjectionYears = 10
	s.CurrentAge = 55 // Young enough to trigger penalty if tax-deferred were used

	t.Run("projection", func(t *testing.T) {
		c := newTestCalc(t, s)
		result := c.RunProjection()

		if !result.Survives {
			t.Fatal("expected fully taxable portfolio to survive")
		}

		// Verify no tax-deferred or Roth withdrawals in any month
		for i, m := range result.Months {
			if m.WithdrawalFromTaxDeferred != 0 {
				t.Errorf("month %d: unexpected tax-deferred withdrawal %f", i, m.WithdrawalFromTaxDeferred)
				break
			}
			if m.WithdrawalFromRoth != 0 {
				t.Errorf("month %d: unexpected Roth withdrawal %f", i, m.WithdrawalFromRoth)
				break
			}
			if m.RMDWithdrawal != 0 {
				t.Errorf("month %d: unexpected RMD withdrawal %f", i, m.RMDWithdrawal)
				break
			}
		}
	})

	t.Run("backtest", func(t *testing.T) {
		c := newTestCalc(t, s)
		result := c.runSingleHistoricalSequence(1990)

		if !result.Survives {
			t.Fatal("expected fully taxable backtest to survive")
		}
	})

	t.Run("budget_fit", func(t *testing.T) {
		c := newTestCalc(t, s)
		bf := c.CalculateBudgetFit()

		if bf.MonthlyRMD != 0 {
			t.Errorf("expected 0 RMD for fully taxable, got %f", bf.MonthlyRMD)
		}
	})

	t.Run("monte_carlo", func(t *testing.T) {
		c := newTestCalc(t, s)
		mc := c.RunMonteCarloSimulation(50)

		if mc == nil || mc.Stats == nil {
			t.Fatal("expected non-nil Monte Carlo result")
		}
		if mc.Stats.SuccessRate <= 0 {
			t.Error("expected positive success rate for well-funded taxable portfolio")
		}
	})

	t.Run("rmd_analysis", func(t *testing.T) {
		c := newTestCalc(t, s)
		rmd := c.BuildRMDAnalysis(c.RunProjection())

		if rmd.TaxDeferredValue != 0 {
			t.Errorf("expected 0 tax-deferred value, got %f", rmd.TaxDeferredValue)
		}
		if len(rmd.Projections) != 0 {
			t.Errorf("expected 0 RMD projections, got %d", len(rmd.Projections))
		}
	})
}
