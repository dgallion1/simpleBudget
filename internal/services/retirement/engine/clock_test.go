package engine

import "testing"

func TestRothQualifiedDistributionClockSatisfied(t *testing.T) {
	cases := []struct {
		name            string
		firstFundedYear int
		calendarYear    int
		wantSatisfied   bool
	}{
		{"unset firstFundedYear is never satisfied", 0, 2030, false},
		{"negative firstFundedYear is never satisfied", -1, 2030, false},
		{"same year as funded is not satisfied", 2026, 2026, false},
		{"one year after funded is not satisfied", 2026, 2027, false},
		{"four years after funded is not satisfied", 2026, 2030, false},
		{"five years after funded is satisfied", 2026, 2031, true},
		{"six years after funded is satisfied", 2026, 2032, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RothQualifiedDistributionClockSatisfied(tc.firstFundedYear, tc.calendarYear)
			if got != tc.wantSatisfied {
				t.Fatalf("got %v, want %v", got, tc.wantSatisfied)
			}
		})
	}
}
