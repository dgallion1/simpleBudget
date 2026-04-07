package retirement

import (
	"budget2/internal/models"
	"math"
)

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

// SSComparisonTable returns a slice of models.SSClaimingOption for claiming ages 62-70,
// skipping ages below currentAge. Each option includes the adjusted benefit and
// cumulative amounts at ages 80, 85, and 90 with annual COLA applied from the
// claiming age onward.
func SSComparisonTable(pia float64, fra int, currentAge int, colaRate float64) []models.SSClaimingOption {
	var options []models.SSClaimingOption

	for age := 62; age <= 70; age++ {
		if age < currentAge {
			continue
		}

		monthly := AdjustedSSBenefit(pia, fra, age)
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

// SSBreakevenAges calculates the breakeven age for each adjacent pair of
// claiming ages (62-63, 63-64, ..., 69-70). The breakeven age is when the
// cumulative benefit of the later claiming age surpasses the earlier one.
// Returns 0 for breakeven_age if it never breaks even within age 100.
func SSBreakevenAges(pia float64, fra int, colaRate float64) []models.SSBreakevenResult {
	var results []models.SSBreakevenResult

	for early := 62; early <= 69; early++ {
		late := early + 1
		earlyMonthly := AdjustedSSBenefit(pia, fra, early)
		lateMonthly := AdjustedSSBenefit(pia, fra, late)

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

	fra := ss.FRA
	if fra == 0 {
		fra = 67
	}
	colaRate := ss.COLARate
	if colaRate == 0 {
		colaRate = 0.02
	}

	options := SSComparisonTable(ss.FRABenefit, fra, c.Settings.CurrentAge, colaRate)
	breakevens := SSBreakevenAges(ss.FRABenefit, fra, colaRate)

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
		spouseFRA := ss.SpouseFRA
		if spouseFRA == 0 {
			spouseFRA = 67
		}
		spouseAge := c.Settings.SpouseAge
		if spouseAge == 0 {
			spouseAge = c.Settings.CurrentAge
		}

		// Effective PIA: higher of own benefit or 50% of worker's PIA
		spousalBenefit := ss.FRABenefit * 0.5
		effectivePIA := ss.SpouseFRABenefit
		if spousalBenefit > effectivePIA {
			effectivePIA = spousalBenefit
		}

		// Spousal benefits don't get delayed retirement credits past FRA,
		// so cap the effective claiming age at FRA for the comparison table.
		spouseOptions = SSComparisonTableCapped(effectivePIA, spouseFRA, spouseAge, colaRate)
		spouseBreakevens = SSBreakevenAgesCapped(effectivePIA, spouseFRA, colaRate)
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

// SSComparisonTableCapped is like SSComparisonTable but caps the effective
// claiming age at FRA (no delayed retirement credits). This reflects SSA rules
// for spousal benefits, which max out at the spouse's FRA.
func SSComparisonTableCapped(pia float64, fra int, currentAge int, colaRate float64) []models.SSClaimingOption {
	var options []models.SSClaimingOption

	for age := 62; age <= 70; age++ {
		if age < currentAge {
			continue
		}

		// For spousal benefits, no increase past FRA
		effectiveAge := age
		if effectiveAge > fra {
			effectiveAge = fra
		}

		monthly := AdjustedSSBenefit(pia, fra, effectiveAge)
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

// SSBreakevenAgesCapped is like SSBreakevenAges but caps benefits at FRA
// (no delayed retirement credits), matching spousal benefit rules.
func SSBreakevenAgesCapped(pia float64, fra int, colaRate float64) []models.SSBreakevenResult {
	var results []models.SSBreakevenResult

	for early := 62; early <= 69; early++ {
		late := early + 1

		earlyEffective := early
		if earlyEffective > fra {
			earlyEffective = fra
		}
		lateEffective := late
		if lateEffective > fra {
			lateEffective = fra
		}

		earlyMonthly := AdjustedSSBenefit(pia, fra, earlyEffective)
		lateMonthly := AdjustedSSBenefit(pia, fra, lateEffective)

		// If both ages are past FRA, benefits are identical — no breakeven
		if earlyEffective == lateEffective {
			continue
		}

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
