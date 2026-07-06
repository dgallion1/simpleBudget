package engine

import (
	"budget2/internal/models"
)

// ProjectionState is the cross-month mutable state shared by every
// projection loop — the canonical deterministic loop (month.go), Monte
// Carlo, and the historical backtest. It owns the account balances, Roth
// basis/5-year clock, expense/inflation accumulators, guardrail state, the
// tax accumulator with its IRMAA MAGI-lookback history, and the scenario
// chain cursor. Loops advance it one month at a time via StepMonth,
// injecting only what legitimately differs between them: the month's
// returns, inflation, and expense adjustments (MonthReturns).
//
// Fields are exported where a loop needs to read them for its own output
// collection (balances, inflation) or, in the canonical loop's depletion
// path, to zero them.
type ProjectionState struct {
	in           Input
	primary      *models.WhatIfSettings
	active       *models.WhatIfSettings
	nextChainIdx int

	TaxDeferredBalance  float64
	RothBalance         float64
	TaxableAccount      TaxableAccountState
	RothBasis           float64
	RothFirstFundedYear int

	// CurrentLivingExpenses is this month's base living expense (inflation-
	// and phase-adjusted, before the guardrail multiplier).
	CurrentLivingExpenses float64
	// CumulativeInflation compounds the injected inflation rate monthly.
	CumulativeInflation float64
	// NetCumulativeInflation compounds (inflation − spending decline),
	// mirroring the no-phase expense accumulation; used to rebase living
	// expenses at chain transitions (F-065).
	NetCumulativeInflation float64

	Guardrails *GuardrailState

	TaxState               ProjectionTaxAccumulator
	TaxCalculator          *TaxCalculator
	CompletedMAGIHistory   []float64
	CurrentYearTaxSnapshot ProjectedTaxSnapshot
	// AssumedLookbackMAGI seeds the IRMAA two-year MAGI lookback for years
	// 0-1, before CompletedMAGIHistory has two entries. It tracks the
	// year-0 MAGI estimate; real history takes over from year 2.
	AssumedLookbackMAGI float64

	// AnnualRMD persists across the year so the trigger-month logic can
	// apply the full year's RMD in a single month (F-074).
	AnnualRMD float64
	// BigTicketRothEarnings carries RothEarningsWithdrawal from the annual
	// big-ticket pass into the first month's PortfolioMonthInput; reset
	// after it is consumed.
	BigTicketRothEarnings float64
}

// MonthReturns carries the per-month inputs that legitimately differ
// between the projection loops. Every field is explicit — the multipliers
// must be 1 (not 0) when unused.
type MonthReturns struct {
	// TaxDeferredMonthly and RothMonthly are monthly compound rates
	// (decimal). TaxableAnnualPercent is the taxable account's total annual
	// return in PERCENT (7.0 = 7%) — the single unit accepted at this seam;
	// BuildTaxableReturnComponents divides by 100 internally.
	TaxDeferredMonthly   float64
	RothMonthly          float64
	TaxableAnnualPercent float64

	// InflationAnnual / NetInflationAnnual are this month's annual
	// inflation and (inflation − spending decline) rates as DECIMALS
	// (0.03 = 3%). The canonical loop passes settings rates /100; Monte
	// Carlo folds in its annual variation; the backtest passes historical
	// rates.
	InflationAnnual    float64
	NetInflationAnnual float64

	// HealthcareMultiplier scales the month's healthcare cost (Monte
	// Carlo's annual variation); 1 elsewhere.
	HealthcareMultiplier float64
	// DiscretionaryMultiplier scales discretionary expense sources after
	// the phase multiplier (Monte Carlo's adaptive-spending cut); 1
	// elsewhere.
	DiscretionaryMultiplier float64
	// ExtraExpenses is added after all expense sources (Monte Carlo's
	// spending/health shocks); 0 elsewhere.
	ExtraExpenses float64
}

// MonthOutcome reports one stepped month back to the loop.
type MonthOutcome struct {
	Result TaxAwarePortfolioMonthResult
	Income MonthlyIncomeBreakdown

	// TotalExpenses is the month's guardrail-adjusted expense total
	// including the IRMAA surcharge; PlannedExpenses is the same without
	// the guardrail adjustment.
	TotalExpenses   float64
	PlannedExpenses float64
	// LivingExpenses is the pre-guardrail base living expense used this
	// month; Healthcare the month's healthcare cost (after any multiplier).
	LivingExpenses float64
	Healthcare     float64

	GuardrailMultiplier float64
	// GuardrailEvent is non-nil when a year-boundary evaluation changed the
	// multiplier this month.
	GuardrailEvent *models.GuardrailEvent

	RothConversion             float64
	AllowTaxDeferredWithdrawal bool
	TotalBalance               float64
}

// NewProjectionState builds the shared loop preamble from a prepared input.
func NewProjectionState(in Input) *ProjectionState {
	s := in.Prepared.Settings()

	st := &ProjectionState{
		in:      in,
		primary: s,
		active:  s,

		TaxDeferredBalance: s.PortfolioValue * (s.TaxDeferredPercent / 100),
		RothBalance:        s.PortfolioValue * (s.RothPercent / 100),
		RothBasis:          s.PortfolioValue * (s.RothPercent / 100),

		CumulativeInflation:    1.0,
		NetCumulativeInflation: 1.0,

		TaxCalculator:        NewTaxCalculator(s.TaxConfig, s.InflationRate),
		CompletedMAGIHistory: make([]float64, 0, s.ProjectionYears),
	}
	st.TaxableAccount = NewTaxableAccountState(s, s.PortfolioValue-st.TaxDeferredBalance-st.RothBalance)
	st.RothFirstFundedYear = s.RothFirstFundedYear
	if st.RothFirstFundedYear == 0 && s.RothPercent > 0 {
		st.RothFirstFundedYear = ParseStartYear(s.StartDate)
	}
	st.CurrentLivingExpenses = livingExpensesAtMonth(s, 0)
	if s.Guardrails != nil && s.Guardrails.Enabled {
		st.Guardrails = NewGuardrailState(s.PortfolioValue)
	}
	return st
}

// Settings returns the active settings (the primary scenario, or the
// current chain link after a transition).
func (st *ProjectionState) Settings() *models.WhatIfSettings {
	return st.active
}

// ZeroBalances empties every account — the canonical loop's depletion
// bookkeeping.
func (st *ProjectionState) ZeroBalances() {
	st.TaxDeferredBalance = 0
	st.RothBalance = 0
	st.TaxableAccount = NewTaxableAccountState(st.active, 0)
}

// StepMonth advances the projection by one month: the year-boundary pass
// (MAGI history, chain transition, tax-calculator refresh, RMD, Roth
// conversion, big-ticket items, guardrail evaluation), the inflation and
// expense advance, the PortfolioMonthInput assembly, and the tax-aware
// month execution with its follow-up bookkeeping.
//
// returnsFor is invoked once, after the year-boundary pass (so it sees the
// post-chain-transition settings), and supplies the month's returns and
// expense adjustments. It is also where a stochastic loop must draw any
// randomness for the month, in its legacy order, to keep RNG streams
// stable.
func (st *ProjectionState) StepMonth(m int, returnsFor func(s *models.WhatIfSettings, month int) MonthReturns) MonthOutcome {
	s := st.active
	currentYear := m / 12
	monthInYear := m % 12
	phaseAge := s.GetPhaseReferenceAge(currentYear)
	bigTicketExpenseThisMonth := 0.0
	rothConversionThisMonth := 0.0
	allowTaxDeferredWithdrawal := !taxDeferredDelayActive(s, currentYear)
	penaltyRate := earlyWithdrawalPenaltyRate(s.CurrentAge, currentYear)

	// Annual adjustments at year boundaries.
	if monthInYear == 0 {
		if m > 0 {
			st.CompletedMAGIHistory = append(st.CompletedMAGIHistory, st.CurrentYearTaxSnapshot.AnnualMAGI)
		}
		st.TaxState = ProjectionTaxAccumulator{}
		if len(st.in.Chain) > 0 {
			newIdx, prepared := st.in.Hooks.ResolveChain(currentYear, st.nextChainIdx, st.primary, st.in.Chain)
			if prepared != nil {
				st.active = prepared
				s = prepared
				st.nextChainIdx = newIdx

				st.CurrentLivingExpenses = RebaseLivingExpensesAtTransition(s, phaseAge, st.CumulativeInflation, st.NetCumulativeInflation)
				st.TaxableAccount.SyncAssumptions(s)
			}
		}
		st.TaxCalculator = NewTaxCalculator(s.TaxConfig, s.InflationRate)
		st.TaxableAccount.RealizedGainsYTD = 0

		// F-074/F-078: annualRMD computed once per year on year-start
		// tax-deferred balance, applied only in the trigger month.
		st.AnnualRMD = AnnualRMDForYear(s, currentYear, st.TaxDeferredBalance)

		rothConversionThisMonth = ApplyRothConversionAtYear(s, currentYear, &st.TaxDeferredBalance, &st.RothBalance, &st.RothBasis, &st.RothFirstFundedYear)

		bigTicketResult := ApplyBigTicketItemsForYear(s, currentYear, allowTaxDeferredWithdrawal, penaltyRate, &st.TaxDeferredBalance, &st.TaxableAccount, &st.RothBalance, &st.RothBasis)
		bigTicketExpenseThisMonth += bigTicketResult.UnfundedExpense
		st.BigTicketRothEarnings = bigTicketResult.RothEarningsWithdrawal
	}

	// The month's injected returns; drawn after the chain transition so a
	// stochastic loop sees the settings it must blend against.
	p := returnsFor(s, m)

	if m > 0 {
		st.CumulativeInflation *= monthlyCompoundFactorFromDecimal(p.InflationAnnual)
		st.NetCumulativeInflation *= monthlyCompoundFactorFromDecimal(p.NetInflationAnnual)
		if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled {
			st.CurrentLivingExpenses = s.MonthlyLivingExpenses * s.GetSpendingMultiplier(phaseAge) * st.CumulativeInflation
		} else {
			st.CurrentLivingExpenses *= monthlyCompoundFactorFromDecimal(p.NetInflationAnnual)
		}
	}

	// Evaluate spending guardrails at year boundaries.
	var guardrailEvent *models.GuardrailEvent
	if st.Guardrails != nil && monthInYear == 0 {
		totalPortfolio := st.TaxDeferredBalance + st.TaxableAccount.MarketValue + st.RothBalance
		prevMult := st.Guardrails.Multiplier()
		st.Guardrails.Evaluate(s.Guardrails, totalPortfolio)
		newMult := st.Guardrails.Multiplier()
		if newMult != prevMult {
			eventType := "cut"
			if newMult > prevMult {
				eventType = "raise"
			}
			guardrailEvent = &models.GuardrailEvent{
				Year:                  currentYear,
				Type:                  eventType,
				Multiplier:            newMult,
				PreviousMultiplier:    prevMult,
				Portfolio:             totalPortfolio,
				MonthlySpendingBefore: st.CurrentLivingExpenses * prevMult,
				MonthlySpendingAfter:  st.CurrentLivingExpenses * newMult,
				CumulativeInflation:   st.CumulativeInflation,
			}
		}
	}

	activeMultiplier := 1.0
	if st.Guardrails != nil {
		activeMultiplier = st.Guardrails.Multiplier()
	}
	adjustedLivingExpenses := st.CurrentLivingExpenses * activeMultiplier

	// Expense assembly. ExpenseSources are not subject to guardrail cuts —
	// planned and adjusted stay in sync for them.
	activeHealthcare := s.GetTotalHealthcareCost(m) * p.HealthcareMultiplier
	propertyTax := PropertyTaxAtMonth(s, m)
	plannedTotalExpenses := st.CurrentLivingExpenses + activeHealthcare + propertyTax + bigTicketExpenseThisMonth
	totalExpenses := adjustedLivingExpenses + activeHealthcare + propertyTax + bigTicketExpenseThisMonth

	for _, source := range s.ExpenseSources {
		expenseAmount := source.GetAdjustedAmount(m, s.InflationRate)
		if s.SpendingPhaseConfig != nil && s.SpendingPhaseConfig.Enabled && source.Discretionary {
			expenseAmount *= s.GetSpendingMultiplier(phaseAge)
		}
		if source.Discretionary && p.DiscretionaryMultiplier != 1 {
			expenseAmount *= p.DiscretionaryMultiplier
		}
		totalExpenses += expenseAmount
		plannedTotalExpenses += expenseAmount
	}
	totalExpenses += p.ExtraExpenses

	incomeBreakdown := CalculateMonthlyIncomeBreakdown(st.in.Hooks, s, m)

	taxableComponents := BuildTaxableReturnComponents(p.TaxableAnnualPercent, s)
	irmaaEligibleAdults := MedicareEligibleAdultCountAtYear(s, currentYear)
	irmaaInflationFactor := PlannerIRMAAInflationFactorForYear(s.InflationRate, float64(YearsFromTaxBase(s, currentYear)))

	// F-074: apply the full annual RMD only in the trigger month.
	monthlyRMD := MonthlyRMDForMonth(s, monthInYear, st.AnnualRMD, st.TaxDeferredBalance)

	monthResult := ExecuteTaxAwarePortfolioMonth(PortfolioMonthInput{
		TotalExpenses:                     totalExpenses,
		IncomeBreakdown:                   incomeBreakdown,
		MonthlyRMD:                        monthlyRMD,
		AllowTaxDeferredWithdrawal:        allowTaxDeferredWithdrawal,
		PenaltyRate:                       penaltyRate,
		TaxDeferredBalance:                &st.TaxDeferredBalance,
		TaxableAccount:                    &st.TaxableAccount,
		RothBalance:                       &st.RothBalance,
		RothBasis:                         &st.RothBasis,
		RothFirstFundedYear:               st.RothFirstFundedYear,
		TaxDeferredMonthlyReturn:          p.TaxDeferredMonthly,
		RothMonthlyReturn:                 p.RothMonthly,
		TaxableComponents:                 taxableComponents,
		Timing:                            s.GetProjectionTiming(),
		TaxState:                          st.TaxState,
		TaxCalculator:                     st.TaxCalculator,
		MonthInYear:                       monthInYear,
		CalendarYear:                      ParseStartYear(s.StartDate) + currentYear,
		RothConversionThisMonth:           rothConversionThisMonth,
		TaxableRothEarningsBeforeCashFlow: st.BigTicketRothEarnings,
		CompletedMAGIHistory:              st.CompletedMAGIHistory,
		AssumedIRMALookbackMAGI:           &st.AssumedLookbackMAGI,
		IRMAAEligibleAdults:               irmaaEligibleAdults,
		IRMAAInflationFactor:              irmaaInflationFactor,
	})
	// Consumed above; reset so later months in the year don't re-fold the
	// same big-ticket earnings into ordinary income.
	st.BigTicketRothEarnings = 0
	st.CurrentYearTaxSnapshot = monthResult.TaxSnapshot
	// Hold the year-0 MAGI estimate as the IRMAA lookback seed for years
	// 0-1; real history drives years 2+ (resolveIRMAALookbackMAGI prefers it).
	if currentYear == 0 {
		st.AssumedLookbackMAGI = monthResult.TaxSnapshot.AnnualMAGI
	}
	ApplyTaxStateMonth(&st.TaxState, incomeBreakdown, monthResult, rothConversionThisMonth)

	return MonthOutcome{
		Result:                     monthResult,
		Income:                     incomeBreakdown,
		TotalExpenses:              totalExpenses + monthResult.IRMAAExpense,
		PlannedExpenses:            plannedTotalExpenses + monthResult.IRMAAExpense,
		LivingExpenses:             st.CurrentLivingExpenses,
		Healthcare:                 activeHealthcare,
		GuardrailMultiplier:        activeMultiplier,
		GuardrailEvent:             guardrailEvent,
		RothConversion:             rothConversionThisMonth,
		AllowTaxDeferredWithdrawal: allowTaxDeferredWithdrawal,
		TotalBalance:               st.TaxDeferredBalance + st.RothBalance + st.TaxableAccount.MarketValue,
	}
}
