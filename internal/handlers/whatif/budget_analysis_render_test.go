package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func budgetFitFixture() *models.BudgetFitAnalysis {
	return &models.BudgetFitAnalysis{
		MonthlyExpenses: 10686, MonthlyIncome: 5300,
		MonthlyGap: 6435.53, RequiredRate: 3.1,
		MonthlyTaxes: 1049.53, MonthlyStateTax: 342.92,
		HasSteadyState: true, SteadyStateYear: 0,
		SteadyStateExpenses: 10686, SteadyStateIncome: 5300,
		SteadyStateGap: 6435.53, SteadyStateRate: 3.1,
		SteadyStateTaxes: 1049.53,
	}
}

func TestBudgetAnalysis_Render(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()

	t.Run("taxes sit under their own group header, not income", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": settings,
			"Analysis": &models.WhatIfAnalysis{BudgetFit: budgetFitFixture()},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "Taxes &amp; Deductions") {
			t.Errorf("expected a Taxes & Deductions group header; got: %s", truncate(out, 1200))
		}
		incomeIdx := strings.Index(out, ">Income<")
		taxHeaderIdx := strings.Index(out, "Taxes &amp; Deductions")
		taxRowIdx := strings.Index(out, "Estimated Taxes")
		if incomeIdx == -1 || taxHeaderIdx == -1 || taxRowIdx == -1 {
			t.Fatalf("missing section markers (income=%d taxHeader=%d taxRow=%d)", incomeIdx, taxHeaderIdx, taxRowIdx)
		}
		if !(incomeIdx < taxHeaderIdx && taxHeaderIdx < taxRowIdx) {
			t.Errorf("Estimated Taxes row must come after its own group header, after the income section")
		}
	})

	t.Run("year 0 suppresses the duplicate steady-state breakdown", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": settings,
			"Analysis": &models.WhatIfAnalysis{BudgetFit: budgetFitFixture()},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if got := strings.Count(out, "Monthly Gap"); got != 1 {
			t.Errorf("expected exactly one Monthly Gap tile at year 0, got %d", got)
		}
		if !strings.Contains(out, "Suggested Withdrawal Mix") {
			t.Errorf("withdrawal mix must survive the year-0 dedup")
		}
	})

	t.Run("year above 0 keeps both blocks", func(t *testing.T) {
		bf := budgetFitFixture()
		bf.SteadyStateYear = 12
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": settings,
			"Analysis": &models.WhatIfAnalysis{BudgetFit: bf},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if got := strings.Count(out, "Monthly Gap"); got != 2 {
			t.Errorf("expected today + year-12 gap tiles, got %d", got)
		}
	})

	t.Run("slider header names the age alongside the year", func(t *testing.T) {
		bf := budgetFitFixture()
		bf.SteadyStateYear = 12
		s := models.DefaultWhatIfSettings()
		s.CurrentAge = 67
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": s,
			"Analysis": &models.WhatIfAnalysis{BudgetFit: bf},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{`id="steady-state-age-display"`, ">79<", `data-base-age="67"`} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in slider header; got: %s", want, truncate(out, 1200))
			}
		}
	})

	t.Run("withdrawal-mix explainer is a collapsed disclosure", func(t *testing.T) {
		bf := budgetFitFixture()
		bf.SteadyStateYear = 12
		out, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
			"Analysis": &models.WhatIfAnalysis{BudgetFit: bf},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "How the withdrawal mix is calculated") {
			t.Errorf("expected disclosure summary; got: %s", truncate(out, 1200))
		}
		if !strings.Contains(out, "IRMAA keys off your MAGI") {
			t.Errorf("explainer body text must be preserved inside the disclosure")
		}
	})
}
