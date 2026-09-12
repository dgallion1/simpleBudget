package analysis

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
)

// ErrInvalidSpendingRequest identifies user-correctable request errors, including
// unsupported chained inputs. Runner and evidence errors do not wrap this sentinel.
var ErrInvalidSpendingRequest = errors.New("invalid spending optimizer request")

const spendingDiscoveryRuns = 32
const spendingSelectionRuns = 1000
const spendingValidationRuns = 1000

type spendingScenarioRunner func(context.Context, engine.Input, int64, int, models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error)

// OptimizeSpending previews a bounded budget/policy search on three independent
// shared scenario streams. It never changes settings or retunes final candidates.
func OptimizeSpending(ctx context.Context, in engine.Input, req models.SpendingOptimizerRequest) (*models.SpendingOptimizerResult, error) {
	return optimizeSpendingWithRunner(ctx, in, req, runSpendingScenarios)
}

func optimizeSpendingWithRunner(ctx context.Context, in engine.Input, req models.SpendingOptimizerRequest, run spendingScenarioRunner) (*models.SpendingOptimizerResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, grid, err := normalizeSpendingRequest(in, req)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("simulation runner is required")
	}
	seed := EffectiveSeed(req.Seed)
	if seed == 0 {
		seed = 1
	}
	req.Seed = seed
	// Distinct XOR domains are bijections. Resolve the sole zero value without
	// colliding with either of the already chosen nonzero stage seeds.
	stageSeed := func(domain uint64, used ...int64) int64 {
		v := int64(uint64(seed) ^ domain)
		for {
			collision := v == 0
			for _, u := range used {
				collision = collision || v == u
			}
			if !collision {
				return v
			}
			v++
		}
	}
	selectionSeed := stageSeed(0xa0761d6478bd642f, seed)
	finalSeed := stageSeed(0xe7037ed1a0b428db, seed, selectionSeed)
	s := in.Prepared.Settings()
	variation := DefaultMonteCarloConfig().LongevityVariation
	result := &models.SpendingOptimizerResult{Request: req, SearchSeed: seed, SelectionSeed: selectionSeed, ValidationSeed: finalSeed,
		SearchRuns: spendingDiscoveryRuns, SelectionRuns: spendingSelectionRuns, ValidationRuns: spendingValidationRuns,
		EffectiveMinMonthlyReal: req.SearchMinMonthlyReal, EffectiveMaxMonthlyReal: req.SearchMaxMonthlyReal, ResolutionMonthlyReal: req.SearchStepMonthlyReal,
		HorizonMinYears: max(10, s.ProjectionYears-variation), HorizonMaxYears: max(10, s.ProjectionYears+variation), RecommendationIDs: []string{}}
	evaluate := func(c *models.SpendingCandidate, stage int64, n int, final bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		candidateInput, err := PrepareSpendingCandidate(in, *c)
		if err != nil {
			return err
		}
		// The runner cannot mutate the frozen request's shared boost pointer.
		runReq := req
		runReq.LivingSpendingBoost = models.CloneLivingSpendingBoost(req.LivingSpendingBoost)
		rows, err := run(ctx, candidateInput, stage, n, runReq)
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		result.EvaluatedCandidates++
		if len(rows) != n {
			return fmt.Errorf("simulation returned %d of %d requested paths", len(rows), n)
		}
		c.Metrics, err = summarizeSpendingRisk(rows, req.NearTermYears)
		if err != nil {
			return err
		}
		c.Qualifies = spendingCandidateQualifies(*c)
		if final {
			// Legacy summarization permits no capture; this optimizer requires complete
			// annual evidence to support every inspectable final frontier row.
			for i := range rows {
				if len(rows[i].FloorOutcome.Years) != rows[i].ProjectionYears {
					return fmt.Errorf("path %d has incomplete annual spending evidence", i)
				}
			}
			c.SimulationYears, err = SummarizeGuardrailSimulationYears(rows)
			if err != nil {
				return err
			}
			c.WorstPathIndex = c.Metrics.WorstPathIndex
			c.WorstPathYears = append([]models.GuardrailPathYear(nil), rows[c.WorstPathIndex].FloorOutcome.Years...)
		}
		return nil
	}
	planned := &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, CeilingRisePct: 20, MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: req.FloorMonthlyReal}
	policies := guardrailOptimizerPolicies(s.Guardrails, req.FloorMonthlyReal)
	var frontier []models.SpendingCandidate
	for _, index := range spendingDiscoveryPositions(len(grid)) {
		amount := float64(grid[index]) / 100
		p, err := spendingCandidateForStart(in, amount, req.FloorMonthlyReal, req.LivingSpendingBoost, planned, "planned", fmt.Sprintf("planned-%d", grid[index]))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidSpendingRequest, err)
		}
		if err = evaluate(&p, seed, spendingDiscoveryRuns, false); err != nil {
			return nil, err
		}
		var best models.SpendingCandidate
		for _, policy := range policies {
			c, err := spendingCandidateForStart(in, amount, req.FloorMonthlyReal, req.LivingSpendingBoost, policy.Guardrails, "flexible", fmt.Sprintf("flexible-%d-%s", grid[index], policy.ID))
			if err != nil {
				return nil, err
			}
			if err = evaluate(&c, seed, spendingDiscoveryRuns, false); err != nil {
				return nil, err
			}
			if best.Metrics == nil || spendingDiscoveryBetter(c, best) {
				best = c
			}
		}
		// Discard screen metrics before selection. The screen chooses rules only.
		p.Metrics = nil
		best.Metrics = nil
		frontier = append(frontier, p, best)
	}
	for i := range frontier {
		if err = evaluate(&frontier[i], selectionSeed, spendingSelectionRuns, false); err != nil {
			return nil, err
		}
	}
	for _, kind := range []string{"planned", "flexible"} {
		for refinement := 0; refinement < 2; refinement++ {
			low := -1
			high := -1
			for i, c := range frontier {
				if c.Kind == kind && c.Qualifies && (low < 0 || c.StartingMonthlyLivingReal > frontier[low].StartingMonthlyLivingReal) {
					low = i
				}
			}
			if low < 0 {
				break
			}
			for i, c := range frontier {
				if c.Kind == kind && !c.Qualifies && c.StartingMonthlyLivingReal > frontier[low].StartingMonthlyLivingReal && (high < 0 || c.StartingMonthlyLivingReal < frontier[high].StartingMonthlyLivingReal) {
					high = i
				}
			}
			if high < 0 {
				break
			}
			lo, hi := spendingCandidateCents(frontier[low].StartingMonthlyLivingReal), spendingCandidateCents(frontier[high].StartingMonthlyLivingReal)
			var interior []int64
			for _, v := range grid {
				if v > lo && v < hi {
					tested := false
					for _, c := range frontier {
						if c.Kind == kind && spendingCandidateCents(c.StartingMonthlyLivingReal) == v {
							tested = true
							break
						}
					}
					if !tested {
						interior = append(interior, v)
					}
				}
			}
			if len(interior) == 0 {
				break
			}
			mid := interior[(len(interior)-1)/2]
			c, err := spendingCandidateForStart(in, float64(mid)/100, req.FloorMonthlyReal, req.LivingSpendingBoost, frontier[low].Guardrails, kind, fmt.Sprintf("%s-%d-refined", kind, mid))
			if err != nil {
				return nil, err
			}
			if err = evaluate(&c, selectionSeed, spendingSelectionRuns, false); err != nil {
				return nil, err
			}
			frontier = append(frontier, c)
		}
	}
	current, err := spendingCandidateForStart(in, 0, req.FloorMonthlyReal, nil, nil, "current", "current")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSpendingRequest, err)
	}
	frontier = append(frontier, current)
	// The complete frontier is now frozen: no budget or policy decisions follow.
	for i := range frontier {
		if err = evaluate(&frontier[i], finalSeed, spendingValidationRuns, true); err != nil {
			return nil, err
		}
	}
	sort.Slice(frontier, func(i, j int) bool {
		a, b := frontier[i], frontier[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.StartingMonthlyLivingReal != b.StartingMonthlyLivingReal {
			return a.StartingMonthlyLivingReal < b.StartingMonthlyLivingReal
		}
		return a.ID < b.ID
	})
	result.Candidates = frontier
	for _, kind := range []string{"planned", "flexible"} {
		best := -1
		for i, c := range frontier {
			if c.Kind == kind && c.Qualifies && (best < 0 || spendingCandidateBetter(c, frontier[best])) {
				best = i
			}
		}
		if best >= 0 {
			c := frontier[best]
			result.RecommendationIDs = append(result.RecommendationIDs, c.ID)
			if c.StartingMonthlyLivingReal == req.SearchMinMonthlyReal || c.StartingMonthlyLivingReal == req.SearchMaxMonthlyReal {
				result.RangeLimited = true
			}
		}
	}
	return result, nil
}

func normalizeSpendingRequest(in engine.Input, req models.SpendingOptimizerRequest) (models.SpendingOptimizerRequest, []int64, error) {
	invalid := func(message string) (models.SpendingOptimizerRequest, []int64, error) {
		return req, nil, fmt.Errorf("%w: %s", ErrInvalidSpendingRequest, message)
	}
	if err := validateSpendingCandidateInput(in); err != nil {
		return invalid(err.Error())
	}
	// Keep cent arithmetic below the exact-integer float64 boundary and int64's
	// range. This is an arithmetic limit, not an economic search assumption.
	finiteMoney := func(v float64) bool { return spendingCandidateFinite(v) && math.Abs(v) < float64(1<<52)/100 }
	if !finiteMoney(req.FloorMonthlyReal) || req.FloorMonthlyReal < .01 {
		return invalid("minimum monthly living must be finite and at least $0.01")
	}
	if req.NearTermYears == 0 {
		req.NearTermYears = 5
	}
	s := in.Prepared.Settings()
	shortest := max(10, s.ProjectionYears-DefaultMonteCarloConfig().LongevityVariation)
	if req.NearTermYears < 1 || req.NearTermYears > 10 || req.NearTermYears > shortest {
		return invalid("near-term years must be 1–10 and within the shortest simulated horizon")
	}
	req.LivingSpendingBoost = models.CloneLivingSpendingBoost(req.LivingSpendingBoost)
	activeBoost := 0.0
	if boost := req.LivingSpendingBoost; boost != nil {
		if !finiteMoney(boost.MonthlyReal) || boost.MonthlyReal <= 0 || math.Abs(boost.MonthlyReal*100-math.Round(boost.MonthlyReal*100)) > 1e-6 {
			return invalid("boost amount must be finite, positive, and whole cents")
		}
		stop, err := models.ParseYearMonth(boost.StopMonth)
		if err != nil {
			return invalid("boost stop month must be valid YYYY-MM")
		}
		start, err := models.ParseYearMonth(s.StartDate)
		if err != nil {
			return invalid(err.Error())
		}
		if !stop.After(start) {
			return invalid("new boost must stop after the plan start month")
		}
		activeBoost = boost.MonthlyReal
	}
	for _, v := range []float64{req.SearchMinMonthlyReal, req.SearchMaxMonthlyReal, req.SearchStepMonthlyReal} {
		if !finiteMoney(v) || v < 0 {
			return invalid("search range and step must be finite and positive")
		}
	}
	if req.SearchMinMonthlyReal == 0 {
		floorCents := spendingCandidateCents(req.FloorMonthlyReal)
		if float64(floorCents)/100 < req.FloorMonthlyReal {
			floorCents++
		}
		req.SearchMinMonthlyReal = float64(max(floorCents, spendingCandidateCents(activeBoost)+1)) / 100
	}
	if req.SearchMaxMonthlyReal == 0 {
		req.SearchMaxMonthlyReal = math.Max(2*engine.LivingExpensesAtMonth(s, 0), 2*req.SearchMinMonthlyReal)
		if finiteMoney(req.SearchMaxMonthlyReal) {
			cents := spendingCandidateCents(req.SearchMaxMonthlyReal)
			if float64(cents)/100 < req.SearchMaxMonthlyReal {
				cents++
			}
			req.SearchMaxMonthlyReal = float64(cents) / 100
		}
	}
	if !finiteMoney(req.SearchMaxMonthlyReal) || req.SearchMaxMonthlyReal < req.SearchMinMonthlyReal || req.SearchMinMonthlyReal < req.FloorMonthlyReal || req.SearchMinMonthlyReal <= activeBoost {
		return invalid("search range must be ordered, at least the minimum, and leave a positive nonboost base")
	}
	// Require representable cent-grid amounts instead of silently rounding a
	// submitted endpoint or resolution and changing the requested range.
	for _, v := range []float64{req.SearchMinMonthlyReal, req.SearchMaxMonthlyReal, req.SearchStepMonthlyReal} {
		if math.Abs(v*100-math.Round(v*100)) > 1e-6 {
			return invalid("search amounts and step must use whole cents")
		}
	}
	lo, hi := spendingCandidateCents(req.SearchMinMonthlyReal), spendingCandidateCents(req.SearchMaxMonthlyReal)
	if req.SearchStepMonthlyReal == 0 {
		req.SearchStepMonthlyReal = math.Max(100, math.Ceil(float64(hi-lo)/200/10000)*100)
	}
	step := spendingCandidateCents(req.SearchStepMonthlyReal)
	if step <= 0 {
		return invalid("search step must be at least $0.01")
	}
	if (hi-lo)/step+1 > 201 {
		return invalid("search exceeds 201 regular positions; use a coarser step or smaller range")
	}
	grid := make([]int64, 0, 202)
	for v := lo; v <= hi; v += step {
		grid = append(grid, v)
	}
	if grid[len(grid)-1] != hi {
		grid = append(grid, hi)
	}
	if _, err := spendingCandidateForStart(in, req.SearchMinMonthlyReal, req.FloorMonthlyReal, req.LivingSpendingBoost, nil, "flexible", "preflight"); err != nil {
		return invalid(err.Error())
	}
	return req, grid, nil
}

func spendingDiscoveryPositions(length int) []int {
	count := min(9, length)
	out := make([]int, count)
	if count == 1 {
		return out
	}
	for i := range out {
		out[i] = int(math.Round(float64(i*(length-1)) / float64(count-1)))
	}
	return out
}

func spendingCandidateQualifies(c models.SpendingCandidate) bool {
	m := c.Metrics
	return m != nil && m.Runs > 0 && m.FloorShortfallPaths == 0 && m.UnpaidObligationPaths == 0 && (c.Kind != "planned" || m.CutPaths == 0)
}

func spendingDiscoveryBetter(a, b models.SpendingCandidate) bool {
	af, bf := a.Metrics.FloorShortfallPaths+a.Metrics.UnpaidObligationPaths, b.Metrics.FloorShortfallPaths+b.Metrics.UnpaidObligationPaths
	if af != bf {
		return af < bf
	}
	return spendingCandidateBetter(a, b)
}

func spendingCandidateBetter(a, b models.SpendingCandidate) bool {
	x, y := a.Metrics, b.Metrics
	if x.MedianNearTermMonthlyReal != y.MedianNearTermMonthlyReal {
		return x.MedianNearTermMonthlyReal > y.MedianNearTermMonthlyReal
	}
	if x.CutPaths != y.CutPaths {
		return x.CutPaths < y.CutPaths
	}
	value := func(v *float64) float64 {
		if v == nil {
			return 0
		}
		return *v
	}
	if value(x.P95MaxCutReal) != value(y.P95MaxCutReal) {
		return value(x.P95MaxCutReal) < value(y.P95MaxCutReal)
	}
	if value(x.P95LongestBelowPlanMonths) != value(y.P95LongestBelowPlanMonths) {
		return value(x.P95LongestBelowPlanMonths) < value(y.P95LongestBelowPlanMonths)
	}
	if a.StartingMonthlyLivingReal != b.StartingMonthlyLivingReal {
		return a.StartingMonthlyLivingReal < b.StartingMonthlyLivingReal
	}
	return a.ID < b.ID
}
