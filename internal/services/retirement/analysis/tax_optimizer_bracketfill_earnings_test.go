package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// Folding a positive Roth-earnings term into bracketFillIncomeForYear must
// shrink the sized conversion so that taxable ordinary income INCLUDING the
// earnings lands on the bracket ceiling. With no Social Security in play, the
// shrink equals the earnings exactly (earnings displace conversion dollar-for-
// dollar in the ordinary bracket).
func TestBracketFillIncomeForYear_FoldsRothEarningsIntoOrdinary(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.Persons = s.Persons[:1] // single filer
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 55)
	s.SpouseAge = 0
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.RothPercent = 0
	s.TaxableDividendYield = 0
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.IncomeSources = nil
	// No SS benefit in the window: isolates the earnings→ordinary fold.
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 0, FRA: 67, ClaimAge: 70, COLARate: 0, COLARateSet: true}

	in := engineInput(t, s)
	ps := in.Prepared.Settings()

	ceiling, ok := inflatedBracketTopForYear(ps, 0.22, 0)
	if !ok {
		t.Fatal("no inflated 22% ceiling for year 0")
	}

	const earnings = 20_000.0
	convNoEarn := bracketFillIncomeForYear(ps, 0, 0).bracketFillConversion(ceiling)
	convWithEarn := bracketFillIncomeForYear(ps, 0, earnings).bracketFillConversion(ceiling)

	if !(convWithEarn < convNoEarn) {
		t.Fatalf("earnings should shrink the conversion: noEarn=%.0f withEarn=%.0f", convNoEarn, convWithEarn)
	}
	// Ordinary income (incl. earnings) at the chosen conversion lands on the ceiling.
	got := bracketFillIncomeForYear(ps, 0, earnings).taxableOrdinaryIncome(convWithEarn)
	if math.Abs(got-ceiling) > 1.0 {
		t.Fatalf("ordinary incl earnings should land on ceiling: got=%.0f ceiling=%.0f", got, ceiling)
	}
	// With no SS, the shrink equals the earnings exactly.
	if d := (convNoEarn - convWithEarn) - earnings; math.Abs(d) > 1.0 {
		t.Fatalf("conversion should shrink by exactly the earnings; off by %.2f", d)
	}
}

func TestHarvestRothEarnings_WindowOnly(t *testing.T) {
	proj := &models.ProjectionResult{
		YearlySummaries: []models.ProjectionYearSummary{
			{TaxableRothEarnings: 100}, // y0 — before window
			{TaxableRothEarnings: 0},   // y1
			{TaxableRothEarnings: 250}, // y2 — in window
			{TaxableRothEarnings: 400}, // y3 — in window
			{TaxableRothEarnings: 900}, // y4 — after window
		},
	}
	got := harvestRothEarnings(proj, 2, 4) // [2,4)
	if len(got) != 2 || got[2] != 250 || got[3] != 400 {
		t.Fatalf("expected {2:250,3:400}, got %v", got)
	}
}

func TestMaxAbsFeedbackDelta(t *testing.T) {
	a := map[int]float64{2: 250, 3: 400}
	b := map[int]float64{2: 250, 3: 380, 5: 30}
	if d := maxAbsFeedbackDelta(a, b); math.Abs(d-30) > 1e-9 {
		t.Fatalf("want 30 (the key-5 difference), got %v", d)
	}
	if d := maxAbsFeedbackDelta(nil, nil); d != 0 {
		t.Fatalf("nil vs nil should be 0, got %v", d)
	}
}

func TestRelaxFeedback_DampsTowardObserved(t *testing.T) {
	prev := map[int]float64{2: 0, 3: 100}
	observed := map[int]float64{2: 200, 3: 0}
	got := relaxFeedback(prev, observed, 0.5)
	if math.Abs(got[2]-100) > 1e-9 { // 0 + 0.5*(200-0)
		t.Fatalf("key2: want 100, got %v", got[2])
	}
	if math.Abs(got[3]-50) > 1e-9 { // 100 + 0.5*(0-100)
		t.Fatalf("key3: want 50, got %v", got[3])
	}
}

// adversarialOverlapSettings reproduces the measured corner: a small Roth drained
// past basis under 59½ DURING the conversion window, producing ~$23k of taxable
// Roth earnings at age 53 that the uncorrected sizer ignores.
func adversarialOverlapSettings(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	s.Persons = s.Persons[:1]
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 50)
	s.SpouseAge = 0
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.PortfolioValue = 700_000
	s.TaxDeferredPercent = 78
	s.RothPercent = 12
	s.TaxableDividendYield = 0
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.IncomeSources = []models.IncomeSource{{Type: models.IncomeFixed, Amount: 2500, StartMonth: 0}}
	s.MonthlyLivingExpenses = 8500
	s.TaxDeferredDelayYears = 6
	s.InvestmentReturn = 7
	s.ProjectionYears = 20
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2500, FRA: 67, ClaimAge: 70, COLARate: 0, COLARateSet: true}
	return s
}

// runWithOverrides prepares s, attaches per-year conversion overrides (prepare
// drops the json:"-" map, so re-attach as cloneSettingsWithSSAndRoth does), and
// runs the engine.
func runWithOverrides(t *testing.T, s *models.WhatIfSettings, overrides map[int]float64) *models.ProjectionResult {
	t.Helper()
	cfg := *s
	cfg.RothConversion = &models.RothConversionConfig{Enabled: true, PerYearOverrides: overrides}
	prepared := prepare.MustFrom(t, &cfg)
	if pset := prepared.Settings(); pset != nil {
		pset.RothConversion = &models.RothConversionConfig{Enabled: true, PerYearOverrides: overrides}
	}
	return engine.New().Run(engine.Input{Prepared: prepared})
}

func overridesFromConversions(ycs []models.YearlyConversion, currentAge int) map[int]float64 {
	out := make(map[int]float64, len(ycs))
	for _, yc := range ycs {
		out[yc.Age-currentAge] = yc.Amount
	}
	return out
}

// actualTaxableOrdinary reuses the solver's own definition of taxable ordinary
// income (incl. Roth earnings) to evaluate what the engine actually produced for
// a conversion year: ordinary (incl. engine earnings) + conversion + taxable SS −
// std deduction.
func actualTaxableOrdinary(ps *models.WhatIfSettings, projYear int, conversion, engineEarnings float64) float64 {
	return bracketFillIncomeForYear(ps, projYear, engineEarnings).taxableOrdinaryIncome(conversion)
}

const overshootYear = 3 // age 53 in adversarialOverlapSettings

// The uncorrected sizer overshoots the 12% ceiling in the overlap year; the
// iterative scoreCandidate eliminates it.
func TestScoreCandidate_EliminatesRothEarningsOvershoot(t *testing.T) {
	s := adversarialOverlapSettings(t)
	in := engineInput(t, s)
	ps := in.Prepared.Settings()

	strat := models.RothOptimizerStrategy{
		Kind: models.RothStrategyBracketFill, TargetBracket: 0.12, StartAge: 50, EndAge: 59,
	}
	ceiling, ok := inflatedBracketTopForYear(ps, 0.12, overshootYear)
	if !ok {
		t.Fatal("no inflated 12% ceiling")
	}

	// Uncorrected baseline (today's behavior): size with nil feedback, run engine.
	uncYCs := strategyYearlyConversions(ps, strat, nil)
	uncOverrides := overridesFromConversions(uncYCs, ps.CurrentAge)
	uncProj := runWithOverrides(t, s, uncOverrides)
	uncEarn := uncProj.YearlySummaries[overshootYear].TaxableRothEarnings
	uncActual := actualTaxableOrdinary(ps, overshootYear, uncOverrides[overshootYear], uncEarn)
	if uncActual <= ceiling+100 {
		t.Fatalf("expected uncorrected to overshoot the ceiling; actual=%.0f ceiling=%.0f (corner not reproduced)", uncActual, ceiling)
	}

	// Corrected: iterative scoreCandidate (SS primary claim 70 matches settings).
	cand := scoreCandidate(engine.New(), in, 70, 0, strat)
	corrOverrides := overridesFromConversions(cand.PerYearConversions, ps.CurrentAge)
	corrProj := runWithOverrides(t, s, corrOverrides)
	corrEarn := corrProj.YearlySummaries[overshootYear].TaxableRothEarnings
	corrActual := actualTaxableOrdinary(ps, overshootYear, corrOverrides[overshootYear], corrEarn)

	if corrActual > ceiling+bracketFillFeedbackTolerance {
		t.Fatalf("overshoot not eliminated: actual=%.0f ceiling=%.0f (uncorrected was %.0f)", corrActual, ceiling, uncActual)
	}
	// Sanity: the corrected conversion shrank in the overshoot year.
	if corrOverrides[overshootYear] >= uncOverrides[overshootYear] {
		t.Fatalf("corrected conversion should shrink: corr=%.0f unc=%.0f", corrOverrides[overshootYear], uncOverrides[overshootYear])
	}
}

// In a normal scenario (ample Roth, no earnings withdrawn in conversion years),
// the iterative path must produce IDENTICAL conversions to the uncorrected sizer.
func TestScoreCandidate_NoChangeWhenNoRothEarnings(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.Persons = s.Persons[:1]
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 55)
	s.SpouseAge = 0
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingSingle}
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 70
	s.RothPercent = 25
	s.TaxableDividendYield = 0
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.IncomeSources = nil
	s.MonthlyLivingExpenses = 6000
	s.TaxDeferredDelayYears = 0
	s.InvestmentReturn = 6
	s.ProjectionYears = 20
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2500, FRA: 67, ClaimAge: 70, COLARate: 0, COLARateSet: true}

	in := engineInput(t, s)
	ps := in.Prepared.Settings()
	strat := models.RothOptimizerStrategy{
		Kind: models.RothStrategyBracketFill, TargetBracket: 0.12, StartAge: 55, EndAge: 65,
	}

	want := strategyYearlyConversions(ps, strat, nil)
	cand := scoreCandidate(engine.New(), in, 70, 0, strat)
	if len(cand.PerYearConversions) != len(want) {
		t.Fatalf("length mismatch: got %d want %d", len(cand.PerYearConversions), len(want))
	}
	for i := range want {
		if math.Abs(cand.PerYearConversions[i].Amount-want[i].Amount) > 0.01 {
			t.Fatalf("year %d differs: iterative=%.2f uncorrected=%.2f (should be identical with no Roth earnings)",
				want[i].Age, cand.PerYearConversions[i].Amount, want[i].Amount)
		}
	}

	// No correction should have been applied at all (proves the single-engine-run
	// fast path: empty observed earnings ⇒ residual 0 ⇒ break at iteration 0).
	if len(cand.BracketFillFeedback) != 0 {
		t.Fatalf("expected empty converged feedback in the common case, got %v", cand.BracketFillFeedback)
	}
	// Scored-projection identity: the iterative score must equal a single
	// uncorrected (nil-feedback) run.
	baseCloned, ok := cloneSettingsWithSSAndRoth(ps, 70, 0, strat, nil)
	if !ok {
		t.Fatal("baseline clone failed")
	}
	baseCand := projectionToCandidate(engine.New().Run(engine.Input{Prepared: baseCloned}), 70, 0, strat)
	if math.Abs(cand.EndingPortfolioReal-baseCand.EndingPortfolioReal) > 0.01 {
		t.Fatalf("scored projection should match a single uncorrected run: iterative=%.2f baseline=%.2f",
			cand.EndingPortfolioReal, baseCand.EndingPortfolioReal)
	}
}

// Monte Carlo finalist refinement re-clones each finalist via
// cloneSettingsWithSSAndRoth(settings, ages, strat, finalists[i].BracketFillFeedback).
// That clone must carry the SAME corrected conversions scoreCandidate disclosed,
// or the MC re-sort would rank finalists on uncorrected (overshooting) sizing.
func TestMCFinalistCloning_UsesCorrectedFeedback(t *testing.T) {
	s := adversarialOverlapSettings(t)
	in := engineInput(t, s)
	settings := in.Prepared.Settings()
	strat := models.RothOptimizerStrategy{
		Kind: models.RothStrategyBracketFill, TargetBracket: 0.12, StartAge: 50, EndAge: 59,
	}

	cand := scoreCandidate(engine.New(), in, 70, 0, strat)
	if len(cand.BracketFillFeedback) == 0 {
		t.Fatal("expected nonzero converged feedback in the overshoot corner")
	}

	// Drive the exact helper the Monte Carlo finalist loop uses, so a regression
	// that drops the feedback argument in the loop is caught here.
	cloned, ok := cloneFinalistForMonteCarlo(settings, cand)
	if !ok {
		t.Fatal("MC clone failed")
	}
	got := cloned.Settings().RothConversion.PerYearOverrides
	for _, yc := range cand.PerYearConversions {
		k := yc.Age - settings.CurrentAge
		if math.Abs(got[k]-yc.Amount) > 0.01 {
			t.Fatalf("MC clone override for proj-year %d = %.2f, want corrected %.2f (MC would rank on uncorrected sizing)",
				k, got[k], yc.Amount)
		}
	}
}
