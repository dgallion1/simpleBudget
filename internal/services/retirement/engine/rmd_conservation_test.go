package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// conservationScenario builds a projection with no income sources, no
// dividends and no inflation, so a projection year's balance change is fully
// explained by growth, expenses, taxes and IRMAA. Any residual is money the
// engine created or destroyed.
func conservationScenario(age int, monthlyExpenses float64) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{{
		ID:         "primary",
		Name:       "You",
		BirthMonth: models.BirthMonthForAge("2026-01", age),
		Role:       models.PersonRolePrimary,
	}}
	s.PortfolioValue = 2_000_000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.MonthlyLivingExpenses = monthlyExpenses
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.ExpenseSources = nil
	s.IncomeSources = nil
	s.ProjectionYears = 1
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.Guardrails = nil
	s.SocialSecurity = nil
	s.TaxableDividendYield = 0
	s.TaxableQualifiedDividendPercent = 0
	s.TaxableCapitalGainsDistributionRate = 0
	return s
}

// TestProjectionYearConservesWealth is the falsification test for audit
// finding F-1 (surplus RMD possibly taxed twice). With no income and no
// dividends, a projection year must satisfy
//
//	ending == starting + growth - expenses - taxes - irmaa
//
// A surplus RMD month withdraws the gross RMD from tax-deferred but deposits
// only gross*(1-marginalRate) into taxable, while taxes on that same gross are
// separately funded as a cash outflow. If that withholding is a second charge,
// the identity fails short by roughly sum(unmetRMD * marginalRate).
func TestProjectionYearConservesWealth(t *testing.T) {
	cases := []struct {
		name            string
		age             int
		monthlyExpenses float64
		wantRMD         bool
	}{
		// Control: taxes are paid, tax-deferred is drawn down, but no RMD is
		// in force. Exercises the same cash-flow path without the reinvest leg.
		{name: "no RMD, expenses force taxable withdrawals", age: 70, monthlyExpenses: 5_000, wantRMD: false},
		// The finding: RMD age, no expenses, so the entire RMD is surplus.
		{name: "surplus RMD, no expenses", age: 75, monthlyExpenses: 0, wantRMD: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proj := New().Run(Input{Prepared: prepare.MustFrom(t, conservationScenario(tc.age, tc.monthlyExpenses))})
			if proj == nil {
				t.Fatal("Run returned nil projection")
			}
			if len(proj.YearlySummaries) != 1 {
				t.Fatalf("yearly summaries=%d, want 1", len(proj.YearlySummaries))
			}
			y := proj.YearlySummaries[0]

			var rmd, income float64
			for _, m := range proj.Months {
				rmd += m.RMDWithdrawal
				income += m.TotalIncome
			}
			if tc.wantRMD && rmd <= 0 {
				t.Fatalf("scenario produced no RMD (age %d); it cannot exercise F-1", tc.age)
			}
			if !tc.wantRMD && rmd != 0 {
				t.Fatalf("control scenario produced an RMD of %.2f; expected none", rmd)
			}
			if income != 0 {
				t.Fatalf("scenario produced income %.2f; the identity below assumes none", income)
			}

			want := y.StartingBalance + y.Growth - y.Expenses - y.Taxes - y.IRMAA
			got := y.EndingBalance
			if math.Abs(got-want) > 1 {
				t.Errorf("wealth not conserved: ending=%.2f, want %.2f (residual %.2f)\n"+
					"  starting=%.2f growth=%.2f expenses=%.2f taxes=%.2f irmaa=%.2f rmd=%.2f",
					got, want, got-want,
					y.StartingBalance, y.Growth, y.Expenses, y.Taxes, y.IRMAA, rmd)
			}
		})
	}
}

// TestSurplusRMDIsNotWithheldTwice pins F-1 at the exact seam, with no tax
// model in play. The month needs $1,000 from the portfolio; the RMD is
// $10,000. The first $1,000 of the RMD funds that need and the remaining
// $9,000 is surplus that must land in the taxable account intact — the tax on
// the full $10,000 gross is already an explicit cash outflow inside
// neededFromPortfolio (portfolio_month.go:388, via TaxableWithdrawals at :417).
// Withholding marginalRate a second time on the surplus destroys
// unmetRMD*marginalRate of household wealth every surplus-RMD month.
func TestSurplusRMDIsNotWithheldTwice(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	taxable := NewTaxableAccountState(s, 0)
	taxDeferred := 100_000.0
	roth, rothBasis := 0.0, 0.0

	const (
		needed     = 1_000.0
		monthlyRMD = 10_000.0
		// The rate the old code withheld a second time, kept here only to
		// report the size of the regression if this test ever fails again.
		oldWithheldRate = 0.22
	)

	result := ExecutePortfolioCashFlowWithTaxableState(
		needed, monthlyRMD, true, 0,
		&taxDeferred, &taxable, &roth, &rothBasis)

	if math.Abs(result.RMDWithdrawal-monthlyRMD) > 0.01 {
		t.Errorf("RMDWithdrawal = %.2f; want %.2f (the full gross distribution)", result.RMDWithdrawal, monthlyRMD)
	}
	if math.Abs(taxDeferred-90_000) > 0.01 {
		t.Errorf("taxDeferred = %.2f; want 90000 (gross RMD leaves the account)", taxDeferred)
	}

	// Only `needed` may leave the household. Everything else moves between
	// accounts.
	deltaTaxDeferred := 100_000.0 - taxDeferred
	deltaTaxable := taxable.MarketValue
	if leaked := deltaTaxDeferred - deltaTaxable - needed; math.Abs(leaked) > 0.01 {
		t.Errorf("surplus RMD leaked %.2f (want 0): taxDeferred fell %.2f, taxable rose %.2f, only %.2f was spent\n"+
			"  expected leak if withheld twice: %.2f",
			leaked, deltaTaxDeferred, deltaTaxable, needed, (monthlyRMD-needed)*oldWithheldRate)
	}
	// Basis must equal what was actually deposited, or later LTCG is wrong (F-049).
	if math.Abs(taxable.CostBasis-taxable.MarketValue) > 0.01 {
		t.Errorf("CostBasis = %.2f, MarketValue = %.2f; a cash deposit must carry full basis", taxable.CostBasis, taxable.MarketValue)
	}
}
