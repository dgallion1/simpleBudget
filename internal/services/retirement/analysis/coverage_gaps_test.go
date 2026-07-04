// Coverage-gap tests for the analysis package: exercises defensive
// branches, clamps, and fallback paths that the behavioral suites skip.
// Documented-unreachable branches (kept uncovered on purpose):
//
//   - scoreCandidate (tax_optimizer.go:292): scoredProj == nil after the
//     loop is unreachable — iteration 0 always assigns scoredProj (the
//     non-bracket-fill arm assigns directly; the bracket-fill arm's first
//     residual is finite and therefore < math.MaxFloat64).
//   - TaxOptimizerWithSeed (tax_optimizer.go:380): dropping a failed
//     candidate requires scoreCandidate to fail inside an eligible run,
//     but with non-nil prepared settings the engine always yields yearly
//     summaries — only fault injection could produce the sentinel here.
//   - TaxOptimizerWithSeed (tax_optimizer.go:404,421,427,432): deflator<=0
//     needs InflationRate <= -100%; effectiveSeed==0 after UnixNano() needs
//     the clock to return exactly 0; mcCloned !ok / mc==nil need nil
//     settings, impossible after the eligibility gate passed.
//   - topKSSPairs (tax_optimizer.go:189): len(out)==0 fallback is
//     unreachable — either the nil/empty early-return fires, or at least
//     one addPair call (optimum, ranked-loop, or current-settings
//     fallback) has appended a pair.
package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

const covEps = 1e-9

func approxEq(a, b float64) bool {
	return math.Abs(a-b) <= covEps*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

// --- TaxOptimizer (exported wrapper, called via retirement.RunTaxOptimizer) ---

func TestTaxOptimizer_WrapperIneligible(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.CurrentAge = 67
	s.ProjectionYears = 31
	s.TaxConfig = nil // ineligible: no filing status
	in := engine.Input{Prepared: perturbAndPrepare(s)}

	got := TaxOptimizer(engine.New(), in, nil)
	if got == nil {
		t.Fatal("TaxOptimizer returned nil for ineligible scenario")
	}
	if got.Eligible {
		t.Error("expected Eligible=false")
	}
	if got.IneligibleReason != "Set tax filing status to enable optimization." {
		t.Errorf("unexpected IneligibleReason %q", got.IneligibleReason)
	}
}

func TestTaxOptimizer_WrapperEligibleAutoSeed(t *testing.T) {
	// Exercises the seed=0 auto-seed path (time-derived seed shared across
	// finalists). MC medians vary run-to-run, so assertions are structural.
	s := eligibleBase()
	prep := perturbAndPrepare(s)
	// Guard against age drift: eligibleBase pins Persons[0].BirthMonth so the
	// PREPARED age matches CurrentAge=67. If prepare.ComputeAges ever derives
	// a different age again (e.g. BirthMonth left at the default-65 value),
	// every eligibleBase-derived engine run silently simulates the wrong age.
	if gotAge := prep.Settings().CurrentAge; gotAge != 67 {
		t.Fatalf("prepared CurrentAge = %d; eligibleBase must yield age 67", gotAge)
	}
	in := engine.Input{Prepared: prep}

	got := TaxOptimizer(engine.New(), in, nil)
	if got == nil || !got.Eligible {
		t.Fatalf("expected eligible result, got %+v", got)
	}
	if len(got.Top) == 0 {
		t.Fatal("expected non-empty Top")
	}
	if len(got.Top) > taxOptimizerTopFinalists {
		t.Errorf("Top length %d exceeds cap %d", len(got.Top), taxOptimizerTopFinalists)
	}
	if got.Best.MCMedianEndingReal != got.Top[0].MCMedianEndingReal ||
		got.Best.RothStrategy.Label != got.Top[0].RothStrategy.Label {
		t.Error("Best should equal Top[0]")
	}
	if got.CandidatesScored == 0 {
		t.Error("CandidatesScored should be > 0")
	}
	if got.MonteCarloRuns != taxOptimizerMonteCarloRuns {
		t.Errorf("MonteCarloRuns: got %d, want %d", got.MonteCarloRuns, taxOptimizerMonteCarloRuns)
	}
}

// --- tax_optimizer.go small helpers ---

func TestCandidateSettingsForSS_NilSettings(t *testing.T) {
	if got := candidateSettingsForSS(nil, 67, 62); got != nil {
		t.Errorf("expected nil for nil settings, got %+v", got)
	}
}

func TestCloneSettingsWithSSAndRoth_NilSettings(t *testing.T) {
	strat := models.RothOptimizerStrategy{Kind: models.RothStrategyNone}
	prep, ok := cloneSettingsWithSSAndRoth(nil, 67, 62, strat, nil)
	if ok {
		t.Error("expected ok=false for nil settings")
	}
	if !prep.IsZero() {
		t.Error("expected zero PreparedSettings for nil settings")
	}
}

func TestScoreCandidate_NilSettingsReturnsFailSentinel(t *testing.T) {
	strat := models.RothOptimizerStrategy{
		Kind: models.RothStrategyLadder, AnnualAmount: 10_000, StartAge: 60, EndAge: 65,
	}
	cand := scoreCandidate(engine.New(), engine.Input{}, 67, 62, strat)
	if cand.EndingPortfolioReal != -math.MaxFloat64 {
		t.Errorf("expected fail sentinel, got %v", cand.EndingPortfolioReal)
	}
	if cand.PrimaryClaimAge != 67 || cand.SpouseClaimAge != 62 {
		t.Errorf("fail candidate must carry claim ages, got %d/%d",
			cand.PrimaryClaimAge, cand.SpouseClaimAge)
	}
	if cand.RothStrategy.AnnualAmount != strat.AnnualAmount {
		t.Error("fail candidate must carry the strategy")
	}
}

func TestProjectionToCandidate_ZeroInflationDeflatorTreatedAsOne(t *testing.T) {
	proj := &models.ProjectionResult{
		YearlySummaries: []models.ProjectionYearSummary{
			{EndingBalanceReal: 1234, Taxes: 100, CumulativeInflation: 0},
		},
	}
	cand := projectionToCandidate(proj, 67, 0, models.RothOptimizerStrategy{})
	if cand.EndingPortfolioReal != 1234 {
		t.Errorf("EndingPortfolioReal: got %v, want 1234", cand.EndingPortfolioReal)
	}
	// CumulativeInflation<=0 must deflate by 1, not divide by zero.
	if cand.LifetimeTaxReal != 100 {
		t.Errorf("LifetimeTaxReal: got %v, want 100 (deflator clamped to 1)", cand.LifetimeTaxReal)
	}
}

func TestCurrentRothStrategyFor_InvertedWindowFallsBackToProjectionEnd(t *testing.T) {
	s := &models.WhatIfSettings{CurrentAge: 60, ProjectionYears: 30}
	s.RothConversion = &models.RothConversionConfig{
		Enabled: true, AnnualAmount: 10_000, StartYear: 5, EndYear: 2, // EndAge 63 <= StartAge 65
	}
	strat := currentRothStrategyFor(s)
	if strat.Kind != models.RothStrategyLadder {
		t.Fatalf("Kind: got %q, want ladder", strat.Kind)
	}
	if strat.StartAge != 65 {
		t.Errorf("StartAge: got %d, want 65", strat.StartAge)
	}
	if strat.EndAge != 90 { // CurrentAge + ProjectionYears
		t.Errorf("EndAge: got %d, want 90 (projection end fallback)", strat.EndAge)
	}
}

func TestTopKSSPairs_KZeroClampedToOne(t *testing.T) {
	ss := &models.SSPortfolioAnalysis{
		PrimaryOptions:    []models.SSPortfolioOption{{ClaimAge: 68, SurvivalRate: 0.9}},
		OptimalPrimaryAge: 68,
		OptimalSpouseAge:  0, // exercises the opt.Spouse==0 -> currentSpouse fallback
	}
	pairs := topKSSPairs(ss, 67, 62, 0)
	if len(pairs) != 1 {
		t.Fatalf("expected exactly 1 pair for k=0, got %d: %+v", len(pairs), pairs)
	}
	if pairs[0] != (ssPair{Primary: 68, Spouse: 62}) {
		t.Errorf("got %+v, want {68 62}", pairs[0])
	}
}

func TestTopKSSPairs_EmptyPrimaryOptionsUsesCurrentPrimary(t *testing.T) {
	ss := &models.SSPortfolioAnalysis{
		SpouseOptions: []models.SSPortfolioOption{
			{ClaimAge: 70, SurvivalRate: 0.9},
			{ClaimAge: 68, SurvivalRate: 0.8},
		},
		OptimalPrimaryAge: 0, // exercises the opt.Primary==0 -> currentPrimary fallback
		OptimalSpouseAge:  70,
	}
	pairs := topKSSPairs(ss, 67, 62, 3)
	want := []ssPair{
		{Primary: 67, Spouse: 70}, // optimum with primary defaulted
		{Primary: 67, Spouse: 68}, // pickPrimary falls back past the empty primary list
		{Primary: 67, Spouse: 62}, // current-settings final fallback
	}
	if len(pairs) != len(want) {
		t.Fatalf("got %d pairs %+v, want %d", len(pairs), pairs, len(want))
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Errorf("pairs[%d]: got %+v, want %+v", i, pairs[i], want[i])
		}
	}
}

// --- tax_optimizer_strategies.go ---

func TestInflatedBracketTopForYear_GuardBranches(t *testing.T) {
	if _, ok := inflatedBracketTopForYear(nil, 0.12, 0); ok {
		t.Error("nil settings: expected ok=false")
	}
	noTax := &models.WhatIfSettings{CurrentAge: 60}
	if _, ok := inflatedBracketTopForYear(noTax, 0.12, 0); ok {
		t.Error("nil TaxConfig: expected ok=false")
	}
	s := &models.WhatIfSettings{
		CurrentAge: 60,
		TaxConfig:  &models.TaxConfig{FilingStatus: models.FilingSingle},
	}
	if _, ok := inflatedBracketTopForYear(s, 0.32, 0); ok {
		t.Error("unsupported 32%% target: expected ok=false")
	}
}

func TestBracketTopFor_UnknownFilingStatus(t *testing.T) {
	ceiling, ok := bracketTopFor(models.FilingStatus("bogus"), 0.12)
	if ok || ceiling != 0 {
		t.Errorf("unknown status: got (%v, %v), want (0, false)", ceiling, ok)
	}
}

func TestEstimateOtherTaxableIncome_NilSettings(t *testing.T) {
	if got := estimateOtherTaxableIncome(nil, 0); got != 0 {
		t.Errorf("nil settings: got %v, want 0", got)
	}
}

// highIncomeSettings has ordinary income far above the 24% bracket top in
// every projection year, so bracket-fill conversion room is always zero.
func highIncomeSettings() *models.WhatIfSettings {
	return &models.WhatIfSettings{
		CurrentAge:         60,
		ProjectionYears:    20,
		PortfolioValue:     1_000_000,
		TaxDeferredPercent: 80,
		InflationRate:      2.5,
		TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingSingle},
		SocialSecurity:     &models.SocialSecurityConfig{FRABenefit: 2000, FRA: 67, ClaimAge: 67},
		IncomeSources: []models.IncomeSource{
			{ID: "p", Name: "Pension", Amount: 60_000, Type: models.IncomeFixed}, // $720k/yr
		},
	}
}

func TestEnumerateBracketFillStrategies_NilTaxConfig(t *testing.T) {
	s := highIncomeSettings()
	s.TaxConfig = nil
	if got := enumerateBracketFillStrategies(s); got != nil {
		t.Errorf("nil TaxConfig: got %+v, want nil", got)
	}
}

func TestEnumerateBracketFillStrategies_UnknownFilingStatusSkipsFamily(t *testing.T) {
	s := highIncomeSettings()
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingStatus("bogus")}
	if got := enumerateBracketFillStrategies(s); got != nil {
		t.Errorf("unknown filing status: got %+v, want nil", got)
	}
}

func TestEnumerateBracketFillStrategies_AllZeroConversionCandidatesSkipped(t *testing.T) {
	// Valid windows exist (age 60, claim 67), but $720k/yr of ordinary
	// income leaves zero conversion room below every supported bracket
	// ceiling — every candidate hits the bracketFillProducesNonZero gate.
	got := enumerateBracketFillStrategies(highIncomeSettings())
	if len(got) != 0 {
		t.Errorf("expected all candidates skipped, got %d: %+v", len(got), got)
	}
}

func TestBracketFillProducesNonZero_Branches(t *testing.T) {
	lowIncome := &models.WhatIfSettings{
		CurrentAge:         60,
		ProjectionYears:    20,
		PortfolioValue:     1_000_000,
		TaxDeferredPercent: 80,
		InflationRate:      2.5,
		TaxConfig:          &models.TaxConfig{FilingStatus: models.FilingSingle},
	}

	// Window starting before CurrentAge: startProjYear clamps to 0 and the
	// scan still finds room in year 0.
	preAge := strategyWindow{StartAge: 55, EndAge: 62, Anchor: "5yr"}
	if !bracketFillProducesNonZero(lowIncome, preAge, 0.12) {
		t.Error("clamped pre-age window with no income should have conversion room")
	}

	// Unsupported target bracket: inflatedBracketTopForYear fails -> false.
	w := strategyWindow{StartAge: 60, EndAge: 65, Anchor: "IRMAA"}
	if bracketFillProducesNonZero(lowIncome, w, 0.32) {
		t.Error("unsupported 32% target should report no non-zero conversions")
	}

	// Income above the ceiling in every year: loop completes -> false.
	if bracketFillProducesNonZero(highIncomeSettings(), w, 0.24) {
		t.Error("income above the 24% ceiling should leave no conversion room")
	}
}

func TestStrategyYearlyConversions_GuardBranches(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:      60,
		ProjectionYears: 20,
		TaxConfig:       &models.TaxConfig{FilingStatus: models.FilingSingle},
	}

	t.Run("ladder_start_before_current_age_clamped", func(t *testing.T) {
		strat := models.RothOptimizerStrategy{
			Kind: models.RothStrategyLadder, AnnualAmount: 10_000, StartAge: 55, EndAge: 63,
		}
		got := strategyYearlyConversions(s, strat, nil)
		if len(got) != 3 { // projection years 0,1,2 -> ages 60,61,62
			t.Fatalf("got %d entries, want 3: %+v", len(got), got)
		}
		for i, yc := range got {
			if yc.Age != 60+i || yc.Amount != 10_000 {
				t.Errorf("entry %d: got %+v, want age %d amount 10000", i, yc, 60+i)
			}
		}
	})

	t.Run("inverted_window_nil", func(t *testing.T) {
		strat := models.RothOptimizerStrategy{
			Kind: models.RothStrategyLadder, AnnualAmount: 10_000, StartAge: 65, EndAge: 65,
		}
		if got := strategyYearlyConversions(s, strat, nil); got != nil {
			t.Errorf("EndAge<=StartAge: got %+v, want nil", got)
		}
	})

	t.Run("bracket_fill_nil_tax_config_nil", func(t *testing.T) {
		noTax := &models.WhatIfSettings{CurrentAge: 60, ProjectionYears: 20}
		strat := models.RothOptimizerStrategy{
			Kind: models.RothStrategyBracketFill, TargetBracket: 0.22, StartAge: 60, EndAge: 65,
		}
		if got := strategyYearlyConversions(noTax, strat, nil); got != nil {
			t.Errorf("nil TaxConfig: got %+v, want nil", got)
		}
	})

	t.Run("bracket_fill_unsupported_target_nil", func(t *testing.T) {
		strat := models.RothOptimizerStrategy{
			Kind: models.RothStrategyBracketFill, TargetBracket: 0.32, StartAge: 60, EndAge: 65,
		}
		if got := strategyYearlyConversions(s, strat, nil); got != nil {
			t.Errorf("unsupported target: got %+v, want nil", got)
		}
	})

	t.Run("unknown_kind_nil", func(t *testing.T) {
		strat := models.RothOptimizerStrategy{
			Kind: models.RothStrategyKind("weird"), AnnualAmount: 10_000, StartAge: 60, EndAge: 65,
		}
		if got := strategyYearlyConversions(s, strat, nil); got != nil {
			t.Errorf("unknown kind: got %+v, want nil", got)
		}
	})
}

func TestBracketFillProjYearWindow_ClampsNegativeStart(t *testing.T) {
	s := &models.WhatIfSettings{CurrentAge: 60}

	start, end := bracketFillProjYearWindow(s, models.RothOptimizerStrategy{StartAge: 55, EndAge: 63})
	if start != 0 || end != 3 {
		t.Errorf("pre-age window: got (%d,%d), want (0,3)", start, end)
	}

	// End is returned raw; callers treat end<=start as empty.
	start, end = bracketFillProjYearWindow(s, models.RothOptimizerStrategy{StartAge: 70, EndAge: 65})
	if start != 10 || end != 5 {
		t.Errorf("inverted window: got (%d,%d), want (10,5)", start, end)
	}
}

// --- perturb.go ---

func TestPerturbAndPrepare_PanicsOnInvalidSettings(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic for nil settings (prepare.From error)")
		}
	}()
	perturbAndPrepare(nil)
}

// --- monte_carlo.go ---

func TestMean_EmptyAndValues(t *testing.T) {
	if got := mean(nil); got != 0 {
		t.Errorf("mean(nil): got %v, want 0", got)
	}
	if got := mean([]float64{2, 4, 9}); got != 5 {
		t.Errorf("mean([2 4 9]): got %v, want 5", got)
	}
}

// --- ss.go ---

func TestNormalizedSSCOLARate_NegativeClampedToZero(t *testing.T) {
	neg := -0.01
	if got := NormalizedSSCOLARate(&neg); got != 0 {
		t.Errorf("negative COLA: got %v, want 0", got)
	}
}

func TestSSConfigCOLARate_NilInputs(t *testing.T) {
	if got := SSConfigCOLARate(nil); got != defaultSocialSecurityCOLARate {
		t.Errorf("nil settings: got %v, want %v", got, defaultSocialSecurityCOLARate)
	}
	if got := SSConfigCOLARate(&models.WhatIfSettings{}); got != defaultSocialSecurityCOLARate {
		t.Errorf("nil SocialSecurity: got %v, want %v", got, defaultSocialSecurityCOLARate)
	}
}

func TestAdjustedSSBenefit_ClampsClaimAge(t *testing.T) {
	// Below 62 clamps to 62.
	if got, want := AdjustedSSBenefit(1000, 67, 50), AdjustedSSBenefit(1000, 67, 62); got != want {
		t.Errorf("claim 50 vs 62: got %v, want %v", got, want)
	}
	// Above 70 clamps to 70: 36 months delayed × 2/300 = +24%.
	if got := AdjustedSSBenefit(1000, 67, 80); !approxEq(got, 1240) {
		t.Errorf("claim 80: got %v, want 1240", got)
	}
}

func TestDerivedPIA_ClampsAndDegenerateFactor(t *testing.T) {
	// Claim age above 70 clamps to 70 (factor 1.24 at FRA 67).
	got := DerivedPIA(1240, 67, 75)
	if !approxEq(got, 1000) {
		t.Errorf("claim 75: got %v, want 1000", got)
	}
	// Absurd FRA drives the adjustment factor <= 0; the actual benefit is
	// returned unchanged rather than divided by a non-positive factor.
	if got := DerivedPIA(2000, 100, 62); got != 2000 {
		t.Errorf("degenerate factor: got %v, want 2000", got)
	}
}

func TestAdjustedSpousalBenefit_ClampsEarlyClaimAge(t *testing.T) {
	// Claim below 62 clamps to 62: 60 months early at FRA 67 -> 35% reduction.
	got := AdjustedSpousalBenefit(1000, 67, 50)
	if !approxEq(got, 650) {
		t.Errorf("claim 50 (clamped 62): got %v, want 650", got)
	}
	if want := AdjustedSpousalBenefit(1000, 67, 62); got != want {
		t.Errorf("clamped result %v differs from explicit age-62 result %v", got, want)
	}
}

func TestSpousalTopUp_NoHigherPIA(t *testing.T) {
	if got := SpousalTopUp(1500, 0, 67, 62); got != 1500 {
		t.Errorf("zero higher PIA: got %v, want own benefit 1500", got)
	}
}

func TestCumulativeBenefit_TargetAtOrBeforeClaim(t *testing.T) {
	if got := cumulativeBenefit(1000, 70, 70, 0.02); got != 0 {
		t.Errorf("target == claim: got %v, want 0", got)
	}
	if got := cumulativeBenefit(1000, 70, 65, 0.02); got != 0 {
		t.Errorf("target < claim: got %v, want 0", got)
	}
}

func TestSSPortfolioEligibility_NilInputs(t *testing.T) {
	if SSPortfolioPrimaryEligible(nil) {
		t.Error("nil settings should not be primary-eligible")
	}
	if SSPortfolioPrimaryEligible(&models.WhatIfSettings{CurrentAge: 60}) {
		t.Error("nil SocialSecurity should not be primary-eligible")
	}
	if SSPortfolioEligible(nil) {
		t.Error("nil settings should not be eligible")
	}
	if SSPortfolioEligible(&models.WhatIfSettings{CurrentAge: 60}) {
		t.Error("nil SocialSecurity should not be eligible")
	}
}

func TestSSAnalysis_NilSettings(t *testing.T) {
	if got := SSAnalysis(engine.Input{}); got != nil {
		t.Errorf("nil prepared settings: got %+v, want nil", got)
	}
}

func TestRunSSPortfolioCellMC_NilSettings(t *testing.T) {
	if got := runSSPortfolioCellMC(engine.New(), engine.Input{}, 67, 0, 1); got != nil {
		t.Errorf("nil prepared settings: got %+v, want nil", got)
	}
}

func TestBuildSSPortfolioOptions_SkipsNilCells(t *testing.T) {
	// Nil prepared settings make every MC cell nil (selected age uses the
	// nil baseline; other ages fail the settings clone), so no options are
	// produced — the nil-mc continue branch runs for every age.
	got := buildSSPortfolioOptions(
		engine.New(), engine.Input{},
		69, 70,
		map[int]float64{69: 2000, 70: 2480},
		func(age int) (int, int) { return age, 0 },
		nil, 1,
	)
	if len(got) != 0 {
		t.Errorf("expected no options when every cell is nil, got %d: %+v", len(got), got)
	}
}

// --- budget_fit.go steady-state helpers ---

func steadyStateFixtureProj() *models.ProjectionResult {
	return &models.ProjectionResult{
		Months: []models.ProjectionMonth{
			{PortfolioBalance: 500, TaxDeferredBalance: 300, TaxableBalance: 150, RothBalance: 50},
			{PortfolioBalance: 150, TaxDeferredBalance: 90, TaxableBalance: 40, RothBalance: 20},
		},
	}
}

func TestBucketBalancesAt_ClampAndClosedFormFallback(t *testing.T) {
	s := &models.WhatIfSettings{PortfolioValue: 100_000, TaxDeferredPercent: 40}

	// Month past the projection end clamps to the final month.
	td, tx := bucketBalancesAt(steadyStateFixtureProj(), 99, s, 10, 5, 60_000)
	if td != 90 || tx != 40 {
		t.Errorf("clamped month: got (%v,%v), want (90,40)", td, tx)
	}

	// No projection: closed-form compound growth over month/12 years.
	td, tx = bucketBalancesAt(nil, 24, s, 10, 5, 60_000)
	wantTD := 40_000 * math.Pow(1.10, 2)
	wantTX := 60_000 * math.Pow(1.05, 2)
	if !approxEq(td, wantTD) || !approxEq(tx, wantTX) {
		t.Errorf("closed-form: got (%v,%v), want (%v,%v)", td, tx, wantTD, wantTX)
	}
}

func TestSteadyStatePortfolioBalance_ClampAndFallback(t *testing.T) {
	s := &models.WhatIfSettings{PortfolioValue: 200_000}
	proj := steadyStateFixtureProj()

	if got := steadyStatePortfolioBalance(proj, 0, s, 10); got != 500 {
		t.Errorf("in-range month: got %v, want 500", got)
	}
	// Month past the end clamps to the final projected month.
	if got := steadyStatePortfolioBalance(proj, 99, s, 10); got != 150 {
		t.Errorf("clamped month: got %v, want 150", got)
	}
	// No projection: closed-form compound growth.
	want := 200_000 * math.Pow(1.10, 1)
	if got := steadyStatePortfolioBalance(nil, 12, s, 10); !approxEq(got, want) {
		t.Errorf("closed-form: got %v, want %v", got, want)
	}
}

func TestSteadyStateWithdrawalMixShares_ClampsToLastMonth(t *testing.T) {
	s := &models.WhatIfSettings{TaxDeferredPercent: 10, RothPercent: 10}
	pTD, pTX, pR := steadyStateWithdrawalMixShares(steadyStateFixtureProj(), 99, s)
	// Final month: 90/40/20 of 150 — not the 10/80/10 static allocation.
	if !approxEq(pTD, 90.0/150) || !approxEq(pTX, 40.0/150) || !approxEq(pR, 20.0/150) {
		t.Errorf("clamped shares: got (%v,%v,%v), want (0.6, 0.2667, 0.1333)", pTD, pTX, pR)
	}
	if !approxEq(pTD+pTX+pR, 1) {
		t.Errorf("shares must sum to 1, got %v", pTD+pTX+pR)
	}
}
