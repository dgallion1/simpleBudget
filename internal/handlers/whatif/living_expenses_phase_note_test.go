package whatif

import (
	"math"
	"strings"
	"testing"

	"budget2/internal/models"
)

// ── buildLivingExpensesPhaseNote (view model) ───────────────────────────────

func twoPhaseSettings(currentMultiplier, nextMultiplier float64) *models.WhatIfSettings {
	return &models.WhatIfSettings{
		CurrentAge:            65,
		PhaseAgeReference:     "primary",
		MonthlyLivingExpenses: 7386,
		InflationRate:         3.0,
		SpendingPhaseConfig: &models.SpendingPhaseConfig{
			Enabled: true,
			Phases: []models.SpendingPhase{
				{Name: "Go-Go", StartAge: 0, Multiplier: currentMultiplier},
				{Name: "Slow-Go", StartAge: 70, Multiplier: nextMultiplier},
			},
		},
	}
}

func TestBuildLivingExpensesPhaseNote_EnabledWithNextTransition(t *testing.T) {
	s := twoPhaseSettings(1.1, 0.9)

	note := buildLivingExpensesPhaseNote(s)

	if !note.Show {
		t.Fatal("expected Show = true for enabled phases with a non-1.0 current multiplier")
	}
	if note.PhaseName != "Go-Go" {
		t.Errorf("PhaseName = %q, want Go-Go", note.PhaseName)
	}
	if note.Multiplier != 1.1 {
		t.Errorf("Multiplier = %v, want 1.1", note.Multiplier)
	}
	if note.MultiplierText != "1.1" {
		t.Errorf("MultiplierText = %q, want 1.1", note.MultiplierText)
	}
	wantEngineMonth0 := 7386.0 * 1.1
	if math.Abs(note.EngineMonth0-wantEngineMonth0) > 0.005 {
		t.Errorf("EngineMonth0 = %v, want ~%v", note.EngineMonth0, wantEngineMonth0)
	}
	if note.NextClause != "×0.9 from age 70" {
		t.Errorf("NextClause = %q, want %q", note.NextClause, "×0.9 from age 70")
	}
}

func TestBuildLivingExpensesPhaseNote_DisabledPhases(t *testing.T) {
	s := twoPhaseSettings(1.1, 0.9)
	s.SpendingPhaseConfig.Enabled = false

	note := buildLivingExpensesPhaseNote(s)

	if note.Show {
		t.Fatalf("expected Show = false when phases are disabled, got %+v", note)
	}
}

func TestBuildLivingExpensesPhaseNote_NilConfig(t *testing.T) {
	s := twoPhaseSettings(1.1, 0.9)
	s.SpendingPhaseConfig = nil

	note := buildLivingExpensesPhaseNote(s)

	if note.Show {
		t.Fatalf("expected Show = false when SpendingPhaseConfig is nil, got %+v", note)
	}
}

func TestBuildLivingExpensesPhaseNote_MultiplierOne(t *testing.T) {
	// Current phase multiplier is exactly 1.0: nothing to explain yet, even
	// though a later phase changes it.
	s := twoPhaseSettings(1.0, 0.9)

	note := buildLivingExpensesPhaseNote(s)

	if note.Show {
		t.Fatalf("expected Show = false when the current multiplier is 1.0, got %+v", note)
	}
}

func TestBuildLivingExpensesPhaseNote_SinglePhaseOmitsNextClause(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:            65,
		PhaseAgeReference:     "primary",
		MonthlyLivingExpenses: 7386,
		SpendingPhaseConfig: &models.SpendingPhaseConfig{
			Enabled: true,
			Phases: []models.SpendingPhase{
				{Name: "Go-Go", StartAge: 0, Multiplier: 1.1},
			},
		},
	}

	note := buildLivingExpensesPhaseNote(s)

	if !note.Show {
		t.Fatal("expected Show = true")
	}
	if note.NextClause != "" {
		t.Errorf("NextClause = %q, want empty for a single-phase config", note.NextClause)
	}
}

func TestBuildLivingExpensesPhaseNote_NilSettings(t *testing.T) {
	note := buildLivingExpensesPhaseNote(nil)
	if note.Show {
		t.Fatalf("expected Show = false for nil settings, got %+v", note)
	}
}

func TestFormatPhaseMultiplier(t *testing.T) {
	cases := map[float64]string{
		1.1:  "1.1",
		0.9:  "0.9",
		0.95: "0.95",
		0.65: "0.65",
	}
	for in, want := range cases {
		if got := formatPhaseMultiplier(in); got != want {
			t.Errorf("formatPhaseMultiplier(%v) = %q, want %q", in, got, want)
		}
	}
}

// ── Template render: whatif-portfolio-settings ──────────────────────────────

func TestPortfolioSettingsRender_LivingExpensesPhaseNote(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	t.Run("shown with figures and next-transition clause", func(t *testing.T) {
		s := twoPhaseSettings(1.1, 0.9)
		out, err := renderer.RenderToString("whatif-portfolio-settings", map[string]any{
			"Settings":                s,
			"LivingExpensesPhaseNote": buildLivingExpensesPhaseNote(s),
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		for _, want := range []string{
			`id="living-expenses-phase-note"`,
			`data-phase-multiplier="1.1"`,
			"$8,124.60",
			"Go-Go ×1.1",
			"×0.9 from age 70",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in output; got: %s", want, truncate(out, 1200))
			}
		}
	})

	t.Run("hidden when phases disabled", func(t *testing.T) {
		s := twoPhaseSettings(1.1, 0.9)
		s.SpendingPhaseConfig.Enabled = false
		out, err := renderer.RenderToString("whatif-portfolio-settings", map[string]any{
			"Settings":                s,
			"LivingExpensesPhaseNote": buildLivingExpensesPhaseNote(s),
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if strings.Contains(out, `id="living-expenses-phase-note"`) {
			t.Errorf("expected phase note absent when phases disabled; got: %s", truncate(out, 1200))
		}
	})

	t.Run("hidden when multiplier is 1.0", func(t *testing.T) {
		s := twoPhaseSettings(1.0, 0.9)
		out, err := renderer.RenderToString("whatif-portfolio-settings", map[string]any{
			"Settings":                s,
			"LivingExpensesPhaseNote": buildLivingExpensesPhaseNote(s),
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if strings.Contains(out, `id="living-expenses-phase-note"`) {
			t.Errorf("expected phase note absent when multiplier is 1.0; got: %s", truncate(out, 1200))
		}
	})

	t.Run("hidden input carries the exact off-grid saved value; range has no name", func(t *testing.T) {
		s := twoPhaseSettings(1.1, 0.9)
		s.MonthlyLivingExpenses = 7386 // off the range's step=100 grid
		out, err := renderer.RenderToString("whatif-portfolio-settings", map[string]any{
			"Settings":                s,
			"LivingExpensesPhaseNote": buildLivingExpensesPhaseNote(s),
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, `id="monthly_living_expenses_value" name="monthly_living_expenses"`) {
			t.Errorf("expected hidden input to carry the submission name; got: %s", truncate(out, 1200))
		}
		if !strings.Contains(out, `value="7386.00"`) {
			t.Errorf("expected hidden input value to be the exact saved amount 7386.00; got: %s", truncate(out, 1200))
		}
		if strings.Contains(out, `id="monthly_living_expenses_input" name=`) {
			t.Errorf("expected the visible range input to have no name attribute (excluded from submission); got: %s", truncate(out, 1200))
		}
	})

	t.Run("aria-valuetext carries the exact saved value, not the step-snapped one", func(t *testing.T) {
		s := twoPhaseSettings(1.1, 0.9)
		s.MonthlyLivingExpenses = 7386 // off the range's step=100 grid; browser would snap the range value to 7400
		out, err := renderer.RenderToString("whatif-portfolio-settings", map[string]any{
			"Settings":                s,
			"LivingExpensesPhaseNote": buildLivingExpensesPhaseNote(s),
		})
		if err != nil {
			t.Fatalf("RenderToString: %v", err)
		}
		if !strings.Contains(out, `aria-valuetext="$7,386"`) {
			t.Errorf("expected aria-valuetext=%q on the visible range input; got: %s", `$7,386`, truncate(out, 1200))
		}
		if strings.Contains(out, `aria-valuetext="$7,400"`) {
			t.Errorf("aria-valuetext must never carry the browser-snapped grid value; got: %s", truncate(out, 1200))
		}
	})
}
