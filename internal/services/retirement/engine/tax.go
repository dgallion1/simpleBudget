package engine

import (
	"math"

	"budget2/internal/models"
)

// taxBaseYear is the calendar year on which the bundled federal tax
// brackets, standard deductions, and SS/NIIT thresholds are based.
// Inflation factors are computed as years-from-this-year.
const taxBaseYear = 2024

// FederalTaxBracket represents a single tax bracket.
type FederalTaxBracket struct {
	MinIncome float64 // Minimum income for this bracket
	MaxIncome float64 // Maximum income (math.MaxFloat64 for top bracket)
	Rate      float64 // Tax rate as decimal (e.g., 0.22 for 22%)
}

type socialSecurityTaxThreshold struct {
	BaseThreshold     float64
	UpperThreshold    float64
	BaseTaxableAmount float64
}

// investmentIncomeTaxBreakdown is the structured return of
// CalculateTaxWithInvestmentIncomeBreakdown. Fields are exported so
// callers can read them; the type itself is unexported because callers
// always rely on type inference.
type investmentIncomeTaxBreakdown struct {
	FederalTax    float64
	StateTax      float64
	NIIT          float64
	TotalTax      float64
	EffectiveRate float64
	MAGI          float64
}

// TaxBrackets2024 contains 2024 federal tax brackets by filing status.
// Source: IRS Revenue Procedure 2023-34.
var TaxBrackets2024 = map[models.FilingStatus][]FederalTaxBracket{
	models.FilingSingle: {
		{0, 11600, 0.10},
		{11600, 47150, 0.12},
		{47150, 100525, 0.22},
		{100525, 191950, 0.24},
		{191950, 243725, 0.32},
		{243725, 609350, 0.35},
		{609350, math.MaxFloat64, 0.37},
	},
	models.FilingMarriedJoint: {
		{0, 23200, 0.10},
		{23200, 94300, 0.12},
		{94300, 201050, 0.22},
		{201050, 383900, 0.24},
		{383900, 487450, 0.32},
		{487450, 731200, 0.35},
		{731200, math.MaxFloat64, 0.37},
	},
	models.FilingMarriedSeparate: {
		{0, 11600, 0.10},
		{11600, 47150, 0.12},
		{47150, 100525, 0.22},
		{100525, 191950, 0.24},
		{191950, 243725, 0.32},
		{243725, 365600, 0.35},
		{365600, math.MaxFloat64, 0.37},
	},
	models.FilingHeadOfHousehold: {
		{0, 16550, 0.10},
		{16550, 63100, 0.12},
		{63100, 100500, 0.22},
		{100500, 191950, 0.24},
		{191950, 243700, 0.32},
		{243700, 609350, 0.35},
		{609350, math.MaxFloat64, 0.37},
	},
}

// LongTermCapitalGainsBrackets2024 contains 2024 federal long-term
// capital gains brackets.
var LongTermCapitalGainsBrackets2024 = map[models.FilingStatus][]FederalTaxBracket{
	models.FilingSingle: {
		{0, 47025, 0.00},
		{47025, 518900, 0.15},
		{518900, math.MaxFloat64, 0.20},
	},
	models.FilingMarriedJoint: {
		{0, 94050, 0.00},
		{94050, 583750, 0.15},
		{583750, math.MaxFloat64, 0.20},
	},
	models.FilingMarriedSeparate: {
		{0, 47025, 0.00},
		{47025, 291850, 0.15},
		{291850, math.MaxFloat64, 0.20},
	},
	models.FilingHeadOfHousehold: {
		{0, 63000, 0.00},
		{63000, 551350, 0.15},
		{551350, math.MaxFloat64, 0.20},
	},
}

// StandardDeduction2024 contains 2024 standard deductions by filing status.
var StandardDeduction2024 = map[models.FilingStatus]float64{
	models.FilingSingle:          14600,
	models.FilingMarriedJoint:    29200,
	models.FilingMarriedSeparate: 14600,
	models.FilingHeadOfHousehold: 21900,
}

// AdditionalStandardDeduction2024Age65 — TY2024 amounts per qualifying
// person 65 or older. Source: IRS Rev. Proc. 2023-34 §3.16(2).
var AdditionalStandardDeduction2024Age65 = map[models.FilingStatus]float64{
	models.FilingSingle:          1950,
	models.FilingHeadOfHousehold: 1950,
	models.FilingMarriedJoint:    1550,
	models.FilingMarriedSeparate: 1550,
}

var socialSecurityTaxThresholds = map[models.FilingStatus]socialSecurityTaxThreshold{
	models.FilingSingle:          {BaseThreshold: 25000, UpperThreshold: 34000, BaseTaxableAmount: 4500},
	models.FilingMarriedJoint:    {BaseThreshold: 32000, UpperThreshold: 44000, BaseTaxableAmount: 6000},
	models.FilingHeadOfHousehold: {BaseThreshold: 25000, UpperThreshold: 34000, BaseTaxableAmount: 4500},
}

// F-018: MFS lived-apart threshold per 26 USC § 86(c)(2)(A).
// MFS lived-with-spouse: handled directly as 85% return (no threshold lookup needed).
var socialSecurityTaxThresholdsMFSLivedApart = socialSecurityTaxThreshold{
	BaseThreshold:     25000,
	UpperThreshold:    34000,
	BaseTaxableAmount: 4500,
}

var niitThresholds = map[models.FilingStatus]float64{
	models.FilingSingle:          200000,
	models.FilingMarriedJoint:    250000,
	models.FilingMarriedSeparate: 125000,
	models.FilingHeadOfHousehold: 200000,
}

// 2026 CMS IRMAA amounts are used as the source table.
// The planner rescales them onto the tax model's 2024 base year, then inflates forward.
var monthlyIRMAASurcharge2026 = map[models.FilingStatus][]struct {
	UpperMAGI float64
	Surcharge float64
}{
	models.FilingSingle: {
		{109000, 0},
		{137000, 81.20 + 14.50},
		{171000, 202.90 + 37.50},
		{205000, 324.60 + 60.40},
		{500000, 446.30 + 83.30},
		{math.MaxFloat64, 487.00 + 91.00},
	},
	models.FilingMarriedJoint: {
		{218000, 0},
		{274000, 81.20 + 14.50},
		{342000, 202.90 + 37.50},
		{410000, 324.60 + 60.40},
		{750000, 446.30 + 83.30},
		{math.MaxFloat64, 487.00 + 91.00},
	},
	models.FilingMarriedSeparate: {
		{109000, 0},
		{391000, 446.30 + 83.30},
		{math.MaxFloat64, 487.00 + 91.00},
	},
	models.FilingHeadOfHousehold: {
		{109000, 0},
		{137000, 81.20 + 14.50},
		{171000, 202.90 + 37.50},
		{205000, 324.60 + 60.40},
		{500000, 446.30 + 83.30},
		{math.MaxFloat64, 487.00 + 91.00},
	},
}

// TaxCalculator computes federal and state income taxes.
type TaxCalculator struct {
	FilingStatus       models.FilingStatus
	StateRate          float64 // State income tax rate as percentage (e.g., 5.0 for 5%)
	InflationRate      float64 // Annual inflation rate for bracket adjustment
	BaseYear           int     // Year the brackets are based on
	Age65Count         int     // F-001: number of filers 65 or older (0, 1, or 2 for MFJ)
	MFSLivedWithSpouse bool    // F-018: 26 USC § 86(c)(2) sub-case; true = lived with spouse → $0/$0 SS thresholds
}

// NewTaxCalculator creates a tax calculator with the given configuration.
func NewTaxCalculator(config *models.TaxConfig, inflationRate float64) *TaxCalculator {
	if config == nil {
		config = models.DefaultTaxConfig()
	}
	return &TaxCalculator{
		FilingStatus:       config.FilingStatus,
		StateRate:          config.StateIncomeTaxRateOrZero(),
		InflationRate:      inflationRate,
		BaseYear:           taxBaseYear,
		Age65Count:         config.Age65Count,
		MFSLivedWithSpouse: config.MFSLivedWithSpouse,
	}
}

// GetAdjustedBrackets returns tax brackets adjusted for inflation from
// base year.
func (tc *TaxCalculator) GetAdjustedBrackets(yearsFromBase int) []FederalTaxBracket {
	baseBrackets := TaxBrackets2024[tc.FilingStatus]
	if baseBrackets == nil {
		baseBrackets = TaxBrackets2024[models.FilingMarriedJoint]
	}

	if yearsFromBase <= 0 {
		return baseBrackets
	}

	inflationFactor := tc.InflationFactor(yearsFromBase)
	adjusted := make([]FederalTaxBracket, len(baseBrackets))

	for i, bracket := range baseBrackets {
		adjusted[i] = FederalTaxBracket{
			MinIncome: bracket.MinIncome * inflationFactor,
			MaxIncome: bracket.MaxIncome,
			Rate:      bracket.Rate,
		}
		if bracket.MaxIncome < math.MaxFloat64 {
			adjusted[i].MaxIncome = bracket.MaxIncome * inflationFactor
		}
	}

	return adjusted
}

func (tc *TaxCalculator) GetAdjustedLongTermCapitalGainsBrackets(yearsFromBase int) []FederalTaxBracket {
	baseBrackets := LongTermCapitalGainsBrackets2024[tc.FilingStatus]
	if baseBrackets == nil {
		baseBrackets = LongTermCapitalGainsBrackets2024[models.FilingMarriedJoint]
	}

	if yearsFromBase <= 0 {
		return baseBrackets
	}

	inflationFactor := tc.InflationFactor(yearsFromBase)
	adjusted := make([]FederalTaxBracket, len(baseBrackets))

	for i, bracket := range baseBrackets {
		adjusted[i] = FederalTaxBracket{
			MinIncome: bracket.MinIncome * inflationFactor,
			MaxIncome: bracket.MaxIncome,
			Rate:      bracket.Rate,
		}
		if bracket.MaxIncome < math.MaxFloat64 {
			adjusted[i].MaxIncome = bracket.MaxIncome * inflationFactor
		}
	}

	return adjusted
}

// GetAdjustedStandardDeduction returns standard deduction adjusted for
// inflation, including the age-65+ additional deduction per IRS Rev.
// Proc. 2023-34 §3.16(2).
func (tc *TaxCalculator) GetAdjustedStandardDeduction(yearsFromBase int) float64 {
	status := NormalizeFilingStatus(tc.FilingStatus)
	base, ok := StandardDeduction2024[status]
	if !ok {
		base = StandardDeduction2024[models.FilingMarriedJoint]
	}
	addPerPerson := AdditionalStandardDeduction2024Age65[status]
	count := tc.Age65Count
	if count < 0 {
		count = 0
	}
	if count > 2 {
		count = 2
	}
	additional := float64(count) * addPerPerson
	return (base + additional) * tc.InflationFactor(yearsFromBase)
}

// InflationFactor returns the cumulative inflation factor for
// yearsFromBase years at the calculator's annual inflation rate.
func (tc *TaxCalculator) InflationFactor(yearsFromBase int) float64 {
	if yearsFromBase <= 0 {
		return 1
	}
	return math.Pow(1+tc.InflationRate/100, float64(yearsFromBase))
}

// NormalizeFilingStatus coerces unknown filing statuses to MFJ. Exported
// so retirement-package code (and tests during the migration window)
// can share the canonicalisation rule.
func NormalizeFilingStatus(filingStatus models.FilingStatus) models.FilingStatus {
	switch filingStatus {
	case models.FilingSingle, models.FilingMarriedJoint, models.FilingMarriedSeparate, models.FilingHeadOfHousehold:
		return filingStatus
	default:
		return models.FilingMarriedJoint
	}
}

// CalculateTaxableSocialSecurity computes the IRS-taxable portion of
// Social Security benefits per 26 USC § 86.
func CalculateTaxableSocialSecurity(ssBenefits, otherIncome, qualifiedDividends, longTermCapitalGains float64, filingStatus models.FilingStatus, mfsLivedWithSpouse bool) float64 {
	if ssBenefits <= 0 {
		return 0
	}

	filingStatus = NormalizeFilingStatus(filingStatus)

	// F-018: MFS has two sub-cases per 26 USC § 86(c)(2).
	if filingStatus == models.FilingMarriedSeparate {
		if mfsLivedWithSpouse {
			// 26 USC § 86(c)(2)(B): $0 base threshold — no exclusion; 85% cap applies
			// immediately to all SS benefits regardless of other income.
			return ssBenefits * 0.85
		}
		// MFS lived-apart: uses Single thresholds ($25K/$34K) per § 86(c)(2)(A).
		thresholds := socialSecurityTaxThresholdsMFSLivedApart
		provisionalIncome := math.Max(0, otherIncome) + math.Max(0, qualifiedDividends) + math.Max(0, longTermCapitalGains) + (0.5 * ssBenefits)
		if provisionalIncome <= thresholds.BaseThreshold {
			return 0
		}
		if provisionalIncome <= thresholds.UpperThreshold {
			return math.Min(ssBenefits*0.5, (provisionalIncome-thresholds.BaseThreshold)*0.5)
		}
		taxable := math.Min(ssBenefits*0.5, thresholds.BaseTaxableAmount) + (provisionalIncome-thresholds.UpperThreshold)*0.85
		return math.Min(ssBenefits*0.85, taxable)
	}

	// NormalizeFilingStatus guarantees a valid key for all non-MFS statuses.
	thresholds := socialSecurityTaxThresholds[filingStatus]

	provisionalIncome := math.Max(0, otherIncome) + math.Max(0, qualifiedDividends) + math.Max(0, longTermCapitalGains) + (0.5 * ssBenefits)
	if provisionalIncome <= thresholds.BaseThreshold {
		return 0
	}
	if provisionalIncome <= thresholds.UpperThreshold {
		return math.Min(ssBenefits*0.5, (provisionalIncome-thresholds.BaseThreshold)*0.5)
	}

	taxable := math.Min(ssBenefits*0.5, thresholds.BaseTaxableAmount) + (provisionalIncome-thresholds.UpperThreshold)*0.85
	return math.Min(ssBenefits*0.85, taxable)
}

// CalculateNIIT computes the 3.8% Net Investment Income Tax surcharge.
func CalculateNIIT(magi, netInvestmentIncome float64, filingStatus models.FilingStatus) float64 {
	if magi <= 0 || netInvestmentIncome <= 0 {
		return 0
	}

	threshold := niitThresholds[NormalizeFilingStatus(filingStatus)]

	excessMAGI := magi - threshold
	if excessMAGI <= 0 {
		return 0
	}

	return math.Min(netInvestmentIncome, excessMAGI) * 0.038
}

// CalculateMonthlyIRMAA returns the monthly IRMAA Part B+D surcharge per
// person at the supplied MAGI, inflating the bundled 2026 bracket table.
//
// The two factors index different things and must not be collapsed into one
// (F-6): thresholdFactor moves the MAGI cutoffs, which are statutorily
// CPI-indexed, while surchargeFactor moves the dollars charged once a cutoff
// is cleared, which track Medicare per-capita cost growth. Passing the CPI
// factor for both understated every future surcharge.
func CalculateMonthlyIRMAA(magi float64, filingStatus models.FilingStatus, thresholdFactor, surchargeFactor float64) float64 {
	if magi <= 0 {
		return 0
	}
	if thresholdFactor <= 0 {
		thresholdFactor = 1
	}
	if surchargeFactor <= 0 {
		surchargeFactor = 1
	}

	filingStatus = NormalizeFilingStatus(filingStatus)
	brackets := monthlyIRMAASurcharge2026[filingStatus]

	var surcharge float64
	for _, bracket := range brackets {
		surcharge = bracket.Surcharge * surchargeFactor
		upperMAGI := bracket.UpperMAGI
		if upperMAGI < math.MaxFloat64 {
			upperMAGI *= thresholdFactor
		}
		if magi <= upperMAGI {
			break
		}
	}

	return surcharge
}

func (tc *TaxCalculator) CalculateTaxableSocialSecurity(ssBenefits, otherIncome, qualifiedDividends, longTermCapitalGains float64) float64 {
	return CalculateTaxableSocialSecurity(ssBenefits, otherIncome, qualifiedDividends, longTermCapitalGains, tc.FilingStatus, tc.MFSLivedWithSpouse)
}

func (tc *TaxCalculator) CalculateNIIT(magi, netInvestmentIncome float64) float64 {
	return CalculateNIIT(magi, netInvestmentIncome, tc.FilingStatus)
}

func (tc *TaxCalculator) CalculateMonthlyIRMAA(magi, thresholdFactor, surchargeFactor float64) float64 {
	return CalculateMonthlyIRMAA(magi, tc.FilingStatus, thresholdFactor, surchargeFactor)
}

// CalculateFederalTax computes federal tax on taxable income.
// Returns: tax amount, effective rate (%), marginal bracket rate (%).
func (tc *TaxCalculator) CalculateFederalTax(grossIncome float64, yearsFromBase int) (tax float64, effectiveRate float64, marginalBracket float64) {
	if grossIncome <= 0 {
		return 0, 0, 0
	}

	standardDeduction := tc.GetAdjustedStandardDeduction(yearsFromBase)
	taxableIncome := math.Max(0, grossIncome-standardDeduction)

	if taxableIncome <= 0 {
		return 0, 0, 10
	}

	brackets := tc.GetAdjustedBrackets(yearsFromBase)

	totalTax, currentBracketRate := calculateFederalTaxOnTaxableIncome(taxableIncome, brackets)

	effectiveRate = (totalTax / grossIncome) * 100
	marginalBracket = currentBracketRate * 100

	return totalTax, effectiveRate, marginalBracket
}

func calculateFederalTaxOnTaxableIncome(taxableIncome float64, brackets []FederalTaxBracket) (tax float64, marginalRate float64) {
	if taxableIncome <= 0 {
		return 0, 0
	}

	for _, bracket := range brackets {
		if taxableIncome <= bracket.MinIncome {
			break
		}

		bracketMin := bracket.MinIncome
		bracketMax := math.Min(bracket.MaxIncome, taxableIncome)
		taxableInBracket := bracketMax - bracketMin

		if taxableInBracket > 0 {
			tax += taxableInBracket * bracket.Rate
			marginalRate = bracket.Rate
		}
	}

	return tax, marginalRate
}

// CalculateStateTax computes state income tax.
func (tc *TaxCalculator) CalculateStateTax(taxableIncome float64) float64 {
	if taxableIncome <= 0 || tc.StateRate <= 0 {
		return 0
	}
	return taxableIncome * (tc.StateRate / 100)
}

// CalculateTotalTax computes combined federal and state tax.
func (tc *TaxCalculator) CalculateTotalTax(grossIncome float64, yearsFromBase int) (federalTax, stateTax, totalTax, effectiveRate float64) {
	federalTax, _, _ = tc.CalculateFederalTax(grossIncome, yearsFromBase)

	standardDeduction := tc.GetAdjustedStandardDeduction(yearsFromBase)
	taxableForState := math.Max(0, grossIncome-standardDeduction)
	stateTax = tc.CalculateStateTax(taxableForState)

	totalTax = federalTax + stateTax
	if grossIncome > 0 {
		effectiveRate = (totalTax / grossIncome) * 100
	}

	return federalTax, stateTax, totalTax, effectiveRate
}

func (tc *TaxCalculator) CalculateTaxWithInvestmentIncome(ordinaryIncome, qualifiedDividends, longTermCapitalGains float64, yearsFromBase int) (federalTax, stateTax, totalTax, effectiveRate float64) {
	breakdown := tc.CalculateTaxBreakdown(InvestmentIncomeTaxInputs{
		OrdinaryIncome:       ordinaryIncome,
		QualifiedDividends:   qualifiedDividends,
		LongTermCapitalGains: longTermCapitalGains,
	}, yearsFromBase)
	return breakdown.FederalTax, breakdown.StateTax, breakdown.TotalTax, breakdown.EffectiveRate
}

// InvestmentIncomeTaxInputs is a year's income split by how each piece is
// taxed. Named fields rather than a positional list: these are all float64
// dollars, so a transposed pair would be silent.
//
// ShortTermCapitalGains and NonQualifiedDividends receive identical treatment
// — taxed at ordinary rates, and counted as net investment income for NIIT —
// but they are kept apart so callers can report them separately and so the
// distinction survives if the two ever diverge.
type InvestmentIncomeTaxInputs struct {
	// OrdinaryIncome is wages, pensions, tax-deferred withdrawals, Roth
	// conversions and the taxable portion of Social Security.
	OrdinaryIncome float64
	// QualifiedDividends and LongTermCapitalGains use the preferential
	// capital-gains brackets, stacked on top of ordinary income.
	QualifiedDividends   float64
	LongTermCapitalGains float64
	// NonQualifiedDividends is the portion of OrdinaryIncome that is
	// non-qualified dividends: already counted there for rate purposes, named
	// here so NIIT can see it.
	NonQualifiedDividends float64
	// ShortTermCapitalGains is gain on positions held one year or less. Unlike
	// LongTermCapitalGains it gets no preferential rate — 26 USC § 1(h)
	// applies only to net long-term gain — so it is taxed as ordinary income,
	// and it crowds long-term gain out of the 0% bracket exactly as wages do.
	//
	// Supply it as its own field, NOT folded into OrdinaryIncome: this
	// function adds it there itself, and also counts it as net investment
	// income for NIIT and includes it in the § 86 provisional-income base.
	ShortTermCapitalGains float64
}

// ordinaryTotal is everything taxed at ordinary rates: ordinary income plus
// short-term capital gain.
func (in InvestmentIncomeTaxInputs) ordinaryTotal() float64 {
	return in.OrdinaryIncome + in.ShortTermCapitalGains
}

// CalculateTaxBreakdown computes federal, state and NIIT liability from a
// full income composition, including short-term capital gain.
func (tc *TaxCalculator) CalculateTaxBreakdown(in InvestmentIncomeTaxInputs, yearsFromBase int) investmentIncomeTaxBreakdown {
	return tc.calculateTaxWithInvestmentIncomeInternal(in, yearsFromBase)
}

// CalculateTaxWithInvestmentIncomeBreakdown returns a detailed tax
// breakdown including NIIT. nonQualifiedDividends should be the portion
// of ordinaryIncome that represents non-qualified dividends — these are
// taxed as ordinary income but also count as net investment income for
// NIIT.
func (tc *TaxCalculator) CalculateTaxWithInvestmentIncomeBreakdown(ordinaryIncome, qualifiedDividends, longTermCapitalGains, nonQualifiedDividends float64, yearsFromBase int) investmentIncomeTaxBreakdown {
	return tc.calculateTaxWithInvestmentIncomeInternal(InvestmentIncomeTaxInputs{
		OrdinaryIncome:        ordinaryIncome,
		QualifiedDividends:    qualifiedDividends,
		LongTermCapitalGains:  longTermCapitalGains,
		NonQualifiedDividends: nonQualifiedDividends,
	}, yearsFromBase)
}

func (tc *TaxCalculator) calculateTaxWithInvestmentIncomeInternal(in InvestmentIncomeTaxInputs, yearsFromBase int) investmentIncomeTaxBreakdown {
	ordinaryIncome := in.ordinaryTotal()
	qualifiedDividends := in.QualifiedDividends
	longTermCapitalGains := in.LongTermCapitalGains

	totalGrossIncome := ordinaryIncome + qualifiedDividends + longTermCapitalGains
	if totalGrossIncome <= 0 {
		return investmentIncomeTaxBreakdown{}
	}

	// The standard deduction is consumed by ordinary income (short-term gain
	// included) before any is left over for preferentially-taxed income.
	standardDeduction := tc.GetAdjustedStandardDeduction(yearsFromBase)
	taxableOrdinaryIncome := math.Max(0, ordinaryIncome-standardDeduction)
	remainingDeduction := math.Max(0, standardDeduction-ordinaryIncome)
	taxableInvestmentIncome := math.Max(0, qualifiedDividends+longTermCapitalGains-remainingDeduction)

	brackets := tc.GetAdjustedBrackets(yearsFromBase)
	ordinaryFederalTax, _ := calculateFederalTaxOnTaxableIncome(taxableOrdinaryIncome, brackets)

	investmentFederalTax := 0.0
	if taxableInvestmentIncome > 0 {
		totalTaxableIncome := taxableOrdinaryIncome + taxableInvestmentIncome
		ltcgBrackets := tc.GetAdjustedLongTermCapitalGainsBrackets(yearsFromBase)
		for _, bracket := range ltcgBrackets {
			start := math.Max(taxableOrdinaryIncome, bracket.MinIncome)
			end := math.Min(totalTaxableIncome, bracket.MaxIncome)
			if end > start {
				investmentFederalTax += (end - start) * bracket.Rate
			}
		}
	}

	magi := ordinaryIncome + qualifiedDividends + longTermCapitalGains
	netInvestmentIncome := qualifiedDividends + longTermCapitalGains + in.NonQualifiedDividends + in.ShortTermCapitalGains
	niit := tc.CalculateNIIT(magi, netInvestmentIncome)
	stateTaxableIncome := taxableOrdinaryIncome + taxableInvestmentIncome
	stateTax := tc.CalculateStateTax(stateTaxableIncome)
	federalTax := ordinaryFederalTax + investmentFederalTax + niit
	totalTax := federalTax + stateTax
	effectiveRate := (totalTax / totalGrossIncome) * 100

	return investmentIncomeTaxBreakdown{
		FederalTax:    federalTax,
		StateTax:      stateTax,
		NIIT:          niit,
		TotalTax:      totalTax,
		EffectiveRate: effectiveRate,
		MAGI:          magi,
	}
}

// EstimateRothConversionTax estimates the tax impact of a Roth
// conversion. Returns the additional tax owed due to the conversion.
func (tc *TaxCalculator) EstimateRothConversionTax(baseIncome, conversionAmount float64, yearsFromBase int) float64 {
	if conversionAmount <= 0 {
		return 0
	}

	taxWithout, _, _ := tc.CalculateFederalTax(baseIncome, yearsFromBase)
	taxWith, _, _ := tc.CalculateFederalTax(baseIncome+conversionAmount, yearsFromBase)

	return taxWith - taxWithout
}

// GetBracketRate returns the statutory ordinary-income bracket rate (percent)
// that a given gross income falls in, after the standard deduction.
//
// This is a table lookup, and it is NOT the marginal tax rate. It cannot see
// capital-gain stacking, the § 86 Social Security phase-in, or NIIT, so the
// real cost of the next dollar is routinely far higher than what this
// returns. For that, use MarginalRateOnOrdinaryIncome, which differentiates
// the actual tax function numerically.
//
// Reporting this value to a user as their "marginal rate" was a defect; the
// name now says what it does.
func (tc *TaxCalculator) GetBracketRate(grossIncome float64, yearsFromBase int) float64 {
	if grossIncome <= 0 {
		return 10
	}

	standardDeduction := tc.GetAdjustedStandardDeduction(yearsFromBase)
	taxableIncome := math.Max(0, grossIncome-standardDeduction)

	brackets := tc.GetAdjustedBrackets(yearsFromBase)

	var rate float64
	for _, bracket := range brackets {
		rate = bracket.Rate * 100
		if taxableIncome <= bracket.MaxIncome {
			break
		}
	}

	return rate
}
