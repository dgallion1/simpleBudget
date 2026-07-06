package retirement

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// TestHistoricalBacktest_ChainTransition exercises the chain
// transition path through the full HistoricalBacktest analysis. It
// lives in the retirement package because the chain-transition path
// goes through engine.Input.Hooks.ResolveChainTransition, populated by
// retirement.DefaultHooks(). Analysis-package tests in isolation can't
// import retirement (cycle) so they can't supply DefaultHooks; this
// test reaches the resolver via Calculator.RunHistoricalBacktest, which
// threads DefaultHooks through Calculator.input().
func TestHistoricalBacktest_ChainTransition(t *testing.T) {
	primary := models.DefaultWhatIfSettings()
	primary.CurrentAge = 60
	primary.ProjectionYears = 30
	primary.PortfolioValue = 500000
	primary.MonthlyLivingExpenses = 2000
	primary.TaxDeferredPercent = 50
	primary.RothPercent = 25

	linked := models.DefaultWhatIfSettings()
	linked.MonthlyLivingExpenses = 8000

	chainCalc := newTestCalcWithChain(t, primary, []engine.PreparedChainLink{
		preparedLink(t, "", 70, linked),
	})
	noChainCalc := newTestCalc(t, primary)

	chainBT := chainCalc.RunHistoricalBacktest()
	noChainBT := noChainCalc.RunHistoricalBacktest()

	if chainBT == nil || noChainBT == nil {
		t.Fatal("expected non-nil backtest results")
	}

	if chainBT.SuccessRate >= noChainBT.SuccessRate {
		t.Errorf("chained backtest success (%f) should be lower than no-chain (%f)",
			chainBT.SuccessRate, noChainBT.SuccessRate)
	}
}
