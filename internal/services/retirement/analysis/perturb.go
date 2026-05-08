package analysis

import (
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// perturbAndPrepare deep-copies and re-prepares a perturbed configuration.
// Perturbations of an already-prepared snapshot only change scalar parameters
// (returns, inflation, expenses), so the result must always be valid; an
// error here indicates a bug.
func perturbAndPrepare(modified *models.WhatIfSettings) prepare.PreparedSettings {
	p, err := prepare.From(modified)
	if err != nil {
		panic(fmt.Sprintf("retirement: perturbation produced invalid settings: %v", err))
	}
	return p
}

// PerturbAndPrepareForCalculator is a parity-window export shim so any
// remaining retirement-side caller resolves through the analysis-side
// implementation. Removed in Task 8.
func PerturbAndPrepareForCalculator(modified *models.WhatIfSettings) prepare.PreparedSettings {
	return perturbAndPrepare(modified)
}
