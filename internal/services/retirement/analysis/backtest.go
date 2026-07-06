package analysis

import (
	"math"
	"runtime"
	"sort"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
)

// HistoricalSequenceResult represents the outcome of one historical
// sequence within a backtest.
type HistoricalSequenceResult struct {
	StartYear           int     // Year this sequence started
	Survives            bool    // Did the portfolio survive the full period?
	FinalBalance        float64 // Nominal balance at end of projection
	FinalBalanceReal    float64 // Inflation-adjusted balance (start-year dollars)
	CumulativeInflation float64 // Total inflation factor over period
	DepletionYear       int     // Year of depletion (0 if survives)
	LowestBalance       float64 // Minimum balance reached
	LowestBalanceYr     int     // Year of lowest balance
	WorstDrawdown       float64 // Worst percentage drawdown from peak
	AvgWithdrawRate     float64 // Average withdrawal rate across the period
	TotalIRMAA          float64 // Cumulative IRMAA surcharge over the period
}

func yearsUntilDepletion(result HistoricalSequenceResult) int {
	if result.DepletionYear <= 0 {
		return 0
	}
	if result.DepletionYear < result.StartYear {
		return 0
	}
	return result.DepletionYear - result.StartYear
}

// YearsUntilDepletion exposes yearsUntilDepletion for retirement-package
// test helpers.
func YearsUntilDepletion(result HistoricalSequenceResult) int {
	return yearsUntilDepletion(result)
}

// HistoricalBacktest runs the projection against all available
// historical sequences in data. Returns an analysis with success rate,
// best/worst start years, and per-sequence details.
func HistoricalBacktest(in engine.Input, data history.Data) *models.HistoricalBacktestAnalysis {
	s := in.Prepared.Settings()
	if s == nil {
		return nil
	}
	projectionYears := s.ProjectionYears

	availableYears := history.AvailableStartYears(data, projectionYears)
	if len(availableYears) == 0 {
		return &models.HistoricalBacktestAnalysis{
			TotalSequences: 0,
			SuccessRate:    0,
		}
	}

	// The sequences are independent deterministic projections, so they
	// run on a worker pool (same pattern as the Monte Carlo runs). Each
	// result lands in its start-year-order slot, keeping the output
	// identical to the sequential form regardless of scheduling.
	results := make([]HistoricalSequenceResult, len(availableYears))
	ParallelIndexed(len(availableYears), runtime.NumCPU(), func(i int) {
		results[i] = runSingleHistoricalSequence(in, data, availableYears[i])
	})

	successCount := 0
	for _, result := range results {
		if result.Survives {
			successCount++
		}
	}

	// Sort results by final balance for percentile calculations
	sort.Slice(results, func(i, j int) bool {
		return results[i].FinalBalance < results[j].FinalBalance
	})

	// Find worst and best starting years
	worstYears := make([]int, 0, 5)
	bestYears := make([]int, 0, 5)

	// Sort by survival first, then by final balance for worst
	sortedByOutcome := make([]HistoricalSequenceResult, len(results))
	copy(sortedByOutcome, results)
	sort.Slice(sortedByOutcome, func(i, j int) bool {
		if sortedByOutcome[i].Survives != sortedByOutcome[j].Survives {
			return !sortedByOutcome[i].Survives // Failures first
		}
		// Among failures, rank by how quickly they fail relative to
		// their own start year.
		if !sortedByOutcome[i].Survives {
			return yearsUntilDepletion(sortedByOutcome[i]) < yearsUntilDepletion(sortedByOutcome[j])
		}
		return sortedByOutcome[i].FinalBalance < sortedByOutcome[j].FinalBalance
	})

	for i := 0; i < min(5, len(sortedByOutcome)); i++ {
		worstYears = append(worstYears, sortedByOutcome[i].StartYear)
	}

	// Best: sort by final balance descending
	sort.Slice(sortedByOutcome, func(i, j int) bool {
		return sortedByOutcome[i].FinalBalance > sortedByOutcome[j].FinalBalance
	})

	for i := 0; i < min(5, len(sortedByOutcome)); i++ {
		bestYears = append(bestYears, sortedByOutcome[i].StartYear)
	}

	// Calculate percentile balances
	p10 := results[len(results)/10].FinalBalance
	p25 := results[len(results)/4].FinalBalance
	p50 := results[len(results)/2].FinalBalance
	p75 := results[len(results)*3/4].FinalBalance
	p90 := results[len(results)*9/10].FinalBalance

	// Re-sort for the UI table: failures first (quickest depletion at
	// top), then survivors ascending by final balance (worst survivor
	// → best survivor). This puts the rows that drive the failure
	// rate at the top of the scrollable list so users immediately see
	// why the success rate isn't 100%, instead of having to scroll
	// past the best survivors to find them.
	sort.Slice(sortedByOutcome, func(i, j int) bool {
		if sortedByOutcome[i].Survives != sortedByOutcome[j].Survives {
			return !sortedByOutcome[i].Survives // failures first
		}
		if !sortedByOutcome[i].Survives {
			return yearsUntilDepletion(sortedByOutcome[i]) < yearsUntilDepletion(sortedByOutcome[j])
		}
		return sortedByOutcome[i].FinalBalance < sortedByOutcome[j].FinalBalance
	})

	// Create sequence details for UI
	sequenceDetails := make([]models.HistoricalBacktestResult, len(results))
	for i, r := range sortedByOutcome {
		sequenceDetails[i] = models.HistoricalBacktestResult{
			StartYear:           r.StartYear,
			Survives:            r.Survives,
			FinalBalance:        r.FinalBalance,
			FinalBalanceReal:    r.FinalBalanceReal,
			CumulativeInflation: r.CumulativeInflation,
			DepletionYear:       r.DepletionYear,
			WorstDrawdown:       r.WorstDrawdown,
		}
	}

	// The percentiles are calculated but not currently stored in the
	// model. They could be added to the UI later if needed.
	_ = p10
	_ = p25
	_ = p50
	_ = p75
	_ = p90

	dataStartYear := 0
	dataEndYear := 0
	if len(data) > 0 {
		dataStartYear = data[0].Year
		dataEndYear = data[len(data)-1].Year
	}

	return &models.HistoricalBacktestAnalysis{
		TotalSequences:  len(results),
		SurvivedCount:   successCount,
		SuccessRate:     float64(successCount) / float64(len(results)) * 100,
		WorstStartYears: worstYears,
		BestStartYears:  bestYears,
		Results:         sequenceDetails,
		DataStartYear:   dataStartYear,
		DataEndYear:     dataEndYear,
	}
}

// RunSingleHistoricalSequence exposes the single-sequence runner for
// retirement-package test helpers.
func RunSingleHistoricalSequence(in engine.Input, data history.Data, startYear int) HistoricalSequenceResult {
	return runSingleHistoricalSequence(in, data, startYear)
}

// runSingleHistoricalSequence runs the projection using historical
// market data starting from a specific year.
func runSingleHistoricalSequence(in engine.Input, data history.Data, startYear int) HistoricalSequenceResult {
	st := engine.NewProjectionState(in)
	s := st.Settings()
	months := s.ProjectionYears * 12

	// Get historical sequence
	sequence := history.Sequence(data, startYear, s.ProjectionYears)
	if sequence == nil {
		return HistoricalSequenceResult{StartYear: startYear, Survives: false}
	}

	peakBalance := s.PortfolioValue
	lowestBalance := s.PortfolioValue
	lowestBalanceYear := 0
	worstDrawdown := 0.0
	totalWithdrawals := 0.0
	totalBalance := s.PortfolioValue

	// Get per-account asset allocations (consistent with main projection and Monte Carlo)
	tdStock, tdBond, tdCash, rothStock, rothBond, rothCash, taxStock, taxBond, taxCash := s.GetAllocationAtYear(0)

	result := HistoricalSequenceResult{
		StartYear:       startYear,
		Survives:        true,
		LowestBalance:   s.PortfolioValue,
		LowestBalanceYr: 0,
	}

	// btReturns supplies each month's historical returns and inflation.
	// Invoked by StepMonth after any chain transition, so the year-boundary
	// allocation refresh sees the active settings.
	btReturns := func(s *models.WhatIfSettings, m int) engine.MonthReturns {
		currentYear := m / 12
		if m%12 == 0 {
			// Refresh allocation for glide path and chain transitions
			tdStock, tdBond, tdCash, rothStock, rothBond, rothCash, taxStock, taxBond, taxCash = s.GetAllocationAtYear(currentYear)
		}

		// This year's historical returns as decimals
		yearData := sequence[currentYear]
		stockReturn := yearData.SP500Return / 100
		bondReturn := yearData.BondReturn / 100
		cashReturn := yearData.CashReturn / 100
		inflationRate := yearData.InflationRate / 100

		// Per-account annual returns based on each account's allocation
		tdAnnualReturn := (tdStock/100)*stockReturn + (tdBond/100)*bondReturn + (tdCash/100)*cashReturn
		rothAnnualReturn := (rothStock/100)*stockReturn + (rothBond/100)*bondReturn + (rothCash/100)*cashReturn
		taxAnnualReturn := (taxStock/100)*stockReturn + (taxBond/100)*bondReturn + (taxCash/100)*cashReturn

		return engine.MonthReturns{
			// Convert to monthly using geometric formula (not simple division)
			TaxDeferredMonthly: math.Pow(1+tdAnnualReturn, 1.0/12) - 1,
			RothMonthly:        math.Pow(1+rothAnnualReturn, 1.0/12) - 1,
			// The seam takes the taxable return in percent; passing the raw
			// decimal here once understated taxable appreciation ~100x.
			TaxableAnnualPercent:    taxAnnualReturn * 100,
			InflationAnnual:         inflationRate,
			NetInflationAnnual:      inflationRate - s.SpendingDeclineRate/100,
			HealthcareMultiplier:    1,
			DiscretionaryMultiplier: 1,
		}
	}

	for m := 0; m < months; m++ {
		currentYear := m / 12

		out := st.StepMonth(m, btReturns)
		result.TotalIRMAA += out.Result.IRMAAExpense
		totalWithdrawals += out.Result.CashFlow.GrossWithdrawal()

		totalBalance = out.TotalBalance
		if engine.ShortfallCausesDepletion(out.Result.Shortfall, out.AllowTaxDeferredWithdrawal, st.TaxDeferredBalance) {
			result.Survives = false
			result.DepletionYear = startYear + currentYear
			break
		}

		// Track metrics
		if totalBalance > peakBalance {
			peakBalance = totalBalance
		}
		if totalBalance < lowestBalance {
			lowestBalance = totalBalance
			lowestBalanceYear = currentYear
		}
		if peakBalance > 0 {
			drawdown := (peakBalance - totalBalance) / peakBalance * 100
			if drawdown > worstDrawdown {
				worstDrawdown = drawdown
			}
		}

		// Check for depletion
		if totalBalance <= 0 {
			result.Survives = false
			result.DepletionYear = startYear + currentYear
			result.FinalBalance = 0
			result.LowestBalance = 0
			result.LowestBalanceYr = currentYear
			result.WorstDrawdown = 100
			return result
		}
	}

	result.FinalBalance = totalBalance
	result.FinalBalanceReal = totalBalance / st.CumulativeInflation // Convert to start-year dollars
	result.CumulativeInflation = st.CumulativeInflation
	result.LowestBalance = lowestBalance
	result.LowestBalanceYr = lowestBalanceYear
	result.WorstDrawdown = worstDrawdown
	if s.PortfolioValue > 0 {
		result.AvgWithdrawRate = (totalWithdrawals / float64(s.ProjectionYears)) / s.PortfolioValue * 100
	}

	return result
}
