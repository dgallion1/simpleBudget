package analysis

import (
	"testing"

	"budget2/internal/models"
)

// Regression (#4): taxable distributions only offset the monthly budget
// to the extent they're realized before the withdrawal. The Budget
// Analysis must mirror the engine's projection-timing split: under the
// default end-of-month timing the full month's distributions are
// spendable; under start-of-month timing they're reinvested and must not
// be counted as income.
func TestBudgetFit_TaxableDistributionOffsetRespectsTiming(t *testing.T) {
	build := func(timing models.ProjectionTiming) *models.BudgetFitAnalysis {
		s := models.DefaultWhatIfSettings()
		s.SocialSecurity = nil
		s.IncomeSources = nil
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.ExpenseSources = nil
		s.PortfolioValue = 1_000_000
		s.TaxDeferredPercent = 0 // 100% taxable account → dividends/distributions
		s.RothPercent = 0
		s.TaxableDividendYield = 3.0
		s.InvestmentReturn = 6.0
		s.MonthlyLivingExpenses = 4_000
		s.ProjectionYears = 20
		s.ProjectionTiming = timing
		proj, in := runProj(t, s)
		return BudgetFit(in, proj)
	}

	endOfMonth := build(models.ProjectionTimingEndOfMonth)
	startOfMonth := build(models.ProjectionTimingStartOfMonth)

	// End-of-month: full distributions are spendable, so income exceeds the
	// start-of-month case (where they're reinvested).
	if !(endOfMonth.MonthlyIncome > startOfMonth.MonthlyIncome) {
		t.Errorf("end-of-month income (%.2f) should exceed start-of-month income (%.2f) by the spendable distributions",
			endOfMonth.MonthlyIncome, startOfMonth.MonthlyIncome)
	}

	// Start-of-month: with no other income source, distributions are
	// reinvested (not spendable), so monthly income is ~0.
	if startOfMonth.MonthlyIncome > 1.0 {
		t.Errorf("start-of-month: reinvested distributions must not count as spendable income, got MonthlyIncome=%.2f",
			startOfMonth.MonthlyIncome)
	}
}
