package models

import (
	"math"
	"testing"
)

func TestHealthcarePersonMonthlyCompounding(t *testing.T) {
	t.Run("pre medicare compounds monthly", func(t *testing.T) {
		person := HealthcarePerson{
			CurrentAge:           60,
			CurrentCoverage:      CoverageACA,
			CurrentMonthlyCost:   1000,
			PreMedicareInflation: 12,
			MedicareMonthlyCost:  400,
			MedicareEligibleAge:  65,
		}

		got := person.GetMonthlyCost(6)
		want := 1000.0 * math.Pow(1.12, 0.5)
		if math.Abs(got-want) > 0.01 {
			t.Fatalf("month 6: want %.2f, got %.2f", want, got)
		}
	})

	t.Run("post medicare compounds monthly after transition", func(t *testing.T) {
		person := HealthcarePerson{
			CurrentAge:            64,
			CurrentCoverage:       CoverageACA,
			CurrentMonthlyCost:    1000,
			PreMedicareInflation:  12,
			MedicareMonthlyCost:   400,
			PostMedicareInflation: 6,
			MedicareEligibleAge:   65,
		}

		got := person.GetMonthlyCost(18)
		want := 400.0 * math.Pow(1.06, 0.5)
		if math.Abs(got-want) > 0.01 {
			t.Fatalf("month 18: want %.2f, got %.2f", want, got)
		}
	})
}

// F-067 tests — month-precise ACA→Medicare transition

// TestHealthcareCost_F067_TransitionAtBirthMonth verifies that a person born
// in July turns 65 in month 6 of the projection (not month 12) when
// BirthMonth and startDate are provided.
//
// Setup: projection starts 2026-01, person born 1961-07 → age 64 at start.
//   Months until 65 = (1961+65=2026-07 - 2026-01) = 6 months.
//   So months 0–5 should use ACA cost; month 6+ should use Medicare cost.
func TestHealthcareCost_F067_TransitionAtBirthMonth(t *testing.T) {
	person := HealthcarePerson{
		BirthMonth:            "1961-07", // born July 1961
		CurrentAge:            64,        // 64 at projection start 2026-01
		CurrentCoverage:       CoverageACA,
		CurrentMonthlyCost:    1100,
		PreMedicareInflation:  0, // zero inflation for predictable math
		MedicareMonthlyCost:   600,
		PostMedicareInflation: 0,
		MedicareEligibleAge:   65,
	}
	startDate := "2026-01"

	// Months 0–5: ACA cost ($1100, no inflation).
	for m := 0; m <= 5; m++ {
		got := person.GetMonthlyCostAt(m, startDate)
		if math.Abs(got-1100) > 0.01 {
			t.Errorf("month %d (pre-Medicare): want $1100, got $%.2f", m, got)
		}
	}
	// Month 6+: Medicare cost ($600, no inflation).
	for m := 6; m <= 12; m++ {
		got := person.GetMonthlyCostAt(m, startDate)
		if math.Abs(got-600) > 0.01 {
			t.Errorf("month %d (post-Medicare): want $600, got $%.2f", m, got)
		}
	}
}

// TestHealthcareCost_F067_YearBucketWouldFail verifies the bug that F-067 fixes:
// without BirthMonth, a year-bucket approach transitions at month 12, not month 6.
// With BirthMonth="1961-07" and startDate="2026-01", transition is at month 6.
func TestHealthcareCost_F067_YearBucketWouldFail(t *testing.T) {
	person := HealthcarePerson{
		BirthMonth:            "1961-07",
		CurrentAge:            64,
		CurrentCoverage:       CoverageACA,
		CurrentMonthlyCost:    1100,
		PreMedicareInflation:  0,
		MedicareMonthlyCost:   600,
		PostMedicareInflation: 0,
		MedicareEligibleAge:   65,
	}

	// With month-precise fix: month 6 is Medicare.
	withFix := person.GetMonthlyCostAt(6, "2026-01")
	if math.Abs(withFix-600) > 0.01 {
		t.Errorf("F-067 fix: month 6 want $600 (Medicare), got $%.2f", withFix)
	}

	// Without fix (no startDate): legacy year-bucket, month 6 still ACA (< 12 months).
	withoutFix := person.GetMonthlyCostAt(6, "")
	if math.Abs(withoutFix-1100) > 0.01 {
		t.Errorf("legacy year-bucket: month 6 want $1100 (ACA), got $%.2f", withoutFix)
	}
}

// TestHealthcareCost_F067_LegacyFallback confirms year-based fallback still works
// when BirthMonth or startDate is empty.
func TestHealthcareCost_F067_LegacyFallback(t *testing.T) {
	person := HealthcarePerson{
		// No BirthMonth.
		CurrentAge:            64,
		CurrentCoverage:       CoverageACA,
		CurrentMonthlyCost:    1100,
		PreMedicareInflation:  0,
		MedicareMonthlyCost:   600,
		PostMedicareInflation: 0,
		MedicareEligibleAge:   65,
	}
	// Year-based: transition at month 12 (1 full year).
	if got := person.GetMonthlyCostAt(11, "2026-01"); math.Abs(got-1100) > 0.01 {
		t.Errorf("legacy month 11: want $1100, got $%.2f", got)
	}
	if got := person.GetMonthlyCostAt(12, "2026-01"); math.Abs(got-600) > 0.01 {
		t.Errorf("legacy month 12: want $600, got $%.2f", got)
	}
}

// TestHealthcareCost_F067_GetTotalHealthcareCostUsesStartDate confirms that
// WhatIfSettings.GetTotalHealthcareCost passes StartDate to GetMonthlyCostAt
// for month-precise F-067 transitions.
func TestHealthcareCost_F067_GetTotalHealthcareCostUsesStartDate(t *testing.T) {
	s := &WhatIfSettings{
		StartDate: "2026-01",
		HealthcarePersons: []HealthcarePerson{
			{
				BirthMonth:            "1961-07", // turns 65 in month 6
				CurrentAge:            64,
				CurrentCoverage:       CoverageACA,
				CurrentMonthlyCost:    1100,
				PreMedicareInflation:  0,
				MedicareMonthlyCost:   600,
				PostMedicareInflation: 0,
				MedicareEligibleAge:   65,
			},
		},
	}

	// Month 5: ACA.
	if got := s.GetTotalHealthcareCost(5); math.Abs(got-1100) > 0.01 {
		t.Errorf("month 5: want $1100, got $%.2f", got)
	}
	// Month 6: Medicare.
	if got := s.GetTotalHealthcareCost(6); math.Abs(got-600) > 0.01 {
		t.Errorf("month 6: want $600, got $%.2f", got)
	}
}
