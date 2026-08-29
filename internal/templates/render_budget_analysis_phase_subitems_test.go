package templates

import (
	"io/fs"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/web"
)

// The Monthly Budget Analysis panel's Living Expenses row must disclose a
// spending-phase multiplier via indented sub-rows, and must not do so when
// the ExpenseBreakdown carries no SubItems (every other row's rendering
// must stay byte-identical to before this feature existed).
func TestRenderBudgetAnalysisShowsLivingExpensePhaseSubItems(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}

	fixtureWithSubItems := &models.BudgetFitAnalysis{
		MonthlyExpenses: 8124.60,
		MonthlyIncome:   5300,
		MonthlyGap:      2824.60,
		RequiredRate:    3.1,
		ExpenseBreakdown: []models.ExpenseBreakdownItem{
			{
				Name:   "Living Expenses",
				Amount: 8124.60,
				SubItems: []models.ExpenseBreakdownItem{
					{Name: "Base (slider setting)", Amount: 7386.00},
					{Name: "Go-Go phase ×1.1", Amount: 738.60, SignedAmount: true},
				},
			},
		},
	}

	html, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
		"Settings": models.DefaultWhatIfSettings(),
		"Analysis": &models.WhatIfAnalysis{BudgetFit: fixtureWithSubItems},
	})
	if err != nil {
		t.Fatalf("RenderToString() error: %v", err)
	}

	for _, want := range []string{"Base (slider setting)", "Go-Go phase ×1.1", "$7,386.00/mo", "+$738.60/mo", "$8,124.60/mo"} {
		if !strings.Contains(html, want) {
			t.Errorf("expected %q in rendered html, got: %s", want, html)
		}
	}

	fixtureWithoutSubItems := &models.BudgetFitAnalysis{
		MonthlyExpenses: 7386,
		MonthlyIncome:   5300,
		MonthlyGap:      2086,
		RequiredRate:    2.0,
		ExpenseBreakdown: []models.ExpenseBreakdownItem{
			{Name: "Living Expenses", Amount: 7386},
		},
	}

	htmlNoPhase, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
		"Settings": models.DefaultWhatIfSettings(),
		"Analysis": &models.WhatIfAnalysis{BudgetFit: fixtureWithoutSubItems},
	})
	if err != nil {
		t.Fatalf("RenderToString() error: %v", err)
	}
	for _, notWant := range []string{"Base (slider setting)", "Go-Go phase"} {
		if strings.Contains(htmlNoPhase, notWant) {
			t.Errorf("did not expect %q in rendered html when SubItems is absent, got: %s", notWant, htmlNoPhase)
		}
	}
}
