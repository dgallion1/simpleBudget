// Package moneyfmt owns the server's exact two-decimal US-dollar rendering.
// It has no application dependencies so models and templates use one rule.
package moneyfmt

import (
	"fmt"
	"strconv"
	"strings"
)

func roundedDecimal(v float64) (negative bool, whole, fraction string, err error) {
	if v == 0 {
		v = 0
	}
	negative = v < 0
	if negative {
		v = -v
	}
	text := fmt.Sprintf("%.2f", v)
	parts := strings.SplitN(text, ".", 2)
	if len(parts) != 2 {
		return false, "", "", fmt.Errorf("cannot format money")
	}
	return negative, parts[0], parts[1], nil
}

// Format renders a number with a dollar sign, comma grouping and two decimals.
// It preserves the application's historical rounding behavior and normalizes
// IEEE negative zero to positive zero.
func Format(v float64) string {
	negative, whole, fraction, err := roundedDecimal(v)
	if err != nil {
		return "$" + fmt.Sprintf("%.2f", v)
	}
	var result strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}
	result.WriteRune('.')
	result.WriteString(fraction)
	if negative {
		return "-$" + result.String()
	}
	return "$" + result.String()
}

// Cents returns the signed integer represented by Format's rounded decimal.
func Cents(v float64) (int64, error) {
	negative, wholeText, fractionText, err := roundedDecimal(v)
	if err != nil {
		return 0, err
	}
	whole, err := strconv.ParseInt(wholeText, 10, 64)
	if err != nil {
		return 0, err
	}
	fraction, err := strconv.ParseInt(fractionText, 10, 64)
	if err != nil {
		return 0, err
	}
	maxInt64 := int64(^uint64(0) >> 1)
	if whole > (maxInt64-fraction)/100 {
		return 0, fmt.Errorf("money cents overflow")
	}
	cents := whole*100 + fraction
	if negative {
		return -cents, nil
	}
	return cents, nil
}
