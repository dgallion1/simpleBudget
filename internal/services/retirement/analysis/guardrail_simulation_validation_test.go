package analysis

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement/prepare"
	"context"
	"reflect"
	"testing"
)

func TestGuardrailSimulationHeldOutReplayEveryCandidate(t *testing.T) {
	s := floorRegressionSettings()
	s.ProjectionYears = 10
	s.MonthlyLivingExpenses = 8000
	s.PortfolioValue = 1200000
	in := engineInput(t, s)
	req := models.GuardrailOptimizerRequest{FloorMonthlyReal: 7500, TargetSuccessPct: 95, Seed: 473}
	r, err := OptimizeGuardrails(context.Background(), in, req)
	if err != nil {
		t.Fatal(err)
	}
	different := false
	for i, c := range r.Candidates {
		cl, err := prepare.Clone(s)
		if err != nil {
			t.Fatal(err)
		}
		cl.Guardrails = guardrailOptimizerCloneConfig(c.Guardrails)
		ci := in
		ci.Prepared = prepare.MustFrom(t, cl)
		rows, err := runGuardrailOptimizerScenarios(context.Background(), ci, r.ValidationSeed, r.ValidationRuns, req.FloorMonthlyReal)
		if err != nil {
			t.Fatal(err)
		}
		y, err := SummarizeGuardrailSimulationYears(rows)
		if err != nil {
			t.Fatal(err)
		}
		m, err := guardrailOptimizerMetrics(rows)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(y, c.SimulationYears) || !reflect.DeepEqual(m, c.Metrics) {
			t.Fatalf("candidate %s differs from held-out replay", c.ID)
		}
		if i > 0 && !reflect.DeepEqual(c.SimulationYears, r.Candidates[0].SimulationYears) {
			different = true
		}
	}
	if !different {
		t.Fatal("no stochastic differences among fixture policies")
	}
}
