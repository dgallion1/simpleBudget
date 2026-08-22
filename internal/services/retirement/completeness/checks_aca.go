package completeness

import "budget2/internal/models"

const (
	codeACAHouseholdSizeUnset = "aca_household_size_unset"
	codeACACreditUnset        = "aca_premium_credit_unset"
	codeACACOBRAForfeits      = "aca_cobra_forfeits_credit"
)

// checkACAHouseholdSizeUnset flags a household with someone on a marketplace
// plan but no household size on file.
//
// Without it the 400%-of-poverty-level cliff cannot be located at all, and
// that cliff is a step: one dollar over forfeits the whole year's premium tax
// credit. For anyone retired before 65 it is routinely the largest
// discontinuity in the plan, larger than any tax bracket.
func checkACAHouseholdSizeUnset(s *models.WhatIfSettings) *Finding {
	if !anyMarketplaceCoverage(s) {
		return nil
	}
	if s.ACA != nil && s.ACA.HouseholdSize > 0 {
		return nil
	}
	return &Finding{
		Severity: SeverityWarn,
		Code:     codeACAHouseholdSizeUnset,
		Title:    "No household size for marketplace coverage",
		Detail: "Someone in this plan is on a marketplace health plan, but without a household size " +
			"the 400% federal-poverty-level subsidy cliff cannot be placed. Crossing that line by one " +
			"dollar forfeits the entire year's premium tax credit. Count everyone in the tax household, " +
			"not just the people on the plan.",
		FormAnchor: "aca-household-size-input",
		Action:     "Set household size",
	}
}

// checkACACreditUnset flags a located cliff whose cost is unknown.
//
// The plan can say how close the household comes to the cliff without this,
// but not what crossing would cost, and a cliff shown with a cost of zero
// reads as harmless when it is merely unmeasured.
func checkACACreditUnset(s *models.WhatIfSettings) *Finding {
	if !anyMarketplaceCoverage(s) {
		return nil
	}
	if s.ACA == nil || s.ACA.HouseholdSize <= 0 {
		return nil // the size warning already covers this household
	}
	if s.ACA.CreditKnown() {
		return nil
	}
	return &Finding{
		Severity: SeverityWarn,
		Code:     codeACACreditUnset,
		Title:    "Premium tax credit amount not set",
		Detail: "The subsidy cliff can be located for this household but not priced, so it is shown " +
			"costing nothing when it may cost thousands. Enter the annual premium tax credit from " +
			"Form 1095-A or the marketplace account. It is not computed here because that needs the " +
			"benchmark silver plan for your rating area, which this planner does not carry.",
		FormAnchor: "aca-premium-credit-input",
		Action:     "Set credit amount",
	}
}

// checkACACOBRAForfeitsCredit flags a household bridging to Medicare on COBRA.
//
// COBRA is employer coverage for premium-tax-credit purposes, so enrolling in
// it forfeits the credit at any income. That makes the COBRA-versus-
// marketplace decision much more than a premium comparison, and it is not a
// trade-off most people know they are making.
func checkACACOBRAForfeitsCredit(s *models.WhatIfSettings) *Finding {
	if s == nil {
		return nil
	}
	onCOBRA := false
	for i := range s.HealthcarePersons {
		if s.HealthcarePersons[i].CurrentCoverage == models.CoverageCOBRA {
			onCOBRA = true
			break
		}
	}
	if !onCOBRA || anyMarketplaceCoverage(s) {
		return nil
	}
	if !models.CoverageCOBRA.DisqualifiesFromPremiumTaxCredit() {
		return nil // rule changed; the finding would be wrong
	}
	return &Finding{
		Severity: SeverityInfo,
		Code:     codeACACOBRAForfeits,
		Title:    "COBRA forfeits the premium tax credit",
		Detail: "COBRA counts as employer coverage, so enrolling in it disqualifies you from the ACA " +
			"premium tax credit at any income — no subsidy cliff applies because there is no subsidy. " +
			"A marketplace plan may cost less after credits even when its sticker premium is higher.",
		FormAnchor: "healthcare-coverage",
		Action:     "Compare marketplace coverage",
	}
}

// anyMarketplaceCoverage reports whether anyone starts out on a marketplace
// plan. Checked against current coverage rather than a projected year, since
// completeness is about what the user has told us, not about what the
// projection derives.
func anyMarketplaceCoverage(s *models.WhatIfSettings) bool {
	if s == nil {
		return false
	}
	for i := range s.HealthcarePersons {
		if s.HealthcarePersons[i].CurrentCoverage == models.CoverageACA {
			return true
		}
	}
	return false
}
