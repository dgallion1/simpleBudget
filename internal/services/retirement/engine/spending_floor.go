package engine

import (
	"budget2/internal/models"
	"math"
)

// RoundLivingCents is the shared half-away-from-zero convention for monthly real spending.
func RoundLivingCents(amount float64) float64 { return math.Round(amount*100) / 100 }

// FundedLiving conservatively funds other obligations before living expenses.
func FundedLiving(adjusted, shortfall float64) float64 {
	return math.Max(0, adjusted-math.Max(0, shortfall))
}

// floorAdjustedLiving applies the real floor after phase/decline adjustments.
// The effective multiplier is for consumers; the policy retains its own state.
func floorAdjustedLiving(s *models.WhatIfSettings, planned, multiplier, cpi float64) (float64, float64) {
	adjusted := planned * multiplier
	if s.Guardrails != nil && s.Guardrails.Enabled && s.Guardrails.MinMonthlySpendingReal > 0 {
		adjusted = math.Max(adjusted, s.Guardrails.MinMonthlySpendingReal*cpi)
		if planned > 0 {
			multiplier = adjusted / planned
		}
	}
	return adjusted, multiplier
}
