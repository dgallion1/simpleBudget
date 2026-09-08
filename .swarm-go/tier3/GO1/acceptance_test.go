package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func go1Set(t *testing.T, obj any, name string, value float64) {
	t.Helper()
	f := reflect.ValueOf(obj).Elem().FieldByName(name)
	if !f.IsValid() {
		t.Fatalf("GO1 contract missing %T.%s", obj, name)
	}
	f.SetFloat(value)
}
func go1Field(t *testing.T, obj any, name string) reflect.Value {
	t.Helper()
	v := reflect.ValueOf(obj)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			t.Fatalf("nil GO1 observation %T", obj)
		}
		v = v.Elem()
	}
	f := v.FieldByName(name)
	if !f.IsValid() {
		t.Fatalf("GO1 contract missing %T.%s", obj, name)
	}
	return f
}
func go1Num(t *testing.T, obj any, name string) float64 { return go1Field(t, obj, name).Float() }
func go1Near(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.00001 {
		t.Fatalf("%s got %.9f want %.9f", label, got, want)
	}
}
func go1Settings() *models.WhatIfSettings {
	return &models.WhatIfSettings{
		StartDate: "2026-01", Persons: []models.Person{{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge("2026-01", 65)}},
		PortfolioValue: 2000000, MonthlyLivingExpenses: 10000, ProjectionYears: 3, RothPercent: 100,
		Guardrails: &models.GuardrailConfig{Enabled: true, FloorDropPct: 1, FloorCutPct: 50, CeilingRisePct: 999, CeilingRaisePct: 10, MinSpendingPct: 10, MaxSpendingPct: 120},
	}
}
func go1Floor(t *testing.T, s *models.WhatIfSettings, v float64) {
	go1Set(t, s.Guardrails, "MinMonthlySpendingReal", v)
}
func go1Observation(t *testing.T, r models.MonteCarloResult) any {
	f := go1Field(t, r, "FloorOutcome")
	if f.IsNil() {
		t.Fatal("floor observer absent")
	}
	return f.Interface()
}
func go1MC(t *testing.T, s *models.WhatIfSettings, floor float64) models.MonteCarloResult {
	c := &MonteCarloConfig{}
	go1Set(t, c, "MinMonthlySpendingReal", floor)
	return RunSingleMonteCarloSimulation(engineInput(t, s), rand.New(rand.NewSource(812)), c)
}

func TestGO1SharedMonthlyFloor(t *testing.T) {
	for _, rate := range []float64{0, 0.12} {
		for _, phase := range []bool{false, true} {
			s := go1Settings()
			go1Floor(t, s, 7500)
			s.SpendingDeclineRate = 30
			if phase {
				s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "initial", StartAge: 0, Multiplier: 1}, {Name: "later", StartAge: 66, Multiplier: 0.3}}}
			}
			st := engine.NewProjectionState(engineInput(t, s))
			for m := 0; m < 36; m++ {
				out := st.StepMonth(m, func(_ *models.WhatIfSettings, m int) engine.MonthReturns {
					return engine.MonthReturns{InflationAnnual: rate + float64(m%3)*rate/10, NetInflationAnnual: -0.3, HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
				})
				adjusted := go1Num(t, out, "AdjustedLivingExpenses")
				funded := go1Num(t, out, "FundedLivingExpenses")
				if adjusted/st.CumulativeInflation < 7500-1e-7 {
					t.Fatalf("floor violated month %d phase=%v CPI=%g real=%g", m, phase, st.CumulativeInflation, adjusted/st.CumulativeInflation)
				}
				go1Near(t, "effective multiplier", out.LivingExpenses*out.GuardrailMultiplier, adjusted)
				go1Near(t, "funded accounting", funded, math.Max(0, adjusted-math.Max(0, out.Result.Shortfall)))
				if e := out.GuardrailEvent; e != nil {
					if e.MonthlySpendingBefore/e.CumulativeInflation < 7500-1e-7 || e.MonthlySpendingAfter/e.CumulativeInflation < 7500-1e-7 {
						t.Fatalf("event contradicts floor: %+v", e)
					}
				}
			}
		}
	}
}
func TestGO1CanonicalAndBacktestConsumers(t *testing.T) {
	s := go1Settings()
	go1Floor(t, s, 7500)
	s.SpendingDeclineRate = 70
	projection, _ := runProj(t, s)
	if len(projection.Months) < 24 {
		t.Fatal("canonical fixture too short")
	}
	for _, m := range projection.Months {
		a := go1Num(t, m, "AdjustedLivingExpenses")
		f := go1Num(t, m, "FundedLivingExpenses")
		if a/m.CumulativeInflation < 7500-1e-7 {
			t.Fatal("canonical floor lost")
		}
		go1Near(t, "canonical funded", f, math.Max(0, a-math.Max(0, m.FundingShortfall)))
	}
	with := runSingleHistoricalSequence(engineInput(t, s), history.DefaultData(), 1982)
	go1Floor(t, s, 0)
	without := runSingleHistoricalSequence(engineInput(t, s), history.DefaultData(), 1982)
	if with.FinalBalance >= without.FinalBalance {
		t.Fatalf("backtest ignored floor: enforced %g legacy %g", with.FinalBalance, without.FinalBalance)
	}
}
func TestGO1DisabledAndLegacy(t *testing.T) {
	s := go1Settings()
	s.Guardrails.Enabled = false
	go1Floor(t, s, 7500)
	s.MonthlyLivingExpenses = 6000
	st := engine.NewProjectionState(engineInput(t, s))
	o := st.StepMonth(0, func(*models.WhatIfSettings, int) engine.MonthReturns {
		return engine.MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
	})
	go1Near(t, "disabled floor", go1Num(t, o, "AdjustedLivingExpenses"), 6000)
	r := go1MC(t, s, 7500)
	if !go1Field(t, go1Observation(t, r), "FloorFailed").Bool() {
		t.Fatal("measurement floor silently enforced disabled baseline")
	}
	legacy := RunSingleMonteCarloSimulation(engineInput(t, s), rand.New(rand.NewSource(812)), &MonteCarloConfig{})
	if !go1Field(t, legacy, "FloorOutcome").IsNil() {
		t.Fatal("legacy unexpectedly observes floor")
	}
}
func TestGO1FundedFloorNotDepletion(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		income, living, floor float64
		start                 int
		failed                bool
	}{
		{"zero assets sufficient income", 750, 750, 750, 0, false},
		{"discretionary gap above floor", 800, 1000, 750, 0, false},
		{"gap below floor", 700, 1000, 750, 0, true},
		{"round up to floor", 749.996, 750, 750, 0, false},
		{"round below floor", 749.994, 750, 750, 0, true},
		{"temporary illiquidity", 750, 750, 750, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := go1Settings()
			s.PortfolioValue = 0
			s.Guardrails = nil
			s.MonthlyLivingExpenses = tc.living
			s.IncomeSources = []models.IncomeSource{{ID: "p", Name: "income", Amount: tc.income, Type: models.IncomeFixed, StartMonth: tc.start}}
			r := go1MC(t, s, tc.floor)
			o := go1Observation(t, r)
			if go1Field(t, o, "FloorFailed").Bool() != tc.failed {
				t.Fatalf("floor failure %+v", o)
			}
			if go1Field(t, o, "MonthsObserved").Int() != 36 {
				t.Fatalf("observer stopped on legacy depletion: %+v", o)
			}
			if r.Survives || r.DepletionYear != 0 {
				t.Fatalf("legacy FIRST depletion changed: %+v", r)
			}
			want := math.Round(math.Min(tc.income, tc.living)*100) / 100 * float64(36-tc.start)
			go1Near(t, "funded lifetime", go1Num(t, o, "TotalFundedLivingReal"), want)
			go1Near(t, "zero final real balance", go1Num(t, o, "FinalBalanceReal"), 0)
		})
	}
}

func TestGO1AnnualCuts(t *testing.T) {
	s := go1Settings()
	s.Guardrails = nil
	s.MonthlyLivingExpenses = 1000
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "first", StartAge: 0, Multiplier: 1}, {Name: "second", StartAge: 66, Multiplier: 0.5}, {Name: "third", StartAge: 67, Multiplier: 0.25}}}
	r := go1MC(t, s, 100)
	o := go1Observation(t, r)
	go1Near(t, "annual funded totals", go1Num(t, o, "TotalFundedLivingReal"), 21000)
	go1Near(t, "worst cut", go1Num(t, o, "WorstAnnualCutPct"), 50)
	if go1Field(t, o, "AnnualCutCount").Int() != 2 {
		t.Fatal(o)
	}
	go1Near(t, "zero CPI final balance", go1Num(t, o, "FinalBalanceReal"), r.FinalBalance)
}

func TestGO1LockedAssets(t *testing.T) {
	s := go1Settings()
	s.Guardrails = nil
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 58)
	s.TaxDeferredPercent = 100
	s.TaxDeferredDelayYears = 1
	s.RothPercent = 0
	s.MonthlyLivingExpenses = 750
	r := go1MC(t, s, 750)
	o := go1Observation(t, r)
	if !go1Field(t, o, "FloorFailed").Bool() {
		t.Fatal("locked assets counted as funding")
	}
	if go1Field(t, o, "MonthsObserved").Int() != 36 {
		t.Fatal("observer stopped")
	}
	if go1Num(t, o, "TotalFundedLivingReal") <= 0 {
		t.Fatal("missed later access")
	}
}
