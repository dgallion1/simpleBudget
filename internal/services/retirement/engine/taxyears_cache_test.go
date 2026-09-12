package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// Every projection month asks the calculator for its inflation-scaled
// brackets and deduction three times. Rebuilding the scaled tables on each
// call allocated ~32 GB across one tax-optimizer run and made the runtime
// allocator's locks the scaling bottleneck (7.5s on 32 cores, GC itself at
// 0%). Projected years are pure functions of (inflation rate, calendar year)
// over an immutable statutory table, so they are memoised process-wide.
func TestResolveTaxYear_ProjectedYearIsAllocationFreeOnceWarm(t *testing.T) {
	tc := NewTaxCalculator(models.DefaultTaxConfig(), 2.5)
	years := LatestStatutoryFederalTaxYear() - taxBaseYear + 7 // well past the table

	tc.GetAdjustedBrackets(years) // warm the memo
	for name, fn := range map[string]func(){
		"GetAdjustedBrackets":                     func() { tc.GetAdjustedBrackets(years) },
		"GetAdjustedLongTermCapitalGainsBrackets": func() { tc.GetAdjustedLongTermCapitalGainsBrackets(years) },
		"GetAdjustedStandardDeduction":            func() { tc.GetAdjustedStandardDeduction(years) },
	} {
		if allocs := testing.AllocsPerRun(200, fn); allocs != 0 {
			t.Errorf("%s(%d): %.1f allocs/op after warm-up; want 0 (memo missed)", name, years, allocs)
		}
	}
}

// The memo must be keyed by inflation rate as well as year: two calculators
// with different assumptions share the statutory table but not the scaled
// figures, and a hit must equal what a cold computation produces.
func TestResolveTaxYear_MemoKeyedByInflationRate(t *testing.T) {
	future := LatestStatutoryFederalTaxYear() + 9
	base := federalTaxYears[len(federalTaxYears)-1]
	for _, rate := range []float64{0, 2, 3.5} {
		tc := NewTaxCalculator(models.DefaultTaxConfig(), rate)
		want := base.StandardDeduction[models.FilingMarriedJoint] * math.Pow(1+rate/100, float64(future-base.Year))
		for pass := 0; pass < 2; pass++ { // cold, then memo hit
			resolved, err := tc.ResolveTaxYear(future, 1)
			if err != nil {
				t.Fatalf("rate %v pass %d: %v", rate, pass, err)
			}
			got := resolved.Record.StandardDeduction[models.FilingMarriedJoint]
			if math.Abs(got-want) > 1e-6 {
				t.Errorf("rate %v pass %d: MFJ standard deduction %v, want %v", rate, pass, got, want)
			}
			if resolved.Record.Year != future || !resolved.Projected() {
				t.Errorf("rate %v pass %d: resolved %+v is not a projection of %d", rate, pass, resolved, future)
			}
		}
	}
}
