package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
)

// SSPortfolioEligible returns true when the settings indicate Social
// Security portfolio analysis should be computed. Forwards to the
// canonical predicate in the analysis package.
func SSPortfolioEligible(s *models.WhatIfSettings) bool {
	return analysis.SSPortfolioEligible(s)
}

// FirstRMDCalendarYear returns the first calendar year in which the older
// person must take an RMD. Var-alias for engine.FirstRMDCalendarYear so
// existing retirement-side callers keep compiling unchanged.
var FirstRMDCalendarYear = engine.FirstRMDCalendarYear
