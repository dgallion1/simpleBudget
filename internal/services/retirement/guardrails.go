package retirement

import (
	"budget2/internal/models"
	"math"
)

// guardrailState tracks portfolio peaks and spending adjustments for a simple
// portfolio-drop/rise guardrail strategy. This is NOT the four-rule
// Guyton & Klinger (2006) model (which fires on withdrawal-rate thresholds,
// not portfolio-value thresholds, and includes an Inflation Rule and a
// Withdrawal Rule). Full G-K implementation is tracked at
// docs/superpowers/specs/2026-05-06-full-gk-guardrails-followup.md.
type guardrailState struct {
	peakPortfolio    float64
	initialPortfolio float64
	spendingMult     float64 // starts at 1.0
}

// newGuardrailState creates a new guardrail state with the given initial
// portfolio value. Peak is set to initial and spending multiplier starts at 1.0.
func newGuardrailState(initialPortfolio float64) *guardrailState {
	return &guardrailState{
		peakPortfolio:    initialPortfolio,
		initialPortfolio: initialPortfolio,
		spendingMult:     1.0,
	}
}

// evaluate checks guardrail triggers and returns the updated spending multiplier.
// Floor is checked first; if it triggers, the ceiling check is skipped.
func (g *guardrailState) evaluate(cfg *models.GuardrailConfig, currentPortfolio float64) float64 {
	// Update peak if current exceeds it.
	if currentPortfolio > g.peakPortfolio {
		g.peakPortfolio = currentPortfolio
	}

	floorTriggered := false

	// Check floor: portfolio has dropped FloorDropPct% from peak.
	if g.peakPortfolio > 0 {
		dropPct := (g.peakPortfolio - currentPortfolio) / g.peakPortfolio * 100
		if dropPct >= cfg.FloorDropPct {
			g.spendingMult *= (1 - cfg.FloorCutPct/100)
			g.peakPortfolio = currentPortfolio
			floorTriggered = true
		}
	}

	// Check ceiling: portfolio has risen CeilingRisePct% above initial.
	// Skipped if floor already triggered this evaluation.
	if !floorTriggered && g.initialPortfolio > 0 {
		risePct := (currentPortfolio - g.initialPortfolio) / g.initialPortfolio * 100
		if risePct >= cfg.CeilingRisePct {
			g.spendingMult *= (1 + cfg.CeilingRaisePct/100)
			g.initialPortfolio = currentPortfolio
		}
	}

	// Clamp multiplier to configured bounds.
	minMult := cfg.MinSpendingPct / 100
	maxMult := cfg.MaxSpendingPct / 100
	g.spendingMult = math.Max(minMult, math.Min(maxMult, g.spendingMult))

	return g.spendingMult
}

// multiplier returns the current spending multiplier.
func (g *guardrailState) multiplier() float64 {
	return g.spendingMult
}
