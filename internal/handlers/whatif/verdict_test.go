package whatif

import (
	"testing"

	"budget2/internal/models"
)

func intPtr(i int) *int { return &i }

func TestBuildVerdict(t *testing.T) {
	t.Run("funded full horizon with strong MC is green", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: true, FinalBalance: 410250},
			BudgetFit:  &models.BudgetFitAnalysis{MonthlyGap: -200, RequiredRate: 0},
			MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 85}},
		}
		v := BuildVerdict(a, s)
		if v.Health != VerdictGreen {
			t.Errorf("Health = %q, want green", v.Health)
		}
		if v.Headline != "Funded through 2064" {
			t.Errorf("Headline = %q, want \"Funded through 2064\"", v.Headline)
		}
		if v.GapIsShortfall {
			t.Errorf("GapIsShortfall = true, want false (surplus)")
		}
		if !v.HasMonteCarlo || v.SuccessRate != 85 {
			t.Errorf("MC = (%v,%v), want (true,85)", v.HasMonteCarlo, v.SuccessRate)
		}
	})

	t.Run("funded but weak MC is amber", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 30, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: true},
			BudgetFit:  &models.BudgetFitAnalysis{MonthlyGap: 100},
			MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 55}},
		}
		if v := BuildVerdict(a, s); v.Health != VerdictAmber {
			t.Errorf("Health = %q, want amber", v.Health)
		}
	})

	t.Run("early depletion is red with depletion-year headline", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: false, DepletionMonth: intPtr(72)}, // 6 years
			BudgetFit:  &models.BudgetFitAnalysis{MonthlyGap: 1601, RequiredRate: 3.1},
			MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 12}},
		}
		v := BuildVerdict(a, s)
		if v.Health != VerdictRed {
			t.Errorf("Health = %q, want red", v.Health)
		}
		if v.Headline != "Funds run out in 2032" {
			t.Errorf("Headline = %q, want \"Funds run out in 2032\"", v.Headline)
		}
		if v.YearsCovered != 6 {
			t.Errorf("YearsCovered = %d, want 6", v.YearsCovered)
		}
		if !v.GapIsShortfall {
			t.Errorf("GapIsShortfall = false, want true")
		}
	})

	t.Run("late depletion is amber", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: false, DepletionMonth: intPtr(300)}, // 25 years
			BudgetFit:  &models.BudgetFitAnalysis{MonthlyGap: 500},
			MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 60}},
		}
		if v := BuildVerdict(a, s); v.Health != VerdictAmber {
			t.Errorf("Health = %q, want amber", v.Health)
		}
	})

	t.Run("nil MonteCarlo degrades gracefully", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 20, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: true},
			BudgetFit:  &models.BudgetFitAnalysis{MonthlyGap: -50},
		}
		v := BuildVerdict(a, s)
		if v.HasMonteCarlo {
			t.Errorf("HasMonteCarlo = true, want false")
		}
		if v.Health != VerdictGreen {
			t.Errorf("Health = %q, want green (survives, no MC)", v.Health)
		}
	})

	t.Run("nil inputs return a defined (non-red) health", func(t *testing.T) {
		if v := BuildVerdict(nil, nil); v.Health != VerdictAmber {
			t.Errorf("Health = %q, want amber for nil inputs (must not silently render red)", v.Health)
		}
	})

	t.Run("steady-state in view reports the selected-year gap and rate", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: true},
			MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 90}},
			BudgetFit: &models.BudgetFitAnalysis{
				MonthlyGap: -200, RequiredRate: 0, // today: surplus
				HasSteadyState:  true,
				SteadyStateYear: 12,
				SteadyStateGap:  3400, // shortfall at year 12
				SteadyStateRate: 4.2,
			},
		}
		v := BuildVerdict(a, s)
		if !v.GapAtSteadyState {
			t.Errorf("GapAtSteadyState = false, want true")
		}
		if v.GapYear != 12 {
			t.Errorf("GapYear = %d, want 12", v.GapYear)
		}
		if v.MonthlyGap != 3400 {
			t.Errorf("MonthlyGap = %v, want 3400 (steady-state gap, not today's -200)", v.MonthlyGap)
		}
		if !v.GapIsShortfall {
			t.Errorf("GapIsShortfall = false, want true (steady-state gap 3400 > 0)")
		}
		if v.RequiredRate != 4.2 {
			t.Errorf("RequiredRate = %v, want 4.2 (steady-state rate)", v.RequiredRate)
		}
	})

	t.Run("no steady-state falls back to today's gap and rate", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: true},
			BudgetFit: &models.BudgetFitAnalysis{
				MonthlyGap: 1500, RequiredRate: 2.5, HasSteadyState: false,
			},
		}
		v := BuildVerdict(a, s)
		if v.GapAtSteadyState {
			t.Errorf("GapAtSteadyState = true, want false")
		}
		if v.MonthlyGap != 1500 || v.RequiredRate != 2.5 {
			t.Errorf("gap/rate = (%v,%v), want (1500,2.5) — today's values", v.MonthlyGap, v.RequiredRate)
		}
	})
}
