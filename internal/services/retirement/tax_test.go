package retirement

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func TestNewTaxCalculator(t *testing.T) {
	config := &models.TaxConfig{
		FilingStatus:       models.FilingMarriedJoint,
		StateIncomeTaxRate: 5.0,
	}

	tc := NewTaxCalculator(config, 3.0)

	if tc.FilingStatus != models.FilingMarriedJoint {
		t.Errorf("Expected FilingMarriedJoint, got %v", tc.FilingStatus)
	}
	if tc.StateRate != 5.0 {
		t.Errorf("Expected StateRate 5.0, got %f", tc.StateRate)
	}
	if tc.InflationRate != 3.0 {
		t.Errorf("Expected InflationRate 3.0, got %f", tc.InflationRate)
	}
}

func TestNewTaxCalculatorWithNilConfig(t *testing.T) {
	tc := NewTaxCalculator(nil, 3.0)

	// Should use defaults
	if tc.FilingStatus != models.FilingMarriedJoint {
		t.Errorf("Expected default FilingMarriedJoint, got %v", tc.FilingStatus)
	}
}

func TestCalculateFederalTax(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingMarriedJoint,
		StateIncomeTaxRate: 0,
	}, 0)

	tests := []struct {
		name           string
		grossIncome    float64
		yearsFromBase  int
		expectTaxAbove float64
		expectTaxBelow float64
	}{
		{
			name:           "Zero income",
			grossIncome:    0,
			yearsFromBase:  0,
			expectTaxAbove: 0,
			expectTaxBelow: 1,
		},
		{
			name:           "Low income under deduction",
			grossIncome:    20000,
			yearsFromBase:  0,
			expectTaxAbove: 0,
			expectTaxBelow: 1,
		},
		{
			name:           "Middle income",
			grossIncome:    100000,
			yearsFromBase:  0,
			expectTaxAbove: 5000,  // Should owe some tax
			expectTaxBelow: 20000, // But not too much
		},
		{
			name:           "Higher income",
			grossIncome:    250000,
			yearsFromBase:  0,
			expectTaxAbove: 30000,
			expectTaxBelow: 80000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tax, effectiveRate, marginalBracket := tc.CalculateFederalTax(tt.grossIncome, tt.yearsFromBase)

			if tax < tt.expectTaxAbove || tax > tt.expectTaxBelow {
				t.Errorf("Tax %f not in expected range [%f, %f]", tax, tt.expectTaxAbove, tt.expectTaxBelow)
			}

			// Effective rate should be reasonable
			if tt.grossIncome > 0 {
				if effectiveRate < 0 || effectiveRate > 40 {
					t.Errorf("Effective rate %f out of reasonable range", effectiveRate)
				}
			}

			// Marginal bracket should be valid
			validBrackets := []float64{10, 12, 22, 24, 32, 35, 37}
			found := false
			for _, b := range validBrackets {
				if marginalBracket == b {
					found = true
					break
				}
			}
			if !found && tt.grossIncome > 0 {
				t.Errorf("Marginal bracket %f not a valid bracket", marginalBracket)
			}
		})
	}
}

func TestInflationAdjustedBrackets(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingSingle,
	}, 3.0) // 3% inflation

	baseBrackets := tc.GetAdjustedBrackets(0)
	futureBrackets := tc.GetAdjustedBrackets(10)

	// After 10 years of 3% inflation, brackets should be ~34% higher
	expectedFactor := math.Pow(1.03, 10) // ~1.344

	for i := range baseBrackets {
		if baseBrackets[i].MaxIncome < math.MaxFloat64 {
			ratio := futureBrackets[i].MinIncome / baseBrackets[i].MinIncome
			if baseBrackets[i].MinIncome > 0 {
				if ratio < expectedFactor*0.99 || ratio > expectedFactor*1.01 {
					t.Errorf("Bracket %d not properly adjusted: expected ratio ~%.3f, got %.3f", i, expectedFactor, ratio)
				}
			}
		}
	}
}

func TestEstimateRothConversionTax(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingMarriedJoint,
	}, 0)

	// Conversion should increase tax
	baseIncome := 50000.0
	conversionAmount := 25000.0

	additionalTax := tc.EstimateRothConversionTax(baseIncome, conversionAmount, 0)

	if additionalTax <= 0 {
		t.Errorf("Roth conversion should result in additional tax, got %f", additionalTax)
	}

	// Additional tax should be less than conversion amount at any bracket
	if additionalTax > conversionAmount*0.37 {
		t.Errorf("Additional tax %f exceeds maximum possible (37%% of conversion)", additionalTax)
	}

	// Zero conversion should result in zero additional tax
	zeroTax := tc.EstimateRothConversionTax(baseIncome, 0, 0)
	if zeroTax != 0 {
		t.Errorf("Zero conversion should result in zero additional tax, got %f", zeroTax)
	}
}

func TestGetMarginalRate(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus: models.FilingSingle,
	}, 0)

	tests := []struct {
		income       float64
		expectedRate float64
	}{
		{0, 10},
		{20000, 10},     // Under standard deduction
		{50000, 12},     // In 12% bracket
		{100000, 22},    // In 22% bracket
		{200000, 24},    // In 24% bracket
		{500000, 35},    // In 35% bracket
		{1000000, 37},   // In 37% bracket
	}

	for _, tt := range tests {
		rate := tc.GetMarginalRate(tt.income, 0)
		if rate != tt.expectedRate {
			t.Errorf("Income %f: expected marginal rate %f, got %f", tt.income, tt.expectedRate, rate)
		}
	}
}
