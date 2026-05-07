package retirement

import (
	"testing"

	"budget2/internal/models"
)

// F-078: helpers that drive calendar-year RMD gating across the projection,
// MC, backtest, chart event label, and RMD analysis. These replace the
// floor'd `olderAge >= EffectiveRMDStartAge(s)` comparisons that slipped
// late-year births by one calendar year.

func TestFirstRMDCalendarYear_F078_BirthMonth(t *testing.T) {
	cases := []struct {
		name         string
		primaryBirth string
		spouseBirth  string
		startDate    string
		want         int
	}{
		{"primary born 1959-12 → 2032 (1959+73)", "1959-12", "", "2026-01", 2032},
		{"primary born 1959-01 → 2032", "1959-01", "", "2026-01", 2032},
		{"primary born 1960-01 → 2035 (1960+75)", "1960-01", "", "2026-01", 2035},
		{"primary born 1960-12 → 2035", "1960-12", "", "2026-01", 2035},
		{"older spouse born 1959-11, primary 1965 → 2032", "1965-06", "1959-11", "2026-01", 2032},
		{"older primary 1953-06 → 2026 (already in RMD)", "1953-06", "", "2026-01", 2026},
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
			if got := FirstRMDCalendarYear(s); got != c.want {
				t.Errorf("FirstRMDCalendarYear = %d; want %d", got, c.want)
			}
		})
	}
}

// Legacy fallback: when no Person carries BirthMonth, the helper must derive
// the birth year from startYear - GetOlderAge() — exactly what the old
// projection gate did, so non-Person callers stay unchanged.
func TestFirstRMDCalendarYear_F078_LegacyFallback(t *testing.T) {
	s := &models.WhatIfSettings{
		StartDate:  "2026-01",
		CurrentAge: 66, // legacy: birth year derived as 2026-66=1960 → applicable 75 → first RMD 2035
	}
	if got := FirstRMDCalendarYear(s); got != 2035 {
		t.Errorf("legacy fallback (CurrentAge=66, 2026 start) = %d; want 2035", got)
	}
}

func TestRMDApplies_F078(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: "1959-12"},
	}
	s.ComputeAges()

	if RMDApplies(s, 2031) {
		t.Errorf("RMDApplies(1959-12, 2031) = true; want false")
	}
	if !RMDApplies(s, 2032) {
		t.Errorf("RMDApplies(1959-12, 2032) = false; want true (attains 73 in Dec 2032)")
	}
	if !RMDApplies(s, 2050) {
		t.Errorf("RMDApplies(1959-12, 2050) = false; want true")
	}
}

// RMDAgeForCalendarYear must return the age the older person attains by
// Dec 31 of the calendar year — the age the IRS Uniform Lifetime Table is
// keyed off. For a 1959-12 birth in calendar 2032 that's 73 (not the
// floor'd 72 the old `GetOlderAge() + currentYear` would have produced).
func TestRMDAgeForCalendarYear_F078(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: "1959-12"},
	}
	s.ComputeAges()

	if got := RMDAgeForCalendarYear(s, 2032); got != 73 {
		t.Errorf("RMDAgeForCalendarYear(1959-12, 2032) = %d; want 73", got)
	}
	if got := RMDAgeForCalendarYear(s, 2033); got != 74 {
		t.Errorf("RMDAgeForCalendarYear(1959-12, 2033) = %d; want 74", got)
	}
}

func TestRMDAgeForCalendarYear_F078_LegacyFallback(t *testing.T) {
	s := &models.WhatIfSettings{
		StartDate:  "2026-01",
		CurrentAge: 70, // legacy fallback: birth year 1956
	}
	if got := RMDAgeForCalendarYear(s, 2030); got != 74 {
		t.Errorf("legacy RMDAgeForCalendarYear(CurrentAge=70, 2030) = %d; want 74", got)
	}
}

// Nil-safe: the helpers must not panic on nil settings (matches existing
// EffectiveRMDStartAge behaviour). FirstRMDCalendarYear and friends fall
// back to the current calendar year + 73.
func TestRMDHelpers_F078_NilSafe(t *testing.T) {
	if !RMDApplies(nil, 9999) {
		t.Errorf("RMDApplies(nil, 9999) = false; want true (nil falls through to default 73 age)")
	}
}
