package engine

import (
	"testing"

	"budget2/internal/models"
)

func TestLifetimePayrollTraditionalAndRothRemainFICAWages(t *testing.T) {
	s := &models.WhatIfSettings{TaxConfig: &models.TaxConfig{FilingStatus: models.FilingMarriedJoint}}
	got, err := CalculatePayrollTaxes(s, 2026, map[string]float64{"a": 10000, "b": 20000}, PayrollYearState{})
	if err != nil {
		t.Fatal(err)
	}
	want := 30000 * (socialSecurityRate + medicareRate)
	if got.Total != want {
		t.Fatalf("payroll=%v want=%v", got.Total, want)
	}
}
func TestLifetimePayrollSocialSecurityCapPerOwnerAndAdditionalMedicareAnnual(t *testing.T) {
	s := &models.WhatIfSettings{TaxConfig: &models.TaxConfig{FilingStatus: models.FilingMarriedJoint}}
	opening := PayrollYearState{Year: 2026, SocialSecurityWagesByOwner: map[string]float64{"a": 180000, "b": 180000}, MedicareWages: 240000}
	got, err := CalculatePayrollTaxes(s, 2026, map[string]float64{"a": 10000, "b": 10000}, opening)
	if err != nil {
		t.Fatal(err)
	}
	if got.SocialSecurityTax != 9000*socialSecurityRate {
		t.Fatalf("SS=%v", got.SocialSecurityTax)
	}
	if got.AdditionalMedicareTax != 10000*additionalMedicareRate {
		t.Fatalf("additional=%v", got.AdditionalMedicareTax)
	}
}
