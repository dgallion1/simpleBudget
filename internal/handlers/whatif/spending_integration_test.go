package whatif

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

// This crosses the actual search, retained graph, atomic save, disk reload,
// canonical execution and final-seed replay boundaries without seeded tokens.
func TestSpendingIntegrationPreviewGraphApplyReload(t *testing.T) {
	rm, s := spendingFixture(t)
	s.ProjectionYears = 10
	s.SocialSecurity = &models.SocialSecurityConfig{FRABenefit: 2400, FRA: 67, ClaimAge: 67, COLARate: .02, COLARateSet: true}
	s.IncomeSources = []models.IncomeSource{{ID: "pension", Name: "Pension", Amount: 1200, StartMonth: 6}}
	if e := rm.Save(s); e != nil {
		t.Fatal(e)
	}
	rm.InvalidateCache()
	before := spendingBytes(t, rm)
	w := httptest.NewRecorder()
	handleSpendingOptimizer(w, spendingPost(url.Values{"request_id": {"integration-preview"}, "floor_monthly_real": {"6000.25"}, "search_min_monthly_real": {"8000.25"}, "search_max_monthly_real": {"8100.25"}, "search_step_monthly_real": {"100"}, "seed": {"20260910"}, "boost_enabled": {"on"}, "boost_monthly_real": {"1000.25"}, "boost_stop_month": {"2027-09"}}))
	if w.Code != 200 {
		t.Fatalf("preview %d %s", w.Code, w.Body.String())
	}
	var view spendingOptimizerView
	if e := json.Unmarshal(w.Body.Bytes(), &view); e != nil {
		t.Fatal(e)
	}
	var chosen spendingOptimizerRow
	for _, row := range view.Rows {
		if row.Token != "" && row.Candidate.Kind == "planned" {
			chosen = row
			break
		}
	}
	if chosen.Token == "" {
		t.Fatal("no actual qualifying plan")
	}
	graph := httptest.NewRecorder()
	handleSpendingOptimizerGraph(graph, spendingPost(url.Values{"request_id": {view.RequestID}, "candidate": {chosen.GraphToken}}))
	if graph.Code != 200 {
		t.Fatalf("graph %d %s", graph.Code, graph.Body.String())
	}
	var payload spendingGraphPayload
	if e := json.Unmarshal(graph.Body.Bytes(), &payload); e != nil {
		t.Fatal(e)
	}
	if before != spendingBytes(t, rm) {
		t.Fatal("preview or graph mutated saved scenario")
	}
	if payload.FundingTimeline == nil || !payload.FundingTimeline.Complete || payload.FundingTimeline.ObservedMonths != 120 {
		t.Fatal("canonical timeline truncated")
	}
	months := payload.FundingTimeline.Months
	if math.Abs(months[0].PlannedLivingReal-chosen.Candidate.StartingMonthlyLivingReal) > .005 || math.Abs(months[11].PlannedLivingReal-months[12].PlannedLivingReal-1000.25) > .005 || months[23].SocialSecurityReal != 0 || months[24].SocialSecurityReal <= 0 || chosen.Candidate.Metrics.CutPaths != 0 {
		t.Fatal("boost expiry, starting dollars, or claiming timeline changed")
	}
	apply := httptest.NewRecorder()
	handleApplySpendingOptimizer(apply, spendingPost(url.Values{"request_id": {view.RequestID}, "recommendation": {chosen.Token}}))
	if apply.Code != 200 {
		t.Fatalf("Apply %d %s", apply.Code, apply.Body.String())
	}
	rm.InvalidateCache()
	saved, e := rm.Load()
	if e != nil {
		t.Fatal(e)
	}
	c := chosen.Candidate
	if saved.MonthlyLivingExpenses != c.BaseMonthlyLivingExpenses || !reflect.DeepEqual(saved.Guardrails, c.Guardrails) || !reflect.DeepEqual(saved.LivingSpendingBoost, c.LivingSpendingBoost) {
		t.Fatal("persisted complete plan differs from preview")
	}
	in, _, e := buildEngineInput(saved)
	if e != nil {
		t.Fatal(e)
	}
	in.Hooks = retirement.DefaultHooks()
	if math.Abs(engine.LivingExpensesAtMonth(in.Prepared.Settings(), 0)-c.StartingMonthlyLivingReal) > .005 {
		t.Fatal("reloaded actual starting dollars")
	}
	projection := getEngine().Run(in)
	timeline, e := analysis.BuildSpendingFundingTimeline(in, projection)
	if e != nil {
		t.Fatal(e)
	}
	addProjectedSSFundingMarkers(timeline, in.Prepared.Settings())
	if !reflect.DeepEqual(timeline, payload.FundingTimeline) {
		t.Fatal("canonical income/withdrawal/tax/funded timeline did not replay exactly")
	}
	cfg := analysis.DefaultMonteCarloConfig()
	cfg.MinMonthlySpendingReal = view.Optimizer.Request.FloorMonthlyReal
	cfg.SpendingExperienceYears = 5
	cfg.CaptureSpendingYears = true
	cfg.AdaptiveSpending = false
	master := rand.New(rand.NewSource(view.Optimizer.ValidationSeed))
	cuts, floors, unpaid := 0, 0, 0
	for i := 0; i < 1000; i++ {
		r := analysis.RunSingleMonteCarloSimulation(in, rand.New(rand.NewSource(master.Int63())), cfg)
		o := r.SpendingOutcome
		if o.MonthsObserved != 12*r.ProjectionYears {
			t.Fatal("seed replay shortened")
		}
		if o.MonthsBelowPlan > 0 {
			cuts++
		}
		if o.FloorShortfallMonths > 0 {
			floors++
		}
		if o.UnpaidObligationMonths > 0 {
			unpaid++
		}
	}
	if cuts != c.Metrics.CutPaths || floors != c.Metrics.FloorShortfallPaths || unpaid != c.Metrics.UnpaidObligationPaths {
		t.Fatal("recorded seed did not reproduce published counts")
	}
	t.Logf("master=%d selection=%d final=%d paths=1000 cut/floor/unpaid=%d/%d/%d base=%.2f start=%.2f boost=%.2f stop=%s", view.Optimizer.SearchSeed, view.Optimizer.SelectionSeed, view.Optimizer.ValidationSeed, cuts, floors, unpaid, saved.MonthlyLivingExpenses, c.StartingMonthlyLivingReal, saved.LivingSpendingBoost.MonthlyReal, saved.LivingSpendingBoost.StopMonth)
	g := saved.Guardrails
	money := func(v float64) string { return fmt.Sprintf("%.2f", v) }
	form := url.Values{"enabled": {"on"}, "floor_drop_pct": {money(g.FloorDropPct)}, "floor_cut_pct": {money(g.FloorCutPct)}, "ceiling_rise_pct": {money(g.CeilingRisePct)}, "ceiling_raise_pct": {money(g.CeilingRaisePct)}, "min_spending_pct": {money(g.MinSpendingPct)}, "max_spending_pct": {money(g.MaxSpendingPct)}, "min_monthly_spending_real": {money(g.MinMonthlySpendingReal)}}
	ordinary := httptest.NewRecorder()
	handleWhatIfGuardrails(ordinary, spendingPost(form))
	if ordinary.Code != 200 {
		t.Fatalf("ordinary save %d", ordinary.Code)
	}
	rm.InvalidateCache()
	saved, e = rm.Load()
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(saved.LivingSpendingBoost, c.LivingSpendingBoost) || saved.Guardrails.MinMonthlySpendingReal != 6000.25 {
		t.Fatal("ordinary form save lost absolute floor or fixed schedule")
	}
	saved.StartDate = "2027-10"
	if e := rm.Save(saved); e != nil {
		t.Fatal(e)
	}
	rm.InvalidateCache()
	advanced, e := rm.Load()
	if e != nil {
		t.Fatal(e)
	}
	if advanced.LivingSpendingBoost.StopMonth != "2027-09" || engine.LivingExpensesAtMonth(advanced, 0) > c.StartingMonthlyLivingReal-1000 {
		t.Fatal("advancing plan start moved or reactivated boost")
	}
}
