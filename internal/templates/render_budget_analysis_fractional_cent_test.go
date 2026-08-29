package templates

import (
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
	"budget2/web"
)

// TestRenderBudgetAnalysis_LivingExpenseSubItems_RenderedSumHoldsOnFractionalCentBase
// guards ruling 2026-08-29b: a "sum must hold" display requirement means the
// RENDERED strings, not the underlying floats. Independent %.2f formatting
// of base/adjustment/total previously broke the displayed identity for a
// fractional-cent base (e.g. 7386.555, the kind of value Sync-from-Dashboard
// saves as totalExpenses/months). This exercises the real
// analysis.BudgetFit computation (not a hand-built fixture) so it actually
// covers the budget_fit.go rounding fix, then extracts the three dollar
// figures from the rendered HTML and asserts they add up exactly as
// displayed.
func TestRenderBudgetAnalysis_LivingExpenseSubItems_RenderedSumHoldsOnFractionalCentBase(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 7386.555
	s.PhaseAgeReference = "older"
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Go-Go", StartAge: 0, Multiplier: 1.1},
		},
	}

	in := engine.Input{Prepared: prepare.MustFrom(t, s)}
	result := analysis.BudgetFit(in, nil)

	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}

	html, err := renderer.RenderToString("whatif-budget-analysis", map[string]any{
		"Settings": s,
		"Analysis": &models.WhatIfAnalysis{BudgetFit: result},
	})
	if err != nil {
		t.Fatalf("RenderToString() error: %v", err)
	}

	total := extractDollarAfter(t, html, "Living Expenses")
	base := extractDollarAfter(t, html, "Base (slider setting)")
	adjustment := extractDollarAfter(t, html, "Go-Go phase")

	if got, want := base+adjustment, total; got != want {
		t.Errorf("rendered base (%.2f) + rendered adjustment (%.2f) = %.2f, want rendered total %.2f (base+adjustment must equal total to the cent as DISPLAYED)", base, adjustment, got, want)
	}
}

var fractionalCentDollarRe = regexp.MustCompile(`([+-]?)\$([\d,]+\.\d{2})`)

// extractDollarAfter finds label in html, then parses the next "$X,XXX.XX"
// (optionally signed) dollar figure that follows it.
func extractDollarAfter(t *testing.T, html, label string) float64 {
	t.Helper()
	idx := strings.Index(html, label)
	if idx < 0 {
		t.Fatalf("label %q not found in rendered html: %s", label, html)
	}
	match := fractionalCentDollarRe.FindStringSubmatch(html[idx:])
	if match == nil {
		t.Fatalf("no dollar figure found after label %q in: %s", label, html[idx:idx+200])
	}
	numStr := strings.ReplaceAll(match[2], ",", "")
	v, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		t.Fatalf("ParseFloat(%q): %v", numStr, err)
	}
	if match[1] == "-" {
		v = -v
	}
	return v
}
