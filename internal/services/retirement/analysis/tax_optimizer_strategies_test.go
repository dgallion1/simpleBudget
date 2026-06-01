package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
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

func TestInflatedBracketTopForYear_GrowsWithCalendarYear(t *testing.T) {
	// Regression: the bracket-fill ceiling is a future-nominal taxable-
	// income threshold and must be inflated to the candidate's calendar
	// year (matching the engine's bracket inflation off taxBaseYear=2024).
	// A frozen 2024 ceiling understated conversion room for later years.
	s := &models.WhatIfSettings{
		StartDate:     "2024-01",
		InflationRate: 3.0,
		TaxConfig:     &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
	}

	base, ok := bracketTopFor(models.FilingMarriedJoint, 0.24)
	if !ok {
		t.Fatal("bracketTopFor failed for MFJ 24%")
	}

	// Year 0 == base year 2024: no inflation.
	y0, ok := inflatedBracketTopForYear(s, 0.24, 0)
	if !ok {
		t.Fatal("inflatedBracketTopForYear failed at year 0")
	}
	if !ssWithinTolerance(y0, base, 0.01) {
		t.Errorf("year 0 ceiling = %v, want base %v (no inflation in base year)", y0, base)
	}

	// Year 10: ceiling must have compounded ~3%/yr above the base.
	y10, _ := inflatedBracketTopForYear(s, 0.24, 10)
	wantY10 := base * math.Pow(1.03, 10)
	if !ssWithinTolerance(y10, wantY10, 1.0) {
		t.Errorf("year 10 ceiling = %v, want %v (base × 1.03^10)", y10, wantY10)
	}
	if y10 <= y0 {
		t.Errorf("ceiling must grow with calendar year: y0=%v y10=%v", y0, y10)
	}
}

func TestEstimateOtherTaxableIncome_TaxableIncomeUnits(t *testing.T) {
	// estimateOtherTaxableIncome returns TAXABLE ordinary income, so a
	// household with only modest Social Security has ~0: SS is below the
	// §86 taxability threshold and the standard deduction covers the rest.
	s := &models.WhatIfSettings{
		CurrentAge:      67,
		ProjectionYears: 30,
		StartDate:       "2024-01",
		TaxConfig:       &models.TaxConfig{FilingStatus: models.FilingSingle},
		SocialSecurity: &models.SocialSecurityConfig{
			FRABenefit: 2_000, FRA: 67, ClaimAge: 67,
		},
	}
	if got := estimateOtherTaxableIncome(s, 0); got > 1.0 {
		t.Errorf("SS-only modest: expected ~0 taxable (SS non-taxable + standard deduction), got %v", got)
	}

	// Add a $96k pension: SS now exceeds the §86 threshold (taxed up to
	// 85%) and the result clears the standard deduction.
	s.IncomeSources = []models.IncomeSource{
		{Type: models.IncomeFixed, Name: "Pension", Amount: 8_000, StartMonth: 0},
	}
	got := estimateOtherTaxableIncome(s, 0)
	// Lower bound: pension alone less the standard deduction is taxable.
	if got <= 96_000-30_000 {
		t.Errorf("with $96k pension, taxable income unexpectedly low: %v", got)
	}
	// Upper bound: SS is capped at 85% taxable and the standard deduction
	// must have been subtracted, so taxable < pension + 85%·SS.
	if got >= 96_000+0.85*24_000 {
		t.Errorf("taxable income %v >= pension + 85%%·SS — standard deduction not subtracted?", got)
	}
}

func TestEstimateOtherTaxableIncome_MFJIncludesSpouseSS(t *testing.T) {
	// Regression: bracket-fill estimator previously omitted spouse SS from
	// the joint return's provisional income. With a pension large enough
	// that SS is fully taxable, dropping the spouse's benefit must lower
	// the household's taxable income by the spouse's taxable portion.
	base := &models.WhatIfSettings{
		CurrentAge:      67,
		SpouseAge:       67,
		ProjectionYears: 30,
		StartDate:       "2024-01",
		TaxConfig:       &models.TaxConfig{FilingStatus: models.FilingMarriedJoint},
		IncomeSources: []models.IncomeSource{
			{Type: models.IncomeFixed, Name: "Pension", Amount: 10_000, StartMonth: 0},
		},
		SocialSecurity: &models.SocialSecurityConfig{
			FRABenefit: 3_000, FRA: 67, ClaimAge: 67,
			SpouseFRABenefit: 2_000, SpouseFRA: 67, SpouseClaimAge: 67, COLARate: 0,
		},
	}
	withSpouse := estimateOtherTaxableIncome(base, 0)

	noSpouse := *base
	ssNoSpouse := *base.SocialSecurity
	ssNoSpouse.SpouseFRABenefit = 0
	noSpouse.SocialSecurity = &ssNoSpouse
	without := estimateOtherTaxableIncome(&noSpouse, 0)

	diff := withSpouse - without
	// Spouse SS is $24k/yr; its taxable contribution is positive and capped
	// at 85%.
	if diff <= 0 || diff > 0.85*24_000+1 {
		t.Errorf("spouse SS contribution to joint taxable income = %v, want in (0, 85%%·$24k]; with=%v without=%v",
			diff, withSpouse, without)
	}

	// Non-MFJ: spouse SS belongs on a separate return, so toggling it must
	// not change this taxpayer's figure.
	mfs := *base
	mfsCfg := *base.TaxConfig
	mfsCfg.FilingStatus = models.FilingMarriedSeparate
	mfs.TaxConfig = &mfsCfg
	mfsWith := estimateOtherTaxableIncome(&mfs, 0)
	mfsNoSpouse := mfs
	mfsNoSpouse.SocialSecurity = &ssNoSpouse
	if mfsWithout := estimateOtherTaxableIncome(&mfsNoSpouse, 0); mfsWith != mfsWithout {
		t.Errorf("non-MFJ must ignore spouse SS: with=%v without=%v", mfsWith, mfsWithout)
	}
}

func TestEstimateOtherTaxableIncome_PostRMD(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:         65,
		ProjectionYears:    30,
		StartDate:          "2024-01", // birth ~1959 → SECURE 2.0 RMD age 73
		PortfolioValue:     2_000_000,
		TaxDeferredPercent: 80,
		InvestmentReturn:   6,
		TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingSingle},
		SocialSecurity: &models.SocialSecurityConfig{
			FRABenefit: 3_000, FRA: 67, ClaimAge: 67, COLARate: 0.02, COLARateSet: true,
		},
	}
	rmdAge := engine.EffectiveRMDStartAge(s)
	projYear := rmdAge - s.CurrentAge // first RMD year

	// At the RMD start age the tax-deferred balance (~$1.6M grown ~8yrs
	// @6%) divided by the IRS Uniform Lifetime factor (~26.5 at 73) yields
	// ~$96k of RMD, plus the 85%-taxable portion of ~$40k SS, less the
	// standard deduction. Wide band to detect either term being dropped.
	got := estimateOtherTaxableIncome(s, projYear)
	if got < 90_000 || got > 145_000 {
		t.Errorf("first RMD year (age %d): expected $90k–$145k taxable, got %v", rmdAge, got)
	}

	// And the RMD must actually be driving it: the year before RMD begins
	// (SS only) is far lower.
	if preRMD := estimateOtherTaxableIncome(s, projYear-1); !(got > preRMD+50_000) {
		t.Errorf("RMD year (%v) should greatly exceed pre-RMD year (%v)", got, preRMD)
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
	// Each override must lift taxable ordinary income to the (inflated) 22%
	// ceiling — not merely equal the ceiling. The gross conversion can exceed
	// the taxable-income ceiling because the standard deduction and the
	// conversion's own effect on SS taxability sit between them.
	for year, amount := range cfg.PerYearOverrides {
		if amount < 0 {
			t.Errorf("year %d: negative override %v", year, amount)
		}
		ceiling, _ := inflatedBracketTopForYear(s, 0.22, year)
		resulting := bracketFillIncomeForYear(s, year, 0).taxableOrdinaryIncome(amount)
		if math.Abs(resulting-ceiling) > 1.0 {
			t.Errorf("year %d: override %v drives taxable ordinary income to %v, want 22%% ceiling %v",
				year, amount, resulting, ceiling)
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

	for i, yc := range got {
		projYear := i // startProjYear=0 because StartAge==CurrentAge
		ceiling, ok := inflatedBracketTopForYear(s, 0.24, projYear)
		if !ok {
			t.Fatalf("year %d: no inflated ceiling for 24%% bracket", projYear)
		}
		// The conversion must lift taxable ordinary income to the ceiling, not
		// merely equal ceiling−other: the standard-deduction headroom (and any
		// conversion-driven SS taxability) sit between the gross conversion and
		// the taxable-income ceiling.
		resulting := bracketFillIncomeForYear(s, projYear, 0).taxableOrdinaryIncome(yc.Amount)
		if math.Abs(resulting-ceiling) > 1.0 {
			t.Errorf("year %d (age %d): conversion %v drives taxable ordinary income to %v, want ceiling %v",
				projYear, yc.Age, yc.Amount, resulting, ceiling)
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
