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
func CalculateMonthlyIncomeBreakdown(s *models.WhatIfSettings, month int) MonthlyIncomeBreakdown {
	breakdown := MonthlyIncomeBreakdown{}
	useOptimizerSS := SocialSecurityProjectionActive(s)

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
		breakdown.SocialSecurityIncome += ProjectedSocialSecurityIncome(s, month)
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
