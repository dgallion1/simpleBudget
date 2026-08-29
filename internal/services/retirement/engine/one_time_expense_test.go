package engine

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// oneTimeExpenseBaseSettings returns a small, deterministic settings object
// for exercising models.OneTimeExpense at the engine seam — a different seam
// than the mcpsvc/plan oracle (T18), which drives the same behavior through
// RunWithOverrides.
func oneTimeExpenseBaseSettings(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{ID: "primary", Name: "Primary", BirthMonth: models.BirthMonthForAge(s.StartDate, 65), Role: models.PersonRolePrimary},
	}
	s.PortfolioValue = 1_500_000
	s.ProjectionYears = 10
	s.InflationRate = 0
	return s
}

// TestOneTimeExpense_PureFunctionYearTargeting exercises
// OneTimeExpensesForYear directly: the entry counts only in its own year,
// and multiple entries in the same year sum.
func TestOneTimeExpense_PureFunctionYearTargeting(t *testing.T) {
	s := oneTimeExpenseBaseSettings(t)
	s.OneTimeExpenses = []models.OneTimeExpense{
		{Description: "roof", Year: 3, Amount: 50_000},
		{Description: "car", Year: 3, Amount: 20_000},
		{Description: "wedding", Year: 5, Amount: 10_000},
	}

	if got := OneTimeExpensesForYear(s, 3); got != 70_000 {
		t.Errorf("year 3 = %v, want 70000 (roof + car)", got)
	}
	if got := OneTimeExpensesForYear(s, 5); got != 10_000 {
		t.Errorf("year 5 = %v, want 10000 (wedding)", got)
	}
	for _, y := range []int{0, 1, 2, 4, 6, 7, 8, 9} {
		if got := OneTimeExpensesForYear(s, y); got != 0 {
			t.Errorf("year %d = %v, want 0", y, got)
		}
	}
}

// TestOneTimeExpense_PureFunctionInflatesFromToday verifies the amount is
// today's dollars inflated by the plan's general InflationRate to its year —
// year 0 uninflated, later years compounded.
func TestOneTimeExpense_PureFunctionInflatesFromToday(t *testing.T) {
	s := oneTimeExpenseBaseSettings(t)
	s.InflationRate = 3
	s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Year: 0, Amount: 50_000}}
	if got := OneTimeExpensesForYear(s, 0); got != 50_000 {
		t.Errorf("year 0 must be uninflated: got %v, want 50000", got)
	}

	s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Year: 3, Amount: 50_000}}
	got := OneTimeExpensesForYear(s, 3)
	want := 50_000 * 1.03 * 1.03 * 1.03
	if diff := got - want; diff > 1 || diff < -1 {
		t.Errorf("year 3 at 3%% inflation = %v, want ~%v", got, want)
	}
}

// TestOneTimeExpense_EmptyListIsZero verifies an absent/empty list changes
// nothing.
func TestOneTimeExpense_EmptyListIsZero(t *testing.T) {
	s := oneTimeExpenseBaseSettings(t)
	s.OneTimeExpenses = nil
	for y := 0; y < s.ProjectionYears; y++ {
		if got := OneTimeExpensesForYear(s, y); got != 0 {
			t.Errorf("year %d = %v, want 0 for empty list", y, got)
		}
	}
}

// TestOneTimeExpense_EngineLoopHitsOnlyItsYear runs the full canonical
// monthly loop (engine.New().Run) — a different seam than the mcpsvc/plan
// oracle — and verifies the year-boundary wiring in stepper.go: the entry's
// year shows the extra expense, no other year does, and the ending balance
// for that year drops by roughly the expense (net of any growth
// difference), confirming the amount flows through the withdrawal
// machinery rather than sitting inert.
func TestOneTimeExpense_EngineLoopHitsOnlyItsYear(t *testing.T) {
	without := oneTimeExpenseBaseSettings(t)
	withoutProj := New().Run(Input{Prepared: prepare.MustFrom(t, without)})

	with := oneTimeExpenseBaseSettings(t)
	with.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Year: 3, Amount: 50_000}}
	withProj := New().Run(Input{Prepared: prepare.MustFrom(t, with)})

	if len(withProj.YearlySummaries) != len(withoutProj.YearlySummaries) {
		t.Fatalf("yearly summaries length mismatch: %d vs %d", len(withProj.YearlySummaries), len(withoutProj.YearlySummaries))
	}

	for y := range withoutProj.YearlySummaries {
		delta := withProj.YearlySummaries[y].Expenses - withoutProj.YearlySummaries[y].Expenses
		if y == 3 {
			if delta < 49_999 || delta > 50_001 {
				t.Errorf("year 3 expense delta = %v, want ~50000", delta)
			}
		} else if delta > 1 || delta < -1 {
			t.Errorf("year %d expense delta = %v, want 0", y, delta)
		}
	}

	if withProj.FinalBalance > withoutProj.FinalBalance-40_000 {
		t.Errorf("final balance %v not meaningfully reduced by the one-time expense (baseline %v)",
			withProj.FinalBalance, withoutProj.FinalBalance)
	}
}
