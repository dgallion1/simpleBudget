package analysis

import (
	"math"
	"math/rand"
	"reflect"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

func monteCarloShockSettings(years int) *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent, s.RothPercent = 0, 100
	s.RothStockPercent, s.RothCashPercent = 0, 100
	s.ProjectionYears = years
	return s
}

func deterministicShockConfig() *MonteCarloConfig {
	c := DefaultMonteCarloConfig()
	c.LongevityVariation, c.CrashProbability = 0, 0
	c.SpendingShockProb, c.HealthShockProb = 1, 0
	c.SpendingShockMin, c.SpendingShockMax = 0, 0
	c.HealthShockMin, c.HealthShockMax = 0, 0
	return c
}

func TestMonteCarloEmergencyChargedInFull(t *testing.T) {
	for _, health := range []bool{false, true} {
		s := monteCarloShockSettings(1)
		in := engineInput(t, s)
		c := deterministicShockConfig()
		if health {
			c.SpendingShockProb, c.HealthShockProb = 0, 1
		}
		run := func() models.MonteCarloResult {
			return RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(123)), c)
		}
		baseline := run()
		if health {
			c.HealthShockMin, c.HealthShockMax = 12_000, 12_000
		} else {
			c.SpendingShockMin, c.SpendingShockMax = 12_000, 12_000
		}
		shocked := run()
		delta := baseline.FinalBalance - shocked.FinalBalance
		t.Logf("health=%v: ending-balance delta %.2f", health, delta)
		// This simulator clamps cash returns to [0,15] percent annually.
		// One full event plus lost growth must lie in this interval.
		if delta < 12_000 || delta > 13_800 {
			t.Fatalf("health=%v: emergency cost %.2f, want [12000,13800]", health, delta)
		}
		if shocked.SpendingShocks+shocked.HealthShocks != 1 {
			t.Fatal("expected exactly one emergency event")
		}
	}
}

func TestMonteCarloEmergencyChargeBounds(t *testing.T) {
	tests := []struct {
		name                       string
		years                      int
		spendingProbability        float64
		healthProbability          float64
		spendingAmount, healthCost float64
		wantSpending, wantHealth   int
		minDelta, maxDelta         float64
		guardrails                 bool
		wantIdentical              bool
	}{
		{
			name: "both event types once in one year", years: 1,
			spendingProbability: 1, healthProbability: 1,
			spendingAmount: 12_000, healthCost: 12_000,
			wantSpending: 1, wantHealth: 1, minDelta: 24_000, maxDelta: 27_600,
		},
		{
			name: "one spending event per year for two years", years: 2,
			spendingProbability: 1, spendingAmount: 12_000,
			wantSpending: 2, minDelta: 24_000, maxDelta: 29_670,
		},
		{
			name: "one event remains full with guardrails enabled", years: 1,
			spendingProbability: 1, spendingAmount: 12_000,
			wantSpending: 1, minDelta: 12_000, maxDelta: 13_800, guardrails: true,
		},
		{
			name: "zero probabilities", years: 1,
			spendingAmount: 12_000, healthCost: 50_000,
			minDelta: 0, maxDelta: 0, wantIdentical: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := monteCarloShockSettings(tc.years)
			if tc.guardrails {
				s.Guardrails = &models.GuardrailConfig{
					Enabled: true, FloorDropPct: 20, FloorCutPct: 10,
					CeilingRisePct: 20, CeilingRaisePct: 10,
					MinSpendingPct: 75, MaxSpendingPct: 120,
				}
			}
			in := engineInput(t, s)
			c := deterministicShockConfig()
			c.SpendingShockProb = tc.spendingProbability
			c.HealthShockProb = tc.healthProbability
			c.SpendingShockMin, c.SpendingShockMax = 0, 0
			c.HealthShockMin, c.HealthShockMax = 0, 0
			baseline := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(123)), c)

			c.SpendingShockMin, c.SpendingShockMax = tc.spendingAmount, tc.spendingAmount
			c.HealthShockMin, c.HealthShockMax = tc.healthCost, tc.healthCost
			shocked := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(123)), c)
			delta := baseline.FinalBalance - shocked.FinalBalance
			t.Logf("ending-balance delta %.2f", delta)
			if delta < tc.minDelta || delta > tc.maxDelta {
				t.Fatalf("ending-balance delta %.2f, want [%.2f, %.2f]", delta, tc.minDelta, tc.maxDelta)
			}
			if shocked.SpendingShocks != tc.wantSpending || shocked.HealthShocks != tc.wantHealth {
				t.Fatalf("events spending=%d health=%d, want spending=%d health=%d",
					shocked.SpendingShocks, shocked.HealthShocks, tc.wantSpending, tc.wantHealth)
			}
			if tc.wantIdentical && !reflect.DeepEqual(shocked, baseline) {
				t.Fatalf("zero-probability run changed with nonzero configured amounts:\n baseline: %#v\n shocked:  %#v", baseline, shocked)
			}
		})
	}
}

func taxDeferredShockSettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent, s.RothPercent = 100, 0
	s.TaxDeferredStockPercent, s.TaxDeferredCashPercent = 0, 100
	s.MonthlyLivingExpenses = 1_000
	s.MonthlyHealthcare = 0
	s.HealthcarePersons = nil
	s.MonthlyPropertyTax = 0
	s.IncomeSources = nil
	s.ExpenseSources = nil
	s.SocialSecurity = nil
	s.BigTicketItems = nil
	s.SpendingPhaseConfig = nil
	s.Guardrails = nil
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.TaxableDividendYield = 0
	s.TaxableCapitalGainsDistributionRate = 0
	s.ProjectionYears = 1
	return s
}

func TestMonteCarloShockMatchesPlannedOneTimeExpenseTaxTreatment(t *testing.T) {
	const amount = 50_000.0
	c := deterministicShockConfig()
	c.SpendingShockMin, c.SpendingShockMax = amount, amount
	shock := RunSingleMonteCarloSimulation(
		engineInput(t, taxDeferredShockSettings()), rand.New(rand.NewSource(123)), c)

	plannedSettings := taxDeferredShockSettings()
	plannedSettings.OneTimeExpenses = []models.OneTimeExpense{{ID: "shock-control", Year: 0, Amount: amount}}
	c.SpendingShockMin, c.SpendingShockMax = 0, 0
	planned := RunSingleMonteCarloSimulation(
		engineInput(t, plannedSettings), rand.New(rand.NewSource(123)), c)
	t.Logf("tax-deferred parity: shock final balance %.2f, planned final balance %.2f", shock.FinalBalance, planned.FinalBalance)

	if diff := math.Abs(shock.FinalBalance - planned.FinalBalance); diff > 0.01 {
		t.Fatalf("shock final balance %.2f, planned one-time expense %.2f (difference %.2f); same-dollar discrete expenses need identical tax treatment",
			shock.FinalBalance, planned.FinalBalance, diff)
	}
	if shock.SpendingShocks != 1 || planned.SpendingShocks != 1 {
		t.Fatalf("expected preserved event draw in both arms, got shock=%d planned=%d", shock.SpendingShocks, planned.SpendingShocks)
	}
}

func TestShockTaxEstimatorCarriesDiscreteWithdrawalAtFaceValue(t *testing.T) {
	tests := []struct {
		name        string
		month       int
		accumulator engine.ProjectionTaxAccumulator
	}{
		{name: "month zero", month: 0},
		{
			name:  "later month with recurring withdrawals YTD",
			month: 5,
			accumulator: engine.ProjectionTaxAccumulator{
				TaxableWithdrawalsYTD: 5_000,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.accumulator.AnnualizedInputs(tc.month, engine.MonthlyTaxInputs{
				TaxableWithdrawals: 51_000,
				OneTimeIncome: engine.OneTimeExpenseIncome{
					TaxDeferredWithdrawals: 50_000,
				},
			})
			// Month zero: recurring $1k x 12 + discrete $50k = $62k.
			// Month five: six recurring $1k months x 2 + discrete $50k = $62k.
			if got.TaxableWithdrawals != 62_000 {
				t.Fatalf("taxable withdrawals %.2f, want 62000; the $50k discrete draw must not be annualized", got.TaxableWithdrawals)
			}
		})
	}
}

func TestStepMonthClassifiesShockAsOneTimeIncome(t *testing.T) {
	for _, shockMonth := range []int{0, 5} {
		t.Run(map[int]string{0: "month zero", 5: "later month"}[shockMonth], func(t *testing.T) {
			st := engine.NewProjectionState(engineInput(t, taxDeferredShockSettings()))
			var out engine.MonthOutcome
			for m := 0; m <= shockMonth; m++ {
				priorWithdrawals := st.TaxState.TaxableWithdrawalsYTD
				out = st.StepMonth(m, func(_ *models.WhatIfSettings, month int) engine.MonthReturns {
					returns := engine.MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
					if month == shockMonth {
						returns.ExtraExpenses = 50_000
					}
					return returns
				})
				if m != shockMonth {
					continue
				}

				if got := out.Result.OneTimeIncome.TaxDeferredWithdrawals; math.Abs(got-50_000) > 0.01 {
					t.Fatalf("one-time tax-deferred withdrawal %.2f, want 50000", got)
				}
				factor := 12.0 / float64(m%12+1)
				withdrawal := out.Result.CashFlow.WithdrawalFromTaxDeferred
				want := (priorWithdrawals+withdrawal-50_000)*factor + 50_000
				if got := out.Result.TaxSnapshot.AnnualInputs.TaxableWithdrawals; math.Abs(got-want) > 0.01 {
					t.Fatalf("annualized taxable withdrawals %.2f, want %.2f; shock must remain discrete alongside recurring withdrawals", got, want)
				}
			}
		})
	}
}

func TestMonteCarloWithdrawalDelayDiagnostic(t *testing.T) {
	settings := taxDeferredShockSettings()
	settings.PortfolioValue = 100_000
	settings.MonthlyLivingExpenses = 0
	settings.ProjectionYears = 2
	settings.TaxDeferredDelayYears = 1
	in := engineInput(t, settings)

	c := deterministicShockConfig()
	control := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(123)), c)
	c.SpendingShockMin, c.SpendingShockMax = 12_000, 12_000
	shocked := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(123)), c)
	if control.SpendingShocks != 2 || shocked.SpendingShocks != 2 {
		t.Fatalf("expected two sampled events in each arm, got control=%d shocked=%d", control.SpendingShocks, shocked.SpendingShocks)
	}

	type boundaryOutcome struct {
		withdrawal float64
		shortfall  float64
	}
	trace := func(amount float64) [2]boundaryOutcome {
		st := engine.NewProjectionState(in)
		var boundaries [2]boundaryOutcome
		for m := 0; m < 24; m++ {
			out := st.StepMonth(m, func(_ *models.WhatIfSettings, month int) engine.MonthReturns {
				returns := engine.MonthReturns{HealthcareMultiplier: 1, DiscretionaryMultiplier: 1}
				if month%12 == 0 {
					returns.ExtraExpenses = amount
				}
				return returns
			})
			if m%12 == 0 {
				boundaries[m/12] = boundaryOutcome{
					withdrawal: out.Result.CashFlow.GrossWithdrawal(),
					shortfall:  out.Result.Shortfall,
				}
			}
		}
		return boundaries
	}
	baseline := trace(0)
	shockTrace := trace(12_000)
	if delta := shockTrace[0].shortfall - baseline[0].shortfall; math.Abs(delta-12_000) > 0.01 {
		t.Fatalf("delay-window month shock shortfall delta %.2f, want 12000", delta)
	}
	if delta := shockTrace[0].withdrawal - baseline[0].withdrawal; math.Abs(delta) > 0.01 {
		t.Fatalf("delay-window month funded-withdrawal delta %.2f, want 0", delta)
	}
	yearOneWithdrawalDelta := shockTrace[1].withdrawal - baseline[1].withdrawal
	if yearOneWithdrawalDelta < 12_000 || yearOneWithdrawalDelta >= 24_000 {
		t.Fatalf("post-delay event funded-withdrawal delta %.2f, want [12000,24000); prior unpaid need must be diagnosed separately", yearOneWithdrawalDelta)
	}
	if delta := shockTrace[1].shortfall - baseline[1].shortfall; math.Abs(delta) > 0.01 {
		t.Fatalf("post-delay shortfall delta %.2f, want 0", delta)
	}
	t.Logf("delay diagnostic: year-0 event withdrawal delta %.2f, shortfall delta %.2f; year-1 event withdrawal delta %.2f, shortfall delta %.2f",
		shockTrace[0].withdrawal-baseline[0].withdrawal,
		shockTrace[0].shortfall-baseline[0].shortfall,
		yearOneWithdrawalDelta,
		shockTrace[1].shortfall-baseline[1].shortfall)
}
