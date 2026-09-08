package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
	"fmt"
	"sort"
)

// SettingsForTaxOptimizerCandidate materializes exactly the input used to score
// a server-produced candidate, including its converged bracket-fill feedback.
// Callers must bind the candidate to the snapshot that generated it.
func SettingsForTaxOptimizerCandidate(s *models.WhatIfSettings, c models.TaxOptimizerCandidate) (*models.WhatIfSettings, error) {
	if s == nil {
		return nil, fmt.Errorf("no settings")
	}
	cfg, err := prepare.Clone(candidateSettingsForSS(s, c.PrimaryClaimAge, c.SpouseClaimAge))
	if err != nil {
		return nil, err
	}
	cfg.RothConversion = rothStrategyToConfig(cfg, c.RothStrategy, c.BracketFillFeedback)
	return cfg, nil
}

// savedRothConversions describes the saved schedule without reconstructing it
// as a ladder (which would falsely show its zero fallback annual amount).
func savedRothConversions(s *models.WhatIfSettings) []models.YearlyConversion {
	if s.RothConversion == nil || !s.RothConversion.Enabled {
		return nil
	}
	if len(s.RothConversion.PerYearOverrides) == 0 {
		return strategyYearlyConversions(s, currentRothStrategyFor(s), nil)
	}
	years := make([]int, 0, len(s.RothConversion.PerYearOverrides))
	for y := range s.RothConversion.PerYearOverrides {
		years = append(years, y)
	}
	sort.Ints(years)
	out := make([]models.YearlyConversion, 0, len(years))
	for _, y := range years {
		out = append(out, models.YearlyConversion{Age: s.CurrentAge + y, Amount: s.RothConversion.PerYearOverrides[y]})
	}
	return out
}
