package analysis

import (
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// TestMonteCarloDeterministicAcrossRuns locks the contract that the parallel
// MonteCarlo fan-out is reproducible for a fixed non-zero seed: the per-run
// RNGs are derived deterministically from the seed, so the aggregate stats
// must be byte-identical regardless of how goroutines are scheduled. (Race
// safety is exercised by `go test -race` on this package.)
func TestMonteCarloDeterministicAcrossRuns(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.PortfolioValue = 1_000_000
	in := engineInput(t, s)
	eng := engine.New()

	const seed int64 = 987654321
	a := MonteCarlo(eng, in, 500, seed)
	b := MonteCarlo(eng, in, 500, seed)

	if a == nil || b == nil || a.Stats == nil || b.Stats == nil {
		t.Fatal("MonteCarlo returned nil stats")
	}

	// Compare the headline aggregate statistics; equality here means every
	// run's RNG stream — and therefore every per-run outcome — reproduced.
	if a.Stats.SuccessRate != b.Stats.SuccessRate {
		t.Errorf("SuccessRate not reproducible: %v vs %v", a.Stats.SuccessRate, b.Stats.SuccessRate)
	}
	if a.Stats.MedianBalance != b.Stats.MedianBalance {
		t.Errorf("MedianBalance not reproducible: %v vs %v", a.Stats.MedianBalance, b.Stats.MedianBalance)
	}
	if a.Stats.MeanBalance != b.Stats.MeanBalance {
		t.Errorf("MeanBalance not reproducible: %v vs %v", a.Stats.MeanBalance, b.Stats.MeanBalance)
	}
	if a.Stats.WorstCase != b.Stats.WorstCase || a.Stats.BestCase != b.Stats.BestCase {
		t.Errorf("range not reproducible: [%v,%v] vs [%v,%v]",
			a.Stats.WorstCase, a.Stats.BestCase, b.Stats.WorstCase, b.Stats.BestCase)
	}
	if a.Stats.AvgCrashesPerRun != b.Stats.AvgCrashesPerRun {
		t.Errorf("AvgCrashesPerRun not reproducible: %v vs %v", a.Stats.AvgCrashesPerRun, b.Stats.AvgCrashesPerRun)
	}
}
