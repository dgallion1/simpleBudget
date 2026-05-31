package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// Bracket-fill conversion sizing must inflate the IRS bracket ceilings to the
// plan's calendar year, exactly as the engine inflates its own brackets
// (TaxCalculator.InflationFactor keyed on YearsFromTaxBase). With frozen 2024
// ceilings the estimator systematically undersizes conversions in later plan
// years, leaving real bracket room unused. This pins each year's conversion to
// the inflated ceiling.
//
// The scenario zeroes every other source of taxable income (no SS, no income
// sources, no dividends, pre-RMD ages) so estimateOtherTaxableIncome is 0 and
// each year's conversion equals the inflated bracket ceiling for that year.
func TestBracketFill_InflatesCeilingToPlanYear(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 60)
	s.Persons = s.Persons[:1] // single filer, no spouse
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.SocialSecurity = nil
	s.IncomeSources = nil
	s.TaxableDividendYield = 0
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.InflationRate = 3.0
	s.ProjectionYears = 10

	in := engineInput(t, s)
	ps := in.Prepared.Settings()

	const target = 0.12
	base, ok := bracketTopFor(ps.TaxConfig.FilingStatus, target)
	if !ok {
		t.Fatalf("bracketTopFor(%v, %.2f) not found", ps.TaxConfig.FilingStatus, target)
	}

	strat := models.RothOptimizerStrategy{
		Kind:          models.RothStrategyBracketFill,
		TargetBracket: target,
		StartAge:      60, // == CurrentAge → projection year 0
		EndAge:        65, // exclusive → years 0..4, all pre-RMD
	}
	ycs := strategyYearlyConversions(ps, strat)
	if len(ycs) != 5 {
		t.Fatalf("expected 5 yearly conversions (ages 60-64), got %d", len(ycs))
	}

	for i, yc := range ycs {
		projYear := yc.Age - ps.CurrentAge
		yfb := engine.YearsFromTaxBase(ps, projYear)
		factor := 1.0
		if yfb > 0 {
			factor = math.Pow(1+ps.InflationRate/100, float64(yfb))
		}
		want := base * factor
		// other taxable income is 0 in this scenario, so conv == inflated ceiling.
		if math.Abs(yc.Amount-want) > 1 {
			t.Errorf("year %d (age %d): conversion = %.2f, want inflated ceiling %.2f (base %.0f × %.4f)",
				i, yc.Age, yc.Amount, want, base, factor)
		}
	}

	// And the conversion must strictly grow year over year (ceilings inflate).
	for i := 1; i < len(ycs); i++ {
		if !(ycs[i].Amount > ycs[i-1].Amount) {
			t.Errorf("conversion should grow with inflated ceilings: year %d = %.2f, year %d = %.2f",
				i-1, ycs[i-1].Amount, i, ycs[i].Amount)
		}
	}
}
