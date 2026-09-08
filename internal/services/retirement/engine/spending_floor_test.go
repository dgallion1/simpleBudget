package engine

import (
	"budget2/internal/models"
	"testing"
)

func TestFloorAdjustmentAndFunding(t *testing.T) {
	s := &models.WhatIfSettings{Guardrails: &models.GuardrailConfig{Enabled: true, MinMonthlySpendingReal: 7500}}
	for _, base := range []float64{0, 3000, 10000} {
		adjusted, mult := floorAdjustedLiving(s, base, 0.5, 1.2)
		if adjusted != 9000 {
			t.Fatalf("base %g adjusted %g", base, adjusted)
		}
		if base > 0 && base*mult != adjusted {
			t.Fatal("effective multiplier lost clamp")
		}
	}
	for _, tc := range []struct{ gap, want float64 }{{-100, 9000}, {0, 9000}, {1000, 8000}, {10000, 0}} {
		if got := FundedLiving(9000, tc.gap); got != tc.want {
			t.Fatalf("gap %g funded %g want %g", tc.gap, got, tc.want)
		}
	}
	for _, tc := range []struct{ value, want float64 }{{1.125, 1.13}, {-1.125, -1.13}, {749.994, 749.99}, {749.996, 750}} {
		if got := RoundLivingCents(tc.value); got != tc.want {
			t.Fatalf("round %g got %g", tc.value, got)
		}
	}
}
