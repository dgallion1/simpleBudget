package analysis

import "testing"

// TestBuildRMD_NilOrEmptyProjection verifies BuildRMD degrades gracefully
// when there is tax-deferred money but no projection to sample: it still
// reports eligibility metadata (including the Joint Life Table flag) with an
// empty schedule instead of panicking.
func TestBuildRMD_NilOrEmptyProjection(t *testing.T) {
	// Owner 73, spouse 60 → 13-year gap qualifies for Table II.
	s := settingsF072(73, 60, 1_000_000, 100, 5, "2026-01")
	in := engineInput(t, s)

	a := BuildRMD(nil, in)
	if a == nil {
		t.Fatal("BuildRMD(nil proj) returned nil")
	}
	if len(a.Projections) != 0 {
		t.Errorf("nil proj: len(Projections) = %d, want 0", len(a.Projections))
	}
	if a.TaxDeferredValue != 1_000_000 {
		t.Errorf("nil proj: TaxDeferredValue = %v, want 1000000", a.TaxDeferredValue)
	}
	if !a.UsesJointLifeTable {
		t.Error("nil proj: UsesJointLifeTable = false, want true (13-year gap)")
	}

	empty := fixtureProjection(0, nil, nil, nil)
	if got := BuildRMD(empty, in); len(got.Projections) != 0 {
		t.Errorf("empty proj: len(Projections) = %d, want 0", len(got.Projections))
	}
}

// TestBuildRMD_ClampsYearsToProjectionLength verifies the row loop is bounded
// by the months actually present in the projection, not ProjectionYears:
// 5 requested years against a 24-month projection yields exactly 2 rows.
func TestBuildRMD_ClampsYearsToProjectionLength(t *testing.T) {
	s := settingsF072(73, 0, 1_000_000, 100, 5, "2026-01")
	proj := fixtureProjection(24,
		func(m int) float64 { return 1_000_000 },
		func(m int) float64 { return 1_000 },
		nil)

	a := BuildRMD(proj, engineInput(t, s))
	if len(a.Projections) != 2 {
		t.Fatalf("len(Projections) = %d, want 2 (24-month projection)", len(a.Projections))
	}
	// Both rows sum a full 12 months of the fixture's 1,000/month RMD.
	for i, row := range a.Projections {
		if row.RMDAmount != 12_000 {
			t.Errorf("row %d RMDAmount = %v, want 12000", i, row.RMDAmount)
		}
	}
}
