package engine

import (
	"testing"

	"budget2/internal/models"
)

func TestTaxCalculatorAdjustedStandardDeduction(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingMarriedJoint,
		Age65Count:   3,
	}, 3)

	assertClose(t, "age 65 count is capped at two", tc.GetAdjustedStandardDeduction(0), 29200+2*1550)
	assertClose(t, "inflation adjusted deduction", tc.GetAdjustedStandardDeduction(2), (29200+2*1550)*1.03*1.03)
}

func TestCalculateFederalTaxUsesStandardDeductionAndMarginalBracket(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{FilingStatus: models.FilingSingle}, 0)

	tax, effectiveRate, marginalBracket := tc.CalculateFederalTax(100000, 0)

	assertClose(t, "federal tax", tax, 13841)
	assertClose(t, "effective rate", effectiveRate, 13.841)
	assertClose(t, "marginal bracket", marginalBracket, 22)
}

func TestCalculateTaxableSocialSecurityThresholds(t *testing.T) {
	cases := []struct {
		name                string
		ssBenefits          float64
		otherIncome         float64
		filingStatus        models.FilingStatus
		mfsLivedWithSpouse  bool
		wantTaxableBenefits float64
	}{
		{
			name:                "below base threshold",
			ssBenefits:          24000,
			otherIncome:         10000,
			filingStatus:        models.FilingSingle,
			wantTaxableBenefits: 0,
		},
		{
			name:                "above upper threshold",
			ssBenefits:          24000,
			otherIncome:         30000,
			filingStatus:        models.FilingSingle,
			wantTaxableBenefits: 11300,
		},
		{
			name:                "married separate lived with spouse taxes eighty five percent",
			ssBenefits:          24000,
			otherIncome:         0,
			filingStatus:        models.FilingMarriedSeparate,
			mfsLivedWithSpouse:  true,
			wantTaxableBenefits: 20400,
		},
		{
			name:                "married separate lived apart uses single thresholds",
			ssBenefits:          24000,
			otherIncome:         30000,
			filingStatus:        models.FilingMarriedSeparate,
			wantTaxableBenefits: 11300,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateTaxableSocialSecurity(tc.ssBenefits, tc.otherIncome, 0, 0, tc.filingStatus, tc.mfsLivedWithSpouse)
			assertClose(t, "taxable benefits", got, tc.wantTaxableBenefits)
		})
	}
}

func TestCalculateNIITThresholds(t *testing.T) {
	assertClose(t, "zero below threshold", CalculateNIIT(190000, 50000, models.FilingSingle), 0)
	assertClose(t, "single threshold excess", CalculateNIIT(260000, 50000, models.FilingSingle), 1900)
	assertClose(t, "mfs threshold excess capped by net investment income", CalculateNIIT(150000, 20000, models.FilingMarriedSeparate), 760)
}

func TestCalculateMonthlyIRMAAThresholdsAndInflation(t *testing.T) {
	assertClose(t, "zero magi", CalculateMonthlyIRMAA(0, models.FilingSingle, 1), 0)
	assertClose(t, "single first surcharge bracket", CalculateMonthlyIRMAA(120000, models.FilingSingle, 1), 81.20+14.50)
	assertClose(t, "inflated threshold and surcharge", CalculateMonthlyIRMAA(300000, models.FilingSingle, 2), (202.90+37.50)*2)
}

func TestCalculateTaxWithInvestmentIncomeBreakdown(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: models.FloatPtr(5),
	}, 0)

	breakdown := tc.CalculateTaxWithInvestmentIncomeBreakdown(30000, 30000, 40000, 5000, 0)

	assertClose(t, "federal tax", breakdown.FederalTax, 7372.25)
	assertClose(t, "state tax", breakdown.StateTax, 4270)
	assertClose(t, "niit", breakdown.NIIT, 0)
	assertClose(t, "total tax", breakdown.TotalTax, 11642.25)
	assertClose(t, "effective rate", breakdown.EffectiveRate, 11.64225)
	assertClose(t, "magi", breakdown.MAGI, 100000)
}

func TestEstimateRothConversionTaxAndMarginalRate(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{FilingStatus: models.FilingSingle}, 0)

	assertClose(t, "zero conversion", tc.EstimateRothConversionTax(50000, 0, 0), 0)
	assertClose(t, "conversion tax", tc.EstimateRothConversionTax(50000, 20000, 0), 3225)
	assertClose(t, "nonpositive marginal rate default", tc.GetMarginalRate(0, 0), 10)
	assertClose(t, "marginal rate", tc.GetMarginalRate(100000, 0), 22)
}

func TestRothConversionAmountForYear_PerYearOverride(t *testing.T) {
	s := &models.WhatIfSettings{
		RothConversion: &models.RothConversionConfig{
			Enabled:      true,
			AnnualAmount: 50_000,
			StartYear:    0,
			EndYear:      10,
			PerYearOverrides: map[int]float64{
				2: 75_000,
				3: 90_000,
			},
		},
	}
	// Year 2 → override wins.
	if got := RothConversionAmountForYear(s, 2, 1_000_000); got != 75_000 {
		t.Errorf("year 2 override: got %v, want 75000", got)
	}
	// Year 3 → override wins.
	if got := RothConversionAmountForYear(s, 3, 1_000_000); got != 90_000 {
		t.Errorf("year 3 override: got %v, want 90000", got)
	}
	// Year 4 → no override, falls back to AnnualAmount.
	if got := RothConversionAmountForYear(s, 4, 1_000_000); got != 50_000 {
		t.Errorf("year 4 fallback: got %v, want 50000", got)
	}
	// Override capped to availableTaxDeferred.
	if got := RothConversionAmountForYear(s, 2, 60_000); got != 60_000 {
		t.Errorf("override capped to available: got %v, want 60000", got)
	}
}

func TestRothConversionAmountForYear_BackwardsCompat(t *testing.T) {
	// Nil PerYearOverrides → identical to previous behavior.
	s := &models.WhatIfSettings{
		RothConversion: &models.RothConversionConfig{
			Enabled:      true,
			AnnualAmount: 50_000,
			StartYear:    1,
			EndYear:      5,
		},
	}
	cases := []struct {
		year int
		want float64
	}{
		{0, 0},      // before StartYear
		{1, 50_000},
		{3, 50_000},
		{5, 50_000},
		{6, 0}, // after EndYear
	}
	for _, c := range cases {
		if got := RothConversionAmountForYear(s, c.year, 1_000_000); got != c.want {
			t.Errorf("year %v: got %v, want %v", c.year, got, c.want)
		}
	}
}

func TestRothConversionAmountForYear_ZeroOverrideSuppresses(t *testing.T) {
	// A zero entry in PerYearOverrides must suppress the conversion
	// for that year, even when AnnualAmount is large. Bracket-fill
	// produces zero amounts in years where other income already fills
	// the target bracket; the engine must honor that.
	s := &models.WhatIfSettings{
		RothConversion: &models.RothConversionConfig{
			Enabled:          true,
			AnnualAmount:     50_000,
			StartYear:        0,
			EndYear:          10,
			PerYearOverrides: map[int]float64{3: 0},
		},
	}
	if got := RothConversionAmountForYear(s, 3, 1_000_000); got != 0 {
		t.Errorf("zero override should suppress conversion: got %v, want 0", got)
	}
	// Sanity: years without an override still use AnnualAmount.
	if got := RothConversionAmountForYear(s, 2, 1_000_000); got != 50_000 {
		t.Errorf("year without override: got %v, want 50000", got)
	}
}
