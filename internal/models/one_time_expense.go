package models

import "encoding/json"

// OneTimeExpense models a discrete future outlay (a new roof, a car, a
// wedding) that hits a single projection month, distinct from the recurring
// living expenses ExpenseSources models. Amount is expressed in TODAY's
// dollars; the engine inflates it to Month using the plan's general
// (CPI) InflationRate — not healthcare inflation, and not scaled by
// spending-phase multipliers.
//
// Month is a MONTH offset from the plan's StartDate (it replaced the
// year-granular Year so the monthly StartDate rollover can shift it without
// rounding it away). A NEGATIVE Month is a past, dormant entry: it is retained
// rather than deleted, and the projection loop never reaches it, so it is
// never charged. Legacy documents carrying "year" are converted on decode.
type OneTimeExpense struct {
	ID          string  `json:"id,omitempty"`
	Description string  `json:"description"`
	Month       int     `json:"month"`  // Month offset from StartDate (0 = first projection month)
	Amount      float64 `json:"amount"` // Today's dollars, >= 0
}

// UnmarshalJSON decodes a OneTimeExpense, deriving Month from the legacy
// "year" key ONLY when "month" is absent from the document. A present "month"
// key always wins over a legacy "year".
func (e *OneTimeExpense) UnmarshalJSON(data []byte) error {
	type alias OneTimeExpense
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if _, present := raw["month"]; !present {
		var legacy struct {
			Year int `json:"year"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return err
		}
		out.Month = legacy.Year * 12
	}

	*e = OneTimeExpense(out)
	return nil
}
