package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
)

func spendingSearchRequest() models.SpendingOptimizerRequest {
	return models.SpendingOptimizerRequest{FloorMonthlyReal: 7000, NearTermYears: 5, SearchMinMonthlyReal: 7000, SearchMaxMonthlyReal: 15000, SearchStepMonthlyReal: 1000, Seed: 1234}
}
func spendingSearchRows(in engine.Input, r models.SpendingOptimizerRequest, n int, funded float64, depleted bool) []models.MonteCarloResult {
	start := engine.LivingExpensesAtMonth(in.Prepared.Settings(), 0)
	years := max(10, in.Prepared.Settings().ProjectionYears)
	o := models.SpendingPathOutcome{MonthsObserved: years * 12, NearTermMonths: r.NearTermYears * 12, NearTermFundedLivingReal: funded * float64(r.NearTermYears*12), MinFundedMonthlyReal: funded, MinFundedMonth: 1}
	if funded < start {
		o.MonthsBelowPlan = years * 12
		o.LongestBelowPlanMonths = years * 12
		o.FirstCutMonth = 1
		o.MaxCutReal = start - funded
		o.MaxCutPct = 100 * (start - funded) / start
		o.BelowPlanAtEnd = true
	}
	if funded < r.FloorMonthlyReal {
		o.FloorShortfallMonths = years * 12
		o.LongestFloorShortfallMonths = years * 12
		o.LargestFloorGapReal = r.FloorMonthlyReal - funded
	}
	if depleted {
		o.DepletionMonth = 1
		o.FloorShortfallMonthsAfterDepletion = o.FloorShortfallMonths
	}
	rows := make([]models.MonteCarloResult, n)
	for i := range rows {
		out := o
		annual := make([]models.GuardrailPathYear, years)
		for j := range annual {
			annual[j] = models.GuardrailPathYear{Year: j + 1, LivingReal: funded * 12, LivingNominal: funded * 12}
		}
		rows[i] = models.MonteCarloResult{ProjectionYears: years, Survives: !depleted, SpendingOutcome: &out, FloorOutcome: &models.MonteCarloFloorOutcome{MonthsObserved: years * 12, FloorFailed: funded < r.FloorMonthlyReal, TotalFundedLivingReal: funded * float64(years*12), Years: annual}}
	}
	return rows
}
func TestSpendingCandidateQualificationUsesCounts(t *testing.T) {
	c := models.SpendingCandidate{Kind: "flexible", Metrics: &models.SpendingRiskMetrics{Runs: 1000}}
	if !spendingCandidateQualifies(c, 0) {
		t.Fatal("zero failures rejected")
	}
	c.Metrics.FloorShortfallPaths = 1
	if spendingCandidateQualifies(c, 0) {
		t.Fatal("floor failure accepted")
	}
	c.Metrics.FloorShortfallPaths = 0
	c.Metrics.UnpaidObligationPaths = 1
	if spendingCandidateQualifies(c, 0) {
		t.Fatal("unpaid obligations accepted")
	}
	c.Metrics.UnpaidObligationPaths = 0
	c.Metrics.CutPaths = 1
	if !spendingCandidateQualifies(c, 0) {
		t.Fatal("flexible cuts rejected")
	}
	c.Kind = "planned"
	if spendingCandidateQualifies(c, 0) {
		t.Fatal("planned cuts accepted")
	}
	c.Metrics = nil
	if spendingCandidateQualifies(c, 0) {
		t.Fatal("missing evidence accepted")
	}
}
func TestSpendingCandidateQualificationAcceptsBoundedShortfall(t *testing.T) {
	// The inclusive boundary uses the same unrounded count-derived rate the
	// results display, so exactly 5.0% qualifies at a 5% allowance.
	c := models.SpendingCandidate{Kind: "flexible", Metrics: &models.SpendingRiskMetrics{Runs: 1000, FloorShortfallPaths: 50, UnpaidObligationPaths: 20, CutPaths: 900}}
	if !spendingCandidateQualifies(c, 5) {
		t.Fatal("5.0% shortfall rejected at a 5% allowance")
	}
	c.Metrics.FloorShortfallPaths = 51
	if spendingCandidateQualifies(c, 5) {
		t.Fatal("5.1% shortfall accepted at a 5% allowance")
	}
	c.Metrics.FloorShortfallPaths = 10
	c.Metrics.UnpaidObligationPaths = 51
	if spendingCandidateQualifies(c, 5) {
		t.Fatal("unpaid obligations above the allowance accepted")
	}
	c.Metrics.UnpaidObligationPaths = 0
	c.Kind = "planned"
	if spendingCandidateQualifies(c, 5) {
		t.Fatal("planned cuts above the allowance accepted")
	}
	c.Metrics.CutPaths = 50
	if !spendingCandidateQualifies(c, 5) {
		t.Fatal("planned cuts within the allowance rejected")
	}
	c.Metrics.Runs = 0
	if spendingCandidateQualifies(c, 5) {
		t.Fatal("no runs accepted")
	}
}
func TestSpendingGuardrailPoliciesFineGrid(t *testing.T) {
	current := &models.GuardrailConfig{Enabled: false, FloorDropPct: 5, FloorCutPct: 2, CeilingRisePct: 10, CeilingRaisePct: 2, MinSpendingPct: 75, MaxSpendingPct: 120, MinMonthlySpendingReal: 1}
	policies := spendingGuardrailPolicies(current, 6000)
	// 3 drops × 3 cuts × 3 rises × 3 raises × 2 caps, plus the saved rules.
	if len(policies) != 163 {
		t.Fatalf("policies = %d, want 163", len(policies))
	}
	seen := map[models.GuardrailConfig]string{}
	for _, p := range policies {
		if p.Guardrails == nil || !p.Guardrails.Enabled || p.Guardrails.MinMonthlySpendingReal != 6000 {
			t.Fatalf("policy %s must be enabled at the requested floor: %+v", p.ID, p.Guardrails)
		}
		if _, dup := seen[*p.Guardrails]; dup {
			t.Fatalf("duplicate policy %s", p.ID)
		}
		seen[*p.Guardrails] = p.ID
	}
	fine := models.GuardrailConfig{Enabled: true, FloorDropPct: 5, FloorCutPct: 2, CeilingRisePct: 10, CeilingRaisePct: 2, MaxSpendingPct: 120, MinMonthlySpendingReal: 6000}
	if _, ok := seen[fine]; !ok {
		t.Fatal("fine 5%/2% policy missing from the grid")
	}
	saved := fine
	saved.MinSpendingPct = 75
	if id := seen[saved]; id != "current-floor" {
		t.Fatalf("saved rules must be tested verbatim with the floor applied: %q", id)
	}
	if got := len(guardrailOptimizerPolicies(current, 6000)); got != 33 {
		t.Fatalf("guardrail optimizer grid changed: %d", got)
	}
}
func TestSpendingOptimizerAcceptableShortfallQualifies(t *testing.T) {
	in := engineInput(t, models.DefaultWhatIfSettings())
	req := spendingSearchRequest()
	req.SearchMaxMonthlyReal = 9000
	req.MaxShortfallPct = 5
	runner := func(ctx context.Context, c engine.Input, seed int64, n int, r models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error) {
		start := engine.LivingExpensesAtMonth(c.Prepared.Settings(), 0)
		rows := spendingSearchRows(c, r, n, start, false)
		// Exactly 5% of futures fall below the minimum at $8,000; 6% at $9,000.
		failing := 0
		switch start {
		case 8000:
			failing = n * 5 / 100
		case 9000:
			failing = n * 6 / 100
		}
		bad := spendingSearchRows(c, r, max(failing, 1), 6000, true)
		copy(rows, bad[:failing])
		return rows, nil
	}
	got, err := optimizeSpendingWithRunner(context.Background(), in, req, runner)
	if err != nil {
		t.Fatal(err)
	}
	leaders := map[string]float64{}
	for _, c := range got.Candidates {
		if c.Baseline {
			continue
		}
		if c.Qualifies != (c.StartingMonthlyLivingReal <= 8000) {
			t.Fatalf("%s at %v qualifies=%v shortfall=%d/%d", c.ID, c.StartingMonthlyLivingReal, c.Qualifies, c.Metrics.FloorShortfallPaths, c.Metrics.Runs)
		}
		for _, id := range got.RecommendationIDs {
			if c.ID == id {
				leaders[c.Kind] = c.StartingMonthlyLivingReal
			}
		}
	}
	if leaders["planned"] != 8000 || leaders["flexible"] != 8000 {
		t.Fatalf("leaders %v", leaders)
	}
	if got.Request.MaxShortfallPct != 5 {
		t.Fatal("allowance not echoed")
	}
	req.MaxShortfallPct = 0
	strict, err := optimizeSpendingWithRunner(context.Background(), in, req, runner)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range strict.Candidates {
		if !c.Baseline && c.Qualifies != (c.StartingMonthlyLivingReal == 7000) {
			t.Fatalf("strict %s qualifies=%v", c.ID, c.Qualifies)
		}
	}
}
func TestSpendingOptimizerFrontierStagesAndBaseline(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 4000
	s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 500, StopMonth: "2000-01"}
	in := engineInput(t, s)
	before, _ := json.Marshal(in.Prepared.Settings())
	req := spendingSearchRequest()
	type record struct {
		seed   int64
		runs   int
		start  float64
		policy models.GuardrailConfig
	}
	var records []record
	runner := func(ctx context.Context, c engine.Input, seed int64, n int, r models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error) {
		cs := c.Prepared.Settings()
		start := engine.LivingExpensesAtMonth(cs, 0)
		rec := record{seed: seed, runs: n, start: start}
		if cs.Guardrails != nil {
			rec.policy = *cs.Guardrails
		}
		records = append(records, rec)
		if start == 4000 {
			b, _ := json.Marshal(cs)
			if string(b) != string(before) {
				t.Fatal("baseline changed")
			}
		}
		funded := start
		if start > 8000 {
			funded = start - 500
		}
		if start > 9000 {
			funded = 6000
		}
		return spendingSearchRows(c, r, n, funded, start == 9000), nil
	}
	got, err := optimizeSpendingWithRunner(context.Background(), in, req, runner)
	if err != nil {
		t.Fatal(err)
	}
	if got.SearchRuns != 32 || got.SelectionRuns != 1000 || got.ValidationRuns != 1000 {
		t.Fatal("sample sizes")
	}
	if got.SearchSeed == 0 || got.SelectionSeed == got.SearchSeed || got.ValidationSeed == got.SearchSeed || got.ValidationSeed == got.SelectionSeed {
		t.Fatal("stage seeds")
	}
	if len(got.Candidates) != 19 {
		t.Fatalf("frontier %d", len(got.Candidates))
	}
	leaders := map[string]float64{}
	for _, c := range got.Candidates {
		if c.Metrics.Runs != 1000 || len(c.SimulationYears) == 0 || len(c.WorstPathYears) == 0 {
			t.Fatal("incomplete final evidence")
		}
		if c.StartingMonthlyLivingReal == 9000 && (c.Metrics.DepletionPaths != 1000 || c.Metrics.DepletionFloorFundedPaths != 1000) {
			t.Fatal("depletion coverage")
		}
		for _, id := range got.RecommendationIDs {
			if c.ID == id {
				leaders[c.Kind] = c.StartingMonthlyLivingReal
			}
		}
	}
	if leaders["planned"] != 8000 || leaders["flexible"] != 9000 {
		t.Fatalf("leaders %v", leaders)
	}
	selection, final := 0, 0
	policies := map[models.GuardrailConfig]bool{}
	seenFinal := false
	for _, r := range records {
		switch r.seed {
		case got.SearchSeed:
			if r.runs != 32 || seenFinal {
				t.Fatal("discovery stage")
			}
			policies[r.policy] = true
		case got.SelectionSeed:
			selection++
			if r.runs != 1000 || seenFinal {
				t.Fatal("selection stage")
			}
		case got.ValidationSeed:
			final++
			seenFinal = true
			if r.runs != 1000 {
				t.Fatal("final size")
			}
		default:
			t.Fatal("unknown stage")
		}
	}
	if len(policies) < 163 || selection != 18 || final != 19 {
		t.Fatalf("policies=%d selection=%d final=%d", len(policies), selection, final)
	}
	if got.EvaluatedCandidates != len(records) {
		t.Fatal("evaluation count")
	}
	after, _ := json.Marshal(in.Prepared.Settings())
	if string(before) != string(after) {
		t.Fatal("source mutated")
	}
	replay, err := optimizeSpendingWithRunner(context.Background(), in, req, runner)
	if err != nil || !reflect.DeepEqual(got, replay) {
		t.Fatalf("replay %v", err)
	}
}
func TestSpendingOptimizerFundedObjectiveAndAllFail(t *testing.T) {
	in := engineInput(t, models.DefaultWhatIfSettings())
	req := spendingSearchRequest()
	req.SearchMinMonthlyReal = 10000
	req.SearchMaxMonthlyReal = 12000
	req.SearchStepMonthlyReal = 2000
	for _, fail := range []bool{false, true} {
		got, err := optimizeSpendingWithRunner(context.Background(), in, req, func(ctx context.Context, c engine.Input, seed int64, n int, r models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error) {
			funded := 9000.0
			high := engine.LivingExpensesAtMonth(c.Prepared.Settings(), 0) == 12000
			if high {
				funded = 8000
			}
			if fail {
				funded = 6000
			}
			rows := spendingSearchRows(c, r, n, funded, false)
			if high {
				for i := range rows {
					rows[i].FloorOutcome.TotalFundedLivingReal = 1e9
				}
			}
			return rows, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Candidates) != 5 {
			t.Fatal("missing failed rows")
		}
		if fail {
			if len(got.RecommendationIDs) != 0 || got.Request.FloorMonthlyReal != 7000 {
				t.Fatal("manufactured recommendation")
			}
			continue
		}
		if len(got.RecommendationIDs) != 1 {
			t.Fatal("missing flexible leader")
		}
		for _, c := range got.Candidates {
			for _, id := range got.RecommendationIDs {
				if c.ID == id && c.StartingMonthlyLivingReal != 10000 {
					t.Fatal("wrong objective")
				}
			}
		}
	}
}
func TestSpendingOptimizerFinalChangesEitherStatus(t *testing.T) {
	in := engineInput(t, models.DefaultWhatIfSettings())
	req := spendingSearchRequest()
	req.SearchMaxMonthlyReal = 8000
	stages := map[int64]int{}
	got, err := optimizeSpendingWithRunner(context.Background(), in, req, func(ctx context.Context, c engine.Input, seed int64, n int, r models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error) {
		if _, ok := stages[seed]; !ok {
			stages[seed] = len(stages)
		}
		start := engine.LivingExpensesAtMonth(c.Prepared.Settings(), 0)
		funded := start
		if (stages[seed] == 1 && start == 7000) || (stages[seed] == 2 && start == 8000) {
			funded = 6000
		}
		return spendingSearchRows(c, r, n, funded, true), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got.Candidates {
		if !c.Baseline && c.Qualifies != (c.StartingMonthlyLivingReal == 7000) {
			t.Fatal("final qualification")
		}
	}
	if len(got.RecommendationIDs) != 2 {
		t.Fatal("funded depleted options should qualify")
	}
}
func TestSpendingOptimizerRequestValidationAndDefaults(t *testing.T) {
	in := engineInput(t, models.DefaultWhatIfSettings())
	base := spendingSearchRequest()
	cases := map[string]func(*models.SpendingOptimizerRequest){"floor": func(r *models.SpendingOptimizerRequest) { r.FloorMonthlyReal = .001 }, "nan": func(r *models.SpendingOptimizerRequest) { r.SearchMinMonthlyReal = math.NaN() }, "infinite": func(r *models.SpendingOptimizerRequest) { r.SearchMaxMonthlyReal = math.Inf(1) }, "reverse": func(r *models.SpendingOptimizerRequest) { r.SearchMaxMonthlyReal = 6000 }, "below floor": func(r *models.SpendingOptimizerRequest) { r.SearchMinMonthlyReal = 6000 }, "negative step": func(r *models.SpendingOptimizerRequest) { r.SearchStepMonthlyReal = -1 }, "too fine": func(r *models.SpendingOptimizerRequest) { r.SearchStepMonthlyReal = .01 }, "years": func(r *models.SpendingOptimizerRequest) { r.NearTermYears = 11 }, "shortfall negative": func(r *models.SpendingOptimizerRequest) { r.MaxShortfallPct = -1 }, "shortfall full": func(r *models.SpendingOptimizerRequest) { r.MaxShortfallPct = 100 }, "shortfall nan": func(r *models.SpendingOptimizerRequest) { r.MaxShortfallPct = math.NaN() }, "expired boost": func(r *models.SpendingOptimizerRequest) {
		r.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 500, StopMonth: "2000-01"}
	}, "fractional boost": func(r *models.SpendingOptimizerRequest) {
		r.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 500.001, StopMonth: "2100-01"}
	}, "date": func(r *models.SpendingOptimizerRequest) {
		r.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 500, StopMonth: "2100-13"}
	}, "no base": func(r *models.SpendingOptimizerRequest) {
		r.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 7000, StopMonth: "2100-01"}
	}}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := base
			mutate(&req)
			_, err := optimizeSpendingWithRunner(context.Background(), in, req, func(context.Context, engine.Input, int64, int, models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error) {
				t.Fatal("invalid reached runner")
				return nil, nil
			})
			if !errors.Is(err, ErrInvalidSpendingRequest) {
				t.Fatalf("request error %v", err)
			}
		})
	}
	req := models.SpendingOptimizerRequest{FloorMonthlyReal: 30000, LivingSpendingBoost: &models.LivingSpendingBoost{MonthlyReal: 1000, StopMonth: "2200-01"}, Seed: 3}
	normalized, grid, err := normalizeSpendingRequest(in, req)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.NearTermYears != 5 || normalized.SearchMinMonthlyReal != 30000 || normalized.SearchMaxMonthlyReal != 60000 || normalized.SearchStepMonthlyReal != 200 || len(grid) != 151 {
		t.Fatalf("defaults %+v grid %d", normalized, len(grid))
	}
	normalized.LivingSpendingBoost.MonthlyReal = 2000
	if req.LivingSpendingBoost.MonthlyReal != 1000 {
		t.Fatal("boost alias")
	}
	base.SearchMaxMonthlyReal = 9500
	_, grid, err = normalizeSpendingRequest(in, base)
	if err != nil || !reflect.DeepEqual(grid, []int64{700000, 800000, 900000, 950000}) {
		t.Fatalf("grid %v %v", grid, err)
	}
}
func TestSpendingOptimizerRejectsIncompleteEvidenceAndCancellation(t *testing.T) {
	in := engineInput(t, models.DefaultWhatIfSettings())
	req := spendingSearchRequest()
	for _, mode := range []string{"partial", "missing", "annual"} {
		_, err := optimizeSpendingWithRunner(context.Background(), in, req, func(ctx context.Context, c engine.Input, seed int64, n int, r models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error) {
			rows := spendingSearchRows(c, r, n, 7000, false)
			switch mode {
			case "partial":
				rows = rows[:n-1]
			case "missing":
				rows[0].SpendingOutcome = nil
			case "annual":
				for i := range rows {
					rows[i].FloorOutcome.Years = nil
				}
			}
			return rows, nil
		})
		if err == nil {
			t.Fatalf("accepted %s", mode)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := OptimizeSpending(ctx, in, req)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSpendingOptimizerRefinementFreezesEveryMeasuredCandidate(t *testing.T) {
	in := engineInput(t, models.DefaultWhatIfSettings())
	req := spendingSearchRequest()
	req.SearchMaxMonthlyReal = 27000
	req.SearchStepMonthlyReal = 100
	req.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 500, StopMonth: "2100-01"}
	type stamp struct {
		budget float64
		policy models.GuardrailConfig
		boost  models.LivingSpendingBoost
	}
	stages := map[int64]int{}
	selected := map[stamp]bool{}
	final := map[stamp]bool{}
	var selectedOrder []stamp
	got, err := optimizeSpendingWithRunner(context.Background(), in, req, func(ctx context.Context, c engine.Input, seed int64, n int, r models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error) {
		if _, ok := stages[seed]; !ok {
			stages[seed] = len(stages)
		}
		s := c.Prepared.Settings()
		start := engine.LivingExpensesAtMonth(s, 0)
		if start >= 7000 {
			if s.LivingSpendingBoost == nil || *s.LivingSpendingBoost != *req.LivingSpendingBoost {
				t.Fatal("boost changed across candidates")
			}
			key := stamp{start, *s.Guardrails, *s.LivingSpendingBoost}
			if stages[seed] == 1 {
				selected[key] = true
				selectedOrder = append(selectedOrder, key)
			}
			if stages[seed] == 2 {
				final[key] = true
				if !selected[key] {
					t.Fatal("final created new candidate")
				}
			}
		}
		funded := start
		if start > 10800 {
			funded = start - 1
		}
		if start > 14200 {
			funded = 6000
		}
		return spendingSearchRows(c, r, n, funded, false), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 22 || len(final) != 22 || len(got.Candidates) != 23 {
		t.Fatalf("refinement counts %d %d %d", len(selected), len(final), len(got.Candidates))
	}
	for _, key := range selectedOrder[18:] {
		if key.budget != 10700 && key.budget != 11300 && key.budget != 13200 && key.budget != 13800 {
			t.Fatalf("unexpected midpoint %.2f", key.budget)
		}
	}
	// Both flexible midpoints inherit the initial $12,000 passing policy.
	var lower models.GuardrailConfig
	for _, key := range selectedOrder {
		if key.budget == 12000 && key.policy.MinSpendingPct != 100 {
			lower = key.policy
		}
	}
	for _, key := range selectedOrder[20:] {
		if key.policy != lower {
			t.Fatal("flexible midpoint policy retuned")
		}
	}
}

func TestSpendingOptimizerSeedsAndTieBreaks(t *testing.T) {
	// Each case starts tied, then makes the lower priority prefer b.
	for _, tc := range []struct {
		name   string
		change func(*models.SpendingCandidate, *models.SpendingCandidate)
	}{
		{"stable ID", func(a, b *models.SpendingCandidate) { a.ID = "a"; b.ID = "b" }},
		{"starting budget before ID", func(a, b *models.SpendingCandidate) { a.StartingMonthlyLivingReal = 9999; a.ID = "z" }},
		{"duration before starting budget", func(a, b *models.SpendingCandidate) {
			a.Metrics.P95LongestBelowPlanMonths = spendingRiskFloat(11)
			a.StartingMonthlyLivingReal = 11000
		}},
		{"depth before duration", func(a, b *models.SpendingCandidate) {
			a.Metrics.P95MaxCutReal = spendingRiskFloat(499)
			a.Metrics.P95LongestBelowPlanMonths = spendingRiskFloat(13)
		}},
		{"cut count before depth", func(a, b *models.SpendingCandidate) {
			a.Metrics.CutPaths = 1
			a.Metrics.P95MaxCutReal = spendingRiskFloat(600)
		}},
		{"funded objective before cut count", func(a, b *models.SpendingCandidate) {
			a.Metrics.MedianNearTermMonthlyReal = 9001
			a.Metrics.CutPaths = 3
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := func() models.SpendingCandidate {
				return models.SpendingCandidate{ID: "b", Kind: "flexible", StartingMonthlyLivingReal: 10000, Metrics: &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: 9000, CutPaths: 2, P95MaxCutReal: spendingRiskFloat(500), P95LongestBelowPlanMonths: spendingRiskFloat(12)}}
			}
			a, b := fresh(), fresh()
			tc.change(&a, &b)
			if !spendingCandidateBetter(a, b) || spendingCandidateBetter(b, a) {
				t.Fatal("comparator precedence or antisymmetry lost")
			}
		})
	}
	result := models.SpendingOptimizerResult{Request: models.SpendingOptimizerRequest{Seed: 9223372036854775807}, SearchSeed: 9223372036854775807, SelectionSeed: -9223372036854775807, ValidationSeed: 9007199254740993}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"search_seed", "selection_seed", "validation_seed"} {
		if _, ok := decoded[key].(string); !ok {
			t.Fatalf("seed %s is lossy JSON number", key)
		}
	}
	if decoded["request"].(map[string]any)["seed"] != "9223372036854775807" {
		t.Fatal("request seed truncated")
	}
}

func TestSpendingOptimizerDiscoveryChoosesFundedPolicyAndFallback(t *testing.T) {
	in := engineInput(t, models.DefaultWhatIfSettings())
	req := spendingSearchRequest()
	req.SearchMinMonthlyReal = 10000
	req.SearchMaxMonthlyReal = 10000
	desired := models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 5, CeilingRisePct: 15, CeilingRaisePct: 5, MaxSpendingPct: 120, MinMonthlySpendingReal: 7000}
	for _, fallback := range []bool{false, true} {
		got, err := optimizeSpendingWithRunner(context.Background(), in, req, func(ctx context.Context, c engine.Input, seed int64, n int, r models.SpendingOptimizerRequest) ([]models.MonteCarloResult, error) {
			g := c.Prepared.Settings().Guardrails
			funded := 7500.0
			failures := 0
			if fallback {
				failures = 3
			}
			if g != nil && *g == desired {
				funded = 8000
				if fallback {
					failures = 1
				}
			} else if g != nil && g.FloorDropPct == 10 && g.MaxSpendingPct == 150 {
				funded = 9500
				failures = 2
			}
			rows := spendingSearchRows(c, r, n, funded, false)
			for i := 0; i < failures; i++ {
				rows[i] = spendingSearchRows(c, r, 1, 6000, false)[0]
			}
			return rows, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range got.Candidates {
			if c.Kind == "flexible" && *c.Guardrails != desired {
				t.Fatalf("discovery picked wrong policy (fallback=%t): %+v", fallback, c.Guardrails)
			}
		}
	}
}

func TestSpendingOptimizerDefaultMinimumUsesExactCents(t *testing.T) {
	in := engineInput(t, models.DefaultWhatIfSettings())
	req := models.SpendingOptimizerRequest{FloorMonthlyReal: .01, SearchMaxMonthlyReal: 1, SearchStepMonthlyReal: .01, LivingSpendingBoost: &models.LivingSpendingBoost{MonthlyReal: .28, StopMonth: "2100-01"}}
	got, _, err := normalizeSpendingRequest(in, req)
	if err != nil {
		t.Fatal(err)
	}
	if got.SearchMinMonthlyReal != .29 {
		t.Fatalf("default minimum %.2f want .29", got.SearchMinMonthlyReal)
	}
}

func TestSpendingOptimizerDefaultMaximumSupportsFractionalPhaseStart(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 4000.01
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "initial", StartAge: 0, Multiplier: 1.07}}}
	in := engineInput(t, s)
	got, _, err := normalizeSpendingRequest(in, models.SpendingOptimizerRequest{FloorMonthlyReal: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if got.SearchMaxMonthlyReal != 8560.03 {
		t.Fatalf("default cent endpoint %.8f want 8560.03", got.SearchMaxMonthlyReal)
	}
}
