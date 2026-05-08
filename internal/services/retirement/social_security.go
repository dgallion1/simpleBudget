package retirement

import (
	"math"

	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
)

// SS math primitives now live canonically in analysis/ss.go. The
// exported aliases below keep existing retirement-side callers
// (templates, primitive-level tests) compiling unchanged. The
// lowercase forms used to live in this file but were retired with the
// move — surviving callers use analysis.X directly. Removed in
// Task 8.
var (
	AdjustedSSBenefit                 = analysis.AdjustedSSBenefit
	DerivedPIA                        = analysis.DerivedPIA
	AdjustedSpousalBenefit            = analysis.AdjustedSpousalBenefit
	SpousalTopUp                      = analysis.SpousalTopUp
	SSComparisonTable                 = analysis.SSComparisonTable
	SSComparisonTableWithSpousalTopUp = analysis.SSComparisonTableWithSpousalTopUp
	SSBreakevenAges                   = analysis.SSBreakevenAges
	SSBreakevenAgesWithSpousalTopUp   = analysis.SSBreakevenAgesWithSpousalTopUp

	// lowercase aliases referenced by retirement-side tests for the
	// underlying primitives (TestCumulativeBenefit,
	// TestBestSSPortfolioOption, TestNormalizedSSCOLARate_*).
	cumulativeBenefit     = analysis.CumulativeBenefit
	bestSSPortfolioOption = analysis.BestSSPortfolioOption
	normalizedSSCOLARate  = analysis.NormalizedSSCOLARate
)

// SocialSecurityProjectionActive reports whether the household has an
// active SS optimizer projection (FRA benefit + valid claim age).
func SocialSecurityProjectionActive(s *models.WhatIfSettings) bool {
	if s == nil || s.SocialSecurity == nil {
		return false
	}
	ss := s.SocialSecurity
	return ss.FRABenefit > 0 && analysis.ValidSSClaimAge(ss.ClaimAge)
}

func socialSecurityProjectionActive(s *models.WhatIfSettings) bool {
	return SocialSecurityProjectionActive(s)
}

// SSPortfolioEligible moved to eligibility.go in this package.

// HasManualSocialSecurityIncomeSource reports whether the user has a
// manual Social Security entry in their income sources (separate from
// the optimizer).
func HasManualSocialSecurityIncomeSource(s *models.WhatIfSettings) bool {
	if s == nil {
		return false
	}
	for _, source := range s.IncomeSources {
		if isSocialSecurityIncomeSource(source) {
			return true
		}
	}
	return false
}

// ProjectedSSEntry describes a single Social Security income stream
// computed by the SS Optimizer for display alongside manual income
// sources.
type ProjectedSSEntry struct {
	Label           string  // "Your Social Security" or "Spouse Social Security"
	MonthlyAmount   float64 // benefit at start of payout, before COLA growth
	ClaimAge        int
	StartMonth      int  // month within the projection at which payments begin
	AlreadyClaiming bool // claim age has already passed at projection start
	SpousalTopUp    bool // amount reflects spousal top-up vs own benefit
}

// ProjectedSSEntries returns the optimizer-derived Social Security
// income streams (primary + spouse where applicable). Returns nil
// when the optimizer is inactive.
func ProjectedSSEntries(s *models.WhatIfSettings) []ProjectedSSEntry {
	if !socialSecurityProjectionActive(s) {
		return nil
	}

	ss := s.SocialSecurity
	fra := analysis.NormalizedSSFRA(ss.FRA)
	spouseFRA := analysis.NormalizedSSFRA(ss.SpouseFRA)

	alreadyClaiming := ss.ClaimAge <= s.CurrentAge
	primaryPIA := ss.FRABenefit
	primaryBase := ss.FRABenefit
	if alreadyClaiming {
		primaryPIA = analysis.DerivedPIA(ss.FRABenefit, fra, ss.ClaimAge)
	} else {
		primaryBase = analysis.AdjustedSSBenefit(primaryPIA, fra, ss.ClaimAge)
	}

	spouseAlreadyClaiming := analysis.ValidSSClaimAge(ss.SpouseClaimAge) && ss.SpouseClaimAge <= s.SpouseAge
	projectSpouse := s.HasSpouse() && s.SpouseAge > 0 && ss.SpouseFRABenefit > 0 && analysis.ValidSSClaimAge(ss.SpouseClaimAge)

	var spousePIA, spouseBase float64
	if projectSpouse {
		spousePIA = ss.SpouseFRABenefit
		if spouseAlreadyClaiming {
			spousePIA = analysis.DerivedPIA(ss.SpouseFRABenefit, spouseFRA, ss.SpouseClaimAge)
			spouseBase = ss.SpouseFRABenefit
		} else {
			spouseBase = analysis.AdjustedSSBenefit(spousePIA, spouseFRA, ss.SpouseClaimAge)
		}
	}

	primaryTopUp := false
	spouseTopUp := false
	if projectSpouse {
		if !spouseAlreadyClaiming && primaryPIA > spousePIA {
			adjusted := analysis.SpousalTopUp(spouseBase, primaryPIA, spouseFRA, ss.SpouseClaimAge)
			if adjusted > spouseBase {
				spouseBase = adjusted
				spouseTopUp = true
			}
		} else if !alreadyClaiming && spousePIA > primaryPIA {
			adjusted := analysis.SpousalTopUp(primaryBase, spousePIA, fra, ss.ClaimAge)
			if adjusted > primaryBase {
				primaryBase = adjusted
				primaryTopUp = true
			}
		}
	}

	entries := []ProjectedSSEntry{{
		Label:           "Your Social Security",
		MonthlyAmount:   primaryBase,
		ClaimAge:        ss.ClaimAge,
		StartMonth:      claimStartMonth(s.CurrentAge, ss.ClaimAge),
		AlreadyClaiming: alreadyClaiming,
		SpousalTopUp:    primaryTopUp,
	}}
	if projectSpouse {
		entries = append(entries, ProjectedSSEntry{
			Label:           "Spouse Social Security",
			MonthlyAmount:   spouseBase,
			ClaimAge:        ss.SpouseClaimAge,
			StartMonth:      claimStartMonth(s.SpouseAge, ss.SpouseClaimAge),
			AlreadyClaiming: spouseAlreadyClaiming,
			SpousalTopUp:    spouseTopUp,
		})
	}
	return entries
}

func projectedSocialSecurityIncome(s *models.WhatIfSettings, month int) float64 {
	entries := ProjectedSSEntries(s)
	if len(entries) == 0 {
		return 0
	}
	colaRate := analysis.SSConfigCOLARate(s)
	total := 0.0
	for _, e := range entries {
		if month >= e.StartMonth {
			total += projectedSSBenefitForMonth(e.MonthlyAmount, colaRate, month-e.StartMonth)
		}
	}
	return total
}

// projectedSSBenefitForMonth applies COLA growth from the claim start
// month. This uses continuous compounding as an approximation; real
// COLA adjustments are applied annually each January, but the
// difference is negligible for projections.
func projectedSSBenefitForMonth(baseMonthly float64, colaRate float64, monthsSinceClaim int) float64 {
	if baseMonthly <= 0 || monthsSinceClaim < 0 {
		return 0
	}
	return baseMonthly * math.Pow(1+colaRate, float64(monthsSinceClaim)/12.0)
}

func claimStartMonth(currentAge, claimAge int) int {
	if claimAge <= currentAge {
		return 0
	}
	return (claimAge - currentAge) * 12
}
