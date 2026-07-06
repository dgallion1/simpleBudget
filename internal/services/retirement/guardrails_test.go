package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"math"
	"testing"
)

const floatTolerance = 1e-9

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < floatTolerance
}

func TestGuardrailState_FloorTrigger(t *testing.T) {
	t.Run("25% drop triggers floor cut", func(t *testing.T) {
		g := engine.NewGuardrailState(1_000_000)
		cfg := &models.GuardrailConfig{
			Enabled:         true,
			FloorDropPct:    20,
			FloorCutPct:     10,
			CeilingRisePct:  50, // high enough to not trigger
			CeilingRaisePct: 10,
			MinSpendingPct:  50,
			MaxSpendingPct:  150,
		}

		mult := g.Evaluate(cfg, 750_000) // 25% drop from 1M peak
		if !almostEqual(mult, 0.9) {
			t.Errorf("expected multiplier 0.9, got %f", mult)
		}
	})
}

func TestGuardrailState_CeilingTrigger(t *testing.T) {
	t.Run("30% rise triggers ceiling raise", func(t *testing.T) {
		g := engine.NewGuardrailState(1_000_000)
		cfg := &models.GuardrailConfig{
			Enabled:         true,
			FloorDropPct:    50, // high enough to not trigger
			FloorCutPct:     10,
			CeilingRisePct:  20,
			CeilingRaisePct: 10,
			MinSpendingPct:  50,
			MaxSpendingPct:  150,
		}

		mult := g.Evaluate(cfg, 1_300_000) // 30% rise from 1M initial
		if !almostEqual(mult, 1.1) {
			t.Errorf("expected multiplier 1.1, got %f", mult)
		}
	})
}

func TestGuardrailState_NoTrigger(t *testing.T) {
	t.Run("10% drop does not trigger 20% floor", func(t *testing.T) {
		g := engine.NewGuardrailState(1_000_000)
		cfg := &models.GuardrailConfig{
			Enabled:         true,
			FloorDropPct:    20,
			FloorCutPct:     10,
			CeilingRisePct:  50,
			CeilingRaisePct: 10,
			MinSpendingPct:  50,
			MaxSpendingPct:  150,
		}

		mult := g.Evaluate(cfg, 900_000) // 10% drop, below 20% threshold
		if !almostEqual(mult, 1.0) {
			t.Errorf("expected multiplier 1.0, got %f", mult)
		}
	})
}

func TestGuardrailState_MinMaxCap(t *testing.T) {
	t.Run("floor clamped to MinSpendingPct", func(t *testing.T) {
		g := engine.NewGuardrailState(1_000_000)
		cfg := &models.GuardrailConfig{
			Enabled:         true,
			FloorDropPct:    10,
			FloorCutPct:     40, // aggressive cuts
			CeilingRisePct:  90,
			CeilingRaisePct: 10,
			MinSpendingPct:  80, // clamp at 0.80
			MaxSpendingPct:  150,
		}

		// First trigger: 1M -> 800K (20% drop), mult = 1.0 * 0.6 = 0.6 -> clamped to 0.8
		g.Evaluate(cfg, 800_000)
		if !almostEqual(g.Multiplier(), 0.8) {
			t.Errorf("expected multiplier clamped to 0.8, got %f", g.Multiplier())
		}
	})

	t.Run("ceiling clamped to MaxSpendingPct", func(t *testing.T) {
		g := engine.NewGuardrailState(1_000_000)
		cfg := &models.GuardrailConfig{
			Enabled:         true,
			FloorDropPct:    90,
			FloorCutPct:     10,
			CeilingRisePct:  10,
			CeilingRaisePct: 40, // aggressive raises
			MinSpendingPct:  50,
			MaxSpendingPct:  120, // clamp at 1.20
		}

		// First trigger: 1M -> 1.2M (20% rise), mult = 1.0 * 1.4 = 1.4 -> clamped to 1.2
		g.Evaluate(cfg, 1_200_000)
		if !almostEqual(g.Multiplier(), 1.2) {
			t.Errorf("expected multiplier clamped to 1.2, got %f", g.Multiplier())
		}
	})
}

func TestGuardrailState_PeakReset(t *testing.T) {
	t.Run("floor triggers based on new peak not initial", func(t *testing.T) {
		g := engine.NewGuardrailState(1_000_000)
		cfg := &models.GuardrailConfig{
			Enabled:         true,
			FloorDropPct:    20,
			FloorCutPct:     10,
			CeilingRisePct:  90, // high enough to not trigger
			CeilingRaisePct: 10,
			MinSpendingPct:  50,
			MaxSpendingPct:  150,
		}

		// Evaluate at 1.5M: sets new peak, no floor trigger.
		mult := g.Evaluate(cfg, 1_500_000)
		if !almostEqual(mult, 1.0) {
			t.Errorf("expected multiplier 1.0 after new peak, got %f", mult)
		}

		// Evaluate at 1.2M: 20% drop from 1.5M peak -> triggers floor.
		mult = g.Evaluate(cfg, 1_200_000)
		if !almostEqual(mult, 0.9) {
			t.Errorf("expected multiplier 0.9 after floor trigger from new peak, got %f", mult)
		}
	})
}

func TestGuardrailState_FloorTakesPriorityOverCeiling(t *testing.T) {
	t.Run("floor wins when both could trigger", func(t *testing.T) {
		// Start at 1M, push peak to 2M, then drop to 1.5M.
		// Floor: (2M - 1.5M) / 2M * 100 = 25% >= 20% -> triggers
		// Ceiling: (1.5M - 1M) / 1M * 100 = 50% >= 20% -> would trigger
		// Floor should win.
		g := engine.NewGuardrailState(1_000_000)
		g.PeakPortfolio = 2_000_000 // simulate a peak at 2M

		cfg := &models.GuardrailConfig{
			Enabled:         true,
			FloorDropPct:    20,
			FloorCutPct:     10,
			CeilingRisePct:  20,
			CeilingRaisePct: 10,
			MinSpendingPct:  50,
			MaxSpendingPct:  150,
		}

		mult := g.Evaluate(cfg, 1_500_000)
		// Floor triggers: mult = 1.0 * 0.9 = 0.9
		// Ceiling should NOT trigger (would have been 1.0 * 1.1 = 1.1)
		if !almostEqual(mult, 0.9) {
			t.Errorf("expected floor to take priority with multiplier 0.9, got %f", mult)
		}
	})
}
