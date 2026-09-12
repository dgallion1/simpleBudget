package analysis

import "budget2/internal/models"

// SummarizeLifetimeYear aggregates committed canonical records without
// reconstructing salary, tax, contribution, or RMD rules.
func SummarizeLifetimeYear(outcomes []models.LifetimeMonthOutcome) models.LifetimeYearSummary {
	return models.AggregateLifetimeYear(outcomes)
}
