package engine

import (
	"math"

	"budget2/internal/models"
)

// irmaaBaseYear is the calendar year on which the bundled CMS IRMAA
// surcharge brackets are based (CMS 2026 amounts). The planner
// rescales them onto the tax model's 2024 base year and then inflates
// forward.
const irmaaBaseYear = 2026

// medicareCostGrowthRate is the annual percentage growth applied to IRMAA
// surcharge DOLLARS (not the MAGI thresholds, which are CPI-indexed).
//
// IRMAA surcharges are recalculated each year off projected Medicare
// per-capita costs rather than CPI, and have historically grown around 5-6%
// a year. 5.5% is the midpoint of that range, chosen the same way the
// conservative 7/4/3 stock/bond/cash return estimates are: a defensible
// planning assumption baked into the engine rather than a user input.
const medicareCostGrowthRate = 5.5

// LivingExpensesAtMonth returns the inflation- and phase-adjusted
// living expense for the given month. Exported so analysis-package
// callers (historical backtest) can reuse the rule.
func LivingExpensesAtMonth(s *models.WhatIfSettings, month int) float64 {
	return livingExpensesAtMonth(s, month)
}

// RebaseLivingExpensesAtTransition anchors currentLivingExpenses to
// the new chain settings at a scenario boundary.
//
// cumulativeInflation    – full-inflation cumulative factor (used for
//
//	spending phases, which compound at the full
//	rate).
//
// netCumulativeInflation – (InflationRate−SpendingDeclineRate)
//
//	cumulative factor (used for the no-phase path,
//	which compounds at the net rate per month).
//	Passing the correct net factor prevents the
//	step-up error described in F-065.
func RebaseLivingExpensesAtTransition(s *models.WhatIfSettings, phaseAge int, cumulativeInflation float64, netCumulativeInflation float64) float64 {
	if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
		// Phases compound currentLivingExpenses at full InflationRate each
		// month, so the rebase anchor must also use full inflation.
		return s.MonthlyLivingExpenses * s.GetSpendingMultiplier(phaseAge) * cumulativeInflation
	}

	// No phases: currentLivingExpenses compounds at net rate
	// (InflationRate − SpendingDeclineRate) each month. Use
	// netCumulativeInflation so the rebase anchor matches the ongoing
	// per-month trajectory. (F-065)
	return s.MonthlyLivingExpenses * netCumulativeInflation
}

// YearsFromTaxBase converts a projection-year offset into the number of
// years between the plan's actual calendar year and the bundled federal
// tax tables' base year (taxBaseYear). The inflation-adjusted bracket,
// standard-deduction, and IRMAA math must key off this value — not the
// raw projection offset — so a plan that starts after the base year does
// not apply stale (un-inflated) brackets in its early years. The result
// may be negative for plans that start before the base year; the
// downstream inflation helpers floor non-positive values to "no
// adjustment".
func YearsFromTaxBase(s *models.WhatIfSettings, currentYear int) int {
	return ParseStartYear(s.StartDate) + currentYear - taxBaseYear
}

// MedicareEligibleAdultCountAtMonth returns the count (0, 1, or 2) of
// household adults actually paying Medicare premiums in the given projection
// month. Used to scale the per-person IRMAA surcharge to a household total.
//
// F-5: this counts enrolment, not age. IRMAA is a surcharge on Part B and
// Part D premiums, so someone who keeps employer coverage past 65 has nothing
// to surcharge — and the expense model already bills them an employer premium
// for those years. Reading age alone made the two models contradict each other
// for exactly the household EmployerCoverageYears exists to describe, and
// ignored HealthcarePerson.MedicareEligibleAge by hardcoding 65.
//
// When the multi-person healthcare model is populated it is authoritative,
// matching GetTotalHealthcareCost — and matching it at the same resolution.
// MedicareStartMonth is month-precise (F-067), and GetMonthlyCostAt starts
// billing the Medicare premium from exactly that month, so this counts from
// exactly that month too. Testing the enclosing year's first month instead
// dropped the surcharge for every remaining month of the transition year,
// while the premium being surcharged was already being billed.
//
// Plans on the legacy single-value model carry no coverage detail and no
// birth month, so they keep the age-65 rule at its native one-year
// resolution; month/12 is the projection year those ages are defined against.
func MedicareEligibleAdultCountAtMonth(s *models.WhatIfSettings, month int) int {
	if s == nil {
		return 0
	}

	if len(s.HealthcarePersons) > 0 {
		count := 0
		for i := range s.HealthcarePersons {
			if month >= s.HealthcarePersons[i].MedicareStartMonth(s.StartDate) {
				count++
			}
		}
		// The surcharge is per filer; a household return covers at most two.
		return min(count, 2)
	}

	year := month / 12
	count := 0
	if s.PrimaryAgeAt(year) >= 65 {
		count++
	}
	if s.HasSpouse() && s.SpouseAgeAt(year) >= 65 {
		count++
	}
	return count
}

// MedicareEligibleAdultCountAtYear is MedicareEligibleAdultCountAtMonth
// evaluated at the first month of the given projection year.
//
// It exists for callers that genuinely have only a year to work with — the
// analysis layer's point-in-time snapshots, which price a single
// representative month rather than accumulating twelve. Anything stepping
// month by month should call MedicareEligibleAdultCountAtMonth directly, or
// it will hold one year's count flat across a mid-year Medicare start.
func MedicareEligibleAdultCountAtYear(s *models.WhatIfSettings, year int) int {
	return MedicareEligibleAdultCountAtMonth(s, year*12)
}

// Age65CountForYear returns the number of filers aged 65 or older in the
// given projection year (0, 1, or 2), which drives the age-65 additional
// standard deduction in GetAdjustedStandardDeduction. A spouse only counts
// when filing jointly — a non-MFJ spouse files a separate return.
//
// F-3: the count is derived here from the ages and filing status the
// projection already knows. TaxCalculator.Age65Count used to be read straight
// off the static TaxConfig.Age65Count JSON field, which has no UI input and
// ships as 0, so the engine dropped the deduction for every saved plan while
// the tax optimizer derived and applied it — leaving the optimizer sizing
// bracket-fill conversions against a larger deduction than the engine then
// used. TaxConfig.Age65Count remains the fallback for callers outside the
// projection, which have no ages to derive from.
func Age65CountForYear(s *models.WhatIfSettings, year int) int {
	if s == nil {
		return 0
	}
	count := 0
	if s.PrimaryAgeAt(year) >= 65 {
		count++
	}
	if s.HasSpouse() && s.SpouseAgeAt(year) >= 65 &&
		s.TaxConfig != nil && NormalizeFilingStatus(s.TaxConfig.FilingStatus) == models.FilingMarriedJoint {
		count++
	}
	return count
}

// PlannerIRMAAInflationFactorForYear inflates the bundled IRMAA MAGI
// thresholds from their CMS 2026 base year to the projection year. The
// thresholds are statutorily CPI-indexed, so this takes the plan's inflation
// rate. Pure math; the offset (irmaaBaseYear − taxBaseYear) keeps the IRMAA
// scaling decoupled from the tax-bracket scaling even though callers express
// both as years-from-tax-base.
//
// Deliberately not floored at zero, unlike GetAdjustedBrackets and
// InflationFactor: a plan year before irmaaBaseYear deflates the 2026 table
// backwards, which is what we want. At 3% the 2025 tier-1 cutoff computes to
// roughly $105.8k/$211.6k against an actual $106k/$212k — closer than pinning
// it at the 2026 figures would be.
func PlannerIRMAAInflationFactorForYear(annualInflationRate float64, yearsFromTaxBase float64) float64 {
	yearsFromIRMAABase := yearsFromTaxBase - float64(irmaaBaseYear-taxBaseYear)
	if yearsFromIRMAABase == 0 {
		return 1
	}
	return math.Pow(1+annualInflationRate/100, yearsFromIRMAABase)
}

// PlannerIRMAASurchargeInflationFactorForYear inflates the IRMAA surcharge
// DOLLARS from the CMS 2026 base year to the projection year.
//
// F-6: surcharge amounts are not CPI-indexed. They are recalculated annually
// from projected Medicare per-capita costs, which have historically grown
// several points faster than CPI. Applying the threshold's CPI factor to the
// surcharge as well understated every future surcharge, and compounded: at 30
// years out the gap is roughly 2x. This deliberately does not take the plan's
// inflation rate — a household assuming lower CPI does not thereby slow
// Medicare cost growth.
func PlannerIRMAASurchargeInflationFactorForYear(yearsFromTaxBase float64) float64 {
	yearsFromIRMAABase := yearsFromTaxBase - float64(irmaaBaseYear-taxBaseYear)
	if yearsFromIRMAABase == 0 {
		return 1
	}
	return math.Pow(1+medicareCostGrowthRate/100, yearsFromIRMAABase)
}

// MonthlyIncomeBreakdown decomposes a month's income into the
// ordinary-income and Social Security buckets the tax accumulator
// needs to estimate per-month tax.
type MonthlyIncomeBreakdown struct {
	OrdinaryIncome       float64
	SocialSecurityIncome float64
	TotalIncome          float64
}

// CalculateMonthlyIncomeBreakdown classifies each income source for
// the given month: manual SS sources are pulled into the SS bucket
// unless the SS optimizer is active (in which case the optimizer's
// ProjectedSocialSecurityIncome value replaces the manual sources).
// hooks supplies the SS-optimizer integration; passing a zero Hooks
// value falls back to manual-sources-only.
func CalculateMonthlyIncomeBreakdown(hooks Hooks, s *models.WhatIfSettings, month int) MonthlyIncomeBreakdown {
	breakdown := MonthlyIncomeBreakdown{}
	useOptimizerSS := hooks.SSActive(s)

	for _, source := range s.IncomeSources {
		amount := source.GetAdjustedAmount(month)
		if amount <= 0 {
			continue
		}
		if IsSocialSecurityIncomeSource(source) {
			if !useOptimizerSS {
				breakdown.SocialSecurityIncome += amount
			}
			continue
		}
		breakdown.OrdinaryIncome += amount
	}

	if useOptimizerSS {
		breakdown.SocialSecurityIncome += hooks.SSIncome(s, month)
	}

	breakdown.TotalIncome = breakdown.OrdinaryIncome + breakdown.SocialSecurityIncome
	return breakdown
}

// RothConversionAmountForYear returns the Roth conversion to apply at
// the year boundary, capped to availableTaxDeferred. Returns 0 when
// the conversion is disabled, the year is outside the configured
// window, or the source balance is zero.
func RothConversionAmountForYear(s *models.WhatIfSettings, currentYear int, availableTaxDeferred float64) float64 {
	if s.RothConversion == nil || !s.RothConversion.Enabled || availableTaxDeferred <= 0 {
		return 0
	}
	if currentYear < s.RothConversion.StartYear {
		return 0
	}
	if s.RothConversion.EndYear != 0 && currentYear > s.RothConversion.EndYear {
		return 0
	}
	amount := s.RothConversion.AnnualAmount
	if override, ok := s.RothConversion.PerYearOverrides[currentYear]; ok {
		amount = override
	}
	return math.Min(amount, availableTaxDeferred)
}

// ShortfallIsTemporaryDueToDelay reports whether a shortfall is
// expected to clear once the tax-deferred-withdrawal delay ends.
// True when the household has a positive shortfall, withdrawals are
// currently blocked, and the tax-deferred bucket still has funds.
func ShortfallIsTemporaryDueToDelay(shortfall float64, allowTaxDeferredWithdrawal bool, taxDeferredBalance float64) bool {
	return shortfall > 0 && !allowTaxDeferredWithdrawal && taxDeferredBalance > 0
}

// ShortfallCausesDepletion reports whether the shortfall is a real
// depletion event (money unavailable, not just delayed).
func ShortfallCausesDepletion(shortfall float64, allowTaxDeferredWithdrawal bool, taxDeferredBalance float64) bool {
	return shortfall > 0 && !ShortfallIsTemporaryDueToDelay(shortfall, allowTaxDeferredWithdrawal, taxDeferredBalance)
}

// AnnualRMDForYear returns the year's required minimum distribution
// for the given tax-deferred balance, applying the calendar-year gate
// (F-078) and IRS Uniform Lifetime divisor lookup. Returns 0 when the
// rule does not yet apply or the tax-deferred bucket is empty.
//
// All three projection loops (canonical, Monte Carlo, backtest) compute
// the annual RMD identically at every year boundary; this helper
// captures that shared rule.
func AnnualRMDForYear(s *models.WhatIfSettings, currentYear int, taxDeferredBalance float64) float64 {
	calendarYear := ParseStartYear(s.StartDate) + currentYear
	if !RMDApplies(s, calendarYear) || taxDeferredBalance <= 0 {
		return 0
	}
	annualRMD, _ := CalculateRMDForYear(s, taxDeferredBalance, calendarYear)
	return annualRMD
}

// MonthlyRMDForMonth returns the monthly RMD withdrawal amount: the
// full annual RMD (capped to the available tax-deferred balance) on
// the user's selected trigger month, zero otherwise.
//
// All three projection loops apply the same trigger-month rule; this
// helper captures the shared math.
func MonthlyRMDForMonth(s *models.WhatIfSettings, monthInYear int, annualRMD, taxDeferredBalance float64) float64 {
	if annualRMD <= 0 || monthInYear != RMDTriggerMonth(s.RMDTiming) {
		return 0
	}
	if annualRMD > taxDeferredBalance {
		return taxDeferredBalance
	}
	return annualRMD
}

// ApplyRothConversionAtYear performs the year-boundary Roth conversion:
// if a conversion is configured and in-window, decrement the tax-
// deferred balance, increment the Roth balance and basis by the same
// amount, and stamp rothFirstFundedYear if blank.
//
// The rothFirstFundedYear pointer holds projection-local state — the
// caller seeds it from s.RothFirstFundedYear and DOES NOT write back
// into s. Persisted settings change only through the settings form.
//
// All three projection loops perform this mutation identically; this
// helper captures the shared in-place update so the loops can shrink
// to a single call per year boundary.
func ApplyRothConversionAtYear(
	s *models.WhatIfSettings,
	currentYear int,
	taxDeferredBalance, rothBalance, rothBasis *float64,
	rothFirstFundedYear *int,
) float64 {
	conversionAmount := RothConversionAmountForYear(s, currentYear, *taxDeferredBalance)
	if conversionAmount <= 0 {
		return 0
	}
	*taxDeferredBalance -= conversionAmount
	*rothBalance += conversionAmount
	*rothBasis += conversionAmount
	if *rothFirstFundedYear <= 0 {
		*rothFirstFundedYear = ParseStartYear(s.StartDate) + currentYear
	}
	return conversionAmount
}

// BigTicketYearResult aggregates a year's big-ticket draws so the
// caller can fold the unfunded expense into the month's expense total
// and route the taxable Roth earnings (if the clock is unsatisfied)
// into that month's tax snapshot.
type BigTicketYearResult struct {
	UnfundedExpense        float64
	RothBasisWithdrawal    float64
	RothEarningsWithdrawal float64
}

// ApplyBigTicketItemsForYear processes every big-ticket item scheduled
// for currentYear: income items add cash to the taxable account,
// expense items are funded via the canonical waterfall, and the
// aggregated unfunded-expense plus Roth split are returned so the
// monthly loop can feed taxable Roth earnings into the tax snapshot.
func ApplyBigTicketItemsForYear(s *models.WhatIfSettings, currentYear int, allowTaxDeferredWithdrawal bool, penaltyRate float64, taxDeferredBalance *float64, taxableAccount *TaxableAccountState, rothBalance, rothBasis *float64) BigTicketYearResult {
	out := BigTicketYearResult{}
	for _, item := range s.BigTicketItems {
		if item.Year != currentYear {
			continue
		}
		if item.Type == models.BigTicketIncome {
			taxableAccount.AddCash(item.Amount)
			continue
		}
		r := ApplyBigTicketExpenseWithTaxableState(item.Amount, allowTaxDeferredWithdrawal, penaltyRate, taxDeferredBalance, taxableAccount, rothBalance, rothBasis)
		out.UnfundedExpense += r.UnfundedExpense
		out.RothBasisWithdrawal += r.RothBasisWithdrawal
		out.RothEarningsWithdrawal += r.RothEarningsWithdrawal
	}
	return out
}

// RothQualifiedDistributionClockSatisfied reports whether the Roth IRA
// 5-tax-year aging requirement is met for the given calendar year. A
// firstFundedYear of 0 or less means unset and the clock is considered
// not satisfied (the conservative projection default). calendarYear is
// a calendar tax year, not a projection-year offset; callers translate
// projection year via ParseStartYear(s.StartDate)+projectionYear.
func RothQualifiedDistributionClockSatisfied(firstFundedYear, calendarYear int) bool {
	if firstFundedYear <= 0 {
		return false
	}
	return calendarYear >= firstFundedYear+5
}

// ApplyTaxStateMonth folds a single month's portfolio result into the
// running ProjectionTaxAccumulator. Captures the long positional
// argument list that all three projection loops would otherwise repeat
// verbatim, so future signature changes touch one place.
//
// TaxableRothEarnings is added to ordinary income so MAGI-sensitive
// calculations (IRMAA, NIIT thresholds) agree with the converged
// monthly tax snapshot.
func ApplyTaxStateMonth(taxState *ProjectionTaxAccumulator, incomeBreakdown MonthlyIncomeBreakdown, monthResult TaxAwarePortfolioMonthResult, rothConversionThisMonth float64) {
	taxState.ApplyMonth(RealizedMonthIncome{
		OrdinaryIncome:        incomeBreakdown.OrdinaryIncome + monthResult.TaxableNonQualifiedDividends + monthResult.TaxableRothEarnings,
		SocialSecurityIncome:  incomeBreakdown.SocialSecurityIncome,
		TaxableWithdrawals:    monthResult.CashFlow.WithdrawalFromTaxDeferred,
		QualifiedDividends:    monthResult.TaxableQualifiedDividends,
		LongTermCapitalGains:  monthResult.TaxableCapitalGains,
		NonQualifiedDividends: monthResult.TaxableNonQualifiedDividends,
		RothConversions:       rothConversionThisMonth,
		TaxesPaid:             monthResult.TaxesPaid,
	})
}

// MarketplaceStatusAtYear reports whether anyone in the household is on a
// marketplace plan in a given projection year, and whether the household is
// barred from the premium tax credit regardless of income.
//
// Enrolment is checked mid-year rather than at January, because coverage
// changes on a birthday: someone turning 65 in March is on a marketplace plan
// for a quarter of the year and Medicare for the rest, and treating January as
// the whole story would either invent or erase a cliff for that year.
//
// Disqualification is reported only when NOBODY is on a marketplace plan and
// somebody is on COBRA or employer coverage. Eligibility is individual, so one
// spouse holding COBRA does not forfeit the other's credit; a household where
// the only pre-Medicare coverage is COBRA has no credit to lose at all, and
// that is worth saying rather than silently registering no cliff.
func MarketplaceStatusAtYear(s *models.WhatIfSettings, projectionYear int) (enrolled bool, disqualified bool) {
	if s == nil {
		return false, false
	}
	month := projectionYear*12 + 6

	sawDisqualifying := false
	for i := range s.HealthcarePersons {
		switch s.HealthcarePersons[i].CoverageAt(month, s.StartDate) {
		case models.CoverageACA:
			enrolled = true
		case models.CoverageCOBRA, models.CoverageEmployer:
			// Medicare also bars the marketplace credit, but someone on
			// Medicare has simply aged out rather than forfeited anything —
			// that is not a finding worth raising.
			sawDisqualifying = true
		}
	}
	if enrolled {
		return true, false
	}
	return false, sawDisqualifying
}
