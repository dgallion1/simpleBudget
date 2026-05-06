package retirement

import (
	"budget2/internal/models"
	"math"
)

const defaultSocialSecurityFRA = 67
const defaultSocialSecurityCOLARate = 0.02
const ssPortfolioMonteCarloRuns = 250

func validSSClaimAge(age int) bool {
	return age >= 62 && age <= 70
}

func normalizedSSFRA(fra int) int {
	if fra == 0 {
		return defaultSocialSecurityFRA
	}
	return fra
}

// normalizedSSCOLARate returns the SS COLA rate to apply.
// nil rate → returns the 2% default (caller did not supply a value).
// Non-nil → returns the value, clamping negatives to 0 (SSA COLA is
// never negative). Explicit 0 is honored. F-026.
func normalizedSSCOLARate(rate *float64) float64 {
	if rate == nil {
		return defaultSocialSecurityCOLARate
	}
	if *rate < 0 {
		return 0.0
	}
	return *rate
}

// ssConfigCOLARate extracts the effective COLA rate from WhatIfSettings.
// If SocialSecurityConfig.COLARateSet is true the stored value is used (even 0);
// otherwise the 2% default is returned. F-026.
func ssConfigCOLARate(s *models.WhatIfSettings) float64 {
	if s == nil || s.SocialSecurity == nil {
		return defaultSocialSecurityCOLARate
	}
	var ptr *float64
	if s.SocialSecurity.COLARateSet {
		ptr = &s.SocialSecurity.COLARate
	}
	return normalizedSSCOLARate(ptr)
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

func SSPortfolioEligible(s *models.WhatIfSettings) bool {
	if s == nil || s.SocialSecurity == nil {
		return false
	}
	return ssPortfolioPrimaryEligible(s) || ssPortfolioSpouseEligible(s)
}

func ssPortfolioPrimaryEligible(s *models.WhatIfSettings) bool {
	if s == nil || s.SocialSecurity == nil {
		return false
	}
	ss := s.SocialSecurity
	if s.CurrentAge <= 0 || ss.FRABenefit <= 0 || !validSSClaimAge(ss.ClaimAge) {
		return false
	}
	return ss.ClaimAge > s.CurrentAge && ss.ClaimAge >= 62
}

func ssPortfolioSpouseEligible(s *models.WhatIfSettings) bool {
	if s == nil || s.SocialSecurity == nil || !s.HasSpouse() {
		return false
	}
	ss := s.SocialSecurity
	if s.SpouseAge <= 0 || ss.SpouseFRABenefit <= 0 || !validSSClaimAge(ss.SpouseClaimAge) {
		return false
	}
	return ss.SpouseClaimAge > s.SpouseAge && ss.SpouseClaimAge >= 62
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

// ProjectedSSEntry describes a single Social Security income stream computed
// by the SS Optimizer for display alongside manual income sources.
type ProjectedSSEntry struct {
	Label           string  // "Your Social Security" or "Spouse Social Security"
	MonthlyAmount   float64 // benefit at start of payout, before COLA growth
	ClaimAge        int
	StartMonth      int  // month within the projection at which payments begin
	AlreadyClaiming bool // claim age has already passed at projection start
	SpousalTopUp    bool // amount reflects spousal top-up vs own benefit
}

// ProjectedSSEntries returns the optimizer-derived Social Security income
// streams (primary + spouse where applicable). Returns nil when the optimizer
// is inactive. The values match what projectedSocialSecurityIncome uses, so
// they accurately represent what the projection includes for SS income.
func ProjectedSSEntries(s *models.WhatIfSettings) []ProjectedSSEntry {
	if !socialSecurityProjectionActive(s) {
		return nil
	}

	ss := s.SocialSecurity
	fra := normalizedSSFRA(ss.FRA)
	spouseFRA := normalizedSSFRA(ss.SpouseFRA)

	alreadyClaiming := ss.ClaimAge <= s.CurrentAge
	primaryPIA := ss.FRABenefit
	primaryBase := ss.FRABenefit
	if alreadyClaiming {
		primaryPIA = DerivedPIA(ss.FRABenefit, fra, ss.ClaimAge)
	} else {
		primaryBase = AdjustedSSBenefit(primaryPIA, fra, ss.ClaimAge)
	}

	spouseAlreadyClaiming := validSSClaimAge(ss.SpouseClaimAge) && ss.SpouseClaimAge <= s.SpouseAge
	projectSpouse := s.HasSpouse() && s.SpouseAge > 0 && ss.SpouseFRABenefit > 0 && validSSClaimAge(ss.SpouseClaimAge)

	var spousePIA, spouseBase float64
	if projectSpouse {
		spousePIA = ss.SpouseFRABenefit
		if spouseAlreadyClaiming {
			spousePIA = DerivedPIA(ss.SpouseFRABenefit, spouseFRA, ss.SpouseClaimAge)
			spouseBase = ss.SpouseFRABenefit
		} else {
			spouseBase = AdjustedSSBenefit(spousePIA, spouseFRA, ss.SpouseClaimAge)
		}
	}

	primaryTopUp := false
	spouseTopUp := false
	if projectSpouse {
		if !spouseAlreadyClaiming && primaryPIA > spousePIA {
			adjusted := SpousalTopUp(spouseBase, primaryPIA, spouseFRA, ss.SpouseClaimAge)
			if adjusted > spouseBase {
				spouseBase = adjusted
				spouseTopUp = true
			}
		} else if !alreadyClaiming && spousePIA > primaryPIA {
			adjusted := SpousalTopUp(primaryBase, spousePIA, fra, ss.ClaimAge)
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
	colaRate := ssConfigCOLARate(s)
	total := 0.0
	for _, e := range entries {
		if month >= e.StartMonth {
			total += projectedSSBenefitForMonth(e.MonthlyAmount, colaRate, month-e.StartMonth)
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

// DerivedPIA back-derives the PIA from an actual benefit amount and the
// claiming age, reversing the SSA actuarial adjustment. This is used when
// a person has already claimed and enters their actual benefit rather than PIA.
func DerivedPIA(actualBenefit float64, fra, claimAge int) float64 {
	if claimAge < 62 {
		claimAge = 62
	}
	if claimAge > 70 {
		claimAge = 70
	}
	// AdjustedSSBenefit(pia, fra, claimAge) = pia * factor
	// So pia = actualBenefit / factor
	factor := AdjustedSSBenefit(1.0, fra, claimAge)
	if factor <= 0 {
		return actualBenefit
	}
	return actualBenefit / factor
}

// AdjustedSpousalBenefit calculates the monthly spousal Social Security benefit
// for a given claiming age. The spousal early-claim reduction is steeper than
// the worker's own reduction: 25/36 of 1% per month for the first 36 months
// before FRA, then 5/12 of 1% per month for additional earlier months. Spousal
// benefits do not earn delayed retirement credits, so claims at or past FRA
// return the full spousal PIA.
func AdjustedSpousalBenefit(spousalPIA float64, spouseFRA, claimAge int) float64 {
	if claimAge < 62 {
		claimAge = 62
	}
	spouseFRA = normalizedSSFRA(spouseFRA)
	if claimAge >= spouseFRA {
		return spousalPIA
	}

	monthsEarly := (spouseFRA - claimAge) * 12
	reduction := 0.0
	if monthsEarly <= 36 {
		reduction = float64(monthsEarly) * 25.0 / 3600.0
	} else {
		reduction = 36.0*25.0/3600.0 + float64(monthsEarly-36)*5.0/1200.0
	}
	return spousalPIA * (1.0 - reduction)
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

	spousalBenefit := AdjustedSpousalBenefit(spousalPIA, spouseFRA, spouseClaimAge)
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

		for age := early; age <= 100; age++ {
			// COLA is a calendar-year raise applied to everyone's PIA regardless
			// of claiming age, so both benefits compound from the same base year.
			cola := math.Pow(1.0+colaRate, float64(age-early))
			earlyBenefit := earlyMonthly * cola
			lateBenefit := lateMonthly * cola

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
	colaRate := ssConfigCOLARate(c.Settings)

	// When already claiming, the entered amount is the actual benefit, not PIA.
	// Back-derive PIA for the comparison table so hypothetical ages are correct.
	primaryPIA := ss.FRABenefit
	if ss.ClaimAge <= c.Settings.CurrentAge && ss.ClaimAge != fra {
		primaryPIA = DerivedPIA(ss.FRABenefit, fra, ss.ClaimAge)
	}
	// Same logic for spouse — derived once so both the spouse-side comparison
	// and the primary-side spousal top-up use the correct PIA.
	spouseFRA := normalizedSSFRA(ss.SpouseFRA)
	spousePIA := ss.SpouseFRABenefit
	if validSSClaimAge(ss.SpouseClaimAge) && ss.SpouseClaimAge <= c.Settings.SpouseAge && ss.SpouseClaimAge != spouseFRA {
		spousePIA = DerivedPIA(ss.SpouseFRABenefit, spouseFRA, ss.SpouseClaimAge)
	}

	options := SSComparisonTable(primaryPIA, fra, c.Settings.CurrentAge, colaRate)
	breakevens := SSBreakevenAges(primaryPIA, fra, colaRate)
	if spousePIA > primaryPIA {
		options = SSComparisonTableWithSpousalTopUp(primaryPIA, fra, c.Settings.CurrentAge, colaRate, spousePIA)
		breakevens = SSBreakevenAgesWithSpousalTopUp(primaryPIA, fra, colaRate, spousePIA)
	}

	bestAge := 0
	bestCum := 0.0
	if ss.ClaimAge <= c.Settings.CurrentAge {
		// Already claiming — don't suggest a different age
		bestAge = ss.ClaimAge
	} else {
		for _, opt := range options {
			if opt.CumulativeAt85 > bestCum {
				bestCum = opt.CumulativeAt85
				bestAge = opt.ClaimAge
			}
		}
	}

	// Spouse analysis — use the higher of own benefit or spousal benefit (50% of worker PIA).
	// Spousal benefits max out at FRA (no delayed retirement credits).
	var spouseOptions []models.SSClaimingOption
	var spouseBreakevens []models.SSBreakevenResult
	spouseBestAge := 0
	if ss.SpouseFRABenefit > 0 {
		spouseAge := c.Settings.SpouseAge
		if spouseAge == 0 {
			spouseAge = c.Settings.CurrentAge
		}

		if primaryPIA > spousePIA {
			spouseOptions = SSComparisonTableWithSpousalTopUp(spousePIA, spouseFRA, spouseAge, colaRate, primaryPIA)
			spouseBreakevens = SSBreakevenAgesWithSpousalTopUp(spousePIA, spouseFRA, colaRate, primaryPIA)
		} else {
			spouseOptions = SSComparisonTable(spousePIA, spouseFRA, spouseAge, colaRate)
			spouseBreakevens = SSBreakevenAges(spousePIA, spouseFRA, colaRate)
		}
		if validSSClaimAge(ss.SpouseClaimAge) && ss.SpouseClaimAge <= spouseAge {
			// Spouse already claiming — don't suggest a different age
			spouseBestAge = ss.SpouseClaimAge
		} else {
			bestCum = 0
			for _, opt := range spouseOptions {
				if opt.CumulativeAt85 > bestCum {
					bestCum = opt.CumulativeAt85
					spouseBestAge = opt.ClaimAge
				}
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

// RunSSPortfolioAnalysis evaluates how eligible claiming ages affect portfolio
// survival while holding the other person's selected claim age fixed.
// The caller must pass the already-computed SS comparison analysis to avoid
// redundant work (RunFullAnalysis already computes it).
func (c *Calculator) RunSSPortfolioAnalysis(ssAnalysis *models.SSComparisonAnalysis) *models.SSPortfolioAnalysis {
	if c == nil || c.Settings == nil || !SSPortfolioEligible(c.Settings) {
		return nil
	}
	if ssAnalysis == nil {
		return nil
	}

	ss := c.Settings.SocialSecurity
	baseline := c.runSSPortfolioCellMC(ss.ClaimAge, ss.SpouseClaimAge)
	if baseline == nil || baseline.Stats == nil {
		return nil
	}

	result := &models.SSPortfolioAnalysis{
		BaselineSurvivalRate: baseline.Stats.SuccessRate,
		MonteCarloRuns:       ssPortfolioMonteCarloRuns,
	}

	primaryBenefits := ssPortfolioBenefitLookup(ssAnalysis.Options)
	spouseBenefits := ssPortfolioBenefitLookup(ssAnalysis.SpouseOptions)

	if ssPortfolioPrimaryEligible(c.Settings) {
		result.PrimaryOptions = c.buildSSPortfolioOptions(
			max(62, c.Settings.CurrentAge),
			ss.ClaimAge,
			primaryBenefits,
			func(age int) (int, int) { return age, ss.SpouseClaimAge },
			baseline,
		)
		if best, ok := bestSSPortfolioOption(result.PrimaryOptions); ok {
			result.OptimalPrimaryAge = best.ClaimAge
			result.OptimalSurvivalRate = max(result.OptimalSurvivalRate, best.SurvivalRate)
		}
	}

	if ssPortfolioSpouseEligible(c.Settings) {
		result.SpouseOptions = c.buildSSPortfolioOptions(
			max(62, c.Settings.SpouseAge),
			ss.SpouseClaimAge,
			spouseBenefits,
			func(age int) (int, int) { return ss.ClaimAge, age },
			baseline,
		)
		if best, ok := bestSSPortfolioOption(result.SpouseOptions); ok {
			result.OptimalSpouseAge = best.ClaimAge
			result.OptimalSurvivalRate = max(result.OptimalSurvivalRate, best.SurvivalRate)
		}
	}

	return result
}

func ssPortfolioBenefitLookup(options []models.SSClaimingOption) map[int]float64 {
	benefits := make(map[int]float64, len(options))
	for _, option := range options {
		benefits[option.ClaimAge] = option.MonthlyBenefit
	}
	return benefits
}

func (c *Calculator) buildSSPortfolioOptions(
	minAge int,
	selectedAge int,
	benefits map[int]float64,
	claimAges func(age int) (int, int),
	baseline *models.MonteCarloAnalysis,
) []models.SSPortfolioOption {
	options := make([]models.SSPortfolioOption, 0, max(0, 71-minAge))
	baselineRate := 0.0
	if baseline != nil && baseline.Stats != nil {
		baselineRate = baseline.Stats.SuccessRate
	}

	for age := minAge; age <= 70; age++ {
		mc := baseline
		if age != selectedAge {
			primaryAge, spouseAge := claimAges(age)
			mc = c.runSSPortfolioCellMC(primaryAge, spouseAge)
		}
		if mc == nil || mc.Stats == nil {
			continue
		}

		option := models.SSPortfolioOption{
			ClaimAge:            age,
			MonthlyBenefit:      benefits[age],
			SurvivalRate:        mc.Stats.SuccessRate,
			MedianEndingBalance: mc.Stats.MedianBalance,
			P10EndingBalance:    mc.Stats.Percentile10,
			P90EndingBalance:    mc.Stats.Percentile90,
		}
		option.DeltaSurvivalRate = option.SurvivalRate - baselineRate
		options = append(options, option)
	}

	return options
}

func (c *Calculator) runSSPortfolioCellMC(primaryClaimAge, spouseClaimAge int) *models.MonteCarloAnalysis {
	clone := c.cloneSettingsWithClaimAges(primaryClaimAge, spouseClaimAge)
	if clone == nil {
		return nil
	}
	cellCalc := NewCalculatorWithChain(clone, c.ResolvedChain)
	return cellCalc.RunMonteCarloSimulation(ssPortfolioMonteCarloRuns)
}

func (c *Calculator) cloneSettingsWithClaimAges(primaryClaimAge, spouseClaimAge int) *models.WhatIfSettings {
	if c == nil || c.Settings == nil {
		return nil
	}

	clone := *c.Settings
	clone.ScenarioChain = append([]models.ScenarioChainLink(nil), c.Settings.ScenarioChain...)
	clone.Persons = append([]models.Person(nil), c.Settings.Persons...)
	clone.HealthcarePersons = append([]models.HealthcarePerson(nil), c.Settings.HealthcarePersons...)
	clone.IncomeSources = append([]models.IncomeSource(nil), c.Settings.IncomeSources...)
	clone.ExpenseSources = append([]models.ExpenseSource(nil), c.Settings.ExpenseSources...)
	clone.RemovedIncomeSources = append([]models.IncomeSource(nil), c.Settings.RemovedIncomeSources...)
	clone.RemovedExpenseSources = append([]models.ExpenseSource(nil), c.Settings.RemovedExpenseSources...)
	clone.BigTicketItems = append([]models.BigTicketItem(nil), c.Settings.BigTicketItems...)
	clone.RemovedBigTicketItems = append([]models.BigTicketItem(nil), c.Settings.RemovedBigTicketItems...)

	if c.Settings.SpendingPhaseConfig != nil {
		phaseConfig := *c.Settings.SpendingPhaseConfig
		phaseConfig.Phases = append([]models.SpendingPhase(nil), c.Settings.SpendingPhaseConfig.Phases...)
		clone.SpendingPhaseConfig = &phaseConfig
	}
	if c.Settings.TaxConfig != nil {
		taxConfig := *c.Settings.TaxConfig
		clone.TaxConfig = &taxConfig
	}
	if c.Settings.RothConversion != nil {
		rothConversion := *c.Settings.RothConversion
		clone.RothConversion = &rothConversion
	}
	if c.Settings.GlidePath != nil {
		glidePath := *c.Settings.GlidePath
		clone.GlidePath = &glidePath
	}
	if c.Settings.Guardrails != nil {
		guardrails := *c.Settings.Guardrails
		clone.Guardrails = &guardrails
	}
	if c.Settings.SocialSecurity != nil {
		ss := *c.Settings.SocialSecurity
		ss.ClaimAge = primaryClaimAge
		ss.SpouseClaimAge = spouseClaimAge
		clone.SocialSecurity = &ss
	}

	return &clone
}

func bestSSPortfolioOption(options []models.SSPortfolioOption) (models.SSPortfolioOption, bool) {
	if len(options) == 0 {
		return models.SSPortfolioOption{}, false
	}

	best := options[0]
	for _, option := range options[1:] {
		if isBetterSSPortfolioOption(option, best) {
			best = option
		}
	}
	return best, true
}

func isBetterSSPortfolioOption(candidate, current models.SSPortfolioOption) bool {
	if candidate.SurvivalRate != current.SurvivalRate {
		return candidate.SurvivalRate > current.SurvivalRate
	}
	if candidate.MedianEndingBalance != current.MedianEndingBalance {
		return candidate.MedianEndingBalance > current.MedianEndingBalance
	}
	return candidate.ClaimAge < current.ClaimAge
}

func cumulativeBenefit(monthlyAtClaim float64, claimAge, targetAge int, colaRate float64) float64 {
	if targetAge <= claimAge {
		return 0
	}

	total := 0.0
	for age := claimAge; age < targetAge; age++ {
		adjustedMonthly := monthlyAtClaim * math.Pow(1.0+colaRate, float64(age-claimAge))
		total += adjustedMonthly * 12.0
	}

	return math.Round(total*100) / 100
}
