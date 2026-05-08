package engine

import (
	"budget2/internal/models"
)

// ExpenseBreakdown holds categorized expenses for adaptive spending analysis.
type ExpenseBreakdown struct {
	Essential     float64 // Non-discretionary expenses (cannot be reduced)
	Discretionary float64 // Discretionary expenses (can be reduced during downturns)
	Total         float64 // Total = Essential + Discretionary
}

// livingExpensesAtMonth is the engine-local mirror of the retirement
// package's calculateLivingExpensesAtMonth. Copied during the migration
// window so this package has no import cycle back to retirement.
func livingExpensesAtMonth(s *models.WhatIfSettings, month int) float64 {
	years := month / 12
	phaseAge := s.GetPhaseReferenceAge(years)
	monthsElapsed := float64(month)

	if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
		return s.MonthlyLivingExpenses * s.GetSpendingMultiplier(phaseAge) * compoundedFactorFromPercent(s.InflationRate, monthsElapsed)
	}

	return s.MonthlyLivingExpenses * compoundedFactorFromPercent(s.InflationRate-s.SpendingDeclineRate, monthsElapsed)
}

// totalExpenses returns total expenses for a specific month.
func totalExpenses(s *models.WhatIfSettings, month int) float64 {
	years := month / 12
	phaseAge := s.GetPhaseReferenceAge(years) // Age used for spending phase calculations (may differ for couples)

	// Calculate living expenses based on spending model
	livingExpenses := livingExpensesAtMonth(s, month)

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

// expenseBreakdown separates expenses into discretionary and essential.
func expenseBreakdown(s *models.WhatIfSettings, month int) ExpenseBreakdown {
	years := month / 12
	phaseAge := s.GetPhaseReferenceAge(years) // Age used for spending phase calculations (may differ for couples)

	// Base living expenses are treated as essential (conservative approach)
	livingExpenses := livingExpensesAtMonth(s, month)

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

// TotalExpensesForCalculator is a parity-window export so Calculator's
// delegator can call into engine. Removed in Task 8.
func TotalExpensesForCalculator(s *models.WhatIfSettings, month int) float64 {
	return totalExpenses(s, month)
}

// ExpenseBreakdownForCalculator is a parity-window export so
// Calculator's delegator can call into engine. Removed in Task 8.
func ExpenseBreakdownForCalculator(s *models.WhatIfSettings, month int) ExpenseBreakdown {
	return expenseBreakdown(s, month)
}
