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

// With guardrails enabled and a forced cut, post-cut months must show:
//
//	GuardrailMultiplier < 1.0
//	PlannedLivingExpenses unchanged (planned line is multiplier-independent)
func TestProjection_PlannedFields_WithCut(t *testing.T) {
	s := defaultSettingsForTest()
	s.InvestmentReturn = 0        // no growth so portfolio shrinks under spending pressure
	s.MonthlyLivingExpenses = 8000 // large enough to deplete >1% of $1M portfolio per year
	s.Guardrails = &models.GuardrailConfig{
		Enabled:         true,
		FloorDropPct:    1,  // hair-trigger so a cut fires near year 1
		FloorCutPct:     10,
		CeilingRisePct:  500, // disabled in practice
		CeilingRaisePct: 10,
		MinSpendingPct:  50,
		MaxSpendingPct:  150,
	}

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
