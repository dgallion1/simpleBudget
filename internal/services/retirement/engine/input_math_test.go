package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func TestHooksNilDefaults(t *testing.T) {
	var hooks Hooks
	settings := &models.WhatIfSettings{}

	if hooks.SSActive(settings) {
		t.Fatal("nil SocialSecurityProjectionActive should default to false")
	}
	if got := hooks.SSIncome(settings, 24); got != 0 {
		t.Fatalf("nil ProjectedSocialSecurityIncome got %.2f, want 0", got)
	}
	next, active := hooks.ResolveChain(2030, 2, settings, nil)
	if next != 2 {
		t.Fatalf("nil ResolveChainTransition next index got %d, want 2", next)
	}
	if active != nil {
		t.Fatalf("nil ResolveChainTransition active scenario got %#v, want nil", active)
	}
}

func TestHooksDelegateWhenConfigured(t *testing.T) {
	settings := &models.WhatIfSettings{ScenarioName: "base"}
	nextSettings := &models.WhatIfSettings{ScenarioName: "next"}
	hooks := Hooks{
		SocialSecurityProjectionActive: func(s *models.WhatIfSettings) bool {
			return s.ScenarioName == "base"
		},
		ProjectedSocialSecurityIncome: func(s *models.WhatIfSettings, month int) float64 {
			return float64(month) * 100
		},
		ResolveChainTransition: func(currentYear, nextChainIndex int, primarySettings *models.WhatIfSettings, chain []PreparedChainLink) (int, *models.WhatIfSettings) {
			return nextChainIndex + currentYear - 2030, nextSettings
		},
	}

	if !hooks.SSActive(settings) {
		t.Fatal("configured SocialSecurityProjectionActive was not called")
	}
	if got := hooks.SSIncome(settings, 24); got != 2400 {
		t.Fatalf("configured ProjectedSocialSecurityIncome got %.2f, want 2400", got)
	}
	next, active := hooks.ResolveChain(2031, 2, settings, nil)
	if next != 3 {
		t.Fatalf("configured ResolveChainTransition next index got %d, want 3", next)
	}
	if active != nextSettings {
		t.Fatalf("configured ResolveChainTransition active scenario got %#v, want next settings", active)
	}
}

func TestMonthlyCompoundingHelpers(t *testing.T) {
	assertClose(t, "zero decimal monthly factor", MonthlyCompoundFactorFromDecimal(0), 1)
	assertClose(t, "decimal monthly factor", MonthlyCompoundFactorFromDecimal(0.12), math.Pow(1.12, 1.0/12.0))
	assertClose(t, "zero percent fractional compound", compoundedFactorFromPercent(0, 6), 1)
	assertClose(t, "zero months fractional compound", compoundedFactorFromPercent(6, 0), 1)
	assertClose(t, "percent fractional compound", compoundedFactorFromPercent(6, 18), math.Pow(1.06, 1.5))
	assertClose(t, "negative fraction monthly return", fractionalMonthlyReturn(0.02, -1), 0)
	assertClose(t, "full fraction monthly return", fractionalMonthlyReturn(0.02, 2), 0.02)
	assertClose(t, "partial fraction monthly return", fractionalMonthlyReturn(0.02, 0.25), math.Pow(1.02, 0.25)-1)
}

func TestPresentValueAnnuityBranches(t *testing.T) {
	assertClose(t, "no payments", PresentValueAnnuity(1000, 5, 0, 0, 0), 0)
	assertClose(t, "zero payment", PresentValueAnnuity(0, 5, 0, 0, 12), 0)
	assertClose(t, "zero discount no growth", PresentValueAnnuity(100, 0, 0, 0, 12), 1200)

	zeroDiscountGrowing := 100.0 * (math.Pow(1.01, 12) - 1) / 0.01
	assertClose(t, "zero discount growing", PresentValueAnnuity(100, 0, monthlyRateToAnnualPercent(0.01), 0, 12), zeroDiscountGrowing)

	equalRatePV := 100.0 * 12
	assertClose(t, "equal discount and growth", PresentValueAnnuity(100, 6, 6, 0, 12), equalRatePV)

	monthlyRate := math.Pow(1.06, 1.0/12.0) - 1
	regularPV := 100 * (1 - math.Pow(1+monthlyRate, -12)) / monthlyRate
	assertClose(t, "regular annuity", PresentValueAnnuity(100, 6, 0, 0, 12), regularPV)
	assertClose(t, "future start discount", PresentValueAnnuity(100, 6, 0, 12, 12), regularPV/math.Pow(1+monthlyRate, 12))
}

func monthlyRateToAnnualPercent(monthlyRate float64) float64 {
	return (math.Pow(1+monthlyRate, 12) - 1) * 100
}
