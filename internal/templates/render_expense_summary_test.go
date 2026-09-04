package templates

import (
	"io/fs"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/web"
)

// newExpenseSummaryTestRenderer builds a renderer over the embedded template
// FS, matching the pattern in render_budget_analysis_phase_subitems_test.go.
func newExpenseSummaryTestRenderer(t *testing.T) *Renderer {
	t.Helper()
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}
	return renderer
}

// expenseRowsFixture is the shared Living/Healthcare(+2 sub-rows)/Property
// Tax fixture used by both the extraction test and the Overview-card test,
// so a single fixture proves the two surfaces render the same rows.
func expenseRowsFixture() *models.BudgetFitAnalysis {
	return &models.BudgetFitAnalysis{
		MonthlyExpenses: 10714.15,
		MonthlyIncome:   5300,
		MonthlyGap:      5414.15,
		RequiredRate:    3.1,
		ExpenseBreakdown: []models.ExpenseBreakdownItem{
			{Name: "Living Expenses", Amount: 7792.85},
			{
				Name:   "Healthcare",
				Amount: 2255.30,
				SubItems: []models.ExpenseBreakdownItem{
					{Name: "Darrell (Medicare)", Amount: 600.00},
					{Name: "Christine (ACA)", Amount: 1655.30},
				},
			},
			{Name: "Property Tax", Amount: 666.00},
		},
	}
}

// (1) The Cash Flow panel's render must still contain the exact same bytes
// for the expenses section after the whatif-expense-rows extraction: the
// standalone partial's own output is asserted to be a literal substring of
// the full panel render for the same fixture/data (ruling 2026-08-29b class
// of check — assert on rendered strings, not on the diff of template
// source).
func TestExpenseRowsExtraction_PartialOutputIsSubstringOfPanel(t *testing.T) {
	renderer := newExpenseSummaryTestRenderer(t)

	data := map[string]any{
		"Settings": models.DefaultWhatIfSettings(),
		"Analysis": &models.WhatIfAnalysis{BudgetFit: expenseRowsFixture()},
	}

	panelOut, err := renderer.RenderToString("whatif-budget-analysis", data)
	if err != nil {
		t.Fatalf("RenderToString(whatif-budget-analysis): %v", err)
	}
	rowsOut, err := renderer.RenderToString("whatif-expense-rows", data)
	if err != nil {
		t.Fatalf("RenderToString(whatif-expense-rows): %v", err)
	}

	if rowsOut == "" {
		t.Fatal("expected whatif-expense-rows to render non-empty output")
	}
	if !strings.Contains(panelOut, rowsOut) {
		t.Errorf("expected panel render to contain the extracted rows partial's output verbatim.\nrows:\n%s\npanel:\n%s", rowsOut, panelOut)
	}

	// The individual row strings must all be present exactly once each, in
	// both the panel and the standalone partial, at the formatMoney
	// precision the card has always used (never formatDollars/whole
	// dollars, which would be a second rounding path for the same figure).
	for _, want := range []string{
		"Living Expenses",
		"$7,792.85/mo",
		"Healthcare",
		"$2,255.30/mo",
		"Darrell (Medicare)",
		"$600.00/mo",
		"Christine (ACA)",
		"$1,655.30/mo",
		"Property Tax",
		"$666.00/mo",
		"$10,714.15", // header total (formatMoney, no /mo suffix on the header row)
	} {
		if !strings.Contains(panelOut, want) {
			t.Errorf("expected %q in panel render", want)
		}
		if !strings.Contains(rowsOut, want) {
			t.Errorf("expected %q in extracted rows partial render", want)
		}
	}
}

// (2) The Overview "Monthly Expenses Today" card renders the same row
// strings as the Cash Flow panel for one fixture with Living / Healthcare
// (2 sub-rows) / Property Tax, and renders nothing for an empty breakdown.
func TestExpenseSummaryCard_MatchesPanelRows_AndHiddenWhenEmpty(t *testing.T) {
	renderer := newExpenseSummaryTestRenderer(t)

	fixture := expenseRowsFixture()
	data := map[string]any{
		"Settings": models.DefaultWhatIfSettings(),
		"Analysis": &models.WhatIfAnalysis{BudgetFit: fixture},
	}

	panelOut, err := renderer.RenderToString("whatif-budget-analysis", data)
	if err != nil {
		t.Fatalf("RenderToString(whatif-budget-analysis): %v", err)
	}
	summaryOut, err := renderer.RenderToString("whatif-expense-summary", data)
	if err != nil {
		t.Fatalf("RenderToString(whatif-expense-summary): %v", err)
	}

	if !strings.Contains(summaryOut, "Monthly Expenses Today") {
		t.Errorf("expected card heading in summary render: %s", summaryOut)
	}
	if !strings.Contains(summaryOut, `data-wf-goto="cashflow"`) {
		t.Errorf("expected data-wf-goto=\"cashflow\" link in summary render: %s", summaryOut)
	}
	if !strings.Contains(summaryOut, "Full cash flow →") {
		t.Errorf("expected verbatim link text 'Full cash flow →' in summary render: %s", summaryOut)
	}
	if strings.Contains(summaryOut, "data-wf-tab") {
		t.Errorf("the goto link must not carry data-wf-tab: %s", summaryOut)
	}

	for _, want := range []string{
		"Living Expenses",
		"$7,792.85/mo",
		"Healthcare",
		"$2,255.30/mo",
		"Darrell (Medicare)",
		"$600.00/mo",
		"Christine (ACA)",
		"$1,655.30/mo",
		"Property Tax",
		"$666.00/mo",
	} {
		if !strings.Contains(panelOut, want) {
			t.Errorf("fixture sanity: expected %q in panel render", want)
		}
		if !strings.Contains(summaryOut, want) {
			t.Errorf("expected %q in Overview summary card render, matching the panel", want)
		}
	}

	t.Run("hidden when breakdown empty", func(t *testing.T) {
		emptyData := map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
			"Analysis": &models.WhatIfAnalysis{BudgetFit: &models.BudgetFitAnalysis{
				MonthlyExpenses:  0,
				ExpenseBreakdown: nil,
			}},
		}
		out, err := renderer.RenderToString("whatif-expense-summary", emptyData)
		if err != nil {
			t.Fatalf("RenderToString(whatif-expense-summary) with empty breakdown: %v", err)
		}
		if strings.TrimSpace(out) != "" {
			t.Errorf("expected no output when ExpenseBreakdown is empty, got: %q", out)
		}
	})

	t.Run("hidden when Analysis absent", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-expense-summary", map[string]any{
			"Settings": models.DefaultWhatIfSettings(),
		})
		if err != nil {
			t.Fatalf("RenderToString(whatif-expense-summary) with no Analysis: %v", err)
		}
		if strings.TrimSpace(out) != "" {
			t.Errorf("expected no output when .Analysis is absent, got: %q", out)
		}
	})
}

// (3) The slider helper text contains the verbatim "Plan total today:
// $10,714/mo." sentence (via formatDollars, whole dollars, matching the
// slider's own display span) for MonthlyExpenses 10714.15, and omits that
// sentence entirely when .Analysis is absent — the same bare-map shape used
// by living_expenses_phase_note_test.go and
// monthly_living_expenses_rounding_test.go, which must keep passing.
func TestPortfolioSettingsSliderNote_PlanTotalAndGuard(t *testing.T) {
	renderer := newExpenseSummaryTestRenderer(t)

	wantSentence := "Plan total today: $10,714/mo."
	wantPrefix := "In today's dollars; inflation is applied during projection. Excludes healthcare premiums and property tax — those are entered below and added on top."

	t.Run("present with .Analysis.BudgetFit", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		out, err := renderer.RenderToString("whatif-portfolio-settings", map[string]any{
			"Settings": settings,
			"Analysis": &models.WhatIfAnalysis{BudgetFit: &models.BudgetFitAnalysis{
				MonthlyExpenses: 10714.15,
			}},
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, wantPrefix) {
			t.Errorf("expected verbatim prefix %q in output; got: %s", wantPrefix, out)
		}
		if !strings.Contains(out, wantSentence) {
			t.Errorf("expected %q in output; got: %s", wantSentence, out)
		}
	})

	t.Run("absent without .Analysis (bare map, matches existing phase-note/rounding tests' shape)", func(t *testing.T) {
		settings := models.DefaultWhatIfSettings()
		out, err := renderer.RenderToString("whatif-portfolio-settings", map[string]any{
			"Settings": settings,
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, wantPrefix) {
			t.Errorf("expected the unconditional prefix to still render; got: %s", out)
		}
		if strings.Contains(out, "Plan total today") {
			t.Errorf("expected 'Plan total today' sentence to be absent without .Analysis; got: %s", out)
		}
	})
}
