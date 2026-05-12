package analysis_test

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

func TestRunTaxOptimizer_EligibleReturnsResult(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.ScenarioName = "test"
	s.CurrentAge = 67
	s.SpouseAge = 54
	s.ProjectionYears = 20
	s.PortfolioValue = 2_000_000
	s.TaxDeferredPercent = 80
	s.InvestmentReturn = 6
	s.InflationRate = 3
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingMarriedJoint}
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 4100, FRA: 67, SpouseFRABenefit: 1500, SpouseFRA: 67,
		ClaimAge: 67, SpouseClaimAge: 62, COLARate: 0.02, COLARateSet: true,
	}
	s.RothConversion = &models.RothConversionConfig{
		Enabled: true, AnnualAmount: 50_000, StartYear: 0,
	}

	prep, err := prepare.From(s)
	if err != nil {
		t.Fatalf("prepare.From: %v", err)
	}
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	result := retirement.RunTaxOptimizer(eng, in)
	if result == nil {
		t.Fatal("RunTaxOptimizer returned nil")
	}
	if !result.Eligible {
		t.Errorf("expected eligible; reason=%q", result.IneligibleReason)
	}
}

func TestRunTaxOptimizer_IneligibleStillNonNil(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	// Use age 75 (post-RMD) to trigger ineligibility. Must set the primary
	// person's BirthMonth so prepare.From derives CurrentAge=75; setting
	// s.CurrentAge directly is overridden by ComputeAges.
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 75)
	s.ProjectionYears = 10
	s.PortfolioValue = 2_000_000
	s.TaxDeferredPercent = 80
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 3000, FRA: 67, ClaimAge: 67, COLARate: 0.02, COLARateSet: true,
	}

	prep, err := prepare.From(s)
	if err != nil {
		t.Fatalf("prepare.From: %v", err)
	}
	in := engine.Input{Prepared: prep}

	result := retirement.RunTaxOptimizer(engine.New(), in)
	if result == nil {
		t.Fatal("expected non-nil result even when ineligible")
	}
	if result.Eligible {
		t.Error("expected Eligible=false for age 75")
	}
	if result.IneligibleReason == "" {
		t.Error("expected non-empty IneligibleReason")
	}
}
