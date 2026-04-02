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
		{20000, 10},   // Under standard deduction
		{50000, 12},   // In 12% bracket
		{100000, 22},  // In 22% bracket
		{200000, 24},  // In 24% bracket
		{500000, 35},  // In 35% bracket
		{1000000, 37}, // In 37% bracket
	}

	for _, tt := range tests {
		rate := tc.GetMarginalRate(tt.income, 0)
		if rate != tt.expectedRate {
			t.Errorf("Income %f: expected marginal rate %f, got %f", tt.income, tt.expectedRate, rate)
		}
	}
}

func TestProjectionTaxAccumulatorEstimateMonthlyTaxes(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: 0,
	}, 0)

	t.Run("allocates recurring annual tax evenly across months", func(t *testing.T) {
		monthlyOrdinaryIncome := 6000.0
		accumulator := projectionTaxAccumulator{}

		wantFederal, wantState, wantTotal, _ := tc.CalculateTotalTax(monthlyOrdinaryIncome*12, 0)
		if wantFederal <= 0 || wantState != 0 {
			t.Fatalf("expected positive federal tax and zero state tax, got federal=%.2f state=%.2f", wantFederal, wantState)
		}

		month0Taxes := accumulator.estimateMonthlyTaxes(tc, 0, 0, monthlyOrdinaryIncome, 0, 0, 0, 0, 0, 0)
		if math.Abs(month0Taxes-wantTotal/12) > 0.01 {
			t.Fatalf("month 0 taxes = %.2f, want %.2f", month0Taxes, wantTotal/12)
		}

		accumulator.applyMonth(monthlyOrdinaryIncome, 0, 0, 0, 0, 0, 0, month0Taxes)
		month1Taxes := accumulator.estimateMonthlyTaxes(tc, 0, 1, monthlyOrdinaryIncome, 0, 0, 0, 0, 0, 0)
		if math.Abs(month1Taxes-wantTotal/12) > 0.01 {
			t.Fatalf("month 1 taxes = %.2f, want %.2f", month1Taxes, wantTotal/12)
		}
	})

	t.Run("treats roth conversions as one-time annual events", func(t *testing.T) {
		conversionAmount := 12000.0
		accumulator := projectionTaxAccumulator{}

		month0Taxes := accumulator.estimateMonthlyTaxes(tc, 0, 0, 0, 0, 0, 0, 0, 0, conversionAmount)
		_, _, wantTotal, _ := tc.CalculateTotalTax(conversionAmount, 0)

		if math.Abs(month0Taxes-wantTotal/12) > 0.01 {
			t.Fatalf("month 0 taxes = %.2f, want %.2f", month0Taxes, wantTotal/12)
		}
	})
}

func TestCalculateTaxWithInvestmentIncome(t *testing.T) {
	tc := NewTaxCalculator(&models.TaxConfig{
		FilingStatus:       models.FilingSingle,
		StateIncomeTaxRate: 0,
	}, 0)

	t.Run("qualified dividends taxed below ordinary income", func(t *testing.T) {
		ordinaryFederal, _, _, _ := tc.CalculateTaxWithInvestmentIncome(80000, 0, 0, 0)
		qualifiedFederal, _, _, _ := tc.CalculateTaxWithInvestmentIncome(0, 80000, 0, 0)
		if qualifiedFederal >= ordinaryFederal {
			t.Fatalf("expected qualified dividends to have lower federal tax than ordinary gains, got qualified=%.2f ordinary=%.2f", qualifiedFederal, ordinaryFederal)
		}
	})

	t.Run("ordinary income still taxes non-qualified dividends", func(t *testing.T) {
		federal, state, total, _ := tc.CalculateTaxWithInvestmentIncome(30000, 5000, 10000, 0)
		if federal <= 0 || total <= 0 {
			t.Fatalf("expected positive tax, got federal=%.2f total=%.2f", federal, total)
		}
		if state != 0 {
			t.Fatalf("expected zero state tax, got %.2f", state)
		}
	})
}

func TestCalculateTaxableSocialSecurity(t *testing.T) {
	tests := []struct {
		name        string
		ssBenefits  float64
		otherIncome float64
		qd          float64
		ltcg        float64
		status      models.FilingStatus
		want        float64
	}{
		{
			name:       "below first threshold",
			ssBenefits: 24000,
			status:     models.FilingSingle,
			want:       0,
		},
		{
			name:        "between thresholds",
			ssBenefits:  24000,
			otherIncome: 20000,
			status:      models.FilingSingle,
			want:        3500,
		},
		{
			name:        "above second threshold capped at 85 percent",
			ssBenefits:  36000,
			otherIncome: 80000,
			status:      models.FilingMarriedJoint,
			want:        30600,
		},
		{
			name:       "married separate treated as 85 percent taxable",
			ssBenefits: 30000,
			status:     models.FilingMarriedSeparate,
			want:       25500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateTaxableSocialSecurity(tt.ssBenefits, tt.otherIncome, tt.qd, tt.ltcg, tt.status)
			if math.Abs(got-tt.want) > 0.01 {
				t.Fatalf("CalculateTaxableSocialSecurity() = %.2f, want %.2f", got, tt.want)
			}
		})
	}
}

func TestCalculateNIIT(t *testing.T) {
	tests := []struct {
		name                string
		magi                float64
		netInvestmentIncome float64
		status              models.FilingStatus
		want                float64
	}{
		{
			name:                "below threshold",
			magi:                190000,
			netInvestmentIncome: 50000,
			status:              models.FilingSingle,
			want:                0,
		},
		{
			name:                "excess magi larger than investment income",
			magi:                260000,
			netInvestmentIncome: 5000,
			status:              models.FilingMarriedJoint,
			want:                190,
		},
		{
			name:                "investment income larger than excess magi",
			magi:                215000,
			netInvestmentIncome: 50000,
			status:              models.FilingSingle,
			want:                570,
		},
		{
			name:                "married separate threshold",
			magi:                140000,
			netInvestmentIncome: 10000,
			status:              models.FilingMarriedSeparate,
			want:                380,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateNIIT(tt.magi, tt.netInvestmentIncome, tt.status)
			if math.Abs(got-tt.want) > 0.01 {
				t.Fatalf("CalculateNIIT() = %.2f, want %.2f", got, tt.want)
			}
		})
	}
}

func TestCalculateMonthlyIRMAA(t *testing.T) {
	tests := []struct {
		name            string
		magi            float64
		status          models.FilingStatus
		inflationFactor float64
		want            float64
	}{
		{
			name:            "below first tier",
			magi:            100000,
			status:          models.FilingSingle,
			inflationFactor: 1,
			want:            0,
		},
		{
			name:            "middle tier",
			magi:            160000,
			status:          models.FilingSingle,
			inflationFactor: 1,
			want:            240.40,
		},
		{
			name:            "top tier",
			magi:            800000,
			status:          models.FilingMarriedJoint,
			inflationFactor: 1,
			want:            578.00,
		},
		{
			name:            "inflation adjusts thresholds",
			magi:            110000,
			status:          models.FilingSingle,
			inflationFactor: 1.05,
			want:            0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateMonthlyIRMAA(tt.magi, tt.status, tt.inflationFactor)
			if math.Abs(got-tt.want) > 0.01 {
				t.Fatalf("CalculateMonthlyIRMAA() = %.2f, want %.2f", got, tt.want)
			}
		})
	}
}
