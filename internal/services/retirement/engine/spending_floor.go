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
//
// primary must be the VIEWED (primary) scenario's settings, per D3' — every
// call site in stepper.go passes st.primary, never the active step's
// settings, so a chained step's own MinMonthlySpendingReal (even when that
// step also has guardrails enabled) is never consulted.
func floorAdjustedLiving(primary *models.WhatIfSettings, planned, multiplier, cpi float64) (float64, float64) {
	adjusted := planned * multiplier
	if GuardrailsGovernStep(primary) && primary.Guardrails.MinMonthlySpendingReal > 0 {
		adjusted = math.Max(adjusted, primary.Guardrails.MinMonthlySpendingReal*cpi)
		if planned > 0 {
			multiplier = adjusted / planned
		}
	}
	return adjusted, multiplier
}
