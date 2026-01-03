package models

import "math"

// IncomeType represents the type of income source
type IncomeType string

const (
	IncomeFixed     IncomeType = "fixed"     // Steady income (e.g., pension, social security)
	IncomeTemporary IncomeType = "temporary" // Income that ends after a period
	IncomeDelayed   IncomeType = "delayed"   // Income that starts in the future
	IncomeVariable  IncomeType = "variable"  // Variable/uncertain income
)

// IncomeSource represents a source of income for retirement planning
type IncomeSource struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Amount            float64    `json:"amount"`       // Monthly amount
	Type              IncomeType `json:"income_type"`
	StartMonth        int        `json:"start_month"`  // 0 = immediate
	EndMonth          *int       `json:"end_month"`    // nil = perpetual
	COLARate          float64    `json:"cola_rate"`    // Cost of living adjustment, e.g., 0.02 for 2%
	InflationAdjusted bool       `json:"inflation_adjusted"`
}

// GetAdjustedAmount returns income for a specific month with COLA applied
func (is *IncomeSource) GetAdjustedAmount(month int) float64 {
	if month < is.StartMonth {
		return 0
	}
	if is.EndMonth != nil && month >= *is.EndMonth {
		return 0
	}

	monthsActive := month - is.StartMonth
	yearsActive := monthsActive / 12

	if is.COLARate > 0 && yearsActive > 0 {
		return is.Amount * math.Pow(1+is.COLARate, float64(yearsActive))
	}
	return is.Amount
}

// IsActive returns whether the income source is active in the given month
func (is *IncomeSource) IsActive(month int) bool {
	if month < is.StartMonth {
		return false
	}
	if is.EndMonth != nil && month >= *is.EndMonth {
		return false
	}
	return true
}

// ExpenseSource represents a planned expense for retirement planning
type ExpenseSource struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Amount        float64 `json:"amount"`        // Monthly amount
	StartYear     int     `json:"start_year"`    // Year offset from now (0 = now)
	EndYear       int     `json:"end_year"`      // 0 = perpetual
	Inflation     bool    `json:"inflation"`     // Whether to adjust for inflation
	Discretionary bool    `json:"discretionary"` // Can be reduced during market downturns
}

// GetAdjustedAmount returns expense for a specific month with optional inflation
func (es *ExpenseSource) GetAdjustedAmount(month int, annualInflationRate float64) float64 {
	if es.Amount <= 0 {
		return 0
	}

	startMonth := es.StartYear * 12
	endMonth := es.EndYear * 12

	if month < startMonth {
		return 0
	}
	if es.EndYear > 0 && month >= endMonth {
		return 0
	}

	amount := es.Amount
	if es.Inflation && annualInflationRate > 0 {
		yearsSinceStart := (month - startMonth) / 12
		amount *= math.Pow(1+annualInflationRate/100, float64(yearsSinceStart))
	}
	return amount
}

// IsActive returns whether the expense is active in the given month
func (es *ExpenseSource) IsActive(month int) bool {
	startMonth := es.StartYear * 12
	endMonth := es.EndYear * 12

	if month < startMonth {
		return false
	}
	if es.EndYear > 0 && month >= endMonth {
		return false
	}
	return true
}
