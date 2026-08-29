package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

// TestSpendingPhasesRender_PhaseDollarLabel guards the phase-dollar preview
// in the "whatif-spending-phases" card: base 7386 x 1.25 = 9232.50, which
// must round half-away-from-zero to whole dollars with thousands separators
// — "$9,233/mo" — matching the JS-side formatWholeDollars rule.
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
	if !strings.Contains(out, "$9,233/mo") {
		t.Errorf("expected phase-dollar label %q in output; got: %s", "$9,233/mo", truncate(out, 900))
	}
}
