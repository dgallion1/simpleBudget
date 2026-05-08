package engine

import (
	"math"

	"budget2/internal/models"
)

// GuardrailState tracks portfolio peaks and spending adjustments for a
// simple portfolio-drop/rise guardrail strategy. This is NOT the
// four-rule Guyton & Klinger (2006) model (which fires on
// withdrawal-rate thresholds, not portfolio-value thresholds, and
// includes an Inflation Rule and a Withdrawal Rule). Full G-K
// implementation is tracked at
// docs/superpowers/specs/2026-05-06-full-gk-guardrails-followup.md.
type GuardrailState struct {
	PeakPortfolio    float64
	InitialPortfolio float64
	SpendingMult     float64 // starts at 1.0
}

// NewGuardrailState creates a new guardrail state with the given initial
// portfolio value. Peak is set to initial and spending multiplier
// starts at 1.0.
func NewGuardrailState(initialPortfolio float64) *GuardrailState {
	return &GuardrailState{
		PeakPortfolio:    initialPortfolio,
		InitialPortfolio: initialPortfolio,
		SpendingMult:     1.0,
	}
}

// Evaluate checks guardrail triggers and returns the updated spending
// multiplier. Floor is checked first; if it triggers, the ceiling check
// is skipped.
func (g *GuardrailState) Evaluate(cfg *models.GuardrailConfig, currentPortfolio float64) float64 {
	// Update peak if current exceeds it.
	if currentPortfolio > g.PeakPortfolio {
		g.PeakPortfolio = currentPortfolio
	}

	floorTriggered := false

	// Check floor: portfolio has dropped FloorDropPct% from peak.
	if g.PeakPortfolio > 0 {
		dropPct := (g.PeakPortfolio - currentPortfolio) / g.PeakPortfolio * 100
		if dropPct >= cfg.FloorDropPct {
			g.SpendingMult *= (1 - cfg.FloorCutPct/100)
			g.PeakPortfolio = currentPortfolio
			floorTriggered = true
		}
	}

	// Check ceiling: portfolio has risen CeilingRisePct% above initial.
	// Skipped if floor already triggered this evaluation.
	if !floorTriggered && g.InitialPortfolio > 0 {
		risePct := (currentPortfolio - g.InitialPortfolio) / g.InitialPortfolio * 100
		if risePct >= cfg.CeilingRisePct {
			g.SpendingMult *= (1 + cfg.CeilingRaisePct/100)
			g.InitialPortfolio = currentPortfolio
		}
	}

	// Clamp multiplier to configured bounds.
	minMult := cfg.MinSpendingPct / 100
	maxMult := cfg.MaxSpendingPct / 100
	g.SpendingMult = math.Max(minMult, math.Min(maxMult, g.SpendingMult))

	return g.SpendingMult
}

// Multiplier returns the current spending multiplier.
func (g *GuardrailState) Multiplier() float64 {
	return g.SpendingMult
}
