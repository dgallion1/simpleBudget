package engine

import (
	"math"

	"budget2/internal/models"
)

// taxDeferredDelayActive reports whether the tax-deferred-withdrawal
// delay window is still in effect for the given calendar year offset.
func taxDeferredDelayActive(s *models.WhatIfSettings, currentYear int) bool {
	return s.TaxDeferredDelayYears > 0 && currentYear < s.TaxDeferredDelayYears
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

// deterministicMonthReturns supplies the canonical loop's per-month
// returns: the explicit InvestmentReturn override when set, otherwise
// blended returns from conservative forward-looking estimates (7% stocks,
// 4% bonds, 3% cash — rather than ~10.5% historical stock averages — for
// more prudent retirement planning).
func deterministicMonthReturns(s *models.WhatIfSettings, m int) MonthReturns {
	currentYear := m / 12
	var taxDeferredReturn, rothReturn, taxableReturn float64
	if s.InvestmentReturn != 0 {
		taxDeferredReturn = s.InvestmentReturn
		rothReturn = s.InvestmentReturn
		taxableReturn = s.InvestmentReturn
	} else {
		stockMean, bondMean, cashMean := 7.0, 4.0, 3.0
		tdStock, tdBond, tdCash, rothStock, rothBond, rothCash, taxStock, taxBond, taxCash := s.GetAllocationAtYear(currentYear)
		taxDeferredReturn = models.GetBlendedReturn(tdStock, tdBond, tdCash, stockMean, bondMean, cashMean)
		rothReturn = models.GetBlendedReturn(rothStock, rothBond, rothCash, stockMean, bondMean, cashMean)
		taxableReturn = models.GetBlendedReturn(taxStock, taxBond, taxCash, stockMean, bondMean, cashMean)
	}

	// Convert annual returns to monthly using the geometric formula —
	// simple division inflates effective returns when compounded monthly.
	return MonthReturns{
		TaxDeferredMonthly:      math.Pow(1+taxDeferredReturn/100, 1.0/12) - 1,
		RothMonthly:             math.Pow(1+rothReturn/100, 1.0/12) - 1,
		TaxableAnnualPercent:    taxableReturn,
		InflationAnnual:         s.InflationRate / 100,
		NetInflationAnnual:      (s.InflationRate - s.SpendingDeclineRate) / 100,
		HealthcareMultiplier:    1,
		DiscretionaryMultiplier: 1,
	}
}

// runMonthlyLoop is the deterministic monthly projection loop. Given
// an Input (prepared settings + optional chain), it returns a fully
// populated *models.ProjectionResult. Pure function: same in, same
// out.
func runMonthlyLoop(in Input) *models.ProjectionResult {
	st := NewProjectionState(in)
	s := st.Settings()
	months := s.ProjectionYears * 12
	projection := make([]models.ProjectionMonth, 0, months)

	var depletionMonth *int
	var longevityYears *float64
	var guardrailEvents []models.GuardrailEvent

	yearlySummaries := make([]models.ProjectionYearSummary, 0, s.ProjectionYears)
	currentYearSummary := models.ProjectionYearSummary{
		Year:            0,
		StartingBalance: s.PortfolioValue,
	}

	finalizeCurrentYear := func(month models.ProjectionMonth) {
		currentYearSummary.MAGI = st.CurrentYearTaxSnapshot.AnnualMAGI
		currentYearSummary.NIIT = st.CurrentYearTaxSnapshot.AnnualNIIT
		currentYearSummary.IRMAA = st.CurrentYearTaxSnapshot.AnnualIRMAA
		currentYearSummary.TaxableSocialSecurityPct = st.CurrentYearTaxSnapshot.TaxableSocialSecurityPct
		// Measured once per projection year, from the composition the year's
		// final snapshot already computed — never in the monthly loop, which
		// Monte Carlo and backtest also drive.
		yearsFromTaxBase := YearsFromTaxBase(st.Settings(), currentYearSummary.Year)
		currentYearSummary.MarginalRate = st.TaxCalculator.MarginalRateOnOrdinaryIncome(
			st.CurrentYearTaxSnapshot.AnnualInputs, yearsFromTaxBase)
		currentYearSummary.MarginalRateLongTermGain = st.TaxCalculator.MarginalRateOnLongTermGain(
			st.CurrentYearTaxSnapshot.AnnualInputs, yearsFromTaxBase)
		currentYearSummary.EndingBalance = month.PortfolioBalance
		currentYearSummary.EndingBalanceReal = month.PortfolioBalanceReal
		currentYearSummary.CumulativeInflation = month.CumulativeInflation
		currentYearSummary.GuardrailMultiplier = month.GuardrailMultiplier
		yearlySummaries = append(yearlySummaries, currentYearSummary)
	}

	for m := 0; m < months; m++ {
		// Rotate the year summary before the stepper appends this year's
		// MAGI to history and starts the new tax year.
		if m%12 == 0 && m > 0 && len(projection) > 0 {
			finalizeCurrentYear(projection[len(projection)-1])
			currentYearSummary = models.ProjectionYearSummary{
				Year:            m / 12,
				StartingBalance: projection[len(projection)-1].PortfolioBalance,
			}
		}

		out := st.StepMonth(m, deterministicMonthReturns)
		if out.GuardrailEvent != nil {
			guardrailEvents = append(guardrailEvents, *out.GuardrailEvent)
		}

		monthResult := out.Result
		cashFlow := monthResult.CashFlow
		taxesPaid := monthResult.TaxesPaid
		totalIncomeMonth := out.Income.TotalIncome

		grossIncome := totalIncomeMonth + monthResult.TaxableQualifiedDividends + monthResult.TaxableNonQualifiedDividends + monthResult.TaxableCapitalGainsDistributions + cashFlow.WithdrawalFromTaxDeferred + cashFlow.WithdrawalFromTaxable + cashFlow.WithdrawalFromRoth
		netIncome := grossIncome - taxesPaid
		currentYearSummary.Growth += monthResult.TotalGrowth
		currentYearSummary.GrossIncome += grossIncome
		currentYearSummary.Taxes += taxesPaid
		currentYearSummary.Expenses += out.TotalExpenses
		currentYearSummary.PlannedExpenses += out.PlannedExpenses
		currentYearSummary.Withdrawals += cashFlow.ActualWithdrawal
		currentYearSummary.TaxableRothEarnings += monthResult.TaxableRothEarnings

		totalBalance := out.TotalBalance
		depleted := false
		if totalBalance <= 0 {
			st.ZeroBalances()
			totalBalance = 0
			depleted = true
			if depletionMonth == nil {
				dm := m
				depletionMonth = &dm
				ly := float64(m) / 12
				longevityYears = &ly
			}
		} else if ShortfallIsTemporaryDueToDelay(monthResult.Shortfall, out.AllowTaxDeferredWithdrawal, st.TaxDeferredBalance) {
			// Temporary shortfall (e.g., accessible accounts empty but locked accounts
			// still have funds during a tax-deferred delay). Not true depletion — the
			// portfolio has money, it's just not accessible this month. Don't stop.
		} else if ShortfallCausesDepletion(monthResult.Shortfall, out.AllowTaxDeferredWithdrawal, st.TaxDeferredBalance) {
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
			CumulativeInflation:       st.CumulativeInflation,
			PortfolioBalance:          totalBalance,
			PortfolioBalanceReal:      totalBalance / st.CumulativeInflation,
			TaxDeferredBalance:        st.TaxDeferredBalance,
			TaxableBalance:            st.TaxableAccount.MarketValue,
			RothBalance:               st.RothBalance,
			GeneralExpenses:           out.LivingExpenses,
			HealthcareExpense:         out.Healthcare,
			TotalExpenses:             out.TotalExpenses,
			TotalExpensesReal:         out.TotalExpenses / st.CumulativeInflation,
			TotalIncome:               totalIncomeMonth + monthResult.TaxableIncomeBeforeCashFlow,
			TotalIncomeReal:           (totalIncomeMonth + monthResult.TaxableIncomeBeforeCashFlow) / st.CumulativeInflation,
			SocialSecurityIncome:      out.Income.SocialSecurityIncome,
			GrossIncome:               grossIncome,
			NetIncome:                 netIncome,
			TaxesPaid:                 taxesPaid,
			StateTaxPaid:              monthResult.TaxSnapshot.MonthlyStateTax,
			NetWithdrawal:             cashFlow.ActualWithdrawal,
			RMDWithdrawal:             cashFlow.RMDWithdrawal,
			TaxableWithdrawals:        cashFlow.WithdrawalFromTaxDeferred,
			RothConversions:           out.RothConversion,
			PortfolioGrowth:           monthResult.TotalGrowth,
			Depleted:                  depleted,
			WithdrawalFromTaxDeferred: cashFlow.WithdrawalFromTaxDeferred,
			WithdrawalFromTaxable:     cashFlow.WithdrawalFromTaxable,
			WithdrawalFromRoth:        cashFlow.WithdrawalFromRoth,
			PlannedLivingExpenses:     out.LivingExpenses,
			GuardrailMultiplier:       out.GuardrailMultiplier,
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
// income). hooks supplies the SS-projection-active predicate; passing a
// zero Hooks value falls back to "no SS optimizer".
func FindSteadyStateMonth(hooks Hooks, s *models.WhatIfSettings) int {
	maxStartMonth := 0

	for _, source := range s.IncomeSources {
		if source.StartMonth > maxStartMonth {
			// Make sure this source will actually be active (hasn't ended)
			if source.EndMonth == nil || *source.EndMonth > source.StartMonth {
				maxStartMonth = source.StartMonth
			}
		}
	}

	if hooks.SSActive(s) {
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
