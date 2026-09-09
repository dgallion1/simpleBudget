package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/engine"
	"context"
	"math"
	"math/rand"
	"reflect"
	"testing"
)

func TestGuardrailSimulationYearsIndependentMeasuresAndEndedPaths(t *testing.T) {
	row := func(years ...models.GuardrailPathYear) models.MonteCarloResult {
		return models.MonteCarloResult{ProjectionYears: len(years), FloorOutcome: &models.MonteCarloFloorOutcome{MonthsObserved: 12 * len(years), Years: years}}
	}
	rows := []models.MonteCarloResult{
		row(models.GuardrailPathYear{Year: 1, LivingReal: 20, LivingNominal: 100, PortfolioReal: 40, PortfolioNominal: 300}),
		row(models.GuardrailPathYear{Year: 1}, models.GuardrailPathYear{Year: 2}),
		row(models.GuardrailPathYear{Year: 1, LivingReal: 100, LivingNominal: 20, PortfolioReal: 300, PortfolioNominal: 40}, models.GuardrailPathYear{Year: 2, LivingReal: 400}),
	}
	got, err := SummarizeGuardrailSimulationYears(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Paths != 3 || got[1].Paths != 2 {
		t.Fatalf("contributors: %+v", got)
	}
	if got[0].LivingReal != (models.GuardrailPercentiles{P10: 4, P50: 20, P90: 84}) || got[0].LivingReal != got[0].LivingNominal {
		t.Fatalf("independent percentile ordering: %+v", got[0])
	}
	if got[1].LivingReal != (models.GuardrailPercentiles{P10: 40, P50: 200, P90: 360}) {
		t.Fatalf("ended path padded or zero excluded: %+v", got[1])
	}
	if rows[0].FloorOutcome.Years[0].LivingReal != 20 {
		t.Fatal("source mutated")
	}
}

func TestGuardrailSimulationYearsRejectMalformedCapture(t *testing.T) {
	valid := models.MonteCarloResult{ProjectionYears: 1, FloorOutcome: &models.MonteCarloFloorOutcome{MonthsObserved: 12, Years: []models.GuardrailPathYear{{Year: 1}}}}
	for _, kind := range []string{"negative", "nan", "infinity", "skip", "duplicate", "incomplete", "horizon", "mixed"} {
		t.Run(kind, func(t *testing.T) {
			row := valid
			f := *valid.FloorOutcome
			f.Years = append([]models.GuardrailPathYear(nil), f.Years...)
			row.FloorOutcome = &f
			rows := []models.MonteCarloResult{row}
			switch kind {
			case "negative":
				f.Years[0].PortfolioNominal = -1
			case "nan":
				f.Years[0].LivingReal = math.NaN()
			case "infinity":
				f.Years[0].PortfolioReal = math.Inf(1)
			case "skip":
				f.Years[0].Year = 2
			case "duplicate":
				f.Years = append(f.Years, f.Years[0])
				f.MonthsObserved = 24
				rows[0].ProjectionYears = 2
			case "incomplete":
				f.MonthsObserved = 11
			case "horizon":
				rows[0].ProjectionYears = 0
			case "mixed":
				rows = append(rows, models.MonteCarloResult{})
			}
			if _, err := SummarizeGuardrailSimulationYears(rows); err == nil {
				t.Fatal("invalid capture accepted")
			}
		})
	}
	if years, err := SummarizeGuardrailSimulationYears([]models.MonteCarloResult{{}, {FloorOutcome: &models.MonteCarloFloorOutcome{}}}); err != nil || years != nil {
		t.Fatalf("legacy capture: %v %v", years, err)
	}
}

func TestGuardrailSimulationCapturePreservesSeededOutcomes(t *testing.T) {
	for _, assets := range []float64{0, 2000000} {
		s := floorRegressionSettings()
		s.PortfolioValue = assets
		s.InflationRate = 9
		s.MonthlyLivingExpenses = 1000.005
		cfg := DefaultMonteCarloConfig()
		cfg.MinMonthlySpendingReal = 750
		in := engineInput(t, s)
		off := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(914)), cfg)
		cfg.CaptureSpendingYears = true
		on := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(914)), cfg)
		years := on.FloorOutcome.Years
		if len(years) != on.ProjectionYears {
			t.Fatal("missing full-horizon years")
		}
		total := 0.0
		for _, y := range years {
			total += y.LivingReal * 12
			if assets == 0 && (y.LivingReal != 0 || y.PortfolioNominal != 0) {
				t.Fatalf("unfunded spending counted: %+v", y)
			}
		}
		if math.Abs(total-on.FloorOutcome.TotalFundedLivingReal) > 1e-6 {
			t.Fatal("monthly cents do not reconcile")
		}
		last := years[len(years)-1]
		if last.PortfolioNominal != on.FinalBalance || last.PortfolioReal != on.FloorOutcome.FinalBalanceReal {
			t.Fatal("year-end balance mismatch")
		}
		on.FloorOutcome.Years = nil
		if !reflect.DeepEqual(off, on) {
			t.Fatal("capture changed stochastic metrics")
		}
		cfg.MinMonthlySpendingReal = 0
		noFloor := RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(914)), cfg)
		if noFloor.FloorOutcome != nil {
			t.Fatal("capture alone enabled full-horizon observation")
		}
	}
}

func TestGuardrailSimulationCandidatesUseValidationRows(t *testing.T) {
	s := floorRegressionSettings()
	s.MonthlyLivingExpenses = 8000
	request := models.GuardrailOptimizerRequest{FloorMonthlyReal: 7500, TargetSuccessPct: 95, Seed: 23}
	runner := func(_ context.Context, _ engine.Input, seed int64, runs int, _ float64) ([]models.MonteCarloResult, error) {
		rows := make([]models.MonteCarloResult, runs)
		for i := range rows {
			v := float64(runs + i)
			rows[i] = models.MonteCarloResult{Survives: true, ProjectionYears: 1, FloorOutcome: &models.MonteCarloFloorOutcome{MonthsObserved: 12, TotalFundedLivingReal: v * 12, Years: []models.GuardrailPathYear{{Year: 1, LivingReal: v, LivingNominal: v * 2, PortfolioReal: float64(seed%1000 + 1000)}}}}
		}
		return rows, nil
	}
	result, err := optimizeGuardrailsWithRunner(context.Background(), engineInput(t, s), request, runner)
	if err != nil {
		t.Fatal(err)
	}
	baselines := 0
	for _, c := range result.Candidates {
		if c.Baseline {
			baselines++
		}
		if len(c.SimulationYears) != 1 {
			t.Fatalf("missing bands: %s", c.ID)
		}
		y := c.SimulationYears[0]
		if y.Paths != result.ValidationRuns || y.LivingReal.P50 != 1499.5 || y.PortfolioReal.P50 != float64(result.ValidationSeed%1000+1000) {
			t.Fatalf("search rows leaked: %s %+v", c.ID, y)
		}
		if y.LivingReal.P50*12 != c.Metrics.MedianLifetimeFundedLivingReal {
			t.Fatal("different metric rows")
		}
	}
	if baselines != 2 {
		t.Fatal("missing baselines")
	}
}
