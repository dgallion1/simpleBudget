package models

// OneTimeExpense models a discrete future outlay (a new roof, a car, a
// wedding) that hits a single projection year, distinct from the recurring
// living expenses ExpenseSources models. Amount is expressed in TODAY's
// dollars; the engine inflates it to Year using the plan's general
// (CPI) InflationRate — not healthcare inflation, and not scaled by
// spending-phase multipliers.
type OneTimeExpense struct {
	ID          string  `json:"id,omitempty"`
	Description string  `json:"description"`
	Year        int     `json:"year"`   // Projection year, 0-based (0 = first projection year)
	Amount      float64 `json:"amount"` // Today's dollars, >= 0
}
