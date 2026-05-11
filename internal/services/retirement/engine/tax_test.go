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
