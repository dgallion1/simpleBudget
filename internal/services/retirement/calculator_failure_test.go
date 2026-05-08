package retirement

import (
	"budget2/internal/models"
	"testing"
)

// healthySettings returns settings where the baseline projection survives.
// Uses a large portfolio, modest expenses, short projection, and explicit return rate.
func healthySettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 1_000_000
	s.MonthlyLivingExpenses = 2000
	s.MonthlyHealthcare = 0
	s.InvestmentReturn = 7.0
	s.InflationRate = 3.0
	s.ProjectionYears = 10
	s.CurrentAge = 65
	s.SpendingPhaseConfig = nil
	return s
}

// failingSettings returns settings where the baseline projection fails quickly.
func failingSettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 10_000
	s.MonthlyLivingExpenses = 8000
	s.MonthlyHealthcare = 0
	s.InvestmentReturn = 1.0
	s.InflationRate = 10.0
	s.ProjectionYears = 10
	s.CurrentAge = 65
	s.SpendingPhaseConfig = nil
	return s
}

func TestCalculateFailurePoints(t *testing.T) {
	t.Run("healthy portfolio returns failure points for all parameters", func(t *testing.T) {
		c := newTestCalc(t, healthySettings())
		result := c.CalculateFailurePoints()

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if !result.BaselineSurvives {
			t.Fatal("expected baseline to survive with healthy settings")
		}
		if len(result.FailurePoints) < 4 {
			t.Fatalf("expected at least 4 failure points, got %d", len(result.FailurePoints))
		}

		// Verify all expected parameter names are present
		found := map[string]bool{}
		for _, fp := range result.FailurePoints {
			found[fp.ParamName] = true
		}
		for _, name := range []string{"investment_return", "inflation_rate", "monthly_expenses", "portfolio_value"} {
			if !found[name] {
				t.Errorf("missing failure point for %s", name)
			}
		}
	})

	t.Run("failing portfolio returns empty failure points", func(t *testing.T) {
		c := newTestCalc(t, failingSettings())
		result := c.CalculateFailurePoints()

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.BaselineSurvives {
			t.Fatal("expected baseline to fail with failing settings")
		}
		if len(result.FailurePoints) != 0 {
			t.Fatalf("expected 0 failure points, got %d", len(result.FailurePoints))
		}
	})
}

func TestFindExpensesThreshold_ZeroExpenses(t *testing.T) {
	s := healthySettings()
	s.MonthlyLivingExpenses = 0
	c := newTestCalc(t, s)
	fp := c.findExpensesThreshold()
	if fp != nil {
		t.Fatalf("expected nil when expenses=0, got %+v", fp)
	}
}

func TestFindPortfolioThreshold_ZeroPortfolio(t *testing.T) {
	s := healthySettings()
	s.PortfolioValue = 0
	c := newTestCalc(t, s)
	fp := c.findPortfolioThreshold()
	if fp != nil {
		t.Fatalf("expected nil when portfolio=0, got %+v", fp)
	}
}

func TestFindReturnThreshold_AllocationMode(t *testing.T) {
	// InvestmentReturn==0 is the sentinel for allocation-based returns; the
	// binary search would override allocation with a flat rate and produce a
	// meaningless 0%/0%/0pts card, so the threshold must be omitted.
	s := healthySettings()
	s.InvestmentReturn = 0
	c := newTestCalc(t, s)
	fp := c.findReturnThreshold()
	if fp != nil {
		t.Fatalf("expected nil when InvestmentReturn=0 (allocation mode), got %+v", fp)
	}
}

func TestFindReturnThreshold(t *testing.T) {
	t.Run("returns failure point with correct fields", func(t *testing.T) {
		c := newTestCalc(t, healthySettings())
		fp := c.findReturnThreshold()
		if fp == nil {
			t.Fatal("expected non-nil failure point")
		}
		if fp.ParamName != "investment_return" {
			t.Errorf("expected param_name investment_return, got %s", fp.ParamName)
		}
		if fp.Direction != "below" {
			t.Errorf("expected direction below, got %s", fp.Direction)
		}
		if fp.CurrentValue != 7.0 {
			t.Errorf("expected current value 7.0, got %f", fp.CurrentValue)
		}
		if fp.Threshold > fp.CurrentValue {
			t.Errorf("threshold %f should be <= current value %f", fp.Threshold, fp.CurrentValue)
		}
	})
}

func TestFindInflationThreshold(t *testing.T) {
	t.Run("returns failure point with correct fields", func(t *testing.T) {
		c := newTestCalc(t, healthySettings())
		fp := c.findInflationThreshold()
		if fp == nil {
			t.Fatal("expected non-nil failure point")
		}
		if fp.ParamName != "inflation_rate" {
			t.Errorf("expected param_name inflation_rate, got %s", fp.ParamName)
		}
		if fp.Direction != "above" {
			t.Errorf("expected direction above, got %s", fp.Direction)
		}
		if fp.Threshold < fp.CurrentValue {
			t.Errorf("threshold %f should be >= current value %f", fp.Threshold, fp.CurrentValue)
		}
	})

	t.Run("binary search path with tight budget", func(t *testing.T) {
		// Settings where baseline survives but 15% inflation causes failure
		s := healthySettings()
		s.PortfolioValue = 500_000
		s.MonthlyLivingExpenses = 3500
		s.InvestmentReturn = 5.0
		s.InflationRate = 3.0
		s.ProjectionYears = 10
		c := newTestCalc(t, s)

		if !c.RunProjection().Survives {
			t.Skip("baseline doesn't survive, adjust settings")
		}

		fp := c.findInflationThreshold()
		if fp == nil {
			t.Fatal("expected non-nil failure point")
		}
		t.Logf("inflation threshold=%f, current=%f, safety=%s, margin=%f", fp.Threshold, fp.CurrentValue, fp.SafetyLevel, fp.Margin)
	})
}

func TestFindExpensesThreshold(t *testing.T) {
	t.Run("returns failure point with correct fields", func(t *testing.T) {
		c := newTestCalc(t, healthySettings())
		fp := c.findExpensesThreshold()
		if fp == nil {
			t.Fatal("expected non-nil failure point")
		}
		if fp.ParamName != "monthly_expenses" {
			t.Errorf("expected param_name monthly_expenses, got %s", fp.ParamName)
		}
		if fp.Direction != "above" {
			t.Errorf("expected direction above, got %s", fp.Direction)
		}
		if fp.Threshold < fp.CurrentValue {
			t.Errorf("threshold %f should be >= current value %f", fp.Threshold, fp.CurrentValue)
		}
	})

	t.Run("binary search path with tight budget", func(t *testing.T) {
		// Settings where baseline survives but 3x expenses causes failure
		s := healthySettings()
		s.PortfolioValue = 500_000
		s.MonthlyLivingExpenses = 3500
		s.InvestmentReturn = 5.0
		s.ProjectionYears = 10
		c := newTestCalc(t, s)

		if !c.RunProjection().Survives {
			t.Skip("baseline doesn't survive, adjust settings")
		}

		fp := c.findExpensesThreshold()
		if fp == nil {
			t.Fatal("expected non-nil failure point")
		}
		t.Logf("expenses threshold=%f, current=%f, safety=%s, margin=%f", fp.Threshold, fp.CurrentValue, fp.SafetyLevel, fp.Margin)
	})
}

func TestFindPortfolioThreshold(t *testing.T) {
	t.Run("returns failure point with correct fields", func(t *testing.T) {
		c := newTestCalc(t, healthySettings())
		fp := c.findPortfolioThreshold()
		if fp == nil {
			t.Fatal("expected non-nil failure point")
		}
		if fp.ParamName != "portfolio_value" {
			t.Errorf("expected param_name portfolio_value, got %s", fp.ParamName)
		}
		if fp.Direction != "below" {
			t.Errorf("expected direction below, got %s", fp.Direction)
		}
		if fp.Threshold > fp.CurrentValue {
			t.Errorf("threshold %f should be <= current value %f", fp.Threshold, fp.CurrentValue)
		}
	})
}

func TestFindInflationThreshold_SurvivesAt15Pct(t *testing.T) {
	// Very large portfolio with tiny expenses: survives even at 15% inflation
	s := healthySettings()
	s.PortfolioValue = 10_000_000
	s.MonthlyLivingExpenses = 100
	s.InvestmentReturn = 7.0
	s.InflationRate = 3.0
	s.ProjectionYears = 5
	c := newTestCalc(t, s)

	fp := c.findInflationThreshold()
	if fp == nil {
		t.Fatal("expected non-nil failure point")
	}
	if fp.Threshold != 15.0 {
		t.Errorf("expected threshold 15.0 (survives at ceiling), got %f", fp.Threshold)
	}
	if fp.SafetyLevel != "safe" {
		t.Errorf("expected safe, got %s", fp.SafetyLevel)
	}
}

func TestFindExpensesThreshold_SurvivesAt3x(t *testing.T) {
	// Very large portfolio: survives even at 3x expenses
	s := healthySettings()
	s.PortfolioValue = 10_000_000
	s.MonthlyLivingExpenses = 100
	s.InvestmentReturn = 7.0
	s.ProjectionYears = 5
	c := newTestCalc(t, s)

	fp := c.findExpensesThreshold()
	if fp == nil {
		t.Fatal("expected non-nil failure point")
	}
	// Should return 3x current as threshold with safe level
	if fp.Threshold != fp.CurrentValue*3 {
		t.Errorf("expected threshold %f (3x current), got %f", fp.CurrentValue*3, fp.Threshold)
	}
	if fp.SafetyLevel != "safe" {
		t.Errorf("expected safe, got %s", fp.SafetyLevel)
	}
}

func TestFindReturnThreshold_SurvivesAtNegative(t *testing.T) {
	// Very large portfolio with tiny expenses: survives even at -5% return
	s := healthySettings()
	s.PortfolioValue = 10_000_000
	s.MonthlyLivingExpenses = 100
	s.InvestmentReturn = 7.0
	s.ProjectionYears = 5
	c := newTestCalc(t, s)

	fp := c.findReturnThreshold()
	if fp == nil {
		t.Fatal("expected non-nil failure point")
	}
	if fp.Threshold != -5.0 {
		t.Errorf("expected threshold -5.0 (survives at floor), got %f", fp.Threshold)
	}
	if fp.SafetyLevel != "safe" {
		t.Errorf("expected safe, got %s", fp.SafetyLevel)
	}
}

func TestFindPortfolioThreshold_SurvivesAtZero(t *testing.T) {
	// Income covers all expenses, portfolio not needed
	s := healthySettings()
	s.PortfolioValue = 500_000
	s.MonthlyLivingExpenses = 1000
	s.InvestmentReturn = 5.0
	s.ProjectionYears = 5
	s.IncomeSources = []models.IncomeSource{
		{Name: "Pension", Amount: 5000, StartMonth: 0, COLARate: 0.03},
	}
	c := newTestCalc(t, s)

	fp := c.findPortfolioThreshold()
	if fp == nil {
		t.Fatal("expected non-nil failure point")
	}
	if fp.Threshold != 0 {
		t.Errorf("expected threshold 0 (survives at $0), got %f", fp.Threshold)
	}
	if fp.SafetyLevel != "safe" {
		t.Errorf("expected safe, got %s", fp.SafetyLevel)
	}
}

func TestSafetyLevels(t *testing.T) {
	t.Run("critical safety level for tight margin on return", func(t *testing.T) {
		// Low return with high expenses to create a tight margin
		s := healthySettings()
		s.InvestmentReturn = 3.0
		s.MonthlyLivingExpenses = 4000
		s.PortfolioValue = 500_000
		s.ProjectionYears = 8
		c := newTestCalc(t, s)

		// Verify baseline survives first
		proj := c.RunProjection()
		if !proj.Survives {
			t.Skip("baseline does not survive with these settings, adjusting test")
		}

		fp := c.findReturnThreshold()
		if fp == nil {
			t.Fatal("expected non-nil failure point")
		}
		// With a low return, the margin should be small
		if fp.Margin >= 2 && fp.SafetyLevel != "safe" {
			t.Errorf("margin=%f should yield safe, got %s", fp.Margin, fp.SafetyLevel)
		}
		if fp.Margin < 1 && fp.SafetyLevel != "critical" {
			t.Errorf("margin=%f should yield critical, got %s", fp.Margin, fp.SafetyLevel)
		}
		if fp.Margin >= 1 && fp.Margin < 2 && fp.SafetyLevel != "marginal" {
			t.Errorf("margin=%f should yield marginal, got %s", fp.Margin, fp.SafetyLevel)
		}
	})

	t.Run("safe level for very healthy portfolio", func(t *testing.T) {
		s := healthySettings()
		s.PortfolioValue = 5_000_000
		s.MonthlyLivingExpenses = 1000
		c := newTestCalc(t, s)

		fp := c.findReturnThreshold()
		if fp == nil {
			t.Fatal("expected non-nil failure point")
		}
		if fp.SafetyLevel != "safe" {
			t.Errorf("expected safe level for very healthy portfolio, got %s (margin=%f)", fp.SafetyLevel, fp.Margin)
		}
	})
}

func TestRunFullAnalysis(t *testing.T) {
	s := healthySettings()
	c := newTestCalc(t, s)
	result := c.RunFullAnalysis()

	if result == nil {
		t.Fatal("expected non-nil analysis result")
	}
	if result.Settings == nil {
		t.Error("expected non-nil settings")
	}
	if result.Projection == nil {
		t.Error("expected non-nil projection")
	}
	if result.BudgetFit == nil {
		t.Error("expected non-nil budget fit")
	}
	if result.PresentValue == nil {
		t.Error("expected non-nil present value")
	}
	if result.Sustainability == nil {
		t.Error("expected non-nil sustainability")
	}
	if result.Sensitivity == nil {
		t.Error("expected non-nil sensitivity")
	}
	if result.FailurePoints == nil {
		t.Error("expected non-nil failure points")
	}
	if result.MonteCarlo == nil {
		t.Error("expected non-nil monte carlo")
	}
	if result.RMD == nil {
		t.Error("expected non-nil RMD")
	}
}
