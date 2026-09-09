package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"context"
	"errors"
	"math"
	"math/rand"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestGuardrailOptimizerRealRunnerMatchesScenarioDraws(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 8000
	s.PortfolioValue = 5000000
	s.ProjectionYears = 10
	s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 10, FloorCutPct: 10, CeilingRisePct: 15, CeilingRaisePct: 5, MaxSpendingPct: 120, MinMonthlySpendingReal: 7500}
	in := engineInput(t, s)
	in.Hooks.SocialSecurityProjectionActive = func(*models.WhatIfSettings) bool { return true }
	in.Hooks.ProjectedSocialSecurityIncome = func(*models.WhatIfSettings, int) float64 { return 12000 }
	before := *in.Prepared.Settings().Guardrails
	rows, err := runGuardrailOptimizerScenarios(context.Background(), in, 417, 3, 7500)
	if err != nil {
		t.Fatal(err)
	}
	again, err := runGuardrailOptimizerScenarios(context.Background(), in, 417, 3, 7500)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows, again) {
		t.Fatal("seeded actual simulations differ")
	}
	master := rand.New(rand.NewSource(417))
	cfg := DefaultMonteCarloConfig()
	cfg.MinMonthlySpendingReal = 7500
	cfg.CaptureSpendingYears = true
	for i, row := range rows {
		expected := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(master.Int63())), cfg)
		if !reflect.DeepEqual(row, expected) {
			t.Fatalf("scenario %d lost input hooks, observation config or scenario seed", i)
		}
		if row.FloorOutcome == nil || row.FloorOutcome.MonthsObserved != row.ProjectionYears*12 {
			t.Fatalf("observer not full horizon: %+v", row)
		}
	}
	if !reflect.DeepEqual(before, *in.Prepared.Settings().Guardrails) {
		t.Fatal("runner mutated policy")
	}
	without := in
	without.Hooks = engine.Hooks{}
	noHooks, err := runGuardrailOptimizerScenarios(context.Background(), without, 417, 3, 7500)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(rows, noHooks) {
		t.Fatal("fixture does not distinguish preserved income hooks")
	}
}

func TestGuardrailOptimizerRealRunnerObservesBaselineWithoutFloorEnforcement(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 8000
	s.PortfolioValue = 1e8
	s.SpendingDeclineRate = 10
	s.ProjectionYears = 10
	s.Guardrails = nil
	in := engineInput(t, s)
	rows, err := runGuardrailOptimizerScenarios(context.Background(), in, 789, 2, 7500)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.FloorOutcome == nil || !r.FloorOutcome.FloorFailed {
			t.Fatal("observation floor incorrectly enforced on disabled baseline")
		}
	}
	if in.Prepared.Settings().Guardrails != nil {
		t.Fatal("baseline acquired guardrails")
	}
}

func TestGuardrailOptimizerRealRunnerCancelsBetweenRuns(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.ProjectionYears = 10
	in := engineInput(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int64
	in.Hooks.SocialSecurityProjectionActive = func(*models.WhatIfSettings) bool { calls.Add(1); cancel(); return false }
	rows, err := runGuardrailOptimizerScenarios(ctx, in, 22, 1000, 7500)
	if !errors.Is(err, context.Canceled) || rows != nil {
		t.Fatalf("canceled runner returned partial success: %d %v", len(rows), err)
	}
	if calls.Load() == 0 || calls.Load() > 10000 {
		t.Fatalf("cancellation didn't bound in-flight work: %d hook calls", calls.Load())
	}
}

func TestGuardrailOptimizerMetricsPercentilesAndBoundary(t *testing.T) {
	rows := make([]models.MonteCarloResult, 1000)
	for i := range rows {
		rows[i] = models.MonteCarloResult{Survives: true, FloorOutcome: &models.MonteCarloFloorOutcome{FloorFailed: i < 50, TotalFundedLivingReal: float64(i + 1), WorstAnnualCutPct: float64(i+1) / 10, AnnualCutCount: i % 3, FinalBalanceReal: float64(1000 - i)}}
	}
	m, err := guardrailOptimizerMetrics(rows)
	if err != nil {
		t.Fatal(err)
	}
	if m.MedianLifetimeFundedLivingReal != 500.5 || math.Abs(m.P10LifetimeFundedLivingReal-100.9) > 1e-10 || math.Abs(m.P95WorstAnnualCutPct-95.005) > 1e-10 || m.MedianEndingBalanceReal != 500.5 {
		t.Fatalf("linear percentiles incorrect: %+v", m)
	}
	if !guardrailOptimizerQualifies(m, 95) || guardrailOptimizerQualifies(m, 95.000001) {
		t.Fatal("inclusive exact count qualification failed")
	}
	rows[0].FloorOutcome = nil
	if _, err := guardrailOptimizerMetrics(rows); err == nil {
		t.Fatal("missing funded observations accepted")
	}
}

func TestGuardrailOptimizerRecommendationsStayParetoAfterValidation(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 8000
	in := engineInput(t, s)
	run := func(_ context.Context, i engine.Input, _ int64, n int, _ float64) ([]models.MonteCarloResult, error) {
		// Search ties keep the stable first configurations. Validation reveals that
		// the cap-150 policy spends equally but makes fewer and milder cuts.
		cut := 5.0
		count := 2
		if n == 1000 && i.Prepared.Settings().Guardrails != nil && i.Prepared.Settings().Guardrails.MaxSpendingPct == 150 {
			cut = 1
			count = 1
		}
		rows := make([]models.MonteCarloResult, n)
		for j := range rows {
			rows[j] = models.MonteCarloResult{Survives: true, FloorOutcome: &models.MonteCarloFloorOutcome{TotalFundedLivingReal: 100, WorstAnnualCutPct: cut, AnnualCutCount: count}}
		}
		return rows, nil
	}
	r, err := optimizeGuardrailsWithRunner(context.Background(), in, models.GuardrailOptimizerRequest{FloorMonthlyReal: 7500, TargetSuccessPct: 95, Seed: 123}, run)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range r.Recommendations {
		for _, c := range r.Candidates {
			if c.ID == id && c.Metrics.P95WorstAnnualCutPct != 1 {
				t.Fatalf("held-out dominated policy recommended: %+v", c)
			}
		}
	}
}

// Qualification uses the unrounded count-derived rate, including custom targets.
func TestGuardrailOptimizerAllExactTenthsBoundaries(t *testing.T) {
	for successes := 1; successes < 1000; successes++ {
		target := float64(successes) / 10
		m := models.GuardrailOptimizerMetrics{Runs: 1000, FloorShortfallPaths: 1000 - successes}
		if !guardrailOptimizerQualifies(m, target) {
			t.Errorf("%d/1000 must qualify at exact target %.17g", successes, target)
		}
		above := math.Nextafter(target, math.Inf(1))
		if guardrailOptimizerQualifies(m, above) {
			t.Errorf("%d/1000 must not qualify above its rate: %.17g", successes, above)
		}
	}
}

func TestGuardrailOptimizerCustomBoundaryService(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 8000
	run := func(_ context.Context, _ engine.Input, _ int64, n int, _ float64) ([]models.MonteCarloResult, error) {
		rows := make([]models.MonteCarloResult, n)
		for j := range rows {
			rows[j] = models.MonteCarloResult{Survives: true, FloorOutcome: &models.MonteCarloFloorOutcome{
				FloorFailed: n == 1000 && j >= 644, TotalFundedLivingReal: 100, MonthsObserved: 120,
			}}
		}
		return rows, nil
	}
	for _, target := range []float64{64.4, math.Nextafter(64.4, math.Inf(1))} {
		result, err := optimizeGuardrailsWithRunner(context.Background(), engineInput(t, s), models.GuardrailOptimizerRequest{
			FloorMonthlyReal: 7500, TargetSuccessPct: target, Seed: 123,
		}, run)
		if err != nil {
			t.Fatal(err)
		}
		wantQualifies := target == 64.4
		if (len(result.Recommendations) > 0) != wantQualifies {
			t.Fatalf("target %.17g: recommendations=%d, want qualifying=%v", target, len(result.Recommendations), wantQualifies)
		}
		if len(result.Candidates) == 0 {
			t.Fatal("missing measured candidates")
		}
		for _, candidate := range result.Candidates {
			if candidate.Metrics.Runs != 1000 || candidate.Metrics.FloorShortfallPaths != 356 || candidate.Metrics.FloorSuccessPct != 64.4 || candidate.Qualifies != wantQualifies {
				t.Fatalf("target %.17g: unexpected candidate: %+v", target, candidate)
			}
		}
	}
}
