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
	return fmt.Sprintf("$%dk/yr %d→%d", int(amount/1000), w.StartAge, w.EndAge)
}

// estimateOtherTaxableIncome returns a closed-form estimate of taxable
// income (excluding the Roth conversion itself) at the given
// projection-year offset. Used by bracket-fill candidate
// pre-computation. Approximate by design — the optimizer ranks on the
// engine's actual projection result, not on this estimate.
//
// Includes: SS benefit (gross, when claimed) + ordinary fixed-income
// sources from settings + estimated RMD when age >= 73 + a small
// taxable-account dividend estimate.
func estimateOtherTaxableIncome(s *models.WhatIfSettings, projectionYear int) float64 {
	if s == nil {
		return 0
	}
	age := s.CurrentAge + projectionYear
	total := 0.0

	// Social Security.
	if s.SocialSecurity != nil && s.SocialSecurity.ClaimAge > 0 && age >= s.SocialSecurity.ClaimAge {
		cola := s.SocialSecurity.COLARate
		if cola == 0 {
			cola = 0.02
		}
		yearsSinceClaim := age - s.SocialSecurity.ClaimAge
		monthly := s.SocialSecurity.FRABenefit
		for i := 0; i < yearsSinceClaim; i++ {
			monthly *= 1 + cola
		}
		total += monthly * 12
	}

	// Ordinary income sources (fixed-amount monthly). The engine has
	// richer logic; this estimator only needs to be directionally
	// correct.
	for _, src := range s.IncomeSources {
		if src.Type != models.IncomeFixed {
			continue
		}
		currentMonth := projectionYear * 12
		if currentMonth < src.StartMonth {
			continue
		}
		if src.EndMonth != nil && currentMonth >= *src.EndMonth {
			continue
		}
		total += src.Amount * 12
	}

	// RMD: rough estimate. After age 73 use ~4% of estimated tax-deferred
	// balance (IRS Uniform Lifetime factor at age 73 = 26.5 → ~3.77%).
	if age >= 73 {
		taxDeferredNow := s.PortfolioValue * (s.TaxDeferredPercent / 100.0)
		rate := s.InvestmentReturn / 100.0
		if rate <= 0 {
			rate = 0.06
		}
		balance := taxDeferredNow
		for i := 0; i < projectionYear; i++ {
			balance *= 1 + rate
		}
		total += balance * 0.04
	}

	// Taxable-account qualified dividends.
	taxableNow := s.PortfolioValue * (1.0 - s.TaxDeferredPercent/100.0 - s.RothPercent/100.0)
	if s.TaxableDividendYield > 0 {
		total += taxableNow * (s.TaxableDividendYield / 100.0)
	}

	return total
}
