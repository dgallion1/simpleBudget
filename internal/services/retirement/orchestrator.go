// Package retirement is the top-level coordinator for what-if retirement
// projections. It owns the settings store, scenario chaining, eligibility
// rules (Medicare, Social Security, RMD), guardrails, the tax calculator,
// and the engine fan-out (RunFull) that produces a complete WhatIfAnalysis
// — deterministic projection, Monte Carlo simulation, backtest, and
// post-projection analyses. The pure projection math lives in the engine
// sub-package; this package is the orchestrator above it.
package retirement

import (
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

	explainability := analysis.BuildExplainability(proj, in)
	budgetFit := analysis.BudgetFit(in, proj)
	presentValue := analysis.PresentValue(in, proj)
	sustainability := analysis.Score(budgetFit.RequiredRate, proj.Survives)
	sensitivity := analysis.Sensitivity(eng, in)
	failurePoints := analysis.FailurePoints(eng, in)
	monteCarlo := analysis.MonteCarlo(eng, in, MonteCarloRuns, mcSeed)
	rmd := analysis.BuildRMD(proj, in)
	tax := analysis.BuildTax(proj, in)
	backtest := analysis.HistoricalBacktest(in, history.DefaultData())

	if backtest != nil && monteCarlo != nil && monteCarlo.Stats != nil {
		backtest.MonteCarloSuccessRate = monteCarlo.Stats.SuccessRate
		backtest.HistoricalVsMC = backtest.SuccessRate - monteCarlo.Stats.SuccessRate
	}

	var ssAnalysis *models.SSComparisonAnalysis
	settings := in.Prepared.Settings()
	if settings.SocialSecurity != nil && settings.SocialSecurity.FRABenefit > 0 {
		ssAnalysis = analysis.SSAnalysis(in)
		if ssAnalysis != nil && SSPortfolioEligible(settings) {
			ssAnalysis.Portfolio = analysis.SSPortfolioWithSeed(eng, in, ssAnalysis, mcSeed)
		}
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
