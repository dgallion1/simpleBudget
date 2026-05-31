package analysis

import (
	"math/rand"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/history"
)

// highMAGIMedicareSeedScenario builds a Medicare-eligible household with high
// fixed ordinary income (a large pension) and a short, two-year horizon. With
// ProjectionYears == 2, every year of the run falls inside the IRMAA two-year
// MAGI lookback window that has no completed history (years 0 and 1), so the
// entire run's IRMAA depends on the assumed-lookback seed. A loop that fails to
// seed the lookback reports $0 IRMAA for the whole run.
func highMAGIMedicareSeedScenario(t *testing.T) *models.WhatIfSettings {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	// Primary is 70 → Medicare-eligible from year 0 (age derived from BirthMonth;
	// prepare overrides CurrentAge).
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 70)
	s.SocialSecurity = nil
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.PortfolioValue = 2_000_000
	s.TaxDeferredPercent = 100 // all pre-tax; pension drives MAGI, not RMD (age < 73)
	s.RothPercent = 0
	s.InvestmentReturn = 5.0
	s.InflationRate = 3.0
	s.MonthlyLivingExpenses = 5000
	s.ProjectionYears = 2
	s.TaxConfig = &models.TaxConfig{FilingStatus: models.FilingMarriedJoint}
	// ~$250k/yr ordinary income → MAGI well above the MFJ IRMAA tier-1 threshold.
	s.IncomeSources = []models.IncomeSource{{Name: "Pension", Amount: 20834, StartMonth: 0}}
	return s
}

// The Monte Carlo loop must seed the IRMAA two-year MAGI lookback for years
// 0-1, exactly as the deterministic projection does. Without the seed a
// high-MAGI Medicare-eligible household is charged $0 IRMAA in the only years
// this short run covers, diverging from the deterministic projection.
func TestMonteCarlo_SeedsEarlyYearIRMAA(t *testing.T) {
	s := highMAGIMedicareSeedScenario(t)
	in := engineInput(t, s)
	rng := rand.New(rand.NewSource(1))
	cfg := DefaultMonteCarloConfig()
	cfg.LongevityVariation = 0 // keep the horizon at exactly 2 years

	result := RunSingleMonteCarloSimulation(in, rng, cfg)

	if result.TotalIRMAA <= 0 {
		t.Errorf("Monte Carlo TotalIRMAA = %.2f, want > 0 (early-year IRMAA lookback not seeded)", result.TotalIRMAA)
	}
}

// The historical backtest loop must seed the IRMAA two-year MAGI lookback for
// years 0-1 just like the deterministic projection.
func TestBacktest_SeedsEarlyYearIRMAA(t *testing.T) {
	s := highMAGIMedicareSeedScenario(t)
	in := engineInput(t, s)

	result := runSingleHistoricalSequence(in, history.DefaultData(), 1990)

	if result.TotalIRMAA <= 0 {
		t.Errorf("backtest TotalIRMAA = %.2f, want > 0 (early-year IRMAA lookback not seeded)", result.TotalIRMAA)
	}
}
