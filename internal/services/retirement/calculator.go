package retirement

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"budget2/internal/models"
)

// ResolvedScenarioChainLink holds a pre-loaded chain link for runtime use
type ResolvedScenarioChainLink struct {
	ScenarioFilename string
	TransitionAge    int
	Settings         *models.WhatIfSettings
}

// Calculator performs retirement projections and analysis
type Calculator struct {
	Settings      *models.WhatIfSettings
	ResolvedChain []ResolvedScenarioChainLink
}

// NewCalculator creates a new retirement calculator with the given settings
func NewCalculator(settings *models.WhatIfSettings) *Calculator {
	return &Calculator{Settings: settings}
}

// NewCalculatorWithChain creates a new retirement calculator with the given settings and scenario chain
func NewCalculatorWithChain(settings *models.WhatIfSettings, chain []ResolvedScenarioChainLink) *Calculator {
	return &Calculator{Settings: settings, ResolvedChain: chain}
}

// PresentValue calculates the present value of a future cash flow
// PV = FV / (1 + r)^n
func PresentValue(futureValue, annualRate float64, periods int) float64 {
	if periods <= 0 {
		return futureValue
	}
	if annualRate <= 0 {
		return futureValue
	}
	monthlyRate := annualRate / 100 / 12
	return futureValue / math.Pow(1+monthlyRate, float64(periods))
}

// PresentValueAnnuity calculates the PV of a series of payments
// Handles both regular and growing annuities
func PresentValueAnnuity(payment, discountRate, growthRate float64, startMonth, numPayments int) float64 {
	if numPayments <= 0 || payment == 0 {
		return 0
	}

	monthlyRate := discountRate / 100 / 12
	monthlyGrowth := growthRate / 100 / 12

	var pvAtStart float64

	if monthlyRate <= 0 {
		// No discounting
		if monthlyGrowth <= 0 {
			pvAtStart = payment * float64(numPayments)
		} else {
			// Sum with growth
			total := 0.0
			for m := 0; m < numPayments; m++ {
				total += payment * math.Pow(1+monthlyGrowth, float64(m))
			}
			pvAtStart = total
		}
	} else if math.Abs(monthlyRate-monthlyGrowth) < 1e-10 {
		// Growth equals discount rate
		pvAtStart = payment * float64(numPayments)
	} else if monthlyGrowth > 0 {
		// Growing annuity formula
		growthFactor := (1 + monthlyGrowth) / (1 + monthlyRate)
		pvAtStart = payment * (1 - math.Pow(growthFactor, float64(numPayments))) / (monthlyRate - monthlyGrowth)
	} else {
		// Regular annuity formula
		pvAtStart = payment * (1 - math.Pow(1+monthlyRate, -float64(numPayments))) / monthlyRate
	}

	// Discount back if payments start in the future
	if startMonth > 0 && monthlyRate > 0 {
		return pvAtStart / math.Pow(1+monthlyRate, float64(startMonth))
	}

	return pvAtStart
}

// calculateHealthcarePV calculates the present value of healthcare costs for a single person
// This handles the Medicare transition where costs and inflation rates change at age 65
func (c *Calculator) calculateHealthcarePV(person models.HealthcarePerson, discountRate float64, totalMonths int) float64 {
	pvTotal := 0.0

	if person.IsOnMedicare() {
		// Already on Medicare - simple PV calculation with post-Medicare inflation
		pvTotal = PresentValueAnnuity(person.CurrentMonthlyCost, discountRate, person.PostMedicareInflation, 0, totalMonths)
	} else {
		// Pre-Medicare: calculate in two phases
		yearsUntilMedicare := person.YearsUntilMedicare()
		preMedicareMonths := yearsUntilMedicare * 12

		if preMedicareMonths >= totalMonths {
			// Entire projection is pre-Medicare
			pvTotal = PresentValueAnnuity(person.CurrentMonthlyCost, discountRate, person.PreMedicareInflation, 0, totalMonths)
		} else {
			// Phase 1: Pre-Medicare period
			if preMedicareMonths > 0 {
				pvTotal += PresentValueAnnuity(person.CurrentMonthlyCost, discountRate, person.PreMedicareInflation, 0, preMedicareMonths)
			}

			// Phase 2: Post-Medicare period
			postMedicareMonths := totalMonths - preMedicareMonths
			if postMedicareMonths > 0 {
				pvTotal += PresentValueAnnuity(person.MedicareMonthlyCost, discountRate, person.PostMedicareInflation, preMedicareMonths, postMedicareMonths)
			}
		}
	}

	return pvTotal
}

// CalculateTotalIncome returns total income for a specific month
func (c *Calculator) CalculateTotalIncome(month int) float64 {
	total := 0.0
	for _, source := range c.Settings.IncomeSources {
		total += source.GetAdjustedAmount(month)
	}
	return total
}

func monthlyCompoundFactorFromDecimal(annualRate float64) float64 {
	if annualRate == 0 {
		return 1.0
	}
	return math.Pow(1+annualRate, 1.0/12.0)
}

func monthlyCompoundFactorFromPercent(annualRatePercent float64) float64 {
	return monthlyCompoundFactorFromDecimal(annualRatePercent / 100)
}

func compoundedFactorFromPercent(annualRatePercent float64, months float64) float64 {
	if annualRatePercent == 0 || months == 0 {
		return 1.0
	}
	return math.Pow(1+annualRatePercent/100, months/12.0)
}

func calculateLivingExpensesAtMonth(s *models.WhatIfSettings, month int) float64 {
	years := month / 12
	phaseAge := s.GetPhaseReferenceAge(years)
	monthsElapsed := float64(month)

	if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
		return s.MonthlyLivingExpenses * s.GetSpendingMultiplier(phaseAge) * compoundedFactorFromPercent(s.InflationRate, monthsElapsed)
	}

	return s.MonthlyLivingExpenses * compoundedFactorFromPercent(s.InflationRate-s.SpendingDeclineRate, monthsElapsed)
}

func rebaseLivingExpensesAtTransition(s *models.WhatIfSettings, phaseAge int, cumulativeInflation float64) float64 {
	if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
		return s.MonthlyLivingExpenses * s.GetSpendingMultiplier(phaseAge) * cumulativeInflation
	}

	return s.MonthlyLivingExpenses * cumulativeInflation
}

// Phase 4 projection tax policy:
//   - Taxable-account sales realize average-cost capital gains based on the current
//     taxable-account basis model.
//   - Pension, Social Security, tax-deferred withdrawals (including RMDs), non-qualified
//     dividends, and Roth conversions are taxed as ordinary income.
//   - Qualified dividends and realized long-term capital gains use the simplified
//     long-term capital-gains brackets in tax.go.
//   - Roth withdrawals are not taxed.
type projectionTaxAccumulator struct {
	OrdinaryIncomeYTD        float64
	SocialSecurityIncomeYTD  float64
	TaxableWithdrawalsYTD    float64
	QualifiedDividendsYTD    float64
	LongTermCapitalGainsYTD  float64
	NonQualifiedDividendsYTD float64
	RothConversionsYTD       float64
	TaxesPaidYTD             float64
}

type projectedAnnualTaxInputs struct {
	OrdinaryIncome        float64
	SocialSecurityIncome  float64
	TaxableWithdrawals    float64
	QualifiedDividends    float64
	LongTermCapitalGains  float64
	NonQualifiedDividends float64
	RothConversions       float64
}

type projectedTaxSnapshot struct {
	MonthlyTax                  float64
	MonthlyIRMAA                float64
	AnnualMAGI                  float64
	AnnualTaxableSocialSecurity float64
	TaxableSocialSecurityPct    float64
	AnnualNIIT                  float64
	AnnualIRMAA                 float64
}

func (a projectionTaxAccumulator) annualizedInputs(monthInYear int, ordinaryIncome, socialSecurityIncome, taxableWithdrawals, qualifiedDividends, longTermCapitalGains, nonQualifiedDividends, rothConversions float64) projectedAnnualTaxInputs {
	monthsElapsed := float64(monthInYear + 1)
	if monthsElapsed <= 0 {
		monthsElapsed = 1
	}
	annualizationFactor := 12.0 / monthsElapsed

	return projectedAnnualTaxInputs{
		OrdinaryIncome:        (a.OrdinaryIncomeYTD + ordinaryIncome) * annualizationFactor,
		SocialSecurityIncome:  (a.SocialSecurityIncomeYTD + socialSecurityIncome) * annualizationFactor,
		TaxableWithdrawals:    (a.TaxableWithdrawalsYTD + taxableWithdrawals) * annualizationFactor,
		QualifiedDividends:    (a.QualifiedDividendsYTD + qualifiedDividends) * annualizationFactor,
		LongTermCapitalGains:  (a.LongTermCapitalGainsYTD + longTermCapitalGains) * annualizationFactor,
		NonQualifiedDividends: (a.NonQualifiedDividendsYTD + nonQualifiedDividends) * annualizationFactor,
		RothConversions:       a.RothConversionsYTD + rothConversions,
	}
}

func (a projectionTaxAccumulator) estimateMonthlySnapshot(
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
) projectedTaxSnapshot {
	if tc == nil {
		return projectedTaxSnapshot{}
	}

	inputs := a.annualizedInputs(monthInYear, ordinaryIncome, socialSecurityIncome, taxableWithdrawals, qualifiedDividends, longTermCapitalGains, nonQualifiedDividends, rothConversions)
	otherIncome := inputs.OrdinaryIncome + inputs.TaxableWithdrawals + inputs.RothConversions
	taxableSocialSecurity := tc.CalculateTaxableSocialSecurity(inputs.SocialSecurityIncome, otherIncome, inputs.QualifiedDividends, inputs.LongTermCapitalGains)
	estimatedOrdinaryIncome := otherIncome + taxableSocialSecurity

	taxBreakdown := tc.CalculateTaxWithInvestmentIncomeBreakdown(estimatedOrdinaryIncome, inputs.QualifiedDividends, inputs.LongTermCapitalGains, inputs.NonQualifiedDividends, yearsFromBase)
	lookbackMAGI, hasIRMALookback := resolveIRMALookbackMAGI(completedMAGIHistory, assumedIRMALookbackMAGI)

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

	return projectedTaxSnapshot{
		MonthlyTax:                  taxDue,
		MonthlyIRMAA:                annualIRMAA / 12,
		AnnualMAGI:                  taxBreakdown.MAGI,
		AnnualTaxableSocialSecurity: taxableSocialSecurity,
		TaxableSocialSecurityPct:    taxableSocialSecurityPct,
		AnnualNIIT:                  taxBreakdown.NIIT,
		AnnualIRMAA:                 annualIRMAA,
	}
}

func resolveIRMALookbackMAGI(completedMAGIHistory []float64, assumedIRMALookbackMAGI *float64) (float64, bool) {
	if len(completedMAGIHistory) >= 2 {
		return completedMAGIHistory[len(completedMAGIHistory)-2], true
	}
	if assumedIRMALookbackMAGI != nil {
		return math.Max(0, *assumedIRMALookbackMAGI), true
	}
	return 0, false
}

func (a projectionTaxAccumulator) estimateMonthlyTaxes(tc *TaxCalculator, yearsFromBase, monthInYear int, ordinaryIncome, socialSecurityIncome, taxableWithdrawals, qualifiedDividends, longTermCapitalGains, nonQualifiedDividends, rothConversions float64) float64 {
	return a.estimateMonthlySnapshot(
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

func medicareEligibleAdultCountAtYear(s *models.WhatIfSettings, year int) int {
	if s == nil {
		return 0
	}

	count := 0
	if s.PrimaryAgeAt(year) >= 65 {
		count++
	}
	if s.HasSpouse() && s.SpouseAgeAt(year) >= 65 {
		count++
	}
	return count
}

func plannerInflationFactorForYear(annualInflationRate float64, years float64) float64 {
	if years <= 0 {
		return 1
	}
	return math.Pow(1+annualInflationRate/100, years)
}

func (a *projectionTaxAccumulator) applyMonth(ordinaryIncome, socialSecurityIncome, taxableWithdrawals, qualifiedDividends, longTermCapitalGains, nonQualifiedDividends, rothConversions, taxesPaid float64) {
	a.OrdinaryIncomeYTD += ordinaryIncome
	a.SocialSecurityIncomeYTD += socialSecurityIncome
	a.TaxableWithdrawalsYTD += taxableWithdrawals
	a.QualifiedDividendsYTD += qualifiedDividends
	a.LongTermCapitalGainsYTD += longTermCapitalGains
	a.NonQualifiedDividendsYTD += nonQualifiedDividends
	a.RothConversionsYTD += rothConversions
	a.TaxesPaidYTD += taxesPaid
}

type monthlyIncomeBreakdown struct {
	OrdinaryIncome       float64
	SocialSecurityIncome float64
	TotalIncome          float64
}

func calculateMonthlyIncomeBreakdown(s *models.WhatIfSettings, month int) monthlyIncomeBreakdown {
	breakdown := monthlyIncomeBreakdown{}
	for _, source := range s.IncomeSources {
		amount := source.GetAdjustedAmount(month)
		if amount <= 0 {
			continue
		}
		if isSocialSecurityIncomeSource(source) {
			breakdown.SocialSecurityIncome += amount
		} else {
			breakdown.OrdinaryIncome += amount
		}
	}
	breakdown.TotalIncome = breakdown.OrdinaryIncome + breakdown.SocialSecurityIncome
	return breakdown
}

func isSocialSecurityIncomeSource(source models.IncomeSource) bool {
	normalizedName := strings.ToLower(strings.ReplaceAll(source.Name, "-", " "))
	return strings.Contains(normalizedName, "social security")
}

func rothConversionAmountForYear(s *models.WhatIfSettings, currentYear int, availableTaxDeferred float64) float64 {
	if s.RothConversion == nil || !s.RothConversion.Enabled || availableTaxDeferred <= 0 {
		return 0
	}
	if currentYear < s.RothConversion.StartYear {
		return 0
	}
	if s.RothConversion.EndYear != 0 && currentYear > s.RothConversion.EndYear {
		return 0
	}
	return math.Min(s.RothConversion.AnnualAmount, availableTaxDeferred)
}

type taxableReturnComponents struct {
	Appreciation             float64
	QualifiedDividend        float64
	NonQualifiedDividend     float64
	CapitalGainsDistribution float64
}

type taxableGrowthResult struct {
	TotalGrowth               float64
	QualifiedDividends        float64
	NonQualifiedDividends     float64
	CapitalGainsDistributions float64
}

type taxableAccountState struct {
	MarketValue                  float64
	CostBasis                    float64
	QualifiedDividendYield       float64
	NonQualifiedDividendYield    float64
	CapitalGainsDistributionRate float64
	RealizedGainsYTD             float64
}

func newTaxableAccountState(s *models.WhatIfSettings, marketValue float64) taxableAccountState {
	qualifiedShare := s.GetTaxableQualifiedDividendPercent() / 100
	totalDividendYield := math.Max(0, s.TaxableDividendYield) / 100
	return taxableAccountState{
		MarketValue:                  marketValue,
		CostBasis:                    marketValue,
		QualifiedDividendYield:       totalDividendYield * qualifiedShare,
		NonQualifiedDividendYield:    totalDividendYield * (1 - qualifiedShare),
		CapitalGainsDistributionRate: math.Max(0, s.TaxableCapitalGainsDistributionRate) / 100,
	}
}

func (a *taxableAccountState) syncAssumptions(s *models.WhatIfSettings) {
	updated := newTaxableAccountState(s, a.MarketValue)
	updated.CostBasis = a.CostBasis
	updated.RealizedGainsYTD = a.RealizedGainsYTD
	*a = updated
}

func (a *taxableAccountState) addCash(amount float64) {
	if amount <= 0 {
		return
	}
	a.MarketValue += amount
	a.CostBasis += amount
}

func (a *taxableAccountState) withdraw(amount float64) (cash, basisReduction, realizedGain float64) {
	if amount <= 0 || a.MarketValue <= 0 {
		return 0, 0, 0
	}

	cash = math.Min(amount, a.MarketValue)
	basisReduction = 0.0
	if a.MarketValue > 0 {
		basisReduction = a.CostBasis * (cash / a.MarketValue)
	}

	a.MarketValue -= cash
	a.CostBasis -= basisReduction
	if a.MarketValue <= 0 {
		a.MarketValue = 0
		a.CostBasis = 0
	}
	if a.CostBasis < 0 {
		a.CostBasis = 0
	}

	realizedGain = cash - basisReduction
	a.RealizedGainsYTD += math.Max(0, realizedGain)
	return cash, basisReduction, realizedGain
}

func buildTaxableReturnComponents(totalAnnualReturnPercent float64, s *models.WhatIfSettings) taxableReturnComponents {
	totalMonthlyReturn := monthlyCompoundFactorFromDecimal(totalAnnualReturnPercent/100) - 1
	qualifiedDividendMonthly := monthlyCompoundFactorFromDecimal(math.Max(0, s.TaxableDividendYield)*(s.GetTaxableQualifiedDividendPercent()/100)/100) - 1
	nonQualifiedDividendMonthly := monthlyCompoundFactorFromDecimal(math.Max(0, s.TaxableDividendYield)*(1-s.GetTaxableQualifiedDividendPercent()/100)/100) - 1
	capitalGainsDistributionMonthly := monthlyCompoundFactorFromDecimal(math.Max(0, s.TaxableCapitalGainsDistributionRate)/100) - 1
	return taxableReturnComponents{
		Appreciation:             totalMonthlyReturn - qualifiedDividendMonthly - nonQualifiedDividendMonthly - capitalGainsDistributionMonthly,
		QualifiedDividend:        qualifiedDividendMonthly,
		NonQualifiedDividend:     nonQualifiedDividendMonthly,
		CapitalGainsDistribution: capitalGainsDistributionMonthly,
	}
}

func (a *taxableAccountState) applyGrowth(components taxableReturnComponents, fraction float64) taxableGrowthResult {
	if a.MarketValue <= 0 {
		return taxableGrowthResult{}
	}

	baseValue := a.MarketValue
	appreciation := baseValue * fractionalMonthlyReturn(components.Appreciation, fraction)
	qualifiedDividends := baseValue * fractionalMonthlyReturn(components.QualifiedDividend, fraction)
	nonQualifiedDividends := baseValue * fractionalMonthlyReturn(components.NonQualifiedDividend, fraction)
	capitalGainsDistributions := baseValue * fractionalMonthlyReturn(components.CapitalGainsDistribution, fraction)

	a.MarketValue += appreciation
	a.RealizedGainsYTD += capitalGainsDistributions

	if a.MarketValue < 0 {
		a.MarketValue = 0
	}
	if a.CostBasis < 0 {
		a.CostBasis = 0
	}

	return taxableGrowthResult{
		TotalGrowth:               appreciation,
		QualifiedDividends:        qualifiedDividends,
		NonQualifiedDividends:     nonQualifiedDividends,
		CapitalGainsDistributions: capitalGainsDistributions,
	}
}

func expectedTaxableMonthlyCashFlow(s *models.WhatIfSettings, taxableMarketValue, taxableAnnualReturn float64) taxableGrowthResult {
	account := newTaxableAccountState(s, taxableMarketValue)
	return account.applyGrowth(buildTaxableReturnComponents(taxableAnnualReturn, s), 1.0)
}

// CalculateTotalExpenses returns total expenses for a specific month
func (c *Calculator) CalculateTotalExpenses(month int) float64 {
	s := c.Settings
	years := month / 12
	phaseAge := s.GetPhaseReferenceAge(years) // Age used for spending phase calculations (may differ for couples)

	// Calculate living expenses based on spending model
	livingExpenses := calculateLivingExpensesAtMonth(s, month)

	// Calculate healthcare expenses using the settings helper (handles both legacy and multi-person)
	healthcareExpenses := s.GetTotalHealthcareCost(month)

	// Add expense sources (discretionary sources also get phase multiplier when enabled)
	for _, source := range s.ExpenseSources {
		expenseAmount := source.GetAdjustedAmount(month, s.InflationRate)
		// Apply phase multiplier to discretionary expenses
		if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled && source.Discretionary {
			expenseAmount *= s.GetSpendingMultiplier(phaseAge)
		}
		livingExpenses += expenseAmount
	}

	return livingExpenses + healthcareExpenses
}

type withdrawalBreakdown struct {
	RemainingNeed             float64
	ActualWithdrawal          float64
	RMDWithdrawal             float64
	WithdrawalFromTaxDeferred float64
	WithdrawalFromTaxable     float64
	WithdrawalFromRoth        float64
	EarlyPenaltyPaid          float64
}

type portfolioCashFlowResult struct {
	Shortfall                 float64
	ActualWithdrawal          float64
	RMDWithdrawal             float64
	WithdrawalFromTaxDeferred float64
	WithdrawalFromTaxable     float64
	WithdrawalFromRoth        float64
	TaxableRealizedGain       float64
}

func (r portfolioCashFlowResult) GrossWithdrawal() float64 {
	return r.WithdrawalFromTaxDeferred + r.WithdrawalFromTaxable + r.WithdrawalFromRoth
}

func projectionTimingGrowthFractions(timing models.ProjectionTiming) (before float64, after float64) {
	switch models.NormalizeProjectionTiming(timing) {
	case models.ProjectionTimingStartOfMonth:
		return 0, 1
	case models.ProjectionTimingMidMonth:
		return 0.5, 0.5
	default:
		return 1, 0
	}
}

func fractionalMonthlyReturn(monthlyReturn, fraction float64) float64 {
	switch {
	case fraction <= 0:
		return 0
	case fraction >= 1:
		return monthlyReturn
	default:
		return math.Pow(1+monthlyReturn, fraction) - 1
	}
}

type taxAwarePortfolioMonthResult struct {
	Shortfall                        float64
	TaxesPaid                        float64
	IRMAAExpense                     float64
	TotalGrowth                      float64
	TaxableIncomeBeforeCashFlow      float64
	TaxableQualifiedDividends        float64
	TaxableNonQualifiedDividends     float64
	TaxableCapitalGains              float64
	TaxableCapitalGainsDistributions float64
	TaxSnapshot                      projectedTaxSnapshot
	CashFlow                         portfolioCashFlowResult
}

func executeTaxAwarePortfolioMonth(
	totalExpenses float64,
	incomeBreakdown monthlyIncomeBreakdown,
	monthlyRMD float64,
	allowTaxDeferredWithdrawal bool,
	penaltyRate float64,
	taxDeferredBalance *float64,
	taxableAccount *taxableAccountState,
	rothBalance *float64,
	taxDeferredMonthlyReturn float64,
	rothMonthlyReturn float64,
	taxableComponents taxableReturnComponents,
	timing models.ProjectionTiming,
	taxState projectionTaxAccumulator,
	taxCalculator *TaxCalculator,
	currentYear int,
	monthInYear int,
	rothConversionThisMonth float64,
	completedMAGIHistory []float64,
	irmaaEligibleAdults int,
	irmaaInflationFactor float64,
) taxAwarePortfolioMonthResult {
	startingTaxDeferred := *taxDeferredBalance
	startingRoth := *rothBalance
	startingTaxable := *taxableAccount
	snapshot := taxState.estimateMonthlySnapshot(
		taxCalculator,
		currentYear,
		monthInYear,
		incomeBreakdown.OrdinaryIncome,
		incomeBreakdown.SocialSecurityIncome,
		0,
		0,
		0,
		0,
		rothConversionThisMonth,
		completedMAGIHistory,
		nil,
		irmaaEligibleAdults,
		irmaaInflationFactor,
	)
	taxesPaid := snapshot.MonthlyTax
	irmaaExpense := snapshot.MonthlyIRMAA
	finalSnapshot := snapshot
	result := taxAwarePortfolioMonthResult{}
	growthBeforeFraction, growthAfterFraction := projectionTimingGrowthFractions(timing)

	for iter := 0; iter < 6; iter++ {
		trialTaxDeferred := startingTaxDeferred
		trialRoth := startingRoth
		trialTaxable := startingTaxable

		tdBeforeGrowth := trialTaxDeferred * fractionalMonthlyReturn(taxDeferredMonthlyReturn, growthBeforeFraction)
		rothBeforeGrowth := trialRoth * fractionalMonthlyReturn(rothMonthlyReturn, growthBeforeFraction)
		trialTaxDeferred += tdBeforeGrowth
		trialRoth += rothBeforeGrowth
		beforeTaxableGrowth := trialTaxable.applyGrowth(taxableComponents, growthBeforeFraction)

		trialNeededFromPortfolio := totalExpenses + irmaaExpense + taxesPaid - incomeBreakdown.TotalIncome - beforeTaxableGrowth.QualifiedDividends - beforeTaxableGrowth.NonQualifiedDividends - beforeTaxableGrowth.CapitalGainsDistributions
		trialCashFlow := executePortfolioCashFlowWithTaxableState(trialNeededFromPortfolio, monthlyRMD, allowTaxDeferredWithdrawal, penaltyRate, &trialTaxDeferred, &trialTaxable, &trialRoth)

		tdAfterGrowth := trialTaxDeferred * fractionalMonthlyReturn(taxDeferredMonthlyReturn, growthAfterFraction)
		rothAfterGrowth := trialRoth * fractionalMonthlyReturn(rothMonthlyReturn, growthAfterFraction)
		trialTaxDeferred += tdAfterGrowth
		trialRoth += rothAfterGrowth
		afterTaxableGrowth := trialTaxable.applyGrowth(taxableComponents, growthAfterFraction)
		trialTaxable.addCash(afterTaxableGrowth.QualifiedDividends + afterTaxableGrowth.NonQualifiedDividends + afterTaxableGrowth.CapitalGainsDistributions)

		trialQualifiedDividends := beforeTaxableGrowth.QualifiedDividends + afterTaxableGrowth.QualifiedDividends
		trialNonQualifiedDividends := beforeTaxableGrowth.NonQualifiedDividends + afterTaxableGrowth.NonQualifiedDividends
		trialCapitalGains := beforeTaxableGrowth.CapitalGainsDistributions + afterTaxableGrowth.CapitalGainsDistributions + trialCashFlow.TaxableRealizedGain

		recalculatedSnapshot := taxState.estimateMonthlySnapshot(
			taxCalculator,
			currentYear,
			monthInYear,
			incomeBreakdown.OrdinaryIncome+trialNonQualifiedDividends,
			incomeBreakdown.SocialSecurityIncome,
			trialCashFlow.WithdrawalFromTaxDeferred,
			trialQualifiedDividends,
			trialCapitalGains,
			trialNonQualifiedDividends,
			rothConversionThisMonth,
			completedMAGIHistory,
			nil,
			irmaaEligibleAdults,
			irmaaInflationFactor,
		)

		*taxDeferredBalance = trialTaxDeferred
		*rothBalance = trialRoth
		*taxableAccount = trialTaxable
		result.CashFlow = trialCashFlow
		result.Shortfall = trialCashFlow.Shortfall
		result.TotalGrowth = tdBeforeGrowth + rothBeforeGrowth + beforeTaxableGrowth.TotalGrowth + tdAfterGrowth + rothAfterGrowth + afterTaxableGrowth.TotalGrowth
		result.TaxableIncomeBeforeCashFlow = beforeTaxableGrowth.QualifiedDividends + beforeTaxableGrowth.NonQualifiedDividends + beforeTaxableGrowth.CapitalGainsDistributions
		result.TaxableQualifiedDividends = trialQualifiedDividends
		result.TaxableNonQualifiedDividends = trialNonQualifiedDividends
		result.TaxableCapitalGains = trialCapitalGains
		result.TaxableCapitalGainsDistributions = beforeTaxableGrowth.CapitalGainsDistributions + afterTaxableGrowth.CapitalGainsDistributions
		finalSnapshot = recalculatedSnapshot

		if math.Abs(recalculatedSnapshot.MonthlyTax-taxesPaid) < 0.01 && math.Abs(recalculatedSnapshot.MonthlyIRMAA-irmaaExpense) < 0.01 {
			taxesPaid = recalculatedSnapshot.MonthlyTax
			irmaaExpense = recalculatedSnapshot.MonthlyIRMAA
			break
		}

		taxesPaid = recalculatedSnapshot.MonthlyTax
		irmaaExpense = recalculatedSnapshot.MonthlyIRMAA
	}

	result.TaxesPaid = taxesPaid
	result.IRMAAExpense = irmaaExpense
	result.TaxSnapshot = finalSnapshot
	return result
}

func reinvestRequiredRMDToTaxableState(monthlyRMD float64, taxDeferredBalance *float64, taxable *taxableAccountState) float64 {
	if monthlyRMD <= 0 || *taxDeferredBalance <= 0 {
		return 0
	}

	rmdWithdrawal := math.Min(monthlyRMD, *taxDeferredBalance)
	*taxDeferredBalance -= rmdWithdrawal
	taxable.addCash(rmdWithdrawal)
	return rmdWithdrawal
}

func applyBigTicketExpenseWithTaxableState(amount float64, allowTaxDeferred bool, earlyPenaltyRate float64, taxDeferredBalance *float64, taxable *taxableAccountState, rothBalance *float64) float64 {
	remaining := amount

	if remaining > 0 && taxable.MarketValue > 0 {
		fromTaxable, _, _ := taxable.withdraw(remaining)
		remaining -= fromTaxable
	}

	if remaining > 0 && *rothBalance > 0 {
		fromRoth := math.Min(remaining, *rothBalance)
		*rothBalance -= fromRoth
		remaining -= fromRoth
	}

	if allowTaxDeferred && remaining > 0 && *taxDeferredBalance > 0 {
		effectiveFactor := 1.0 - earlyPenaltyRate
		grossNeeded := remaining / effectiveFactor
		fromTaxDeferred := math.Min(grossNeeded, *taxDeferredBalance)
		*taxDeferredBalance -= fromTaxDeferred
		remaining -= fromTaxDeferred * effectiveFactor
	}

	return remaining
}

func executePortfolioCashFlowWithTaxableState(neededFromPortfolio, monthlyRMD float64, allowTaxDeferred bool, earlyPenaltyRate float64, taxDeferredBalance *float64, taxable *taxableAccountState, rothBalance *float64) portfolioCashFlowResult {
	result := portfolioCashFlowResult{}

	if neededFromPortfolio > 0 {
		withdrawal := withdrawForExpenses(neededFromPortfolio, monthlyRMD, allowTaxDeferred, earlyPenaltyRate, taxDeferredBalance, &taxable.MarketValue, rothBalance)
		result.Shortfall = withdrawal.RemainingNeed
		result.ActualWithdrawal = withdrawal.ActualWithdrawal
		result.RMDWithdrawal = withdrawal.RMDWithdrawal
		result.WithdrawalFromTaxDeferred = withdrawal.WithdrawalFromTaxDeferred
		result.WithdrawalFromRoth = withdrawal.WithdrawalFromRoth

		if withdrawal.WithdrawalFromTaxable > 0 {
			// Undo the legacy taxable withdrawal, then re-apply it with basis tracking.
			taxable.MarketValue += withdrawal.WithdrawalFromTaxable
			cash, _, realizedGain := taxable.withdraw(withdrawal.WithdrawalFromTaxable)
			result.WithdrawalFromTaxable = cash
			result.TaxableRealizedGain += math.Max(0, realizedGain)
		}

		unmetRMD := monthlyRMD - withdrawal.RMDWithdrawal
		if unmetRMD > 0 {
			reinvested := reinvestRequiredRMDToTaxableState(unmetRMD, taxDeferredBalance, taxable)
			result.RMDWithdrawal += reinvested
			result.WithdrawalFromTaxDeferred += reinvested
		}
	} else {
		if neededFromPortfolio < 0 {
			taxable.addCash(math.Abs(neededFromPortfolio))
		}
		reinvested := reinvestRequiredRMDToTaxableState(monthlyRMD, taxDeferredBalance, taxable)
		result.RMDWithdrawal = reinvested
		result.WithdrawalFromTaxDeferred += reinvested
	}

	return result
}

func taxDeferredDelayActive(s *models.WhatIfSettings, currentYear int) bool {
	return s.TaxDeferredDelayYears > 0 && currentYear < s.TaxDeferredDelayYears
}

func shortfallIsTemporaryDueToDelay(shortfall float64, allowTaxDeferredWithdrawal bool, taxDeferredBalance float64) bool {
	return shortfall > 0 && !allowTaxDeferredWithdrawal && taxDeferredBalance > 0
}

func shortfallCausesDepletion(shortfall float64, allowTaxDeferredWithdrawal bool, taxDeferredBalance float64) bool {
	return shortfall > 0 && !shortfallIsTemporaryDueToDelay(shortfall, allowTaxDeferredWithdrawal, taxDeferredBalance)
}

// earlyWithdrawalPenaltyRate returns the IRS 10% early distribution penalty rate
// for tax-deferred withdrawals before age 59½. Uses age 60 as the cutoff since
// the model operates in whole years.
func earlyWithdrawalPenaltyRate(currentAge, currentYear int) float64 {
	if currentAge+currentYear < 60 {
		return 0.10
	}
	return 0
}

func withdrawForExpenses(neededFromPortfolio, monthlyRMD float64, allowTaxDeferred bool, earlyPenaltyRate float64, taxDeferredBalance, taxableBalance, rothBalance *float64) withdrawalBreakdown {
	breakdown := withdrawalBreakdown{RemainingNeed: neededFromPortfolio}
	if neededFromPortfolio <= 0 {
		return breakdown
	}

	if monthlyRMD > 0 && *taxDeferredBalance > 0 {
		rmdUsed := math.Min(monthlyRMD, breakdown.RemainingNeed)
		rmdUsed = math.Min(rmdUsed, *taxDeferredBalance)
		*taxDeferredBalance -= rmdUsed
		breakdown.RemainingNeed -= rmdUsed
		breakdown.RMDWithdrawal = rmdUsed
		breakdown.WithdrawalFromTaxDeferred += rmdUsed
		breakdown.ActualWithdrawal += rmdUsed
	}

	if breakdown.RemainingNeed > 0 && *taxableBalance > 0 {
		fromTaxable := math.Min(breakdown.RemainingNeed, *taxableBalance)
		*taxableBalance -= fromTaxable
		breakdown.RemainingNeed -= fromTaxable
		breakdown.WithdrawalFromTaxable += fromTaxable
		breakdown.ActualWithdrawal += fromTaxable
	}

	if breakdown.RemainingNeed > 0 && *rothBalance > 0 {
		fromRoth := math.Min(breakdown.RemainingNeed, *rothBalance)
		*rothBalance -= fromRoth
		breakdown.RemainingNeed -= fromRoth
		breakdown.WithdrawalFromRoth += fromRoth
		breakdown.ActualWithdrawal += fromRoth
	}

	if allowTaxDeferred && breakdown.RemainingNeed > 0 && *taxDeferredBalance > 0 {
		// Early withdrawal penalty: before age 59½, only (1-penalty) of each dollar
		// withdrawn from tax-deferred is available for spending
		effectiveFactor := 1.0 - earlyPenaltyRate
		grossNeeded := breakdown.RemainingNeed / effectiveFactor
		fromTaxDeferred := math.Min(grossNeeded, *taxDeferredBalance)
		*taxDeferredBalance -= fromTaxDeferred
		netSpending := fromTaxDeferred * effectiveFactor
		penalty := fromTaxDeferred - netSpending
		breakdown.RemainingNeed -= netSpending
		breakdown.WithdrawalFromTaxDeferred += fromTaxDeferred
		breakdown.ActualWithdrawal += fromTaxDeferred
		breakdown.EarlyPenaltyPaid += penalty
	}

	return breakdown
}

func applyBigTicketExpense(amount float64, allowTaxDeferred bool, earlyPenaltyRate float64, taxDeferredBalance, taxableBalance, rothBalance *float64) float64 {
	remaining := amount

	if remaining > 0 && *taxableBalance > 0 {
		fromTaxable := math.Min(remaining, *taxableBalance)
		*taxableBalance -= fromTaxable
		remaining -= fromTaxable
	}

	if remaining > 0 && *rothBalance > 0 {
		fromRoth := math.Min(remaining, *rothBalance)
		*rothBalance -= fromRoth
		remaining -= fromRoth
	}

	if allowTaxDeferred && remaining > 0 && *taxDeferredBalance > 0 {
		effectiveFactor := 1.0 - earlyPenaltyRate
		grossNeeded := remaining / effectiveFactor
		fromTaxDeferred := math.Min(grossNeeded, *taxDeferredBalance)
		*taxDeferredBalance -= fromTaxDeferred
		remaining -= fromTaxDeferred * effectiveFactor
	}

	return remaining
}

// ExpenseBreakdown holds categorized expenses for adaptive spending analysis
type ExpenseBreakdown struct {
	Essential     float64 // Non-discretionary expenses (cannot be reduced)
	Discretionary float64 // Discretionary expenses (can be reduced during downturns)
	Total         float64 // Total = Essential + Discretionary
}

// CalculateExpenseBreakdown separates expenses into discretionary and essential
func (c *Calculator) CalculateExpenseBreakdown(month int) ExpenseBreakdown {
	s := c.Settings
	years := month / 12
	phaseAge := s.GetPhaseReferenceAge(years) // Age used for spending phase calculations (may differ for couples)

	// Base living expenses are treated as essential (conservative approach)
	livingExpenses := calculateLivingExpensesAtMonth(s, month)

	// Healthcare is always essential
	healthcareExpenses := s.GetTotalHealthcareCost(month)

	essential := livingExpenses + healthcareExpenses
	discretionary := 0.0

	// Categorize expense sources
	for _, source := range s.ExpenseSources {
		amount := source.GetAdjustedAmount(month, s.InflationRate)
		// Apply phase multiplier to discretionary expenses
		if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled && source.Discretionary {
			amount *= s.GetSpendingMultiplier(phaseAge)
		}
		if source.Discretionary {
			discretionary += amount
		} else {
			essential += amount
		}
	}

	return ExpenseBreakdown{
		Essential:     essential,
		Discretionary: discretionary,
		Total:         essential + discretionary,
	}
}

// RunProjection runs a full retirement projection with RMD integration
func (c *Calculator) RunProjection() *models.ProjectionResult {
	primarySettings := c.Settings
	activeSettings := c.Settings
	nextChainIdx := 0
	s := activeSettings
	months := s.ProjectionYears * 12
	projection := make([]models.ProjectionMonth, 0, months)

	// Split portfolio into tax-deferred, Roth, and taxable portions
	taxDeferredBalance := s.PortfolioValue * (s.TaxDeferredPercent / 100)
	rothBalance := s.PortfolioValue * (s.RothPercent / 100)
	taxableAccount := newTaxableAccountState(s, s.PortfolioValue-taxDeferredBalance-rothBalance)

	var depletionMonth *int
	var longevityYears *float64

	currentLivingExpenses := calculateLivingExpensesAtMonth(s, 0)

	// Track cumulative inflation for spending phase calculations
	cumulativeInflation := 1.0

	// Track annual RMD (calculated once per year, distributed monthly)
	var annualRMD float64
	var monthlyRMD float64
	var taxState projectionTaxAccumulator
	taxCalculator := NewTaxCalculator(s.TaxConfig, s.InflationRate)
	completedMAGIHistory := make([]float64, 0, s.ProjectionYears)
	currentYearTaxSnapshot := projectedTaxSnapshot{}
	yearlySummaries := make([]models.ProjectionYearSummary, 0, s.ProjectionYears)
	currentYearSummary := models.ProjectionYearSummary{
		Year:            0,
		StartingBalance: s.PortfolioValue,
	}

	finalizeCurrentYear := func(month models.ProjectionMonth) {
		currentYearSummary.MAGI = currentYearTaxSnapshot.AnnualMAGI
		currentYearSummary.NIIT = currentYearTaxSnapshot.AnnualNIIT
		currentYearSummary.IRMAA = currentYearTaxSnapshot.AnnualIRMAA
		currentYearSummary.TaxableSocialSecurityPct = currentYearTaxSnapshot.TaxableSocialSecurityPct
		currentYearSummary.EndingBalance = month.PortfolioBalance
		currentYearSummary.EndingBalanceReal = month.PortfolioBalanceReal
		currentYearSummary.CumulativeInflation = month.CumulativeInflation
		yearlySummaries = append(yearlySummaries, currentYearSummary)
	}

	for m := 0; m < months; m++ {
		currentYear := m / 12
		monthInYear := m % 12
		phaseAge := s.GetPhaseReferenceAge(currentYear) // Age used for spending phase calculations (may differ for couples)
		// RMD uses OLDER person's age - whoever hits 73 first triggers RMD
		olderAge := s.GetOlderAge() + currentYear
		bigTicketExpenseThisMonth := 0.0
		rothConversionThisMonth := 0.0
		allowTaxDeferredWithdrawal := !taxDeferredDelayActive(s, currentYear)
		penaltyRate := earlyWithdrawalPenaltyRate(s.CurrentAge, currentYear)

		// Annual adjustments at year boundaries
		if m%12 == 0 {
			if m > 0 && len(projection) > 0 {
				completedMAGIHistory = append(completedMAGIHistory, currentYearTaxSnapshot.AnnualMAGI)
				finalizeCurrentYear(projection[len(projection)-1])
				currentYearSummary = models.ProjectionYearSummary{
					Year:            currentYear,
					StartingBalance: projection[len(projection)-1].PortfolioBalance,
				}
			}

			taxState = projectionTaxAccumulator{}
			// Check for chain transition
			if len(c.ResolvedChain) > 0 {
				newIdx, prepared := c.nextChainTransition(currentYear, nextChainIdx, primarySettings)
				if prepared != nil {
					activeSettings = prepared
					s = activeSettings
					nextChainIdx = newIdx

					currentLivingExpenses = rebaseLivingExpensesAtTransition(s, phaseAge, cumulativeInflation)
					taxableAccount.syncAssumptions(s)
				}
			}
			taxCalculator = NewTaxCalculator(s.TaxConfig, s.InflationRate)
			taxableAccount.RealizedGainsYTD = 0

			// Calculate annual RMD at start of each year (age 73+)
			if olderAge >= RMDStartAge && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, olderAge)
				monthlyRMD = annualRMD / 12
			} else {
				annualRMD = 0
				monthlyRMD = 0
			}

			// Process Roth conversions (annual, at year boundary)
			if conversionAmount := rothConversionAmountForYear(s, currentYear, taxDeferredBalance); conversionAmount > 0 {
				taxDeferredBalance -= conversionAmount
				rothBalance += conversionAmount
				rothConversionThisMonth = conversionAmount
			}

			// Process big ticket items for this year
			for _, item := range s.BigTicketItems {
				if item.Year == currentYear {
					if item.Type == models.BigTicketIncome {
						// Income adds to taxable balance (e.g., inheritance, home sale)
						taxableAccount.addCash(item.Amount)
					} else {
						remaining := applyBigTicketExpenseWithTaxableState(item.Amount, allowTaxDeferredWithdrawal, penaltyRate, &taxDeferredBalance, &taxableAccount, &rothBalance)
						bigTicketExpenseThisMonth += remaining
					}
				}
			}
		}

		if m > 0 {
			cumulativeInflation *= monthlyCompoundFactorFromPercent(s.InflationRate)
			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
				currentLivingExpenses = s.MonthlyLivingExpenses * s.GetSpendingMultiplier(phaseAge) * cumulativeInflation
			} else {
				currentLivingExpenses *= monthlyCompoundFactorFromPercent(s.InflationRate - s.SpendingDeclineRate)
			}
		}

		// Calculate healthcare expenses using multi-person model
		activeHealthcare := s.GetTotalHealthcareCost(m)
		totalExpenses := currentLivingExpenses + activeHealthcare + bigTicketExpenseThisMonth

		// Add expense sources (discretionary sources get phase multiplier when enabled)
		for _, source := range s.ExpenseSources {
			expenseAmount := source.GetAdjustedAmount(m, s.InflationRate)
			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled && source.Discretionary {
				expenseAmount *= s.GetSpendingMultiplier(phaseAge)
			}
			totalExpenses += expenseAmount
		}

		// Calculate income using the active settings for this month.
		incomeBreakdown := calculateMonthlyIncomeBreakdown(s, m)
		totalIncome := incomeBreakdown.TotalIncome

		// Apply investment growth to each account based on its asset allocation
		// If InvestmentReturn is explicitly set (non-zero), use it as override for all accounts
		var taxDeferredReturn, rothReturn, taxableReturn float64
		if s.InvestmentReturn != 0 {
			// Override mode: use single rate for all accounts
			taxDeferredReturn = s.InvestmentReturn
			rothReturn = s.InvestmentReturn
			taxableReturn = s.InvestmentReturn
		} else {
			// Per-account allocation mode: calculate blended returns from conservative estimates
			// Using forward-looking estimates (7% stocks, 4% bonds, 3% cash) rather than
			// historical averages (~10.5% stocks) for more prudent retirement planning
			stockMean, bondMean, cashMean := 7.0, 4.0, 3.0
			tdStock, tdBond, tdCash := s.GetTaxDeferredAllocation()
			rothStock, rothBond, rothCash := s.GetRothAllocation()
			taxStock, taxBond, taxCash := s.GetTaxableAllocation()
			taxDeferredReturn = models.GetBlendedReturn(tdStock, tdBond, tdCash, stockMean, bondMean, cashMean)
			rothReturn = models.GetBlendedReturn(rothStock, rothBond, rothCash, stockMean, bondMean, cashMean)
			taxableReturn = models.GetBlendedReturn(taxStock, taxBond, taxCash, stockMean, bondMean, cashMean)
		}

		// Convert annual returns to monthly using geometric formula (not simple division)
		// Simple division inflates effective returns when compounded monthly
		taxDeferredMonthly := math.Pow(1+taxDeferredReturn/100, 1.0/12) - 1
		rothMonthly := math.Pow(1+rothReturn/100, 1.0/12) - 1
		taxableComponents := buildTaxableReturnComponents(taxableReturn, s)
		totalGrowth := 0.0
		irmaaEligibleAdults := medicareEligibleAdultCountAtYear(s, currentYear)
		irmaaInflationFactor := plannerInflationFactorForYear(s.InflationRate, float64(currentYear))

		monthResult := executeTaxAwarePortfolioMonth(
			totalExpenses,
			incomeBreakdown,
			monthlyRMD,
			allowTaxDeferredWithdrawal,
			penaltyRate,
			&taxDeferredBalance,
			&taxableAccount,
			&rothBalance,
			taxDeferredMonthly,
			rothMonthly,
			taxableComponents,
			s.GetProjectionTiming(),
			taxState,
			taxCalculator,
			currentYear,
			monthInYear,
			rothConversionThisMonth,
			completedMAGIHistory,
			irmaaEligibleAdults,
			irmaaInflationFactor,
		)
		totalGrowth = monthResult.TotalGrowth
		shortfall := monthResult.Shortfall
		cashFlow := monthResult.CashFlow
		taxesPaid := monthResult.TaxesPaid
		totalExpenses += monthResult.IRMAAExpense
		currentYearTaxSnapshot = monthResult.TaxSnapshot

		taxState.applyMonth(
			incomeBreakdown.OrdinaryIncome+monthResult.TaxableNonQualifiedDividends,
			incomeBreakdown.SocialSecurityIncome,
			cashFlow.WithdrawalFromTaxDeferred,
			monthResult.TaxableQualifiedDividends,
			monthResult.TaxableCapitalGains,
			monthResult.TaxableNonQualifiedDividends,
			rothConversionThisMonth,
			taxesPaid,
		)

		grossIncome := totalIncome + monthResult.TaxableQualifiedDividends + monthResult.TaxableNonQualifiedDividends + monthResult.TaxableCapitalGainsDistributions + cashFlow.WithdrawalFromTaxDeferred + cashFlow.WithdrawalFromTaxable + cashFlow.WithdrawalFromRoth
		netIncome := grossIncome - taxesPaid
		currentYearSummary.Growth += totalGrowth
		currentYearSummary.GrossIncome += grossIncome
		currentYearSummary.Taxes += taxesPaid
		currentYearSummary.Expenses += totalExpenses
		currentYearSummary.Withdrawals += cashFlow.ActualWithdrawal

		totalBalance := taxDeferredBalance + rothBalance + taxableAccount.MarketValue
		depleted := false
		if totalBalance <= 0 {
			taxDeferredBalance = 0
			rothBalance = 0
			taxableAccount = newTaxableAccountState(s, 0)
			totalBalance = 0
			depleted = true
			if depletionMonth == nil {
				dm := m
				depletionMonth = &dm
				ly := float64(m) / 12
				longevityYears = &ly
			}
		} else if shortfallIsTemporaryDueToDelay(shortfall, allowTaxDeferredWithdrawal, taxDeferredBalance) {
			// Temporary shortfall (e.g., accessible accounts empty but locked accounts
			// still have funds during a tax-deferred delay). Not true depletion — the
			// portfolio has money, it's just not accessible this month. Don't stop.
		} else if shortfallCausesDepletion(shortfall, allowTaxDeferredWithdrawal, taxDeferredBalance) {
			depleted = true
			if depletionMonth == nil {
				dm := m
				depletionMonth = &dm
				ly := float64(m) / 12
				longevityYears = &ly
			}
		}

		projection = append(projection, models.ProjectionMonth{
			Month:                     m,
			Year:                      float64(m) / 12,
			CumulativeInflation:       cumulativeInflation,
			PortfolioBalance:          totalBalance,
			PortfolioBalanceReal:      totalBalance / cumulativeInflation,
			TaxDeferredBalance:        taxDeferredBalance,
			TaxableBalance:            taxableAccount.MarketValue,
			RothBalance:               rothBalance,
			GeneralExpenses:           currentLivingExpenses,
			HealthcareExpense:         activeHealthcare,
			TotalExpenses:             totalExpenses,
			TotalExpensesReal:         totalExpenses / cumulativeInflation,
			TotalIncome:               totalIncome + monthResult.TaxableIncomeBeforeCashFlow,
			TotalIncomeReal:           (totalIncome + monthResult.TaxableIncomeBeforeCashFlow) / cumulativeInflation,
			GrossIncome:               grossIncome,
			NetIncome:                 netIncome,
			TaxesPaid:                 taxesPaid,
			NetWithdrawal:             cashFlow.ActualWithdrawal,
			RMDWithdrawal:             cashFlow.RMDWithdrawal,
			TaxableWithdrawals:        cashFlow.WithdrawalFromTaxDeferred,
			RothConversions:           rothConversionThisMonth,
			PortfolioGrowth:           totalGrowth,
			Depleted:                  depleted,
			WithdrawalFromTaxDeferred: cashFlow.WithdrawalFromTaxDeferred,
			WithdrawalFromTaxable:     cashFlow.WithdrawalFromTaxable,
			WithdrawalFromRoth:        cashFlow.WithdrawalFromRoth,
		})

		if depleted {
			break
		}
	}

	finalBalance := 0.0
	if len(projection) > 0 {
		finalBalance = projection[len(projection)-1].PortfolioBalance
		finalizeCurrentYear(projection[len(projection)-1])
	}

	return &models.ProjectionResult{
		Months:          projection,
		YearlySummaries: yearlySummaries,
		LongevityYears:  longevityYears,
		FinalBalance:    finalBalance,
		DepletionMonth:  depletionMonth,
		Survives:        depletionMonth == nil,
	}
}

// CalculateBudgetFit analyzes monthly budget gap
func (c *Calculator) CalculateBudgetFit() *models.BudgetFitAnalysis {
	s := c.Settings

	// Calculate first month expenses and income
	baseMonthlyExpenses := c.CalculateTotalExpenses(0)
	incomeSummary := calculateMonthlyIncomeBreakdown(s, 0)
	monthlyIncome := incomeSummary.TotalIncome

	// Build expense breakdown for transparency
	var breakdown []models.ExpenseBreakdownItem
	if s.MonthlyLivingExpenses > 0 {
		breakdown = append(breakdown, models.ExpenseBreakdownItem{
			Name:   "Living Expenses",
			Amount: s.MonthlyLivingExpenses,
		})
	}
	healthcareCost := s.GetTotalHealthcareCost(0)
	if healthcareCost > 0 {
		breakdown = append(breakdown, models.ExpenseBreakdownItem{
			Name:   "Healthcare",
			Amount: healthcareCost,
		})
	} else if len(s.HealthcarePersons) > 0 {
		// Show healthcare even when $0 so user knows it's tracked
		note := "employer covered"
		for _, p := range s.HealthcarePersons {
			if p.EmployerCoverageYears > 0 {
				note = fmt.Sprintf("employer covered (%d yr)", p.EmployerCoverageYears)
				break
			}
		}
		breakdown = append(breakdown, models.ExpenseBreakdownItem{
			Name:   "Healthcare",
			Amount: 0,
			Note:   note,
		})
	}
	for _, source := range s.ExpenseSources {
		amt := source.GetAdjustedAmount(0, s.InflationRate)
		note := ""
		if source.EndYear > 0 {
			note = fmt.Sprintf("ends year %d", source.EndYear)
		}
		breakdown = append(breakdown, models.ExpenseBreakdownItem{
			Name:   source.Name,
			Amount: amt,
			Note:   note,
		})
	}

	// Build income breakdown
	var incomeItems []models.ExpenseBreakdownItem
	for _, source := range s.IncomeSources {
		amt := source.GetAdjustedAmount(0)
		if amt > 0 {
			note := ""
			if source.StartMonth > 0 {
				note = fmt.Sprintf("starts year %d", source.StartMonth/12)
			}
			incomeItems = append(incomeItems, models.ExpenseBreakdownItem{
				Name:   source.Name,
				Amount: amt,
				Note:   note,
			})
		}
	}

	taxableAnnualReturn := s.InvestmentReturn
	if taxableAnnualReturn == 0 {
		stockMean, bondMean, cashMean := 7.0, 4.0, 3.0
		taxStock, taxBond, taxCash := s.GetTaxableAllocation()
		taxableAnnualReturn = models.GetBlendedReturn(taxStock, taxBond, taxCash, stockMean, bondMean, cashMean)
	}
	taxableMarketValue := s.PortfolioValue * math.Max(0, (100-s.TaxDeferredPercent-s.RothPercent)/100)
	taxableCashFlow := expectedTaxableMonthlyCashFlow(s, taxableMarketValue, taxableAnnualReturn)
	taxableIncome := taxableCashFlow.QualifiedDividends + taxableCashFlow.NonQualifiedDividends + taxableCashFlow.CapitalGainsDistributions
	if taxableIncome > 0 {
		monthlyIncome += taxableIncome
		incomeItems = append(incomeItems, models.ExpenseBreakdownItem{
			Name:   "Taxable Distributions",
			Amount: taxableIncome,
			Note:   "assumed monthly dividends/gain distributions",
		})
	}

	// Calculate RMD if age 73+ and have tax-deferred balance
	// Uses older person's age - whoever hits 73 first triggers RMD
	monthlyRMD := 0.0
	olderAge := s.GetOlderAge()
	if olderAge >= RMDStartAge && s.TaxDeferredPercent > 0 {
		taxDeferredBalance := s.PortfolioValue * (s.TaxDeferredPercent / 100)
		annualRMD, _ := CalculateRMD(taxDeferredBalance, olderAge)
		monthlyRMD = annualRMD / 12
	}

	rothConversionThisMonth := rothConversionAmountForYear(s, 0, s.PortfolioValue*(s.TaxDeferredPercent/100))
	taxState := projectionTaxAccumulator{}
	taxCalculator := NewTaxCalculator(s.TaxConfig, s.InflationRate)
	irmaaEligibleAdults := medicareEligibleAdultCountAtYear(s, 0)
	currentSnapshotBeforeRMD := taxState.estimateMonthlySnapshot(
		taxCalculator,
		0,
		0,
		incomeSummary.OrdinaryIncome+taxableCashFlow.NonQualifiedDividends,
		incomeSummary.SocialSecurityIncome,
		0,
		taxableCashFlow.QualifiedDividends,
		taxableCashFlow.CapitalGainsDistributions,
		taxableCashFlow.NonQualifiedDividends,
		rothConversionThisMonth,
		nil,
		nil,
		irmaaEligibleAdults,
		1,
	)
	currentSnapshot := taxState.estimateMonthlySnapshot(
		taxCalculator,
		0,
		0,
		incomeSummary.OrdinaryIncome+taxableCashFlow.NonQualifiedDividends,
		incomeSummary.SocialSecurityIncome,
		monthlyRMD,
		taxableCashFlow.QualifiedDividends,
		taxableCashFlow.CapitalGainsDistributions,
		taxableCashFlow.NonQualifiedDividends,
		rothConversionThisMonth,
		nil,
		nil,
		irmaaEligibleAdults,
		1,
	)
	monthlyTaxesBeforeRMD := currentSnapshotBeforeRMD.MonthlyTax
	monthlyTaxes := currentSnapshot.MonthlyTax
	monthlyExpenses := baseMonthlyExpenses + currentSnapshot.MonthlyIRMAA
	grossIncome := monthlyIncome + monthlyRMD
	netIncome := grossIncome - monthlyTaxes
	netRMD := math.Max(0, monthlyRMD-(monthlyTaxes-monthlyTaxesBeforeRMD))

	// Calculate gap before RMD (what's the shortfall from income alone?)
	gapBeforeRMD := baseMonthlyExpenses + currentSnapshotBeforeRMD.MonthlyIRMAA - monthlyIncome + monthlyTaxesBeforeRMD

	// Calculate how RMD affects the gap
	var rmdCoverage, excessRMD float64
	if netRMD > 0 {
		if gapBeforeRMD > 0 {
			// There's a shortfall - RMD can help cover it
			if netRMD >= gapBeforeRMD {
				// RMD fully covers the gap and then some
				rmdCoverage = gapBeforeRMD
				excessRMD = netRMD - gapBeforeRMD
			} else {
				// RMD partially covers the gap
				rmdCoverage = netRMD
				excessRMD = 0
			}
		} else {
			// No shortfall (income covers expenses) - all RMD is excess
			rmdCoverage = 0
			excessRMD = netRMD
		}
	}

	// Gap = Expenses - after-tax cash flow (income + RMD - taxes)
	monthlyGap := monthlyExpenses - netIncome
	annualGap := monthlyGap * 12

	// Calculate required withdrawal rate (only for positive gap after RMD)
	requiredRate := 0.0
	if s.PortfolioValue > 0 && monthlyGap > 0 {
		requiredRate = (annualGap / s.PortfolioValue) * 100
	}

	result := &models.BudgetFitAnalysis{
		MonthlyExpenses:          monthlyExpenses,
		MonthlyIncome:            monthlyIncome,
		GrossIncome:              grossIncome,
		NetIncome:                netIncome,
		MonthlyTaxes:             monthlyTaxes,
		MonthlyNIIT:              currentSnapshot.AnnualNIIT / 12,
		MonthlyIRMAA:             currentSnapshot.MonthlyIRMAA,
		TaxableSocialSecurityPct: currentSnapshot.TaxableSocialSecurityPct,
		MonthlyRMD:               monthlyRMD,
		MonthlyGap:               monthlyGap,
		AnnualGap:                annualGap,
		RequiredRate:             requiredRate,
		ExpenseBreakdown:         breakdown,
		IncomeBreakdown:          incomeItems,
		GapBeforeRMD:             gapBeforeRMD,
		RMDCoverage:              rmdCoverage,
		ExcessRMD:                excessRMD,
	}
	if currentSnapshot.MonthlyIRMAA > 0 {
		result.ExpenseBreakdown = append(result.ExpenseBreakdown, models.ExpenseBreakdownItem{
			Name:   "IRMAA Surcharge",
			Amount: currentSnapshot.MonthlyIRMAA,
			Note:   "estimated per Medicare-eligible adult",
		})
	}

	// Calculate steady-state analysis (when all income sources are active)
	minSteadyStateMonth := c.findSteadyStateMonth()
	minSteadyStateYear := float64(minSteadyStateMonth) / 12

	// Use override year if set and >= minimum, otherwise use auto-calculated
	steadyStateYear := minSteadyStateYear
	if s.SteadyStateOverrideYear > 0 && s.SteadyStateOverrideYear >= minSteadyStateYear {
		steadyStateYear = s.SteadyStateOverrideYear
	}
	steadyStateMonth := int(steadyStateYear * 12)

	// Always show steady state section (allows sliding forward even if no delayed income)
	result.HasSteadyState = true
	result.MinSteadyStateYear = minSteadyStateYear
	result.SteadyStateMonth = steadyStateMonth
	result.SteadyStateYear = steadyStateYear

	if steadyStateMonth > 0 {
		// Calculate expenses and income at steady state
		baseSteadyStateExpenses := c.CalculateTotalExpenses(steadyStateMonth)
		result.SteadyStateExpenses = baseSteadyStateExpenses
		steadyStateIncomeBreakdown := calculateMonthlyIncomeBreakdown(s, steadyStateMonth)
		result.SteadyStateIncome = steadyStateIncomeBreakdown.TotalIncome

		// Determine effective annual return (allocation-derived when InvestmentReturn is 0)
		effectiveReturn := s.InvestmentReturn
		if effectiveReturn == 0 {
			effectiveReturn = s.GetExpectedReturnFromAllocation()
		}
		yearsToSteadyState := float64(steadyStateMonth) / 12
		steadyStateTaxableBalance := taxableMarketValue * math.Pow(1+taxableAnnualReturn/100, yearsToSteadyState)
		steadyStateTaxableCashFlow := expectedTaxableMonthlyCashFlow(s, steadyStateTaxableBalance, taxableAnnualReturn)
		result.SteadyStateIncome += steadyStateTaxableCashFlow.QualifiedDividends + steadyStateTaxableCashFlow.NonQualifiedDividends + steadyStateTaxableCashFlow.CapitalGainsDistributions

		// Calculate RMD at steady state age (uses older person's age)
		steadyStateOlderAge := s.GetOlderAge() + (steadyStateMonth / 12)
		estimatedTaxDeferred := 0.0
		if steadyStateOlderAge >= RMDStartAge && s.TaxDeferredPercent > 0 {
			// Estimate tax-deferred balance at steady state (simplified: assume growth only)
			estimatedTaxDeferred = s.PortfolioValue * (s.TaxDeferredPercent / 100) *
				math.Pow(1+effectiveReturn/100, yearsToSteadyState)
			annualRMD, _ := CalculateRMD(estimatedTaxDeferred, steadyStateOlderAge)
			result.SteadyStateRMD = annualRMD / 12
		}

		steadyStateRothConversion := rothConversionAmountForYear(s, steadyStateMonth/12, estimatedTaxDeferred)
		steadyStateTaxState := projectionTaxAccumulator{}
		steadyStateIRMALookbackMAGI := (*float64)(nil)
		steadyStateSnapshot := steadyStateTaxState.estimateMonthlySnapshot(
			taxCalculator,
			steadyStateMonth/12,
			steadyStateMonth%12,
			steadyStateIncomeBreakdown.OrdinaryIncome+steadyStateTaxableCashFlow.NonQualifiedDividends,
			steadyStateIncomeBreakdown.SocialSecurityIncome,
			result.SteadyStateRMD,
			steadyStateTaxableCashFlow.QualifiedDividends,
			steadyStateTaxableCashFlow.CapitalGainsDistributions,
			steadyStateTaxableCashFlow.NonQualifiedDividends,
			steadyStateRothConversion,
			nil,
			nil,
			medicareEligibleAdultCountAtYear(s, steadyStateMonth/12),
			plannerInflationFactorForYear(s.InflationRate, steadyStateYear),
		)
		if steadyStateYear >= 2 {
			lookbackMAGI := steadyStateSnapshot.AnnualMAGI
			steadyStateIRMALookbackMAGI = &lookbackMAGI
			steadyStateSnapshot = steadyStateTaxState.estimateMonthlySnapshot(
				taxCalculator,
				steadyStateMonth/12,
				steadyStateMonth%12,
				steadyStateIncomeBreakdown.OrdinaryIncome+steadyStateTaxableCashFlow.NonQualifiedDividends,
				steadyStateIncomeBreakdown.SocialSecurityIncome,
				result.SteadyStateRMD,
				steadyStateTaxableCashFlow.QualifiedDividends,
				steadyStateTaxableCashFlow.CapitalGainsDistributions,
				steadyStateTaxableCashFlow.NonQualifiedDividends,
				steadyStateRothConversion,
				nil,
				steadyStateIRMALookbackMAGI,
				medicareEligibleAdultCountAtYear(s, steadyStateMonth/12),
				plannerInflationFactorForYear(s.InflationRate, steadyStateYear),
			)
		}
		steadyStateTaxes := steadyStateSnapshot.MonthlyTax
		result.SteadyStateExpenses += steadyStateSnapshot.MonthlyIRMAA
		result.SteadyStateGrossIncome = result.SteadyStateIncome + result.SteadyStateRMD
		result.SteadyStateNetIncome = result.SteadyStateGrossIncome - steadyStateTaxes
		result.SteadyStateTaxes = steadyStateTaxes
		result.SteadyStateNIIT = steadyStateSnapshot.AnnualNIIT / 12
		result.SteadyStateIRMAA = steadyStateSnapshot.MonthlyIRMAA
		result.SteadyStateTaxableSocialSecurityPct = steadyStateSnapshot.TaxableSocialSecurityPct

		// Calculate steady state gap
		result.SteadyStateGap = result.SteadyStateExpenses - result.SteadyStateNetIncome

		// Calculate steady state withdrawal rate
		if s.PortfolioValue > 0 && result.SteadyStateGap > 0 {
			// Use estimated portfolio value at steady state
			yearsToSteadyState := float64(steadyStateMonth) / 12
			estimatedPortfolio := s.PortfolioValue * math.Pow(1+effectiveReturn/100, yearsToSteadyState)
			result.SteadyStateRate = (result.SteadyStateGap * 12 / estimatedPortfolio) * 100
		}
	}

	return result
}

// findSteadyStateMonth finds the month when all income sources are active
// Returns 0 if all sources start immediately (no delayed income)
func (c *Calculator) findSteadyStateMonth() int {
	maxStartMonth := 0

	for _, source := range c.Settings.IncomeSources {
		if source.StartMonth > maxStartMonth {
			// Make sure this source will actually be active (hasn't ended)
			if source.EndMonth == nil || *source.EndMonth > source.StartMonth {
				maxStartMonth = source.StartMonth
			}
		}
	}

	// Cap at projection length
	maxMonth := c.Settings.ProjectionYears * 12
	if maxStartMonth > maxMonth {
		maxStartMonth = maxMonth
	}

	return maxStartMonth
}

// CalculatePresentValueAnalysis computes PV of expenses and income
func (c *Calculator) CalculatePresentValueAnalysis() *models.PresentValueAnalysis {
	s := c.Settings
	months := s.ProjectionYears * 12
	discountRate := s.DiscountRate

	// Calculate PV of expenses
	pvExpenses := 0.0

	// Living expenses with inflation - spending decline
	netInflation := s.InflationRate - s.SpendingDeclineRate
	pvExpenses += PresentValueAnnuity(s.MonthlyLivingExpenses, discountRate, netInflation, 0, months)

	// Healthcare expenses using multi-person model or legacy
	if len(s.HealthcarePersons) > 0 {
		// Multi-person model: calculate PV for each person
		for _, person := range s.HealthcarePersons {
			pvExpenses += c.calculateHealthcarePV(person, discountRate, months)
		}
	} else if s.MonthlyHealthcare > 0 {
		// Legacy single-value model
		healthcareStartMonth := s.HealthcareStartYears * 12
		healthcareMonths := months - healthcareStartMonth
		if healthcareMonths > 0 {
			pvExpenses += PresentValueAnnuity(s.MonthlyHealthcare, discountRate, s.HealthcareInflation, healthcareStartMonth, healthcareMonths)
		}
	}

	// Add expense sources
	for _, source := range s.ExpenseSources {
		startMonth := source.StartYear * 12
		endMonth := months
		if source.EndYear > 0 {
			endMonth = min(source.EndYear*12, months)
		}
		duration := endMonth - startMonth
		if duration > 0 {
			growthRate := 0.0
			if source.Inflation {
				growthRate = s.InflationRate
			}
			pvExpenses += PresentValueAnnuity(source.Amount, discountRate, growthRate, startMonth, duration)
		}
	}

	// Calculate PV of income sources
	pvIncome := 0.0
	for _, source := range s.IncomeSources {
		endMonth := months
		if source.EndMonth != nil {
			endMonth = min(*source.EndMonth, months)
		}
		duration := endMonth - source.StartMonth
		if duration > 0 {
			pvIncome += PresentValueAnnuity(source.Amount, discountRate, source.COLARate*100, source.StartMonth, duration)
		}
	}

	pvGap := pvExpenses - pvIncome
	coverageRatio := 0.0
	if pvExpenses > 0 {
		coverageRatio = (s.PortfolioValue + pvIncome) / pvExpenses
	}
	surplusDeficit := s.PortfolioValue + pvIncome - pvExpenses

	return &models.PresentValueAnalysis{
		PVExpenses:     pvExpenses,
		PVIncome:       pvIncome,
		PVGap:          pvGap,
		CoverageRatio:  coverageRatio,
		SurplusDeficit: surplusDeficit,
	}
}

// CalculateSustainabilityScore computes the sustainability score
func (c *Calculator) CalculateSustainabilityScore(projection *models.ProjectionResult) *models.SustainabilityScore {
	budgetFit := c.CalculateBudgetFit()
	return models.CalculateSustainabilityScore(budgetFit.RequiredRate, projection.Survives)
}

// CalculateSensitivity runs sensitivity analysis on key parameters
func (c *Calculator) CalculateSensitivity() []models.SensitivityResult {
	results := make([]models.SensitivityResult, 0)

	// Get baseline score
	baseProjection := c.RunProjection()
	baseScore := c.CalculateSustainabilityScore(baseProjection)

	// Get effective baseline return (either explicit or calculated from per-account allocation)
	// When InvestmentReturn=0, the projection uses per-account asset allocation blended returns
	effectiveReturn := c.Settings.InvestmentReturn
	if effectiveReturn == 0 {
		effectiveReturn = c.Settings.GetExpectedReturnFromAllocation()
	}

	// Define scenarios
	scenarios := []models.SensitivityScenario{
		{Name: "Higher Returns", ParamName: "investment_return", ParamValue: effectiveReturn + 2, Change: "+2%"},
		{Name: "Lower Returns", ParamName: "investment_return", ParamValue: effectiveReturn - 2, Change: "-2%"},
		{Name: "Higher Inflation", ParamName: "inflation_rate", ParamValue: c.Settings.InflationRate + 1, Change: "+1%"},
		{Name: "Lower Inflation", ParamName: "inflation_rate", ParamValue: c.Settings.InflationRate - 1, Change: "-1%"},
		{Name: "Higher Spending", ParamName: "monthly_living_expenses", ParamValue: c.Settings.MonthlyLivingExpenses * 1.1, Change: "+10%"},
		{Name: "Higher Healthcare", ParamName: "monthly_healthcare", ParamValue: c.Settings.MonthlyHealthcare * 1.5, Change: "+50%"},
	}

	for _, scenario := range scenarios {
		// Clone settings and apply variation
		modifiedSettings := *c.Settings
		modifiedSettings.IncomeSources = append([]models.IncomeSource{}, c.Settings.IncomeSources...)
		modifiedSettings.ExpenseSources = append([]models.ExpenseSource{}, c.Settings.ExpenseSources...)

		switch scenario.ParamName {
		case "investment_return":
			modifiedSettings.InvestmentReturn = scenario.ParamValue
		case "inflation_rate":
			modifiedSettings.InflationRate = scenario.ParamValue
		case "monthly_living_expenses":
			modifiedSettings.MonthlyLivingExpenses = scenario.ParamValue
		case "monthly_healthcare":
			modifiedSettings.MonthlyHealthcare = scenario.ParamValue
		}

		// Run projection with modified settings
		modCalc := NewCalculatorWithChain(&modifiedSettings, c.ResolvedChain)
		modProjection := modCalc.RunProjection()
		modScore := modCalc.CalculateSustainabilityScore(modProjection)

		results = append(results, models.SensitivityResult{
			Scenario:       scenario,
			LongevityYears: modProjection.LongevityYears,
			FinalBalance:   modProjection.FinalBalance,
			Survives:       modProjection.Survives,
			ScoreChange:    modScore.Score - baseScore.Score,
		})
	}

	return results
}

// CalculateFailurePoints finds exact thresholds where the portfolio fails
func (c *Calculator) CalculateFailurePoints() *models.FailurePointAnalysis {
	baseProjection := c.RunProjection()
	failurePoints := make([]models.FailurePoint, 0)

	// If baseline already fails, we can't find "failure thresholds"
	if !baseProjection.Survives {
		return &models.FailurePointAnalysis{
			FailurePoints:    failurePoints,
			BaselineSurvives: false,
		}
	}

	// Find minimum investment return needed
	if fp := c.findReturnThreshold(); fp != nil {
		failurePoints = append(failurePoints, *fp)
	}

	// Find maximum inflation tolerable
	if fp := c.findInflationThreshold(); fp != nil {
		failurePoints = append(failurePoints, *fp)
	}

	// Find maximum expenses tolerable
	if fp := c.findExpensesThreshold(); fp != nil {
		failurePoints = append(failurePoints, *fp)
	}

	// Find minimum portfolio needed
	if fp := c.findPortfolioThreshold(); fp != nil {
		failurePoints = append(failurePoints, *fp)
	}

	return &models.FailurePointAnalysis{
		FailurePoints:    failurePoints,
		BaselineSurvives: true,
	}
}

// findReturnThreshold finds minimum investment return to survive
func (c *Calculator) findReturnThreshold() *models.FailurePoint {
	current := c.Settings.InvestmentReturn

	// Binary search between 0% and current value
	low, high := -5.0, current
	precision := 0.1

	// First check if 0% return survives
	modSettings := *c.Settings
	modSettings.IncomeSources = append([]models.IncomeSource{}, c.Settings.IncomeSources...)
	modSettings.ExpenseSources = append([]models.ExpenseSource{}, c.Settings.ExpenseSources...)
	modSettings.InvestmentReturn = low
	modCalc := NewCalculatorWithChain(&modSettings, c.ResolvedChain)
	if modCalc.RunProjection().Survives {
		// Survives even at -5%, no meaningful threshold
		return &models.FailurePoint{
			ParamName:    "investment_return",
			ParamLabel:   "Investment Return",
			CurrentValue: current,
			Threshold:    -5.0,
			Direction:    "below",
			Margin:       current + 5.0,
			SafetyLevel:  "safe",
		}
	}

	// Binary search for threshold
	for high-low > precision {
		mid := (low + high) / 2
		modSettings.InvestmentReturn = mid
		modCalc := NewCalculatorWithChain(&modSettings, c.ResolvedChain)
		if modCalc.RunProjection().Survives {
			high = mid
		} else {
			low = mid
		}
	}

	threshold := math.Round(high*10) / 10
	margin := current - threshold
	safetyLevel := "safe"
	if margin < 1 {
		safetyLevel = "critical"
	} else if margin < 2 {
		safetyLevel = "marginal"
	}

	return &models.FailurePoint{
		ParamName:    "investment_return",
		ParamLabel:   "Investment Return",
		CurrentValue: current,
		Threshold:    threshold,
		Direction:    "below",
		Margin:       margin,
		SafetyLevel:  safetyLevel,
	}
}

// findInflationThreshold finds maximum inflation before failure
func (c *Calculator) findInflationThreshold() *models.FailurePoint {
	current := c.Settings.InflationRate

	// Binary search between current and 15%
	low, high := current, 15.0
	precision := 0.1

	// First check if 15% inflation fails
	modSettings := *c.Settings
	modSettings.IncomeSources = append([]models.IncomeSource{}, c.Settings.IncomeSources...)
	modSettings.ExpenseSources = append([]models.ExpenseSource{}, c.Settings.ExpenseSources...)
	modSettings.InflationRate = high
	modCalc := NewCalculatorWithChain(&modSettings, c.ResolvedChain)
	if modCalc.RunProjection().Survives {
		// Survives even at 15%, very robust
		return &models.FailurePoint{
			ParamName:    "inflation_rate",
			ParamLabel:   "Inflation Rate",
			CurrentValue: current,
			Threshold:    15.0,
			Direction:    "above",
			Margin:       15.0 - current,
			SafetyLevel:  "safe",
		}
	}

	// Binary search for threshold
	for high-low > precision {
		mid := (low + high) / 2
		modSettings.InflationRate = mid
		modCalc := NewCalculatorWithChain(&modSettings, c.ResolvedChain)
		if modCalc.RunProjection().Survives {
			low = mid
		} else {
			high = mid
		}
	}

	threshold := math.Round(low*10) / 10
	margin := threshold - current
	safetyLevel := "safe"
	if margin < 1 {
		safetyLevel = "critical"
	} else if margin < 2 {
		safetyLevel = "marginal"
	}

	return &models.FailurePoint{
		ParamName:    "inflation_rate",
		ParamLabel:   "Inflation Rate",
		CurrentValue: current,
		Threshold:    threshold,
		Direction:    "above",
		Margin:       margin,
		SafetyLevel:  safetyLevel,
	}
}

// findExpensesThreshold finds maximum monthly expenses before failure
func (c *Calculator) findExpensesThreshold() *models.FailurePoint {
	current := c.Settings.MonthlyLivingExpenses
	if current <= 0 {
		return nil
	}

	// Binary search between current and 3x current
	low, high := current, current*3
	precision := 50.0 // $50 precision

	// First check if 3x expenses fails
	modSettings := *c.Settings
	modSettings.IncomeSources = append([]models.IncomeSource{}, c.Settings.IncomeSources...)
	modSettings.ExpenseSources = append([]models.ExpenseSource{}, c.Settings.ExpenseSources...)
	modSettings.MonthlyLivingExpenses = high
	modCalc := NewCalculatorWithChain(&modSettings, c.ResolvedChain)
	if modCalc.RunProjection().Survives {
		// Survives even at 3x expenses
		margin := ((high / current) - 1) * 100
		return &models.FailurePoint{
			ParamName:    "monthly_expenses",
			ParamLabel:   "Monthly Expenses",
			CurrentValue: current,
			Threshold:    high,
			Direction:    "above",
			Margin:       margin,
			SafetyLevel:  "safe",
		}
	}

	// Binary search for threshold
	for high-low > precision {
		mid := (low + high) / 2
		modSettings.MonthlyLivingExpenses = mid
		modCalc := NewCalculatorWithChain(&modSettings, c.ResolvedChain)
		if modCalc.RunProjection().Survives {
			low = mid
		} else {
			high = mid
		}
	}

	threshold := math.Round(low/50) * 50 // Round to nearest $50
	margin := ((threshold / current) - 1) * 100
	safetyLevel := "safe"
	if margin < 10 {
		safetyLevel = "critical"
	} else if margin < 25 {
		safetyLevel = "marginal"
	}

	return &models.FailurePoint{
		ParamName:    "monthly_expenses",
		ParamLabel:   "Monthly Expenses",
		CurrentValue: current,
		Threshold:    threshold,
		Direction:    "above",
		Margin:       margin,
		SafetyLevel:  safetyLevel,
	}
}

// findPortfolioThreshold finds minimum portfolio needed to survive
func (c *Calculator) findPortfolioThreshold() *models.FailurePoint {
	current := c.Settings.PortfolioValue
	if current <= 0 {
		return nil
	}

	// Binary search between 0 and current
	low, high := 0.0, current
	precision := 1000.0 // $1000 precision

	// First check if $0 survives (e.g., income covers all expenses)
	modSettings := *c.Settings
	modSettings.IncomeSources = append([]models.IncomeSource{}, c.Settings.IncomeSources...)
	modSettings.ExpenseSources = append([]models.ExpenseSource{}, c.Settings.ExpenseSources...)
	modSettings.PortfolioValue = low
	modCalc := NewCalculatorWithChain(&modSettings, c.ResolvedChain)
	if modCalc.RunProjection().Survives {
		return &models.FailurePoint{
			ParamName:    "portfolio_value",
			ParamLabel:   "Portfolio Value",
			CurrentValue: current,
			Threshold:    0,
			Direction:    "below",
			Margin:       100, // 100% buffer
			SafetyLevel:  "safe",
		}
	}

	// Binary search for threshold
	for high-low > precision {
		mid := (low + high) / 2
		modSettings.PortfolioValue = mid
		modCalc := NewCalculatorWithChain(&modSettings, c.ResolvedChain)
		if modCalc.RunProjection().Survives {
			high = mid
		} else {
			low = mid
		}
	}

	threshold := math.Round(high/1000) * 1000 // Round to nearest $1000
	margin := ((current - threshold) / current) * 100
	safetyLevel := "safe"
	if margin < 10 {
		safetyLevel = "critical"
	} else if margin < 25 {
		safetyLevel = "marginal"
	}

	return &models.FailurePoint{
		ParamName:    "portfolio_value",
		ParamLabel:   "Portfolio Value",
		CurrentValue: current,
		Threshold:    threshold,
		Direction:    "below",
		Margin:       margin,
		SafetyLevel:  safetyLevel,
	}
}

// MonteCarloConfig defines parameters for enhanced simulation
type MonteCarloConfig struct {
	// Market dynamics
	ReturnVolatility float64 // Annual return standard deviation (e.g., 15 for 15%)
	CrashProbability float64 // Annual probability of a crash (e.g., 0.05 for 5%)
	CrashSeverity    float64 // How bad crashes are (e.g., -30 for -30% return)
	RecoveryBoost    float64 // Extra return after crash years (mean reversion)

	// Spending shocks
	SpendingShockProb float64 // Annual probability of spending shock
	SpendingShockMin  float64 // Minimum shock amount ($)
	SpendingShockMax  float64 // Maximum shock amount ($)

	// Healthcare emergencies
	HealthShockProb float64 // Annual probability of health emergency
	HealthShockMin  float64 // Minimum health shock ($)
	HealthShockMax  float64 // Maximum health shock ($)

	// Longevity
	LongevityVariation int // Years +/- to vary projection length

	// Adaptive spending (reducing discretionary expenses during crashes)
	AdaptiveSpending        bool    // Enable adaptive spending during crashes
	DiscretionaryCutPercent float64 // % to cut discretionary spending during crash (e.g., 40 for 40%)
	AdaptationRecoveryYears int     // Years to maintain reduced spending after crash
}

// DefaultMonteCarloConfig returns realistic simulation parameters
func DefaultMonteCarloConfig() *MonteCarloConfig {
	return &MonteCarloConfig{
		// Market: ~15% annual volatility, 5% crash chance, crashes are -30% on average
		ReturnVolatility: 15.0,
		CrashProbability: 0.05,
		CrashSeverity:    -30.0,
		RecoveryBoost:    5.0,

		// Spending: 8% chance of $5K-$25K emergency per year
		SpendingShockProb: 0.08,
		SpendingShockMin:  5000,
		SpendingShockMax:  25000,

		// Health: 5% chance of $10K-$50K health event per year
		HealthShockProb: 0.05,
		HealthShockMin:  10000,
		HealthShockMax:  50000,

		// Longevity: +/- 5 years from base projection
		LongevityVariation: 5,
	}
}

// RunMonteCarloSimulation runs enhanced randomized scenario analysis
func (c *Calculator) RunMonteCarloSimulation(runs int) *models.MonteCarloAnalysis {
	if runs <= 0 {
		runs = 1000
	}

	config := DefaultMonteCarloConfig()
	results := make([]models.MonteCarloResult, runs)
	successCount := 0
	totalDepletionYears := 0.0
	depletionCount := 0

	// Track aggregate shock statistics
	totalCrashes := 0
	totalSpendingShocks := 0
	totalHealthShocks := 0
	runsWithCrashes := 0
	runsWithSpendingShocks := 0
	runsWithHealthShocks := 0

	// Create a new random source seeded with current time
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := 0; i < runs; i++ {
		result := c.runSingleMonteCarloSimulation(rng, config)
		results[i] = result

		// Aggregate statistics
		if result.Survives {
			successCount++
		}
		if result.DepletionYear > 0 {
			totalDepletionYears += result.DepletionYear
			depletionCount++
		}

		totalCrashes += result.MarketCrashes
		totalSpendingShocks += result.SpendingShocks
		totalHealthShocks += result.HealthShocks

		if result.MarketCrashes > 0 {
			runsWithCrashes++
		}
		if result.SpendingShocks > 0 {
			runsWithSpendingShocks++
		}
		if result.HealthShocks > 0 {
			runsWithHealthShocks++
		}
	}

	// Calculate statistics
	balances := make([]float64, runs)
	for i, r := range results {
		balances[i] = r.FinalBalance
	}
	sortFloat64s(balances)

	stats := &models.MonteCarloStats{
		Runs:          runs,
		SuccessRate:   float64(successCount) / float64(runs) * 100,
		MedianBalance: balances[runs/2],
		MeanBalance:   mean(balances),
		Percentile10:  balances[runs/10],
		Percentile25:  balances[runs/4],
		Percentile75:  balances[runs*3/4],
		Percentile90:  balances[runs*9/10],
		WorstCase:     balances[0],
		BestCase:      balances[runs-1],

		// Enhanced stats
		MarketCrashCount:   runsWithCrashes,
		SpendingShockCount: runsWithSpendingShocks,
		HealthShockCount:   runsWithHealthShocks,
		AvgCrashesPerRun:   float64(totalCrashes) / float64(runs),
		AvgShocksPerRun:    float64(totalSpendingShocks+totalHealthShocks) / float64(runs),
	}

	if depletionCount > 0 {
		stats.AvgDepletionYr = totalDepletionYears / float64(depletionCount)
	}

	// Calculate sequence risk impact by comparing early vs late crash outcomes
	stats.SequenceRiskImpact = c.calculateSequenceRiskImpact(results)

	// Calculate detailed sequence risk breakdown with expense context
	// Use CalculateTotalExpenses to include all expense sources
	annualExpenses := c.CalculateTotalExpenses(0) * 12
	stats.SequenceRisk = c.calculateSequenceRiskBreakdown(results, annualExpenses, c.Settings.PortfolioValue)

	// Run adaptive spending simulations if discretionary expenses exist
	if stats.SequenceRisk != nil && stats.SequenceRisk.HasDiscretionary {
		adaptiveConfig := *config
		adaptiveConfig.AdaptiveSpending = true
		adaptiveConfig.DiscretionaryCutPercent = 40 // Cut 40% of discretionary during crashes
		adaptiveConfig.AdaptationRecoveryYears = 3  // Maintain reduced spending for 3 years after crash

		// Run adaptive simulations (smaller sample for performance)
		adaptiveRuns := runs / 2
		adaptiveResults := make([]models.MonteCarloResult, adaptiveRuns)
		adaptiveRng := rand.New(rand.NewSource(42)) // Fixed seed for reproducibility

		for i := 0; i < adaptiveRuns; i++ {
			adaptiveResults[i] = c.runSingleMonteCarloSimulation(adaptiveRng, &adaptiveConfig)
		}

		// Calculate adapted early crash survival rate
		var adaptedEarlySurvived, adaptedEarlyTotal int
		for _, r := range adaptiveResults {
			if r.EarlyCrashes > 0 {
				adaptedEarlyTotal++
				if r.Survives {
					adaptedEarlySurvived++
				}
			}
		}

		if adaptedEarlyTotal > 0 {
			adaptedSurvival := float64(adaptedEarlySurvived) / float64(adaptedEarlyTotal) * 100
			stats.SequenceRisk.EarlyCrashSurvivalAdapted = adaptedSurvival
			stats.SequenceRisk.AdaptationBoost = adaptedSurvival - stats.SequenceRisk.EarlyCrashSurvival
			stats.SequenceRisk.DiscretionaryCutPercent = adaptiveConfig.DiscretionaryCutPercent

			// Generate rationale based on improvement
			if stats.SequenceRisk.AdaptationBoost >= 15 {
				stats.SequenceRisk.AdaptationRationale = "Significant protection: cutting discretionary spending during crashes substantially improves survival"
			} else if stats.SequenceRisk.AdaptationBoost >= 5 {
				stats.SequenceRisk.AdaptationRationale = "Moderate benefit: reducing discretionary expenses during downturns provides meaningful protection"
			} else if stats.SequenceRisk.AdaptationBoost > 0 {
				stats.SequenceRisk.AdaptationRationale = "Slight improvement: spending flexibility provides some buffer against early crashes"
			} else {
				stats.SequenceRisk.AdaptationRationale = "Limited impact: your plan is resilient even without spending cuts"
			}
		}
	}

	// Create distribution buckets
	distribution := c.createDistributionBuckets(balances)

	return &models.MonteCarloAnalysis{
		Stats:        stats,
		Distribution: distribution,
	}
}

// runSingleMonteCarloSimulation runs one complete simulation with all risk factors
func (c *Calculator) runSingleMonteCarloSimulation(rng *rand.Rand, config *MonteCarloConfig) models.MonteCarloResult {
	primarySettings := c.Settings
	activeSettings := c.Settings
	nextChainIdx := 0
	s := activeSettings

	// Vary projection length for longevity risk
	projectionYears := s.ProjectionYears
	if config.LongevityVariation > 0 {
		variation := rng.Intn(config.LongevityVariation*2+1) - config.LongevityVariation
		projectionYears = max(10, s.ProjectionYears+variation)
	}
	months := projectionYears * 12

	// Initialize balances (3-bucket model)
	taxDeferredBalance := s.PortfolioValue * (s.TaxDeferredPercent / 100)
	rothBalance := s.PortfolioValue * (s.RothPercent / 100)
	taxableAccount := newTaxableAccountState(s, s.PortfolioValue-taxDeferredBalance-rothBalance)

	var depletionYear float64
	depleted := false

	currentLivingExpenses := calculateLivingExpensesAtMonth(s, 0)

	// Track shocks for this run
	crashTiming := &CrashTiming{}
	spendingShocks := 0
	healthShocks := 0
	lastCrashYear := -999 // Track for recovery boost

	// Annual RMD tracking
	var monthlyRMD float64
	var taxState projectionTaxAccumulator
	taxCalculator := NewTaxCalculator(s.TaxConfig, s.InflationRate)
	completedMAGIHistory := make([]float64, 0, projectionYears)
	currentYearTaxSnapshot := projectedTaxSnapshot{}

	// Healthcare cost variation multiplier (updated annually)
	healthcareVariation := 1.0
	inflationVar := 1.0

	// Track cumulative inflation for spending phase calculations
	cumulativeInflation := 1.0

	// Adaptive spending: track when we're in reduced-spending mode
	adaptationEndYear := -1 // Year when adaptation ends (-1 = not adapting)

	// Generate year-by-year returns upfront for sequence of returns (per asset class)
	assetReturns := c.generateAssetReturns(rng, config, projectionYears, crashTiming, &lastCrashYear)

	// Get per-account allocations for blending returns
	tdStock, tdBond, tdCash := s.GetTaxDeferredAllocation()
	rothStock, rothBond, rothCash := s.GetRothAllocation()
	taxStock, taxBond, taxCash := s.GetTaxableAllocation()

	for m := 0; m < months; m++ {
		if depleted {
			break
		}

		currentYear := m / 12
		phaseAge := s.GetPhaseReferenceAge(currentYear) // Age used for spending phase calculations (may differ for couples)
		// RMD uses OLDER person's age - whoever hits 73 first triggers RMD
		olderAge := s.GetOlderAge() + currentYear
		bigTicketExpenseThisMonth := 0.0
		rothConversionThisMonth := 0.0
		allowTaxDeferredWithdrawal := !taxDeferredDelayActive(s, currentYear)
		penaltyRate := earlyWithdrawalPenaltyRate(s.CurrentAge, currentYear)

		// Annual adjustments at year boundaries
		if m%12 == 0 {
			if m > 0 {
				completedMAGIHistory = append(completedMAGIHistory, currentYearTaxSnapshot.AnnualMAGI)
			}
			taxState = projectionTaxAccumulator{}
			// Check for chain transition
			if len(c.ResolvedChain) > 0 {
				newIdx, prepared := c.nextChainTransition(currentYear, nextChainIdx, primarySettings)
				if prepared != nil {
					activeSettings = prepared
					s = activeSettings
					nextChainIdx = newIdx

					currentLivingExpenses = rebaseLivingExpensesAtTransition(s, phaseAge, cumulativeInflation)
					taxableAccount.syncAssumptions(s)

					// Refresh cached allocation variables
					tdStock, tdBond, tdCash = s.GetTaxDeferredAllocation()
					rothStock, rothBond, rothCash = s.GetRothAllocation()
					taxStock, taxBond, taxCash = s.GetTaxableAllocation()
				}
			}
			taxCalculator = NewTaxCalculator(s.TaxConfig, s.InflationRate)
			taxableAccount.RealizedGainsYTD = 0

			// Apply inflation with some random variation
			inflationVar = 1 + (rng.Float64()-0.5)*0.02 // +/- 1%

			// Healthcare cost variation (healthcare is more volatile, +/- 2%)
			healthcareVariation = 1 + (rng.Float64()-0.5)*0.04

			// Calculate annual RMD
			if olderAge >= RMDStartAge && taxDeferredBalance > 0 {
				annualRMD, _ := CalculateRMD(taxDeferredBalance, olderAge)
				monthlyRMD = annualRMD / 12
			} else {
				monthlyRMD = 0
			}

			// Process Roth conversions (annual, at year boundary)
			if conversionAmount := rothConversionAmountForYear(s, currentYear, taxDeferredBalance); conversionAmount > 0 {
				taxDeferredBalance -= conversionAmount
				rothBalance += conversionAmount
				rothConversionThisMonth = conversionAmount
			}

			// Process big ticket items for this year
			for _, item := range s.BigTicketItems {
				if item.Year == currentYear {
					if item.Type == models.BigTicketIncome {
						taxableAccount.addCash(item.Amount)
					} else {
						remaining := applyBigTicketExpenseWithTaxableState(item.Amount, allowTaxDeferredWithdrawal, penaltyRate, &taxDeferredBalance, &taxableAccount, &rothBalance)
						bigTicketExpenseThisMonth += remaining
					}
				}
			}
		}

		if m > 0 {
			cumulativeInflation *= monthlyCompoundFactorFromDecimal(s.InflationRate / 100 * inflationVar)
			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
				currentLivingExpenses = s.MonthlyLivingExpenses * s.GetSpendingMultiplier(phaseAge) * cumulativeInflation
			} else {
				currentLivingExpenses *= monthlyCompoundFactorFromDecimal((s.InflationRate - s.SpendingDeclineRate) / 100 * inflationVar)
			}
		}

		// Calculate healthcare expenses using multi-person model with variation
		activeHealthcare := s.GetTotalHealthcareCost(m) * healthcareVariation
		totalExpenses := currentLivingExpenses + activeHealthcare + bigTicketExpenseThisMonth

		// Check if we should enter adaptation mode (crash detected this year via stock returns)
		if config.AdaptiveSpending && assetReturns.Stock[currentYear] < -15 {
			// Crash year: start adapting spending
			adaptationEndYear = currentYear + config.AdaptationRecoveryYears
		}
		inAdaptationMode := config.AdaptiveSpending && currentYear <= adaptationEndYear

		// Add expense sources (with phase multiplier and adaptive spending reduction if applicable)
		for _, source := range s.ExpenseSources {
			expenseAmount := source.GetAdjustedAmount(m, s.InflationRate)
			// Apply phase multiplier to discretionary expenses
			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled && source.Discretionary {
				expenseAmount *= s.GetSpendingMultiplier(phaseAge)
			}
			// Reduce discretionary expenses during adaptation
			if inAdaptationMode && source.Discretionary {
				expenseAmount *= (1 - config.DiscretionaryCutPercent/100)
			}
			totalExpenses += expenseAmount
		}

		// Apply spending shock (checked monthly, but represents annual probability)
		if m%12 == 0 && rng.Float64() < config.SpendingShockProb {
			shockAmount := config.SpendingShockMin + rng.Float64()*(config.SpendingShockMax-config.SpendingShockMin)
			totalExpenses += shockAmount / 12 // Spread over the year
			spendingShocks++
		}

		// Apply health shock (separate from regular healthcare)
		if m%12 == 0 && rng.Float64() < config.HealthShockProb {
			healthShockAmount := config.HealthShockMin + rng.Float64()*(config.HealthShockMax-config.HealthShockMin)
			totalExpenses += healthShockAmount / 12 // Spread over the year
			healthShocks++
		}

		incomeBreakdown := calculateMonthlyIncomeBreakdown(s, m)

		// Apply this year's investment returns (per-account based on allocation)
		stockReturn := assetReturns.Stock[currentYear]
		bondReturn := assetReturns.Bond[currentYear]
		cashReturn := assetReturns.Cash[currentYear]

		// Calculate per-account blended returns
		tdReturn := models.GetBlendedReturn(tdStock, tdBond, tdCash, stockReturn, bondReturn, cashReturn)
		rothReturnRate := models.GetBlendedReturn(rothStock, rothBond, rothCash, stockReturn, bondReturn, cashReturn)
		taxReturn := models.GetBlendedReturn(taxStock, taxBond, taxCash, stockReturn, bondReturn, cashReturn)

		// Convert annual returns to monthly using geometric formula (not simple division)
		tdMonthly := math.Pow(1+tdReturn/100, 1.0/12) - 1
		rothMonthlyRate := math.Pow(1+rothReturnRate/100, 1.0/12) - 1
		taxableComponents := buildTaxableReturnComponents(taxReturn, s)
		irmaaEligibleAdults := medicareEligibleAdultCountAtYear(s, currentYear)
		monthResult := executeTaxAwarePortfolioMonth(
			totalExpenses,
			incomeBreakdown,
			monthlyRMD,
			allowTaxDeferredWithdrawal,
			penaltyRate,
			&taxDeferredBalance,
			&taxableAccount,
			&rothBalance,
			tdMonthly,
			rothMonthlyRate,
			taxableComponents,
			s.GetProjectionTiming(),
			taxState,
			taxCalculator,
			currentYear,
			m%12,
			rothConversionThisMonth,
			completedMAGIHistory,
			irmaaEligibleAdults,
			cumulativeInflation,
		)
		currentYearTaxSnapshot = monthResult.TaxSnapshot
		taxState.applyMonth(
			incomeBreakdown.OrdinaryIncome+monthResult.TaxableNonQualifiedDividends,
			incomeBreakdown.SocialSecurityIncome,
			monthResult.CashFlow.WithdrawalFromTaxDeferred,
			monthResult.TaxableQualifiedDividends,
			monthResult.TaxableCapitalGains,
			monthResult.TaxableNonQualifiedDividends,
			rothConversionThisMonth,
			monthResult.TaxesPaid,
		)
		shortfall := monthResult.Shortfall

		// Check for depletion
		totalBalance := taxDeferredBalance + rothBalance + taxableAccount.MarketValue
		if shortfallCausesDepletion(shortfall, allowTaxDeferredWithdrawal, taxDeferredBalance) {
			depleted = true
			depletionYear = float64(m) / 12
		}
		if totalBalance <= 0 {
			depleted = true
			depletionYear = float64(m) / 12
		}
	}

	finalBalance := taxDeferredBalance + rothBalance + taxableAccount.MarketValue
	if finalBalance < 0 {
		finalBalance = 0
	}

	return models.MonteCarloResult{
		FinalBalance:    finalBalance,
		DepletionYear:   depletionYear,
		Survives:        !depleted,
		MarketCrashes:   crashTiming.TotalCrashes,
		SpendingShocks:  spendingShocks,
		HealthShocks:    healthShocks,
		ProjectionYears: projectionYears,
		EarlyCrashes:    crashTiming.EarlyCrashes,
		MidCrashes:      crashTiming.MidCrashes,
		LateCrashes:     crashTiming.LateCrashes,
		FirstCrashYear:  crashTiming.FirstCrashYear,
	}
}

// CrashTiming tracks when crashes occurred during simulation
type CrashTiming struct {
	TotalCrashes   int
	EarlyCrashes   int // Years 1-5 (index 0-4)
	MidCrashes     int // Years 6-15 (index 5-14)
	LateCrashes    int // Years 16+ (index 15+)
	FirstCrashYear int // 0 means no crashes (1-indexed for display)
}

// AssetReturns holds per-asset-class returns for Monte Carlo simulation
type AssetReturns struct {
	Stock []float64
	Bond  []float64
	Cash  []float64
}

// generateAssetReturns creates sequences of annual returns per asset class with crashes and volatility
func (c *Calculator) generateAssetReturns(rng *rand.Rand, config *MonteCarloConfig, years int, timing *CrashTiming, lastCrashYear *int) *AssetReturns {
	stockReturns := make([]float64, years)
	bondReturns := make([]float64, years)
	cashReturns := make([]float64, years)

	// Get historical statistics for asset classes
	stockMean, bondMean, cashMean, _, stockStdDev, bondStdDev := GetHistoricalStats()

	for y := 0; y < years; y++ {
		var stockReturn, bondReturn, cashReturn float64

		// Check for market crash (affects stocks primarily)
		if rng.Float64() < config.CrashProbability {
			// Crash year: severe negative stock return, bonds often rally (flight to safety)
			stockReturn = config.CrashSeverity + (rng.Float64()-0.5)*10 // -35% to -25%
			bondReturn = 10 + rng.NormFloat64()*5                       // Bonds typically rally during crashes
			cashReturn = cashMean + rng.Float64()*1                     // Cash stable

			timing.TotalCrashes++
			*lastCrashYear = y

			// Track first crash year (1-indexed for human readability)
			if timing.FirstCrashYear == 0 {
				timing.FirstCrashYear = y + 1
			}

			// Categorize by timing
			if y < 5 {
				timing.EarlyCrashes++
			} else if y < 15 {
				timing.MidCrashes++
			} else {
				timing.LateCrashes++
			}
		} else if y == *lastCrashYear+1 {
			// Recovery year after crash: strong stock recovery, bonds normalize
			stockReturn = stockMean + config.RecoveryBoost + rng.NormFloat64()*stockStdDev*0.8
			bondReturn = bondMean + rng.NormFloat64()*bondStdDev
			cashReturn = cashMean + rng.Float64()*0.5
		} else {
			// Normal year: returns with volatility (normal distribution)
			stockReturn = stockMean + rng.NormFloat64()*stockStdDev
			bondReturn = bondMean + rng.NormFloat64()*bondStdDev
			cashReturn = cashMean + rng.Float64()*1 - 0.5 // Small variation
		}

		// Clamp individual returns to reasonable bounds
		stockReturns[y] = math.Max(-50, math.Min(60, stockReturn))
		bondReturns[y] = math.Max(-20, math.Min(40, bondReturn))
		cashReturns[y] = math.Max(0, math.Min(15, cashReturn))
	}

	return &AssetReturns{
		Stock: stockReturns,
		Bond:  bondReturns,
		Cash:  cashReturns,
	}
}

// generateYearlyReturns creates a sequence of annual returns with crashes and volatility
// Deprecated: Use generateAssetReturns for per-account allocation support
func (c *Calculator) generateYearlyReturns(rng *rand.Rand, config *MonteCarloConfig, years int, timing *CrashTiming, lastCrashYear *int) []float64 {
	assetReturns := c.generateAssetReturns(rng, config, years, timing, lastCrashYear)
	s := c.Settings

	// Get user's asset allocation with defaults applied
	stockPercent, bondPercent, cashPercent := s.GetEffectiveAssetAllocation()

	// Blend returns based on user's allocation
	returns := make([]float64, years)
	for y := 0; y < years; y++ {
		returns[y] = (stockPercent/100)*assetReturns.Stock[y] +
			(bondPercent/100)*assetReturns.Bond[y] +
			(cashPercent/100)*assetReturns.Cash[y]
	}

	return returns
}

// calculateSequenceRiskImpact measures how sequence of returns affected outcomes
func (c *Calculator) calculateSequenceRiskImpact(results []models.MonteCarloResult) float64 {
	// Compare success rates of runs with crashes vs without
	// A value > 0 means crashes hurt outcomes (expected)
	if len(results) < 100 {
		return 0
	}

	crashRunsSucceeded := 0
	crashRunsTotal := 0
	noCrashRunsSucceeded := 0
	noCrashRunsTotal := 0

	for _, r := range results {
		if r.MarketCrashes > 0 {
			crashRunsTotal++
			if r.Survives {
				crashRunsSucceeded++
			}
		} else {
			noCrashRunsTotal++
			if r.Survives {
				noCrashRunsSucceeded++
			}
		}
	}

	if crashRunsTotal == 0 || noCrashRunsTotal == 0 {
		return 0
	}

	// Return the difference in survival rates
	survivalWithCrashes := float64(crashRunsSucceeded) / float64(crashRunsTotal) * 100
	survivalWithoutCrashes := float64(noCrashRunsSucceeded) / float64(noCrashRunsTotal) * 100

	return survivalWithoutCrashes - survivalWithCrashes
}

// calculateSequenceRiskBreakdown provides detailed analysis of crash timing impact
func (c *Calculator) calculateSequenceRiskBreakdown(results []models.MonteCarloResult, annualExpenses float64, portfolioValue float64) *models.SequenceRiskBreakdown {
	if len(results) < 100 {
		return nil
	}

	// Track survival by crash timing category
	var noCrashSurvived, noCrashTotal int
	var earlyCrashSurvived, earlyCrashTotal int
	var midCrashSurvived, midCrashTotal int
	var lateCrashSurvived, lateCrashTotal int

	// For recovery analysis
	var earlyRecoveries int
	var totalFirstCrashYears float64
	var firstCrashCount int

	for _, r := range results {
		// Categorize by where crashes occurred
		hasEarlyCrash := r.EarlyCrashes > 0
		hasMidCrash := r.MidCrashes > 0
		hasLateCrash := r.LateCrashes > 0
		hasAnyCrash := r.MarketCrashes > 0

		if !hasAnyCrash {
			noCrashTotal++
			if r.Survives {
				noCrashSurvived++
			}
		} else {
			// Track first crash timing for recovery analysis
			if r.FirstCrashYear > 0 {
				totalFirstCrashYears += float64(r.FirstCrashYear)
				firstCrashCount++
			}

			// Categorize by earliest crash (most impactful)
			if hasEarlyCrash {
				earlyCrashTotal++
				if r.Survives {
					earlyCrashSurvived++
					earlyRecoveries++
				}
			} else if hasMidCrash {
				midCrashTotal++
				if r.Survives {
					midCrashSurvived++
				}
			} else if hasLateCrash {
				lateCrashTotal++
				if r.Survives {
					lateCrashSurvived++
				}
			}
		}
	}

	// Calculate survival rates (as percentages)
	safeDiv := func(num, denom int) float64 {
		if denom == 0 {
			return 0
		}
		return float64(num) / float64(denom) * 100
	}

	noCrashSurvival := safeDiv(noCrashSurvived, noCrashTotal)
	earlyCrashSurvival := safeDiv(earlyCrashSurvived, earlyCrashTotal)
	midCrashSurvival := safeDiv(midCrashSurvived, midCrashTotal)
	lateCrashSurvival := safeDiv(lateCrashSurvived, lateCrashTotal)

	// Calculate impact metrics
	earlyVsLateImpact := lateCrashSurvival - earlyCrashSurvival
	earlyVsNoneImpact := noCrashSurvival - earlyCrashSurvival

	// Recovery analysis
	recoveryRate := safeDiv(earlyRecoveries, earlyCrashTotal)
	avgRecoveryYears := 0.0
	if firstCrashCount > 0 {
		avgRecoveryYears = totalFirstCrashYears / float64(firstCrashCount)
	}

	// Buffer recommendation based on impact
	recommendedBuffer := 2 // Default minimum
	rationale := "Standard 2-year buffer for moderate sequence risk"

	if earlyVsNoneImpact > 30 {
		recommendedBuffer = 5
		rationale = "High sequence risk: 5-year buffer to weather early crashes"
	} else if earlyVsNoneImpact > 20 {
		recommendedBuffer = 4
		rationale = "Significant sequence risk: 4-year buffer recommended"
	} else if earlyVsNoneImpact > 10 {
		recommendedBuffer = 3
		rationale = "Moderate sequence risk: 3-year buffer provides good protection"
	} else if earlyVsNoneImpact <= 5 {
		recommendedBuffer = 2
		rationale = "Low sequence risk: 2-year buffer is sufficient"
	}

	// Calculate buffer amount accounting for partial portfolio value during crash
	// Key insight: Even during a 30% crash, portfolio still has 70% of its value
	// You can still safely withdraw from the reduced portfolio (at a conservative rate)
	// The buffer only needs to cover the SHORTFALL, not full expenses
	crashDrawdownPercent := 30.0                                        // Expected crash severity (from DefaultMonteCarloConfig)
	crashedPortfolio := portfolioValue * (1 - crashDrawdownPercent/100) // Portfolio after crash
	safeWithdrawalRate := 0.03                                          // Conservative 3% during crash years
	safeWithdrawalDuringCrash := crashedPortfolio * safeWithdrawalRate  // Annual safe withdrawal from crashed portfolio
	annualShortfall := annualExpenses - safeWithdrawalDuringCrash       // Gap that buffer must cover
	if annualShortfall < 0 {
		annualShortfall = 0 // No shortfall if safe withdrawal covers expenses
	}

	// Calculate both naive and improved buffer amounts
	naiveBufferAmount := float64(recommendedBuffer) * annualExpenses
	bufferAmount := float64(recommendedBuffer) * annualShortfall

	// Update rationale to explain the improved calculation
	if annualShortfall > 0 && annualShortfall < annualExpenses {
		savingsPercent := (1 - annualShortfall/annualExpenses) * 100
		rationale = fmt.Sprintf("%s (%.0f%% less than naive calculation because crashed portfolio still provides $%.0f/yr)",
			rationale, savingsPercent, safeWithdrawalDuringCrash)
	}

	// Calculate adjusted monthly spending if buffer is set aside from portfolio
	// Uses a 4% safe withdrawal rate on the remaining portfolio after buffer
	adjustedSpending := 0.0
	remainingPortfolio := portfolioValue - bufferAmount
	if remainingPortfolio > 0 {
		adjustedSpending = (remainingPortfolio * 0.04) / 12 // 4% annual withdrawal rate, monthly
	}

	// Calculate expense breakdown for adaptive spending analysis
	expenseBreakdown := c.CalculateExpenseBreakdown(0)
	hasDiscretionary := expenseBreakdown.Discretionary > 0

	return &models.SequenceRiskBreakdown{
		NoCrashSurvival:    noCrashSurvival,
		EarlyCrashSurvival: earlyCrashSurvival,
		MidCrashSurvival:   midCrashSurvival,
		LateCrashSurvival:  lateCrashSurvival,

		NoCrashCount:    noCrashTotal,
		EarlyCrashCount: earlyCrashTotal,
		MidCrashCount:   midCrashTotal,
		LateCrashCount:  lateCrashTotal,

		EarlyVsLateImpact: earlyVsLateImpact,
		EarlyVsNoneImpact: earlyVsNoneImpact,

		RecoveryRate:     recoveryRate,
		AvgRecoveryYears: avgRecoveryYears,

		RecommendedBuffer: recommendedBuffer,
		BufferRationale:   rationale,
		BufferAmount:      bufferAmount,
		AnnualExpenses:    annualExpenses,
		AdjustedSpending:  adjustedSpending,

		// Buffer calculation breakdown
		CrashDrawdownPercent:      crashDrawdownPercent,
		CrashedPortfolioValue:     crashedPortfolio,
		SafeWithdrawalDuringCrash: safeWithdrawalDuringCrash,
		AnnualShortfall:           annualShortfall,
		NaiveBufferAmount:         naiveBufferAmount,

		// Adaptive spending fields
		HasDiscretionary:     hasDiscretionary,
		MonthlyDiscretionary: expenseBreakdown.Discretionary,
		MonthlyEssential:     expenseBreakdown.Essential,
	}
}

// createDistributionBuckets creates histogram buckets for visualization
func (c *Calculator) createDistributionBuckets(sortedBalances []float64) *models.MonteCarloDistribution {
	buckets := make([]models.MonteCarloDistBucket, 0)
	total := len(sortedBalances)

	// Define bucket boundaries based on data range
	maxVal := sortedBalances[total-1]

	// Use fixed boundaries with more detail in 0-3M range
	var boundaries []float64
	if maxVal <= 0 {
		boundaries = []float64{0}
	} else if maxVal < 100000 {
		boundaries = []float64{0, 10000, 25000, 50000, 75000, 100000}
	} else if maxVal < 1000000 {
		boundaries = []float64{0, 100000, 250000, 500000, 750000, 1000000}
	} else if maxVal < 3000000 {
		// Fine detail for 0-3M range
		boundaries = []float64{0, 250000, 500000, 1000000, 1500000, 2000000, 2500000, 3000000}
	} else {
		// Fixed boundaries with detail in 0-3M, then larger buckets for higher values
		boundaries = []float64{0, 250000, 500000, 1000000, 2000000, 3000000, 5000000, 10000000}
		// Add boundaries beyond 10M if needed
		if maxVal > 10000000 {
			boundaries = append(boundaries, 20000000)
		}
	}

	// Count items in each bucket
	for i := 0; i < len(boundaries)-1; i++ {
		low := boundaries[i]
		high := boundaries[i+1]
		count := 0
		for _, b := range sortedBalances {
			if b >= low && b < high {
				count++
			}
		}
		if count > 0 || i == 0 { // Always show first bucket even if empty
			buckets = append(buckets, models.MonteCarloDistBucket{
				Label:      formatBucketLabel(low, high),
				Count:      count,
				Percentage: float64(count) / float64(total) * 100,
			})
		}
	}

	// Add final bucket for values at or above last boundary
	lastBoundary := boundaries[len(boundaries)-1]
	count := 0
	for _, b := range sortedBalances {
		if b >= lastBoundary {
			count++
		}
	}
	if count > 0 {
		buckets = append(buckets, models.MonteCarloDistBucket{
			Label:      formatBucketLabel(lastBoundary, -1),
			Count:      count,
			Percentage: float64(count) / float64(total) * 100,
		})
	}

	return &models.MonteCarloDistribution{Buckets: buckets}
}

// formatBucketLabel formats a bucket range for display
func formatBucketLabel(low, high float64) string {
	formatVal := func(v float64) string {
		if v >= 1000000 {
			return fmt.Sprintf("$%.1fM", v/1000000)
		}
		return fmt.Sprintf("$%.0fK", v/1000)
	}

	if high < 0 {
		return formatVal(low) + "+"
	}
	return formatVal(low) + "-" + formatVal(high)
}

// Helper functions
func sortFloat64s(a []float64) {
	for i := 0; i < len(a)-1; i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

func mean(a []float64) float64 {
	if len(a) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range a {
		sum += v
	}
	return sum / float64(len(a))
}

func (c *Calculator) buildProjectionExplainability(projection *models.ProjectionResult) *models.ProjectionExplainability {
	if projection == nil || len(projection.Months) == 0 {
		return nil
	}

	summaries := projection.YearlySummaries
	if len(summaries) == 0 {
		summaries = make([]models.ProjectionYearSummary, 0, len(projection.Months)/12+1)
		startingBalance := c.Settings.PortfolioValue
		currentYear := projection.Months[0].Month / 12
		summary := models.ProjectionYearSummary{
			Year:            currentYear,
			StartingBalance: startingBalance,
		}

		finalizeYear := func(month models.ProjectionMonth) {
			summary.EndingBalance = month.PortfolioBalance
			summary.EndingBalanceReal = month.PortfolioBalanceReal
			summary.CumulativeInflation = month.CumulativeInflation
			summaries = append(summaries, summary)
		}

		for idx, month := range projection.Months {
			year := month.Month / 12
			if year != currentYear {
				prev := projection.Months[idx-1]
				finalizeYear(prev)
				startingBalance = prev.PortfolioBalance
				currentYear = year
				summary = models.ProjectionYearSummary{
					Year:            currentYear,
					StartingBalance: startingBalance,
				}
			}

			summary.Growth += month.PortfolioGrowth
			summary.GrossIncome += month.GrossIncome
			summary.Taxes += month.TaxesPaid
			summary.Expenses += month.TotalExpenses
			summary.Withdrawals += month.NetWithdrawal
		}

		finalizeYear(projection.Months[len(projection.Months)-1])
	}

	totalTaxes := 0.0
	totalGrossIncome := 0.0
	for _, summary := range summaries {
		totalTaxes += summary.Taxes
		totalGrossIncome += summary.GrossIncome
	}

	lastMonth := projection.Months[len(projection.Months)-1]
	taxShare := 0.0
	if totalGrossIncome > 0 {
		taxShare = totalTaxes / totalGrossIncome * 100
	}
	inflationLossPercent := 0.0
	if lastMonth.PortfolioBalance > 0 {
		inflationLossPercent = (1 - (lastMonth.PortfolioBalanceReal / lastMonth.PortfolioBalance)) * 100
	}

	return &models.ProjectionExplainability{
		YearlySummaries:         summaries,
		TotalTaxes:              totalTaxes,
		TotalGrossIncome:        totalGrossIncome,
		TaxShareOfGrossCashFlow: taxShare,
		FinalBalanceReal:        lastMonth.PortfolioBalanceReal,
		CumulativeInflation:     lastMonth.CumulativeInflation,
		InflationLossPercent:    inflationLossPercent,
	}
}

// RunFullAnalysis performs complete what-if analysis
func (c *Calculator) RunFullAnalysis() *models.WhatIfAnalysis {
	projection := c.RunProjection()
	projectionExplainability := c.buildProjectionExplainability(projection)
	budgetFit := c.CalculateBudgetFit()
	presentValue := c.CalculatePresentValueAnalysis()
	sustainability := c.CalculateSustainabilityScore(projection)
	sensitivity := c.CalculateSensitivity()
	failurePoints := c.CalculateFailurePoints()
	monteCarlo := c.RunMonteCarloSimulation(1000)
	rmd := c.CalculateRMDAnalysis()
	historicalBacktest := c.RunHistoricalBacktest()

	// Add Monte Carlo success rate for comparison
	if historicalBacktest != nil && monteCarlo != nil && monteCarlo.Stats != nil {
		historicalBacktest.MonteCarloSuccessRate = monteCarlo.Stats.SuccessRate
		historicalBacktest.HistoricalVsMC = historicalBacktest.SuccessRate - monteCarlo.Stats.SuccessRate
	}

	return &models.WhatIfAnalysis{
		Settings:                 c.Settings,
		Projection:               projection,
		ProjectionExplainability: projectionExplainability,
		BudgetFit:                budgetFit,
		PresentValue:             presentValue,
		Sustainability:           sustainability,
		Sensitivity:              sensitivity,
		FailurePoints:            failurePoints,
		MonteCarlo:               monteCarlo,
		RMD:                      rmd,
		HistoricalBacktest:       historicalBacktest,
	}
}
