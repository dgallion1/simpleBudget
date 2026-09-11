package analysis

import (
	"encoding/json"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

func TestSpendingCandidateMapsPhaseAdjustedStart(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 8000
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true,
		Phases: []models.SpendingPhase{{Name: "Go-Go", StartAge: 0, Multiplier: 1.1}}}
	s.IncomeSources = []models.IncomeSource{{Name: "Pension", Amount: 2500}}
	in := engineInput(t, s)
	in.Hooks.SocialSecurityProjectionActive = func(*models.WhatIfSettings) bool { return true }
	in.Hooks.ProjectedSocialSecurityIncome = func(*models.WhatIfSettings, int) float64 { return 1234 }
	in.Hooks.ResolveChainTransition = func(_ int, index int, _ *models.WhatIfSettings, _ []engine.PreparedChainLink) (int, *models.WhatIfSettings) {
		return index, nil
	}
	before, err := prepare.Clone(in.Prepared.Settings())
	if err != nil {
		t.Fatal(err)
	}
	g := &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, CeilingRisePct: 20,
		MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: 7000}
	c, err := spendingCandidateForStart(in, 11000, 7000, nil, g, "planned", "p-11000")
	if err != nil {
		t.Fatal(err)
	}
	out, err := PrepareSpendingCandidate(in, c)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(c.BaseMonthlyLivingExpenses-10000) > 1e-9 {
		t.Fatalf("candidate base = %.12f, want 10000", c.BaseMonthlyLivingExpenses)
	}
	if c.Guardrails == g || !reflect.DeepEqual(c.Guardrails, g) {
		t.Fatal("candidate did not independently copy the requested guardrails")
	}
	if math.Abs(out.Prepared.Settings().MonthlyLivingExpenses-10000) > .005 {
		t.Fatal("candidate did not invert the active phase multiplier")
	}
	if math.Abs(engine.LivingExpensesAtMonth(out.Prepared.Settings(), 0)-11000) > .005 {
		t.Fatal("displayed starting budget differs from prepared candidate")
	}
	if !reflect.DeepEqual(before, in.Prepared.Settings()) {
		t.Fatal("candidate construction or preparation mutated the original plan")
	}
	gotOther, err := prepare.Clone(out.Prepared.Settings())
	if err != nil {
		t.Fatal(err)
	}
	gotOther.MonthlyLivingExpenses = before.MonthlyLivingExpenses
	gotOther.Guardrails = cloneSpendingCandidateTestGuardrails(before.Guardrails)
	gotOther.LivingSpendingBoost = models.CloneLivingSpendingBoost(before.LivingSpendingBoost)
	if !reflect.DeepEqual(before, gotOther) {
		t.Fatal("candidate preparation changed settings outside living, boost, and guardrails")
	}
	if !out.Hooks.SSActive(out.Prepared.Settings()) || out.Hooks.SSIncome(out.Prepared.Settings(), 0) != 1234 {
		t.Fatal("candidate preparation lost input hooks")
	}
	if reflect.ValueOf(out.Hooks.ResolveChainTransition).Pointer() != reflect.ValueOf(in.Hooks.ResolveChainTransition).Pointer() {
		t.Fatal("candidate preparation replaced the chain hook")
	}
}

func TestSpendingCandidateMapsNonUnitPhaseFactorsAndBoost(t *testing.T) {
	tests := []struct {
		name       string
		multiplier float64
		start      float64
		boost      *models.LivingSpendingBoost
		wantBase   float64
	}{
		{name: "above one", multiplier: 1.25, start: 12500, wantBase: 10000},
		{name: "below one", multiplier: .8, start: 8000, wantBase: 10000},
		{name: "active boost subtracted before division", multiplier: 1.1, start: 12000,
			boost: &models.LivingSpendingBoost{MonthlyReal: 1000, StopMonth: "2036-09"}, wantBase: 10000},
		{name: "full precision retained", multiplier: 1.07, start: 12345.67, wantBase: 11538.00934579439},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.StartDate = "2026-09"
			s.MonthlyLivingExpenses = 6000 // The current plan may be below the desired floor.
			s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true,
				Phases: []models.SpendingPhase{{Name: "Active", StartAge: 0, Multiplier: tt.multiplier}}}
			in := engineInput(t, s)
			policy := &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, CeilingRisePct: 20,
				MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: 7000}
			c, err := spendingCandidateForStart(in, tt.start, 7000, tt.boost, policy, "planned", "candidate")
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(c.BaseMonthlyLivingExpenses-tt.wantBase) > 1e-8 {
				t.Fatalf("base = %.12f, want %.12f", c.BaseMonthlyLivingExpenses, tt.wantBase)
			}
			out, err := PrepareSpendingCandidate(in, c)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(engine.LivingExpensesAtMonth(out.Prepared.Settings(), 0)-tt.start) > .005 {
				t.Fatalf("canonical start = %.12f, want %.2f", engine.LivingExpensesAtMonth(out.Prepared.Settings(), 0), tt.start)
			}
		})
	}
}

func TestSpendingCandidateRejectsInvalidMappingInputs(t *testing.T) {
	valid := models.DefaultWhatIfSettings()
	valid.StartDate = "2026-09"
	in := engineInput(t, valid)
	policy := &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, CeilingRisePct: 20,
		MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: 7000}

	if _, err := spendingCandidateForStart(engine.Input{}, 8000, 7000, nil, policy, "planned", "zero"); err == nil {
		t.Fatal("zero prepared input accepted")
	}
	chainInput := in
	chainInput.Chain = []engine.PreparedChainLink{{ScenarioFilename: "later.json"}}
	if _, err := spendingCandidateForStart(chainInput, 8000, 7000, nil, policy, "planned", "chain"); err == nil {
		t.Fatal("resolved scenario chain accepted")
	}
	chainSettings := models.DefaultWhatIfSettings()
	chainSettings.ScenarioChain = []models.ScenarioChainLink{{ScenarioFilename: "later.json", TransitionAge: 75}}
	if _, err := spendingCandidateForStart(engineInput(t, chainSettings), 8000, 7000, nil, policy, "planned", "chain"); err == nil {
		t.Fatal("configured scenario chain accepted")
	}
	boost := &models.LivingSpendingBoost{MonthlyReal: 8000, StopMonth: "2036-09"}
	if _, err := spendingCandidateForStart(in, 7000, 7000, boost, policy, "planned", "no-base"); err == nil {
		t.Fatal("requested start with no positive nonboost base accepted")
	}
	if _, err := PrepareSpendingCandidate(engine.Input{}, models.SpendingCandidate{BaseMonthlyLivingExpenses: 8000}); err == nil {
		t.Fatal("public preparation accepted zero input")
	}
	if _, err := PrepareSpendingCandidate(chainInput, models.SpendingCandidate{BaseMonthlyLivingExpenses: 8000}); err == nil {
		t.Fatal("public preparation accepted a chained input")
	}

	for _, tc := range []struct {
		name       string
		multiplier float64
	}{
		{name: "zero", multiplier: 0},
		{name: "negative", multiplier: -1},
		{name: "nan", multiplier: math.NaN()},
		{name: "infinity", multiplier: math.Inf(1)},
	} {
		t.Run(tc.name+" factor", func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true,
				Phases: []models.SpendingPhase{{StartAge: 0, Multiplier: 1}}}
			factorInput := engineInput(t, s)
			factorInput.Prepared.Settings().SpendingPhaseConfig.Phases[0].Multiplier = tc.multiplier
			if _, err := spendingCandidateForStart(factorInput, 8000, 7000, nil, policy, "planned", "bad-factor"); err == nil {
				t.Fatalf("%s phase factor accepted", tc.name)
			}
		})
	}
}

func TestSpendingCandidateCurrentBaselineRetainsSavedPlan(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-09"
	s.MonthlyLivingExpenses = 8000
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true,
		Phases: []models.SpendingPhase{{StartAge: 0, Multiplier: 1.1}}}
	s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 500, StopMonth: "2031-04"}
	s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 17, FloorCutPct: 9,
		CeilingRisePct: 23, CeilingRaisePct: 7, MinSpendingPct: 72, MaxSpendingPct: 131, MinMonthlySpendingReal: 6200}
	in := engineInput(t, s)
	replacement := &models.GuardrailConfig{Enabled: true, FloorDropPct: 1, FloorCutPct: 0,
		CeilingRisePct: 1, CeilingRaisePct: 0, MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: 7000}
	c, err := spendingCandidateForStart(in, 12000, 7000, nil, replacement, "current", "current")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Baseline || c.BaseMonthlyLivingExpenses != 8000 || math.Abs(c.StartingMonthlyLivingReal-9300) > .005 {
		t.Fatalf("current baseline changed budget: %+v", c)
	}
	if !reflect.DeepEqual(c.Guardrails, s.Guardrails) || !reflect.DeepEqual(c.LivingSpendingBoost, s.LivingSpendingBoost) {
		t.Fatalf("current baseline changed saved policy or boost: %+v", c)
	}
	if c.Guardrails == s.Guardrails || c.LivingSpendingBoost == s.LivingSpendingBoost {
		t.Fatal("current baseline aliases saved mutable settings")
	}
	out, err := PrepareSpendingCandidate(in, c)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Prepared.Settings(), in.Prepared.Settings()) {
		t.Fatal("prepared current baseline differs from saved plan")
	}
}

func TestSpendingCandidateNoExtraCutsPolicyFollowsPlannedSchedule(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-09"
	s.MonthlyLivingExpenses = 10000
	s.PortfolioValue = 1e9
	s.ProjectionYears = 10
	s.InflationRate = 3
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{
		{Name: "Go-Go", StartAge: 0, Multiplier: 1},
		{Name: "Slow-Go", StartAge: 66, Multiplier: .5},
	}}
	in := engineInput(t, s)
	policy := &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 0,
		CeilingRisePct: 20, CeilingRaisePct: 0, MinSpendingPct: 100, MaxSpendingPct: 100,
		MinMonthlySpendingReal: 7000}
	c, err := spendingCandidateForStart(in, 10000, 7000, nil, policy, "planned", "planned-10000")
	if err != nil {
		t.Fatal(err)
	}
	out, err := PrepareSpendingCandidate(in, c)
	if err != nil {
		t.Fatal(err)
	}
	projection := engine.New().Run(out)
	for _, month := range projection.Months {
		if month.AdjustedLivingExpenses+.005 < month.PlannedLivingExpenses {
			t.Fatalf("month %d cut planned living: planned %.2f adjusted %.2f", month.Month, month.PlannedLivingExpenses, month.AdjustedLivingExpenses)
		}
	}
	month12 := projection.Months[12]
	wantFloor := 7000 * month12.CumulativeInflation
	if !(month12.PlannedLivingExpenses < wantFloor) || math.Abs(month12.AdjustedLivingExpenses-wantFloor) > .01 {
		t.Fatalf("phase reduction was not clamped by floor: planned %.2f adjusted %.2f floor %.2f", month12.PlannedLivingExpenses, month12.AdjustedLivingExpenses, wantFloor)
	}
	cfg := DefaultMonteCarloConfig()
	cfg.ReturnVolatility = 0
	cfg.CrashProbability = 0
	cfg.SpendingShockProb = 0
	cfg.HealthShockProb = 0
	cfg.LongevityVariation = 0
	cfg.MinMonthlySpendingReal = 7000
	cfg.CaptureSpendingYears = true
	row := RunSingleMonteCarloSimulation(out, rand.New(rand.NewSource(42)), cfg)
	if row.GuardrailImpact == nil || row.GuardrailImpact.MonthsBelowPlan != 0 {
		t.Fatalf("seeded no-extra-cuts projection cut below plan: %+v", row.GuardrailImpact)
	}
}

func cloneSpendingCandidateTestGuardrails(g *models.GuardrailConfig) *models.GuardrailConfig {
	if g == nil {
		return nil
	}
	clone := *g
	return &clone
}

func TestSpendingCandidateJSONRoundTripPreservesContract(t *testing.T) {
	metrics := &models.SpendingRiskMetrics{Runs: 1000, CutPaths: 7, LowestObservedMonthlyReal: 6999.99}
	c := models.SpendingCandidate{
		ID: "flex-12345.67", Kind: "flexible", BaseMonthlyLivingExpenses: 11537.07476635514,
		StartingMonthlyLivingReal: 12345.67,
		Guardrails: &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 10,
			CeilingRisePct: 25, CeilingRaisePct: 5, MinSpendingPct: 75, MaxSpendingPct: 125, MinMonthlySpendingReal: 7000},
		LivingSpendingBoost: &models.LivingSpendingBoost{MonthlyReal: 1000.01, StopMonth: "2036-09"},
		Qualifies:           true, Metrics: metrics, WorstPathIndex: 9,
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var got models.SpendingCandidate
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, got) {
		t.Fatalf("candidate JSON round trip changed values:\nwant %+v\n got %+v", c, got)
	}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(models.SpendingOptimizerRequest{}),
		reflect.TypeOf(models.SpendingCandidate{}),
		reflect.TypeOf(models.SpendingRiskMetrics{}),
	} {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			tag := strings.Split(field.Tag.Get("json"), ",")[0]
			if tag == "" || tag == "-" || strings.ToLower(tag) != tag || strings.Contains(tag, " ") {
				t.Fatalf("%s.%s lacks an explicit snake_case JSON tag: %q", typ.Name(), field.Name, tag)
			}
		}
	}
}
