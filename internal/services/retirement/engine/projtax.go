package engine

import "math"

// ProjectionTaxAccumulator tracks year-to-date taxable income and taxes
// paid during a monthly projection. Tax law is annual; this struct lets
// the monthly loop estimate per-month tax with proper year-to-date
// awareness.
//
// Tax-treatment policy:
//   - Pension, Social Security, tax-deferred withdrawals (including
//     RMDs), non-qualified dividends, and Roth conversions are taxed as
//     ordinary income.
//   - Qualified dividends and realized long-term capital gains use the
//     simplified long-term capital-gains brackets in tax.go.
//   - Roth withdrawals are not taxed.
type ProjectionTaxAccumulator struct {
	OrdinaryIncomeYTD        float64
	SocialSecurityIncomeYTD  float64
	TaxableWithdrawalsYTD    float64
	QualifiedDividendsYTD    float64
	LongTermCapitalGainsYTD  float64
	ShortTermCapitalGainsYTD float64
	NonQualifiedDividendsYTD float64
	RothConversionsYTD       float64
	TaxesPaidYTD             float64
}

// ProjectedAnnualTaxInputs is the YTD-plus-current-month income picture
// extrapolated to a full-year projection.
type ProjectedAnnualTaxInputs struct {
	OrdinaryIncome        float64
	SocialSecurityIncome  float64
	TaxableWithdrawals    float64
	QualifiedDividends    float64
	LongTermCapitalGains  float64
	ShortTermCapitalGains float64
	NonQualifiedDividends float64
	RothConversions       float64
}

// ProjectedTaxSnapshot is the per-month tax + IRMAA picture produced by
// EstimateMonthlySnapshot.
type ProjectedTaxSnapshot struct {
	MonthlyTax                  float64
	MonthlyStateTax             float64 // State portion of MonthlyTax (federal = MonthlyTax - MonthlyStateTax)
	MonthlyIRMAA                float64
	AnnualMAGI                  float64
	AnnualTaxableSocialSecurity float64
	TaxableSocialSecurityPct    float64
	AnnualNIIT                  float64
	AnnualIRMAA                 float64

	// AnnualInputs is the annualized income composition this snapshot was
	// computed from. Carried so callers can re-evaluate the tax function at
	// a perturbed income (e.g. MarginalRateOnOrdinaryIncome) without having
	// to reconstruct the composition — it is a plain copy of a value the
	// snapshot already built, so carrying it costs nothing.
	AnnualInputs ProjectedAnnualTaxInputs
}

// AnnualizedInputs extrapolates YTD totals plus the current month to a
// full year by linear annualization. Roth conversions are deliberately
// not annualized — they're discrete events.
func (a ProjectionTaxAccumulator) AnnualizedInputs(monthInYear int, m MonthlyTaxInputs) ProjectedAnnualTaxInputs {
	monthsElapsed := float64(monthInYear + 1)
	if monthsElapsed <= 0 {
		monthsElapsed = 1
	}
	annualizationFactor := 12.0 / monthsElapsed

	return ProjectedAnnualTaxInputs{
		OrdinaryIncome:        (a.OrdinaryIncomeYTD + m.OrdinaryIncome) * annualizationFactor,
		SocialSecurityIncome:  (a.SocialSecurityIncomeYTD + m.SocialSecurityIncome) * annualizationFactor,
		TaxableWithdrawals:    (a.TaxableWithdrawalsYTD + m.TaxableWithdrawals) * annualizationFactor,
		QualifiedDividends:    (a.QualifiedDividendsYTD + m.QualifiedDividends) * annualizationFactor,
		LongTermCapitalGains:  (a.LongTermCapitalGainsYTD + m.LongTermCapitalGains) * annualizationFactor,
		ShortTermCapitalGains: (a.ShortTermCapitalGainsYTD + m.ShortTermCapitalGains) * annualizationFactor,
		NonQualifiedDividends: (a.NonQualifiedDividendsYTD + m.NonQualifiedDividends) * annualizationFactor,
		RothConversions:       a.RothConversionsYTD + m.RothConversions,
	}
}

// MonthlyTaxInputs bundles the arguments to EstimateMonthlySnapshot: the
// month's income components plus the IRMAA lookback context. Named fields
// replace what was a 14-argument positional list — the same treatment
// PortfolioMonthInput gave the cash-flow waterfall.
type MonthlyTaxInputs struct {
	Calculator    *TaxCalculator
	YearsFromBase int
	MonthInYear   int

	OrdinaryIncome        float64
	SocialSecurityIncome  float64
	TaxableWithdrawals    float64
	QualifiedDividends    float64
	LongTermCapitalGains  float64
	ShortTermCapitalGains float64
	NonQualifiedDividends float64
	RothConversions       float64

	CompletedMAGIHistory    []float64
	AssumedIRMALookbackMAGI *float64
	IRMAAEligibleAdults     int
	// IRMAAInflationFactor indexes the MAGI thresholds (CPI);
	// IRMAASurchargeInflationFactor indexes the surcharge dollars (Medicare
	// per-capita cost growth). They are separate series — see
	// CalculateMonthlyIRMAA (F-6). A zero surcharge factor is treated as 1,
	// so zero-value callers keep un-inflated surcharges.
	IRMAAInflationFactor          float64
	IRMAASurchargeInflationFactor float64
}

// EstimateMonthlySnapshot computes the per-month tax + IRMAA estimate
// for a given month, given YTD state and the month's income components.
// The remaining-months division ensures actual taxes paid converge to
// the annual liability over the year.
func (a ProjectionTaxAccumulator) EstimateMonthlySnapshot(in MonthlyTaxInputs) ProjectedTaxSnapshot {
	tc := in.Calculator
	yearsFromBase := in.YearsFromBase
	monthInYear := in.MonthInYear
	completedMAGIHistory := in.CompletedMAGIHistory
	assumedIRMALookbackMAGI := in.AssumedIRMALookbackMAGI
	irmaaEligibleAdults := in.IRMAAEligibleAdults
	irmaaInflationFactor := in.IRMAAInflationFactor
	irmaaSurchargeInflationFactor := in.IRMAASurchargeInflationFactor
	if tc == nil {
		return ProjectedTaxSnapshot{}
	}

	inputs := a.AnnualizedInputs(monthInYear, in)
	taxableSocialSecurity := taxableSocialSecurityFor(tc, inputs)
	estimatedOrdinaryIncome := ordinaryIncomeBeforeSocialSecurity(inputs) + taxableSocialSecurity

	taxBreakdown := tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
		OrdinaryIncome:        estimatedOrdinaryIncome,
		QualifiedDividends:    inputs.QualifiedDividends,
		LongTermCapitalGains:  inputs.LongTermCapitalGains,
		NonQualifiedDividends: inputs.NonQualifiedDividends,
		ShortTermCapitalGains: inputs.ShortTermCapitalGains,
	}, yearsFromBase)
	lookbackMAGI, hasIRMALookback := resolveIRMAALookbackMAGI(completedMAGIHistory, assumedIRMALookbackMAGI)

	annualIRMAA := 0.0
	if irmaaEligibleAdults > 0 && hasIRMALookback {
		annualIRMAA = tc.CalculateMonthlyIRMAA(lookbackMAGI, irmaaInflationFactor, irmaaSurchargeInflationFactor) * float64(irmaaEligibleAdults) * 12
	}
	remainingMonths := 12 - monthInYear
	if remainingMonths <= 0 {
		remainingMonths = 1
	}

	taxDue := (taxBreakdown.TotalTax - a.TaxesPaidYTD) / float64(remainingMonths)
	if taxDue < 0 {
		taxDue = 0
	}

	monthlyStateTax := 0.0
	if taxBreakdown.TotalTax > 0 {
		monthlyStateTax = taxDue * (taxBreakdown.StateTax / taxBreakdown.TotalTax)
	}

	taxableSocialSecurityPct := 0.0
	if inputs.SocialSecurityIncome > 0 {
		taxableSocialSecurityPct = taxableSocialSecurity / inputs.SocialSecurityIncome * 100
	}

	return ProjectedTaxSnapshot{
		MonthlyTax:                  taxDue,
		MonthlyStateTax:             monthlyStateTax,
		MonthlyIRMAA:                annualIRMAA / 12,
		AnnualMAGI:                  taxBreakdown.MAGI,
		AnnualTaxableSocialSecurity: taxableSocialSecurity,
		TaxableSocialSecurityPct:    taxableSocialSecurityPct,
		AnnualNIIT:                  taxBreakdown.NIIT,
		AnnualIRMAA:                 annualIRMAA,
		AnnualInputs:                inputs,
	}
}

// marginalRateProbe is the income perturbation used to measure a marginal
// rate numerically.
//
// One dollar, because the question is literally "what does the next dollar
// cost". A wider probe silently averages across any boundary it spans and
// reports a rate nobody actually faces: at $100, a single filer $50 below the
// 12%/22% edge was reported at 17%, being half a probe of each, when the next
// dollar genuinely costs 12%.
//
// A dollar is also numerically safe here. The difference between two annual
// tax figures a dollar apart is on the order of cents against totals in the
// thousands — six or more decimal orders above float64 epsilon — so rounding
// cannot dominate it. The residual blur is now one dollar wide, which is the
// natural quantum of the thing being measured.
const marginalRateProbe = 1.0

// AnnualIncomeTaxOn returns the total income tax (federal + state + NIIT)
// implied by a full-year income composition, recomputing the § 86 taxable
// portion of Social Security from that composition.
//
// IRMAA is deliberately excluded. It is a Medicare premium surcharge
// assessed on a two-year MAGI lookback, so it is not a cost of *this*
// year's marginal dollar — it lands on a different year's bill. Callers
// reasoning about IRMAA must treat it as its own discontinuity.
func (tc *TaxCalculator) AnnualIncomeTaxOn(in ProjectedAnnualTaxInputs, yearsFromBase int) float64 {
	if tc == nil {
		return 0
	}
	ordinaryIncome := ordinaryIncomeBeforeSocialSecurity(in) + taxableSocialSecurityFor(tc, in)
	return tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
		OrdinaryIncome:        ordinaryIncome,
		QualifiedDividends:    in.QualifiedDividends,
		LongTermCapitalGains:  in.LongTermCapitalGains,
		NonQualifiedDividends: in.NonQualifiedDividends,
		ShortTermCapitalGains: in.ShortTermCapitalGains,
	}, yearsFromBase).TotalTax
}

// ordinaryIncomeBeforeSocialSecurity is everything taxed at ordinary rates
// except the taxable portion of Social Security, which depends on this total
// and so must be computed from it.
//
// Short-term capital gain is deliberately NOT included here even though it is
// taxed at ordinary rates: CalculateTaxBreakdown adds it itself, and counting
// it in both places would tax it twice. It does belong in the § 86
// provisional-income base, which is why taxableSocialSecurityFor adds it
// separately.
func ordinaryIncomeBeforeSocialSecurity(in ProjectedAnnualTaxInputs) float64 {
	return in.OrdinaryIncome + in.TaxableWithdrawals + in.RothConversions
}

// taxableSocialSecurityFor computes the § 86 taxable portion of benefits for
// an annual income picture.
//
// Provisional income is AGI-before-benefits plus tax-exempt interest plus half
// of gross benefits, and short-term capital gain is part of AGI — so it drags
// benefits into taxation exactly as wages do. Omitting it here would understate
// taxable Social Security for anyone realising short-term gain inside the
// phase-in band. This is one of the six distinct income measures the design
// notes warn must each be derived on their own.
func taxableSocialSecurityFor(tc *TaxCalculator, in ProjectedAnnualTaxInputs) float64 {
	provisionalOther := ordinaryIncomeBeforeSocialSecurity(in) + in.ShortTermCapitalGains
	return tc.CalculateTaxableSocialSecurity(
		in.SocialSecurityIncome, provisionalOther, in.QualifiedDividends, in.LongTermCapitalGains)
}

// MarginalRateOnOrdinaryIncome returns the effective marginal tax rate, as a
// percentage, on the next dollar of ordinary income given a full-year income
// composition.
//
// This is a numeric derivative of the real tax function —
// (cost(income + delta) - cost(income)) / delta — not a bracket-table lookup.
// The distinction is not cosmetic. A household in the nominal 12% bracket
// with long-term gains straddling the 0%/15% boundary faces a real rate of
// 27% on ordinary income, because each dollar of ordinary income both costs
// 12 cents directly and pushes one dollar of gain out of the 0% band. The
// § 86 Social Security phase-in produces the same kind of amplification.
// Reading 12% off the bracket table and calling it the marginal rate
// understates the true cost by more than a factor of two.
//
// Use GetBracketRate when the question really is "which statutory bracket
// does this income fall in"; use this when the question is "what does the
// next dollar cost".
func (tc *TaxCalculator) MarginalRateOnOrdinaryIncome(in ProjectedAnnualTaxInputs, yearsFromBase int) float64 {
	return tc.marginalRateOn(in, yearsFromBase, func(p *ProjectedAnnualTaxInputs) {
		p.OrdinaryIncome += marginalRateProbe
	})
}

// MarginalRateOnLongTermGain returns the effective marginal tax rate, as a
// percentage, on the next dollar of realized LONG-TERM capital gain.
//
// This is the number behind "how much gain can I realize this year", and it is
// not the capital-gains bracket. Three things move it that a bracket table
// cannot see:
//
//   - The 0% bracket runs out. While headroom remains the rate really is 0%;
//     one dollar past the ceiling it steps to 15%, and the step is invisible
//     until you differentiate.
//   - Social Security. Realized gain is part of the § 86 provisional-income
//     base, so inside the phase-in band each dollar of "0%" gain drags up to
//     $0.85 of benefits into ordinary tax. Gain in the 0% bracket is routinely
//     not free, and a headroom figure that ignores this overstates what can be
//     realized cheaply.
//   - NIIT. Above the threshold, gain carries the 3.8% surtax on top of
//     whatever bracket applies.
//
// Short-term gain is ordinary income, so MarginalRateOnOrdinaryIncome already
// answers the question for it; there is no separate short-term variant.
func (tc *TaxCalculator) MarginalRateOnLongTermGain(in ProjectedAnnualTaxInputs, yearsFromBase int) float64 {
	return tc.marginalRateOn(in, yearsFromBase, func(p *ProjectedAnnualTaxInputs) {
		p.LongTermCapitalGains += marginalRateProbe
	})
}

// marginalRateOn differentiates the tax function numerically with respect to
// whichever income component bump adds to. Shared so the ordinary-income and
// long-term-gain rates cannot drift apart in method.
func (tc *TaxCalculator) marginalRateOn(in ProjectedAnnualTaxInputs, yearsFromBase int, bump func(*ProjectedAnnualTaxInputs)) float64 {
	if tc == nil {
		return 0
	}
	base := tc.AnnualIncomeTaxOn(in, yearsFromBase)

	probed := in
	bump(&probed)
	rate := (tc.AnnualIncomeTaxOn(probed, yearsFromBase) - base) / marginalRateProbe * 100

	// Total tax is non-decreasing in income everywhere this engine models
	// (there are no subsidy cliffs in the income-tax path), so a negative
	// result can only be float64 noise near a boundary.
	if rate < 0 {
		return 0
	}
	return rate
}

// resolveIRMAALookbackMAGI picks the MAGI from two years prior (the
// IRMAA two-year lookback rule). If we have at least two completed
// years of history, use the older one; otherwise fall back to the
// caller's assumed MAGI.
func resolveIRMAALookbackMAGI(completedMAGIHistory []float64, assumedIRMALookbackMAGI *float64) (float64, bool) {
	if len(completedMAGIHistory) >= 2 {
		return completedMAGIHistory[len(completedMAGIHistory)-2], true
	}
	if assumedIRMALookbackMAGI != nil {
		return math.Max(0, *assumedIRMALookbackMAGI), true
	}
	return 0, false
}

// EstimateMonthlyTaxes is a thin wrapper around EstimateMonthlySnapshot
// for callers that only need the monthly tax figure (no IRMAA, no MAGI
// breakdown).
func (a ProjectionTaxAccumulator) EstimateMonthlyTaxes(tc *TaxCalculator, yearsFromBase, monthInYear int, ordinaryIncome, socialSecurityIncome, taxableWithdrawals, qualifiedDividends, longTermCapitalGains, nonQualifiedDividends, rothConversions float64) float64 {
	return a.EstimateMonthlySnapshot(MonthlyTaxInputs{
		Calculator:            tc,
		YearsFromBase:         yearsFromBase,
		MonthInYear:           monthInYear,
		OrdinaryIncome:        ordinaryIncome,
		SocialSecurityIncome:  socialSecurityIncome,
		TaxableWithdrawals:    taxableWithdrawals,
		QualifiedDividends:    qualifiedDividends,
		LongTermCapitalGains:  longTermCapitalGains,
		NonQualifiedDividends: nonQualifiedDividends,
		RothConversions:       rothConversions,
		IRMAAInflationFactor:  1,
	}).MonthlyTax
}

// ApplyMonth folds a month's realised income and tax payments into the
// accumulator. Mutating receiver — caller resets at year boundary.
func (a *ProjectionTaxAccumulator) ApplyMonth(m RealizedMonthIncome) {
	a.OrdinaryIncomeYTD += m.OrdinaryIncome
	a.SocialSecurityIncomeYTD += m.SocialSecurityIncome
	a.TaxableWithdrawalsYTD += m.TaxableWithdrawals
	a.QualifiedDividendsYTD += m.QualifiedDividends
	a.LongTermCapitalGainsYTD += m.LongTermCapitalGains
	a.ShortTermCapitalGainsYTD += m.ShortTermCapitalGains
	a.NonQualifiedDividendsYTD += m.NonQualifiedDividends
	a.RothConversionsYTD += m.RothConversions
	a.TaxesPaidYTD += m.TaxesPaid
}

// RealizedMonthIncome is one month's realised income split by tax treatment,
// plus the tax actually paid. Named fields for the same reason
// InvestmentIncomeTaxInputs has them: nine float64 dollars in a row is a
// transposition waiting to happen.
type RealizedMonthIncome struct {
	OrdinaryIncome        float64
	SocialSecurityIncome  float64
	TaxableWithdrawals    float64
	QualifiedDividends    float64
	LongTermCapitalGains  float64
	ShortTermCapitalGains float64
	NonQualifiedDividends float64
	RothConversions       float64
	TaxesPaid             float64
}
