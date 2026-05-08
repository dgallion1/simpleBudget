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
	NonQualifiedDividends float64
	RothConversions       float64
}

// ProjectedTaxSnapshot is the per-month tax + IRMAA picture produced by
// EstimateMonthlySnapshot.
type ProjectedTaxSnapshot struct {
	MonthlyTax                  float64
	MonthlyIRMAA                float64
	AnnualMAGI                  float64
	AnnualTaxableSocialSecurity float64
	TaxableSocialSecurityPct    float64
	AnnualNIIT                  float64
	AnnualIRMAA                 float64
}

// AnnualizedInputs extrapolates YTD totals plus the current month to a
// full year by linear annualization. Roth conversions are deliberately
// not annualized — they're discrete events.
func (a ProjectionTaxAccumulator) AnnualizedInputs(monthInYear int, ordinaryIncome, socialSecurityIncome, taxableWithdrawals, qualifiedDividends, longTermCapitalGains, nonQualifiedDividends, rothConversions float64) ProjectedAnnualTaxInputs {
	monthsElapsed := float64(monthInYear + 1)
	if monthsElapsed <= 0 {
		monthsElapsed = 1
	}
	annualizationFactor := 12.0 / monthsElapsed

	return ProjectedAnnualTaxInputs{
		OrdinaryIncome:        (a.OrdinaryIncomeYTD + ordinaryIncome) * annualizationFactor,
		SocialSecurityIncome:  (a.SocialSecurityIncomeYTD + socialSecurityIncome) * annualizationFactor,
		TaxableWithdrawals:    (a.TaxableWithdrawalsYTD + taxableWithdrawals) * annualizationFactor,
		QualifiedDividends:    (a.QualifiedDividendsYTD + qualifiedDividends) * annualizationFactor,
		LongTermCapitalGains:  (a.LongTermCapitalGainsYTD + longTermCapitalGains) * annualizationFactor,
		NonQualifiedDividends: (a.NonQualifiedDividendsYTD + nonQualifiedDividends) * annualizationFactor,
		RothConversions:       a.RothConversionsYTD + rothConversions,
	}
}

// EstimateMonthlySnapshot computes the per-month tax + IRMAA estimate
// for a given month, given YTD state and the month's income components.
// The remaining-months division ensures actual taxes paid converge to
// the annual liability over the year.
func (a ProjectionTaxAccumulator) EstimateMonthlySnapshot(
	tc *TaxCalculator,
	yearsFromBase int,
	monthInYear int,
	ordinaryIncome float64,
	socialSecurityIncome float64,
	taxableWithdrawals float64,
	qualifiedDividends float64,
	longTermCapitalGains float64,
	nonQualifiedDividends float64,
	rothConversions float64,
	completedMAGIHistory []float64,
	assumedIRMALookbackMAGI *float64,
	irmaaEligibleAdults int,
	irmaaInflationFactor float64,
) ProjectedTaxSnapshot {
	if tc == nil {
		return ProjectedTaxSnapshot{}
	}

	inputs := a.AnnualizedInputs(monthInYear, ordinaryIncome, socialSecurityIncome, taxableWithdrawals, qualifiedDividends, longTermCapitalGains, nonQualifiedDividends, rothConversions)
	otherIncome := inputs.OrdinaryIncome + inputs.TaxableWithdrawals + inputs.RothConversions
	taxableSocialSecurity := tc.CalculateTaxableSocialSecurity(inputs.SocialSecurityIncome, otherIncome, inputs.QualifiedDividends, inputs.LongTermCapitalGains)
	estimatedOrdinaryIncome := otherIncome + taxableSocialSecurity

	taxBreakdown := tc.CalculateTaxWithInvestmentIncomeBreakdown(estimatedOrdinaryIncome, inputs.QualifiedDividends, inputs.LongTermCapitalGains, inputs.NonQualifiedDividends, yearsFromBase)
	lookbackMAGI, hasIRMALookback := resolveIRMAALookbackMAGI(completedMAGIHistory, assumedIRMALookbackMAGI)

	annualIRMAA := 0.0
	if irmaaEligibleAdults > 0 && hasIRMALookback {
		annualIRMAA = tc.CalculateMonthlyIRMAA(lookbackMAGI, irmaaInflationFactor) * float64(irmaaEligibleAdults) * 12
	}
	remainingMonths := 12 - monthInYear
	if remainingMonths <= 0 {
		remainingMonths = 1
	}

	taxDue := (taxBreakdown.TotalTax - a.TaxesPaidYTD) / float64(remainingMonths)
	if taxDue < 0 {
		taxDue = 0
	}

	taxableSocialSecurityPct := 0.0
	if inputs.SocialSecurityIncome > 0 {
		taxableSocialSecurityPct = taxableSocialSecurity / inputs.SocialSecurityIncome * 100
	}

	return ProjectedTaxSnapshot{
		MonthlyTax:                  taxDue,
		MonthlyIRMAA:                annualIRMAA / 12,
		AnnualMAGI:                  taxBreakdown.MAGI,
		AnnualTaxableSocialSecurity: taxableSocialSecurity,
		TaxableSocialSecurityPct:    taxableSocialSecurityPct,
		AnnualNIIT:                  taxBreakdown.NIIT,
		AnnualIRMAA:                 annualIRMAA,
	}
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
	return a.EstimateMonthlySnapshot(
		tc,
		yearsFromBase,
		monthInYear,
		ordinaryIncome,
		socialSecurityIncome,
		taxableWithdrawals,
		qualifiedDividends,
		longTermCapitalGains,
		nonQualifiedDividends,
		rothConversions,
		nil,
		nil,
		0,
		1,
	).MonthlyTax
}

// ApplyMonth folds a month's realised income and tax payments into the
// accumulator. Mutating receiver — caller resets at year boundary.
func (a *ProjectionTaxAccumulator) ApplyMonth(ordinaryIncome, socialSecurityIncome, taxableWithdrawals, qualifiedDividends, longTermCapitalGains, nonQualifiedDividends, rothConversions, taxesPaid float64) {
	a.OrdinaryIncomeYTD += ordinaryIncome
	a.SocialSecurityIncomeYTD += socialSecurityIncome
	a.TaxableWithdrawalsYTD += taxableWithdrawals
	a.QualifiedDividendsYTD += qualifiedDividends
	a.LongTermCapitalGainsYTD += longTermCapitalGains
	a.NonQualifiedDividendsYTD += nonQualifiedDividends
	a.RothConversionsYTD += rothConversions
	a.TaxesPaidYTD += taxesPaid
}
