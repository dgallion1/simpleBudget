package analysis

import (
	"encoding/json"
	"math/rand"
	"testing"

	"budget2/internal/models"
)

func TestLifestyleEvidenceScenarios(t *testing.T) {
	cases := []struct {
		name                    string
		change                  func(*models.WhatIfSettings, *MonteCarloConfig)
		wantCuts, wantShortfall bool
	}{
		{name: "no_cut_funded"},
		{name: "cut_funded", wantCuts: true, change: func(s *models.WhatIfSettings, c *MonteCarloConfig) {
			s.PortfolioValue = 10000000
			s.RothStockPercent, s.RothCashPercent = 100, 0
			c.CrashProbability = 1
		}},
		{name: "shortfall_despite_guardrails", wantCuts: true, wantShortfall: true, change: func(s *models.WhatIfSettings, c *MonteCarloConfig) {
			s.PortfolioValue = 300000
			s.MonthlyLivingExpenses = 10000
			s.RothStockPercent, s.RothCashPercent = 100, 0
			c.CrashProbability = 1
		}},
		{name: "guardrails_disabled", change: func(s *models.WhatIfSettings, c *MonteCarloConfig) { s.Guardrails = nil }},
		{name: "zero_living", change: func(s *models.WhatIfSettings, c *MonteCarloConfig) {
			s.MonthlyLivingExpenses = 0
			s.MonthlyHealthcare = 1000
			s.RothStockPercent, s.RothCashPercent = 100, 0
			c.CrashProbability = 1
		}},
		{name: "scheduled_phase", change: func(s *models.WhatIfSettings, c *MonteCarloConfig) {
			s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "planned", StartAge: 0, Multiplier: 1}, {Name: "later", StartAge: 67, Multiplier: .7}}}
		}},
		{name: "healthcare_property_heavy", wantShortfall: true, change: func(s *models.WhatIfSettings, c *MonteCarloConfig) {
			s.PortfolioValue = 200000
			s.MonthlyLivingExpenses = 100
			s.MonthlyHealthcare = 10000
			s.MonthlyPropertyTax = 5000
		}},
		{name: "delayed_withdrawals", wantShortfall: true, change: func(s *models.WhatIfSettings, c *MonteCarloConfig) {
			s.TaxDeferredPercent, s.RothPercent = 100, 0
			s.TaxDeferredStockPercent, s.TaxDeferredCashPercent = 0, 100
			s.TaxDeferredDelayYears = 1
		}},
		{name: "july_start", change: func(s *models.WhatIfSettings, c *MonteCarloConfig) {
			s.StartDate = "2026-07"
			s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := monteCarloShockSettings(5)
			s.MonthlyLivingExpenses = 1000
			s.MonthlyHealthcare = 0
			s.MonthlyPropertyTax = 0
			s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 10, FloorCutPct: 10, CeilingRisePct: 100, CeilingRaisePct: 0, MinSpendingPct: 50, MaxSpendingPct: 100}
			c := deterministicShockConfig()
			c.SpendingShockProb, c.HealthShockProb = 0, 0
			if tc.change != nil {
				tc.change(s, c)
			}
			in := engineInput(t, s)
			runs := make([]models.MonteCarloResult, 100)
			for i := range runs {
				runs[i] = RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(7000+int64(i))), c)
			}
			stats := aggregateLifestyleOutcomes(runs)
			if stats == nil || stats.FundedWithoutCuts+stats.FundedWithCuts+stats.Shortfall != 100 {
				t.Fatalf("invalid count partition: %+v", stats)
			}
			if tc.wantCuts && stats.RunsWithCuts == 0 {
				t.Fatal("fixture did not exercise cuts")
			}
			if !tc.wantCuts && tc.name != "healthcare_property_heavy" && stats.RunsWithCuts != 0 {
				t.Fatalf("unexpected cuts: %+v", stats)
			}
			if tc.wantShortfall && stats.Shortfall == 0 {
				t.Fatal("fixture did not exercise shortfall")
			}
			if !tc.wantShortfall && stats.Shortfall != 0 {
				t.Fatalf("unexpected shortfall: %+v", stats)
			}
			if tc.name == "delayed_withdrawals" && (stats.Shortfall != 100 || !runs[0].Survives) {
				t.Fatal("delay must distinguish funding from legacy survival")
			}
			raw, err := json.Marshal(stats)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("EVIDENCE %s %s", tc.name, raw)
		})
	}
}

func TestLifestyleEvidenceEmergencyScale(t *testing.T) {
	s := monteCarloShockSettings(30)
	s.RothStockPercent, s.RothCashPercent = 70, 30
	s.MonthlyLivingExpenses = 3200
	s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 10, CeilingRisePct: 20, CeilingRaisePct: 10, MinSpendingPct: 75, MaxSpendingPct: 120}
	in := engineInput(t, s)
	// All-Roth fixture isolates charge size; discrete taxable-withdrawal classification
	// is independently covered by L1 and is not part of this scale comparison.
	for _, full := range []bool{false, true} {
		c := DefaultMonteCarloConfig()
		if !full {
			c.SpendingShockMin /= 12
			c.SpendingShockMax /= 12
			c.HealthShockMin /= 12
			c.HealthShockMax /= 12
		}
		runs := make([]models.MonteCarloResult, 300)
		for i := range runs {
			runs[i] = RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(8765+int64(i))), c)
		}
		stats := aggregateLifestyleOutcomes(runs)
		if stats == nil || stats.FundedWithoutCuts+stats.FundedWithCuts+stats.Shortfall != 300 {
			t.Fatalf("invalid partition: %+v", stats)
		}
		raw, err := json.Marshal(stats)
		if err != nil {
			t.Fatal(err)
		}
		core, _ := monteCarloCoreStats(runs)
		if core.SuccessRate != 100*float64(stats.FundedWithoutCuts+stats.FundedWithCuts)/float64(stats.Runs) {
			t.Fatalf("all-Roth no-delay funding and depletion rates differ: %v vs %+v", core.SuccessRate, stats)
		}
		t.Logf("DEPLETION_AVOIDANCE full_emergency_charge=%v success_rate=%.12g", full, core.SuccessRate)
		t.Logf("EVIDENCE full_emergency_charge=%v %s", full, raw)
	}
}
