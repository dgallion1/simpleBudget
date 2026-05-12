package models

import (
	"testing"
)

func TestTaxOptimizerAnalysis_ZeroValue(t *testing.T) {
	var a TaxOptimizerAnalysis
	if a.Eligible {
		t.Error("zero-value Eligible should be false")
	}
	if a.CandidatesScored != 0 {
		t.Error("zero-value CandidatesScored should be 0")
	}
	if a.Top != nil {
		t.Error("zero-value Top should be nil slice")
	}
}

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

func TestWhatIfAnalysis_HasTaxOptimizerField(t *testing.T) {
	a := WhatIfAnalysis{TaxOptimizer: &TaxOptimizerAnalysis{Eligible: true}}
	if a.TaxOptimizer == nil || !a.TaxOptimizer.Eligible {
		t.Error("WhatIfAnalysis.TaxOptimizer should hold a pointer to TaxOptimizerAnalysis")
	}
}
