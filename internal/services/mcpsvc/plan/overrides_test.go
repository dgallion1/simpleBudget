package plan

import (
	"testing"

	"budget2/internal/models"
)

func baseSettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons[0].BirthMonth = models.BirthMonthForAge("2026-01", 65)
	s.PortfolioValue = 1_500_000
	s.ProjectionYears = 5
	return s
}

// TestPreparedWithOverrides_PreservesPerYearOverridesAcrossBoundary covers a
// second, independent drop point: prepare.From runs its own DeepCopy
// internally, so Apply's re-attach (verified above) does not by itself
// survive into the prepared snapshot the engine actually runs against.
// preparedWithOverrides must re-attach a second time, after prepare.From,
// mirroring analysis/tax_optimizer.go's cloneSettingsWithSSAndRoth.
func TestPreparedWithOverrides_PreservesPerYearOverridesAcrossBoundary(t *testing.T) {
	base := baseSettings()
	base.RothConversion = &models.RothConversionConfig{
		Enabled:          true,
		PerYearOverrides: map[int]float64{0: 50_000},
	}
	prepared, err := preparedWithOverrides(base, Overrides{})
	if err != nil {
		t.Fatalf("preparedWithOverrides: %v", err)
	}
	s := prepared.Settings()
	if s == nil || s.RothConversion == nil || s.RothConversion.PerYearOverrides[0] != 50_000 {
		t.Fatalf("PerYearOverrides lost across the prepare boundary: %+v", s.RothConversion)
	}
}

func TestRunWithOverrides_HigherExpensesLowerFinalBalance(t *testing.T) {
	lo, err := RunWithOverrides(baseSettings(), Overrides{MonthlyLivingExpenses: ptr(3_000.0)})
	if err != nil {
		t.Fatalf("RunWithOverrides(low): %v", err)
	}
	hi, err := RunWithOverrides(baseSettings(), Overrides{MonthlyLivingExpenses: ptr(9_000.0)})
	if err != nil {
		t.Fatalf("RunWithOverrides(high): %v", err)
	}
	if hi.Headline.FinalBalance >= lo.Headline.FinalBalance {
		t.Errorf("higher expenses should reduce final balance: low=%v high=%v",
			lo.Headline.FinalBalance, hi.Headline.FinalBalance)
	}
}

// TestRunWithOverrides_ExpenseOverrideKeepsProjectionYears pins the horizon
// against a field-drop regression along the whole run_scenario path (Apply →
// prepare.From → RunFull → ShapeAnalysis): overriding monthly_living_expenses
// alone must leave projection_years — and every other unset field — at the
// scenario's saved value, including when spending phases are enabled (the
// suspected interaction in the 2026-08-28 field report, which turned out to be
// a stale read of the saved file rather than a drop).
func TestRunWithOverrides_ExpenseOverrideKeepsProjectionYears(t *testing.T) {
	base := baseSettings()
	base.ProjectionYears = 7
	base.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  models.DefaultSpendingPhases(),
	}
	v, err := RunWithOverrides(base, Overrides{MonthlyLivingExpenses: ptr(8_125.0)})
	if err != nil {
		t.Fatalf("RunWithOverrides: %v", err)
	}
	if v.Headline.ProjectionYears != 7 {
		t.Errorf("Headline.ProjectionYears = %d, want the scenario's saved 7", v.Headline.ProjectionYears)
	}
	if len(v.Years) != 7 {
		t.Errorf("len(Years) = %d, want 7", len(v.Years))
	}
}

func TestRunWithOverrides_OmitsMonteCarlo(t *testing.T) {
	v, err := RunWithOverrides(baseSettings(), Overrides{})
	if err != nil {
		t.Fatalf("RunWithOverrides: %v", err)
	}
	if v.MonteCarlo != nil {
		t.Error("run_scenario output must omit Monte Carlo: it is auto-seeded and varies between identical runs")
	}
}

// TestRunWithOverrides_HealthcareMonthlyCostMeasurablyChangesAnalysis proves
// the engine actually consumes the distributed CurrentMonthlyCost values, not
// just that Apply sets them: the base scenario has no HealthcarePersons, so
// this exercises the legacy-scalar branch through the full run.
func TestRunWithOverrides_HealthcareMonthlyCostMeasurablyChangesAnalysis(t *testing.T) {
	lo, err := RunWithOverrides(baseSettings(), Overrides{HealthcareMonthlyCost: ptr(200.0)})
	if err != nil {
		t.Fatalf("RunWithOverrides(low): %v", err)
	}
	hi, err := RunWithOverrides(baseSettings(), Overrides{HealthcareMonthlyCost: ptr(5_000.0)})
	if err != nil {
		t.Fatalf("RunWithOverrides(high): %v", err)
	}
	if hi.Headline.FinalBalance >= lo.Headline.FinalBalance {
		t.Errorf("higher healthcare cost should reduce final balance: low=%v high=%v",
			lo.Headline.FinalBalance, hi.Headline.FinalBalance)
	}
}

// baseSettingsWithSS is baseSettings plus a populated SocialSecurity config.
// FRABenefit > 0 with a valid ClaimAge is what activates the SS-optimizer
// projection (retirement.SocialSecurityProjectionActive), so without this the
// FRA-benefit overrides would be applied but never consumed by the engine.
func baseSettingsWithSS() *models.WhatIfSettings {
	s := baseSettings()
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 2_500,
		FRA:        67,
		ClaimAge:   67,
	}
	return s
}

// TestRunWithOverrides_SocialSecurityFRABenefitMeasurablyChangesAnalysis
// proves the engine actually consumes an overridden FRABenefit through the
// full run — the claim age (67, person aged 65) falls inside the 5-year
// projection, so the benefit pays out for the final three years.
func TestRunWithOverrides_SocialSecurityFRABenefitMeasurablyChangesAnalysis(t *testing.T) {
	lo, err := RunWithOverrides(baseSettingsWithSS(), Overrides{SocialSecurityFRABenefit: ptr(500.0)})
	if err != nil {
		t.Fatalf("RunWithOverrides(low): %v", err)
	}
	hi, err := RunWithOverrides(baseSettingsWithSS(), Overrides{SocialSecurityFRABenefit: ptr(5_000.0)})
	if err != nil {
		t.Fatalf("RunWithOverrides(high): %v", err)
	}
	if hi.Headline.FinalBalance <= lo.Headline.FinalBalance {
		t.Errorf("higher FRA benefit should increase final balance: low=%v high=%v",
			lo.Headline.FinalBalance, hi.Headline.FinalBalance)
	}
}

// TestRunWithOverrides_SpouseFRABenefitMeasurablyChangesAnalysis is the
// spouse-side counterpart. The engine only pays a spouse benefit when the
// household has a spouse person and a valid SpouseClaimAge, so the fixture
// supplies both on top of baseSettingsWithSS.
func TestRunWithOverrides_SpouseFRABenefitMeasurablyChangesAnalysis(t *testing.T) {
	withSpouse := func() *models.WhatIfSettings {
		s := baseSettingsWithSS()
		s.Persons = append(s.Persons, models.Person{
			ID:         "spouse",
			Name:       "Spouse",
			Role:       models.PersonRoleSpouse,
			BirthMonth: models.BirthMonthForAge(s.StartDate, 65),
		})
		s.SocialSecurity.SpouseFRABenefit = 2_000
		s.SocialSecurity.SpouseFRA = 67
		s.SocialSecurity.SpouseClaimAge = 67
		return s
	}
	lo, err := RunWithOverrides(withSpouse(), Overrides{SpouseFRABenefit: ptr(500.0)})
	if err != nil {
		t.Fatalf("RunWithOverrides(low): %v", err)
	}
	hi, err := RunWithOverrides(withSpouse(), Overrides{SpouseFRABenefit: ptr(5_000.0)})
	if err != nil {
		t.Fatalf("RunWithOverrides(high): %v", err)
	}
	if hi.Headline.FinalBalance <= lo.Headline.FinalBalance {
		t.Errorf("higher spouse FRA benefit should increase final balance: low=%v high=%v",
			lo.Headline.FinalBalance, hi.Headline.FinalBalance)
	}
}

func ptr(f float64) *float64 { return &f }
