package analysis

import (
	"testing"

	"budget2/internal/models"
)

func TestEnumerateLadderStrategies_DefaultShape(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:      67,
		ProjectionYears: 31,
		SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 67},
	}
	strategies := enumerateLadderStrategies(s)

	if len(strategies) == 0 {
		t.Fatal("expected non-empty strategy slice")
	}

	// All have Kind=ladder.
	for _, st := range strategies {
		if st.Kind != models.RothStrategyLadder {
			t.Errorf("expected Kind=ladder, got %q", st.Kind)
		}
	}

	// All windows respect currentAge and are non-degenerate.
	for _, st := range strategies {
		if st.StartAge < s.CurrentAge {
			t.Errorf("window starts before currentAge: %+v", st)
		}
		if st.EndAge <= st.StartAge {
			t.Errorf("invalid window: %+v", st)
		}
	}

	// Each strategy has a non-empty Label.
	for _, st := range strategies {
		if st.Label == "" {
			t.Errorf("missing Label: %+v", st)
		}
	}
}

func TestEnumerateLadderStrategies_SkipsZeroAmountDups(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:      60,
		ProjectionYears: 35,
		SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 67},
	}
	strategies := enumerateLadderStrategies(s)

	zeroCount := 0
	for _, st := range strategies {
		if st.AnnualAmount == 0 {
			zeroCount++
		}
	}
	// Only one $0 candidate (no-conversion baseline) — not one per window.
	if zeroCount != 1 {
		t.Errorf("expected exactly one $0 ladder candidate, got %d", zeroCount)
	}
}

func TestEnumerateLadderStrategies_LabelsAreStable(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:      67,
		ProjectionYears: 31,
		SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 70},
	}
	strategies := enumerateLadderStrategies(s)

	// Find the $100k/yr to RMD age (73) candidate and assert its label.
	var found *models.RothOptimizerStrategy
	for i, st := range strategies {
		if st.AnnualAmount == 100_000 && st.EndAge == 73 {
			found = &strategies[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected to find $100k/yr 67→73 candidate")
	}
	want := "$100k/yr 67→73"
	if found.Label != want {
		t.Errorf("label: got %q, want %q", found.Label, want)
	}
}

func TestEnumerateLadderStrategies_ExactCountForRepresentativeSettings(t *testing.T) {
	// currentAge=67, claimAge=67:
	//   - "5yr"   window (67, 72) valid
	//   - "SS"    window (67, 67) FILTERED (EndAge == StartAge)
	//   - "IRMAA" window (67, 65) FILTERED (EndAge < StartAge)
	//   - "RMD"   window (67, 73) valid
	//   - "mid"   window (72, 77) valid
	// → 3 valid windows
	//
	// Candidate breakdown:
	//   1 × "No conversion" baseline
	//   6 non-zero amounts × 3 windows = 18
	// → 19 total
	s := &models.WhatIfSettings{
		CurrentAge:      67,
		ProjectionYears: 31,
		SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 67},
	}
	got := enumerateLadderStrategies(s)
	if want := 19; len(got) != want {
		t.Errorf("ladder candidate count: got %d, want %d", len(got), want)
		for i, st := range got {
			t.Logf("  [%d] %s (start=%d end=%d amount=%v)", i, st.Label, st.StartAge, st.EndAge, st.AnnualAmount)
		}
	}
}

func TestStrategyWindows_FiltersOneYearWindows(t *testing.T) {
	// With currentAge=66 and SS claim at 67, the "SS" window would be
	// (66, 67) — a 1-year window that would translate to EndYear=0 in
	// RothConversionConfig, which the engine treats as "indefinite."
	// strategyWindows must drop these.
	s := &models.WhatIfSettings{
		CurrentAge:      66,
		ProjectionYears: 30,
		SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 67},
	}
	windows := strategyWindows(s)
	for _, w := range windows {
		if w.EndAge-w.StartAge < 2 {
			t.Errorf("found 1-year window in output: %+v (would trigger engine EndYear=0 bug)", w)
		}
	}
}

func TestEstimateOtherTaxableIncome_PreSSAndPreRMD(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:         60,
		ProjectionYears:    30,
		PortfolioValue:     2_000_000,
		TaxDeferredPercent: 80,
		SocialSecurity: &models.SocialSecurityConfig{
			FRABenefit: 3_000,
			FRA:        67,
			ClaimAge:   67,
		},
	}
	// Year 0: age 60. No SS yet, no RMD yet, no income sources.
	got := estimateOtherTaxableIncome(s, 0)
	if got > 1.0 {
		t.Errorf("year 0 pre-SS pre-RMD expected ~0, got %v", got)
	}
}

func TestEstimateOtherTaxableIncome_PostSS(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:      60,
		ProjectionYears: 30,
		SocialSecurity: &models.SocialSecurityConfig{
			FRABenefit: 3_000,
			FRA:        67,
			ClaimAge:   67,
			COLARate:   0.02,
		},
	}
	// Year 7: age 67, claim age 67. SS benefit ≈ $3,000 × 12 = $36,000
	// (estimator includes gross for simplicity; taxable portion is engine's job).
	got := estimateOtherTaxableIncome(s, 7)
	if got < 20_000 || got > 50_000 {
		t.Errorf("year 7 (post-SS) expected ~$36k, got %v", got)
	}
}

func TestEstimateOtherTaxableIncome_MFJIncludesSpouseSS(t *testing.T) {
	// Regression: bracket-fill estimator previously omitted spouse SS,
	// causing MFJ candidates to over-convert above the target bracket.
	s := &models.WhatIfSettings{
		CurrentAge:      60,
		SpouseAge:       60,
		ProjectionYears: 30,
		TaxConfig:       &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
		SocialSecurity: &models.SocialSecurityConfig{
			FRABenefit:       3_000,
			FRA:              67,
			ClaimAge:         67,
			SpouseFRABenefit: 2_000,
			SpouseFRA:        67,
			SpouseClaimAge:   67,
			COLARate:         0,
		},
	}
	// Year 7: both spouses age 67, both claim at FRA. Primary
	// $3,000×12 = $36k, spouse $2,000×12 = $24k. Total ≈ $60k.
	got := estimateOtherTaxableIncome(s, 7)
	if got < 55_000 || got > 65_000 {
		t.Errorf("MFJ year 7 expected ~$60k (primary+spouse SS), got %v", got)
	}

	// Non-MFJ with same data: spouse SS must NOT be added (would belong
	// on spouse's separate return).
	s.TaxConfig.FilingStatus = models.FilingMarriedSeparate
	gotMFS := estimateOtherTaxableIncome(s, 7)
	if gotMFS < 30_000 || gotMFS > 42_000 {
		t.Errorf("MFS year 7 expected ~$36k (primary SS only), got %v", gotMFS)
	}
}

func TestEstimateOtherTaxableIncome_PostRMD(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:         60,
		ProjectionYears:    30,
		PortfolioValue:     2_000_000,
		TaxDeferredPercent: 80,
		InvestmentReturn:   6,
		SocialSecurity: &models.SocialSecurityConfig{
			FRABenefit: 3_000, FRA: 67, ClaimAge: 67, COLARate: 0.02, COLARateSet: true,
		},
	}
	// Year 13: age 73 (first RMD year). With claim at FRA, SS adjustment
	// is neutral — monthly SS ≈ $3000 grown 6yrs @ 2% COLA ≈ $3,378/mo
	// → ~$40.5k/yr. RMD ≈ $1.6M × 1.06^13 × 4% ≈ $136.5k. Total ≈ $177k.
	// Bound: 150k ≤ got ≤ 220k to detect either-term-dropped bugs.
	got := estimateOtherTaxableIncome(s, 13)
	if got < 150_000 || got > 220_000 {
		t.Errorf("year 13 (post-RMD) expected $150k–$220k, got %v", got)
	}
}

func TestEnumerateBracketFillStrategies_DefaultShape(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:         67,
		ProjectionYears:    31,
		PortfolioValue:     2_000_000,
		TaxDeferredPercent: 80,
		SocialSecurity:     &models.SocialSecurityConfig{FRABenefit: 3000, FRA: 67, ClaimAge: 67},
		TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
	}
	strategies := enumerateBracketFillStrategies(s)

	for _, st := range strategies {
		if st.Kind != models.RothStrategyBracketFill {
			t.Errorf("expected Kind=bracket_fill, got %q", st.Kind)
		}
		if st.TargetBracket < 0.10 || st.TargetBracket > 0.40 {
			t.Errorf("unexpected TargetBracket: %v", st.TargetBracket)
		}
		if st.Label == "" {
			t.Errorf("missing Label: %+v", st)
		}
	}
}

func TestEnumerateBracketFillStrategies_SkipsAllZeroCandidates(t *testing.T) {
	// Scenario where current age >= all window ends (no valid windows).
	s := &models.WhatIfSettings{
		CurrentAge:      80,
		ProjectionYears: 5,
		SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 70},
		TaxConfig:       &models.TaxConfig{FilingStatus: models.FilingSingle},
	}
	strategies := enumerateBracketFillStrategies(s)
	if len(strategies) != 0 {
		t.Errorf("expected 0 bracket-fill strategies for age-80 scenario, got %d", len(strategies))
	}
}

func TestEnumerateRothStrategies_CombinesFamiliesAndBaseline(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:         67,
		ProjectionYears:    31,
		PortfolioValue:     2_000_000,
		TaxDeferredPercent: 80,
		SocialSecurity:     &models.SocialSecurityConfig{FRABenefit: 3000, FRA: 67, ClaimAge: 67},
		TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
	}
	all := enumerateRothStrategies(s)

	var ladders, brackets, baselines int
	for _, st := range all {
		switch st.Kind {
		case models.RothStrategyLadder:
			if st.AnnualAmount == 0 {
				baselines++
			} else {
				ladders++
			}
		case models.RothStrategyBracketFill:
			brackets++
		}
	}
	if baselines != 1 {
		t.Errorf("expected exactly 1 baseline (no-conversion) candidate, got %d", baselines)
	}
	if ladders < 1 {
		t.Error("expected at least one non-zero ladder candidate")
	}
	if brackets < 1 {
		t.Error("expected at least one bracket-fill candidate")
	}
}

func TestRothStrategyToConfig_LadderProducesFixedAmount(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:      67,
		ProjectionYears: 20,
		TaxConfig:       &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
	}
	strat := models.RothOptimizerStrategy{
		Kind:         models.RothStrategyLadder,
		AnnualAmount: 75_000,
		StartAge:     67,
		EndAge:       73,
	}
	cfg := rothStrategyToConfig(s, strat)
	if cfg == nil || !cfg.Enabled {
		t.Fatal("expected enabled config")
	}
	if cfg.AnnualAmount != 75_000 {
		t.Errorf("AnnualAmount: got %v, want 75000", cfg.AnnualAmount)
	}
	if cfg.StartYear != 0 {
		t.Errorf("StartYear: got %v, want 0", cfg.StartYear)
	}
	if cfg.EndYear != 5 {
		t.Errorf("EndYear: got %v, want 5 (inclusive end of 67→73 minus 1)", cfg.EndYear)
	}
	if cfg.PerYearOverrides != nil {
		t.Errorf("ladder strategy should not produce PerYearOverrides, got %+v", cfg.PerYearOverrides)
	}
}

func TestRothStrategyToConfig_BracketFillProducesOverrides(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:         67,
		ProjectionYears:    20,
		PortfolioValue:     2_000_000,
		TaxDeferredPercent: 80,
		SocialSecurity:     &models.SocialSecurityConfig{FRABenefit: 3000, FRA: 67, ClaimAge: 67, COLARate: 0.02, COLARateSet: true},
		TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
	}
	strat := models.RothOptimizerStrategy{
		Kind:          models.RothStrategyBracketFill,
		TargetBracket: 0.22,
		StartAge:      67,
		EndAge:        73,
	}
	cfg := rothStrategyToConfig(s, strat)
	if cfg == nil || !cfg.Enabled {
		t.Fatal("expected enabled config")
	}
	if cfg.PerYearOverrides == nil {
		t.Fatal("bracket-fill should set PerYearOverrides")
	}
	// 6 years (67→72 inclusive on projection-year offsets 0..5).
	if len(cfg.PerYearOverrides) != 6 {
		t.Errorf("PerYearOverrides should have 6 entries, got %d: %+v", len(cfg.PerYearOverrides), cfg.PerYearOverrides)
	}
	// No override exceeds the 22% bracket ceiling for MFJ ($201,050 for 2024).
	// Conversion = ceiling - other income; should never be negative or > ceiling.
	for year, amount := range cfg.PerYearOverrides {
		if amount < 0 {
			t.Errorf("year %d: negative override %v", year, amount)
		}
		if amount > 201_050 {
			t.Errorf("year %d: override %v exceeds 22%% MFJ ceiling", year, amount)
		}
	}
}

func TestRothStrategyToConfig_NoConversionBaseline(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:      67,
		ProjectionYears: 20,
		TaxConfig:       &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
	}
	strat := models.RothOptimizerStrategy{
		Kind: models.RothStrategyLadder,
		// AnnualAmount intentionally 0 (the "No conversion" baseline)
		StartAge: 67,
		EndAge:   72,
	}
	cfg := rothStrategyToConfig(s, strat)
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Enabled {
		t.Errorf("no-conversion baseline should be Disabled, got Enabled=true")
	}
}

func TestEnumerateBracketFillStrategies_SECURE2_0RMDAge(t *testing.T) {
	// Age 69. strategyWindows produces:
	//   - 5yr  (69, 74) — EndAge=74
	//   - SS   (69, 67) — filtered (67 < 69)
	//   - IRMAA(69, 65) — filtered (65 < 69)
	//   - RMD  (69, 73) — EndAge=73
	//   - mid  (74, 79) — EndAge=79
	//
	// With rmdAge=73 (born 1957): keeps only windows where EndAge ≤ 73.
	//   → RMD (69,73) kept (73 ≤ 73). 5yr (69,74) dropped (74 > 73).
	// With rmdAge=75 (born 1965): keeps windows where EndAge ≤ 75.
	//   → RMD (69,73) kept. 5yr (69,74) ALSO kept (74 ≤ 75).
	//
	// So the 1965-birth case should produce strictly more bracket-fill
	// strategies than the 1957-birth case.
	base := &models.WhatIfSettings{
		CurrentAge:         69,
		ProjectionYears:    20,
		StartDate:          "2026-01",
		PortfolioValue:     2_000_000,
		TaxDeferredPercent: 80,
		SocialSecurity:     &models.SocialSecurityConfig{FRABenefit: 3000, FRA: 67, ClaimAge: 67, COLARate: 0.02, COLARateSet: true},
		TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
	}

	// Pre-SECURE 2.0: born 1957 → RMD age 73.
	s1957 := *base
	s1957.Persons = []models.Person{
		{ID: "p1", Name: "Primary", BirthMonth: "1957-01", Role: models.PersonRolePrimary},
	}
	n1957 := len(enumerateBracketFillStrategies(&s1957))

	// Post-SECURE 2.0: born 1965 → RMD age 75.
	s1965 := *base
	s1965.Persons = []models.Person{
		{ID: "p1", Name: "Primary", BirthMonth: "1965-01", Role: models.PersonRolePrimary},
	}
	n1965 := len(enumerateBracketFillStrategies(&s1965))

	// The 1965-birth scenario admits the extra 5yr (69,74) window, so must
	// produce strictly more strategies.
	if n1965 <= n1957 {
		t.Errorf("expected SECURE 2.0 (birth 1965, RMD age 75) to admit MORE bracket-fill "+
			"windows than pre-SECURE (birth 1957, RMD age 73), got n1957=%d n1965=%d",
			n1957, n1965)
	}
}

func TestStrategyYearlyConversions_Ladder(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:      60,
		ProjectionYears: 35,
		TaxConfig:       &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
		SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 67},
	}
	strat := models.RothOptimizerStrategy{
		Kind:         models.RothStrategyLadder,
		AnnualAmount: 75_000,
		StartAge:     67,
		EndAge:       72, // exclusive — 5 years (67..71)
	}
	got := strategyYearlyConversions(s, strat)

	if len(got) != 5 {
		t.Fatalf("len: got %d, want 5", len(got))
	}
	for i, yc := range got {
		wantAge := 67 + i
		if yc.Age != wantAge {
			t.Errorf("entry %d: Age=%d, want %d", i, yc.Age, wantAge)
		}
		if yc.Amount != 75_000 {
			t.Errorf("entry %d: Amount=%v, want 75000", i, yc.Amount)
		}
	}
}

func TestStrategyYearlyConversions_BracketFill_MFJ(t *testing.T) {
	s := eligibleBase()
	s.CurrentAge = 60
	s.ProjectionYears = 35
	// Push SS out so the estimator sees "no SS yet" for early years.
	s.SocialSecurity.ClaimAge = 67
	s.SocialSecurity.SpouseClaimAge = 67
	s.SpouseAge = 58
	strat := models.RothOptimizerStrategy{
		Kind:          models.RothStrategyBracketFill,
		TargetBracket: 0.24,
		StartAge:      60,
		EndAge:        63, // 3 years: 60, 61, 62
	}

	got := strategyYearlyConversions(s, strat)
	if len(got) != 3 {
		t.Fatalf("len: got %d, want 3", len(got))
	}

	ceiling, _ := bracketTopFor(models.FilingMarriedJoint, 0.24)
	for i, yc := range got {
		projYear := i // startProjYear=0 because StartAge==CurrentAge
		other := estimateOtherTaxableIncome(s, projYear)
		want := ceiling - other
		if want < 0 {
			want = 0
		}
		if yc.Amount != want {
			t.Errorf("year %d (age %d): Amount=%v, want %v", projYear, yc.Age, yc.Amount, want)
		}
	}
}

func TestStrategyYearlyConversions_NoConversion(t *testing.T) {
	s := eligibleBase()
	got := strategyYearlyConversions(s, models.RothOptimizerStrategy{Kind: models.RothStrategyNone})
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestStrategyYearlyConversions_ZeroAmountLadder(t *testing.T) {
	s := eligibleBase()
	got := strategyYearlyConversions(s, models.RothOptimizerStrategy{
		Kind:         models.RothStrategyLadder,
		AnnualAmount: 0,
		StartAge:     67,
		EndAge:       72,
	})
	if got != nil {
		t.Errorf("expected nil for zero-amount ladder, got %v", got)
	}
}

func TestRothStrategyToConfig_MatchesYearlyConversions(t *testing.T) {
	// Drift-guard: rothStrategyToConfig and strategyYearlyConversions
	// must produce identical per-year amounts. This locks the shared-
	// math invariant — if either path diverges, this test fails.
	s := eligibleBase()
	s.CurrentAge = 60
	s.ProjectionYears = 35
	s.SocialSecurity.ClaimAge = 67
	s.SocialSecurity.SpouseClaimAge = 67
	s.SpouseAge = 58

	strat := models.RothOptimizerStrategy{
		Kind:          models.RothStrategyBracketFill,
		TargetBracket: 0.24,
		StartAge:      60,
		EndAge:        67,
	}

	cfg := rothStrategyToConfig(s, strat)
	if cfg == nil || cfg.PerYearOverrides == nil {
		t.Fatal("expected non-nil PerYearOverrides for bracket-fill")
	}

	got := strategyYearlyConversions(s, strat)
	if len(got) == 0 {
		t.Fatal("expected non-empty YearlyConversions")
	}

	for _, yc := range got {
		projYear := yc.Age - s.CurrentAge
		want, ok := cfg.PerYearOverrides[projYear]
		if !ok {
			t.Errorf("projYear %d (age %d) missing from PerYearOverrides", projYear, yc.Age)
			continue
		}
		if yc.Amount != want {
			t.Errorf("projYear %d (age %d): YearlyConversion=%v, PerYearOverrides=%v",
				projYear, yc.Age, yc.Amount, want)
		}
	}

	// Symmetric check: every override key must have a YearlyConversion.
	if got, want := len(got), len(cfg.PerYearOverrides); got != want {
		t.Errorf("entry count mismatch: YearlyConversions=%d, PerYearOverrides=%d", got, want)
	}
}
