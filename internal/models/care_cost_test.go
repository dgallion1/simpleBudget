package models

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// CC1: late-life care cost tests.

func TestCareCostAt_ZeroConfigReturnsZero(t *testing.T) {
	t.Run("CareStartAge unset", func(t *testing.T) {
		hp := HealthcarePerson{CurrentAge: 70, CareMonthlyCost: 8000}
		if got := hp.CareCostAt(240, "2026-01"); got != 0 {
			t.Fatalf("got %f, want 0", got)
		}
	})

	t.Run("CareMonthlyCost unset", func(t *testing.T) {
		hp := HealthcarePerson{CurrentAge: 70, CareStartAge: 85}
		if got := hp.CareCostAt(240, "2026-01"); got != 0 {
			t.Fatalf("got %f, want 0", got)
		}
	})

	t.Run("CareMonthlyCost negative", func(t *testing.T) {
		hp := HealthcarePerson{CurrentAge: 70, CareStartAge: 85, CareMonthlyCost: -100}
		if got := hp.CareCostAt(240, "2026-01"); got != 0 {
			t.Fatalf("got %f, want 0", got)
		}
	})
}

// TestCareCostAt_YearFallbackStartMonth verifies the year-based fallback
// (no BirthMonth) starts care at (CareStartAge-CurrentAge)*12, clamped at 0.
func TestCareCostAt_YearFallbackStartMonth(t *testing.T) {
	hp := HealthcarePerson{
		CurrentAge:            70,
		CareStartAge:          85,
		CareMonthlyCost:       8000,
		PostMedicareInflation: 4.0,
	}
	// (85-70)*12 = 180 months until care starts.
	if got := hp.CareCostAt(179, ""); got != 0 {
		t.Fatalf("month 179 (before start): got %f, want 0", got)
	}
	if got := hp.CareCostAt(180, ""); got == 0 {
		t.Fatalf("month 180 (start): got 0, want nonzero")
	}
}

// TestCareCostAt_YearFallbackClampsAtZero verifies a CareStartAge already
// reached (or before CurrentAge) clamps the start month to 0, not negative.
func TestCareCostAt_YearFallbackClampsAtZero(t *testing.T) {
	hp := HealthcarePerson{
		CurrentAge:            90,
		CareStartAge:          85, // already past
		CareMonthlyCost:       8000,
		PostMedicareInflation: 4.0,
	}
	want := 8000.0 // month 0, no inflation yet
	if got := hp.CareCostAt(0, ""); math.Abs(got-want) > 0.01 {
		t.Fatalf("month 0: got %f, want %f", got, want)
	}
}

// TestCareCostAt_BirthMonthPrecise verifies month-precise start using
// BirthMonth + startDate, mirroring monthsUntilMedicareEligible (F-067).
//
// Setup: projection starts 2026-01, person born 1961-07 → age 64 at start.
// CareStartAge 85 → reached in 1961-07 + 85y = 2046-07.
// Months from 2026-01 to 2046-07 = 20*12 + 6 = 246.
func TestCareCostAt_BirthMonthPrecise(t *testing.T) {
	hp := HealthcarePerson{
		BirthMonth:            "1961-07",
		CurrentAge:            64,
		CareStartAge:          85,
		CareMonthlyCost:       8000,
		PostMedicareInflation: 4.0,
	}
	if got := hp.CareCostAt(245, "2026-01"); got != 0 {
		t.Fatalf("month 245 (before birth-month-precise start): got %f, want 0", got)
	}
	if got := hp.CareCostAt(246, "2026-01"); got == 0 {
		t.Fatalf("month 246 (birth-month-precise start): got 0, want nonzero")
	}

	// Year-based fallback (age 64 vs 85 = 21 years = 252 months) would have
	// started 6 months later than the birth-month-precise value (246);
	// confirm the two disagree so this test actually exercises F-067
	// precision rather than coincidentally matching the fallback.
	yearFallbackStart := (85 - 64) * 12
	if yearFallbackStart == 246 {
		t.Fatalf("test setup invalid: year fallback (%d) coincides with month-precise start (246)", yearFallbackStart)
	}
}

// TestCareCostAt_InflationExactAtStart verifies the formula
// CareMonthlyCost * (1+PostMedicareInflation/100)^(month/12) exactly at the
// month care starts.
func TestCareCostAt_InflationExactAtStart(t *testing.T) {
	hp := HealthcarePerson{
		CurrentAge:            70,
		CareStartAge:          85,
		CareMonthlyCost:       8000,
		PostMedicareInflation: 4.0,
	}
	startMonth := 180
	got := hp.CareCostAt(startMonth, "")
	want := 8000.0 * math.Pow(1+4.0/100, float64(startMonth)/12.0)
	if math.Abs(got-want) > 0.0001 {
		t.Fatalf("at start month %d: got %f, want %f", startMonth, got, want)
	}
}

// TestCareCostAt_InflationExactPlusNYears verifies the same formula several
// years after care start, confirming inflation compounds from month 0 (not
// from care start).
func TestCareCostAt_InflationExactPlusNYears(t *testing.T) {
	hp := HealthcarePerson{
		CurrentAge:            70,
		CareStartAge:          85,
		CareMonthlyCost:       8000,
		PostMedicareInflation: 4.0,
	}
	month := 180 + 5*12 // 5 years after care start
	got := hp.CareCostAt(month, "")
	want := 8000.0 * math.Pow(1+4.0/100, float64(month)/12.0)
	if math.Abs(got-want) > 0.0001 {
		t.Fatalf("at month %d: got %f, want %f", month, got, want)
	}
}

// TestGetTotalHealthcareCost_LegacySinglePersonUnaffectedByCareFields
// confirms the legacy single-value healthcare path (no HealthcarePersons)
// is untouched by care — care requires the multi-person model.
func TestGetTotalHealthcareCost_LegacySinglePersonUnaffectedByCareFields(t *testing.T) {
	s := &WhatIfSettings{
		MonthlyHealthcare:    500,
		HealthcareStartYears: 0,
		HealthcareInflation:  6.0,
	}
	want := 500.0 * math.Pow(1.06, 1.0)
	if got := s.GetTotalHealthcareCost(12); math.Abs(got-want) > 0.01 {
		t.Fatalf("legacy path: got %f, want %f", got, want)
	}
}

// TestCareCostAt_JSONRoundtrip verifies CareStartAge and CareMonthlyCost
// survive the WhatIfSettings JSON save/load round-trip via their struct
// tags.
func TestCareCostAt_JSONRoundtrip(t *testing.T) {
	original := WhatIfSettings{
		HealthcarePersons: []HealthcarePerson{
			{
				ID:              "hp1",
				Name:            "User",
				CurrentAge:      70,
				CurrentCoverage: CoverageMedicare,
				CareStartAge:    85,
				CareMonthlyCost: 8000,
			},
		},
	}
	raw, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Pin the wire keys literally (CC1 fix O6): a silent struct-tag rename
	// (e.g. `care_start_age` -> `careStartAge`) must fail this test even
	// though the Go round-trip via the (still-renamed) tag would still
	// succeed.
	if !strings.Contains(string(raw), `"care_start_age":85`) {
		t.Fatalf("marshaled JSON missing literal \"care_start_age\":85, got %s", raw)
	}
	if !strings.Contains(string(raw), `"care_monthly_cost":8000`) {
		t.Fatalf("marshaled JSON missing literal \"care_monthly_cost\":8000, got %s", raw)
	}

	var decoded WhatIfSettings
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.HealthcarePersons) != 1 {
		t.Fatalf("expected 1 healthcare person, got %d", len(decoded.HealthcarePersons))
	}
	got := decoded.HealthcarePersons[0]
	if got.CareStartAge != 85 {
		t.Fatalf("CareStartAge: got %d, want 85", got.CareStartAge)
	}
	if got.CareMonthlyCost != 8000 {
		t.Fatalf("CareMonthlyCost: got %f, want 8000", got.CareMonthlyCost)
	}
}
