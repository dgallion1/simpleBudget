package engine

import (
	"budget2/internal/models"
)

// healthcarePV calculates the present value of healthcare costs for a
// single person. Handles the Medicare transition where costs and
// inflation rates change at age 65.
func healthcarePV(s *models.WhatIfSettings, person models.HealthcarePerson, discountRate float64, totalMonths int) float64 {
	pvTotal := 0.0

	if person.IsOnMedicare() {
		// Already on Medicare - simple PV calculation with post-Medicare inflation
		pvTotal = presentValueAnnuity(person.CurrentMonthlyCost, discountRate, person.PostMedicareInflation, 0, totalMonths)
	} else {
		// Pre-Medicare: calculate in two phases
		yearsUntilMedicare := person.YearsUntilMedicare()
		preMedicareMonths := yearsUntilMedicare * 12

		if preMedicareMonths >= totalMonths {
			// Entire projection is pre-Medicare
			pvTotal = presentValueAnnuity(person.CurrentMonthlyCost, discountRate, person.PreMedicareInflation, 0, totalMonths)
		} else {
			// Phase 1: Pre-Medicare period
			if preMedicareMonths > 0 {
				pvTotal += presentValueAnnuity(person.CurrentMonthlyCost, discountRate, person.PreMedicareInflation, 0, preMedicareMonths)
			}

			// Phase 2: Post-Medicare period
			postMedicareMonths := totalMonths - preMedicareMonths
			if postMedicareMonths > 0 {
				pvTotal += presentValueAnnuity(person.MedicareMonthlyCost, discountRate, person.PostMedicareInflation, preMedicareMonths, postMedicareMonths)
			}
		}
	}

	return pvTotal
}

// HealthcarePVForCalculator is a parity-window export so Calculator's
// delegator can call into engine. Removed in Task 8.
func HealthcarePVForCalculator(s *models.WhatIfSettings, person models.HealthcarePerson, discountRate float64, totalMonths int) float64 {
	return healthcarePV(s, person, discountRate, totalMonths)
}
