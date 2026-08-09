package models

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// CoverageType represents the type of healthcare coverage
type CoverageType string

const (
	CoverageMedicare CoverageType = "medicare"
	CoverageACA      CoverageType = "aca"
	CoverageEmployer CoverageType = "employer"
)

// HealthcarePerson represents one person's healthcare costs and coverage
type HealthcarePerson struct {
	ID                    string       `json:"id"`
	Name                  string       `json:"name"`
	PersonID              string       `json:"person_id,omitempty"`
	CurrentAge            int          `json:"current_age"`
	CurrentCoverage       CoverageType `json:"current_coverage"`
	CurrentMonthlyCost    float64      `json:"current_monthly_cost"`
	PreMedicareInflation  float64      `json:"pre_medicare_inflation"`  // Annual % (e.g., 7 for 7%)
	MedicareMonthlyCost   float64      `json:"medicare_monthly_cost"`   // Cost when turning 65
	PostMedicareInflation float64      `json:"post_medicare_inflation"` // Annual % (e.g., 4 for 4%)
	MedicareEligibleAge   int          `json:"medicare_eligible_age"`   // Usually 65

	// F-067: birth month ("YYYY-MM") for month-precise ACA→Medicare transition.
	// When set, GetMonthlyCost uses exact birth-month arithmetic instead of
	// integer-year bucketing (which can be off by up to 11 months for
	// mid-year birthdays). Populated from the linked Person.BirthMonth.
	BirthMonth string `json:"birth_month,omitempty"`

	// Employer coverage transition fields
	EmployerCoverageYears int     `json:"employer_coverage_years"` // Years of remaining employer coverage (0 = indefinite until Medicare)
	ACACostAfterEmployer  float64 `json:"aca_cost_after_employer"` // Monthly ACA cost when employer coverage ends
}

func (hp HealthcarePerson) IsLinked() bool {
	return hp.PersonID != ""
}

// NewHealthcarePerson creates a new healthcare person with default values
func NewHealthcarePerson(name string, age int, coverage CoverageType) *HealthcarePerson {
	hp := &HealthcarePerson{
		ID:                    uuid.New().String(),
		Name:                  name,
		CurrentAge:            age,
		CurrentCoverage:       coverage,
		MedicareEligibleAge:   65,
		PostMedicareInflation: 4.0, // 4% default post-Medicare inflation
	}

	// Set defaults based on coverage type
	switch coverage {
	case CoverageMedicare:
		hp.CurrentMonthlyCost = 459 // Part B + Medigap G + Part D
		hp.MedicareMonthlyCost = 459
		hp.PreMedicareInflation = 4.0 // Not applicable, but set reasonable default
	case CoverageACA:
		hp.CurrentMonthlyCost = 1100  // ACA marketplace
		hp.PreMedicareInflation = 7.0 // 4% healthcare + 3% age-rating
		hp.MedicareMonthlyCost = 600  // Projected Medicare cost at 65
	case CoverageEmployer:
		hp.CurrentMonthlyCost = 500    // Employer-subsidized
		hp.PreMedicareInflation = 7.0  // ACA healthcare inflation (used after employer ends)
		hp.MedicareMonthlyCost = 600   // Projected Medicare cost at 65
		hp.EmployerCoverageYears = 0   // 0 = until Medicare
		hp.ACACostAfterEmployer = 1100 // ACA cost when employer coverage ends
	}

	return hp
}

// IsOnMedicare returns true if the person is currently on Medicare
func (hp *HealthcarePerson) IsOnMedicare() bool {
	return hp.CurrentCoverage == CoverageMedicare || hp.CurrentAge >= hp.MedicareEligibleAge
}

// YearsUntilMedicare returns years until Medicare eligibility (0 if already eligible)
func (hp *HealthcarePerson) YearsUntilMedicare() int {
	if hp.IsOnMedicare() {
		return 0
	}
	return hp.MedicareEligibleAge - hp.CurrentAge
}

// monthsUntilMedicareEligible returns the number of months from projection
// month 0 until this person becomes Medicare-eligible.
//
// F-067: when BirthMonth ("YYYY-MM") and startDate ("YYYY-MM") are both set,
// use month-precise arithmetic so that mid-year birthdays don't round to the
// wrong year (up to 11-month error with the year-based fallback).
func (hp *HealthcarePerson) monthsUntilMedicareEligible(startDate string) int {
	if hp.BirthMonth != "" && startDate != "" {
		birth, err := time.Parse("2006-01", hp.BirthMonth)
		if err == nil {
			start, err2 := time.Parse("2006-01", startDate)
			if err2 == nil {
				eligibleDate := birth.AddDate(hp.MedicareEligibleAge, 0, 0)
				months := (eligibleDate.Year()-start.Year())*12 + int(eligibleDate.Month()) - int(start.Month())
				if months < 0 {
					return 0
				}
				return months
			}
		}
	}
	// Legacy year-based fallback.
	yearsUntil := hp.MedicareEligibleAge - hp.CurrentAge
	if yearsUntil < 0 {
		return 0
	}
	return yearsUntil * 12
}

// MedicareStartMonth returns the projection month in which this person
// actually starts paying Medicare premiums. This is the single definition of
// that transition: GetMonthlyCostAt prices the coverage from it, and the
// engine decides IRMAA eligibility from it.
//
// Reaching 65 is not enough. Someone who keeps employer coverage past their
// eligible age is not enrolled and pays no Part B or Part D premium, so there
// is nothing for IRMAA to surcharge until that coverage lapses — which is why
// the employer branch below wins over the age-based transition (F-5).
//
// startDate ("YYYY-MM") enables month-precise arithmetic when BirthMonth is
// also set (F-067); otherwise the year-based fallback applies.
func (hp *HealthcarePerson) MedicareStartMonth(startDate string) int {
	if hp.CurrentCoverage == CoverageMedicare {
		return 0
	}
	monthsUntilMedicare := hp.monthsUntilMedicareEligible(startDate)
	if hp.CurrentCoverage == CoverageEmployer && hp.EmployerCoverageYears > 0 {
		if monthsOnEmployer := hp.EmployerCoverageYears * 12; monthsOnEmployer > monthsUntilMedicare {
			return monthsOnEmployer
		}
	}
	return monthsUntilMedicare
}

// GetMonthlyCost calculates the healthcare cost for a given month in the projection
// month 0 = current month, month 12 = 1 year from now, etc.
//
// Note: prefer GetMonthlyCostAt for month-precise F-067 ACA→Medicare transitions
// when hp.BirthMonth is set. This method uses the legacy year-based fallback.
func (hp *HealthcarePerson) GetMonthlyCost(month int) float64 {
	return hp.GetMonthlyCostAt(month, "")
}

// GetMonthlyCostAt calculates the healthcare cost for a given month.
// startDate ("YYYY-MM") enables month-precise ACA→Medicare transitions
// (F-067) when hp.BirthMonth is also set; otherwise falls back to the
// year-based legacy calculation.
func (hp *HealthcarePerson) GetMonthlyCostAt(month int, startDate string) float64 {
	yearsElapsedFloat := float64(month) / 12.0

	// F-067: use month-precise Medicare transition when BirthMonth+startDate are set.
	monthsUntilMedicare := hp.monthsUntilMedicareEligible(startDate)

	// Already on Medicare coverage type
	if hp.CurrentCoverage == CoverageMedicare {
		return hp.CurrentMonthlyCost * math.Pow(1+hp.PostMedicareInflation/100, yearsElapsedFloat)
	}

	// Employer coverage with limited duration - check this BEFORE age-based Medicare transition
	// This handles the case where someone is already past Medicare age but still has employer coverage
	if hp.CurrentCoverage == CoverageEmployer && hp.EmployerCoverageYears > 0 {
		monthsOnEmployer := hp.EmployerCoverageYears * 12
		// Still on employer coverage
		if month < monthsOnEmployer {
			return hp.CurrentMonthlyCost // Employer cost doesn't inflate (it's subsidized)
		}

		// Employer coverage ended. Medicare picks up at MedicareStartMonth —
		// immediately if already eligible, otherwise after an ACA gap.
		medicareStart := hp.MedicareStartMonth(startDate)
		if month < medicareStart {
			// ACA bridge between employer coverage and Medicare eligibility.
			return hp.ACACostAfterEmployer * math.Pow(1+hp.PreMedicareInflation/100, float64(month-monthsOnEmployer)/12.0)
		}
		return hp.MedicareMonthlyCost * math.Pow(1+hp.PostMedicareInflation/100, float64(month-medicareStart)/12.0)
	}

	// Check if person transitions to Medicare in this projection period (ACA or unlimited employer)
	if month >= monthsUntilMedicare {
		monthsOnMedicare := month - monthsUntilMedicare
		return hp.MedicareMonthlyCost * math.Pow(1+hp.PostMedicareInflation/100, float64(monthsOnMedicare)/12.0)
	}

	// Pre-Medicare (ACA or unlimited Employer)
	return hp.CurrentMonthlyCost * math.Pow(1+hp.PreMedicareInflation/100, yearsElapsedFloat)
}

// GetMonthlyCostWithVariation returns healthcare cost with Monte Carlo variation
// variation is a multiplier (e.g., 0.98 to 1.02 for +/- 2% variation)
func (hp *HealthcarePerson) GetMonthlyCostWithVariation(month int, variation float64) float64 {
	return hp.GetMonthlyCost(month) * variation
}

// GetTransitionInfo returns information about Medicare transition for display
func (hp *HealthcarePerson) GetTransitionInfo() (hasTransition bool, yearsUntil int, currentCostAtTransition float64, medicareCost float64) {
	if hp.IsOnMedicare() {
		return false, 0, 0, 0
	}

	yearsUntil = hp.YearsUntilMedicare()

	// Calculate cost just before Medicare transition
	// yearsUntil is always >= 1 here (IsOnMedicare returned false), so this is always >= 11
	currentCostAtTransition = hp.CurrentMonthlyCost * math.Pow(1+hp.PreMedicareInflation/100, float64(yearsUntil))

	// Medicare cost at transition (no inflation applied yet)
	medicareCost = hp.MedicareMonthlyCost

	return true, yearsUntil, currentCostAtTransition, medicareCost
}

// DefaultHealthcarePersons returns default healthcare persons for a typical scenario
// User (67, Medicare) + Spouse (54, ACA)
func DefaultHealthcarePersons() []HealthcarePerson {
	return []HealthcarePerson{
		{
			ID:                    uuid.New().String(),
			Name:                  "User",
			CurrentAge:            67,
			CurrentCoverage:       CoverageMedicare,
			CurrentMonthlyCost:    459, // Part B + Medigap G + Part D
			PreMedicareInflation:  4.0, // N/A but set reasonable
			MedicareMonthlyCost:   459,
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		},
		{
			ID:                    uuid.New().String(),
			Name:                  "Spouse",
			CurrentAge:            54,
			CurrentCoverage:       CoverageACA,
			CurrentMonthlyCost:    1100, // ACA marketplace
			PreMedicareInflation:  7.0,  // 4% healthcare + 3% age-rating
			MedicareMonthlyCost:   600,  // Projected 2037 dollars
			PostMedicareInflation: 4.0,
			MedicareEligibleAge:   65,
		},
	}
}
