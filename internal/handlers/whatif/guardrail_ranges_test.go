package whatif

import (
	"budget2/internal/models"
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

// Distinct simulation results may share the exact same base projection. The
// graph response must retain each candidate's held-out data without recomputing
// it or accepting client replacements for the floor, seed, or classification.
func TestGuardrailGraphRetainsSimulationSource(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, rev, _ := rm.LoadContextWithRevision(context.Background())
	raw, _ := json.Marshal(s)
	const first = `{"id":"a","qualifies":false,"metrics":{"floor_success_pct":30.8},"simulation_years":[{"year":1,"paths":1000,"living_real":{"p10":0,"p50":7500.125,"p90":9000},"living_nominal":{"p10":0,"p50":8000,"p90":9500},"portfolio_real":{"p10":0,"p50":123456,"p90":456789},"portfolio_nominal":{"p10":0,"p50":234567,"p90":567890}},{"year":2,"paths":700,"living_real":{"p10":0,"p50":0,"p90":0},"living_nominal":{"p10":0,"p50":0,"p90":0},"portfolio_real":{"p10":0,"p50":0,"p90":0},"portfolio_nominal":{"p10":0,"p50":0,"p90":0}}]}`
	second := strings.ReplaceAll(strings.Replace(first, `"id":"a"`, `"id":"b"`, 1), "7500.125", "6500.125")
	entry := &guardrailPreview{manager: rm, scenario: rm.ActiveFilename(), revision: rev, fingerprint: sha256.Sum256(raw), expires: time.Now().Add(time.Minute), request: models.GuardrailOptimizerRequest{FloorMonthlyReal: 7500, TargetSuccessPct: 99}, validationSeed: 9223372036854775700, validationRuns: 1000, graphs: map[string]models.GuardrailOptimizerCandidate{}}
	guardrailPreviews.entries["range-source"] = entry
	defer delete(guardrailPreviews.entries, "range-source")
	var baselineChart any
	for i, fixture := range []string{first, second} {
		var candidate models.GuardrailOptimizerCandidate
		if err := json.Unmarshal([]byte(fixture), &candidate); err != nil {
			t.Fatal(err)
		}
		entry.graphs[candidate.ID] = candidate
		for _, mode := range []string{"real", "nominal"} {
			form := url.Values{"request_id": {"range-source"}, "candidate": {candidate.ID}, "display_dollars": {mode}, "floor_monthly_real": {"1"}, "validation_seed": {"1"}}
			r := httptest.NewRequest("POST", "/whatif/guardrails/optimize/graph", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			handleGuardrailOptimizerGraph(w, r)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var got, want map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(fixture), &want); err != nil {
				t.Fatal(err)
			}
			actual := got["candidate"].(map[string]any)
			if !reflect.DeepEqual(actual["simulation_years"], want["simulation_years"]) {
				t.Fatal("retained simulation years changed or missing", actual)
			}
			if got["success_text"] != "30.80" || actual["qualifies"] != false || got["validation_seed"] != "9223372036854775700" || got["validation_runs"] != float64(1000) || got["floor_monthly_real"] != float64(7500) || got["target"] != float64(99) {
				t.Fatal("retained metadata changed", got)
			}
			if mode == "real" {
				if i == 0 {
					baselineChart = got["chart"]
				} else if !reflect.DeepEqual(baselineChart, got["chart"]) {
					t.Fatal("fixture base charts differ")
				}
			}
		}
	}
	after, afterRev, _ := rm.LoadContextWithRevision(context.Background())
	afterRaw, _ := json.Marshal(after)
	if string(afterRaw) != string(raw) || afterRev != rev {
		t.Fatal("graph changed settings")
	}
}
