package analysis

import (
	"testing"

	"budget2/internal/models"
)

// Federal tax brackets must be inflated from the bundled tax tables' base
// year to the plan's ACTUAL calendar year, not from projection-year 0.
// Two identical plans that differ only by StartDate must therefore see
// different year-0 taxes: a plan whose first year is well after the base
// year gets brackets inflated more, so the same ordinary income lands in
// lower brackets and owes less tax. Before the calendar-anchoring fix
// both plans passed yearsFromBase=0 at month 0 and the taxes were equal.
func TestBudgetFit_TaxBracketsAnchorToCalendarYear(t *testing.T) {
	build := func(startDate string) *models.BudgetFitAnalysis {
		s := models.DefaultWhatIfSettings()
		s.StartDate = startDate
		s.InflationRate = 3.0
		s.CurrentAge = 55 // < 65: no IRMAA; < 73: no RMD — isolates bracket inflation
		s.SpouseAge = 0
		s.SocialSecurity = nil
		s.InvestmentReturn = 5.0
		s.PortfolioValue = 1_000_000
		s.MonthlyLivingExpenses = 4000
		s.ProjectionYears = 30
		// A large non-Social-Security ordinary income stream so federal tax
		// is comfortably above the standard deduction and bracket-sensitive.
		s.IncomeSources = []models.IncomeSource{
			{Name: "Pension", Amount: 10000, StartMonth: 0},
		}

		_, in := runProj(t, s)
		return BudgetFit(in, nil)
	}

	base := build("2024-01")  // first year == tax base year → no extra inflation
	later := build("2030-01") // first year 6 years past base → brackets inflated 6 years

	if base.MonthlyTaxes <= 0 || later.MonthlyTaxes <= 0 {
		t.Fatalf("test needs positive federal tax in both plans: base=%.2f later=%.2f", base.MonthlyTaxes, later.MonthlyTaxes)
	}

	if !(later.MonthlyTaxes < base.MonthlyTaxes) {
		t.Errorf("expected a plan starting after the tax base year to owe LESS year-0 tax (brackets inflated to its calendar year); got base=%.2f later=%.2f",
			base.MonthlyTaxes, later.MonthlyTaxes)
	}
}
