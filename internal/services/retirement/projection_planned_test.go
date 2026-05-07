package retirement

import (
	"budget2/internal/models"
	"testing"
)

// With guardrails disabled, every ProjectionMonth must have:
//
//	GuardrailMultiplier == 1.0
//	PlannedLivingExpenses > 0 and finite
//	PlannedLivingExpenses == GeneralExpenses (they describe the same value)
func TestProjection_PlannedFields_NoGuardrails(t *testing.T) {
	s := defaultSettingsForTest()
	s.Guardrails = nil // explicitly disabled

	calc := NewCalculator(s)
	result := calc.RunFullAnalysis()

	if result.Projection == nil || len(result.Projection.Months) == 0 {
		t.Fatalf("expected projection months, got none")
	}

	for i, m := range result.Projection.Months {
		if m.GuardrailMultiplier != 1.0 {
			t.Errorf("month %d: GuardrailMultiplier = %v, want 1.0", i, m.GuardrailMultiplier)
		}
		if m.PlannedLivingExpenses <= 0 {
			t.Errorf("month %d: PlannedLivingExpenses = %v, want > 0", i, m.PlannedLivingExpenses)
		}
		if !almostEqual(m.PlannedLivingExpenses, m.GeneralExpenses) {
			t.Errorf("month %d: PlannedLivingExpenses (%v) != GeneralExpenses (%v)",
				i, m.PlannedLivingExpenses, m.GeneralExpenses)
		}
	}
}

// cutTriggeringSettings produces a fixture that forces a guardrail floor cut
// in year 1: zero growth + high spending + hair-trigger drop pct. Used by every
// guardrail-active test in this file.
func cutTriggeringSettings() *models.WhatIfSettings {
	s := defaultSettingsForTest()
	s.InvestmentReturn = 0        // no growth so portfolio shrinks under spending pressure
	s.MonthlyLivingExpenses = 8000 // large enough to deplete >1% of $1M portfolio per year
	s.Guardrails = &models.GuardrailConfig{
		Enabled:         true,
		FloorDropPct:    1, // hair-trigger so a cut fires near year 1
		FloorCutPct:     10,
		CeilingRisePct:  500, // disabled in practice
		CeilingRaisePct: 10,
		MinSpendingPct:  50,
		MaxSpendingPct:  150,
	}
	return s
}

// With guardrails enabled and a forced cut, post-cut months must show:
//
//	GuardrailMultiplier < 1.0
//	PlannedLivingExpenses unchanged (planned line is multiplier-independent)
func TestProjection_PlannedFields_WithCut(t *testing.T) {
	s := cutTriggeringSettings()

	calc := NewCalculator(s)
	result := calc.RunFullAnalysis()

	sawAdjusted := false
	for i, m := range result.Projection.Months {
		if m.GuardrailMultiplier < 1.0 {
			sawAdjusted = true
			if m.PlannedLivingExpenses <= 0 {
				t.Fatalf("month %d: PlannedLivingExpenses must remain > 0 even after a cut, got %v", i, m.PlannedLivingExpenses)
			}
			// PlannedLivingExpenses must remain multiplier-independent: it should still equal
			// GeneralExpenses (both are sourced from currentLivingExpenses pre-multiplier).
			// A bug that stored the adjusted value in PlannedLivingExpenses would diverge here.
			if !almostEqual(m.PlannedLivingExpenses, m.GeneralExpenses) {
				t.Errorf("month %d: PlannedLivingExpenses (%v) != GeneralExpenses (%v) — planned line should be multiplier-independent",
					i, m.PlannedLivingExpenses, m.GeneralExpenses)
			}
		}
	}
	if !sawAdjusted {
		t.Fatalf("expected at least one month with GuardrailMultiplier < 1.0 given hair-trigger cut")
	}
}

// YearSummary.PlannedExpenses must equal Expenses when guardrails are disabled —
// across all expense components: living, healthcare, IRMAA, and ExpenseSources.
// The ExpenseSource is included to catch a mirroring bug where the projection loop
// forgets to accumulate non-living-expense components into PlannedExpenses.
func TestProjectionYear_PlannedExpenses_NoGuardrails(t *testing.T) {
	s := defaultSettingsForTest()
	s.Guardrails = nil
	s.ExpenseSources = []models.ExpenseSource{
		{ID: "subs", Name: "Subscriptions", Amount: 500, StartYear: 0, EndYear: 0, Inflation: false},
	}

	result := NewCalculator(s).RunFullAnalysis()
	for i, ys := range result.Projection.YearlySummaries {
		if !almostEqual(ys.PlannedExpenses, ys.Expenses) {
			t.Errorf("year %d: PlannedExpenses (%v) != Expenses (%v) when guardrails disabled",
				i, ys.PlannedExpenses, ys.Expenses)
		}
		if ys.GuardrailMultiplier != 1.0 {
			t.Errorf("year %d: GuardrailMultiplier = %v, want 1.0", i, ys.GuardrailMultiplier)
		}
	}
}

func TestProjectionYear_PlannedExpenses_WithCut(t *testing.T) {
	s := cutTriggeringSettings()

	result := NewCalculator(s).RunFullAnalysis()
	sawCutYear := false
	for _, ys := range result.Projection.YearlySummaries {
		if ys.GuardrailMultiplier < 1.0 {
			sawCutYear = true
			if ys.PlannedExpenses <= ys.Expenses {
				t.Errorf("year %d: planned (%v) must exceed adjusted (%v) when multiplier < 1.0",
					ys.Year, ys.PlannedExpenses, ys.Expenses)
			}
		}
	}
	if !sawCutYear {
		t.Fatalf("expected at least one year with GuardrailMultiplier < 1.0")
	}
}

// GuardrailEvent.MonthlySpendingBefore/After must be populated and consistent with the multiplier change.
func TestGuardrailEvent_DollarFields(t *testing.T) {
	s := cutTriggeringSettings()

	result := NewCalculator(s).RunFullAnalysis()
	if len(result.Projection.GuardrailEvents) == 0 {
		t.Fatalf("expected at least one guardrail event")
	}
	for _, e := range result.Projection.GuardrailEvents {
		if e.MonthlySpendingBefore <= 0 {
			t.Errorf("year %d: MonthlySpendingBefore = %v, want > 0", e.Year, e.MonthlySpendingBefore)
		}
		if e.MonthlySpendingAfter <= 0 {
			t.Errorf("year %d: MonthlySpendingAfter = %v, want > 0", e.Year, e.MonthlySpendingAfter)
		}
		if e.Type == "cut" && e.MonthlySpendingAfter >= e.MonthlySpendingBefore {
			t.Errorf("year %d: cut should reduce spending, got %v -> %v", e.Year, e.MonthlySpendingBefore, e.MonthlySpendingAfter)
		}
	}
}
