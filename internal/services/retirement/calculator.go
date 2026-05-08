package retirement

import (
	"fmt"
	"math"
	"math/rand"

	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
	"budget2/internal/services/retirement/prepare"
)

// PreparedChainLink is re-exported from the engine package so existing
// callers (handlers, tests) keep compiling during the migration. The
// alias is removed in Task 8 alongside Calculator.
type PreparedChainLink = engine.PreparedChainLink

// monteCarloSeedOverride forces a deterministic RNG seed in
// RunMonteCarloSimulation so parity tests can compare runs. Parity-window
// only; removed in Task 8 alongside Calculator.
type monteCarloSeedOverride struct {
	set  bool
	seed int64
}

// Calculator performs retirement projections and analysis
type Calculator struct {
	Prepared      prepare.PreparedSettings
	Settings      *models.WhatIfSettings
	ResolvedChain []PreparedChainLink

	mcSeedOverride monteCarloSeedOverride // parity-window only; removed in Task 8
}

// SetMonteCarloSeedForParity pins the Monte Carlo RNG to a specific seed
// so parity tests can compare two RunFullAnalysis invocations
// byte-equal. Parity-window only; removed in Task 8.
func (c *Calculator) SetMonteCarloSeedForParity(seed int64) {
	c.mcSeedOverride = monteCarloSeedOverride{set: true, seed: seed}
}

// NewCalculator creates a new retirement calculator from a prepared settings snapshot.
func NewCalculator(prepared prepare.PreparedSettings) *Calculator {
	return &Calculator{
		Prepared: prepared,
		Settings: prepared.Settings(),
	}
}

// NewCalculatorWithChain creates a new retirement calculator with the given prepared settings and scenario chain.
func NewCalculatorWithChain(prepared prepare.PreparedSettings, chain []PreparedChainLink) *Calculator {
	return &Calculator{
		Prepared:      prepared,
		Settings:      prepared.Settings(),
		ResolvedChain: chain,
	}
}

// perturbAndPrepare moved to analysis/perturb.go. The retirement-side
// alias used to live here for SS and backtest, but those analyses now
// live in the analysis package and call the analysis-internal copy
// directly — no remaining retirement-side caller. Removed.

// PresentValue calculates the present value of a future cash flow
// PV = FV / (1 + r)^n
func PresentValue(futureValue, annualRate float64, periods int) float64 {
	if periods <= 0 {
		return futureValue
	}
	if annualRate <= 0 {
		return futureValue
	}
	monthlyRate := monthlyCompoundFactorFromPercent(annualRate) - 1
	return futureValue / math.Pow(1+monthlyRate, float64(periods))
}

// PresentValueAnnuity calculates the PV of a series of payments
// Handles both regular and growing annuities
func PresentValueAnnuity(payment, discountRate, growthRate float64, startMonth, numPayments int) float64 {
	if numPayments <= 0 || payment == 0 {
		return 0
	}

	monthlyRate := monthlyCompoundFactorFromPercent(discountRate) - 1
	monthlyGrowth := monthlyCompoundFactorFromPercent(growthRate) - 1

	var pvAtStart float64

	if monthlyRate <= 0 {
		if monthlyGrowth == 0 {
			pvAtStart = payment * float64(numPayments)
		} else {
			total := 0.0
			for m := 0; m < numPayments; m++ {
				total += payment * math.Pow(1+monthlyGrowth, float64(m))
			}
			pvAtStart = total
		}
	} else if math.Abs(monthlyRate-monthlyGrowth) < 1e-10 {
		// Growth equals discount rate
		pvAtStart = payment * float64(numPayments)
	} else if monthlyGrowth != 0 {
		// Growing annuity formula
		growthFactor := (1 + monthlyGrowth) / (1 + monthlyRate)
		pvAtStart = payment * (1 - math.Pow(growthFactor, float64(numPayments))) / (monthlyRate - monthlyGrowth)
	} else {
		// Regular annuity formula
		pvAtStart = payment * (1 - math.Pow(1+monthlyRate, -float64(numPayments))) / monthlyRate
	}

	// Discount back if payments start in the future
	if startMonth > 0 && monthlyRate > 0 {
		return pvAtStart / math.Pow(1+monthlyRate, float64(startMonth))
	}

	return pvAtStart
}

// CalculateTotalIncome returns total income for a specific month
func (c *Calculator) CalculateTotalIncome(month int) float64 {
	return engine.TotalIncomeForCalculator(c.Settings, month)
}

func monthlyCompoundFactorFromDecimal(annualRate float64) float64 {
	if annualRate == 0 {
		return 1.0
	}
	return math.Pow(1+annualRate, 1.0/12.0)
}

func monthlyCompoundFactorFromPercent(annualRatePercent float64) float64 {
	return monthlyCompoundFactorFromDecimal(annualRatePercent / 100)
}

// compoundedFactorFromPercent moved to engine.compoundedFactorFromPercent;
// no retirement-side caller remains after Bundle F. Removed in Task 8.

// rebaseLivingExpensesAtTransition was moved to the engine package.
// The var-alias keeps tests in coverage_gaps_test.go (which exercise
// the helper directly) compiling unchanged. Removed in Task 8. The
// companion calculateLivingExpensesAtMonth alias was retired with
// backtest.go's move to the analysis package.
var rebaseLivingExpensesAtTransition = engine.RebaseLivingExpensesAtTransition

// Phase 4 projection tax policy:
//   - Taxable-account sales realize average-cost capital gains based on the current
//     taxable-account basis model.
//   - Pension, Social Security, tax-deferred withdrawals (including RMDs), non-qualified
//     dividends, and Roth conversions are taxed as ordinary income.
//   - Qualified dividends and realized long-term capital gains use the simplified
//     long-term capital-gains brackets in tax.go.
//   - Roth withdrawals are not taxed.
//
// projectionTaxAccumulator + its inputs/snapshot live in the engine package
// during the migration window. The aliases keep existing call sites in
// calculator.go and backtest.go compiling unchanged. Removed in Task 8.
type (
	projectionTaxAccumulator = engine.ProjectionTaxAccumulator
	projectedTaxSnapshot     = engine.ProjectedTaxSnapshot
)

// medicareEligibleAdultCountAtYear, plannerIRMAAInflationFactorForYear,
// calculateMonthlyIncomeBreakdown, isSocialSecurityIncomeSource, and
// rothConversionAmountForYear all moved to the engine package. The
// var-aliases keep existing call sites (here, in backtest.go,
// social_security.go, and tests) compiling unchanged. Removed in
// Task 8.
var (
	medicareEligibleAdultCountAtYear   = engine.MedicareEligibleAdultCountAtYear
	plannerIRMAAInflationFactorForYear = engine.PlannerIRMAAInflationFactorForYear
	calculateMonthlyIncomeBreakdown    = engine.CalculateMonthlyIncomeBreakdown
	isSocialSecurityIncomeSource       = engine.IsSocialSecurityIncomeSource
	rothConversionAmountForYear        = engine.RothConversionAmountForYear
)

// plannerInflationFactorForYear is referenced by other retirement-package
// code (e.g. CalculatePresentValueAnalysis); leave its body in place.
func plannerInflationFactorForYear(annualInflationRate float64, years float64) float64 {
	if years <= 0 {
		return 1
	}
	return math.Pow(1+annualInflationRate/100, years)
}

// IsSocialSecurityIncomeSource exposes the SS income-source detection
// rule for use in templates that need to flag manual SS sources
// excluded by the optimizer. Forwards to the engine implementation.
func IsSocialSecurityIncomeSource(source models.IncomeSource) bool {
	return engine.IsSocialSecurityIncomeSource(source)
}

// Taxable-account state machine + return-decomposition primitives now
// live in the engine package; the aliases (and var-aliases) keep
// existing call sites compiling unchanged. Removed in Task 8.
type (
	taxableReturnComponents = engine.TaxableReturnComponents
	taxableGrowthResult     = engine.TaxableGrowthResult
	taxableAccountState     = engine.TaxableAccountState
)

var (
	newTaxableAccountState       = engine.NewTaxableAccountState
	buildTaxableReturnComponents = engine.BuildTaxableReturnComponents
)

func expectedTaxableMonthlyCashFlow(s *models.WhatIfSettings, taxableMarketValue, taxableAnnualReturn float64) taxableGrowthResult {
	account := newTaxableAccountState(s, taxableMarketValue)
	return account.ApplyGrowth(buildTaxableReturnComponents(taxableAnnualReturn, s), 1.0)
}

// CalculateTotalExpenses returns total expenses for a specific month
func (c *Calculator) CalculateTotalExpenses(month int) float64 {
	return engine.TotalExpensesForCalculator(c.Settings, month)
}

// Projection-month state machine moved to the engine package. The
// var-aliases below keep tests compiling unchanged. The
// executeTaxAwarePortfolioMonth alias was retired with backtest.go's
// move to analysis. Removed in Task 8.
var (
	reinvestRequiredRMDToTaxableState     = engine.ReinvestRequiredRMDToTaxableState
	applyBigTicketExpenseWithTaxableState = engine.ApplyBigTicketExpenseWithTaxableState
	withdrawForExpenses                   = engine.WithdrawForExpenses
)

// taxDeferredDelayActive, shortfallCausesDepletion, and
// earlyWithdrawalPenaltyRate were retirement-side helpers consumed
// only by backtest.go and the deprecated RunMonteCarloSimulation
// body. Both have moved to the analysis package; engine still owns
// the canonical implementations. The retirement-side wrappers were
// retired with the moves.

// withdrawForExpenses moved to engine.WithdrawForExpenses; the
// var-alias near the top of this file keeps existing call sites
// unchanged. The legacy applyBigTicketExpense (which operates on raw
// *float64 balances rather than the taxable-account state machine)
// stays in retirement for now — it's no longer used by RunProjection
// but other helpers may reference it. Removed in Task 8.
func applyBigTicketExpense(amount float64, allowTaxDeferred bool, earlyPenaltyRate float64, taxDeferredBalance, taxableBalance, rothBalance *float64) float64 {
	remaining := amount

	if remaining > 0 && *taxableBalance > 0 {
		fromTaxable := math.Min(remaining, *taxableBalance)
		*taxableBalance -= fromTaxable
		remaining -= fromTaxable
	}

	if remaining > 0 && *rothBalance > 0 {
		fromRoth := math.Min(remaining, *rothBalance)
		*rothBalance -= fromRoth
		remaining -= fromRoth
	}

	if allowTaxDeferred && remaining > 0 && *taxDeferredBalance > 0 {
		effectiveFactor := 1.0 - earlyPenaltyRate
		grossNeeded := remaining / effectiveFactor
		fromTaxDeferred := math.Min(grossNeeded, *taxDeferredBalance)
		*taxDeferredBalance -= fromTaxDeferred
		remaining -= fromTaxDeferred * effectiveFactor
	}

	return remaining
}

// ExpenseBreakdown is re-exported from the engine package so existing
// callers keep compiling during the migration. The alias is removed in
// Task 8 alongside Calculator.
type ExpenseBreakdown = engine.ExpenseBreakdown

// CalculateExpenseBreakdown separates expenses into discretionary and essential
func (c *Calculator) CalculateExpenseBreakdown(month int) ExpenseBreakdown {
	return engine.ExpenseBreakdownForCalculator(c.Settings, month)
}

// RunProjection runs a full retirement projection with RMD integration
func (c *Calculator) RunProjection() *models.ProjectionResult {
	return engine.New().Run(engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}


// CalculateBudgetFit analyzes monthly budget gap. Delegates to
// analysis.BudgetFit; the body now lives in the analysis package.
func (c *Calculator) CalculateBudgetFit() *models.BudgetFitAnalysis {
	return analysis.BudgetFit(engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}

// CalculatePresentValueAnalysis computes PV of expenses and income.
// Delegates to analysis.PresentValue; the body now lives in the analysis
// package.
func (c *Calculator) CalculatePresentValueAnalysis() *models.PresentValueAnalysis {
	return analysis.PresentValue(engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}

// findSteadyStateMonth finds the month when all income sources are active.
// Returns 0 if all sources start immediately (no delayed income). Delegates
// to engine.FindSteadyStateMonthForCalculator.
func (c *Calculator) findSteadyStateMonth() int {
	return engine.FindSteadyStateMonthForCalculator(c.Settings)
}

// CalculateSustainabilityScore computes the sustainability score
func (c *Calculator) CalculateSustainabilityScore(projection *models.ProjectionResult) *models.SustainabilityScore {
	return analysis.Score(projection, c.CalculateBudgetFit())
}

// CalculateSensitivity runs sensitivity analysis on key parameters.
// Delegates to analysis.Sensitivity; the body now lives in the analysis
// package.
func (c *Calculator) CalculateSensitivity() []models.SensitivityResult {
	return analysis.Sensitivity(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}

// CalculateFailurePoints finds exact thresholds where the portfolio
// fails. Delegates to analysis.FailurePoints; the body and the four
// binary-search helpers (findReturnThreshold, findInflationThreshold,
// findExpensesThreshold, findPortfolioThreshold) now live in the
// analysis package.
func (c *Calculator) CalculateFailurePoints() *models.FailurePointAnalysis {
	return analysis.FailurePoints(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}

// findReturnThreshold, findInflationThreshold, findExpensesThreshold,
// and findPortfolioThreshold forward to their analysis-package
// counterparts. Parity-window only — used by retirement-side tests in
// coverage_gaps_test.go that exercise the binary-search helpers
// directly. Removed in Task 8.
func (c *Calculator) findReturnThreshold() *models.FailurePoint {
	return analysis.FindReturnThresholdForCalculator(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}

func (c *Calculator) findInflationThreshold() *models.FailurePoint {
	return analysis.FindInflationThresholdForCalculator(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}

func (c *Calculator) findExpensesThreshold() *models.FailurePoint {
	return analysis.FindExpensesThresholdForCalculator(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}

func (c *Calculator) findPortfolioThreshold() *models.FailurePoint {
	return analysis.FindPortfolioThresholdForCalculator(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}

// MonteCarloConfig and the related Monte Carlo types now live in the
// analysis package. The aliases keep existing retirement-side callers
// (test fixtures, the parity-window MC method wrappers below) compiling
// unchanged. Removed in Task 8.
type MonteCarloConfig = analysis.MonteCarloConfig

// DefaultMonteCarloConfig forwards to analysis.DefaultMonteCarloConfig.
// Removed in Task 8.
var DefaultMonteCarloConfig = analysis.DefaultMonteCarloConfig

// RunMonteCarloSimulation runs enhanced randomized scenario analysis.
// Delegates to analysis.MonteCarlo, threading the parity-window seed
// override (if set) so deterministic comparisons stay reproducible.
func (c *Calculator) RunMonteCarloSimulation(runs int) *models.MonteCarloAnalysis {
	seed := int64(0)
	if c.mcSeedOverride.set {
		seed = c.mcSeedOverride.seed
	}
	return analysis.MonteCarlo(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	}, runs, seed)
}

// The Monte Carlo simulation body (runSingleMonteCarloSimulation,
// generateAssetReturns, generateYearlyReturns,
// calculateSequenceRiskImpact, calculateSequenceRiskBreakdown,
// createDistributionBuckets, plus the CrashTiming and AssetReturns
// types) now lives in the analysis package. The Calculator-side
// methods stay as one-line delegators so existing tests
// (calculator_test.go in particular) keep compiling unchanged. The
// type aliases preserve byte-equal struct literals at call sites.
// Removed in Task 8.

type (
	CrashTiming  = analysis.CrashTiming
	AssetReturns = analysis.AssetReturns
)

// runSingleMonteCarloSimulation forwards to analysis.RunSingleMonteCarloSimulationForCalculator.
func (c *Calculator) runSingleMonteCarloSimulation(rng *rand.Rand, config *MonteCarloConfig) models.MonteCarloResult {
	return analysis.RunSingleMonteCarloSimulationForCalculator(engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	}, rng, config)
}

// generateYearlyReturns forwards to analysis.GenerateYearlyReturnsForCalculator.
// Parity-window only — exercised by calculator_test.go directly. The
// allocation-based generateAssetReturns helper has no surviving
// retirement-side caller, so it's gone — analysis.GenerateAssetReturnsForCalculator
// remains for any future shim need. Removed in Task 8.
func (c *Calculator) generateYearlyReturns(rng *rand.Rand, config *MonteCarloConfig, years int, timing *CrashTiming, lastCrashYear *int) []float64 {
	return analysis.GenerateYearlyReturnsForCalculator(c.Settings, rng, config, years, timing, lastCrashYear)
}

// calculateSequenceRiskImpact forwards to
// analysis.CalculateSequenceRiskImpactForCalculator. Parity-window
// only — used by existing tests in calculator_test.go.
func (c *Calculator) calculateSequenceRiskImpact(results []models.MonteCarloResult) float64 {
	return analysis.CalculateSequenceRiskImpactForCalculator(results)
}

// createDistributionBuckets forwards to
// analysis.CreateDistributionBucketsForCalculator. Parity-window only —
// used by existing tests in coverage_gaps_test.go.
func (c *Calculator) createDistributionBuckets(sortedBalances []float64) *models.MonteCarloDistribution {
	return analysis.CreateDistributionBucketsForCalculator(sortedBalances)
}


// formatBucketLabel formats a bucket range for display
func formatBucketLabel(low, high float64) string {
	formatVal := func(v float64) string {
		if v >= 1000000 {
			return fmt.Sprintf("$%.1fM", v/1000000)
		}
		return fmt.Sprintf("$%.0fK", v/1000)
	}

	if high < 0 {
		return formatVal(low) + "+"
	}
	return formatVal(low) + "-" + formatVal(high)
}

// Helper functions

// sortFloat64s previously lived here; the moved-to-analysis Monte Carlo
// loop owns its own copy, and no retirement-side caller remains.
// Removed.

func mean(a []float64) float64 {
	if len(a) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range a {
		sum += v
	}
	return sum / float64(len(a))
}

func (c *Calculator) buildProjectionExplainability(projection *models.ProjectionResult) *models.ProjectionExplainability {
	return analysis.BuildExplainability(projection, engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}

// RunSSAnalysis computes the full SS claiming-age comparison.
// Delegates to analysis.SSAnalysis.
func (c *Calculator) RunSSAnalysis() *models.SSComparisonAnalysis {
	return analysis.SSAnalysis(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	})
}

// RunSSPortfolioAnalysis evaluates how eligible claiming ages affect
// portfolio survival. Delegates to analysis.SSPortfolioWithSeed,
// threading the parity-window MC seed override (if set) so
// deterministic comparisons stay reproducible.
func (c *Calculator) RunSSPortfolioAnalysis(ss *models.SSComparisonAnalysis) *models.SSPortfolioAnalysis {
	seed := int64(0)
	if c.mcSeedOverride.set {
		seed = c.mcSeedOverride.seed
	}
	return analysis.SSPortfolioWithSeed(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	}, ss, seed)
}

// RunHistoricalBacktest runs the projection against all available
// historical sequences. Delegates to analysis.HistoricalBacktest.
func (c *Calculator) RunHistoricalBacktest() *models.HistoricalBacktestAnalysis {
	return analysis.HistoricalBacktest(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	}, history.DefaultData())
}

// runSingleHistoricalSequence forwards to
// analysis.RunSingleHistoricalSequenceForCalculator. Parity-window
// only — used by retirement-side tests in coverage_gaps_test.go that
// exercise a single historical sequence directly. Removed in Task 8.
func (c *Calculator) runSingleHistoricalSequence(startYear int) HistoricalSequenceResult {
	return analysis.RunSingleHistoricalSequenceForCalculator(engine.New(), engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
	}, history.DefaultData(), startYear)
}

// HistoricalSequenceResult is re-exported from analysis so existing
// retirement-side callers (tests) keep compiling. Removed in Task 8.
type HistoricalSequenceResult = analysis.HistoricalSequenceResult

// yearsUntilDepletion forwards to
// analysis.YearsUntilDepletionForCalculator. Parity-window only —
// used by retirement-side tests in coverage_gaps_test.go. Removed in
// Task 8.
var yearsUntilDepletion = analysis.YearsUntilDepletionForCalculator

// RunFullAnalysis performs complete what-if analysis
func (c *Calculator) RunFullAnalysis() *models.WhatIfAnalysis {
	projection := c.RunProjection()
	projectionExplainability := c.buildProjectionExplainability(projection)
	budgetFit := c.CalculateBudgetFit()
	presentValue := c.CalculatePresentValueAnalysis()
	sustainability := c.CalculateSustainabilityScore(projection)
	sensitivity := c.CalculateSensitivity()
	failurePoints := c.CalculateFailurePoints()
	monteCarlo := c.RunMonteCarloSimulation(1000)
	rmd := c.BuildRMDAnalysis(projection)
	historicalBacktest := c.RunHistoricalBacktest()

	// Add Monte Carlo success rate for comparison
	if historicalBacktest != nil && monteCarlo != nil && monteCarlo.Stats != nil {
		historicalBacktest.MonteCarloSuccessRate = monteCarlo.Stats.SuccessRate
		historicalBacktest.HistoricalVsMC = historicalBacktest.SuccessRate - monteCarlo.Stats.SuccessRate
	}

	var ssAnalysis *models.SSComparisonAnalysis
	if c.Settings.SocialSecurity != nil && c.Settings.SocialSecurity.FRABenefit > 0 {
		ssAnalysis = c.RunSSAnalysis()
		if ssAnalysis != nil && SSPortfolioEligible(c.Settings) {
			ssAnalysis.Portfolio = c.RunSSPortfolioAnalysis(ssAnalysis)
		}
	}

	return &models.WhatIfAnalysis{
		Settings:                 c.Settings,
		Projection:               projection,
		ProjectionExplainability: projectionExplainability,
		BudgetFit:                budgetFit,
		PresentValue:             presentValue,
		Sustainability:           sustainability,
		Sensitivity:              sensitivity,
		FailurePoints:            failurePoints,
		MonteCarlo:               monteCarlo,
		RMD:                      rmd,
		HistoricalBacktest:       historicalBacktest,
		SocialSecurity:           ssAnalysis,
	}
}
