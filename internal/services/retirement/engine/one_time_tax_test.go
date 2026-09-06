package engine

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
	"math"
	"testing"
)

// The annual tax calculator is independent of the monthly estimator.
// A January purchase must not incur tax on twelve copies of its funding draw.
func TestOneTimeExpenseTaxesReconcileToActualAnnualIncome(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		deferred, roth, basis float64
		living                float64
	}{
		{"tax deferred", 100, 0, 0, 0},
		{"taxable embedded gains", 0, 0, 0, 0},
		{"taxable half basis", 0, 0, 250000, 1000},
		{"mixed accounts", 95, 0, 10000, 1000},
		{"Roth funded", 0, 100, 0, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.StartDate = "2026-01"
			s.Persons = []models.Person{{ID: "primary", Name: "Primary", BirthMonth: "1961-01", Role: models.PersonRolePrimary}}
			s.PortfolioValue = 500000
			s.TaxDeferredPercent = tc.deferred
			s.RothPercent = tc.roth
			s.TaxableCostBasis = &tc.basis
			s.MonthlyLivingExpenses = tc.living
			s.MonthlyPropertyTax = 0
			s.MonthlyHealthcare = 0
			s.HealthcarePersons = nil
			s.SocialSecurity = nil
			s.IncomeSources = nil
			s.ExpenseSources = nil
			s.BigTicketItems = nil
			s.SpendingPhaseConfig = nil
			s.Guardrails = nil
			s.InflationRate = 0
			s.SpendingDeclineRate = 0
			s.TaxableDividendYield = 0
			s.TaxableCapitalGainsDistributionRate = 0
			s.ProjectionYears = 2
			s.OneTimeExpenses = []models.OneTimeExpense{{ID: "roof", Description: "Roof", Year: 0, Amount: 50000}}
			in := Input{Prepared: prepare.MustFrom(t, s)}
			proj := New().Run(in)
			calc := NewTaxCalculator(in.Prepared.Settings().TaxConfig, 0)
			calc.Age65Count = 1 // primary is 65 throughout this two-year test
			for year, ys := range proj.YearlySummaries {
				deferred := 0.0
				for _, m := range proj.Months[year*12 : (year+1)*12] {
					deferred += m.WithdrawalFromTaxDeferred
				}
				// No dividends, SS, conversions, or Roth earnings: remaining MAGI
				// is capital gain from taxable sales.
				inputs := ProjectedAnnualTaxInputs{TaxableWithdrawals: deferred, LongTermCapitalGains: math.Max(0, ys.MAGI-deferred)}
				want := calc.AnnualIncomeTaxOn(inputs, YearsFromTaxBase(in.Prepared.Settings(), year))
				if math.Abs(ys.Taxes-want) > 1 {
					t.Errorf("year %d: paid %.2f tax, annual liability on actual income %.2f (MAGI %.2f)", year, ys.Taxes, want, ys.MAGI)
				}
			}
		})
	}
}
