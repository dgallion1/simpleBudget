package retirement

import (
	"math/rand"

	"budget2/internal/models"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
	"budget2/internal/services/retirement/prepare"
)

// Calculator is a test-only helper that wraps engine + analysis to give
// retirement-package tests a stable façade for exercising the projection
// pipeline. The production code path runs through engine.New() +
// retirement.RunFull(); this helper keeps the legacy method-call shape so
// the existing test suite continues to compile without bulk rewrites.
type Calculator struct {
	Prepared      prepare.PreparedSettings
	Settings      *models.WhatIfSettings
	ResolvedChain []PreparedChainLink

	mcSeed    int64
	mcSeedSet bool
}

// SetMonteCarloSeedForParity pins the Monte Carlo RNG to a specific seed
// so deterministic tests can compare runs.
func (c *Calculator) SetMonteCarloSeedForParity(seed int64) {
	c.mcSeed = seed
	c.mcSeedSet = true
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

func (c *Calculator) input() engine.Input {
	return engine.Input{
		Prepared: c.Prepared,
		Chain:    c.ResolvedChain,
		Hooks:    DefaultHooks(),
	}
}

// CalculateTotalIncome returns total income for a specific month.
func (c *Calculator) CalculateTotalIncome(month int) float64 {
	return engine.TotalIncome(c.Settings, month)
}

// CalculateTotalExpenses returns total expenses for a specific month.
func (c *Calculator) CalculateTotalExpenses(month int) float64 {
	return engine.TotalExpenses(c.Settings, month)
}

// CalculateExpenseBreakdown separates expenses into discretionary and essential.
func (c *Calculator) CalculateExpenseBreakdown(month int) ExpenseBreakdown {
	return engine.CalculateExpenseBreakdown(c.Settings, month)
}

// RunProjection runs a full retirement projection.
func (c *Calculator) RunProjection() *models.ProjectionResult {
	return engine.New().Run(c.input())
}

// CalculateBudgetFit analyzes monthly budget gap.
func (c *Calculator) CalculateBudgetFit() *models.BudgetFitAnalysis {
	return analysis.BudgetFit(c.input())
}

// CalculatePresentValueAnalysis computes PV of expenses and income.
func (c *Calculator) CalculatePresentValueAnalysis() *models.PresentValueAnalysis {
	return analysis.PresentValue(c.input())
}

// findSteadyStateMonth finds the month when all income sources are active.
func (c *Calculator) findSteadyStateMonth() int {
	return engine.FindSteadyStateMonth(DefaultHooks(), c.Settings)
}

// CalculateSustainabilityScore computes the sustainability score.
func (c *Calculator) CalculateSustainabilityScore(projection *models.ProjectionResult) *models.SustainabilityScore {
	return analysis.Score(projection, c.CalculateBudgetFit())
}

// CalculateSensitivity runs sensitivity analysis on key parameters.
func (c *Calculator) CalculateSensitivity() []models.SensitivityResult {
	return analysis.Sensitivity(engine.New(), c.input())
}

// CalculateFailurePoints finds exact thresholds where the portfolio fails.
func (c *Calculator) CalculateFailurePoints() *models.FailurePointAnalysis {
	return analysis.FailurePoints(engine.New(), c.input())
}

func (c *Calculator) findReturnThreshold() *models.FailurePoint {
	return analysis.FindReturnThreshold(engine.New(), c.input())
}

func (c *Calculator) findInflationThreshold() *models.FailurePoint {
	return analysis.FindInflationThreshold(engine.New(), c.input())
}

func (c *Calculator) findExpensesThreshold() *models.FailurePoint {
	return analysis.FindExpensesThreshold(engine.New(), c.input())
}

func (c *Calculator) findPortfolioThreshold() *models.FailurePoint {
	return analysis.FindPortfolioThreshold(engine.New(), c.input())
}

// MonteCarloConfig is re-exported from analysis so existing tests keep compiling.
type MonteCarloConfig = analysis.MonteCarloConfig

// DefaultMonteCarloConfig forwards to analysis.DefaultMonteCarloConfig.
var DefaultMonteCarloConfig = analysis.DefaultMonteCarloConfig

// CrashTiming and AssetReturns are re-exported from analysis so existing
// tests keep compiling.
type (
	CrashTiming  = analysis.CrashTiming
	AssetReturns = analysis.AssetReturns
)

// RunMonteCarloSimulation runs enhanced randomized scenario analysis.
func (c *Calculator) RunMonteCarloSimulation(runs int) *models.MonteCarloAnalysis {
	seed := int64(0)
	if c.mcSeedSet {
		seed = c.mcSeed
	}
	return analysis.MonteCarlo(engine.New(), c.input(), runs, seed)
}

func (c *Calculator) runSingleMonteCarloSimulation(rng *rand.Rand, config *MonteCarloConfig) models.MonteCarloResult {
	return analysis.RunSingleMonteCarloSimulation(c.input(), rng, config)
}

func (c *Calculator) generateYearlyReturns(rng *rand.Rand, config *MonteCarloConfig, years int, timing *CrashTiming, lastCrashYear *int) []float64 {
	return analysis.GenerateYearlyReturns(c.Settings, rng, config, years, timing, lastCrashYear)
}

func (c *Calculator) calculateSequenceRiskImpact(results []models.MonteCarloResult) float64 {
	return analysis.CalculateSequenceRiskImpact(results)
}

func (c *Calculator) createDistributionBuckets(sortedBalances []float64) *models.MonteCarloDistribution {
	return analysis.CreateDistributionBuckets(sortedBalances)
}

func (c *Calculator) buildProjectionExplainability(projection *models.ProjectionResult) *models.ProjectionExplainability {
	return analysis.BuildExplainability(projection, c.input())
}

// RunSSAnalysis computes the full SS claiming-age comparison.
func (c *Calculator) RunSSAnalysis() *models.SSComparisonAnalysis {
	return analysis.SSAnalysis(c.input())
}

// RunSSPortfolioAnalysis evaluates how eligible claiming ages affect portfolio survival.
func (c *Calculator) RunSSPortfolioAnalysis(ss *models.SSComparisonAnalysis) *models.SSPortfolioAnalysis {
	seed := int64(0)
	if c.mcSeedSet {
		seed = c.mcSeed
	}
	return analysis.SSPortfolioWithSeed(engine.New(), c.input(), ss, seed)
}

// RunHistoricalBacktest runs the projection against all available historical sequences.
func (c *Calculator) RunHistoricalBacktest() *models.HistoricalBacktestAnalysis {
	return analysis.HistoricalBacktest(c.input(), history.DefaultData())
}

func (c *Calculator) runSingleHistoricalSequence(startYear int) HistoricalSequenceResult {
	return analysis.RunSingleHistoricalSequence(c.input(), history.DefaultData(), startYear)
}

// HistoricalSequenceResult is re-exported from analysis so existing tests keep compiling.
type HistoricalSequenceResult = analysis.HistoricalSequenceResult

// yearsUntilDepletion forwards to analysis.YearsUntilDepletion.
var yearsUntilDepletion = analysis.YearsUntilDepletion

// BuildRMDAnalysis is a thin delegator over analysis.BuildRMD.
func (c *Calculator) BuildRMDAnalysis(projection *models.ProjectionResult) *models.RMDAnalysis {
	return analysis.BuildRMD(projection, c.input())
}

// RunFullAnalysis performs complete what-if analysis. Test-only delegator
// over RunFull, threading the parity-window MC seed override (if set).
func (c *Calculator) RunFullAnalysis() *models.WhatIfAnalysis {
	if c.mcSeedSet {
		return runFullWithSeed(engine.New(), c.input(), c.mcSeed)
	}
	return RunFull(engine.New(), c.input())
}
