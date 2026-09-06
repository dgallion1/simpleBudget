package templates

import (
	"fmt"
	"strconv"
	"strings"
)

// budgetGapRoundingAdjustment reconciles the displayed components to the
// displayed gap without changing the underlying budget. Expenses already include
// IRMAA and taxes already include NIIT. Parse formatMoney itself so half-cent
// boundaries use exactly the same formatting path as the visible amounts.
func budgetGapRoundingAdjustment(gap, expenses, income, rmd, taxes float64) (float64, error) {
	var cents [5]int64
	for i, value := range []float64{gap, expenses, income, rmd, taxes} {
		decimal := strings.NewReplacer("$", "", ",", "", ".", "").Replace(formatMoney(value))
		parsed, err := strconv.ParseInt(decimal, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("budget rounding adjustment: %w", err)
		}
		cents[i] = parsed
	}
	return float64(cents[0]-cents[1]+cents[2]+cents[3]-cents[4]) / 100, nil
}
