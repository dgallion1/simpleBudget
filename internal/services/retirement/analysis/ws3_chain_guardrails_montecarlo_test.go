package analysis

import (
	"math/rand"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// ws3MCChainFixture builds the primary (guardrails ON) half of the WS3 chain
// Monte Carlo fixture: a short guardrails-enabled step that hands off, at
// year 2, to a step with NO guardrails and a large discretionary expense
// (built by ws3MCChainedStep). No income, no inflation, no phases —
// portfolio survival depends only on whether adaptive spending cuts the
// discretionary source during the (guaranteed) crash years.
func ws3MCChainFixture() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{{ID: "p1", Name: "You", Role: models.PersonRolePrimary, BirthMonth: models.BirthMonthForAge(s.StartDate, 65)}}
	s.CurrentAge = 65
	s.PortfolioValue = 300_000
	s.MonthlyLivingExpenses = 1_000
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 0
	s.InflationRate = 0
	s.SpendingDeclineRate = 0
	s.ProjectionYears = 10
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: false}
	s.IncomeSources = nil
	s.TaxDeferredPercent = 0
	s.RothPercent = 100
	s.Guardrails = &models.GuardrailConfig{
		Enabled: true, FloorDropPct: 5, FloorCutPct: 10,
		CeilingRisePct: 1000, CeilingRaisePct: 0,
		MinSpendingPct: 40, MaxSpendingPct: 200,
	}
	return s
}

// ws3MCChainedStep is the chained-into step: guardrails OFF (on its own
// settings — under D3' this is never consulted), and a large discretionary
// expense source. Whether the portfolio survives the projection now depends
// ENTIRELY on whether Monte Carlo's adaptive spending cuts this source
// during a crash.
func ws3MCChainedStep() *models.WhatIfSettings {
	s := ws3MCChainFixture()
	s.Guardrails = nil
	s.ExpenseSources = []models.ExpenseSource{
		{ID: "disc", Name: "Discretionary", Amount: 8_000, Discretionary: true},
	}
	return s
}

// ws3MCConfig guarantees a "crash" (stock return < -15%) every single year
// (CrashProbability: 1.0), so adaptive spending -- when not suppressed --
// triggers from the chained step's very first year and never lapses within
// this 10-year projection. DiscretionaryCutPercent: 100 makes the effect
// unambiguous: the $8,000/mo discretionary source is either fully paid or
// fully cut, nothing in between.
func ws3MCConfig() *MonteCarloConfig {
	return &MonteCarloConfig{
		CrashProbability:        1.0,
		CrashSeverity:           -30,
		AdaptiveSpending:        true,
		DiscretionaryCutPercent: 100,
		AdaptationRecoveryYears: 100,
	}
}

// TestMonteCarloAdaptiveSpendingFollowsPrimaryGuardrails is the WS3
// attempt-3 permanent regression for D3': after an on->off chain
// transition, Monte Carlo's adaptive-spending suppression must still follow
// the VIEWED (primary) scenario's guardrail setting, not the active
// (chained-into) step's — even though the active step's OWN settings have
// guardrails off. The primary has guardrails on, so adaptive spending stays
// SUPPRESSED for the whole projection (guardrails, not adaptive spending,
// is meant to own the response to a crash); the $8,000/mo discretionary
// source is paid in full through every guaranteed crash year, and — because
// the guardrail multiplier only ever discounts CurrentLivingExpenses, never
// ExpenseSources — nothing else cuts that spending, so the portfolio
// depletes.
//
// Before this fix (WS3 attempt 2's D3), Monte Carlo asked
// engine.GuardrailsGovernStep(s) with s the ACTIVE settings: false in the
// chained step, so adaptive spending fired and cut the discretionary source
// to $0 every crash year, and the portfolio survived. That is exactly the
// mutation this test kills: reverting the predicate's argument from
// st.Primary() back to the active-settings parameter s reproduces the old
// (wrong, under D3') Survives=true outcome.
func TestMonteCarloAdaptiveSpendingFollowsPrimaryGuardrails(t *testing.T) {
	primary := ws3MCChainFixture()
	next := ws3MCChainedStep()

	in := engineInput(t, primary)
	in.Chain = []engine.PreparedChainLink{{
		ScenarioFilename: "next.json",
		TransitionAge:    primary.CurrentAge + 2,
		Settings:         engineInput(t, next).Prepared,
	}}
	in.Hooks.ResolveChainTransition = func(currentYear, nextChainIndex int, _ *models.WhatIfSettings, chain []engine.PreparedChainLink) (int, *models.WhatIfSettings) {
		if nextChainIndex == 0 && currentYear >= 2 {
			return 1, chain[0].Settings.Settings()
		}
		return nextChainIndex, nil
	}

	result := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(1)), ws3MCConfig())

	if result.Survives {
		t.Fatalf("expected the portfolio to DEPLETE (primary's guardrails govern, so Monte Carlo's adaptive spending must stay suppressed and the $8,000/mo discretionary source is paid in full every crash year): final balance %v", result.FinalBalance)
	}
}
