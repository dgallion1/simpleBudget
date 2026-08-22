package models

// Affordable Care Act premium tax credits.
//
// Most planning tools ignore health coverage entirely. For anyone retired
// before 65 it can dominate every other consideration: the premium tax credit
// phases out at 400% of the federal poverty level as a CLIFF, not a ramp, so
// one dollar of extra income can cost the whole year's credit.
//
// Two facts about it are easy to get wrong and expensive to get wrong:
//
//   - It is measured against its own definition of income. ACA MAGI counts
//     100% of Social Security benefits, where § 86 provisional income counts
//     half and New York AGI counts none. A household comfortably under the
//     cliff on AGI can be over it on ACA MAGI.
//   - COBRA disqualifies you outright. COBRA is employer coverage for this
//     purpose, so enrolling in it forfeits the credit at ANY income. The
//     choice between COBRA and a marketplace plan is therefore not a premium
//     comparison; it can be worth more than the premiums either way.

const (
	// CoverageCOBRA is continuation coverage after employment ends.
	//
	// Kept distinct from CoverageEmployer despite being employer-sponsored,
	// because the cost behaves completely differently — the household now pays
	// the full unsubsidised premium plus an administrative fee — while the
	// premium-tax-credit disqualification is identical.
	CoverageCOBRA CoverageType = "cobra"
)

// DisqualifiesFromPremiumTaxCredit reports whether being enrolled in this kind
// of coverage forfeits the ACA premium tax credit regardless of income.
//
// Eligibility for employer coverage — COBRA included — bars the credit under
// 26 USC § 36B(c)(2)(B). Medicare enrollees are outside the marketplace
// credit entirely.
func (c CoverageType) DisqualifiesFromPremiumTaxCredit() bool {
	switch c {
	case CoverageEmployer, CoverageCOBRA, CoverageMedicare:
		return true
	default:
		return false
	}
}

// ACAConfig holds the household-level facts a premium tax credit depends on
// that are not derivable from the rest of the plan.
type ACAConfig struct {
	// HouseholdSize is the number of people in the tax household, which sets
	// where the federal poverty level — and therefore the 400% cliff — falls.
	// It is NOT the number of people on a marketplace plan: a household of
	// four with one member on a marketplace plan still measures against the
	// four-person poverty level. Zero means unset.
	HouseholdSize int `json:"household_size,omitempty"`

	// AnnualPremiumTaxCredit is the credit the household actually receives,
	// in dollars per year, as reported on Form 1095-A or shown in the
	// marketplace account.
	//
	// This is supplied rather than computed. Deriving it needs the
	// second-lowest-cost silver plan for the household's rating area and the
	// age of every enrollee, which is local data this planner does not carry;
	// inventing a national benchmark premium would produce a confident number
	// that is wrong everywhere. nil means unset, and the cliff's cost is then
	// unknown rather than assumed to be zero.
	AnnualPremiumTaxCredit *float64 `json:"annual_premium_tax_credit,omitempty"`

	// AdvanceCreditsTaken records whether the credit is being paid in advance
	// to the insurer each month rather than claimed at filing.
	//
	// It matters for more than cash flow. Advance credits are reconciled on
	// the return, and the repayment of excess advance credits is CAPPED at
	// lower incomes but UNCAPPED at and above 400% FPL. So realising a gain
	// that pushes the household over the cliff does not merely stop the
	// credit going forward — it claws back everything already paid out that
	// year, in one lump, at filing.
	AdvanceCreditsTaken bool `json:"advance_credits_taken,omitempty"`
}

// FederalPovertyLevel returns the poverty guideline for a household size,
// given the one-person figure and the increment per additional person.
//
// Household sizes below one are treated as one; there is no such thing as a
// zero-person household for this purpose, and returning zero would place the
// cliff at zero income.
func FederalPovertyLevel(onePerson, perAdditionalPerson float64, householdSize int) float64 {
	if householdSize < 1 {
		householdSize = 1
	}
	return onePerson + float64(householdSize-1)*perAdditionalPerson
}

// ACAConfigured reports whether enough is known to locate the cliff for this
// household: a size to measure the poverty level against.
func (a *ACAConfig) ACAConfigured() bool {
	return a != nil && a.HouseholdSize > 0
}

// CreditKnown reports whether the cost of crossing the cliff is known.
func (a *ACAConfig) CreditKnown() bool {
	return a != nil && a.AnnualPremiumTaxCredit != nil && *a.AnnualPremiumTaxCredit > 0
}

// AnnualCreditOrZero returns the configured credit, or zero when unset.
func (a *ACAConfig) AnnualCreditOrZero() float64 {
	if !a.CreditKnown() {
		return 0
	}
	return *a.AnnualPremiumTaxCredit
}
