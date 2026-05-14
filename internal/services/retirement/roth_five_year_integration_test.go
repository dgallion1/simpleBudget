package retirement

import (
	"testing"

	"budget2/internal/models"
)

// buildRothEarningsScenario builds a scenario designed to exercise the
// 5-year-rule earnings-tax path.
//
// Portfolio: 100% Roth, $100k, 6% return, $2k/month expenses.
//
// Math:
//   - Initial basis = balance = $100k.
//   - Monthly growth ≈ $500 (year 1), declining as balance falls.
//   - Net depletion ≈ $1,500/month.
//   - At ~month 50 (year 4), the cumulative withdrawals equal the initial
//     basis ($100k), so remaining basis → 0 while the balance is still
//     positive (~$15k).
//   - Months 51–58 (still in year 4, calendarYear 2030 < 2031):
//     ALL withdrawals are from earnings (basis = 0). These ≈$16k of
//     earnings are taxable ordinary income when the clock is unsatisfied.
//   - With a ~$14,600 standard deduction (single filer, 2024), year-4
//     taxable income ≈ $16k − $14.6k = $1.4k → ~$140 federal tax.
//     The satisfied-clock scenario pays $0.
func buildRothEarningsScenario(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.CurrentAge = 65
	s.Persons = []models.Person{
		{
			ID:         "p1",
			Name:       "You",
			Role:       models.PersonRolePrimary,
			BirthMonth: models.BirthMonthForAge("2026-01", 65),
		},
	}
	s.PortfolioValue = 100_000
	s.TaxDeferredPercent = 0
	s.RothPercent = 100
	// Taxable = 0%
	s.MonthlyLivingExpenses = 2_000
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.ExpenseSources = nil
	s.IncomeSources = nil
	s.InvestmentReturn = 6.0
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.ProjectionYears = 10
	return s
}

// sumEarlyTax returns the sum of Taxes across the first n yearly
// summaries of the projection result.
func sumEarlyTax(result *models.ProjectionResult, n int) float64 {
	total := 0.0
	for i, ys := range result.YearlySummaries {
		if i >= n {
			break
		}
		total += ys.Taxes
	}
	return total
}

// TestRunMonthlyLoop_RothEarningsTaxed_WhenClockUnsatisfied asserts that
// a projection with an unsatisfied Roth 5-year clock accumulates higher
// taxes in the early years than the same scenario with the clock already
// satisfied. The delta comes from Roth earnings being folded into ordinary
// income when the clock test fails.
func TestRunMonthlyLoop_RothEarningsTaxed_WhenClockUnsatisfied(t *testing.T) {
	// Clock unsatisfied: funded in 2026, matures 2031; years 0–4 are in-window.
	sUnsat := buildRothEarningsScenario(t)
	sUnsat.RothFirstFundedYear = 2026

	// Clock satisfied: funded in 2015, matured 2020 — well before projection start.
	sSat := buildRothEarningsScenario(t)
	sSat.RothFirstFundedYear = 2015

	resultUnsat := newTestCalc(t, sUnsat).RunProjection()
	resultSat := newTestCalc(t, sSat).RunProjection()

	if len(resultUnsat.YearlySummaries) == 0 {
		t.Fatal("unsatisfied-clock projection produced no yearly summaries")
	}
	if len(resultSat.YearlySummaries) == 0 {
		t.Fatal("satisfied-clock projection produced no yearly summaries")
	}

	// Sum taxes over the 5 years while the unsatisfied clock applies (years 0–4).
	taxUnsat := sumEarlyTax(resultUnsat, 5)
	taxSat := sumEarlyTax(resultSat, 5)

	if taxUnsat <= taxSat {
		t.Fatalf(
			"expected unsatisfied-clock early tax > satisfied-clock early tax;\n"+
				"  unsatisfied sum[0..4] = %.2f\n"+
				"  satisfied   sum[0..4] = %.2f\n"+
				"  diff = %.2f\n"+
				"  (verify Roth earnings are reaching ordinary income when clock unsatisfied)",
			taxUnsat, taxSat, taxUnsat-taxSat,
		)
	}
}
