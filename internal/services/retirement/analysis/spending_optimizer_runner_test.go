package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
)

func TestSpendingRunnerMatchesCanonicalPathsAndHooks(t *testing.T) {
	for _, name := range []string{"inflation", "early crash", "health shock", "phase change", "late depletion"} {
		t.Run(name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.StartDate = "2026-09"
			s.ProjectionYears = 10
			s.MonthlyLivingExpenses = 8000
			s.PortfolioValue = 1500000
			s.InflationRate = 8
			if name == "early crash" {
				s.PortfolioValue = 100000
			}
			if name == "phase change" {
				s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "initial", StartAge: 0, Multiplier: 1}, {Name: "later", StartAge: 70, Multiplier: .6}}}
			}
			if name == "late depletion" {
				s.PortfolioValue = 700000
			}
			in := engineInput(t, s)
			in.Hooks.SocialSecurityProjectionActive = func(*models.WhatIfSettings) bool { return true }
			in.Hooks.ProjectedSocialSecurityIncome = func(*models.WhatIfSettings, int) float64 { return 1200 }
			req := spendingSearchRequest()
			rows, err := runSpendingScenarios(context.Background(), in, 42, 4, req)
			if err != nil {
				t.Fatal(err)
			}
			cfg := DefaultMonteCarloConfig()
			cfg.MinMonthlySpendingReal = req.FloorMonthlyReal
			cfg.CaptureSpendingYears = true
			cfg.SpendingExperienceYears = req.NearTermYears
			cfg.AdaptiveSpending = false
			master := rand.New(rand.NewSource(42))
			for i := range rows {
				want := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(master.Int63())), cfg)
				if !reflect.DeepEqual(rows[i], want) {
					t.Fatalf("path %d not canonical", i)
				}
				if rows[i].SpendingOutcome.MonthsObserved != rows[i].ProjectionYears*12 {
					t.Fatal("shortened horizon")
				}
			}
		})
	}
}
func TestSpendingRunnerCancellationAndWorkerBound(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.ProjectionYears = 10
	in := engineInput(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active, peak, calls atomic.Int32
	in.Hooks.SocialSecurityProjectionActive = func(*models.WhatIfSettings) bool { return true }
	in.Hooks.ProjectedSocialSecurityIncome = func(*models.WhatIfSettings, int) float64 {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		if calls.Add(1) == 1 {
			cancel()
		}
		return 1200
	}
	rows, err := runSpendingScenarios(ctx, in, 42, 1000, spendingSearchRequest())
	if !errors.Is(err, context.Canceled) || rows != nil {
		t.Fatalf("partial cancellation %d %v", len(rows), err)
	}
	limit := int32(min(8, runtime.GOMAXPROCS(0)))
	if peak.Load() > limit {
		t.Fatalf("workers %d > %d", peak.Load(), limit)
	}
	if calls.Load() > limit*15*12*4 {
		t.Fatalf("queued work continued: %d hook calls", calls.Load())
	}
}

// Fixed representative fixture: 35-year plan, three tax account buckets,
// account-specific allocations, SS hooks, pension, phases, taxes, and timed boost.
func spendingBenchmarkInput(tb testing.TB) engine.Input {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-09"
	s.Persons[0].ID = "benchmark-primary"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	s.ProjectionYears = 35
	s.PortfolioValue = 2400000
	s.MonthlyLivingExpenses = 8000
	s.MonthlyHealthcare = 900
	s.TaxDeferredPercent = 60
	s.RothPercent = 15
	s.TaxDeferredStockPercent = 60
	s.TaxDeferredCashPercent = 5
	s.RothStockPercent = 80
	s.TaxableStockPercent = 50
	s.TaxableCashPercent = 10
	s.IncomeSources = []models.IncomeSource{{Name: "Pension", Amount: 1500, COLARate: .02}}
	s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 1000, StopMonth: "2031-09"}
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "early", StartAge: 0, Multiplier: 1}, {Name: "later", StartAge: 75, Multiplier: .85}}}
	in := engineInput(tb, s)
	in.Hooks.SocialSecurityProjectionActive = func(*models.WhatIfSettings) bool { return true }
	in.Hooks.ProjectedSocialSecurityIncome = func(*models.WhatIfSettings, int) float64 { return 3000 }
	return in
}
func BenchmarkSpendingOptimizer(b *testing.B) {
	in := spendingBenchmarkInput(b)
	req := models.SpendingOptimizerRequest{FloorMonthlyReal: 7000, NearTermYears: 5, Seed: 20260910, LivingSpendingBoost: models.CloneLivingSpendingBoost(in.Prepared.Settings().LivingSpendingBoost)}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		paths, calls := 0, 0
		runner := func(ctx context.Context, c engine.Input, seed int64, n int, r models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error) {
			paths += n
			calls++
			return runSpendingScenarios(ctx, c, seed, n, r)
		}
		result, err := optimizeSpendingWithRunner(context.Background(), in, req, runner)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(paths), "paths/op")
		b.ReportMetric(float64(calls), "evaluations/op")
		b.ReportMetric(float64(len(result.Candidates)), "final-candidates/op")
		b.Logf("master=%d selection=%d validation=%d workers=%d search=%d selection/final=%d/%d grid=$%.2f..$%.2f step=$%.2f horizon=%d..%d", result.SearchSeed, result.SelectionSeed, result.ValidationSeed, min(8, runtime.GOMAXPROCS(0)), result.SearchRuns, result.SelectionRuns, result.ValidationRuns, result.EffectiveMinMonthlyReal, result.EffectiveMaxMonthlyReal, result.ResolutionMonthlyReal, result.HorizonMinYears, result.HorizonMaxYears)
	}
}
func BenchmarkSpendingScenarioWorkers(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			old := runtime.GOMAXPROCS(workers)
			defer runtime.GOMAXPROCS(old)
			in := spendingBenchmarkInput(b)
			req := spendingSearchRequest()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rows, err := runSpendingScenarios(context.Background(), in, 20260910, 256, req)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(rows)), "paths/op")
			}
		})
	}
}

func TestSpendingRunnerObservesActualScenarioEvents(t *testing.T) {
	for _, name := range []string{"early crash", "health shock", "inflation", "phase change", "late depletion"} {
		t.Run(name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.StartDate = "2026-09"
			s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
			s.ProjectionYears = 15
			s.MonthlyLivingExpenses = 8000
			s.PortfolioValue = 1500000
			s.InflationRate = 8
			if name == "phase change" {
				s.PortfolioValue = 1e9
				s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "early", StartAge: 0, Multiplier: 1}, {Name: "late", StartAge: 70, Multiplier: .6}}}
			}
			in := engineInput(t, s)
			req := spendingSearchRequest()
			rows, err := runSpendingScenarios(context.Background(), in, 42, 16, req)
			if err != nil {
				t.Fatal(err)
			}
			observed := false
			for _, r := range rows {
				if r.SpendingOutcome.MonthsObserved != 12*r.ProjectionYears || len(r.FloorOutcome.Years) != r.ProjectionYears {
					t.Fatal("event truncated capture")
				}
				switch name {
				case "early crash":
					observed = observed || (r.FirstCrashYear > 0 && r.FirstCrashYear <= 5)
				case "health shock":
					observed = observed || r.HealthShocks > 0
				case "inflation":
					y := r.FloorOutcome.Years[4]
					observed = observed || y.LivingNominal > 1.2*y.LivingReal
				case "phase change":
					ys := r.FloorOutcome.Years
					observed = observed || ys[6].LivingReal < .7*ys[3].LivingReal
					if r.SpendingOutcome.MonthsBelowPlan != 0 {
						t.Fatal("scheduled phase reduction counted as a cut")
					}
				case "late depletion":
					d := r.SpendingOutcome.DepletionMonth
					observed = observed || (d > 60 && d < r.ProjectionYears*12)
				}
			}
			if !observed {
				t.Fatal("fixture did not exercise named event")
			}
		})
	}
}

func TestSpendingRunnerWorkerBoundAtOneAndEight(t *testing.T) {
	for _, procs := range []int{1, 8, 16} {
		t.Run(fmt.Sprintf("gomaxprocs-%d", procs), func(t *testing.T) {
			old := runtime.GOMAXPROCS(procs)
			defer runtime.GOMAXPROCS(old)
			s := models.DefaultWhatIfSettings()
			s.ProjectionYears = 10
			in := engineInput(t, s)
			var active, peak atomic.Int32
			in.Hooks.SocialSecurityProjectionActive = func(*models.WhatIfSettings) bool { return true }
			in.Hooks.ProjectedSocialSecurityIncome = func(*models.WhatIfSettings, int) float64 {
				n := active.Add(1)
				for old := peak.Load(); n > old; old = peak.Load() {
					if peak.CompareAndSwap(old, n) {
						break
					}
				}
				runtime.Gosched()
				active.Add(-1)
				return 1200
			}
			if _, err := runSpendingScenarios(context.Background(), in, 42, 16, spendingSearchRequest()); err != nil {
				t.Fatal(err)
			}
			if peak.Load() > int32(min(8, procs)) || peak.Load() == 0 {
				t.Fatalf("observed hook concurrency %d", peak.Load())
			}
		})
	}
}
