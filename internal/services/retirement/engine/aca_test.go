package engine

import (
	"math"
	"strings"
	"testing"

	"budget2/internal/models"
)

func acaCalculator(t *testing.T) *TaxCalculator {
	t.Helper()
	zero := 0.0
	return NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingMarriedJoint,
		StateIncomeTaxRate: &zero,
	}, 3.0)
}

func acaOptions(cfg *models.ACAConfig, coverageYear int) ThresholdRegistryOptions {
	return ThresholdRegistryOptions{
		CoverageYear:        coverageYear,
		ACA:                 cfg,
		MarketplaceEnrolled: true,
	}
}

// TestACAModifiedAGI_CountsAllSocialSecurity is the finding that makes this
// worth modelling separately. A household can sit comfortably under the
// subsidy cliff on adjusted gross income and be well over it on ACA MAGI,
// because ACA counts every dollar of Social Security while § 86 counts half
// and AGI counts only the taxable portion.
func TestACAModifiedAGI_CountsAllSocialSecurity(t *testing.T) {
	tc := acaCalculator(t)
	in := ProjectedAnnualTaxInputs{
		OrdinaryIncome:       30000,
		SocialSecurityIncome: 60000,
	}

	taxableSS := taxableSocialSecurityFor(tc, in)
	adjustedGross := in.OrdinaryIncome + taxableSS
	acaMAGI := ACAModifiedAGI(tc, in)

	// Ordinary income plus 100% of benefits, by construction.
	if want := in.OrdinaryIncome + in.SocialSecurityIncome; math.Abs(acaMAGI-want) > 0.01 {
		t.Errorf("ACA MAGI = %.2f, want %.2f (all benefits counted)", acaMAGI, want)
	}
	if acaMAGI <= adjustedGross {
		t.Fatalf("ACA MAGI %.2f should exceed AGI %.2f when benefits are partly untaxed",
			acaMAGI, adjustedGross)
	}

	// The three measures must genuinely disagree — that is the whole point.
	provisional := ProvisionalIncome(in)
	if provisional >= acaMAGI {
		t.Errorf("provisional income %.2f should be below ACA MAGI %.2f: § 86 counts "+
			"half of benefits, ACA counts all of them", provisional, acaMAGI)
	}
	t.Logf("same household: AGI %.0f, provisional %.0f, ACA MAGI %.0f",
		adjustedGross, provisional, acaMAGI)
}

// TestACAModifiedAGI_AddsBackTaxExemptInterest — municipal interest is
// invisible in taxable income but counts here, so it walks a household toward
// the cliff without appearing in any tax figure.
func TestACAModifiedAGI_AddsBackTaxExemptInterest(t *testing.T) {
	tc := acaCalculator(t)
	base := ProjectedAnnualTaxInputs{OrdinaryIncome: 50000}
	withMuni := base
	withMuni.TaxExemptInterest = 12000

	if got, want := ACAModifiedAGI(tc, withMuni)-ACAModifiedAGI(tc, base), 12000.0; math.Abs(got-want) > 0.01 {
		t.Errorf("tax-exempt interest moved ACA MAGI by %.2f, want %.2f", got, want)
	}
	// And it must not have changed the tax owed.
	if a, b := tc.AnnualIncomeTaxOn(base, 0), tc.AnnualIncomeTaxOn(withMuni, 0); math.Abs(a-b) > 0.01 {
		t.Errorf("tax-exempt interest changed income tax from %.2f to %.2f; it is exempt", a, b)
	}
}

// TestPovertyGuidelineFor_UsesThePriorYearTable — marketplace eligibility for
// a coverage year is measured against the guidelines published before that
// year's open enrolment, which are the prior year's. Reading the same year's
// table would misplace the cliff by a year of indexation.
func TestPovertyGuidelineFor_UsesThePriorYearTable(t *testing.T) {
	tc := acaCalculator(t)

	resolved, err := tc.PovertyGuidelineFor(2025)
	if err != nil {
		t.Fatalf("PovertyGuidelineFor(2025): %v", err)
	}
	if resolved.GuidelineYear != 2024 {
		t.Errorf("coverage year 2025 used the %d guidelines, want 2024",
			resolved.GuidelineYear)
	}
	if resolved.Basis != BasisStatutory {
		t.Errorf("Basis = %q; the 2024 guidelines are published, not projected", resolved.Basis)
	}
	if resolved.Record.Provenance.Source == "" || resolved.Record.Provenance.VerifiedOn == "" {
		t.Errorf("published guidelines must carry provenance, got %+v", resolved.Record.Provenance)
	}
}

func TestPovertyGuidelineFor_ProjectsAndFailsLoudly(t *testing.T) {
	tc := acaCalculator(t)

	projected, err := tc.PovertyGuidelineFor(2031)
	if err != nil {
		t.Fatalf("PovertyGuidelineFor(2031): %v", err)
	}
	if !projected.Projected() {
		t.Error("a coverage year with no published guidelines must be marked projected")
	}
	if projected.DerivedFromYear != 2024 {
		t.Errorf("DerivedFromYear = %d, want 2024", projected.DerivedFromYear)
	}
	if projected.Record.OnePerson <= povertyGuidelines[0].OnePerson {
		t.Errorf("projected one-person guideline %.2f did not rise from %.2f",
			projected.Record.OnePerson, povertyGuidelines[0].OnePerson)
	}

	// Earlier than any table is a bug, not a forecast.
	if _, err := tc.PovertyGuidelineFor(povertyGuidelines[0].Year); err == nil {
		t.Error("a coverage year before the earliest table should error, not silently reuse it")
	}
}

func TestFederalPovertyLevel_ScalesWithHousehold(t *testing.T) {
	cases := []struct {
		size int
		want float64
	}{
		{size: 1, want: 15060},
		{size: 2, want: 15060 + 5380},
		{size: 4, want: 15060 + 3*5380},
		{size: 0, want: 15060}, // there is no zero-person household
	}
	for _, c := range cases {
		if got := models.FederalPovertyLevel(15060, 5380, c.size); math.Abs(got-c.want) > 0.01 {
			t.Errorf("household of %d: %.2f, want %.2f", c.size, got, c.want)
		}
	}
}

// TestACACliff_RegisteredAtFourHundredPercent pins the location and price.
func TestACACliff_RegisteredAtFourHundredPercent(t *testing.T) {
	tc := acaCalculator(t)
	cfg := &models.ACAConfig{HouseholdSize: 2, AnnualPremiumTaxCredit: models.FloatPtr(9600)}

	th := findThreshold(t, tc.ThresholdRegistry(acaOptions(cfg, 2025)), "aca_premium_credit_cliff")

	if th.Kind != ThresholdCliff {
		t.Errorf("Kind = %q, want cliff — the credit does not taper", th.Kind)
	}
	if th.Measure != MeasureACAMAGI {
		t.Errorf("Measure = %q, want ACA MAGI; testing it against any other income "+
			"definition places the cliff in the wrong spot", th.Measure)
	}
	// 2025 coverage uses the published 2024 guidelines, so no indexation.
	want := (15060.0 + 5380.0) * 4
	if math.Abs(th.Amount-want) > 0.01 {
		t.Errorf("cliff at %.2f, want %.2f (400%% of the two-person guideline)", th.Amount, want)
	}
	if math.Abs(th.AnnualCostOfCrossing-9600) > 0.01 {
		t.Errorf("AnnualCostOfCrossing = %.2f, want the credit at stake, 9600",
			th.AnnualCostOfCrossing)
	}
}

func TestACACliff_NotRegisteredWithoutAStakeInIt(t *testing.T) {
	tc := acaCalculator(t)
	full := &models.ACAConfig{HouseholdSize: 2, AnnualPremiumTaxCredit: models.FloatPtr(9600)}

	tests := []struct {
		name string
		opts ThresholdRegistryOptions
		why  string
	}{
		{
			name: "nobody on a marketplace plan",
			opts: func() ThresholdRegistryOptions {
				o := acaOptions(full, 2025)
				o.MarketplaceEnrolled = false
				return o
			}(),
			why: "no credit to lose",
		},
		{
			name: "disqualified by COBRA or employer coverage",
			opts: func() ThresholdRegistryOptions {
				o := acaOptions(full, 2025)
				o.DisqualifiedFromPremiumCredit = true
				return o
			}(),
			why: "barred from the credit at any income, so there is no cliff",
		},
		{
			name: "household size unknown",
			opts: acaOptions(&models.ACAConfig{AnnualPremiumTaxCredit: models.FloatPtr(9600)}, 2025),
			why:  "the poverty level, and so the cliff, cannot be located",
		},
		{
			name: "no coverage year",
			opts: acaOptions(full, 0),
			why:  "which year's guidelines apply is unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, th := range tc.ThresholdRegistry(tt.opts) {
				if th.Code == "aca_premium_credit_cliff" {
					t.Errorf("registered an ACA cliff when %s", tt.why)
				}
			}
		})
	}
}

// TestACACliff_SaysWhenItsPriceIsUnknown — a cliff shown costing nothing reads
// as harmless. When the credit has not been supplied the cost is unmeasured,
// not zero, and the note has to say so.
func TestACACliff_SaysWhenItsPriceIsUnknown(t *testing.T) {
	tc := acaCalculator(t)
	cfg := &models.ACAConfig{HouseholdSize: 2}

	th := findThreshold(t, tc.ThresholdRegistry(acaOptions(cfg, 2025)), "aca_premium_credit_cliff")
	if th.AnnualCostOfCrossing != 0 {
		t.Errorf("cost = %.2f with no credit supplied, want 0", th.AnnualCostOfCrossing)
	}
	if !strings.Contains(th.Note, "unknown, not nil") {
		t.Errorf("note must distinguish an unpriced cliff from a free one; got: %s", th.Note)
	}
}

// TestACACliff_WarnsAboutAdvanceCreditRepayment — advance credits are
// reconciled at filing, and repayment is uncapped at and above 400% FPL, so
// crossing claws back what has already been paid out rather than merely
// stopping future credit.
func TestACACliff_WarnsAboutAdvanceCreditRepayment(t *testing.T) {
	tc := acaCalculator(t)
	taking := &models.ACAConfig{
		HouseholdSize: 2, AnnualPremiumTaxCredit: models.FloatPtr(9600), AdvanceCreditsTaken: true,
	}
	claiming := &models.ACAConfig{
		HouseholdSize: 2, AnnualPremiumTaxCredit: models.FloatPtr(9600),
	}

	withAdvance := findThreshold(t, tc.ThresholdRegistry(acaOptions(taking, 2025)), "aca_premium_credit_cliff")
	withoutAdvance := findThreshold(t, tc.ThresholdRegistry(acaOptions(claiming, 2025)), "aca_premium_credit_cliff")

	if !strings.Contains(withAdvance.Note, "uncapped") {
		t.Errorf("advance credits must warn about uncapped repayment; got: %s", withAdvance.Note)
	}
	if strings.Contains(withoutAdvance.Note, "uncapped") {
		t.Error("a household claiming at filing has nothing to repay; the warning should not appear")
	}
}

// TestACACliff_ProximityUsesACAMAGI is the payoff: a household under the cliff
// on adjusted gross income is correctly reported as over it on ACA MAGI.
func TestACACliff_ProximityUsesACAMAGI(t *testing.T) {
	tc := acaCalculator(t)
	cfg := &models.ACAConfig{HouseholdSize: 2, AnnualPremiumTaxCredit: models.FloatPtr(9600)}
	registry := tc.ThresholdRegistry(acaOptions(cfg, 2025))

	in := ProjectedAnnualTaxInputs{OrdinaryIncome: 30000, SocialSecurityIncome: 60000}
	measures := tc.MeasureThresholdInputs(in, 0, 0)

	cliff := findThreshold(t, registry, "aca_premium_credit_cliff")
	if measures.MAGICurrentYear >= cliff.Amount {
		t.Fatalf("fixture must sit UNDER the cliff on ordinary MAGI (%.2f vs %.2f)",
			measures.MAGICurrentYear, cliff.Amount)
	}

	for _, p := range ThresholdProximities(registry, measures) {
		if p.Code != "aca_premium_credit_cliff" {
			continue
		}
		if !p.Crossed {
			t.Errorf("not marked crossed: ACA MAGI %.0f is past the %.0f cliff even though "+
				"ordinary MAGI %.0f is not", measures.ACAMAGI, p.Amount, measures.MAGICurrentYear)
		}
		if want := measures.ACAMAGI - p.Amount; math.Abs(p.Overage()-want) > 0.01 {
			t.Errorf("Overage() = %.2f, want %.2f", p.Overage(), want)
		}
		return
	}
	t.Fatal("ACA cliff missing from proximities")
}
