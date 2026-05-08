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

// MedicareEligibleAdultCountAtYear returns the count (0, 1, or 2) of
// household adults that are 65+ in the given projection year. Used
// to scale the per-person IRMAA surcharge to a household total.
func MedicareEligibleAdultCountAtYear(s *models.WhatIfSettings, year int) int {
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

// PlannerIRMAAInflationFactorForYear inflates the bundled IRMAA
// surcharge brackets from their CMS 2026 base year to the projection
// year. Pure math; the offset (irmaaBaseYear − taxBaseYear) keeps the
// IRMAA scaling decoupled from the tax-bracket scaling even though
// callers express both as years-from-tax-base.
func PlannerIRMAAInflationFactorForYear(annualInflationRate float64, yearsFromTaxBase float64) float64 {
	yearsFromIRMAABase := yearsFromTaxBase - float64(irmaaBaseYear-taxBaseYear)
	if yearsFromIRMAABase == 0 {
		return 1
	}
	return math.Pow(1+annualInflationRate/100, yearsFromIRMAABase)
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
	return math.Min(s.RothConversion.AnnualAmount, availableTaxDeferred)
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
	annualRMD, _ := CalculateRMD(taxDeferredBalance, RMDAgeForCalendarYear(s, calendarYear))
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
// deferred balance and increment the Roth balance by the same amount.
// Returns the conversion amount applied (0 when no conversion ran).
//
// All three projection loops perform this mutation identically; this
// helper captures the shared in-place update so the loops can shrink
// to a single call per year boundary.
func ApplyRothConversionAtYear(s *models.WhatIfSettings, currentYear int, taxDeferredBalance, rothBalance *float64) float64 {
	conversionAmount := RothConversionAmountForYear(s, currentYear, *taxDeferredBalance)
	if conversionAmount <= 0 {
		return 0
	}
	*taxDeferredBalance -= conversionAmount
	*rothBalance += conversionAmount
	return conversionAmount
}

// ApplyBigTicketItemsForYear processes every big-ticket item scheduled
// for currentYear: income items add cash to the taxable account, and
// expense items are funded via the canonical waterfall (taxable →
// Roth → tax-deferred when allowed). Returns the residual expense that
// could not be funded — added to the month's expense total by the
// caller so the depletion path runs when needed.
//
// All three projection loops walk s.BigTicketItems with identical
// semantics; this helper captures the shared loop body.
func ApplyBigTicketItemsForYear(s *models.WhatIfSettings, currentYear int, allowTaxDeferredWithdrawal bool, penaltyRate float64, taxDeferredBalance *float64, taxableAccount *TaxableAccountState, rothBalance *float64) float64 {
	bigTicketExpenseThisMonth := 0.0
	for _, item := range s.BigTicketItems {
		if item.Year != currentYear {
			continue
		}
		if item.Type == models.BigTicketIncome {
			taxableAccount.AddCash(item.Amount)
			continue
		}
		remaining := ApplyBigTicketExpenseWithTaxableState(item.Amount, allowTaxDeferredWithdrawal, penaltyRate, taxDeferredBalance, taxableAccount, rothBalance)
		bigTicketExpenseThisMonth += remaining
	}
	return bigTicketExpenseThisMonth
}

// ApplyTaxStateMonth folds a single month's portfolio result into the
// running ProjectionTaxAccumulator. Captures the long positional
// argument list that all three projection loops would otherwise repeat
// verbatim, so future signature changes touch one place.
func ApplyTaxStateMonth(taxState *ProjectionTaxAccumulator, incomeBreakdown MonthlyIncomeBreakdown, monthResult TaxAwarePortfolioMonthResult, rothConversionThisMonth float64) {
	taxState.ApplyMonth(
		incomeBreakdown.OrdinaryIncome+monthResult.TaxableNonQualifiedDividends,
		incomeBreakdown.SocialSecurityIncome,
		monthResult.CashFlow.WithdrawalFromTaxDeferred,
		monthResult.TaxableQualifiedDividends,
		monthResult.TaxableCapitalGains,
		monthResult.TaxableNonQualifiedDividends,
		rothConversionThisMonth,
		monthResult.TaxesPaid,
	)
}
