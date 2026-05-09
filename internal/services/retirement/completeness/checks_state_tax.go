package completeness

import "budget2/internal/models"

const codeStateTaxUnset = "state_tax_unset"

// checkStateTaxUnset flags scenarios where the user has not set a state
// income tax rate at all. The field is *float64: nil means unset (fire
// the warning), and any explicit value — including ptr-to-0 — means
// configured. This lets users in no-income-tax states (FL, TX, WA, etc.)
// dismiss the banner by entering 0, while users in tax states still get
// prompted until they set a rate.
func checkStateTaxUnset(s *models.WhatIfSettings) *Finding {
	if s.TaxConfig != nil && s.TaxConfig.StateIncomeTaxRate != nil {
		return nil
	}
	return &Finding{
		Severity:   SeverityWarn,
		Code:       codeStateTaxUnset,
		Title:      "No state income tax configured",
		Detail:     "Projections currently model federal tax only. If you live in a state with income tax, your after-tax balances are overstated. Enter 0 if you live in a no-income-tax state (FL, TX, WA, etc.).",
		FormAnchor: "state-income-tax-rate-input",
		Action:     "Set state tax rate",
	}
}
