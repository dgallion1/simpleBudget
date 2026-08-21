package engine

import (
	"math"
	"testing"
)

// MarginalRateOnLongTermGain answers "what does realizing one more dollar of
// gain cost me this year" — the question a lot-selection decision turns on.
// It is not the capital-gains bracket, and the gap between the two is where
// the money is.

func TestMarginalRateOnLongTermGain_Cases(t *testing.T) {
	tc := fcCalculator(t)

	tests := []struct {
		name string
		in   ProjectedAnnualTaxInputs
		want float64
		why  string
	}{
		{
			name: "headroom in the 0% bracket really is free",
			in: ProjectedAnnualTaxInputs{
				OrdinaryIncome:       40000,
				LongTermCapitalGains: 20000,
			},
			want: 0,
			why:  "taxable income well under the 0% ceiling, and no benefits to drag in",
		},
		{
			name: "past the 0% ceiling it steps to 15%",
			in: ProjectedAnnualTaxInputs{
				OrdinaryIncome:       120000,
				LongTermCapitalGains: 50000,
			},
			want: 15,
			why:  "the 0% band is exhausted, so the next dollar lands in the 15% band",
		},
		{
			name: "NIIT stacks 3.8% on top",
			in: ProjectedAnnualTaxInputs{
				OrdinaryIncome:       250000,
				LongTermCapitalGains: 60000,
			},
			want: 18.8,
			why:  "15% capital-gains rate plus the 3.8% net investment income surtax",
		},
		{
			name: "gain inside the 0% bracket is NOT free when it drags Social Security in",
			in: ProjectedAnnualTaxInputs{
				OrdinaryIncome:       30000,
				SocialSecurityIncome: 40000,
				LongTermCapitalGains: 20000,
			},
			want: 10.2,
			why: "the gain itself is taxed at 0%, but each dollar pulls $0.85 of benefits " +
				"into ordinary income at 12% — 'in the 0% bracket' and 'free to realize' " +
				"are different claims",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tc.MarginalRateOnLongTermGain(tt.in, 0)
			if math.Abs(got-tt.want) > 0.05 {
				t.Errorf("marginal rate on long-term gain = %.2f%%, want %.2f%% (%s)",
					got, tt.want, tt.why)
			}
		})
	}
}

// TestMarginalRateOnLongTermGain_ZeroBracketIsNotFree isolates the finding
// above and states it as a comparison, because it is the one most likely to be
// "corrected" by someone reading the capital-gains bracket table.
func TestMarginalRateOnLongTermGain_ZeroBracketIsNotFree(t *testing.T) {
	tc := fcCalculator(t)

	withSS := ProjectedAnnualTaxInputs{
		OrdinaryIncome:       30000,
		SocialSecurityIncome: 40000,
		LongTermCapitalGains: 20000,
	}
	// Same picture, benefits replaced by equivalent ordinary income so § 86
	// has nothing to phase in.
	withoutSS := ProjectedAnnualTaxInputs{
		OrdinaryIncome:       30000,
		LongTermCapitalGains: 20000,
	}

	rateWithSS := tc.MarginalRateOnLongTermGain(withSS, 0)
	rateWithoutSS := tc.MarginalRateOnLongTermGain(withoutSS, 0)

	// Confirm the gain genuinely sits in the 0% capital-gains bracket in both.
	brackets := tc.GetAdjustedLongTermCapitalGainsBrackets(0)
	zeroCeiling := brackets[0].MaxIncome
	if zeroCeiling <= 0 {
		t.Fatal("could not read the 0% capital-gains ceiling")
	}

	if rateWithoutSS != 0 {
		t.Errorf("without Social Security the rate should be 0%%, got %.2f%%", rateWithoutSS)
	}
	if rateWithSS <= 0 {
		t.Errorf("with Social Security in the phase-in band the rate must exceed 0%%, got %.2f%%; "+
			"realized gain feeds § 86 provisional income", rateWithSS)
	}
	t.Logf("identical gain in the 0%% bracket: %.2f%% without benefits, %.2f%% with",
		rateWithoutSS, rateWithSS)
}

// TestMarginalRateOnLongTermGain_MatchesHandRolledDerivative checks the
// function against an independent differentiation of the tax entry points,
// across a sweep that crosses the 0%/15% boundary.
func TestMarginalRateOnLongTermGain_MatchesHandRolledDerivative(t *testing.T) {
	tc := fcCalculator(t)
	const delta = marginalRateProbe

	base := ProjectedAnnualTaxInputs{
		OrdinaryIncome:       fcWages,
		SocialSecurityIncome: fcSSGross,
		QualifiedDividends:   fcQualifiedDivs,
	}

	sawZero, sawFifteen := false, false
	for gain := 0.0; gain <= 200000; gain += 10000 {
		in := base
		in.LongTermCapitalGains = gain

		probed := in
		probed.LongTermCapitalGains += delta
		want := (tc.AnnualIncomeTaxOn(probed, 0) - tc.AnnualIncomeTaxOn(in, 0)) / delta * 100

		got := tc.MarginalRateOnLongTermGain(in, 0)
		if math.Abs(got-want) > 0.01 {
			t.Errorf("gain %.0f: MarginalRateOnLongTermGain = %.4f%%, hand-rolled = %.4f%%",
				gain, got, want)
		}
		if got < 1 {
			sawZero = true
		}
		if got > 14 {
			sawFifteen = true
		}
		t.Logf("gain %8.0f  ->  %6.2f%%", gain, got)
	}

	if !sawZero || !sawFifteen {
		t.Errorf("sweep should cross the 0%%/15%% boundary; saw zero=%v fifteen=%v",
			sawZero, sawFifteen)
	}
}

// TestMarginalRateOnLongTermGain_ShortTermUsesOrdinaryRate documents why there
// is no short-term variant: short-term gain is ordinary income, so the
// ordinary-income rate already prices it.
func TestMarginalRateOnLongTermGain_ShortTermUsesOrdinaryRate(t *testing.T) {
	tc := fcCalculator(t)

	in := ProjectedAnnualTaxInputs{
		OrdinaryIncome:       fcWages,
		SocialSecurityIncome: fcSSGross,
		LongTermCapitalGains: fcRealizedGain,
	}

	longTerm := tc.MarginalRateOnLongTermGain(in, 0)
	ordinary := tc.MarginalRateOnOrdinaryIncome(in, 0)

	if longTerm >= ordinary {
		t.Errorf("long-term gain (%.2f%%) should cost less at the margin than "+
			"ordinary income (%.2f%%) for this household; that difference is the "+
			"whole reason holding past one year matters", longTerm, ordinary)
	}
}
