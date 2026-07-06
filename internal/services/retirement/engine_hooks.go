package retirement

import (
	"log"

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
		prepared, err := prepareChainedSettings(link.Settings.Settings(), primarySettings, transitionYear)
		if err != nil {
			// The chain was validated at save time; failing here means the
			// stored snapshot or the rebase produced an invalid scenario.
			// Skip the transition (keep projecting on the current settings)
			// rather than run the engine on unvalidated input.
			log.Printf("retirement: chain transition at age %d skipped: %v", link.TransitionAge, err)
			return nextChainIndex + 1, nil
		}
		return nextChainIndex + 1, prepared.Settings()
	}
	return nextChainIndex, nil
}
