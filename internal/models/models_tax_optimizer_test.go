package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRothStrategyKind_Constants(t *testing.T) {
	cases := map[RothStrategyKind]string{
		RothStrategyNone:        "none",
		RothStrategyLadder:      "ladder",
		RothStrategyBracketFill: "bracket_fill",
	}
	for k, want := range cases {
		if string(k) != want {
			t.Errorf("RothStrategyKind constant: got %q, want %q", string(k), want)
		}
	}
}

func TestTaxOptimizerAnalysis_JSONShape(t *testing.T) {
	a := TaxOptimizerAnalysis{
		Eligible:         true,
		Baseline:         TaxOptimizerCandidate{PrimaryClaimAge: 67, SpouseClaimAge: 62},
		Best:             TaxOptimizerCandidate{PrimaryClaimAge: 70, RothStrategy: RothOptimizerStrategy{Kind: RothStrategyLadder, Label: "best"}},
		MonteCarloRuns:   32,
		CandidatesScored: 135,
	}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)

	// Wire-level field names must remain stable for the handler/template.
	mustContain := []string{
		`"eligible":true`,
		`"baseline":{`,
		`"best":{`,
		`"monte_carlo_runs":32`,
		`"candidates_scored":135`,
		`"primary_claim_age":67`,
		`"spouse_claim_age":62`,   // present when non-zero
		`"primary_claim_age":70`,
		`"kind":"ladder"`,
		`"label":"best"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(body, want) {
			t.Errorf("expected JSON to contain %q; got %s", want, body)
		}
	}

	// ineligible_reason omitempty: not present when empty.
	if strings.Contains(body, "ineligible_reason") {
		t.Errorf("ineligible_reason should be omitted when empty; got %s", body)
	}

	// Best candidate's SpouseClaimAge is 0 → omitempty drops it.
	if strings.Contains(body, `"primary_claim_age":70,"spouse_claim_age"`) {
		t.Errorf("expected SpouseClaimAge to be omitted when 0; got %s", body)
	}

	// Round-trip: re-parse and check key fields survived.
	var back TaxOptimizerAnalysis
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.Eligible || back.MonteCarloRuns != 32 || back.CandidatesScored != 135 {
		t.Errorf("round-trip lost fields: %+v", back)
	}
	if back.Best.RothStrategy.Kind != RothStrategyLadder || back.Best.RothStrategy.Label != "best" {
		t.Errorf("round-trip lost Best.RothStrategy: %+v", back.Best.RothStrategy)
	}
}
