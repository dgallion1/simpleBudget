package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// When the SS optimizer is active, PresentValue must include the
// optimizer's projected Social Security stream — the same hook the
// projection and BudgetFit use. Iterating only s.IncomeSources omits it
// entirely, understating PV income and overstating the PV gap / coverage
// ratio on every plan that uses the optimizer.
//
// DiscountRate is 0 so the PV of a level stream equals its nominal sum,
// making the expected value independent of any payment-timing convention.
func TestPresentValue_IncludesOptimizerSocialSecurity(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.IncomeSources = nil
	s.DiscountRate = 0
	s.ProjectionYears = 30

	const monthlyBenefit = 2500.0
	const claimMonth = 60 // SS begins 5 years out
	hooks := engine.Hooks{
		SocialSecurityProjectionActive: func(*models.WhatIfSettings) bool { return true },
		ProjectedSocialSecurityIncome: func(_ *models.WhatIfSettings, month int) float64 {
			if month >= claimMonth {
				return monthlyBenefit
			}
			return 0
		},
	}
	in := engine.Input{Prepared: prepare.MustFrom(t, s), Hooks: hooks}

	result := PresentValue(in)

	months := s.ProjectionYears * 12
	want := monthlyBenefit * float64(months-claimMonth)
	if math.Abs(result.PVIncome-want) > 0.01 {
		t.Fatalf("PVIncome = %.2f, want %.2f (optimizer SS stream omitted from PV?)", result.PVIncome, want)
	}
}

// When the SS optimizer is active and a manual Social-Security income
// source is ALSO present, PresentValue must count SS once (from the
// optimizer hook), not add the stale manual source on top — mirroring
// CalculateMonthlyIncomeBreakdown, which replaces manual SS sources with
// the optimizer value when active.
func TestPresentValue_OptimizerSocialSecurityNotDoubleCounted(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.DiscountRate = 0
	s.ProjectionYears = 30
	s.IncomeSources = []models.IncomeSource{
		{ID: "ss", Name: "Social Security", Amount: 9999, StartMonth: 0},
	}

	const monthlyBenefit = 2500.0
	hooks := engine.Hooks{
		SocialSecurityProjectionActive: func(*models.WhatIfSettings) bool { return true },
		ProjectedSocialSecurityIncome: func(_ *models.WhatIfSettings, _ int) float64 {
			return monthlyBenefit
		},
	}
	in := engine.Input{Prepared: prepare.MustFrom(t, s), Hooks: hooks}

	result := PresentValue(in)

	months := s.ProjectionYears * 12
	want := monthlyBenefit * float64(months) // only the optimizer stream
	if math.Abs(result.PVIncome-want) > 0.01 {
		t.Fatalf("PVIncome = %.2f, want %.2f (manual SS source double-counted with optimizer?)", result.PVIncome, want)
	}
}
