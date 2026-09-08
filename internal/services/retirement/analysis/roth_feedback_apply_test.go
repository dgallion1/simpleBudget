package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
	"encoding/json"
	"reflect"
	"testing"
)

func TestApplyRothFeedbackScheduleSurvivesReload(t *testing.T) {
	in := engineInput(t, adversarialOverlapSettings(t))
	s := in.Prepared.Settings()
	strat := models.RothOptimizerStrategy{Kind: models.RothStrategyBracketFill, TargetBracket: .12, StartAge: 50, EndAge: 59}
	c := scoreCandidate(engine.New(), in, 70, 0, strat)
	if c.BracketFillFeedback[overshootYear] <= 100 {
		t.Fatal("fixture did not exercise Roth earnings feedback")
	}
	exact, err := SettingsForTaxOptimizerCandidate(s, c)
	if err != nil {
		t.Fatal(err)
	}
	uncorrected := rothStrategyToConfig(candidateSettingsForSS(s, 70, 0), strat, nil)
	if reflect.DeepEqual(exact.RothConversion, uncorrected) {
		t.Fatal("feedback ignored")
	}
	raw, _ := json.Marshal(exact)
	var reload models.WhatIfSettings
	if err := json.Unmarshal(raw, &reload); err != nil {
		t.Fatal(err)
	}
	prepared := prepare.MustFrom(t, &reload)
	got := projectionToCandidate(engine.New().Run(engine.Input{Prepared: prepared, Chain: in.Chain, Hooks: in.Hooks}), 70, 0, strat)
	if got.EndingPortfolioReal != c.EndingPortfolioReal || got.LifetimeTaxReal != c.LifetimeTaxReal {
		t.Fatal("saved feedback schedule differs from scored projection")
	}
	// All stochastic consumers receive the same prepared JSON-visible schedule.
	original, ok := cloneFinalistForMonteCarlo(s, c)
	if !ok {
		t.Fatal("clone failed")
	}
	a := MonteCarlo(engine.New(), engine.Input{Prepared: original}, 4, 123)
	b := MonteCarlo(engine.New(), engine.Input{Prepared: prepared}, 4, 123)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("reloaded schedule differs in Monte Carlo")
	}
}
