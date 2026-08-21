package completeness

import "budget2/internal/models"

const codeTaxableCostBasisUnset = "taxable_cost_basis_unset"

// checkTaxableCostBasisUnset flags scenarios with a taxable brokerage
// balance but no cost basis supplied. The field is *float64: nil means unset
// (fire the warning), and any explicit value — including ptr-to-0, a fully
// appreciated position — means configured.
//
// This matters more than most silent defaults. With no basis the engine
// treats the account's starting market value as its own basis, so the
// projection begins with zero unrealized gain and understates the tax on
// every taxable withdrawal for the entire horizon. Unlike a missing
// assumption that shifts one year, this one compounds.
//
// Scenarios with no taxable balance at all (100% tax-deferred plus Roth) get
// no finding — there is nothing to have a basis for.
func checkTaxableCostBasisUnset(s *models.WhatIfSettings) *Finding {
	if s.TaxableCostBasis != nil {
		return nil
	}
	if taxableBalance(s) <= 0 {
		return nil
	}
	return &Finding{
		Severity:   SeverityWarn,
		Code:       codeTaxableCostBasisUnset,
		Title:      "No cost basis for the taxable account",
		Detail:     "Projections currently assume your taxable holdings have no unrealized gain, so the tax on selling them is understated for every year of the plan. Enter what you paid for them — your broker reports it as cost basis. Enter 0 if the position is fully appreciated.",
		FormAnchor: "taxable-cost-basis-input",
		Action:     "Set cost basis",
	}
}

// taxableBalance is the scenario's starting taxable brokerage value: whatever
// share of the portfolio is neither tax-deferred nor Roth. Mirrors the seeding
// arithmetic in engine.NewProjectionState.
func taxableBalance(s *models.WhatIfSettings) float64 {
	taxDeferred := s.PortfolioValue * (s.TaxDeferredPercent / 100)
	roth := s.PortfolioValue * (s.RothPercent / 100)
	return s.PortfolioValue - taxDeferred - roth
}
