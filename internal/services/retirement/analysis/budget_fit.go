package analysis

import (
	"fmt"
	"math"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// BudgetFit analyzes the monthly budget gap (expenses vs. income) at
// month 0 and at a steady-state month when delayed income sources have
// activated. If proj is non-nil, the steady-state Tax-Deferred and
// Taxable balances are read from the projection's per-month state so
// they reflect actual drawdown (RMD, withdrawals) rather than naïve
// compound growth — important at far-out years where decades of RMDs
// would otherwise be ignored. Pass nil to fall back to the closed-form
// compound-growth estimate.
func BudgetFit(in engine.Input, proj *models.ProjectionResult) *models.BudgetFitAnalysis {
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
	// Phase multiplier at month 0, mirroring engine.TotalExpenses so the
	// itemized rows reconcile with the MonthlyExpenses header. Returns 1.0
	// when spending phases are disabled.
	phaseMultiplier := s.GetSpendingMultiplier(s.GetPhaseReferenceAge(0))
	if s.MonthlyLivingExpenses > 0 {
		breakdown = append(breakdown, models.ExpenseBreakdownItem{
			Name: "Living Expenses",
			// Use the engine's phase-/decline-adjusted living expense at
			// month 0, not the raw setting, so this row matches the total.
			Amount: engine.LivingExpensesAtMonth(s, 0),
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
		// engine.TotalExpenses applies the phase multiplier to discretionary
		// sources; match it here so the row reconciles with the total.
		if source.Discretionary {
			amt *= phaseMultiplier
		}
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
		// Social Security sources are surfaced once below via the aggregated
		// SocialSecurityIncome row (CalculateMonthlyIncomeBreakdown routes them
		// into the SS bucket). Skip them here so they aren't listed twice when
		// the optimizer is inactive, nor shown as a phantom row that isn't part
		// of the income total when the optimizer overrides them.
		if engine.IsSocialSecurityIncomeSource(source) {
			continue
		}
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
	// Social Security is computed separately from s.IncomeSources (it comes
	// from the SS-optimizer hook or manual SS-typed sources that are pulled
	// into the SS bucket by CalculateMonthlyIncomeBreakdown). Surface it as
	// its own breakdown row so the user sees where their income comes from.
	if incomeSummary.SocialSecurityIncome > 0 {
		incomeItems = append(incomeItems, models.ExpenseBreakdownItem{
			Name:   "Social Security",
			Amount: incomeSummary.SocialSecurityIncome,
		})
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
		// Proportional split: each bucket's net contribution to the gap
		// matches its share of the portfolio allocation. Net amounts sum
		// to monthlyGap. Gross amounts may exceed net (Tax-Deferred
		// grosses up for ordinary-income tax).
		pTD, pTX, pR := withdrawalMixShares(s)
		result.NetWithdrawalTaxDeferred = monthlyGap * pTD
		result.NetWithdrawalTaxable = monthlyGap * pTX
		result.NetWithdrawalRoth = monthlyGap * pR

		// Roth: no tax, gross = net.
		result.GrossWithdrawalRoth = result.NetWithdrawalRoth

		// Tax-deferred: simulate adding the TD-share to RMD (ordinary-income).
		if result.NetWithdrawalTaxDeferred > 0 {
			tdSnap := estimateTaxSnapshot(0, taxableCashFlow, monthlyRMD+result.NetWithdrawalTaxDeferred, rothConversionThisMonth, &currentIRMALookbackMAGI)
			extraTax := tdSnap.MonthlyTax - currentSnapshot.MonthlyTax
			marginal := extraTax / result.NetWithdrawalTaxDeferred
			if marginal < 0 {
				marginal = 0
			}
			if marginal > 0.95 {
				marginal = 0.95
			}
			result.MarginalRateTaxDeferred = marginal * 100
			result.GrossWithdrawalTaxDeferred = result.NetWithdrawalTaxDeferred / (1 - marginal)
		}

		// Taxable: at month 0 cost basis ≈ market value (engine initializes
		// CostBasis = MarketValue), so an additional sale incurs no LTCG.
		result.EffectiveRateTaxable = 0
		result.GrossWithdrawalTaxable = result.NetWithdrawalTaxable
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

	// Override always wins when non-negative. The slider posts its current
	// value (0..ProjectionYears) and the user expects to view exactly that
	// year — including year 0 (Current values) and years below the
	// auto-calculated steady-state when income is still ramping in.
	steadyStateYear := minSteadyStateYear
	if s.SteadyStateOverrideYear >= 0 {
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
		// Prefer the projection's actual per-month balances (they reflect
		// drawdown from RMDs and withdrawals); fall back to closed-form
		// compound growth when no projection was passed.
		estimatedTaxDeferred, steadyStateTaxableBalance := bucketBalancesAt(proj, steadyStateMonth, s, effectiveReturn, taxableAnnualReturn, taxableMarketValue)
		steadyStateTaxableCashFlow := engine.ExpectedTaxableMonthlyCashFlow(s, steadyStateTaxableBalance, taxableAnnualReturn)
		result.SteadyStateIncome += steadyStateTaxableCashFlow.QualifiedDividends + steadyStateTaxableCashFlow.NonQualifiedDividends + steadyStateTaxableCashFlow.CapitalGainsDistributions

		// F-078: gate on calendar year + use age-at-year-end for the divisor.
		steadyStateCalendarYear := engine.ParseStartYear(s.StartDate) + (steadyStateMonth / 12)
		if engine.RMDApplies(s, steadyStateCalendarYear) && s.TaxDeferredPercent > 0 {
			annualRMD, _ := engine.CalculateRMD(estimatedTaxDeferred, engine.RMDAgeForCalendarYear(s, steadyStateCalendarYear))
			result.SteadyStateRMD = annualRMD / 12
		}

		steadyStateRothConversion := engine.RothConversionAmountForYear(s, steadyStateMonth/12, estimatedTaxDeferred)
		steadyStateIRMALookbackMAGI := (*float64)(nil)
		if steadyStateMonth >= 24 {
			lookbackMonth := steadyStateMonth - 24
			lookbackTaxDeferred, lookbackTaxableBalance := bucketBalancesAt(proj, lookbackMonth, s, effectiveReturn, taxableAnnualReturn, taxableMarketValue)
			lookbackTaxableCashFlow := engine.ExpectedTaxableMonthlyCashFlow(s, lookbackTaxableBalance, taxableAnnualReturn)

			lookbackCalendarYear := engine.ParseStartYear(s.StartDate) + (lookbackMonth / 12)
			lookbackRMD := 0.0
			// F-078: calendar-year gate + age-at-year-end divisor.
			if engine.RMDApplies(s, lookbackCalendarYear) && s.TaxDeferredPercent > 0 {
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

		// Suggested withdrawal mix at steady state (only when gap > 0).
		// Proportional split across allocation; net amounts sum to gap.
		if result.SteadyStateGap > 0 {
			pTDss, pTXss, pRss := withdrawalMixShares(s)
			result.SteadyStateNetWithdrawalTaxDeferred = result.SteadyStateGap * pTDss
			result.SteadyStateNetWithdrawalTaxable = result.SteadyStateGap * pTXss
			result.SteadyStateNetWithdrawalRoth = result.SteadyStateGap * pRss

			// Roth: no tax, gross = net.
			result.SteadyStateGrossWithdrawalRoth = result.SteadyStateNetWithdrawalRoth

			// Tax-deferred: simulate extra ordinary withdrawal at steady state for the TD share.
			if result.SteadyStateNetWithdrawalTaxDeferred > 0 {
				tdSnapSS := estimateTaxSnapshot(steadyStateMonth, steadyStateTaxableCashFlow, result.SteadyStateRMD+result.SteadyStateNetWithdrawalTaxDeferred, steadyStateRothConversion, steadyStateIRMALookbackMAGI)
				extraTaxSS := tdSnapSS.MonthlyTax - steadyStateSnapshot.MonthlyTax
				marginalSS := extraTaxSS / result.SteadyStateNetWithdrawalTaxDeferred
				if marginalSS < 0 {
					marginalSS = 0
				}
				if marginalSS > 0.95 {
					marginalSS = 0.95
				}
				result.SteadyStateMarginalRateTaxDeferred = marginalSS * 100
				result.SteadyStateGrossWithdrawalTaxDeferred = result.SteadyStateNetWithdrawalTaxDeferred / (1 - marginalSS)
			}

			// Taxable: gain fraction grows with time; gross-up the TX share.
			if result.SteadyStateNetWithdrawalTaxable > 0 {
				yearsToSteadyState := float64(steadyStateMonth) / 12
				gainFractionSS := 1.0 - math.Pow(1.0+taxableAnnualReturn/100.0, -yearsToSteadyState)
				if gainFractionSS < 0 {
					gainFractionSS = 0
				}
				taxableExtraSS := steadyStateTaxableCashFlow
				taxableExtraSS.CapitalGainsDistributions += result.SteadyStateNetWithdrawalTaxable * gainFractionSS
				txSnapSS := estimateTaxSnapshot(steadyStateMonth, taxableExtraSS, result.SteadyStateRMD, steadyStateRothConversion, steadyStateIRMALookbackMAGI)
				txExtraTaxSS := txSnapSS.MonthlyTax - steadyStateSnapshot.MonthlyTax
				txEffectiveSS := txExtraTaxSS / result.SteadyStateNetWithdrawalTaxable
				if txEffectiveSS < 0 {
					txEffectiveSS = 0
				}
				if txEffectiveSS > 0.95 {
					txEffectiveSS = 0.95
				}
				result.SteadyStateEffectiveRateTaxable = txEffectiveSS * 100
				result.SteadyStateGrossWithdrawalTaxable = result.SteadyStateNetWithdrawalTaxable / (1 - txEffectiveSS)
			}
		}

		// Calculate steady state withdrawal rate. Divide the gap by the
		// projection's actual portfolio balance (which reflects drawdown
		// from withdrawals/RMDs), consistent with the proj-based gap above;
		// fall back to compound growth only when no projection is available.
		if s.PortfolioValue > 0 && result.SteadyStateGap > 0 {
			estimatedPortfolio := steadyStatePortfolioBalance(proj, steadyStateMonth, s, effectiveReturn)
			if estimatedPortfolio > 0 {
				result.SteadyStateRate = (result.SteadyStateGap * 12 / estimatedPortfolio) * 100
			}
		}
	} else {
		// At year 0 the steady-state panel mirrors the Current (Today)
		// values so the slider can slide back to 0 and still show real
		// numbers rather than a column of zeros.
		result.SteadyStateExpenses = result.MonthlyExpenses
		result.SteadyStateIncome = result.MonthlyIncome
		result.SteadyStateGrossIncome = result.GrossIncome
		result.SteadyStateNetIncome = result.NetIncome
		result.SteadyStateTaxes = result.MonthlyTaxes
		result.SteadyStateStateTax = result.MonthlyStateTax
		result.SteadyStateNIIT = result.MonthlyNIIT
		result.SteadyStateIRMAA = result.MonthlyIRMAA
		result.SteadyStateTaxableSocialSecurityPct = result.TaxableSocialSecurityPct
		result.SteadyStateRMD = result.MonthlyRMD
		result.SteadyStateGap = result.MonthlyGap
		result.SteadyStateRate = result.RequiredRate
		result.SteadyStateGrossWithdrawalTaxDeferred = result.GrossWithdrawalTaxDeferred
		result.SteadyStateNetWithdrawalTaxDeferred = result.NetWithdrawalTaxDeferred
		result.SteadyStateMarginalRateTaxDeferred = result.MarginalRateTaxDeferred
		result.SteadyStateGrossWithdrawalTaxable = result.GrossWithdrawalTaxable
		result.SteadyStateNetWithdrawalTaxable = result.NetWithdrawalTaxable
		result.SteadyStateEffectiveRateTaxable = result.EffectiveRateTaxable
		result.SteadyStateGrossWithdrawalRoth = result.GrossWithdrawalRoth
		result.SteadyStateNetWithdrawalRoth = result.NetWithdrawalRoth
	}

	return result
}

// bucketBalancesAt returns the tax-deferred and taxable balances at the
// given month. When proj is non-nil and the month is within the
// projection's range, the actual simulated balances are used (these
// reflect drawdown from RMDs, withdrawals, and growth over time).
// Otherwise the legacy closed-form compound-growth estimate is used,
// which overstates balances at far-out years because it ignores
// withdrawals.
func bucketBalancesAt(
	proj *models.ProjectionResult,
	month int,
	s *models.WhatIfSettings,
	effectiveReturn, taxableAnnualReturn, taxableMarketValue float64,
) (taxDeferred, taxable float64) {
	if proj != nil && len(proj.Months) > 0 && month >= 0 {
		// The slider's max equals ProjectionYears, which maps to one
		// month past the end of the simulation. Clamp to the final
		// projected month so the steady-state view stays consistent
		// with the projection's last data point instead of silently
		// falling back to inflated compound-growth values.
		idx := month
		if idx >= len(proj.Months) {
			idx = len(proj.Months) - 1
		}
		m := proj.Months[idx]
		return m.TaxDeferredBalance, m.TaxableBalance
	}
	yearsToMonth := float64(month) / 12
	taxDeferred = s.PortfolioValue * (s.TaxDeferredPercent / 100) * math.Pow(1+effectiveReturn/100, yearsToMonth)
	taxable = taxableMarketValue * math.Pow(1+taxableAnnualReturn/100, yearsToMonth)
	return taxDeferred, taxable
}

// steadyStatePortfolioBalance returns the total portfolio value at the
// given month, used as the steady-state withdrawal-rate denominator.
// Prefers the projection's actual balance (which reflects drawdown from
// withdrawals/RMDs) and falls back to closed-form compound growth when no
// projection is available — mirroring bucketBalancesAt's clamp so the rate
// stays consistent with the gap.
func steadyStatePortfolioBalance(proj *models.ProjectionResult, month int, s *models.WhatIfSettings, effectiveReturn float64) float64 {
	if proj != nil && len(proj.Months) > 0 && month >= 0 {
		idx := month
		if idx >= len(proj.Months) {
			idx = len(proj.Months) - 1
		}
		return proj.Months[idx].PortfolioBalance
	}
	yearsToMonth := float64(month) / 12
	return s.PortfolioValue * math.Pow(1+effectiveReturn/100, yearsToMonth)
}

// withdrawalMixShares returns the proportional split of a gap-closing
// withdrawal across (tax-deferred, taxable, Roth) buckets, derived from
// the user's portfolio allocation. The three values always sum to 1.
// When the declared allocation totals more than 100% (e.g. mis-configured
// settings imported outside the form) the taxable share is clamped to
// zero and the tax-deferred / Roth shares are scaled down proportionally
// so the contract holds.
func withdrawalMixShares(s *models.WhatIfSettings) (pTD, pTX, pR float64) {
	pTD = s.TaxDeferredPercent / 100
	pR = s.RothPercent / 100
	pTX = 1 - pTD - pR
	if pTX < 0 {
		pTX = 0
		if sum := pTD + pR; sum > 0 {
			pTD /= sum
			pR /= sum
		}
	}
	return pTD, pTX, pR
}
