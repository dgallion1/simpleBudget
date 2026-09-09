package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// gv2TriggerFixture builds a settings scenario that fires at least one
// guardrail cut and one guardrail raise: a large living budget drains the
// portfolio for the first years, then a delayed pension lifts it back above
// the raise trigger. Mirrors the GV2 acceptance oracle's fixture.
func gv2TriggerFixture() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 500000
	s.MonthlyLivingExpenses = 12000
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 0
	s.InflationRate = 2.5
	s.SpendingDeclineRate = 0
	s.InvestmentReturn = 1.0
	s.ProjectionYears = 8
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: false}
	s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 10, CeilingRisePct: 20, CeilingRaisePct: 10, MinSpendingPct: 50, MaxSpendingPct: 150}
	s.IncomeSources = []models.IncomeSource{{ID: "gv2-pension", Name: "Pension", Amount: 30000, Type: models.IncomeDelayed, StartMonth: 36}}
	return s
}

func gv2Close(a, b float64) bool {
	if a == b {
		return true
	}
	return math.Abs(a-b) <= 1e-6*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

// TestGuardrailTriggerLevelsMatchEvaluate is the GV2 criterion-2 permanent
// test: the engine's exposed GuardrailPeak/GuardrailBaseline/
// GuardrailCutTrigger/GuardrailRaiseTrigger fields, on every yearly summary,
// are exactly the state GuardrailState.Evaluate produced (peak/baseline) and
// the thresholds derived from it — never a re-derivation. It restates
// Evaluate's own semantics (see guardrails.go), not new arithmetic.
func TestGuardrailTriggerLevelsMatchEvaluate(t *testing.T) {
	s := gv2TriggerFixture()
	proj := New().Run(Input{Prepared: prepare.MustFrom(t, s)})

	cuts, raises := 0, 0
	for _, e := range proj.GuardrailEvents {
		if e.Type == "cut" {
			cuts++
		} else {
			raises++
		}
	}
	if cuts == 0 || raises == 0 {
		t.Fatalf("fixture defect: need >=1 cut and >=1 raise, got cuts=%d raises=%d events=%+v", cuts, raises, proj.GuardrailEvents)
	}

	g := s.Guardrails
	for y, ys := range proj.YearlySummaries {
		if ys.GuardrailPeak <= 0 || ys.GuardrailBaseline <= 0 || ys.GuardrailCutTrigger <= 0 || ys.GuardrailRaiseTrigger <= 0 {
			t.Fatalf("year %d: guardrail threshold fields missing or zero: %+v", y, ys)
		}
		if !gv2Close(ys.GuardrailCutTrigger, ys.GuardrailPeak*(1-g.FloorDropPct/100)) {
			t.Fatalf("year %d: cut trigger %.6f != peak %.6f x (1-%.0f%%)", y, ys.GuardrailCutTrigger, ys.GuardrailPeak, g.FloorDropPct)
		}
		if !gv2Close(ys.GuardrailRaiseTrigger, ys.GuardrailBaseline*(1+g.CeilingRisePct/100)) {
			t.Fatalf("year %d: raise trigger %.6f != baseline %.6f x (1+%.0f%%)", y, ys.GuardrailRaiseTrigger, ys.GuardrailBaseline, g.CeilingRisePct)
		}
	}
	if !gv2Close(proj.YearlySummaries[0].GuardrailPeak, s.PortfolioValue) || !gv2Close(proj.YearlySummaries[0].GuardrailBaseline, s.PortfolioValue) {
		t.Fatalf("year 0 peak/baseline %.2f/%.2f != starting portfolio %.2f", proj.YearlySummaries[0].GuardrailPeak, proj.YearlySummaries[0].GuardrailBaseline, s.PortfolioValue)
	}

	events := map[int]models.GuardrailEvent{}
	for _, e := range proj.GuardrailEvents {
		events[e.Year] = e
	}
	for n := 1; n < s.ProjectionYears; n++ {
		prev := proj.YearlySummaries[n-1]
		check := proj.Months[n*12-1].PortfolioBalance
		e, hasEvent := events[n]
		if !hasEvent {
			mult := proj.YearlySummaries[n].GuardrailMultiplier
			pinned := gv2Close(mult, g.MinSpendingPct/100) || gv2Close(mult, g.MaxSpendingPct/100)
			if !pinned && (check <= prev.GuardrailCutTrigger || check >= prev.GuardrailRaiseTrigger) {
				t.Fatalf("year %d: no event but balance %.2f outside (%.2f, %.2f)", n, check, prev.GuardrailCutTrigger, prev.GuardrailRaiseTrigger)
			}
			continue
		}
		if !gv2Close(e.Portfolio, check) {
			t.Fatalf("year %d: event portfolio %.2f != check-month balance %.2f", n, e.Portfolio, check)
		}
		switch e.Type {
		case "cut":
			if e.Portfolio > prev.GuardrailCutTrigger {
				t.Fatalf("year %d: cut with portfolio %.2f ABOVE cut trigger %.2f", n, e.Portfolio, prev.GuardrailCutTrigger)
			}
			if !gv2Close(proj.YearlySummaries[n].GuardrailPeak, e.Portfolio) {
				t.Fatalf("year %d: after cut, peak %.2f should reset to %.2f", n, proj.YearlySummaries[n].GuardrailPeak, e.Portfolio)
			}
		case "raise":
			if e.Portfolio < prev.GuardrailRaiseTrigger {
				t.Fatalf("year %d: raise with portfolio %.2f BELOW raise trigger %.2f", n, e.Portfolio, prev.GuardrailRaiseTrigger)
			}
			if !gv2Close(proj.YearlySummaries[n].GuardrailBaseline, e.Portfolio) {
				t.Fatalf("year %d: after raise, baseline %.2f should reset to %.2f", n, proj.YearlySummaries[n].GuardrailBaseline, e.Portfolio)
			}
		}
	}
}

// TestGuardrailTriggerLevelsZeroWhenDisabled asserts the additive fields are
// zero (and therefore omitted from JSON via omitempty) whenever guardrails
// are off, so every pre-existing JSON consumer of ProjectionYearSummary sees
// byte-identical output.
func TestGuardrailTriggerLevelsZeroWhenDisabled(t *testing.T) {
	s := gv2TriggerFixture()
	s.Guardrails = nil
	proj := New().Run(Input{Prepared: prepare.MustFrom(t, s)})
	for y, ys := range proj.YearlySummaries {
		if ys.GuardrailPeak != 0 || ys.GuardrailBaseline != 0 || ys.GuardrailCutTrigger != 0 || ys.GuardrailRaiseTrigger != 0 {
			t.Fatalf("year %d: guardrails disabled but thresholds carried: %+v", y, ys)
		}
	}
}

// TestGuardrailTriggerLevelsNoEventFixture covers a scenario where the
// multiplier never moves: thresholds must still be populated every year.
func TestGuardrailTriggerLevelsNoEventFixture(t *testing.T) {
	s := gv2TriggerFixture()
	s.MonthlyLivingExpenses = 1000
	s.IncomeSources = nil
	s.ProjectionYears = 6
	s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 10, CeilingRisePct: 20, CeilingRaisePct: 10, MinSpendingPct: 75, MaxSpendingPct: 120}
	proj := New().Run(Input{Prepared: prepare.MustFrom(t, s)})
	if len(proj.GuardrailEvents) != 0 {
		t.Fatalf("fixture defect: expected no events, got %+v", proj.GuardrailEvents)
	}
	for y, ys := range proj.YearlySummaries {
		if ys.GuardrailCutTrigger <= 0 || ys.GuardrailRaiseTrigger <= 0 {
			t.Fatalf("year %d: thresholds missing with no events", y)
		}
	}
}
