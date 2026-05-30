package analysis

import (
	"math"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// When spending phases are enabled the projection applies an age-stepped
// multiplier to living expenses; the Present Value panel must do the same
// rather than falling back to the phase-blind flat-inflation annuity.
func TestPresentValue_LivingExpensesUseSpendingPhases(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.SocialSecurity = nil
	s.IncomeSources = nil
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.MonthlyPropertyTax = 0
	s.ExpenseSources = nil
	s.MonthlyLivingExpenses = 5000
	s.CurrentAge = 70
	s.DiscountRate = 5.0
	s.InflationRate = 3.0
	s.SpendingDeclineRate = 0
	s.ProjectionYears = 30
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{StartAge: 0, Multiplier: 1.0},
			{StartAge: 80, Multiplier: 0.75},
		},
	}

	in := engineInput(t, s)
	result := PresentValue(in, nil)

	// Oracle uses the prepared settings PresentValue actually reads, not the
	// raw input (prepare deep-copies and normalizes).
	ps := in.Prepared.Settings()
	months := ps.ProjectionYears * 12
	// The phase-aware PV equals the discounted per-month living expense the
	// engine actually projects.
	want := presentValueOfMonthlyStream(func(m int) float64 {
		return engine.LivingExpensesAtMonth(ps, m)
	}, ps.DiscountRate, months)
	if math.Abs(result.PVExpenses-want) > 0.01 {
		t.Errorf("PVExpenses = %.2f, want phase-aware %.2f", result.PVExpenses, want)
	}

	// And it must differ from the phase-blind flat-inflation annuity that
	// the old code used (the 0.75 multiplier after age 80 lowers it).
	flat := engine.PresentValueAnnuity(ps.MonthlyLivingExpenses, ps.DiscountRate, ps.InflationRate-ps.SpendingDeclineRate, 0, months)
	if math.Abs(result.PVExpenses-flat) < 1.0 {
		t.Errorf("expected phase-aware PV to differ from flat-inflation PV %.2f, got %.2f", flat, result.PVExpenses)
	}
	if !(result.PVExpenses < flat) {
		t.Errorf("declining phases should lower PV: phase-aware=%.2f flat=%.2f", result.PVExpenses, flat)
	}
}
