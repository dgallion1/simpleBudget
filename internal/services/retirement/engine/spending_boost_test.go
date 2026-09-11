package engine

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
	"encoding/json"
	"math"
	"testing"
)

func boostFixture(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	s := &models.WhatIfSettings{StartDate: "2026-09", Persons: []models.Person{{ID: "p", Name: "You", Role: models.PersonRolePrimary, BirthMonth: "1961-09"}}, PortfolioValue: 10000000, RothPercent: 100, MonthlyLivingExpenses: 10000, ProjectionYears: 11,
		SpendingPhaseConfig: &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "constant", StartAge: 0, Multiplier: 1.1}}}}
	if err := json.Unmarshal([]byte(`{"living_spending_boost":{"monthly_real":1000,"stop_month":"2036-09"}}`), s); err != nil {
		t.Fatal(err)
	}
	return s
}
func boostReturns(*models.WhatIfSettings, int) MonthReturns {
	return MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
}
func boostNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("%s got %.9f want %.9f", name, got, want)
	}
}

// Catches omission, accumulation into base state, phase multiplication, and inclusive expiry.
func TestLivingSpendingBoostSchedule(t *testing.T) {
	s := boostFixture(t)
	in := Input{Prepared: prepare.MustFrom(t, s)}
	st := NewProjectionState(in)
	boostNear(t, "initial base", st.CurrentLivingExpenses, 11000)
	for m := 0; m <= 120; m++ {
		out := st.StepMonth(m, boostReturns)
		want := 12000.0
		if m == 120 {
			want = 11000
		}
		boostNear(t, "planned", out.LivingExpenses, want)
		boostNear(t, "funded", out.FundedLivingExpenses, want)
		boostNear(t, "deterministic helper", LivingExpensesAtMonth(s, m), want)
		boostNear(t, "base remains separate", st.CurrentLivingExpenses, 11000)
	}
	replay := New().Run(in)
	boostNear(t, "canonical month zero", replay.Months[0].FundedLivingExpenses, 12000)
	boostNear(t, "canonical expiry", replay.Months[120].FundedLivingExpenses, 11000)
	s.StartDate = "2035-09"
	boostNear(t, "moved start before fixed expiry", LivingExpensesAtMonth(s, 11), 12000)
	boostNear(t, "moved start fixed expiry", LivingExpensesAtMonth(s, 12), 11000)
}

// Supplied path CPI applies to the boost independently of the base decline rate.
func TestLivingSpendingBoostPathCPI(t *testing.T) {
	for _, phase := range []bool{false, true} {
		s := boostFixture(t)
		s.SpendingPhaseConfig.Enabled = phase
		st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
		for m := 0; m <= 120; m++ {
			out := st.StepMonth(m, func(_ *models.WhatIfSettings, month int) MonthReturns {
				p := boostReturns(nil, month)
				p.InflationAnnual = math.Pow(2, 12.0/119) - 1
				p.NetInflationAnnual = 0
				return p
			})
			if m == 119 {
				want := 12000.0
				if phase {
					want = 24000
				}
				boostNear(t, "CPI=2", out.LivingExpenses, want)
			}
			if m == 120 {
				boostNear(t, "expiry removes path-inflated boost", out.LivingExpenses, st.CurrentLivingExpenses)
			}
		}
	}
	s := boostFixture(t)
	s.SpendingPhaseConfig = nil
	s.InflationRate = 10
	s.SpendingDeclineRate = 10
	boostNear(t, "deterministic boost full CPI", LivingExpensesAtMonth(s, 12), 11100)
}

// Combined living must be cut by the guardrail before the absolute floor is applied.
func TestLivingSpendingBoostGuardrail(t *testing.T) {
	for _, floor := range []float64{0, 11000} {
		s := boostFixture(t)
		s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 1, FloorCutPct: 10, CeilingRisePct: 999, MinSpendingPct: 1, MaxSpendingPct: 200, MinMonthlySpendingReal: floor}
		st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
		st.RothBalance *= 0.5
		out := st.StepMonth(0, boostReturns)
		want := 10800.0
		if floor > 0 {
			want = 11000
		}
		boostNear(t, "adjusted combined living", out.AdjustedLivingExpenses, want)
		if out.GuardrailEvent == nil {
			t.Fatal("expected cut event")
		}
		boostNear(t, "event before", out.GuardrailEvent.MonthlySpendingBefore, 12000)
		boostNear(t, "event after", out.GuardrailEvent.MonthlySpendingAfter, want)
	}
}

// Active chain settings cannot restart the projection calendar or double-add the boost.
func TestLivingSpendingBoostChain(t *testing.T) {
	for _, phase := range []bool{false, true} {
		s := boostFixture(t)
		s.SpendingPhaseConfig.Enabled = phase
		next := boostFixture(t)
		next.SpendingPhaseConfig.Enabled = phase
		next.StartDate = "2036-09"
		in := Input{Prepared: prepare.MustFrom(t, s), Chain: []PreparedChainLink{{Settings: prepare.MustFrom(t, next)}}}
		in.Hooks.ResolveChainTransition = func(year, index int, _ *models.WhatIfSettings, chain []PreparedChainLink) (int, *models.WhatIfSettings) {
			if year == 1 {
				return 1, chain[0].Settings.Settings()
			}
			return index, nil
		}
		st := NewProjectionState(in)
		for m := 0; m <= 120; m++ {
			out := st.StepMonth(m, boostReturns)
			want := 11000.0
			if phase {
				want = 12000
			}
			if m == 120 {
				want -= 1000
			}
			boostNear(t, "chain original calendar", out.LivingExpenses, want)
		}
	}
}

func TestLivingSpendingBoostDisabledAndExpired(t *testing.T) {
	for _, boost := range []*models.LivingSpendingBoost{nil, {MonthlyReal: 1000, StopMonth: "2026-09"}, {MonthlyReal: 1000, StopMonth: "2025-01"}} {
		s := boostFixture(t)
		s.LivingSpendingBoost = boost
		st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
		for m := 0; m < 24; m++ {
			out := st.StepMonth(m, boostReturns)
			boostNear(t, "inactive schedule", out.LivingExpenses, 11000)
			boostNear(t, "inactive deterministic schedule", LivingExpensesAtMonth(s, m), 11000)
		}
	}
}

// Base phase reductions and spending decline never reduce the separate real boost.
func TestLivingSpendingBoostBaseReductions(t *testing.T) {
	for _, phase := range []bool{false, true} {
		s := boostFixture(t)
		s.SpendingPhaseConfig = nil
		s.SpendingDeclineRate = 50
		if phase {
			s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "first", StartAge: 0, Multiplier: 1.1}, {Name: "later", StartAge: 66, Multiplier: 0.8}}}
		}
		st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
		var out MonthOutcome
		for m := 0; m <= 12; m++ {
			out = st.StepMonth(m, func(*models.WhatIfSettings, int) MonthReturns {
				p := boostReturns(nil, 0)
				p.NetInflationAnnual = -0.5
				return p
			})
		}
		want := 6000.0
		if phase {
			want = 9000
		}
		boostNear(t, "base reduction leaves boost intact", out.LivingExpenses, want)
		boostNear(t, "deterministic reduction parity", LivingExpensesAtMonth(st.Settings(), 12), want)
	}
}

func TestLivingSpendingBoostAdditionalObligations(t *testing.T) {
	s := boostFixture(t)
	s.MonthlyHealthcare = 200
	s.MonthlyPropertyTax = 100
	s.ExpenseSources = []models.ExpenseSource{{ID: "other", Name: "Other", Amount: 300}}
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	out := st.StepMonth(0, boostReturns)
	boostNear(t, "living alone", out.LivingExpenses, 12000)
	boostNear(t, "obligations additional", out.TotalExpenses, 12600)
	boostNear(t, "planned total", out.PlannedExpenses, 12600)
	boostNear(t, "deterministic expenses", TotalExpenses(s, 0), 12600)
	boostNear(t, "expense breakdown", CalculateExpenseBreakdown(s, 0).Total, 12600)
}
