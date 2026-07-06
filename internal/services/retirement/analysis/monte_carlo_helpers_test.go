package analysis

import (
	"math"
	"testing"
)

func TestFormatBucketLabel(t *testing.T) {
	tests := []struct {
		low, high float64
		expected  string
	}{
		{0, 100000, "$0K-$100K"},
		{250000, 500000, "$250K-$500K"},
		{1000000, 2000000, "$1.0M-$2.0M"},
		{5000000, -1, "$5.0M+"},
	}
	for _, tt := range tests {
		got := formatBucketLabel(tt.low, tt.high)
		if got != tt.expected {
			t.Errorf("formatBucketLabel(%f, %f) = %q, want %q", tt.low, tt.high, got, tt.expected)
		}
	}
}

func TestMean(t *testing.T) {
	t.Run("empty slice returns zero", func(t *testing.T) {
		if got := mean([]float64{}); got != 0 {
			t.Errorf("mean of empty slice: want 0, got %f", got)
		}
	})

	t.Run("single element", func(t *testing.T) {
		if got := mean([]float64{42.5}); math.Abs(got-42.5) > 0.001 {
			t.Errorf("mean of [42.5]: want 42.5, got %f", got)
		}
	})

	t.Run("multiple elements", func(t *testing.T) {
		if got := mean([]float64{10, 20, 30}); math.Abs(got-20) > 0.001 {
			t.Errorf("mean of [10,20,30]: want 20, got %f", got)
		}
	})
}
