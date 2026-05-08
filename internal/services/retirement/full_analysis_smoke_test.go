package retirement

import (
	"testing"

	"budget2/internal/models"
)

// healthySettingsForFullAnalysis mirrors the test fixture previously
// hosted in calculator_failure_test.go before that file was migrated to
// the analysis package. Used only by TestRunFullAnalysis here.
func healthySettingsForFullAnalysis() *models.WhatIfSettings {
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

// TestRunFullAnalysis checks that RunFullAnalysis populates every major
// sub-analysis pointer, since the orchestrator on Calculator wires up
// settings, projection, budget fit, present value, sustainability,
// sensitivity, failure points, Monte Carlo, and RMD.
func TestRunFullAnalysis(t *testing.T) {
	s := healthySettingsForFullAnalysis()
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
