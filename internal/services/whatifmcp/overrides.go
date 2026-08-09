package whatifmcp

import (
	"fmt"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// Overrides is a sparse set of scenario changes. A nil pointer means "leave
// unchanged" — that is why every field is a pointer rather than a value.
type Overrides struct {
	MonthlyLivingExpenses  *float64 `json:"monthly_living_expenses,omitempty" jsonschema:"monthly living expenses in dollars"`
	HealthcareInflation    *float64 `json:"healthcare_inflation,omitempty" jsonschema:"annual healthcare inflation as a percent, e.g. 6 for 6%"`
	InflationRate          *float64 `json:"inflation_rate,omitempty" jsonschema:"annual general inflation as a percent"`
	InvestmentReturn       *float64 `json:"investment_return,omitempty" jsonschema:"annual investment return as a percent; 0 means use the asset allocation"`
	RothConversionAmount   *float64 `json:"roth_conversion_amount,omitempty" jsonschema:"annual Roth conversion amount in dollars"`
	RothConversionStart    *int     `json:"roth_conversion_start_year,omitempty" jsonschema:"projection year the conversions begin, 0 = now"`
	RothConversionEnd      *int     `json:"roth_conversion_end_year,omitempty" jsonschema:"projection year the conversions end, 0 = indefinite"`
	SocialSecurityClaimAge *int     `json:"social_security_claim_age,omitempty" jsonschema:"primary Social Security claim age, 62-70"`
	SpouseClaimAge         *int     `json:"spouse_claim_age,omitempty" jsonschema:"spouse Social Security claim age, 62-70"`
	ProjectionYears        *int     `json:"projection_years,omitempty" jsonschema:"length of the projection in years"`
	FilingStatus           *string  `json:"filing_status,omitempty" jsonschema:"single, married_joint, married_separate, or head_of_household"`
}

// Apply returns a deep copy of base with the overrides applied. base is never
// mutated. Invalid values are rejected before any engine work, naming the field.
func Apply(base *models.WhatIfSettings, o Overrides) (*models.WhatIfSettings, error) {
	if base == nil {
		return nil, fmt.Errorf("apply overrides: nil base settings")
	}
	if err := o.validate(); err != nil {
		return nil, err
	}

	s, err := prepare.DeepCopy(base)
	if err != nil {
		return nil, fmt.Errorf("copy settings: %w", err)
	}
	// prepare.DeepCopy round-trips through JSON, so fields tagged json:"-" do
	// not survive. PerYearOverrides is the known instance; re-attach it here
	// so Apply's own return value is correct. NOTE: this does not survive a
	// subsequent prepare.From (RunWithOverrides calls it) — that runs its own
	// DeepCopy and drops the map again. The re-attach that actually matters
	// for the engine run is the post-boundary one in preparedWithOverrides,
	// mirroring analysis/tax_optimizer.go:90-100.
	if base.RothConversion != nil && base.RothConversion.PerYearOverrides != nil && s.RothConversion != nil {
		s.RothConversion.PerYearOverrides = base.RothConversion.PerYearOverrides
	}

	if o.MonthlyLivingExpenses != nil {
		s.MonthlyLivingExpenses = *o.MonthlyLivingExpenses
	}
	if o.HealthcareInflation != nil {
		s.HealthcareInflation = *o.HealthcareInflation
	}
	if o.InflationRate != nil {
		s.InflationRate = *o.InflationRate
	}
	if o.InvestmentReturn != nil {
		s.InvestmentReturn = *o.InvestmentReturn
	}
	if o.ProjectionYears != nil {
		s.ProjectionYears = *o.ProjectionYears
	}
	if o.RothConversionAmount != nil || o.RothConversionStart != nil || o.RothConversionEnd != nil {
		if s.RothConversion == nil {
			s.RothConversion = &models.RothConversionConfig{}
		}
		if o.RothConversionAmount != nil {
			s.RothConversion.AnnualAmount = *o.RothConversionAmount
			s.RothConversion.Enabled = *o.RothConversionAmount > 0
		}
		if o.RothConversionStart != nil {
			s.RothConversion.StartYear = *o.RothConversionStart
		}
		if o.RothConversionEnd != nil {
			s.RothConversion.EndYear = *o.RothConversionEnd
		}
	}
	if o.SocialSecurityClaimAge != nil || o.SpouseClaimAge != nil {
		if s.SocialSecurity == nil {
			return nil, fmt.Errorf("scenario has no social_security configuration to override")
		}
		if o.SocialSecurityClaimAge != nil {
			s.SocialSecurity.ClaimAge = *o.SocialSecurityClaimAge
		}
		if o.SpouseClaimAge != nil {
			s.SocialSecurity.SpouseClaimAge = *o.SpouseClaimAge
		}
	}
	if o.FilingStatus != nil {
		if s.TaxConfig == nil {
			s.TaxConfig = models.DefaultTaxConfig()
		}
		s.TaxConfig.FilingStatus = models.FilingStatus(*o.FilingStatus)
	}
	return s, nil
}

func (o Overrides) validate() error {
	if o.MonthlyLivingExpenses != nil && *o.MonthlyLivingExpenses < 0 {
		return fmt.Errorf("monthly_living_expenses must be >= 0, got %v", *o.MonthlyLivingExpenses)
	}
	if o.RothConversionAmount != nil && *o.RothConversionAmount < 0 {
		return fmt.Errorf("roth_conversion_amount must be >= 0, got %v", *o.RothConversionAmount)
	}
	if o.ProjectionYears != nil && (*o.ProjectionYears < 1 || *o.ProjectionYears > 60) {
		return fmt.Errorf("projection_years must be between 1 and 60, got %d", *o.ProjectionYears)
	}
	if o.RothConversionStart != nil && o.RothConversionEnd != nil && *o.RothConversionEnd != 0 &&
		*o.RothConversionEnd < *o.RothConversionStart {
		return fmt.Errorf(
			"roth_conversion_end_year (%d) must not be before roth_conversion_start_year (%d): this window would run zero conversions in every year",
			*o.RothConversionEnd, *o.RothConversionStart)
	}
	if a := o.SocialSecurityClaimAge; a != nil && (*a < 62 || *a > 70) {
		return fmt.Errorf("social_security_claim_age must be between 62 and 70, got %d", *a)
	}
	if a := o.SpouseClaimAge; a != nil && (*a < 62 || *a > 70) {
		return fmt.Errorf("spouse_claim_age must be between 62 and 70, got %d", *a)
	}
	if f := o.FilingStatus; f != nil {
		switch models.FilingStatus(*f) {
		case models.FilingSingle, models.FilingMarriedJoint,
			models.FilingMarriedSeparate, models.FilingHeadOfHousehold:
		default:
			return fmt.Errorf("filing_status %q is not one of single, married_joint, married_separate, head_of_household", *f)
		}
	}
	return nil
}

// preparedWithOverrides applies the overrides and prepares the result for the
// engine. prepare.From runs its own DeepCopy internally, which drops
// PerYearOverrides (json:"-") a second time even though Apply already
// re-attached it once. Re-attach it again onto the prepared snapshot,
// following the same shape as analysis/tax_optimizer.go's
// cloneSettingsWithSSAndRoth (lines 90-100): the override map is
// reconstructed in-memory and never persisted, so this is a deliberate,
// narrow mutation of the prepared snapshot rather than a violation of the
// "don't mutate base" rule (base itself is untouched).
func preparedWithOverrides(base *models.WhatIfSettings, o Overrides) (prepare.PreparedSettings, error) {
	s, err := Apply(base, o)
	if err != nil {
		return prepare.PreparedSettings{}, err
	}
	prepared, err := prepare.From(s)
	if err != nil {
		return prepare.PreparedSettings{}, fmt.Errorf("prepare settings: %w", err)
	}
	if base.RothConversion != nil && base.RothConversion.PerYearOverrides != nil {
		if prepSettings := prepared.Settings(); prepSettings != nil && prepSettings.RothConversion != nil {
			prepSettings.RothConversion.PerYearOverrides = base.RothConversion.PerYearOverrides
		}
	}
	return prepared, nil
}

// RunWithOverrides applies the overrides and runs the full analysis, returning
// the shaped view. Monte Carlo is excluded: the orchestrator auto-seeds its RNG
// from the clock, so including it would make two identical runs disagree.
func RunWithOverrides(base *models.WhatIfSettings, o Overrides) (AnalysisView, error) {
	prepared, err := preparedWithOverrides(base, o)
	if err != nil {
		return AnalysisView{}, err
	}
	a := retirement.RunFull(engine.New(), engine.Input{Prepared: prepared})
	return ShapeAnalysis(a, false), nil
}
