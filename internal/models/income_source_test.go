package models

import (
	"math"
	"testing"
)

func TestIncomeSourceGetAdjustedAmount(t *testing.T) {
	endMonth := 24

	tests := []struct {
		name   string
		source IncomeSource
		month  int
		want   float64
	}{
		{
			name:   "before start",
			source: IncomeSource{Amount: 1000, StartMonth: 12},
			month:  6,
			want:   0,
		},
		{
			name:   "at start no COLA",
			source: IncomeSource{Amount: 1000, StartMonth: 0},
			month:  0,
			want:   1000,
		},
		{
			name:   "after end",
			source: IncomeSource{Amount: 1000, StartMonth: 0, EndMonth: &endMonth},
			month:  24,
			want:   0,
		},
		{
			name:   "active with no COLA",
			source: IncomeSource{Amount: 1000, StartMonth: 0, COLARate: 0},
			month:  12,
			want:   1000,
		},
		{
			name:   "active with COLA",
			source: IncomeSource{Amount: 1000, StartMonth: 0, COLARate: 0.03},
			month:  12,
			want:   1000 * math.Pow(1.03, 1.0),
		},
		{
			name:   "perpetual no end",
			source: IncomeSource{Amount: 2000, StartMonth: 0, EndMonth: nil},
			month:  100,
			want:   2000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.source.GetAdjustedAmount(tt.month)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("got %f, want %f", got, tt.want)
			}
		})
	}
}

func TestIncomeSourceIsActive(t *testing.T) {
	endMonth := 24

	tests := []struct {
		name   string
		source IncomeSource
		month  int
		want   bool
	}{
		{"before start", IncomeSource{StartMonth: 12}, 6, false},
		{"at start", IncomeSource{StartMonth: 12}, 12, true},
		{"after end", IncomeSource{StartMonth: 0, EndMonth: &endMonth}, 24, false},
		{"before end", IncomeSource{StartMonth: 0, EndMonth: &endMonth}, 23, true},
		{"perpetual", IncomeSource{StartMonth: 0, EndMonth: nil}, 999, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.source.IsActive(tt.month); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExpenseSourceGetAdjustedAmount(t *testing.T) {
	tests := []struct {
		name      string
		source    ExpenseSource
		month     int
		inflation float64
		want      float64
	}{
		{
			name:   "zero amount",
			source: ExpenseSource{Amount: 0, StartYear: 0},
			month:  12, inflation: 3.0,
			want: 0,
		},
		{
			name:   "before start",
			source: ExpenseSource{Amount: 500, StartYear: 2},
			month:  12, inflation: 3.0,
			want: 0,
		},
		{
			name:   "after end",
			source: ExpenseSource{Amount: 500, StartYear: 0, EndYear: 2},
			month:  24, inflation: 3.0,
			want: 0,
		},
		{
			name:   "active no inflation",
			source: ExpenseSource{Amount: 500, StartYear: 0, Inflation: false},
			month:  12, inflation: 3.0,
			want: 500,
		},
		{
			name:   "active with inflation",
			source: ExpenseSource{Amount: 500, StartYear: 0, Inflation: true},
			month:  12, inflation: 3.0,
			want: 500 * math.Pow(1.03, 1.0),
		},
		{
			name:   "perpetual (EndYear=0)",
			source: ExpenseSource{Amount: 500, StartYear: 0, EndYear: 0},
			month:  100, inflation: 0,
			want: 500,
		},
		{
			name:   "inflation rate zero",
			source: ExpenseSource{Amount: 500, StartYear: 0, Inflation: true},
			month:  12, inflation: 0,
			want: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.source.GetAdjustedAmount(tt.month, tt.inflation)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("got %f, want %f", got, tt.want)
			}
		})
	}
}

func TestExpenseSourceIsActive(t *testing.T) {
	tests := []struct {
		name   string
		source ExpenseSource
		month  int
		want   bool
	}{
		{"before start", ExpenseSource{StartYear: 2}, 12, false},
		{"at start", ExpenseSource{StartYear: 1}, 12, true},
		{"after end", ExpenseSource{StartYear: 0, EndYear: 2}, 24, false},
		{"before end", ExpenseSource{StartYear: 0, EndYear: 2}, 23, true},
		{"perpetual", ExpenseSource{StartYear: 0, EndYear: 0}, 999, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.source.IsActive(tt.month); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
