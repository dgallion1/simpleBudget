package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
)

// rmdTimingScenario is a married household past RMD age drawing nothing but
// the RMD: $500k all tax-deferred, no other income, zero inflation and zero
// return. The year's only taxable income is the RMD (~$21k), which lands
// below the MFJ standard deduction — so the annual liability is tiny and,
// critically, identical no matter which month the RMD is withdrawn in.
func rmdTimingScenario(timing models.RMDTiming) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{ID: "primary", Name: "You", BirthMonth: models.BirthMonthForAge("2026-01", 76), Role: models.PersonRolePrimary},
		{ID: "spouse", Name: "Spouse", BirthMonth: models.BirthMonthForAge("2026-01", 74), Role: models.PersonRoleSpouse},
	}
	s.PortfolioValue = 500_000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.MonthlyLivingExpenses = 1_000
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.ExpenseSources = nil
	s.IncomeSources = nil
	s.SocialSecurity = nil
	s.ProjectionYears = 1
	s.InflationRate = 0
	s.InvestmentReturn = 0
	s.SpendingDeclineRate = 0
	s.Guardrails = nil
	s.TaxableDividendYield = 0
	s.TaxableQualifiedDividendPercent = 0
	s.TaxableCapitalGainsDistributionRate = 0
	s.RMDTiming = timing

	tc := models.DefaultTaxConfig()
	tc.FilingStatus = models.FilingMarriedJoint
	s.TaxConfig = tc
	return s
}

// TestRMDTimingDoesNotChangeAnnualTaxes pins the fix for the start-of-year
// RMD over-taxation defect: AnnualizedInputs annualized the RMD lump by
// 12/(monthInYear+1), so with rmd_timing=start_of_year the whole year's RMD
// was extrapolated x12 in month 0 and the pay-as-you-go estimator collected
// thousands in tax on a year whose true liability is ~$0 — and the floor at
// taxDue >= 0 meant the overpayment was never refunded within the year. The
// RMD is a discrete annual event (the full amount lands in one trigger
// month), so like RothConversions it must not be annualized. Withdrawal
// timing shapes growth drag, not the annual tax bill: with zero return and
// zero inflation the year's taxes must be identical across all timings.
func TestRMDTimingDoesNotChangeAnnualTaxes(t *testing.T) {
	taxesFor := func(timing models.RMDTiming) float64 {
		proj := New().Run(Input{Prepared: prepare.MustFrom(t, rmdTimingScenario(timing))})
		if len(proj.YearlySummaries) != 1 {
			t.Fatalf("timing %q: got %d yearly summaries, want 1", timing, len(proj.YearlySummaries))
		}
		return proj.YearlySummaries[0].Taxes
	}

	endOfYear := taxesFor(models.RMDTimingEndOfYear)
	for _, timing := range []models.RMDTiming{models.RMDTimingStartOfYear, models.RMDTimingMidYear} {
		got := taxesFor(timing)
		if math.Abs(got-endOfYear) > 1 {
			t.Errorf("year-0 taxes with rmd_timing=%q = %.2f, want %.2f (end_of_year): the RMD lump must not be annualized",
				timing, got, endOfYear)
		}
	}
}

// TestAnnualizedInputsDoesNotAnnualizeRMDLump pins the accumulator contract
// directly: recurring withdrawals are linearly annualized, the RMD lump is
// carried at face value, and both land in the same TaxableWithdrawals total.
func TestAnnualizedInputsDoesNotAnnualizeRMDLump(t *testing.T) {
	acc := ProjectionTaxAccumulator{RMDWithdrawalsYTD: 21_000}
	// Month 1 (second month of the year): factor 12/2 = 6.
	got := acc.AnnualizedInputs(1, MonthlyTaxInputs{TaxableWithdrawals: 500, RMDWithdrawals: 1_000})
	want := 500*6.0 + 21_000 + 1_000
	if math.Abs(got.TaxableWithdrawals-want) > 1e-9 {
		t.Errorf("TaxableWithdrawals = %.2f, want %.2f (recurring x6, RMD lump at face value)", got.TaxableWithdrawals, want)
	}
}
