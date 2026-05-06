package retirement

import (
	"math"
	"time"

	"budget2/internal/models"
)

// RMD start age per IRS rules (SECURE 2.0 Act)
const RMDStartAge = 73

// EffectiveRMDStartAge returns the SECURE 2.0 RMD start age for the
// projection's start year. 73 for projections starting before 2033;
// 75 for projections starting 2033 or later (per SECURE 2.0 Act of 2022).
func EffectiveRMDStartAge(s *models.WhatIfSettings) int {
	if s == nil {
		return 73
	}
	year := parseStartYear(s.StartDate)
	if year >= 2033 {
		return 75
	}
	return 73
}

// parseStartYear extracts the year from a "YYYY-MM" start date string.
// Returns the current year if the string is empty or unparseable.
func parseStartYear(startDate string) int {
	if startDate == "" {
		return time.Now().Year()
	}
	t, err := time.Parse("2006-01", startDate)
	if err != nil {
		return time.Now().Year()
	}
	return t.Year()
}

// uniformLifetimeTable contains IRS Uniform Lifetime Table factors
// Used when the sole beneficiary is not a spouse more than 10 years younger
// Source: IRS Publication 590-B, Table III
var uniformLifetimeTable = map[int]float64{
	72:  27.4,
	73:  26.5,
	74:  25.5,
	75:  24.6,
	76:  23.7,
	77:  22.9,
	78:  22.0,
	79:  21.1,
	80:  20.2,
	81:  19.4,
	82:  18.5,
	83:  17.7,
	84:  16.8,
	85:  16.0,
	86:  15.2,
	87:  14.4,
	88:  13.7,
	89:  12.9,
	90:  12.2,
	91:  11.5,
	92:  10.8,
	93:  10.1,
	94:  9.5,
	95:  8.9,
	96:  8.4,
	97:  7.8,
	98:  7.3,
	99:  6.8,
	100: 6.4,
	101: 6.0,
	102: 5.6,
	103: 5.2,
	104: 4.9,
	105: 4.6,
	106: 4.3,
	107: 4.1,
	108: 3.9,
	109: 3.7,
	110: 3.5,
	111: 3.4,
	112: 3.3,
	113: 3.1,
	114: 3.0,
	115: 2.9,
	116: 2.8,
	117: 2.7,
	118: 2.5,
	119: 2.3,
	120: 2.0,
}

// GetLifeExpectancyFactor returns the IRS Uniform Lifetime Table factor for a given age
func GetLifeExpectancyFactor(age int) float64 {
	if age < 72 {
		return 0 // No RMD required
	}
	if factor, ok := uniformLifetimeTable[age]; ok {
		return factor
	}
	// For ages beyond 120, use minimum
	return 2.0
}

// CalculateRMD calculates the Required Minimum Distribution for a given balance and age
func CalculateRMD(taxDeferredBalance float64, age int) (amount float64, percent float64) {
	factor := GetLifeExpectancyFactor(age)
	if factor == 0 {
		return 0, 0
	}
	amount = taxDeferredBalance / factor
	percent = (1.0 / factor) * 100
	return amount, percent
}

// rmdGrowthFractions returns the fraction of annual growth applied before and
// after the RMD withdrawal, based on the configured timing.
func rmdGrowthFractions(timing models.RMDTiming) (before, after float64) {
	switch timing {
	case models.RMDTimingStartOfYear:
		return 0.0, 1.0
	case models.RMDTimingEndOfYear:
		return 1.0, 0.0
	default: // mid_year
		return 0.5, 0.5
	}
}

// CalculateRMDAnalysis generates RMD projections based on current settings
func (c *Calculator) CalculateRMDAnalysis() *models.RMDAnalysis {
	s := c.Settings

	// Calculate tax-deferred portion of portfolio
	taxDeferredValue := s.PortfolioValue * (s.TaxDeferredPercent / 100)

	// RMD uses OLDER person's age - whoever hits 73 first triggers RMD.
	// F-032: start age depends on projection start year per SECURE 2.0.
	effectiveStartAge := EffectiveRMDStartAge(s)
	olderAge := s.GetOlderAge()

	// Calculate years until RMDs begin
	startsInYears := effectiveStartAge - olderAge
	if startsInYears < 0 {
		startsInYears = 0
	}

	// Generate projections for the projection period
	projections := make([]models.RMDProjection, 0)
	totalRMDs10Yr := 0.0

	// Get effective return (either explicit or calculated from per-account allocation)
	// When InvestmentReturn=0, the projection uses per-account asset allocation blended returns
	investmentReturn := s.InvestmentReturn
	if investmentReturn == 0 {
		investmentReturn = s.GetExpectedReturnFromAllocation()
	}
	monthlyReturn := monthlyCompoundFactorFromPercent(investmentReturn) - 1
	currentBalance := taxDeferredValue

	// F-035: honour configured RMD timing.
	timing := models.NormalizeRMDTiming(s.RMDTiming)
	beforeFraction, afterFraction := rmdGrowthFractions(timing)

	rmdCount := 0
	for year := 0; year <= s.ProjectionYears && rmdCount < 20; year++ {
		age := olderAge + year

		// Only project RMDs for ages effectiveStartAge+
		if age >= effectiveStartAge {
			// F-035: apply pre-RMD growth fraction.
			preRMDBalance := currentBalance * math.Pow(1+monthlyReturn, 12*beforeFraction)

			factor := GetLifeExpectancyFactor(age)
			rmdAmount, rmdPercent := CalculateRMD(preRMDBalance, age)

			projections = append(projections, models.RMDProjection{
				Age:            age,
				Year:           year,
				TaxDeferredBal: preRMDBalance,
				LifeExpFactor:  factor,
				RMDAmount:      rmdAmount,
				RMDPercent:     rmdPercent,
			})

			if rmdCount < 10 {
				totalRMDs10Yr += rmdAmount
			}
			rmdCount++

			// Reduce balance by RMD, then apply post-RMD growth fraction.
			afterRMD := preRMDBalance - rmdAmount
			if afterRMD < 0 {
				afterRMD = 0
			}
			currentBalance = afterRMD * math.Pow(1+monthlyReturn, 12*afterFraction)
		} else {
			// Pre-RMD year: apply full year of growth.
			for m := 0; m < 12; m++ {
				currentBalance *= (1 + monthlyReturn)
			}
		}
	}

	return &models.RMDAnalysis{
		StartsInYears:     startsInYears,
		StartAge:          effectiveStartAge,
		CurrentAge:        olderAge, // Uses older person's age for RMD calculations
		TaxDeferredValue:  taxDeferredValue,
		Projections:       projections,
		TotalRMDsOver10Yr: totalRMDs10Yr,
	}
}
