package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestBudgetSteadyState_GrossWithdrawalRowsRender(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	t.Run("renders rows when SteadyStateGap > 0", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-budget-steady-state", map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
			"Analysis": &models.WhatIfAnalysis{
				BudgetFit: &models.BudgetFitAnalysis{
					HasSteadyState:                        true,
					SteadyStateYear:                       15,
					SteadyStateExpenses:                   12000,
					SteadyStateIncome:                     2000,
					SteadyStateGap:                        5000,
					SteadyStateRate:                       3.0,
					SteadyStateNetWithdrawalTaxDeferred:   3000,
					SteadyStateGrossWithdrawalTaxDeferred: 4109.59,
					SteadyStateMarginalRateTaxDeferred:    27.0,
					SteadyStateNetWithdrawalTaxable:       1500,
					SteadyStateGrossWithdrawalTaxable:     1648.35,
					SteadyStateEffectiveRateTaxable:       9.0,
					SteadyStateNetWithdrawalRoth:          500,
					SteadyStateGrossWithdrawalRoth:        500,
				},
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "Suggested Withdrawal Mix") {
			t.Errorf("expected withdrawal-mix heading; got: %s", truncate(out, 500))
		}
		if !strings.Contains(out, "From Tax-Deferred") {
			t.Errorf("expected From Tax-Deferred row")
		}
		if !strings.Contains(out, "From Taxable") {
			t.Errorf("expected From Taxable row")
		}
		if !strings.Contains(out, "From Roth") {
			t.Errorf("expected From Roth row")
		}
		if !strings.Contains(out, "27% marginal") {
			t.Errorf("expected marginal rate annotation; got: %s", truncate(out, 500))
		}
	})

	t.Run("RMD-driven surplus shows RMD as Tax-Deferred withdrawal", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-budget-steady-state", map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
			"Analysis": &models.WhatIfAnalysis{
				BudgetFit: &models.BudgetFitAnalysis{
					HasSteadyState:      true,
					SteadyStateYear:     20,
					SteadyStateExpenses: 3000,
					SteadyStateIncome:   5000,
					SteadyStateGap:      -2000, // surplus
					SteadyStateRMD:      1500,
				},
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "Suggested Withdrawal Mix") {
			t.Errorf("expected mix heading even on surplus; got: %s", truncate(out, 500))
		}
		for _, row := range []string{"From Tax-Deferred", "From Taxable", "From Roth"} {
			if !strings.Contains(out, row) {
				t.Errorf("expected %q row on surplus", row)
			}
		}
		// Tax-Deferred row must show the RMD amount, not $0.
		if !strings.Contains(out, "$1,500.00/mo") {
			t.Errorf("expected RMD ($1,500/mo) shown as Tax-Deferred withdrawal; got: %s", truncate(out, 800))
		}
		if !strings.Contains(out, "(mandatory RMD)") {
			t.Errorf("expected (mandatory RMD) annotation on Tax-Deferred row")
		}
		if !strings.Contains(out, "mandatory RMD covers spending") {
			t.Errorf("expected RMD-surplus caption; got: %s", truncate(out, 800))
		}
		// Shortfall caption must not appear.
		if strings.Contains(out, "three rows sum to the gap") {
			t.Errorf("did not expect shortfall caption on surplus")
		}
	})

	t.Run("surplus without RMD shows zero rows with no-withdrawal caption", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-budget-steady-state", map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
			"Analysis": &models.WhatIfAnalysis{
				BudgetFit: &models.BudgetFitAnalysis{
					HasSteadyState:      true,
					SteadyStateYear:     5,
					SteadyStateExpenses: 3000,
					SteadyStateIncome:   5000,
					SteadyStateGap:      -2000, // surplus, no RMD
					SteadyStateRMD:      0,
				},
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "$0.00/mo") {
			t.Errorf("expected $0.00/mo in mix rows when no RMD and surplus; got: %s", truncate(out, 800))
		}
		if strings.Contains(out, "(mandatory RMD)") {
			t.Errorf("did not expect (mandatory RMD) annotation when SteadyStateRMD == 0")
		}
		if !strings.Contains(out, "No withdrawal needed") {
			t.Errorf("expected no-withdrawal caption; got: %s", truncate(out, 800))
		}
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
