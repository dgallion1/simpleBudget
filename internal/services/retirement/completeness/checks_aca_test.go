package completeness

import (
	"testing"

	"budget2/internal/models"
)

func personOn(coverage models.CoverageType) models.HealthcarePerson {
	return models.HealthcarePerson{CurrentCoverage: coverage}
}

func TestCheck_ACAHouseholdSizeUnset(t *testing.T) {
	cases := []struct {
		name      string
		settings  *models.WhatIfSettings
		wantFound bool
	}{
		{
			name: "marketplace coverage with no household size",
			settings: &models.WhatIfSettings{
				HealthcarePersons: []models.HealthcarePerson{personOn(models.CoverageACA)},
			},
			wantFound: true,
		},
		{
			name: "marketplace coverage with a household size",
			settings: &models.WhatIfSettings{
				HealthcarePersons: []models.HealthcarePerson{personOn(models.CoverageACA)},
				ACA:               &models.ACAConfig{HouseholdSize: 2},
			},
			wantFound: false,
		},
		{
			name: "nobody on a marketplace plan",
			settings: &models.WhatIfSettings{
				HealthcarePersons: []models.HealthcarePerson{personOn(models.CoverageMedicare)},
			},
			wantFound: false,
		},
		{
			name:      "no healthcare configured at all",
			settings:  &models.WhatIfSettings{},
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasCode(Check(tc.settings), codeACAHouseholdSizeUnset); got != tc.wantFound {
				t.Errorf("hasCode(%s) = %v, want %v", codeACAHouseholdSizeUnset, got, tc.wantFound)
			}
		})
	}
}

func TestCheck_ACACreditUnset(t *testing.T) {
	marketplace := []models.HealthcarePerson{personOn(models.CoverageACA)}

	cases := []struct {
		name      string
		aca       *models.ACAConfig
		wantFound bool
		why       string
	}{
		{
			name:      "size known, credit missing",
			aca:       &models.ACAConfig{HouseholdSize: 2},
			wantFound: true,
			why:       "the cliff can be located but not priced",
		},
		{
			name:      "size and credit known",
			aca:       &models.ACAConfig{HouseholdSize: 2, AnnualPremiumTaxCredit: models.FloatPtr(9600)},
			wantFound: false,
		},
		{
			name:      "size unknown",
			aca:       nil,
			wantFound: false,
			why:       "the household-size finding already covers this case; two warnings for one gap is noise",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &models.WhatIfSettings{HealthcarePersons: marketplace, ACA: tc.aca}
			if got := hasCode(Check(s), codeACACreditUnset); got != tc.wantFound {
				t.Errorf("hasCode(%s) = %v, want %v (%s)",
					codeACACreditUnset, got, tc.wantFound, tc.why)
			}
		})
	}
}

// TestCheck_ACACOBRAForfeitsCredit — the trade-off most people do not know
// they are making. COBRA is employer coverage for credit purposes, so it
// forfeits the subsidy at any income.
func TestCheck_ACACOBRAForfeitsCredit(t *testing.T) {
	cobraOnly := &models.WhatIfSettings{
		HealthcarePersons: []models.HealthcarePerson{personOn(models.CoverageCOBRA)},
	}
	if !hasCode(Check(cobraOnly), codeACACOBRAForfeits) {
		t.Error("a household bridging to Medicare on COBRA should be told it forfeits the credit")
	}

	// Someone already on a marketplace plan has made the choice; no finding.
	mixed := &models.WhatIfSettings{
		HealthcarePersons: []models.HealthcarePerson{
			personOn(models.CoverageCOBRA), personOn(models.CoverageACA),
		},
	}
	if hasCode(Check(mixed), codeACACOBRAForfeits) {
		t.Error("no forfeiture finding when someone is already on a marketplace plan")
	}

	if hasCode(Check(&models.WhatIfSettings{}), codeACACOBRAForfeits) {
		t.Error("no COBRA finding for a household with no COBRA")
	}
}

func TestCheck_ACAFindingsAreWellFormed(t *testing.T) {
	s := &models.WhatIfSettings{
		HealthcarePersons: []models.HealthcarePerson{personOn(models.CoverageACA)},
	}
	f := findByCode(Check(s), codeACAHouseholdSizeUnset)
	if f == nil {
		t.Fatal("expected the household-size finding")
	}
	if f.Severity != SeverityWarn {
		t.Errorf("Severity = %v, want SeverityWarn", f.Severity)
	}
	if f.Title == "" || f.Detail == "" || f.Action == "" {
		t.Errorf("finding has empty user-facing fields: %+v", f)
	}
	// The anchor must match a real input id so the banner can deep-link.
	if f.FormAnchor != "aca-household-size-input" {
		t.Errorf("FormAnchor = %q; want it to match the form input id", f.FormAnchor)
	}
}
