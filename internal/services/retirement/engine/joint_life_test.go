package engine

import "testing"

// jointLifeSpotChecks are values hand-read from the published IRS Joint and
// Last Survivor Table (26 CFR 1.401(a)(9)-9(d)), independent of the generator
// script. This set is intentionally a separate copy of the SPOT_CHECKS in
// scripts/gen-joint-life-table.py — the script's copy self-checks generation;
// this copy is the standing regression guard. Do not DRY them together. They span both band edges (spouse 18, spouse owner-11), the 120+
// row, the youngest owner row (72), and interior cells. If the generated data
// drifts from the regulation, these fail.
var jointLifeSpotChecks = []struct {
	owner, spouse int
	factor        float64
}{
	{72, 18, 67.1},  // youngest owner, youngest spouse column
	{72, 50, 36.9},  // spec spot check
	{72, 61, 28.1},  // owner 72, spouse at band edge (owner-11)
	{73, 60, 28.6},  // spec: the user's household at owner age 73
	{73, 62, 27.2},  // owner 73, band edge
	{76, 55, 32.3},  // interior
	{80, 65, 23.8},  // spec spot check
	{80, 69, 20.9},  // owner 80, band edge
	{85, 74, 16.7},  // owner 85, band edge
	{90, 40, 45.8},  // interior, wide age gap
	{95, 84, 9.4},   // owner 95, band edge
	{100, 50, 36.2}, // interior
	{110, 30, 55.3}, // interior, very wide gap
	{120, 18, 67.0}, // 120+ row, youngest spouse
	{120, 80, 11.2}, // spec: 120+ row interior
	{120, 109, 2.0}, // 120+ row, band edge
}

func TestJointLifeFactor_SpotChecks(t *testing.T) {
	for _, tc := range jointLifeSpotChecks {
		got := jointLifeFactor(tc.owner, tc.spouse)
		if got != tc.factor {
			t.Errorf("jointLifeFactor(%d, %d) = %v, want %v",
				tc.owner, tc.spouse, got, tc.factor)
		}
	}
}

func TestJointLifeFactor_Clamps(t *testing.T) {
	tests := []struct {
		name          string
		owner, spouse int
		wantSame      [2]int // expected to equal jointLifeFactor(wantSame...)
	}{
		{"owner 121 clamps to 120", 121, 80, [2]int{120, 80}},
		{"owner 200 clamps to 120", 200, 18, [2]int{120, 18}},
		{"owner below 72 clamps to 72", 71, 18, [2]int{72, 18}},
		{"spouse 17 clamps to 18", 73, 17, [2]int{73, 18}},
		{"spouse 10 clamps to 18", 80, 10, [2]int{80, 18}},
		{"spouse above owner-11 clamps to edge", 72, 70, [2]int{72, 61}},
		{"spouse equal owner clamps to edge", 90, 90, [2]int{90, 79}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := jointLifeFactor(tc.owner, tc.spouse)
			want := jointLifeFactor(tc.wantSame[0], tc.wantSame[1])
			if got != want {
				t.Errorf("jointLifeFactor(%d, %d) = %v, want %v (== factor(%d,%d))",
					tc.owner, tc.spouse, got, want, tc.wantSame[0], tc.wantSame[1])
			}
		})
	}
}

// TestJointLifeBand_Structure guards the generated data's shape: every owner
// row 72..120 must cover exactly spouse ages 18..owner-11, hold positive
// factors, and be non-increasing as the spouse ages (joint life expectancy
// can only fall as either person ages). Total band size must be 3332 cells.
func TestJointLifeBand_Structure(t *testing.T) {
	if jointLifeMinOwnerAge != 72 || jointLifeMaxOwnerAge != 120 || jointLifeMinSpouseAge != 18 {
		t.Fatalf("unexpected band bounds: owner [%d,%d] spouse min %d",
			jointLifeMinOwnerAge, jointLifeMaxOwnerAge, jointLifeMinSpouseAge)
	}

	total := 0
	for owner := jointLifeMinOwnerAge; owner <= jointLifeMaxOwnerAge; owner++ {
		row, ok := jointLifeBand[owner]
		if !ok {
			t.Errorf("missing band row for owner age %d", owner)
			continue
		}
		wantLen := owner - 11 - jointLifeMinSpouseAge + 1
		if len(row) != wantLen {
			t.Errorf("owner %d: row length %d, want %d", owner, len(row), wantLen)
		}
		total += len(row)
		prev := 1e9
		for i, f := range row {
			spouse := jointLifeMinSpouseAge + i
			if f <= 0 {
				t.Errorf("owner %d spouse %d: non-positive factor %v", owner, spouse, f)
			}
			if f > prev {
				t.Errorf("owner %d spouse %d: factor %v rose above previous %v (should be non-increasing)",
					owner, spouse, f, prev)
			}
			prev = f
		}
	}
	if total != 3332 {
		t.Errorf("band total = %d cells, want 3332", total)
	}

	if len(jointLifeBand) != jointLifeMaxOwnerAge-jointLifeMinOwnerAge+1 {
		t.Errorf("band has %d owner rows, want %d",
			len(jointLifeBand), jointLifeMaxOwnerAge-jointLifeMinOwnerAge+1)
	}
}
