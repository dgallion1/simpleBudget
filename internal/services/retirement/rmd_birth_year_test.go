package retirement

import (
	"testing"

	"budget2/internal/models"
)

// F-077: a 2026-start projection with the older spouse aged 65 (born 1961)
// MUST NOT trigger RMD at age 73 in projection year 8 (calendar 2034) because
// the person attains age 73 after Dec 31, 2032 — applicable age is 75 per
// SECURE 2.0 §107 / IRS Notice 2023-23. RMD must start in projection year 10
// (calendar 2036) when the person turns 75.
func TestProjection_F077_BornAfter1959ReachesRMDAt75(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 65
	s.SpouseAge = 0
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 12
	s.StartDate = "2026-01"
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).RunProjection()
	if proj == nil || len(proj.Months) < 12*12 {
		t.Fatalf("nil/short projection: months=%d", func() int {
			if proj == nil {
				return 0
			}
			return len(proj.Months)
		}())
	}

	// Year 8 (months 96..107, calendar 2034, person aged 73): NO RMD allowed.
	for m := 96; m < 108; m++ {
		if proj.Months[m].RMDWithdrawal != 0 {
			t.Errorf("month %d (year 8, age 73, calendar 2034) RMDWithdrawal = %.2f; want 0 — born 1961 must wait until 75",
				m, proj.Months[m].RMDWithdrawal)
		}
	}

	// Year 9 (months 108..119, calendar 2035, person aged 74): NO RMD allowed.
	for m := 108; m < 120; m++ {
		if proj.Months[m].RMDWithdrawal != 0 {
			t.Errorf("month %d (year 9, age 74) RMDWithdrawal = %.2f; want 0", m, proj.Months[m].RMDWithdrawal)
		}
	}

	// Year 10 (months 120..131, calendar 2036, person aged 75): RMD MUST trigger.
	var year10Total float64
	for m := 120; m < 132; m++ {
		year10Total += proj.Months[m].RMDWithdrawal
	}
	if year10Total <= 0 {
		t.Errorf("year-10 total RMDWithdrawal = %.2f; want > 0 (person turns 75 in 2036, applicable age 75)", year10Total)
	}
}

// F-077: a 2026-start projection with the older spouse aged 67 (born 1959)
// MUST trigger RMD at age 73 in projection year 6 (calendar 2032) because the
// person attains 73 in 2032 — applicable age stays at 73 per SECURE 2.0.
func TestProjection_F077_BornBefore1960ReachesRMDAt73(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 67
	s.SpouseAge = 0
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 10
	s.StartDate = "2026-01"
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).RunProjection()
	if proj == nil || len(proj.Months) < 7*12 {
		t.Fatal("nil/short projection")
	}

	// Years 0..5 (months 0..71, ages 67..72): no RMD.
	for m := 0; m < 72; m++ {
		if proj.Months[m].RMDWithdrawal != 0 {
			t.Errorf("month %d (year %d, age %d) RMDWithdrawal = %.2f; want 0",
				m, m/12, 67+m/12, proj.Months[m].RMDWithdrawal)
		}
	}

	// Year 6 (months 72..83, calendar 2032, person aged 73): RMD MUST trigger.
	var year6Total float64
	for m := 72; m < 84; m++ {
		year6Total += proj.Months[m].RMDWithdrawal
	}
	if year6Total <= 0 {
		t.Errorf("year-6 total RMDWithdrawal = %.2f; want > 0 (person turns 73 in 2032, applicable age 73)", year6Total)
	}
}

// F-077: when the older person in a couple is the spouse, applicability uses
// the spouse's birth year (because GetOlderAge() returns the spouse's age).
func TestProjection_F077_OlderSpouseDrivesRMDAge(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 60
	s.SpouseAge = 70 // older; born 1956 → applicable age 73
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 5
	s.StartDate = "2026-01"
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := NewCalculator(s).RunProjection()
	if proj == nil || len(proj.Months) < 4*12 {
		t.Fatal("nil/short projection")
	}

	// Year 3 (months 36..47, calendar 2029, spouse aged 73): RMD MUST trigger.
	var year3Total float64
	for m := 36; m < 48; m++ {
		year3Total += proj.Months[m].RMDWithdrawal
	}
	if year3Total <= 0 {
		t.Errorf("year-3 RMDWithdrawal = %.2f; want > 0 (spouse turns 73 in 2029, applicable age 73)", year3Total)
	}
}

// F-077: birth-year boundary — exactly the SECURE 2.0 cusp.
func TestEffectiveRMDStartAge_F077_BirthYearBoundary(t *testing.T) {
	cases := []struct {
		name      string
		startYear string
		age       int
		want      int
	}{
		{"born 1959 (attains 73 in 2032) → 73", "2026-01", 67, 73},
		{"born 1960 (attains 73 in 2033) → 75", "2026-01", 66, 75},
		{"born 1958 (attains 73 in 2031) → 73", "2033-01", 75, 73},
		{"born 1961 (attains 73 in 2034) → 75", "2026-01", 65, 75},
		{"born 1953 (attains 73 in 2026) → 73", "2026-01", 73, 73},
	}
	for _, c := range cases {
		s := &models.WhatIfSettings{
			StartDate:  c.startYear,
			CurrentAge: c.age,
		}
		got := EffectiveRMDStartAge(s)
		if got != c.want {
			t.Errorf("%s: EffectiveRMDStartAge = %d; want %d", c.name, got, c.want)
		}
	}
}
