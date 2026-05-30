package analysis

import (
	"testing"

	"budget2/internal/models"
)

// The Present Value panel must be after-tax: when a projection is
// supplied, the projection's actual income taxes and IRMAA are discounted
// into Total Needs, so the coverage ratio and surplus match the after-tax
// Budget Analysis instead of reading optimistically pre-tax. Passing nil
// keeps the pre-tax estimate.
func TestPresentValue_DiscountsProjectionTaxes(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 70
	s.SocialSecurity = nil
	s.IncomeSources = nil
	s.PortfolioValue = 1_500_000
	s.TaxDeferredPercent = 100 // all pre-tax → RMDs + withdrawals generate ordinary-income tax
	s.RothPercent = 0
	s.MonthlyLivingExpenses = 8000
	s.InvestmentReturn = 5.0
	s.InflationRate = 3.0
	s.DiscountRate = 5.0
	s.ProjectionYears = 25

	proj, in := runProj(t, s)

	preTax := PresentValue(in, nil)
	afterTax := PresentValue(in, proj)

	if afterTax.PVTaxes <= 0 {
		t.Fatalf("expected positive discounted taxes when a taxable projection is supplied, got PVTaxes=%.2f", afterTax.PVTaxes)
	}
	if preTax.PVTaxes != 0 {
		t.Errorf("nil projection must stay pre-tax, got PVTaxes=%.2f", preTax.PVTaxes)
	}
	// Taxes are a need, so the after-tax view must show weaker coverage and
	// a smaller surplus than the pre-tax view.
	if !(afterTax.CoverageRatio < preTax.CoverageRatio) {
		t.Errorf("expected after-tax coverage (%.4f) < pre-tax coverage (%.4f)", afterTax.CoverageRatio, preTax.CoverageRatio)
	}
	if !(afterTax.SurplusDeficit < preTax.SurplusDeficit) {
		t.Errorf("expected after-tax surplus (%.2f) < pre-tax surplus (%.2f)", afterTax.SurplusDeficit, preTax.SurplusDeficit)
	}
	// Gap and surplus must reconcile with the published components.
	wantGap := afterTax.PVExpenses + afterTax.PVTaxes - afterTax.PVIncome
	if d := afterTax.PVGap - wantGap; d > 0.01 || d < -0.01 {
		t.Errorf("PVGap = %.2f, want PVExpenses+PVTaxes-PVIncome = %.2f", afterTax.PVGap, wantGap)
	}
}

// Property tax is part of the projection's expenses; the Present Value
// panel must include it in Total Needs rather than silently dropping it.
func TestPresentValue_IncludesPropertyTax(t *testing.T) {
	build := func(monthlyPropertyTax float64) *models.PresentValueAnalysis {
		s := models.DefaultWhatIfSettings()
		s.SocialSecurity = nil
		s.IncomeSources = nil
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.MonthlyLivingExpenses = 4000
		s.MonthlyPropertyTax = monthlyPropertyTax
		s.ProjectionYears = 30
		_, in := runProj(t, s)
		return PresentValue(in, nil) // nil → isolate the closed-form expense change
	}

	without := build(0)
	with := build(1000)

	if !(with.PVExpenses > without.PVExpenses) {
		t.Errorf("expected property tax to raise PV expenses: with=%.2f without=%.2f", with.PVExpenses, without.PVExpenses)
	}
}
