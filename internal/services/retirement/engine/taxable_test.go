package engine

import (
	"math"
	"testing"

	"budget2/internal/models"
)

func TestNewTaxableAccountStateSplitsDividendAssumptions(t *testing.T) {
	settings := &models.WhatIfSettings{
		TaxableDividendYield:                4,
		TaxableQualifiedDividendPercent:     75,
		TaxableCapitalGainsDistributionRate: 1.5,
	}

	account := NewTaxableAccountState(settings, 120000)

	assertClose(t, "market value", account.MarketValue, 120000)
	assertClose(t, "cost basis", account.CostBasis, 120000)
	assertClose(t, "qualified yield", account.QualifiedDividendYield, 0.03)
	assertClose(t, "non-qualified yield", account.NonQualifiedDividendYield, 0.01)
	assertClose(t, "capital gains distribution rate", account.CapitalGainsDistributionRate, 0.015)
	assertClose(t, "realized gains", account.RealizedGainsYTD, 0)
}

func TestTaxableAccountStateCashAndWithdrawalsTrackBasisAndGains(t *testing.T) {
	account := TaxableAccountState{MarketValue: 100000, CostBasis: 60000}

	account.AddCash(-100)
	assertClose(t, "negative deposit market value", account.MarketValue, 100000)
	assertClose(t, "negative deposit basis", account.CostBasis, 60000)

	account.AddCash(20000)
	assertClose(t, "deposit market value", account.MarketValue, 120000)
	assertClose(t, "deposit basis", account.CostBasis, 80000)

	cash, basis, gain := account.Withdraw(30000)
	assertClose(t, "cash withdrawn", cash, 30000)
	assertClose(t, "basis reduction", basis, 20000)
	assertClose(t, "realized gain", gain, 10000)
	assertClose(t, "remaining market value", account.MarketValue, 90000)
	assertClose(t, "remaining basis", account.CostBasis, 60000)
	assertClose(t, "realized gains ytd", account.RealizedGainsYTD, 10000)

	cash, basis, gain = account.Withdraw(200000)
	assertClose(t, "cash withdrawn to zero", cash, 90000)
	assertClose(t, "basis reduction to zero", basis, 60000)
	assertClose(t, "realized gain to zero", gain, 30000)
	assertClose(t, "zeroed market value", account.MarketValue, 0)
	assertClose(t, "zeroed basis", account.CostBasis, 0)
	assertClose(t, "accumulated gains", account.RealizedGainsYTD, 40000)
}

func TestTaxableAccountStateApplyGrowthReturnsTaxableComponents(t *testing.T) {
	account := TaxableAccountState{MarketValue: 100000, CostBasis: 80000}
	components := TaxableReturnComponents{
		Appreciation:             0.01,
		QualifiedDividend:        0.002,
		NonQualifiedDividend:     0.001,
		CapitalGainsDistribution: 0.003,
	}

	result := account.ApplyGrowth(components, 0.5)

	assertClose(t, "appreciation", result.TotalGrowth, 100000*(math.Pow(1.01, 0.5)-1))
	assertClose(t, "qualified dividends", result.QualifiedDividends, 100000*(math.Pow(1.002, 0.5)-1))
	assertClose(t, "non-qualified dividends", result.NonQualifiedDividends, 100000*(math.Pow(1.001, 0.5)-1))
	assertClose(t, "capital gains distributions", result.CapitalGainsDistributions, 100000*(math.Pow(1.003, 0.5)-1))
	assertClose(t, "market value", account.MarketValue, 100000+result.TotalGrowth)
	assertClose(t, "basis unchanged", account.CostBasis, 80000)
	assertClose(t, "realized gains ytd", account.RealizedGainsYTD, result.CapitalGainsDistributions)
}

func TestBuildTaxableReturnComponentsAndExpectedCashFlow(t *testing.T) {
	settings := &models.WhatIfSettings{
		TaxableDividendYield:                6,
		TaxableQualifiedDividendPercent:     50,
		TaxableCapitalGainsDistributionRate: 3,
	}

	components := BuildTaxableReturnComponents(12, settings)
	totalMonthly := math.Pow(1.12, 1.0/12.0) - 1
	qualifiedMonthly := math.Pow(1.03, 1.0/12.0) - 1
	nonQualifiedMonthly := math.Pow(1.03, 1.0/12.0) - 1
	capitalGainsMonthly := math.Pow(1.03, 1.0/12.0) - 1

	assertClose(t, "appreciation", components.Appreciation, totalMonthly-qualifiedMonthly-nonQualifiedMonthly-capitalGainsMonthly)
	assertClose(t, "qualified dividend", components.QualifiedDividend, qualifiedMonthly)
	assertClose(t, "non-qualified dividend", components.NonQualifiedDividend, nonQualifiedMonthly)
	assertClose(t, "capital gains distribution", components.CapitalGainsDistribution, capitalGainsMonthly)

	cashFlow := ExpectedTaxableMonthlyCashFlow(settings, 100000, 12)
	assertClose(t, "expected qualified dividends", cashFlow.QualifiedDividends, 100000*qualifiedMonthly)
	assertClose(t, "expected non-qualified dividends", cashFlow.NonQualifiedDividends, 100000*nonQualifiedMonthly)
	assertClose(t, "expected capital gains distributions", cashFlow.CapitalGainsDistributions, 100000*capitalGainsMonthly)
}

func TestTaxableAccountStateSyncAssumptionsPreservesRunningBalances(t *testing.T) {
	account := TaxableAccountState{MarketValue: 125000, CostBasis: 90000, RealizedGainsYTD: 7000}
	settings := &models.WhatIfSettings{
		TaxableDividendYield:                5,
		TaxableQualifiedDividendPercent:     40,
		TaxableCapitalGainsDistributionRate: 2,
	}

	account.SyncAssumptions(settings)

	assertClose(t, "market value", account.MarketValue, 125000)
	assertClose(t, "cost basis", account.CostBasis, 90000)
	assertClose(t, "realized gains ytd", account.RealizedGainsYTD, 7000)
	assertClose(t, "qualified yield", account.QualifiedDividendYield, 0.02)
	assertClose(t, "non-qualified yield", account.NonQualifiedDividendYield, 0.03)
	assertClose(t, "capital gains distribution rate", account.CapitalGainsDistributionRate, 0.02)
}

func assertClose(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s: got %.12f, want %.12f", label, got, want)
	}
}
