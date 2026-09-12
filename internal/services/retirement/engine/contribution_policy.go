package engine

import "fmt"

// ContributionPolicy is the versioned standard employee 401(k) policy used by
// every contribution calculation. FrozenFromYear is nonzero when unpublished
// future-year dollar limits use the latest published entry.
type ContributionPolicy struct {
	PolicyYear               int
	FrozenFromYear           int
	RegularDeferralLimit     float64
	AnnualAdditionsLimit     float64
	CompensationLimit        float64
	OrdinaryCatchUpLimit     float64
	Age60To63CatchUpLimit    float64
	RothCatchUpWageThreshold float64
	Assumption               string
	SourceURLs               []string
}

var contributionPolicies = map[int]ContributionPolicy{
	2026: {
		PolicyYear:               2026,
		RegularDeferralLimit:     24500,
		AnnualAdditionsLimit:     72000,
		CompensationLimit:        360000,
		OrdinaryCatchUpLimit:     8000,
		Age60To63CatchUpLimit:    11250,
		RothCatchUpWageThreshold: 150000,
		Assumption:               "Published 2026 standard employee 401(k) dollar limits.",
		SourceURLs: []string{
			"https://www.irs.gov/retirement-plans/plan-participant-employee/retirement-topics-401k-and-profit-sharing-plan-contribution-limits",
			"https://www.irs.gov/retirement-plans/plan-participant-employee/retirement-topics-catch-up-contributions",
			"https://www.irs.gov/irb/2025-49_IRB",
		},
	},
}

// ContributionPolicyForYear returns a published policy or visibly freezes the
// latest published dollar limits for a future year. Earlier unsupported years
// fail rather than silently applying the wrong law.
func ContributionPolicyForYear(year int) (ContributionPolicy, error) {
	if p, ok := contributionPolicies[year]; ok {
		p.SourceURLs = append([]string(nil), p.SourceURLs...)
		return p, nil
	}
	latest := 2026
	if year > latest {
		p := contributionPolicies[latest]
		p.SourceURLs = append([]string(nil), p.SourceURLs...)
		p.PolicyYear = year
		p.FrozenFromYear = latest
		p.Assumption = fmt.Sprintf("%d dollar limits are not published; using frozen %d limits.", year, latest)
		return p, nil
	}
	return ContributionPolicy{}, fmt.Errorf("unsupported contribution policy year %d", year)
}

func (p ContributionPolicy) CatchUpLimit(ageAtYearEnd int) float64 {
	switch {
	case ageAtYearEnd < 50:
		return 0
	case ageAtYearEnd >= 60 && ageAtYearEnd <= 63:
		return p.Age60To63CatchUpLimit
	default:
		return p.OrdinaryCatchUpLimit
	}
}

func (p ContributionPolicy) RequiresRothCatchUp(priorSponsorSocialSecurityWages float64) bool {
	return priorSponsorSocialSecurityWages > p.RothCatchUpWageThreshold
}
