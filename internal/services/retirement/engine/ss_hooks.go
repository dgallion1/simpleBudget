package engine

import (
	"strings"

	"budget2/internal/models"
)

// IsSocialSecurityIncomeSource reports whether the supplied income
// source represents a Social Security stream.
//
// Social Security optimizer hooks (SocialSecurityProjectionActive,
// ProjectedSocialSecurityIncome) used to live here as package-level
// function vars wired by retirement's init(). They moved to fields on
// engine.Input.Hooks so engine.Run is a pure function of its Input.
// retirement.DefaultHooks() returns the production set.
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
