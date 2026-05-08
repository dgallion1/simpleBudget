package engine

import (
	"budget2/internal/models"
)

// totalIncome returns total income for a specific month.
func totalIncome(s *models.WhatIfSettings, month int) float64 {
	total := 0.0
	for _, source := range s.IncomeSources {
		total += source.GetAdjustedAmount(month)
	}
	return total
}

// TotalIncomeForCalculator is a parity-window export so Calculator's
// delegator can call into engine. Removed in Task 8.
func TotalIncomeForCalculator(s *models.WhatIfSettings, month int) float64 {
	return totalIncome(s, month)
}
