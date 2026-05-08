package retirement

import (
	"math"
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

	proj := newTestCalc(t, s).RunProjection()
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
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, s.CurrentAge)
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := newTestCalc(t, s).RunProjection()
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
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, s.CurrentAge)
	s.Persons = append(s.Persons, models.Person{
		ID:         "spouse-test",
		Name:       "Spouse",
		Role:       models.PersonRoleSpouse,
		BirthMonth: models.BirthMonthForAge(s.StartDate, s.SpouseAge),
	})
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := newTestCalc(t, s).RunProjection()
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

// F-077 fixup: when Person.BirthMonth is set, applicable age must derive from
// the actual birth year, not the floor'd integer age. Prior code used
// startYear - GetOlderAge(); for a Dec 1959 birthday and a Jan 2026 start,
// the floor'd age is 66 → derived birth year 1960 → wrong answer 75. Real
// birth year 1959 should keep applicable age at 73 per SECURE 2.0.
func TestEffectiveRMDStartAge_F077Fixup_BirthMonthBeatsAgeFloor(t *testing.T) {
	cases := []struct {
		name             string
		primaryBirth     string
		spouseBirth      string
		startDate        string
		wantApplicable   int
		wantOlderAgeOnly int // documents legacy fallback to assert it would have failed
	}{
		{
			name:             "primary born Dec 1959, Jan 2026 start → 73 (was 75)",
			primaryBirth:     "1959-12",
			startDate:        "2026-01",
			wantApplicable:   73,
			wantOlderAgeOnly: 75,
		},
		{
			name:             "primary born Jan 1959, Jan 2026 start → 73 (matches legacy)",
			primaryBirth:     "1959-01",
			startDate:        "2026-01",
			wantApplicable:   73,
			wantOlderAgeOnly: 73,
		},
		{
			name:             "primary born Dec 1960, Jan 2026 start → 75 (right answer either way)",
			primaryBirth:     "1960-12",
			startDate:        "2026-01",
			wantApplicable:   75,
			wantOlderAgeOnly: 75,
		},
		{
			name:             "older spouse born Nov 1959, primary 1965 → 73 (was 75)",
			primaryBirth:     "1965-06",
			spouseBirth:      "1959-11",
			startDate:        "2026-01",
			wantApplicable:   73,
			wantOlderAgeOnly: 75,
		},
		{
			name:             "both born 1959 with late months → 73 (was 75)",
			primaryBirth:     "1959-12",
			spouseBirth:      "1959-11",
			startDate:        "2026-01",
			wantApplicable:   73,
			wantOlderAgeOnly: 75,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.StartDate = c.startDate
			s.Persons = []models.Person{
				{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: c.primaryBirth},
			}
			if c.spouseBirth != "" {
				s.Persons = append(s.Persons, models.Person{
					ID: "s1", Name: "Spouse", Role: models.PersonRoleSpouse, BirthMonth: c.spouseBirth,
				})
			}
			s.ComputeAges()

			got := EffectiveRMDStartAge(s)
			if got != c.wantApplicable {
				t.Errorf("EffectiveRMDStartAge = %d; want %d (CurrentAge=%d, SpouseAge=%d)",
					got, c.wantApplicable, s.CurrentAge, s.SpouseAge)
			}
		})
	}
}

// F-077 fixup: when no Person has BirthMonth set (legacy code paths that
// build WhatIfSettings with raw CurrentAge/SpouseAge), the function must
// preserve the existing startYear - olderAge derivation so older callers
// keep working.
func TestEffectiveRMDStartAge_F077Fixup_LegacyFallbackUnchanged(t *testing.T) {
	s := &models.WhatIfSettings{
		StartDate:  "2026-01",
		CurrentAge: 66, // legacy: no BirthMonth, age implies birth year 1960 → 75
	}
	if got := EffectiveRMDStartAge(s); got != 75 {
		t.Errorf("legacy fallback (no BirthMonth, age 66, 2026 start) = %d; want 75", got)
	}
}

// F-078: a primary born 1959-12 with StartDate=2026-01 must trigger RMD in
// calendar year 2032 (year offset 6) — they attain age 73 in Dec 2032,
// applicable age 73, first RMD year = 1959 + 73 = 2032. Pre-F-078 the
// projection gate read olderAge=72 in year 6 and slipped RMD to 2033.
// The trigger-year divisor must use age 73 (UL Table factor 26.5),
// not the floor'd age 72 (factor 27.4).
func TestProjection_F078_Born1959_12_TriggersRMDIn2032(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: "1959-12"},
	}
	s.ComputeAges()
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.MonthlyLivingExpenses = 0
	s.InvestmentReturn = 0
	s.InflationRate = 0
	s.ProjectionYears = 9
	s.RMDTiming = models.RMDTimingStartOfYear

	proj := newTestCalc(t, s).RunProjection()
	if proj == nil || len(proj.Months) < 7*12 {
		t.Fatalf("nil/short projection: months=%d", func() int {
			if proj == nil {
				return 0
			}
			return len(proj.Months)
		}())
	}

	// Year offset 5 (calendar 2031, age 72): NO RMD.
	for m := 5 * 12; m < 6*12; m++ {
		if proj.Months[m].RMDWithdrawal != 0 {
			t.Errorf("month %d (year 5, calendar 2031) RMDWithdrawal = %.2f; want 0",
				m, proj.Months[m].RMDWithdrawal)
		}
	}

	// Year offset 6 (calendar 2032, attains 73): RMD must fire.
	year6Total := 0.0
	for m := 6 * 12; m < 7*12; m++ {
		year6Total += proj.Months[m].RMDWithdrawal
	}
	if year6Total <= 0 {
		t.Fatalf("year-6 (calendar 2032) total RMDWithdrawal = %.2f; want > 0 (born 1959-12, attains 73 in 2032)", year6Total)
	}

	// Divisor must use age 73 (factor 26.5), not 72 (27.4). The projection's
	// InvestmentReturn=0 sentinel triggers allocation-based blended returns,
	// so the year-6 starting balance is whatever the projection compounded
	// to. Use the ratio test: with start-of-year RMD timing the entire
	// annual RMD is withdrawn in month 72 from a balance very close to
	// proj.Months[71].TaxDeferredBalance (last month of year 5). If the
	// gate had used age 72 (factor 27.4) the divisor would yield ~3% less.
	year5End := proj.Months[71].TaxDeferredBalance
	if year5End <= 0 {
		t.Fatalf("year-5 ending tax-deferred balance = %.2f; expected > 0", year5End)
	}
	impliedDivisor := year5End / year6Total
	// age 73 → 26.5, age 72 → 27.4. Allow a small slack for the same-month
	// growth applied between the start-of-month balance and the trigger.
	if math.Abs(impliedDivisor-26.5) > 0.2 {
		t.Errorf("implied RMD divisor = %.3f (year5End=%.2f, RMD=%.2f); want ~26.5 (age 73), not ~27.4 (age 72)",
			impliedDivisor, year5End, year6Total)
	}
}
