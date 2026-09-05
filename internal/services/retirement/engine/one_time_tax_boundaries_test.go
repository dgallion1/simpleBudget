package engine

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
	"math"
	"testing"
)

func TestOneTimeExpenseTaxWithRMD(t *testing.T) {
	for _, timing := range []models.RMDTiming{models.RMDTimingStartOfYear, models.RMDTimingMidYear, models.RMDTimingEndOfYear} {
		t.Run(string(timing), func(t *testing.T) {
			s := rmdTimingScenario(timing)
			s.OneTimeExpenses = []models.OneTimeExpense{{ID: "roof", Year: 0, Amount: 50000}}
			in := Input{Prepared: prepare.MustFrom(t, s)}
			proj := New().Run(in)
			withdrawals := 0.0
			for _, m := range proj.Months {
				withdrawals += m.WithdrawalFromTaxDeferred
			}
			calc := NewTaxCalculator(in.Prepared.Settings().TaxConfig, 0)
			calc.Age65Count = 2
			want := calc.AnnualIncomeTaxOn(ProjectedAnnualTaxInputs{TaxableWithdrawals: withdrawals}, 2)
			if got := proj.YearlySummaries[0].Taxes; math.Abs(got-want) > 1 {
				t.Fatalf("taxes %.2f, want actual annual liability %.2f; RMD must not be subtracted twice", got, want)
			}
		})
	}
}

func TestOneTimeExpenseTaxOnUnqualifiedRothEarnings(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{{ID: "primary", Name: "Primary", BirthMonth: "1961-01", Role: models.PersonRolePrimary}}
	s.PortfolioValue = 100000
	s.TaxDeferredPercent = 0
	s.RothPercent = 100
	s.MonthlyLivingExpenses = 0
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 0
	s.SpendingPhaseConfig = nil
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.OneTimeExpenses = []models.OneTimeExpense{{ID: "care", Year: 0, Amount: 50000}}
	st := NewProjectionState(Input{Prepared: prepare.MustFrom(t, s)})
	// Exercise an existing Roth account whose contribution basis is exhausted
	// and whose five-year qualified-distribution clock has not elapsed.
	st.RothBasis = 0
	st.RothFirstFundedYear = 2026
	for m := 0; m < 12; m++ {
		st.StepMonth(m, func(*models.WhatIfSettings, int) MonthReturns { return MonthReturns{} })
	}
	want := st.TaxCalculator.AnnualIncomeTaxOn(ProjectedAnnualTaxInputs{OrdinaryIncome: st.TaxState.OrdinaryIncomeYTD}, 2)
	if math.Abs(st.TaxState.TaxesPaidYTD-want) > 1 {
		t.Fatalf("paid %.2f, annual tax on actual Roth earnings %.2f", st.TaxState.TaxesPaidYTD, want)
	}
	if want <= 0 {
		t.Fatal("fixture must exercise taxable earnings")
	}
}
