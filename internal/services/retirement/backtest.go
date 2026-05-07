package retirement

import (
	"math"
	"sort"

	"budget2/internal/models"
)

// HistoricalSequenceResult represents the outcome of one historical sequence
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

// RunHistoricalBacktest runs the projection against all available historical sequences
func (c *Calculator) RunHistoricalBacktest() *models.HistoricalBacktestAnalysis {
	s := c.Settings
	projectionYears := s.ProjectionYears

	// Get all available starting years
	availableYears := GetAvailableStartYears(projectionYears)
	if len(availableYears) == 0 {
		return &models.HistoricalBacktestAnalysis{
			TotalSequences: 0,
			SuccessRate:    0,
		}
	}

	results := make([]HistoricalSequenceResult, 0, len(availableYears))
	successCount := 0

	for _, startYear := range availableYears {
		result := c.runSingleHistoricalSequence(startYear)
		results = append(results, result)
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
		// Failed sequences are worse than surviving ones
		if sortedByOutcome[i].Survives != sortedByOutcome[j].Survives {
			return !sortedByOutcome[i].Survives // Failures first
		}
		// Among failures, rank by how quickly they fail relative to their own start year.
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

	// Re-sort for the UI table: failures first (quickest depletion at top),
	// then survivors ascending by final balance (worst survivor → best
	// survivor). This puts the rows that drive the failure rate at the top of
	// the scrollable list so users immediately see why the success rate isn't
	// 100%, instead of having to scroll past the best survivors to find them.
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

	// The percentiles are calculated but not currently stored in the model
	// They could be added to the UI later if needed
	_ = p10
	_ = p25
	_ = p50
	_ = p75
	_ = p90

	return &models.HistoricalBacktestAnalysis{
		TotalSequences:  len(results),
		SurvivedCount:   successCount,
		SuccessRate:     float64(successCount) / float64(len(results)) * 100,
		WorstStartYears: worstYears,
		BestStartYears:  bestYears,
		Results:         sequenceDetails,
		DataStartYear:   HistoricalReturns[0].Year,
		DataEndYear:     HistoricalReturns[len(HistoricalReturns)-1].Year,
	}
}

// runSingleHistoricalSequence runs projection using historical data starting from a specific year
func (c *Calculator) runSingleHistoricalSequence(startYear int) HistoricalSequenceResult {
	primarySettings := c.Settings
	activeSettings := c.Settings
	nextChainIdx := 0
	s := activeSettings
	months := s.ProjectionYears * 12

	// Get historical sequence
	sequence := GetHistoricalSequence(startYear, s.ProjectionYears)
	if sequence == nil {
		return HistoricalSequenceResult{StartYear: startYear, Survives: false}
	}

	// Initialize 3-bucket model
	taxDeferredBalance := s.PortfolioValue * (s.TaxDeferredPercent / 100)
	rothBalance := s.PortfolioValue * (s.RothPercent / 100)
	taxableAccount := newTaxableAccountState(s, s.PortfolioValue-taxDeferredBalance-rothBalance)

	currentLivingExpenses := calculateLivingExpensesAtMonth(s, 0)
	// F-074: annualRMD persists across months so the trigger-month logic
	// can apply the full year's RMD in a single month.
	var annualRMD float64
	var monthlyRMD float64
	var taxState projectionTaxAccumulator
	taxCalculator := NewTaxCalculator(s.TaxConfig, s.InflationRate)
	completedMAGIHistory := make([]float64, 0, s.ProjectionYears)
	currentYearTaxSnapshot := projectedTaxSnapshot{}

	peakBalance := s.PortfolioValue
	lowestBalance := s.PortfolioValue
	lowestBalanceYear := 0
	worstDrawdown := 0.0
	totalWithdrawals := 0.0
	totalBalance := s.PortfolioValue
	cumulativeInflation := 1.0 // Track cumulative inflation for real balance calculation
	// netCumulativeInflation tracks (inflationRate−SpendingDeclineRate) compounding,
	// mirroring the per-month no-phase expense accumulation. Used by
	// rebaseLivingExpensesAtTransition to avoid the F-065 step-up error.
	netCumulativeInflation := 1.0
	inflationRate := 0.0

	// Spending guardrails for this backtest run
	var btGrState *guardrailState
	if s.Guardrails != nil && s.Guardrails.Enabled {
		btGrState = newGuardrailState(s.PortfolioValue)
	}

	// Get per-account asset allocations (consistent with main projection and Monte Carlo)
	tdStock, tdBond, tdCash, rothStock, rothBond, rothCash, taxStock, taxBond, taxCash := s.GetAllocationAtYear(0)

	result := HistoricalSequenceResult{
		StartYear:       startYear,
		Survives:        true,
		LowestBalance:   s.PortfolioValue,
		LowestBalanceYr: 0,
	}

	for m := 0; m < months; m++ {
		currentYear := m / 12
		phaseAge := s.GetPhaseReferenceAge(currentYear) // Age used for spending phase calculations (may differ for couples)
		// RMD uses OLDER person's age - whoever hits 73 first triggers RMD
		olderAge := s.GetOlderAge() + currentYear
		bigTicketExpenseThisMonth := 0.0
		rothConversionThisMonth := 0.0
		allowTaxDeferredWithdrawal := !taxDeferredDelayActive(s, currentYear)
		penaltyRate := earlyWithdrawalPenaltyRate(s.CurrentAge, currentYear)

		// Annual adjustments at year boundaries
		if m%12 == 0 {
			if m > 0 {
				completedMAGIHistory = append(completedMAGIHistory, currentYearTaxSnapshot.AnnualMAGI)
			}
			taxState = projectionTaxAccumulator{}
			// Check for chain transition
			if len(c.ResolvedChain) > 0 {
				newIdx, prepared := c.nextChainTransition(currentYear, nextChainIdx, primarySettings)
				if prepared != nil {
					activeSettings = prepared
					s = activeSettings
					nextChainIdx = newIdx

					currentLivingExpenses = rebaseLivingExpensesAtTransition(s, phaseAge, cumulativeInflation, netCumulativeInflation)
					taxableAccount.syncAssumptions(s)
				}
			}
			// Refresh allocation for glide path and chain transitions
			tdStock, tdBond, tdCash, rothStock, rothBond, rothCash, taxStock, taxBond, taxCash = s.GetAllocationAtYear(currentYear)
			taxCalculator = NewTaxCalculator(s.TaxConfig, s.InflationRate)
			taxableAccount.RealizedGainsYTD = 0

			// Get this year's historical data
			yearData := sequence[currentYear]
			inflationRate = yearData.InflationRate / 100

			// F-074: see PR 2 — annualRMD computed once per year, applied
			// only in the trigger month inside the month loop.
			if olderAge >= RMDStartAge && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, olderAge)
			} else {
				annualRMD = 0
			}
			monthlyRMD = 0

			// Process Roth conversions
			if conversionAmount := rothConversionAmountForYear(s, currentYear, taxDeferredBalance); conversionAmount > 0 {
				taxDeferredBalance -= conversionAmount
				rothBalance += conversionAmount
				rothConversionThisMonth = conversionAmount
			}

			// Process big ticket items
			for _, item := range s.BigTicketItems {
				if item.Year == currentYear {
					if item.Type == models.BigTicketIncome {
						taxableAccount.addCash(item.Amount)
					} else {
						remaining := applyBigTicketExpenseWithTaxableState(item.Amount, allowTaxDeferredWithdrawal, penaltyRate, &taxDeferredBalance, &taxableAccount, &rothBalance)
						bigTicketExpenseThisMonth += remaining
					}
				}
			}
		}

		if m > 0 {
			cumulativeInflation *= monthlyCompoundFactorFromDecimal(inflationRate)
			netCumulativeInflation *= monthlyCompoundFactorFromDecimal(inflationRate - s.SpendingDeclineRate/100)
			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
				currentLivingExpenses = s.MonthlyLivingExpenses * s.GetSpendingMultiplier(phaseAge) * cumulativeInflation
			} else {
				currentLivingExpenses *= monthlyCompoundFactorFromDecimal(inflationRate - s.SpendingDeclineRate/100)
			}
		}

		// Evaluate guardrails at year boundaries
		if btGrState != nil && m%12 == 0 {
			totalPortfolio := taxDeferredBalance + taxableAccount.MarketValue + rothBalance
			btGrState.evaluate(s.Guardrails, totalPortfolio)
		}

		// Apply guardrail spending multiplier
		btAdjustedLiving := currentLivingExpenses
		if btGrState != nil {
			btAdjustedLiving *= btGrState.multiplier()
		}

		// Calculate expenses
		activeHealthcare := s.GetTotalHealthcareCost(m)
		totalExpenses := btAdjustedLiving + activeHealthcare + bigTicketExpenseThisMonth

		for _, source := range s.ExpenseSources {
			expenseAmount := source.GetAdjustedAmount(m, s.InflationRate)
			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled && source.Discretionary {
				expenseAmount *= s.GetSpendingMultiplier(phaseAge)
			}
			totalExpenses += expenseAmount
		}

		incomeBreakdown := calculateMonthlyIncomeBreakdown(s, m)

		// Get this year's returns and calculate per-account blended returns
		yearData := sequence[currentYear]
		stockReturn := yearData.SP500Return / 100
		bondReturn := yearData.BondReturn / 100
		cashReturn := yearData.CashReturn / 100

		// Calculate per-account annual returns based on each account's allocation
		tdAnnualReturn := (tdStock/100)*stockReturn + (tdBond/100)*bondReturn + (tdCash/100)*cashReturn
		rothAnnualReturn := (rothStock/100)*stockReturn + (rothBond/100)*bondReturn + (rothCash/100)*cashReturn
		taxAnnualReturn := (taxStock/100)*stockReturn + (taxBond/100)*bondReturn + (taxCash/100)*cashReturn

		// Convert to monthly using geometric formula (not simple division)
		tdMonthlyReturn := math.Pow(1+tdAnnualReturn, 1.0/12) - 1
		rothMonthlyReturn := math.Pow(1+rothAnnualReturn, 1.0/12) - 1
		taxableComponents := buildTaxableReturnComponents(taxAnnualReturn, s)
		irmaaEligibleAdults := medicareEligibleAdultCountAtYear(s, currentYear)
		irmaaInflationFactor := plannerIRMAAInflationFactorForYear(s.InflationRate, float64(currentYear))

		// F-074: apply the full annual RMD only in the trigger month.
		monthlyRMD = 0
		if annualRMD > 0 && m%12 == rmdTriggerMonth(s.RMDTiming) {
			monthlyRMD = math.Min(annualRMD, taxDeferredBalance)
		}

		monthResult := executeTaxAwarePortfolioMonth(
			totalExpenses,
			incomeBreakdown,
			monthlyRMD,
			allowTaxDeferredWithdrawal,
			penaltyRate,
			&taxDeferredBalance,
			&taxableAccount,
			&rothBalance,
			tdMonthlyReturn,
			rothMonthlyReturn,
			taxableComponents,
			s.GetProjectionTiming(),
			taxState,
			taxCalculator,
			currentYear,
			m%12,
			rothConversionThisMonth,
			completedMAGIHistory,
			irmaaEligibleAdults,
			irmaaInflationFactor,
		)
		currentYearTaxSnapshot = monthResult.TaxSnapshot
		taxState.applyMonth(
			incomeBreakdown.OrdinaryIncome+monthResult.TaxableNonQualifiedDividends,
			incomeBreakdown.SocialSecurityIncome,
			monthResult.CashFlow.WithdrawalFromTaxDeferred,
			monthResult.TaxableQualifiedDividends,
			monthResult.TaxableCapitalGains,
			monthResult.TaxableNonQualifiedDividends,
			rothConversionThisMonth,
			monthResult.TaxesPaid,
		)
		totalWithdrawals += monthResult.CashFlow.GrossWithdrawal()
		shortfall := monthResult.Shortfall

		totalBalance = taxDeferredBalance + rothBalance + taxableAccount.MarketValue
		if shortfallCausesDepletion(shortfall, allowTaxDeferredWithdrawal, taxDeferredBalance) {
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
	result.FinalBalanceReal = totalBalance / cumulativeInflation // Convert to start-year dollars
	result.CumulativeInflation = cumulativeInflation
	result.LowestBalance = lowestBalance
	result.LowestBalanceYr = lowestBalanceYear
	result.WorstDrawdown = worstDrawdown
	if s.PortfolioValue > 0 {
		result.AvgWithdrawRate = (totalWithdrawals / float64(s.ProjectionYears)) / s.PortfolioValue * 100
	}

	return result
}
