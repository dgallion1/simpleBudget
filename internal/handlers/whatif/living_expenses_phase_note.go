package whatif

import (
	"fmt"
	"strconv"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// livingExpensesPhaseNote is the view model for the informational note
// rendered under the What-If "Monthly Living Expenses" slider caption in
// portfolio-settings.html. On its own, the raw slider figure says nothing
// about the spending-phase multiplier the engine actually applies to it
// (e.g. Go-Go ×1.1 today, dropping to ×0.9 at a later age) — this note makes
// that visible next to the control the user is looking at.
type livingExpensesPhaseNote struct {
	// Show gates the whole note: false when phases are disabled or the
	// phase active this month has a 1.0 multiplier (nothing to explain).
	Show bool

	// PhaseName is the label of the phase active at month 0 (e.g. "Go-Go").
	PhaseName string

	// Multiplier is the raw current-phase multiplier. Rendered into a data
	// attribute so client JS can recompute the dollar figure during a drag
	// without a server round-trip.
	Multiplier float64

	// MultiplierText is Multiplier formatted for display with no trailing
	// zeros: 1.1, 0.9, 0.95, etc.
	MultiplierText string

	// EngineMonth0 is engine.LivingExpensesAtMonth(settings, 0): the actual
	// dollar amount the engine spends this month, i.e. the saved setting
	// times the current-phase multiplier (month-0 inflation is always a 1.0
	// factor, so no other adjustment applies here).
	EngineMonth0 float64

	// NextClause is "×<mult> from age <age>" describing the next later
	// phase transition (if any), or "" when the current phase is the last
	// one configured. Callers should omit the leading "; " when NextClause
	// is empty.
	NextClause string
}

// buildLivingExpensesPhaseNote derives the phase-note view model from the
// settings' spending-phase configuration, reusing the same exported
// WhatIfSettings accessors (GetPhaseReferenceAge, GetSpendingMultiplier) the
// engine itself uses, so the phase-age-reference rule (older/younger/
// primary/spouse) always matches what was actually projected.
func buildLivingExpensesPhaseNote(s *models.WhatIfSettings) livingExpensesPhaseNote {
	if s == nil {
		return livingExpensesPhaseNote{}
	}
	cfg := s.SpendingPhaseConfig
	if cfg == nil || !cfg.Enabled || len(cfg.Phases) == 0 {
		return livingExpensesPhaseNote{}
	}

	age0 := s.GetPhaseReferenceAge(0)
	multiplier := s.GetSpendingMultiplier(age0)
	if multiplier == 1.0 {
		return livingExpensesPhaseNote{}
	}

	note := livingExpensesPhaseNote{
		Show:           true,
		PhaseName:      trajectoryPhaseName(s, 0),
		Multiplier:     multiplier,
		MultiplierText: formatPhaseMultiplier(multiplier),
		EngineMonth0:   engine.LivingExpensesAtMonth(s, 0),
	}

	// Next transition: the configured phase with the smallest StartAge that
	// still exceeds the current reference age. Scans rather than assuming
	// Phases is sorted (GetSpendingMultiplier/trajectoryPhaseName do assume
	// ascending order, but there is no enforced invariant), so a
	// mis-ordered config still finds the nearest later transition.
	nextIdx := -1
	for i, p := range cfg.Phases {
		if p.StartAge <= age0 {
			continue
		}
		if nextIdx == -1 || p.StartAge < cfg.Phases[nextIdx].StartAge {
			nextIdx = i
		}
	}
	if nextIdx >= 0 {
		next := cfg.Phases[nextIdx]
		note.NextClause = fmt.Sprintf("×%s from age %d", formatPhaseMultiplier(next.Multiplier), next.StartAge)
	}

	return note
}

// formatPhaseMultiplier renders a spending-phase multiplier for display
// with no trailing zeros: 1.1, 0.9, 0.95, etc.
func formatPhaseMultiplier(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
