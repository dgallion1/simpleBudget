package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// boolPtr is a helper to create a *bool for SpouseSoleBeneficiary.
func boolPtr(b bool) *bool { return &b }

// gap13Settings builds a two-person household with a 13-year birth-year gap
// (primary 1958, spouse 1971). Owner attains age 73 in calendarYear 2031.
func gap13Settings() *models.WhatIfSettings {
	return &models.WhatIfSettings{
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: "1958-11"},
			{Role: models.PersonRoleSpouse, BirthMonth: "1971-08"},
		},
	}
}

// TestUsesJointLifeTable_Gap13 verifies the household qualifies for Table II.
func TestUsesJointLifeTable_Gap13(t *testing.T) {
	s := gap13Settings()
	if !UsesJointLifeTable(s) {
		t.Error("gap13: UsesJointLifeTable should be true (gap ≥ 11)")
	}
}

// TestRMDLifeFactor_Gap13_Joint verifies jointLifeFactor(73, 60) = 28.6 is
// returned for owner age 73 in calendarYear 2031 (1958+73=2031, spouse 1971→60).
func TestRMDLifeFactor_Gap13_Joint(t *testing.T) {
	s := gap13Settings()
	// owner: 2031-1958=73; spouse: 2031-1971=60
	got := RMDLifeFactor(s, 2031)
	want := 28.6
	if got != want {
		t.Errorf("RMDLifeFactor gap13 2031 = %v, want %v", got, want)
	}
}

// TestRMDLifeFactor_Gap11_Joint verifies the boundary: gap of exactly 11 years
// still uses Table II. Primary 1958-01, spouse 1969-01 → gap 11.
// Owner 2031-1958=73, spouse 2031-1969=62 → jointLifeFactor(73,62)=27.2.
func TestRMDLifeFactor_Gap11_Joint(t *testing.T) {
	s := &models.WhatIfSettings{
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: "1958-01"},
			{Role: models.PersonRoleSpouse, BirthMonth: "1969-01"},
		},
	}
	if !UsesJointLifeTable(s) {
		t.Error("gap11: UsesJointLifeTable should be true (gap == 11, i.e. ≥ 11)")
	}
	got := RMDLifeFactor(s, 2031)
	want := 27.2
	if got != want {
		t.Errorf("RMDLifeFactor gap11 2031 = %v, want %v", got, want)
	}
}

// TestRMDLifeFactor_Gap10_Uniform verifies that a gap of exactly 10 years falls
// through to the Uniform Lifetime Table. Primary 1958-01, spouse 1968-01.
// Owner age 73 → GetLifeExpectancyFactor(73) = 26.5.
func TestRMDLifeFactor_Gap10_Uniform(t *testing.T) {
	s := &models.WhatIfSettings{
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: "1958-01"},
			{Role: models.PersonRoleSpouse, BirthMonth: "1968-01"},
		},
	}
	if UsesJointLifeTable(s) {
		t.Error("gap10: UsesJointLifeTable should be false (gap < 11)")
	}
	got := RMDLifeFactor(s, 2031)
	want := 26.5
	if got != want {
		t.Errorf("RMDLifeFactor gap10 2031 = %v, want %v", got, want)
	}
}

// TestUsesJointLifeTable_SettingOff verifies that setting SpouseSoleBeneficiary
// to false forces Uniform Lifetime Table even when the gap is ≥ 11.
func TestUsesJointLifeTable_SettingOff(t *testing.T) {
	s := gap13Settings()
	s.SpouseSoleBeneficiary = boolPtr(false)
	if UsesJointLifeTable(s) {
		t.Error("setting off: UsesJointLifeTable should be false when SpouseSoleBeneficiary=false")
	}
	got := RMDLifeFactor(s, 2031)
	want := 26.5
	if got != want {
		t.Errorf("RMDLifeFactor setting-off 2031 = %v, want %v", got, want)
	}
}

// TestUsesJointLifeTable_NoSpouse verifies that a single-person household uses
// the Uniform Lifetime Table.
func TestUsesJointLifeTable_NoSpouse(t *testing.T) {
	s := &models.WhatIfSettings{
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: "1958-11"},
		},
	}
	if UsesJointLifeTable(s) {
		t.Error("no spouse: UsesJointLifeTable should be false")
	}
	got := RMDLifeFactor(s, 2031)
	want := 26.5
	if got != want {
		t.Errorf("RMDLifeFactor no-spouse 2031 = %v, want %v", got, want)
	}
}

// TestUsesJointLifeTable_NilSettings verifies nil settings don't panic and
// return false/0.
func TestUsesJointLifeTable_NilSettings(t *testing.T) {
	if UsesJointLifeTable(nil) {
		t.Error("nil: UsesJointLifeTable should be false")
	}
	// Must not panic; result is just some non-zero float (Uniform path for
	// whatever olderBirthYear(nil) returns). We only verify no panic and
	// UsesJointLifeTable path is false.
	_ = RMDLifeFactor(nil, 2031)
}

// TestRMDLifeFactor_AgeBelowRMD verifies that a calendar year where the owner
// is 71 returns 0 (no RMD required).
func TestRMDLifeFactor_AgeBelowRMD(t *testing.T) {
	s := gap13Settings()
	// owner: 2029-1958 = 71 (below 72)
	got := RMDLifeFactor(s, 2029)
	if got != 0 {
		t.Errorf("RMDLifeFactor age 71 = %v, want 0", got)
	}
}

// TestCalculateRMDForYear_JointAndUniform verifies the settings-aware wrapper.
func TestCalculateRMDForYear_JointAndUniform(t *testing.T) {
	const balance = 1_810_000.0
	const eps = 1e-6

	// Gap-13 household → Joint Table II divisor 28.6.
	s := gap13Settings()
	wantAmount := balance / 28.6
	wantPct := (1.0 / 28.6) * 100
	gotAmt, gotPct := CalculateRMDForYear(s, balance, 2031)
	if math.Abs(gotAmt-wantAmount) > eps {
		t.Errorf("CalculateRMDForYear joint amount = %v, want %v", gotAmt, wantAmount)
	}
	if math.Abs(gotPct-wantPct) > eps {
		t.Errorf("CalculateRMDForYear joint percent = %v, want %v", gotPct, wantPct)
	}

	// Same balance, setting off → Uniform 26.5.
	sOff := gap13Settings()
	sOff.SpouseSoleBeneficiary = boolPtr(false)
	wantAmountU := balance / 26.5
	wantPctU := (1.0 / 26.5) * 100
	gotAmtU, gotPctU := CalculateRMDForYear(sOff, balance, 2031)
	if math.Abs(gotAmtU-wantAmountU) > eps {
		t.Errorf("CalculateRMDForYear uniform amount = %v, want %v", gotAmtU, wantAmountU)
	}
	if math.Abs(gotPctU-wantPctU) > eps {
		t.Errorf("CalculateRMDForYear uniform percent = %v, want %v", gotPctU, wantPctU)
	}
}

// TestRMDLifeFactor_UniformParity verifies that for a single-person household,
// RMDLifeFactor is bit-identical to GetLifeExpectancyFactor for several ages.
func TestRMDLifeFactor_UniformParity(t *testing.T) {
	// Single-person household (no spouse) → must use Uniform path.
	s := &models.WhatIfSettings{
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: "1958-01"},
		},
	}
	// Owner birth year 1958; test several calendar years and matching ages.
	years := []int{2030, 2031, 2032, 2035, 2040}
	for _, y := range years {
		ownerAge := y - 1958
		got := RMDLifeFactor(s, y)
		want := GetLifeExpectancyFactor(ownerAge)
		if got != want {
			t.Errorf("year %d (age %d): RMDLifeFactor = %v, GetLifeExpectancyFactor = %v (want parity)",
				y, ownerAge, got, want)
		}
	}
}
