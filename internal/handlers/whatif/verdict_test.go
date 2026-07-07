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
		if v.Health != models.HealthGreen {
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
		if want := "spending covered for all 38 years"; v.Detail != want {
			t.Errorf("Detail = %q, want %q (strong MC keeps the plain detail)", v.Detail, want)
		}
	})

	t.Run("funded but weak MC is amber and says so", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 30, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: true},
			BudgetFit:  &models.BudgetFitAnalysis{MonthlyGap: 100},
			MonteCarlo: &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{SuccessRate: 55}},
		}
		v := BuildVerdict(a, s)
		if v.Health != models.HealthAmber {
			t.Errorf("Health = %q, want amber", v.Health)
		}
		if want := "covers the median path — 45% of market simulations fall short"; v.Detail != want {
			t.Errorf("Detail = %q, want %q", v.Detail, want)
		}
		if v.Headline != "Funded through 2056" {
			t.Errorf("Headline = %q, want \"Funded through 2056\"", v.Headline)
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
		if v.Health != models.HealthRed {
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
		if v := BuildVerdict(a, s); v.Health != models.HealthAmber {
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
		if v.Health != models.HealthGreen {
			t.Errorf("Health = %q, want green (survives, no MC)", v.Health)
		}
	})

	t.Run("nil inputs return a defined (non-red) health", func(t *testing.T) {
		if v := BuildVerdict(nil, nil); v.Health != models.HealthAmber {
			t.Errorf("Health = %q, want amber for nil inputs (must not silently render red)", v.Health)
		}
	})

	t.Run("depletion exactly at the early-depletion boundary is amber", func(t *testing.T) {
		// 120 months / 12 = 10 years; 10 < earlyDepletionYears(10) is false → amber.
		// Pins the < vs <= boundary so a future slip can't flip it silently.
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{Projection: &models.ProjectionResult{Survives: false, DepletionMonth: intPtr(120)}}
		if v := BuildVerdict(a, s); v.Health != models.HealthAmber {
			t.Errorf("Health = %q, want amber at exactly 10 years", v.Health)
		}
		// One month earlier (119 → 9 years) crosses into red.
		a.Projection.DepletionMonth = intPtr(119)
		if v := BuildVerdict(a, s); v.Health != models.HealthRed {
			t.Errorf("Health = %q, want red just under 10 years", v.Health)
		}
	})

	t.Run("not-survives with nil depletion month falls back to the full horizon", func(t *testing.T) {
		// Defensive: a non-surviving plan that recorded no depletion month must
		// not panic; it falls back to ProjectionYears (amber, not red).
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{Projection: &models.ProjectionResult{Survives: false, DepletionMonth: nil}}
		v := BuildVerdict(a, s)
		if v.YearsCovered != 38 {
			t.Errorf("YearsCovered = %d, want 38 (fallback to horizon)", v.YearsCovered)
		}
		if v.Health != models.HealthAmber {
			t.Errorf("Health = %q, want amber (38 >= early-depletion cutoff)", v.Health)
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

	t.Run("carries lifetime taxes and end balance for the summary strip", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		a := &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{Survives: true, FinalBalance: 342706.42},
			Tax:        &models.TaxAnalysis{TotalTaxPaid: 1624993.75},
		}
		v := BuildVerdict(a, s)
		if !v.HasTaxes || v.TotalTaxes != 1624993.75 {
			t.Errorf("taxes = (%v, %v), want (true, 1624993.75)", v.HasTaxes, v.TotalTaxes)
		}
		if !v.HasEndBalance || v.EndBalance != 342706.42 {
			t.Errorf("end balance = (%v, %v), want (true, 342706.42)", v.HasEndBalance, v.EndBalance)
		}
	})
	t.Run("nil analysis sections leave strip extras unset", func(t *testing.T) {
		s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: "2026-01"}
		v := BuildVerdict(&models.WhatIfAnalysis{}, s)
		if v.HasTaxes || v.HasEndBalance {
			t.Errorf("expected HasTaxes/HasEndBalance false, got %v/%v", v.HasTaxes, v.HasEndBalance)
		}
	})
}
