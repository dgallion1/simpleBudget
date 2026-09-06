package whatif

import (
	"context"
	"math"
	"regexp"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	retirementanalysis "budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

func lifestyleFixture() *models.LifestyleOutcomeStats {
	return &models.LifestyleOutcomeStats{Runs: 1000, FundedWithoutCuts: 600, FundedWithCuts: 300, Shortfall: 100, RunsWithCuts: 350, MedianCutMonths: 24, P90CutMonths: 60, P90LongestCutMonths: 36, MedianMaxLivingCutPct: 10, P90MaxLivingCutPct: 25, P90MaxMonthlyLivingCutReal: 1200, CutRunsEndingBelowPlan: 50}
}

func TestLifestyleOutcomesRender(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	for _, tc := range []struct {
		name    string
		stats   *models.MonteCarloStats
		pending bool
		want    []string
		absent  []string
	}{
		{name: "complete", stats: &models.MonteCarloStats{Runs: 1000, SuccessRate: 90, Lifestyle: lifestyleFixture()}, want: []string{"Funded without living-budget cuts", "Funded with living-budget cuts", "Funding shortfall", "60.0%", "30.0%", "10.0%", "600 / 1000", "300 / 1000", "100 / 1000", "Among runs with cuts", "350 / 1000", "2.0 years", "5.0 years", "3.0 years", "25.0%", "$1,200", "today's dollars", "50 / 350", "incomplete observed durations"}, absent: []string{"text-green", "text-lime", "text-red", "only because"}},
		{name: "pending", pending: true, want: []string{"Risk results pending"}, absent: []string{"0.0%", "Funded without"}},
		{name: "missing", want: []string{"Risk results unavailable"}, absent: []string{"0.0%", "Funded without"}},
		{name: "legacy stats lack lifestyle", stats: &models.MonteCarloStats{Runs: 100, SuccessRate: 100}, want: []string{"Lifestyle outcomes unavailable"}, absent: []string{"0.0%", "Funded without"}},
		{name: "zero runs", stats: &models.MonteCarloStats{Lifestyle: &models.LifestyleOutcomeStats{}}, want: []string{"Risk results unavailable"}, absent: []string{"0.0%", "Funded without"}},
		{name: "no cuts", stats: &models.MonteCarloStats{Runs: 100, Lifestyle: &models.LifestyleOutcomeStats{Runs: 100, FundedWithoutCuts: 100}}, want: []string{"100.0%", "No below-plan living-budget cuts were observed"}, absent: []string{"Among runs with cuts", "0.0 years"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &models.WhatIfAnalysis{}
			if tc.stats != nil {
				a.MonteCarlo = &models.MonteCarloAnalysis{Stats: tc.stats}
			}
			out, err := renderer.RenderToString("whatif-lifestyle-outcomes", map[string]any{"Analysis": a, "AnalysisPending": tc.pending})
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in %s", want, out)
				}
			}
			for _, bad := range tc.absent {
				if strings.Contains(out, bad) {
					t.Errorf("unexpected %q", bad)
				}
			}
		})
	}
}

func TestLifestyleFundingSurvivesResultRefresh(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s := models.DefaultWhatIfSettings()
	a, err := runAnalysisWithCache(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	a.BudgetFit.MonthlyGap = 1601.38
	a.BudgetFit.RequiredRate = 3.1
	a.BudgetFit.HasSteadyState = true
	a.BudgetFit.SteadyStateGap = -2500
	a.BudgetFit.SteadyStateYear = 12
	a.BudgetFit.SteadyStateRate = 0
	a.Projection.YearlySummaries = []models.ProjectionYearSummary{{Year: 0, Withdrawals: 43210}}
	a.MonteCarlo.Stats.Lifestyle = lifestyleFixture()
	for _, tc := range []struct {
		name, template string
		pending        bool
	}{
		{"full results", "whatif-results", false}, {"settings refresh", "whatif-results-with-oob", false}, {"fast partial", "whatif-results", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := buildResultsPartialData(s, a, nil)
			data["AnalysisPending"] = tc.pending
			out, err := renderer.RenderToString(tc.template, data)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Needed from portfolio after estimated taxes and RMDs", "$1,601", "Selected year 12", "$2,500", "Additional withdrawal rate after RMDs", "First projection year net portfolio outflow", "$43,210", `id="whatif-lifestyle-outcomes"`} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q", want)
				}
			}
			verdict := strings.Index(out, `id="whatif-verdict-bar"`)
			outcomes := strings.Index(out, `id="whatif-lifestyle-outcomes"`)
			tabs := strings.Index(out, `id="whatif-tabs"`)
			if !(verdict < outcomes && outcomes < tabs) {
				t.Error("outcome block must follow verdict before tabs")
			}
			if tc.pending {
				if !strings.Contains(out, "Risk results pending") {
					t.Error("missing pending state")
				}
			} else if !strings.Contains(out, "600 / 1000") {
				t.Error("missing completed outcomes")
			}
		})
	}
	// Real fast assembly lacks simulations: it must render an explicit pending state.
	in := engine.Input{Prepared: prepare.MustFrom(t, s)}
	fast := retirement.RunFast(engine.New(), in)
	data := buildResultsPartialData(s, fast, nil)
	data["AnalysisPending"] = true
	out, err := renderer.RenderToString("whatif-results", data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Risk results pending") {
		t.Error("RunFast omitted pending outcome state")
	}
}

func TestLifestyleFirstYearOutflowRMD(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.ProjectionYears = 2
	s.Persons = []models.Person{{ID: "primary", Role: models.PersonRolePrimary, Name: "You", BirthMonth: models.BirthMonthForAge(s.StartDate, 78)}}
	s.PortfolioValue = 1000000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	in := engine.Input{Prepared: prepare.MustFrom(t, s)}
	p := engine.New().Run(in)
	rmd := 0.0
	for _, m := range p.Months[:12] {
		rmd += m.RMDWithdrawal
	}
	if rmd <= 0 {
		t.Fatal("fixture must include first-year RMD")
	}
	copyProjection := *p
	copyProjection.YearlySummaries = nil
	explain := retirementanalysis.BuildExplainability(&copyProjection, in)
	if explain == nil || len(explain.YearlySummaries) == 0 {
		t.Fatal("missing explainability")
	}
	want := p.YearlySummaries[0].Withdrawals
	if math.Abs(want-explain.YearlySummaries[0].Withdrawals) > 1e-7 {
		t.Fatalf("engine %v != explainability %v", want, explain.YearlySummaries[0].Withdrawals)
	}
	v := BuildVerdict(&models.WhatIfAnalysis{Projection: p}, s)
	if !v.HasFirstYearOutflow || v.FirstYearOutflow != want {
		t.Errorf("verdict outflow %v want %v", v.FirstYearOutflow, want)
	}
}

func TestLifestyleSuccessRateSurfacesNeutral(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s := models.DefaultWhatIfSettings()
	a, err := runAnalysisWithCache(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	a.TaxOptimizer = &models.TaxOptimizerAnalysis{Eligible: true, MonteCarloRuns: 100, Top: []models.TaxOptimizerCandidate{{MCSurvivalRate: 95}}}
	a.MonteCarlo.Stats.SuccessRate = 95
	a.HistoricalBacktest.SuccessRate = 95
	a.HistoricalBacktest.MonteCarloSuccessRate = 95
	bad := regexp.MustCompile(`(?s)<(?:span|div|td)[^>]*class="[^"]*(?:text-green|text-lime|text-red|text-positive|text-negative|text-warning)[^"]*"[^>]*>\s*95(?:\.0)?%`)
	for _, name := range []string{"whatif-verdict-bar", "whatif-monte-carlo", "whatif-historical-backtest", "whatif-tax-optimizer-results"} {
		out, err := renderer.RenderToString(name, buildResultsPartialData(s, a, nil))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "95") {
			t.Errorf("%s missing fixture percentage", name)
		}
		if bad.MatchString(out) {
			t.Errorf("%s colors aggregate success: %s", name, bad.FindString(out))
		}
	}
}

func TestLifestyleSupplementaryRiskNeutral(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	a := &models.WhatIfAnalysis{MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{Runs: 100, SequenceRiskImpact: 10, SequenceRisk: &models.SequenceRiskBreakdown{NoCrashSurvival: 95, EarlyCrashSurvival: 95, MidCrashSurvival: 95, LateCrashSurvival: 95, EarlyCrashCount: 10, NoCrashCount: 10, MidCrashCount: 10, LateCrashCount: 10, RecoveryRate: 95, HasDiscretionary: true, AdaptationBoost: 5, EarlyCrashSurvivalAdapted: 95}}}}
	out, err := renderer.RenderToString("whatif-sequence-risk", map[string]any{"Analysis": a})
	if err != nil {
		t.Fatal(err)
	}
	bad := regexp.MustCompile(`(?s)<(?:span|div)[^>]*class="[^"]*(?:text-positive|text-negative|text-warning)[^"]*"[^>]*>\s*95%`)
	if bad.MatchString(out) {
		t.Errorf("colored supplementary survival: %s", bad.FindString(out))
	}
	for _, want := range []string{"Separate discretionary-expense experiment", "does not define protected essentials", "avoiding modeled depletion"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestLifestyleMissingRiskCard(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	for _, a := range []*models.WhatIfAnalysis{{}, {MonteCarlo: &models.MonteCarloAnalysis{}}, {MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{}}}} {
		out, err := renderer.RenderToString("whatif-monte-carlo", map[string]any{"Analysis": a})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Risk results unavailable") || strings.Contains(out, "0.0%") {
			t.Errorf("missing risk rendered as measured zero: %s", out)
		}
	}
}

func TestLifestyleRiskWithoutDistribution(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	a := &models.WhatIfAnalysis{MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{Runs: 100, SuccessRate: 90}}}
	out, err := renderer.RenderToString("whatif-monte-carlo", map[string]any{"Analysis": a})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "90.0%") {
		t.Error("missing measured depletion-avoidance rate")
	}
}

func TestLifestyleBasicCutBurdenVisible(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	a := &models.WhatIfAnalysis{MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{Runs: 1000, Lifestyle: lifestyleFixture()}}}
	out, err := renderer.RenderToString("whatif-lifestyle-outcomes", map[string]any{"Analysis": a})
	if err != nil {
		t.Fatal(err)
	}
	details := strings.Index(out, "<details")
	for _, want := range []string{"Among runs with cuts", "Median largest living-budget cut: 10.0%", "median total time below plan: 2.0 years"} {
		at := strings.Index(out, want)
		if at < 0 || details < 0 || at > details {
			t.Errorf("%q must be visible before disclosure", want)
		}
	}
	if strings.Contains(out, "legacy") {
		t.Error("implementation terminology in product copy")
	}
}
