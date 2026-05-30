package engine

import (
	"testing"

	"budget2/internal/models"
)

func TestYearsFromTaxBase(t *testing.T) {
	tests := []struct {
		name        string
		startDate   string
		currentYear int
		want        int
	}{
		{
			name:        "plan starting in the tax base year, projection year 0",
			startDate:   "2024-01",
			currentYear: 0,
			want:        0,
		},
		{
			name:        "plan starting in the tax base year, projection year 5",
			startDate:   "2024-01",
			currentYear: 5,
			want:        5,
		},
		{
			name:        "plan starting after the base year carries the calendar gap at year 0",
			startDate:   "2026-01",
			currentYear: 0,
			want:        2,
		},
		{
			name:        "plan starting after the base year compounds the gap forward",
			startDate:   "2026-01",
			currentYear: 3,
			want:        5,
		},
		{
			name:        "plan starting before the base year is negative (downstream floors to no adjustment)",
			startDate:   "2020-01",
			currentYear: 1,
			want:        -3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &models.WhatIfSettings{StartDate: tt.startDate}
			if got := YearsFromTaxBase(s, tt.currentYear); got != tt.want {
				t.Fatalf("YearsFromTaxBase(%q, %d) = %d, want %d", tt.startDate, tt.currentYear, got, tt.want)
			}
		})
	}
}
