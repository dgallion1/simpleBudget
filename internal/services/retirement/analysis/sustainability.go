package analysis

import (
	"budget2/internal/models"
)

// Score computes the sustainability score from a projection and a
// pre-computed BudgetFitAnalysis. Only RequiredRate and Survives drive
// the score; the analysis package keeps no projection state of its own.
//
// BudgetFit construction still lives on Calculator (parity-window
// scope). Callers pass it explicitly so this function stays pure and
// avoids importing the parent retirement package.
func Score(proj *models.ProjectionResult, budgetFit *models.BudgetFitAnalysis) *models.SustainabilityScore {
	return models.CalculateSustainabilityScore(budgetFit.RequiredRate, proj.Survives)
}
