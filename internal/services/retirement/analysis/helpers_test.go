package analysis

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// runProj prepares the given settings and runs the engine projection.
// Use from analysis-package tests instead of building a Calculator: the
// analysis package must not import its retirement parent.
func runProj(tb testing.TB, s *models.WhatIfSettings) (*models.ProjectionResult, engine.Input) {
	tb.Helper()
	in := engine.Input{Prepared: prepare.MustFrom(tb, s)}
	return engine.New().Run(in), in
}

// engineInput is a shorthand for tests that only need the input value
// (e.g., when feeding BuildRMD a fixture projection).
func engineInput(tb testing.TB, s *models.WhatIfSettings) engine.Input {
	tb.Helper()
	return engine.Input{Prepared: prepare.MustFrom(tb, s)}
}
