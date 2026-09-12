package analysis

import (
	"budget2/internal/models"
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// Promoted from independent GO1/GO2 checker probes (GO4).
func TestGuardrailRecoveryPreservesFirstDepletion(t *testing.T) {
	s := floorRegressionSettings()
	s.PortfolioValue = 1000
	s.Guardrails = nil
	s.MonthlyLivingExpenses = 750
	s.IncomeSources = []models.IncomeSource{{ID: "later", Name: "later", Amount: 1000, Type: models.IncomeFixed, StartMonth: 12}}
	r := floorRegressionMC(t, s, 750)
	if r.Survives || r.DepletionYear != 1.0/12 || !r.FloorOutcome.FloorFailed || r.FloorOutcome.MonthsObserved != 36 {
		t.Fatalf("first depletion/recovery %+v outcome %+v", r, r.FloorOutcome)
	}
	if r.FloorOutcome.TotalFundedLivingReal < 19000 || r.FloorOutcome.TotalFundedLivingReal > 20000 || r.FinalBalance < 6000 {
		t.Fatalf("funding recovery %+v outcome %+v", r, r.FloorOutcome)
	}
}

func TestGuardrailRecoveryRunnerFundsIncomeAndBoundsConcurrency(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 8000
	s.PortfolioValue = 0
	s.ProjectionYears = 10
	s.Guardrails = &models.GuardrailConfig{Enabled: true, MinMonthlySpendingReal: 7500, MaxSpendingPct: 120}
	in := engineInput(t, s)
	var active, peak atomic.Int64
	in.Hooks.SocialSecurityProjectionActive = func(*models.WhatIfSettings) bool {
		n := active.Add(1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(time.Microsecond)
		active.Add(-1)
		return true
	}
	in.Hooks.ProjectedSocialSecurityIncome = func(*models.WhatIfSettings, int) float64 { return 1000000 }
	rows, err := runGuardrailOptimizerScenarios(context.Background(), in, 417, 12, 7500)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.FloorOutcome == nil || r.FloorOutcome.MonthsObserved != r.ProjectionYears*12 || r.FloorOutcome.FloorFailed || r.FloorOutcome.TotalFundedLivingReal <= 0 {
			t.Fatalf("income/full horizon invalid: %+v", r.FloorOutcome)
		}
	}
	// The runner bounds workers at min(GOMAXPROCS, runs); 12 runs here.
	if bound := int64(min(runtime.GOMAXPROCS(0), 12)); peak.Load() < 1 || peak.Load() > bound {
		t.Fatalf("concurrency %d exceeds worker bound %d", peak.Load(), bound)
	}
	t.Logf("12 actual paths income-funded through full horizon; peak concurrent hooks=%d", peak.Load())
	in.Hooks.ProjectedSocialSecurityIncome = func(*models.WhatIfSettings, int) float64 { return 0 }
	rows, err = runGuardrailOptimizerScenarios(context.Background(), in, 417, 2, 7500)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if !r.FloorOutcome.FloorFailed || r.FloorOutcome.TotalFundedLivingReal != 0 {
			t.Fatalf("unfunded request counted: %+v", r.FloorOutcome)
		}
	}
	t.Log("unfunded requests score zero real living")
}
