package moneyfmt

import (
	"math"
	"testing"
)

func TestFormatLegacyBehavior(t *testing.T) {
	for _, tc := range []struct {
		value float64
		want  string
	}{
		{0, "$0.00"}, {math.Copysign(0, -1), "$0.00"}, {1234.5, "$1,234.50"}, {-1234.5, "-$1,234.50"}, {1.005, "$1.00"},
	} {
		if got := Format(tc.value); got != tc.want {
			t.Fatalf("Format(%v)=%q want %q", tc.value, got, tc.want)
		}
	}
}
