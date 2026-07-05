package retirement

import (
	"reflect"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// parallelFanOutSettings builds a scenario that exercises every
// concurrent branch of runFullWithSeed: an explicit InvestmentReturn so
// all four failure-point searches run, healthcare persons so the
// sensitivity healthcare scenario takes the per-person path, and a
// two-person SS config so the SS comparison + portfolio analysis spawn.
func parallelFanOutSettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 1_200_000
	s.MonthlyLivingExpenses = 5500
	s.MonthlyHealthcare = 400
	s.InvestmentReturn = 6.0
	s.InflationRate = 3.0
	s.ProjectionYears = 12
	s.CurrentAge = 64
	s.SpendingPhaseConfig = nil
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 2400,
		FRA:        67,
		ClaimAge:   67,
	}
	return s
}

// TestRunFullDeterministicUnderParallelism pins the Candidate #3
// contract: the concurrent fan-out (sensitivity scenarios, failure-point
// searches, backtest worker pool, Monte Carlo, SS analysis) must produce
// bit-identical results across runs for a pinned seed — independent of
// goroutine scheduling. Two full runs over the same prepared input are
// compared sub-analysis by sub-analysis.
func TestRunFullDeterministicUnderParallelism(t *testing.T) {
	s := parallelFanOutSettings()
	in := engine.Input{Prepared: prepare.MustFrom(t, s)}
	eng := engine.New()

	const seed = 42
	a := runFullWithSeed(eng, in, seed)
	b := runFullWithSeed(eng, in, seed)

	if a.SocialSecurity == nil || a.SocialSecurity.Portfolio == nil {
		t.Fatal("precondition: SS analysis + portfolio must run so the SS fan-out branch is exercised")
	}
	if len(a.FailurePoints.FailurePoints) == 0 {
		t.Fatal("precondition: expected at least one failure point")
	}
	if len(a.Sensitivity) != 6 {
		t.Fatalf("precondition: expected 6 sensitivity scenarios, got %d", len(a.Sensitivity))
	}

	if !reflect.DeepEqual(a.Projection, b.Projection) {
		t.Error("Projection differs between identical seeded runs")
	}
	if !reflect.DeepEqual(a.Sensitivity, b.Sensitivity) {
		t.Errorf("Sensitivity differs between identical seeded runs:\n%v\nvs\n%v", a.Sensitivity, b.Sensitivity)
	}
	if !reflect.DeepEqual(a.FailurePoints, b.FailurePoints) {
		t.Errorf("FailurePoints differ between identical seeded runs:\n%v\nvs\n%v", a.FailurePoints, b.FailurePoints)
	}
	if !reflect.DeepEqual(a.MonteCarlo, b.MonteCarlo) {
		t.Error("MonteCarlo differs between identical seeded runs")
	}
	if !reflect.DeepEqual(a.HistoricalBacktest, b.HistoricalBacktest) {
		t.Error("HistoricalBacktest differs between identical seeded runs")
	}
	if !reflect.DeepEqual(a.SocialSecurity, b.SocialSecurity) {
		t.Error("SocialSecurity analysis differs between identical seeded runs")
	}
	if !reflect.DeepEqual(a.BudgetFit, b.BudgetFit) {
		t.Error("BudgetFit differs between identical seeded runs")
	}
}

// TestSensitivityWithBaselineMatchesSensitivity pins the baseline-reuse
// refactor: handing Sensitivity/FailurePoints the already-computed
// baseline must not change their output versus computing it themselves.
func TestSensitivityWithBaselineMatchesSensitivity(t *testing.T) {
	s := parallelFanOutSettings()
	in := engine.Input{Prepared: prepare.MustFrom(t, s), Hooks: DefaultHooks()}
	eng := engine.New()

	viaSelf := analysis.Sensitivity(eng, in)
	proj := eng.Run(in)
	viaBaseline := analysis.SensitivityWithBaseline(eng, in, proj, analysis.BudgetFit(in, proj))

	if !reflect.DeepEqual(viaSelf, viaBaseline) {
		t.Errorf("SensitivityWithBaseline diverges from Sensitivity:\n%v\nvs\n%v", viaSelf, viaBaseline)
	}

	fpSelf := analysis.FailurePoints(eng, in)
	fpBaseline := analysis.FailurePointsWithBaseline(eng, in, proj)
	if !reflect.DeepEqual(fpSelf, fpBaseline) {
		t.Errorf("FailurePointsWithBaseline diverges from FailurePoints:\n%v\nvs\n%v", fpSelf, fpBaseline)
	}
}

// BenchmarkRunFull tracks the wall-clock cost of the full analysis
// fan-out — the number the whatif recalc path pays on every cache miss.
func BenchmarkRunFull(b *testing.B) {
	s := parallelFanOutSettings()
	in := engine.Input{Prepared: prepare.MustFrom(b, s)}
	eng := engine.New()

	b.ResetTimer()
	for b.Loop() {
		runFullWithSeed(eng, in, 42)
	}
}

// BenchmarkRunFullNoSS is BenchmarkRunFull without the SS comparison —
// the shape of a plan with no Social Security config, where the
// sensitivity/failure-point/backtest fan-out dominates the wall-clock.
func BenchmarkRunFullNoSS(b *testing.B) {
	s := parallelFanOutSettings()
	s.SocialSecurity = nil
	in := engine.Input{Prepared: prepare.MustFrom(b, s)}
	eng := engine.New()

	b.ResetTimer()
	for b.Loop() {
		runFullWithSeed(eng, in, 42)
	}
}
