package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// The cumulative-benefit columns drive the "best claiming age" pick, so
// they must compound COLA from a common base year (current age / today),
// not from each option's own claim age. Compounding from the claim age
// omits the COLA accrued between now and a later claim, understating
// later-claim options and biasing the recommendation toward claiming
// early. The breakeven calc already uses a common base; the comparison
// table must match.
func TestSSComparisonTable_CumulativeUsesCommonCOLABase(t *testing.T) {
	const pia = 2000.0
	const fra = 67
	const currentAge = 62
	const cola = 0.03

	opts := SSComparisonTable(pia, fra, currentAge, cola)

	var opt70 *models.SSClaimingOption
	for i := range opts {
		if opts[i].ClaimAge == 70 {
			opt70 = &opts[i]
		}
	}
	if opt70 == nil {
		t.Fatalf("no age-70 option in comparison table")
	}

	// Expected: COLA compounded from currentAge (common base) across the
	// payout years 70..84, not from the claim age 70.
	monthly70 := AdjustedSSBenefit(pia, fra, 70)
	want := 0.0
	for age := 70; age < 85; age++ {
		want += monthly70 * math.Pow(1+cola, float64(age-currentAge)) * 12.0
	}
	want = math.Round(want*100) / 100

	if math.Abs(opt70.CumulativeAt85-want) > 0.01 {
		t.Errorf("CumulativeAt85 for age 70 = %.2f, want %.2f (COLA should compound from current age, not claim age)",
			opt70.CumulativeAt85, want)
	}
}
