// Tax Optimizer: ranks (SS claim pair × Roth strategy) candidates by
// real ending portfolio. See
// docs/superpowers/specs/2026-05-12-tax-optimizer-design.md.
package analysis

import (
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// Tax Optimizer tuning constants — single block for easy adjustment.
const (
	taxOptimizerEligibilityMinTaxDeferred     = 100_000.0
	taxOptimizerEligibilityMaxStartAge        = 73
	taxOptimizerEligibilityMinProjectionYears = 5
	taxOptimizerTopSSPairs                    = 3
	taxOptimizerTopFinalists                  = 5
	taxOptimizerMonteCarloRuns                = 32
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
	return perturbAndPrepare(&cfg), true
}
