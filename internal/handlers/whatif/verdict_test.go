package whatif

import (
	"budget2/internal/models"
	"strings"
	"testing"
)

func intPtr(i int) *int { return &i }

func TestBuildVerdict(t *testing.T) {
	for _, tc := range []struct {
		name, start, headline, detail string
		survives, guards              bool
		depletion                     *int
		months                        []models.ProjectionMonth
		health                        models.Health
	}{
		{name: "funded full horizon is conditional and neutral", start: "2026-01", survives: true, headline: "Funded through Dec 2063", detail: "Base projection funds your planned lifestyle", health: models.HealthNeutral},
		{name: "April endpoint", start: "2026-04", survives: true, headline: "Funded through Mar 2064", detail: "Base projection funds your planned lifestyle", health: models.HealthNeutral},
		{name: "funded with below-plan cuts", start: "2026-01", survives: true, guards: true, months: []models.ProjectionMonth{{PlannedLivingExpenses: 1000, GuardrailMultiplier: 0.9}}, headline: "Funded through Dec 2063", detail: "Base projection funds spending with circuit-breaker cuts", health: models.HealthAmber},
		{name: "early depletion", start: "2026-01", depletion: intPtr(72), headline: "Funds run out in Jan 2032", detail: "Base projection has a funding shortfall", health: models.HealthRed},
		{name: "late depletion despite guardrails", start: "2026-04", guards: true, depletion: intPtr(300), headline: "Funds run out in Apr 2051", detail: "Base projection has a funding shortfall despite configured guardrails", health: models.HealthRed},
		{name: "depletion crossing New Year", start: "2026-04", depletion: intPtr(9), headline: "Funds run out in Jan 2027", detail: "Base projection has a funding shortfall", health: models.HealthRed},
		{name: "unpaid gap overrides survival", start: "2026-01", survives: true, months: []models.ProjectionMonth{{FundingShortfall: 1}}, headline: "Base projection has a funding shortfall", detail: "unpaid", health: models.HealthRed},
		{name: "missing depletion date", start: "2026-01", headline: "Base projection has a funding shortfall", detail: "Base projection has a funding shortfall", health: models.HealthRed},
		{name: "lowering prior raise stays above plan", start: "2026-01", survives: true, months: []models.ProjectionMonth{{PlannedLivingExpenses: 1000, GuardrailMultiplier: 1.1}}, headline: "Funded through Dec 2063", detail: "Base projection funds your planned lifestyle", health: models.HealthNeutral},
		{name: "legacy zero multiplier without baseline", start: "2026-01", survives: true, months: []models.ProjectionMonth{{GuardrailMultiplier: 0}}, headline: "Funded through Dec 2063", detail: "Base projection funds your planned lifestyle", health: models.HealthNeutral},
		{name: "zero multiplier with positive baseline is a cut", start: "2026-01", survives: true, months: []models.ProjectionMonth{{PlannedLivingExpenses: 1000, GuardrailMultiplier: 0}}, headline: "Funded through Dec 2063", detail: "circuit-breaker cuts", health: models.HealthAmber},
		{name: "cut tolerance", start: "2026-01", survives: true, months: []models.ProjectionMonth{{PlannedLivingExpenses: 1000, GuardrailMultiplier: 1 - 1e-10, FundingShortfall: 1e-8}}, headline: "Funded through Dec 2063", detail: "Base projection funds your planned lifestyle", health: models.HealthNeutral},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &models.WhatIfSettings{ProjectionYears: 38, StartDate: tc.start, Guardrails: &models.GuardrailConfig{Enabled: tc.guards}}
			a := &models.WhatIfAnalysis{Projection: &models.ProjectionResult{Survives: tc.survives, DepletionMonth: tc.depletion, Months: tc.months}}
			for _, rate := range []float64{0, 55, 70, 95, 100} {
				a.MonteCarlo = &models.MonteCarloAnalysis{Stats: &models.MonteCarloStats{Runs: 100, SuccessRate: rate}}
				v := BuildVerdict(a, s)
				if v.Headline != tc.headline || !strings.Contains(v.Detail, tc.detail) || v.Health != tc.health {
					t.Errorf("rate %v: verdict = %+v; want %q, %q, %s", rate, v, tc.headline, tc.detail, tc.health)
				}
			}
		})
	}
	t.Run("missing inputs and projection", func(t *testing.T) {
		for _, v := range []VerdictView{BuildVerdict(nil, nil), BuildVerdict(&models.WhatIfAnalysis{}, &models.WhatIfSettings{StartDate: "2026-01", ProjectionYears: 38})} {
			if v.Headline != "Projection unavailable" || v.Health != models.HealthNeutral {
				t.Errorf("missing projection = %+v", v)
			}
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
		if !v.HasBudgetFit || v.CurrentMonthlyGap != -200 || v.CurrentRequiredRate != 0 {
			t.Errorf("current funding needs overwritten by selected year: %+v", v)
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

func TestProjectionHasLivingCutsNil(t *testing.T) {
	if projectionHasLivingCuts(nil) {
		t.Error("nil projection cannot establish living cuts")
	}
}
