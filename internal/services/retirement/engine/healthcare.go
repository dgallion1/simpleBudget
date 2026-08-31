package engine

import (
	"budget2/internal/models"
)

// HealthcarePV calculates the present value of healthcare costs for a
// single person. Handles the Medicare transition where costs and
// inflation rates change at age 65.
func HealthcarePV(person models.HealthcarePerson, discountRate float64, totalMonths int) float64 {
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

	pvTotal += carePV(person, discountRate, totalMonths)

	return pvTotal
}

// carePV discounts a person's late-life care cost stream back to present
// value. Every care dollar comes from person.CareCostAt — the one formula
// for care (models.HealthcarePerson.CareCostAt) — using the year-fallback
// ("") startDate precision this estimate panel accepts. Care doesn't fit
// presentValueAnnuity's growing-annuity shape: it pays nothing before care
// start and then compounds from month 0 (not from care start), so it can't
// be re-derived as a growth rate — it's summed month by month instead,
// using this file's existing monthly discounting convention
// (monthlyCompoundFactorFromPercent(discountRate)) and the same
// ordinary-annuity timing presentValueAnnuity uses above (the month-m
// payment is discounted by (1+monthlyRate)^(m+1), i.e. the first payment
// falls one month out).
func carePV(person models.HealthcarePerson, discountRate float64, totalMonths int) float64 {
	monthlyFactor := monthlyCompoundFactorFromPercent(discountRate)
	discount := monthlyFactor
	pv := 0.0
	for m := 0; m < totalMonths; m++ {
		if care := person.CareCostAt(m, ""); care > 0 {
			pv += care / discount
		}
		discount *= monthlyFactor
	}
	return pv
}
