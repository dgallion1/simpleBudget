package completeness

import (
	"time"

	"budget2/internal/models"
)

const (
	codeSSUnconfigured = "ss_unconfigured"
	codeSSPartial      = "ss_partial"
)

// ssAttentionAge is the age at which we expect a user to start thinking
// about Social Security. Below this, the absence of SS configuration is
// not flagged — many users build long-horizon scenarios decades before
// claiming and shouldn't be nagged.
const ssAttentionAge = 50

// checkSSUnconfigured flags scenarios where Social Security is entirely
// absent (nil pointer) and at least one Person is at or near claiming
// age. The engine silently produces zero SS income when SocialSecurity
// is nil — for retirees this can mean tens of thousands of dollars per
// year of missing income with no visual indication.
//
// We only flag when a Person is age >= 50 because younger users
// modelling far-out retirements legitimately may not yet know their
// FRA benefit.
func checkSSUnconfigured(s *models.WhatIfSettings) *Finding {
	if s.SocialSecurity != nil {
		return nil
	}
	if !anyPersonAtLeast(s, ssAttentionAge) {
		return nil
	}
	return &Finding{
		Severity:   SeverityWarn,
		Code:       codeSSUnconfigured,
		Title:      "Social Security not configured",
		Detail:     "No FRA benefit or claim age set. The projection assumes zero Social Security income, which can underestimate retirement income by hundreds of thousands of dollars over a 30-year horizon.",
		FormAnchor: "whatif-social-security-card",
		Action:     "Add Social Security",
	}
}

// checkSSPartial flags scenarios where SS is partially configured —
// a benefit is entered but no claim age, or vice versa. The engine
// guards against this case by returning zero income when ClaimAge is
// zero, but the user has clearly intended to configure SS, so silent
// zero is a likely surprise.
//
// One finding covers both primary and spouse — emitted if either side
// is partial.
func checkSSPartial(s *models.WhatIfSettings) *Finding {
	if s.SocialSecurity == nil {
		return nil
	}
	ss := s.SocialSecurity

	primaryPartial := ss.FRABenefit > 0 && ss.ClaimAge == 0
	spousePartial := ss.SpouseFRABenefit > 0 && ss.SpouseClaimAge == 0

	if !primaryPartial && !spousePartial {
		return nil
	}
	return &Finding{
		Severity:   SeverityWarn,
		Code:       codeSSPartial,
		Title:      "Social Security claim age missing",
		Detail:     "A Social Security benefit is configured but no claim age is set. The engine treats this as zero income — the entered benefit has no effect until you also pick a claim age.",
		FormAnchor: "whatif-social-security-card",
		Action:     "Set claim age",
	}
}

// anyPersonAtLeast returns true if any Person in settings is at or
// above the given age as of the scenario's start date.
//
// Falls back to settings.CurrentAge / SpouseAge when the Person record
// lacks BirthMonth (legacy scenarios). This is intentional: legacy
// scenarios should still trigger the warning rather than silently slip
// through because BirthMonth is the empty string.
func anyPersonAtLeast(s *models.WhatIfSettings, minAge int) bool {
	startYear := parseStartYear(s.StartDate)
	for _, p := range s.Persons {
		if personAge(p, startYear) >= minAge {
			return true
		}
	}
	if s.CurrentAge >= minAge {
		return true
	}
	if s.SpouseAge >= minAge {
		return true
	}
	return false
}

// personAge computes the age in (whole) years at the given reference
// year. BirthMonth is "YYYY-MM"; if it cannot be parsed we return 0
// so the Person doesn't trigger age-gated checks (the CurrentAge
// fallback in anyPersonAtLeast still applies).
func personAge(p models.Person, refYear int) int {
	if len(p.BirthMonth) < 4 {
		return 0
	}
	birthYear, err := atoiFour(p.BirthMonth[:4])
	if err != nil {
		return 0
	}
	return refYear - birthYear
}

func parseStartYear(startDate string) int {
	if len(startDate) < 4 {
		return time.Now().Year()
	}
	y, err := atoiFour(startDate[:4])
	if err != nil {
		return time.Now().Year()
	}
	return y
}

// atoiFour parses exactly four ASCII digits. We avoid strconv.Atoi to
// keep the function allocation-free and to refuse anything that isn't
// a clean YYYY (e.g. "20xx").
func atoiFour(s string) (int, error) {
	if len(s) != 4 {
		return 0, errBadYear
	}
	n := 0
	for i := 0; i < 4; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, errBadYear
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

type completenessError string

func (e completenessError) Error() string { return string(e) }

const errBadYear completenessError = "completeness: year is not four digits"
