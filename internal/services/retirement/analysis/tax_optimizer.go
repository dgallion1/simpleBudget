// Tax Optimizer: ranks (SS claim pair × Roth strategy) candidates by
// real ending portfolio. See
// docs/superpowers/specs/2026-05-12-tax-optimizer-design.md.
package analysis

import (
	"fmt"
	"math"
	"sort"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// Tax Optimizer tuning constants — single block for easy adjustment.
const (
	taxOptimizerEligibilityMinTaxDeferred     = 100_000.0
	taxOptimizerEligibilityMaxStartAge        = 73
	taxOptimizerEligibilityMinProjectionYears = 5
	taxOptimizerTopSSPairs                    = 3
	taxOptimizerTopFinalists                  = 5
	// taxOptimizerMonteCarloRuns is intentionally lower than
	// ssPortfolioMonteCarloRuns (250) because the Tax Optimizer applies
	// MC to ~5 top finalists × ~135 deterministic candidates. The total
	// MC budget (5 × 32 = 160 runs) is comparable to a single SS
	// Portfolio analysis cell.
	taxOptimizerMonteCarloRuns = 32
)

// taxOptimizerEligible reports whether the scenario qualifies for the
// optimizer. Returns (false, reason) when ineligible; reason is the
// user-facing string rendered in the panel.
func taxOptimizerEligible(s *models.WhatIfSettings) (bool, string) {
	if s == nil {
		return false, "No scenario loaded."
	}
	if s.TaxConfig == nil || s.TaxConfig.FilingStatus == "" {
		return false, "Set tax filing status to enable optimization."
	}
	taxDeferred := s.PortfolioValue * (s.TaxDeferredPercent / 100.0)
	if taxDeferred < taxOptimizerEligibilityMinTaxDeferred {
		return false, fmt.Sprintf("Tax-deferred balance too small to optimize ($%.0f).", taxDeferred)
	}
	if s.CurrentAge >= taxOptimizerEligibilityMaxStartAge {
		return false, "Optimizer requires pre-RMD horizon."
	}
	if s.ProjectionYears < taxOptimizerEligibilityMinProjectionYears {
		return false, "Projection too short to optimize."
	}
	return true, ""
}

// cloneSettingsWithSSAndRoth returns a prepared snapshot identical to s
// except for the SS claim ages and Roth conversion config. The deep
// copy in prepare.From handles slice/pointer aliasing for the rest of
// the struct. Pattern mirrors cloneSettingsWithClaimAges in ss.go.
func cloneSettingsWithSSAndRoth(s *models.WhatIfSettings, primaryClaimAge, spouseClaimAge int, strat models.RothOptimizerStrategy) (prepare.PreparedSettings, bool) {
	if s == nil {
		return prepare.PreparedSettings{}, false
	}
	cfg := *s
	if s.SocialSecurity != nil {
		ssCopy := *s.SocialSecurity
		ssCopy.ClaimAge = primaryClaimAge
		ssCopy.SpouseClaimAge = spouseClaimAge
		cfg.SocialSecurity = &ssCopy
	}
	cfg.RothConversion = rothStrategyToConfig(s, strat)
	prepared := perturbAndPrepare(&cfg)

	// PerYearOverrides is tagged json:"-" so prepare.From's JSON-based
	// DeepCopy drops it. Re-attach the in-memory map onto the prepared
	// snapshot. This intentionally mutates Settings() — the same kind
	// of shallow violation the existing cloneSettingsWithClaimAges
	// accepts — because the override map is constructed in-memory
	// on each optimizer run and never persisted.
	if cfg.RothConversion != nil && cfg.RothConversion.PerYearOverrides != nil {
		if prepSettings := prepared.Settings(); prepSettings != nil && prepSettings.RothConversion != nil {
			prepSettings.RothConversion.PerYearOverrides = cfg.RothConversion.PerYearOverrides
		}
	}
	return prepared, true
}

// ssPair holds one (primary, spouse) claim-age combination.
type ssPair struct {
	Primary int
	Spouse  int
}

// topKSSPairs returns up to k joint SS pairs to search. When ss is nil
// or empty, returns a single fallback pair using the user's current
// settings. Otherwise composes pairs from each axis's top-survival
// candidates plus the joint optimum reported by SSPortfolio.
func topKSSPairs(ss *models.SSPortfolioAnalysis, currentPrimary, currentSpouse, k int) []ssPair {
	if ss == nil || (len(ss.PrimaryOptions) == 0 && len(ss.SpouseOptions) == 0) {
		return []ssPair{{Primary: currentPrimary, Spouse: currentSpouse}}
	}
	if k <= 0 {
		k = 1
	}

	primaryRanked := append([]models.SSPortfolioOption{}, ss.PrimaryOptions...)
	sort.SliceStable(primaryRanked, func(i, j int) bool {
		return primaryRanked[i].SurvivalRate > primaryRanked[j].SurvivalRate
	})
	spouseRanked := append([]models.SSPortfolioOption{}, ss.SpouseOptions...)
	sort.SliceStable(spouseRanked, func(i, j int) bool {
		return spouseRanked[i].SurvivalRate > spouseRanked[j].SurvivalRate
	})

	pickPrimary := func(i int) int {
		if i < len(primaryRanked) {
			return primaryRanked[i].ClaimAge
		}
		return currentPrimary
	}
	pickSpouse := func(i int) int {
		if i < len(spouseRanked) {
			return spouseRanked[i].ClaimAge
		}
		return currentSpouse
	}

	seen := map[ssPair]bool{}
	out := make([]ssPair, 0, k)
	addPair := func(p ssPair) {
		if seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}

	// Always seed with the SSPortfolio-reported joint optimum.
	if ss.OptimalPrimaryAge > 0 || ss.OptimalSpouseAge > 0 {
		opt := ssPair{
			Primary: ss.OptimalPrimaryAge,
			Spouse:  ss.OptimalSpouseAge,
		}
		if opt.Primary == 0 {
			opt.Primary = currentPrimary
		}
		if opt.Spouse == 0 {
			opt.Spouse = currentSpouse
		}
		addPair(opt)
	}
	for i := 0; len(out) < k; i++ {
		if i >= len(primaryRanked) && i >= len(spouseRanked) {
			break
		}
		addPair(ssPair{Primary: pickPrimary(i), Spouse: pickSpouse(i)})
	}
	if len(out) == 0 {
		out = append(out, ssPair{Primary: currentPrimary, Spouse: currentSpouse})
	}
	return out
}

// projectionToCandidate extracts scoring fields from a finished
// projection. Returns a candidate with the "failed projection"
// sentinel EndingPortfolioReal == -math.MaxFloat64 for nil/empty/NaN
// projections so callers can drop the candidate while still counting
// it. PeakMarginalBracket and TotalRothConverted require explainability
// fields the engine does not expose in Phase 1; both remain zero
// and the UI renders "—" when zero.
func projectionToCandidate(proj *models.ProjectionResult, primaryClaim, spouseClaim int, strat models.RothOptimizerStrategy) models.TaxOptimizerCandidate {
	cand := models.TaxOptimizerCandidate{
		PrimaryClaimAge: primaryClaim,
		SpouseClaimAge:  spouseClaim,
		RothStrategy:    strat,
	}
	if proj == nil || len(proj.YearlySummaries) == 0 {
		cand.EndingPortfolioReal = -math.MaxFloat64
		return cand
	}
	last := proj.YearlySummaries[len(proj.YearlySummaries)-1]
	ending := last.EndingBalanceReal
	if math.IsNaN(ending) || math.IsInf(ending, 0) {
		cand.EndingPortfolioReal = -math.MaxFloat64
		return cand
	}
	cand.EndingPortfolioReal = ending
	for _, ys := range proj.YearlySummaries {
		cand.LifetimeTaxReal += ys.Taxes
	}
	return cand
}

// scoreCandidate runs a deterministic projection for the given (SS
// pair, Roth strategy) override and returns the scored candidate.
func scoreCandidate(eng *engine.Engine, in engine.Input, primaryClaim, spouseClaim int, strat models.RothOptimizerStrategy) models.TaxOptimizerCandidate {
	cloned, ok := cloneSettingsWithSSAndRoth(in.Prepared.Settings(), primaryClaim, spouseClaim, strat)
	if !ok {
		return models.TaxOptimizerCandidate{
			PrimaryClaimAge:     primaryClaim,
			SpouseClaimAge:      spouseClaim,
			RothStrategy:        strat,
			EndingPortfolioReal: -math.MaxFloat64,
		}
	}
	cellInput := engine.Input{Prepared: cloned, Chain: in.Chain, Hooks: in.Hooks}
	proj := eng.Run(cellInput)
	return projectionToCandidate(proj, primaryClaim, spouseClaim, strat)
}
