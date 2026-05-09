package completeness

import (
	"testing"

	"budget2/internal/models"
)

func TestCheck_StateTaxUnset(t *testing.T) {
	cases := []struct {
		name      string
		settings  *models.WhatIfSettings
		wantCode  string
		wantFound bool
	}{
		{
			name:      "nil TaxConfig emits state_tax_unset",
			settings:  &models.WhatIfSettings{TaxConfig: nil},
			wantCode:  codeStateTaxUnset,
			wantFound: true,
		},
		{
			name: "zero StateIncomeTaxRate emits state_tax_unset",
			settings: &models.WhatIfSettings{
				TaxConfig: &models.TaxConfig{StateIncomeTaxRate: 0.0},
			},
			wantCode:  codeStateTaxUnset,
			wantFound: true,
		},
		{
			name: "non-zero StateIncomeTaxRate emits no state_tax finding",
			settings: &models.WhatIfSettings{
				TaxConfig: &models.TaxConfig{StateIncomeTaxRate: 5.0},
			},
			wantCode:  codeStateTaxUnset,
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.settings)
			if got := hasCode(findings, tc.wantCode); got != tc.wantFound {
				t.Fatalf("Check() finding %q present = %v, want %v (got %d findings)",
					tc.wantCode, got, tc.wantFound, len(findings))
			}
		})
	}
}

func TestCheck_StateTaxFindingShape(t *testing.T) {
	settings := &models.WhatIfSettings{TaxConfig: nil}
	findings := Check(settings)

	f := findByCode(findings, codeStateTaxUnset)
	if f == nil {
		t.Fatal("expected state_tax_unset finding, got none")
	}
	if f.Severity != SeverityWarn {
		t.Errorf("Severity = %v, want SeverityWarn", f.Severity)
	}
	if f.Title == "" || f.Detail == "" || f.Action == "" {
		t.Errorf("Finding has empty user-facing fields: %+v", f)
	}
}

func hasCode(findings []Finding, code string) bool {
	return findByCode(findings, code) != nil
}

func findByCode(findings []Finding, code string) *Finding {
	for i := range findings {
		if findings[i].Code == code {
			return &findings[i]
		}
	}
	return nil
}

func TestCheck_SSUnconfigured(t *testing.T) {
	cases := []struct {
		name      string
		settings  *models.WhatIfSettings
		wantFound bool
	}{
		{
			name: "nil SocialSecurity with primary age >= 50 emits ss_unconfigured",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"}, // age ~56 in 2026
				},
				SocialSecurity: nil,
			},
			wantFound: true,
		},
		{
			name: "nil SocialSecurity with primary age 30 does not emit ss_unconfigured",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1996-01"}, // age ~30 in 2026
				},
				SocialSecurity: nil,
			},
			wantFound: false,
		},
		{
			name: "primary age 50 (boundary) emits ss_unconfigured",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1976-06"}, // age 50 by year-only arithmetic
				},
				SocialSecurity: nil,
			},
			wantFound: true,
		},
		{
			name: "configured SocialSecurity does not emit ss_unconfigured",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
				},
				SocialSecurity: &models.SocialSecurityConfig{
					FRABenefit: 2500,
					ClaimAge:   67,
				},
			},
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.settings)
			if got := hasCode(findings, codeSSUnconfigured); got != tc.wantFound {
				t.Fatalf("ss_unconfigured present = %v, want %v", got, tc.wantFound)
			}
		})
	}
}

func TestCheck_SSPartial(t *testing.T) {
	cases := []struct {
		name      string
		settings  *models.WhatIfSettings
		wantFound bool
	}{
		{
			name: "FRABenefit set, ClaimAge zero emits ss_partial",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons:   []models.Person{{Role: models.PersonRolePrimary, BirthMonth: "1970-01"}},
				SocialSecurity: &models.SocialSecurityConfig{
					FRABenefit: 2500,
					ClaimAge:   0,
				},
			},
			wantFound: true,
		},
		{
			name: "SpouseFRABenefit set, SpouseClaimAge zero emits ss_partial",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
					{Role: models.PersonRoleSpouse, BirthMonth: "1972-01"},
				},
				SocialSecurity: &models.SocialSecurityConfig{
					FRABenefit:       2500,
					ClaimAge:         67,
					SpouseFRABenefit: 1800,
					SpouseClaimAge:   0,
				},
			},
			wantFound: true,
		},
		{
			name: "fully configured SS does not emit ss_partial",
			settings: &models.WhatIfSettings{
				StartDate: "2026-01",
				Persons:   []models.Person{{Role: models.PersonRolePrimary, BirthMonth: "1970-01"}},
				SocialSecurity: &models.SocialSecurityConfig{
					FRABenefit: 2500,
					ClaimAge:   67,
				},
			},
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.settings)
			if got := hasCode(findings, codeSSPartial); got != tc.wantFound {
				t.Fatalf("ss_partial present = %v, want %v", got, tc.wantFound)
			}
		})
	}
}

func TestCheck_MFJNoSpousePerson(t *testing.T) {
	cases := []struct {
		name      string
		settings  *models.WhatIfSettings
		wantFound bool
	}{
		{
			name: "MFJ filing with no spouse Person emits mfj_no_spouse_person",
			settings: &models.WhatIfSettings{
				TaxConfig: &models.TaxConfig{
					FilingStatus:       models.FilingMarriedJoint,
					StateIncomeTaxRate: 5.0,
				},
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
				},
			},
			wantFound: true,
		},
		{
			name: "MFJ filing with spouse Person does not emit",
			settings: &models.WhatIfSettings{
				TaxConfig: &models.TaxConfig{
					FilingStatus:       models.FilingMarriedJoint,
					StateIncomeTaxRate: 5.0,
				},
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
					{Role: models.PersonRoleSpouse, BirthMonth: "1972-01"},
				},
			},
			wantFound: false,
		},
		{
			name: "Single filing with no spouse Person does not emit",
			settings: &models.WhatIfSettings{
				TaxConfig: &models.TaxConfig{
					FilingStatus:       models.FilingSingle,
					StateIncomeTaxRate: 5.0,
				},
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
				},
			},
			wantFound: false,
		},
		{
			name: "nil TaxConfig does not emit (no filing status to compare)",
			settings: &models.WhatIfSettings{
				TaxConfig: nil,
				Persons: []models.Person{
					{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
				},
			},
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.settings)
			if got := hasCode(findings, codeMFJNoSpousePerson); got != tc.wantFound {
				t.Fatalf("mfj_no_spouse_person present = %v, want %v", got, tc.wantFound)
			}
		})
	}
}

func TestCheck_NilSettings(t *testing.T) {
	findings := Check(nil)
	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 finding for nil settings, got %d", len(findings))
	}
	f := findings[0]
	if f.Code != codeSettingsNil {
		t.Errorf("Code = %q, want %q", f.Code, codeSettingsNil)
	}
	if f.Severity != SeverityError {
		t.Errorf("Severity = %v, want SeverityError", f.Severity)
	}
}

func TestCheck_OrderingErrorsFirst(t *testing.T) {
	settings := &models.WhatIfSettings{
		TaxConfig: &models.TaxConfig{
			FilingStatus:       models.FilingMarriedJoint,
			StateIncomeTaxRate: 0, // triggers Warn
		},
		Persons: []models.Person{
			{Role: models.PersonRolePrimary, BirthMonth: "1970-01"},
		},
	}
	findings := Check(settings)

	// Both findings should be present.
	if !hasCode(findings, codeMFJNoSpousePerson) || !hasCode(findings, codeStateTaxUnset) {
		t.Fatalf("expected both findings, got: %+v", findings)
	}
	// Errors must come before warns.
	if findings[0].Code != codeMFJNoSpousePerson {
		t.Errorf("expected mfj_no_spouse_person first, got %q", findings[0].Code)
	}
}

func TestDefaultWhatIfSettings_NoErrorFindings(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	findings := Check(settings)

	for _, f := range findings {
		if f.Severity == SeverityError {
			t.Errorf("DefaultWhatIfSettings should produce no SeverityError findings, got: code=%s title=%q", f.Code, f.Title)
		}
	}
}
