package engine

// jointLifeFactor returns the IRS Joint and Last Survivor Table ("Table II",
// 26 CFR 1.401(a)(9)-9(d)) life-expectancy divisor for an account owner aged
// ownerAge whose sole-beneficiary spouse is aged spouseAge in the distribution
// calendar year. The owner is the older household member; the spouse is the
// younger. A larger factor yields a smaller required distribution.
//
// The data lives in jointLifeBand (joint_life_table.go, generated from the
// eCFR source). All clamps are conservative — each pins an out-of-band age to
// the edge that can only *increase* the required distribution, so a bad input
// never understates the legal minimum:
//
//   - ownerAge > 120 → 120 (the regulation's 120+ row; smallest factors).
//   - ownerAge < 72  → 72 (defensive; callers only reach this path once RMDs
//     apply, i.e. ownerAge ≥ 73, so this is never exercised in practice).
//   - spouseAge < 18 → 18 (the table's youngest column; a younger spouse would
//     only give a larger factor, so clamping up is conservative).
//   - spouseAge > ownerAge-11 → ownerAge-11 (the eligibility gate guarantees a
//     gap ≥ 11, so this is defensive; it pins to the row's last column).
func jointLifeFactor(ownerAge, spouseAge int) float64 {
	if ownerAge > jointLifeMaxOwnerAge {
		ownerAge = jointLifeMaxOwnerAge
	}
	if ownerAge < jointLifeMinOwnerAge {
		ownerAge = jointLifeMinOwnerAge
	}
	if spouseAge < jointLifeMinSpouseAge {
		spouseAge = jointLifeMinSpouseAge
	}
	if maxSpouse := ownerAge - 11; spouseAge > maxSpouse {
		spouseAge = maxSpouse
	}
	row := jointLifeBand[ownerAge]
	idx := spouseAge - jointLifeMinSpouseAge
	if idx < 0 || idx >= len(row) {
		// Unreachable given the clamps above; never panic on unexpected input.
		return 0
	}
	return row[idx]
}
