package whatif

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

// TestBuildConversionSweepRows_UsesDefaultHooks pins the regression where
// the sweep ran engine.Run with zero-valued Hooks and therefore projected
// every row WITHOUT Social Security optimizer income: the table depleted
// decades before the plan it claimed to sweep (found 2026-09-03 when the
// saved plan survived 39 years but every sweep row died at ~21).
//
// Every row must match a direct engine run under retirement.DefaultHooks,
// and — so the test cannot pass vacuously — that run must actually carry
// SS income and must differ from the zero-hooks run.
func TestBuildConversionSweepRows_UsesDefaultHooks(t *testing.T) {
	settings := sweepScenarioSettings()
	settings.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 3000,
		FRA:        67,
		COLARate:   0.02,
		ClaimAge:   67,
	}

	rows, err := buildConversionSweepRows(settings)
	if err != nil {
		t.Fatalf("buildConversionSweepRows: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows")
	}

	eng := engine.New()
	for _, row := range rows {
		candidate := candidateSettingsForConversionAmount(settings, row.Amount)
		prepared, err := prepare.From(candidate)
		if err != nil {
			t.Fatalf("prepare amount %.0f: %v", row.Amount, err)
		}
		withHooks := eng.Run(engine.Input{Prepared: prepared, Hooks: retirement.DefaultHooks()})
		noHooks := eng.Run(engine.Input{Prepared: prepared})

		ssSeen := false
		for _, m := range withHooks.Months {
			if m.SocialSecurityIncome > 0 {
				ssSeen = true
				break
			}
		}
		if !ssSeen {
			t.Fatalf("amount %.0f: DefaultHooks run carries no SS income; fixture does not exercise the hook", row.Amount)
		}
		if finalReal(withHooks) == finalReal(noHooks) {
			t.Fatalf("amount %.0f: hooks make no difference to the ending balance; fixture does not discriminate", row.Amount)
		}

		if row.Survives != withHooks.Survives || (!row.Survives && row.DepletionMonth != depletion(withHooks)) {
			t.Errorf("amount %.0f: row survives=%v depletion=%d; DefaultHooks run survives=%v depletion=%d (zero-hooks run: survives=%v depletion=%d) — sweep is not using DefaultHooks",
				row.Amount, row.Survives, row.DepletionMonth, withHooks.Survives, depletion(withHooks), noHooks.Survives, depletion(noHooks))
		}
		if row.Survives && row.EndingBalanceReal != finalReal(withHooks) {
			t.Errorf("amount %.0f: row ending real balance %.2f, DefaultHooks run %.2f (zero-hooks run %.2f) — sweep is not using DefaultHooks",
				row.Amount, row.EndingBalanceReal, finalReal(withHooks), finalReal(noHooks))
		}
		wantTax, wantIRMAA := lifetimeRealTaxAndIRMAA(withHooks)
		if row.LifetimeTax != wantTax || row.LifetimeIRMAA != wantIRMAA {
			t.Errorf("amount %.0f: lifetime tax/IRMAA %.2f/%.2f, want %.2f/%.2f", row.Amount, row.LifetimeTax, row.LifetimeIRMAA, wantTax, wantIRMAA)
		}
	}
}

func depletion(p *models.ProjectionResult) int {
	if p == nil || p.DepletionMonth == nil {
		return -1
	}
	return *p.DepletionMonth
}

func finalReal(p *models.ProjectionResult) float64 {
	if p == nil || len(p.Months) == 0 {
		return 0
	}
	return p.Months[len(p.Months)-1].PortfolioBalanceReal
}
