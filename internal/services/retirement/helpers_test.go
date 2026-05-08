package retirement

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// newTestCalc preps the given settings and constructs a Calculator. Use
// from tests instead of NewCalculator to avoid threading prepare.MustFrom
// through every callsite. On invalid settings, prepare.MustFrom calls
// tb.Fatalf.
func newTestCalc(tb testing.TB, s *models.WhatIfSettings) *Calculator {
	tb.Helper()
	return NewCalculator(prepare.MustFrom(tb, s))
}

// newTestCalcWithChain preps the given primary settings and constructs a
// chain-aware Calculator. Chain links must already be PreparedChainLink
// values (use preparedLink to build them from raw settings).
func newTestCalcWithChain(tb testing.TB, s *models.WhatIfSettings, chain []PreparedChainLink) *Calculator {
	tb.Helper()
	return NewCalculatorWithChain(prepare.MustFrom(tb, s), chain)
}

// preparedLink builds a PreparedChainLink from raw settings, for tests that
// previously constructed ResolvedScenarioChainLink literals.
func preparedLink(tb testing.TB, filename string, transitionAge int, s *models.WhatIfSettings) PreparedChainLink {
	tb.Helper()
	return PreparedChainLink{
		ScenarioFilename: filename,
		TransitionAge:    transitionAge,
		Settings:         prepare.MustFrom(tb, s),
	}
}
