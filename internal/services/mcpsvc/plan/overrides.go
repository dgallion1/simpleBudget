package plan

import (
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/overrides"
	"budget2/internal/services/retirement/prepare"
)

// Overrides is the shared sparse settings vocabulary. Aliased rather than
// re-declared so tool schemas and existing call sites are unaffected by the
// move to internal/services/retirement/overrides.
type Overrides = overrides.Overrides

// Apply is re-exported so this package's callers need not import both.
var Apply = overrides.Apply

// preparedWithOverrides applies the overrides and prepares the result for the
// engine. prepare.From runs its own DeepCopy internally, which drops
// PerYearOverrides (json:"-") a second time even though Apply already
// re-attached it once. Re-attach it again onto the prepared snapshot,
// following the same shape as analysis/tax_optimizer.go's
// cloneSettingsWithSSAndRoth (lines 90-100): the override map is
// reconstructed in-memory and never persisted, so this is a deliberate,
// narrow mutation of the prepared snapshot rather than a violation of the
// "don't mutate base" rule (base itself is untouched).
func preparedWithOverrides(base *models.WhatIfSettings, o Overrides) (prepare.PreparedSettings, error) {
	s, err := Apply(base, o)
	if err != nil {
		return prepare.PreparedSettings{}, err
	}
	prepared, err := prepare.From(s)
	if err != nil {
		return prepare.PreparedSettings{}, fmt.Errorf("prepare settings: %w", err)
	}
	if base.RothConversion != nil && base.RothConversion.PerYearOverrides != nil {
		if prepSettings := prepared.Settings(); prepSettings != nil && prepSettings.RothConversion != nil {
			prepSettings.RothConversion.PerYearOverrides = base.RothConversion.PerYearOverrides
		}
	}
	return prepared, nil
}

// RunWithOverrides applies the overrides and runs the full analysis, returning
// the shaped view. Monte Carlo is excluded: the orchestrator auto-seeds its RNG
// from the clock, so including it would make two identical runs disagree.
func RunWithOverrides(base *models.WhatIfSettings, o Overrides) (AnalysisView, error) {
	prepared, err := preparedWithOverrides(base, o)
	if err != nil {
		return AnalysisView{}, err
	}
	a := retirement.RunFull(engine.New(), engine.Input{Prepared: prepared})
	return ShapeAnalysis(a, false), nil
}
