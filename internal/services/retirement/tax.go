package retirement

import (
	"math"

	"budget2/internal/models"
)

// FederalTaxBracket represents a single tax bracket
type FederalTaxBracket struct {
	MinIncome float64 // Minimum income for this bracket
	MaxIncome float64 // Maximum income (math.MaxFloat64 for top bracket)
	Rate      float64 // Tax rate as decimal (e.g., 0.22 for 22%)
}

// TaxBrackets2024 contains 2024 federal tax brackets by filing status
// Source: IRS Revenue Procedure 2023-34
var TaxBrackets2024 = map[models.FilingStatus][]FederalTaxBracket{
	models.FilingSingle: {
		{0, 11600, 0.10},
		{11600, 47150, 0.12},
		{47150, 100525, 0.22},
		{100525, 191950, 0.24},
		{191950, 243725, 0.32},
		{243725, 609350, 0.35},
		{609350, math.MaxFloat64, 0.37},
	},
	models.FilingMarriedJoint: {
		{0, 23200, 0.10},
		{23200, 94300, 0.12},
		{94300, 201050, 0.22},
		{201050, 383900, 0.24},
		{383900, 487450, 0.32},
		{487450, 731200, 0.35},
		{731200, math.MaxFloat64, 0.37},
	},
	models.FilingMarriedSeparate: {
		{0, 11600, 0.10},
		{11600, 47150, 0.12},
		{47150, 100525, 0.22},
		{100525, 191950, 0.24},
		{191950, 243725, 0.32},
		{243725, 365600, 0.35},
		{365600, math.MaxFloat64, 0.37},
	},
	models.FilingHeadOfHousehold: {
		{0, 16550, 0.10},
		{16550, 63100, 0.12},
		{63100, 100500, 0.22},
		{100500, 191950, 0.24},
		{191950, 243700, 0.32},
		{243700, 609350, 0.35},
		{609350, math.MaxFloat64, 0.37},
	},
}

// StandardDeduction2024 contains 2024 standard deductions by filing status
var StandardDeduction2024 = map[models.FilingStatus]float64{
	models.FilingSingle:          14600,
	models.FilingMarriedJoint:    29200,
	models.FilingMarriedSeparate: 14600,
	models.FilingHeadOfHousehold: 21900,
}

// TaxCalculator computes federal and state income taxes
type TaxCalculator struct {
	FilingStatus  models.FilingStatus
	StateRate     float64 // State income tax rate as percentage (e.g., 5.0 for 5%)
	InflationRate float64 // Annual inflation rate for bracket adjustment
	BaseYear      int     // Year the brackets are based on (2024)
}

// NewTaxCalculator creates a tax calculator with the given configuration
func NewTaxCalculator(config *models.TaxConfig, inflationRate float64) *TaxCalculator {
	if config == nil {
		config = models.DefaultTaxConfig()
	}
	return &TaxCalculator{
		FilingStatus:  config.FilingStatus,
		StateRate:     config.StateIncomeTaxRate,
		InflationRate: inflationRate,
		BaseYear:      2024,
	}
}

// GetAdjustedBrackets returns tax brackets adjusted for inflation from base year
func (tc *TaxCalculator) GetAdjustedBrackets(yearsFromBase int) []FederalTaxBracket {
	baseBrackets := TaxBrackets2024[tc.FilingStatus]
	if baseBrackets == nil {
		baseBrackets = TaxBrackets2024[models.FilingMarriedJoint]
	}

	if yearsFromBase <= 0 {
		return baseBrackets
	}

	// Adjust bracket thresholds for inflation
	inflationFactor := math.Pow(1+tc.InflationRate/100, float64(yearsFromBase))
	adjusted := make([]FederalTaxBracket, len(baseBrackets))

	for i, bracket := range baseBrackets {
		adjusted[i] = FederalTaxBracket{
			MinIncome: bracket.MinIncome * inflationFactor,
			MaxIncome: bracket.MaxIncome,
			Rate:      bracket.Rate,
		}
		// Don't adjust MaxFloat64
		if bracket.MaxIncome < math.MaxFloat64 {
			adjusted[i].MaxIncome = bracket.MaxIncome * inflationFactor
		}
	}

	return adjusted
}

// GetAdjustedStandardDeduction returns standard deduction adjusted for inflation
func (tc *TaxCalculator) GetAdjustedStandardDeduction(yearsFromBase int) float64 {
	baseDeduction := StandardDeduction2024[tc.FilingStatus]
	if baseDeduction == 0 {
		baseDeduction = StandardDeduction2024[models.FilingMarriedJoint]
	}

	if yearsFromBase <= 0 {
		return baseDeduction
	}

	inflationFactor := math.Pow(1+tc.InflationRate/100, float64(yearsFromBase))
	return baseDeduction * inflationFactor
}

// CalculateFederalTax computes federal tax on taxable income
// Returns: tax amount, effective rate (%), marginal bracket rate (%)
func (tc *TaxCalculator) CalculateFederalTax(grossIncome float64, yearsFromBase int) (tax float64, effectiveRate float64, marginalBracket float64) {
	if grossIncome <= 0 {
		return 0, 0, 0
	}

	// Apply standard deduction
	standardDeduction := tc.GetAdjustedStandardDeduction(yearsFromBase)
	taxableIncome := math.Max(0, grossIncome-standardDeduction)

	if taxableIncome <= 0 {
		return 0, 0, 10 // Still in lowest bracket
	}

	brackets := tc.GetAdjustedBrackets(yearsFromBase)

	var totalTax float64
	var currentBracketRate float64

	for _, bracket := range brackets {
		if taxableIncome <= bracket.MinIncome {
			break
		}

		// Calculate taxable amount in this bracket
		bracketMin := bracket.MinIncome
		bracketMax := math.Min(bracket.MaxIncome, taxableIncome)
		taxableInBracket := bracketMax - bracketMin

		if taxableInBracket > 0 {
			totalTax += taxableInBracket * bracket.Rate
			currentBracketRate = bracket.Rate
		}
	}

	effectiveRate = (totalTax / grossIncome) * 100
	marginalBracket = currentBracketRate * 100

	return totalTax, effectiveRate, marginalBracket
}

// CalculateStateTax computes state income tax
func (tc *TaxCalculator) CalculateStateTax(taxableIncome float64) float64 {
	if taxableIncome <= 0 || tc.StateRate <= 0 {
		return 0
	}
	return taxableIncome * (tc.StateRate / 100)
}

// CalculateTotalTax computes combined federal and state tax
func (tc *TaxCalculator) CalculateTotalTax(grossIncome float64, yearsFromBase int) (federalTax, stateTax, totalTax, effectiveRate float64) {
	federalTax, _, _ = tc.CalculateFederalTax(grossIncome, yearsFromBase)

	// State tax is typically on AGI (gross income minus adjustments)
	// Simplified: apply to same taxable income base
	standardDeduction := tc.GetAdjustedStandardDeduction(yearsFromBase)
	taxableForState := math.Max(0, grossIncome-standardDeduction)
	stateTax = tc.CalculateStateTax(taxableForState)

	totalTax = federalTax + stateTax
	if grossIncome > 0 {
		effectiveRate = (totalTax / grossIncome) * 100
	}

	return federalTax, stateTax, totalTax, effectiveRate
}

// EstimateRothConversionTax estimates the tax impact of a Roth conversion
// Returns the additional tax owed due to the conversion
func (tc *TaxCalculator) EstimateRothConversionTax(baseIncome, conversionAmount float64, yearsFromBase int) float64 {
	if conversionAmount <= 0 {
		return 0
	}

	// Tax without conversion
	taxWithout, _, _ := tc.CalculateFederalTax(baseIncome, yearsFromBase)

	// Tax with conversion (conversion is added to ordinary income)
	taxWith, _, _ := tc.CalculateFederalTax(baseIncome+conversionAmount, yearsFromBase)

	return taxWith - taxWithout
}

// GetMarginalRate returns the marginal tax rate for a given income level
func (tc *TaxCalculator) GetMarginalRate(grossIncome float64, yearsFromBase int) float64 {
	if grossIncome <= 0 {
		return 10 // Lowest bracket
	}

	standardDeduction := tc.GetAdjustedStandardDeduction(yearsFromBase)
	taxableIncome := math.Max(0, grossIncome-standardDeduction)

	brackets := tc.GetAdjustedBrackets(yearsFromBase)

	for _, bracket := range brackets {
		if taxableIncome <= bracket.MaxIncome {
			return bracket.Rate * 100
		}
	}

	// Top bracket
	return 37
}
