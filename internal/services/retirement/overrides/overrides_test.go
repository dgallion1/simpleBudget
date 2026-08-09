package overrides

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
		{"inverted roth conversion window", Overrides{RothConversionStart: ptrInt(5), RothConversionEnd: ptrInt(2)}, "roth_conversion_end_year"},
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

// baseSettingsWithSS is baseSettings plus a non-nil SocialSecurity config,
// needed because Apply errors on SocialSecurityClaimAge/SpouseClaimAge
// overrides when the base scenario has no social_security configuration.
func baseSettingsWithSS() *models.WhatIfSettings {
	s := baseSettings()
	s.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit:     2_500,
		FRA:            67,
		ClaimAge:       67,
		SpouseClaimAge: 67,
	}
	return s
}

// TestApply_EachFieldChangesOnlyItsDestination guards against a transposed
// assignment inside Apply (e.g. writing a HealthcareInflation override into
// InflationRate): for every override field, apply exactly that one field and
// assert both that it landed in the right place and that a plausible
// neighbouring field was left alone.
func TestApply_EachFieldChangesOnlyItsDestination(t *testing.T) {
	for _, tc := range []struct {
		name  string
		base  func() *models.WhatIfSettings
		o     Overrides
		check func(t *testing.T, base, got *models.WhatIfSettings)
	}{
		{
			name: "HealthcareInflation",
			base: baseSettings,
			o:    Overrides{HealthcareInflation: ptr(9.5)},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.HealthcareInflation != 9.5 {
					t.Errorf("HealthcareInflation = %v, want 9.5", got.HealthcareInflation)
				}
				if got.InflationRate != base.InflationRate {
					t.Errorf("InflationRate changed to %v, want unchanged %v", got.InflationRate, base.InflationRate)
				}
			},
		},
		{
			name: "InflationRate",
			base: baseSettings,
			o:    Overrides{InflationRate: ptr(4.25)},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.InflationRate != 4.25 {
					t.Errorf("InflationRate = %v, want 4.25", got.InflationRate)
				}
				if got.HealthcareInflation != base.HealthcareInflation {
					t.Errorf("HealthcareInflation changed to %v, want unchanged %v", got.HealthcareInflation, base.HealthcareInflation)
				}
			},
		},
		{
			name: "InvestmentReturn",
			base: baseSettings,
			o:    Overrides{InvestmentReturn: ptr(5.5)},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.InvestmentReturn != 5.5 {
					t.Errorf("InvestmentReturn = %v, want 5.5", got.InvestmentReturn)
				}
				if got.DiscountRate != base.DiscountRate {
					t.Errorf("DiscountRate changed to %v, want unchanged %v", got.DiscountRate, base.DiscountRate)
				}
			},
		},
		{
			name: "RothConversionAmount",
			base: baseSettings,
			o:    Overrides{RothConversionAmount: ptr(20_000.0)},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.RothConversion == nil || got.RothConversion.AnnualAmount != 20_000 {
					t.Fatalf("RothConversion.AnnualAmount = %+v, want 20000", got.RothConversion)
				}
				if got.RothConversion.StartYear != 0 {
					t.Errorf("RothConversion.StartYear changed to %v, want unchanged 0", got.RothConversion.StartYear)
				}
			},
		},
		{
			name: "RothConversionStart",
			base: baseSettings,
			o:    Overrides{RothConversionStart: ptrInt(3)},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.RothConversion == nil || got.RothConversion.StartYear != 3 {
					t.Fatalf("RothConversion.StartYear = %+v, want 3", got.RothConversion)
				}
				if got.RothConversion.AnnualAmount != 0 {
					t.Errorf("RothConversion.AnnualAmount changed to %v, want unchanged 0", got.RothConversion.AnnualAmount)
				}
			},
		},
		{
			name: "RothConversionEnd",
			base: baseSettings,
			o:    Overrides{RothConversionEnd: ptrInt(8)},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.RothConversion == nil || got.RothConversion.EndYear != 8 {
					t.Fatalf("RothConversion.EndYear = %+v, want 8", got.RothConversion)
				}
				if got.RothConversion.StartYear != 0 {
					t.Errorf("RothConversion.StartYear changed to %v, want unchanged 0", got.RothConversion.StartYear)
				}
			},
		},
		{
			name: "SocialSecurityClaimAge",
			base: baseSettingsWithSS,
			o:    Overrides{SocialSecurityClaimAge: ptrInt(70)},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.SocialSecurity == nil || got.SocialSecurity.ClaimAge != 70 {
					t.Fatalf("SocialSecurity.ClaimAge = %+v, want 70", got.SocialSecurity)
				}
				if got.SocialSecurity.SpouseClaimAge != base.SocialSecurity.SpouseClaimAge {
					t.Errorf("SocialSecurity.SpouseClaimAge changed to %v, want unchanged %v",
						got.SocialSecurity.SpouseClaimAge, base.SocialSecurity.SpouseClaimAge)
				}
			},
		},
		{
			name: "SpouseClaimAge",
			base: baseSettingsWithSS,
			o:    Overrides{SpouseClaimAge: ptrInt(62)},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.SocialSecurity == nil || got.SocialSecurity.SpouseClaimAge != 62 {
					t.Fatalf("SocialSecurity.SpouseClaimAge = %+v, want 62", got.SocialSecurity)
				}
				if got.SocialSecurity.ClaimAge != base.SocialSecurity.ClaimAge {
					t.Errorf("SocialSecurity.ClaimAge changed to %v, want unchanged %v",
						got.SocialSecurity.ClaimAge, base.SocialSecurity.ClaimAge)
				}
			},
		},
		{
			name: "ProjectionYears",
			base: baseSettings,
			o:    Overrides{ProjectionYears: ptrInt(12)},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.ProjectionYears != 12 {
					t.Errorf("ProjectionYears = %v, want 12", got.ProjectionYears)
				}
				if got.MonthlyLivingExpenses != base.MonthlyLivingExpenses {
					t.Errorf("MonthlyLivingExpenses changed to %v, want unchanged %v",
						got.MonthlyLivingExpenses, base.MonthlyLivingExpenses)
				}
			},
		},
		{
			name: "FilingStatus",
			base: baseSettings,
			o:    Overrides{FilingStatus: strPtr("married_joint")},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.TaxConfig == nil || got.TaxConfig.FilingStatus != models.FilingMarriedJoint {
					t.Fatalf("TaxConfig.FilingStatus = %+v, want married_joint", got.TaxConfig)
				}
				if got.TaxConfig.StateIncomeTaxRate != base.TaxConfig.StateIncomeTaxRate {
					t.Errorf("TaxConfig.StateIncomeTaxRate changed, want unchanged nil")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := tc.base()
			got, err := Apply(base, tc.o)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			tc.check(t, base, got)
		})
	}
}

func ptr(f float64) *float64  { return &f }
func ptrInt(i int) *int       { return &i }
func strPtr(s string) *string { return &s }
