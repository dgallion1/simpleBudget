package engine

import (
	"fmt"
	"math"

	"budget2/internal/models"
)

// Threshold registry: the income points where this plan's tax behaviour
// changes discontinuously or abruptly.
//
// Optimizers that assume a smooth objective walk off cliffs or miss the
// optimum sitting one dollar below one, so the locations have to be
// enumerated rather than searched for. Users need them for a different
// reason: "you are $10,319 over the cliff" is a more actionable sentence
// than any rate.

// ThresholdKind separates the two things commonly lumped together as
// "cliffs". The difference is worth keeping: crossing a cliff costs real
// money immediately, while crossing a kink only changes what the NEXT dollar
// costs.
type ThresholdKind string

const (
	// ThresholdCliff is a true discontinuity: total annual cost steps up the
	// moment the threshold is crossed. One dollar of extra income can cost
	// thousands.
	ThresholdCliff ThresholdKind = "cliff"
	// ThresholdKink is continuous in total cost but changes the marginal
	// rate. Crossing it costs nothing by itself.
	ThresholdKink ThresholdKind = "kink"
)

// ThresholdMeasure names which definition of income a threshold is tested
// against. Getting this wrong is the classic failure: an `income` scalar
// compared against every threshold is wrong for most of them, because these
// measures include different things and, for IRMAA, a different YEAR.
type ThresholdMeasure string

const (
	// MeasureMAGICurrentYear is this year's modified AGI.
	MeasureMAGICurrentYear ThresholdMeasure = "magi_current_year"
	// MeasureMAGITwoYearsPrior is the MAGI Medicare actually bills against:
	// IRMAA uses a two-year lookback, so this year's income sets a surcharge
	// two years out, and today's surcharge was set by income already earned.
	MeasureMAGITwoYearsPrior ThresholdMeasure = "magi_two_years_prior"
	// MeasureProvisionalIncome is the § 86 base for taxing Social Security:
	// income excluding benefits, plus half of gross benefits.
	MeasureProvisionalIncome ThresholdMeasure = "provisional_income"
	// MeasureTaxableIncome is income after the standard deduction — what the
	// bracket tables are indexed by.
	MeasureTaxableIncome ThresholdMeasure = "taxable_income"
	// MeasureACAMAGI is the Affordable Care Act's own modified AGI: it counts
	// ALL Social Security benefits, not the taxable half, plus tax-exempt
	// interest. A household under every other threshold can be over this one.
	MeasureACAMAGI ThresholdMeasure = "aca_magi"
)

// Threshold is one registered point.
type Threshold struct {
	Code    string
	Label   string
	Kind    ThresholdKind
	Measure ThresholdMeasure
	// Amount is the value of Measure at which the threshold bites, already
	// inflated to the projection year where the underlying figure is indexed.
	Amount float64
	// AnnualCostOfCrossing is the immediate jump in annual cost for a cliff,
	// in dollars. Zero for a kink, which by definition costs nothing to cross.
	AnnualCostOfCrossing float64
	// Inflated records whether Amount is indexed. Several of these are fixed
	// in statute and are NOT indexed — the § 86 thresholds have been $32,000
	// and $44,000 since 1993, and the NIIT thresholds have never moved — so
	// inflation pushes households across them over time with no change in law.
	Inflated bool
	Note     string
}

// ThresholdRegistryOptions describes the household well enough to place the
// thresholds that actually apply to it.
type ThresholdRegistryOptions struct {
	YearsFromBase int
	// IRMAAEligibleAdults scales the surcharge to a household total. Zero
	// means nobody is paying Medicare premiums yet, so IRMAA cliffs are not
	// registered at all.
	IRMAAEligibleAdults int
	// IRMAAThresholdFactor indexes the MAGI cutoffs (CPI);
	// IRMAASurchargeFactor indexes the dollars charged (Medicare per-capita
	// cost growth). They are different series — see CalculateMonthlyIRMAA.
	IRMAAThresholdFactor float64
	IRMAASurchargeFactor float64

	// CoverageYear is the calendar year the household is being covered in.
	// Needed to place the ACA cliff, which is measured against the poverty
	// guidelines published before that year's open enrolment.
	CoverageYear int
	// ACA carries household size, the credit at stake and the advance-credit
	// flag. nil, or a household with nobody on a marketplace plan, means no
	// ACA cliff is registered.
	ACA *models.ACAConfig
	// MarketplaceEnrolled reports whether anyone in the household is actually
	// on a marketplace plan this year, and therefore has a credit to lose.
	MarketplaceEnrolled bool
	// DisqualifiedFromPremiumCredit reports whether the household is barred
	// from the credit regardless of income — COBRA or other employer coverage.
	// No cliff is registered when it is, because there is no credit to fall
	// off.
	DisqualifiedFromPremiumCredit bool
}

// ThresholdRegistry enumerates the income points where this household's tax
// behaviour changes abruptly, ordered by amount.
//
// Known omission: the ACA premium tax credit cliff at 400% of the federal
// poverty level is NOT here, because this engine does not model premium
// credits at all — marketplace coverage is a flat monthly cost independent of
// income. For a household retiring before 65 that cliff can dominate every
// entry below. Registering it requires household size, an FPL table, and
// income-dependent premium modelling.
func (tc *TaxCalculator) ThresholdRegistry(opts ThresholdRegistryOptions) []Threshold {
	if tc == nil {
		return nil
	}
	status := NormalizeFilingStatus(tc.FilingStatus)
	out := make([]Threshold, 0, 12)

	out = append(out, tc.irmaaThresholds(status, opts)...)
	if cliff, ok := tc.acaSubsidyCliff(opts); ok {
		out = append(out, cliff)
	}

	// The 0% long-term capital-gains ceiling. A kink, not a cliff: gain below
	// it is untaxed and gain above it is taxed at 15%, with no step in total
	// cost. It earns its place because it is the boundary a "how much can I
	// realize this year" decision is built around.
	if ltcg := tc.GetAdjustedLongTermCapitalGainsBrackets(opts.YearsFromBase); len(ltcg) > 0 {
		out = append(out, Threshold{
			Code:     "ltcg_zero_ceiling",
			Label:    "Top of the 0% long-term capital-gains bracket",
			Kind:     ThresholdKink,
			Measure:  MeasureTaxableIncome,
			Amount:   ltcg[0].MaxIncome,
			Inflated: true,
			Note: "Long-term gain stacks on top of ordinary income, so ordinary income " +
				"consumes this headroom first. Gain below the ceiling can still cost " +
				"money by dragging Social Security into taxation — see MarginalRateOnLongTermGain.",
		})
	}

	// NIIT. A kink: the 3.8% surtax applies to the lesser of net investment
	// income and the excess over the threshold, so it phases in continuously.
	if niit, ok := niitThresholds[status]; ok {
		out = append(out, Threshold{
			Code:     "niit_threshold",
			Label:    "Net investment income tax (3.8%) begins",
			Kind:     ThresholdKink,
			Measure:  MeasureMAGICurrentYear,
			Amount:   niit,
			Inflated: false,
			Note:     "Fixed in statute and never indexed, so inflation walks households into it.",
		})
	}

	// § 86 Social Security phase-in boundaries.
	if ss, ok := socialSecurityTaxThresholds[status]; ok {
		out = append(out,
			Threshold{
				Code:     "ss_phase_in_start",
				Label:    "Social Security benefits begin to be taxed",
				Kind:     ThresholdKink,
				Measure:  MeasureProvisionalIncome,
				Amount:   ss.BaseThreshold,
				Inflated: false,
				Note:     "Above this, each dollar drags $0.50 of benefits into taxable income.",
			},
			Threshold{
				Code:     "ss_phase_in_85pct",
				Label:    "Social Security 85% phase-in begins",
				Kind:     ThresholdKink,
				Measure:  MeasureProvisionalIncome,
				Amount:   ss.UpperThreshold,
				Inflated: false,
				Note: "Above this, each dollar drags $0.85 of benefits in, until 85% of " +
					"benefits are taxed. Unindexed since 1993.",
			},
		)
	}

	sortThresholdsByAmount(out)
	return out
}

// irmaaThresholds registers one cliff per IRMAA tier boundary. These are the
// only true discontinuities this engine models.
func (tc *TaxCalculator) irmaaThresholds(status models.FilingStatus, opts ThresholdRegistryOptions) []Threshold {
	if opts.IRMAAEligibleAdults <= 0 {
		return nil
	}
	tiers := monthlyIRMAASurcharge2026[status]
	if len(tiers) == 0 {
		return nil
	}

	thresholdFactor := opts.IRMAAThresholdFactor
	if thresholdFactor <= 0 {
		thresholdFactor = 1
	}
	surchargeFactor := opts.IRMAASurchargeFactor
	if surchargeFactor <= 0 {
		surchargeFactor = 1
	}

	out := make([]Threshold, 0, len(tiers))
	for i, tier := range tiers {
		// The final tier is unbounded above; there is no boundary to cross.
		if tier.UpperMAGI >= math.MaxFloat64 || i+1 >= len(tiers) {
			continue
		}
		step := (tiers[i+1].Surcharge - tier.Surcharge) * surchargeFactor
		if step <= 0 {
			continue
		}
		out = append(out, Threshold{
			Code:                 fmt.Sprintf("irmaa_tier_%d", i+1),
			Label:                fmt.Sprintf("IRMAA tier %d", i+1),
			Kind:                 ThresholdCliff,
			Measure:              MeasureMAGITwoYearsPrior,
			Amount:               tier.UpperMAGI * thresholdFactor,
			AnnualCostOfCrossing: step * 12 * float64(opts.IRMAAEligibleAdults),
			Inflated:             thresholdFactor != 1,
			Note: "A step, not a ramp: one dollar over costs the whole tier. Assessed on " +
				"MAGI from two years prior, so this year's income sets a surcharge two years out.",
		})
	}
	return out
}

// acaSubsidyCliff registers the 400%-of-poverty-level premium tax credit
// cliff, when one applies to this household.
//
// It is a cliff in the strictest sense: at 400% FPL the credit is the
// difference between the benchmark premium and the household's expected
// contribution; one dollar past it the credit is zero. Nothing tapers.
//
// No cliff is registered when the household cannot lose a credit it does not
// have — nobody on a marketplace plan, or everyone disqualified by employer
// or COBRA coverage. The disqualification is itself worth surfacing, but as a
// finding rather than as a threshold.
func (tc *TaxCalculator) acaSubsidyCliff(opts ThresholdRegistryOptions) (Threshold, bool) {
	if !opts.MarketplaceEnrolled || opts.DisqualifiedFromPremiumCredit {
		return Threshold{}, false
	}
	if !opts.ACA.ACAConfigured() || opts.CoverageYear <= 0 {
		return Threshold{}, false
	}

	guideline, err := tc.PovertyGuidelineFor(opts.CoverageYear)
	if err != nil {
		return Threshold{}, false
	}
	povertyLevel := guideline.FederalPovertyLevel(opts.ACA.HouseholdSize)
	if povertyLevel <= 0 {
		return Threshold{}, false
	}

	note := "A step, not a ramp: one dollar over forfeits the whole year's credit. " +
		"Measured against ACA MAGI, which counts all Social Security benefits, not just the taxable part."
	if opts.ACA.AdvanceCreditsTaken {
		note += " Advance credits are being taken, so crossing also claws back what has " +
			"already been paid to the insurer this year — repayment is uncapped at and above 400%."
	}
	if !opts.ACA.CreditKnown() {
		note += " The cost shown is zero only because the credit amount has not been supplied; " +
			"it is unknown, not nil."
	}

	return Threshold{
		Code:                 "aca_premium_credit_cliff",
		Label:                "ACA premium tax credit cliff (400% of the federal poverty level)",
		Kind:                 ThresholdCliff,
		Measure:              MeasureACAMAGI,
		Amount:               povertyLevel * 4,
		AnnualCostOfCrossing: opts.ACA.AnnualCreditOrZero(),
		Inflated:             guideline.Projected(),
		Note:                 note,
	}, true
}

// sortThresholdsByAmount orders ascending by amount; insertion sort because
// the registry is a dozen entries at most.
func sortThresholdsByAmount(ts []Threshold) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].Amount < ts[j-1].Amount; j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}

// ThresholdProximity places a household against one registered threshold.
type ThresholdProximity struct {
	Threshold
	// Value is the household's current value of Threshold.Measure.
	Value float64
	// Distance is Amount - Value: positive means headroom remaining, negative
	// means the threshold has been crossed by that much.
	Distance float64
	Crossed  bool
}

// Headroom returns the remaining distance below the threshold, or 0 once it
// has been crossed.
func (p ThresholdProximity) Headroom() float64 {
	if p.Crossed {
		return 0
	}
	return p.Distance
}

// Overage returns how far past the threshold the household is, or 0 if it has
// not been crossed. This is the "$10,319 over" figure.
func (p ThresholdProximity) Overage() float64 {
	if !p.Crossed {
		return 0
	}
	return -p.Distance
}

// ThresholdMeasures is a household's value for each registered income
// measure. Computed once and reused so every threshold is tested against the
// right definition.
type ThresholdMeasures struct {
	MAGICurrentYear   float64
	MAGITwoYearsPrior float64
	ProvisionalIncome float64
	TaxableIncome     float64
	ACAMAGI           float64
}

// MeasureThresholdInputs computes every income measure the registry tests
// against, from one annual income picture.
//
// lookbackMAGI is the MAGI from two years prior, which IRMAA bills against.
// It is a genuinely different year's number and cannot be derived from `in`;
// callers hold it (see resolveIRMAALookbackMAGI). Passing this year's MAGI
// would silently answer the wrong question.
func (tc *TaxCalculator) MeasureThresholdInputs(in ProjectedAnnualTaxInputs, yearsFromBase int, lookbackMAGI float64) ThresholdMeasures {
	if tc == nil {
		return ThresholdMeasures{}
	}
	ordinaryIncome := ordinaryIncomeBeforeSocialSecurity(in) + taxableSocialSecurityFor(tc, in)
	breakdown := tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
		OrdinaryIncome:        ordinaryIncome,
		QualifiedDividends:    in.QualifiedDividends,
		LongTermCapitalGains:  in.LongTermCapitalGains,
		NonQualifiedDividends: in.NonQualifiedDividends,
		ShortTermCapitalGains: in.ShortTermCapitalGains,
	}, yearsFromBase)

	return ThresholdMeasures{
		MAGICurrentYear:   breakdown.MAGI,
		MAGITwoYearsPrior: lookbackMAGI,
		ProvisionalIncome: ProvisionalIncome(in),
		TaxableIncome:     breakdown.TaxableIncome,
		ACAMAGI:           ACAModifiedAGI(tc, in),
	}
}

// valueFor returns the household's value for a given measure.
func (m ThresholdMeasures) valueFor(measure ThresholdMeasure) float64 {
	switch measure {
	case MeasureMAGICurrentYear:
		return m.MAGICurrentYear
	case MeasureMAGITwoYearsPrior:
		return m.MAGITwoYearsPrior
	case MeasureProvisionalIncome:
		return m.ProvisionalIncome
	case MeasureTaxableIncome:
		return m.TaxableIncome
	case MeasureACAMAGI:
		return m.ACAMAGI
	default:
		return 0
	}
}

// ThresholdProximities places a household against every registered threshold,
// ordered by amount. Each threshold is compared against its own income
// measure, never against a single "income" number.
func ThresholdProximities(registry []Threshold, measures ThresholdMeasures) []ThresholdProximity {
	out := make([]ThresholdProximity, 0, len(registry))
	for _, t := range registry {
		value := measures.valueFor(t.Measure)
		distance := t.Amount - value
		out = append(out, ThresholdProximity{
			Threshold: t,
			Value:     value,
			Distance:  distance,
			// A tier holds up to and including its amount (CalculateMonthlyIRMAA
			// breaks on magi <= upper), so equality is not yet a crossing.
			Crossed: value > t.Amount,
		})
	}
	return out
}

// NextCliff returns the nearest not-yet-crossed true discontinuity, and
// whether one exists. This is the "you are $N under the next cliff" figure —
// the single most useful sentence a plan can show, because it is the only one
// where a dollar of extra income has a step cost.
func NextCliff(proximities []ThresholdProximity) (ThresholdProximity, bool) {
	var best ThresholdProximity
	found := false
	for _, p := range proximities {
		if p.Kind != ThresholdCliff || p.Crossed {
			continue
		}
		if !found || p.Distance < best.Distance {
			best, found = p, true
		}
	}
	return best, found
}
