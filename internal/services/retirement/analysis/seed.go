package analysis

import "time"

// EffectiveSeed resolves a caller-supplied Monte Carlo seed into the ONE
// non-zero seed shared by every MC consumer of an analysis pass (main MC,
// SS baseline cell, each SS grid cell, tax-optimizer finalists). seed == 0
// means "auto-seed": derive a one-shot seed from the clock, preserving the
// "default = unpredictable" contract across calls.
//
// This is the load-bearing common-random-numbers contract: passing 0 down
// instead would make each consumer auto-seed independently, so delta
// columns (e.g. SS DeltaSurvivalRate) would compare success rates across
// non-common random paths. Resolve once per analysis pass and thread the
// result everywhere — never re-derive per consumer.
func EffectiveSeed(seed int64) int64 {
	if seed != 0 {
		return seed
	}
	s := time.Now().UnixNano()
	if s == 0 {
		s = 1 // MonteCarlo treats 0 as auto-seed
	}
	return s
}
