package retirement

import (
	"fmt"
	"math"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// PreparedChainLink is re-exported from the engine package so existing
// retirement-package code (tests, helpers) keeps using the local name.
type PreparedChainLink = engine.PreparedChainLink

// ExpenseBreakdown is re-exported from the engine package.
type ExpenseBreakdown = engine.ExpenseBreakdown

// PresentValue calculates the present value of a future cash flow.
// PV = FV / (1 + r)^n.
func PresentValue(futureValue, annualRate float64, periods int) float64 {
	if periods <= 0 {
		return futureValue
	}
	if annualRate <= 0 {
		return futureValue
	}
	monthlyRate := engine.MonthlyCompoundFactorFromDecimal(annualRate/100) - 1
	return futureValue / math.Pow(1+monthlyRate, float64(periods))
}

// IsSocialSecurityIncomeSource exposes the SS income-source detection
// rule for use in templates that need to flag manual SS sources excluded
// by the optimizer. Forwards to the engine implementation.
func IsSocialSecurityIncomeSource(source models.IncomeSource) bool {
	return engine.IsSocialSecurityIncomeSource(source)
}

// Engine-side helpers kept reachable from retirement-package tests
// without forcing each test to import engine.
var (
	rebaseLivingExpensesAtTransition      = engine.RebaseLivingExpensesAtTransition
	medicareEligibleAdultCountAtYear      = engine.MedicareEligibleAdultCountAtYear
	plannerIRMAAInflationFactorForYear    = engine.PlannerIRMAAInflationFactorForYear
	isSocialSecurityIncomeSource          = engine.IsSocialSecurityIncomeSource
	rothConversionAmountForYear           = engine.RothConversionAmountForYear
	newTaxableAccountState                = engine.NewTaxableAccountState
	buildTaxableReturnComponents          = engine.BuildTaxableReturnComponents
	reinvestRequiredRMDToTaxableState     = engine.ReinvestRequiredRMDToTaxableState
	applyBigTicketExpenseWithTaxableState = engine.ApplyBigTicketExpenseWithTaxableState
	withdrawForExpenses                   = engine.WithdrawForExpenses
)

// calculateMonthlyIncomeBreakdown is the retirement-package shim over
// engine.CalculateMonthlyIncomeBreakdown that auto-supplies DefaultHooks
// so existing retirement-package tests calling the lowercase form
// continue to exercise the production SS-optimizer integration without
// every callsite having to thread Hooks explicitly.
func calculateMonthlyIncomeBreakdown(s *models.WhatIfSettings, month int) engine.MonthlyIncomeBreakdown {
	return engine.CalculateMonthlyIncomeBreakdown(DefaultHooks(), s, month)
}

// Engine-side type aliases used by retirement-package tests.
type (
	projectionTaxAccumulator = engine.ProjectionTaxAccumulator
	projectedTaxSnapshot     = engine.ProjectedTaxSnapshot
	taxableReturnComponents  = engine.TaxableReturnComponents
	taxableGrowthResult      = engine.TaxableGrowthResult
	taxableAccountState      = engine.TaxableAccountState
)

// expectedTaxableMonthlyCashFlow is a thin wrapper consumed by retirement
// projection helpers and tests.
func expectedTaxableMonthlyCashFlow(s *models.WhatIfSettings, taxableMarketValue, taxableAnnualReturn float64) taxableGrowthResult {
	account := newTaxableAccountState(s, taxableMarketValue)
	return account.ApplyGrowth(buildTaxableReturnComponents(taxableAnnualReturn, s), 1.0)
}

// applyBigTicketExpense (operating on raw *float64 balances rather than
// the taxable-account state machine) is retained for tests that exercise
// it directly.
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

// formatBucketLabel formats a bucket range for display; retained for
// tests that exercise the helper directly.
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

// mean computes the arithmetic mean of a slice; retained for tests.
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
