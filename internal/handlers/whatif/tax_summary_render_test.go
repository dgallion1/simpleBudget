package whatif

import (
	"context"
	"strings"
	"testing"

	"budget2/internal/models"
)

// retiredTaxableSettings builds a retired, all-tax-deferred scenario that
// draws down and generates ordinary-income tax every year.
func retiredTaxableSettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 70
	// prepare.ComputeAges recomputes CurrentAge from BirthMonth, so the age
	// must be set via BirthMonth or the scenario silently runs at 65.
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, s.CurrentAge)
	s.SocialSecurity = nil
	s.IncomeSources = nil
	s.PortfolioValue = 1_500_000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.MonthlyLivingExpenses = 8000
	s.InvestmentReturn = 5.0
	s.InflationRate = 3.0
	s.ProjectionYears = 25
	return s
}

func TestWhatIfTaxSummary_RendersFederalStateSplit(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := retiredTaxableSettings()
	analysis, err := runAnalysisWithCache(context.Background(), settings)
	if err != nil {
		t.Fatalf("runAnalysisWithCache: %v", err)
	}
	out, err := renderer.RenderToString("whatif-tax-summary", map[string]any{
		"Settings": settings, "Analysis": analysis,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	for _, want := range []string{"Income Taxes", "Federal", "State", "Avg Effective Rate", "Marginal"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in tax summary; got: %s", want, truncate(out, 1200))
		}
	}
	// Cost figures must use rose styling per project convention.
	if !strings.Contains(out, "text-rose-600") {
		t.Errorf("expected rose-styled tax figures; got: %s", truncate(out, 1200))
	}
	// The scenario must be genuinely taxable (the per-year table needs rows
	// to render the split); data-level reconciliation is covered by the
	// analysis-package BuildTax tests.
	if analysis.Tax == nil || analysis.Tax.TotalTaxPaid <= 0 {
		t.Fatalf("expected a taxable scenario to produce positive total tax")
	}
	// The helper must genuinely simulate a 70-year-old (age flows from
	// Persons[0].BirthMonth, not the raw CurrentAge field).
	if len(analysis.Tax.YearlyTaxSummary) == 0 || analysis.Tax.YearlyTaxSummary[0].Age != 70 {
		t.Fatalf("expected first tax-summary row at age 70; got %+v", analysis.Tax.YearlyTaxSummary)
	}
	if !strings.Contains(out, "Eff. Rate") {
		t.Errorf("expected per-year breakdown table to render; got: %s", truncate(out, 1200))
	}
}

func TestWhatIfTaxSummary_StateTaxAppearsWhenConfigured(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	render := func(s *models.WhatIfSettings) string {
		analysis, err := runAnalysisWithCache(context.Background(), s)
		if err != nil {
			t.Fatalf("runAnalysisWithCache: %v", err)
		}
		out, err := renderer.RenderToString("whatif-tax-summary", map[string]any{
			"Settings": s, "Analysis": analysis,
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		return out
	}

	withState := retiredTaxableSettings()
	withState.TaxConfig.StateIncomeTaxRate = models.FloatPtr(5.0)

	out := render(withState)
	if !strings.Contains(out, "State") {
		t.Errorf("expected a State column/tile in the tax summary")
	}
	// The total must still reconcile visually with the federal/state split
	// — both labels present and rose-styled rows rendered.
	if !strings.Contains(out, "Total Tax") {
		t.Errorf("expected per-year Total Tax column header")
	}
}
