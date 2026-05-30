package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
)

// The steady-state withdrawal RATE must divide the steady-state gap by the
// projection's ACTUAL portfolio balance at that month — which reflects
// drawdown from withdrawals/RMDs — not a naive compound-growth estimate.
// Using the inflated compound estimate understates the rate and is
// inconsistent with the gap numerator, which already comes from the
// projection's drawn-down balances.
func TestBudgetFit_SteadyStateRateUsesProjectionBalance(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.IncomeSources = nil
	s.SocialSecurity = nil
	s.CurrentAge = 55 // steady-state age 65 → no RMD muddying the gap
	s.InvestmentReturn = 5.0
	s.PortfolioValue = 2_000_000
	s.MonthlyLivingExpenses = 6000
	s.ProjectionYears = 30
	s.SteadyStateOverrideYear = 10 // month 120, well inside the projection

	proj, in := runProj(t, s)
	result := BudgetFit(in, proj)

	if result.SteadyStateGap <= 0 {
		t.Fatalf("test needs a positive steady-state gap, got %.2f", result.SteadyStateGap)
	}

	const idx = 120 // SteadyStateOverrideYear * 12
	balance := proj.Months[idx].PortfolioBalance
	if balance <= 0 {
		t.Fatalf("projection depleted before steady-state month; pick a survivable scenario")
	}
	wantRate := result.SteadyStateGap * 12 / balance * 100

	if math.Abs(result.SteadyStateRate-wantRate) > 1e-6 {
		t.Errorf("SteadyStateRate = %.6f, want %.6f (should divide the gap by the projection's actual portfolio balance, not a compound-growth estimate)",
			result.SteadyStateRate, wantRate)
	}
}
