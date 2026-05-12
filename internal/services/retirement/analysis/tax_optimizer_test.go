package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
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

func TestScoreCandidate_PopulatesFields(t *testing.T) {
	s := eligibleBase()
	prep, ok := cloneSettingsWithSSAndRoth(s, 67, 62, models.RothOptimizerStrategy{
		Kind: models.RothStrategyLadder, AnnualAmount: 50_000, StartAge: 67, EndAge: 73,
	})
	if !ok {
		t.Fatal("clone failed")
	}
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	cand := scoreCandidate(eng, in, 67, 62, models.RothOptimizerStrategy{
		Kind: models.RothStrategyLadder, AnnualAmount: 50_000, StartAge: 67, EndAge: 73,
	})

	if cand.PrimaryClaimAge != 67 {
		t.Errorf("PrimaryClaimAge: got %d", cand.PrimaryClaimAge)
	}
	if cand.SpouseClaimAge != 62 {
		t.Errorf("SpouseClaimAge: got %d", cand.SpouseClaimAge)
	}
	if cand.EndingPortfolioReal <= 0 {
		t.Errorf("EndingPortfolioReal not populated: %v", cand.EndingPortfolioReal)
	}
	if cand.LifetimeTaxReal < 0 {
		t.Errorf("LifetimeTaxReal negative: %v", cand.LifetimeTaxReal)
	}
}

func TestProjectionToCandidate_NilProjection(t *testing.T) {
	cand := projectionToCandidate(nil, 67, 62, models.RothOptimizerStrategy{
		Kind: models.RothStrategyLadder, Label: "test",
	})
	if cand.PrimaryClaimAge != 67 {
		t.Errorf("PrimaryClaimAge not set: got %d", cand.PrimaryClaimAge)
	}
	if cand.EndingPortfolioReal != -math.MaxFloat64 {
		t.Errorf("nil projection should yield sentinel -MaxFloat64, got %v", cand.EndingPortfolioReal)
	}
}

func TestProjectionToCandidate_EmptyYearlySummaries(t *testing.T) {
	proj := &models.ProjectionResult{YearlySummaries: nil}
	cand := projectionToCandidate(proj, 67, 62, models.RothOptimizerStrategy{})
	if cand.EndingPortfolioReal != -math.MaxFloat64 {
		t.Errorf("empty yearly summaries should yield sentinel, got %v", cand.EndingPortfolioReal)
	}
}

func TestProjectionToCandidate_NaNCoerced(t *testing.T) {
	proj := &models.ProjectionResult{
		YearlySummaries: []models.ProjectionYearSummary{
			{EndingBalanceReal: math.NaN()},
		},
	}
	cand := projectionToCandidate(proj, 67, 62, models.RothOptimizerStrategy{})
	if cand.EndingPortfolioReal != -math.MaxFloat64 {
		t.Errorf("NaN should be coerced to sentinel, got %v", cand.EndingPortfolioReal)
	}
}

func TestTopKSSPairs_ExtractsBestPairs(t *testing.T) {
	ss := &models.SSPortfolioAnalysis{
		PrimaryOptions: []models.SSPortfolioOption{
			{ClaimAge: 67, SurvivalRate: 0.85},
			{ClaimAge: 70, SurvivalRate: 0.90}, // best
			{ClaimAge: 65, SurvivalRate: 0.80},
		},
		SpouseOptions: []models.SSPortfolioOption{
			{ClaimAge: 62, SurvivalRate: 0.88},
			{ClaimAge: 67, SurvivalRate: 0.91}, // best
		},
		OptimalPrimaryAge: 70,
		OptimalSpouseAge:  67,
	}
	currentPrimary, currentSpouse := 67, 62

	pairs := topKSSPairs(ss, currentPrimary, currentSpouse, 3)
	if len(pairs) == 0 {
		t.Fatal("expected at least one pair")
	}
	// Joint optimum (70, 67) must appear.
	foundOptimum := false
	for _, p := range pairs {
		if p.Primary == 70 && p.Spouse == 67 {
			foundOptimum = true
		}
	}
	if !foundOptimum {
		t.Error("expected (70, 67) joint optimum pair in result")
	}
	if len(pairs) > 3 {
		t.Errorf("expected ≤3 pairs, got %d", len(pairs))
	}
}

func TestTopKSSPairs_NilFallsBackToCurrent(t *testing.T) {
	pairs := topKSSPairs(nil, 67, 62, 3)
	if len(pairs) != 1 {
		t.Fatalf("expected single-pair fallback, got %d", len(pairs))
	}
	if pairs[0].Primary != 67 || pairs[0].Spouse != 62 {
		t.Errorf("fallback pair: got (%d, %d), want (67, 62)", pairs[0].Primary, pairs[0].Spouse)
	}
}

func TestTopKSSPairs_EmptyOptionsFallsBack(t *testing.T) {
	ss := &models.SSPortfolioAnalysis{} // no options
	pairs := topKSSPairs(ss, 67, 62, 3)
	if len(pairs) != 1 {
		t.Fatalf("expected single-pair fallback for empty options, got %d", len(pairs))
	}
	if pairs[0].Primary != 67 || pairs[0].Spouse != 62 {
		t.Errorf("fallback pair: got (%d, %d), want (67, 62)", pairs[0].Primary, pairs[0].Spouse)
	}
}

func TestProjectionToCandidate_LifetimeTaxIsInflationAdjusted(t *testing.T) {
	// Two-year projection: $10k tax in year 0, $10k tax in year 1.
	// CumulativeInflation = 1.0 at year 0, 1.05 at year 1 (5% inflation).
	// Real lifetime tax = 10000/1.0 + 10000/1.05 ≈ 19,524.
	// If we summed nominal, we'd get 20,000.
	proj := &models.ProjectionResult{
		YearlySummaries: []models.ProjectionYearSummary{
			{Taxes: 10_000, CumulativeInflation: 1.0, EndingBalanceReal: 1_000_000},
			{Taxes: 10_000, CumulativeInflation: 1.05, EndingBalanceReal: 1_000_000},
		},
	}
	cand := projectionToCandidate(proj, 67, 62, models.RothOptimizerStrategy{})
	if cand.LifetimeTaxReal < 19_000 || cand.LifetimeTaxReal > 19_700 {
		t.Errorf("LifetimeTaxReal: got %v, expected ~19,524 (real) not 20,000 (nominal)", cand.LifetimeTaxReal)
	}
}

func TestTopKSSPairs_ShortListsStillReachK(t *testing.T) {
	// Single-option per axis + matching optimum: previously returned only 1 pair
	// even when k=3, because the zip loop terminated immediately and the
	// current-settings fallback was unreachable.
	ss := &models.SSPortfolioAnalysis{
		PrimaryOptions:    []models.SSPortfolioOption{{ClaimAge: 70, SurvivalRate: 0.90}},
		SpouseOptions:     []models.SSPortfolioOption{{ClaimAge: 67, SurvivalRate: 0.91}},
		OptimalPrimaryAge: 70,
		OptimalSpouseAge:  67,
	}
	pairs := topKSSPairs(ss, 67, 62, 3)
	if len(pairs) < 2 {
		t.Errorf("expected at least 2 pairs (joint optimum + current fallback) when k=3, got %d", len(pairs))
	}
	// The joint optimum must be present.
	foundOptimum := false
	foundCurrent := false
	for _, p := range pairs {
		if p.Primary == 70 && p.Spouse == 67 {
			foundOptimum = true
		}
		if p.Primary == 67 && p.Spouse == 62 {
			foundCurrent = true
		}
	}
	if !foundOptimum {
		t.Error("joint optimum (70, 67) missing")
	}
	if !foundCurrent {
		t.Error("current-settings fallback (67, 62) missing despite k=3")
	}
}
