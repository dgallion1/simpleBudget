// Package retirement is the top-level coordinator for what-if retirement
// projections. It owns the settings store, scenario chaining, eligibility
// rules (Medicare, Social Security, RMD), guardrails, the tax calculator,
// and the engine fan-out (RunFull) that produces a complete WhatIfAnalysis
// — deterministic projection, Monte Carlo simulation, backtest, and
// post-projection analyses. The pure projection math lives in the engine
// sub-package; this package is the orchestrator above it.
package retirement

import (
	"sync"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
)

// MonteCarloRuns is the default number of Monte Carlo iterations.
const MonteCarloRuns = 1000

// MonteCarloSeed is the default RNG seed for orchestrator-driven runs.
// 0 = auto-seed from time.
const MonteCarloSeed int64 = 0

// RunFull executes the full what-if analysis fan-out for in. Returns a
// fully populated *models.WhatIfAnalysis. Auto-fills DefaultHooks when
// the caller passes a zero-valued Input.Hooks so existing handler call
// sites keep working without explicitly wiring SS/chain hooks.
func RunFull(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
	return runFullWithSeed(eng, in, MonteCarloSeed)
}

// runFullWithSeed is RunFull with an explicit MC seed. Retained as an
// unexported helper for the retirement-package test helper that pins the
// MC seed for deterministic comparisons.
func runFullWithSeed(eng *engine.Engine, in engine.Input, mcSeed int64) *models.WhatIfAnalysis {
	if in.Hooks.SocialSecurityProjectionActive == nil &&
		in.Hooks.ProjectedSocialSecurityIncome == nil &&
		in.Hooks.ResolveChainTransition == nil {
		in.Hooks = DefaultHooks()
	}
	proj := eng.Run(in)

	// Cheap post-projection analyses derived from the single baseline run.
	explainability := analysis.BuildExplainability(proj, in)
	budgetFit := analysis.BudgetFit(in, proj)
	presentValue := analysis.PresentValue(in, proj)
	sustainability := analysis.Score(budgetFit.RequiredRate, proj.Survives)
	rmd := analysis.BuildRMD(proj, in)
	tax := analysis.BuildTax(proj, in)

	// The expensive analyses are independent of one another: sensitivity,
	// failure points, Monte Carlo, backtest, and the SS comparison each
	// run their own perturbed projections off the same read-only Input.
	// Fan them out concurrently (Candidate #3) — engine.Run is a pure
	// function of its Input and every perturbed run builds its own
	// PreparedSettings, so results are identical to the sequential form.
	// Sensitivity and failure points reuse the baseline projection above
	// instead of re-running it.
	var (
		sensitivity   []models.SensitivityResult
		failurePoints *models.FailurePointAnalysis
		monteCarlo    *models.MonteCarloAnalysis
		backtest      *models.HistoricalBacktestAnalysis
		ssAnalysis    *models.SSComparisonAnalysis
	)
	settings := in.Prepared.Settings()

	var wg sync.WaitGroup
	spawn := func(f func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f()
		}()
	}
	spawn(func() { sensitivity = analysis.SensitivityWithBaseline(eng, in, proj, budgetFit) })
	spawn(func() { failurePoints = analysis.FailurePointsWithBaseline(eng, in, proj) })
	spawn(func() { monteCarlo = analysis.MonteCarlo(eng, in, MonteCarloRuns, mcSeed) })
	spawn(func() { backtest = analysis.HistoricalBacktest(in, history.DefaultData()) })
	if settings.SocialSecurity != nil && settings.SocialSecurity.FRABenefit > 0 {
		spawn(func() {
			ssAnalysis = analysis.SSAnalysis(in)
			if ssAnalysis != nil && SSPortfolioEligible(settings) {
				ssAnalysis.Portfolio = analysis.SSPortfolioWithSeed(eng, in, ssAnalysis, mcSeed)
			}
		})
	}
	wg.Wait()

	if backtest != nil && monteCarlo != nil && monteCarlo.Stats != nil {
		backtest.MonteCarloSuccessRate = monteCarlo.Stats.SuccessRate
		backtest.HistoricalVsMC = backtest.SuccessRate - monteCarlo.Stats.SuccessRate
	}

	return &models.WhatIfAnalysis{
		Settings:                 settings,
		Projection:               proj,
		ProjectionExplainability: explainability,
		BudgetFit:                budgetFit,
		PresentValue:             presentValue,
		Sustainability:           sustainability,
		Sensitivity:              sensitivity,
		FailurePoints:            failurePoints,
		MonteCarlo:               monteCarlo,
		RMD:                      rmd,
		Tax:                      tax,
		HistoricalBacktest:       backtest,
		SocialSecurity:           ssAnalysis,
	}
}

// RunTaxOptimizer runs only the Tax Optimizer analysis (and the
// upstream SS analyses it depends on). Called by the explicit
// /api/whatif/tax-optimize endpoint, NOT by RunFull, because the
// optimizer's cost is too high for the interactive HTMX recalc path.
// Mirrors RunFull's hooks auto-fill convention.
func RunTaxOptimizer(eng *engine.Engine, in engine.Input) *models.TaxOptimizerAnalysis {
	return runTaxOptimizerWithSeed(eng, in, MonteCarloSeed)
}

func runTaxOptimizerWithSeed(eng *engine.Engine, in engine.Input, mcSeed int64) *models.TaxOptimizerAnalysis {
	if in.Hooks.SocialSecurityProjectionActive == nil &&
		in.Hooks.ProjectedSocialSecurityIncome == nil &&
		in.Hooks.ResolveChainTransition == nil {
		in.Hooks = DefaultHooks()
	}
	settings := in.Prepared.Settings()

	// Pin one effective MC seed for the entire optimizer call so both
	// the SS-pair pruning pass (SSPortfolioWithSeed) AND the optimizer's
	// finalist refinement run against the same Monte Carlo paths. If we
	// pass mcSeed=0 down, each MC cell auto-seeds from time.Now()
	// independently, so the SS-pair selection feeding the optimizer is
	// already chosen from non-comparable paths before finalists are
	// even scored. Tests pin a non-zero seed and bypass derivation.
	effectiveSeed := mcSeed
	if effectiveSeed == 0 {
		effectiveSeed = time.Now().UnixNano()
		if effectiveSeed == 0 {
			effectiveSeed = 1 // MonteCarlo treats 0 as auto-seed
		}
	}

	var ssPortfolio *models.SSPortfolioAnalysis
	if settings.SocialSecurity != nil && settings.SocialSecurity.FRABenefit > 0 {
		ssAnalysis := analysis.SSAnalysis(in)
		if ssAnalysis != nil && SSPortfolioEligible(settings) {
			ssAnalysis.Portfolio = analysis.SSPortfolioWithSeed(eng, in, ssAnalysis, effectiveSeed)
			ssPortfolio = ssAnalysis.Portfolio
		}
	}
	return analysis.TaxOptimizerWithSeed(eng, in, ssPortfolio, effectiveSeed)
}
