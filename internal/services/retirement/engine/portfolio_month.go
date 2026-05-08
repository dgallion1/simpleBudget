package engine

import (
	"math"

	"budget2/internal/models"
)

// WithdrawalBreakdown details the buckets touched by a single month's
// portfolio withdrawal: how much came from each account, how much of
// it was the legally-required RMD, and how much (if any) of the need
// could not be met.
type WithdrawalBreakdown struct {
	RemainingNeed             float64
	ActualWithdrawal          float64
	RMDWithdrawal             float64
	WithdrawalFromTaxDeferred float64
	WithdrawalFromTaxable     float64
	WithdrawalFromRoth        float64
	EarlyPenaltyPaid          float64
}

// PortfolioCashFlowResult is the Outer view of a month's
// portfolio-side cash flow: per-bucket withdrawal amounts plus the
// realized capital gain on any taxable sale and the amount of need
// left unmet.
type PortfolioCashFlowResult struct {
	Shortfall                 float64
	ActualWithdrawal          float64
	RMDWithdrawal             float64
	WithdrawalFromTaxDeferred float64
	WithdrawalFromTaxable     float64
	WithdrawalFromRoth        float64
	TaxableRealizedGain       float64
}

// GrossWithdrawal returns the sum of pre-tax withdrawals across
// account types.
func (r PortfolioCashFlowResult) GrossWithdrawal() float64 {
	return r.WithdrawalFromTaxDeferred + r.WithdrawalFromTaxable + r.WithdrawalFromRoth
}

// ProjectionTimingGrowthFractions splits a month's growth into the
// fraction applied before vs. after the cash-flow leg, matching the
// user-selected projection-timing convention.
func ProjectionTimingGrowthFractions(timing models.ProjectionTiming) (before float64, after float64) {
	switch models.NormalizeProjectionTiming(timing) {
	case models.ProjectionTimingStartOfMonth:
		return 0, 1
	case models.ProjectionTimingMidMonth:
		return 0.5, 0.5
	default:
		return 1, 0
	}
}

// WithdrawForExpenses pulls cash from the three buckets in priority
// order (RMD → taxable → Roth → tax-deferred discretionary) to meet
// the requested needFromPortfolio. Mutates the supplied balances in
// place; returns a structured breakdown of what came from where.
func WithdrawForExpenses(neededFromPortfolio, monthlyRMD float64, allowTaxDeferred bool, earlyPenaltyRate float64, taxDeferredBalance, taxableBalance, rothBalance *float64) WithdrawalBreakdown {
	breakdown := WithdrawalBreakdown{RemainingNeed: neededFromPortfolio}
	if neededFromPortfolio <= 0 {
		return breakdown
	}

	if monthlyRMD > 0 && *taxDeferredBalance > 0 {
		rmdUsed := math.Min(monthlyRMD, breakdown.RemainingNeed)
		rmdUsed = math.Min(rmdUsed, *taxDeferredBalance)
		*taxDeferredBalance -= rmdUsed
		breakdown.RemainingNeed -= rmdUsed
		breakdown.RMDWithdrawal = rmdUsed
		breakdown.WithdrawalFromTaxDeferred += rmdUsed
		breakdown.ActualWithdrawal += rmdUsed
	}

	if breakdown.RemainingNeed > 0 && *taxableBalance > 0 {
		fromTaxable := math.Min(breakdown.RemainingNeed, *taxableBalance)
		*taxableBalance -= fromTaxable
		breakdown.RemainingNeed -= fromTaxable
		breakdown.WithdrawalFromTaxable += fromTaxable
		breakdown.ActualWithdrawal += fromTaxable
	}

	if breakdown.RemainingNeed > 0 && *rothBalance > 0 {
		fromRoth := math.Min(breakdown.RemainingNeed, *rothBalance)
		*rothBalance -= fromRoth
		breakdown.RemainingNeed -= fromRoth
		breakdown.WithdrawalFromRoth += fromRoth
		breakdown.ActualWithdrawal += fromRoth
	}

	if allowTaxDeferred && breakdown.RemainingNeed > 0 && *taxDeferredBalance > 0 {
		// Early withdrawal penalty: before age 59½, only (1-penalty) of each dollar
		// withdrawn from tax-deferred is available for spending.
		effectiveFactor := 1.0 - earlyPenaltyRate
		grossNeeded := breakdown.RemainingNeed / effectiveFactor
		fromTaxDeferred := math.Min(grossNeeded, *taxDeferredBalance)
		*taxDeferredBalance -= fromTaxDeferred
		netSpending := fromTaxDeferred * effectiveFactor
		penalty := fromTaxDeferred - netSpending
		breakdown.RemainingNeed -= netSpending
		breakdown.WithdrawalFromTaxDeferred += fromTaxDeferred
		breakdown.ActualWithdrawal += fromTaxDeferred
		breakdown.EarlyPenaltyPaid += penalty
	}

	return breakdown
}

// ReinvestRequiredRMDToTaxableState moves an RMD from tax-deferred
// into the taxable account, with the after-tax amount as new basis.
// The pre-tax amount is decremented from tax-deferred (the gross RMD
// is the legal distribution); the after-tax portion (gross × (1 -
// marginalRate)) is added to the taxable account with that net amount
// as cost basis. Returns (gross, net) — gross is the IRS distribution
// amount that callers must report as ordinary taxable income; net is
// the cash actually deposited into the taxable account.
//
// F-049: prior implementation used gross as both reinvested amount
// and basis, which silently understated future LTCG on later
// withdrawals. F-073: prior implementation returned only the net
// amount; callers stored that net into RMDWithdrawal and
// WithdrawalFromTaxDeferred, which understated ordinary income,
// taxes, MAGI, and RMD-analysis totals by exactly gross × marginalRate
// every surplus-RMD month.
func ReinvestRequiredRMDToTaxableState(monthlyRMD, marginalRate float64, taxDeferredBalance *float64, taxable *TaxableAccountState) (gross, net float64) {
	if monthlyRMD <= 0 || *taxDeferredBalance <= 0 {
		return 0, 0
	}
	if marginalRate < 0 {
		marginalRate = 0
	}
	if marginalRate > 1 {
		marginalRate = 1
	}

	gross = math.Min(monthlyRMD, *taxDeferredBalance)
	*taxDeferredBalance -= gross
	net = gross * (1 - marginalRate)
	taxable.AddCash(net)
	return gross, net
}

// ApplyBigTicketExpenseWithTaxableState pulls a one-off big-ticket
// expense from the portfolio in priority order (taxable → Roth → tax
// deferred) and returns any unmet remainder. Tax-deferred withdrawals
// honour the early-withdrawal penalty when active.
func ApplyBigTicketExpenseWithTaxableState(amount float64, allowTaxDeferred bool, earlyPenaltyRate float64, taxDeferredBalance *float64, taxable *TaxableAccountState, rothBalance *float64) float64 {
	remaining := amount

	if remaining > 0 && taxable.MarketValue > 0 {
		fromTaxable, _, _ := taxable.Withdraw(remaining)
		remaining -= fromTaxable
	}

	if remaining > 0 && *rothBalance > 0 {
		fromRoth := math.Min(remaining, *rothBalance)
		*rothBalance -= fromRoth
		remaining -= fromRoth
	}

	if allowTaxDeferred && remaining > 0 && *taxDeferredBalance > 0 {
		effectiveFactor := 1.0 - earlyPenaltyRate
		grossNeeded := remaining / effectiveFactor
		fromTaxDeferred := math.Min(grossNeeded, *taxDeferredBalance)
		*taxDeferredBalance -= fromTaxDeferred
		remaining -= fromTaxDeferred * effectiveFactor
	}

	return remaining
}

// ExecutePortfolioCashFlowWithTaxableState orchestrates a month's
// cash-flow draws against the taxable account state machine. It calls
// WithdrawForExpenses for the bucket-priority logic, then re-applies
// any taxable withdrawal through TaxableAccountState.Withdraw so that
// cost-basis-aware realized gains are tracked. Required-but-unmet
// RMDs are reinvested into the taxable account at the supplied
// marginal rate.
func ExecutePortfolioCashFlowWithTaxableState(neededFromPortfolio, monthlyRMD float64, allowTaxDeferred bool, earlyPenaltyRate, marginalRate float64, taxDeferredBalance *float64, taxable *TaxableAccountState, rothBalance *float64) PortfolioCashFlowResult {
	result := PortfolioCashFlowResult{}

	if neededFromPortfolio > 0 {
		withdrawal := WithdrawForExpenses(neededFromPortfolio, monthlyRMD, allowTaxDeferred, earlyPenaltyRate, taxDeferredBalance, &taxable.MarketValue, rothBalance)
		result.Shortfall = withdrawal.RemainingNeed
		result.ActualWithdrawal = withdrawal.ActualWithdrawal
		result.RMDWithdrawal = withdrawal.RMDWithdrawal
		result.WithdrawalFromTaxDeferred = withdrawal.WithdrawalFromTaxDeferred
		result.WithdrawalFromRoth = withdrawal.WithdrawalFromRoth

		if withdrawal.WithdrawalFromTaxable > 0 {
			// Undo the legacy taxable withdrawal, then re-apply it with basis tracking.
			taxable.MarketValue += withdrawal.WithdrawalFromTaxable
			cash, _, realizedGain := taxable.Withdraw(withdrawal.WithdrawalFromTaxable)
			result.WithdrawalFromTaxable = cash
			result.TaxableRealizedGain += math.Max(0, realizedGain)
		}

		unmetRMD := monthlyRMD - withdrawal.RMDWithdrawal
		if unmetRMD > 0 {
			gross, _ := ReinvestRequiredRMDToTaxableState(unmetRMD, marginalRate, taxDeferredBalance, taxable)
			result.RMDWithdrawal += gross
			result.WithdrawalFromTaxDeferred += gross
		}
	} else {
		if neededFromPortfolio < 0 {
			taxable.AddCash(math.Abs(neededFromPortfolio))
		}
		gross, _ := ReinvestRequiredRMDToTaxableState(monthlyRMD, marginalRate, taxDeferredBalance, taxable)
		result.RMDWithdrawal = gross
		result.WithdrawalFromTaxDeferred += gross
	}

	return result
}

// TaxAwarePortfolioMonthResult bundles the outputs of a single
// projection month: cash-flow effects, growth, taxable-income
// components, taxes/IRMAA paid, and the converged tax snapshot.
type TaxAwarePortfolioMonthResult struct {
	Shortfall                        float64
	TaxesPaid                        float64
	IRMAAExpense                     float64
	TotalGrowth                      float64
	TaxableIncomeBeforeCashFlow      float64
	TaxableQualifiedDividends        float64
	TaxableNonQualifiedDividends     float64
	TaxableCapitalGains              float64
	TaxableCapitalGainsDistributions float64
	TaxSnapshot                      ProjectedTaxSnapshot
	CashFlow                         PortfolioCashFlowResult
}

// PortfolioMonthInput bundles the inputs to ExecuteTaxAwarePortfolioMonth.
// Replaces a 19-positional-arg signature so future parameter additions/
// renames don't ripple through every call site.
//
// The pointer fields (TaxDeferredBalance, TaxableAccount, RothBalance) keep
// their pointer semantics — ExecuteTaxAwarePortfolioMonth mutates the values
// they point to, and that contract is preserved.
type PortfolioMonthInput struct {
	TotalExpenses              float64
	IncomeBreakdown            MonthlyIncomeBreakdown
	MonthlyRMD                 float64
	AllowTaxDeferredWithdrawal bool
	PenaltyRate                float64
	TaxDeferredBalance         *float64
	TaxableAccount             *TaxableAccountState
	RothBalance                *float64
	TaxDeferredMonthlyReturn   float64
	RothMonthlyReturn          float64
	TaxableComponents          TaxableReturnComponents
	Timing                     models.ProjectionTiming
	TaxState                   ProjectionTaxAccumulator
	TaxCalculator              *TaxCalculator
	CurrentYear                int
	MonthInYear                int
	RothConversionThisMonth    float64
	CompletedMAGIHistory       []float64
	IRMAAEligibleAdults        int
	IRMAAInflationFactor       float64
}

// ExecuteTaxAwarePortfolioMonth runs the inner fixed-point iteration
// that converges a single projection month's tax estimate, marginal
// rate, cash flow, and growth allocations. Mutates the supplied
// balances in place.
func ExecuteTaxAwarePortfolioMonth(in PortfolioMonthInput) TaxAwarePortfolioMonthResult {
	startingTaxDeferred := *in.TaxDeferredBalance
	startingRoth := *in.RothBalance
	startingTaxable := *in.TaxableAccount
	snapshot := in.TaxState.EstimateMonthlySnapshot(
		in.TaxCalculator,
		in.CurrentYear,
		in.MonthInYear,
		in.IncomeBreakdown.OrdinaryIncome,
		in.IncomeBreakdown.SocialSecurityIncome,
		0,
		0,
		0,
		0,
		in.RothConversionThisMonth,
		in.CompletedMAGIHistory,
		nil,
		in.IRMAAEligibleAdults,
		in.IRMAAInflationFactor,
	)
	taxesPaid := snapshot.MonthlyTax
	irmaaExpense := snapshot.MonthlyIRMAA
	// Marginal rate derived from estimated annual MAGI; updated each iteration
	// alongside taxesPaid/irmaaExpense so the RMD after-tax reinvestment uses a
	// converged rate. GetMarginalRate returns a percent (e.g. 22.0), so divide by 100.
	marginalRate := 0.0
	if in.TaxCalculator != nil {
		marginalRate = in.TaxCalculator.GetMarginalRate(snapshot.AnnualMAGI, in.CurrentYear) / 100
	}
	finalSnapshot := snapshot
	result := TaxAwarePortfolioMonthResult{}
	growthBeforeFraction, growthAfterFraction := ProjectionTimingGrowthFractions(in.Timing)

	for iter := 0; iter < 6; iter++ {
		trialTaxDeferred := startingTaxDeferred
		trialRoth := startingRoth
		trialTaxable := startingTaxable

		tdBeforeGrowth := trialTaxDeferred * fractionalMonthlyReturn(in.TaxDeferredMonthlyReturn, growthBeforeFraction)
		rothBeforeGrowth := trialRoth * fractionalMonthlyReturn(in.RothMonthlyReturn, growthBeforeFraction)
		trialTaxDeferred += tdBeforeGrowth
		trialRoth += rothBeforeGrowth
		beforeTaxableGrowth := trialTaxable.ApplyGrowth(in.TaxableComponents, growthBeforeFraction)

		trialNeededFromPortfolio := in.TotalExpenses + irmaaExpense + taxesPaid - in.IncomeBreakdown.TotalIncome - beforeTaxableGrowth.QualifiedDividends - beforeTaxableGrowth.NonQualifiedDividends - beforeTaxableGrowth.CapitalGainsDistributions
		trialCashFlow := ExecutePortfolioCashFlowWithTaxableState(trialNeededFromPortfolio, in.MonthlyRMD, in.AllowTaxDeferredWithdrawal, in.PenaltyRate, marginalRate, &trialTaxDeferred, &trialTaxable, &trialRoth)

		tdAfterGrowth := trialTaxDeferred * fractionalMonthlyReturn(in.TaxDeferredMonthlyReturn, growthAfterFraction)
		rothAfterGrowth := trialRoth * fractionalMonthlyReturn(in.RothMonthlyReturn, growthAfterFraction)
		trialTaxDeferred += tdAfterGrowth
		trialRoth += rothAfterGrowth
		afterTaxableGrowth := trialTaxable.ApplyGrowth(in.TaxableComponents, growthAfterFraction)
		trialTaxable.AddCash(afterTaxableGrowth.QualifiedDividends + afterTaxableGrowth.NonQualifiedDividends + afterTaxableGrowth.CapitalGainsDistributions)

		trialQualifiedDividends := beforeTaxableGrowth.QualifiedDividends + afterTaxableGrowth.QualifiedDividends
		trialNonQualifiedDividends := beforeTaxableGrowth.NonQualifiedDividends + afterTaxableGrowth.NonQualifiedDividends
		trialCapitalGains := beforeTaxableGrowth.CapitalGainsDistributions + afterTaxableGrowth.CapitalGainsDistributions + trialCashFlow.TaxableRealizedGain

		recalculatedSnapshot := in.TaxState.EstimateMonthlySnapshot(
			in.TaxCalculator,
			in.CurrentYear,
			in.MonthInYear,
			in.IncomeBreakdown.OrdinaryIncome+trialNonQualifiedDividends,
			in.IncomeBreakdown.SocialSecurityIncome,
			trialCashFlow.WithdrawalFromTaxDeferred,
			trialQualifiedDividends,
			trialCapitalGains,
			trialNonQualifiedDividends,
			in.RothConversionThisMonth,
			in.CompletedMAGIHistory,
			nil,
			in.IRMAAEligibleAdults,
			in.IRMAAInflationFactor,
		)

		*in.TaxDeferredBalance = trialTaxDeferred
		*in.RothBalance = trialRoth
		*in.TaxableAccount = trialTaxable
		result.CashFlow = trialCashFlow
		result.Shortfall = trialCashFlow.Shortfall
		result.TotalGrowth = tdBeforeGrowth + rothBeforeGrowth + beforeTaxableGrowth.TotalGrowth + tdAfterGrowth + rothAfterGrowth + afterTaxableGrowth.TotalGrowth
		result.TaxableIncomeBeforeCashFlow = beforeTaxableGrowth.QualifiedDividends + beforeTaxableGrowth.NonQualifiedDividends + beforeTaxableGrowth.CapitalGainsDistributions
		result.TaxableQualifiedDividends = trialQualifiedDividends
		result.TaxableNonQualifiedDividends = trialNonQualifiedDividends
		result.TaxableCapitalGains = trialCapitalGains
		result.TaxableCapitalGainsDistributions = beforeTaxableGrowth.CapitalGainsDistributions + afterTaxableGrowth.CapitalGainsDistributions
		finalSnapshot = recalculatedSnapshot

		if math.Abs(recalculatedSnapshot.MonthlyTax-taxesPaid) < 0.01 && math.Abs(recalculatedSnapshot.MonthlyIRMAA-irmaaExpense) < 0.01 {
			taxesPaid = recalculatedSnapshot.MonthlyTax
			irmaaExpense = recalculatedSnapshot.MonthlyIRMAA
			break
		}

		taxesPaid = recalculatedSnapshot.MonthlyTax
		irmaaExpense = recalculatedSnapshot.MonthlyIRMAA
		// Update marginal rate from converged MAGI for next iteration's RMD reinvestment.
		if in.TaxCalculator != nil {
			marginalRate = in.TaxCalculator.GetMarginalRate(recalculatedSnapshot.AnnualMAGI, in.CurrentYear) / 100
		}
	}

	result.TaxesPaid = taxesPaid
	result.IRMAAExpense = irmaaExpense
	result.TaxSnapshot = finalSnapshot
	return result
}
