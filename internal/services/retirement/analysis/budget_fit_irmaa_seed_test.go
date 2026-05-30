package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// A deliberate current-year Roth conversion must not inflate the current
// year's IRMAA. IRMAA uses a two-year MAGI lookback, so a conversion done
// today only affects the surcharge two years later (the projection
// captures that via real MAGI history). BudgetFit's first-year IRMAA
// lookback seed therefore excludes the current-year conversion, so the
// "Current (Today)" IRMAA is identical whether or not a conversion is
// scheduled this year.
func TestBudgetFit_IRMAASeedExcludesCurrentYearRothConversion(t *testing.T) {
	build := func(withConversion bool) *models.BudgetFitAnalysis {
		s := models.DefaultWhatIfSettings()
		s.StartDate = "2026-01"
		// Age is derived from the primary Person's BirthMonth (prepare
		// overrides CurrentAge). Make the primary 67 → Medicare-eligible.
		s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 67)
		s.SocialSecurity = nil
		s.MonthlyHealthcare = 0
		s.HealthcarePersons = nil
		s.PortfolioValue = 2_000_000
		s.TaxDeferredPercent = 100 // all pre-tax: room to convert, no taxable distributions
		s.RothPercent = 0
		s.InvestmentReturn = 5.0
		s.InflationRate = 3.0
		s.MonthlyLivingExpenses = 5000
		s.ProjectionYears = 25
		s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingMarriedJoint}
		// ~$250k/yr ordinary income → MAGI ≈ $250k, in the MFJ $218k–$274k
		// IRMAA tier. A $150k conversion would lift the seed to ≈ $400k (a
		// higher tier) unless it's correctly excluded.
		s.IncomeSources = []models.IncomeSource{{Name: "Pension", Amount: 20834, StartMonth: 0}}
		if withConversion {
			s.RothConversion = &models.RothConversionConfig{Enabled: true, StartYear: 0, EndYear: 0, AnnualAmount: 150_000}
		}
		_, in := runProj(t, s)
		return BudgetFit(in, nil)
	}

	withConv := build(true)
	without := build(false)

	if without.MonthlyIRMAA <= 0 {
		t.Fatalf("test needs a non-zero baseline IRMAA, got %.2f", without.MonthlyIRMAA)
	}
	if math.Abs(withConv.MonthlyIRMAA-without.MonthlyIRMAA) > 0.01 {
		t.Errorf("current-year IRMAA must not change with a current-year Roth conversion: withConversion=%.2f without=%.2f",
			withConv.MonthlyIRMAA, without.MonthlyIRMAA)
	}
}
