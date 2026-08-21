package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// Golden cases and property tests transcribed from FINANCEAPPCONCERNS.md
// (§4, §9). The source document records a live tax-planning session for an
// MFJ household in Rochester NY, 2026, and publishes hand-verified answers.
//
// Only the statute-year-independent claims are asserted here. The document's
// absolute 2026 dollar figures depend on constants this engine does not carry
// (2026 statutory brackets, the $6,000 senior deduction, NY-specific AGI
// rules, ACA premium credits); those live in finance_concerns_gaps_test.go
// behind the `financegaps` build tag so this suite stays green.
//
// Fixture (FINANCEAPPCONCERNS.md §9):
//
//	Household: MFJ, both 50+, one spouse 65+, Rochester NY, 2026.
//	Wages $60,000. Social Security gross $32,919. Qualified dividends $2,000.
const (
	fcWages         = 60000.0
	fcSSGross       = 32919.0
	fcQualifiedDivs = 2000.0
	fcIRADeduction  = 17200.0
	fcRealizedGain  = 71868.0
)

// fcCalculator builds a TaxCalculator for the golden household with
// inflation indexing switched off, so results depend only on the bundled
// base-year tables and not on a projection offset. State rate is zero to
// isolate federal behaviour; NY is covered in the gaps suite.
func fcCalculator(t *testing.T) *TaxCalculator {
	t.Helper()
	zero := 0.0
	return NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingMarriedJoint,
		StateIncomeTaxRate: &zero,
		Age65Count:         1, // one spouse 65+
	}, 0)
}

// TestFinanceConcerns_ProvisionalIncomeDefinition pins the §86 provisional
// income definition the engine uses: other income plus one half of gross
// Social Security. FINANCEAPPCONCERNS.md §3 lists this as one of six
// distinct income measures; getting it wrong silently mis-taxes benefits.
//
// Probed at a low income level, because at the golden household's income
// the 85% cap binds and provisional income is no longer observable through
// the return value.
func TestFinanceConcerns_ProvisionalIncomeDefinition(t *testing.T) {
	const ss = 20000.0

	tests := []struct {
		name        string
		otherIncome float64
		want        float64
		why         string
	}{
		{
			name:        "below base threshold: none taxable",
			otherIncome: 20000, // PI = 20,000 + 10,000 = 30,000 < 32,000
			want:        0,
			why:         "provisional income under the $32,000 MFJ base threshold",
		},
		{
			name:        "inside the 50% phase-in band",
			otherIncome: 25000, // PI = 25,000 + 10,000 = 35,000
			want:        1500,  // (35,000 - 32,000) * 0.50
			why:         "50 cents of benefit per dollar over the base threshold",
		},
		{
			name:        "exactly at the base threshold",
			otherIncome: 22000, // PI = 22,000 + 10,000 = 32,000
			want:        0,
			why:         "the threshold is inclusive; one dollar more starts the phase-in",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateTaxableSocialSecurity(ss, tc.otherIncome, 0, 0, models.FilingMarriedJoint, false)
			if math.Abs(got-tc.want) > 0.01 {
				t.Errorf("taxable SS = %.2f, want %.2f (%s)", got, tc.want, tc.why)
			}
		})
	}
}

// TestFinanceConcerns_TaxableSocialSecurityHitsCap reproduces the first
// golden row of FINANCEAPPCONCERNS.md §9: taxable Social Security $27,981,
// with the 85% cap reached.
//
// §86 thresholds are not inflation-indexed in statute, and this engine
// correctly does not index them, so the golden value must reproduce exactly
// regardless of projection year.
func TestFinanceConcerns_TaxableSocialSecurityHitsCap(t *testing.T) {
	got := CalculateTaxableSocialSecurity(fcSSGross, fcWages, fcQualifiedDivs, 0, models.FilingMarriedJoint, false)

	// The document rounds to $27,981; the exact 85% cap is 0.85 x 32,919.
	want := fcSSGross * 0.85
	if math.Abs(got-want) > 0.01 {
		t.Errorf("taxable SS = %.2f, want %.2f (FINANCEAPPCONCERNS.md §9 golden: $27,981)", got, want)
	}

	// Confirm it is the cap that binds, not the phase-in formula — the
	// document's parenthetical "(85% cap reached)" is part of the claim.
	provisional := fcWages + fcQualifiedDivs + 0.5*fcSSGross
	uncapped := 6000 + (provisional-44000)*0.85
	if uncapped <= want {
		t.Errorf("expected the 85%% cap to bind: uncapped formula = %.2f, cap = %.2f", uncapped, want)
	}

	// FINANCEAPPCONCERNS.md §9 golden: provisional income $78,460.
	if math.Abs(provisional-78459.50) > 0.51 {
		t.Errorf("provisional income = %.2f, want 78,459.50 (document rounds to $78,460)", provisional)
	}
}

// TestFinanceConcerns_GainStackingLeverage is the test FINANCEAPPCONCERNS.md
// §9 calls "the important test": the only difference between two scenarios is
// a $17,200 above-the-line deduction, and it moves total tax far more than the
// household's nominal 12% bracket would suggest — "mostly by relocating
// [gain] from the 15% band to 0%".
//
// §4 states the expected effective marginal rate on ordinary income in this
// zone explicitly: "27%, not 12%". Every dollar of ordinary income removed
// saves 12 cents of ordinary tax AND pushes one dollar of long-term gain from
// the 15% band down into the 0% band, saving another 15 cents.
//
// That 12 + 15 = 27 decomposition depends only on which brackets the household
// straddles, not on the statutory year, so it is asserted exactly here. If
// this test fails, gain stacking is wrong.
func TestFinanceConcerns_GainStackingLeverage(t *testing.T) {
	tc := fcCalculator(t)

	federalTaxFor := func(wages float64) float64 {
		taxableSS := CalculateTaxableSocialSecurity(
			fcSSGross, wages, fcQualifiedDivs, fcRealizedGain, models.FilingMarriedJoint, false)
		ordinary := wages + taxableSS
		fed, _, _, _ := tc.CalculateTaxWithInvestmentIncome(ordinary, fcQualifiedDivs, fcRealizedGain, 0)
		return fed
	}

	withoutIRA := federalTaxFor(fcWages)
	withIRA := federalTaxFor(fcWages - fcIRADeduction)
	saving := withoutIRA - withIRA

	if saving <= 0 {
		t.Fatalf("a deduction must not increase tax: without = %.2f, with = %.2f", withoutIRA, withIRA)
	}

	effectiveMarginal := saving / fcIRADeduction * 100
	const wantEffective = 27.0

	if math.Abs(effectiveMarginal-wantEffective) > 0.05 {
		t.Errorf("effective marginal rate on the deducted slice = %.2f%%, want %.2f%%\n"+
			"  federal tax without the $%.0f deduction: %.2f\n"+
			"  federal tax with    the $%.0f deduction: %.2f\n"+
			"  saving: %.2f\n"+
			"FINANCEAPPCONCERNS.md §4: 12%% ordinary + 15%% gain relocation = 27%%. "+
			"A mismatch here means capital-gain stacking is wrong.",
			effectiveMarginal, wantEffective,
			fcIRADeduction, withoutIRA, fcIRADeduction, withIRA, saving)
	}

	// The whole point of §4: the bracket table does not describe this.
	nominal := tc.GetBracketRate(fcWages+CalculateTaxableSocialSecurity(
		fcSSGross, fcWages, fcQualifiedDivs, fcRealizedGain, models.FilingMarriedJoint, false), 0)
	if effectiveMarginal <= nominal {
		t.Errorf("expected the effective marginal rate (%.2f%%) to exceed the nominal bracket (%.2f%%)",
			effectiveMarginal, nominal)
	}
	t.Logf("effective marginal %.2f%% vs nominal bracket %.2f%% — a %.1fx understatement",
		effectiveMarginal, nominal, effectiveMarginal/nominal)
}

// TestFinanceConcerns_PropertyTaxNonDecreasingInIncome asserts the first
// property test of FINANCEAPPCONCERNS.md §9: "Total tax is non-decreasing in
// income except at subsidy cliffs — and the set of places where it decreases
// should exactly equal the cliff registry."
//
// This engine models no subsidy cliffs inside the income-tax path, so the
// exception set is empty and the invariant must hold everywhere.
func TestFinanceConcerns_PropertyTaxNonDecreasingInIncome(t *testing.T) {
	tc := fcCalculator(t)

	prev := math.Inf(-1)
	prevIncome := 0.0
	for income := 0.0; income <= 500000; income += 250 {
		taxableSS := CalculateTaxableSocialSecurity(
			fcSSGross, income, fcQualifiedDivs, fcRealizedGain, models.FilingMarriedJoint, false)
		_, _, total, _ := tc.CalculateTaxWithInvestmentIncome(
			income+taxableSS, fcQualifiedDivs, fcRealizedGain, 0)

		if total < prev-0.005 {
			t.Fatalf("total tax decreased with income: %.2f at income %.0f -> %.2f at income %.0f\n"+
				"FINANCEAPPCONCERNS.md §9: a decrease is only legal at a registered cliff, "+
				"and this engine registers none in the income-tax path.",
				prev, prevIncome, total, income)
		}
		prev, prevIncome = total, income
	}
}

// TestFinanceConcerns_PropertyDeductionsNeverIncreaseTax asserts the third
// property test of FINANCEAPPCONCERNS.md §9. Swept across the whole range
// where gain stacking, the §86 phase-in and the bracket edges interact.
func TestFinanceConcerns_PropertyDeductionsNeverIncreaseTax(t *testing.T) {
	tc := fcCalculator(t)

	for _, gain := range []float64{0, 25000, fcRealizedGain, 200000} {
		prev := math.Inf(1)
		prevDeduction := 0.0
		for deduction := 0.0; deduction <= 60000; deduction += 100 {
			wages := math.Max(0, fcWages*3-deduction)
			taxableSS := CalculateTaxableSocialSecurity(
				fcSSGross, wages, fcQualifiedDivs, gain, models.FilingMarriedJoint, false)
			_, _, total, _ := tc.CalculateTaxWithInvestmentIncome(
				wages+taxableSS, fcQualifiedDivs, gain, 0)

			if total > prev+0.005 {
				t.Fatalf("gain %.0f: a larger deduction increased total tax: "+
					"%.2f at deduction %.0f -> %.2f at deduction %.0f",
					gain, prev, prevDeduction, total, deduction)
			}
			prev, prevDeduction = total, deduction
		}
	}
}

// TestFinanceConcerns_MarginalRateIsNumericDerivative asserts the §4
// requirement directly: "compute marginal cost numerically. (total_cost(income
// + δ) − total_cost(income)) / δ. Never read a rate off a bracket table and
// present it as the marginal rate."
//
// MarginalRateOnOrdinaryIncome must agree with an independently hand-rolled
// derivative of the tax function, and must diverge from GetBracketRate exactly
// where §4 says it should — in the zone where long-term gains straddle the
// 0%/15% boundary.
func TestFinanceConcerns_MarginalRateIsNumericDerivative(t *testing.T) {
	tc := fcCalculator(t)
	const delta = 100.0

	inputsAt := func(wages float64) ProjectedAnnualTaxInputs {
		return ProjectedAnnualTaxInputs{
			OrdinaryIncome:       wages,
			SocialSecurityIncome: fcSSGross,
			QualifiedDividends:   fcQualifiedDivs,
			LongTermCapitalGains: fcRealizedGain,
		}
	}

	// Independent oracle: differentiate total tax by hand, going through the
	// public tax entry points rather than the function under test.
	handRolledDerivative := func(wages float64) float64 {
		cost := func(w float64) float64 {
			taxableSS := CalculateTaxableSocialSecurity(
				fcSSGross, w, fcQualifiedDivs, fcRealizedGain, models.FilingMarriedJoint, false)
			_, _, total, _ := tc.CalculateTaxWithInvestmentIncome(
				w+taxableSS, fcQualifiedDivs, fcRealizedGain, 0)
			return total
		}
		return (cost(wages+delta) - cost(wages)) / delta * 100
	}

	sawDivergence := false
	for wages := 10000.0; wages <= 160000; wages += 10000 {
		got := tc.MarginalRateOnOrdinaryIncome(inputsAt(wages), 0)
		want := handRolledDerivative(wages)

		if math.Abs(got-want) > 0.01 {
			t.Errorf("wages %.0f: MarginalRateOnOrdinaryIncome = %.4f%%, "+
				"hand-rolled derivative = %.4f%%", wages, got, want)
		}

		taxableSS := CalculateTaxableSocialSecurity(
			fcSSGross, wages, fcQualifiedDivs, fcRealizedGain, models.FilingMarriedJoint, false)
		bracket := tc.GetBracketRate(wages+taxableSS, 0)
		if got > bracket+1.0 {
			sawDivergence = true
		}
		t.Logf("wages %9.0f  marginal %6.2f%%  bracket %6.2f%%", wages, got, bracket)
	}

	if !sawDivergence {
		t.Error("expected the numeric marginal rate to exceed the bracket rate somewhere " +
			"in the sweep; if it never does, the derivative is not seeing gain stacking")
	}
}

// TestFinanceConcerns_MarginalRateHeadline pins FINANCEAPPCONCERNS.md §4's
// worked example: for this household the effective marginal rate on ordinary
// income is "27%, not 12%".
func TestFinanceConcerns_MarginalRateHeadline(t *testing.T) {
	tc := fcCalculator(t)

	in := ProjectedAnnualTaxInputs{
		OrdinaryIncome:       fcWages,
		SocialSecurityIncome: fcSSGross,
		QualifiedDividends:   fcQualifiedDivs,
		LongTermCapitalGains: fcRealizedGain,
	}

	marginal := tc.MarginalRateOnOrdinaryIncome(in, 0)
	if math.Abs(marginal-27.0) > 0.05 {
		t.Errorf("effective marginal rate = %.2f%%, want 27.00%% "+
			"(FINANCEAPPCONCERNS.md §4: 12%% ordinary + 15%% gain relocation)", marginal)
	}

	taxableSS := CalculateTaxableSocialSecurity(
		fcSSGross, fcWages, fcQualifiedDivs, fcRealizedGain, models.FilingMarriedJoint, false)
	bracket := tc.GetBracketRate(fcWages+taxableSS, 0)
	if math.Abs(bracket-12.0) > 0.01 {
		t.Errorf("statutory bracket = %.2f%%, want 12.00%%", bracket)
	}
}

// TestFinanceConcerns_MarginalRateSeesSocialSecurityPhaseIn covers the other
// amplifier §4 names: "Between the thresholds, each $1 of income drags $0.85
// of benefits into taxation — effective rates north of 40%."
//
// Probed with no capital gains at all, so the only amplifier in play is § 86.
func TestFinanceConcerns_MarginalRateSeesSocialSecurityPhaseIn(t *testing.T) {
	tc := fcCalculator(t)

	// Inside the MFJ phase-in band: provisional income between $32k and $44k,
	// and far enough into the 85% ramp that each dollar drags $0.85.
	in := ProjectedAnnualTaxInputs{
		OrdinaryIncome:       36000,
		SocialSecurityIncome: fcSSGross,
	}

	marginal := tc.MarginalRateOnOrdinaryIncome(in, 0)
	bracket := tc.GetBracketRate(in.OrdinaryIncome+
		CalculateTaxableSocialSecurity(fcSSGross, in.OrdinaryIncome, 0, 0, models.FilingMarriedJoint, false), 0)

	if marginal <= bracket {
		t.Errorf("marginal rate %.2f%% should exceed the %.2f%% bracket inside the "+
			"§86 phase-in: each extra dollar also drags $0.85 of benefits into tax",
			marginal, bracket)
	}
	t.Logf("§86 phase-in: marginal %.2f%% vs bracket %.2f%%", marginal, bracket)
}
