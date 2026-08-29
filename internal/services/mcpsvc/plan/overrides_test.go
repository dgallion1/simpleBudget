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

func ptr(f float64) *float64 { return &f }
