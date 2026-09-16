package engine

import (
	"math"
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

// TestOneTimeExpense_PureFunctionMonthTargeting exercises
// OneTimeExpensesForMonth directly: the entry counts only in its own month,
// and multiple entries in the same month sum.
func TestOneTimeExpense_PureFunctionMonthTargeting(t *testing.T) {
	s := oneTimeExpenseBaseSettings(t)
	s.OneTimeExpenses = []models.OneTimeExpense{
		{Description: "roof", Month: 3 * 12, Amount: 50_000},
		{Description: "car", Month: 3 * 12, Amount: 20_000},
		{Description: "wedding", Month: 5 * 12, Amount: 10_000},
	}

	if got := OneTimeExpensesForMonth(s, 36); got != 70_000 {
		t.Errorf("month 36 = %v, want 70000 (roof + car)", got)
	}
	if got := OneTimeExpensesForMonth(s, 60); got != 10_000 {
		t.Errorf("month 60 = %v, want 10000 (wedding)", got)
	}
	for month := 0; month < 120; month++ {
		if month == 36 || month == 60 {
			continue
		}
		if got := OneTimeExpensesForMonth(s, month); got != 0 {
			t.Errorf("month %d = %v, want 0", month, got)
		}
	}
}

// TestOneTimeExpense_PureFunctionNonYearAlignedMonth is the rollover case: an
// entry that has been shifted off a year boundary fires in its exact month
// and in NO other month — in particular not in the surrounding year-boundary
// months, where a year-granular schedule would have put it.
func TestOneTimeExpense_PureFunctionNonYearAlignedMonth(t *testing.T) {
	s := oneTimeExpenseBaseSettings(t)
	s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Month: 11, Amount: 12_000}}

	if got := OneTimeExpensesForMonth(s, 11); got != 12_000 {
		t.Errorf("month 11 = %v, want 12000", got)
	}
	for _, month := range []int{0, 10, 12, 23} {
		if got := OneTimeExpensesForMonth(s, month); got != 0 {
			t.Errorf("month %d = %v, want 0", month, got)
		}
	}
}

// TestOneTimeExpense_PureFunctionPastMonthNeverCharged pins the retention
// contract: an entry whose month has gone by keeps a negative offset and the
// projection — which starts at month 0 — never charges it.
func TestOneTimeExpense_PureFunctionPastMonthNeverCharged(t *testing.T) {
	s := oneTimeExpenseBaseSettings(t)
	s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Month: -1, Amount: 12_000}}
	for month := 0; month < 120; month++ {
		if got := OneTimeExpensesForMonth(s, month); got != 0 {
			t.Errorf("month %d = %v, want 0 for a past entry", month, got)
		}
	}
}

// TestOneTimeExpense_PureFunctionInflatesFromToday verifies the amount is
// today's dollars inflated by the plan's general InflationRate to its month —
// month 0 uninflated, later months compounded. A year-aligned month produces
// exactly the figure the year-granular predecessor produced.
func TestOneTimeExpense_PureFunctionInflatesFromToday(t *testing.T) {
	s := oneTimeExpenseBaseSettings(t)
	s.InflationRate = 3
	s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Month: 0, Amount: 50_000}}
	if got := OneTimeExpensesForMonth(s, 0); got != 50_000 {
		t.Errorf("month 0 must be uninflated: got %v, want 50000", got)
	}

	s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Month: 3 * 12, Amount: 50_000}}
	got := OneTimeExpensesForMonth(s, 36)
	want := 50_000 * 1.03 * 1.03 * 1.03
	if diff := got - want; diff > 1 || diff < -1 {
		t.Errorf("month 36 at 3%% inflation = %v, want ~%v", got, want)
	}

	// A 12000 entry at month 12 with 3% inflation is exactly 12360.00 — the
	// regression figure for "year-aligned entries are unchanged".
	s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Month: 12, Amount: 12_000}}
	if got := OneTimeExpensesForMonth(s, 12); got < 12_359.99 || got > 12_360.01 {
		t.Errorf("month 12 at 3%% inflation = %v, want 12360.00", got)
	}

	// A month off the year boundary compounds by that exact fraction of a
	// year, not by the surrounding whole year.
	s.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Month: 11, Amount: 12_000}}
	gotEleven := OneTimeExpensesForMonth(s, 11)
	wantEleven := 12_000 * math.Pow(1.03, 11.0/12)
	if diff := gotEleven - wantEleven; diff > 0.01 || diff < -0.01 {
		t.Errorf("month 11 at 3%% inflation = %v, want %v", gotEleven, wantEleven)
	}
	if gotEleven >= 12_360 {
		t.Errorf("month 11 (%v) must be inflated LESS than month 12 (12360.00)", gotEleven)
	}
}

// TestOneTimeExpense_EmptyListIsZero verifies an absent/empty list changes
// nothing.
func TestOneTimeExpense_EmptyListIsZero(t *testing.T) {
	s := oneTimeExpenseBaseSettings(t)
	s.OneTimeExpenses = nil
	for month := 0; month < s.ProjectionYears*12; month++ {
		if got := OneTimeExpensesForMonth(s, month); got != 0 {
			t.Errorf("month %d = %v, want 0 for empty list", month, got)
		}
	}
}

// TestOneTimeExpense_EngineLoopHitsOnlyItsYear runs the full canonical
// monthly loop (engine.New().Run) — a different seam than the mcpsvc/plan
// oracle — and verifies the per-month wiring in stepper.go: the entry's
// year shows the extra expense, no other year does, and the ending balance
// for that year drops by roughly the expense (net of any growth
// difference), confirming the amount flows through the withdrawal
// machinery rather than sitting inert.
func TestOneTimeExpense_EngineLoopHitsOnlyItsYear(t *testing.T) {
	without := oneTimeExpenseBaseSettings(t)
	withoutProj := New().Run(Input{Prepared: prepare.MustFrom(t, without)})

	with := oneTimeExpenseBaseSettings(t)
	with.OneTimeExpenses = []models.OneTimeExpense{{Description: "roof", Month: 3 * 12, Amount: 50_000}}
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
