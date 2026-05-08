package retirement

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// Tax base years. Duplicated from engine because plannerIRMAAInflationFactorForYear
// (which lives in calculator.go for now) reads them. Removed in Task 8
// alongside calculator.go.
const (
	taxBaseYear   = 2024
	irmaaBaseYear = 2026
)

// FederalTaxBracket re-exported from engine so existing retirement
// callers and tests keep compiling during the migration window.
type FederalTaxBracket = engine.FederalTaxBracket

// TaxCalculator re-exported from engine. Removed in Task 8.
type TaxCalculator = engine.TaxCalculator

// Bundled tax tables — re-exported as aliased values so test helpers
// that read TaxBrackets2024 etc. keep working.
var (
	TaxBrackets2024                      = engine.TaxBrackets2024
	LongTermCapitalGainsBrackets2024     = engine.LongTermCapitalGainsBrackets2024
	StandardDeduction2024                = engine.StandardDeduction2024
	AdditionalStandardDeduction2024Age65 = engine.AdditionalStandardDeduction2024Age65
)

// Function-value aliases. Tests and the rest of the retirement package
// invoke these by name; the call expression is identical to a normal
// function call.
var (
	NewTaxCalculator               = engine.NewTaxCalculator
	CalculateTaxableSocialSecurity = engine.CalculateTaxableSocialSecurity
	CalculateNIIT                  = engine.CalculateNIIT
	CalculateMonthlyIRMAA          = engine.CalculateMonthlyIRMAA
)

// normalizeFilingStatus is referenced by tests; alias to the exported
// engine helper.
func normalizeFilingStatus(filingStatus models.FilingStatus) models.FilingStatus {
	return engine.NormalizeFilingStatus(filingStatus)
}
