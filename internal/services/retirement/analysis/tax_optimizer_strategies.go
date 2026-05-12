package analysis

import (
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// Constants tuned per design spec. Single block for easy adjustment.
var (
	taxOptimizerLadderAmounts = []float64{0, 25_000, 50_000, 75_000, 100_000, 150_000, 200_000}
	// taxOptimizerBracketFillTargets lists the marginal brackets targeted
	// by the bracket-fill enumeration family.
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
		// Require a window of at least 2 years. A 1-year window
		// (endProjYear=1) translates to RothConversionConfig.EndYear=0,
		// which the engine treats as "indefinite" — wrecking the candidate
		// score. See engine/loop_helpers.go:133.
		if w.EndAge-w.StartAge >= 2 {
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
// Includes: primary SS benefit (gross, when claimed) + spouse SS
// benefit when filing MFJ and spouse has claimed + ordinary fixed-income
// sources from settings + estimated RMD when age >= 73 + a small
// taxable-account dividend estimate.
func estimateOtherTaxableIncome(s *models.WhatIfSettings, projectionYear int) float64 {
	if s == nil {
		return 0
	}
	age := s.CurrentAge + projectionYear
	total := 0.0

	// Social Security — primary.
	if s.SocialSecurity != nil && s.SocialSecurity.ClaimAge > 0 && age >= s.SocialSecurity.ClaimAge {
		cola := SSConfigCOLARate(s)
		yearsSinceClaim := age - s.SocialSecurity.ClaimAge
		monthly := AdjustedSSBenefit(
			s.SocialSecurity.FRABenefit,
			NormalizedSSFRA(s.SocialSecurity.FRA),
			s.SocialSecurity.ClaimAge,
		)
		for i := 0; i < yearsSinceClaim; i++ {
			monthly *= 1 + cola
		}
		total += monthly * 12
	}

	// Social Security — spouse. Only contributes to non-conversion taxable
	// income for joint filers; non-MFJ spouses report on a separate return
	// and don't consume bracket-fill room for this taxpayer. Spouse age
	// can differ from primary age, so compute it independently rather
	// than reusing `age`.
	if s.HasSpouse() &&
		s.TaxConfig != nil && s.TaxConfig.FilingStatus == models.FilingMarriedJoint &&
		s.SocialSecurity != nil &&
		s.SocialSecurity.SpouseClaimAge > 0 &&
		s.SocialSecurity.SpouseFRABenefit > 0 {
		spouseAge := s.SpouseAge + projectionYear
		if spouseAge >= s.SocialSecurity.SpouseClaimAge {
			cola := SSConfigCOLARate(s)
			yearsSinceClaim := spouseAge - s.SocialSecurity.SpouseClaimAge
			monthly := AdjustedSSBenefit(
				s.SocialSecurity.SpouseFRABenefit,
				NormalizedSSFRA(s.SocialSecurity.SpouseFRA),
				s.SocialSecurity.SpouseClaimAge,
			)
			for i := 0; i < yearsSinceClaim; i++ {
				monthly *= 1 + cola
			}
			total += monthly * 12
		}
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

// bracketTopFor returns the top of the given target marginal bracket
// for the filing status. Returns (ceiling, ok). ok=false signals
// either an unknown filing status OR a target rate that is not in
// the table (e.g., 0.32 — currently unsupported). Callers should
// treat ok=false as "skip this candidate" or "skip the bracket-fill
// family" depending on context.
//
// Values are 2024 IRS thresholds, in nominal dollars. Acceptable
// approximation for optimization — the optimizer ranks on the engine's
// actual output, which uses the engine's full tax tables; this table
// is only used to set per-year conversion targets.
func bracketTopFor(status models.FilingStatus, target float64) (float64, bool) {
	table := map[models.FilingStatus]map[float64]float64{
		models.FilingSingle: {
			0.12: 47_150,
			0.22: 100_525,
			0.24: 191_950,
		},
		models.FilingMarriedJoint: {
			0.12: 94_300,
			0.22: 201_050,
			0.24: 383_900,
		},
		models.FilingMarriedSeparate: {
			0.12: 47_150,
			0.22: 100_525,
			0.24: 191_950,
		},
		models.FilingHeadOfHousehold: {
			0.12: 63_100,
			0.22: 100_500,
			0.24: 191_950,
		},
	}
	rows, ok := table[status]
	if !ok {
		return 0, false
	}
	ceiling, ok := rows[target]
	return ceiling, ok
}

// enumerateBracketFillStrategies generates bracket-fill candidates:
// cross-product of target brackets × valid windows. Only windows that
// end at or before RMD age are considered — bracket-fill is only useful
// before mandatory distributions begin. Uses engine.EffectiveRMDStartAge
// so it correctly honors SECURE 2.0 (73/75 depending on birth year).
// Candidates that yield zero conversion for every year of the window
// are skipped (they duplicate the no-conversion baseline).
func enumerateBracketFillStrategies(s *models.WhatIfSettings) []models.RothOptimizerStrategy {
	if s.TaxConfig == nil {
		return nil
	}
	rmdAge := engine.EffectiveRMDStartAge(s)
	allWindows := strategyWindows(s)
	// Filter to pre-RMD windows only — bracket-fill is only meaningful
	// before mandatory distributions begin. Uses EffectiveRMDStartAge
	// so it correctly honors SECURE 2.0 (73/75 depending on birth year).
	windows := make([]strategyWindow, 0, len(allWindows))
	for _, w := range allWindows {
		if w.EndAge <= rmdAge {
			windows = append(windows, w)
		}
	}
	if len(windows) == 0 {
		return nil
	}

	out := make([]models.RothOptimizerStrategy, 0, len(windows)*len(taxOptimizerBracketFillTargets))
	for _, target := range taxOptimizerBracketFillTargets {
		ceiling, ok := bracketTopFor(s.TaxConfig.FilingStatus, target)
		if !ok {
			return nil // unknown filing status: skip the entire family
		}
		for _, w := range windows {
			if !bracketFillProducesNonZero(s, w, ceiling) {
				continue
			}
			out = append(out, models.RothOptimizerStrategy{
				Kind:          models.RothStrategyBracketFill,
				TargetBracket: target,
				StartAge:      w.StartAge,
				EndAge:        w.EndAge,
				Label:         formatBracketFillLabel(target, w),
			})
		}
	}
	return out
}

func bracketFillProducesNonZero(s *models.WhatIfSettings, w strategyWindow, ceiling float64) bool {
	startProjYear := w.StartAge - s.CurrentAge
	endProjYear := w.EndAge - s.CurrentAge
	if startProjYear < 0 {
		startProjYear = 0
	}
	for y := startProjYear; y < endProjYear; y++ {
		other := estimateOtherTaxableIncome(s, y)
		if ceiling-other > 1 {
			return true
		}
	}
	return false
}

func formatBracketFillLabel(target float64, w strategyWindow) string {
	return fmt.Sprintf("Fill %.0f%% bracket, %d→%d", target*100, w.StartAge, w.EndAge)
}

// enumerateRothStrategies returns the full candidate set: ladder family
// followed by bracket-fill family. Order is stable for test
// reproducibility.
func enumerateRothStrategies(s *models.WhatIfSettings) []models.RothOptimizerStrategy {
	out := enumerateLadderStrategies(s)
	out = append(out, enumerateBracketFillStrategies(s)...)
	return out
}

// rothStrategyToConfig produces the RothConversionConfig that, when
// substituted into settings, makes the engine apply the strategy.
// Ladder strategies translate to a fixed AnnualAmount across a window.
// Bracket-fill strategies translate to a PerYearOverrides map
// pre-computed via estimateOtherTaxableIncome. A zero-amount ladder
// (the "No conversion" baseline) returns a disabled config.
func rothStrategyToConfig(s *models.WhatIfSettings, strat models.RothOptimizerStrategy) *models.RothConversionConfig {
	if strat.Kind == models.RothStrategyNone {
		return &models.RothConversionConfig{Enabled: false}
	}
	if strat.Kind == models.RothStrategyLadder && strat.AnnualAmount == 0 {
		return &models.RothConversionConfig{Enabled: false}
	}

	startProjYear := strat.StartAge - s.CurrentAge
	endProjYear := strat.EndAge - s.CurrentAge
	if startProjYear < 0 {
		startProjYear = 0
	}

	cfg := &models.RothConversionConfig{
		Enabled:   true,
		StartYear: startProjYear,
		EndYear:   endProjYear - 1, // inclusive end-year semantics
	}

	switch strat.Kind {
	case models.RothStrategyLadder:
		cfg.AnnualAmount = strat.AnnualAmount
	case models.RothStrategyBracketFill:
		yearly := strategyYearlyConversions(s, strat)
		if yearly == nil {
			return &models.RothConversionConfig{Enabled: false}
		}
		overrides := make(map[int]float64, len(yearly))
		for _, yc := range yearly {
			overrides[yc.Age-s.CurrentAge] = yc.Amount
		}
		cfg.PerYearOverrides = overrides
	}
	return cfg
}

// strategyYearlyConversions returns the per-year conversion amounts
// implied by strat. Returns nil for the no-conversion baseline (none-kind
// or a zero-amount ladder). For ladder strategies every entry shares
// strat.AnnualAmount. For bracket-fill strategies each entry equals
// the bracket ceiling minus estimateOtherTaxableIncome for that year
// (clamped to zero). Mirrors the math in rothStrategyToConfig so the
// displayed amounts match what the engine actually applied.
func strategyYearlyConversions(s *models.WhatIfSettings, strat models.RothOptimizerStrategy) []models.YearlyConversion {
	if strat.Kind == models.RothStrategyNone {
		return nil
	}
	if strat.Kind == models.RothStrategyLadder && strat.AnnualAmount == 0 {
		return nil
	}

	startProjYear := strat.StartAge - s.CurrentAge
	endProjYear := strat.EndAge - s.CurrentAge
	if startProjYear < 0 {
		startProjYear = 0
	}
	if endProjYear <= startProjYear {
		return nil
	}

	out := make([]models.YearlyConversion, 0, endProjYear-startProjYear)

	switch strat.Kind {
	case models.RothStrategyLadder:
		for y := startProjYear; y < endProjYear; y++ {
			out = append(out, models.YearlyConversion{
				Age:    s.CurrentAge + y,
				Amount: strat.AnnualAmount,
			})
		}
	case models.RothStrategyBracketFill:
		if s.TaxConfig == nil {
			return nil
		}
		ceiling, ok := bracketTopFor(s.TaxConfig.FilingStatus, strat.TargetBracket)
		if !ok {
			return nil
		}
		for y := startProjYear; y < endProjYear; y++ {
			other := estimateOtherTaxableIncome(s, y)
			conv := ceiling - other
			if conv < 0 {
				conv = 0
			}
			out = append(out, models.YearlyConversion{
				Age:    s.CurrentAge + y,
				Amount: conv,
			})
		}
	default:
		return nil
	}
	return out
}
