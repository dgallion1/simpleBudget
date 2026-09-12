package engine

import (
	"fmt"
	"math"

	"budget2/internal/models"
)

const (
	socialSecurityRate         = 0.062
	medicareRate               = 0.0145
	additionalMedicareRate     = 0.009
	socialSecurityWageBase2026 = 184500.0
)

type PayrollYearState struct {
	Year                       int
	SocialSecurityWagesByOwner map[string]float64
	MedicareWages              float64
	AdditionalMedicarePaid     float64
}

func clonePayrollYearState(in PayrollYearState) PayrollYearState {
	out := in
	if in.SocialSecurityWagesByOwner == nil {
		return out
	}
	out.SocialSecurityWagesByOwner = map[string]float64{}
	for k, v := range in.SocialSecurityWagesByOwner {
		out.SocialSecurityWagesByOwner[k] = v
	}
	return out
}

type PayrollMonth struct {
	SocialSecurityTax, MedicareTax, AdditionalMedicareTax, Total float64
	Next                                                         PayrollYearState
}

func additionalMedicareThreshold(status models.FilingStatus) float64 {
	switch status {
	case models.FilingMarriedJoint:
		return 250000
	case models.FilingMarriedSeparate:
		return 125000
	default:
		return 200000
	}
}

func CalculatePayrollTaxes(s *models.WhatIfSettings, year int, wages map[string]float64, opening PayrollYearState) (PayrollMonth, error) {
	out := PayrollMonth{}
	if year < 2026 {
		return out, fmt.Errorf("unsupported payroll policy year %d", year)
	}
	next := PayrollYearState{Year: year, SocialSecurityWagesByOwner: map[string]float64{}, MedicareWages: opening.MedicareWages, AdditionalMedicarePaid: opening.AdditionalMedicarePaid}
	if opening.Year == year {
		for k, v := range opening.SocialSecurityWagesByOwner {
			next.SocialSecurityWagesByOwner[k] = v
		}
	} else {
		next.MedicareWages = 0
		next.AdditionalMedicarePaid = 0
	}
	for owner, w := range wages {
		if math.IsNaN(w) || math.IsInf(w, 0) || w < 0 {
			return out, fmt.Errorf("payroll wages must be finite and nonnegative")
		}
		prior := next.SocialSecurityWagesByOwner[owner]
		subject := math.Min(w, math.Max(0, socialSecurityWageBase2026-prior))
		out.SocialSecurityTax += subject * socialSecurityRate
		next.SocialSecurityWagesByOwner[owner] = prior + w
		next.MedicareWages += w
		out.MedicareTax += w * medicareRate
	}
	threshold := additionalMedicareThreshold(s.TaxConfig.FilingStatus)
	liability := math.Max(0, next.MedicareWages-threshold) * additionalMedicareRate
	out.AdditionalMedicareTax = math.Max(0, liability-next.AdditionalMedicarePaid)
	next.AdditionalMedicarePaid += out.AdditionalMedicareTax
	out.Total = out.SocialSecurityTax + out.MedicareTax + out.AdditionalMedicareTax
	out.Next = next
	return out, nil
}
