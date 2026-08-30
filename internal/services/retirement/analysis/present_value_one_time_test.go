package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// oneTimeExpensePVSettings builds a scenario identical across calls except
// for OneTimeExpenses, mirroring oneTimeExpenseRegressionSettings in
// one_time_expense_test.go but for PresentValue rather than the projection
// loops.
func oneTimeExpensePVSettings(oneTime []models.OneTimeExpense) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.ProjectionYears = 30
	s.OneTimeExpenses = oneTime
	return s
}

// TestPresentValue_IncludesOneTimeExpense verifies PVExpenses picks up a
// one-time expense (Z1): the diff between a fixture with and without the
// expense must equal the engine-inflated amount discounted at its charge
// month, using the same ordinary-annuity convention and monthly-rate
// derivation as presentValueOfMonthlyStream / PresentValueAnnuity.
func TestPresentValue_IncludesOneTimeExpense(t *testing.T) {
	const (
		amount = 50_000.0
		year   = 5
	)

	without := oneTimeExpensePVSettings(nil)
	withOne := oneTimeExpensePVSettings([]models.OneTimeExpense{
		{Description: "roof", Year: year, Amount: amount},
	})

	resultWithout := PresentValue(engineInput(t, without), nil)
	resultWith := PresentValue(engineInput(t, withOne), nil)

	diff := resultWith.PVExpenses - resultWithout.PVExpenses

	chargeMonth := year * 12
	monthlyRate := engine.MonthlyCompoundFactorFromDecimal(withOne.DiscountRate/100) - 1
	inflationFactor := math.Pow(1+withOne.InflationRate/100, float64(chargeMonth)/12.0)
	inflatedAmount := amount * inflationFactor
	want := inflatedAmount / math.Pow(1+monthlyRate, float64(chargeMonth+1))

	if relErr := math.Abs(diff-want) / want; relErr > 1e-6 {
		t.Fatalf("PVExpenses diff = %.6f, want %.6f (rel err %.9f) — one-time expense missing or mis-discounted from PVExpenses",
			diff, want, relErr)
	}
}

// TestPresentValue_OneTimeExpense_ZeroDiscountRate verifies that with a 0%
// discount rate the one-time expense's PV contribution equals the
// engine-inflated amount exactly (no discounting applied), matching
// presentValueOfMonthlyStream's monthlyRate<=0 branch.
func TestPresentValue_OneTimeExpense_ZeroDiscountRate(t *testing.T) {
	const (
		amount = 20_000.0
		year   = 4
	)

	without := oneTimeExpensePVSettings(nil)
	without.DiscountRate = 0
	withOne := oneTimeExpensePVSettings([]models.OneTimeExpense{
		{Description: "car", Year: year, Amount: amount},
	})
	withOne.DiscountRate = 0

	resultWithout := PresentValue(engineInput(t, without), nil)
	resultWith := PresentValue(engineInput(t, withOne), nil)

	diff := resultWith.PVExpenses - resultWithout.PVExpenses

	chargeMonth := year * 12
	inflationFactor := math.Pow(1+withOne.InflationRate/100, float64(chargeMonth)/12.0)
	want := amount * inflationFactor

	if math.Abs(diff-want) > 1e-6 {
		t.Fatalf("PVExpenses diff = %.6f, want %.6f (zero discount rate should leave the inflated amount undiscounted)",
			diff, want)
	}
}

// TestPresentValue_OneTimeExpense_AtOrBeyondHorizonContributesNothing
// verifies an expense scheduled at or beyond the projection horizon
// (e.Year*12 >= months) is excluded from PVExpenses, per the Z1 scope note.
func TestPresentValue_OneTimeExpense_AtOrBeyondHorizonContributesNothing(t *testing.T) {
	for _, year := range []int{10, 15} {
		year := year
		t.Run("", func(t *testing.T) {
			without := oneTimeExpensePVSettings(nil)
			without.ProjectionYears = 10
			withOne := oneTimeExpensePVSettings([]models.OneTimeExpense{
				{Description: "wedding", Year: year, Amount: 30_000},
			})
			withOne.ProjectionYears = 10

			resultWithout := PresentValue(engineInput(t, without), nil)
			resultWith := PresentValue(engineInput(t, withOne), nil)

			diff := resultWith.PVExpenses - resultWithout.PVExpenses
			if math.Abs(diff) > 1e-9 {
				t.Errorf("year=%d: PVExpenses diff = %.9f, want 0 (expense at or beyond the %d-year horizon must not contribute)",
					year, diff, withOne.ProjectionYears)
			}
		})
	}
}
