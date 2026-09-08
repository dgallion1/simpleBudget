package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
)

func go2Input(t *testing.T) engine.Input {
	t.Helper()
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 8000
	s.ProjectionYears = 20
	s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 17, FloorCutPct: 7, CeilingRisePct: 23, CeilingRaisePct: 9, MinSpendingPct: 72, MaxSpendingPct: 137, MinMonthlySpendingReal: 1234}
	return engine.Input{Prepared: prepare.MustFrom(t, s)}
}
func go2Req() models.GuardrailOptimizerRequest {
	return models.GuardrailOptimizerRequest{FloorMonthlyReal: 7500, TargetSuccessPct: 95, Seed: 887766}
}

// Reflection intentionally decouples the oracle from the additive GO1 outcome
// type's name while still requiring every approved exported observation field.
func go2Outcome(fail bool, spending, cut, count, ending float64) models.MonteCarloResult {
	r := models.MonteCarloResult{Survives: true, ProjectionYears: 20, FinalBalance: 999999999}
	field := reflect.ValueOf(&r).Elem().FieldByName("FloorOutcome")
	field.Set(reflect.New(field.Type().Elem()))
	o := field.Elem()
	o.FieldByName("FloorFailed").SetBool(fail)
	for k, v := range map[string]float64{"TotalFundedLivingReal": spending, "WorstAnnualCutPct": cut, "FinalBalanceReal": ending} {
		o.FieldByName(k).SetFloat(v)
	}
	o.FieldByName("AnnualCutCount").SetInt(int64(count))
	o.FieldByName("MonthsObserved").SetInt(240)
	return r
}
func go2Rows(n, failures int, spending, cut, count float64) []models.MonteCarloResult {
	out := make([]models.MonteCarloResult, n)
	for i := range out {
		out[i] = go2Outcome(i < failures, spending, cut, count, 4321)
		if i < 7 {
			out[i].Survives = false
			out[i].DepletionYear = 2
		}
	}
	return out
}
func go2Key(g *models.GuardrailConfig) string { b, _ := json.Marshal(g); return string(b) }

func TestGO2OracleGridSeedsMetricsAndBaselines(t *testing.T) {
	in := go2Input(t)
	req := go2Req()
	before := go2Key(in.Prepared.Settings().Guardrails)
	var mu sync.Mutex
	search := map[string]int{}
	validation := map[string]int{}
	seeds := map[int]int64{}
	runner := func(ctx context.Context, got engine.Input, seed int64, n int, floor float64) ([]models.MonteCarloResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if floor != 7500 {
			t.Errorf("observer floor=%v", floor)
		}
		if n != 64 && n != 1000 {
			t.Errorf("unbounded/default count %d", n)
		}
		if seed == 0 {
			t.Error("unresolved zero seed")
		}
		if old, ok := seeds[n]; ok && old != seed {
			t.Error("candidates used different scenarios")
		}
		seeds[n] = seed
		key := go2Key(got.Prepared.Settings().Guardrails)
		if n == 64 {
			search[key]++
		} else {
			validation[key]++
		}
		return go2Rows(n, n/20, 123456, 12.5, 3), nil
	}
	result, err := optimizeGuardrailsWithRunner(context.Background(), in, req, runner)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if len(search) != 33 {
		t.Fatalf("searched %d policies, want 32 grid + distinct current-floor", len(search))
	}
	for _, drop := range []float64{10, 20} {
		for _, cut := range []float64{5, 10} {
			for _, rise := range []float64{15, 25} {
				for _, raise := range []float64{5, 10} {
					for _, cap := range []float64{120, 150} {
						g := &models.GuardrailConfig{Enabled: true, FloorDropPct: drop, FloorCutPct: cut, CeilingRisePct: rise, CeilingRaisePct: raise, MaxSpendingPct: cap, MinMonthlySpendingReal: 7500}
						if search[go2Key(g)] != 1 {
							t.Errorf("missing/duplicate grid setting %+v", g)
						}
					}
				}
			}
		}
	}
	currentFloor := *in.Prepared.Settings().Guardrails
	currentFloor.MinMonthlySpendingReal = 7500
	currentFloor.MinSpendingPct = 0
	if search[go2Key(&currentFloor)] != 1 {
		t.Error("current with floor absent")
	}
	if seeds[64] == seeds[1000] {
		t.Error("validation reused search seed")
	}
	if result.Seed != req.Seed || result.ValidationSeed != seeds[1000] || result.SearchRuns != 64 || result.ValidationRuns != 1000 || !reflect.DeepEqual(result.Request, req) {
		t.Errorf("provenance metadata incorrect: %+v", result)
	}
	if go2Key(in.Prepared.Settings().Guardrails) != before {
		t.Error("input mutated")
	}
	if len(validation) < 3 || len(validation) > 8 {
		t.Errorf("validation policy count %d, want 1..6 shortlist + 2 baselines", len(validation))
	}
	if validation[before] != 1 {
		t.Error("current baseline missing or policy changed")
	}
	baselineCount := 0
	seen := map[string]bool{}
	for _, c := range result.Candidates {
		if c.ID == "" || seen[c.ID] {
			t.Error("candidate ID missing/duplicate")
		}
		seen[c.ID] = true
		if c.Baseline {
			baselineCount++
			switch c.ID {
			case "current":
				if go2Key(c.Guardrails) != before {
					t.Error("current baseline result changed")
				}
			case "no-guardrails":
				if c.Guardrails != nil && c.Guardrails.Enabled {
					t.Error("disabled baseline remains enabled")
				}
			default:
				t.Errorf("unexpected baseline identity %s", c.ID)
			}
		}
		m := c.Metrics
		if m.Runs != 1000 || m.FloorShortfallPaths != 50 || m.FloorSuccessPct != 95 || m.DepletionPaths != 7 || math.Abs(m.DepletionRiskPct-.7) > 1e-10 {
			t.Errorf("counts/rates wrong: %+v", m)
		}
		if m.MedianLifetimeFundedLivingReal != 123456 || m.P10LifetimeFundedLivingReal != 123456 || m.P95WorstAnnualCutPct != 12.5 || math.Abs(m.MeanAnnualCutCount-3) > 1e-10 || m.MedianEndingBalanceReal != 4321 {
			t.Errorf("funded/real metrics wrong: %+v", m)
		}
		if math.Abs(m.FloorSuccessCILowPct-93.4686179756) > 1e-5 || math.Abs(m.FloorSuccessCIHighPct-96.1869737607) > 1e-5 {
			t.Errorf("Wilson95 interval wrong: [%v,%v]", m.FloorSuccessCILowPct, m.FloorSuccessCIHighPct)
		}
		if !c.Qualifies {
			t.Error("exact 95% boundary rejected")
		}
	}
	if baselineCount != 2 {
		t.Errorf("baseline count %d", baselineCount)
	}
	if result.HorizonMinYears != 15 || result.HorizonMaxYears != 25 {
		t.Errorf("longevity assumptions missing: %d..%d", result.HorizonMinYears, result.HorizonMaxYears)
	}
	if len(result.Recommendations) != 1 {
		t.Errorf("identical-metric winners should deduplicate: %v", result.Recommendations)
	}
	for _, id := range result.Recommendations {
		if !seen[id] {
			t.Errorf("unresolvable recommendation %s", id)
		}
	}
}

func TestGO2OracleValidationAndCancellation(t *testing.T) {
	in := go2Input(t)
	calls := 0
	runner := func(context.Context, engine.Input, int64, int, float64) ([]models.MonteCarloResult, error) {
		calls++
		return nil, fmt.Errorf("must not run")
	}
	for _, f := range []float64{0, -1, 8000.01, math.NaN(), math.Inf(1), math.Inf(-1)} {
		r := go2Req()
		r.FloorMonthlyReal = f
		if _, err := optimizeGuardrailsWithRunner(context.Background(), in, r, runner); err == nil {
			t.Errorf("floor accepted %v", f)
		}
	}
	for _, v := range []float64{0, 100, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		r := go2Req()
		r.TargetSuccessPct = v
		if _, err := optimizeGuardrailsWithRunner(context.Background(), in, r, runner); err == nil {
			t.Errorf("target accepted %v", v)
		}
	}
	chain := in
	chain.Chain = []engine.PreparedChainLink{{}}
	if _, err := optimizeGuardrailsWithRunner(context.Background(), chain, go2Req(), runner); err == nil {
		t.Error("chain accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := optimizeGuardrailsWithRunner(ctx, in, go2Req(), runner); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel error %v", err)
	}
	if calls != 0 {
		t.Errorf("invalid or canceled requests simulated %d times", calls)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	runner = func(context.Context, engine.Input, int64, int, float64) ([]models.MonteCarloResult, error) {
		calls++
		cancel()
		return go2Rows(64, 0, 1, 0, 0), nil
	}
	if _, err := optimizeGuardrailsWithRunner(ctx, in, go2Req(), runner); !errors.Is(err, context.Canceled) {
		t.Errorf("mid-search cancel error %v", err)
	}
	if calls > 8 {
		t.Errorf("cancellation failed bounded work: %d calls", calls)
	}
}

func TestGO2OracleHeldOutFailureAndExactQualification(t *testing.T) {
	for _, tc := range []struct {
		target float64
		fail   int
		qual   bool
	}{{95, 50, true}, {95.00001, 50, false}, {95, 51, false}, {99, 10, true}, {99.00001, 10, false}} {
		t.Run(fmt.Sprintf("%g_%d", tc.target, tc.fail), func(t *testing.T) {
			req := go2Req()
			req.TargetSuccessPct = tc.target
			runner := func(_ context.Context, _ engine.Input, _ int64, n int, _ float64) ([]models.MonteCarloResult, error) {
				fail := 0
				if n == 1000 {
					fail = tc.fail
				}
				return go2Rows(n, fail, 100, 1, 1), nil
			}
			r, err := optimizeGuardrailsWithRunner(context.Background(), go2Input(t), req, runner)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range r.Candidates {
				if c.Qualifies != tc.qual {
					t.Errorf("qualification rounded or based on search: %+v", c)
				}
			}
			if !tc.qual && len(r.Recommendations) != 0 {
				t.Error("infeasible validation recommended a policy")
			}
		})
	}
}

func TestGO2OracleRepeatableRanking(t *testing.T) {
	runner := func(_ context.Context, in engine.Input, _ int64, n int, _ float64) ([]models.MonteCarloResult, error) {
		g := in.Prepared.Settings().Guardrails
		spend, cut, count, fail := 100.0, 20.0, 4.0, n/100
		if g != nil && g.Enabled {
			// Three objectively different choices; all other configurations dominated.
			switch {
			case g.FloorDropPct == 10 && g.FloorCutPct == 5 && g.CeilingRisePct == 15 && g.CeilingRaisePct == 5 && g.MaxSpendingPct == 120:
				spend, cut, count, fail = 300, 15, 3, n/20
			case g.FloorDropPct == 20 && g.FloorCutPct == 5 && g.CeilingRisePct == 15 && g.CeilingRaisePct == 5 && g.MaxSpendingPct == 120:
				spend, cut, count, fail = 200, 2, 1, n/25
			case g.FloorDropPct == 10 && g.FloorCutPct == 10 && g.CeilingRisePct == 15 && g.CeilingRaisePct == 5 && g.MaxSpendingPct == 120:
				spend, cut, count, fail = 150, 10, 2, 0
			}
		}
		return go2Rows(n, fail, spend, cut, count), nil
	}
	in := go2Input(t)
	req := go2Req()
	a, err := optimizeGuardrailsWithRunner(context.Background(), in, req, runner)
	if err != nil {
		t.Fatal(err)
	}
	b, err := optimizeGuardrailsWithRunner(context.Background(), in, req, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Error("same seed not repeatable")
	}
	if len(a.Recommendations) != 3 {
		t.Fatalf("want distinct spending/smooth/risk choices, got %v", a.Recommendations)
	}
	want := []float64{300, 200, 150}
	labels := []string{"Higher spending", "Smoother spending", "Lower risk"}
	for i, id := range a.Recommendations {
		found := false
		for _, c := range a.Candidates {
			if c.ID == id {
				found = true
				if !reflect.DeepEqual(c.Labels, []string{labels[i]}) {
					t.Errorf("category label incorrect: %v", c.Labels)
				}
				if c.Baseline || !c.Qualifies || c.Metrics.MedianLifetimeFundedLivingReal != want[i] {
					t.Errorf("rank %d wrong: %+v", i, c)
				}
			}
		}
		if !found {
			t.Error("missing recommendation")
		}
	}
	for _, c := range a.Candidates {
		if !c.Baseline && c.Metrics.MedianLifetimeFundedLivingReal == 100 {
			t.Error("strictly dominated policy shortlisted")
		}
	}
}

func TestGO2OracleDedupAndAutoSeed(t *testing.T) {
	in := go2Input(t)
	s, _ := prepare.Clone(in.Prepared.Settings())
	s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 10, FloorCutPct: 5, CeilingRisePct: 15, CeilingRaisePct: 5, MaxSpendingPct: 120}
	in.Prepared = prepare.MustFrom(t, s)
	req := go2Req()
	req.Seed = 0
	req.FloorMonthlyReal = 8000
	var mu sync.Mutex
	searches := map[string]int{}
	runner := func(_ context.Context, i engine.Input, seed int64, n int, floor float64) ([]models.MonteCarloResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if seed == 0 {
			t.Error("auto seed unresolved")
		}
		if floor != 8000 {
			t.Error("equal-to-start floor changed")
		}
		if n == 64 {
			searches[go2Key(i.Prepared.Settings().Guardrails)]++
		}
		return go2Rows(n, 0, 10, 0, 0), nil
	}
	r, err := optimizeGuardrailsWithRunner(context.Background(), in, req, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(searches) != 32 {
		t.Errorf("grid/current dedup policies %d", len(searches))
	}
	for _, n := range searches {
		if n != 1 {
			t.Error("duplicate policy simulated")
		}
	}
	if r.Seed == 0 || r.ValidationSeed == 0 || r.Seed == r.ValidationSeed {
		t.Error("auto-seed provenance invalid")
	}
	// Once resolved, the seed must reproduce all observable results.
	req.Seed = r.Seed
	again, err := optimizeGuardrailsWithRunner(context.Background(), in, req, runner)
	if err != nil {
		t.Fatal(err)
	}
	r.Request.Seed = req.Seed
	if !reflect.DeepEqual(r, again) {
		t.Error("resolved seed cannot reproduce result")
	}
}

func TestGO2OracleAllExactBoundaries(t *testing.T){
 for successes:=1;successes<1000;successes++ {target:=float64(successes)/10; m:=models.GuardrailOptimizerMetrics{Runs:1000,FloorShortfallPaths:1000-successes}
 if !guardrailOptimizerQualifies(m,target){t.Errorf("exact boundary successes=%d target=%.17g target*runs=%.17g",successes,target,target*1000)}
 }
}

func TestGO2OracleCustomBoundaryService(t *testing.T){
 s:=models.DefaultWhatIfSettings();s.MonthlyLivingExpenses=8000
 run:=func(_ context.Context,_ engine.Input,_ int64,n int,_ float64)([]models.MonteCarloResult,error){
 rows:=make([]models.MonteCarloResult,n);for j:=range rows {rows[j]=models.MonteCarloResult{Survives:true,FloorOutcome:&models.MonteCarloFloorOutcome{FloorFailed:n==1000&&j>=644,TotalFundedLivingReal:100,MonthsObserved:120}}};return rows,nil }
 r,err:=optimizeGuardrailsWithRunner(context.Background(),engineInput(t,s),models.GuardrailOptimizerRequest{FloorMonthlyReal:7500,TargetSuccessPct:64.4,Seed:123},run)
 if err!=nil{t.Fatal(err)}
 if len(r.Recommendations)==0{t.Fatalf("no recommendations despite exact64.4%%; first metrics=%+v qualifies=%v",r.Candidates[0].Metrics,r.Candidates[0].Qualifies)}
}
func TestGO2OracleRemedyPremise(t *testing.T){
 for successes:=1;successes<1000;successes++{target:=float64(successes)/10;observed:=float64(successes)*100/1000
 if observed<target || observed>=target+0.0000001 {t.Fatalf("direct unrounded rate remedy fails %d",successes)}
 }
}
