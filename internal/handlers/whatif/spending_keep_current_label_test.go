package whatif

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"budget2/internal/models"
)

// KC1 (2026-09-16): the Current plan card's submit button reads "Keep current
// plan"; applying it re-saves the plan as-is, so the recommendation cards'
// "Apply this option" label misled a user into expecting a change. The
// recommendation cards and the frontier rows keep their labels.
func TestSpendingOptimizerRenderKeepCurrentPlanLabel(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	rules := &models.GuardrailConfig{Enabled: true, MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: 7000}
	metrics := func(v float64) *models.SpendingRiskMetrics {
		return &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: v}
	}
	current := models.SpendingCandidate{ID: "current", Kind: "current", Baseline: true, Qualifies: true, BaseMonthlyLivingExpenses: 8000, StartingMonthlyLivingReal: 8500, Metrics: metrics(8000)}
	planned8500 := models.SpendingCandidate{ID: "planned-8500", Kind: "planned", Qualifies: true, BaseMonthlyLivingExpenses: 8500, StartingMonthlyLivingReal: 9000, Guardrails: rules, Metrics: metrics(8500)}
	flex9000 := models.SpendingCandidate{ID: "flex-9000", Kind: "flexible", Qualifies: true, BaseMonthlyLivingExpenses: 9000, StartingMonthlyLivingReal: 9500, Guardrails: rules, Metrics: metrics(9000)}
	result := &models.SpendingOptimizerResult{
		Request:           models.SpendingOptimizerRequest{FloorMonthlyReal: 7000, NearTermYears: 5},
		SelectionRuns:     1000,
		ValidationRuns:    1000,
		Candidates:        []models.SpendingCandidate{current, planned8500, flex9000},
		RecommendationIDs: []string{"planned-8500", "flex-9000"},
	}
	view, _, _, err := buildSpendingOptimizerView("keep-current-fixture", result)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-spending-optimizer-results", view); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	buttons := regexp.MustCompile(`(?s)<button[^>]*data-spending-apply="([^"]+)"[^>]*>(.*?)</button>`).FindAllStringSubmatch(body, -1)
	// 1 current card + 2 headline cards + 2 frontier rows.
	if len(buttons) != 5 {
		t.Fatalf("submit buttons = %d, want 5", len(buttons))
	}
	tags := regexp.MustCompile(`<[^>]+>`)
	labels := map[string][]string{}
	for _, m := range buttons {
		labels[m[1]] = append(labels[m[1]], strings.TrimSpace(tags.ReplaceAllString(m[2], "")))
	}
	if got := labels["current"]; len(got) != 1 || got[0] != "Keep current plan" {
		t.Errorf("current card button = %q, want [\"Keep current plan\"]", got)
	}
	// The current card is never a frontier row, so no "Apply" row for it.
	for _, id := range []string{"planned-8500", "flex-9000"} {
		got := labels[id]
		if len(got) != 2 || got[0] != "Apply this option" || got[1] != "Apply" {
			t.Errorf("%s buttons = %q, want card \"Apply this option\" then row \"Apply\"", id, got)
		}
	}
	// The card label is a visible text node, not an aria-label override
	// (WCAG 2.5.3 Label in Name): the form is otherwise unchanged.
	if !strings.Contains(body, `data-spending-apply="current" class=`) || strings.Contains(body, `aria-label="Keep current plan"`) {
		t.Errorf("current card button must carry its label as visible text")
	}
	if strings.Count(body, "Keep current plan") != 1 {
		t.Errorf("\"Keep current plan\" occurrences = %d, want exactly 1", strings.Count(body, "Keep current plan"))
	}
}
