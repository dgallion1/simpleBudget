package analysis

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
)

// MonteCarloConfig defines parameters for enhanced simulation. Mirrors
// the parity-window type previously hosted on Calculator.
type MonteCarloConfig struct {
	// Market dynamics
	ReturnVolatility float64 // Annual return standard deviation (e.g., 15 for 15%)
	CrashProbability float64 // Annual probability of a crash (e.g., 0.05 for 5%)
	CrashSeverity    float64 // How bad crashes are (e.g., -30 for -30% return)
	RecoveryBoost    float64 // Extra return after crash years (mean reversion)

	// Spending shocks
	SpendingShockProb float64 // Annual probability of spending shock
	SpendingShockMin  float64 // Minimum shock amount ($)
	SpendingShockMax  float64 // Maximum shock amount ($)

	// Healthcare emergencies
	HealthShockProb float64 // Annual probability of health emergency
	HealthShockMin  float64 // Minimum health shock ($)
	HealthShockMax  float64 // Maximum health shock ($)

	// Longevity
	LongevityVariation int // Years +/- to vary projection length

	// Adaptive spending (reducing discretionary expenses during crashes)
	AdaptiveSpending        bool    // Enable adaptive spending during crashes
	DiscretionaryCutPercent float64 // % to cut discretionary spending during crash (e.g., 40 for 40%)
	AdaptationRecoveryYears int     // Years to maintain reduced spending after crash
}

// DefaultMonteCarloConfig returns realistic simulation parameters.
func DefaultMonteCarloConfig() *MonteCarloConfig {
	return &MonteCarloConfig{
		// Market: ~15% annual volatility, 5% crash chance, crashes are -30% on average
		ReturnVolatility: 15.0,
		CrashProbability: 0.05,
		CrashSeverity:    -30.0,
		RecoveryBoost:    5.0,

		// Spending: 8% chance of $5K-$25K emergency per year
		SpendingShockProb: 0.08,
		SpendingShockMin:  5000,
		SpendingShockMax:  25000,

		// Health: 5% chance of $10K-$50K health event per year
		HealthShockProb: 0.05,
		HealthShockMin:  10000,
		HealthShockMax:  50000,

		// Longevity: +/- 5 years from base projection
		LongevityVariation: 5,
	}
}

// CrashTiming tracks when crashes occurred during simulation.
type CrashTiming struct {
	TotalCrashes   int
	EarlyCrashes   int // Years 1-5 (index 0-4)
	MidCrashes     int // Years 6-15 (index 5-14)
	LateCrashes    int // Years 16+ (index 15+)
	FirstCrashYear int // 0 means no crashes (1-indexed for display)
}

// AssetReturns holds per-asset-class returns for Monte Carlo simulation.
type AssetReturns struct {
	Stock []float64
	Bond  []float64
	Cash  []float64
}

// MonteCarlo runs enhanced randomized scenario analysis. seed == 0 means
// auto-seed from time.Now(); any non-zero seed is used directly so
// deterministic parity tests get reproducible RNG sequences.
func MonteCarlo(eng *engine.Engine, in engine.Input, runs int, seed int64) *models.MonteCarloAnalysis {
	if runs <= 0 {
		runs = 1000
	}

	s := in.Prepared.Settings()
	config := DefaultMonteCarloConfig()
	results := make([]models.MonteCarloResult, runs)
	successCount := 0
	totalDepletionYears := 0.0
	depletionCount := 0

	// Track aggregate shock statistics
	totalCrashes := 0
	totalSpendingShocks := 0
	totalHealthShocks := 0
	runsWithCrashes := 0
	runsWithSpendingShocks := 0
	runsWithHealthShocks := 0

	// Create a new random source. seed == 0 means auto-seed (preserve
	// the legacy "default = unpredictable" contract); any non-zero seed
	// is used directly so parity tests get a reproducible RNG sequence.
	var rng *rand.Rand
	if seed != 0 {
		rng = rand.New(rand.NewSource(seed))
	} else {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	for i := 0; i < runs; i++ {
		result := runSingleMonteCarloSimulation(in, rng, config)
		results[i] = result

		// Aggregate statistics
		if result.Survives {
			successCount++
		}
		if result.DepletionYear > 0 {
			totalDepletionYears += result.DepletionYear
			depletionCount++
		}

		totalCrashes += result.MarketCrashes
		totalSpendingShocks += result.SpendingShocks
		totalHealthShocks += result.HealthShocks

		if result.MarketCrashes > 0 {
			runsWithCrashes++
		}
		if result.SpendingShocks > 0 {
			runsWithSpendingShocks++
		}
		if result.HealthShocks > 0 {
			runsWithHealthShocks++
		}
	}

	// Calculate statistics
	balances := make([]float64, runs)
	for i, r := range results {
		balances[i] = r.FinalBalance
	}
	sortFloat64s(balances)

	stats := &models.MonteCarloStats{
		Runs:          runs,
		SuccessRate:   float64(successCount) / float64(runs) * 100,
		MedianBalance: balances[runs/2],
		MeanBalance:   mean(balances),
		Percentile10:  balances[runs/10],
		Percentile25:  balances[runs/4],
		Percentile75:  balances[runs*3/4],
		Percentile90:  balances[runs*9/10],
		WorstCase:     balances[0],
		BestCase:      balances[runs-1],

		// Enhanced stats
		MarketCrashCount:   runsWithCrashes,
		SpendingShockCount: runsWithSpendingShocks,
		HealthShockCount:   runsWithHealthShocks,
		AvgCrashesPerRun:   float64(totalCrashes) / float64(runs),
		AvgShocksPerRun:    float64(totalSpendingShocks+totalHealthShocks) / float64(runs),
	}

	if depletionCount > 0 {
		stats.AvgDepletionYr = totalDepletionYears / float64(depletionCount)
	}

	// Calculate sequence risk impact by comparing early vs late crash outcomes
	stats.SequenceRiskImpact = calculateSequenceRiskImpact(results)

	// Calculate detailed sequence risk breakdown with expense context
	// Use TotalExpenses to include all expense sources
	annualExpenses := engine.TotalExpenses(s, 0) * 12
	stats.SequenceRisk = calculateSequenceRiskBreakdown(s, results, annualExpenses, s.PortfolioValue)

	// Run adaptive spending simulations if discretionary expenses exist
	if stats.SequenceRisk != nil && stats.SequenceRisk.HasDiscretionary {
		adaptiveConfig := *config
		adaptiveConfig.AdaptiveSpending = true
		adaptiveConfig.DiscretionaryCutPercent = 40 // Cut 40% of discretionary during crashes
		adaptiveConfig.AdaptationRecoveryYears = 3  // Maintain reduced spending for 3 years after crash

		// Run adaptive simulations (smaller sample for performance)
		adaptiveRuns := runs / 2
		adaptiveResults := make([]models.MonteCarloResult, adaptiveRuns)
		adaptiveRng := rand.New(rand.NewSource(42)) // Fixed seed for reproducibility

		for i := 0; i < adaptiveRuns; i++ {
			adaptiveResults[i] = runSingleMonteCarloSimulation(in, adaptiveRng, &adaptiveConfig)
		}

		// Calculate adapted early crash survival rate
		var adaptedEarlySurvived, adaptedEarlyTotal int
		for _, r := range adaptiveResults {
			if r.EarlyCrashes > 0 {
				adaptedEarlyTotal++
				if r.Survives {
					adaptedEarlySurvived++
				}
			}
		}

		if adaptedEarlyTotal > 0 {
			adaptedSurvival := float64(adaptedEarlySurvived) / float64(adaptedEarlyTotal) * 100
			stats.SequenceRisk.EarlyCrashSurvivalAdapted = adaptedSurvival
			stats.SequenceRisk.AdaptationBoost = adaptedSurvival - stats.SequenceRisk.EarlyCrashSurvival
			stats.SequenceRisk.DiscretionaryCutPercent = adaptiveConfig.DiscretionaryCutPercent

			// Generate rationale based on improvement
			if stats.SequenceRisk.AdaptationBoost >= 15 {
				stats.SequenceRisk.AdaptationRationale = "Significant protection: cutting discretionary spending during crashes substantially improves survival"
			} else if stats.SequenceRisk.AdaptationBoost >= 5 {
				stats.SequenceRisk.AdaptationRationale = "Moderate benefit: reducing discretionary expenses during downturns provides meaningful protection"
			} else if stats.SequenceRisk.AdaptationBoost > 0 {
				stats.SequenceRisk.AdaptationRationale = "Slight improvement: spending flexibility provides some buffer against early crashes"
			} else {
				stats.SequenceRisk.AdaptationRationale = "Limited impact: your plan is resilient even without spending cuts"
			}
		}
	}

	// Create distribution buckets
	distribution := createDistributionBuckets(balances)

	return &models.MonteCarloAnalysis{
		Stats:        stats,
		Distribution: distribution,
	}
}

// RunSingleMonteCarloSimulation drives a single MC run. Exposed so
// retirement-package test helpers can exercise the simulator directly
// without going through the full MonteCarlo fan-out.
func RunSingleMonteCarloSimulation(in engine.Input, rng *rand.Rand, config *MonteCarloConfig) models.MonteCarloResult {
	return runSingleMonteCarloSimulation(in, rng, config)
}

// CalculateSequenceRiskImpact exposes the sequence-of-returns risk
// calculation so retirement-package test helpers can call it directly.
func CalculateSequenceRiskImpact(results []models.MonteCarloResult) float64 {
	return calculateSequenceRiskImpact(results)
}

// CreateDistributionBuckets exposes the distribution-bucket builder so
// retirement-package test helpers can call it directly.
func CreateDistributionBuckets(sortedBalances []float64) *models.MonteCarloDistribution {
	return createDistributionBuckets(sortedBalances)
}

// GenerateYearlyReturns blends the per-asset returns by the settings'
// effective allocation. Exposed so retirement-package test helpers can
// drive the generator directly.
func GenerateYearlyReturns(s *models.WhatIfSettings, rng *rand.Rand, config *MonteCarloConfig, years int, timing *CrashTiming, lastCrashYear *int) []float64 {
	assetReturns := generateAssetReturns(rng, config, years, timing, lastCrashYear)

	// Get user's asset allocation with defaults applied
	stockPercent, bondPercent, cashPercent := s.GetEffectiveAssetAllocation()

	// Blend returns based on user's allocation
	returns := make([]float64, years)
	for y := 0; y < years; y++ {
		returns[y] = (stockPercent/100)*assetReturns.Stock[y] +
			(bondPercent/100)*assetReturns.Bond[y] +
			(cashPercent/100)*assetReturns.Cash[y]
	}
	return returns
}

// runSingleMonteCarloSimulation runs one complete simulation with all risk factors.
func runSingleMonteCarloSimulation(in engine.Input, rng *rand.Rand, config *MonteCarloConfig) models.MonteCarloResult {
	primarySettings := in.Prepared.Settings()
	activeSettings := primarySettings
	chain := in.Chain
	nextChainIdx := 0
	s := activeSettings

	// Vary projection length for longevity risk
	projectionYears := s.ProjectionYears
	if config.LongevityVariation > 0 {
		variation := rng.Intn(config.LongevityVariation*2+1) - config.LongevityVariation
		projectionYears = max(10, s.ProjectionYears+variation)
	}
	months := projectionYears * 12

	// Initialize balances (3-bucket model)
	taxDeferredBalance := s.PortfolioValue * (s.TaxDeferredPercent / 100)
	rothBalance := s.PortfolioValue * (s.RothPercent / 100)
	taxableAccount := engine.NewTaxableAccountState(s, s.PortfolioValue-taxDeferredBalance-rothBalance)

	// Roth basis and 5-year clock state, persisted across months within the run.
	rothBasisLocal := s.PortfolioValue * (s.RothPercent / 100)
	rothFirstFundedYearLocal := s.RothFirstFundedYear
	if rothFirstFundedYearLocal == 0 && s.RothPercent > 0 {
		rothFirstFundedYearLocal = engine.ParseStartYear(s.StartDate)
	}
	// bigTicketRothEarnings carries RothEarningsWithdrawal from the annual big-ticket
	// pass into the monthly PortfolioMonthInput. Reset each year.
	bigTicketRothEarnings := 0.0

	var depletionYear float64
	depleted := false

	currentLivingExpenses := engine.LivingExpensesAtMonth(s, 0)

	// Spending guardrails for this MC run
	var mcGrState *engine.GuardrailState
	if s.Guardrails != nil && s.Guardrails.Enabled {
		mcGrState = engine.NewGuardrailState(s.PortfolioValue)
	}

	// Track shocks for this run
	crashTiming := &CrashTiming{}
	spendingShocks := 0
	healthShocks := 0
	lastCrashYear := -999 // Track for recovery boost

	// Annual RMD tracking (F-074: annualRMD persists across months so the
	// trigger-month logic can apply the full year's RMD in a single month).
	var annualRMD float64
	var monthlyRMD float64
	var taxState engine.ProjectionTaxAccumulator
	taxCalculator := engine.NewTaxCalculator(s.TaxConfig, s.InflationRate)
	completedMAGIHistory := make([]float64, 0, projectionYears)
	currentYearTaxSnapshot := engine.ProjectedTaxSnapshot{}

	// Healthcare cost variation multiplier (updated annually)
	healthcareVariation := 1.0
	inflationVar := 1.0

	// Track cumulative inflation for spending phase calculations
	cumulativeInflation := 1.0
	// netCumulativeInflation tracks (InflationRate−SpendingDeclineRate) compounding,
	// mirroring the per-month no-phase expense accumulation. Used by
	// rebaseLivingExpensesAtTransition to avoid the F-065 step-up error.
	netCumulativeInflation := 1.0

	// Adaptive spending: track when we're in reduced-spending mode
	adaptationEndYear := -1 // Year when adaptation ends (-1 = not adapting)

	// Generate year-by-year returns upfront for sequence of returns (per asset class)
	assetReturns := generateAssetReturns(rng, config, projectionYears, crashTiming, &lastCrashYear)

	// Get per-account allocations for blending returns
	tdStock, tdBond, tdCash, rothStock, rothBond, rothCash, taxStock, taxBond, taxCash := s.GetAllocationAtYear(0)

	for m := 0; m < months; m++ {
		if depleted {
			break
		}

		currentYear := m / 12
		phaseAge := s.GetPhaseReferenceAge(currentYear) // Age used for spending phase calculations (may differ for couples)
		bigTicketExpenseThisMonth := 0.0
		rothConversionThisMonth := 0.0
		allowTaxDeferredWithdrawal := !engine.TaxDeferredDelayActive(s, currentYear)
		penaltyRate := engine.EarlyWithdrawalPenaltyRate(s.CurrentAge, currentYear)

		// Annual adjustments at year boundaries
		if m%12 == 0 {
			if m > 0 {
				completedMAGIHistory = append(completedMAGIHistory, currentYearTaxSnapshot.AnnualMAGI)
			}
			taxState = engine.ProjectionTaxAccumulator{}
			// Check for chain transition
			if len(chain) > 0 {
				newIdx, prepared := in.Hooks.ResolveChain(currentYear, nextChainIdx, primarySettings, chain)
				if prepared != nil {
					activeSettings = prepared
					s = activeSettings
					nextChainIdx = newIdx

					currentLivingExpenses = engine.RebaseLivingExpensesAtTransition(s, phaseAge, cumulativeInflation, netCumulativeInflation)
					taxableAccount.SyncAssumptions(s)
				}
			}
			// Refresh allocation for glide path and chain transitions
			tdStock, tdBond, tdCash, rothStock, rothBond, rothCash, taxStock, taxBond, taxCash = s.GetAllocationAtYear(currentYear)
			taxCalculator = engine.NewTaxCalculator(s.TaxConfig, s.InflationRate)
			taxableAccount.RealizedGainsYTD = 0

			// Apply inflation with some random variation
			inflationVar = 1 + (rng.Float64()-0.5)*0.02 // +/- 1%

			// Healthcare cost variation (healthcare is more volatile, +/- 2%)
			healthcareVariation = 1 + (rng.Float64()-0.5)*0.04

			// F-074/F-078: annualRMD computed once per year, applied only
			// in the trigger month inside the month loop. Calendar-year
			// gate + age-at-year-end divisor so MC matches the
			// deterministic projection for late-year births.
			annualRMD = engine.AnnualRMDForYear(s, currentYear, taxDeferredBalance)
			monthlyRMD = 0

			rothConversionThisMonth = engine.ApplyRothConversionAtYear(s, currentYear, &taxDeferredBalance, &rothBalance, &rothBasisLocal, &rothFirstFundedYearLocal)

			bigTicketResult := engine.ApplyBigTicketItemsForYear(s, currentYear, allowTaxDeferredWithdrawal, penaltyRate, &taxDeferredBalance, &taxableAccount, &rothBalance, &rothBasisLocal)
			bigTicketExpenseThisMonth += bigTicketResult.UnfundedExpense
			bigTicketRothEarnings = bigTicketResult.RothEarningsWithdrawal
		}

		if m > 0 {
			cumulativeInflation *= engine.MonthlyCompoundFactorFromDecimal(s.InflationRate / 100 * inflationVar)
			netCumulativeInflation *= engine.MonthlyCompoundFactorFromDecimal((s.InflationRate - s.SpendingDeclineRate) / 100 * inflationVar)
			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
				currentLivingExpenses = s.MonthlyLivingExpenses * s.GetSpendingMultiplier(phaseAge) * cumulativeInflation
			} else {
				currentLivingExpenses *= engine.MonthlyCompoundFactorFromDecimal((s.InflationRate - s.SpendingDeclineRate) / 100 * inflationVar)
			}
		}

		// Evaluate guardrails at year boundaries in MC
		if mcGrState != nil && m%12 == 0 {
			totalPortfolio := taxDeferredBalance + taxableAccount.MarketValue + rothBalance
			mcGrState.Evaluate(s.Guardrails, totalPortfolio)
		}

		// Apply guardrail spending multiplier in MC
		mcAdjustedLiving := currentLivingExpenses
		if mcGrState != nil {
			mcAdjustedLiving *= mcGrState.Multiplier()
		}

		// Calculate healthcare expenses using multi-person model with variation
		activeHealthcare := s.GetTotalHealthcareCost(m) * healthcareVariation
		propertyTax := engine.PropertyTaxAtMonth(s, m)
		totalExpenses := mcAdjustedLiving + activeHealthcare + propertyTax + bigTicketExpenseThisMonth

		// Check if we should enter adaptation mode (crash detected this year via stock returns)
		// Skip adaptive spending when guardrails are active (guardrails subsume this)
		if mcGrState == nil && config.AdaptiveSpending && assetReturns.Stock[currentYear] < -15 {
			adaptationEndYear = currentYear + config.AdaptationRecoveryYears
		}
		inAdaptationMode := mcGrState == nil && config.AdaptiveSpending && currentYear <= adaptationEndYear

		// Add expense sources (with phase multiplier and adaptive spending reduction if applicable)
		for _, source := range s.ExpenseSources {
			expenseAmount := source.GetAdjustedAmount(m, s.InflationRate)
			// Apply phase multiplier to discretionary expenses
			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled && source.Discretionary {
				expenseAmount *= s.GetSpendingMultiplier(phaseAge)
			}
			// Reduce discretionary expenses during adaptation
			if inAdaptationMode && source.Discretionary {
				expenseAmount *= (1 - config.DiscretionaryCutPercent/100)
			}
			totalExpenses += expenseAmount
		}

		// Apply spending shock (checked monthly, but represents annual probability)
		if m%12 == 0 && rng.Float64() < config.SpendingShockProb {
			shockAmount := config.SpendingShockMin + rng.Float64()*(config.SpendingShockMax-config.SpendingShockMin)
			totalExpenses += shockAmount / 12 // Spread over the year
			spendingShocks++
		}

		// Apply health shock (separate from regular healthcare)
		if m%12 == 0 && rng.Float64() < config.HealthShockProb {
			healthShockAmount := config.HealthShockMin + rng.Float64()*(config.HealthShockMax-config.HealthShockMin)
			totalExpenses += healthShockAmount / 12 // Spread over the year
			healthShocks++
		}

		incomeBreakdown := engine.CalculateMonthlyIncomeBreakdown(in.Hooks, s, m)

		// Apply this year's investment returns (per-account based on allocation)
		stockReturn := assetReturns.Stock[currentYear]
		bondReturn := assetReturns.Bond[currentYear]
		cashReturn := assetReturns.Cash[currentYear]

		// Calculate per-account blended returns
		tdReturn := models.GetBlendedReturn(tdStock, tdBond, tdCash, stockReturn, bondReturn, cashReturn)
		rothReturnRate := models.GetBlendedReturn(rothStock, rothBond, rothCash, stockReturn, bondReturn, cashReturn)
		taxReturn := models.GetBlendedReturn(taxStock, taxBond, taxCash, stockReturn, bondReturn, cashReturn)

		// Convert annual returns to monthly using geometric formula (not simple division)
		tdMonthly := math.Pow(1+tdReturn/100, 1.0/12) - 1
		rothMonthlyRate := math.Pow(1+rothReturnRate/100, 1.0/12) - 1
		taxableComponents := engine.BuildTaxableReturnComponents(taxReturn, s)
		irmaaEligibleAdults := engine.MedicareEligibleAdultCountAtYear(s, currentYear)
		irmaaInflationFactor := engine.PlannerIRMAAInflationFactorForYear(s.InflationRate, float64(engine.YearsFromTaxBase(s, currentYear)))

		// F-074: apply the full annual RMD only in the trigger month.
		monthlyRMD = engine.MonthlyRMDForMonth(s, m%12, annualRMD, taxDeferredBalance)

		monthResult := engine.ExecuteTaxAwarePortfolioMonth(engine.PortfolioMonthInput{
			TotalExpenses:                     totalExpenses,
			IncomeBreakdown:                   incomeBreakdown,
			MonthlyRMD:                        monthlyRMD,
			AllowTaxDeferredWithdrawal:        allowTaxDeferredWithdrawal,
			PenaltyRate:                       penaltyRate,
			TaxDeferredBalance:                &taxDeferredBalance,
			TaxableAccount:                    &taxableAccount,
			RothBalance:                       &rothBalance,
			RothBasis:                         &rothBasisLocal,
			RothFirstFundedYear:               rothFirstFundedYearLocal,
			TaxDeferredMonthlyReturn:          tdMonthly,
			RothMonthlyReturn:                 rothMonthlyRate,
			TaxableComponents:                 taxableComponents,
			Timing:                            s.GetProjectionTiming(),
			TaxState:                          taxState,
			TaxCalculator:                     taxCalculator,
			MonthInYear:                       m % 12,
			CalendarYear:                      engine.ParseStartYear(s.StartDate) + currentYear,
			RothConversionThisMonth:           rothConversionThisMonth,
			TaxableRothEarningsBeforeCashFlow: bigTicketRothEarnings,
			CompletedMAGIHistory:              completedMAGIHistory,
			IRMAAEligibleAdults:               irmaaEligibleAdults,
			IRMAAInflationFactor:              irmaaInflationFactor,
		})
		bigTicketRothEarnings = 0
		currentYearTaxSnapshot = monthResult.TaxSnapshot
		engine.ApplyTaxStateMonth(&taxState, incomeBreakdown, monthResult, rothConversionThisMonth)
		shortfall := monthResult.Shortfall

		// Check for depletion
		totalBalance := taxDeferredBalance + rothBalance + taxableAccount.MarketValue
		if engine.ShortfallCausesDepletion(shortfall, allowTaxDeferredWithdrawal, taxDeferredBalance) {
			depleted = true
			depletionYear = float64(m) / 12
		}
		if totalBalance <= 0 {
			depleted = true
			depletionYear = float64(m) / 12
		}
	}

	finalBalance := taxDeferredBalance + rothBalance + taxableAccount.MarketValue
	if finalBalance < 0 {
		finalBalance = 0
	}

	return models.MonteCarloResult{
		FinalBalance:    finalBalance,
		DepletionYear:   depletionYear,
		Survives:        !depleted,
		MarketCrashes:   crashTiming.TotalCrashes,
		SpendingShocks:  spendingShocks,
		HealthShocks:    healthShocks,
		ProjectionYears: projectionYears,
		EarlyCrashes:    crashTiming.EarlyCrashes,
		MidCrashes:      crashTiming.MidCrashes,
		LateCrashes:     crashTiming.LateCrashes,
		FirstCrashYear:  crashTiming.FirstCrashYear,
	}
}

// generateAssetReturns creates sequences of annual returns per asset class with crashes and volatility.
func generateAssetReturns(rng *rand.Rand, config *MonteCarloConfig, years int, timing *CrashTiming, lastCrashYear *int) *AssetReturns {
	stockReturns := make([]float64, years)
	bondReturns := make([]float64, years)
	cashReturns := make([]float64, years)

	// Get historical statistics for asset classes (canonical dataset
	// lives in the history package — pure leaf, safe to import from
	// analysis).
	stockMean, bondMean, cashMean, _, stockStdDev, bondStdDev := history.Stats(history.DefaultData())

	for y := 0; y < years; y++ {
		var stockReturn, bondReturn, cashReturn float64

		// Check for market crash (affects stocks primarily)
		if rng.Float64() < config.CrashProbability {
			// Crash year: severe negative stock return, bonds often rally (flight to safety)
			stockReturn = config.CrashSeverity + (rng.Float64()-0.5)*10 // -35% to -25%
			bondReturn = 10 + rng.NormFloat64()*5                       // Bonds typically rally during crashes
			cashReturn = cashMean + rng.Float64()*1                     // Cash stable

			timing.TotalCrashes++
			*lastCrashYear = y

			// Track first crash year (1-indexed for human readability)
			if timing.FirstCrashYear == 0 {
				timing.FirstCrashYear = y + 1
			}

			// Categorize by timing
			if y < 5 {
				timing.EarlyCrashes++
			} else if y < 15 {
				timing.MidCrashes++
			} else {
				timing.LateCrashes++
			}
		} else if y == *lastCrashYear+1 {
			// Recovery year after crash: strong stock recovery, bonds normalize
			stockReturn = stockMean + config.RecoveryBoost + rng.NormFloat64()*stockStdDev*0.8
			bondReturn = bondMean + rng.NormFloat64()*bondStdDev
			cashReturn = cashMean + rng.Float64()*0.5
		} else {
			// Normal year: returns with volatility (normal distribution)
			stockReturn = stockMean + rng.NormFloat64()*stockStdDev
			bondReturn = bondMean + rng.NormFloat64()*bondStdDev
			cashReturn = cashMean + rng.Float64()*1 - 0.5 // Small variation
		}

		// Clamp individual returns to reasonable bounds
		stockReturns[y] = math.Max(-50, math.Min(60, stockReturn))
		bondReturns[y] = math.Max(-20, math.Min(40, bondReturn))
		cashReturns[y] = math.Max(0, math.Min(15, cashReturn))
	}

	return &AssetReturns{
		Stock: stockReturns,
		Bond:  bondReturns,
		Cash:  cashReturns,
	}
}

// calculateSequenceRiskImpact measures how sequence of returns affected outcomes.
func calculateSequenceRiskImpact(results []models.MonteCarloResult) float64 {
	// Compare success rates of runs with crashes vs without
	// A value > 0 means crashes hurt outcomes (expected)
	if len(results) < 100 {
		return 0
	}

	crashRunsSucceeded := 0
	crashRunsTotal := 0
	noCrashRunsSucceeded := 0
	noCrashRunsTotal := 0

	for _, r := range results {
		if r.MarketCrashes > 0 {
			crashRunsTotal++
			if r.Survives {
				crashRunsSucceeded++
			}
		} else {
			noCrashRunsTotal++
			if r.Survives {
				noCrashRunsSucceeded++
			}
		}
	}

	if crashRunsTotal == 0 || noCrashRunsTotal == 0 {
		return 0
	}

	// Return the difference in survival rates
	survivalWithCrashes := float64(crashRunsSucceeded) / float64(crashRunsTotal) * 100
	survivalWithoutCrashes := float64(noCrashRunsSucceeded) / float64(noCrashRunsTotal) * 100

	return survivalWithoutCrashes - survivalWithCrashes
}

// calculateSequenceRiskBreakdown provides detailed analysis of crash timing impact.
func calculateSequenceRiskBreakdown(s *models.WhatIfSettings, results []models.MonteCarloResult, annualExpenses float64, portfolioValue float64) *models.SequenceRiskBreakdown {
	if len(results) < 100 {
		return nil
	}

	// Track survival by crash timing category
	var noCrashSurvived, noCrashTotal int
	var earlyCrashSurvived, earlyCrashTotal int
	var midCrashSurvived, midCrashTotal int
	var lateCrashSurvived, lateCrashTotal int

	// For recovery analysis
	var earlyRecoveries int
	var totalFirstCrashYears float64
	var firstCrashCount int

	for _, r := range results {
		// Categorize by where crashes occurred
		hasEarlyCrash := r.EarlyCrashes > 0
		hasMidCrash := r.MidCrashes > 0
		hasLateCrash := r.LateCrashes > 0
		hasAnyCrash := r.MarketCrashes > 0

		if !hasAnyCrash {
			noCrashTotal++
			if r.Survives {
				noCrashSurvived++
			}
		} else {
			// Track first crash timing for recovery analysis
			if r.FirstCrashYear > 0 {
				totalFirstCrashYears += float64(r.FirstCrashYear)
				firstCrashCount++
			}

			// Categorize by earliest crash (most impactful)
			if hasEarlyCrash {
				earlyCrashTotal++
				if r.Survives {
					earlyCrashSurvived++
					earlyRecoveries++
				}
			} else if hasMidCrash {
				midCrashTotal++
				if r.Survives {
					midCrashSurvived++
				}
			} else if hasLateCrash {
				lateCrashTotal++
				if r.Survives {
					lateCrashSurvived++
				}
			}
		}
	}

	// Calculate survival rates (as percentages)
	safeDiv := func(num, denom int) float64 {
		if denom == 0 {
			return 0
		}
		return float64(num) / float64(denom) * 100
	}

	noCrashSurvival := safeDiv(noCrashSurvived, noCrashTotal)
	earlyCrashSurvival := safeDiv(earlyCrashSurvived, earlyCrashTotal)
	midCrashSurvival := safeDiv(midCrashSurvived, midCrashTotal)
	lateCrashSurvival := safeDiv(lateCrashSurvived, lateCrashTotal)

	// Calculate impact metrics
	earlyVsLateImpact := lateCrashSurvival - earlyCrashSurvival
	earlyVsNoneImpact := noCrashSurvival - earlyCrashSurvival

	// Recovery analysis
	recoveryRate := safeDiv(earlyRecoveries, earlyCrashTotal)
	avgRecoveryYears := 0.0
	if firstCrashCount > 0 {
		avgRecoveryYears = totalFirstCrashYears / float64(firstCrashCount)
	}

	// Buffer recommendation based on impact
	recommendedBuffer := 2 // Default minimum
	rationale := "Standard 2-year buffer for moderate sequence risk"

	if earlyVsNoneImpact > 30 {
		recommendedBuffer = 5
		rationale = "High sequence risk: 5-year buffer to weather early crashes"
	} else if earlyVsNoneImpact > 20 {
		recommendedBuffer = 4
		rationale = "Significant sequence risk: 4-year buffer recommended"
	} else if earlyVsNoneImpact > 10 {
		recommendedBuffer = 3
		rationale = "Moderate sequence risk: 3-year buffer provides good protection"
	} else if earlyVsNoneImpact <= 5 {
		recommendedBuffer = 2
		rationale = "Low sequence risk: 2-year buffer is sufficient"
	}

	// Calculate buffer amount accounting for partial portfolio value during crash
	// Key insight: Even during a 30% crash, portfolio still has 70% of its value
	// You can still safely withdraw from the reduced portfolio (at a conservative rate)
	// The buffer only needs to cover the SHORTFALL, not full expenses
	crashDrawdownPercent := 30.0                                        // Expected crash severity (from DefaultMonteCarloConfig)
	crashedPortfolio := portfolioValue * (1 - crashDrawdownPercent/100) // Portfolio after crash
	safeWithdrawalRate := 0.03                                          // Conservative 3% during crash years
	safeWithdrawalDuringCrash := crashedPortfolio * safeWithdrawalRate  // Annual safe withdrawal from crashed portfolio
	annualShortfall := annualExpenses - safeWithdrawalDuringCrash       // Gap that buffer must cover
	if annualShortfall < 0 {
		annualShortfall = 0 // No shortfall if safe withdrawal covers expenses
	}

	// Calculate both naive and improved buffer amounts
	naiveBufferAmount := float64(recommendedBuffer) * annualExpenses
	bufferAmount := float64(recommendedBuffer) * annualShortfall

	// Update rationale to explain the improved calculation
	if annualShortfall > 0 && annualShortfall < annualExpenses {
		savingsPercent := (1 - annualShortfall/annualExpenses) * 100
		rationale = fmt.Sprintf("%s (%.0f%% less than naive calculation because crashed portfolio still provides $%.0f/yr)",
			rationale, savingsPercent, safeWithdrawalDuringCrash)
	}

	// Calculate adjusted monthly spending if buffer is set aside from portfolio
	// Uses a 4% safe withdrawal rate on the remaining portfolio after buffer
	adjustedSpending := 0.0
	remainingPortfolio := portfolioValue - bufferAmount
	if remainingPortfolio > 0 {
		adjustedSpending = (remainingPortfolio * 0.04) / 12 // 4% annual withdrawal rate, monthly
	}

	// Calculate expense breakdown for adaptive spending analysis
	expenseBreakdown := engine.CalculateExpenseBreakdown(s, 0)
	hasDiscretionary := expenseBreakdown.Discretionary > 0

	return &models.SequenceRiskBreakdown{
		NoCrashSurvival:    noCrashSurvival,
		EarlyCrashSurvival: earlyCrashSurvival,
		MidCrashSurvival:   midCrashSurvival,
		LateCrashSurvival:  lateCrashSurvival,

		NoCrashCount:    noCrashTotal,
		EarlyCrashCount: earlyCrashTotal,
		MidCrashCount:   midCrashTotal,
		LateCrashCount:  lateCrashTotal,

		EarlyVsLateImpact: earlyVsLateImpact,
		EarlyVsNoneImpact: earlyVsNoneImpact,

		RecoveryRate:     recoveryRate,
		AvgRecoveryYears: avgRecoveryYears,

		RecommendedBuffer: recommendedBuffer,
		BufferRationale:   rationale,
		BufferAmount:      bufferAmount,
		AnnualExpenses:    annualExpenses,
		AdjustedSpending:  adjustedSpending,

		// Buffer calculation breakdown
		CrashDrawdownPercent:      crashDrawdownPercent,
		CrashedPortfolioValue:     crashedPortfolio,
		SafeWithdrawalDuringCrash: safeWithdrawalDuringCrash,
		AnnualShortfall:           annualShortfall,
		NaiveBufferAmount:         naiveBufferAmount,

		// Adaptive spending fields
		HasDiscretionary:     hasDiscretionary,
		MonthlyDiscretionary: expenseBreakdown.Discretionary,
		MonthlyEssential:     expenseBreakdown.Essential,
	}
}

// createDistributionBuckets creates histogram buckets for visualization.
func createDistributionBuckets(sortedBalances []float64) *models.MonteCarloDistribution {
	buckets := make([]models.MonteCarloDistBucket, 0)
	total := len(sortedBalances)

	// Define bucket boundaries based on data range
	maxVal := sortedBalances[total-1]

	// Use fixed boundaries with more detail in 0-3M range
	var boundaries []float64
	if maxVal <= 0 {
		boundaries = []float64{0}
	} else if maxVal < 100000 {
		boundaries = []float64{0, 10000, 25000, 50000, 75000, 100000}
	} else if maxVal < 1000000 {
		boundaries = []float64{0, 100000, 250000, 500000, 750000, 1000000}
	} else if maxVal < 3000000 {
		// Fine detail for 0-3M range
		boundaries = []float64{0, 250000, 500000, 1000000, 1500000, 2000000, 2500000, 3000000}
	} else {
		// Fixed boundaries with detail in 0-3M, then larger buckets for higher values
		boundaries = []float64{0, 250000, 500000, 1000000, 2000000, 3000000, 5000000, 10000000}
		// Add boundaries beyond 10M if needed
		if maxVal > 10000000 {
			boundaries = append(boundaries, 20000000)
		}
	}

	// Count items in each bucket
	for i := 0; i < len(boundaries)-1; i++ {
		low := boundaries[i]
		high := boundaries[i+1]
		count := 0
		for _, b := range sortedBalances {
			if b >= low && b < high {
				count++
			}
		}
		if count > 0 || i == 0 { // Always show first bucket even if empty
			buckets = append(buckets, models.MonteCarloDistBucket{
				Label:      formatBucketLabel(low, high),
				Count:      count,
				Percentage: float64(count) / float64(total) * 100,
			})
		}
	}

	// Add final bucket for values at or above last boundary
	lastBoundary := boundaries[len(boundaries)-1]
	count := 0
	for _, b := range sortedBalances {
		if b >= lastBoundary {
			count++
		}
	}
	if count > 0 {
		buckets = append(buckets, models.MonteCarloDistBucket{
			Label:      formatBucketLabel(lastBoundary, -1),
			Count:      count,
			Percentage: float64(count) / float64(total) * 100,
		})
	}

	return &models.MonteCarloDistribution{Buckets: buckets}
}

// formatBucketLabel formats a bucket range for display.
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

// sortFloat64s sorts the slice in ascending order using a simple
// insertion sort. The expected slice length is small (a few hundred to
// a few thousand) and the previous implementation used the same
// quadratic algorithm, so we preserve byte-equal behavior.
func sortFloat64s(a []float64) {
	for i := 0; i < len(a)-1; i++ {
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

// mean returns the arithmetic mean of the slice (0 for empty).
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
