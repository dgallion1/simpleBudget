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
	// If we still haven't reached k pairs, fall back to the user's
	// current settings as the final candidate. The optimizer can score
	// it and let the user see how their saved scenario compares.
	if len(out) < k {
		addPair(ssPair{Primary: currentPrimary, Spouse: currentSpouse})
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
// it.
//
// LifetimeTaxReal includes federal + state + NIIT (the engine's Taxes
// field) deflated by CumulativeInflation. IRMAA is reported separately
// as a spending cost in the engine and is therefore NOT included; this
// is acceptable for Phase 1 since IRMAA variation across candidates
// also indirectly affects EndingPortfolioReal (the primary ranking
// metric).
//
// PeakMarginalBracket and TotalRothConverted require explainability
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
		deflator := ys.CumulativeInflation
		if deflator <= 0 {
			deflator = 1 // pre-projection or unset
		}
		cand.LifetimeTaxReal += ys.Taxes / deflator
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

// TaxOptimizer runs the Tax Optimizer and returns a recommendation.
// Always synchronous. Eligibility is gated; ineligible scenarios
// return a non-nil result with Eligible=false and IneligibleReason set.
// Uses the auto-seed convention (seed=0) for Monte Carlo refinement
// (Phase 1.5 — MC refinement is added in a later task).
func TaxOptimizer(eng *engine.Engine, in engine.Input, ss *models.SSPortfolioAnalysis) *models.TaxOptimizerAnalysis {
	return TaxOptimizerWithSeed(eng, in, ss, 0)
}

// currentRothStrategyFor reconstructs a RothOptimizerStrategy that
// describes the saved RothConversionConfig as a strategy. Used to
// populate Baseline.RothStrategy for the delta-column UI. The reverse
// of rothStrategyToConfig: where the forward direction writes
// EndYear = endProjYear - 1 (because the engine's EndYear is
// inclusive), this direction adds the 1 back to recover the exclusive
// EndAge that RothOptimizerStrategy uses.
func currentRothStrategyFor(settings *models.WhatIfSettings) models.RothOptimizerStrategy {
	if settings.RothConversion == nil || !settings.RothConversion.Enabled {
		return models.RothOptimizerStrategy{
			Kind:  models.RothStrategyNone,
			Label: "Current (no conversions)",
		}
	}
	strat := models.RothOptimizerStrategy{
		Kind:         models.RothStrategyLadder,
		AnnualAmount: settings.RothConversion.AnnualAmount,
		StartAge:     settings.CurrentAge + settings.RothConversion.StartYear,
		EndAge:       settings.CurrentAge + settings.RothConversion.EndYear + 1,
		Label:        "Current scenario",
	}
	if strat.EndAge <= strat.StartAge {
		strat.EndAge = settings.CurrentAge + settings.ProjectionYears
	}
	return strat
}

// TaxOptimizerWithSeed is TaxOptimizer with an explicit Monte Carlo
// seed for deterministic tests. seed=0 means auto-seed.
func TaxOptimizerWithSeed(eng *engine.Engine, in engine.Input, ss *models.SSPortfolioAnalysis, seed int64) *models.TaxOptimizerAnalysis {
	settings := in.Prepared.Settings()
	if ok, reason := taxOptimizerEligible(settings); !ok {
		return &models.TaxOptimizerAnalysis{
			Eligible:         false,
			IneligibleReason: reason,
		}
	}

	currentPrimary, currentSpouse := 0, 0
	if settings.SocialSecurity != nil {
		currentPrimary = settings.SocialSecurity.ClaimAge
		currentSpouse = settings.SocialSecurity.SpouseClaimAge
	}
	currentRoth := currentRothStrategyFor(settings)

	// Baseline: score the user's saved input directly (not through a
	// reconstructed strategy clone) so its metrics exactly match what
	// the rest of the page shows for this scenario.
	baselineProj := eng.Run(in)
	baseline := projectionToCandidate(baselineProj, currentPrimary, currentSpouse, currentRoth)

	pairs := topKSSPairs(ss, currentPrimary, currentSpouse, taxOptimizerTopSSPairs)
	strategies := enumerateRothStrategies(settings)

	// If strategies is empty (no valid windows after eligibility checks)
	// or pairs is empty (cannot happen — topKSSPairs always returns at
	// least one), scored stays empty and the result has an empty Top
	// slice. The handler should treat that as "no candidates evaluated"
	// rather than a failure.
	scored := make([]models.TaxOptimizerCandidate, 0, len(pairs)*len(strategies))
	for _, p := range pairs {
		for _, strat := range strategies {
			cand := scoreCandidate(eng, in, p.Primary, p.Spouse, strat)
			if cand.EndingPortfolioReal == -math.MaxFloat64 {
				continue // drop failed projections from Top
			}
			scored = append(scored, cand)
		}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].EndingPortfolioReal > scored[j].EndingPortfolioReal
	})

	finalists := scored
	if len(finalists) > taxOptimizerTopFinalists {
		finalists = finalists[:taxOptimizerTopFinalists]
	}

	// Monte Carlo refinement of top finalists. Reuses the existing
	// analysis.MonteCarlo entry point with a small budget. Seed=0
	// means auto-seed (preserves "default = unpredictable" contract);
	// tests pin a fixed seed for reproducibility.
	for i := range finalists {
		mcCloned, ok := cloneSettingsWithSSAndRoth(settings,
			finalists[i].PrimaryClaimAge,
			finalists[i].SpouseClaimAge,
			finalists[i].RothStrategy,
		)
		if !ok {
			continue
		}
		mcInput := engine.Input{Prepared: mcCloned, Chain: in.Chain, Hooks: in.Hooks}
		mc := MonteCarlo(eng, mcInput, taxOptimizerMonteCarloRuns, seed)
		if mc == nil || mc.Stats == nil {
			continue
		}
		finalists[i].MCSurvivalRate = mc.Stats.SuccessRate
		finalists[i].MCMedianEndingReal = mc.Stats.MedianBalance
	}

	// Re-sort by MC median ending balance (MC tiebreak).
	sort.SliceStable(finalists, func(i, j int) bool {
		return finalists[i].MCMedianEndingReal > finalists[j].MCMedianEndingReal
	})

	result := &models.TaxOptimizerAnalysis{
		Eligible:         true,
		Baseline:         baseline,
		Top:              finalists,
		CandidatesScored: len(pairs) * len(strategies),
		MonteCarloRuns:   taxOptimizerMonteCarloRuns,
	}
	if len(finalists) > 0 {
		result.Best = finalists[0]
	}
	return result
}
