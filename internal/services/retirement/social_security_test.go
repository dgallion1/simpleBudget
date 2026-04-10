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

	t.Run("early claim reduces spousal amount", func(t *testing.T) {
		got := SpousalTopUp(500, 4000, 67, 62)
		want := AdjustedSSBenefit(2000, 67, 62)
		if !withinTolerance(got, want, 0.01) {
			t.Fatalf("SpousalTopUp = %.2f, want %.2f", got, want)
		}
	})

	t.Run("delayed claim does not add delayed credits to spousal component", func(t *testing.T) {
		got := SpousalTopUp(1000, 4000, 67, 70)
		if got != 2000 {
			t.Fatalf("SpousalTopUp delayed claim = %.2f, want capped 2000", got)
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

func TestBestSSPortfolioOption(t *testing.T) {
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
}
