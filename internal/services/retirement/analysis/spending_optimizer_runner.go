package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
)

// runSpendingScenarios preserves the engine's stochastic model and hooks while
// observing complete horizons. Candidates are serial; only paths run in parallel.
func runSpendingScenarios(ctx context.Context, in engine.Input, seed int64, runs int, req models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if runs <= 0 {
		return nil, fmt.Errorf("positive simulation count required")
	}
	if err := validateSpendingCandidateInput(in); err != nil {
		return nil, err
	}
	if !spendingCandidateFinite(req.FloorMonthlyReal) || req.FloorMonthlyReal < .01 || req.NearTermYears < 1 || req.NearTermYears > 10 {
		return nil, fmt.Errorf("valid spending observation floor and years required")
	}
	cfg := DefaultMonteCarloConfig()
	cfg.MinMonthlySpendingReal = req.FloorMonthlyReal
	cfg.CaptureSpendingYears = true
	cfg.SpendingExperienceYears = req.NearTermYears
	cfg.AdaptiveSpending = false
	seeds := make([]int64, runs)
	master := rand.New(rand.NewSource(seed))
	for i := range seeds {
		seeds[i] = master.Int63()
	}
	rows := make([]models.MonteCarloResult, runs)
	completed := make([]bool, runs)
	jobs := make(chan int)
	var workers sync.WaitGroup
	// One worker per available core. The design spec sized this at eight
	// pending a scaling measurement; measured, the cap pinned the search at
	// ~760% CPU on a 32-core box (6.2s -> 3.7s uncapped).
	for w := 0; w < min(runtime.GOMAXPROCS(0), runs); w++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				rows[i] = RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(seeds[i])), cfg)
				completed[i] = true
			}
		}()
	}
dispatch:
	for i := range rows {
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- i:
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i, done := range completed {
		if !done {
			return nil, fmt.Errorf("simulation path %d was not completed", i)
		}
	}
	if _, err := summarizeSpendingRisk(rows, req.NearTermYears); err != nil {
		return nil, err
	}
	return rows, nil
}
