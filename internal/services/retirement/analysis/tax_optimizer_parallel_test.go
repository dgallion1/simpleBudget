package analysis

import (
	"reflect"
	"testing"

	"budget2/internal/services/retirement/engine"
)

// scoreCandidates fans candidate scoring out across workers but must hand
// back exactly the sequence a sequential pairs-outer / strategies-inner loop
// would build: same candidates, same order, failed projections dropped. The
// later sort.SliceStable relies on that order to break ties identically.
func TestScoreCandidates_MatchesSequentialOrder(t *testing.T) {
	s := eligibleBase()
	prep := perturbAndPrepare(s)
	in := engine.Input{Prepared: prep}
	eng := engine.New()
	settings := in.Prepared.Settings()

	pairs := []ssPair{{Primary: 67, Spouse: 0}, {Primary: 70, Spouse: 0}}
	strategies := enumerateRothStrategies(settings)
	if len(strategies) < 2 {
		t.Fatalf("fixture enumerates %d strategies; need several to detect slot mixups", len(strategies))
	}

	var want []candidateResult
	for _, p := range pairs {
		for _, strat := range strategies {
			cand := scoreCandidate(eng, in, p.Primary, p.Spouse, strat)
			want = append(want, candidateResult{cand: cand, ok: !candidateFailed(cand)})
		}
	}

	got := scoreCandidates(eng, in, pairs, strategies)

	if len(got) != len(want) {
		t.Fatalf("scoreCandidates returned %d slots, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("slot %d differs from sequential scoring:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}
