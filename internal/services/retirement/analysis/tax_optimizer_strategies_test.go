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
