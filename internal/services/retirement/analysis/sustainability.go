package analysis

import (
	"budget2/internal/models"
)

// Score computes the sustainability score. Only the budget fit's
// required withdrawal rate and the projection's survival flag drive the
// score, so the signature takes exactly those two scalars — callers pull
// them from their BudgetFitAnalysis / ProjectionResult. The analysis
// package keeps no projection state of its own.
func Score(requiredRate float64, survives bool) *models.SustainabilityScore {
	return models.CalculateSustainabilityScore(requiredRate, survives)
}
