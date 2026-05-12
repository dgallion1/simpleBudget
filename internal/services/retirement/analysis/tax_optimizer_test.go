package analysis

import (
	"testing"

	"budget2/internal/models"
)

// eligibleBase returns a fully-valid WhatIfSettings that passes all
// eligibility checks. Tests mutate a copy of this to exercise each
// rejection rule without triggering unrelated validation failures.
func eligibleBase() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 67
	s.ProjectionYears = 31
	s.PortfolioValue = 2_000_000
	s.TaxDeferredPercent = 80
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingMarriedJoint}
	return s
}

func TestTaxOptimizerEligible_HappyPath(t *testing.T) {
	s := eligibleBase()
	ok, reason := taxOptimizerEligible(s)
	if !ok {
		t.Errorf("expected eligible, got reason=%q", reason)
	}
}

func TestTaxOptimizerEligible_Rejections(t *testing.T) {
	base := eligibleBase

	cases := []struct {
		name   string
		mutate func(*models.WhatIfSettings)
	}{
		{"no_tax_config", func(s *models.WhatIfSettings) { s.TaxConfig = nil }},
		{"empty_filing_status", func(s *models.WhatIfSettings) { s.TaxConfig.FilingStatus = "" }},
		{"tax_deferred_too_small", func(s *models.WhatIfSettings) {
			s.PortfolioValue = 100_000
			s.TaxDeferredPercent = 50 // → $50k tax-deferred
		}},
		{"post_rmd_age", func(s *models.WhatIfSettings) { s.CurrentAge = 73 }},
		{"projection_too_short", func(s *models.WhatIfSettings) { s.ProjectionYears = 4 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := base()
			c.mutate(s)
			ok, reason := taxOptimizerEligible(s)
			if ok {
				t.Errorf("expected ineligible, got ok=true")
			}
			if reason == "" {
				t.Errorf("expected non-empty reason")
			}
		})
	}
}

func TestTaxOptimizerEligible_Boundaries(t *testing.T) {
	// age 72 = eligible, age 73 = not eligible
	s72 := eligibleBase()
	s72.CurrentAge = 72
	s72.ProjectionYears = 10
	s72.PortfolioValue = 1_000_000
	s72.TaxDeferredPercent = 50
	s72.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	if ok, _ := taxOptimizerEligible(s72); !ok {
		t.Error("age 72 should be eligible")
	}
	s73 := *s72
	s73.CurrentAge = 73
	if ok, _ := taxOptimizerEligible(&s73); ok {
		t.Error("age 73 should be ineligible")
	}

	// tax-deferred exactly $100k = eligible; $99,999.50 = not eligible
	s100k := eligibleBase()
	s100k.CurrentAge = 60
	s100k.ProjectionYears = 30
	s100k.PortfolioValue = 200_000
	s100k.TaxDeferredPercent = 50
	s100k.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	if ok, _ := taxOptimizerEligible(s100k); !ok {
		t.Error("$100k tax-deferred should be eligible")
	}
	sBelow := *s100k
	sBelow.PortfolioValue = 199_999
	if ok, _ := taxOptimizerEligible(&sBelow); ok {
		t.Error("$99,999.50 tax-deferred should be ineligible")
	}
}

func TestTaxOptimizerConstants_Sanity(t *testing.T) {
	// Guard rail: MC budget must be small enough that 5 finalists × runs
	// stays well below 1000 total MC invocations per optimizer call.
	const maxAcceptableMCRuns = 200
	if taxOptimizerMonteCarloRuns > maxAcceptableMCRuns {
		t.Errorf("taxOptimizerMonteCarloRuns=%d exceeds budget ceiling %d",
			taxOptimizerMonteCarloRuns, maxAcceptableMCRuns)
	}
}

func TestTaxOptimizerEligible_NilSettings(t *testing.T) {
	ok, reason := taxOptimizerEligible(nil)
	if ok {
		t.Error("nil settings should be ineligible")
	}
	if reason == "" {
		t.Error("nil settings should produce a non-empty reason")
	}
}

func TestCloneSettingsWithSSAndRoth_PreservesBracketFillOverrides(t *testing.T) {
	// Regression test: PerYearOverrides has json:"-" tag, and
	// prepare.From uses JSON DeepCopy. Without manual re-attachment
	// the map would be dropped, causing bracket-fill candidates to
	// score identically to the no-conversion baseline.
	s := eligibleBase()
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 3000, FRA: 67, ClaimAge: 70, SpouseClaimAge: 67,
	}
	strat := models.RothOptimizerStrategy{
		Kind:          models.RothStrategyBracketFill,
		TargetBracket: 0.22,
		StartAge:      s.CurrentAge,
		EndAge:        s.CurrentAge + 5,
	}
	prepared, ok := cloneSettingsWithSSAndRoth(s, s.SocialSecurity.ClaimAge, s.SocialSecurity.SpouseClaimAge, strat)
	if !ok {
		t.Fatal("expected clone to succeed")
	}
	cloned := prepared.Settings()
	if cloned == nil || cloned.RothConversion == nil {
		t.Fatal("expected non-nil cloned RothConversion")
	}
	if cloned.RothConversion.PerYearOverrides == nil {
		t.Fatal("PerYearOverrides was dropped during clone (json tag bug)")
	}
	// 5-year window (currentAge → currentAge+5) → 5 entries (projection years 0..4).
	if got, want := len(cloned.RothConversion.PerYearOverrides), 5; got != want {
		t.Errorf("PerYearOverrides entries: got %d, want %d", got, want)
	}
	// Each per-year override must be non-negative.
	for year, amount := range cloned.RothConversion.PerYearOverrides {
		if amount < 0 {
			t.Errorf("year %d: negative override %v", year, amount)
		}
	}
}

func TestCloneSettingsWithSSAndRoth_AppliesOverrides(t *testing.T) {
	s := eligibleBase()
	s.SpouseAge = 54
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 3000, FRA: 67, ClaimAge: 67, SpouseClaimAge: 62,
	}
	s.RothConversion = &models.RothConversionConfig{
		Enabled: true, AnnualAmount: 50_000, StartYear: 0, EndYear: 10,
	}
	strat := models.RothOptimizerStrategy{
		Kind:         models.RothStrategyLadder,
		AnnualAmount: 100_000,
		StartAge:     67,
		EndAge:       73,
	}

	prepared, ok := cloneSettingsWithSSAndRoth(s, 70, 67, strat)
	if !ok {
		t.Fatal("expected clone to succeed")
	}

	cloned := prepared.Settings()
	if cloned.SocialSecurity.ClaimAge != 70 {
		t.Errorf("primary claim age: got %d, want 70", cloned.SocialSecurity.ClaimAge)
	}
	if cloned.SocialSecurity.SpouseClaimAge != 67 {
		t.Errorf("spouse claim age: got %d, want 67", cloned.SocialSecurity.SpouseClaimAge)
	}
	if cloned.RothConversion.AnnualAmount != 100_000 {
		t.Errorf("Roth amount: got %v, want 100000", cloned.RothConversion.AnnualAmount)
	}

	// Original must be unchanged.
	if s.SocialSecurity.ClaimAge != 67 {
		t.Error("original ClaimAge mutated")
	}
	if s.RothConversion.AnnualAmount != 50_000 {
		t.Error("original RothConversion mutated")
	}
}
