//go:build !short

package retirement

import "budget2/internal/models"

// defaultParitySettings returns a baseline single-filer scenario used as
// the seed for every parity fixture. Structure mirrors the test
// helpers already in use elsewhere in the package
// (defaultSettingsForTest / DefaultWhatIfSettings).
func defaultParitySettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 1_000_000
	s.MonthlyLivingExpenses = 4_000
	s.ProjectionYears = 20
	s.CurrentAge = 65
	s.InvestmentReturn = 6.0 // override mode → deterministic returns
	s.InflationRate = 3.0
	s.HealthcareInflation = 6.0
	s.SpendingDeclineRate = 1.0
	s.TaxDeferredPercent = 60.0
	s.RothPercent = 10.0
	s.TaxConfig = models.DefaultTaxConfig()
	s.TaxConfig.FilingStatus = models.FilingSingle
	return s
}

// parityBaselineSolo: single filer, no SS, no guardrails, no RMD active.
func parityBaselineSolo() *models.WhatIfSettings {
	return defaultParitySettings()
}

// parityMFJWithSS: married-filing-jointly with SS optimizer active.
func parityMFJWithSS() *models.WhatIfSettings {
	s := defaultParitySettings()
	s.TaxConfig.FilingStatus = models.FilingMarriedJoint
	s.SpouseAge = 63
	// Add a spouse person so HasSpouse() returns true.
	startDate := s.StartDate
	s.Persons = append(s.Persons, models.Person{
		ID:         "spouse-fixture",
		Name:       "Spouse",
		BirthMonth: models.BirthMonthForAge(startDate, 63),
		Role:       models.PersonRoleSpouse,
	})
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit:       2_500,
		FRA:              67,
		ClaimAge:         67,
		SpouseFRABenefit: 1_800,
		SpouseFRA:        67,
		SpouseClaimAge:   67,
	}
	return s
}

// parityRMDActive: age 73 with a large tax-deferred balance so the RMD
// path is exercised on year 0.
func parityRMDActive() *models.WhatIfSettings {
	s := defaultParitySettings()
	s.CurrentAge = 73
	s.PortfolioValue = 1_500_000
	s.TaxDeferredPercent = 80
	s.RothPercent = 10
	// Re-seed the primary person's birth month so prepare.ComputeAges
	// agrees with CurrentAge=73.
	if len(s.Persons) > 0 {
		s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 73)
	}
	return s
}

// parityGuardrailsOn: same as baseline but with spending guardrails
// enabled. Thresholds chosen so they may or may not trigger over the
// 20-year horizon.
func parityGuardrailsOn() *models.WhatIfSettings {
	s := defaultParitySettings()
	s.Guardrails = &models.GuardrailConfig{
		Enabled:         true,
		FloorDropPct:    20,
		FloorCutPct:     10,
		CeilingRisePct:  20,
		CeilingRaisePct: 10,
		MinSpendingPct:  75,
		MaxSpendingPct:  120,
	}
	return s
}

// parityTaxableMix: shift the mix so the taxable bucket is the
// dominant share, exercising the taxable-account state machine + LTCG
// realization path.
func parityTaxableMix() *models.WhatIfSettings {
	s := defaultParitySettings()
	s.TaxDeferredPercent = 20
	s.RothPercent = 10
	// Taxable share = 100 - 20 - 10 = 70%.
	return s
}
