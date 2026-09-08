package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
	"encoding/json"
	"reflect"
	"testing"
)

func TestOptimizerCandidateSettingsPersistExactProjection(t *testing.T) {
	s := eligibleBase()
	s.ProjectionYears = 8
	in := engineInput(t, s)
	s = in.Prepared.Settings()
	for _, strat := range []models.RothOptimizerStrategy{
		{Kind: models.RothStrategyNone},
		{Kind: models.RothStrategyLadder, AnnualAmount: 12345.67, StartAge: 67, EndAge: 70},
		{Kind: models.RothStrategyBracketFill, TargetBracket: .22, StartAge: 67, EndAge: 72},
	} {
		t.Run(string(strat.Kind), func(t *testing.T) {
			c := scoreCandidate(engine.New(), in, 70, 67, strat)
			got, err := SettingsForTaxOptimizerCandidate(s, c)
			if err != nil {
				t.Fatal(err)
			}
			if got.SocialSecurity.ClaimAge != 70 || got.SocialSecurity.SpouseClaimAge != 67 {
				t.Fatal("SS pair lost")
			}
			if strat.Kind == models.RothStrategyBracketFill && (len(got.RothConversion.PerYearOverrides) == 0 || got.RothConversion.PerYearOverrides[0] <= 0) {
				t.Fatal("fixture did not produce bracket-fill schedule")
			}
			want := rothStrategyToConfig(candidateSettingsForSS(s, 70, 67), strat, c.BracketFillFeedback)
			if !reflect.DeepEqual(got.RothConversion, want) {
				t.Fatalf("wrong exact config %#v want %#v", got.RothConversion, want)
			}
			raw, _ := json.Marshal(got)
			var reloaded models.WhatIfSettings
			if err := json.Unmarshal(raw, &reloaded); err != nil {
				t.Fatal(err)
			}
			proj := engine.New().Run(engine.Input{Prepared: prepare.MustFrom(t, &reloaded), Chain: in.Chain, Hooks: in.Hooks})
			scored := projectionToCandidate(proj, 70, 67, strat)
			if scored.EndingPortfolioReal != c.EndingPortfolioReal || scored.LifetimeTaxReal != c.LifetimeTaxReal {
				t.Fatalf("reload changed projection: %#v versus %#v", scored, c)
			}
			got.RothConversion = s.RothConversion
			got.SocialSecurity = s.SocialSecurity
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(s)
			if string(a) != string(b) {
				t.Fatal("unrelated settings changed")
			}
		})
	}
}
