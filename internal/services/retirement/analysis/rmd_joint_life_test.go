package analysis

import "testing"

// TestBuildRMD_JointLifeTable_E2E is an end-to-end check that the RMD schedule
// surfaces the Joint and Last Survivor Table factor (and sets the
// UsesJointLifeTable flag) when the spouse is the sole beneficiary and more than
// 10 years younger, and the Uniform Lifetime Table otherwise. Household: owner
// age 73, spouse age 60 (13-year gap); the first projection row is owner age 73,
// whose Joint Table II divisor is 28.6 versus the Uniform 26.5.
func TestBuildRMD_JointLifeTable_E2E(t *testing.T) {
	proj := fixtureProjection(60,
		func(m int) float64 { return 1_810_000 },
		func(m int) float64 { return 0 },
		nil)

	// Default (setting unset → ON): Joint Table II, age-73 factor 28.6.
	s := settingsF072(73, 60, 1_810_000, 100, 5, "2026-01")
	a := BuildRMD(proj, engineInput(t, s))
	if !a.UsesJointLifeTable {
		t.Error("UsesJointLifeTable = false, want true (13-year-gap sole-beneficiary spouse)")
	}
	if len(a.Projections) == 0 {
		t.Fatal("expected projections")
	}
	if got := a.Projections[0].LifeExpFactor; got != 28.6 {
		t.Errorf("age-73 LifeExpFactor = %v, want 28.6 (Joint Table II)", got)
	}

	// Setting off → Uniform Lifetime Table, age-73 factor 26.5, flag false.
	sOff := settingsF072(73, 60, 1_810_000, 100, 5, "2026-01")
	sOff.SpouseSoleBeneficiary = new(bool) // zero value: false
	aOff := BuildRMD(proj, engineInput(t, sOff))
	if aOff.UsesJointLifeTable {
		t.Error("UsesJointLifeTable = true with setting off, want false")
	}
	if got := aOff.Projections[0].LifeExpFactor; got != 26.5 {
		t.Errorf("age-73 LifeExpFactor (setting off) = %v, want 26.5 (Uniform)", got)
	}
}
