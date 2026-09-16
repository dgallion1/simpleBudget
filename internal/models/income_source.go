package models

import (
	"encoding/json"
	"math"
)

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
	Amount            float64    `json:"amount"` // Monthly amount
	Type              IncomeType `json:"income_type"`
	StartMonth        int        `json:"start_month"` // 0 = immediate
	EndMonth          *int       `json:"end_month"`   // nil = perpetual
	COLARate          float64    `json:"cola_rate"`   // Cost of living adjustment, e.g., 0.02 for 2% (decimal rate)
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

	if is.COLARate != 0 && monthsActive > 0 {
		return is.Amount * math.Pow(1+is.COLARate, float64(monthsActive)/12.0)
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

// The rollover-clamped states of a schedule entry.
//
// shiftScheduleOffsets re-anchors every offset when the plan's StartDate moves
// to the current month, flooring income/expense starts and ends at 0 so an
// entry whose window has passed is kept but never charged. The floor is LOSSY:
// the month the entry really started or ended is gone. These two predicates
// are the ONE place that names those states, so every surface — the source
// rows, the Budget Fit breakdown, the timeline events, the spending-funding
// markers — reads the same rule and none of them invents a month the clamp
// discarded (ruling 2026-09-16h). Value receivers: templates range over
// []IncomeSource / []ExpenseSource, whose elements are not addressable, so a
// pointer-receiver method would be invisible to text/template.

// ScheduleEnded reports whether the source has already finished: an end offset
// of 0 means "ends at month 0", i.e. it contributes nothing for the whole
// projection. Only the rollover clamp produces it — the calendar-month form
// stores a "Through" month as offset+1, so a user-set end is always >= 1 —
// which is why a row in this state has no honest end month to display.
func (is IncomeSource) ScheduleEnded() bool {
	return is.EndMonth != nil && *is.EndMonth <= 0
}

// SinceStart reports whether the source is already running at the start of the
// projection. It is true both for an entry the user scheduled for the plan's
// first month and for one the clamp pulled back to it; surfaces must therefore
// say "since plan start" rather than announcing a start that either never
// happens in the projection or happened before it.
func (is IncomeSource) SinceStart() bool {
	return is.StartMonth == 0
}

// ExpenseSource represents a planned expense for retirement planning.
//
// StartMonth/EndMonth are MONTH offsets from the plan's StartDate, the same
// shape (and the same end-exclusive convention) as IncomeSource. They replaced
// the year-granular StartYear/EndYear so that the monthly StartDate rollover
// can shift a schedule by one month without rounding it away; legacy documents
// carrying start_year/end_year are converted on decode by UnmarshalJSON.
type ExpenseSource struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Amount        float64 `json:"amount"`        // Monthly amount
	StartMonth    int     `json:"start_month"`   // Month offset from StartDate (0 = immediate)
	EndMonth      *int    `json:"end_month"`     // nil = perpetual; end exclusive
	Inflation     bool    `json:"inflation"`     // Whether to adjust for inflation
	Discretionary bool    `json:"discretionary"` // Can be reduced during market downturns
}

// UnmarshalJSON decodes an ExpenseSource, deriving StartMonth/EndMonth from
// the legacy year keys ONLY when the month key is absent from the document.
// A present month key always wins, so a mixed document (both keys) decodes to
// the month value rather than silently rounding to the year's.
func (es *ExpenseSource) UnmarshalJSON(data []byte) error {
	type alias ExpenseSource
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if _, present := raw["start_month"]; !present {
		var legacy struct {
			StartYear int `json:"start_year"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return err
		}
		out.StartMonth = legacy.StartYear * 12
	}

	if _, present := raw["end_month"]; !present {
		var legacy struct {
			EndYear int `json:"end_year"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return err
		}
		// Legacy end_year 0 meant "perpetual", which the month shape spells
		// as a nil EndMonth.
		if legacy.EndYear > 0 {
			endMonth := legacy.EndYear * 12
			out.EndMonth = &endMonth
		} else {
			out.EndMonth = nil
		}
	}

	*es = ExpenseSource(out)
	return nil
}

// ScheduleEnded reports whether the expense has already finished — see
// IncomeSource.ScheduleEnded for why an end offset of 0 is a rollover artifact
// with no displayable month behind it.
func (es ExpenseSource) ScheduleEnded() bool {
	return es.EndMonth != nil && *es.EndMonth <= 0
}

// SinceStart reports whether the expense is already running at the start of
// the projection — see IncomeSource.SinceStart.
func (es ExpenseSource) SinceStart() bool {
	return es.StartMonth == 0
}

// GetAdjustedAmount returns expense for a specific month with optional inflation
func (es *ExpenseSource) GetAdjustedAmount(month int, annualInflationRate float64) float64 {
	if es.Amount <= 0 {
		return 0
	}

	if month < es.StartMonth {
		return 0
	}
	if es.EndMonth != nil && month >= *es.EndMonth {
		return 0
	}

	amount := es.Amount
	if es.Inflation && annualInflationRate > 0 {
		monthsSinceStart := month - es.StartMonth
		amount *= math.Pow(1+annualInflationRate/100, float64(monthsSinceStart)/12.0)
	}
	return amount
}

// IsActive returns whether the expense is active in the given month
func (es *ExpenseSource) IsActive(month int) bool {
	if month < es.StartMonth {
		return false
	}
	if es.EndMonth != nil && month >= *es.EndMonth {
		return false
	}
	return true
}
