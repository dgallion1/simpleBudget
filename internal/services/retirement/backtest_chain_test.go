package retirement

import (
	"testing"

	"budget2/internal/models"
)

// TestHistoricalBacktest_ChainTransition exercises the chain
// transition path through the full HistoricalBacktest analysis. It
// lives in the retirement package because the chain-transition path
// goes through engine.NextChainTransitionHook, which is wired by
// retirement's init() — analysis tests in isolation can't trigger
// that wiring.
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

	chainCalc := newTestCalcWithChain(t, primary, []PreparedChainLink{
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
