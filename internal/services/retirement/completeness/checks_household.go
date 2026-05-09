package completeness

import "budget2/internal/models"

const codeMFJNoSpousePerson = "mfj_no_spouse_person"

// checkMFJNoSpousePerson flags scenarios where filing status is married
// joint but the household has no spouse Person on record. This is an
// Error rather than a Warn because the projection is internally
// inconsistent: federal tax brackets, standard deduction, and Roth
// thresholds use the MFJ filing status (effectively two filers), while
// IRMAA, Medicare premium counts, and RMD calendars walk the Persons
// slice (one filer). The two halves of the projection disagree about
// household size.
//
// nil TaxConfig is treated as "no opinion" — DefaultTaxConfig will be
// applied at engine boundary if not explicitly set, but until the user
// has chosen a filing status we don't pretend to know.
func checkMFJNoSpousePerson(s *models.WhatIfSettings) *Finding {
	if s.TaxConfig == nil {
		return nil
	}
	if s.TaxConfig.FilingStatus != models.FilingMarriedJoint {
		return nil
	}
	if s.GetSpousePerson() != nil {
		return nil
	}
	return &Finding{
		Severity:   SeverityError,
		Code:       codeMFJNoSpousePerson,
		Title:      "Filing married-jointly but no spouse on record",
		Detail:     "Tax brackets and standard deduction assume two filers, but Medicare premiums, IRMAA, and RMD timing only see one person. Add a spouse or change the filing status.",
		FormAnchor: "whatif-portfolio-settings-card",
		Action:     "Add spouse",
	}
}
