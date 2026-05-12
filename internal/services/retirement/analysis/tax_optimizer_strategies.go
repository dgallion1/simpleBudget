package analysis

import (
	"fmt"

	"budget2/internal/models"
)

// Constants tuned per design spec. Single block for easy adjustment.
var (
	taxOptimizerLadderAmounts = []float64{0, 25_000, 50_000, 75_000, 100_000, 150_000, 200_000}
	// taxOptimizerBracketFillTargets is used by the bracket-fill family
	// enumerated in the next task. Declared here to keep all optimizer
	// constants co-located.
	//lint:ignore U1000 consumed by Task 4 bracket-fill enumeration
	taxOptimizerBracketFillTargets = []float64{0.12, 0.22, 0.24}
)

// strategyWindow names a (startAge, endAge) anchor used for both ladder
// and bracket-fill enumeration. Centralizes the anchor points so labels
// stay consistent across families.
type strategyWindow struct {
	StartAge int
	EndAge   int
	Anchor   string // human-readable end-anchor: "5yr", "SS", "IRMAA", "RMD", "mid"
}

// strategyWindows returns the windows that are valid for s
// (i.e. EndAge > StartAge given CurrentAge and the user's SS claim age).
// Order is stable for test assertions.
func strategyWindows(s *models.WhatIfSettings) []strategyWindow {
	a := s.CurrentAge
	var ssClaim int
	if s.SocialSecurity != nil {
		ssClaim = s.SocialSecurity.ClaimAge
	}
	candidates := []strategyWindow{
		{a, a + 5, "5yr"},
		{a, ssClaim, "SS"},
		{a, 65, "IRMAA"},
		{a, 73, "RMD"},
		{a + 5, a + 10, "mid"},
	}
	out := make([]strategyWindow, 0, len(candidates))
	for _, w := range candidates {
		if w.EndAge > w.StartAge {
			out = append(out, w)
		}
	}
	return out
}

// enumerateLadderStrategies returns the ladder family of Roth
// conversion strategies: cross-product of taxOptimizerLadderAmounts and
// strategyWindows(s), with zero-amount duplicates collapsed to a single
// "No conversion" baseline candidate.
func enumerateLadderStrategies(s *models.WhatIfSettings) []models.RothOptimizerStrategy {
	windows := strategyWindows(s)
	out := make([]models.RothOptimizerStrategy, 0, len(windows)*len(taxOptimizerLadderAmounts))

	zeroEmitted := false
	for _, amount := range taxOptimizerLadderAmounts {
		if amount == 0 {
			if zeroEmitted || len(windows) == 0 {
				continue
			}
			zeroEmitted = true
			// Emit a single representative "no-conversion" baseline.
			w := windows[0]
			out = append(out, models.RothOptimizerStrategy{
				Kind:     models.RothStrategyLadder,
				StartAge: w.StartAge,
				EndAge:   w.EndAge,
				Label:    "No conversion",
			})
			continue
		}
		for _, w := range windows {
			out = append(out, models.RothOptimizerStrategy{
				Kind:         models.RothStrategyLadder,
				AnnualAmount: amount,
				StartAge:     w.StartAge,
				EndAge:       w.EndAge,
				Label:        formatLadderLabel(amount, w),
			})
		}
	}
	return out
}

func formatLadderLabel(amount float64, w strategyWindow) string {
	if amount == 0 {
		return "No conversion"
	}
	return fmt.Sprintf("$%dk/yr %d→%d", int(amount/1000), w.StartAge, w.EndAge)
}
