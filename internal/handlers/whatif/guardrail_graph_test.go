package whatif

import (
	"budget2/internal/models"
	"budget2/internal/services/retirement"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGuardrailGraphExactPolicyAndNoMutation(t *testing.T) {
	for _, mode := range []string{"real", "nominal"} {
		for _, policy := range []string{"selected", "current", "no-guardrails"} {
			t.Run(mode+policy, func(t *testing.T) {
				rm, cleanup := setupTestEnv(t)
				defer cleanup()
				s, _ := rm.Load()
				s.ProjectionYears = 3
				s.PortfolioValue = 2400000
				s.MonthlyLivingExpenses = 9000
				s.InflationRate = 4
				s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 5, CeilingRisePct: 15, CeilingRaisePct: 10, MaxSpendingPct: 150}
				if err := rm.Save(s); err != nil {
					t.Fatal(err)
				}
				s, revision, _ := rm.LoadContextWithRevision(context.Background())
				raw, _ := json.Marshal(s)
				cfg := &models.GuardrailConfig{Enabled: true, FloorDropPct: 1, FloorCutPct: 25, CeilingRisePct: 1, CeilingRaisePct: 5, MaxSpendingPct: 120, MinMonthlySpendingReal: 7500}
				if policy == "current" {
					cfg = s.Guardrails
				}
				if policy == "no-guardrails" {
					cfg = nil
				}
				candidate := models.GuardrailOptimizerCandidate{ID: policy, Guardrails: cfg, Qualifies: false}
				id := "graph-fidelity-" + mode + policy
				guardrailPreviews.entries[id] = &guardrailPreview{manager: rm, scenario: rm.ActiveFilename(), revision: revision, fingerprint: sha256.Sum256(raw), expires: time.Now().Add(time.Minute), graphs: map[string]models.GuardrailOptimizerCandidate{"graph-only": candidate}}
				defer delete(guardrailPreviews.entries, id)
				form := url.Values{"request_id": {id}, "candidate": {"graph-only"}, "display_dollars": {mode}, "min_monthly_spending_real": {"1"}}
				req := httptest.NewRequest("POST", "/whatif/guardrails/optimize/graph", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				w := httptest.NewRecorder()
				handleGuardrailOptimizerGraph(w, req)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				var got struct {
					Chart     map[string]any                     `json:"chart"`
					Candidate models.GuardrailOptimizerCandidate `json:"candidate"`
					Mode      string                             `json:"display_dollars"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				clone := *s
				clone.Guardrails = cfg
				in, _, err := buildEngineInput(&clone)
				if err != nil {
					t.Fatal(err)
				}
				in.Hooks = retirement.DefaultHooks()
				expectedRaw, _ := json.Marshal(buildProjectionChartData(&clone, getEngine().Run(in), mode))
				var expected map[string]any
				_ = json.Unmarshal(expectedRaw, &expected)
				if !reflect.DeepEqual(got.Chart, expected) || !reflect.DeepEqual(got.Candidate, candidate) || got.Mode != mode {
					t.Fatal("graph differs from canonical retained policy")
				}
				// GV2 criterion 5: the base-case preview shares
				// buildProjectionChartData, so a guardrails-enabled candidate
				// must show the trigger/budget-panel traces and a
				// guardrails-disabled candidate (the no-guardrails baseline)
				// must show none of them.
				wantGuardrailTraces := cfg != nil && cfg.Enabled
				for _, name := range []string{"Cut trigger", "Raise trigger", "Planned", "After guardrails"} {
					if gv2GraphHasTrace(got.Chart, name) != wantGuardrailTraces {
						t.Fatalf("policy %s (guardrails enabled=%v): trace %q present=%v, want %v", policy, wantGuardrailTraces, name, !wantGuardrailTraces, wantGuardrailTraces)
					}
				}
				after, afterRev, _ := rm.LoadContextWithRevision(context.Background())
				afterRaw, _ := json.Marshal(after)
				if string(afterRaw) != string(raw) || afterRev != revision {
					t.Fatal("graph saved settings or revision")
				}
				req = httptest.NewRequest("POST", "/whatif/guardrails/optimize/apply", strings.NewReader(url.Values{"request_id": {id}, "recommendation": {"graph-only"}}.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				w = httptest.NewRecorder()
				handleApplyGuardrailOptimizer(w, req)
				if w.Code != 409 {
					t.Fatal("graph token granted Apply")
				}
			})
		}
	}
}

func TestGuardrailGraphRejectsInvalidAccess(t *testing.T) {
	for _, mode := range []string{"forged", "expired", "cancelled", "wrong-request", "manager", "scenario", "revision", "fingerprint"} {
		t.Run(mode, func(t *testing.T) {
			rm, cleanup := setupTestEnv(t)
			defer cleanup()
			s, rev, _ := rm.LoadContextWithRevision(context.Background())
			raw, _ := json.Marshal(s)
			id := "graph-access-" + mode
			token := "graph-token"
			entry := &guardrailPreview{manager: rm, scenario: rm.ActiveFilename(), revision: rev, fingerprint: sha256.Sum256(raw), expires: time.Now().Add(time.Minute), cancel: func() {}, graphs: map[string]models.GuardrailOptimizerCandidate{token: {ID: "no-guardrails"}}}
			guardrailPreviews.entries[id] = entry
			defer delete(guardrailPreviews.entries, id)
			switch mode {
			case "forged":
				token = "forged"
			case "expired":
				entry.expires = time.Now().Add(-time.Minute)
			case "cancelled":
				delete(guardrailPreviews.entries, id)
			case "wrong-request":
				id = "absent"
			case "manager":
				entry.manager = nil
			case "scenario":
				entry.scenario = "another.json"
			case "revision":
				entry.revision++
			case "fingerprint":
				entry.fingerprint = [32]byte{1}
			}
			req := httptest.NewRequest("POST", "/whatif/guardrails/optimize/graph", strings.NewReader(url.Values{"request_id": {id}, "candidate": {token}}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			handleGuardrailOptimizerGraph(w, req)
			if w.Code != 409 {
				t.Fatal("invalid graph access", w.Code, w.Body.String())
			}
			after, afterRev, _ := rm.LoadContextWithRevision(context.Background())
			afterRaw, _ := json.Marshal(after)
			if string(afterRaw) != string(raw) || afterRev != rev {
				t.Fatal("rejected graph modified plan")
			}
		})
	}
}

func TestGuardrailGraphEveryRowHasIndependentAction(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	rows := []guardrailOptimizerRow{{Candidate: models.GuardrailOptimizerCandidate{ID: "below"}, GraphToken: "below-token"}, {Candidate: models.GuardrailOptimizerCandidate{ID: "current", Baseline: true}, GraphToken: "current-token"}, {Candidate: models.GuardrailOptimizerCandidate{ID: "no-guardrails", Baseline: true}, GraphToken: "nil-token"}}
	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-guardrail-optimizer-results", map[string]any{"Optimizer": &models.GuardrailOptimizerResult{}, "Rows": rows, "Target": "99", "RequestID": "row-test"}); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		node := guardrailTestElement(t, w.Body.String(), "data-guardrail-graph", row.GraphToken)
		if node.Data != "button" || guardrailTestAttribute(node, "aria-pressed") != "false" {
			t.Fatal("row lacks accessible graph action")
		}
	}
	if strings.Contains(w.Body.String(), ">Apply</button>") {
		t.Fatal("graph made below-target/baseline applicable")
	}
}

// gv2GraphHasTrace reports whether a decoded chart JSON (map[string]any, as
// produced by http.ResponseWriter round-tripping buildProjectionChartData
// through encoding/json) has a trace with the given "name".
func gv2GraphHasTrace(chart map[string]any, name string) bool {
	data, _ := chart["data"].([]any)
	for _, raw := range data {
		trace, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := trace["name"].(string); n == name {
			return true
		}
	}
	return false
}
