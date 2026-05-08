package analysis

import (
	"math"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

const (
	defaultSocialSecurityFRA      = 67
	defaultSocialSecurityCOLARate = 0.02
	ssPortfolioMonteCarloRuns     = 250
)

// ValidSSClaimAge reports whether age is a valid Social Security
// claiming age (62-70 inclusive).
func ValidSSClaimAge(age int) bool {
	return age >= 62 && age <= 70
}

// NormalizedSSFRA returns the FRA to use, defaulting to 67 if fra==0.
func NormalizedSSFRA(fra int) int {
	if fra == 0 {
		return defaultSocialSecurityFRA
	}
	return fra
}

// NormalizedSSCOLARate returns the SS COLA rate to apply.
// nil rate → returns the 2% default (caller did not supply a value).
// Non-nil → returns the value, clamping negatives to 0 (SSA COLA is
// never negative). Explicit 0 is honored. F-026.
func NormalizedSSCOLARate(rate *float64) float64 {
	if rate == nil {
		return defaultSocialSecurityCOLARate
	}
	if *rate < 0 {
		return 0.0
	}
	return *rate
}

// SSConfigCOLARate extracts the effective COLA rate from
// WhatIfSettings. If SocialSecurityConfig.COLARateSet is true the
// stored value is used (even 0); otherwise the 2% default is
// returned. F-026.
func SSConfigCOLARate(s *models.WhatIfSettings) float64 {
	if s == nil || s.SocialSecurity == nil {
		return defaultSocialSecurityCOLARate
	}
	var ptr *float64
	if s.SocialSecurity.COLARateSet {
		ptr = &s.SocialSecurity.COLARate
	}
	return NormalizedSSCOLARate(ptr)
}

// AdjustedSSBenefit calculates the monthly Social Security benefit for
// a given claiming age based on SSA actuarial adjustment rules. The
// pia is the Primary Insurance Amount (monthly benefit at FRA), fra is
// the full retirement age, and claimAge is the age at which benefits
// are claimed (clamped to 62-70).
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

// DerivedPIA back-derives the PIA from an actual benefit amount and
// the claiming age, reversing the SSA actuarial adjustment. This is
// used when a person has already claimed and enters their actual
// benefit rather than PIA.
func DerivedPIA(actualBenefit float64, fra, claimAge int) float64 {
	if claimAge < 62 {
		claimAge = 62
	}
	if claimAge > 70 {
		claimAge = 70
	}
	factor := AdjustedSSBenefit(1.0, fra, claimAge)
	if factor <= 0 {
		return actualBenefit
	}
	return actualBenefit / factor
}

// AdjustedSpousalBenefit calculates the monthly spousal Social
// Security benefit for a given claiming age. The spousal early-claim
// reduction is steeper than the worker's own reduction: 25/36 of 1%
// per month for the first 36 months before FRA, then 5/12 of 1% per
// month for additional earlier months. Spousal benefits do not earn
// delayed retirement credits, so claims at or past FRA return the
// full spousal PIA.
func AdjustedSpousalBenefit(spousalPIA float64, spouseFRA, claimAge int) float64 {
	if claimAge < 62 {
		claimAge = 62
	}
	spouseFRA = NormalizedSSFRA(spouseFRA)
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

// SpousalTopUp returns the larger of the spouse's own benefit or the
// spousal benefit derived from the higher earner's PIA.
func SpousalTopUp(spouseOwnBenefit, higherPIA float64, spouseFRA, spouseClaimAge int) float64 {
	if higherPIA <= 0 {
		return spouseOwnBenefit
	}
	spouseFRA = NormalizedSSFRA(spouseFRA)

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

// benefitAdjuster optionally transforms a base monthly benefit for a
// given claiming age. Used to apply spousal top-up when the other
// spouse has a higher PIA.
type benefitAdjuster func(baseBenefit float64, claimAge int) float64

func noAdjustment(baseBenefit float64, _ int) float64 { return baseBenefit }

// SSComparisonTable returns a slice of models.SSClaimingOption for
// claiming ages 62-70, skipping ages below currentAge. Each option
// includes the adjusted benefit and cumulative amounts at ages 80, 85,
// and 90 with annual COLA applied from the claiming age onward.
func SSComparisonTable(pia float64, fra int, currentAge int, colaRate float64) []models.SSClaimingOption {
	return ssComparisonTable(pia, fra, currentAge, colaRate, noAdjustment)
}

// SSComparisonTableWithSpousalTopUp is SSComparisonTable with the
// spousal top-up adjuster applied.
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

// SSBreakevenAgesWithSpousalTopUp is SSBreakevenAges with the spousal
// top-up adjuster applied.
func SSBreakevenAgesWithSpousalTopUp(pia float64, fra int, colaRate float64, higherPIA float64) []models.SSBreakevenResult {
	return ssBreakevenAges(pia, fra, colaRate, func(base float64, age int) float64 {
		return SpousalTopUp(base, higherPIA, fra, age)
	})
}

// SSBreakevenAges calculates the breakeven age for each adjacent pair
// of claiming ages (62-63, 63-64, ..., 69-70). The breakeven age is
// when the cumulative benefit of the later claiming age surpasses the
// earlier one. Returns 0 for breakeven_age if it never breaks even
// within age 100.
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
			// COLA is a calendar-year raise applied to everyone's PIA
			// regardless of claiming age, so both benefits compound
			// from the same base year.
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

// SSPortfolioPrimaryEligible reports whether the primary's
// configuration enables the SS portfolio analysis.
func SSPortfolioPrimaryEligible(s *models.WhatIfSettings) bool {
	if s == nil || s.SocialSecurity == nil {
		return false
	}
	ss := s.SocialSecurity
	if s.CurrentAge <= 0 || ss.FRABenefit <= 0 || !ValidSSClaimAge(ss.ClaimAge) {
		return false
	}
	return ss.ClaimAge > s.CurrentAge && ss.ClaimAge >= 62
}

// SSPortfolioSpouseEligible reports whether the spouse's
// configuration enables the SS portfolio analysis.
func SSPortfolioSpouseEligible(s *models.WhatIfSettings) bool {
	if s == nil || s.SocialSecurity == nil || !s.HasSpouse() {
		return false
	}
	ss := s.SocialSecurity
	if s.SpouseAge <= 0 || ss.SpouseFRABenefit <= 0 || !ValidSSClaimAge(ss.SpouseClaimAge) {
		return false
	}
	return ss.SpouseClaimAge > s.SpouseAge && ss.SpouseClaimAge >= 62
}

// SSPortfolioEligible reports whether at least one person in the
// household qualifies for SS portfolio analysis.
func SSPortfolioEligible(s *models.WhatIfSettings) bool {
	if s == nil || s.SocialSecurity == nil {
		return false
	}
	return SSPortfolioPrimaryEligible(s) || SSPortfolioSpouseEligible(s)
}

// SSAnalysis computes the full SS claiming-age comparison for the
// configured settings. Returns nil if SS is not configured.
func SSAnalysis(_ *engine.Engine, in engine.Input) *models.SSComparisonAnalysis {
	s := in.Prepared.Settings()
	if s == nil {
		return nil
	}
	ss := s.SocialSecurity
	if ss == nil || ss.FRABenefit <= 0 {
		return nil
	}

	fra := NormalizedSSFRA(ss.FRA)
	colaRate := SSConfigCOLARate(s)

	// When already claiming, the entered amount is the actual benefit,
	// not PIA. Back-derive PIA for the comparison table so hypothetical
	// ages are correct.
	primaryPIA := ss.FRABenefit
	if ss.ClaimAge <= s.CurrentAge && ss.ClaimAge != fra {
		primaryPIA = DerivedPIA(ss.FRABenefit, fra, ss.ClaimAge)
	}
	// Same logic for spouse — derived once so both the spouse-side
	// comparison and the primary-side spousal top-up use the correct
	// PIA.
	spouseFRA := NormalizedSSFRA(ss.SpouseFRA)
	spousePIA := ss.SpouseFRABenefit
	if ValidSSClaimAge(ss.SpouseClaimAge) && ss.SpouseClaimAge <= s.SpouseAge && ss.SpouseClaimAge != spouseFRA {
		spousePIA = DerivedPIA(ss.SpouseFRABenefit, spouseFRA, ss.SpouseClaimAge)
	}

	options := SSComparisonTable(primaryPIA, fra, s.CurrentAge, colaRate)
	breakevens := SSBreakevenAges(primaryPIA, fra, colaRate)
	if spousePIA > primaryPIA {
		options = SSComparisonTableWithSpousalTopUp(primaryPIA, fra, s.CurrentAge, colaRate, spousePIA)
		breakevens = SSBreakevenAgesWithSpousalTopUp(primaryPIA, fra, colaRate, spousePIA)
	}

	bestAge := 0
	bestCum := 0.0
	if ss.ClaimAge <= s.CurrentAge {
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

	// Spouse analysis — use the higher of own benefit or spousal
	// benefit (50% of worker PIA). Spousal benefits max out at FRA (no
	// delayed retirement credits).
	var spouseOptions []models.SSClaimingOption
	var spouseBreakevens []models.SSBreakevenResult
	spouseBestAge := 0
	if ss.SpouseFRABenefit > 0 {
		spouseAge := s.SpouseAge
		if spouseAge == 0 {
			spouseAge = s.CurrentAge
		}

		if primaryPIA > spousePIA {
			spouseOptions = SSComparisonTableWithSpousalTopUp(spousePIA, spouseFRA, spouseAge, colaRate, primaryPIA)
			spouseBreakevens = SSBreakevenAgesWithSpousalTopUp(spousePIA, spouseFRA, colaRate, primaryPIA)
		} else {
			spouseOptions = SSComparisonTable(spousePIA, spouseFRA, spouseAge, colaRate)
			spouseBreakevens = SSBreakevenAges(spousePIA, spouseFRA, colaRate)
		}
		if ValidSSClaimAge(ss.SpouseClaimAge) && ss.SpouseClaimAge <= spouseAge {
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
		// F-029: When primary is already claiming at a non-FRA age,
		// FRABenefit is the reduced (or DRC-increased) amount, not the
		// PIA. The spousal benefit entitlement is 50% of the primary
		// PIA, so derive PIA first. primaryPIA is already correctly
		// computed above (back-derived when primary is already claiming
		// at non-FRA); use it here instead of raw ss.FRABenefit to
		// avoid understating the spousal entitlement.
		result.SpouseUsingSpousalBenefit = primaryPIA*0.5 > spousePIA

		// Calculate gap between earliest and best cumulative at 85
		if len(spouseOptions) > 1 && bestCum > 0 {
			earliestCum := spouseOptions[0].CumulativeAt85
			result.SpouseEarlyClaimGapPct = (bestCum - earliestCum) / bestCum * 100
		}
	}

	return result
}

// SSPortfolio evaluates how eligible claiming ages affect portfolio
// survival while holding the other person's selected claim age fixed.
// The caller must pass the already-computed SS comparison analysis to
// avoid redundant work.
//
// SSPortfolio uses the default Monte Carlo seed (auto-seed). For
// deterministic/parity runs, see SSPortfolioWithSeed.
func SSPortfolio(eng *engine.Engine, in engine.Input, ss *models.SSComparisonAnalysis) *models.SSPortfolioAnalysis {
	return SSPortfolioWithSeed(eng, in, ss, 0)
}

// SSPortfolioWithSeed is SSPortfolio but threads a fixed Monte Carlo
// seed through every cell. seed == 0 means auto-seed (preserves the
// legacy "default = unpredictable" contract); any non-zero seed is
// used directly so deterministic comparisons are reproducible.
func SSPortfolioWithSeed(eng *engine.Engine, in engine.Input, ssAnalysis *models.SSComparisonAnalysis, seed int64) *models.SSPortfolioAnalysis {
	s := in.Prepared.Settings()
	if s == nil || !SSPortfolioEligible(s) {
		return nil
	}
	if ssAnalysis == nil {
		return nil
	}

	ss := s.SocialSecurity
	baseline := runSSPortfolioCellMC(eng, in, ss.ClaimAge, ss.SpouseClaimAge, seed)
	if baseline == nil || baseline.Stats == nil {
		return nil
	}

	result := &models.SSPortfolioAnalysis{
		BaselineSurvivalRate: baseline.Stats.SuccessRate,
		MonteCarloRuns:       ssPortfolioMonteCarloRuns,
	}

	primaryBenefits := ssPortfolioBenefitLookup(ssAnalysis.Options)
	spouseBenefits := ssPortfolioBenefitLookup(ssAnalysis.SpouseOptions)

	if SSPortfolioPrimaryEligible(s) {
		result.PrimaryOptions = buildSSPortfolioOptions(
			eng,
			in,
			max(62, s.CurrentAge),
			ss.ClaimAge,
			primaryBenefits,
			func(age int) (int, int) { return age, ss.SpouseClaimAge },
			baseline,
			seed,
		)
		if best, ok := bestSSPortfolioOption(result.PrimaryOptions); ok {
			result.OptimalPrimaryAge = best.ClaimAge
			result.OptimalSurvivalRate = max(result.OptimalSurvivalRate, best.SurvivalRate)
		}
	}

	if SSPortfolioSpouseEligible(s) {
		result.SpouseOptions = buildSSPortfolioOptions(
			eng,
			in,
			max(62, s.SpouseAge),
			ss.SpouseClaimAge,
			spouseBenefits,
			func(age int) (int, int) { return ss.ClaimAge, age },
			baseline,
			seed,
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

func buildSSPortfolioOptions(
	eng *engine.Engine,
	in engine.Input,
	minAge int,
	selectedAge int,
	benefits map[int]float64,
	claimAges func(age int) (int, int),
	baseline *models.MonteCarloAnalysis,
	seed int64,
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
			mc = runSSPortfolioCellMC(eng, in, primaryAge, spouseAge, seed)
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

func runSSPortfolioCellMC(eng *engine.Engine, in engine.Input, primaryClaimAge, spouseClaimAge int, seed int64) *models.MonteCarloAnalysis {
	clone, ok := cloneSettingsWithClaimAges(in.Prepared.Settings(), primaryClaimAge, spouseClaimAge)
	if !ok {
		return nil
	}
	cellInput := engine.Input{Prepared: clone, Chain: in.Chain}
	return MonteCarlo(eng, cellInput, ssPortfolioMonteCarloRuns, seed)
}

// cloneSettingsWithClaimAges produces a prepared snapshot identical to
// the input settings except for the SS claim ages. The deep-copy in
// prepare.From handles slice/pointer aliasing, so we only need a
// shallow struct copy plus a fresh SocialSecurity struct here.
func cloneSettingsWithClaimAges(s *models.WhatIfSettings, primaryClaimAge, spouseClaimAge int) (prepare.PreparedSettings, bool) {
	if s == nil {
		return prepare.PreparedSettings{}, false
	}

	cfg := *s
	if s.SocialSecurity != nil {
		ssCopy := *s.SocialSecurity
		ssCopy.ClaimAge = primaryClaimAge
		ssCopy.SpouseClaimAge = spouseClaimAge
		cfg.SocialSecurity = &ssCopy
	}
	return perturbAndPrepare(&cfg), true
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

// BestSSPortfolioOption is exported so retirement-package code (and
// tests) can call the SS portfolio-option ranker directly.
func BestSSPortfolioOption(options []models.SSPortfolioOption) (models.SSPortfolioOption, bool) {
	return bestSSPortfolioOption(options)
}

// CumulativeBenefit is exported so retirement-package code (and tests)
// can call the cumulative-benefit calculation directly.
func CumulativeBenefit(monthlyAtClaim float64, claimAge, targetAge int, colaRate float64) float64 {
	return cumulativeBenefit(monthlyAtClaim, claimAge, targetAge, colaRate)
}

