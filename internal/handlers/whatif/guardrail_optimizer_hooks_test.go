package whatif

import (
	"math"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
)

// guardrailHooksFixture is a plan where Social Security is the difference
// between surviving and depleting: a small portfolio, living expenses the
// portfolio alone cannot carry for the horizon, and an SS-optimizer benefit
// already being claimed. This lets the regression tests below discriminate
// between a search that simulates with the plan's engine hooks and one that
// silently drops the Social Security income stream.
func guardrailHooksFixture(t *testing.T, rm *retirement.SettingsManager) *models.WhatIfSettings {
	t.Helper()
	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.PortfolioValue = 60000
	s.MonthlyLivingExpenses = 4000
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 0
	s.ProjectionYears = 4
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: false}
	s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 10, CeilingRisePct: 20, CeilingRaisePct: 10, MinSpendingPct: 75, MaxSpendingPct: 120}
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 3500, FRA: 67, ClaimAge: 65}
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	loaded, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !retirement.SocialSecurityProjectionActive(loaded) {
		t.Fatal("fixture defect: SS projection is not active")
	}
	return loaded
}

// guardrailHooksDepletionCount runs a direct Monte Carlo (not through the
// optimizer) with the given hooks, so tests can compare the optimizer's
// retained metrics against an independently reproduced run.
func guardrailHooksDepletionCount(t *testing.T, s *models.WhatIfSettings, hooks engine.Hooks, runs int, seed int64) int {
	t.Helper()
	in, _, err := buildEngineInput(s)
	if err != nil {
		t.Fatal(err)
	}
	in.Hooks = hooks
	_, rows := analysis.MonteCarloWithResults(getEngine(), in, runs, seed)
	n := 0
	for _, r := range rows.Runs {
		if !r.Survives {
			n++
		}
	}
	return n
}

// TestGuardrailOptimizerSearchUsesPlanHooks is criterion 2: the optimizer's
// "current" baseline candidate must reproduce a direct Monte Carlo run built
// with the plan's engine hooks, exactly, per-run seed for per-run seed.
func TestGuardrailOptimizerSearchUsesPlanHooks(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s := guardrailHooksFixture(t, rm)

	const id = "guardrail-hooks-current"
	form := url.Values{"floor_monthly_real": {"3000"}, "target_success_pct": {"50"}, "request_id": {id}}
	req := httptest.NewRequest("POST", "/whatif/guardrails/optimize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleGuardrailOptimizer(w, req)
	if w.Code != 200 {
		t.Fatalf("optimizer status %d: %s", w.Code, w.Body.String())
	}

	guardrailPreviews.Lock()
	p := guardrailPreviews.entries[id]
	guardrailPreviews.Unlock()
	if p == nil {
		t.Fatal("no retained preview for the request")
	}
	var current *models.GuardrailOptimizerCandidate
	for _, c := range p.graphs {
		if c.ID == "current" {
			cc := c
			current = &cc
		}
	}
	if current == nil {
		t.Fatal("no 'current' baseline candidate retained")
	}
	if current.Metrics.Runs != p.validationRuns {
		t.Fatalf("baseline metrics runs %d != validation runs %d", current.Metrics.Runs, p.validationRuns)
	}

	withHooks := guardrailHooksDepletionCount(t, s, retirement.DefaultHooks(), p.validationRuns, p.validationSeed)
	noHooks := guardrailHooksDepletionCount(t, s, engine.Hooks{}, p.validationRuns, p.validationSeed)
	if withHooks == noHooks {
		t.Fatal("fixture defect: hooks make no difference on this plan, the regression cannot discriminate")
	}
	if withHooks >= p.validationRuns/2 {
		t.Fatalf("fixture defect: even with Social Security %d/%d runs deplete", withHooks, p.validationRuns)
	}
	if current.Metrics.DepletionPaths != withHooks {
		t.Fatalf("optimizer 'current' depletion paths = %d, direct Monte Carlo with the plan's hooks = %d (without hooks = %d); the search is not simulating with the plan's engine hooks",
			current.Metrics.DepletionPaths, withHooks, noHooks)
	}
	if math.Abs(current.Metrics.DepletionRiskPct-float64(withHooks)/float64(p.validationRuns)*100) > 1e-9 {
		t.Fatalf("DepletionRiskPct %.4f is not %d/%d*100", current.Metrics.DepletionRiskPct, withHooks, p.validationRuns)
	}
}

// TestGuardrailOptimizerEveryCandidateUsesPlanHooks is criterion 3: every
// retained candidate — grid alternatives and both baselines — was simulated
// with the plan's hooks. For each candidate, clone the plan with exactly
// that candidate's guardrail config (the graph endpoint's clone rule) and
// reproduce its depletion count with a direct hooked Monte Carlo on the
// validation seed.
func TestGuardrailOptimizerEveryCandidateUsesPlanHooks(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s := guardrailHooksFixture(t, rm)

	const id = "guardrail-hooks-candidates"
	form := url.Values{"floor_monthly_real": {"3000"}, "target_success_pct": {"50"}, "request_id": {id}}
	req := httptest.NewRequest("POST", "/whatif/guardrails/optimize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleGuardrailOptimizer(w, req)
	if w.Code != 200 {
		t.Fatalf("optimizer status %d: %s", w.Code, w.Body.String())
	}
	guardrailPreviews.Lock()
	p := guardrailPreviews.entries[id]
	guardrailPreviews.Unlock()
	if p == nil {
		t.Fatal("no retained preview")
	}
	seen := map[string]bool{}
	for _, c := range p.graphs {
		clone := *s
		clone.Guardrails = nil
		if c.Guardrails != nil {
			cfg := *c.Guardrails
			clone.Guardrails = &cfg
		}
		withHooks := guardrailHooksDepletionCount(t, &clone, retirement.DefaultHooks(), p.validationRuns, p.validationSeed)
		if c.Metrics.DepletionPaths != withHooks {
			t.Fatalf("candidate %s depletion %d != hooked direct run %d", c.ID, c.Metrics.DepletionPaths, withHooks)
		}
		seen[c.ID] = true
	}
	if !seen["current"] || !seen["no-guardrails"] {
		t.Fatalf("both baselines must be retained, saw %v", seen)
	}
}
