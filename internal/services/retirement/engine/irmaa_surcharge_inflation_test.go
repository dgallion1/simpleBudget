package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// Audit finding F-6. IRMAA has two moving parts that do not move together:
// the MAGI thresholds are statutorily CPI-indexed, but the surcharge dollars
// are set from Medicare per-capita cost growth, historically well above CPI.
// Applying one CPI factor to both systematically understates future
// surcharges — the further out the projection year, the worse.
//
// This is a specification, not a falsification: the old single-factor
// behaviour was not self-contradictory, just indexed to the wrong series.

func TestIRMAASurchargeAndThresholdsUseDifferentIndices(t *testing.T) {
	const years = 10.0
	cpi := 3.0

	thresholdFactor := PlannerIRMAAInflationFactorForYear(cpi, years)
	surchargeFactor := PlannerIRMAASurchargeInflationFactorForYear(years)

	if surchargeFactor <= thresholdFactor {
		t.Errorf("surcharge factor %.4f should outgrow the CPI threshold factor %.4f over %.0f years; "+
			"Medicare per-capita cost growth runs ahead of CPI", surchargeFactor, thresholdFactor, years)
	}

	// The surcharge index must not depend on the plan's CPI assumption: a
	// household choosing a lower inflation rate does not thereby slow Medicare
	// cost growth.
	if other := PlannerIRMAASurchargeInflationFactorForYear(years); math.Abs(other-surchargeFactor) > 1e-9 {
		t.Errorf("surcharge factor is not a pure function of the year: %.6f vs %.6f", other, surchargeFactor)
	}
}

// The two factors are applied to different parts of the bracket table: the
// threshold factor moves the MAGI cutoffs, the surcharge factor moves the
// dollars charged once a cutoff is cleared.
func TestCalculateMonthlyIRMAAAppliesEachFactorToItsOwnPart(t *testing.T) {
	// Tier-1 single surcharge in the bundled 2026 table.
	const tier1 = 81.20 + 14.50

	// Surcharge doubles, thresholds unchanged: a MAGI in tier 1 pays twice the
	// tier-1 surcharge.
	got := CalculateMonthlyIRMAA(120_000, models.FilingSingle, 1, 2)
	if math.Abs(got-tier1*2) > 0.01 {
		t.Errorf("surcharge factor 2 => %.2f, want %.2f", got, tier1*2)
	}

	// Thresholds double, surcharge unchanged: the same MAGI now falls below
	// the tier-1 cutoff and pays nothing.
	if got := CalculateMonthlyIRMAA(120_000, models.FilingSingle, 2, 1); got != 0 {
		t.Errorf("threshold factor 2 should lift 120k below the first cutoff, got %.2f", got)
	}

	// Non-positive factors are still treated as 1, independently.
	base := CalculateMonthlyIRMAA(120_000, models.FilingSingle, 1, 1)
	if got := CalculateMonthlyIRMAA(120_000, models.FilingSingle, 0, -3); math.Abs(got-base) > 0.01 {
		t.Errorf("non-positive factors should clamp to 1: got %.2f, want %.2f", got, base)
	}
}

// End to end: the surcharge a plan is actually charged in a far-out year must
// exceed what the CPI-only model produced.
func TestFarOutIRMAAExceedsCPIOnlyEstimate(t *testing.T) {
	const years = 20.0
	cpi := 3.0

	thresholdFactor := PlannerIRMAAInflationFactorForYear(cpi, years)
	surchargeFactor := PlannerIRMAASurchargeInflationFactorForYear(years)

	magi := 400_000.0
	cpiOnly := CalculateMonthlyIRMAA(magi, models.FilingSingle, thresholdFactor, thresholdFactor)
	corrected := CalculateMonthlyIRMAA(magi, models.FilingSingle, thresholdFactor, surchargeFactor)

	if corrected <= cpiOnly {
		t.Errorf("20-year surcharge %.2f should exceed the CPI-only figure %.2f", corrected, cpiOnly)
	}
	t.Logf("monthly IRMAA at MAGI %.0f, %.0f years out: CPI-only %.2f, Medicare-indexed %.2f",
		magi, years, cpiOnly, corrected)
}
