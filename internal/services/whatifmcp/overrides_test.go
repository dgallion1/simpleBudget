package whatifmcp

import (
	"strings"
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

func TestApply_ChangesOnlyTheNamedField(t *testing.T) {
	base := baseSettings()
	got, err := Apply(base, Overrides{MonthlyLivingExpenses: ptr(7_000.0)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.MonthlyLivingExpenses != 7_000 {
		t.Errorf("MonthlyLivingExpenses = %v, want 7000", got.MonthlyLivingExpenses)
	}
	if got.ProjectionYears != base.ProjectionYears {
		t.Errorf("ProjectionYears changed to %v, want %v", got.ProjectionYears, base.ProjectionYears)
	}
	if base.MonthlyLivingExpenses == 7_000 {
		t.Error("Apply mutated the base settings; it must operate on a copy")
	}
}

func TestApply_PreservesPerYearOverridesAcrossDeepCopy(t *testing.T) {
	base := baseSettings()
	base.RothConversion = &models.RothConversionConfig{
		Enabled:          true,
		PerYearOverrides: map[int]float64{0: 50_000},
	}
	got, err := Apply(base, Overrides{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.RothConversion == nil || got.RothConversion.PerYearOverrides[0] != 50_000 {
		t.Fatalf("PerYearOverrides lost in copy: %+v", got.RothConversion)
	}
}

func TestApply_RejectsInvalidValuesNamingTheField(t *testing.T) {
	for _, tc := range []struct {
		name  string
		o     Overrides
		field string
	}{
		{"negative expenses", Overrides{MonthlyLivingExpenses: ptr(-1.0)}, "monthly_living_expenses"},
		{"claim age too low", Overrides{SocialSecurityClaimAge: ptrInt(61)}, "social_security_claim_age"},
		{"claim age too high", Overrides{SocialSecurityClaimAge: ptrInt(71)}, "social_security_claim_age"},
		{"zero projection years", Overrides{ProjectionYears: ptrInt(0)}, "projection_years"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Apply(baseSettings(), tc.o); err == nil {
				t.Fatal("expected a validation error")
			} else if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("error should name %q, got: %v", tc.field, err)
			}
		})
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

func TestRunWithOverrides_OmitsMonteCarlo(t *testing.T) {
	v, err := RunWithOverrides(baseSettings(), Overrides{})
	if err != nil {
		t.Fatalf("RunWithOverrides: %v", err)
	}
	if v.MonteCarlo != nil {
		t.Error("run_scenario output must omit Monte Carlo: it is auto-seeded and varies between identical runs")
	}
}

func ptr(f float64) *float64 { return &f }
func ptrInt(i int) *int      { return &i }
