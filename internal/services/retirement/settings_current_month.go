package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
	"time"
)

// resolveCurrentMonth runs at the settings boundary, keeping the engine deterministic.
func resolveCurrentMonth(settings *models.WhatIfSettings, now time.Time) {
	if settings.UseCurrentMonth {
		settings.StartDate = now.Format("2006-01")
		prepare.ComputeAges(settings)
	}
}

// Refresh a private snapshot even when the manager cache predates this month.
func cloneForCurrentMonth(settings *models.WhatIfSettings) (*models.WhatIfSettings, error) {
	cloned, err := prepare.Clone(settings)
	if err != nil {
		return nil, err
	}
	resolveCurrentMonth(cloned, time.Now())
	return cloned, nil
}
