package analysis

import (
	"fmt"
	"testing"

	"budget2/internal/models"
)

func TestEnumerateLadderStrategies_DefaultShape(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:      67,
		ProjectionYears: 31,
		SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 67},
	}
	strategies := enumerateLadderStrategies(s)

	if len(strategies) == 0 {
		t.Fatal("expected non-empty strategy slice")
	}

	// All have Kind=ladder.
	for _, st := range strategies {
		if st.Kind != models.RothStrategyLadder {
			t.Errorf("expected Kind=ladder, got %q", st.Kind)
		}
	}

	// All windows respect currentAge and are non-degenerate.
	for _, st := range strategies {
		if st.StartAge < s.CurrentAge {
			t.Errorf("window starts before currentAge: %+v", st)
		}
		if st.EndAge <= st.StartAge {
			t.Errorf("invalid window: %+v", st)
		}
	}

	// Each strategy has a non-empty Label.
	for _, st := range strategies {
		if st.Label == "" {
			t.Errorf("missing Label: %+v", st)
		}
	}
}

func TestEnumerateLadderStrategies_SkipsZeroAmountDups(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:      60,
		ProjectionYears: 35,
		SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 67},
	}
	strategies := enumerateLadderStrategies(s)

	zeroCount := 0
	for _, st := range strategies {
		if st.AnnualAmount == 0 {
			zeroCount++
		}
	}
	// Only one $0 candidate (no-conversion baseline) — not one per window.
	if zeroCount != 1 {
		t.Errorf("expected exactly one $0 ladder candidate, got %d", zeroCount)
	}
}

func TestEnumerateLadderStrategies_LabelsAreStable(t *testing.T) {
	s := &models.WhatIfSettings{
		CurrentAge:      67,
		ProjectionYears: 31,
		SocialSecurity:  &models.SocialSecurityConfig{ClaimAge: 70},
	}
	strategies := enumerateLadderStrategies(s)

	// Find the $100k/yr to RMD age (73) candidate and assert its label.
	var found *models.RothOptimizerStrategy
	for i, st := range strategies {
		if st.AnnualAmount == 100_000 && st.EndAge == 73 {
			found = &strategies[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected to find $100k/yr 67→73 candidate")
	}
	want := "$100k/yr 67→73"
	if found.Label != want {
		t.Errorf("label: got %q, want %q", found.Label, want)
	}
}

// Compile guard so fmt import is exercised in this file even if no test uses it directly.
var _ = fmt.Sprintf
