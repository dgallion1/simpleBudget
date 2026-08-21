package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func registryCalculator(t *testing.T) *TaxCalculator {
	t.Helper()
	zero := 0.0
	return NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingMarriedJoint,
		StateIncomeTaxRate: &zero,
		Age65Count:         2,
	}, 0)
}

func defaultRegistryOptions() ThresholdRegistryOptions {
	return ThresholdRegistryOptions{
		IRMAAEligibleAdults:  2,
		IRMAAThresholdFactor: 1,
		IRMAASurchargeFactor: 1,
	}
}

func findThreshold(t *testing.T, reg []Threshold, code string) Threshold {
	t.Helper()
	for _, th := range reg {
		if th.Code == code {
			return th
		}
	}
	t.Fatalf("threshold %q not in registry", code)
	return Threshold{}
}

// TestThresholdRegistry_CliffCostMatchesTheTaxFunction is the test that makes
// the registry trustworthy: every cliff's advertised cost is cross-checked
// against what CalculateMonthlyIRMAA actually charges either side of it. A
// registry that misreports the cost of crossing is worse than no registry,
// because a plan would route around the wrong number.
func TestThresholdRegistry_CliffCostMatchesTheTaxFunction(t *testing.T) {
	tc := registryCalculator(t)
	opts := defaultRegistryOptions()
	reg := tc.ThresholdRegistry(opts)

	cliffs := 0
	for _, th := range reg {
		if th.Kind != ThresholdCliff {
			continue
		}
		cliffs++

		below := CalculateMonthlyIRMAA(th.Amount, tc.FilingStatus,
			opts.IRMAAThresholdFactor, opts.IRMAASurchargeFactor)
		above := CalculateMonthlyIRMAA(th.Amount+1, tc.FilingStatus,
			opts.IRMAAThresholdFactor, opts.IRMAASurchargeFactor)

		wantAnnual := (above - below) * 12 * float64(opts.IRMAAEligibleAdults)
		if math.Abs(th.AnnualCostOfCrossing-wantAnnual) > 0.01 {
			t.Errorf("%s: registry says crossing costs %.2f/yr, but the surcharge "+
				"actually steps %.2f -> %.2f per month per person (%.2f/yr for %d adults)",
				th.Code, th.AnnualCostOfCrossing, below, above, wantAnnual,
				opts.IRMAAEligibleAdults)
		}
		if th.AnnualCostOfCrossing <= 0 {
			t.Errorf("%s: a cliff must cost something to cross", th.Code)
		}
	}

	if cliffs == 0 {
		t.Fatal("no cliffs registered for a Medicare-age MFJ household")
	}
}

// TestThresholdRegistry_FirstIRMAATier pins the concrete numbers so a table
// edit cannot silently move them.
func TestThresholdRegistry_FirstIRMAATier(t *testing.T) {
	tc := registryCalculator(t)
	th := findThreshold(t, tc.ThresholdRegistry(defaultRegistryOptions()), "irmaa_tier_1")

	if th.Amount != 218000 {
		t.Errorf("first MFJ IRMAA tier at %.0f, want 218000", th.Amount)
	}
	if th.Kind != ThresholdCliff {
		t.Errorf("Kind = %q, want cliff", th.Kind)
	}
	if th.Measure != MeasureMAGITwoYearsPrior {
		t.Errorf("Measure = %q; IRMAA bills against MAGI from two years prior, "+
			"not this year's", th.Measure)
	}
	// (81.20 + 14.50) * 12 months * 2 adults
	if want := (81.20 + 14.50) * 12 * 2; math.Abs(th.AnnualCostOfCrossing-want) > 0.01 {
		t.Errorf("AnnualCostOfCrossing = %.2f, want %.2f", th.AnnualCostOfCrossing, want)
	}
}

// TestThresholdRegistry_KinksAreContinuous validates the cliff/kink split.
// Crossing a kink must cost nothing by itself — only the next dollar changes
// price. If a kink turns out to have a step, it is a cliff and is misfiled.
func TestThresholdRegistry_KinksAreContinuous(t *testing.T) {
	tc := registryCalculator(t)

	for _, th := range tc.ThresholdRegistry(defaultRegistryOptions()) {
		if th.Kind != ThresholdKink {
			continue
		}
		if th.AnnualCostOfCrossing != 0 {
			t.Errorf("%s: kinks cost nothing to cross, but AnnualCostOfCrossing = %.2f",
				th.Code, th.AnnualCostOfCrossing)
		}
	}

	// Demonstrated concretely at the 0% capital-gains ceiling: with no ordinary
	// income, the standard deduction shelters the first slice of gain, so
	// taxable income lands exactly on the ceiling.
	ceiling := findThreshold(t, tc.ThresholdRegistry(defaultRegistryOptions()), "ltcg_zero_ceiling")
	deduction := tc.GetAdjustedStandardDeduction(0)

	at := tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
		LongTermCapitalGains: ceiling.Amount + deduction,
	}, 0)
	justOver := tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
		LongTermCapitalGains: ceiling.Amount + deduction + 1,
	}, 0)

	if math.Abs(at.TaxableIncome-ceiling.Amount) > 0.01 {
		t.Fatalf("fixture missed the ceiling: taxable income %.2f, ceiling %.2f",
			at.TaxableIncome, ceiling.Amount)
	}
	if step := justOver.TotalTax - at.TotalTax; step > 1 {
		t.Errorf("crossing the 0%% capital-gains ceiling cost %.2f for one dollar of "+
			"income; that is a step, so it is a cliff and misfiled as a kink", step)
	}
}

// TestThresholdRegistry_CeilingIsWhereTheRateActuallyChanges ties the
// registered amount to the numeric derivative: at the ceiling the next dollar
// of gain is free, one dollar past it costs 15%.
func TestThresholdRegistry_CeilingIsWhereTheRateActuallyChanges(t *testing.T) {
	tc := registryCalculator(t)
	ceiling := findThreshold(t, tc.ThresholdRegistry(defaultRegistryOptions()), "ltcg_zero_ceiling")
	deduction := tc.GetAdjustedStandardDeduction(0)

	// Leave a probe's worth of headroom so the measurement stays inside the band.
	below := ProjectedAnnualTaxInputs{
		LongTermCapitalGains: ceiling.Amount + deduction - marginalRateProbe - 1,
	}
	above := ProjectedAnnualTaxInputs{
		LongTermCapitalGains: ceiling.Amount + deduction + 1,
	}

	if got := tc.MarginalRateOnLongTermGain(below, 0); got > 0.01 {
		t.Errorf("just below the registered ceiling the marginal rate on gain is %.2f%%, want 0%%", got)
	}
	if got := tc.MarginalRateOnLongTermGain(above, 0); math.Abs(got-15) > 0.05 {
		t.Errorf("just above the registered ceiling the marginal rate on gain is %.2f%%, want 15%%", got)
	}
}

// TestThresholdRegistry_NoIRMAABeforeMedicare — a household with nobody on
// Medicare has no IRMAA cliffs to route around.
func TestThresholdRegistry_NoIRMAABeforeMedicare(t *testing.T) {
	tc := registryCalculator(t)
	opts := defaultRegistryOptions()
	opts.IRMAAEligibleAdults = 0

	for _, th := range tc.ThresholdRegistry(opts) {
		if th.Kind == ThresholdCliff {
			t.Errorf("registered cliff %q for a household with no Medicare enrollees", th.Code)
		}
	}
}

// TestThresholdRegistry_IndexingIsPerThreshold — some of these move with
// inflation and some are frozen in statute. Conflating them would silently
// misplace half the registry in later projection years.
func TestThresholdRegistry_IndexingIsPerThreshold(t *testing.T) {
	zero := 0.0
	inflating := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingMarriedJoint,
		StateIncomeTaxRate: &zero,
		Age65Count:         2,
	}, 3.0)

	opts := defaultRegistryOptions()
	opts.YearsFromBase = 10
	opts.IRMAAThresholdFactor = 1.34 // ~10 years of CPI

	base := inflating.ThresholdRegistry(defaultRegistryOptions())
	later := inflating.ThresholdRegistry(opts)

	mustMove := []string{"irmaa_tier_1", "ltcg_zero_ceiling"}
	mustNotMove := []string{"niit_threshold", "ss_phase_in_start", "ss_phase_in_85pct"}

	for _, code := range mustMove {
		if a, b := findThreshold(t, base, code), findThreshold(t, later, code); b.Amount <= a.Amount {
			t.Errorf("%s: %.2f -> %.2f; this threshold is indexed and must rise",
				code, a.Amount, b.Amount)
		}
	}
	for _, code := range mustNotMove {
		a, b := findThreshold(t, base, code), findThreshold(t, later, code)
		if a.Amount != b.Amount {
			t.Errorf("%s: %.2f -> %.2f; this threshold is fixed in statute and must not move",
				code, a.Amount, b.Amount)
		}
		if b.Inflated {
			t.Errorf("%s: marked Inflated, but it is not indexed", code)
		}
	}
}

// TestThresholdRegistry_SortedByAmount keeps the registry presentable and
// makes "the next one up" a scan rather than a search.
func TestThresholdRegistry_SortedByAmount(t *testing.T) {
	tc := registryCalculator(t)
	reg := tc.ThresholdRegistry(defaultRegistryOptions())
	if len(reg) < 5 {
		t.Fatalf("expected a populated registry, got %d entries", len(reg))
	}
	for i := 1; i < len(reg); i++ {
		if reg[i].Amount < reg[i-1].Amount {
			t.Errorf("registry out of order at %d: %s (%.0f) after %s (%.0f)",
				i, reg[i].Code, reg[i].Amount, reg[i-1].Code, reg[i-1].Amount)
		}
	}
}

// TestThresholdProximities_UsesTheRightMeasure is the § 3 guard: IRMAA is
// billed on a different YEAR's income, so testing it against this year's MAGI
// would answer the wrong question entirely.
func TestThresholdProximities_UsesTheRightMeasure(t *testing.T) {
	tc := registryCalculator(t)
	reg := tc.ThresholdRegistry(defaultRegistryOptions())

	in := ProjectedAnnualTaxInputs{
		OrdinaryIncome:       80000,
		SocialSecurityIncome: 40000,
		LongTermCapitalGains: 20000,
	}
	const lookbackMAGI float64 = 260000 // well past the first MFJ tier

	measures := tc.MeasureThresholdInputs(in, 0, lookbackMAGI)
	if measures.MAGICurrentYear >= 218000 {
		t.Fatalf("fixture is meant to sit under the first tier on current-year MAGI, got %.2f",
			measures.MAGICurrentYear)
	}
	if measures.MAGITwoYearsPrior != lookbackMAGI {
		t.Errorf("MAGITwoYearsPrior = %.2f, want the supplied %.2f",
			measures.MAGITwoYearsPrior, lookbackMAGI)
	}

	proximities := ThresholdProximities(reg, measures)
	for _, p := range proximities {
		if p.Code != "irmaa_tier_1" {
			continue
		}
		if !p.Crossed {
			t.Errorf("irmaa_tier_1 not marked crossed: the household's two-years-prior "+
				"MAGI of %.0f is past the %.0f tier, even though this year's MAGI (%.0f) is not",
				lookbackMAGI, p.Amount, measures.MAGICurrentYear)
		}
		if want := lookbackMAGI - p.Amount; math.Abs(p.Overage()-want) > 0.01 {
			t.Errorf("Overage() = %.2f, want %.2f", p.Overage(), want)
		}
		if p.Headroom() != 0 {
			t.Errorf("Headroom() = %.2f on a crossed threshold, want 0", p.Headroom())
		}
		return
	}
	t.Fatal("irmaa_tier_1 missing from proximities")
}

func TestThresholdProximities_HeadroomAndNextCliff(t *testing.T) {
	tc := registryCalculator(t)
	reg := tc.ThresholdRegistry(defaultRegistryOptions())

	in := ProjectedAnnualTaxInputs{OrdinaryIncome: 100000}
	const lookbackMAGI float64 = 200000 // under the 218,000 first tier

	measures := tc.MeasureThresholdInputs(in, 0, lookbackMAGI)
	proximities := ThresholdProximities(reg, measures)

	next, ok := NextCliff(proximities)
	if !ok {
		t.Fatal("expected an uncrossed cliff ahead")
	}
	if next.Code != "irmaa_tier_1" {
		t.Errorf("next cliff = %s, want irmaa_tier_1 (the nearest uncrossed one)", next.Code)
	}
	if want := 218000 - lookbackMAGI; math.Abs(next.Headroom()-want) > 0.01 {
		t.Errorf("Headroom() = %.2f, want %.2f — the \"you are $N under the cliff\" figure",
			next.Headroom(), want)
	}
	if next.Overage() != 0 {
		t.Errorf("Overage() = %.2f on an uncrossed threshold, want 0", next.Overage())
	}
}

// TestNextCliff_SkipsKinksAndCrossings — only an uncrossed true discontinuity
// qualifies, because only there does one dollar have a step cost.
func TestNextCliff_SkipsKinksAndCrossings(t *testing.T) {
	proximities := []ThresholdProximity{
		{Threshold: Threshold{Code: "kink_near", Kind: ThresholdKink, Amount: 10}, Distance: 5},
		{Threshold: Threshold{Code: "cliff_crossed", Kind: ThresholdCliff, Amount: 20}, Distance: -3, Crossed: true},
		{Threshold: Threshold{Code: "cliff_far", Kind: ThresholdCliff, Amount: 90}, Distance: 80},
		{Threshold: Threshold{Code: "cliff_near", Kind: ThresholdCliff, Amount: 40}, Distance: 30},
	}

	next, ok := NextCliff(proximities)
	if !ok {
		t.Fatal("expected a cliff")
	}
	if next.Code != "cliff_near" {
		t.Errorf("NextCliff = %s, want cliff_near", next.Code)
	}

	if _, ok := NextCliff(proximities[:2]); ok {
		t.Error("expected no cliff when the only ones present are kinks or already crossed")
	}
}
