package analysis

import (
	"fmt"
	"math"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// BudgetFit analyzes the monthly budget gap (expenses vs. income) at
// month 0 and at a steady-state month when delayed income sources have
// activated. Runs no projection of its own — all estimates come from
// closed-form helpers in the engine package (taxable cash-flow,
// projection-tax accumulator, RMD math).
func BudgetFit(in engine.Input) *models.BudgetFitAnalysis {
	s := in.Prepared.Settings()

	estimateTaxSnapshot := func(targetMonth int, taxableCashFlow engine.TaxableGrowthResult, monthlyRMD float64, rothConversion float64, assumedIRMALookbackMAGI *float64) engine.ProjectedTaxSnapshot {
		targetYear := targetMonth / 12
		incomeBreakdown := engine.CalculateMonthlyIncomeBreakdown(in.Hooks, s, targetMonth)
		taxState := engine.ProjectionTaxAccumulator{}
		taxCalculator := engine.NewTaxCalculator(s.TaxConfig, s.InflationRate)

		return taxState.EstimateMonthlySnapshot(
			taxCalculator,
			targetYear,
			targetMonth%12,
			incomeBreakdown.OrdinaryIncome+taxableCashFlow.NonQualifiedDividends,
			incomeBreakdown.SocialSecurityIncome,
			monthlyRMD,
			taxableCashFlow.QualifiedDividends,
			taxableCashFlow.CapitalGainsDistributions,
			taxableCashFlow.NonQualifiedDividends,
			rothConversion,
			nil,
			assumedIRMALookbackMAGI,
			engine.MedicareEligibleAdultCountAtYear(s, targetYear),
			engine.PlannerIRMAAInflationFactorForYear(s.InflationRate, float64(targetMonth)/12),
		)
	}

	// Calculate first month expenses and income
	baseMonthlyExpenses := engine.TotalExpenses(s, 0)
	incomeSummary := engine.CalculateMonthlyIncomeBreakdown(in.Hooks, s, 0)
	monthlyIncome := incomeSummary.TotalIncome

	// Build expense breakdown for transparency
	var breakdown []models.ExpenseBreakdownItem
	if s.MonthlyLivingExpenses > 0 {
		breakdown = append(breakdown, models.ExpenseBreakdownItem{
			Name:   "Living Expenses",
			Amount: s.MonthlyLivingExpenses,
		})
	}
	healthcareCost := s.GetTotalHealthcareCost(0)
	if healthcareCost > 0 {
		breakdown = append(breakdown, models.ExpenseBreakdownItem{
			Name:   "Healthcare",
			Amount: healthcareCost,
		})
	} else if len(s.HealthcarePersons) > 0 {
		// Show healthcare even when $0 so user knows it's tracked
		note := "employer covered"
		for _, p := range s.HealthcarePersons {
			if p.EmployerCoverageYears > 0 {
				note = fmt.Sprintf("employer covered (%d yr)", p.EmployerCoverageYears)
				break
			}
		}
		breakdown = append(breakdown, models.ExpenseBreakdownItem{
			Name:   "Healthcare",
			Amount: 0,
			Note:   note,
		})
	}
	if s.MonthlyPropertyTax > 0 {
		breakdown = append(breakdown, models.ExpenseBreakdownItem{
			Name:   "Property Tax",
			Amount: s.MonthlyPropertyTax,
		})
	}
	for _, source := range s.ExpenseSources {
		amt := source.GetAdjustedAmount(0, s.InflationRate)
		note := ""
		if source.EndYear > 0 {
			note = fmt.Sprintf("ends year %d", source.EndYear)
		}
		breakdown = append(breakdown, models.ExpenseBreakdownItem{
			Name:   source.Name,
			Amount: amt,
			Note:   note,
		})
	}

	// Build income breakdown
	var incomeItems []models.ExpenseBreakdownItem
	for _, source := range s.IncomeSources {
		amt := source.GetAdjustedAmount(0)
		if amt > 0 {
			note := ""
			if source.StartMonth > 0 {
				note = fmt.Sprintf("starts year %d", source.StartMonth/12)
			}
			incomeItems = append(incomeItems, models.ExpenseBreakdownItem{
				Name:   source.Name,
				Amount: amt,
				Note:   note,
			})
		}
	}

	taxableAnnualReturn := s.InvestmentReturn
	if taxableAnnualReturn == 0 {
		stockMean, bondMean, cashMean := 7.0, 4.0, 3.0
		_, _, _, _, _, _, taxStock, taxBond, taxCash := s.GetAllocationAtYear(0)
		taxableAnnualReturn = models.GetBlendedReturn(taxStock, taxBond, taxCash, stockMean, bondMean, cashMean)
	}
	taxableMarketValue := s.PortfolioValue * math.Max(0, (100-s.TaxDeferredPercent-s.RothPercent)/100)
	taxableCashFlow := engine.ExpectedTaxableMonthlyCashFlow(s, taxableMarketValue, taxableAnnualReturn)
	taxableIncome := taxableCashFlow.QualifiedDividends + taxableCashFlow.NonQualifiedDividends + taxableCashFlow.CapitalGainsDistributions
	if taxableIncome > 0 {
		monthlyIncome += taxableIncome
		incomeItems = append(incomeItems, models.ExpenseBreakdownItem{
			Name:   "Taxable Distributions",
			Amount: taxableIncome,
			Note:   "assumed monthly dividends/gain distributions",
		})
	}

	// F-078: first-year snapshot gates on calendar year vs FirstRMDCalendarYear
	// and uses RMDAgeForCalendarYear so a household where the older person
	// turns 73 later this calendar year still produces a non-zero current
	// snapshot (and uses the right UL Table row).
	monthlyRMD := 0.0
	currentCalendarYear := engine.ParseStartYear(s.StartDate)
	if engine.RMDApplies(s, currentCalendarYear) && s.TaxDeferredPercent > 0 {
		taxDeferredBalance := s.PortfolioValue * (s.TaxDeferredPercent / 100)
		annualRMD, _ := engine.CalculateRMD(taxDeferredBalance, engine.RMDAgeForCalendarYear(s, currentCalendarYear))
		monthlyRMD = annualRMD / 12
	}

	rothConversionThisMonth := engine.RothConversionAmountForYear(s, 0, s.PortfolioValue*(s.TaxDeferredPercent/100))
	currentSnapshotBeforeRMD := estimateTaxSnapshot(0, taxableCashFlow, 0, rothConversionThisMonth, nil)
	currentIRMALookbackMAGI := currentSnapshotBeforeRMD.AnnualMAGI
	currentSnapshotBeforeRMD = estimateTaxSnapshot(0, taxableCashFlow, 0, rothConversionThisMonth, &currentIRMALookbackMAGI)
	currentSnapshot := estimateTaxSnapshot(0, taxableCashFlow, monthlyRMD, rothConversionThisMonth, &currentIRMALookbackMAGI)
	monthlyTaxesBeforeRMD := currentSnapshotBeforeRMD.MonthlyTax
	monthlyTaxes := currentSnapshot.MonthlyTax
	monthlyExpenses := baseMonthlyExpenses + currentSnapshot.MonthlyIRMAA
	grossIncome := monthlyIncome + monthlyRMD
	netIncome := grossIncome - monthlyTaxes
	netRMD := math.Max(0, monthlyRMD-(monthlyTaxes-monthlyTaxesBeforeRMD))

	// Calculate gap before RMD (what's the shortfall from income alone?)
	gapBeforeRMD := baseMonthlyExpenses + currentSnapshotBeforeRMD.MonthlyIRMAA - monthlyIncome + monthlyTaxesBeforeRMD

	// Calculate how RMD affects the gap
	var rmdCoverage, excessRMD float64
	if netRMD > 0 {
		if gapBeforeRMD > 0 {
			// There's a shortfall - RMD can help cover it
			if netRMD >= gapBeforeRMD {
				// RMD fully covers the gap and then some
				rmdCoverage = gapBeforeRMD
				excessRMD = netRMD - gapBeforeRMD
			} else {
				// RMD partially covers the gap
				rmdCoverage = netRMD
				excessRMD = 0
			}
		} else {
			// No shortfall (income covers expenses) - all RMD is excess
			rmdCoverage = 0
			excessRMD = netRMD
		}
	}

	// Gap = Expenses - after-tax cash flow (income + RMD - taxes)
	monthlyGap := monthlyExpenses - netIncome
	annualGap := monthlyGap * 12

	// Calculate required withdrawal rate (only for positive gap after RMD)
	requiredRate := 0.0
	if s.PortfolioValue > 0 && monthlyGap > 0 {
		requiredRate = (annualGap / s.PortfolioValue) * 100
	}

	result := &models.BudgetFitAnalysis{
		MonthlyExpenses:          monthlyExpenses,
		MonthlyIncome:            monthlyIncome,
		GrossIncome:              grossIncome,
		NetIncome:                netIncome,
		MonthlyTaxes:             monthlyTaxes,
		MonthlyStateTax:          currentSnapshot.MonthlyStateTax,
		MonthlyNIIT:              currentSnapshot.AnnualNIIT / 12,
		MonthlyIRMAA:             currentSnapshot.MonthlyIRMAA,
		TaxableSocialSecurityPct: currentSnapshot.TaxableSocialSecurityPct,
		MonthlyRMD:               monthlyRMD,
		MonthlyGap:               monthlyGap,
		AnnualGap:                annualGap,
		RequiredRate:             requiredRate,
		ExpenseBreakdown:         breakdown,
		IncomeBreakdown:          incomeItems,
		GapBeforeRMD:             gapBeforeRMD,
		RMDCoverage:              rmdCoverage,
		ExcessRMD:                excessRMD,
	}
	if monthlyGap > 0 {
		result.GrossWithdrawalRoth = monthlyGap

		// Tax-deferred: simulate adding the gap to RMD (ordinary-income withdrawal).
		tdSnap := estimateTaxSnapshot(0, taxableCashFlow, monthlyRMD+monthlyGap, rothConversionThisMonth, &currentIRMALookbackMAGI)
		extraTax := tdSnap.MonthlyTax - currentSnapshot.MonthlyTax
		marginal := extraTax / monthlyGap
		if marginal < 0 {
			marginal = 0
		}
		if marginal > 0.95 {
			marginal = 0.95
		}
		result.MarginalRateTaxDeferred = marginal * 100
		result.GrossWithdrawalTaxDeferred = monthlyGap / (1 - marginal)

		// Taxable: at month 0 cost basis ≈ market value, so simulated gain fraction is 0.
		// Compute via simulation for consistency with steady-state path; result will be ≈ gap.
		taxableExtra := taxableCashFlow
		taxableGainFractionCurrent := 0.0 // basis = market at month 0
		taxableExtra.CapitalGainsDistributions += monthlyGap * taxableGainFractionCurrent
		txSnap := estimateTaxSnapshot(0, taxableExtra, monthlyRMD, rothConversionThisMonth, &currentIRMALookbackMAGI)
		txExtraTax := txSnap.MonthlyTax - currentSnapshot.MonthlyTax
		txEffective := txExtraTax / monthlyGap
		if txEffective < 0 {
			txEffective = 0
		}
		if txEffective > 0.95 {
			txEffective = 0.95
		}
		result.EffectiveRateTaxable = txEffective * 100
		result.GrossWithdrawalTaxable = monthlyGap / (1 - txEffective)
	}
	if currentSnapshot.MonthlyIRMAA > 0 {
		result.ExpenseBreakdown = append(result.ExpenseBreakdown, models.ExpenseBreakdownItem{
			Name:   "IRMAA Surcharge",
			Amount: currentSnapshot.MonthlyIRMAA,
			Note:   "estimated per Medicare-eligible adult",
		})
	}

	// Calculate steady-state analysis (when all income sources are active)
	minSteadyStateMonth := engine.FindSteadyStateMonth(in.Hooks, s)
	minSteadyStateYear := float64(minSteadyStateMonth) / 12

	// Use override year if set and >= minimum, otherwise use auto-calculated
	steadyStateYear := minSteadyStateYear
	if s.SteadyStateOverrideYear > 0 && s.SteadyStateOverrideYear >= minSteadyStateYear {
		steadyStateYear = s.SteadyStateOverrideYear
	}
	steadyStateMonth := int(steadyStateYear * 12)

	// Always show steady state section (allows sliding forward even if no delayed income)
	result.HasSteadyState = true
	result.MinSteadyStateYear = minSteadyStateYear
	result.SteadyStateMonth = steadyStateMonth
	result.SteadyStateYear = steadyStateYear

	if steadyStateMonth > 0 {
		// Calculate expenses and income at steady state
		baseSteadyStateExpenses := engine.TotalExpenses(s, steadyStateMonth)
		result.SteadyStateExpenses = baseSteadyStateExpenses
		steadyStateIncomeBreakdown := engine.CalculateMonthlyIncomeBreakdown(in.Hooks, s, steadyStateMonth)
		result.SteadyStateIncome = steadyStateIncomeBreakdown.TotalIncome

		// Determine effective annual return (allocation-derived when InvestmentReturn is 0)
		effectiveReturn := s.InvestmentReturn
		if effectiveReturn == 0 {
			effectiveReturn = s.GetExpectedReturnFromAllocation()
		}
		yearsToSteadyState := float64(steadyStateMonth) / 12
		steadyStateTaxableBalance := taxableMarketValue * math.Pow(1+taxableAnnualReturn/100, yearsToSteadyState)
		steadyStateTaxableCashFlow := engine.ExpectedTaxableMonthlyCashFlow(s, steadyStateTaxableBalance, taxableAnnualReturn)
		result.SteadyStateIncome += steadyStateTaxableCashFlow.QualifiedDividends + steadyStateTaxableCashFlow.NonQualifiedDividends + steadyStateTaxableCashFlow.CapitalGainsDistributions

		// F-078: gate on calendar year + use age-at-year-end for the divisor.
		steadyStateCalendarYear := engine.ParseStartYear(s.StartDate) + (steadyStateMonth / 12)
		estimatedTaxDeferred := 0.0
		if engine.RMDApplies(s, steadyStateCalendarYear) && s.TaxDeferredPercent > 0 {
			// Estimate tax-deferred balance at steady state (simplified: assume growth only)
			estimatedTaxDeferred = s.PortfolioValue * (s.TaxDeferredPercent / 100) *
				math.Pow(1+effectiveReturn/100, yearsToSteadyState)
			annualRMD, _ := engine.CalculateRMD(estimatedTaxDeferred, engine.RMDAgeForCalendarYear(s, steadyStateCalendarYear))
			result.SteadyStateRMD = annualRMD / 12
		}

		steadyStateRothConversion := engine.RothConversionAmountForYear(s, steadyStateMonth/12, estimatedTaxDeferred)
		steadyStateIRMALookbackMAGI := (*float64)(nil)
		if steadyStateMonth >= 24 {
			lookbackMonth := steadyStateMonth - 24
			yearsToLookback := float64(lookbackMonth) / 12
			lookbackTaxableBalance := taxableMarketValue * math.Pow(1+taxableAnnualReturn/100, yearsToLookback)
			lookbackTaxableCashFlow := engine.ExpectedTaxableMonthlyCashFlow(s, lookbackTaxableBalance, taxableAnnualReturn)

			lookbackCalendarYear := engine.ParseStartYear(s.StartDate) + (lookbackMonth / 12)
			lookbackTaxDeferred := 0.0
			lookbackRMD := 0.0
			// F-078: calendar-year gate + age-at-year-end divisor.
			if engine.RMDApplies(s, lookbackCalendarYear) && s.TaxDeferredPercent > 0 {
				lookbackTaxDeferred = s.PortfolioValue * (s.TaxDeferredPercent / 100) *
					math.Pow(1+effectiveReturn/100, yearsToLookback)
				annualRMD, _ := engine.CalculateRMD(lookbackTaxDeferred, engine.RMDAgeForCalendarYear(s, lookbackCalendarYear))
				lookbackRMD = annualRMD / 12
			}

			lookbackRothConversion := engine.RothConversionAmountForYear(s, lookbackMonth/12, lookbackTaxDeferred)
			lookbackSnapshot := estimateTaxSnapshot(lookbackMonth, lookbackTaxableCashFlow, lookbackRMD, lookbackRothConversion, nil)
			lookbackMAGI := lookbackSnapshot.AnnualMAGI
			steadyStateIRMALookbackMAGI = &lookbackMAGI
		}

		steadyStateSnapshot := estimateTaxSnapshot(steadyStateMonth, steadyStateTaxableCashFlow, result.SteadyStateRMD, steadyStateRothConversion, steadyStateIRMALookbackMAGI)
		steadyStateTaxes := steadyStateSnapshot.MonthlyTax
		result.SteadyStateExpenses += steadyStateSnapshot.MonthlyIRMAA
		result.SteadyStateGrossIncome = result.SteadyStateIncome + result.SteadyStateRMD
		result.SteadyStateNetIncome = result.SteadyStateGrossIncome - steadyStateTaxes
		result.SteadyStateTaxes = steadyStateTaxes
		result.SteadyStateStateTax = steadyStateSnapshot.MonthlyStateTax
		result.SteadyStateNIIT = steadyStateSnapshot.AnnualNIIT / 12
		result.SteadyStateIRMAA = steadyStateSnapshot.MonthlyIRMAA
		result.SteadyStateTaxableSocialSecurityPct = steadyStateSnapshot.TaxableSocialSecurityPct

		// Calculate steady state gap
		result.SteadyStateGap = result.SteadyStateExpenses - result.SteadyStateNetIncome

		// Gross withdrawal mirrors at steady state (only when gap > 0).
		if result.SteadyStateGap > 0 {
			result.SteadyStateGrossWithdrawalRoth = result.SteadyStateGap

			// Tax-deferred: simulate extra ordinary withdrawal at steady state.
			tdSnapSS := estimateTaxSnapshot(steadyStateMonth, steadyStateTaxableCashFlow, result.SteadyStateRMD+result.SteadyStateGap, steadyStateRothConversion, steadyStateIRMALookbackMAGI)
			extraTaxSS := tdSnapSS.MonthlyTax - steadyStateSnapshot.MonthlyTax
			marginalSS := extraTaxSS / result.SteadyStateGap
			if marginalSS < 0 {
				marginalSS = 0
			}
			if marginalSS > 0.95 {
				marginalSS = 0.95
			}
			result.SteadyStateMarginalRateTaxDeferred = marginalSS * 100
			result.SteadyStateGrossWithdrawalTaxDeferred = result.SteadyStateGap / (1 - marginalSS)

			// Taxable: gain fraction grows with time (smooth approximation 1 - (1+r)^-years).
			gainFractionSS := 1.0 - math.Pow(1.0+taxableAnnualReturn/100.0, -yearsToSteadyState)
			if gainFractionSS < 0 {
				gainFractionSS = 0
			}
			taxableExtraSS := steadyStateTaxableCashFlow
			taxableExtraSS.CapitalGainsDistributions += result.SteadyStateGap * gainFractionSS
			txSnapSS := estimateTaxSnapshot(steadyStateMonth, taxableExtraSS, result.SteadyStateRMD, steadyStateRothConversion, steadyStateIRMALookbackMAGI)
			txExtraTaxSS := txSnapSS.MonthlyTax - steadyStateSnapshot.MonthlyTax
			txEffectiveSS := txExtraTaxSS / result.SteadyStateGap
			if txEffectiveSS < 0 {
				txEffectiveSS = 0
			}
			if txEffectiveSS > 0.95 {
				txEffectiveSS = 0.95
			}
			result.SteadyStateEffectiveRateTaxable = txEffectiveSS * 100
			result.SteadyStateGrossWithdrawalTaxable = result.SteadyStateGap / (1 - txEffectiveSS)
		}

		// Calculate steady state withdrawal rate
		if s.PortfolioValue > 0 && result.SteadyStateGap > 0 {
			// Use estimated portfolio value at steady state
			yearsToSteadyState := float64(steadyStateMonth) / 12
			estimatedPortfolio := s.PortfolioValue * math.Pow(1+effectiveReturn/100, yearsToSteadyState)
			result.SteadyStateRate = (result.SteadyStateGap * 12 / estimatedPortfolio) * 100
		}
	}

	return result
}
