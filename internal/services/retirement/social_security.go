package retirement

import (
	"budget2/internal/models"
	"math"
)

const defaultSocialSecurityFRA = 67
const defaultSocialSecurityCOLARate = 0.02

func validSSClaimAge(age int) bool {
	return age >= 62 && age <= 70
}

func normalizedSSFRA(fra int) int {
	if fra == 0 {
		return defaultSocialSecurityFRA
	}
	return fra
}

func normalizedSSCOLARate(colaRate float64) float64 {
	if colaRate == 0 {
		return defaultSocialSecurityCOLARate
	}
	return colaRate
}

func SocialSecurityProjectionActive(s *models.WhatIfSettings) bool {
	if s == nil || s.SocialSecurity == nil {
		return false
	}
	ss := s.SocialSecurity
	return ss.FRABenefit > 0 && validSSClaimAge(ss.ClaimAge)
}

func socialSecurityProjectionActive(s *models.WhatIfSettings) bool {
	return SocialSecurityProjectionActive(s)
}

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

func projectedSocialSecurityIncome(s *models.WhatIfSettings, month int) float64 {
	if !socialSecurityProjectionActive(s) {
		return 0
	}

	ss := s.SocialSecurity
	fra := normalizedSSFRA(ss.FRA)
	spouseFRA := normalizedSSFRA(ss.SpouseFRA)
	colaRate := normalizedSSCOLARate(ss.COLARate)

	primaryBase := AdjustedSSBenefit(ss.FRABenefit, fra, ss.ClaimAge)
	spouseBase := 0.0
	projectSpouse := s.HasSpouse() && s.SpouseAge > 0 && ss.SpouseFRABenefit > 0 && validSSClaimAge(ss.SpouseClaimAge)
	if projectSpouse {
		spouseBase = AdjustedSSBenefit(ss.SpouseFRABenefit, spouseFRA, ss.SpouseClaimAge)
		if ss.FRABenefit > ss.SpouseFRABenefit {
			spouseBase = SpousalTopUp(spouseBase, ss.FRABenefit, spouseFRA, ss.SpouseClaimAge)
		} else if ss.SpouseFRABenefit > ss.FRABenefit {
			primaryBase = SpousalTopUp(primaryBase, ss.SpouseFRABenefit, fra, ss.ClaimAge)
		}
	}

	total := 0.0
	primaryStart := claimStartMonth(s.CurrentAge, ss.ClaimAge)
	if month >= primaryStart {
		total += projectedSSBenefitForMonth(primaryBase, colaRate, month-primaryStart)
	}

	if projectSpouse {
		spouseStart := claimStartMonth(s.SpouseAge, ss.SpouseClaimAge)
		if month >= spouseStart {
			total += projectedSSBenefitForMonth(spouseBase, colaRate, month-spouseStart)
		}
	}

	return total
}

// projectedSSBenefitForMonth applies COLA growth from the claim start month.
// This uses continuous compounding as an approximation; real COLA adjustments
// are applied annually each January, but the difference is negligible for projections.
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

// AdjustedSSBenefit calculates the monthly Social Security benefit for a given
// claiming age based on SSA actuarial adjustment rules. The pia is the Primary
// Insurance Amount (monthly benefit at FRA), fra is the full retirement age,
// and claimAge is the age at which benefits are claimed (clamped to 62-70).
func AdjustedSSBenefit(pia float64, fra int, claimAge int) float64 {
	if claimAge < 62 {
		claimAge = 62
	}
	if claimAge > 70 {
		claimAge = 70
	}

	monthsDiff := (claimAge - fra) * 12

	if monthsDiff < 0 {
		earlyMonths := -monthsDiff
		reduction := 0.0
		if earlyMonths <= 36 {
			reduction = float64(earlyMonths) * 5.0 / 900.0
		} else {
			reduction = 36.0*5.0/900.0 + float64(earlyMonths-36)*5.0/1200.0
		}
		return pia * (1.0 - reduction)
	}

	if monthsDiff > 0 {
		increase := float64(monthsDiff) * 2.0 / 300.0
		return pia * (1.0 + increase)
	}

	return pia
}

func SpousalTopUp(spouseOwnBenefit, higherPIA float64, spouseFRA, spouseClaimAge int) float64 {
	if higherPIA <= 0 {
		return spouseOwnBenefit
	}
	spouseFRA = normalizedSSFRA(spouseFRA)

	spousalPIA := higherPIA * 0.5
	if spouseOwnBenefit >= spousalPIA {
		return spouseOwnBenefit
	}

	effectiveClaimAge := spouseClaimAge
	if effectiveClaimAge > spouseFRA {
		effectiveClaimAge = spouseFRA
	}

	spousalBenefit := AdjustedSSBenefit(spousalPIA, spouseFRA, effectiveClaimAge)
	if spousalBenefit > spouseOwnBenefit {
		return spousalBenefit
	}
	return spouseOwnBenefit
}

// benefitAdjuster optionally transforms a base monthly benefit for a given claiming age.
// Used to apply spousal top-up when the other spouse has a higher PIA.
type benefitAdjuster func(baseBenefit float64, claimAge int) float64

func noAdjustment(baseBenefit float64, _ int) float64 { return baseBenefit }

// SSComparisonTable returns a slice of models.SSClaimingOption for claiming ages 62-70,
// skipping ages below currentAge. Each option includes the adjusted benefit and
// cumulative amounts at ages 80, 85, and 90 with annual COLA applied from the
// claiming age onward.
func SSComparisonTable(pia float64, fra int, currentAge int, colaRate float64) []models.SSClaimingOption {
	return ssComparisonTable(pia, fra, currentAge, colaRate, noAdjustment)
}

func SSComparisonTableWithSpousalTopUp(pia float64, fra int, currentAge int, colaRate float64, higherPIA float64) []models.SSClaimingOption {
	return ssComparisonTable(pia, fra, currentAge, colaRate, func(base float64, age int) float64 {
		return SpousalTopUp(base, higherPIA, fra, age)
	})
}

func ssComparisonTable(pia float64, fra int, currentAge int, colaRate float64, adjust benefitAdjuster) []models.SSClaimingOption {
	var options []models.SSClaimingOption

	for age := 62; age <= 70; age++ {
		if age < currentAge {
			continue
		}

		monthly := adjust(AdjustedSSBenefit(pia, fra, age), age)
		annual := monthly * 12.0
		pctOfPIA := monthly / pia * 100.0

		opt := models.SSClaimingOption{
			ClaimAge:       age,
			MonthlyBenefit: math.Round(monthly*100) / 100,
			AnnualBenefit:  math.Round(annual*100) / 100,
			PctOfPIA:       math.Round(pctOfPIA*10) / 10,
			CumulativeAt80: cumulativeBenefit(monthly, age, 80, colaRate),
			CumulativeAt85: cumulativeBenefit(monthly, age, 85, colaRate),
			CumulativeAt90: cumulativeBenefit(monthly, age, 90, colaRate),
		}
		options = append(options, opt)
	}

	return options
}

func SSBreakevenAgesWithSpousalTopUp(pia float64, fra int, colaRate float64, higherPIA float64) []models.SSBreakevenResult {
	return ssBreakevenAges(pia, fra, colaRate, func(base float64, age int) float64 {
		return SpousalTopUp(base, higherPIA, fra, age)
	})
}

// SSBreakevenAges calculates the breakeven age for each adjacent pair of
// claiming ages (62-63, 63-64, ..., 69-70). The breakeven age is when the
// cumulative benefit of the later claiming age surpasses the earlier one.
// Returns 0 for breakeven_age if it never breaks even within age 100.
func SSBreakevenAges(pia float64, fra int, colaRate float64) []models.SSBreakevenResult {
	return ssBreakevenAges(pia, fra, colaRate, noAdjustment)
}

func ssBreakevenAges(pia float64, fra int, colaRate float64, adjust benefitAdjuster) []models.SSBreakevenResult {
	var results []models.SSBreakevenResult

	for early := 62; early <= 69; early++ {
		late := early + 1
		earlyMonthly := adjust(AdjustedSSBenefit(pia, fra, early), early)
		lateMonthly := adjust(AdjustedSSBenefit(pia, fra, late), late)

		breakevenAge := 0
		earlyCum := 0.0
		lateCum := 0.0
		earlyBenefit := earlyMonthly
		lateBenefit := lateMonthly

		for age := early; age <= 100; age++ {
			if age > early {
				earlyBenefit = earlyMonthly * math.Pow(1.0+colaRate, float64(age-early))
			}
			if age > late {
				lateBenefit = lateMonthly * math.Pow(1.0+colaRate, float64(age-late))
			}

			earlyCum += earlyBenefit * 12.0
			if age >= late {
				lateCum += lateBenefit * 12.0
			}

			if lateCum > earlyCum && breakevenAge == 0 {
				breakevenAge = age
				break
			}
		}

		results = append(results, models.SSBreakevenResult{
			EarlyAge:     early,
			LateAge:      late,
			BreakevenAge: breakevenAge,
		})
	}

	return results
}

// RunSSAnalysis computes the full SS claiming age comparison for the configured settings.
func (c *Calculator) RunSSAnalysis() *models.SSComparisonAnalysis {
	ss := c.Settings.SocialSecurity
	if ss == nil || ss.FRABenefit <= 0 {
		return nil
	}

	fra := normalizedSSFRA(ss.FRA)
	colaRate := normalizedSSCOLARate(ss.COLARate)

	options := SSComparisonTable(ss.FRABenefit, fra, c.Settings.CurrentAge, colaRate)
	breakevens := SSBreakevenAges(ss.FRABenefit, fra, colaRate)
	if ss.SpouseFRABenefit > ss.FRABenefit {
		options = SSComparisonTableWithSpousalTopUp(ss.FRABenefit, fra, c.Settings.CurrentAge, colaRate, ss.SpouseFRABenefit)
		breakevens = SSBreakevenAgesWithSpousalTopUp(ss.FRABenefit, fra, colaRate, ss.SpouseFRABenefit)
	}

	bestAge := 0
	bestCum := 0.0
	for _, opt := range options {
		if opt.CumulativeAt85 > bestCum {
			bestCum = opt.CumulativeAt85
			bestAge = opt.ClaimAge
		}
	}

	// Spouse analysis — use the higher of own benefit or spousal benefit (50% of worker PIA).
	// Spousal benefits max out at FRA (no delayed retirement credits).
	var spouseOptions []models.SSClaimingOption
	var spouseBreakevens []models.SSBreakevenResult
	spouseBestAge := 0
	if ss.SpouseFRABenefit > 0 {
		spouseFRA := normalizedSSFRA(ss.SpouseFRA)
		spouseAge := c.Settings.SpouseAge
		if spouseAge == 0 {
			spouseAge = c.Settings.CurrentAge
		}

		if ss.FRABenefit > ss.SpouseFRABenefit {
			spouseOptions = SSComparisonTableWithSpousalTopUp(ss.SpouseFRABenefit, spouseFRA, spouseAge, colaRate, ss.FRABenefit)
			spouseBreakevens = SSBreakevenAgesWithSpousalTopUp(ss.SpouseFRABenefit, spouseFRA, colaRate, ss.FRABenefit)
		} else {
			spouseOptions = SSComparisonTable(ss.SpouseFRABenefit, spouseFRA, spouseAge, colaRate)
			spouseBreakevens = SSBreakevenAges(ss.SpouseFRABenefit, spouseFRA, colaRate)
		}
		bestCum = 0
		for _, opt := range spouseOptions {
			if opt.CumulativeAt85 > bestCum {
				bestCum = opt.CumulativeAt85
				spouseBestAge = opt.ClaimAge
			}
		}
	}

	result := &models.SSComparisonAnalysis{
		Options:    options,
		Breakevens: breakevens,
		BestAge:    bestAge,
	}

	if len(spouseOptions) > 0 {
		result.SpouseOptions = spouseOptions
		result.SpouseBreakevens = spouseBreakevens
		result.SpouseBestAge = spouseBestAge
		result.SpouseUsingSpousalBenefit = ss.FRABenefit*0.5 > ss.SpouseFRABenefit

		// Calculate gap between earliest and best cumulative at 85
		if len(spouseOptions) > 1 && bestCum > 0 {
			earliestCum := spouseOptions[0].CumulativeAt85
			result.SpouseEarlyClaimGapPct = (bestCum - earliestCum) / bestCum * 100
		}
	}

	return result
}


func cumulativeBenefit(monthlyAtClaim float64, claimAge, targetAge int, colaRate float64) float64 {
	if targetAge <= claimAge {
		return 0
	}

	total := 0.0
	for age := claimAge; age < targetAge; age++ {
		yearsFromClaim := age - claimAge
		adjustedMonthly := monthlyAtClaim * math.Pow(1.0+colaRate, float64(yearsFromClaim))
		total += adjustedMonthly * 12.0
	}

	return math.Round(total*100) / 100
}
