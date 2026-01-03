package models

import (
	"math"

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
	CurrentAge            int          `json:"current_age"`
	CurrentCoverage       CoverageType `json:"current_coverage"`
	CurrentMonthlyCost    float64      `json:"current_monthly_cost"`
	PreMedicareInflation  float64      `json:"pre_medicare_inflation"`  // Annual % (e.g., 7 for 7%)
	MedicareMonthlyCost   float64      `json:"medicare_monthly_cost"`   // Cost when turning 65
	PostMedicareInflation float64      `json:"post_medicare_inflation"` // Annual % (e.g., 4 for 4%)
	MedicareEligibleAge   int          `json:"medicare_eligible_age"`   // Usually 65
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
		hp.CurrentMonthlyCost = 459    // Part B + Medigap G + Part D
		hp.MedicareMonthlyCost = 459
		hp.PreMedicareInflation = 4.0  // Not applicable, but set reasonable default
	case CoverageACA:
		hp.CurrentMonthlyCost = 1100   // ACA marketplace
		hp.PreMedicareInflation = 7.0  // 4% healthcare + 3% age-rating
		hp.MedicareMonthlyCost = 600   // Projected Medicare cost at 65
	case CoverageEmployer:
		hp.CurrentMonthlyCost = 500    // Employer-subsidized
		hp.PreMedicareInflation = 5.0  // Healthcare inflation + some increase
		hp.MedicareMonthlyCost = 500   // Projected Medicare cost at 65
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

// GetMonthlyCost calculates the healthcare cost for a given month in the projection
// month 0 = current month, month 12 = 1 year from now, etc.
func (hp *HealthcarePerson) GetMonthlyCost(month int) float64 {
	yearsElapsed := month / 12
	ageAtMonth := hp.CurrentAge + yearsElapsed

	// Check if person transitions to Medicare during this projection
	if hp.CurrentCoverage != CoverageMedicare && ageAtMonth >= hp.MedicareEligibleAge {
		// Calculate when they hit Medicare eligibility
		yearsUntilMedicare := hp.MedicareEligibleAge - hp.CurrentAge
		if yearsUntilMedicare < 0 {
			yearsUntilMedicare = 0
		}

		// Years on Medicare after transition
		yearsOnMedicare := yearsElapsed - yearsUntilMedicare

		// Medicare cost with post-Medicare inflation
		return hp.MedicareMonthlyCost * math.Pow(1+hp.PostMedicareInflation/100, float64(yearsOnMedicare))
	}

	// Already on Medicare
	if hp.CurrentCoverage == CoverageMedicare {
		return hp.CurrentMonthlyCost * math.Pow(1+hp.PostMedicareInflation/100, float64(yearsElapsed))
	}

	// Pre-Medicare (ACA or Employer)
	return hp.CurrentMonthlyCost * math.Pow(1+hp.PreMedicareInflation/100, float64(yearsElapsed))
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
	monthBeforeMedicare := (yearsUntil * 12) - 1
	if monthBeforeMedicare < 0 {
		monthBeforeMedicare = 0
	}
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
			CurrentMonthlyCost:    459,  // Part B + Medigap G + Part D
			PreMedicareInflation:  4.0,  // N/A but set reasonable
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
