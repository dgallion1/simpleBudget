package whatif

import (
	"budget2/internal/models"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGuardrailOptimizerInvalidInputsDoNotSave(t *testing.T) {
	for _, tc := range []struct{ floor, target string }{{"7500", ""}, {"NaN", "95"}, {"+Inf", "95"}, {"0", "95"}, {"999999999", "95"}, {"7500", "100"}, {"7500", "NaN"}, {"7500", "0"}} {
		t.Run(tc.floor+"_"+tc.target, func(t *testing.T) {
			rm, cleanup := setupTestEnv(t)
			defer cleanup()
			s, _ := rm.Load()
			s.MonthlyLivingExpenses = 9000
			if err := rm.Save(s); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(s)
			req := httptest.NewRequest("POST", "/whatif/guardrails/optimize", strings.NewReader(url.Values{"floor_monthly_real": {tc.floor}, "target_success_pct": {tc.target}, "request_id": {"validation"}}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			handleGuardrailOptimizer(w, req)
			if w.Code != 400 {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			s, _ = rm.Load()
			after, _ := json.Marshal(s)
			if string(before) != string(after) {
				t.Fatal("invalid preview saved settings")
			}
		})
	}
}

func TestGuardrailOptimizerCardAndResultsRender(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	s := models.DefaultWhatIfSettings()
	s.Guardrails = &models.GuardrailConfig{Enabled: true, MinMonthlySpendingReal: 7500}
	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-guardrails", map[string]any{"Settings": s}); err != nil {
		t.Fatal(err)
	}
	if value := guardrailTestAttribute(guardrailTestElement(t, w.Body.String(), "id", "guardrail-optimizer-floor"), "value"); value != "7500" {
		t.Fatalf("optimizer default floor = %q, want 7500", value)
	}
	for _, want := range []string{`<option value="">Choose a target</option>`, "90%", "95%", "99%", "Custom", "Healthcare, taxes", "Chance of maintaining my minimum spending", "Run optimizer", "Cancel search", `name="min_monthly_spending_real"`, `min="0" max="100"`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	result := &models.GuardrailOptimizerResult{SearchRuns: 64, ValidationRuns: 1000, HorizonMinYears: 20, HorizonMaxYears: 35}
	c := models.GuardrailOptimizerCandidate{ID: "boundary", Metrics: models.GuardrailOptimizerMetrics{Runs: 1000, FloorShortfallPaths: 51, FloorSuccessPct: 94.9, FloorSuccessCILowPct: 93.4, FloorSuccessCIHighPct: 96.1, DepletionPaths: 3, DepletionRiskPct: 0.3, MedianLifetimeFundedLivingReal: 1234567, P10LifetimeFundedLivingReal: 876543}}
	w = httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-guardrail-optimizer-results", map[string]any{"Optimizer": result, "Rows": []guardrailOptimizerRow{{Candidate: c}}, "Target": "95", "RequestID": "test"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"No options met your minimum-spending target", "94.90%", "Below target", "51 / 1000", "0.30% (3 / 1000)", "Wilson 95%", "20–35 years", "other obligations first", "scope=\"col\"", "scope=\"row\""} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	body := w.Body.String()
	if strings.Count(body, `scope="col"`) != 5 {
		t.Error("primary comparison must have five columns")
	}
	for _, want := range []string{"Chance of maintaining minimum", "Supporting details", "About these simulations", "Lower spending outcome", "10th percentile"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Index(body, "Chance of maintaining minimum") > strings.Index(body, "Lifetime living spending") {
		t.Error("minimum spending chance must lead the outcome columns")
	}
	detailsIndex := strings.Index(body, "<details")
	if detailsIndex < 0 || strings.Index(body, "0.30% (3 / 1000)") < detailsIndex {
		t.Error("depletion belongs in supporting details")
	}
	if strings.Contains(w.Body.String(), ">Apply</button>") {
		t.Fatal("infeasible preview has Apply")
	}
}

func TestGuardrailOptimizerApplyAndCancellation(t *testing.T) {
	for _, mode := range []string{"apply", "stale", "cancelled", "forged", "expired", "wrong-request"} {
		t.Run(mode, func(t *testing.T) {
			rm, cleanup := setupTestEnv(t)
			defer cleanup()
			s, rev, err := rm.LoadContextWithRevision(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(s)
			cfg := &models.GuardrailConfig{Enabled: true, MinMonthlySpendingReal: 7500, MinSpendingPct: 0, FloorDropPct: 20, FloorCutPct: 5, CeilingRisePct: 15, CeilingRaisePct: 10, MaxSpendingPct: 150}
			id := "apply-test-" + mode
			token := "opaque-server-token"
			_, cancel := context.WithCancel(context.Background())
			defer cancel()
			entry := &guardrailPreview{manager: rm, scenario: rm.ActiveFilename(), revision: rev, fingerprint: sha256.Sum256(raw), request: models.GuardrailOptimizerRequest{FloorMonthlyReal: 7500, TargetSuccessPct: 95}, cancel: cancel, expires: time.Now().Add(time.Minute), tokens: map[string]*models.GuardrailConfig{token: cfg}}
			guardrailPreviews.Lock()
			guardrailPreviews.entries[id] = entry
			guardrailPreviews.Unlock()
			if mode == "stale" {
				s.MonthlyLivingExpenses++
				if err := rm.Save(s); err != nil {
					t.Fatal(err)
				}
				raw, _ = json.Marshal(s)
			}
			if mode == "expired" {
				entry.expires = time.Now().Add(-time.Minute)
			}
			if mode == "cancelled" {
				r := httptest.NewRequest("POST", "/whatif/guardrails/optimize/cancel", strings.NewReader(url.Values{"request_id": {id}}.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				handleCancelGuardrailOptimizer(httptest.NewRecorder(), r)
			}
			if mode == "forged" {
				token = "forged"
			}
			if mode == "wrong-request" {
				id = "other-request"
			}
			r := httptest.NewRequest("POST", "/whatif/guardrails/optimize/apply", strings.NewReader(url.Values{"request_id": {id}, "recommendation": {token}, "min_monthly_spending_real": {"1"}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			handleApplyGuardrailOptimizer(w, r)
			got, err := rm.Load()
			if err != nil {
				t.Fatal(err)
			}
			if mode == "apply" {
				if w.Code != 200 || w.Header().Get("HX-Redirect") == "" {
					t.Fatalf("apply failed: %d %s", w.Code, w.Body.String())
				}
				if !reflect.DeepEqual(got.Guardrails, cfg) {
					t.Fatalf("wrong config %#v", got.Guardrails)
				}
				got.Guardrails = s.Guardrails
			} else if w.Code != 409 {
				t.Fatalf("expected conflict: %d", w.Code)
			}
			after, _ := json.Marshal(got)
			if string(raw) != string(after) {
				t.Fatal("unrelated/rejected settings changed")
			}
		})
	}
}

func TestGuardrailOptimizerOrdinaryFormPreservesFloor(t *testing.T) {
	for _, mode := range []string{"preserve", "edit", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			rm, cleanup := setupTestEnv(t)
			defer cleanup()
			s, _ := rm.Load()
			s.MonthlyLivingExpenses = 9000
			s.Guardrails = &models.GuardrailConfig{Enabled: true, MinMonthlySpendingReal: 7500, MinSpendingPct: 0, MaxSpendingPct: 150, FloorDropPct: 20, FloorCutPct: 5, CeilingRisePct: 15, CeilingRaisePct: 5}
			if err := rm.Save(s); err != nil {
				t.Fatal(err)
			}
			form := url.Values{"enabled": {"on"}, "min_spending_pct": {"0"}}
			if mode == "edit" {
				form.Set("min_monthly_spending_real", "8000")
			}
			if mode == "invalid" {
				form.Set("min_monthly_spending_real", "NaN")
			}
			r := httptest.NewRequest("POST", "/whatif/guardrails", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			handleWhatIfGuardrails(w, r)
			got, err := rm.Load()
			if err != nil {
				t.Fatal(err)
			}
			expected := 7500.0
			if mode == "edit" {
				expected = 8000
			}
			if got.Guardrails.MinMonthlySpendingReal != expected || got.Guardrails.MinSpendingPct != 0 {
				t.Fatalf("floor/minpct lost: %#v", got.Guardrails)
			}
			if mode == "invalid" && w.Code != 400 {
				t.Fatalf("invalid floor status %d", w.Code)
			}
		})
	}
}

func TestGuardrailOptimizerPreviewThenExplicitApply(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, _ := rm.Load()
	s.MonthlyLivingExpenses = 9000
	s.ProjectionYears = 1
	s.IncomeSources = []models.IncomeSource{{ID: "fixture-pension", Name: "Fixture pension", Amount: 1000000, Type: models.IncomeFixed, COLARate: 0.1}}
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(s)
	request := func(path string, form url.Values) *http.Request {
		r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}
	w := httptest.NewRecorder()
	handleGuardrailOptimizer(w, request("/whatif/guardrails/optimize", url.Values{"floor_monthly_real": {"7500"}, "target_success_pct": {"95"}, "request_id": {"real-preview-test"}}))
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	got, _ := rm.Load()
	after, _ := json.Marshal(got)
	if string(before) != string(after) {
		t.Fatal("search saved settings")
	}
	var response struct {
		Rows      []guardrailOptimizerRow
		RequestID string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	seenGraphTokens := make(map[string]bool)
	for _, row := range response.Rows {
		if row.GraphToken == "" || seenGraphTokens[row.GraphToken] {
			t.Fatal("returned row missing unique graph token")
		}
		seenGraphTokens[row.GraphToken] = true
		retained, ok := guardrailPreviews.entries[response.RequestID].graphs[row.GraphToken]
		if !ok || !reflect.DeepEqual(retained, row.Candidate) {
			t.Fatal("retained graph candidate differs from search result")
		}
		if row.Token != "" && row.Token == row.GraphToken {
			t.Fatal("graph token reused Apply token")
		}
	}
	var selected guardrailOptimizerRow
	for _, row := range response.Rows {
		if row.Token != "" {
			selected = row
			break
		}
	}
	if selected.Token == "" {
		t.Fatal("income-funded fixture produced no qualifying recommendation")
	}
	w = httptest.NewRecorder()
	handleApplyGuardrailOptimizer(w, request("/whatif/guardrails/optimize/apply", url.Values{"request_id": {response.RequestID}, "recommendation": {selected.Token}}))
	if w.Code != 200 || w.Header().Get("HX-Redirect") == "" {
		t.Fatalf("apply: %d %s", w.Code, w.Body.String())
	}
	got, _ = rm.Load()
	if !reflect.DeepEqual(got.Guardrails, selected.Candidate.Guardrails) {
		t.Fatal("applied different configuration")
	}
	w = httptest.NewRecorder()
	handleApplyGuardrailOptimizer(w, request("/whatif/guardrails/optimize/apply", url.Values{"request_id": {response.RequestID}, "recommendation": {selected.Token}}))
	if w.Code != 409 {
		t.Fatal("consumed recommendation replayed")
	}
}

// guardrailTestElement tokenizes rendered HTML with the standard-library HTML
// entities/autoclose rules. Assertions bind to one real element, not another
// control with the same value elsewhere in the ordinary guardrail form.
type guardrailTestHTMLNode struct {
	Data string
	Attr []xml.Attr
	Text string
}

func guardrailTestElement(t *testing.T, markup, attribute, value string) *guardrailTestHTMLNode {
	t.Helper()
	decoder := xml.NewDecoder(strings.NewReader(markup))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity
	for {
		token, err := decoder.Token()
		if err != nil {
			t.Fatalf("missing element %s=%q: %v", attribute, value, err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		matched := false
		for _, a := range start.Attr {
			if a.Name.Local == attribute && a.Value == value {
				matched = true
			}
		}
		if !matched {
			continue
		}
		result := &guardrailTestHTMLNode{Data: start.Name.Local, Attr: start.Attr}
		if start.Name.Local == "h4" {
			depth := 1
			for depth > 0 {
				child, err := decoder.Token()
				if err != nil {
					t.Fatal(err)
				}
				switch c := child.(type) {
				case xml.StartElement:
					depth++
				case xml.EndElement:
					depth--
				case xml.CharData:
					result.Text += string(c)
				}
			}
		}
		return result
	}
}

func guardrailTestAttribute(node *guardrailTestHTMLNode, key string) string {
	for _, a := range node.Attr {
		if a.Name.Local == key {
			return a.Value
		}
	}
	return ""
}

func guardrailTestText(node *guardrailTestHTMLNode) string { return strings.TrimSpace(node.Text) }

func TestGuardrailOptimizerOutcomeHasMeaningfulFocusTarget(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()
	for _, qualifies := range []bool{false, true} {
		result := &models.GuardrailOptimizerResult{}
		want := "No options met your minimum-spending target"
		if qualifies {
			result.Recommendations = []string{"candidate"}
			want = "Options meet your minimum-spending target"
		}
		w := httptest.NewRecorder()
		if err := renderer.RenderPartial(w, "whatif-guardrail-optimizer-results", map[string]any{"Optimizer": result, "Rows": []guardrailOptimizerRow{}, "Target": "95", "RequestID": "test"}); err != nil {
			t.Fatal(err)
		}
		heading := guardrailTestElement(t, w.Body.String(), "data-guardrail-outcome", "")
		if heading.Data != "h4" || guardrailTestAttribute(heading, "tabindex") != "-1" {
			t.Fatal("outcome must be a focusable heading")
		}
		if got := guardrailTestText(heading); got != want {
			t.Errorf("focused outcome = %q, want %q", got, want)
		}
	}
	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-guardrail-optimizer", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	status := guardrailTestElement(t, w.Body.String(), "id", "guardrail-optimizer-status")
	if guardrailTestAttribute(status, "role") != "status" || guardrailTestAttribute(status, "aria-live") != "polite" {
		t.Fatal("outcome status must announce politely")
	}
}
