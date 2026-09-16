package whatif

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"budget2/internal/models"
)

// CP1 (2026-09-14): the current plan card is applyable under the same
// qualification rule as every other card; its rules may be disabled or nil.
func TestSpendingCandidateCanApplyRule(t *testing.T) {
	enabled := &models.GuardrailConfig{Enabled: true}
	disabled := &models.GuardrailConfig{Enabled: false}
	for _, tc := range []struct {
		name string
		c    models.SpendingCandidate
		want bool
	}{
		{"current qualifies enabled rules", models.SpendingCandidate{Kind: "current", Baseline: true, Qualifies: true, Guardrails: enabled}, true},
		{"current qualifies disabled rules", models.SpendingCandidate{Kind: "current", Baseline: true, Qualifies: true, Guardrails: disabled}, true},
		{"current qualifies nil rules", models.SpendingCandidate{Kind: "current", Baseline: true, Qualifies: true}, true},
		{"current does not qualify", models.SpendingCandidate{Kind: "current", Baseline: true, Qualifies: false, Guardrails: enabled}, false},
		{"flexible qualifies", models.SpendingCandidate{Kind: "flexible", Qualifies: true, Guardrails: enabled}, true},
		{"planned qualifies", models.SpendingCandidate{Kind: "planned", Qualifies: true, Guardrails: enabled}, true},
		{"flexible does not qualify", models.SpendingCandidate{Kind: "flexible", Qualifies: false, Guardrails: enabled}, false},
		{"flexible disabled rules", models.SpendingCandidate{Kind: "flexible", Qualifies: true, Guardrails: disabled}, false},
		{"flexible nil rules", models.SpendingCandidate{Kind: "flexible", Qualifies: true}, false},
		{"non-current baseline", models.SpendingCandidate{Kind: "flexible", Baseline: true, Qualifies: true, Guardrails: enabled}, false},
	} {
		if got := spendingCandidateCanApply(tc.c); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

// spendingCurrentCandidate mirrors analysis.spendingCandidateForStart's
// "current" branch for the saved settings: the same base budget, boost and
// rules (nil stays nil), flagged as the qualifying baseline.
func spendingCurrentCandidate(s *models.WhatIfSettings) models.SpendingCandidate {
	var rules *models.GuardrailConfig
	if s.Guardrails != nil {
		cfg := *s.Guardrails
		rules = &cfg
	}
	return models.SpendingCandidate{
		ID: "current", Kind: "current", Baseline: true, Qualifies: true,
		BaseMonthlyLivingExpenses: s.MonthlyLivingExpenses,
		StartingMonthlyLivingReal: s.MonthlyLivingExpenses,
		LivingSpendingBoost:       models.CloneLivingSpendingBoost(s.LivingSpendingBoost),
		Guardrails:                rules,
		Metrics:                   &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: s.MonthlyLivingExpenses},
	}
}

func TestSpendingApplyCurrentPlan(t *testing.T) {
	for _, mode := range []string{"rules", "disabled-rules", "nil-rules"} {
		t.Run(mode, func(t *testing.T) {
			rm, _ := spendingFixture(t)
			if mode != "rules" {
				s, e := rm.Load()
				if e != nil {
					t.Fatal(e)
				}
				if mode == "nil-rules" {
					s.Guardrails = nil
				} else {
					s.Guardrails.Enabled = false
				}
				if e := rm.Save(s); e != nil {
					t.Fatal(e)
				}
				rm.InvalidateCache()
			}
			before, e := rm.Load()
			if e != nil {
				t.Fatal(e)
			}
			if (before.Guardrails == nil) != (mode == "nil-rules") {
				t.Fatalf("fixture rules %v for %s", before.Guardrails, mode)
			}
			c := spendingCurrentCandidate(before)
			id, p := spendingSeed(t, rm, c)
			w := httptest.NewRecorder()
			handleApplySpendingOptimizer(w, spendingPost(url.Values{"request_id": {id}, "recommendation": {"apply-token"}}))
			if w.Code != 200 || !strings.Contains(w.Header().Get("HX-Redirect"), "spending_applied=") || !strings.Contains(w.Header().Get("HX-Redirect"), "#spending-optimizer") {
				t.Fatalf("apply %d %q %s", w.Code, w.Header().Get("HX-Redirect"), w.Body.String())
			}
			got, rev, e := rm.LoadContextWithRevision(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			if rev != p.revision+1 {
				t.Fatalf("revision %d want %d", rev, p.revision+1)
			}
			if got.SpendingSearch == nil {
				t.Fatal("search preferences not saved")
			}
			ev := got.AppliedSpendingEvidence
			if ev == nil || ev.Candidate.Kind != "current" || !ev.Candidate.Baseline || !appliedSpendingEvidenceFresh(got) {
				t.Fatalf("applied evidence %+v", ev)
			}
			// Everything except the two recorded fields is byte-identical to
			// the pre-apply plan: the current plan re-saves itself.
			got.SpendingSearch, before.SpendingSearch = nil, nil
			got.AppliedSpendingEvidence, before.AppliedSpendingEvidence = nil, nil
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(before)
			if string(a) != string(b) {
				t.Fatalf("current-plan apply changed the plan:\n%s\n%s", a, b)
			}
			if mode == "nil-rules" && got.Guardrails != nil {
				t.Fatal("nil rules were written")
			}
			w = httptest.NewRecorder()
			handleApplySpendingOptimizer(w, spendingPost(url.Values{"request_id": {id}, "recommendation": {"apply-token"}}))
			if w.Code != 409 {
				t.Fatal("replay accepted")
			}
		})
	}
}

// The Current plan card offers Apply exactly when the current plan
// qualifies; the frontier tables never list it, so the only apply control
// with its id is the one in its card.
func TestSpendingOptimizerRenderCurrentPlanApply(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	for _, qualifies := range []bool{true, false} {
		current := models.SpendingCandidate{ID: "current", Kind: "current", Baseline: true, Qualifies: qualifies, BaseMonthlyLivingExpenses: 8000, StartingMonthlyLivingReal: 8500, Metrics: &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: 8000}}
		flexible := models.SpendingCandidate{ID: "flex-9000", Kind: "flexible", Qualifies: true, BaseMonthlyLivingExpenses: 9000, StartingMonthlyLivingReal: 9500, Guardrails: &models.GuardrailConfig{Enabled: true, MinSpendingPct: 100, MaxSpendingPct: 100, MinMonthlySpendingReal: 7000}, Metrics: &models.SpendingRiskMetrics{Runs: 1000, MedianNearTermMonthlyReal: 9000}}
		result := &models.SpendingOptimizerResult{Request: models.SpendingOptimizerRequest{FloorMonthlyReal: 7000, NearTermYears: 5}, SelectionRuns: 1000, ValidationRuns: 1000, Candidates: []models.SpendingCandidate{current, flexible}, RecommendationIDs: []string{"flex-9000"}}
		view, _, tokens, err := buildSpendingOptimizerView("current-apply-fixture", result)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		if err := renderer.RenderPartial(w, "whatif-spending-optimizer-results", view); err != nil {
			t.Fatal(err)
		}
		body := w.Body.String()
		if got := strings.Count(body, `data-spending-apply="current"`); got != map[bool]int{true: 1, false: 0}[qualifies] {
			t.Errorf("qualifies=%v: current apply controls = %d", qualifies, got)
		}
		if got := strings.Count(body, `data-spending-apply="flex-9000"`); got != 2 {
			t.Errorf("qualifies=%v: flexible apply controls = %d (card + frontier)", qualifies, got)
		}
		if got := len(tokens); got != map[bool]int{true: 2, false: 1}[qualifies] {
			t.Errorf("qualifies=%v: apply tokens = %d", qualifies, got)
		}
		if view.Current == nil || (view.Current.Token != "") != qualifies {
			t.Errorf("qualifies=%v: current row token %q", qualifies, view.Current.Token)
		}
		if strings.Contains(body, "current-plan rows remain available") || !strings.Contains(body, "Failed rows remain available for graph inspection only.") {
			t.Error("inspect-only copy not updated")
		}
	}
}

// RC1 (2026-09-16): the comparison minimum the user ran the optimizer with
// is saved as a search preference on Apply and prefills the form on reload,
// regardless of which candidate was applied. Applying the current plan
// keeps the plan's own rules (policy floor 5000 in the fixture, or none),
// so the policy floor cannot stand in for the entered minimum.
func TestSpendingApplyKeepsComparisonMinimum(t *testing.T) {
	for _, mode := range []string{"current-with-rules", "current-nil-rules", "searched"} {
		t.Run(mode, func(t *testing.T) {
			rm, _ := spendingFixture(t)
			if mode == "current-nil-rules" {
				s, e := rm.Load()
				if e != nil {
					t.Fatal(e)
				}
				s.Guardrails = nil
				if e := rm.Save(s); e != nil {
					t.Fatal(e)
				}
				rm.InvalidateCache()
			}
			before, e := rm.Load()
			if e != nil {
				t.Fatal(e)
			}
			c := spendingAccepted()
			if mode != "searched" {
				c = spendingCurrentCandidate(before)
			}
			id, p := spendingSeed(t, rm, c)
			p.request.FloorMonthlyReal, p.form.FloorMonthlyReal = 7000, 7000
			w := httptest.NewRecorder()
			handleApplySpendingOptimizer(w, spendingPost(url.Values{"request_id": {id}, "recommendation": {"apply-token"}}))
			if w.Code != 200 {
				t.Fatalf("apply %d %s", w.Code, w.Body.String())
			}
			got, e := rm.Load()
			if e != nil {
				t.Fatal(e)
			}
			if got.SpendingSearch == nil || got.SpendingSearch.FloorMonthlyReal != 7000 {
				t.Fatalf("saved search preferences %+v", got.SpendingSearch)
			}
			switch mode {
			case "current-with-rules":
				if got.Guardrails == nil || got.Guardrails.MinMonthlySpendingReal != 5000 {
					t.Fatalf("current-plan apply changed the policy floor: %+v", got.Guardrails)
				}
			case "current-nil-rules":
				if got.Guardrails != nil {
					t.Fatalf("current-plan apply wrote rules: %+v", got.Guardrails)
				}
			case "searched":
				if got.Guardrails == nil || got.Guardrails.MinMonthlySpendingReal != 6000 {
					t.Fatalf("searched apply lost the candidate's rules: %+v", got.Guardrails)
				}
			}
			form := spendingOptimizerFormData(got, nil)
			if form["FloorMonthlyReal"] != 7000.0 || form["MinimumAboveCurrent"] != false {
				t.Fatalf("form minimum %#v above-current %#v", form["FloorMonthlyReal"], form["MinimumAboveCurrent"])
			}
			// The persisted record round-trips through JSON under its own key.
			raw, _ := json.Marshal(got.SpendingSearch)
			if !strings.Contains(string(raw), `"floor_monthly_real":7000`) {
				t.Fatalf("search preferences JSON %s", raw)
			}
		})
	}
}

// The rendered form input carries the saved comparison minimum through the
// template's single %.2f formatter.
func TestSpendingOptimizerFormRendersSavedMinimum(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = 8000
	s.Guardrails = &models.GuardrailConfig{Enabled: true, MinMonthlySpendingReal: 5000}
	s.SpendingSearch = &models.SpendingSearchPreferences{FloorMonthlyReal: 7000, MaxShortfallPct: 20}
	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-spending-optimizer", map[string]any{"SpendingOptimizerForm": spendingOptimizerFormData(s, nil)}); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if !strings.Contains(body, `name="floor_monthly_real"`) || !strings.Contains(body, `value="7000.00"`) || strings.Contains(body, `value="5000.00"`) {
		t.Fatalf("form did not prefill the saved minimum:\n%s", body)
	}
}
