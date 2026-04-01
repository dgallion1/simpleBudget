package models

import "testing"

func TestDefaultUserSettings(t *testing.T) {
	s := DefaultUserSettings()

	if s == nil {
		t.Fatal("DefaultUserSettings returned nil")
	}
	if s.CurrentSavings != 500000 {
		t.Errorf("CurrentSavings = %f, want 500000", s.CurrentSavings)
	}
	if s.MonthlyExpenses != 5000 {
		t.Errorf("MonthlyExpenses = %f, want 5000", s.MonthlyExpenses)
	}
	if s.WithdrawalRate != 4.0 {
		t.Errorf("WithdrawalRate = %f, want 4.0", s.WithdrawalRate)
	}
	if s.InflationRate != 3.0 {
		t.Errorf("InflationRate = %f, want 3.0", s.InflationRate)
	}
	if s.InvestmentReturn != 6.0 {
		t.Errorf("InvestmentReturn = %f, want 6.0", s.InvestmentReturn)
	}
	if s.ProjectionYears != 30 {
		t.Errorf("ProjectionYears = %d, want 30", s.ProjectionYears)
	}
	if s.DefaultDateRange != "ytd" {
		t.Errorf("DefaultDateRange = %q, want ytd", s.DefaultDateRange)
	}
	if s.Budgets == nil {
		t.Error("Budgets should not be nil")
	}
	if s.IncomeSources == nil {
		t.Error("IncomeSources should not be nil")
	}
	if s.ExpenseSources == nil {
		t.Error("ExpenseSources should not be nil")
	}
	if s.SavingsGoals == nil {
		t.Error("SavingsGoals should not be nil")
	}
	if s.EnabledFiles == nil {
		t.Error("EnabledFiles should not be nil")
	}
}
