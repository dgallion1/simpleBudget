package whatif

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/analysis"
	"budget2/internal/services/retirement/engine"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func spendingPost(f url.Values) *http.Request {
	r := httptest.NewRequest("POST", "/whatif/spending/optimize", strings.NewReader(f.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}
func spendingFixture(t *testing.T) (*retirement.SettingsManager, *models.WhatIfSettings) {
	t.Helper()
	rm, cl := setupTestEnv(t)
	t.Cleanup(cl)
	s, e := rm.Load()
	if e != nil {
		t.Fatal(e)
	}
	s.UseCurrentMonth = false // fixtures pin a start date; see current-month migration
	s.StartDate = "2026-09"
	s.ProjectionYears = 12
	s.Persons[0].BirthMonth = models.BirthMonthForAge(s.StartDate, 65)
	s.MonthlyLivingExpenses = 8000
	s.PortfolioValue = 10000000
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: true, Phases: []models.SpendingPhase{{Name: "Active", StartAge: 0, Multiplier: 0.8}}}
	s.LivingSpendingBoost = &models.LivingSpendingBoost{MonthlyReal: 500, StopMonth: "2031-09"}
	s.Guardrails = &models.GuardrailConfig{Enabled: true, MinMonthlySpendingReal: 5000, MinSpendingPct: 50, MaxSpendingPct: 150, FloorDropPct: 20, FloorCutPct: 5, CeilingRisePct: 20, CeilingRaisePct: 5}
	if e := rm.Save(s); e != nil {
		t.Fatal(e)
	}
	rm.InvalidateCache() // Resolve legacy load migrations before preview identity is recorded.
	s, e = rm.Load()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		spendingPreviews.Lock()
		defer spendingPreviews.Unlock()
		for id, p := range spendingPreviews.entries {
			if p.manager == rm {
				p.cancel()
				delete(spendingPreviews.entries, id)
			}
		}
	})
	return rm, s
}
func spendingAccepted() models.SpendingCandidate {
	return models.SpendingCandidate{ID: "flexible-10000", Kind: "flexible", BaseMonthlyLivingExpenses: 10000, StartingMonthlyLivingReal: 9000, Qualifies: true, LivingSpendingBoost: &models.LivingSpendingBoost{MonthlyReal: 1000, StopMonth: "2036-09"}, Guardrails: &models.GuardrailConfig{Enabled: true, MinMonthlySpendingReal: 6000, MinSpendingPct: 0, MaxSpendingPct: 160, FloorDropPct: 15, FloorCutPct: 10, CeilingRisePct: 25, CeilingRaisePct: 5}, Metrics: &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: 9000}, SimulationYears: []models.GuardrailSimulationYear{{Year: 1, Paths: 1000, LivingReal: models.GuardrailPercentiles{P10: 8000, P50: 9000, P90: 10000}}}, WorstPathYears: []models.GuardrailPathYear{{Year: 1, LivingReal: 8000, LivingNominal: 8200}}, WorstPathIndex: 42}
}
func spendingSeed(t *testing.T, rm *retirement.SettingsManager, c models.SpendingCandidate) (string, *spendingPreview) {
	t.Helper()
	s, rev, e := rm.LoadContextWithRevision(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(s)
	_, cancel := context.WithCancel(context.Background())
	id := "spending-test-request"
	p := &spendingPreview{manager: rm, scenario: rm.ActiveFilename(), revision: rev, fingerprint: sha256.Sum256(raw), expires: time.Now().Add(time.Minute), cancel: cancel, request: models.SpendingOptimizerRequest{FloorMonthlyReal: 6000, NearTermYears: 5, Seed: 9223372036854775807}, result: &models.SpendingOptimizerResult{ValidationSeed: 9223372036854775807, ValidationRuns: 1000}, tokens: map[string]models.SpendingCandidate{"apply-token": c}, graphs: map[string]models.SpendingCandidate{"graph-token": c}}
	spendingPreviews.Lock()
	spendingPreviews.entries[id] = p
	spendingPreviews.Unlock()
	return id, p
}
func spendingBytes(t *testing.T, rm *retirement.SettingsManager) string {
	t.Helper()
	s, e := rm.Load()
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(s)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestSpendingApplyAtomicCompleteCandidate(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "boost", true: "remove"}[remove], func(t *testing.T) {
			rm, before := spendingFixture(t)
			c := spendingAccepted()
			if remove {
				c.LivingSpendingBoost = nil
				c.StartingMonthlyLivingReal = 8000
			}
			id, p := spendingSeed(t, rm, c)
			in, _, e := buildEngineInput(before)
			if e != nil {
				t.Fatal(e)
			}
			in.Hooks = retirement.DefaultHooks()
			pi, e := analysis.PrepareSpendingCandidate(in, c)
			if e != nil {
				t.Fatal(e)
			}
			preview := getEngine().Run(pi)
			w := httptest.NewRecorder()
			handleApplySpendingOptimizer(w, spendingPost(url.Values{"request_id": {id}, "recommendation": {"apply-token"}, "monthly_living_expenses": {"999999"}, "boost_monthly_real": {"99999"}, "min_monthly_spending_real": {"1"}}))
			if w.Code != 200 || !strings.Contains(w.Header().Get("HX-Redirect"), "#spending-optimizer") {
				t.Fatalf("apply %d %s", w.Code, w.Body.String())
			}
			got, rev, e := rm.LoadContextWithRevision(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			if rev != p.revision+1 || got.MonthlyLivingExpenses != 10000 || !reflect.DeepEqual(got.LivingSpendingBoost, c.LivingSpendingBoost) || !reflect.DeepEqual(got.Guardrails, c.Guardrails) {
				t.Fatalf("not atomic rev=%d", rev)
			}
			si, _, e := buildEngineInput(got)
			if e != nil {
				t.Fatal(e)
			}
			si.Hooks = retirement.DefaultHooks()
			saved := getEngine().Run(si)
			for _, idx := range []int{0, 119, 120} {
				if idx >= len(saved.Months) {
					t.Fatalf("short projection %d", len(saved.Months))
				}
				if !reflect.DeepEqual(saved.Months[idx], preview.Months[idx]) {
					t.Fatalf("preview/apply month %d differs", idx)
				}
			}
			got.MonthlyLivingExpenses = before.MonthlyLivingExpenses
			got.LivingSpendingBoost = before.LivingSpendingBoost
			got.Guardrails = before.Guardrails
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(before)
			if string(a) != string(b) {
				t.Fatal("unrelated settings changed")
			}
			w = httptest.NewRecorder()
			handleApplySpendingOptimizer(w, spendingPost(url.Values{"request_id": {id}, "recommendation": {"apply-token"}}))
			if w.Code != 409 {
				t.Fatal("replay accepted")
			}
		})
	}
}
func TestSpendingApplyRejections(t *testing.T) {
	for _, mode := range []string{"expired", "cancelled", "revision", "fingerprint", "scenario", "manager", "unknown", "baseline", "failed", "missing-policy"} {
		t.Run(mode, func(t *testing.T) {
			rm, _ := spendingFixture(t)
			c := spendingAccepted()
			id, p := spendingSeed(t, rm, c)
			token := "apply-token"
			switch mode {
			case "expired":
				p.expires = time.Now().Add(-time.Minute)
			case "cancelled":
				handleCancelSpendingOptimizer(httptest.NewRecorder(), spendingPost(url.Values{"request_id": {id}}))
			case "revision":
				s, _ := rm.Load()
				s.DiscountRate++
				if e := rm.Save(s); e != nil {
					t.Fatal(e)
				}
			case "fingerprint":
				s, _ := rm.Load()
				s.DiscountRate++
				if e := rm.Save(s); e != nil {
					t.Fatal(e)
				}
				_, p.revision, _ = rm.LoadContextWithRevision(context.Background())
			case "scenario":
				if _, e := rm.CreateScenario("Other"); e != nil {
					t.Fatal(e)
				}
			case "manager":
				p.manager = retirement.NewSettingsManager(t.TempDir(), nil)
			case "unknown":
				token = "graph-token"
			case "baseline":
				c.Baseline = true
				p.tokens[token] = c
			case "failed":
				c.Qualifies = false
				p.tokens[token] = c
			case "missing-policy":
				c.Guardrails = nil
				p.tokens[token] = c
			}
			before := spendingBytes(t, rm)
			w := httptest.NewRecorder()
			handleApplySpendingOptimizer(w, spendingPost(url.Values{"request_id": {id}, "recommendation": {token}}))
			if w.Code != 409 {
				t.Fatalf("status %d %s", w.Code, w.Body.String())
			}
			if before != spendingBytes(t, rm) {
				t.Fatal("rejected apply saved")
			}
		})
	}
}
func TestSpendingOptimizerInvalidInput(t *testing.T) {
	for _, tc := range []struct{ k, v string }{{"floor_monthly_real", "NaN"}, {"floor_monthly_real", "0"}, {"search_min_monthly_real", "0"}, {"search_max_monthly_real", "-1"}, {"search_step_monthly_real", "+Inf"}, {"near_term_years", "0"}, {"seed", "9223372036854775808"}} {
		t.Run(tc.k+tc.v, func(t *testing.T) {
			rm, _ := spendingFixture(t)
			before := spendingBytes(t, rm)
			f := url.Values{"request_id": {"invalid-test"}, "floor_monthly_real": {"6000"}}
			f.Set(tc.k, tc.v)
			w := httptest.NewRecorder()
			handleSpendingOptimizerWithRunner(w, spendingPost(f), func(context.Context, engine.Input, models.SpendingOptimizerRequest) (*models.SpendingOptimizerResult, error) {
				t.Fatal("invalid form reached runner")
				return nil, nil
			})
			if w.Code != 400 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if before != spendingBytes(t, rm) {
				t.Fatal("invalid search saved")
			}
		})
	}
}
func TestSpendingOptimizerErrorStatuses(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{analysis.ErrInvalidSpendingRequest, 400}, {errors.New("runner failed"), 500}, {context.Canceled, 409}, {context.DeadlineExceeded, 409}} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			rm, _ := spendingFixture(t)
			before := spendingBytes(t, rm)
			w := httptest.NewRecorder()
			handleSpendingOptimizerWithRunner(w, spendingPost(url.Values{"request_id": {"error-test"}, "floor_monthly_real": {"6000"}}), func(context.Context, engine.Input, models.SpendingOptimizerRequest) (*models.SpendingOptimizerResult, error) {
				return nil, tc.err
			})
			if w.Code != tc.status {
				t.Fatalf("status %d want %d", w.Code, tc.status)
			}
			if before != spendingBytes(t, rm) {
				t.Fatal("error saved")
			}
		})
	}
}

func spendingResult(s *models.WhatIfSettings, req models.SpendingOptimizerRequest) *models.SpendingOptimizerResult {
	accepted := spendingAccepted()
	second := spendingAccepted()
	second.ID = "non-headline"
	second.BaseMonthlyLivingExpenses = 9000
	second.StartingMonthlyLivingReal = 8200
	failed := spendingAccepted()
	failed.ID = "failed"
	failed.Qualifies = false
	failed.Metrics = &models.SpendingRiskMetrics{Runs: 1000, FloorShortfallPaths: 1}
	baseline := spendingAccepted()
	baseline.ID = "current"
	baseline.Kind = "current"
	baseline.Baseline = true
	baseline.BaseMonthlyLivingExpenses = s.MonthlyLivingExpenses
	baseline.StartingMonthlyLivingReal = 6900
	baseline.Metrics = &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: 6900}
	baseline.LivingSpendingBoost = models.CloneLivingSpendingBoost(s.LivingSpendingBoost)
	return &models.SpendingOptimizerResult{Request: req, SearchSeed: req.Seed, SelectionSeed: -9223372036854775807, ValidationSeed: 9223372036854775807, SearchRuns: 32, SelectionRuns: 1000, ValidationRuns: 1000, Candidates: []models.SpendingCandidate{second, accepted, failed, baseline}, RecommendationIDs: []string{accepted.ID}}
}
func TestSpendingOptimizerPreviewTokensHooksAndStrings(t *testing.T) {
	rm, s := spendingFixture(t)
	before := spendingBytes(t, rm)
	id := "complete-preview"
	f := url.Values{"request_id": {id}, "floor_monthly_real": {"6000"}, "seed": {"9223372036854775807"}, "search_step_monthly_real": {""}}
	w := httptest.NewRecorder()
	handleSpendingOptimizerWithRunner(w, spendingPost(f), func(ctx context.Context, in engine.Input, req models.SpendingOptimizerRequest) (*models.SpendingOptimizerResult, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Minute || time.Until(deadline) < 179*time.Second {
			t.Fatal("missing three-minute bound")
		}
		if reflect.ValueOf(in.Hooks).IsZero() {
			t.Fatal("request hooks missing")
		}
		if req.LivingSpendingBoost != nil {
			t.Fatal("disabled boost reused saved schedule")
		}
		if req.SearchStepMonthlyReal != 0 {
			t.Fatal("blank did not select default")
		}
		req.NearTermYears = 5
		return spendingResult(s, req), nil
	})
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var view spendingOptimizerView
	if e := json.Unmarshal(w.Body.Bytes(), &view); e != nil {
		t.Fatal(e)
	}
	if view.Request.Seed != 9223372036854775807 || len(view.Rows) != 4 || len(view.FlexibleRows) != 3 || len(view.Headlines) != 1 || view.Current == nil {
		t.Fatalf("bad view %+v", view)
	}
	if !strings.Contains(w.Body.String(), `"seed":"9223372036854775807"`) || !strings.Contains(w.Body.String(), `"validation_seed":"9223372036854775807"`) {
		t.Fatal("seed precision lost")
	}
	seen := map[string]bool{}
	for _, row := range view.Rows {
		if len(row.GraphToken) != 64 || seen[row.GraphToken] {
			t.Fatal("graph token not unique opaque")
		}
		seen[row.GraphToken] = true
		if spendingCandidateCanApply(row.Candidate) {
			if len(row.Token) != 64 || seen[row.Token] {
				t.Fatal("qualifying frontier missing independent Apply")
			}
			seen[row.Token] = true
		} else if row.Token != "" {
			t.Fatal("baseline/failure authorized Apply")
		}
	}
	if before != spendingBytes(t, rm) {
		t.Fatal("preview saved")
	}
}
func TestSpendingOptimizerCancellationDoesNotDeleteNewSearch(t *testing.T) {
	rm, s := spendingFixture(t)
	before := spendingBytes(t, rm)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	id := "same-request-id"
	oldW := httptest.NewRecorder()
	form := url.Values{"request_id": {id}, "floor_monthly_real": {"6000"}}
	go func() {
		defer close(done)
		handleSpendingOptimizerWithRunner(oldW, spendingPost(form), func(ctx context.Context, in engine.Input, req models.SpendingOptimizerRequest) (*models.SpendingOptimizerResult, error) {
			close(started)
			<-release
			return spendingResult(s, req), nil
		})
	}()
	<-started
	busy := httptest.NewRecorder()
	handleSpendingOptimizerWithRunner(busy, spendingPost(url.Values{"request_id": {"different-request"}, "floor_monthly_real": {"6000"}}), func(context.Context, engine.Input, models.SpendingOptimizerRequest) (*models.SpendingOptimizerResult, error) {
		t.Error("concurrent search ran")
		return nil, nil
	})
	if busy.Code != 409 {
		t.Fatalf("busy %d", busy.Code)
	}
	handleCancelSpendingOptimizer(httptest.NewRecorder(), spendingPost(url.Values{"request_id": {id}}))
	freshW := httptest.NewRecorder()
	handleSpendingOptimizerWithRunner(freshW, spendingPost(form), func(ctx context.Context, in engine.Input, req models.SpendingOptimizerRequest) (*models.SpendingOptimizerResult, error) {
		return spendingResult(s, req), nil
	})
	close(release)
	<-done
	if oldW.Code != 409 || freshW.Code != 200 {
		t.Fatalf("old/new %d/%d", oldW.Code, freshW.Code)
	}
	spendingPreviews.Lock()
	fresh := spendingPreviews.entries[id]
	spendingPreviews.Unlock()
	if fresh == nil || len(fresh.tokens) != 2 {
		t.Fatal("old search deleted/replaced fresh preview")
	}
	if before != spendingBytes(t, rm) {
		t.Fatal("cancel/search saved")
	}
}
func TestSpendingApplyConcurrentAndSaveWindowGuards(t *testing.T) {
	for _, mode := range []string{"switch", "edit", "concurrent"} {
		t.Run(mode, func(t *testing.T) {
			rm, _ := spendingFixture(t)
			other := ""
			if mode == "switch" {
				if _, e := rm.CreateScenario("Other"); e != nil {
					t.Fatal(e)
				}
				other = rm.ActiveFilename()
				if e := rm.SwitchScenario("whatif.json"); e != nil {
					t.Fatal(e)
				}
			}
			id, p := spendingSeed(t, rm, spendingAccepted())
			before := spendingBytes(t, rm)
			w := httptest.NewRecorder()
			form := url.Values{"request_id": {id}, "recommendation": {"apply-token"}}
			if mode == "concurrent" {
				done := make(chan int, 2)
				for i := 0; i < 2; i++ {
					go func() {
						w := httptest.NewRecorder()
						handleApplySpendingOptimizer(w, spendingPost(form))
						done <- w.Code
					}()
				}
				a, b := <-done, <-done
				if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
					t.Fatalf("statuses %d %d", a, b)
				}
				_, rev, _ := rm.LoadContextWithRevision(context.Background())
				if rev != p.revision+1 {
					t.Fatal("multiple revisions")
				}
				return
			}
			var expected string
			handleApplySpendingOptimizerWithHook(w, spendingPost(form), func() {
				if mode == "switch" {
					if e := rm.SwitchScenario(other); e != nil {
						t.Fatal(e)
					}
				} else {
					s, _ := rm.Load()
					s.DiscountRate = 9.99
					if e := rm.Save(s); e != nil {
						t.Fatal(e)
					}
				}
				expected = spendingBytes(t, rm)
			})
			if w.Code != 409 {
				t.Fatalf("%s accepted: %d", mode, w.Code)
			}
			if spendingBytes(t, rm) != expected {
				t.Fatal("overwrote concurrent settings")
			}
			if mode == "switch" {
				if e := rm.SwitchScenario("whatif.json"); e != nil {
					t.Fatal(e)
				}
				if before != spendingBytes(t, rm) {
					t.Fatal("original scenario changed")
				}
			}
		})
	}
}
func TestSpendingOptimizerPrepareReadOnlyDefaults(t *testing.T) {
	rm, _ := spendingFixture(t)
	before := spendingBytes(t, rm)
	spendingPreviews.Lock()
	count := len(spendingPreviews.entries)
	spendingPreviews.Unlock()
	w := httptest.NewRecorder()
	handlePrepareSpendingOptimizer(w, spendingPost(url.Values{"floor_monthly_real": {"6000"}, "search_step_monthly_real": {""}, "seed": {"9223372036854775807"}}))
	if w.Code != 200 {
		t.Fatalf("prepare %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Request                    models.SpendingOptimizerRequest `json:"request"`
		CurrentBaseMonthlyReal     float64                         `json:"current_base_monthly_real"`
		CurrentStartingMonthlyReal float64                         `json:"current_starting_monthly_real"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &got); e != nil {
		t.Fatal(e)
	}
	if got.Request.SearchMinMonthlyReal != 6000 || got.Request.SearchMaxMonthlyReal != 13800 || got.Request.SearchStepMonthlyReal != 100 || got.Request.NearTermYears != 5 || got.Request.Seed != 9223372036854775807 || got.CurrentBaseMonthlyReal != 8000 || got.CurrentStartingMonthlyReal != 6900 {
		t.Fatalf("prepare response %+v", got)
	}
	spendingPreviews.Lock()
	afterCount := len(spendingPreviews.entries)
	spendingPreviews.Unlock()
	if count != afterCount || before != spendingBytes(t, rm) {
		t.Fatal("prepare mutated registry/settings")
	}
	w = httptest.NewRecorder()
	handlePrepareSpendingOptimizer(w, spendingPost(url.Values{"floor_monthly_real": {"6000"}, "search_max_monthly_real": {"100000"}}))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if e := json.Unmarshal(w.Body.Bytes(), &got); e != nil {
		t.Fatal(e)
	}
	if got.Request.SearchStepMonthlyReal != 500 || got.Request.Seed != 0 {
		t.Fatalf("coarse default/seed %+v", got.Request)
	}
}
func TestSpendingOptimizerChainAndCapacity(t *testing.T) {
	rm, s := spendingFixture(t)
	if _, e := rm.CreateScenario("Other"); e != nil {
		t.Fatal(e)
	}
	if e := rm.SwitchScenario("whatif.json"); e != nil {
		t.Fatal(e)
	}
	s.ScenarioChain = []models.ScenarioChainLink{{ScenarioFilename: "whatif_other.json", TransitionAge: 70}}
	if e := rm.Save(s); e != nil {
		t.Fatal(e)
	}
	before := spendingBytes(t, rm)
	w := httptest.NewRecorder()
	handleSpendingOptimizer(w, spendingPost(url.Values{"request_id": {"chained-test"}, "floor_monthly_real": {"6000"}}))
	if w.Code != 400 || before != spendingBytes(t, rm) {
		t.Fatal("chain accepted/mutated")
	}
	s.ScenarioChain = nil
	if e := rm.Save(s); e != nil {
		t.Fatal(e)
	}
	spendingPreviews.Lock()
	saved := spendingPreviews.entries
	spendingPreviews.entries = make(map[string]*spendingPreview)
	for i := 0; i < 128; i++ {
		spendingPreviews.entries[string(rune(i))] = &spendingPreview{manager: rm, expires: time.Now().Add(time.Minute), cancel: func() {}}
	}
	spendingPreviews.Unlock()
	defer func() { spendingPreviews.Lock(); spendingPreviews.entries = saved; spendingPreviews.Unlock() }()
	w = httptest.NewRecorder()
	handleSpendingOptimizerWithRunner(w, spendingPost(url.Values{"request_id": {"capacity-test"}, "floor_monthly_real": {"6000"}}), func(context.Context, engine.Input, models.SpendingOptimizerRequest) (*models.SpendingOptimizerResult, error) {
		t.Fatal("capacity allowed search")
		return nil, nil
	})
	if w.Code != 409 {
		t.Fatalf("capacity %d", w.Code)
	}
	spendingPreviews.Lock()
	for _, p := range spendingPreviews.entries {
		p.expires = time.Now().Add(-time.Minute)
	}
	spendingPreviews.Unlock()
	w = httptest.NewRecorder()
	handleSpendingOptimizerWithRunner(w, spendingPost(url.Values{"request_id": {"capacity-test"}, "floor_monthly_real": {"6000"}}), func(ctx context.Context, in engine.Input, req models.SpendingOptimizerRequest) (*models.SpendingOptimizerResult, error) {
		return spendingResult(s, req), nil
	})
	if w.Code != 200 {
		t.Fatalf("expiry cleanup %d", w.Code)
	}
}
func TestSpendingOptimizerDisplayedComparisonCents(t *testing.T) {
	result := spendingResult(models.DefaultWhatIfSettings(), models.SpendingOptimizerRequest{})
	result.Candidates[3].Metrics.MedianNearTermMonthlyReal = 8000.004
	result.Candidates[0].Metrics.MedianNearTermMonthlyReal = 9000.006
	view, _, _, e := buildSpendingOptimizerView("test", result)
	if e != nil {
		t.Fatal(e)
	}
	if view.Rows[0].NearTermComparison != "$1,000.01 more per month than the current plan" {
		t.Fatal(view.Rows[0].NearTermComparison)
	}
}

func TestSpendingOptimizerComparisonUnavailableAndRoundedZero(t *testing.T) {
	result := spendingResult(models.DefaultWhatIfSettings(), models.SpendingOptimizerRequest{})
	result.Candidates[3].Metrics = nil
	view, _, _, e := buildSpendingOptimizerView("test", result)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(view.Rows[0].NearTermComparison, "more") {
		t.Fatal("missing baseline claims more")
	}
	result.Candidates[3].Metrics = &models.SpendingRiskMetrics{MedianNearTermMonthlyReal: 8000.001}
	result.Candidates[0].Metrics.MedianNearTermMonthlyReal = 8000.004
	view, _, _, e = buildSpendingOptimizerView("test", result)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(view.Rows[0].NearTermComparison, "more") {
		t.Fatal("displayed equal values claim more")
	}
}

func TestSpendingOptimizerRealPreviewApply(t *testing.T) {
	rm, s := spendingFixture(t)
	s.ProjectionYears = 1
	if e := rm.Save(s); e != nil {
		t.Fatal(e)
	}
	before := spendingBytes(t, rm)
	w := httptest.NewRecorder()
	handleSpendingOptimizer(w, spendingPost(url.Values{"request_id": {"real-spending-preview"}, "floor_monthly_real": {"6000"}, "search_min_monthly_real": {"8000"}, "search_max_monthly_real": {"8100"}, "search_step_monthly_real": {"100"}, "seed": {"12345"}, "boost_enabled": {"on"}, "boost_monthly_real": {"1000"}, "boost_stop_month": {"2027-09"}}))
	if w.Code != 200 {
		t.Fatalf("real preview %d %s", w.Code, w.Body.String())
	}
	if before != spendingBytes(t, rm) {
		t.Fatal("search saved")
	}
	var view spendingOptimizerView
	if e := json.Unmarshal(w.Body.Bytes(), &view); e != nil {
		t.Fatal(e)
	}
	if len(view.Rows) != 5 || view.Optimizer.ValidationRuns != 1000 || view.Optimizer.Request.NearTermYears != 5 {
		t.Fatalf("unexpected frontier %+v", view.Optimizer)
	}
	var chosen spendingOptimizerRow
	for _, row := range view.Rows {
		if len(row.Candidate.SimulationYears) == 0 || len(row.Candidate.WorstPathYears) == 0 {
			t.Fatal("missing retained annual evidence")
		}
		if row.Candidate.Baseline {
			if !reflect.DeepEqual(row.Candidate.LivingSpendingBoost, s.LivingSpendingBoost) {
				t.Fatal("baseline schedule lost")
			}
		} else if row.Token != "" && !row.Headline {
			chosen = row
		}
	}
	if chosen.Token == "" {
		t.Fatal("missing qualifying non-headline Apply")
	}
	w = httptest.NewRecorder()
	handleApplySpendingOptimizer(w, spendingPost(url.Values{"request_id": {view.RequestID}, "recommendation": {chosen.Token}}))
	if w.Code != 200 {
		t.Fatalf("apply %d %s", w.Code, w.Body.String())
	}
	got, _ := rm.Load()
	if got.MonthlyLivingExpenses != chosen.Candidate.BaseMonthlyLivingExpenses || !reflect.DeepEqual(got.LivingSpendingBoost, chosen.Candidate.LivingSpendingBoost) || !reflect.DeepEqual(got.Guardrails, chosen.Candidate.Guardrails) {
		t.Fatal("real applied candidate changed")
	}
}
func TestSpendingApplyRetryableSaveFailure(t *testing.T) {
	rm, _ := spendingFixture(t)
	id, p := spendingSeed(t, rm, spendingAccepted())
	before := spendingBytes(t, rm)
	dir := rm.SettingsDir()
	if e := os.Chmod(dir, 0500); e != nil {
		t.Fatal(e)
	}
	defer os.Chmod(dir, 0700)
	w := httptest.NewRecorder()
	handleApplySpendingOptimizer(w, spendingPost(url.Values{"request_id": {id}, "recommendation": {"apply-token"}}))
	if w.Code != 500 {
		t.Fatalf("save failure %d %s", w.Code, w.Body.String())
	}
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if before != spendingBytes(t, rm) {
		t.Fatal("failed save changed settings")
	}
	spendingPreviews.Lock()
	retained := spendingPreviews.entries[id]
	spendingPreviews.Unlock()
	if retained != p {
		t.Fatal("retryable preview consumed")
	}
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	w = httptest.NewRecorder()
	handleApplySpendingOptimizer(w, spendingPost(url.Values{"request_id": {id}, "recommendation": {"apply-token"}}))
	if w.Code != 200 {
		t.Fatalf("retry %d %s", w.Code, w.Body.String())
	}
}

func TestSpendingApplyStalePreviewConsumed(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-load", true: "save-window"}[during], func(t *testing.T) {
			rm, _ := spendingFixture(t)
			id, _ := spendingSeed(t, rm, spendingAccepted())
			change := func() {
				s, _ := rm.Load()
				s.DiscountRate++
				if e := rm.Save(s); e != nil {
					t.Fatal(e)
				}
			}
			var hook func()
			if during {
				hook = change
			} else {
				change()
			}
			w := httptest.NewRecorder()
			handleApplySpendingOptimizerWithHook(w, spendingPost(url.Values{"request_id": {id}, "recommendation": {"apply-token"}}), hook)
			if w.Code != 409 {
				t.Fatalf("status %d", w.Code)
			}
			spendingPreviews.Lock()
			p := spendingPreviews.entries[id]
			spendingPreviews.Unlock()
			if p != nil {
				t.Fatal("nonretryable stale preview retained")
			}
		})
	}
}

// A real ResponseWriter may stop consuming bytes indefinitely. Signal only once
// delivery reaches Write, then keep that request blocked until the test releases it.
type spendingBlockingWriter struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	release chan struct{}
}

func (w *spendingBlockingWriter) Write(body []byte) (int, error) {
	close(w.entered)
	<-w.release
	return w.ResponseRecorder.Write(body)
}
func spendingSeedNamed(t *testing.T, rm *retirement.SettingsManager, id string) *spendingPreview {
	t.Helper()
	original, p := spendingSeed(t, rm, spendingAccepted())
	spendingPreviews.Lock()
	delete(spendingPreviews.entries, original)
	spendingPreviews.entries[id] = p
	spendingPreviews.Unlock()
	return p
}

func TestSpendingOptimizerBlockedDeliveryAllowsOtherPreviews(t *testing.T) {
	for _, kind := range []string{"search", "graph"} {
		for _, stale := range []bool{false, true} {
			name := kind + "-success"
			if stale {
				name = kind + "-stale-error"
			}
			t.Run(name, func(t *testing.T) {
				rm, s := spendingFixture(t)
				spendingSeedNamed(t, rm, "unrelated-cancel")
				applyPreview := spendingSeedNamed(t, rm, "unrelated-apply")
				const deliveryID = "blocked-delivery"
				if kind == "graph" {
					spendingSeedNamed(t, rm, deliveryID)
				}
				invalidate := func() {
					if stale {
						spendingPreviews.Lock()
						delete(spendingPreviews.entries, deliveryID)
						spendingPreviews.Unlock()
					}
				}
				// Removing this request's entry without canceling its context reproduces
				// the final pointer-identity rejection, after all earlier checks pass.
				writer := &spendingBlockingWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: make(chan struct{})}
				deliveryDone := make(chan struct{})
				go func() {
					defer close(deliveryDone)
					if kind == "search" {
						handleSpendingOptimizerWithRunner(writer, spendingPost(url.Values{"request_id": {deliveryID}, "floor_monthly_real": {"6000"}}), func(ctx context.Context, in engine.Input, req models.SpendingOptimizerRequest) (*models.SpendingOptimizerResult, error) {
							invalidate()
							return spendingResult(s, req), nil
						})
					} else {
						handleSpendingOptimizerGraphWithRunner(writer, spendingPost(url.Values{"request_id": {deliveryID}, "candidate": {"graph-token"}}), func(in engine.Input) *models.ProjectionResult { invalidate(); return getEngine().Run(in) })
					}
				}()
				select {
				case <-writer.entered:
				case <-time.After(3 * time.Second):
					close(writer.release)
					<-deliveryDone
					t.Fatal("response never reached blocked Write")
				}
				cancelW, applyW := httptest.NewRecorder(), httptest.NewRecorder()
				cancelDone, applyDone := make(chan struct{}), make(chan struct{})
				go func() {
					defer close(cancelDone)
					handleCancelSpendingOptimizer(cancelW, spendingPost(url.Values{"request_id": {"unrelated-cancel"}}))
				}()
				go func() {
					defer close(applyDone)
					handleApplySpendingOptimizer(applyW, spendingPost(url.Values{"request_id": {"unrelated-apply"}, "recommendation": {"apply-token"}}))
				}()
				// Always release the blocked client and join every goroutine, including RED.
				defer func() {
					close(writer.release)
					<-deliveryDone
					<-cancelDone
					<-applyDone
					want := 200
					if stale {
						want = 409
					}
					if writer.Code != want {
						t.Errorf("delivery status %d want %d", writer.Code, want)
					}
					if stale && strings.Contains(writer.Body.String(), `"Rows"`) {
						t.Error("stale result published")
					}
				}()
				for _, operation := range []struct {
					name string
					done <-chan struct{}
				}{{"cancellation", cancelDone}, {"Apply", applyDone}} {
					select {
					case <-operation.done:
					case <-time.After(time.Second):
						t.Errorf("unrelated %s blocked behind %s response delivery", operation.name, name)
						return
					}
				}
				if cancelW.Code != 204 || applyW.Code != 200 {
					t.Fatalf("unrelated cancel/Apply statuses %d/%d", cancelW.Code, applyW.Code)
				}
				got, rev, err := rm.LoadContextWithRevision(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if got.MonthlyLivingExpenses != 10000 || rev != applyPreview.revision+1 {
					t.Fatal("unrelated Apply did not complete during blocked delivery")
				}
				spendingPreviews.Lock()
				cancelled := spendingPreviews.entries["unrelated-cancel"] == nil
				spendingPreviews.Unlock()
				if !cancelled {
					t.Fatal("unrelated cancellation did not complete during blocked delivery")
				}
			})
		}
	}
}

func TestSpendingOptimizerFormMinimumRequiresSavedFloor(t *testing.T) {
	for _, tc := range []struct {
		name       string
		guardrails *models.GuardrailConfig
		want       any
	}{
		{name: "no-policy"},
		{name: "no-floor", guardrails: &models.GuardrailConfig{Enabled: true}},
		{name: "saved-floor", guardrails: &models.GuardrailConfig{Enabled: true, MinMonthlySpendingReal: 6250.25}, want: 6250.25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := models.DefaultWhatIfSettings()
			s.MonthlyLivingExpenses = 8000
			s.Guardrails = tc.guardrails
			form := spendingOptimizerFormData(s, nil)
			if !reflect.DeepEqual(form["FloorMonthlyReal"], tc.want) {
				t.Fatalf("floor %#v want %#v", form["FloorMonthlyReal"], tc.want)
			}
			if form["CurrentBaseMonthlyReal"] != 8000.0 {
				t.Fatal("base amount lost")
			}
			encoded, err := json.Marshal(form)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == nil && !strings.Contains(string(encoded), `"FloorMonthlyReal":null`) {
				t.Fatalf("missing minimum must serialize as null: %s", encoded)
			}
		})
	}
}
