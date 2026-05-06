package retirement

import (
	"budget2/internal/models"
	"math"
	"testing"
)

func withinTolerance(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestAdjustedSSBenefit(t *testing.T) {
	tests := []struct {
		name     string
		pia      float64
		fra      int
		claimAge int
		wantMin  float64
		wantMax  float64
	}{
		{
			name:     "Claim at 62 with FRA 67 (60 months early, 30% reduction)",
			pia:      2000,
			fra:      67,
			claimAge: 62,
			wantMin:  1399,
			wantMax:  1401,
		},
		{
			name:     "Claim at FRA 67 (no adjustment)",
			pia:      2000,
			fra:      67,
			claimAge: 67,
			wantMin:  1999,
			wantMax:  2001,
		},
		{
			name:     "Claim at 70 with FRA 67 (36 months delayed, 24% increase)",
			pia:      2000,
			fra:      67,
			claimAge: 70,
			wantMin:  2479,
			wantMax:  2481,
		},
		{
			name:     "Claim at 64 with FRA 67 (36 months early, 20% reduction)",
			pia:      2000,
			fra:      67,
			claimAge: 64,
			wantMin:  1599,
			wantMax:  1601,
		},
		{
			name:     "PIA $3000, FRA 66, claim at 62 (48 months early, 25% reduction)",
			pia:      3000,
			fra:      66,
			claimAge: 62,
			wantMin:  2249,
			wantMax:  2251,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AdjustedSSBenefit(tc.pia, tc.fra, tc.claimAge)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("AdjustedSSBenefit(%.0f, %d, %d) = %.2f, want between %.0f and %.0f",
					tc.pia, tc.fra, tc.claimAge, got, tc.wantMin, tc.wantMax)
			}
		})
	}

	t.Run("Edge: claimAge below 62 returns same as 62", func(t *testing.T) {
		at62 := AdjustedSSBenefit(2000, 67, 62)
		at60 := AdjustedSSBenefit(2000, 67, 60)
		if !withinTolerance(at62, at60, 0.01) {
			t.Errorf("claimAge 60 = %.2f, claimAge 62 = %.2f; expected same result", at60, at62)
		}
	})

	t.Run("Edge: claimAge above 70 returns same as 70", func(t *testing.T) {
		at70 := AdjustedSSBenefit(2000, 67, 70)
		at73 := AdjustedSSBenefit(2000, 67, 73)
		if !withinTolerance(at70, at73, 0.01) {
			t.Errorf("claimAge 73 = %.2f, claimAge 70 = %.2f; expected same result", at73, at70)
		}
	})
}

func TestSSComparisonTable(t *testing.T) {
	t.Run("Standard case: PIA $2000, FRA 67, currentAge 55, COLA 2%", func(t *testing.T) {
		options := SSComparisonTable(2000, 67, 55, 0.02)

		if len(options) != 9 {
			t.Fatalf("expected 9 options (ages 62-70), got %d", len(options))
		}

		// First option should be age 62, last should be 70
		if options[0].ClaimAge != 62 {
			t.Errorf("first option ClaimAge = %d, want 62", options[0].ClaimAge)
		}
		if options[8].ClaimAge != 70 {
			t.Errorf("last option ClaimAge = %d, want 70", options[8].ClaimAge)
		}

		// Monthly benefits should be strictly increasing
		for i := 1; i < len(options); i++ {
			if options[i].MonthlyBenefit <= options[i-1].MonthlyBenefit {
				t.Errorf("monthly benefit at age %d (%.2f) should exceed age %d (%.2f)",
					options[i].ClaimAge, options[i].MonthlyBenefit,
					options[i-1].ClaimAge, options[i-1].MonthlyBenefit)
			}
		}

		// AnnualBenefit should be 12 * MonthlyBenefit
		for _, opt := range options {
			expected := opt.MonthlyBenefit * 12
			if !withinTolerance(opt.AnnualBenefit, expected, 1.0) {
				t.Errorf("age %d: AnnualBenefit %.2f != 12 * MonthlyBenefit %.2f",
					opt.ClaimAge, opt.AnnualBenefit, expected)
			}
		}

		// PctOfPIA for FRA should be 100%
		fraOption := options[5] // age 67 is index 5 (62,63,64,65,66,67)
		if fraOption.ClaimAge != 67 {
			t.Fatalf("expected option at index 5 to be age 67, got %d", fraOption.ClaimAge)
		}
		if !withinTolerance(fraOption.PctOfPIA, 100.0, 0.5) {
			t.Errorf("PctOfPIA at FRA = %.1f%%, want 100%%", fraOption.PctOfPIA)
		}

		// At longevity (age 90), delayed claiming at 70 should beat claiming at 62
		cum90at62 := options[0].CumulativeAt90
		cum90at70 := options[8].CumulativeAt90
		if cum90at70 <= cum90at62 {
			t.Errorf("cumulative at 90: age 70 (%.0f) should exceed age 62 (%.0f)",
				cum90at70, cum90at62)
		}

		// At age 80, early claiming at 62 should beat late claiming at 70
		cum80at62 := options[0].CumulativeAt80
		cum80at70 := options[8].CumulativeAt80
		if cum80at62 <= cum80at70 {
			t.Errorf("cumulative at 80: age 62 (%.0f) should exceed age 70 (%.0f)",
				cum80at62, cum80at70)
		}

		// Cumulative values should increase with age: 80 < 85 < 90
		for _, opt := range options {
			if opt.CumulativeAt85 <= opt.CumulativeAt80 {
				t.Errorf("age %d: cumulative at 85 (%.0f) should exceed at 80 (%.0f)",
					opt.ClaimAge, opt.CumulativeAt85, opt.CumulativeAt80)
			}
			if opt.CumulativeAt90 <= opt.CumulativeAt85 {
				t.Errorf("age %d: cumulative at 90 (%.0f) should exceed at 85 (%.0f)",
					opt.ClaimAge, opt.CumulativeAt90, opt.CumulativeAt85)
			}
		}
	})

	t.Run("Current age 65: only returns ages 65-70", func(t *testing.T) {
		options := SSComparisonTable(2000, 67, 65, 0.02)

		if len(options) != 6 {
			t.Fatalf("expected 6 options (ages 65-70), got %d", len(options))
		}
		if options[0].ClaimAge != 65 {
			t.Errorf("first option ClaimAge = %d, want 65", options[0].ClaimAge)
		}
		if options[5].ClaimAge != 70 {
			t.Errorf("last option ClaimAge = %d, want 70", options[5].ClaimAge)
		}
	})

	t.Run("Current age 70: only returns age 70", func(t *testing.T) {
		options := SSComparisonTable(2000, 67, 70, 0.02)

		if len(options) != 1 {
			t.Fatalf("expected 1 option (age 70), got %d", len(options))
		}
		if options[0].ClaimAge != 70 {
			t.Errorf("option ClaimAge = %d, want 70", options[0].ClaimAge)
		}
	})
}

func TestProjectedSSBenefitForMonth(t *testing.T) {
	got := projectedSSBenefitForMonth(2000, 0.02, 6)
	want := 2000 * math.Pow(1.02, 0.5)
	if !withinTolerance(got, want, 0.01) {
		t.Fatalf("projectedSSBenefitForMonth = %.2f, want %.2f", got, want)
	}

	t.Run("zero base returns zero", func(t *testing.T) {
		if got := projectedSSBenefitForMonth(0, 0.02, 12); got != 0 {
			t.Fatalf("expected 0 for zero base, got %.2f", got)
		}
	})

	t.Run("negative months returns zero", func(t *testing.T) {
		if got := projectedSSBenefitForMonth(2000, 0.02, -1); got != 0 {
			t.Fatalf("expected 0 for negative months, got %.2f", got)
		}
	})
}

func TestDerivedPIA(t *testing.T) {
	t.Run("round trips with AdjustedSSBenefit", func(t *testing.T) {
		for _, age := range []int{62, 65, 67, 70} {
			pia := 2000.0
			benefit := AdjustedSSBenefit(pia, 67, age)
			derived := DerivedPIA(benefit, 67, age)
			if !withinTolerance(derived, pia, 0.01) {
				t.Errorf("age %d: DerivedPIA(%.2f) = %.2f, want %.2f", age, benefit, derived, pia)
			}
		}
	})

	t.Run("claimAge clamped above 70", func(t *testing.T) {
		got := DerivedPIA(2480, 67, 75)
		want := DerivedPIA(2480, 67, 70)
		if !withinTolerance(got, want, 0.01) {
			t.Fatalf("claimAge 75 = %.2f, claimAge 70 = %.2f; expected same", got, want)
		}
	})
}

func TestHasManualSocialSecurityIncomeSource(t *testing.T) {
	t.Run("nil settings", func(t *testing.T) {
		if HasManualSocialSecurityIncomeSource(nil) {
			t.Fatal("expected false for nil settings")
		}
	})

	t.Run("no income sources", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		if HasManualSocialSecurityIncomeSource(s) {
			t.Fatal("expected false with no income sources")
		}
	})

	t.Run("non-SS income source", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.IncomeSources = []models.IncomeSource{{Name: "Pension", Amount: 1000}}
		if HasManualSocialSecurityIncomeSource(s) {
			t.Fatal("expected false for pension")
		}
	})

	t.Run("SS income source detected", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.IncomeSources = []models.IncomeSource{{Name: "Social Security", Amount: 2000}}
		if !HasManualSocialSecurityIncomeSource(s) {
			t.Fatal("expected true for Social Security income")
		}
	})

	t.Run("SSI token detected", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.IncomeSources = []models.IncomeSource{{Name: "SSI", Amount: 1500}}
		if !HasManualSocialSecurityIncomeSource(s) {
			t.Fatal("expected true for SSI income")
		}
	})
}

func TestAdjustedSpousalBenefit(t *testing.T) {
	tests := []struct {
		name       string
		spousalPIA float64
		spouseFRA  int
		claimAge   int
		wantMin    float64
		wantMax    float64
	}{
		{
			// SSA rule: 25/36 of 1% per month for first 36 months early, then
			// 5/12 of 1%. At FRA 67 / claim 62 → 60 months early →
			// 36*25/3600 + 24*5/1200 = 0.25 + 0.10 = 0.35 reduction → 65%.
			// 65% × 50% of worker PIA = 32.5% of worker PIA.
			name:       "claim at 62 with FRA 67 (35% spousal reduction = 32.5% of $4100 worker PIA)",
			spousalPIA: 2050, // 50% of $4100 worker PIA
			spouseFRA:  67,
			claimAge:   62,
			wantMin:    1332,
			wantMax:    1333,
		},
		{
			name:       "claim at FRA 67 (no reduction → 50% of worker PIA)",
			spousalPIA: 2050,
			spouseFRA:  67,
			claimAge:   67,
			wantMin:    2049,
			wantMax:    2051,
		},
		{
			name:       "claim at 70 with FRA 67 (no DRC for spousal benefits)",
			spousalPIA: 2050,
			spouseFRA:  67,
			claimAge:   70,
			wantMin:    2049,
			wantMax:    2051,
		},
		{
			// 36 months early × 25/3600 = 25% reduction → 75%.
			name:       "claim at 64 with FRA 67 (25% reduction)",
			spousalPIA: 2000,
			spouseFRA:  67,
			claimAge:   64,
			wantMin:    1499,
			wantMax:    1501,
		},
		{
			// 48 months early at FRA 66: 36*25/3600 + 12*5/1200 = 0.30 → 70%.
			name:       "FRA 66, claim at 62 (30% reduction)",
			spousalPIA: 1500,
			spouseFRA:  66,
			claimAge:   62,
			wantMin:    1049,
			wantMax:    1051,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AdjustedSpousalBenefit(tc.spousalPIA, tc.spouseFRA, tc.claimAge)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("AdjustedSpousalBenefit(%.0f, %d, %d) = %.2f, want between %.0f and %.0f",
					tc.spousalPIA, tc.spouseFRA, tc.claimAge, got, tc.wantMin, tc.wantMax)
			}
		})
	}

	t.Run("spousal early reduction is steeper than worker reduction", func(t *testing.T) {
		spousal := AdjustedSpousalBenefit(2000, 67, 62)
		worker := AdjustedSSBenefit(2000, 67, 62)
		if spousal >= worker {
			t.Errorf("expected spousal (%.2f) < worker (%.2f) at age 62 with FRA 67", spousal, worker)
		}
	})

	t.Run("claim below 62 clamped to 62", func(t *testing.T) {
		at62 := AdjustedSpousalBenefit(2000, 67, 62)
		at60 := AdjustedSpousalBenefit(2000, 67, 60)
		if !withinTolerance(at62, at60, 0.01) {
			t.Errorf("claimAge 60 = %.2f, claimAge 62 = %.2f; expected same", at60, at62)
		}
	})
}

func TestSpousalTopUp(t *testing.T) {
	t.Run("own benefit already exceeds half higher PIA", func(t *testing.T) {
		got := SpousalTopUp(2100, 4000, 67, 67)
		if got != 2100 {
			t.Fatalf("SpousalTopUp = %.2f, want own benefit 2100", got)
		}
	})

	t.Run("claim at FRA tops up to half higher PIA", func(t *testing.T) {
		got := SpousalTopUp(1000, 4000, 67, 67)
		if got != 2000 {
			t.Fatalf("SpousalTopUp = %.2f, want 2000", got)
		}
	})

	t.Run("early claim uses spousal reduction (not worker reduction)", func(t *testing.T) {
		got := SpousalTopUp(500, 4000, 67, 62)
		want := AdjustedSpousalBenefit(2000, 67, 62)
		if !withinTolerance(got, want, 0.01) {
			t.Fatalf("SpousalTopUp = %.2f, want %.2f", got, want)
		}
	})

	t.Run("32.5%% rule at age 62 with worker PIA $4100", func(t *testing.T) {
		got := SpousalTopUp(0, 4100, 67, 62)
		if !withinTolerance(got, 1332.50, 0.5) {
			t.Fatalf("SpousalTopUp(0, 4100, 67, 62) = %.2f, want ~1332.50", got)
		}
	})

	t.Run("delayed claim does not add delayed credits to spousal component", func(t *testing.T) {
		got := SpousalTopUp(1000, 4000, 67, 70)
		if got != 2000 {
			t.Fatalf("SpousalTopUp delayed claim = %.2f, want capped 2000", got)
		}
	})

	t.Run("zero higher PIA returns own benefit", func(t *testing.T) {
		got := SpousalTopUp(1500, 0, 67, 67)
		if got != 1500 {
			t.Fatalf("SpousalTopUp = %.2f, want 1500", got)
		}
	})
}

func TestSSBreakevenAges(t *testing.T) {
	t.Run("Standard PIA $2000, FRA 67, COLA 2%", func(t *testing.T) {
		results := SSBreakevenAges(2000, 67, 0.02)

		if len(results) == 0 {
			t.Fatal("expected at least one breakeven result")
		}

		// Should have breakeven comparisons between adjacent claiming ages
		// (62 vs 63, 63 vs 64, ..., 69 vs 70) = 8 pairs
		if len(results) != 8 {
			t.Errorf("expected 8 breakeven results (adjacent pairs 62-70), got %d", len(results))
		}

		for _, r := range results {
			// Every breakeven age should be after the later claiming age
			if r.BreakevenAge <= r.LateAge {
				t.Errorf("breakeven %d should be after late claim age %d", r.BreakevenAge, r.LateAge)
			}

			// Breakeven should be reasonable: not past 100
			if r.BreakevenAge > 100 {
				t.Errorf("breakeven age %d (early=%d, late=%d) seems unreasonably high",
					r.BreakevenAge, r.EarlyAge, r.LateAge)
			}

			// EarlyAge should be less than LateAge
			if r.EarlyAge >= r.LateAge {
				t.Errorf("early age %d should be less than late age %d",
					r.EarlyAge, r.LateAge)
			}
		}
	})

	t.Run("Breakeven 62 vs 63 around 75-80", func(t *testing.T) {
		results := SSBreakevenAges(2000, 67, 0.02)

		var found bool
		for _, r := range results {
			if r.EarlyAge == 62 && r.LateAge == 63 {
				found = true
				if r.BreakevenAge < 73 || r.BreakevenAge > 82 {
					t.Errorf("breakeven 62 vs 63 = %d, expected roughly 75-80", r.BreakevenAge)
				}
				break
			}
		}
		if !found {
			t.Error("did not find breakeven result for ages 62 vs 63")
		}
	})

	t.Run("Breakeven 69 vs 70 around 82-85", func(t *testing.T) {
		results := SSBreakevenAges(2000, 67, 0.02)

		var found bool
		for _, r := range results {
			if r.EarlyAge == 69 && r.LateAge == 70 {
				found = true
				if r.BreakevenAge < 80 || r.BreakevenAge > 88 {
					t.Errorf("breakeven 69 vs 70 = %d, expected roughly 82-85", r.BreakevenAge)
				}
				break
			}
		}
		if !found {
			t.Error("did not find breakeven result for ages 69 vs 70")
		}
	})

	t.Run("All breakeven ages between claim ages and 100", func(t *testing.T) {
		results := SSBreakevenAges(2000, 67, 0.02)

		for _, r := range results {
			if r.BreakevenAge < r.LateAge || r.BreakevenAge > 100 {
				t.Errorf("breakeven age %d out of range [%d, 100] for early=%d late=%d",
					r.BreakevenAge, r.LateAge, r.EarlyAge, r.LateAge)
			}
		}
	})

	t.Run("Breakeven ages do not have large non-monotonic jumps", func(t *testing.T) {
		// The SSA's two-tier early reduction formula (5/9% per month for the
		// first 36 months, 5/12% beyond) means marginal benefit of waiting
		// isn't perfectly monotonic — pairs crossing the tier boundary can
		// break even slightly earlier. But jumps larger than 3 years indicate
		// a COLA compounding bug (e.g., using different base years for early
		// vs late benefits).
		for _, tc := range []struct {
			pia  float64
			fra  int
			cola float64
		}{
			{2000, 67, 0.02},
			{1500, 66, 0.03},
			{3000, 67, 0.00},
		} {
			results := SSBreakevenAges(tc.pia, tc.fra, tc.cola)
			for i := 1; i < len(results); i++ {
				drop := results[i-1].BreakevenAge - results[i].BreakevenAge
				if drop > 3 {
					t.Errorf("PIA=%.0f FRA=%d COLA=%.0f%%: breakeven %d vs %d = age %d, "+
						"but %d vs %d = age %d (drop of %d years is too large)",
						tc.pia, tc.fra, tc.cola*100,
						results[i-1].EarlyAge, results[i-1].LateAge, results[i-1].BreakevenAge,
						results[i].EarlyAge, results[i].LateAge, results[i].BreakevenAge, drop)
				}
			}
		}
	})
}

func TestSSBreakevenAgesWithSpousalTopUp(t *testing.T) {
	t.Run("Breakeven ages do not have large non-monotonic jumps", func(t *testing.T) {
		// Same COLA-compounding invariant as the non-spousal version.
		// The spousal top-up path previously shared the same bug.
		for _, tc := range []struct {
			pia       float64
			fra       int
			cola      float64
			higherPIA float64
		}{
			{1500, 67, 0.02, 3000},
			{1000, 66, 0.03, 2500},
		} {
			results := SSBreakevenAgesWithSpousalTopUp(tc.pia, tc.fra, tc.cola, tc.higherPIA)
			if len(results) != 8 {
				t.Fatalf("expected 8 results, got %d", len(results))
			}
			for i := 1; i < len(results); i++ {
				// Skip pairs where either never breaks even (0 = never within age 100),
				// which is normal for spousal benefits past FRA.
				if results[i].BreakevenAge == 0 || results[i-1].BreakevenAge == 0 {
					continue
				}
				drop := results[i-1].BreakevenAge - results[i].BreakevenAge
				if drop > 3 {
					t.Errorf("PIA=%.0f higherPIA=%.0f FRA=%d COLA=%.0f%%: "+
						"breakeven %d vs %d = age %d, but %d vs %d = age %d (drop of %d years)",
						tc.pia, tc.higherPIA, tc.fra, tc.cola*100,
						results[i-1].EarlyAge, results[i-1].LateAge, results[i-1].BreakevenAge,
						results[i].EarlyAge, results[i].LateAge, results[i].BreakevenAge, drop)
				}
			}
		}
	})

	t.Run("All breakeven ages in valid range", func(t *testing.T) {
		results := SSBreakevenAgesWithSpousalTopUp(1500, 67, 0.02, 3000)
		for _, r := range results {
			if r.BreakevenAge != 0 && (r.BreakevenAge < r.LateAge || r.BreakevenAge > 100) {
				t.Errorf("breakeven age %d out of range [%d, 100] for early=%d late=%d",
					r.BreakevenAge, r.LateAge, r.EarlyAge, r.LateAge)
			}
		}
	})
}

func TestSSPortfolioEligible(t *testing.T) {
	base := func() *models.WhatIfSettings {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 67
		s.SpouseAge = 54
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", BirthMonth: "1958-11", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", BirthMonth: "1971-08", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       4100,
			FRA:              66,
			COLARate:         0.02,
			SpouseFRABenefit: 154,
			SpouseFRA:        67,
		}
		return s
	}

	t.Run("nil settings", func(t *testing.T) {
		if SSPortfolioEligible(nil) {
			t.Fatal("nil settings should not be eligible")
		}
	})

	t.Run("nil social security config", func(t *testing.T) {
		s := base()
		s.SocialSecurity = nil
		if SSPortfolioEligible(s) {
			t.Fatal("nil config should not be eligible")
		}
	})

	t.Run("no claim ages set", func(t *testing.T) {
		if SSPortfolioEligible(base()) {
			t.Fatal("unset claim ages should not be eligible")
		}
	})

	t.Run("primary claim age equals current age", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 67 // same as CurrentAge
		if SSPortfolioEligible(s) {
			t.Fatal("primary claim age equal to current age should be ineligible (already claiming)")
		}
	})

	t.Run("primary claim age in future", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 68
		if !SSPortfolioEligible(s) {
			t.Fatal("primary claim age in the future should be eligible")
		}
	})

	t.Run("only spouse claim age set", func(t *testing.T) {
		s := base()
		s.SocialSecurity.SpouseClaimAge = 62
		if !SSPortfolioEligible(s) {
			t.Fatal("spouse-only selection should be eligible")
		}
	})

	t.Run("both claim ages set future", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 68
		s.SocialSecurity.SpouseClaimAge = 62
		if !SSPortfolioEligible(s) {
			t.Fatal("dual selection with future ages should be eligible")
		}
	})

	t.Run("primary claim age below current age", func(t *testing.T) {
		s := base()
		s.CurrentAge = 70
		s.SocialSecurity.ClaimAge = 69
		if SSPortfolioEligible(s) {
			t.Fatal("primary claim age below current age should be ineligible")
		}
	})

	t.Run("spouse claim age below spouse age", func(t *testing.T) {
		s := base()
		s.SpouseAge = 65
		s.SocialSecurity.SpouseClaimAge = 62
		if SSPortfolioEligible(s) {
			t.Fatal("spouse claim age below spouse age should be ineligible")
		}
	})

	t.Run("single person primary only future", func(t *testing.T) {
		s := base()
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", BirthMonth: "1958-11", Role: models.PersonRolePrimary},
		}
		s.SpouseAge = 0
		s.SocialSecurity.SpouseFRABenefit = 0
		s.SocialSecurity.ClaimAge = 68
		if !SSPortfolioEligible(s) {
			t.Fatal("single primary with future claim age should be eligible")
		}
	})

	t.Run("selected person with zero fra benefit", func(t *testing.T) {
		s := base()
		s.SocialSecurity.FRABenefit = 0
		s.SocialSecurity.ClaimAge = 68
		if SSPortfolioEligible(s) {
			t.Fatal("selected primary with zero FRA benefit should be ineligible")
		}
	})
}

func TestProjectedSocialSecurityIncome(t *testing.T) {
	t.Run("inactive projection returns zero", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.SocialSecurity = nil
		if got := projectedSocialSecurityIncome(s, 12); got != 0 {
			t.Fatalf("expected 0 for nil SS config, got %.2f", got)
		}
	})

	t.Run("primary with spouse top-up when spouse PIA higher", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 60
		s.SpouseAge = 58
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       1000,  // lower
			FRA:              67,
			COLARate:         0.02,
			ClaimAge:         67,
			SpouseFRABenefit: 4000,  // higher
			SpouseFRA:        67,
			SpouseClaimAge:   67,
		}
		// At month where both are collecting, the primary should get
		// spousal top-up because spouse PIA ($4000) > primary PIA ($1000)
		month := 12 * 10 // both should be past claim age
		got := projectedSocialSecurityIncome(s, month)
		if got <= 0 {
			t.Fatalf("expected positive income, got %.2f", got)
		}

		// Compare without spousal top-up by making PIAs equal
		s2 := *s
		ss2 := *s.SocialSecurity
		ss2.SpouseFRABenefit = 500 // lower than primary, no top-up
		s2.SocialSecurity = &ss2
		got2 := projectedSocialSecurityIncome(&s2, month)
		if got <= got2 {
			t.Fatalf("with spousal top-up (%.2f) should exceed without (%.2f)", got, got2)
		}
	})

	t.Run("spouse gets spousal top-up even when primary already claiming", func(t *testing.T) {
		// Bug 2 + 3 regression. Primary $4100 actual benefit at age 67 with FRA 66
		// → true PIA = 4100 / 1.08 ≈ $3796. Spouse age 54 plans to claim at 62
		// with own PIA $1500. Spousal benefit at 62 (65% of half true PIA) =
		// 0.65 × $1898 ≈ $1234, which exceeds her own reduced benefit ($1500 × 0.70
		// = $1050). The pre-fix code skipped top-up entirely when primary alreadyClaiming.
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 67
		s.SpouseAge = 54
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       4100, // actual benefit (already claiming)
			FRA:              66,
			COLARate:         0.02, // explicit 2% (matches normalized default)
			ClaimAge:         67,   // = CurrentAge → alreadyClaiming
			SpouseFRABenefit: 1500, // spouse's own PIA
			SpouseFRA:        67,
			SpouseClaimAge:   62, // future claim
		}

		spouseStart := (62 - 54) * 12 // month 96
		got := projectedSocialSecurityIncome(s, spouseStart)
		// Primary at month 96: 4100 × 1.02^8 ≈ 4803.95.
		// Spouse top-up at month 96 (just claimed, no COLA yet):
		//   0.65 × 0.5 × DerivedPIA(4100, 66, 67) = 0.65 × 0.5 × 3796.30 ≈ 1233.80.
		want := 4803.95 + 1233.80
		if !withinTolerance(got, want, 5.0) {
			t.Errorf("expected ≈ $%.2f (primary $4804 + spouse w/ top-up ≈ $1234), got %.2f", want, got)
		}
	})

	t.Run("spousal benefit applies SSA reduction not worker reduction", func(t *testing.T) {
		// Worker formula at age 62 / FRA 67 → 30% reduction → 70% × spousalPIA.
		// Spousal formula at age 62 / FRA 67 → 35% reduction → 65% × spousalPIA.
		// With $4000 worker PIA, spousal at 62 should be 0.65 × $2000 = $1300,
		// not 0.70 × $2000 = $1400.
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 67
		s.SpouseAge = 60 // not yet claiming
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       4000, // claim at FRA → actual benefit = PIA
			FRA:              67,
			COLARate:         0.02, // matches normalized default
			ClaimAge:         67,   // = CurrentAge → alreadyClaiming, derived PIA = 4000
			SpouseFRABenefit: 1,    // tiny so spousal top-up dominates
			SpouseFRA:        67,
			SpouseClaimAge:   62, // future claim, 24 months away
		}

		spouseStart := (62 - 60) * 12 // month 24
		got := projectedSocialSecurityIncome(s, spouseStart)
		// Primary at month 24: 4000 × 1.02^2 ≈ 4161.60.
		// Spouse spousal at 62 (just claimed): 0.65 × $2000 = $1300.
		want := 4161.60 + 1300.0
		if !withinTolerance(got, want, 5.0) {
			t.Errorf("expected ≈ $%.0f (worker $4162 + spousal $1300 with SSA reduction), got %.2f",
				want, got)
		}
	})
}

func TestProjectedSSEntries(t *testing.T) {
	t.Run("inactive optimizer returns nil", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.SocialSecurity = nil
		if got := ProjectedSSEntries(s); got != nil {
			t.Fatalf("expected nil for nil SS config, got %+v", got)
		}
	})

	t.Run("primary only when no spouse configured", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 60
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 2000,
			FRA:        67,
			COLARate:   0.02,
			ClaimAge:   67,
		}
		entries := ProjectedSSEntries(s)
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}
		if entries[0].Label != "Your Social Security" {
			t.Errorf("label = %q, want 'Your Social Security'", entries[0].Label)
		}
		if !withinTolerance(entries[0].MonthlyAmount, 2000, 0.01) {
			t.Errorf("amount = %.2f, want 2000", entries[0].MonthlyAmount)
		}
		if entries[0].ClaimAge != 67 {
			t.Errorf("claim age = %d, want 67", entries[0].ClaimAge)
		}
		if entries[0].StartMonth != 7*12 {
			t.Errorf("start month = %d, want %d", entries[0].StartMonth, 7*12)
		}
		if entries[0].AlreadyClaiming {
			t.Error("expected AlreadyClaiming=false")
		}
		if entries[0].SpousalTopUp {
			t.Error("expected SpousalTopUp=false")
		}
	})

	t.Run("primary alreadyClaiming uses entered actual benefit", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 67
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 4100, // actual benefit
			FRA:        66,
			COLARate:   0.02,
			ClaimAge:   67, // = CurrentAge
		}
		entries := ProjectedSSEntries(s)
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}
		if !withinTolerance(entries[0].MonthlyAmount, 4100, 0.01) {
			t.Errorf("amount = %.2f, want 4100 (actual benefit, not adjusted)", entries[0].MonthlyAmount)
		}
		if !entries[0].AlreadyClaiming {
			t.Error("expected AlreadyClaiming=true")
		}
		if entries[0].StartMonth != 0 {
			t.Errorf("start month = %d, want 0", entries[0].StartMonth)
		}
	})

	t.Run("spouse entry includes spousal top-up flag when applicable", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 67
		s.SpouseAge = 54
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       4100,
			FRA:              66,
			COLARate:         0.02,
			ClaimAge:         67,
			SpouseFRABenefit: 1500,
			SpouseFRA:        67,
			SpouseClaimAge:   62,
		}
		entries := ProjectedSSEntries(s)
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries (primary + spouse), got %d", len(entries))
		}
		spouse := entries[1]
		if spouse.Label != "Spouse Social Security" {
			t.Errorf("spouse label = %q, want 'Spouse Social Security'", spouse.Label)
		}
		if !spouse.SpousalTopUp {
			t.Error("expected spousal top-up flag set (own $1500 < spousal ~$1234... actually own > spousal here, recheck)")
		}
		// Spouse own at 62 = 1500 × 0.70 = 1050.
		// Spousal top-up at 62 = 0.65 × 0.5 × DerivedPIA(4100, 66, 67) ≈ 1233.80.
		// Top-up wins → expect ~1234 with flag set.
		if !withinTolerance(spouse.MonthlyAmount, 1233.80, 5.0) {
			t.Errorf("spouse amount = %.2f, want ~1234 (spousal top-up)", spouse.MonthlyAmount)
		}
		if spouse.StartMonth != (62-54)*12 {
			t.Errorf("spouse start month = %d, want %d", spouse.StartMonth, (62-54)*12)
		}
	})

	t.Run("spouse entry without top-up when own benefit higher", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 60
		s.SpouseAge = 58
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       1000,
			FRA:              67,
			COLARate:         0.02,
			ClaimAge:         67,
			SpouseFRABenefit: 4000, // spouse PIA higher → primary should get top-up
			SpouseFRA:        67,
			SpouseClaimAge:   67,
		}
		entries := ProjectedSSEntries(s)
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}
		if !entries[0].SpousalTopUp {
			t.Error("expected primary to receive spousal top-up (PIA $1000 < $2000 spousal)")
		}
		if entries[1].SpousalTopUp {
			t.Error("expected spouse not to receive top-up (own $4000 > $500 primary spousal)")
		}
	})

	t.Run("spouse entry omitted when no spouse claim age set", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 67
		s.SpouseAge = 54
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       4100,
			FRA:              66,
			ClaimAge:         67,
			SpouseFRABenefit: 1500,
			SpouseFRA:        67,
			// SpouseClaimAge is zero → not yet configured
		}
		entries := ProjectedSSEntries(s)
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry (no spouse claim age), got %d", len(entries))
		}
	})
}

func TestRunSSAnalysis(t *testing.T) {
	t.Run("nil SS config returns nil", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.SocialSecurity = nil
		c := NewCalculator(s)
		if result := c.RunSSAnalysis(); result != nil {
			t.Fatal("expected nil for nil SS config")
		}
	})

	t.Run("zero FRA benefit returns nil", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 0}
		c := NewCalculator(s)
		if result := c.RunSSAnalysis(); result != nil {
			t.Fatal("expected nil for zero FRA benefit")
		}
	})

	t.Run("already claiming back-derives PIA", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 65
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 1500, // actual benefit being received
			FRA:        67,
			COLARate:   0.02,
			ClaimAge:   63,
		}
		c := NewCalculator(s)
		result := c.RunSSAnalysis()
		if result == nil {
			t.Fatal("expected analysis")
		}
		// Should use derived PIA, not entered benefit, for comparison
		if result.BestAge != 63 {
			t.Fatalf("already claiming: BestAge = %d, want 63 (current claim)", result.BestAge)
		}
	})

	t.Run("spouse has higher PIA uses spousal top-up for primary", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 60
		s.SpouseAge = 60
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       1000,
			FRA:              67,
			COLARate:         0.02,
			ClaimAge:         67,
			SpouseFRABenefit: 3000, // higher than primary
			SpouseFRA:        67,
			SpouseClaimAge:   67,
		}
		c := NewCalculator(s)
		result := c.RunSSAnalysis()
		if result == nil {
			t.Fatal("expected analysis")
		}
		if len(result.Options) == 0 {
			t.Fatal("expected primary options with spousal top-up")
		}
		if len(result.SpouseOptions) == 0 {
			t.Fatal("expected spouse options")
		}
	})

	t.Run("spouse already claiming uses their claim age", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 60
		s.SpouseAge = 65
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       3000,
			FRA:              67,
			COLARate:         0.02,
			SpouseFRABenefit: 1000,
			SpouseFRA:        67,
			SpouseClaimAge:   63, // <= SpouseAge, already claiming
		}
		c := NewCalculator(s)
		result := c.RunSSAnalysis()
		if result == nil {
			t.Fatal("expected analysis")
		}
		if result.SpouseBestAge != 63 {
			t.Fatalf("SpouseBestAge = %d, want 63 (already claiming)", result.SpouseBestAge)
		}
	})

	t.Run("spouse age zero falls back to current age", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 60
		s.SpouseAge = 0
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       3000,
			FRA:              67,
			COLARate:         0.02,
			SpouseFRABenefit: 1000,
			SpouseFRA:        67,
		}
		c := NewCalculator(s)
		result := c.RunSSAnalysis()
		if result == nil {
			t.Fatal("expected analysis")
		}
		// With spouseAge=0 falling back to currentAge=60, spouse options
		// should include ages 60-70
		if len(result.SpouseOptions) == 0 {
			t.Fatal("expected spouse options with fallback age")
		}
	})
}

func TestRunSSPortfolioAnalysis(t *testing.T) {
	base := func() *models.WhatIfSettings {
		s := models.DefaultWhatIfSettings()
		s.StartDate = "2026-04"
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 5000
		s.TaxDeferredPercent = 60
		s.RothPercent = 10
		s.ProjectionYears = 15
		s.CurrentAge = 67
		s.SpouseAge = 54
		s.Persons = []models.Person{
			{ID: "p1", Name: "You", BirthMonth: "1958-11", Role: models.PersonRolePrimary},
			{ID: "p2", Name: "Spouse", BirthMonth: "1971-08", Role: models.PersonRoleSpouse},
		}
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit:       4100,
			FRA:              66,
			COLARate:         0.02,
			SpouseFRABenefit: 154,
			SpouseFRA:        67,
		}
		return s
	}

	t.Run("not eligible returns nil", func(t *testing.T) {
		c := NewCalculator(base())
		if result := c.RunSSPortfolioAnalysis(c.RunSSAnalysis()); result != nil {
			t.Fatal("expected nil when no claim ages are selected")
		}
	})

	t.Run("nil ssAnalysis returns nil", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 68
		c := NewCalculator(s)
		if result := c.RunSSPortfolioAnalysis(nil); result != nil {
			t.Fatal("expected nil for nil ssAnalysis")
		}
	})

	t.Run("spouse only varies spouse ages", func(t *testing.T) {
		s := base()
		s.SocialSecurity.SpouseClaimAge = 62
		c := NewCalculator(s)
		result := c.RunSSPortfolioAnalysis(c.RunSSAnalysis())
		if result == nil {
			t.Fatal("expected analysis")
		}
		if len(result.PrimaryOptions) != 0 {
			t.Fatalf("expected no primary options, got %d", len(result.PrimaryOptions))
		}
		if len(result.SpouseOptions) != 9 {
			t.Fatalf("expected 9 spouse options, got %d", len(result.SpouseOptions))
		}
		if result.MonteCarloRuns != ssPortfolioMonteCarloRuns {
			t.Fatalf("MonteCarloRuns = %d, want %d", result.MonteCarloRuns, ssPortfolioMonteCarloRuns)
		}
		for _, opt := range result.SpouseOptions {
			if opt.ClaimAge < 62 || opt.ClaimAge > 70 {
				t.Fatalf("unexpected spouse claim age %d", opt.ClaimAge)
			}
			if opt.SurvivalRate < 0 || opt.SurvivalRate > 100 {
				t.Fatalf("spouse survival rate out of range: %.2f", opt.SurvivalRate)
			}
		}
		if result.OptimalSpouseAge < 62 || result.OptimalSpouseAge > 70 {
			t.Fatalf("optimal spouse age %d out of range", result.OptimalSpouseAge)
		}
	})

	t.Run("primary only varies primary ages", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 68 // must be > CurrentAge (67)
		c := NewCalculator(s)
		result := c.RunSSPortfolioAnalysis(c.RunSSAnalysis())
		if result == nil {
			t.Fatal("expected analysis")
		}
		if len(result.SpouseOptions) != 0 {
			t.Fatalf("expected no spouse options, got %d", len(result.SpouseOptions))
		}
		if len(result.PrimaryOptions) != 4 {
			t.Fatalf("expected 4 primary options, got %d", len(result.PrimaryOptions))
		}
		for _, opt := range result.PrimaryOptions {
			if opt.ClaimAge < 67 {
				t.Fatalf("unexpected primary claim age %d", opt.ClaimAge)
			}
		}
	})

	t.Run("both selected returns both tables", func(t *testing.T) {
		s := base()
		s.SocialSecurity.ClaimAge = 68 // must be > CurrentAge (67)
		s.SocialSecurity.SpouseClaimAge = 62
		c := NewCalculator(s)
		result := c.RunSSPortfolioAnalysis(c.RunSSAnalysis())
		if result == nil {
			t.Fatal("expected analysis")
		}
		if len(result.PrimaryOptions) == 0 || len(result.SpouseOptions) == 0 {
			t.Fatalf("expected both primary and spouse options, got %d and %d", len(result.PrimaryOptions), len(result.SpouseOptions))
		}
		if result.BaselineSurvivalRate < 0 || result.BaselineSurvivalRate > 100 {
			t.Fatalf("baseline survival rate out of range: %.2f", result.BaselineSurvivalRate)
		}
	})

	t.Run("baseline delta is zero for selected age", func(t *testing.T) {
		s := base()
		s.SocialSecurity.SpouseClaimAge = 62
		c := NewCalculator(s)
		result := c.RunSSPortfolioAnalysis(c.RunSSAnalysis())
		if result == nil {
			t.Fatal("expected analysis")
		}
		found := false
		for _, opt := range result.SpouseOptions {
			if opt.ClaimAge == 62 {
				found = true
				if !withinTolerance(opt.DeltaSurvivalRate, 0, 0.0001) {
					t.Fatalf("baseline delta = %.6f, want 0", opt.DeltaSurvivalRate)
				}
			}
		}
		if !found {
			t.Fatal("selected spouse age 62 not found in options")
		}
	})

	t.Run("monthly benefits match SS comparison table", func(t *testing.T) {
		s := base()
		s.SocialSecurity.SpouseClaimAge = 62
		c := NewCalculator(s)
		ssAnalysis := c.RunSSAnalysis()
		if ssAnalysis == nil {
			t.Fatal("expected SS analysis")
		}
		result := c.RunSSPortfolioAnalysis(ssAnalysis)
		if result == nil {
			t.Fatal("expected analysis")
		}

		expected := make(map[int]float64, len(ssAnalysis.SpouseOptions))
		for _, opt := range ssAnalysis.SpouseOptions {
			expected[opt.ClaimAge] = opt.MonthlyBenefit
		}
		for _, opt := range result.SpouseOptions {
			if !withinTolerance(opt.MonthlyBenefit, expected[opt.ClaimAge], 0.01) {
				t.Fatalf("age %d monthly benefit = %.2f, want %.2f", opt.ClaimAge, opt.MonthlyBenefit, expected[opt.ClaimAge])
			}
		}
	})
}

func TestCloneSettingsWithClaimAges(t *testing.T) {
	t.Run("nil calculator returns nil", func(t *testing.T) {
		var c *Calculator
		if got := c.cloneSettingsWithClaimAges(67, 65); got != nil {
			t.Fatal("expected nil for nil calculator")
		}
	})

	t.Run("minimal settings without optional configs", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 2000,
			FRA:        67,
			ClaimAge:   67,
		}
		// Ensure optional configs are nil
		s.SpendingPhaseConfig = nil
		s.TaxConfig = nil
		s.RothConversion = nil
		s.GlidePath = nil
		s.Guardrails = nil

		c := NewCalculator(s)
		clone := c.cloneSettingsWithClaimAges(68, 65)
		if clone == nil {
			t.Fatal("expected non-nil clone")
		}
		if clone.SocialSecurity.ClaimAge != 68 {
			t.Fatalf("ClaimAge = %d, want 68", clone.SocialSecurity.ClaimAge)
		}
		if clone.SocialSecurity.SpouseClaimAge != 65 {
			t.Fatalf("SpouseClaimAge = %d, want 65", clone.SocialSecurity.SpouseClaimAge)
		}
	})

	t.Run("full settings clones all sub-configs", func(t *testing.T) {
		s := models.DefaultWhatIfSettings()
		s.SocialSecurity = &models.SocialSecurityConfig{
			FRABenefit: 2000,
			FRA:        67,
			ClaimAge:   67,
		}
		s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
			Phases: []models.SpendingPhase{{Name: "go-go", Multiplier: 1.0}},
		}
		s.TaxConfig = &models.TaxConfig{FilingStatus: "married"}
		s.RothConversion = &models.RothConversionConfig{AnnualAmount: 50000}
		s.GlidePath = &models.GlidePathConfig{Enabled: true}
		s.Guardrails = &models.GuardrailConfig{Enabled: true}

		c := NewCalculator(s)
		clone := c.cloneSettingsWithClaimAges(70, 62)
		if clone == nil {
			t.Fatal("expected non-nil clone")
		}

		// Verify deep copy — mutating clone shouldn't affect original
		clone.SocialSecurity.FRABenefit = 9999
		if s.SocialSecurity.FRABenefit == 9999 {
			t.Fatal("clone shares SocialSecurity pointer with original")
		}
		clone.SpendingPhaseConfig.Phases[0].Name = "mutated"
		if s.SpendingPhaseConfig.Phases[0].Name == "mutated" {
			t.Fatal("clone shares SpendingPhaseConfig.Phases with original")
		}
	})
}

func TestCumulativeBenefit(t *testing.T) {
	t.Run("target at or before claim age returns zero", func(t *testing.T) {
		if got := cumulativeBenefit(2000, 67, 67, 0.02); got != 0 {
			t.Fatalf("same age: got %.2f, want 0", got)
		}
		if got := cumulativeBenefit(2000, 67, 60, 0.02); got != 0 {
			t.Fatalf("target before claim: got %.2f, want 0", got)
		}
	})

	t.Run("positive accumulation", func(t *testing.T) {
		got := cumulativeBenefit(2000, 65, 67, 0.02)
		if got <= 0 {
			t.Fatalf("expected positive cumulative, got %.2f", got)
		}
	})
}

func TestBestSSPortfolioOption(t *testing.T) {
	t.Run("empty options returns false", func(t *testing.T) {
		_, ok := bestSSPortfolioOption(nil)
		if ok {
			t.Fatal("expected false for empty options")
		}
	})

	t.Run("selects by survival rate then median balance then earlier age", func(t *testing.T) {
		options := []models.SSPortfolioOption{
			{ClaimAge: 67, SurvivalRate: 88.0, MedianEndingBalance: 500_000},
			{ClaimAge: 68, SurvivalRate: 91.0, MedianEndingBalance: 450_000},
			{ClaimAge: 69, SurvivalRate: 91.0, MedianEndingBalance: 475_000},
			{ClaimAge: 70, SurvivalRate: 91.0, MedianEndingBalance: 475_000},
		}

		best, ok := bestSSPortfolioOption(options)
		if !ok {
			t.Fatal("expected best option")
		}
		if best.ClaimAge != 69 {
			t.Fatalf("best claim age = %d, want 69", best.ClaimAge)
		}
	})

	t.Run("equal survival and median picks earlier age", func(t *testing.T) {
		options := []models.SSPortfolioOption{
			{ClaimAge: 68, SurvivalRate: 90.0, MedianEndingBalance: 500_000},
			{ClaimAge: 66, SurvivalRate: 90.0, MedianEndingBalance: 500_000},
		}
		best, ok := bestSSPortfolioOption(options)
		if !ok {
			t.Fatal("expected best option")
		}
		if best.ClaimAge != 66 {
			t.Fatalf("best claim age = %d, want 66 (earlier)", best.ClaimAge)
		}
	})
}

// F-026: explicit zero COLA must be honored, not silently substituted.
func TestNormalizedSSCOLARate_F026_ExplicitZero(t *testing.T) {
	zero := 0.0
	got := normalizedSSCOLARate(&zero)
	if got != 0.0 {
		t.Errorf("explicit zero COLA = %.4f; want 0.0", got)
	}
}

func TestNormalizedSSCOLARate_F026_UnsetUsesDefault(t *testing.T) {
	got := normalizedSSCOLARate(nil)
	want := 0.02 // 2% default (as decimal) when caller did not supply a value
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("unset COLA = %.4f; want %.4f (default)", got, want)
	}
}

func TestNormalizedSSCOLARate_F026_NegativeClamped(t *testing.T) {
	neg := -1.0
	got := normalizedSSCOLARate(&neg)
	if got != 0.0 {
		t.Errorf("negative COLA = %.4f; want 0.0 (SS COLA never negative)", got)
	}
}

func TestNormalizedSSCOLARate_F026_PositiveValuePreserved(t *testing.T) {
	v := 3.5
	got := normalizedSSCOLARate(&v)
	if math.Abs(got-3.5) > 1e-9 {
		t.Errorf("positive COLA = %.4f; want 3.5", got)
	}
}

// F-029: When the primary is already claiming at a non-FRA age, the
// SpouseUsingSpousalBenefit flag must be derived from the primary PIA
// (back-derived from FRABenefit + claim age + FRA), not from the raw
// FRABenefit.  Pre-fix: ss.FRABenefit*0.5 underestimates the spousal
// entitlement, which can cause the flag to be false when the spousal
// benefit actually exceeds the spouse's own benefit.
//
// Setup:
//   Primary: FRABenefit=$1,000 at claim 62, FRA 67, already claiming.
//   PIA = 1000 / 0.70 ≈ 1428.57  (30% early-claim reduction).
//   Spousal entitlement at FRA = PIA × 0.5 ≈ 714.28 > SpouseFRABenefit 600.
//   → SpouseUsingSpousalBenefit should be TRUE.
//
//   Buggy path: 1000 × 0.5 = 500 < 600 → flag set FALSE (wrong).
func TestRunSSAnalysis_F029_SpousalUsesPrimaryPIA(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 67
	s.SpouseAge = 62
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit:       1000.0, // actual benefit at claim 62; PIA ≈ 1428.57
		FRA:              67,
		COLARate:         0.02,
		ClaimAge:         62, // primary already claiming at 62
		SpouseFRABenefit: 600.0, // spouse own PIA; not yet claiming
		SpouseFRA:        67,
		// SpouseClaimAge intentionally zero — spouse not yet claiming
	}
	calc := NewCalculator(s)
	analysis := calc.RunSSAnalysis()
	if analysis == nil {
		t.Fatal("expected non-nil SS analysis")
	}
	if !analysis.SpouseUsingSpousalBenefit {
		t.Errorf("SpouseUsingSpousalBenefit = false; want true "+
			"(primaryPIA≈1428.57 × 0.5 ≈ 714 > SpouseFRABenefit 600). "+
			"Bug: ss.FRABenefit(1000) × 0.5 = 500 < 600 gives wrong false.")
	}
}
