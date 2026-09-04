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
	// Cost figures must use the negative token (U6; was rose styling).
	if !strings.Contains(out, "text-negative") {
		t.Errorf("expected negative-token tax figures; got: %s", truncate(out, 1200))
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

// fixtureTaxAnalysis builds a hand-crafted (non-engine) TaxAnalysis/RMDAnalysis
// pair so tests can assert on exact dollar-and-cents figures, independent of
// whatever numbers the projection pipeline happens to produce.
func fixtureTaxAnalysis(rmdStartAge int) *models.WhatIfAnalysis {
	return &models.WhatIfAnalysis{
		Tax: &models.TaxAnalysis{
			TotalTaxPaid:         1624993.75,
			TotalFederalTaxPaid:  1400000.88,
			TotalStateTaxPaid:    224992.87,
			AverageEffectiveRate: 18.4,
			YearlyTaxSummary: []models.YearlyTaxSummary{
				{
					Year: 2025, Age: 72,
					TaxableIncome: 90000.50, FederalTax: 8575.91, StateTax: 1200.25,
					TotalTax: 9776.16, EffectiveRate: 10.9, MarginalRate: 22,
					MarginalRateLongTermGain: 15,
				},
				{
					Year: 2026, Age: 73,
					TaxableIncome: 120000.33, FederalTax: 15200.44, StateTax: 1800.10,
					TotalTax: 17000.54, EffectiveRate: 14.2, MarginalRate: 24,
					MarginalRateLongTermGain: 18.8,
				},
			},
		},
		RMD: &models.RMDAnalysis{StartAge: rmdStartAge},
	}
}

// renderTaxSummary renders the tax-summary partial against fixtureTaxAnalysis
// with its default RMD-start age (73, matching the fixture's age-73 row).
func renderTaxSummary(t *testing.T) string {
	t.Helper()
	return renderTaxSummaryWithRMDStartAge(t, 73)
}

// renderTaxSummaryWithRMDStartAge renders the tax-summary partial with the
// fixture's RMD.StartAge overridden, so tests can move the badge on/off the
// age-73 row.
func renderTaxSummaryWithRMDStartAge(t *testing.T, rmdStartAge int) string {
	t.Helper()
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	analysis := fixtureTaxAnalysis(rmdStartAge)
	out, err := renderer.RenderToString("whatif-tax-summary", map[string]any{
		"Settings": models.DefaultWhatIfSettings(), "Analysis": analysis,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return out
}

func TestWhatIfTaxSummary_WholeDollarsAndRMDBadge(t *testing.T) {
	t.Run("tiles and table are whole dollars with no minus sign", func(t *testing.T) {
		out := renderTaxSummary(t)
		if strings.Contains(out, "-$") {
			t.Errorf("tax figures must not render a minus sign (red/rose styling already encodes cost): %s", truncate(out, 900))
		}
		if strings.Contains(out, ".75") || strings.Contains(out, ".88") || strings.Contains(out, ".91") || strings.Contains(out, ".16") {
			t.Errorf("tax figures must be whole dollars: %s", truncate(out, 900))
		}
		if !strings.Contains(out, "$1,624,994") {
			t.Errorf("expected TotalTaxPaid rounded to whole dollars ($1,624,994); got: %s", truncate(out, 900))
		}
	})

	t.Run("RMD start year row is badged", func(t *testing.T) {
		out := renderTaxSummaryWithRMDStartAge(t, 73)
		if !strings.Contains(out, "RMDs begin") {
			t.Errorf("expected the age-73 row to carry the RMDs-begin badge: %s", truncate(out, 1200))
		}
		if !strings.Contains(out, "bg-warning-soft") {
			t.Errorf("expected the RMD-start row to carry the warning-token highlight (U6; was amber): %s", truncate(out, 1200))
		}
	})

	t.Run("no RMD badge when start age doesn't match any row", func(t *testing.T) {
		out := renderTaxSummaryWithRMDStartAge(t, 80)
		if strings.Contains(out, "RMDs begin") {
			t.Errorf("did not expect an RMDs-begin badge when StartAge matches no row: %s", truncate(out, 1200))
		}
	})
}

// TestWhatIfTaxSummary_RendersMarginalRateOnGains covers the "On Gains" column:
// the marginal cost of realizing one more dollar of long-term gain, which is
// not the same as the ordinary-income marginal rate beside it.
func TestWhatIfTaxSummary_RendersMarginalRateOnGains(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	html := renderTaxSummary(t)

	if !strings.Contains(html, "On Gains") {
		t.Error("expected an On Gains column header")
	}
	// Fixture rows carry 15% and 18.8%, rendered to whole percent.
	for _, want := range []string{">15%<", ">19%<"} {
		if !strings.Contains(strings.Join(strings.Fields(html), " "), want) {
			t.Errorf("expected the gains column to render %s", want)
		}
	}
	// The stale footnote from before the marginal rate became a derivative.
	if strings.Contains(html, "top federal bracket reached") {
		t.Error("footnote still describes the old bracket-lookup behaviour")
	}
}

// TestWhatIfTaxSummary_RendersNearestCliff covers the cliff panel: the
// "you are $N under the cliff" sentence, which is only useful if the number
// and the year are both right.
func TestWhatIfTaxSummary_RendersNearestCliff(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	analysis := fixtureTaxAnalysis(73)
	analysis.Tax.NearestCliff = &models.CliffProximity{
		Year: 2031, Age: 78, Label: "IRMAA tier 1",
		Headroom: 10319, AnnualCost: 2296.80,
	}

	out, err := renderer.RenderToString("whatif-tax-summary", map[string]any{
		"Settings": retiredTaxableSettings(), "Analysis": analysis,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	html := strings.Join(strings.Fields(out), " ")

	for _, want := range []string{"2031", "age 78", "IRMAA tier 1", "$10,319", "$2,297"} {
		if !strings.Contains(html, want) {
			t.Errorf("cliff panel missing %q", want)
		}
	}
	if !strings.Contains(html, "step, not a ramp") {
		t.Error("cliff panel should explain that one dollar over costs the whole amount")
	}
}

// TestWhatIfTaxSummary_OmitsCliffPanelWhenNone — no cliff applies to plenty of
// households, and an empty warning box is worse than none.
func TestWhatIfTaxSummary_OmitsCliffPanelWhenNone(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	analysis := fixtureTaxAnalysis(73)
	analysis.Tax.NearestCliff = nil

	out, err := renderer.RenderToString("whatif-tax-summary", map[string]any{
		"Settings": retiredTaxableSettings(), "Analysis": analysis,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if strings.Contains(out, "Closest approach to a cost step") {
		t.Error("rendered the cliff panel with no cliff to report")
	}
}

// TestWhatIfTaxSummary_RendersConstantsProvenance — the tax panel must say
// which published figures it rests on and which years are extrapolated.
func TestWhatIfTaxSummary_RendersConstantsProvenance(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	analysis := fixtureTaxAnalysis(73)
	analysis.Tax.ConstantsBasis = &models.TaxConstantsBasis{
		StatutoryYear:      2024,
		Source:             "IRS Rev. Proc. 2023-34 (tax year 2024)",
		VerifiedOn:         "2024-11-09",
		FirstProjectedYear: 2025,
		LastProjectedYear:  2055,
		InflationRate:      3.0,
	}

	out, err := renderer.RenderToString("whatif-tax-summary", map[string]any{
		"Settings": retiredTaxableSettings(), "Analysis": analysis,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	html := strings.Join(strings.Fields(out), " ")

	for _, want := range []string{
		"IRS Rev. Proc. 2023-34",
		"last verified 2024-11-09",
		"2025",
		"2055",
		"3.0%/yr",
		"estimates, not published law",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("provenance line missing %q", want)
		}
	}
}

// TestWhatIfTaxSummary_OmitsProjectedNoteWhenFullyStatutory — a projection
// entirely covered by published figures should not carry a forecast warning.
func TestWhatIfTaxSummary_OmitsProjectedNoteWhenFullyStatutory(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	analysis := fixtureTaxAnalysis(73)
	analysis.Tax.ConstantsBasis = &models.TaxConstantsBasis{
		StatutoryYear: 2024,
		Source:        "IRS Rev. Proc. 2023-34 (tax year 2024)",
		VerifiedOn:    "2024-11-09",
	}

	out, err := renderer.RenderToString("whatif-tax-summary", map[string]any{
		"Settings": retiredTaxableSettings(), "Analysis": analysis,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if strings.Contains(out, "estimates, not published law") {
		t.Error("warned about projected figures when none are projected")
	}
	if !strings.Contains(out, "IRS Rev. Proc. 2023-34") {
		t.Error("source should still be shown when everything is statutory")
	}
}
