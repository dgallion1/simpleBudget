package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestBudgetAnalysis_GrossWithdrawalRowsRender(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	t.Run("renders rows when MonthlyGap > 0", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
			"Analysis": &models.WhatIfAnalysis{
				BudgetFit: &models.BudgetFitAnalysis{
					MonthlyExpenses:            5000,
					MonthlyIncome:              1000,
					MonthlyGap:                 4000,
					RequiredRate:               4.0,
					NetWithdrawalTaxDeferred:   2400,
					GrossWithdrawalTaxDeferred: 3287.67,
					MarginalRateTaxDeferred:    27.0,
					NetWithdrawalTaxable:       1200,
					GrossWithdrawalTaxable:     1250.00,
					EffectiveRateTaxable:       4.0,
					NetWithdrawalRoth:          400,
					GrossWithdrawalRoth:        400,
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

	t.Run("hides rows on surplus", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
			"Analysis": &models.WhatIfAnalysis{
				BudgetFit: &models.BudgetFitAnalysis{
					MonthlyExpenses: 3000,
					MonthlyIncome:   5000,
					MonthlyGap:      -2000, // surplus
				},
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if strings.Contains(out, "Suggested Withdrawal Mix") {
			t.Errorf("expected no withdrawal-mix heading on surplus; got: %s", truncate(out, 500))
		}
		if strings.Contains(out, "From Tax-Deferred") {
			t.Errorf("expected no From Tax-Deferred row on surplus")
		}
		if strings.Contains(out, "From Taxable") {
			t.Errorf("expected no From Taxable row on surplus")
		}
		if strings.Contains(out, "From Roth") {
			t.Errorf("expected no From Roth row on surplus")
		}
	})
}

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

	t.Run("hides rows on steady-state surplus", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-budget-steady-state", map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
			"Analysis": &models.WhatIfAnalysis{
				BudgetFit: &models.BudgetFitAnalysis{
					HasSteadyState:      true,
					SteadyStateYear:     15,
					SteadyStateExpenses: 3000,
					SteadyStateIncome:   5000,
					SteadyStateGap:      -2000, // surplus
				},
			},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if strings.Contains(out, "Suggested Withdrawal Mix") {
			t.Errorf("expected no withdrawal-mix heading on steady-state surplus; got: %s", truncate(out, 500))
		}
		if strings.Contains(out, "From Tax-Deferred") {
			t.Errorf("expected no From Tax-Deferred row on surplus")
		}
		if strings.Contains(out, "From Taxable") {
			t.Errorf("expected no From Taxable row on surplus")
		}
		if strings.Contains(out, "From Roth") {
			t.Errorf("expected no From Roth row on surplus")
		}
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
