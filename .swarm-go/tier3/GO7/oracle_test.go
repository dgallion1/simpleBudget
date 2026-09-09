package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func go7Near(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("got %.10f want %.10f", got, want)
	}
}
func go7Row(ys ...models.GuardrailPathYear) models.MonteCarloResult {
	return models.MonteCarloResult{ProjectionYears: len(ys), FloorOutcome: &models.MonteCarloFloorOutcome{MonthsObserved: 12 * len(ys), Years: ys}}
}
func TestGO7OraclePercentiles(t *testing.T) {
	rows := []models.MonteCarloResult{
		go7Row(models.GuardrailPathYear{Year: 1}, models.GuardrailPathYear{Year: 2}),
		go7Row(models.GuardrailPathYear{Year: 1, LivingReal: 100, LivingNominal: 600, PortfolioReal: 300, PortfolioNominal: 400}),
		go7Row(models.GuardrailPathYear{Year: 1, LivingReal: 300, LivingNominal: 200, PortfolioReal: 900, PortfolioNominal: 1200}, models.GuardrailPathYear{Year: 2, LivingReal: 500, LivingNominal: 700, PortfolioReal: 1000, PortfolioNominal: 1500}),
	}
	got, e := SummarizeGuardrailSimulationYears(rows)
	if e != nil {
		t.Fatal(e)
	}
	if len(got) != 2 || got[0].Paths != 3 || got[1].Paths != 2 || got[1].Year != 2 {
		t.Fatalf("horizon counts %+v", got)
	}
	want := []models.GuardrailPercentiles{{20, 100, 260}, {40, 200, 520}, {60, 300, 780}, {80, 400, 1040}, {50, 250, 450}}
	actual := []models.GuardrailPercentiles{got[0].LivingReal, got[0].LivingNominal, got[0].PortfolioReal, got[0].PortfolioNominal, got[1].LivingReal}
	for i := range want {
		go7Near(t, actual[i].P10, want[i].P10)
		go7Near(t, actual[i].P50, want[i].P50)
		go7Near(t, actual[i].P90, want[i].P90)
	}
	blob, e := json.Marshal(got)
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"living_real", "living_nominal", "portfolio_real", "portfolio_nominal", "paths", "p10", "p50", "p90"} {
		if !strings.Contains(string(blob), "\""+key+"\"") {
			t.Errorf("JSON consumer missing %s", key)
		}
	}
	inconsistent := go7Row(models.GuardrailPathYear{Year: 1})
	inconsistent.FloorOutcome.MonthsObserved = 11
	if _, e := SummarizeGuardrailSimulationYears([]models.MonteCarloResult{inconsistent}); e == nil {
		t.Error("incomplete year accepted")
	}
	if out, e := SummarizeGuardrailSimulationYears([]models.MonteCarloResult{{FloorOutcome: &models.MonteCarloFloorOutcome{}}}); e != nil || out != nil {
		t.Fatalf("legacy nil %v %v", out, e)
	}
	for _, bad := range []models.GuardrailPathYear{{Year: 0}, {Year: 2}, {Year: 1, LivingReal: -1}, {Year: 1, LivingNominal: math.NaN()}, {Year: 1, PortfolioReal: math.Inf(1)}, {Year: 1, PortfolioNominal: -1}} {
		if _, e := SummarizeGuardrailSimulationYears([]models.MonteCarloResult{go7Row(bad)}); e == nil {
			t.Errorf("invalid annual value accepted %+v", bad)
		}
	}
	if _, e := SummarizeGuardrailSimulationYears([]models.MonteCarloResult{rows[0], {FloorOutcome: &models.MonteCarloFloorOutcome{}}}); e == nil {
		t.Error("mixed capture accepted")
	}
}
func TestGO7OracleCaptureParity(t *testing.T) {
	for _, assets := range []float64{0, 2000000} {
		s := floorRegressionSettings()
		s.PortfolioValue = assets
		s.InflationRate = 12
		s.MonthlyLivingExpenses = 1000.005
		in := engineInput(t, s)
		off := DefaultMonteCarloConfig()
		off.MinMonthlySpendingReal = 750
		on := *off
		on.CaptureSpendingYears = true
		a := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(812)), off)
		b := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(812)), &on)
		if a.FloorOutcome == nil || b.FloorOutcome == nil {
			t.Fatal("missing observer")
		}
		ys := b.FloorOutcome.Years
		if len(ys) != b.FloorOutcome.MonthsObserved/12 || len(ys) == 0 {
			t.Fatal("annual capture missing")
		}
		sum := 0.0
		for i, y := range ys {
			if y.Year != i+1 {
				t.Fatal("year index")
			}
			sum += 12 * y.LivingReal
			if y.LivingReal < 0 || y.PortfolioReal < 0 {
				t.Fatal("negative values")
			}
		}
		go7Near(t, sum, b.FloorOutcome.TotalFundedLivingReal)
		go7Near(t, ys[len(ys)-1].PortfolioReal, math.Max(0, b.FloorOutcome.FinalBalanceReal))
		go7Near(t, ys[len(ys)-1].PortfolioNominal, math.Max(0, b.FinalBalance))
		b.FloorOutcome.Years = nil
		if !reflect.DeepEqual(a, b) {
			t.Fatal("capture changed seeded outcome metrics")
		}
		on.MinMonthlySpendingReal = 0
		c := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(812)), &on)
		if c.FloorOutcome != nil {
			t.Fatal("capture enabled observation without floor")
		}
	}
}
func TestGO7OracleHeldOutConsumers(t *testing.T) {
	s := floorRegressionSettings()
	s.MonthlyLivingExpenses = 8000
	in := engineInput(t, s)
	run := func(_ context.Context, _ engine.Input, seed int64, n int, _ float64) ([]models.MonteCarloResult, error) {
		rows := make([]models.MonteCarloResult, n)
		v := float64(n)
		for i := range rows {
			rows[i] = go7Row(models.GuardrailPathYear{Year: 1, LivingReal: v, LivingNominal: v * 2, PortfolioReal: float64(seed%10000 + 10000), PortfolioNominal: v * 3})
			rows[i].Survives = true
			rows[i].FloorOutcome.TotalFundedLivingReal = v * 12
		}
		return rows, nil
	}
	req := models.GuardrailOptimizerRequest{FloorMonthlyReal: 7500, TargetSuccessPct: 95, Seed: 42}
	result, e := optimizeGuardrailsWithRunner(context.Background(), in, req, run)
	if e != nil {
		t.Fatal(e)
	}
	base := 0
	for _, c := range result.Candidates {
		if c.Baseline {
			base++
		}
		if len(c.SimulationYears) != 1 {
			t.Fatalf("%s missing bands", c.ID)
		}
		y := c.SimulationYears[0]
		if y.Paths != 1000 || y.LivingReal.P50 != 1000 || y.PortfolioReal.P50 != float64(result.ValidationSeed%10000+10000) {
			t.Fatalf("%s not held out %+v", c.ID, y)
		}
		go7Near(t, y.LivingReal.P50*12, c.Metrics.MedianLifetimeFundedLivingReal)
	}
	if base != 2 {
		t.Fatal("baselines absent")
	}
	// Production runner must opt in and share exactly the same seeded rows.
	rows, e := runGuardrailOptimizerScenarios(context.Background(), in, 99, 5, 7500)
	if e != nil {
		t.Fatal(e)
	}
	again, e := runGuardrailOptimizerScenarios(context.Background(), in, 99, 5, 7500)
	if e != nil || !reflect.DeepEqual(rows, again) {
		t.Fatal("runner nondeterministic")
	}
	for _, r := range rows {
		if r.FloorOutcome == nil || len(r.FloorOutcome.Years) == 0 {
			t.Fatal("real runner capture missing")
		}
	}
	// A small actual search proves every final candidate includes validation bands.
	s.ProjectionYears = 1
	in.Prepared = prepare.MustFrom(t, s)
	real, e := OptimizeGuardrails(context.Background(), in, req)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range real.Candidates {
		if len(c.SimulationYears) == 0 || c.SimulationYears[0].Paths != c.Metrics.Runs {
			t.Fatalf("actual candidate missing contributors %s", c.ID)
		}
	}
}

func TestGO7OracleAnnualNominalAndCPI(t *testing.T) {
	s := floorRegressionSettings()
	s.Guardrails = nil
	s.MonthlyLivingExpenses = 1000
	s.InflationRate = 12
	cfg := &MonteCarloConfig{MinMonthlySpendingReal: 750, CaptureSpendingYears: true}
	row := RunSingleMonteCarloSimulation(engineInput(t, s), rand.New(rand.NewSource(812)), cfg)
	rng := rand.New(rand.NewSource(812))
	last := -999
	generateAssetReturns(rng, cfg, s.ProjectionYears, &CrashTiming{}, &last)
	cpi := 1.0
	for year := 0; year < s.ProjectionYears; year++ {
		rate := s.InflationRate / 100 * (1 + (rng.Float64()-.5)*.02)
		rng.Float64()
		rng.Float64()
		rng.Float64()
		nominalSum := 0.0
		for month := 0; month < 12; month++ {
			if year != 0 || month != 0 {
				cpi *= math.Pow(1+rate, 1.0/12)
			}
			nominalSum += 1000 * cpi
		}
		y := row.FloorOutcome.Years[year]
		go7Near(t, y.LivingNominal, nominalSum/12)
		go7Near(t, y.LivingReal, 1000)
		go7Near(t, y.PortfolioReal, y.PortfolioNominal/cpi)
	}
}
