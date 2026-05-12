package retirement

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
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
	// TaxOptimizer is no longer populated by RunFull; it is wired to the
	// explicit /api/whatif/tax-optimize endpoint via RunTaxOptimizer.
	if result.TaxOptimizer != nil {
		t.Error("RunFull should not populate TaxOptimizer; use RunTaxOptimizer")
	}
}

// TestRunTaxOptimizer_EligibleReturnsResult verifies that RunTaxOptimizer
// returns a non-nil, eligible result for a well-formed scenario.
func TestRunTaxOptimizer_EligibleReturnsResult(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 2_000_000
	s.MonthlyLivingExpenses = 5000
	s.InvestmentReturn = 7.0
	s.InflationRate = 3.0
	s.ProjectionYears = 25
	s.CurrentAge = 65
	s.TaxDeferredPercent = 80
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingMarriedJoint}
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 3500, FRA: 67, ClaimAge: 67,
		SpouseFRABenefit: 1500, SpouseFRA: 67, SpouseClaimAge: 62,
		COLARate: 0.02, COLARateSet: true,
	}

	prep, err := prepare.From(s)
	if err != nil {
		t.Fatalf("prepare.From: %v", err)
	}
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	result := RunTaxOptimizer(eng, in)
	if result == nil {
		t.Fatal("RunTaxOptimizer returned nil")
	}
	if !result.Eligible {
		t.Errorf("expected Eligible=true, got false (reason: %s)", result.IneligibleReason)
	}
	if len(result.Top) == 0 {
		t.Error("expected non-empty Top candidates")
	}
}
