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

func floorRegressionSet(t *testing.T, obj any, name string, value float64) {
	t.Helper()
	f := reflect.ValueOf(obj).Elem().FieldByName(name)
	if !f.IsValid() {
		t.Fatalf("GO1 contract missing %T.%s", obj, name)
	}
	f.SetFloat(value)
}
func floorRegressionField(t *testing.T, obj any, name string) reflect.Value {
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
func floorRegressionNum(t *testing.T, obj any, name string) float64 {
	return floorRegressionField(t, obj, name).Float()
}
func floorRegressionNear(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.00001 {
		t.Fatalf("%s got %.9f want %.9f", label, got, want)
	}
}
func floorRegressionSettings() *models.WhatIfSettings {
	return &models.WhatIfSettings{
		StartDate: "2026-01", Persons: []models.Person{{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge("2026-01", 65)}},
		PortfolioValue: 2000000, MonthlyLivingExpenses: 10000, ProjectionYears: 3, RothPercent: 100,
		Guardrails: &models.GuardrailConfig{Enabled: true, FloorDropPct: 1, FloorCutPct: 50, CeilingRisePct: 999, CeilingRaisePct: 10, MinSpendingPct: 10, MaxSpendingPct: 120},
	}
}
func floorRegressionFloor(t *testing.T, s *models.WhatIfSettings, v float64) {
	floorRegressionSet(t, s.Guardrails, "MinMonthlySpendingReal", v)
}
func floorRegressionObservation(t *testing.T, r models.MonteCarloResult) any {
	f := floorRegressionField(t, r, "FloorOutcome")
	if f.IsNil() {
		t.Fatal("floor observer absent")
	}
	return f.Interface()
}
func floorRegressionMC(t *testing.T, s *models.WhatIfSettings, floor float64) models.MonteCarloResult {
	c := &MonteCarloConfig{}
	floorRegressionSet(t, c, "MinMonthlySpendingReal", floor)
	return RunSingleMonteCarloSimulation(engineInput(t, s), rand.New(rand.NewSource(812)), c)
}

func TestFloorRegressionSharedMonthlyFloor(t *testing.T) {
	for _, rate := range []float64{0, 0.12} {
		for _, phase := range []bool{false, true} {
			s := floorRegressionSettings()
			floorRegressionFloor(t, s, 7500)
			s.SpendingDeclineRate = 30
			if phase {
				s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "initial", StartAge: 0, Multiplier: 1}, {Name: "later", StartAge: 66, Multiplier: 0.3}}}
			}
			st := engine.NewProjectionState(engineInput(t, s))
			for m := 0; m < 36; m++ {
				out := st.StepMonth(m, func(_ *models.WhatIfSettings, m int) engine.MonthReturns {
					return engine.MonthReturns{InflationAnnual: rate + float64(m%3)*rate/10, NetInflationAnnual: -0.3, HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
				})
				adjusted := floorRegressionNum(t, out, "AdjustedLivingExpenses")
				funded := floorRegressionNum(t, out, "FundedLivingExpenses")
				if adjusted/st.CumulativeInflation < 7500-1e-7 {
					t.Fatalf("floor violated month %d phase=%v CPI=%g real=%g", m, phase, st.CumulativeInflation, adjusted/st.CumulativeInflation)
				}
				floorRegressionNear(t, "effective multiplier", out.LivingExpenses*out.GuardrailMultiplier, adjusted)
				floorRegressionNear(t, "funded accounting", funded, math.Max(0, adjusted-math.Max(0, out.Result.Shortfall)))
				if e := out.GuardrailEvent; e != nil {
					if e.MonthlySpendingBefore/e.CumulativeInflation < 7500-1e-7 || e.MonthlySpendingAfter/e.CumulativeInflation < 7500-1e-7 {
						t.Fatalf("event contradicts floor: %+v", e)
					}
				}
			}
		}
	}
}
func TestFloorRegressionCanonicalAndBacktestConsumers(t *testing.T) {
	s := floorRegressionSettings()
	floorRegressionFloor(t, s, 7500)
	s.SpendingDeclineRate = 70
	projection, _ := runProj(t, s)
	if len(projection.Months) < 24 {
		t.Fatal("canonical fixture too short")
	}
	for _, m := range projection.Months {
		a := floorRegressionNum(t, m, "AdjustedLivingExpenses")
		f := floorRegressionNum(t, m, "FundedLivingExpenses")
		if a/m.CumulativeInflation < 7500-1e-7 {
			t.Fatal("canonical floor lost")
		}
		floorRegressionNear(t, "canonical funded", f, math.Max(0, a-math.Max(0, m.FundingShortfall)))
	}
	with := runSingleHistoricalSequence(engineInput(t, s), history.DefaultData(), 1982)
	floorRegressionFloor(t, s, 0)
	without := runSingleHistoricalSequence(engineInput(t, s), history.DefaultData(), 1982)
	if with.FinalBalance >= without.FinalBalance {
		t.Fatalf("backtest ignored floor: enforced %g legacy %g", with.FinalBalance, without.FinalBalance)
	}
}
func TestFloorRegressionDisabledAndLegacy(t *testing.T) {
	s := floorRegressionSettings()
	s.Guardrails.Enabled = false
	floorRegressionFloor(t, s, 7500)
	s.MonthlyLivingExpenses = 6000
	st := engine.NewProjectionState(engineInput(t, s))
	o := st.StepMonth(0, func(*models.WhatIfSettings, int) engine.MonthReturns {
		return engine.MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
	})
	floorRegressionNear(t, "disabled floor", floorRegressionNum(t, o, "AdjustedLivingExpenses"), 6000)
	r := floorRegressionMC(t, s, 7500)
	if !floorRegressionField(t, floorRegressionObservation(t, r), "FloorFailed").Bool() {
		t.Fatal("measurement floor silently enforced disabled baseline")
	}
	legacy := RunSingleMonteCarloSimulation(engineInput(t, s), rand.New(rand.NewSource(812)), &MonteCarloConfig{})
	if !floorRegressionField(t, legacy, "FloorOutcome").IsNil() {
		t.Fatal("legacy unexpectedly observes floor")
	}
}
func TestFloorRegressionFundedFloorNotDepletion(t *testing.T) {
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
			s := floorRegressionSettings()
			s.PortfolioValue = 0
			s.Guardrails = nil
			s.MonthlyLivingExpenses = tc.living
			s.IncomeSources = []models.IncomeSource{{ID: "p", Name: "income", Amount: tc.income, Type: models.IncomeFixed, StartMonth: tc.start}}
			r := floorRegressionMC(t, s, tc.floor)
			o := floorRegressionObservation(t, r)
			if floorRegressionField(t, o, "FloorFailed").Bool() != tc.failed {
				t.Fatalf("floor failure %+v", o)
			}
			if floorRegressionField(t, o, "MonthsObserved").Int() != 36 {
				t.Fatalf("observer stopped on legacy depletion: %+v", o)
			}
			if r.Survives || r.DepletionYear != 0 {
				t.Fatalf("legacy FIRST depletion changed: %+v", r)
			}
			want := math.Round(math.Min(tc.income, tc.living)*100) / 100 * float64(36-tc.start)
			floorRegressionNear(t, "funded lifetime", floorRegressionNum(t, o, "TotalFundedLivingReal"), want)
			floorRegressionNear(t, "zero final real balance", floorRegressionNum(t, o, "FinalBalanceReal"), 0)
		})
	}
}

func TestFloorRegressionAnnualCuts(t *testing.T) {
	s := floorRegressionSettings()
	s.Guardrails = nil
	s.MonthlyLivingExpenses = 1000
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "first", StartAge: 0, Multiplier: 1}, {Name: "second", StartAge: 66, Multiplier: 0.5}, {Name: "third", StartAge: 67, Multiplier: 0.25}}}
	r := floorRegressionMC(t, s, 100)
	o := floorRegressionObservation(t, r)
	floorRegressionNear(t, "annual funded totals", floorRegressionNum(t, o, "TotalFundedLivingReal"), 21000)
	floorRegressionNear(t, "worst cut", floorRegressionNum(t, o, "WorstAnnualCutPct"), 50)
	if floorRegressionField(t, o, "AnnualCutCount").Int() != 2 {
		t.Fatal(o)
	}
	floorRegressionNear(t, "zero CPI final balance", floorRegressionNum(t, o, "FinalBalanceReal"), r.FinalBalance)
}

func TestFloorRegressionLockedAssets(t *testing.T) {
	s := floorRegressionSettings()
	s.Guardrails = nil
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 58)
	s.TaxDeferredPercent = 100
	s.TaxDeferredDelayYears = 1
	s.RothPercent = 0
	s.MonthlyLivingExpenses = 750
	r := floorRegressionMC(t, s, 750)
	o := floorRegressionObservation(t, r)
	if !floorRegressionField(t, o, "FloorFailed").Bool() {
		t.Fatal("locked assets counted as funding")
	}
	if floorRegressionField(t, o, "MonthsObserved").Int() != 36 {
		t.Fatal("observer stopped")
	}
	if floorRegressionNum(t, o, "TotalFundedLivingReal") <= 0 {
		t.Fatal("missed later access")
	}
}

func TestFloorOutcomeCompleteYearsAboveOriginalBudget(t *testing.T) {
	tracker := floorOutcomeTracker{floor: 750}
	for _, amount := range []float64{1000, 2000, 1500} {
		for m := 0; m < 12; m++ {
			tracker.observe(amount*1.2, 1.2)
		}
	}
	for m := 0; m < 11; m++ {
		tracker.observe(0, 1.2)
	}
	got := tracker.result(120, 1.2)
	if got.AnnualCutCount != 1 || got.WorstAnnualCutPct != 25 || got.MonthsObserved != 47 || got.TotalFundedLivingReal != 54000 || got.FinalBalanceReal != 100 || !got.FloorFailed {
		t.Fatalf("unexpected observation %+v", got)
	}
}

func TestFloorClampedNoOpEvents(t *testing.T) {
	s := floorRegressionSettings()
	s.MonthlyLivingExpenses = 7500
	s.Guardrails.MinMonthlySpendingReal = 7500
	st := engine.NewProjectionState(engineInput(t, s))
	var changedPolicy bool
	for m := 0; m < 36; m++ {
		out := st.StepMonth(m, func(*models.WhatIfSettings, int) engine.MonthReturns {
			return engine.MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
		})
		if st.Guardrails.Multiplier() < 1 {
			changedPolicy = true
		}
		if out.GuardrailEvent != nil {
			t.Fatalf("clamped policy evaluation emitted spending event %+v", out.GuardrailEvent)
		}
		if out.AdjustedLivingExpenses != 7500 || out.GuardrailMultiplier != 1 {
			t.Fatalf("floor clamp lost %+v", out)
		}
	}
	if !changedPolicy {
		t.Fatal("fixture never triggered policy cut")
	}
}

func TestFloorAdditionalObligationsTakePriority(t *testing.T) {
	s := floorRegressionSettings()
	s.PortfolioValue = 0
	s.MonthlyLivingExpenses = 1000
	s.Guardrails.MinMonthlySpendingReal = 750
	s.MonthlyHealthcare = 200
	s.MonthlyPropertyTax = 100
	s.IncomeSources = []models.IncomeSource{{ID: "income", Name: "income", Amount: 1000, Type: models.IncomeFixed}}
	st := engine.NewProjectionState(engineInput(t, s))
	out := st.StepMonth(0, func(*models.WhatIfSettings, int) engine.MonthReturns {
		return engine.MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
	})
	if out.TotalExpenses != 1300 || out.Healthcare != 200 || out.FundedLivingExpenses != 700 {
		t.Fatalf("additional obligations lost %+v", out)
	}
	tracker := floorOutcomeTracker{floor: 750}
	tracker.observe(out.FundedLivingExpenses, st.CumulativeInflation)
	if !tracker.result(0, 1).FloorFailed {
		t.Fatal("other obligations counted as funded living")
	}
}

func TestFloorStochasticCPIFinalBalance(t *testing.T) {
	s := floorRegressionSettings()
	s.InflationRate = 12
	s.Guardrails = nil
	config := &MonteCarloConfig{MinMonthlySpendingReal: 100}
	result := RunSingleMonteCarloSimulation(engineInput(t, s), rand.New(rand.NewSource(812)), config)
	rng := rand.New(rand.NewSource(812))
	last := -999
	generateAssetReturns(rng, config, s.ProjectionYears, &CrashTiming{}, &last)
	cpi := 1.0
	for year := 0; year < s.ProjectionYears; year++ {
		inflation := s.InflationRate / 100 * (1 + (rng.Float64()-0.5)*0.02)
		rng.Float64()
		rng.Float64()
		rng.Float64()
		months := 12
		if year == 0 {
			months = 11
		}
		for m := 0; m < months; m++ {
			cpi *= math.Pow(1+inflation, 1.0/12)
		}
	}
	floorRegressionNear(t, "stochastic CPI real final", result.FloorOutcome.FinalBalanceReal, result.FinalBalance/cpi)
	if math.Abs(result.FinalBalance-result.FloorOutcome.FinalBalanceReal) < 100 {
		t.Fatal("fixture lacks material inflation")
	}
}
func TestFloorActiveChainReplacement(t *testing.T) {
	s := floorRegressionSettings()
	s.Guardrails.MinMonthlySpendingReal = 7500
	s.MonthlyLivingExpenses = 5000
	next := floorRegressionSettings()
	next.Guardrails.MinMonthlySpendingReal = 9000
	next.MonthlyLivingExpenses = 5000
	in := engineInput(t, s)
	in.Chain = []engine.PreparedChainLink{{Settings: engineInput(t, next).Prepared}}
	in.Hooks.ResolveChainTransition = func(year, index int, _ *models.WhatIfSettings, chain []engine.PreparedChainLink) (int, *models.WhatIfSettings) {
		if year == 1 {
			return 1, chain[0].Settings.Settings()
		}
		return index, nil
	}
	st := engine.NewProjectionState(in)
	for m := 0; m < 24; m++ {
		out := st.StepMonth(m, func(*models.WhatIfSettings, int) engine.MonthReturns {
			return engine.MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
		})
		want := 7500.0
		if m >= 12 {
			want = 9000
		}
		if out.AdjustedLivingExpenses != want {
			t.Fatalf("chain month %d got %g want %g", m, out.AdjustedLivingExpenses, want)
		}
	}
}
func TestFloorActualEventUsesClampedDollars(t *testing.T) {
	s := floorRegressionSettings()
	s.Guardrails.MinMonthlySpendingReal = 7500
	st := engine.NewProjectionState(engineInput(t, s))
	found := false
	for m := 0; m < 24; m++ {
		out := st.StepMonth(m, func(*models.WhatIfSettings, int) engine.MonthReturns {
			return engine.MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
		})
		if e := out.GuardrailEvent; e != nil {
			found = true
			if e.MonthlySpendingBefore != 10000 || e.MonthlySpendingAfter != 7500 || e.PreviousMultiplier != 1 || e.Multiplier != 0.75 {
				t.Fatalf("event not effective %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("fixture never cut")
	}
}
func TestFloorTaxesRemainAdditional(t *testing.T) {
	s := floorRegressionSettings()
	s.PortfolioValue = 0
	s.MonthlyLivingExpenses = 10000
	s.Guardrails.MinMonthlySpendingReal = 9000
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.IncomeSources = []models.IncomeSource{{ID: "income", Name: "income", Amount: 10000, Type: models.IncomeFixed}}
	st := engine.NewProjectionState(engineInput(t, s))
	out := st.StepMonth(0, func(*models.WhatIfSettings, int) engine.MonthReturns {
		return engine.MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
	})
	if out.Result.Shortfall <= 0 || out.FundedLivingExpenses >= 10000 {
		t.Fatalf("tax obligations lost %+v", out)
	}
	floorRegressionNear(t, "tax funding priority", out.FundedLivingExpenses, 10000-out.Result.Shortfall)
}
