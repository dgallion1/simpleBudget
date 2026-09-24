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

// GuardrailsGovernStep is the ONE predicate for "do guardrails currently
// govern spending" — per D3', evaluated on the VIEWED (primary) scenario's
// settings, NEVER the active step's. After a scenario-chain transition, the
// primary's guardrail setting governs the WHOLE chain; a chained step's own
// guardrail config is not read, not merged, and not used as a fallback —
// even when that step turns guardrails on (or off) with a config of its
// own. Every call site — the stepper's guardrail evaluation and trigger
// derivation, the spending floor (floorAdjustedLiving), and Monte Carlo's
// adaptive-spending suppression — must pass the PRIMARY settings (the
// stepper's st.primary; Monte Carlo reaches it via
// ProjectionState.Primary(), never the returnsFor closure's active-settings
// parameter) and must source the guardrail CONFIG (thresholds, percentages,
// MinMonthlySpendingReal) from that SAME primary settings object, not from
// whichever settings happen to be active that month.
//
// D3' replaces the earlier per-step D3 (WS3 attempts 1-2), which evaluated
// this predicate — and the guardrail config itself — on the ACTIVE step.
// That produced a second, disagreeing classifier: the projection chart's
// trigger lines and summary caption were already gated on the primary
// settings (they render from whatever LoadContext() returns, the viewed
// scenario), while the underlying per-year figures came from whichever step
// was active, so an on->off chain drew a fabricated "cut at $0.00" line and
// an off->on chain drew none at all despite guardrails genuinely acting
// (ruling 2026-09-23g, WS3 attempt-2 hard stop). Deciding everything from
// the ONE settings object every UI surface already reads removes that
// second classifier instead of reconciling it.
func GuardrailsGovernStep(s *models.WhatIfSettings) bool {
	return s != nil && s.Guardrails != nil && s.Guardrails.Enabled
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
