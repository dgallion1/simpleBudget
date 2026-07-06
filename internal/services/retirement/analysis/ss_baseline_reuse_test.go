package analysis

import (
	"reflect"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// ssBaselineReuseSettings builds a primary-eligible SS portfolio scenario
// (ClaimAge > CurrentAge) kept small so the grid stays cheap.
func ssBaselineReuseSettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-04"
	s.PortfolioValue = 1_000_000
	s.MonthlyLivingExpenses = 5000
	s.TaxDeferredPercent = 60
	s.RothPercent = 10
	s.ProjectionYears = 12
	s.CurrentAge = 67
	s.Persons = []models.Person{
		{ID: "p1", Name: "You", BirthMonth: "1958-11", Role: models.PersonRolePrimary},
	}
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 4100,
		FRA:        66,
		COLARate:   0.02,
		ClaimAge:   68, // > CurrentAge → primary eligible
	}
	return s
}

// TestSSPortfolioBaselineFromMainMCMatchesResimulatedCell pins the
// Finding-3 refactor: under a common pinned seed, deriving the baseline
// cell from the first ssPortfolioMonteCarloRuns per-run results of the
// main Monte Carlo must be byte-identical to re-simulating the cell (the
// old path). This holds because the baseline cell clones the settings
// with the CURRENT claim ages (a semantic no-op) and per-run seeds derive
// from the same master sequence regardless of total run count, so cell
// runs 0..249 ≡ main runs 0..249.
func TestSSPortfolioBaselineFromMainMCMatchesResimulatedCell(t *testing.T) {
	in := engineInput(t, ssBaselineReuseSettings())
	eng := engine.New()
	const seed = 20260706

	ssAnalysis := SSAnalysis(in)
	if ssAnalysis == nil {
		t.Fatal("precondition: SS analysis must run")
	}

	oldPath := SSPortfolioWithSeed(eng, in, ssAnalysis, seed)
	if oldPath == nil {
		t.Fatal("precondition: SS portfolio analysis must run (old path)")
	}

	// Main MC with more runs than the cell needs, same seed — the
	// orchestrator's RunFull shape (1000 main runs, 250-cell baseline).
	_, mainRuns := MonteCarloWithResults(eng, in, 300, seed)
	if len(mainRuns) < ssPortfolioMonteCarloRuns {
		t.Fatalf("precondition: main MC produced %d runs, need >= %d", len(mainRuns), ssPortfolioMonteCarloRuns)
	}

	newPath := SSPortfolioFromMainMC(eng, in, ssAnalysis, seed, mainRuns)
	if newPath == nil {
		t.Fatal("SS portfolio analysis must run (baseline-reuse path)")
	}

	if !reflect.DeepEqual(oldPath, newPath) {
		t.Errorf("baseline-reuse path diverges from re-simulated cell:\nold: %+v\nnew: %+v", oldPath, newPath)
	}
}

// TestSSPortfolioFromMainMCFallsBackWithoutEnoughRuns: with fewer main-MC
// results than the cell size (or none), the baseline cell must be
// re-simulated — identical to SSPortfolioWithSeed.
func TestSSPortfolioFromMainMCFallsBackWithoutEnoughRuns(t *testing.T) {
	in := engineInput(t, ssBaselineReuseSettings())
	eng := engine.New()
	const seed = 424242

	ssAnalysis := SSAnalysis(in)
	oldPath := SSPortfolioWithSeed(eng, in, ssAnalysis, seed)
	viaNil := SSPortfolioFromMainMC(eng, in, ssAnalysis, seed, nil)
	viaShort := SSPortfolioFromMainMC(eng, in, ssAnalysis, seed, make([]models.MonteCarloResult, ssPortfolioMonteCarloRuns-1))

	if !reflect.DeepEqual(oldPath, viaNil) {
		t.Error("nil main runs must fall back to re-simulating the baseline cell")
	}
	if !reflect.DeepEqual(oldPath, viaShort) {
		t.Error("short main runs must fall back to re-simulating the baseline cell")
	}
}
