package engine

import (
	"testing"

	"budget2/internal/models"
)

func TestApplyRothConversionAtYear_BasisAndClock(t *testing.T) {
	t.Run("no conversion → no mutation", func(t *testing.T) {
		s := &models.WhatIfSettings{}
		td := 100000.0
		roth := 0.0
		basis := 0.0
		firstFunded := 0
		got := ApplyRothConversionAtYear(s, 0, &td, &roth, &basis, &firstFunded)
		if got != 0 || td != 100000 || roth != 0 || basis != 0 || firstFunded != 0 {
			t.Fatalf("unexpected mutation: got=%v td=%v roth=%v basis=%v ff=%v", got, td, roth, basis, firstFunded)
		}
	})

	t.Run("active conversion increments balance and basis equally and sets clock", func(t *testing.T) {
		s := &models.WhatIfSettings{
			StartDate: "2026-01",
			RothConversion: &models.RothConversionConfig{
				Enabled:      true,
				StartYear:    0,
				EndYear:      0,
				AnnualAmount: 25000,
			},
		}
		td := 100000.0
		roth := 0.0
		basis := 0.0
		firstFunded := 0
		got := ApplyRothConversionAtYear(s, 0, &td, &roth, &basis, &firstFunded)
		if got != 25000 || td != 75000 || roth != 25000 || basis != 25000 {
			t.Fatalf("balances wrong: got=%v td=%v roth=%v basis=%v", got, td, roth, basis)
		}
		if firstFunded != 2026 {
			t.Fatalf("clock not set: firstFunded=%d, want 2026", firstFunded)
		}
	})

	t.Run("second conversion preserves earlier firstFundedYear", func(t *testing.T) {
		s := &models.WhatIfSettings{
			StartDate: "2026-01",
			RothConversion: &models.RothConversionConfig{
				Enabled:      true,
				StartYear:    0,
				EndYear:      0,
				AnnualAmount: 25000,
			},
		}
		td := 100000.0
		roth := 0.0
		basis := 0.0
		firstFunded := 2020 // pre-existing
		_ = ApplyRothConversionAtYear(s, 0, &td, &roth, &basis, &firstFunded)
		if firstFunded != 2020 {
			t.Fatalf("clock overwritten: firstFunded=%d, want 2020", firstFunded)
		}
	})
}

func TestApplyTaxStateMonth_IncludesTaxableRothEarnings(t *testing.T) {
	taxState := &ProjectionTaxAccumulator{}
	income := MonthlyIncomeBreakdown{OrdinaryIncome: 1000}
	monthResult := TaxAwarePortfolioMonthResult{
		TaxableRothEarnings:          200,
		TaxableNonQualifiedDividends: 0,
		CashFlow:                     PortfolioCashFlowResult{},
	}
	ApplyTaxStateMonth(taxState, income, monthResult, 0)

	// OrdinaryIncomeYTD must include both the base ordinary income and the
	// taxable Roth earnings (non-qualified distribution from pre-clock Roth).
	got := taxState.OrdinaryIncomeYTD
	want := 1000.0 + 200.0
	if got != want {
		t.Fatalf("OrdinaryIncomeYTD=%v, want %v", got, want)
	}
}
