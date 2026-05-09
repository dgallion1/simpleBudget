package completeness

import "budget2/internal/models"

const codeStateTaxUnset = "state_tax_unset"

// checkStateTaxUnset flags scenarios where state income tax is silently
// zero. The engine's tax calculator reads TaxConfig.StateIncomeTaxRate
// and applies state tax correctly when it is non-zero — the gap is that
// nothing prompts the user to set it. A user in a tax state will see
// federal tax modeled but state tax silently absent.
//
// nil TaxConfig and zero rate are both treated as "unset". They produce
// the same outcome (no state tax computed), so the user sees one
// uniform warning.
func checkStateTaxUnset(s *models.WhatIfSettings) *Finding {
	if s.TaxConfig.StateIncomeTaxRateOrZero() > 0 {
		return nil
	}
	return &Finding{
		Severity:   SeverityWarn,
		Code:       codeStateTaxUnset,
		Title:      "No state income tax configured",
		Detail:     "Projections currently model federal tax only. If you live in a state with income tax, your after-tax balances are overstated.",
		FormAnchor: "state-income-tax-rate-input",
		Action:     "Set state tax rate",
	}
}
