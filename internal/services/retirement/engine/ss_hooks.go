package engine

import (
	"strings"

	"budget2/internal/models"
)

// Social Security hooks. The optimizer/projection-planner logic for
// Social Security still lives in internal/services/retirement (in
// social_security.go and chain.go). The retirement package's init()
// wires these vars to the retirement-side helpers so engine code can
// route through the canonical SS implementations without an import
// cycle.
//
// Defaults are no-ops so the engine package is safe to use in
// isolation (tests, future engine-only callers) before the retirement
// init() has run, or when SS isn't relevant.
var (
	// SocialSecurityProjectionActive reports whether the household has
	// an active SS optimizer projection (i.e. FRA benefit + valid claim
	// age). When true, manual SS income sources are excluded from
	// CalculateMonthlyIncomeBreakdown's manual-income tally — the
	// projector adds them back via ProjectedSocialSecurityIncome.
	SocialSecurityProjectionActive = func(*models.WhatIfSettings) bool { return false }

	// ProjectedSocialSecurityIncome returns the optimizer-projected SS
	// monthly amount for the given month, including primary, spouse,
	// COLA growth, and any spousal top-up.
	ProjectedSocialSecurityIncome = func(*models.WhatIfSettings, int) float64 { return 0 }
)

// IsSocialSecurityIncomeSource reports whether the supplied income
// source represents a Social Security stream.
func IsSocialSecurityIncomeSource(source models.IncomeSource) bool {
	normalizedName := strings.ToLower(strings.ReplaceAll(source.Name, "-", " "))
	if strings.Contains(normalizedName, "social security") {
		return true
	}

	for _, token := range strings.Fields(normalizedName) {
		if token == "ssi" {
			return true
		}
	}

	return false
}
