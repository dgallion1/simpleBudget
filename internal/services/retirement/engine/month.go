package engine

import (
	"math"

	"budget2/internal/models"
)

// NextChainTransitionHook is the chain-transition resolver. The engine
// package doesn't import retirement (cycle), so the projection loop
// reaches into retirement's prepareChainedSettings via this hook. The
// retirement package's init() wires it. Default returns nil (no
// transition) so engine is safe in isolation.
var NextChainTransitionHook = func(currentYear, nextChainIndex int, primarySettings *models.WhatIfSettings, chain []PreparedChainLink) (int, *models.WhatIfSettings) {
	return nextChainIndex, nil
}

// taxDeferredDelayActive reports whether the tax-deferred-withdrawal
// delay window is still in effect for the given calendar year offset.
func taxDeferredDelayActive(s *models.WhatIfSettings, currentYear int) bool {
	return s.TaxDeferredDelayYears > 0 && currentYear < s.TaxDeferredDelayYears
}

// TaxDeferredDelayActive is the exported counterpart of
// taxDeferredDelayActive so analysis-package callers (Monte Carlo,
// historical backtest) can reuse the rule without redefining it.
func TaxDeferredDelayActive(s *models.WhatIfSettings, currentYear int) bool {
	return taxDeferredDelayActive(s, currentYear)
}

// earlyWithdrawalPenaltyRate returns the IRS 10% early distribution
// penalty rate for tax-deferred withdrawals before age 59½. Uses age
// 60 as the cutoff since the model operates in whole years.
func earlyWithdrawalPenaltyRate(currentAge, currentYear int) float64 {
	if currentAge+currentYear < 60 {
		return 0.10
	}
	return 0
}

// EarlyWithdrawalPenaltyRate is the exported counterpart of
// earlyWithdrawalPenaltyRate so analysis-package callers (Monte Carlo,
// historical backtest) can reuse the rule.
func EarlyWithdrawalPenaltyRate(currentAge, currentYear int) float64 {
	return earlyWithdrawalPenaltyRate(currentAge, currentYear)
}

// runMonthlyLoop is the deterministic monthly projection loop. Given
// an Input (prepared settings + optional chain), it returns a fully
// populated *models.ProjectionResult. Pure function: same in, same
// out.
func runMonthlyLoop(in Input) *models.ProjectionResult {
	primarySettings := in.Prepared.Settings()
	activeSettings := primarySettings
	chain := in.Chain
	nextChainIdx := 0
	s := activeSettings
	months := s.ProjectionYears * 12
	projection := make([]models.ProjectionMonth, 0, months)

	// Split portfolio into tax-deferred, Roth, and taxable portions
	taxDeferredBalance := s.PortfolioValue * (s.TaxDeferredPercent / 100)
	rothBalance := s.PortfolioValue * (s.RothPercent / 100)
	taxableAccount := NewTaxableAccountState(s, s.PortfolioValue-taxDeferredBalance-rothBalance)

	var depletionMonth *int
	var longevityYears *float64

	currentLivingExpenses := livingExpensesAtMonth(s, 0)

	// Track cumulative inflation for spending phase calculations
	cumulativeInflation := 1.0
	// netCumulativeInflation tracks (InflationRate−SpendingDeclineRate) compounding,
	// mirroring the per-month no-phase expense accumulation. Used by
	// rebaseLivingExpensesAtTransition to avoid the F-065 step-up error.
	netCumulativeInflation := 1.0

	// Spending guardrails
	var grState *GuardrailState
	var guardrailEvents []models.GuardrailEvent
	if s.Guardrails != nil && s.Guardrails.Enabled {
		grState = NewGuardrailState(s.PortfolioValue)
	}

	// Track annual RMD (calculated once per year, distributed monthly)
	var annualRMD float64
	var monthlyRMD float64
	var taxState ProjectionTaxAccumulator
	taxCalculator := NewTaxCalculator(s.TaxConfig, s.InflationRate)
	completedMAGIHistory := make([]float64, 0, s.ProjectionYears)
	currentYearTaxSnapshot := ProjectedTaxSnapshot{}
	yearlySummaries := make([]models.ProjectionYearSummary, 0, s.ProjectionYears)
	currentYearSummary := models.ProjectionYearSummary{
		Year:            0,
		StartingBalance: s.PortfolioValue,
	}

	finalizeCurrentYear := func(month models.ProjectionMonth) {
		currentYearSummary.MAGI = currentYearTaxSnapshot.AnnualMAGI
		currentYearSummary.NIIT = currentYearTaxSnapshot.AnnualNIIT
		currentYearSummary.IRMAA = currentYearTaxSnapshot.AnnualIRMAA
		currentYearSummary.TaxableSocialSecurityPct = currentYearTaxSnapshot.TaxableSocialSecurityPct
		currentYearSummary.EndingBalance = month.PortfolioBalance
		currentYearSummary.EndingBalanceReal = month.PortfolioBalanceReal
		currentYearSummary.CumulativeInflation = month.CumulativeInflation
		currentYearSummary.GuardrailMultiplier = month.GuardrailMultiplier
		yearlySummaries = append(yearlySummaries, currentYearSummary)
	}

	for m := 0; m < months; m++ {
		currentYear := m / 12
		monthInYear := m % 12
		phaseAge := s.GetPhaseReferenceAge(currentYear) // Age used for spending phase calculations (may differ for couples)
		bigTicketExpenseThisMonth := 0.0
		rothConversionThisMonth := 0.0
		allowTaxDeferredWithdrawal := !taxDeferredDelayActive(s, currentYear)
		penaltyRate := earlyWithdrawalPenaltyRate(s.CurrentAge, currentYear)

		// Annual adjustments at year boundaries
		if m%12 == 0 {
			if m > 0 && len(projection) > 0 {
				completedMAGIHistory = append(completedMAGIHistory, currentYearTaxSnapshot.AnnualMAGI)
				finalizeCurrentYear(projection[len(projection)-1])
				currentYearSummary = models.ProjectionYearSummary{
					Year:            currentYear,
					StartingBalance: projection[len(projection)-1].PortfolioBalance,
				}
			}

			taxState = ProjectionTaxAccumulator{}
			// Check for chain transition
			if len(chain) > 0 {
				newIdx, prepared := NextChainTransitionHook(currentYear, nextChainIdx, primarySettings, chain)
				if prepared != nil {
					activeSettings = prepared
					s = activeSettings
					nextChainIdx = newIdx

					currentLivingExpenses = RebaseLivingExpensesAtTransition(s, phaseAge, cumulativeInflation, netCumulativeInflation)
					taxableAccount.SyncAssumptions(s)
				}
			}
			taxCalculator = NewTaxCalculator(s.TaxConfig, s.InflationRate)
			taxableAccount.RealizedGainsYTD = 0

			// F-074: compute annualRMD once per year on year-start tax-deferred
			// balance (matches IRS "December 31 prior year" rule). Per-month
			// monthlyRMD is set inside the month loop based on RMDTiming.
			// F-078: gate on calendar year vs FirstRMDCalendarYear and pass
			// age-at-year-end to CalculateRMD so late-year births attain
			// age 73 (or 75) in the right calendar year and the divisor
			// reads the correct UL Table row.
			calendarYear := ParseStartYear(s.StartDate) + currentYear
			if RMDApplies(s, calendarYear) && taxDeferredBalance > 0 {
				annualRMD, _ = CalculateRMD(taxDeferredBalance, RMDAgeForCalendarYear(s, calendarYear))
			} else {
				annualRMD = 0
			}
			monthlyRMD = 0

			// Process Roth conversions (annual, at year boundary)
			if conversionAmount := RothConversionAmountForYear(s, currentYear, taxDeferredBalance); conversionAmount > 0 {
				taxDeferredBalance -= conversionAmount
				rothBalance += conversionAmount
				rothConversionThisMonth = conversionAmount
			}

			// Process big ticket items for this year
			for _, item := range s.BigTicketItems {
				if item.Year == currentYear {
					if item.Type == models.BigTicketIncome {
						// Income adds to taxable balance (e.g., inheritance, home sale)
						taxableAccount.AddCash(item.Amount)
					} else {
						remaining := ApplyBigTicketExpenseWithTaxableState(item.Amount, allowTaxDeferredWithdrawal, penaltyRate, &taxDeferredBalance, &taxableAccount, &rothBalance)
						bigTicketExpenseThisMonth += remaining
					}
				}
			}
		}

		if m > 0 {
			cumulativeInflation *= monthlyCompoundFactorFromPercent(s.InflationRate)
			netCumulativeInflation *= monthlyCompoundFactorFromPercent(s.InflationRate - s.SpendingDeclineRate)
			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
				currentLivingExpenses = s.MonthlyLivingExpenses * s.GetSpendingMultiplier(phaseAge) * cumulativeInflation
			} else {
				currentLivingExpenses *= monthlyCompoundFactorFromPercent(s.InflationRate - s.SpendingDeclineRate)
			}
		}

		// Evaluate spending guardrails at year boundaries
		if grState != nil && m%12 == 0 {
			totalPortfolio := taxDeferredBalance + taxableAccount.MarketValue + rothBalance
			prevMult := grState.Multiplier()
			grState.Evaluate(s.Guardrails, totalPortfolio)
			newMult := grState.Multiplier()
			if newMult != prevMult {
				eventType := "cut"
				if newMult > prevMult {
					eventType = "raise"
				}
				guardrailEvents = append(guardrailEvents, models.GuardrailEvent{
					Year:                  currentYear,
					Type:                  eventType,
					Multiplier:            newMult,
					Portfolio:             totalPortfolio,
					MonthlySpendingBefore: currentLivingExpenses * prevMult,
					MonthlySpendingAfter:  currentLivingExpenses * newMult,
				})
			}
		}

		// Apply guardrail spending multiplier
		activeMultiplier := 1.0
		if grState != nil {
			activeMultiplier = grState.Multiplier()
		}
		adjustedLivingExpenses := currentLivingExpenses * activeMultiplier

		// Calculate healthcare expenses using multi-person model
		activeHealthcare := s.GetTotalHealthcareCost(m)
		plannedTotalExpenses := currentLivingExpenses + activeHealthcare + bigTicketExpenseThisMonth
		totalExpensesAcc := adjustedLivingExpenses + activeHealthcare + bigTicketExpenseThisMonth

		// Add expense sources (discretionary sources get phase multiplier when enabled)
		// ExpenseSources are not subject to guardrail cuts — keep planned and adjusted in sync.
		for _, source := range s.ExpenseSources {
			expenseAmount := source.GetAdjustedAmount(m, s.InflationRate)
			if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled && source.Discretionary {
				expenseAmount *= s.GetSpendingMultiplier(phaseAge)
			}
			totalExpensesAcc += expenseAmount
			plannedTotalExpenses += expenseAmount
		}

		// Calculate income using the active settings for this month.
		incomeBreakdown := CalculateMonthlyIncomeBreakdown(s, m)
		totalIncomeMonth := incomeBreakdown.TotalIncome

		// Apply investment growth to each account based on its asset allocation
		// If InvestmentReturn is explicitly set (non-zero), use it as override for all accounts
		var taxDeferredReturn, rothReturn, taxableReturn float64
		if s.InvestmentReturn != 0 {
			// Override mode: use single rate for all accounts
			taxDeferredReturn = s.InvestmentReturn
			rothReturn = s.InvestmentReturn
			taxableReturn = s.InvestmentReturn
		} else {
			// Per-account allocation mode: calculate blended returns from conservative estimates
			// Using forward-looking estimates (7% stocks, 4% bonds, 3% cash) rather than
			// historical averages (~10.5% stocks) for more prudent retirement planning
			stockMean, bondMean, cashMean := 7.0, 4.0, 3.0
			tdStock, tdBond, tdCash, rothStock, rothBond, rothCash, taxStock, taxBond, taxCash := s.GetAllocationAtYear(currentYear)
			taxDeferredReturn = models.GetBlendedReturn(tdStock, tdBond, tdCash, stockMean, bondMean, cashMean)
			rothReturn = models.GetBlendedReturn(rothStock, rothBond, rothCash, stockMean, bondMean, cashMean)
			taxableReturn = models.GetBlendedReturn(taxStock, taxBond, taxCash, stockMean, bondMean, cashMean)
		}

		// Convert annual returns to monthly using geometric formula (not simple division)
		// Simple division inflates effective returns when compounded monthly
		taxDeferredMonthly := math.Pow(1+taxDeferredReturn/100, 1.0/12) - 1
		rothMonthly := math.Pow(1+rothReturn/100, 1.0/12) - 1
		taxableComponents := BuildTaxableReturnComponents(taxableReturn, s)
		totalGrowth := 0.0
		irmaaEligibleAdults := MedicareEligibleAdultCountAtYear(s, currentYear)
		irmaaInflationFactor := PlannerIRMAAInflationFactorForYear(s.InflationRate, float64(currentYear))

		// F-074: apply the full annual RMD only in the trigger month for
		// the user's selected timing. Other months withdraw 0.
		monthlyRMD = 0
		if annualRMD > 0 && monthInYear == RMDTriggerMonth(s.RMDTiming) {
			monthlyRMD = math.Min(annualRMD, taxDeferredBalance)
		}

		monthResult := ExecuteTaxAwarePortfolioMonth(
			totalExpensesAcc,
			incomeBreakdown,
			monthlyRMD,
			allowTaxDeferredWithdrawal,
			penaltyRate,
			&taxDeferredBalance,
			&taxableAccount,
			&rothBalance,
			taxDeferredMonthly,
			rothMonthly,
			taxableComponents,
			s.GetProjectionTiming(),
			taxState,
			taxCalculator,
			currentYear,
			monthInYear,
			rothConversionThisMonth,
			completedMAGIHistory,
			irmaaEligibleAdults,
			irmaaInflationFactor,
		)
		totalGrowth = monthResult.TotalGrowth
		shortfall := monthResult.Shortfall
		cashFlow := monthResult.CashFlow
		taxesPaid := monthResult.TaxesPaid
		totalExpensesAcc += monthResult.IRMAAExpense
		plannedTotalExpenses += monthResult.IRMAAExpense
		currentYearTaxSnapshot = monthResult.TaxSnapshot

		taxState.ApplyMonth(
			incomeBreakdown.OrdinaryIncome+monthResult.TaxableNonQualifiedDividends,
			incomeBreakdown.SocialSecurityIncome,
			cashFlow.WithdrawalFromTaxDeferred,
			monthResult.TaxableQualifiedDividends,
			monthResult.TaxableCapitalGains,
			monthResult.TaxableNonQualifiedDividends,
			rothConversionThisMonth,
			taxesPaid,
		)

		grossIncome := totalIncomeMonth + monthResult.TaxableQualifiedDividends + monthResult.TaxableNonQualifiedDividends + monthResult.TaxableCapitalGainsDistributions + cashFlow.WithdrawalFromTaxDeferred + cashFlow.WithdrawalFromTaxable + cashFlow.WithdrawalFromRoth
		netIncome := grossIncome - taxesPaid
		currentYearSummary.Growth += totalGrowth
		currentYearSummary.GrossIncome += grossIncome
		currentYearSummary.Taxes += taxesPaid
		currentYearSummary.Expenses += totalExpensesAcc
		currentYearSummary.PlannedExpenses += plannedTotalExpenses
		currentYearSummary.Withdrawals += cashFlow.ActualWithdrawal

		totalBalance := taxDeferredBalance + rothBalance + taxableAccount.MarketValue
		depleted := false
		if totalBalance <= 0 {
			taxDeferredBalance = 0
			rothBalance = 0
			taxableAccount = NewTaxableAccountState(s, 0)
			totalBalance = 0
			depleted = true
			if depletionMonth == nil {
				dm := m
				depletionMonth = &dm
				ly := float64(m) / 12
				longevityYears = &ly
			}
		} else if ShortfallIsTemporaryDueToDelay(shortfall, allowTaxDeferredWithdrawal, taxDeferredBalance) {
			// Temporary shortfall (e.g., accessible accounts empty but locked accounts
			// still have funds during a tax-deferred delay). Not true depletion — the
			// portfolio has money, it's just not accessible this month. Don't stop.
		} else if ShortfallCausesDepletion(shortfall, allowTaxDeferredWithdrawal, taxDeferredBalance) {
			depleted = true
			if depletionMonth == nil {
				dm := m
				depletionMonth = &dm
				ly := float64(m) / 12
				longevityYears = &ly
			}
		}

		projection = append(projection, models.ProjectionMonth{
			Month:                     m,
			Year:                      float64(m) / 12,
			CumulativeInflation:       cumulativeInflation,
			PortfolioBalance:          totalBalance,
			PortfolioBalanceReal:      totalBalance / cumulativeInflation,
			TaxDeferredBalance:        taxDeferredBalance,
			TaxableBalance:            taxableAccount.MarketValue,
			RothBalance:               rothBalance,
			GeneralExpenses:           currentLivingExpenses,
			HealthcareExpense:         activeHealthcare,
			TotalExpenses:             totalExpensesAcc,
			TotalExpensesReal:         totalExpensesAcc / cumulativeInflation,
			TotalIncome:               totalIncomeMonth + monthResult.TaxableIncomeBeforeCashFlow,
			TotalIncomeReal:           (totalIncomeMonth + monthResult.TaxableIncomeBeforeCashFlow) / cumulativeInflation,
			GrossIncome:               grossIncome,
			NetIncome:                 netIncome,
			TaxesPaid:                 taxesPaid,
			NetWithdrawal:             cashFlow.ActualWithdrawal,
			RMDWithdrawal:             cashFlow.RMDWithdrawal,
			TaxableWithdrawals:        cashFlow.WithdrawalFromTaxDeferred,
			RothConversions:           rothConversionThisMonth,
			PortfolioGrowth:           totalGrowth,
			Depleted:                  depleted,
			WithdrawalFromTaxDeferred: cashFlow.WithdrawalFromTaxDeferred,
			WithdrawalFromTaxable:     cashFlow.WithdrawalFromTaxable,
			WithdrawalFromRoth:        cashFlow.WithdrawalFromRoth,
			PlannedLivingExpenses:     currentLivingExpenses,
			GuardrailMultiplier:       activeMultiplier,
		})

		if depleted {
			break
		}
	}

	finalBalance := 0.0
	if len(projection) > 0 {
		finalBalance = projection[len(projection)-1].PortfolioBalance
		finalizeCurrentYear(projection[len(projection)-1])
	}

	return &models.ProjectionResult{
		Months:          projection,
		YearlySummaries: yearlySummaries,
		LongevityYears:  longevityYears,
		FinalBalance:    finalBalance,
		DepletionMonth:  depletionMonth,
		Survives:        depletionMonth == nil,
		GuardrailEvents: guardrailEvents,
	}
}

// FindSteadyStateMonth finds the month when all income sources are
// active. Returns 0 if all sources start immediately (no delayed
// income).
func FindSteadyStateMonth(s *models.WhatIfSettings) int {
	maxStartMonth := 0

	for _, source := range s.IncomeSources {
		if source.StartMonth > maxStartMonth {
			// Make sure this source will actually be active (hasn't ended)
			if source.EndMonth == nil || *source.EndMonth > source.StartMonth {
				maxStartMonth = source.StartMonth
			}
		}
	}

	if SocialSecurityProjectionActive(s) {
		ss := s.SocialSecurity
		if ss != nil && ss.FRABenefit > 0 {
			if startMonth := ssClaimStartMonth(s.CurrentAge, ss.ClaimAge); startMonth > maxStartMonth {
				maxStartMonth = startMonth
			}
		}
		if ss != nil && s.HasSpouse() && ss.SpouseFRABenefit > 0 && ssValidClaimAge(ss.SpouseClaimAge) {
			if startMonth := ssClaimStartMonth(s.SpouseAge, ss.SpouseClaimAge); startMonth > maxStartMonth {
				maxStartMonth = startMonth
			}
		}
	}

	// Cap at projection length
	maxMonth := s.ProjectionYears * 12
	if maxStartMonth > maxMonth {
		maxStartMonth = maxMonth
	}

	return maxStartMonth
}

// ssValidClaimAge mirrors retirement.validSSClaimAge (62..70 inclusive).
// Duplicated here to keep FindSteadyStateMonth free of an import cycle.
func ssValidClaimAge(age int) bool {
	return age >= 62 && age <= 70
}

// ssClaimStartMonth mirrors retirement.claimStartMonth: months from the
// current age to the claim age, clamped to 0.
func ssClaimStartMonth(currentAge, claimAge int) int {
	if claimAge <= currentAge {
		return 0
	}
	return (claimAge - currentAge) * 12
}
