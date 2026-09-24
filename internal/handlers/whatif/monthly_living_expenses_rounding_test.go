package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

// TestMonthlyLivingExpensesWholeDollarRounding_HalfEvenTie guards the WS1
// R-FMT fix to a pre-existing (master, 643fa54) split: living expenses'
// slider/aria/Quick-Adjust surfaces used a half-away-from-zero rule while
// every OTHER whole-dollar surface (Coverage Timeline, healthcare sliders,
// etc.) used Go formatNumber's %.0f half-EVEN rule, so a .50 tie could show
// two different figures on the same screen depending which surface you
// looked at. R-FMT converges every such surface onto formatNumber: this
// must hold at the .50 tie where the two rules disagree — 7386.50 must
// render "$7,386" (half-even; 7386 is even) everywhere, never "$7,387"
// (the old half-away-from-zero figure).
func TestMonthlyLivingExpensesWholeDollarRounding_HalfEvenTie(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 7386.50
	s.PhaseAgeReference = "older"
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  []models.SpendingPhase{{Name: "Go-Go", StartAge: 0, Multiplier: 1.1}},
	}

	t.Run("portfolio-settings: aria-valuetext and display span", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-portfolio-settings", map[string]any{
			"Settings":                s,
			"LivingExpensesPhaseNote": buildLivingExpensesPhaseNote(s),
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, `aria-valuetext="$7,386"`) {
			t.Errorf("expected aria-valuetext=%q on the visible range input; got: %s", "$7,386", truncate(out, 1200))
		}
		if strings.Contains(out, `aria-valuetext="$7,387"`) {
			t.Errorf(".50 tie must round half-even (formatNumber), not half-away-from-zero; got: %s", truncate(out, 1200))
		}
		if !strings.Contains(out, `id="monthly_living_expenses_display"`) {
			t.Fatalf("display span not found: %s", truncate(out, 1200))
		}
		idx := strings.Index(out, `id="monthly_living_expenses_display"`)
		if !strings.Contains(out[idx:idx+200], "$7,386") {
			t.Errorf("expected display span to read $7,386; got: %s", truncate(out[idx:idx+200], 200))
		}
	})

	t.Run("quick-adjust mirror: aria-valuetext and display span", func(t *testing.T) {
		out, err := renderer.RenderToString("whatif-quick-adjust-portfolio-content", map[string]any{
			"Settings": s,
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, `aria-valuetext="$7,386"`) {
			t.Errorf("expected mirror aria-valuetext=%q; got: %s", "$7,386", truncate(out, 1200))
		}
		idx := strings.Index(out, `data-quick-adjust-display="monthly_living_expenses"`)
		if idx < 0 {
			t.Fatalf("mirror display span not found: %s", truncate(out, 1200))
		}
		if !strings.Contains(out[idx:idx+200], "$7,386") {
			t.Errorf("expected mirror display span to read $7,386; got: %s", truncate(out[idx:idx+200], 200))
		}
	})

	t.Run("phase-dollar preview label rounds the tie the same way", func(t *testing.T) {
		// 7386.50 x 1.1 = 8125.15 -- not itself a tie, but exercises the same
		// formatNumber path the .50 base above renders through, at both the
		// quick-adjust mirror tab and the primary spending-phases card.
		out, err := renderer.RenderToString("whatif-quick-adjust-phases-content", map[string]any{
			"Settings": s,
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, "$8,125/mo") {
			t.Errorf("expected phase-dollar label $8,125/mo; got: %s", truncate(out, 1200))
		}
	})
}
