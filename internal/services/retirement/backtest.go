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
		// Among same survival status, sort by final balance (or depletion year for failures)
		if !sortedByOutcome[i].Survives {
			return sortedByOutcome[i].DepletionYear < sortedByOutcome[j].DepletionYear
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
	s := c.Settings
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

	// Get user's asset allocation (default to 60/40 stocks/bonds if not set)
	stockPercent := s.StockPercent
	cashPercent := s.CashPercent
	if stockPercent == 0 && cashPercent == 0 {
		stockPercent = 60.0 // Default 60% stocks
	}
	bondPercent := 100.0 - stockPercent - cashPercent

	result := HistoricalSequenceResult{
		StartYear:       startYear,
		Survives:        true,
		LowestBalance:   s.PortfolioValue,
		LowestBalanceYr: 0,
	}

	for m := 0; m < months; m++ {
		currentAge := s.CurrentAge + (m / 12)
		currentYear := m / 12

		// Annual adjustments at year boundaries
		if m%12 == 0 {
			// Get this year's historical data
			yearData := sequence[currentYear]
			inflationRate := yearData.InflationRate / 100

			// Track cumulative inflation for real balance calculation
			if m > 0 {
				cumulativeInflation *= (1 + inflationRate)
			}

			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
				phaseMultiplier := s.GetSpendingMultiplier(currentAge)
				if m > 0 {
					inflationFactor := math.Pow(1+inflationRate, float64(currentYear))
					currentLivingExpenses = s.MonthlyLivingExpenses * phaseMultiplier * inflationFactor
				} else {
					currentLivingExpenses = s.MonthlyLivingExpenses * phaseMultiplier
				}
			} else {
				if m > 0 {
					currentLivingExpenses *= (1 + inflationRate)
				}
			}

			// Calculate RMD
			if currentAge >= RMDStartAge && taxDeferredBalance > 0 {
				annualRMD, _ := CalculateRMD(taxDeferredBalance, currentAge)
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
						remaining := item.Amount
						if taxableBalance >= remaining {
							taxableBalance -= remaining
							remaining = 0
						} else if taxableBalance > 0 {
							remaining -= taxableBalance
							taxableBalance = 0
						}
						if remaining > 0 {
							if rothBalance >= remaining {
								rothBalance -= remaining
								remaining = 0
							} else if rothBalance > 0 {
								remaining -= rothBalance
								rothBalance = 0
							}
						}
						if remaining > 0 {
							if taxDeferredBalance >= remaining {
								taxDeferredBalance -= remaining
							} else {
								taxDeferredBalance = 0
							}
						}
					}
				}
			}
		}

		// Calculate expenses
		activeHealthcare := s.GetTotalHealthcareCost(m)
		totalExpenses := currentLivingExpenses + activeHealthcare

		for _, source := range s.ExpenseSources {
			expenseAmount := source.GetAdjustedAmount(m, s.InflationRate)
			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled && source.Discretionary {
				expenseAmount *= s.GetSpendingMultiplier(currentAge)
			}
			totalExpenses += expenseAmount
		}

		// Calculate income
		totalIncome := c.CalculateTotalIncome(m)
		neededFromPortfolio := totalExpenses - totalIncome

		// Get this month's returns using user's asset allocation
		yearData := sequence[currentYear]
		// Blend returns based on user's stock/bond/cash allocation
		monthlyReturn := (stockPercent/100)*yearData.SP500Return/100/12 +
			(bondPercent/100)*yearData.BondReturn/100/12 +
			(cashPercent/100)*yearData.CashReturn/100/12

		// Apply returns to all accounts
		taxDeferredBalance *= (1 + monthlyReturn)
		rothBalance *= (1 + monthlyReturn)
		taxableBalance *= (1 + monthlyReturn)

		// Process withdrawals
		if neededFromPortfolio > 0 {
			// RMD first
			if monthlyRMD > 0 && taxDeferredBalance > 0 {
				rmdUsed := math.Min(monthlyRMD, math.Min(neededFromPortfolio, taxDeferredBalance))
				taxDeferredBalance -= rmdUsed
				neededFromPortfolio -= rmdUsed
				totalWithdrawals += rmdUsed
			}

			// Taxable second
			if neededFromPortfolio > 0 && taxableBalance > 0 {
				fromTaxable := math.Min(neededFromPortfolio, taxableBalance)
				taxableBalance -= fromTaxable
				neededFromPortfolio -= fromTaxable
				totalWithdrawals += fromTaxable
			}

			// Roth third
			if neededFromPortfolio > 0 && rothBalance > 0 {
				fromRoth := math.Min(neededFromPortfolio, rothBalance)
				rothBalance -= fromRoth
				neededFromPortfolio -= fromRoth
				totalWithdrawals += fromRoth
			}

			// Tax-deferred last
			if neededFromPortfolio > 0 && taxDeferredBalance > 0 {
				fromTaxDeferred := math.Min(neededFromPortfolio, taxDeferredBalance)
				taxDeferredBalance -= fromTaxDeferred
				totalWithdrawals += fromTaxDeferred
			}
		} else if monthlyRMD > 0 && taxDeferredBalance > 0 {
			// RMD still required even when income covers expenses
			rmdWithdrawal := math.Min(monthlyRMD, taxDeferredBalance)
			taxDeferredBalance -= rmdWithdrawal
			taxableBalance += rmdWithdrawal
		}

		totalBalance = taxDeferredBalance + rothBalance + taxableBalance

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
