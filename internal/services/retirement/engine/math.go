package engine

import "math"

// monthlyCompoundFactorFromDecimal converts an annual rate (decimal,
// e.g. 0.07 for 7%) to its monthly compounding factor.
//
// Duplicated from internal/services/retirement during the migration
// window so this package has no import cycle back to retirement. The
// duplicates are removed in Task 8 when calculator.go is deleted.
func monthlyCompoundFactorFromDecimal(annualRate float64) float64 {
	if annualRate == 0 {
		return 1.0
	}
	return math.Pow(1+annualRate, 1.0/12.0)
}

// monthlyCompoundFactorFromPercent converts an annual rate (percent,
// e.g. 7.0 for 7%) to its monthly compounding factor.
func monthlyCompoundFactorFromPercent(annualRatePercent float64) float64 {
	return monthlyCompoundFactorFromDecimal(annualRatePercent / 100)
}

// compoundedFactorFromPercent compounds a percent annual rate over a
// fractional number of months.
func compoundedFactorFromPercent(annualRatePercent float64, months float64) float64 {
	if annualRatePercent == 0 || months == 0 {
		return 1.0
	}
	return math.Pow(1+annualRatePercent/100, months/12.0)
}

// presentValueAnnuity calculates the PV of a series of payments
// (regular or growing). Lowercase mirror of the retirement-package
// PresentValueAnnuity used during the migration window.
func presentValueAnnuity(payment, discountRate, growthRate float64, startMonth, numPayments int) float64 {
	if numPayments <= 0 || payment == 0 {
		return 0
	}

	monthlyRate := monthlyCompoundFactorFromPercent(discountRate) - 1
	monthlyGrowth := monthlyCompoundFactorFromPercent(growthRate) - 1

	var pvAtStart float64

	if monthlyRate <= 0 {
		if monthlyGrowth == 0 {
			pvAtStart = payment * float64(numPayments)
		} else {
			total := 0.0
			for m := 0; m < numPayments; m++ {
				total += payment * math.Pow(1+monthlyGrowth, float64(m))
			}
			pvAtStart = total
		}
	} else if math.Abs(monthlyRate-monthlyGrowth) < 1e-10 {
		// Growth equals discount rate
		pvAtStart = payment * float64(numPayments)
	} else if monthlyGrowth != 0 {
		// Growing annuity formula
		growthFactor := (1 + monthlyGrowth) / (1 + monthlyRate)
		pvAtStart = payment * (1 - math.Pow(growthFactor, float64(numPayments))) / (monthlyRate - monthlyGrowth)
	} else {
		// Regular annuity formula
		pvAtStart = payment * (1 - math.Pow(1+monthlyRate, -float64(numPayments))) / monthlyRate
	}

	// Discount back if payments start in the future
	if startMonth > 0 && monthlyRate > 0 {
		return pvAtStart / math.Pow(1+monthlyRate, float64(startMonth))
	}

	return pvAtStart
}
