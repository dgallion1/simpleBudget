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
	if result.DepletionYear <= result.StartYear {
		return result.DepletionYear
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
	taxableBalance := s.PortfolioValue - taxDeferredBalance - rothBalance

	currentLivingExpenses := s.MonthlyLivingExpenses
	var monthlyRMD float64

	peakBalance := s.PortfolioValue
	lowestBalance := s.PortfolioValue
	lowestBalanceYear := 0
	worstDrawdown := 0.0
	totalWithdrawals := 0.0
	totalBalance := s.PortfolioValue
	cumulativeInflation := 1.0 // Track cumulative inflation for real balance calculation

	// Get per-account asset allocations (consistent with main projection and Monte Carlo)
	tdStock, tdBond, tdCash := s.GetTaxDeferredAllocation()
	rothStock, rothBond, rothCash := s.GetRothAllocation()
	taxStock, taxBond, taxCash := s.GetTaxableAllocation()

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
		allowTaxDeferredWithdrawal := !taxDeferredDelayActive(s, currentYear)
		penaltyRate := earlyWithdrawalPenaltyRate(s.CurrentAge, currentYear)

		// Annual adjustments at year boundaries
		if m%12 == 0 {
			// Check for chain transition
			if len(c.ResolvedChain) > 0 {
				newIdx, prepared := c.nextChainTransition(currentYear, nextChainIdx, primarySettings)
				if prepared != nil {
					activeSettings = prepared
					s = activeSettings
					nextChainIdx = newIdx

					// Recalculate living expenses from new settings
					if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
						phaseMultiplier := s.GetSpendingMultiplier(phaseAge)
						currentLivingExpenses = s.MonthlyLivingExpenses * phaseMultiplier * cumulativeInflation
					} else {
						currentLivingExpenses = s.MonthlyLivingExpenses * cumulativeInflation
					}

					// Refresh cached allocation variables
					tdStock, tdBond, tdCash = s.GetTaxDeferredAllocation()
					rothStock, rothBond, rothCash = s.GetRothAllocation()
					taxStock, taxBond, taxCash = s.GetTaxableAllocation()
				}
			}

			// Get this year's historical data
			yearData := sequence[currentYear]
			inflationRate := yearData.InflationRate / 100

			// Track cumulative inflation for real balance calculation
			if m > 0 {
				cumulativeInflation *= (1 + inflationRate)
			}

			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
				phaseMultiplier := s.GetSpendingMultiplier(phaseAge)
				// Use cumulative inflation (properly tracked above) for spending phase calculations
				currentLivingExpenses = s.MonthlyLivingExpenses * phaseMultiplier * cumulativeInflation
			} else {
				if m > 0 {
					currentLivingExpenses *= (1 + inflationRate)
				}
			}

			// Calculate RMD
			if olderAge >= RMDStartAge && taxDeferredBalance > 0 {
				annualRMD, _ := CalculateRMD(taxDeferredBalance, olderAge)
				monthlyRMD = annualRMD / 12
			} else {
				monthlyRMD = 0
			}

			// Process Roth conversions
			if s.RothConversion != nil && s.RothConversion.Enabled && taxDeferredBalance > 0 {
				if currentYear >= s.RothConversion.StartYear &&
					(s.RothConversion.EndYear == 0 || currentYear <= s.RothConversion.EndYear) {
					conversionAmount := math.Min(s.RothConversion.AnnualAmount, taxDeferredBalance)
					if conversionAmount > 0 {
						taxDeferredBalance -= conversionAmount
						rothBalance += conversionAmount
					}
				}
			}

			// Process big ticket items
			for _, item := range s.BigTicketItems {
				if item.Year == currentYear {
					if item.Type == models.BigTicketIncome {
						taxableBalance += item.Amount
					} else {
						remaining := applyBigTicketExpense(item.Amount, allowTaxDeferredWithdrawal, penaltyRate, &taxDeferredBalance, &taxableBalance, &rothBalance)
						bigTicketExpenseThisMonth += remaining
					}
				}
			}
		}

		// Calculate expenses
		activeHealthcare := s.GetTotalHealthcareCost(m)
		totalExpenses := currentLivingExpenses + activeHealthcare + bigTicketExpenseThisMonth

		for _, source := range s.ExpenseSources {
			expenseAmount := source.GetAdjustedAmount(m, s.InflationRate)
			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled && source.Discretionary {
				expenseAmount *= s.GetSpendingMultiplier(phaseAge)
			}
			totalExpenses += expenseAmount
		}

		// Calculate income
		totalIncome := c.CalculateTotalIncome(m)
		neededFromPortfolio := totalExpenses - totalIncome

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
		taxMonthlyReturn := math.Pow(1+taxAnnualReturn, 1.0/12) - 1

		// Apply returns to each account based on its allocation
		taxDeferredBalance *= (1 + tdMonthlyReturn)
		rothBalance *= (1 + rothMonthlyReturn)
		taxableBalance *= (1 + taxMonthlyReturn)

		// Process withdrawals
		shortfall := 0.0
		if neededFromPortfolio > 0 {
			withdrawal := withdrawForExpenses(neededFromPortfolio, monthlyRMD, allowTaxDeferredWithdrawal, penaltyRate, &taxDeferredBalance, &taxableBalance, &rothBalance)
			totalWithdrawals += withdrawal.ActualWithdrawal
			shortfall = withdrawal.RemainingNeed
			// Enforce remaining RMD obligation
			unmetRMD := monthlyRMD - withdrawal.RMDWithdrawal
			if unmetRMD > 0 {
				totalWithdrawals += reinvestRequiredRMD(unmetRMD, &taxDeferredBalance, &taxableBalance)
			}
		} else {
			// Surplus income: reinvest into taxable account
			if neededFromPortfolio < 0 {
				taxableBalance += math.Abs(neededFromPortfolio)
			}
			totalWithdrawals += reinvestRequiredRMD(monthlyRMD, &taxDeferredBalance, &taxableBalance)
		}

		totalBalance = taxDeferredBalance + rothBalance + taxableBalance
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
