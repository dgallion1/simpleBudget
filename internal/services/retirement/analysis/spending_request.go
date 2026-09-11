package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// NormalizeSpendingRequest validates and resolves automatic form defaults without
// running simulations, selecting random seeds, or changing settings.
func NormalizeSpendingRequest(in engine.Input, req models.SpendingOptimizerRequest) (models.SpendingOptimizerRequest, error) {
	normalized, _, err := normalizeSpendingRequest(in, req)
	return normalized, err
}
