package engine

import (
	"budget2/internal/models"
)

// TotalIncome returns total income for a specific month.
func TotalIncome(s *models.WhatIfSettings, month int) float64 {
	total := 0.0
	for _, source := range s.IncomeSources {
		total += source.GetAdjustedAmount(month)
	}
	return total
}
