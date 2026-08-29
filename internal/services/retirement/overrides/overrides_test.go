package overrides

import (
	"encoding/json"
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
			name: "HealthcareMonthlyCost",
			base: baseSettings,
			o:    Overrides{HealthcareMonthlyCost: ptr(750)},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.MonthlyHealthcare != 750 {
					t.Errorf("MonthlyHealthcare = %v, want 750", got.MonthlyHealthcare)
				}
				if got.HealthcareInflation != base.HealthcareInflation {
					t.Errorf("HealthcareInflation changed to %v, want unchanged %v", got.HealthcareInflation, base.HealthcareInflation)
				}
			},
		},
		{
			name: "SocialSecurityFRABenefit",
			base: baseSettingsWithSS,
			o:    Overrides{SocialSecurityFRABenefit: ptr(3_000)},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.SocialSecurity == nil || got.SocialSecurity.FRABenefit != 3_000 {
					t.Fatalf("SocialSecurity.FRABenefit = %+v, want 3000", got.SocialSecurity)
				}
				if got.SocialSecurity.SpouseFRABenefit != base.SocialSecurity.SpouseFRABenefit {
					t.Errorf("SpouseFRABenefit changed to %v, want unchanged %v",
						got.SocialSecurity.SpouseFRABenefit, base.SocialSecurity.SpouseFRABenefit)
				}
			},
		},
		{
			name: "SpouseFRABenefit",
			base: baseSettingsWithSS,
			o:    Overrides{SpouseFRABenefit: ptr(1_200)},
			check: func(t *testing.T, base, got *models.WhatIfSettings) {
				if got.SocialSecurity == nil || got.SocialSecurity.SpouseFRABenefit != 1_200 {
					t.Fatalf("SocialSecurity.SpouseFRABenefit = %+v, want 1200", got.SocialSecurity)
				}
				if got.SocialSecurity.FRABenefit != base.SocialSecurity.FRABenefit {
					t.Errorf("FRABenefit changed to %v, want unchanged %v",
						got.SocialSecurity.FRABenefit, base.SocialSecurity.FRABenefit)
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

func TestValidate_RejectsAbsurdRates(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    Overrides
		want string
	}{
		{"inflation too high", Overrides{InflationRate: ptr(500)}, "inflation_rate"},
		{"inflation too low", Overrides{InflationRate: ptr(-50)}, "inflation_rate"},
		{"return too high", Overrides{InvestmentReturn: ptr(1000)}, "investment_return"},
		{"return too low", Overrides{InvestmentReturn: ptr(-99)}, "investment_return"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.o.validate()
			if err == nil {
				t.Fatalf("expected an error naming %s, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name the field %q", err, tc.want)
			}
		})
	}
}

func TestValidate_AcceptsPlausibleRates(t *testing.T) {
	o := Overrides{InflationRate: ptr(2.5), InvestmentReturn: ptr(7)}
	if err := o.validate(); err != nil {
		t.Fatalf("plausible rates rejected: %v", err)
	}
	// Zero must stay legal: investment_return 0 means "use the asset allocation".
	if err := (Overrides{InvestmentReturn: ptr(0)}).validate(); err != nil {
		t.Fatalf("investment_return 0 must remain legal: %v", err)
	}
}

func TestValidateWritable_RejectsHealthcareInflation(t *testing.T) {
	err := Overrides{HealthcareInflation: ptr(6)}.ValidateWritable()
	if err == nil {
		t.Fatal("expected healthcare_inflation to be rejected on the write path")
	}
	if !strings.Contains(err.Error(), "healthcare_inflation") {
		t.Fatalf("error %q does not name the field", err)
	}
}

func TestValidateWritable_AllowsTheTenWritableFields(t *testing.T) {
	o := Overrides{
		MonthlyLivingExpenses:  ptr(5000),
		ProjectionYears:        ptrInt(30),
		InflationRate:          ptr(2.5),
		InvestmentReturn:       ptr(7),
		FilingStatus:           strPtr("married_joint"),
		RothConversionAmount:   ptr(50000),
		RothConversionStart:    ptrInt(1),
		RothConversionEnd:      ptrInt(10),
		SocialSecurityClaimAge: ptrInt(67),
		SpouseClaimAge:         ptrInt(65),
	}
	if err := o.ValidateWritable(); err != nil {
		t.Fatalf("the ten writable fields were rejected: %v", err)
	}
}

func TestValidateWritable_RejectsRothWindowWithoutAmount(t *testing.T) {
	err := Overrides{RothConversionStart: ptrInt(1), RothConversionEnd: ptrInt(5)}.ValidateWritable()
	if err == nil {
		t.Fatal("expected a Roth window with no amount to be rejected")
	}
	if !strings.Contains(err.Error(), "roth_conversion_amount") {
		t.Fatalf("error %q should name the missing field", err)
	}
}

func TestApply_HealthcareMonthlyCost_NoPersons_SetsLegacyScalar(t *testing.T) {
	base := baseSettings()
	base.MonthlyHealthcare = 500
	got, err := Apply(base, Overrides{HealthcareMonthlyCost: ptr(900)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.MonthlyHealthcare != 900 {
		t.Errorf("MonthlyHealthcare = %v, want 900", got.MonthlyHealthcare)
	}
	if base.MonthlyHealthcare != 500 {
		t.Error("Apply mutated base.MonthlyHealthcare")
	}
}

func TestApply_HealthcareMonthlyCost_DistributesProportionally(t *testing.T) {
	base := baseSettings()
	base.HealthcarePersons = []models.HealthcarePerson{
		{ID: "a", CurrentMonthlyCost: 1500},
		{ID: "b", CurrentMonthlyCost: 750},
	}
	got, err := Apply(base, Overrides{HealthcareMonthlyCost: ptr(1600)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	const tol = 0.01
	if abs(got.HealthcarePersons[0].CurrentMonthlyCost-1066.666667) > tol {
		t.Errorf("person a CurrentMonthlyCost = %v, want ~1066.67", got.HealthcarePersons[0].CurrentMonthlyCost)
	}
	if abs(got.HealthcarePersons[1].CurrentMonthlyCost-533.333333) > tol {
		t.Errorf("person b CurrentMonthlyCost = %v, want ~533.33", got.HealthcarePersons[1].CurrentMonthlyCost)
	}
	sum := got.HealthcarePersons[0].CurrentMonthlyCost + got.HealthcarePersons[1].CurrentMonthlyCost
	if abs(sum-1600) > tol {
		t.Errorf("distributed sum = %v, want 1600", sum)
	}
	// base must not be mutated
	if base.HealthcarePersons[0].CurrentMonthlyCost != 1500 || base.HealthcarePersons[1].CurrentMonthlyCost != 750 {
		t.Error("Apply mutated base.HealthcarePersons")
	}
}

func TestApply_HealthcareMonthlyCost_SplitsEquallyWhenExistingTotalIsZero(t *testing.T) {
	base := baseSettings()
	base.HealthcarePersons = []models.HealthcarePerson{
		{ID: "a", CurrentMonthlyCost: 0},
		{ID: "b", CurrentMonthlyCost: 0},
	}
	got, err := Apply(base, Overrides{HealthcareMonthlyCost: ptr(1600)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.HealthcarePersons[0].CurrentMonthlyCost != 800 || got.HealthcarePersons[1].CurrentMonthlyCost != 800 {
		t.Errorf("expected an even 800/800 split, got %v/%v",
			got.HealthcarePersons[0].CurrentMonthlyCost, got.HealthcarePersons[1].CurrentMonthlyCost)
	}
}

func TestApply_HealthcareMonthlyCost_DoesNotTouchMedicareOrACAFields(t *testing.T) {
	base := baseSettings()
	base.HealthcarePersons = []models.HealthcarePerson{
		{ID: "a", CurrentMonthlyCost: 1000, MedicareMonthlyCost: 300, ACACostAfterEmployer: 450},
	}
	got, err := Apply(base, Overrides{HealthcareMonthlyCost: ptr(1200)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.HealthcarePersons[0].MedicareMonthlyCost != 300 {
		t.Errorf("MedicareMonthlyCost changed to %v, want unchanged 300", got.HealthcarePersons[0].MedicareMonthlyCost)
	}
	if got.HealthcarePersons[0].ACACostAfterEmployer != 450 {
		t.Errorf("ACACostAfterEmployer changed to %v, want unchanged 450", got.HealthcarePersons[0].ACACostAfterEmployer)
	}
}

func TestApply_SocialSecurityFRABenefits(t *testing.T) {
	base := baseSettingsWithSS()
	got, err := Apply(base, Overrides{
		SocialSecurityFRABenefit: ptr(2_800),
		SpouseFRABenefit:         ptr(1_900),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.SocialSecurity.FRABenefit != 2_800 {
		t.Errorf("FRABenefit = %v, want 2800", got.SocialSecurity.FRABenefit)
	}
	if got.SocialSecurity.SpouseFRABenefit != 1_900 {
		t.Errorf("SpouseFRABenefit = %v, want 1900", got.SocialSecurity.SpouseFRABenefit)
	}
	if base.SocialSecurity.FRABenefit != 2_500 {
		t.Error("Apply mutated base.SocialSecurity.FRABenefit")
	}
}

func TestApply_SocialSecurityFRABenefit_NilConfigErrors(t *testing.T) {
	base := baseSettings() // no SocialSecurity configured
	if _, err := Apply(base, Overrides{SocialSecurityFRABenefit: ptr(2_000)}); err == nil {
		t.Fatal("expected an error when overriding FRA benefit with no social_security configuration")
	} else if !strings.Contains(err.Error(), "social_security") {
		t.Errorf("error should mention social_security configuration, got: %v", err)
	}
}

func TestApply_SpouseFRABenefit_NilConfigErrors(t *testing.T) {
	base := baseSettings() // no SocialSecurity configured
	if _, err := Apply(base, Overrides{SpouseFRABenefit: ptr(1_000)}); err == nil {
		t.Fatal("expected an error when overriding spouse FRA benefit with no social_security configuration")
	} else if !strings.Contains(err.Error(), "social_security") {
		t.Errorf("error should mention social_security configuration, got: %v", err)
	}
}

func TestApply_RejectsNegativeHealthcareAndSSValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		o     Overrides
		field string
	}{
		{"negative healthcare cost", Overrides{HealthcareMonthlyCost: ptr(-1)}, "healthcare_monthly_cost"},
		{"negative FRA benefit", Overrides{SocialSecurityFRABenefit: ptr(-1)}, "social_security_fra_benefit"},
		{"negative spouse FRA benefit", Overrides{SpouseFRABenefit: ptr(-1)}, "spouse_fra_benefit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Apply(baseSettingsWithSS(), tc.o); err == nil {
				t.Fatal("expected a validation error")
			} else if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("error should name %q, got: %v", tc.field, err)
			}
		})
	}
}

// TestApply_HealthcareAndSocialSecurityFieldsSurviveJSONRoundTrip covers the
// persistence mechanism apply_changes actually relies on (the settings
// manager marshals *models.WhatIfSettings to JSON and writes it to disk):
// the values Apply produces for the three new fields must round-trip through
// JSON encode/decode unchanged.
func TestApply_HealthcareAndSocialSecurityFieldsSurviveJSONRoundTrip(t *testing.T) {
	base := baseSettingsWithSS()
	base.HealthcarePersons = []models.HealthcarePerson{
		{ID: "a", CurrentMonthlyCost: 1500},
		{ID: "b", CurrentMonthlyCost: 750},
	}
	got, err := Apply(base, Overrides{
		HealthcareMonthlyCost:    ptr(1600),
		SocialSecurityFRABenefit: ptr(2_800),
		SpouseFRABenefit:         ptr(1_900),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var roundTripped models.WhatIfSettings
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	const tol = 0.01
	if abs(roundTripped.HealthcarePersons[0].CurrentMonthlyCost-got.HealthcarePersons[0].CurrentMonthlyCost) > tol {
		t.Errorf("person a CurrentMonthlyCost did not round-trip: got %v, want %v",
			roundTripped.HealthcarePersons[0].CurrentMonthlyCost, got.HealthcarePersons[0].CurrentMonthlyCost)
	}
	if abs(roundTripped.HealthcarePersons[1].CurrentMonthlyCost-got.HealthcarePersons[1].CurrentMonthlyCost) > tol {
		t.Errorf("person b CurrentMonthlyCost did not round-trip: got %v, want %v",
			roundTripped.HealthcarePersons[1].CurrentMonthlyCost, got.HealthcarePersons[1].CurrentMonthlyCost)
	}
	if roundTripped.SocialSecurity == nil || roundTripped.SocialSecurity.FRABenefit != got.SocialSecurity.FRABenefit {
		t.Errorf("FRABenefit did not round-trip: got %+v, want %v", roundTripped.SocialSecurity, got.SocialSecurity.FRABenefit)
	}
	if roundTripped.SocialSecurity.SpouseFRABenefit != got.SocialSecurity.SpouseFRABenefit {
		t.Errorf("SpouseFRABenefit did not round-trip: got %v, want %v",
			roundTripped.SocialSecurity.SpouseFRABenefit, got.SocialSecurity.SpouseFRABenefit)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func ptr(f float64) *float64  { return &f }
func ptrInt(i int) *int       { return &i }
func strPtr(s string) *string { return &s }
