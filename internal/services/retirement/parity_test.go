//go:build !short

package retirement

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

type parityFixture struct {
	Name     string
	Settings *models.WhatIfSettings
	MCSeed   int64
}

func parityFixtures(t testing.TB) []parityFixture {
	t.Helper()
	return []parityFixture{
		{Name: "baseline-solo", Settings: parityBaselineSolo(), MCSeed: 0xCAFEF00D},
		{Name: "mfj-with-ss", Settings: parityMFJWithSS(), MCSeed: 0xCAFEF00D},
		{Name: "rmd-active", Settings: parityRMDActive(), MCSeed: 0xCAFEF00D},
		{Name: "guardrails-on", Settings: parityGuardrailsOn(), MCSeed: 0xCAFEF00D},
		{Name: "taxable-mix", Settings: parityTaxableMix(), MCSeed: 0xCAFEF00D},
	}
}

func TestParity_FullAnalysis_AcrossFixtures(t *testing.T) {
	for _, f := range parityFixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			calc := NewCalculator(prepare.MustFrom(t, f.Settings))
			calc.SetMonteCarloSeedForParity(f.MCSeed)
			old := calc.RunFullAnalysis()

			in := engine.Input{Prepared: prepare.MustFrom(t, f.Settings)}
			newOut := runFullForParity(engine.New(), in, f.MCSeed)

			if diff := compareWhatIfAnalysis(old, newOut); diff != "" {
				t.Fatalf("parity diff:\n%s", diff)
			}
		})
	}
}
