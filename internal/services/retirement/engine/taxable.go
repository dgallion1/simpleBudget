package engine

import (
	"math"

	"budget2/internal/models"
)

// TaxableReturnComponents decomposes a taxable account's monthly return
// into its tax-relevant pieces.
type TaxableReturnComponents struct {
	Appreciation             float64
	QualifiedDividend        float64
	NonQualifiedDividend     float64
	CapitalGainsDistribution float64
}

// TaxableGrowthResult captures one month of taxable-account growth,
// split into the buckets the tax accumulator needs.
type TaxableGrowthResult struct {
	TotalGrowth               float64
	QualifiedDividends        float64
	NonQualifiedDividends     float64
	CapitalGainsDistributions float64
}

// TaxableAccountState tracks the running market value, cost basis, and
// year-to-date realized gains of a taxable brokerage account during
// projection.
type TaxableAccountState struct {
	MarketValue                  float64
	CostBasis                    float64
	QualifiedDividendYield       float64
	NonQualifiedDividendYield    float64
	CapitalGainsDistributionRate float64
	RealizedGainsYTD             float64
}

// NewTaxableAccountState seeds a TaxableAccountState from settings: the
// initial market value is treated as cost basis (cf. cost-basis policy
// in project_retirement_calculator.md), and dividend yields are split
// into qualified vs. non-qualified shares.
func NewTaxableAccountState(s *models.WhatIfSettings, marketValue float64) TaxableAccountState {
	qualifiedShare := s.GetTaxableQualifiedDividendPercent() / 100
	totalDividendYield := math.Max(0, s.TaxableDividendYield) / 100
	return TaxableAccountState{
		MarketValue:                  marketValue,
		CostBasis:                    marketValue,
		QualifiedDividendYield:       totalDividendYield * qualifiedShare,
		NonQualifiedDividendYield:    totalDividendYield * (1 - qualifiedShare),
		CapitalGainsDistributionRate: math.Max(0, s.TaxableCapitalGainsDistributionRate) / 100,
	}
}

// SyncAssumptions refreshes the dividend/capital-gains assumptions from
// the supplied settings while preserving running market value, basis,
// and YTD realized gains. Used after a scenario transition.
func (a *TaxableAccountState) SyncAssumptions(s *models.WhatIfSettings) {
	updated := NewTaxableAccountState(s, a.MarketValue)
	updated.CostBasis = a.CostBasis
	updated.RealizedGainsYTD = a.RealizedGainsYTD
	*a = updated
}

// AddCash records a deposit (e.g., a big-ticket reinvestment) — both
// market value and cost basis grow by the same amount.
func (a *TaxableAccountState) AddCash(amount float64) {
	if amount <= 0 {
		return
	}
	a.MarketValue += amount
	a.CostBasis += amount
}

// Withdraw pulls cash from the account, recognising a pro-rata realized
// gain against the current basis. Returns the cash extracted, the
// reduction in cost basis, and the realized gain (which may be zero).
func (a *TaxableAccountState) Withdraw(amount float64) (cash, basisReduction, realizedGain float64) {
	if amount <= 0 || a.MarketValue <= 0 {
		return 0, 0, 0
	}

	cash = math.Min(amount, a.MarketValue)
	basisReduction = 0.0
	if a.MarketValue > 0 {
		basisReduction = a.CostBasis * (cash / a.MarketValue)
	}

	a.MarketValue -= cash
	a.CostBasis -= basisReduction
	if a.MarketValue <= 0 {
		a.MarketValue = 0
		a.CostBasis = 0
	}
	if a.CostBasis < 0 {
		a.CostBasis = 0
	}

	realizedGain = cash - basisReduction
	a.RealizedGainsYTD += math.Max(0, realizedGain)
	return cash, basisReduction, realizedGain
}

// BuildTaxableReturnComponents derives a per-month return decomposition
// from the supplied total annual return percent and the dividend /
// cap-gains distribution assumptions in settings.
func BuildTaxableReturnComponents(totalAnnualReturnPercent float64, s *models.WhatIfSettings) TaxableReturnComponents {
	totalMonthlyReturn := monthlyCompoundFactorFromDecimal(totalAnnualReturnPercent/100) - 1
	qualifiedDividendMonthly := monthlyCompoundFactorFromDecimal(math.Max(0, s.TaxableDividendYield)*(s.GetTaxableQualifiedDividendPercent()/100)/100) - 1
	nonQualifiedDividendMonthly := monthlyCompoundFactorFromDecimal(math.Max(0, s.TaxableDividendYield)*(1-s.GetTaxableQualifiedDividendPercent()/100)/100) - 1
	capitalGainsDistributionMonthly := monthlyCompoundFactorFromDecimal(math.Max(0, s.TaxableCapitalGainsDistributionRate)/100) - 1
	return TaxableReturnComponents{
		Appreciation:             totalMonthlyReturn - qualifiedDividendMonthly - nonQualifiedDividendMonthly - capitalGainsDistributionMonthly,
		QualifiedDividend:        qualifiedDividendMonthly,
		NonQualifiedDividend:     nonQualifiedDividendMonthly,
		CapitalGainsDistribution: capitalGainsDistributionMonthly,
	}
}

// ApplyGrowth advances the account by a fractional month using the
// supplied return components. Appreciation moves into market value;
// dividends and cap-gains distributions are returned for the caller to
// reinvest as cash (so they hit cost basis correctly via AddCash).
func (a *TaxableAccountState) ApplyGrowth(components TaxableReturnComponents, fraction float64) TaxableGrowthResult {
	if a.MarketValue <= 0 {
		return TaxableGrowthResult{}
	}

	baseValue := a.MarketValue
	appreciation := baseValue * fractionalMonthlyReturn(components.Appreciation, fraction)
	qualifiedDividends := baseValue * fractionalMonthlyReturn(components.QualifiedDividend, fraction)
	nonQualifiedDividends := baseValue * fractionalMonthlyReturn(components.NonQualifiedDividend, fraction)
	capitalGainsDistributions := baseValue * fractionalMonthlyReturn(components.CapitalGainsDistribution, fraction)

	a.MarketValue += appreciation
	a.RealizedGainsYTD += capitalGainsDistributions

	if a.MarketValue < 0 {
		a.MarketValue = 0
	}
	if a.CostBasis < 0 {
		a.CostBasis = 0
	}

	return TaxableGrowthResult{
		TotalGrowth:               appreciation,
		QualifiedDividends:        qualifiedDividends,
		NonQualifiedDividends:     nonQualifiedDividends,
		CapitalGainsDistributions: capitalGainsDistributions,
	}
}

// ExpectedTaxableMonthlyCashFlow returns the monthly dividend / cap-gains
// distribution decomposition for a taxable account at the given market
// value and assumed total annual return. Used by analysis-package
// snapshots (BudgetFit) that estimate first-month and steady-state cash
// flow without running a full projection.
func ExpectedTaxableMonthlyCashFlow(s *models.WhatIfSettings, taxableMarketValue, taxableAnnualReturn float64) TaxableGrowthResult {
	account := NewTaxableAccountState(s, taxableMarketValue)
	return account.ApplyGrowth(BuildTaxableReturnComponents(taxableAnnualReturn, s), 1.0)
}
