package models

import (
	"fmt"
	"strconv"
)

// RoundToCents rounds v to the 2-decimal precision every dollar figure is
// DISPLAYED at, using the exact same primitive Go performs when
// internal/templates' formatMoney does fmt.Sprintf("%.2f", v): a
// correctly-rounded conversion of v's actual IEEE-754 float64 value to a
// fixed 2-decimal decimal string, which is then parsed back to the nearest
// float64. This is NOT the same as a hand-rolled decimal-rounding rule
// like math.Round(v*100)/100 -- that computes a DIFFERENT intermediate
// float (v*100) and can disagree with fmt's rounding on real inputs (e.g.
// 2.675 -> fmt gives 2.67, math.Round(267.5...)/100 gives 2.68; 150.005 ->
// fmt gives 150.00, math.Round gives 150.01). Two formatters for one value
// WILL disagree (W2 defect class) -- so any code that needs a dollar
// figure's DISPLAYED value for further arithmetic (e.g. deriving a Change
// = current - previous from figures already shown to the user) must round
// through this exact function, not reimplement rounding.
//
// formatMoney itself is intentionally left untouched (wide blast radius,
// dozens of call sites): this performs the identical fmt.Sprintf("%.2f",
// v) primitive that formatMoney's own rounding step relies on, so a value
// rounded here and then rendered by formatMoney always displays unchanged.
func RoundToCents(v float64) float64 {
	r, err := strconv.ParseFloat(fmt.Sprintf("%.2f", v), 64)
	if err != nil {
		// fmt.Sprintf("%.2f", v) always produces a valid float literal for
		// any finite v; this is unreachable for real inputs (NaN/Inf format
		// as "NaN"/"+Inf", which also parse back fine). Defensive only.
		return v
	}
	return r
}
