package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/history"
	"budget2/internal/services/retirement/prepare"
	"encoding/json"
	"reflect"
	"testing"
)

func TestRA1AdversarialBacktest(t *testing.T) {
	in := engineInput(t, adversarialOverlapSettings(t))
	s := in.Prepared.Settings()
	c := scoreCandidate(engine.New(), in, 70, 0, models.RothOptimizerStrategy{Kind: models.RothStrategyBracketFill, TargetBracket: .12, StartAge: 50, EndAge: 59})
	orig, ok := cloneFinalistForMonteCarlo(s, c)
	if !ok {
		t.Fatal("clone")
	}
	exact, err := SettingsForTaxOptimizerCandidate(s, c)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exact)
	var reload models.WhatIfSettings
	if err := json.Unmarshal(raw, &reload); err != nil {
		t.Fatal(err)
	}
	prep := prepare.MustFrom(t, &reload)
	a := HistoricalBacktest(engine.Input{Prepared: orig}, history.DefaultData())
	b := HistoricalBacktest(engine.Input{Prepared: prep}, history.DefaultData())
	if a.TotalSequences == 0 {
		t.Fatal("vacuous")
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("historical persisted schedule mismatch")
	}
	t.Logf("%d historical sequences match", a.TotalSequences)
}
