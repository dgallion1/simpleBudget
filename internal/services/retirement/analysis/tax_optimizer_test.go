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
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 4100, FRA: 67, ClaimAge: 67,
		SpouseFRABenefit: 1500, SpouseFRA: 67, SpouseClaimAge: 62,
		COLARate: 0.02, COLARateSet: true,
	}
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
		{"no_ss_config", func(s *models.WhatIfSettings) { s.SocialSecurity = nil }},
		{"ss_claim_age_zero", func(s *models.WhatIfSettings) { s.SocialSecurity.ClaimAge = 0 }},
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

func TestScoreCandidate_PopulatesPerYearConversions(t *testing.T) {
	// NOTE on age handling: cloneSettingsWithSSAndRoth → perturbAndPrepare →
	// prepare.From runs ComputeAges, which DERIVES CurrentAge from
	// StartDate + the primary Person's BirthMonth. This silently overrides
	// any in-memory s.CurrentAge mutation. Read the post-prep CurrentAge
	// from prep.Settings() instead of relying on eligibleBase()'s value.
	s := eligibleBase()

	prep, ok := cloneSettingsWithSSAndRoth(s, 67, 62, models.RothOptimizerStrategy{
		Kind: models.RothStrategyLadder, AnnualAmount: 50_000, StartAge: 67, EndAge: 73,
	})
	if !ok {
		t.Fatal("clone failed")
	}
	currentAge := prep.Settings().CurrentAge
	if currentAge <= 0 {
		t.Fatalf("prepared CurrentAge unset: %d", currentAge)
	}

	// 5-year ladder window starting at the post-prep CurrentAge so no
	// startProjYear clamping occurs.
	strat := models.RothOptimizerStrategy{
		Kind:         models.RothStrategyLadder,
		AnnualAmount: 60_000,
		StartAge:     currentAge,
		EndAge:       currentAge + 5, // exclusive — 5 entries
	}

	in := engine.Input{Prepared: prep}
	eng := engine.New()

	cand := scoreCandidate(eng, in, 67, 62, strat)

	if len(cand.PerYearConversions) != 5 {
		t.Fatalf("PerYearConversions: got %d entries, want 5", len(cand.PerYearConversions))
	}
	for i, yc := range cand.PerYearConversions {
		if want := currentAge + i; yc.Age != want {
			t.Errorf("entry %d: Age=%d, want %d", i, yc.Age, want)
		}
		if yc.Amount != 60_000 {
			t.Errorf("entry %d: Amount=%v, want 60000", i, yc.Amount)
		}
	}
}

func TestScoreCandidate_NoneStrategyHasEmptyPerYearConversions(t *testing.T) {
	s := eligibleBase()
	prep, ok := cloneSettingsWithSSAndRoth(s, 67, 62, models.RothOptimizerStrategy{Kind: models.RothStrategyNone})
	if !ok {
		t.Fatal("clone failed")
	}
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	cand := scoreCandidate(eng, in, 67, 62, models.RothOptimizerStrategy{Kind: models.RothStrategyNone})

	if cand.PerYearConversions != nil {
		t.Errorf("expected nil PerYearConversions for none strategy, got %v", cand.PerYearConversions)
	}
}

// TestCloneSettingsWithSSAndRoth_BracketFillUsesCandidateSSAges pins the
// fix for the cross-binding bug: bracket-fill PerYearOverrides must be
// computed against the CANDIDATE SS claim ages, not the saved ones.
// Otherwise the optimizer scores "claim at 70 + fill 22% bracket" using
// conversion amounts shrunk by SS income from the saved (earlier) claim,
// biasing ranking.
func TestCloneSettingsWithSSAndRoth_BracketFillUsesCandidateSSAges(t *testing.T) {
	s := eligibleBase()
	// Single filer for a tight test: no spouse SS contribution to compare.
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.SpouseAge = 0
	// A pension lifts provisional income above the §86 threshold so the
	// presence/absence of SS actually changes TAXABLE income — otherwise
	// modest SS alone is non-taxable and claim age wouldn't move the
	// bracket-fill room.
	s.IncomeSources = []models.IncomeSource{
		{Type: models.IncomeFixed, Name: "Pension", Amount: 4000, StartMonth: 0},
	}
	// Saved claim age = 62 means SS income is in the bracket-fill years.
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 4000, FRA: 67, ClaimAge: 62,
		COLARate: 0.02, COLARateSet: true,
	}

	strat := models.RothOptimizerStrategy{
		Kind:          models.RothStrategyBracketFill,
		TargetBracket: 0.22,
		StartAge:      s.CurrentAge,
		EndAge:        s.CurrentAge + 3, // ages 67..69 — all pre-candidate-claim
	}

	// Expected: amounts derived under candidate SS (claim 70 → no SS in
	// projection years 0..2 since age 67..69 < 70).
	candidateSettings := *s
	ssCopy := *s.SocialSecurity
	ssCopy.ClaimAge = 70
	candidateSettings.SocialSecurity = &ssCopy
	wantCfg := rothStrategyToConfig(&candidateSettings, strat)
	if wantCfg == nil || wantCfg.PerYearOverrides == nil {
		t.Fatalf("setup: candidate-SS bracket-fill produced no overrides; cfg=%+v", wantCfg)
	}
	want := wantCfg.PerYearOverrides

	// Sanity: under saved SS the overrides MUST differ, otherwise the
	// test scenario isn't actually exercising the bug surface.
	savedCfg := rothStrategyToConfig(s, strat)
	if savedCfg == nil || savedCfg.PerYearOverrides == nil {
		t.Fatalf("setup: saved-SS bracket-fill produced no overrides; cfg=%+v", savedCfg)
	}
	differs := false
	for y, savedAmt := range savedCfg.PerYearOverrides {
		if math.Abs(savedAmt-want[y]) > 1 {
			differs = true
			break
		}
	}
	if !differs {
		t.Fatalf("test scenario insufficient: saved and candidate amounts identical (saved=%v want=%v)",
			savedCfg.PerYearOverrides, want)
	}

	// Exercise the bug path.
	prepared, ok := cloneSettingsWithSSAndRoth(s, 70, 0, strat)
	if !ok {
		t.Fatal("clone failed")
	}
	cloned := prepared.Settings()
	if cloned == nil || cloned.RothConversion == nil {
		t.Fatal("cloned settings or RothConversion is nil")
	}
	got := cloned.RothConversion.PerYearOverrides
	if got == nil {
		t.Fatal("PerYearOverrides was dropped")
	}
	if len(got) != len(want) {
		t.Fatalf("override count: got %d, want %d", len(got), len(want))
	}
	for y, wantAmt := range want {
		gotAmt, ok := got[y]
		if !ok {
			t.Errorf("year %d missing from PerYearOverrides; got map=%v", y, got)
			continue
		}
		if math.Abs(gotAmt-wantAmt) > 1 {
			t.Errorf("year %d: PerYearOverrides=%.2f, want %.2f (saved-SS amount was %.2f — fix not applied)",
				y, gotAmt, wantAmt, savedCfg.PerYearOverrides[y])
		}
	}
}

// TestScoreCandidate_DisclosureUsesCandidateSSAges verifies that
// cand.PerYearConversions (the per-year disclosure shown in the UI) is
// computed from the CANDIDATE SS claim ages, not the saved ones. If
// scoreCandidate uses the saved settings, users see amounts that don't
// match what the engine actually applied — and that mismatch is a tell
// that the engine input is also wrong (twin bug at line 73 of
// tax_optimizer.go).
func TestScoreCandidate_DisclosureUsesCandidateSSAges(t *testing.T) {
	s := eligibleBase()
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.SpouseAge = 0
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 4000, FRA: 67, ClaimAge: 62,
		COLARate: 0.02, COLARateSet: true,
	}

	strat := models.RothOptimizerStrategy{
		Kind:          models.RothStrategyBracketFill,
		TargetBracket: 0.22,
		StartAge:      s.CurrentAge,
		EndAge:        s.CurrentAge + 3,
	}

	// Build the engine.Input around the SAVED settings (this mirrors what
	// the optimizer does: scoreCandidate is called with the user's input,
	// not the candidate's).
	prep, ok := cloneSettingsWithSSAndRoth(s, s.SocialSecurity.ClaimAge, 0, models.RothOptimizerStrategy{Kind: models.RothStrategyNone})
	if !ok {
		t.Fatal("baseline clone failed")
	}
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	cand := scoreCandidate(eng, in, 70, 0, strat)
	if len(cand.PerYearConversions) == 0 {
		t.Fatalf("disclosure is empty; cand=%+v", cand)
	}

	// Compute the expected amounts directly from a candidate-SS-applied
	// settings. The disclosure must match these — not the saved-SS values.
	candidateSettings := *prep.Settings()
	if candidateSettings.SocialSecurity != nil {
		ssCopy := *candidateSettings.SocialSecurity
		ssCopy.ClaimAge = 70
		ssCopy.SpouseClaimAge = 0
		candidateSettings.SocialSecurity = &ssCopy
	}
	wantSeq := strategyYearlyConversions(&candidateSettings, strat)
	// Saved-SS sequence for the comparison failure message.
	savedSeq := strategyYearlyConversions(prep.Settings(), strat)

	if len(cand.PerYearConversions) != len(wantSeq) {
		t.Fatalf("disclosure length: got %d, want %d", len(cand.PerYearConversions), len(wantSeq))
	}
	for i, want := range wantSeq {
		got := cand.PerYearConversions[i]
		if got.Age != want.Age {
			t.Errorf("entry %d: Age=%d, want %d", i, got.Age, want.Age)
		}
		if math.Abs(got.Amount-want.Amount) > 1 {
			savedAmt := 0.0
			if i < len(savedSeq) {
				savedAmt = savedSeq[i].Amount
			}
			t.Errorf("entry %d (Age %d): disclosure Amount=%.2f, want %.2f (saved-SS would be %.2f — fix not applied)",
				i, got.Age, got.Amount, want.Amount, savedAmt)
		}
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

func TestTaxOptimizer_IneligibleReturnsReason(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 67
	s.ProjectionYears = 31
	s.PortfolioValue = 50_000        // 60% tax-deferred → $30k, below $100k threshold
	s.TaxDeferredPercent = 60        // explicit for clarity
	s.TaxConfig = nil                // also triggers ineligibility (belt-and-suspenders)
	prep := perturbAndPrepare(s)
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	result := TaxOptimizerWithSeed(eng, in, nil, 42)
	if result == nil {
		t.Fatal("expected non-nil result for ineligible scenario")
	}
	if result.Eligible {
		t.Error("expected Eligible=false")
	}
	if result.IneligibleReason == "" {
		t.Error("expected non-empty IneligibleReason")
	}
}

func TestTaxOptimizer_EligibleProducesTop5(t *testing.T) {
	s := eligibleBase()
	prep := perturbAndPrepare(s)
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	result := TaxOptimizerWithSeed(eng, in, nil, 42)
	if result == nil || !result.Eligible {
		t.Fatalf("expected eligible result, got %+v", result)
	}
	if len(result.Top) == 0 {
		t.Fatal("expected non-empty Top")
	}
	if len(result.Top) > taxOptimizerTopFinalists {
		t.Errorf("Top length %d exceeds finalists cap %d", len(result.Top), taxOptimizerTopFinalists)
	}
	// Top sorted desc by MCMedianEndingReal after MC refinement.
	for i := 1; i < len(result.Top); i++ {
		if result.Top[i].MCMedianEndingReal > result.Top[i-1].MCMedianEndingReal {
			t.Errorf("Top not sorted desc by MCMedianEndingReal: index %d > %d", i, i-1)
		}
	}
	if result.Best.MCMedianEndingReal != result.Top[0].MCMedianEndingReal {
		t.Error("Best should match Top[0] by MCMedianEndingReal")
	}
	if result.CandidatesScored == 0 {
		t.Error("CandidatesScored should be > 0")
	}
}

func TestTaxOptimizer_BaselineMatchesCurrent(t *testing.T) {
	s := eligibleBase()
	// Give the scenario a SS config so baseline claim-age assertions are meaningful.
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 3000, FRA: 67, ClaimAge: 70, SpouseClaimAge: 67,
	}
	prep := perturbAndPrepare(s)
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	result := TaxOptimizerWithSeed(eng, in, nil, 42)
	if result == nil || !result.Eligible {
		t.Fatal("expected eligible")
	}
	if result.Baseline.PrimaryClaimAge != s.SocialSecurity.ClaimAge {
		t.Errorf("Baseline.PrimaryClaimAge: got %d, want %d", result.Baseline.PrimaryClaimAge, s.SocialSecurity.ClaimAge)
	}
	if result.Baseline.SpouseClaimAge != s.SocialSecurity.SpouseClaimAge {
		t.Errorf("Baseline.SpouseClaimAge: got %d, want %d", result.Baseline.SpouseClaimAge, s.SocialSecurity.SpouseClaimAge)
	}
}

func TestTaxOptimizer_Deterministic(t *testing.T) {
	// Phase 1: seed is unused, so this test verifies determinism of
	// the deterministic projection layer only. Becomes the load-bearing
	// MC reproducibility guard once Task 9 wires the MC refinement.
	s := eligibleBase()
	prep := perturbAndPrepare(s)
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	r1 := TaxOptimizerWithSeed(eng, in, nil, 42)
	r2 := TaxOptimizerWithSeed(eng, in, nil, 42)
	if r1 == nil || r2 == nil {
		t.Fatal("expected both results non-nil")
	}
	if len(r1.Top) != len(r2.Top) {
		t.Fatalf("top length mismatch: %d vs %d", len(r1.Top), len(r2.Top))
	}
	for i := range r1.Top {
		if r1.Top[i].PrimaryClaimAge != r2.Top[i].PrimaryClaimAge ||
			r1.Top[i].SpouseClaimAge != r2.Top[i].SpouseClaimAge ||
			r1.Top[i].RothStrategy.Label != r2.Top[i].RothStrategy.Label {
			t.Errorf("non-deterministic at index %d: r1=%+v r2=%+v", i, r1.Top[i], r2.Top[i])
		}
	}
}

func TestCurrentRothStrategyFor_RoundTripsLadder(t *testing.T) {
	s := eligibleBase()
	s.CurrentAge = 67
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 50_000,
		StartYear:    0,
		EndYear:      5, // engine reads inclusive: stops when currentYear > 5
	}
	strat := currentRothStrategyFor(s)
	// Round-trip through rothStrategyToConfig should produce the same
	// engine EndYear=5.
	cfg := rothStrategyToConfig(s, strat)
	if cfg == nil || !cfg.Enabled {
		t.Fatal("expected enabled round-trip config")
	}
	if cfg.StartYear != 0 {
		t.Errorf("StartYear round-trip: got %d, want 0", cfg.StartYear)
	}
	if cfg.EndYear != 5 {
		t.Errorf("EndYear round-trip: got %d, want 5", cfg.EndYear)
	}
	if cfg.AnnualAmount != 50_000 {
		t.Errorf("AnnualAmount round-trip: got %v, want 50000", cfg.AnnualAmount)
	}
}

func TestCurrentRothStrategyFor_NoRoth(t *testing.T) {
	s := eligibleBase()
	s.RothConversion = nil
	strat := currentRothStrategyFor(s)
	if strat.Kind != models.RothStrategyNone {
		t.Errorf("expected Kind=None for nil RothConversion, got %q", strat.Kind)
	}
}

func TestCurrentRothStrategyFor_Disabled(t *testing.T) {
	s := eligibleBase()
	s.RothConversion = &models.RothConversionConfig{Enabled: false, AnnualAmount: 50_000}
	strat := currentRothStrategyFor(s)
	if strat.Kind != models.RothStrategyNone {
		t.Errorf("expected Kind=None for disabled RothConversion, got %q", strat.Kind)
	}
}

func TestTaxOptimizer_MCRefinementPopulatesFields(t *testing.T) {
	s := eligibleBase()
	// eligibleBase doesn't include SS by default; add one so the
	// pipeline runs end-to-end without SS-portfolio fallback dominating.
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 4100, FRA: 67, ClaimAge: 67,
		SpouseFRABenefit: 1500, SpouseFRA: 67, SpouseClaimAge: 62,
		COLARate: 0.02, COLARateSet: true,
	}
	prep := perturbAndPrepare(s)
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	result := TaxOptimizerWithSeed(eng, in, nil, 42)
	if result == nil || !result.Eligible {
		t.Fatalf("expected eligible: %+v", result)
	}
	if result.MonteCarloRuns != taxOptimizerMonteCarloRuns {
		t.Errorf("MonteCarloRuns should equal %d, got %d", taxOptimizerMonteCarloRuns, result.MonteCarloRuns)
	}
	if len(result.Top) == 0 {
		t.Fatal("expected non-empty Top")
	}
	for i, cand := range result.Top {
		if cand.MCMedianEndingReal == 0 {
			t.Errorf("Top[%d].MCMedianEndingReal should be populated after MC refinement", i)
		}
		if cand.MCSurvivalRate < 0 || cand.MCSurvivalRate > 100 {
			t.Errorf("Top[%d].MCSurvivalRate out of [0,100]: %v", i, cand.MCSurvivalRate)
		}
	}
}

func TestTaxOptimizer_MCMedianIsReal(t *testing.T) {
	// With nonzero inflation, MCMedianEndingReal must be substantially
	// less than what you'd see with zero inflation (deflator=1). If the
	// deflator was NOT applied, both runs would return a similar nominal
	// median and the 3%-inflation run would NOT be ~2.5x smaller.
	//
	// We compare two optimizer runs on the same base scenario:
	//   run0: InflationRate=0  → deflator=1, MCMedianEndingReal ≈ nominal
	//   run3: InflationRate=3  → deflator≈2.5, MCMedianEndingReal ≈ nominal/2.5
	//
	// run3.MCMedianEndingReal must be materially less than run0.MCMedianEndingReal
	// because the cumulative deflator over 31 years at 3% ≈ 2.5x.
	base := eligibleBase()
	base.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 4100, FRA: 67, ClaimAge: 67,
		SpouseFRABenefit: 1500, SpouseFRA: 67, SpouseClaimAge: 62,
		COLARate: 0.02, COLARateSet: true,
	}
	base.ProjectionYears = 31

	eng := engine.New()

	s0 := *base
	s0.InflationRate = 0
	prep0 := perturbAndPrepare(&s0)
	r0 := TaxOptimizerWithSeed(eng, engine.Input{Prepared: prep0}, nil, 42)
	if r0 == nil || !r0.Eligible || len(r0.Top) == 0 {
		t.Fatalf("run0 (inflation=0): expected eligible: %+v", r0)
	}

	s3 := *base
	s3.InflationRate = 3
	prep3 := perturbAndPrepare(&s3)
	r3 := TaxOptimizerWithSeed(eng, engine.Input{Prepared: prep3}, nil, 42)
	if r3 == nil || !r3.Eligible || len(r3.Top) == 0 {
		t.Fatalf("run3 (inflation=3): expected eligible: %+v", r3)
	}

	// Cumulative deflator over 31 years at 3% ≈ 2.5x.
	deflator := math.Pow(1.03, float64(base.ProjectionYears))
	if deflator < 2.0 {
		t.Fatalf("test setup expects deflator > 2x, got %v", deflator)
	}

	med0 := r0.Best.MCMedianEndingReal
	med3 := r3.Best.MCMedianEndingReal
	if med0 <= 0 || med3 <= 0 {
		t.Fatalf("MCMedianEndingReal must be positive: run0=%v run3=%v", med0, med3)
	}

	// If the deflator was NOT applied, med3 ≈ med0 (both would be nominal).
	// With proper deflation, med3 ≈ med0 / deflator (roughly).
	// We require med3 < med0 * 0.75 (conservative: any deflation < 1.33x
	// over 31 years would fail). At 3% for 31 years the real ratio ≈ 0.40.
	ratio := med3 / med0
	if ratio >= 0.75 {
		t.Errorf("MCMedianEndingReal ratio (3%%/0%% inflation) = %v, expected < 0.75 — "+
			"deflator may not have been applied (run0=%v run3=%v)", ratio, med0, med3)
	}
}

func TestTaxOptimizer_MCDeterministicWithSeed(t *testing.T) {
	s := eligibleBase()
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 4100, FRA: 67, ClaimAge: 67,
		SpouseFRABenefit: 1500, SpouseFRA: 67, SpouseClaimAge: 62,
		COLARate: 0.02, COLARateSet: true,
	}
	prep := perturbAndPrepare(s)
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	r1 := TaxOptimizerWithSeed(eng, in, nil, 42)
	r2 := TaxOptimizerWithSeed(eng, in, nil, 42)
	if r1 == nil || r2 == nil || len(r1.Top) != len(r2.Top) {
		t.Fatalf("expected same length results: r1=%v r2=%v", r1, r2)
	}
	for i := range r1.Top {
		if math.Abs(r1.Top[i].MCMedianEndingReal-r2.Top[i].MCMedianEndingReal) > 1 {
			t.Errorf("MCMedianEndingReal differs at index %d: %v vs %v",
				i, r1.Top[i].MCMedianEndingReal, r2.Top[i].MCMedianEndingReal)
		}
		if math.Abs(r1.Top[i].MCSurvivalRate-r2.Top[i].MCSurvivalRate) > 0.001 {
			t.Errorf("MCSurvivalRate differs at index %d: %v vs %v",
				i, r1.Top[i].MCSurvivalRate, r2.Top[i].MCSurvivalRate)
		}
	}
}

func TestTaxOptimizerWithSeed_PopulatesBaselinePerYearConversions(t *testing.T) {
	// When the user's saved scenario has an enabled Roth conversion,
	// the baseline candidate must surface the per-year amounts so the
	// UI's "Show conversion amounts" disclosure renders for the
	// baseline row when (if) we later expose it.
	//
	// NOTE: prepare.From → ComputeAges derives CurrentAge from
	// StartDate + BirthMonth, overriding in-memory mutations. Read the
	// post-prep CurrentAge from prep.Settings() for assertions.
	s := eligibleBase()
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 40_000,
		StartYear:    0,
		EndYear:      4, // inclusive — 5 years
	}

	prep := perturbAndPrepare(s)
	in := engine.Input{Prepared: prep}
	eng := engine.New()
	preparedCurrentAge := prep.Settings().CurrentAge

	result := TaxOptimizerWithSeed(eng, in, nil, 12345)
	if result == nil || !result.Eligible {
		t.Fatalf("expected eligible result, got %+v", result)
	}
	if len(result.Baseline.PerYearConversions) != 5 {
		t.Fatalf("Baseline.PerYearConversions: got %d entries, want 5",
			len(result.Baseline.PerYearConversions))
	}
	for i, yc := range result.Baseline.PerYearConversions {
		if yc.Amount != 40_000 {
			t.Errorf("entry %d: Amount=%v, want 40000", i, yc.Amount)
		}
		if want := preparedCurrentAge + i; yc.Age != want {
			t.Errorf("entry %d: Age=%d, want %d", i, yc.Age, want)
		}
	}
}

func TestTaxOptimizerWithSeed_BaselineEmptyForDisabledRoth(t *testing.T) {
	s := eligibleBase()
	s.RothConversion = &models.RothConversionConfig{Enabled: false}

	prep := perturbAndPrepare(s)
	in := engine.Input{Prepared: prep}
	eng := engine.New()

	result := TaxOptimizerWithSeed(eng, in, nil, 12345)
	if result == nil || !result.Eligible {
		t.Fatalf("expected eligible result, got %+v", result)
	}
	if result.Baseline.PerYearConversions != nil {
		t.Errorf("expected nil baseline PerYearConversions for disabled Roth, got %v",
			result.Baseline.PerYearConversions)
	}
}
