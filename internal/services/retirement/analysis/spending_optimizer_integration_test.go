package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Changes to obligations-first funding, real-cent rounding, or full-horizon
// observation must break these independently calculated engine/observer oracles.
func TestSpendingIntegrationIncomeOnlyAccounting(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		income, funded, unpaid   float64
		cuts, floor, obligations int
		flexible, planned        bool
	}{
		{"cut above minimum", 115, 95, 0, 36, 0, 0, true, false},
		{"below minimum", 105, 85, 0, 36, 36, 0, false, false},
		{"overlapping failures", 15, 0, 5, 36, 36, 36, false, false},
		{"zero assets full plan", 120, 100, 0, 0, 0, 0, true, true},
		{"single cent below minimum", 109.99, 89.99, 0, 36, 36, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := floorRegressionSettings()
			s.Guardrails = nil
			s.PortfolioValue = 0
			s.MonthlyLivingExpenses = 100
			// Property tax makes this small-dollar MC oracle exact. Healthcare variation
			// and configured healthcare remain in the stress and schedule fixtures below.
			s.MonthlyPropertyTax = 20
			s.IncomeSources = []models.IncomeSource{{ID: "income", Name: "income", Type: models.IncomeFixed, Amount: tc.income}}
			in := engineInput(t, s)
			ps := in.Prepared.Settings()
			if ps.MonthlyLivingExpenses != 100 || ps.MonthlyPropertyTax != 20 || ps.PortfolioValue != 0 {
				t.Fatal("prepared fixture changed")
			}
			tr := newSpendingExperienceTracker(90, 1)
			st := engine.NewProjectionState(in)
			for m := 0; m < ps.ProjectionYears*12; m++ {
				out := st.StepMonth(m, func(*models.WhatIfSettings, int) engine.MonthReturns {
					return engine.MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
				})
				floorRegressionNear(t, "planned", out.LivingExpenses, 100)
				floorRegressionNear(t, "adjusted", out.AdjustedLivingExpenses, 100)
				floorRegressionNear(t, "funded", out.FundedLivingExpenses, tc.funded)
				floorRegressionNear(t, "unpaid obligations", math.Max(0, out.Result.Shortfall-out.AdjustedLivingExpenses), tc.unpaid)
				tr.observe(spendingMonthObservation{Planned: 100, Adjusted: 100, Funded: tc.funded, Shortfall: 120 - tc.income, CPI: 1, Depleted: true})
			}
			cfg := &MonteCarloConfig{MinMonthlySpendingReal: 90, SpendingExperienceYears: 1, CaptureSpendingYears: true}
			got := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(812)), cfg)
			if !reflect.DeepEqual(got.SpendingOutcome, tr.result()) {
				t.Fatalf("simulated capture differs from hand oracle: got=%+v want=%+v", got.SpendingOutcome, tr.result())
			}
			o := got.SpendingOutcome
			if o.MonthsObserved != 36 || o.MonthsBelowPlan != tc.cuts || o.FloorShortfallMonths != tc.floor || o.UnpaidObligationMonths != tc.obligations || len(got.FloorOutcome.Years) != 3 {
				t.Fatalf("full-horizon counters: %+v", o)
			}
			metrics, err := summarizeSpendingRisk([]models.MonteCarloResult{got}, 1)
			if err != nil {
				t.Fatal(err)
			}
			for kind, want := range map[string]bool{"planned": tc.planned, "flexible": tc.flexible} {
				if spendingCandidateQualifies(models.SpendingCandidate{Kind: kind, Metrics: metrics}) != want {
					t.Fatalf("%s qualification", kind)
				}
			}
		})
	}
}

func TestSpendingIntegrationOneCentFinalMonth(t *testing.T) {
	s := floorRegressionSettings()
	s.Guardrails = nil
	s.PortfolioValue = 0
	s.MonthlyLivingExpenses = 90
	s.MonthlyPropertyTax = 20
	s.IncomeSources = []models.IncomeSource{{ID: "income", Amount: 110, Type: models.IncomeFixed, EndMonth: new(35)}, {ID: "last", Amount: 109.99, Type: models.IncomeFixed, StartMonth: 35}}
	cfg := &MonteCarloConfig{MinMonthlySpendingReal: 90, SpendingExperienceYears: 1, CaptureSpendingYears: true}
	bad := RunSingleMonteCarloSimulation(engineInput(t, s), rand.New(rand.NewSource(812)), cfg)
	if bad.SpendingOutcome.FloorShortfallMonths != 1 || bad.SpendingOutcome.MinFundedMonth != 36 {
		t.Fatalf("last-cent fixture %+v", bad.SpendingOutcome)
	}
	s.IncomeSources[0].EndMonth = nil
	s.IncomeSources = s.IncomeSources[:1]
	good := RunSingleMonteCarloSimulation(engineInput(t, s), rand.New(rand.NewSource(812)), cfg)
	rows := make([]models.MonteCarloResult, 1000)
	for i := range rows {
		rows[i] = good
	}
	rows[999] = bad
	m, e := summarizeSpendingRisk(rows, 1)
	if e != nil {
		t.Fatal(e)
	}
	if m.FloorShortfallPaths != 1 || spendingCandidateQualifies(models.SpendingCandidate{Kind: "flexible", Metrics: m}) {
		t.Fatalf("one failed cent/path accepted: %+v", m)
	}
}

func TestSpendingIntegrationStochasticObserverParity(t *testing.T) {
	in := spendingBenchmarkInput(t)
	s := in.Prepared.Settings()
	s.InflationRate = 8
	s.ProjectionYears = 45
	s.HealthcarePersons = []models.HealthcarePerson{{ID: "care", PersonID: s.Persons[0].ID, CurrentCoverage: models.CoverageMedicare, MedicareEligibleAge: 65, MedicareMonthlyCost: 900, PostMedicareInflation: 4, CareStartAge: 85, CareMonthlyCost: 1800}}
	hooks := in.Hooks
	in = engineInput(t, s)
	in.Hooks = hooks
	cfg := DefaultMonteCarloConfig()
	cfg.MinMonthlySpendingReal = 7000
	cfg.CaptureSpendingYears = true
	cfg.CrashProbability = 1
	cfg.HealthShockProb = 1
	cfg.SpendingShockProb = 1
	observed := *cfg
	observed.SpendingExperienceYears = 5
	for _, seed := range []int64{281, 812, 20260910} {
		without := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(seed)), cfg)
		with := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(seed)), &observed)
		if with.HealthShocks == 0 || with.SpendingShocks == 0 || with.FirstCrashYear == 0 || len(with.FloorOutcome.Years) != with.ProjectionYears || with.ProjectionYears < 40 || with.SpendingOutcome.MonthsObserved != with.ProjectionYears*12 {
			t.Fatalf("stress fixture missing event/capture: %+v", with)
		}
		if with.FloorOutcome.Years[4].LivingNominal <= with.FloorOutcome.Years[4].LivingReal {
			t.Fatal("no stochastic inflation exposure")
		}
		with.SpendingOutcome = nil
		if !reflect.DeepEqual(without, with) {
			t.Fatal("observer changed seeded financial path or annual captures")
		}
	}
}

func TestSpendingIntegrationScheduleAndCuttableBoost(t *testing.T) {
	for _, stop := range []string{"2026-07", "2027-01", "2027-07"} {
		t.Run(stop, func(t *testing.T) {
			s := floorRegressionSettings()
			s.MonthlyLivingExpenses = 100
			s.Guardrails = nil
			s.MonthlyHealthcare = 20
			s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 30, StopMonth: stop}
			s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "early", StartAge: 0, Multiplier: 1}, {Name: "later", StartAge: 66, Multiplier: .8}}}
			in := engineInput(t, s)
			in.Hooks.SocialSecurityProjectionActive = func(*models.WhatIfSettings) bool { return true }
			in.Hooks.ProjectedSocialSecurityIncome = func(_ *models.WhatIfSettings, m int) float64 {
				if m >= 12 {
					return 120
				}
				return 0
			}
			st := engine.NewProjectionState(in)
			stopDate, _ := models.ParseYearMonth(stop)
			startDate, _ := models.ParseYearMonth(s.StartDate)
			stopM := (stopDate.Year()-startDate.Year())*12 + int(stopDate.Month()-startDate.Month())
			for m := 0; m < 36; m++ {
				o := st.StepMonth(m, func(*models.WhatIfSettings, int) engine.MonthReturns {
					return engine.MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
				})
				want := 100.0
				if m >= 12 {
					want = 80
				}
				if m < stopM {
					want += 30
				}
				floorRegressionNear(t, "phase plus boost once", o.LivingExpenses, want)
				floorRegressionNear(t, "health obligation retained", o.Healthcare, 20)
			}
			cfg := &MonteCarloConfig{MinMonthlySpendingReal: 90, SpendingExperienceYears: 1, CaptureSpendingYears: true}
			r := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(812)), cfg)
			wantFloorMonths := 36 - max(12, stopM)
			if r.SpendingOutcome.MonthsBelowPlan != 0 || r.SpendingOutcome.FloorShortfallMonths != wantFloorMonths {
				t.Fatalf("schedule became risk cut or phase floor missed: %+v", r.SpendingOutcome)
			}
			for y, row := range r.FloorOutcome.Years {
				want := 0.0
				for m := 12 * y; m < 12*(y+1); m++ {
					v := 100.0
					if m >= 12 {
						v = 80
					}
					if m < stopM {
						v += 30
					}
					want += v
				}
				floorRegressionNear(t, "actual annual schedule capture", row.LivingReal, want/12)
			}
		})
	}
	s := floorRegressionSettings()
	s.PortfolioValue = 0
	s.Guardrails = nil
	s.MonthlyLivingExpenses = 90
	s.MonthlyPropertyTax = 20
	s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 20, StopMonth: "2027-01"}
	s.IncomeSources = []models.IncomeSource{{ID: "income", Amount: 110, Type: models.IncomeFixed}}
	r := RunSingleMonteCarloSimulation(engineInput(t, s), rand.New(rand.NewSource(812)), &MonteCarloConfig{MinMonthlySpendingReal: 90, SpendingExperienceYears: 1, CaptureSpendingYears: true})
	if r.SpendingOutcome.MonthsBelowPlan != 12 || r.SpendingOutcome.FloorShortfallMonths != 0 || r.SpendingOutcome.UnpaidObligationMonths != 0 || r.SpendingOutcome.BelowPlanAtEnd {
		t.Fatalf("cuttable boost protected as obligation or expiry miscounted: %+v", r.SpendingOutcome)
	}
}

func TestSpendingIntegrationFundedNearTermAdvantage(t *testing.T) {
	s := floorRegressionSettings()
	s.ProjectionYears = 10
	s.PortfolioValue = 1800
	s.MonthlyLivingExpenses = 90
	s.MonthlyPropertyTax = 20
	s.Guardrails = nil
	s.IncomeSources = []models.IncomeSource{{ID: "income", Amount: 110, Type: models.IncomeFixed}}
	in := engineInput(t, s)
	req := models.SpendingOptimizerRequest{FloorMonthlyReal: 90, NearTermYears: 5, SearchMinMonthlyReal: 90, SearchMaxMonthlyReal: 120, SearchStepMonthlyReal: 30, Seed: 812}
	runner := func(ctx context.Context, c engine.Input, seed int64, n int, r models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error) {
		// All actual engine months execute; the resulting deterministic future is
		// repeated to exercise search stages without inventing financial outcomes.
		row := RunSingleMonteCarloSimulation(c, rand.New(rand.NewSource(seed)), &MonteCarloConfig{MinMonthlySpendingReal: r.FloorMonthlyReal, SpendingExperienceYears: r.NearTermYears, CaptureSpendingYears: true})
		rows := make([]models.MonteCarloResult, n)
		for i := range rows {
			rows[i] = row
		}
		return rows, nil
	}
	result, e := optimizeSpendingWithRunner(context.Background(), in, req, runner)
	if e != nil {
		t.Fatal(e)
	}
	leaders := map[string]models.SpendingCandidate{}
	for _, c := range result.Candidates {
		for _, id := range result.RecommendationIDs {
			if c.ID == id {
				leaders[c.Kind] = c
			}
		}
	}
	p, f := leaders["planned"], leaders["flexible"]
	if p.Metrics == nil || f.Metrics == nil || f.Metrics.MedianNearTermMonthlyReal <= p.Metrics.MedianNearTermMonthlyReal || f.Metrics.CutPaths != 1000 || f.Metrics.FloorShortfallPaths != 0 || f.Metrics.UnpaidObligationPaths != 0 || len(f.WorstPathYears) != 10 || f.WorstPathYears[9].LivingReal >= f.WorstPathYears[0].LivingReal {
		t.Fatalf("funded advantage and later cuts: planned=%+v flexible=%+v", p.Metrics, f.Metrics)
	}
	t.Logf("deterministic master=812 planned funded=%.2f flexible funded=%.2f cut paths=%d/1000 full months=120", p.Metrics.MedianNearTermMonthlyReal, f.Metrics.MedianNearTermMonthlyReal, f.Metrics.CutPaths)
	req.FloorMonthlyReal = 110
	req.SearchMinMonthlyReal = 110
	req.SearchMaxMonthlyReal = 120
	req.SearchStepMonthlyReal = 10
	failed, e := optimizeSpendingWithRunner(context.Background(), in, req, runner)
	if e != nil {
		t.Fatal(e)
	}
	if len(failed.RecommendationIDs) != 0 {
		t.Fatal("infeasible minimum recommended")
	}
	for _, c := range failed.Candidates {
		if c.Metrics.FloorShortfallPaths != 1000 || c.Qualifies {
			t.Fatal("failed futures hidden")
		}
	}
}

func TestSpendingIntegrationActiveCancellationLatency(t *testing.T) {
	in := spendingBenchmarkInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	var once sync.Once
	old := in.Hooks.ProjectedSocialSecurityIncome
	in.Hooks.ProjectedSocialSecurityIncome = func(s *models.WhatIfSettings, m int) float64 { once.Do(func() { close(started) }); return old(s, m) }
	done := make(chan error, 1)
	go func() {
		result, e := OptimizeSpending(ctx, in, models.SpendingOptimizerRequest{FloorMonthlyReal: 7000, Seed: 20260910})
		if result != nil {
			done <- fmt.Errorf("published partial result")
			return
		}
		done <- e
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("search never started")
	}
	start := time.Now()
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
		t.Logf("active 35y search cancellation=%s workers<=%d", time.Since(start), runtime.GOMAXPROCS(0))
	case <-time.After(2 * time.Second):
		t.Fatal("active cancellation exceeded 2 seconds")
	}
}

func BenchmarkSpendingIntegrationHorizons(b *testing.B) {
	for _, years := range []int{10, 45} {
		b.Run(fmt.Sprintf("years-%d", years), func(b *testing.B) {
			base := spendingBenchmarkInput(b)
			s := base.Prepared.Settings()
			s.ProjectionYears = years
			in := engineInput(b, s)
			in.Hooks = base.Hooks
			req := models.SpendingOptimizerRequest{FloorMonthlyReal: 7000, NearTermYears: 5, Seed: 20260910, LivingSpendingBoost: models.CloneLivingSpendingBoost(s.LivingSpendingBoost)}
			_, grid, e := normalizeSpendingRequest(in, req)
			if e != nil {
				b.Fatal(e)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				paths, calls, lo, hi := 0, 0, 1000, 0
				runner := func(ctx context.Context, c engine.Input, seed int64, n int, r models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error) {
					paths += n
					calls++
					rows, e := runSpendingScenarios(ctx, c, seed, n, r)
					for _, row := range rows {
						lo = min(lo, row.ProjectionYears)
						hi = max(hi, row.ProjectionYears)
					}
					return rows, e
				}
				result, e := optimizeSpendingWithRunner(context.Background(), in, req, runner)
				if e != nil {
					b.Fatal(e)
				}
				b.ReportMetric(float64(paths), "paths/op")
				b.ReportMetric(float64(calls), "evaluations/op")
				b.ReportMetric(float64(len(result.Candidates)), "final-candidates/op")
				b.Logf("master=%d selection=%d final=%d workers=%d grid_positions=%d range=%.2f..%.2f step=%.2f samples=%d/%d/%d observed_horizon=%d..%d", result.SearchSeed, result.SelectionSeed, result.ValidationSeed, runtime.GOMAXPROCS(0), len(grid), result.EffectiveMinMonthlyReal, result.EffectiveMaxMonthlyReal, result.ResolutionMonthlyReal, result.SearchRuns, result.SelectionRuns, result.ValidationRuns, lo, hi)
			}
		})
	}
}
