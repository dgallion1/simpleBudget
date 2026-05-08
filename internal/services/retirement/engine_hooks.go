package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// init wires the engine package's Social Security hooks to the
// retirement-side helpers. The engine doesn't import retirement
// (cycle), so the projection-loop helpers in the engine package
// reach into retirement's SS optimizer through these function-valued
// vars.
//
// Removed in Task 8 once SS analysis lives inside engine.
func init() {
	engine.SocialSecurityProjectionActive = socialSecurityProjectionActive
	engine.ProjectedSocialSecurityIncome = projectedSocialSecurityIncome
	engine.NextChainTransitionHook = nextChainTransitionForEngine
	engine.HistoricalStatsHook = GetHistoricalStats
}

// nextChainTransitionForEngine resolves the next chain transition for
// engine.runMonthlyLoop. The engine doesn't import retirement, so the
// chain rebase logic (prepareChainedSettings) reaches in through this
// hook. Removed in Task 8.
func nextChainTransitionForEngine(currentYear, nextChainIndex int, primarySettings *models.WhatIfSettings, chain []engine.PreparedChainLink) (int, *models.WhatIfSettings) {
	if nextChainIndex >= len(chain) {
		return nextChainIndex, nil
	}
	link := chain[nextChainIndex]
	currentAge := primarySettings.CurrentAge + currentYear
	if currentAge >= link.TransitionAge {
		transitionYear := link.TransitionAge - primarySettings.CurrentAge
		prepared := prepareChainedSettings(link.Settings.Settings(), primarySettings, transitionYear)
		return nextChainIndex + 1, prepared
	}
	return nextChainIndex, nil
}
