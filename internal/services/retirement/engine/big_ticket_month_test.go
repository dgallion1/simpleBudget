package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

func bigTicketMonthSettings(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-10"
	s.UseCurrentMonth = false
	s.Persons = []models.Person{
		{ID: "primary", Name: "Primary", BirthMonth: models.BirthMonthForAge(s.StartDate, 65), Role: models.PersonRolePrimary},
	}
	s.PortfolioValue = 1_500_000
	s.ProjectionYears = 3
	s.InflationRate = 0
	return s
}

// A big-ticket item scheduled off a year boundary — the state the monthly
// StartDate rollover produces — is applied in its EXACT month, and in neither
// of the surrounding months a year-granular schedule would have used.
func TestApplyBigTicketItemsForMonth_NonYearAlignedMonth(t *testing.T) {
	s := bigTicketMonthSettings(t)
	s.BigTicketItems = []models.BigTicketItem{
		{ID: "sale", Name: "Boat sale", Amount: 5_000, Month: 11, Type: models.BigTicketIncome},
	}

	for _, tc := range []struct {
		month int
		want  float64
	}{{10, 0}, {11, 5_000}, {12, 0}, {0, 0}, {23, 0}} {
		td, roth, basis := 100_000.0, 0.0, 0.0
		taxable := TaxableAccountState{MarketValue: 1_000, CostBasis: 1_000}
		ApplyBigTicketItemsForMonth(s, tc.month, true, 0, &td, &taxable, &roth, &basis)
		if got := taxable.MarketValue - 1_000; math.Abs(got-tc.want) > 0.01 {
			t.Errorf("month %d taxable delta = %.2f, want %.2f", tc.month, got, tc.want)
		}
	}
}

// A year-aligned item still lands exactly where it always did, so the change
// of unit cannot move an existing plan's cash flow.
func TestApplyBigTicketItemsForMonth_YearAlignedUnchanged(t *testing.T) {
	s := bigTicketMonthSettings(t)
	s.BigTicketItems = []models.BigTicketItem{
		{ID: "sale", Name: "Boat sale", Amount: 5_000, Month: 1 * 12, Type: models.BigTicketIncome},
	}
	for _, tc := range []struct {
		month int
		want  float64
	}{{11, 0}, {12, 5_000}, {13, 0}} {
		td, roth, basis := 100_000.0, 0.0, 0.0
		taxable := TaxableAccountState{MarketValue: 1_000, CostBasis: 1_000}
		ApplyBigTicketItemsForMonth(s, tc.month, true, 0, &td, &taxable, &roth, &basis)
		if got := taxable.MarketValue - 1_000; math.Abs(got-tc.want) > 0.01 {
			t.Errorf("month %d taxable delta = %.2f, want %.2f", tc.month, got, tc.want)
		}
	}
}

// A past item keeps a negative month and is never applied: the projection
// loop starts at month 0 and never reaches it.
func TestApplyBigTicketItemsForMonth_PastItemNeverApplied(t *testing.T) {
	s := bigTicketMonthSettings(t)
	s.BigTicketItems = []models.BigTicketItem{
		{ID: "past", Name: "Old sale", Amount: 777, Month: -1, Type: models.BigTicketIncome},
	}
	for month := 0; month < s.ProjectionYears*12; month++ {
		td, roth, basis := 100_000.0, 0.0, 0.0
		taxable := TaxableAccountState{MarketValue: 1_000, CostBasis: 1_000}
		ApplyBigTicketItemsForMonth(s, month, true, 0, &td, &taxable, &roth, &basis)
		if got := taxable.MarketValue - 1_000; got != 0 {
			t.Fatalf("month %d applied a past item: delta %.2f", month, got)
		}
	}
}

// The canonical monthly loop charges both a big-ticket expense and a one-time
// expense in their exact, non-year-aligned month — proving the stepper calls
// them every month rather than only at a year boundary.
func TestEngineLoop_ChargesScheduledMonthNotYearBoundary(t *testing.T) {
	base := bigTicketMonthSettings(t)
	baseProj := New().Run(Input{Prepared: prepare.MustFrom(t, base)})

	with := bigTicketMonthSettings(t)
	with.OneTimeExpenses = []models.OneTimeExpense{{ID: "roof", Description: "Roof", Month: 11, Amount: 12_000}}
	with.BigTicketItems = []models.BigTicketItem{{ID: "car", Name: "Car", Amount: 5_000, Month: 11, Type: models.BigTicketExpense}}
	withProj := New().Run(Input{Prepared: prepare.MustFrom(t, with)})

	for _, tc := range []struct {
		month int
		want  float64
	}{{10, 0}, {11, 12_000}, {12, 0}} {
		got := withProj.Months[tc.month].TotalExpenses - baseProj.Months[tc.month].TotalExpenses
		if math.Abs(got-tc.want) > 1 {
			t.Errorf("month %d expense delta = %.2f, want %.2f", tc.month, got, tc.want)
		}
	}
}
