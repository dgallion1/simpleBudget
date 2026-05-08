package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// DefaultHooks returns the canonical engine.Hooks for production
// projections. Wires the SS-optimizer projection (lives in this
// package's social_security.go) and the chain-transition resolver
// (which depends on prepareChainedSettings, also in this package).
//
// engine.Run consumes Hooks via Input. Callers that go through
// retirement.RunFull get DefaultHooks auto-filled when they pass a
// zero-valued Hooks. Direct engine.New().Run callers (notably
// retirement-package tests outside the orchestrator path) must either
// supply DefaultHooks explicitly or accept the safe-default behaviour
// (no SS optimizer, no chain transition) baked into Hooks' nil-safe
// methods.
func DefaultHooks() engine.Hooks {
	return engine.Hooks{
		SocialSecurityProjectionActive: socialSecurityProjectionActive,
		ProjectedSocialSecurityIncome:  projectedSocialSecurityIncome,
		ResolveChainTransition:         nextChainTransitionForEngine,
	}
}

// nextChainTransitionForEngine resolves the next chain transition for
// engine.runMonthlyLoop. The engine package doesn't import retirement
// (cycle), so the chain rebase logic (prepareChainedSettings) reaches
// in through this hook.
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
