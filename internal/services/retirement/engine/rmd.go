package engine

import (
	"time"

	"budget2/internal/models"
)

// RMDStartAge is the SECURE 2.0 RMD applicable age for the pre-2033
// cohort. Kept exported for legacy callers (and the SECURE 2.0
// boundary helper effectiveRMDStartAgeForBirthYear).
const RMDStartAge = 73

// EffectiveRMDStartAge returns the SECURE 2.0 RMD applicable age for
// the older person in the household. Per SECURE 2.0 §107 and IRS
// Notice 2023-23, the applicable age is determined by the calendar
// year the person attains age 73:
//
//   - Attains age 73 in 2032 or earlier (born 1959 or earlier) → 73
//   - Attains age 73 in 2033 or later  (born 1960 or later)   → 75
//
// The older spouse drives the household's RMD timing. F-077 fixup:
// when any Person.BirthMonth is set, derive the birth year directly
// from it — the floor'd integer ages on WhatIfSettings (CurrentAge /
// SpouseAge) read 1 year low whenever the birthday hasn't yet occurred
// in StartDate's calendar year, which silently pushed people born late
// in 1959 onto the post-2032 (age 75) rule. Falls back to startYear -
// GetOlderAge() only for legacy callers that build settings without
// populating Persons.
func EffectiveRMDStartAge(s *models.WhatIfSettings) int {
	if s == nil {
		return 73
	}
	if year, ok := earliestPersonBirthYear(s); ok {
		return effectiveRMDStartAgeForBirthYear(year)
	}
	startYear := ParseStartYear(s.StartDate)
	olderBirthYear := startYear - s.GetOlderAge()
	return effectiveRMDStartAgeForBirthYear(olderBirthYear)
}

// earliestPersonBirthYear returns the earliest (oldest) parseable
// BirthMonth year across primary and spouse. Only the year matters
// for the SECURE 2.0 cusp, and the older person — the one with the
// earlier birth year — drives the household's RMD timing.
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

// effectiveRMDStartAgeForBirthYear returns the SECURE 2.0 RMD
// applicable age for a person born in the given calendar year.
// Boundary: birth year ≥ 1960 → 75 (attains 73 in 2033 or later);
// otherwise → 73.
func effectiveRMDStartAgeForBirthYear(birthYear int) int {
	if birthYear+73 >= 2033 {
		return 75
	}
	return 73
}

// olderBirthYear returns the older household member's birth year.
// Prefers the BirthMonth on Person records; falls back to startYear -
// GetOlderAge() for legacy callers that build settings without
// populating Persons. The older person — the one with the earlier
// birth year — drives the household's RMD timing per SECURE 2.0.
//
// MODELLING ASSUMPTION (F-4): tax-deferred savings are a single household
// pool with no owner attribution, and the older member drives both the first
// RMD year (here) and the Uniform Lifetime divisor (RMDAgeForCalendarYear).
// In reality RMDs are per-account and keyed to the account OWNER's age.
//
// Where this bites: a household whose tax-deferred money belongs entirely to
// the YOUNGER spouse. The model starts RMDs from the older spouse's age —
// potentially close to a decade early — and applies a smaller divisor, so it
// overstates forced distributions, taxable income, and the resulting IRMAA
// and tax drag. Plans in that shape read as more RMD-constrained than they
// are, which biases the Roth-conversion case upward.
//
// Households where both spouses hold tax-deferred balances, or where the
// older spouse holds most of it, are modelled correctly. Fixing the general
// case needs per-person ownership on tax-deferred balances, which the
// settings model does not currently carry.
func olderBirthYear(s *models.WhatIfSettings) int {
	if s == nil {
		return time.Now().Year() - 73
	}
	if y, ok := earliestPersonBirthYear(s); ok {
		return y
	}
	return ParseStartYear(s.StartDate) - s.GetOlderAge()
}

// FirstRMDCalendarYear returns the first calendar year in which the
// older household member must take an RMD under SECURE 2.0. Equals
// the older person's birth year + their applicable age (73 or 75).
// Anchors all calendar-year RMD gating so floor'd integer ages can't
// slip the first RMD year by one for late-year births.
func FirstRMDCalendarYear(s *models.WhatIfSettings) int {
	return olderBirthYear(s) + EffectiveRMDStartAge(s)
}

// RMDApplies reports whether RMD applies to the older household
// member in the given calendar year.
func RMDApplies(s *models.WhatIfSettings, calendarYear int) bool {
	return calendarYear >= FirstRMDCalendarYear(s)
}

// RMDAgeForCalendarYear returns the age the older household member
// attains by December 31 of the given calendar year. This is the age
// the IRS Uniform Lifetime Table is keyed off, so it's the age that
// must be passed to CalculateRMD — not the start-of-year floor'd age
// that GetOlderAge() returns.
func RMDAgeForCalendarYear(s *models.WhatIfSettings, calendarYear int) int {
	return calendarYear - olderBirthYear(s)
}

// RMDTriggerMonth returns the month-of-year (0-11) at which the full
// annual RMD is withdrawn for the given timing. F-074: the
// projection applies the entire year's RMD as a single monthly amount
// in the trigger month and zero in the others, so user-selected
// timing actually shapes portfolio growth (early withdrawal = more
// years lost to growth drag).
func RMDTriggerMonth(timing models.RMDTiming) int {
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

// ParseStartYear extracts the year from a "YYYY-MM" start date
// string. Returns the current year if the string is empty or
// unparseable.
func ParseStartYear(startDate string) int {
	if startDate == "" {
		return time.Now().Year()
	}
	t, err := time.Parse("2006-01", startDate)
	if err != nil {
		return time.Now().Year()
	}
	return t.Year()
}

// uniformLifetimeTable contains IRS Uniform Lifetime Table factors.
// Used when the sole beneficiary is not a spouse more than 10 years
// younger. Source: IRS Publication 590-B, Table III.
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

// GetLifeExpectancyFactor returns the IRS Uniform Lifetime Table
// factor for a given age.
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

// CalculateRMD calculates the Required Minimum Distribution for a
// given balance and age.
func CalculateRMD(taxDeferredBalance float64, age int) (amount float64, percent float64) {
	factor := GetLifeExpectancyFactor(age)
	if factor == 0 {
		return 0, 0
	}
	amount = taxDeferredBalance / factor
	percent = (1.0 / factor) * 100
	return amount, percent
}

// latestPersonBirthYear returns the latest (youngest) parseable BirthMonth
// year across primary and spouse — the mirror of earliestPersonBirthYear. The
// younger person is the sole-beneficiary spouse for Joint Life Table II.
func latestPersonBirthYear(s *models.WhatIfSettings) (int, bool) {
	candidates := []*models.Person{s.GetPrimaryPerson(), s.GetSpousePerson()}
	latest := 0
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
		if !found || y > latest {
			latest = y
			found = true
		}
	}
	return latest, found
}

// youngerBirthYear returns the younger household member's birth year, mirroring
// olderBirthYear. Prefers the latest parseable Person.BirthMonth; falls back to
// ParseStartYear(StartDate) - GetYoungerAge() for legacy callers that build
// settings without populating Persons. The younger member is the Joint Life
// Table II beneficiary spouse.
func youngerBirthYear(s *models.WhatIfSettings) int {
	if s == nil {
		return time.Now().Year() - 73
	}
	if y, ok := latestPersonBirthYear(s); ok {
		return y
	}
	return ParseStartYear(s.StartDate) - s.GetYoungerAge()
}

// UsesJointLifeTable reports whether RMDs for this household use the IRS Joint
// and Last Survivor Table (Table II) rather than the Uniform Lifetime Table.
// It applies when the user keeps the spouse-sole-beneficiary setting on
// (default), the household has a spouse, and the younger member is more than 10
// years younger than the older (owner) member — a birth-year gap of at least 11
// per 26 CFR 1.401(a)(9)-9(d). The result is year-independent (the gap and the
// setting do not vary by year), so the RMD display can surface it directly.
func UsesJointLifeTable(s *models.WhatIfSettings) bool {
	if s == nil || !s.IsSpouseSoleBeneficiary() || !s.HasSpouse() {
		return false
	}
	return youngerBirthYear(s)-olderBirthYear(s) >= 11
}

// RMDLifeFactor returns the RMD life-expectancy divisor for the household's
// tax-deferred pool in the given calendar year. It uses the Joint and Last
// Survivor Table (Table II) when UsesJointLifeTable reports the spouse is the
// sole beneficiary and >10 years younger, and otherwise the Uniform Lifetime
// Table — in which case the result is bit-for-bit GetLifeExpectancyFactor for
// the owner's attained age, preserving current behavior. Ages below 72 return
// 0 (no RMD) under either table, exactly as today.
func RMDLifeFactor(s *models.WhatIfSettings, calendarYear int) float64 {
	ownerAge := RMDAgeForCalendarYear(s, calendarYear)
	if ownerAge < 72 {
		return 0 // No RMD required (matches GetLifeExpectancyFactor).
	}
	if !UsesJointLifeTable(s) {
		return GetLifeExpectancyFactor(ownerAge)
	}
	spouseAge := calendarYear - youngerBirthYear(s)
	return jointLifeFactor(ownerAge, spouseAge)
}

// CalculateRMDForYear computes the RMD amount and percentage for the given
// tax-deferred balance and calendar year, selecting the Joint and Last
// Survivor Table or the Uniform Lifetime Table via RMDLifeFactor. It is the
// settings-aware analogue of CalculateRMD.
func CalculateRMDForYear(s *models.WhatIfSettings, taxDeferredBalance float64, calendarYear int) (amount float64, percent float64) {
	factor := RMDLifeFactor(s, calendarYear)
	if factor == 0 {
		return 0, 0
	}
	amount = taxDeferredBalance / factor
	percent = (1.0 / factor) * 100
	return amount, percent
}
