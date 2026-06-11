package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// TestAnnualRMDForYear_JointLifeTable_E2E is an end-to-end check that the
// projection-loop RMD helper (AnnualRMDForYear → CalculateRMDForYear) applies
// the Joint and Last Survivor Table when the spouse is the sole beneficiary and
// more than 10 years younger, and falls back to the Uniform Lifetime Table when
// the setting is off. Household: owner born 1953, spouse born 1966 (13-year
// gap); the first RMD year is 2026 (owner attains age 73).
func TestAnnualRMDForYear_JointLifeTable_E2E(t *testing.T) {
	const balance = 1_810_000.0
	const eps = 1e-6
	mk := func() *models.WhatIfSettings {
		return &models.WhatIfSettings{
			StartDate: "2026-01",
			Persons: []models.Person{
				{Role: models.PersonRolePrimary, BirthMonth: "1953-06"},
				{Role: models.PersonRoleSpouse, BirthMonth: "1966-06"},
			},
		}
	}

	// Default (setting unset → ON): Joint Table II divisor 28.6 at owner age 73.
	got := AnnualRMDForYear(mk(), 0, balance)
	if want := balance / 28.6; math.Abs(got-want) > eps {
		t.Errorf("joint AnnualRMDForYear = %v, want %v (balance/28.6)", got, want)
	}

	// Setting off → Uniform divisor 26.5.
	off := mk()
	off.SpouseSoleBeneficiary = new(bool) // zero value: false
	gotU := AnnualRMDForYear(off, 0, balance)
	if want := balance / 26.5; math.Abs(gotU-want) > eps {
		t.Errorf("uniform AnnualRMDForYear = %v, want %v (balance/26.5)", gotU, want)
	}
}
