package retirement

import (
	"reflect"
	"testing"

	"budget2/internal/models"
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
	// SetPrimaryAge pins the primary BirthMonth too — prepare.ComputeAges
	// recomputes CurrentAge from BirthMonth+StartDate, so a raw
	// s.CurrentAge assignment would silently run the scenario at the
	// default person's age instead of 64.
	s.SetPrimaryAge(64)
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

	// Guard the fixture's prepared age: prepare recomputes CurrentAge from
	// BirthMonth+StartDate, so if SetPrimaryAge ever regresses to a raw
	// CurrentAge assignment this scenario silently runs at a different age
	// (third strike for this fixture pattern).
	if got := in.Prepared.Settings().CurrentAge; got != 64 {
		t.Fatalf("prepared CurrentAge = %d; fixture must run at 64", got)
	}

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

// TestRunFastMatchesRunFullCheapFields is the refactor's no-drift oracle:
// RunFast must leave the expensive fields nil and its cheap fields must be
// reflect.DeepEqual to the same fields off a seeded full run, proving
// fastAnalysis (shared by RunFast and runFullWithSeed) was not duplicated
// or drifted by the refactor.
func TestRunFastMatchesRunFullCheapFields(t *testing.T) {
	s := parallelFanOutSettings()
	in := engine.Input{Prepared: prepare.MustFrom(t, s)}
	eng := engine.New()

	fast := RunFast(eng, in)
	full := runFullWithSeed(eng, in, 42)

	if fast.Sensitivity != nil {
		t.Error("RunFast.Sensitivity must be nil")
	}
	if fast.FailurePoints != nil {
		t.Error("RunFast.FailurePoints must be nil")
	}
	if fast.MonteCarlo != nil {
		t.Error("RunFast.MonteCarlo must be nil")
	}
	if fast.HistoricalBacktest != nil {
		t.Error("RunFast.HistoricalBacktest must be nil")
	}
	if fast.SocialSecurity != nil {
		t.Error("RunFast.SocialSecurity must be nil")
	}

	if !reflect.DeepEqual(fast.Projection, full.Projection) {
		t.Error("Projection differs between RunFast and RunFull")
	}
	if !reflect.DeepEqual(fast.BudgetFit, full.BudgetFit) {
		t.Error("BudgetFit differs between RunFast and RunFull")
	}
	if !reflect.DeepEqual(fast.PresentValue, full.PresentValue) {
		t.Error("PresentValue differs between RunFast and RunFull")
	}
	if !reflect.DeepEqual(fast.Sustainability, full.Sustainability) {
		t.Error("Sustainability differs between RunFast and RunFull")
	}
	if !reflect.DeepEqual(fast.RMD, full.RMD) {
		t.Error("RMD differs between RunFast and RunFull")
	}
	if !reflect.DeepEqual(fast.Tax, full.Tax) {
		t.Error("Tax differs between RunFast and RunFull")
	}
	if !reflect.DeepEqual(fast.ProjectionExplainability, full.ProjectionExplainability) {
		t.Error("ProjectionExplainability differs between RunFast and RunFull")
	}
	if !reflect.DeepEqual(fast.Settings, full.Settings) {
		t.Error("Settings differs between RunFast and RunFull")
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
