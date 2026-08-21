package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// Short-term capital gain is gain on a position held one year or less. The
// preferential rates of 26 USC § 1(h) apply only to net LONG-term gain, so
// short-term gain is taxed as ordinary income — and, being investment income,
// it also counts toward NIIT and toward the § 86 provisional-income base.
//
// Before this change the engine had no short-term concept at all: every
// realized gain was treated as long-term.

// TestShortTermGain_TaxedAsOrdinaryIncome is the headline claim. A dollar of
// short-term gain must cost exactly what a dollar of wages costs.
func TestShortTermGain_TaxedAsOrdinaryIncome(t *testing.T) {
	tc := fcCalculator(t)
	const gain = 40000.0

	asWages := tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
		OrdinaryIncome: 80000 + gain,
	}, 0)
	asShortTerm := tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
		OrdinaryIncome:        80000,
		ShortTermCapitalGains: gain,
	}, 0)

	if math.Abs(asWages.TotalTax-asShortTerm.TotalTax) > 0.01 {
		t.Errorf("short-term gain taxed at %.2f but the same amount of wages costs %.2f; "+
			"§ 1(h) preferential rates apply only to net long-term gain",
			asShortTerm.TotalTax, asWages.TotalTax)
	}
	if math.Abs(asWages.MAGI-asShortTerm.MAGI) > 0.01 {
		t.Errorf("MAGI = %.2f with short-term gain, %.2f with wages; "+
			"short-term gain is part of AGI", asShortTerm.MAGI, asWages.MAGI)
	}
}

// TestShortTermGain_CostsMoreThanLongTerm is the distinction that matters to a
// user deciding whether to hold a position past the one-year mark.
func TestShortTermGain_CostsMoreThanLongTerm(t *testing.T) {
	tc := fcCalculator(t)
	const gain = 60000.0

	longTerm := tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
		OrdinaryIncome:       90000,
		LongTermCapitalGains: gain,
	}, 0)
	shortTerm := tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
		OrdinaryIncome:        90000,
		ShortTermCapitalGains: gain,
	}, 0)

	if shortTerm.TotalTax <= longTerm.TotalTax {
		t.Fatalf("short-term gain (%.2f) must cost more than long-term (%.2f)",
			shortTerm.TotalTax, longTerm.TotalTax)
	}
	t.Logf("$%.0f of gain: long-term %.2f, short-term %.2f (+%.2f, %.1f%% of the gain)",
		gain, longTerm.TotalTax, shortTerm.TotalTax,
		shortTerm.TotalTax-longTerm.TotalTax,
		(shortTerm.TotalTax-longTerm.TotalTax)/gain*100)
}

// TestShortTermGain_CrowdsLongTermOutOfZeroBracket connects short-term gain to
// the capital-gain stacking behaviour measured in the §4 work: because
// short-term gain is ordinary income, it pushes long-term gain out of the 0%
// band exactly as wages do.
//
// The golden household sits in the 12% bracket with long-term gains straddling
// the 0%/15% boundary, so each dollar of short-term gain should cost 12 cents
// directly plus 15 cents of relocation — 27%, the same figure wages produce.
func TestShortTermGain_CrowdsLongTermOutOfZeroBracket(t *testing.T) {
	tc := fcCalculator(t)
	const probe = 10000.0

	taxFor := func(shortTerm float64) float64 {
		taxableSS := CalculateTaxableSocialSecurity(
			fcSSGross, fcWages+shortTerm, fcQualifiedDivs, fcRealizedGain,
			models.FilingMarriedJoint, false)
		return tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
			OrdinaryIncome:        fcWages + taxableSS,
			QualifiedDividends:    fcQualifiedDivs,
			LongTermCapitalGains:  fcRealizedGain,
			ShortTermCapitalGains: shortTerm,
		}, 0).TotalTax
	}

	effective := (taxFor(probe) - taxFor(0)) / probe * 100
	const want = 27.0

	if math.Abs(effective-want) > 0.05 {
		t.Errorf("effective rate on short-term gain = %.2f%%, want %.2f%% "+
			"(12%% ordinary + 15%% long-term relocation)", effective, want)
	}
}

// TestShortTermGain_CountsAsNetInvestmentIncome — short-term gain is
// investment income for NIIT even though it is taxed at ordinary rates.
func TestShortTermGain_CountsAsNetInvestmentIncome(t *testing.T) {
	tc := fcCalculator(t)
	// Well above the $250,000 MFJ NIIT threshold so the surtax is live.
	const wages = 300000.0
	const gain = 50000.0

	noInvestment := tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
		OrdinaryIncome: wages + gain,
	}, 0)
	withShortTerm := tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
		OrdinaryIncome:        wages,
		ShortTermCapitalGains: gain,
	}, 0)

	if noInvestment.NIIT != 0 {
		t.Fatalf("wages alone should attract no NIIT, got %.2f", noInvestment.NIIT)
	}
	if withShortTerm.NIIT <= 0 {
		t.Errorf("NIIT = %.2f; short-term gain is net investment income and must be "+
			"surtaxed above the threshold", withShortTerm.NIIT)
	}
	// Same ordinary rate, so the whole difference is the 3.8% surtax.
	wantNIIT := gain * 0.038
	if math.Abs(withShortTerm.NIIT-wantNIIT) > 0.01 {
		t.Errorf("NIIT = %.2f, want %.2f (3.8%% of the gain)", withShortTerm.NIIT, wantNIIT)
	}
}

// TestShortTermGain_EntersProvisionalIncome — § 86 provisional income is built
// from AGI before benefits, and short-term gain is part of AGI. Omitting it
// would understate taxable Social Security for anyone realising short-term
// gain inside the phase-in band.
func TestShortTermGain_EntersProvisionalIncome(t *testing.T) {
	tc := fcCalculator(t)

	// Inside the MFJ phase-in band, where every extra dollar drags benefits
	// into taxation. Well clear of the 85% cap so movement is observable.
	base := ProjectedAnnualTaxInputs{
		OrdinaryIncome:       30000,
		SocialSecurityIncome: fcSSGross,
	}
	withGain := base
	withGain.ShortTermCapitalGains = 8000

	ssBase := taxableSocialSecurityFor(tc, base)
	ssWithGain := taxableSocialSecurityFor(tc, withGain)

	if ssWithGain <= ssBase {
		t.Errorf("taxable Social Security = %.2f with short-term gain, %.2f without; "+
			"short-term gain belongs in the provisional-income base", ssWithGain, ssBase)
	}

	// It must drag benefits in at the same rate wages would.
	asWages := base
	asWages.OrdinaryIncome += 8000
	if got, want := ssWithGain, taxableSocialSecurityFor(tc, asWages); math.Abs(got-want) > 0.01 {
		t.Errorf("taxable SS = %.2f from short-term gain but %.2f from the same wages; "+
			"provisional income does not distinguish them", got, want)
	}
}

// TestShortTermGain_NotDoubleCounted guards the seam introduced here:
// CalculateTaxBreakdown folds ShortTermCapitalGains into ordinary income
// itself, so ordinaryIncomeBeforeSocialSecurity must NOT also include it.
// Counting it in both places would tax it twice, silently.
func TestShortTermGain_NotDoubleCounted(t *testing.T) {
	tc := fcCalculator(t)
	const amount = 25000.0

	// Below the NIIT threshold, short-term gain and wages are interchangeable,
	// so the two compositions must produce identical tax.
	asGain := ProjectedAnnualTaxInputs{
		OrdinaryIncome:        70000,
		ShortTermCapitalGains: amount,
	}
	asWages := ProjectedAnnualTaxInputs{
		OrdinaryIncome: 70000 + amount,
	}

	gainTax := tc.AnnualIncomeTaxOn(asGain, 0)
	wagesTax := tc.AnnualIncomeTaxOn(asWages, 0)

	if math.Abs(gainTax-wagesTax) > 0.01 {
		t.Errorf("AnnualIncomeTaxOn = %.2f with short-term gain vs %.2f with the "+
			"equivalent wages. A mismatch here means the gain is counted twice "+
			"(or not at all) somewhere between ordinaryIncomeBeforeSocialSecurity "+
			"and CalculateTaxBreakdown.", gainTax, wagesTax)
	}
}

// TestShortTermGain_MarginalRateSeesIt — the numeric marginal rate is measured
// from the whole composition, so short-term gain in the picture must change
// what the next ordinary dollar costs.
//
// The amount matters. $30,000 moves nothing: taxable ordinary income rises to
// $87,231, still inside the 12% bracket, and $6,819 of the 0% long-term band
// survives — so the rate stays at 12 + 15 = 27%. $50,000 crosses both edges at
// once, lifting ordinary income into the 22% bracket AND exhausting the 0%
// band, which removes the 15-point relocation penalty entirely. The rate
// therefore falls to a flat 22%, which is the jagged behaviour a bracket-table
// lookup could never show.
func TestShortTermGain_MarginalRateSeesIt(t *testing.T) {
	tc := fcCalculator(t)

	compositionWith := func(shortTerm float64) ProjectedAnnualTaxInputs {
		return ProjectedAnnualTaxInputs{
			OrdinaryIncome:        fcWages,
			SocialSecurityIncome:  fcSSGross,
			QualifiedDividends:    fcQualifiedDivs,
			LongTermCapitalGains:  fcRealizedGain,
			ShortTermCapitalGains: shortTerm,
		}
	}

	tests := []struct {
		name      string
		shortTerm float64
		want      float64
		why       string
	}{
		{
			name:      "no short-term gain",
			shortTerm: 0,
			want:      27.0,
			why:       "12% bracket plus 15% of long-term relocation",
		},
		{
			name:      "not enough to cross a boundary",
			shortTerm: 30000,
			want:      27.0,
			why:       "still in the 12% bracket with 0% long-term band left; nothing moves",
		},
		{
			name:      "crosses into the 22% bracket and exhausts the 0% band",
			shortTerm: 50000,
			want:      22.0,
			why:       "22% ordinary, and no 0% long-term band left to be pushed out of",
		},
	}

	for _, tc2 := range tests {
		t.Run(tc2.name, func(t *testing.T) {
			got := tc.MarginalRateOnOrdinaryIncome(compositionWith(tc2.shortTerm), 0)
			if math.Abs(got-tc2.want) > 0.05 {
				t.Errorf("marginal rate = %.2f%%, want %.2f%% (%s)", got, tc2.want, tc2.why)
			}
		})
	}

	// The point of the test: the composition changes the answer.
	if a, b := tc.MarginalRateOnOrdinaryIncome(compositionWith(0), 0),
		tc.MarginalRateOnOrdinaryIncome(compositionWith(50000), 0); math.Abs(a-b) < 0.01 {
		t.Errorf("marginal rate unchanged (%.2f%%) by $50,000 of short-term gain; "+
			"the derivative is not seeing it in the composition", a)
	}
}

// TestShortTermGain_AnnualizedThroughAccumulator covers the projection
// plumbing: a month's short-term gain must annualize like every other
// component so a producer can supply it.
func TestShortTermGain_AnnualizedThroughAccumulator(t *testing.T) {
	acc := ProjectionTaxAccumulator{ShortTermCapitalGainsYTD: 1000}

	// Month index 2 → three months elapsed → factor 4.
	got := acc.AnnualizedInputs(2, MonthlyTaxInputs{ShortTermCapitalGains: 500})
	if want := (1000.0 + 500.0) * 4; math.Abs(got.ShortTermCapitalGains-want) > 0.01 {
		t.Errorf("annualized short-term gain = %.2f, want %.2f", got.ShortTermCapitalGains, want)
	}

	var folded ProjectionTaxAccumulator
	folded.ApplyMonth(RealizedMonthIncome{ShortTermCapitalGains: 750})
	if folded.ShortTermCapitalGainsYTD != 750 {
		t.Errorf("ShortTermCapitalGainsYTD = %.2f, want 750", folded.ShortTermCapitalGainsYTD)
	}
}
