package retirement

import (
	"time"

	"budget2/internal/models"
)

// RMD start age per IRS rules (SECURE 2.0 Act)
const RMDStartAge = 73

// EffectiveRMDStartAge returns the SECURE 2.0 RMD applicable age for the
// older person in the household. Per SECURE 2.0 §107 and IRS Notice 2023-23,
// the applicable age is determined by the calendar year the person attains
// age 73:
//
//   - Attains age 73 in 2032 or earlier (born 1959 or earlier) → 73
//   - Attains age 73 in 2033 or later  (born 1960 or later)   → 75
//
// The older spouse drives the household's RMD timing. F-077 fixup: when any
// Person.BirthMonth is set, derive the birth year directly from it — the
// floor'd integer ages on WhatIfSettings (CurrentAge/SpouseAge) read 1 year
// low whenever the birthday hasn't yet occurred in StartDate's calendar year,
// which silently pushed people born late in 1959 onto the post-2032 (age 75)
// rule. Falls back to startYear - GetOlderAge() only for legacy callers that
// build settings without populating Persons.
func EffectiveRMDStartAge(s *models.WhatIfSettings) int {
	if s == nil {
		return 73
	}
	if year, ok := earliestPersonBirthYear(s); ok {
		return effectiveRMDStartAgeForBirthYear(year)
	}
	startYear := parseStartYear(s.StartDate)
	olderBirthYear := startYear - s.GetOlderAge()
	return effectiveRMDStartAgeForBirthYear(olderBirthYear)
}

// earliestPersonBirthYear returns the earliest (oldest) parseable BirthMonth
// year across primary and spouse. Only the year matters for the SECURE 2.0
// cusp, and the older person — the one with the earlier birth year — drives
// the household's RMD timing.
func earliestPersonBirthYear(s *models.WhatIfSettings) (int, bool) {
	candidates := []*models.Person{s.GetPrimaryPerson(), s.GetSpousePerson()}
	earliest := 0
	found := false
	for _, p := range candidates {
		if p == nil || p.BirthMonth == "" {
			continue
		}
		t, err := time.Parse("2006-01", p.BirthMonth)
		if err != nil {
			continue
		}
		y := t.Year()
		if !found || y < earliest {
			earliest = y
			found = true
		}
	}
	return earliest, found
}

// effectiveRMDStartAgeForBirthYear returns the SECURE 2.0 RMD applicable age
// for a person born in the given calendar year. Boundary: birth year ≥ 1960
// → 75 (attains 73 in 2033 or later); otherwise → 73.
func effectiveRMDStartAgeForBirthYear(birthYear int) int {
	if birthYear+73 >= 2033 {
		return 75
	}
	return 73
}

// olderBirthYear returns the older household member's birth year. Prefers
// the BirthMonth on Person records; falls back to startYear - GetOlderAge()
// for legacy callers that build settings without populating Persons. The
// older person — the one with the earlier birth year — drives the
// household's RMD timing per SECURE 2.0.
func olderBirthYear(s *models.WhatIfSettings) int {
	if s == nil {
		return time.Now().Year() - 73
	}
	if y, ok := earliestPersonBirthYear(s); ok {
		return y
	}
	return parseStartYear(s.StartDate) - s.GetOlderAge()
}

// FirstRMDCalendarYear returns the first calendar year in which the older
// household member must take an RMD under SECURE 2.0. Equals the older
// person's birth year + their applicable age (73 or 75). Anchors all
// calendar-year RMD gating so floor'd integer ages can't slip the first
// RMD year by one for late-year births.
func FirstRMDCalendarYear(s *models.WhatIfSettings) int {
	return olderBirthYear(s) + EffectiveRMDStartAge(s)
}

// RMDApplies reports whether RMD applies to the older household member in
// the given calendar year.
func RMDApplies(s *models.WhatIfSettings, calendarYear int) bool {
	return calendarYear >= FirstRMDCalendarYear(s)
}

// RMDAgeForCalendarYear returns the age the older household member attains
// by December 31 of the given calendar year. This is the age the IRS
// Uniform Lifetime Table is keyed off, so it's the age that must be passed
// to CalculateRMD — not the start-of-year floor'd age that GetOlderAge()
// returns.
func RMDAgeForCalendarYear(s *models.WhatIfSettings, calendarYear int) int {
	return calendarYear - olderBirthYear(s)
}

// rmdTriggerMonth returns the month-of-year (0-11) at which the full
// annual RMD is withdrawn for the given timing. F-074: the projection
// applies the entire year's RMD as a single monthly amount in the trigger
// month and zero in the others, so user-selected timing actually shapes
// portfolio growth (early withdrawal = more years lost to growth drag).
func rmdTriggerMonth(timing models.RMDTiming) int {
	switch models.NormalizeRMDTiming(timing) {
	case models.RMDTimingStartOfYear:
		return 0
	case models.RMDTimingEndOfYear:
		return 11
	default:
		// RMDTimingMidYear and any unknown value
		return 6
	}
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

// BuildRMDAnalysis (F-072) builds the RMD analysis from the actual projection
// instead of an isolated standalone math model. It samples each RMD year's
// starting tax-deferred balance and sums the actual RMDWithdrawal over the
// year, so the panel cannot diverge from the main projection.
func (c *Calculator) BuildRMDAnalysis(projection *models.ProjectionResult) *models.RMDAnalysis {
	s := c.Settings

	taxDeferredValue := s.PortfolioValue * (s.TaxDeferredPercent / 100)
	effectiveStartAge := EffectiveRMDStartAge(s)
	olderAge := s.GetOlderAge()
	startYear := parseStartYear(s.StartDate)
	firstRMDYear := FirstRMDCalendarYear(s)
	// F-078: startsInYears = calendar gap to first RMD year, not floor'd-age
	// subtraction. Late-year births differ by one.
	startsInYears := firstRMDYear - startYear
	if startsInYears < 0 {
		startsInYears = 0
	}

	result := &models.RMDAnalysis{
		StartsInYears:    startsInYears,
		StartAge:         effectiveStartAge,
		CurrentAge:       olderAge,
		TaxDeferredValue: taxDeferredValue,
		Projections:      []models.RMDProjection{},
	}

	if taxDeferredValue == 0 {
		return result
	}

	if projection == nil || len(projection.Months) == 0 {
		return result
	}

	// Surface depletion year (whole years from start; floor of months/12).
	if projection.DepletionMonth != nil {
		dy := *projection.DepletionMonth / 12
		// F-078: depletion age uses age-at-year-end of the depletion calendar
		// year so it stays consistent with the per-row Age column and the
		// projection engine's RMD trigger year for late-year births.
		da := RMDAgeForCalendarYear(s, startYear+dy)
		result.DepletionYear = &dy
		result.DepletionAge = &da
		if dy <= startsInYears {
			result.DepletedBeforeRMD = true
			return result
		}
	}

	// Iterate projection years, emit a row once RMDs apply to the older
	// household member for that calendar year (F-078 calendar-year gate).
	maxYears := s.ProjectionYears
	if maxYears > len(projection.Months)/12 {
		maxYears = len(projection.Months) / 12
	}

	rmdCount := 0
	for y := 0; y < maxYears && rmdCount < 20; y++ {
		calendarYear := startYear + y
		if !RMDApplies(s, calendarYear) {
			continue
		}
		age := RMDAgeForCalendarYear(s, calendarYear)
		// Stop at depletion year — no further rows.
		if result.DepletionYear != nil && y >= *result.DepletionYear {
			break
		}

		// Start-of-year tax-deferred balance.
		startIdx := 12*y - 1
		if y == 0 {
			startIdx = 0
		}
		if startIdx >= len(projection.Months) {
			break
		}
		startBalance := projection.Months[startIdx].TaxDeferredBalance

		// Sum actual RMDWithdrawal across the 12 months of this year.
		startMonth := 12 * y
		endMonth := startMonth + 12
		if endMonth > len(projection.Months) {
			endMonth = len(projection.Months)
		}
		rmdAmount := 0.0
		for m := startMonth; m < endMonth; m++ {
			rmdAmount += projection.Months[m].RMDWithdrawal
		}

		factor := GetLifeExpectancyFactor(age)
		rmdPercent := 0.0
		if factor > 0 {
			rmdPercent = 100.0 / factor
		}

		result.Projections = append(result.Projections, models.RMDProjection{
			Age:            age,
			Year:           y,
			TaxDeferredBal: startBalance,
			LifeExpFactor:  factor,
			RMDAmount:      rmdAmount,
			RMDPercent:     rmdPercent,
		})

		if rmdCount < 10 {
			result.TotalRMDsOver10Yr += rmdAmount
		}
		rmdCount++
	}

	return result
}
