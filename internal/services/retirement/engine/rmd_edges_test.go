package engine

import (
	"math"
	"testing"
	"time"

	"budget2/internal/models"
)

// This file closes coverage gaps in the joint-life RMD plumbing: the SECURE
// 2.0 start-age fallbacks for legacy settings without Person records, the
// birth-year helpers' bad-input branches, and the standalone RMD math
// (CalculateRMD, GetLifeExpectancyFactor, RMDTriggerMonth, ParseStartYear)
// that the table-driven joint-life tests never touch.

// TestEffectiveRMDStartAge_NilSettings verifies the nil guard returns the
// pre-2033 default of 73 without panicking.
func TestEffectiveRMDStartAge_NilSettings(t *testing.T) {
	if got := EffectiveRMDStartAge(nil); got != 73 {
		t.Errorf("EffectiveRMDStartAge(nil) = %d, want 73", got)
	}
}

// TestEffectiveRMDStartAge_LegacyFallback exercises the no-BirthMonth path:
// birth year is derived from StartDate minus GetOlderAge, and the older
// spouse drives the household's applicable age across the 1959/1960 cusp.
func TestEffectiveRMDStartAge_LegacyFallback(t *testing.T) {
	tests := []struct {
		name                  string
		currentAge, spouseAge int
		want                  int
	}{
		{"born 1959 → 73", 67, 0, 73},  // 2026 - 67 = 1959
		{"born 1960 → 75", 66, 0, 75},  // 2026 - 66 = 1960
		{"older spouse drives", 60, 67, 73}, // spouse born 1959
		{"younger spouse ignored", 67, 60, 73},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &models.WhatIfSettings{
				StartDate:  "2026-01",
				CurrentAge: tc.currentAge,
				SpouseAge:  tc.spouseAge,
			}
			if got := EffectiveRMDStartAge(s); got != tc.want {
				t.Errorf("EffectiveRMDStartAge(age %d/spouse %d) = %d, want %d",
					tc.currentAge, tc.spouseAge, got, tc.want)
			}
		})
	}
}

// TestEffectiveRMDStartAge_UnparseableBirthMonth verifies that a garbage
// BirthMonth is skipped: with no other parseable Person the code falls back
// to StartDate - GetOlderAge, and with a parseable spouse it uses that year.
func TestEffectiveRMDStartAge_UnparseableBirthMonth(t *testing.T) {
	// Both unparseable → legacy fallback (2026 - 66 = 1960 → 75).
	s := &models.WhatIfSettings{
		StartDate:  "2026-01",
		CurrentAge: 66,
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: "not-a-date"},
		},
	}
	if got := EffectiveRMDStartAge(s); got != 75 {
		t.Errorf("EffectiveRMDStartAge(garbage BirthMonth, fallback 1960) = %d, want 75", got)
	}

	// Primary unparseable, spouse parseable 1958 → 73 from the spouse.
	s2 := &models.WhatIfSettings{
		StartDate:  "2026-01",
		CurrentAge: 60,
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: "bogus"},
			{Role: models.PersonRoleSpouse, BirthMonth: "1958-06"},
		},
	}
	if got := EffectiveRMDStartAge(s2); got != 73 {
		t.Errorf("EffectiveRMDStartAge(spouse 1958) = %d, want 73", got)
	}
}

// TestFirstRMDCalendarYear_LegacyFallback covers olderBirthYear's
// no-Persons fallback: StartDate 2026, older age 67 → born 1959 → first
// RMD calendar year 1959 + 73 = 2032.
func TestFirstRMDCalendarYear_LegacyFallback(t *testing.T) {
	s := &models.WhatIfSettings{StartDate: "2026-01", CurrentAge: 67}
	if got := FirstRMDCalendarYear(s); got != 2032 {
		t.Errorf("FirstRMDCalendarYear(legacy 1959) = %d, want 2032", got)
	}
	if RMDApplies(s, 2031) {
		t.Error("RMDApplies(2031) = true, want false (first RMD year is 2032)")
	}
	if !RMDApplies(s, 2032) {
		t.Error("RMDApplies(2032) = false, want true")
	}
}

// TestYoungerBirthYear_NilAndFallback covers youngerBirthYear's nil guard
// and its no-Persons fallback (StartDate - GetYoungerAge).
func TestYoungerBirthYear_NilAndFallback(t *testing.T) {
	if got, want := youngerBirthYear(nil), time.Now().Year()-73; got != want {
		t.Errorf("youngerBirthYear(nil) = %d, want %d", got, want)
	}
	s := &models.WhatIfSettings{StartDate: "2026-01", CurrentAge: 67, SpouseAge: 55}
	if got := youngerBirthYear(s); got != 1971 {
		t.Errorf("youngerBirthYear(legacy 55-year-old spouse) = %d, want 1971", got)
	}
}

// TestBirthYearHelpers_SkipBadPersons verifies both birth-year scanners skip
// empty and unparseable BirthMonth values instead of failing.
func TestBirthYearHelpers_SkipBadPersons(t *testing.T) {
	// Primary empty, spouse parseable: both helpers land on the spouse.
	s := &models.WhatIfSettings{
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: ""},
			{Role: models.PersonRoleSpouse, BirthMonth: "1971-08"},
		},
	}
	if y, ok := latestPersonBirthYear(s); !ok || y != 1971 {
		t.Errorf("latestPersonBirthYear(empty primary) = %d, %v; want 1971, true", y, ok)
	}
	if y, ok := earliestPersonBirthYear(s); !ok || y != 1971 {
		t.Errorf("earliestPersonBirthYear(empty primary) = %d, %v; want 1971, true", y, ok)
	}

	// Spouse unparseable: both helpers land on the primary.
	s2 := &models.WhatIfSettings{
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: "1958-11"},
			{Role: models.PersonRoleSpouse, BirthMonth: "13/40/1971"},
		},
	}
	if y, ok := latestPersonBirthYear(s2); !ok || y != 1958 {
		t.Errorf("latestPersonBirthYear(bad spouse) = %d, %v; want 1958, true", y, ok)
	}

	// Nothing parseable: found=false.
	s3 := &models.WhatIfSettings{
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: "junk"},
		},
	}
	if _, ok := latestPersonBirthYear(s3); ok {
		t.Error("latestPersonBirthYear(all bad) reported found=true, want false")
	}
}

// TestRMDTriggerMonth maps each timing (and unknown values, which normalize
// to mid-year) to its withdrawal month.
func TestRMDTriggerMonth(t *testing.T) {
	tests := []struct {
		timing models.RMDTiming
		want   int
	}{
		{models.RMDTimingStartOfYear, 0},
		{models.RMDTimingMidYear, 6},
		{models.RMDTimingEndOfYear, 11},
		{models.RMDTiming(""), 6},
		{models.RMDTiming("bogus"), 6},
	}
	for _, tc := range tests {
		if got := RMDTriggerMonth(tc.timing); got != tc.want {
			t.Errorf("RMDTriggerMonth(%q) = %d, want %d", tc.timing, got, tc.want)
		}
	}
}

// TestParseStartYear covers the empty and unparseable fallbacks (current
// year) alongside the happy path.
func TestParseStartYear(t *testing.T) {
	if got := ParseStartYear("2026-01"); got != 2026 {
		t.Errorf("ParseStartYear(2026-01) = %d, want 2026", got)
	}
	now := time.Now().Year()
	if got := ParseStartYear(""); got != now {
		t.Errorf("ParseStartYear(\"\") = %d, want current year %d", got, now)
	}
	if got := ParseStartYear("January 2026"); got != now {
		t.Errorf("ParseStartYear(unparseable) = %d, want current year %d", got, now)
	}
}

// TestGetLifeExpectancyFactor_Edges pins the Uniform Lifetime Table edges:
// no RMD below 72, table hits inside the range, and the 2.0 floor past 120.
func TestGetLifeExpectancyFactor_Edges(t *testing.T) {
	tests := []struct {
		age  int
		want float64
	}{
		{0, 0},
		{71, 0},
		{72, 27.4},
		{73, 26.5},
		{120, 2.0},
		{121, 2.0},
		{150, 2.0},
	}
	for _, tc := range tests {
		if got := GetLifeExpectancyFactor(tc.age); got != tc.want {
			t.Errorf("GetLifeExpectancyFactor(%d) = %v, want %v", tc.age, got, tc.want)
		}
	}
}

// TestCalculateRMD verifies the age-keyed Uniform Table entry point: exact
// divisor math at 73 and the zero result below RMD age.
func TestCalculateRMD(t *testing.T) {
	const eps = 1e-9

	amount, percent := CalculateRMD(1_060_000, 73)
	if want := 1_060_000 / 26.5; math.Abs(amount-want) > eps {
		t.Errorf("CalculateRMD amount = %v, want %v", amount, want)
	}
	if want := 100 / 26.5; math.Abs(percent-want) > eps {
		t.Errorf("CalculateRMD percent = %v, want %v", percent, want)
	}

	amount, percent = CalculateRMD(1_060_000, 70)
	if amount != 0 || percent != 0 {
		t.Errorf("CalculateRMD(age 70) = %v, %v; want 0, 0", amount, percent)
	}
}

// TestCalculateRMDForYear_BeforeRMDAge covers the zero-factor branch of the
// settings-aware wrapper: owner age 71 in 2029 (born 1958) → no RMD.
func TestCalculateRMDForYear_BeforeRMDAge(t *testing.T) {
	s := gap13Settings()
	amount, percent := CalculateRMDForYear(s, 1_810_000, 2029)
	if amount != 0 || percent != 0 {
		t.Errorf("CalculateRMDForYear(owner 71) = %v, %v; want 0, 0", amount, percent)
	}
}

// TestJointLifeFactor_DefensiveShortRow covers the never-panic guard for a
// malformed band row. The clamps make the guard unreachable with the real
// table, so the test swaps in a nil row for one owner age and restores it.
func TestJointLifeFactor_DefensiveShortRow(t *testing.T) {
	orig := jointLifeBand[73]
	jointLifeBand[73] = nil
	defer func() { jointLifeBand[73] = orig }()

	if got := jointLifeFactor(73, 60); got != 0 {
		t.Errorf("jointLifeFactor with nil band row = %v, want 0", got)
	}
}
