package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/history"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

// Literal full-horizon totals catch omission/double-addition and ensure scheduled
// expiry does not register as a below-plan cut in the actual MC observer.
func TestLivingSpendingBoostProjectionLoopParity(t *testing.T) {
	for _, phase := range []bool{false, true} {
		s := floorRegressionSettings()
		s.StartDate = "2026-09"
		s.Persons[0].BirthMonth = "1961-09"
		s.ProjectionYears = 11
		s.PortfolioValue = 10000000
		s.Guardrails = nil
		s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 1000, StopMonth: "2036-09"}
		wantTotal := 1440000.0
		if phase {
			s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "constant", StartAge: 0, Multiplier: 1.1}}}
			wantTotal = 1572000
		}
		projection, _ := runProj(t, s)
		total := 0.0
		for _, month := range projection.Months {
			total += month.FundedLivingExpenses
		}
		if math.Abs(total-wantTotal) > 1e-6 {
			t.Fatalf("canonical total %g want %g", total, wantTotal)
		}
		data := make(history.Data, 11)
		for year := range data {
			data[year] = models.HistoricalYear{Year: 1980 + year}
		}
		historical := runSingleHistoricalSequence(engineInput(t, s), data, 1980)
		if math.Abs(historical.FinalBalance-(10000000-wantTotal)) > 1e-6 {
			t.Fatalf("backtest total wrong: %+v", historical)
		}
		for _, inflation := range []float64{0, 12} {
			s.InflationRate = inflation
			in := engineInput(t, s)
			cfg := &MonteCarloConfig{MinMonthlySpendingReal: 100, CaptureSpendingYears: true}
			mc := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(812)), cfg)
			replay := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(812)), cfg)
			if !reflect.DeepEqual(mc, replay) {
				t.Fatal("prepared-input replay changed")
			}
			if mc.FloorOutcome == nil || mc.FloorOutcome.MonthsObserved != 132 {
				t.Fatal("missing full horizon")
			}
			// With phases the whole base follows CPI; without phases the zero-decline
			// base follows the same injected rate, so today's-dollar totals also agree.
			if math.Abs(mc.FloorOutcome.TotalFundedLivingReal-wantTotal) > 0.01 {
				t.Fatalf("MC real total %g want %g", mc.FloorOutcome.TotalFundedLivingReal, wantTotal)
			}
			if mc.GuardrailImpact.MonthsBelowPlan != 0 || mc.GuardrailImpact.FundingGapMonths != 0 {
				t.Fatalf("expiry counted as cut: %+v", mc.GuardrailImpact)
			}
		}
	}
}

// Historical inflation is path CPI, not the configured deterministic rate.
func TestLivingSpendingBoostHistoricalCPI(t *testing.T) {
	s := floorRegressionSettings()
	s.Guardrails = nil
	s.PortfolioValue = 10000000
	s.MonthlyLivingExpenses = 0
	s.ProjectionYears = 2
	s.StartDate = "2026-09"
	s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 1000, StopMonth: "2027-10"}
	data := history.Data{{Year: 1980, InflationRate: 100}, {Year: 1981, InflationRate: 100}}
	result := runSingleHistoricalSequence(engineInput(t, s), data, 1980)
	// Thirteen boosted months: geometric CPI progression 2^(m/12), m=0..12.
	want := 1000 * (math.Pow(2, 13.0/12) - 1) / (math.Pow(2, 1.0/12) - 1)
	if math.Abs((10000000-result.FinalBalance)-want) > 1e-6 {
		t.Fatalf("historical boosted withdrawals %g want %g", (10000000 - result.FinalBalance), want)
	}
}
