package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

// TestSpendingPhasesRender_PhaseDollarLabel guards the phase-dollar preview
// in the "whatif-spending-phases" card: base 7386 x 1.25 = 9232.50, an exact
// .50 tie, which must round HALF-EVEN to whole dollars with thousands
// separators — "$9,232" (9232 is even) — matching Go formatNumber and the
// JS-side formatWholeDollars rule (WS1 R-FMT: one whole-dollar rounding
// rule for every surface).
func TestSpendingPhasesRender_PhaseDollarLabel(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 7386
	s.PhaseAgeReference = "older"
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  []models.SpendingPhase{{Name: "Go-Go", StartAge: 0, Multiplier: 1.25}},
	}

	out, err := renderer.RenderToString("whatif-spending-phases", map[string]any{
		"Settings": s,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "$9,232/mo") {
		t.Errorf("expected phase-dollar label %q in output; got: %s", "$9,232/mo", truncate(out, 900))
	}
	if strings.Contains(out, "$9,233/mo") {
		t.Errorf(".50 tie must round half-even, not half-away-from-zero; got: %s", truncate(out, 900))
	}
}

// TestSpendingPhasesRender_HealthcareCareCarveOut guards the intro blurb's
// disclosure that healthcare and late-life care are modeled separately from
// the spending-phase multipliers (CC2 spec §2c.3) — without it, a reader
// could believe the phase multiplier reduces the care/healthcare line too.
func TestSpendingPhasesRender_HealthcareCareCarveOut(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()

	out, err := renderer.RenderToString("whatif-spending-phases", map[string]any{
		"Settings": s,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	want := "Healthcare and late-life care are modeled separately and are not reduced by these multipliers."
	if !strings.Contains(out, want) {
		t.Errorf("expected blurb %q in output; got: %s", want, truncate(out, 900))
	}
}
