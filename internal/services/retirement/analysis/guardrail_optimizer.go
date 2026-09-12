package analysis

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sort"
	"sync"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

const (
	guardrailOptimizerSearchRuns     = 64
	guardrailOptimizerValidationRuns = 1000
	guardrailOptimizerShortlistLimit = 6
)

type guardrailOptimizerRunner func(context.Context, engine.Input, int64, int, float64) ([]models.MonteCarloResult, error)

// OptimizeGuardrails compares a bounded policy grid and validates its Pareto
// shortlist on fresh shared scenarios. This function only produces a preview.
func OptimizeGuardrails(ctx context.Context, in engine.Input, req models.GuardrailOptimizerRequest) (*models.GuardrailOptimizerResult, error) {
	return optimizeGuardrailsWithRunner(ctx, in, req, runGuardrailOptimizerScenarios)
}

func optimizeGuardrailsWithRunner(ctx context.Context, in engine.Input, req models.GuardrailOptimizerRequest, run guardrailOptimizerRunner) (*models.GuardrailOptimizerResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if in.Prepared.IsZero() {
		return nil, fmt.Errorf("prepared retirement settings are required")
	}
	s := in.Prepared.Settings()
	if !guardrailOptimizerFinite(req.FloorMonthlyReal) || req.FloorMonthlyReal <= 0 || !guardrailOptimizerFinite(s.MonthlyLivingExpenses) || req.FloorMonthlyReal > s.MonthlyLivingExpenses {
		return nil, fmt.Errorf("minimum monthly spending must be finite, positive, and no greater than Monthly Living Expenses")
	}
	if !guardrailOptimizerFinite(req.TargetSuccessPct) || req.TargetSuccessPct <= 0 || req.TargetSuccessPct >= 100 {
		return nil, fmt.Errorf("minimum-spending success target must be greater than 0 and less than 100")
	}
	if len(in.Chain) > 0 || len(s.ScenarioChain) > 0 {
		return nil, fmt.Errorf("guardrail optimization does not support chained scenarios")
	}
	if run == nil {
		return nil, fmt.Errorf("simulation runner is required")
	}
	seed := EffectiveSeed(req.Seed)
	// A domain-separated master stream makes held-out scenarios independent of
	// search. XOR is one-to-one and never returns the original nonzero seed.
	validationSeed := int64(uint64(seed) ^ uint64(0xd1b54a32d192ed03))
	if validationSeed == 0 {
		validationSeed = 1
		if validationSeed == seed {
			validationSeed = 2
		}
	}
	variation := DefaultMonteCarloConfig().LongevityVariation
	result := &models.GuardrailOptimizerResult{Request: req, Seed: seed, ValidationSeed: validationSeed, SearchRuns: guardrailOptimizerSearchRuns, ValidationRuns: guardrailOptimizerValidationRuns, HorizonMinYears: max(10, s.ProjectionYears-variation), HorizonMaxYears: max(10, s.ProjectionYears+variation)}
	candidates := guardrailOptimizerPolicies(s.Guardrails, req.FloorMonthlyReal)
	evaluate := func(candidate *models.GuardrailOptimizerCandidate, runs int, baseSeed int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		cloned, err := prepare.Clone(s)
		if err != nil {
			return err
		}
		cloned.Guardrails = guardrailOptimizerCloneConfig(candidate.Guardrails)
		prepared, err := prepare.From(cloned)
		if err != nil {
			return err
		}
		candidateInput := in // Preserve all input hooks and future non-policy inputs.
		candidateInput.Prepared = prepared
		rows, err := run(ctx, candidateInput, baseSeed, runs, req.FloorMonthlyReal)
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if len(rows) != runs {
			return fmt.Errorf("simulation returned %d of %d requested paths", len(rows), runs)
		}
		candidate.Metrics, err = guardrailOptimizerMetrics(rows)
		if err != nil {
			return err
		}
		if baseSeed == validationSeed {
			candidate.SimulationYears, err = SummarizeGuardrailSimulationYears(rows)
			if err != nil {
				return err
			}
		}
		candidate.Qualifies = guardrailOptimizerQualifies(candidate.Metrics, req.TargetSuccessPct)
		return nil
	}
	// Policies are serial; only simulations within a policy use a bounded pool.
	// This avoids multiplying concurrent work by both policy and scenario counts.
	for i := range candidates {
		if err := evaluate(&candidates[i], guardrailOptimizerSearchRuns, seed); err != nil {
			return nil, err
		}
	}
	shortlisted := guardrailOptimizerShortlist(candidates)
	current := models.GuardrailOptimizerCandidate{ID: "current", Labels: []string{"Current guardrails"}, Guardrails: guardrailOptimizerCloneConfig(s.Guardrails), Baseline: true}
	disabled := guardrailOptimizerCloneConfig(s.Guardrails)
	if disabled != nil {
		disabled.Enabled = false
	}
	shortlisted = append(shortlisted, current, models.GuardrailOptimizerCandidate{ID: "no-guardrails", Labels: []string{"No guardrails"}, Guardrails: disabled, Baseline: true})
	for i := range shortlisted {
		if err := evaluate(&shortlisted[i], guardrailOptimizerValidationRuns, validationSeed); err != nil {
			return nil, err
		}
	}
	result.Candidates = shortlisted
	for rank, label := range []string{"Higher spending", "Smoother spending", "Lower risk"} {
		best := -1
		for i, c := range result.Candidates {
			if c.Baseline || !c.Qualifies {
				continue
			}
			// Independent validation can change the Pareto frontier. Keep all
			// measured rows visible, but never recommend a dominated policy.
			dominated := false
			for j, other := range result.Candidates {
				if i != j && !other.Baseline && other.Qualifies && guardrailOptimizerDominates(other.Metrics, c.Metrics) {
					dominated = true
					break
				}
			}
			if dominated {
				continue
			}
			if best < 0 || guardrailOptimizerBetter(c.Metrics, result.Candidates[best].Metrics, rank) {
				best = i
			}
		}
		if best < 0 {
			continue
		}
		c := &result.Candidates[best]
		if len(c.Labels) == 0 {
			result.Recommendations = append(result.Recommendations, c.ID)
		}
		c.Labels = append(c.Labels, label)
	}
	return result, nil
}

func guardrailOptimizerCloneConfig(g *models.GuardrailConfig) *models.GuardrailConfig {
	if g == nil {
		return nil
	}
	copy := *g
	return &copy
}

func guardrailOptimizerPolicies(current *models.GuardrailConfig, floor float64) []models.GuardrailOptimizerCandidate {
	var out []models.GuardrailOptimizerCandidate
	seen := map[models.GuardrailConfig]bool{}
	add := func(g models.GuardrailConfig, id string) {
		if !seen[g] {
			seen[g] = true
			out = append(out, models.GuardrailOptimizerCandidate{ID: id, Guardrails: &g})
		}
	}
	for _, drop := range []float64{10, 20} {
		for _, cut := range []float64{5, 10} {
			for _, rise := range []float64{15, 25} {
				for _, raise := range []float64{5, 10} {
					for _, cap := range []float64{120, 150} {
						add(models.GuardrailConfig{Enabled: true, FloorDropPct: drop, FloorCutPct: cut, CeilingRisePct: rise, CeilingRaisePct: raise, MaxSpendingPct: cap, MinMonthlySpendingReal: floor}, fmt.Sprintf("grid-%g-%g-%g-%g-%g", drop, cut, rise, raise, cap))
					}
				}
			}
		}
	}
	// An absent current policy uses the ordinary guardrail form defaults. A
	// disabled policy becomes an enabled alternative, but its baseline is intact.
	g := models.GuardrailConfig{FloorDropPct: 20, FloorCutPct: 10, CeilingRisePct: 20, CeilingRaisePct: 10, MaxSpendingPct: 125}
	if current != nil {
		g = *current
	}
	g.Enabled = true
	g.MinSpendingPct = 0
	g.MinMonthlySpendingReal = floor
	add(g, "current-floor")
	return out
}

func guardrailOptimizerShortlist(candidates []models.GuardrailOptimizerCandidate) []models.GuardrailOptimizerCandidate {
	var pareto []models.GuardrailOptimizerCandidate
	for i, c := range candidates {
		dominated := false
		for j, other := range candidates {
			if i != j && guardrailOptimizerDominates(other.Metrics, c.Metrics) {
				dominated = true
				break
			}
		}
		if !dominated {
			pareto = append(pareto, c)
		}
	}
	if len(pareto) <= guardrailOptimizerShortlistLimit {
		return pareto
	}
	// Preserve the best two policies on each displayed tradeoff. Resolve ties
	// by the original grid order; never collapse tradeoffs into a weighted score.
	chosen := map[int]bool{}
	for rank := 0; rank < 3; rank++ {
		order := make([]int, len(pareto))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(i, j int) bool {
			return guardrailOptimizerBetter(pareto[order[i]].Metrics, pareto[order[j]].Metrics, rank)
		})
		for _, index := range order[:min(2, len(order))] {
			chosen[index] = true
		}
	}
	for i := 0; len(chosen) < guardrailOptimizerShortlistLimit && i < len(pareto); i++ {
		chosen[i] = true
	}
	out := make([]models.GuardrailOptimizerCandidate, 0, guardrailOptimizerShortlistLimit)
	for i, c := range pareto {
		if chosen[i] {
			out = append(out, c)
		}
	}
	return out
}

func guardrailOptimizerDominates(a, b models.GuardrailOptimizerMetrics) bool {
	return a.MedianLifetimeFundedLivingReal >= b.MedianLifetimeFundedLivingReal && a.P95WorstAnnualCutPct <= b.P95WorstAnnualCutPct && a.MeanAnnualCutCount <= b.MeanAnnualCutCount && a.FloorShortfallPaths <= b.FloorShortfallPaths &&
		(a.MedianLifetimeFundedLivingReal > b.MedianLifetimeFundedLivingReal || a.P95WorstAnnualCutPct < b.P95WorstAnnualCutPct || a.MeanAnnualCutCount < b.MeanAnnualCutCount || a.FloorShortfallPaths < b.FloorShortfallPaths)
}

func guardrailOptimizerBetter(a, b models.GuardrailOptimizerMetrics, rank int) bool {
	switch rank {
	case 1:
		if a.P95WorstAnnualCutPct != b.P95WorstAnnualCutPct {
			return a.P95WorstAnnualCutPct < b.P95WorstAnnualCutPct
		}
		if a.MeanAnnualCutCount != b.MeanAnnualCutCount {
			return a.MeanAnnualCutCount < b.MeanAnnualCutCount
		}
	case 2:
		if a.FloorShortfallPaths != b.FloorShortfallPaths {
			return a.FloorShortfallPaths < b.FloorShortfallPaths
		}
	}
	return a.MedianLifetimeFundedLivingReal > b.MedianLifetimeFundedLivingReal
}

func guardrailOptimizerQualifies(m models.GuardrailOptimizerMetrics, target float64) bool {
	// Use the same unrounded count-derived percentage as the reported metric.
	// Multiplying the target by runs can round an inclusive boundary upward.
	return m.Runs > 0 && 100*float64(m.Runs-m.FloorShortfallPaths)/float64(m.Runs) >= target
}

func guardrailOptimizerFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func guardrailOptimizerMetrics(rows []models.MonteCarloResult) (models.GuardrailOptimizerMetrics, error) {
	m := models.GuardrailOptimizerMetrics{Runs: len(rows)}
	if len(rows) == 0 {
		return m, fmt.Errorf("no simulated paths")
	}
	spending := make([]float64, len(rows))
	cuts := make([]float64, len(rows))
	ending := make([]float64, len(rows))
	totalCuts := 0
	for i, r := range rows {
		f := r.FloorOutcome
		if f == nil || !guardrailOptimizerFinite(f.TotalFundedLivingReal) || !guardrailOptimizerFinite(f.WorstAnnualCutPct) || !guardrailOptimizerFinite(f.FinalBalanceReal) {
			return m, fmt.Errorf("path %d has no finite funded-spending observation", i)
		}
		if f.FloorFailed {
			m.FloorShortfallPaths++
		}
		if !r.Survives {
			m.DepletionPaths++
		}
		spending[i] = f.TotalFundedLivingReal
		cuts[i] = f.WorstAnnualCutPct
		ending[i] = f.FinalBalanceReal
		totalCuts += f.AnnualCutCount
	}
	sort.Float64s(spending)
	sort.Float64s(cuts)
	sort.Float64s(ending)
	percentile := func(values []float64, q float64) float64 {
		position := float64(len(values)-1) * q
		low := int(position)
		high := min(low+1, len(values)-1)
		return values[low] + (values[high]-values[low])*(position-float64(low))
	}
	m.MedianLifetimeFundedLivingReal = percentile(spending, .5)
	m.P10LifetimeFundedLivingReal = percentile(spending, .1)
	m.P95WorstAnnualCutPct = percentile(cuts, .95)
	m.MedianEndingBalanceReal = percentile(ending, .5)
	n := float64(m.Runs)
	m.MeanAnnualCutCount = float64(totalCuts) / n
	m.FloorSuccessPct = 100 * float64(m.Runs-m.FloorShortfallPaths) / n
	m.DepletionRiskPct = 100 * float64(m.DepletionPaths) / n
	// Wilson score interval (two-sided 95%) remains informative at 0/n and n/n.
	p := float64(m.Runs-m.FloorShortfallPaths) / n
	const z = 1.959963984540054
	denominator := 1 + z*z/n
	midpoint := (p + z*z/(2*n)) / denominator
	half := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n)) / denominator
	m.FloorSuccessCILowPct = math.Max(0, 100*(midpoint-half))
	m.FloorSuccessCIHighPct = math.Min(100, 100*(midpoint+half))
	return m, nil
}

// runGuardrailOptimizerScenarios uses the same per-run seed derivation as MC,
// with a small worker bound and cancellation before each run. The observation
// floor is not a policy change; disabled/current baselines remain untouched.
func runGuardrailOptimizerScenarios(ctx context.Context, in engine.Input, seed int64, runs int, floor float64) ([]models.MonteCarloResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if runs <= 0 {
		return nil, fmt.Errorf("positive simulation count required")
	}
	config := DefaultMonteCarloConfig()
	config.MinMonthlySpendingReal = floor
	config.CaptureSpendingYears = true
	master := rand.New(rand.NewSource(seed))
	seeds := make([]int64, runs)
	for i := range seeds {
		seeds[i] = master.Int63()
	}
	rows := make([]models.MonteCarloResult, runs)
	jobs := make(chan int)
	var workers sync.WaitGroup
	// One worker per available core: each run is CPU-bound and, since the
	// engine stopped allocating per month, scales cleanly (a cap of 4 had
	// pinned this at ~400% CPU on a 32-core box, 3.9s -> 1.3s uncapped).
	for w := 0; w < min(runtime.GOMAXPROCS(0), runs); w++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				rows[i] = RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(seeds[i])), config)
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
	return rows, nil
}
