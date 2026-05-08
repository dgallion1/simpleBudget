package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
)

// MonteCarloRuns is the default number of Monte Carlo iterations.
const MonteCarloRuns = 1000

// MonteCarloSeed is the default RNG seed for orchestrator-driven runs.
// 0 = auto-seed from time, matching Calculator.RunFullAnalysis behavior.
const MonteCarloSeed int64 = 0

// RunFull executes the full what-if analysis fan-out for in. Returns a
// fully populated *models.WhatIfAnalysis. Replaces Calculator.RunFullAnalysis.
func RunFull(eng *engine.Engine, in engine.Input) *models.WhatIfAnalysis {
	return runFullWithSeed(eng, in, MonteCarloSeed)
}

// runFullWithSeed is RunFull with an explicit MC seed. Used by
// Calculator.RunFullAnalysis to thread its parity-window seed override.
// Inlined back into RunFull and deleted in Task 8.
func runFullWithSeed(eng *engine.Engine, in engine.Input, mcSeed int64) *models.WhatIfAnalysis {
	proj := eng.Run(in)

	explainability := analysis.BuildExplainability(proj, in)
	budgetFit := analysis.BudgetFit(in)
	presentValue := analysis.PresentValue(in)
	sustainability := analysis.Score(proj, budgetFit)
	sensitivity := analysis.Sensitivity(eng, in)
	failurePoints := analysis.FailurePoints(eng, in)
	monteCarlo := analysis.MonteCarlo(eng, in, MonteCarloRuns, mcSeed)
	rmd := analysis.BuildRMD(proj, in)
	backtest := analysis.HistoricalBacktest(eng, in, history.DefaultData())

	if backtest != nil && monteCarlo != nil && monteCarlo.Stats != nil {
		backtest.MonteCarloSuccessRate = monteCarlo.Stats.SuccessRate
		backtest.HistoricalVsMC = backtest.SuccessRate - monteCarlo.Stats.SuccessRate
	}

	var ssAnalysis *models.SSComparisonAnalysis
	settings := in.Prepared.Settings()
	if settings.SocialSecurity != nil && settings.SocialSecurity.FRABenefit > 0 {
		ssAnalysis = analysis.SSAnalysis(eng, in)
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
		HistoricalBacktest:       backtest,
		SocialSecurity:           ssAnalysis,
	}
}
