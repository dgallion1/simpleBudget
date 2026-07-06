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
		// BirthMonth derived from StartDate so the prepared age stays
		// pinned at 67 even if the fixture's StartDate changes — a
		// hardcoded month would silently drift the age and flip the
		// ClaimAge(68) > CurrentAge eligibility precondition.
		{ID: "p1", Name: "You", BirthMonth: models.BirthMonthForAge(s.StartDate, 67), Role: models.PersonRolePrimary},
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
// old path). This holds because the baseline cell holds the CURRENT claim
// ages and per-run seeds derive from the same master sequence regardless
// of total run count, so cell runs 0..249 ≡ main runs 0..249.
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
	if len(mainRuns.Runs) < ssPortfolioMonteCarloRuns {
		t.Fatalf("precondition: main MC produced %d runs, need >= %d", len(mainRuns.Runs), ssPortfolioMonteCarloRuns)
	}
	if mainRuns.Seed != seed {
		t.Fatalf("MonteCarloWithResults must report the pinned seed, got %d want %d", mainRuns.Seed, seed)
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
	if oldPath == nil {
		t.Fatal("precondition: SS portfolio analysis must run (re-simulated path)")
	}
	viaEmpty := SSPortfolioFromMainMC(eng, in, ssAnalysis, seed, MainMCRuns{})
	viaShort := SSPortfolioFromMainMC(eng, in, ssAnalysis, seed, MainMCRuns{
		Runs: make([]models.MonteCarloResult, ssPortfolioMonteCarloRuns-1),
		Seed: seed,
	})

	if !reflect.DeepEqual(oldPath, viaEmpty) {
		t.Error("empty main runs must fall back to re-simulating the baseline cell")
	}
	if !reflect.DeepEqual(oldPath, viaShort) {
		t.Error("short main runs must fall back to re-simulating the baseline cell")
	}
}

// TestSSPortfolioFromMainMCIgnoresRunsFromDifferentSeed pins the
// common-random-numbers provenance guard: main-MC runs simulated under a
// DIFFERENT seed than the grid cells must be ignored (baseline
// re-simulated under the grid seed), never aggregated — otherwise every
// DeltaSurvivalRate would compare success rates across non-common random
// paths.
func TestSSPortfolioFromMainMCIgnoresRunsFromDifferentSeed(t *testing.T) {
	in := engineInput(t, ssBaselineReuseSettings())
	eng := engine.New()
	const gridSeed = 20260706
	const otherSeed = 999

	ssAnalysis := SSAnalysis(in)
	oldPath := SSPortfolioWithSeed(eng, in, ssAnalysis, gridSeed)
	if oldPath == nil {
		t.Fatal("precondition: SS portfolio analysis must run (re-simulated path)")
	}

	_, mismatched := MonteCarloWithResults(eng, in, 300, otherSeed)
	viaMismatch := SSPortfolioFromMainMC(eng, in, ssAnalysis, gridSeed, mismatched)
	if viaMismatch == nil {
		t.Fatal("mismatched-seed runs must fall back, not bail out")
	}
	if !reflect.DeepEqual(oldPath, viaMismatch) {
		t.Error("runs from a different seed must be ignored and the baseline cell re-simulated under the grid seed")
	}
}
