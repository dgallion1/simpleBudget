package engine

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// Input bundles everything Engine.Run needs. Chain may be nil for
// single-scenario projections. Hooks may be zero-valued — every hook
// callsite has a nil-safe default that mirrors the old package-level
// no-op vars (no SS optimizer, no chain transition).
type Input struct {
	Prepared prepare.PreparedSettings
	Chain    []PreparedChainLink
	Hooks    Hooks
}

// PreparedChainLink describes a scenario transition that fires when the
// reference person reaches TransitionAge. Settings is the prepared
// snapshot for the post-transition scenario.
type PreparedChainLink struct {
	ScenarioFilename string
	TransitionAge    int
	Settings         prepare.PreparedSettings
}

// Hooks bundles the function dependencies the projection loop needs but
// cannot define itself: SS-optimizer projection (which lives in the
// retirement package's analysis layer) and chain-transition resolution
// (which depends on retirement.prepareChainedSettings). Callers populate
// Hooks when constructing Input. retirement.DefaultHooks() returns the
// production set; tests can leave fields nil to get safe no-op
// behaviour, or supply stubs.
//
// Hooks replaces the old package-level function vars
// (SocialSecurityProjectionActive, ProjectedSocialSecurityIncome,
// NextChainTransitionHook) that retirement's init() used to overwrite.
// Routing through Input makes engine.Run a pure function of its Input
// — no hidden init-order dependency.
type Hooks struct {
	// SocialSecurityProjectionActive reports whether SS projection should
	// override manual SS income sources for the household. nil ⇒ false.
	SocialSecurityProjectionActive func(*models.WhatIfSettings) bool

	// ProjectedSocialSecurityIncome returns the SS-optimizer-projected
	// monthly income at the given month. nil ⇒ 0.
	ProjectedSocialSecurityIncome func(*models.WhatIfSettings, int) float64

	// ResolveChainTransition is invoked when the projection loop reaches
	// a chain-transition boundary. It returns the new active scenario
	// and the next chain index. nil ⇒ no transition (correct for
	// chain-less inputs).
	ResolveChainTransition func(currentYear, nextChainIndex int, primarySettings *models.WhatIfSettings, chain []PreparedChainLink) (int, *models.WhatIfSettings)
}

// SSActive reports whether the SS-optimizer projection is active for s.
// Wraps Hooks.SocialSecurityProjectionActive with a nil-safe default of
// false (matches the old package-level var's no-op default).
func (h Hooks) SSActive(s *models.WhatIfSettings) bool {
	if h.SocialSecurityProjectionActive == nil {
		return false
	}
	return h.SocialSecurityProjectionActive(s)
}

// SSIncome returns the SS-optimizer-projected monthly income at month.
// Wraps Hooks.ProjectedSocialSecurityIncome with a nil-safe default of
// 0 (matches the old package-level var's no-op default).
func (h Hooks) SSIncome(s *models.WhatIfSettings, month int) float64 {
	if h.ProjectedSocialSecurityIncome == nil {
		return 0
	}
	return h.ProjectedSocialSecurityIncome(s, month)
}

// ResolveChain invokes the chain-transition resolver, falling back to
// "no transition" when the resolver is nil.
func (h Hooks) ResolveChain(currentYear, nextChainIndex int, primarySettings *models.WhatIfSettings, chain []PreparedChainLink) (int, *models.WhatIfSettings) {
	if h.ResolveChainTransition == nil {
		return nextChainIndex, nil
	}
	return h.ResolveChainTransition(currentYear, nextChainIndex, primarySettings, chain)
}
