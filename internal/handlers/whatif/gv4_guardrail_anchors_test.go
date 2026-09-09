package whatif

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/templates"
)

// gv4AnchorFixtureSettings mirrors gv2ChartSettings's shape (same package,
// GV2's engine-run fixture) but tuned so the year-0 GuardrailCutTrigger/
// GuardrailRaiseTrigger land on a fractional cent
// (987654.32 x 0.85 = 839506.172; x 1.15 = 1135802.468), so a stray %.0f
// or an un-rounded float would show up in the rendered string (ruling
// 2026-08-29b: assert on rendered strings, not floats).
func gv4AnchorFixtureSettings() *models.WhatIfSettings {
	guardrails := &models.GuardrailConfig{Enabled: true, FloorDropPct: 15, FloorCutPct: 10, CeilingRisePct: 15, CeilingRaisePct: 10, MinSpendingPct: 50, MaxSpendingPct: 150}
	s := gv2ChartSettings(guardrails)
	s.PortfolioValue = 987654.32
	return s
}

// TestBuildGuardrailAnchors_YearZeroTriggers is the GV4 criterion-1 unit
// test: buildGuardrailAnchors copies the engine's year-0
// GuardrailCutTrigger/GuardrailRaiseTrigger verbatim (SPEC.md ss3 -- one
// source per figure). The fixture-defect checks below independently confirm
// year 0's triggers equal starting portfolio x (1-drop%) / x (1+rise%),
// matching the task's documented year-0 formula, before trusting the
// engine's own field for the real assertion.
func TestBuildGuardrailAnchors_YearZeroTriggers(t *testing.T) {
	s := gv4AnchorFixtureSettings()
	p := gv2RunChart(t, s)
	if len(p.YearlySummaries) == 0 {
		t.Fatal("fixture defect: no yearly summaries")
	}
	year0 := p.YearlySummaries[0]
	if year0.GuardrailCutTrigger <= 0 || year0.GuardrailRaiseTrigger <= 0 {
		t.Fatalf("fixture defect: year-0 triggers not positive: %+v", year0)
	}

	wantCut := s.PortfolioValue * (1 - s.Guardrails.FloorDropPct/100)
	wantRaise := s.PortfolioValue * (1 + s.Guardrails.CeilingRisePct/100)
	if !gv2ChartClose(year0.GuardrailCutTrigger, wantCut) {
		t.Fatalf("fixture defect: year-0 cut trigger %.6f != %.6f", year0.GuardrailCutTrigger, wantCut)
	}
	if !gv2ChartClose(year0.GuardrailRaiseTrigger, wantRaise) {
		t.Fatalf("fixture defect: year-0 raise trigger %.6f != %.6f", year0.GuardrailRaiseTrigger, wantRaise)
	}

	got := buildGuardrailAnchors(s, p)
	if got == nil {
		t.Fatal("expected non-nil anchors")
	}
	if got.CutBelow != year0.GuardrailCutTrigger || got.RaiseAbove != year0.GuardrailRaiseTrigger {
		t.Fatalf("anchors = %+v, want CutBelow=%.6f RaiseAbove=%.6f", got, year0.GuardrailCutTrigger, year0.GuardrailRaiseTrigger)
	}
}

// TestBuildGuardrailAnchors_NilCases covers criterion 1's "absent, never
// $0.00" requirement: disabled/missing guardrails, no projection, no
// yearly summaries, and zero triggers must all yield nil.
func TestBuildGuardrailAnchors_NilCases(t *testing.T) {
	projection := &models.ProjectionResult{YearlySummaries: []models.ProjectionYearSummary{{Year: 0, GuardrailCutTrigger: 100, GuardrailRaiseTrigger: 200}}}
	cases := []*models.WhatIfSettings{
		nil,
		{Guardrails: nil},
		{Guardrails: &models.GuardrailConfig{Enabled: false}},
	}
	for i, s := range cases {
		if got := buildGuardrailAnchors(s, projection); got != nil {
			t.Fatalf("case %d: expected nil, got %+v", i, got)
		}
	}

	enabled := &models.WhatIfSettings{Guardrails: &models.GuardrailConfig{Enabled: true}}
	if got := buildGuardrailAnchors(enabled, nil); got != nil {
		t.Fatal("expected nil anchors for nil projection")
	}
	if got := buildGuardrailAnchors(enabled, &models.ProjectionResult{}); got != nil {
		t.Fatal("expected nil anchors when there are no yearly summaries")
	}
	zero := &models.ProjectionResult{YearlySummaries: []models.ProjectionYearSummary{{Year: 0}}}
	if got := buildGuardrailAnchors(enabled, zero); got != nil {
		t.Fatal("expected nil anchors when year-0 triggers are zero")
	}
}

// TestBuildGuardrailAnchors_UsesEngineTriggersNotPercentages is attempt-2's
// mutation-kill for mutation (a) (checker-tests FAIL on attempt 1):
// TestBuildGuardrailAnchors_YearZeroTriggers only exercises an engine-run
// projection whose year-0 triggers happen to equal the percentage products,
// so a buildGuardrailAnchors rewritten to compute
// PortfolioValue*(1-FloorDropPct/100) / *(1+CeilingRisePct/100) instead of
// copying the engine's year-0 GuardrailCutTrigger/GuardrailRaiseTrigger
// stayed green. Here the hand-set year-0 triggers (100000/200000)
// deliberately differ from the percentage products (900000/1200000), so
// that mutation returns the wrong numbers and this test catches it.
func TestBuildGuardrailAnchors_UsesEngineTriggersNotPercentages(t *testing.T) {
	s := &models.WhatIfSettings{
		PortfolioValue: 1_000_000,
		Guardrails: &models.GuardrailConfig{
			Enabled:        true,
			FloorDropPct:   10, // percentage product would be 900,000
			CeilingRisePct: 20, // percentage product would be 1,200,000
		},
	}
	projection := &models.ProjectionResult{
		YearlySummaries: []models.ProjectionYearSummary{
			{Year: 0, GuardrailCutTrigger: 100000, GuardrailRaiseTrigger: 200000},
		},
	}

	got := buildGuardrailAnchors(s, projection)
	if got == nil {
		t.Fatal("expected non-nil anchors")
	}
	if got.CutBelow != 100000 {
		t.Errorf("CutBelow = %v, want 100000 (the engine's year-0 trigger, not PortfolioValue*(1-FloorDropPct/100)=900000)", got.CutBelow)
	}
	if got.RaiseAbove != 200000 {
		t.Errorf("RaiseAbove = %v, want 200000 (the engine's year-0 trigger, not PortfolioValue*(1+CeilingRisePct/100)=1200000)", got.RaiseAbove)
	}
}

// TestRenderGuardrailAnchorsPartial is criterion 1's rendering test: the
// "whatif-guardrail-anchors" block renders the exact
// "Next yearly check: cut if ..." / "Next yearly check: raise if ..." lines
// via formatMoney.
func TestRenderGuardrailAnchorsPartial(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	anchors := &GuardrailAnchors{CutBelow: 1_920_000.004, RaiseAbove: 2_880_000.006}
	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-guardrail-anchors", map[string]any{"GuardrailAnchors": anchors}); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()

	wantCut := "Next yearly check: cut if the portfolio falls below " + templates.FormatMoney(anchors.CutBelow)
	wantRaise := "Next yearly check: raise if it rises above " + templates.FormatMoney(anchors.RaiseAbove)
	if !strings.Contains(body, wantCut) {
		t.Errorf("missing cut line %q\nbody:\n%s", wantCut, body)
	}
	if !strings.Contains(body, wantRaise) {
		t.Errorf("missing raise line %q\nbody:\n%s", wantRaise, body)
	}
}

// TestRenderGuardrailAnchorsPartial_AbsentWhenNil covers the "lines absent"
// half of criterion 1: with no GuardrailAnchors value, the block renders
// nothing (never "$0.00").
func TestRenderGuardrailAnchorsPartial_AbsentWhenNil(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-guardrail-anchors", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(w.Body.String()); got != "" {
		t.Fatalf("expected empty output when GuardrailAnchors is absent, got %q", got)
	}
}

// TestRenderGuardrailsCard_AnchorsInsideCard is attempt-2's mutation-kill for
// mutation (d) (checker-tests FAIL on attempt 1): deleting
// `<div id="whatif-guardrail-anchors">{{template "whatif-guardrail-anchors" .}}</div>`
// from guardrails.html (leaving the visible card with no anchor lines)
// survived TestRenderGuardrailAnchorsPartial, which renders the standalone
// "whatif-guardrail-anchors" block directly and never touches the card. This
// test instead renders the FULL "whatif-guardrails" card partial for the
// existing fractional-cent fixture and asserts both figures appear inside
// the card's #whatif-guardrail-anchors container, and that a disabled-
// guardrails render of the same card shows neither figure nor the cut
// phrasing.
func TestRenderGuardrailsCard_AnchorsInsideCard(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := gv4AnchorFixtureSettings()
	p := gv2RunChart(t, s)
	if len(p.YearlySummaries) == 0 {
		t.Fatal("fixture defect: no yearly summaries")
	}
	year0 := p.YearlySummaries[0]
	if year0.GuardrailCutTrigger <= 0 || year0.GuardrailRaiseTrigger <= 0 {
		t.Fatal("fixture defect: year-0 triggers not positive")
	}
	anchors := buildGuardrailAnchors(s, p)
	if anchors == nil {
		t.Fatal("expected non-nil anchors")
	}

	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-guardrails", map[string]any{"Settings": s, "GuardrailAnchors": anchors}); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()

	idx := strings.Index(body, `id="whatif-guardrail-anchors"`)
	if idx < 0 {
		t.Fatalf("missing #whatif-guardrail-anchors container in guardrails card\nbody:\n%s", body)
	}
	rest := body[idx:]
	end := strings.Index(rest, "</div>")
	if end < 0 {
		t.Fatal("could not find closing </div> for #whatif-guardrail-anchors container")
	}
	container := rest[:end]

	wantCut := templates.FormatMoney(anchors.CutBelow)
	wantRaise := templates.FormatMoney(anchors.RaiseAbove)
	if !strings.Contains(container, wantCut) {
		t.Errorf("guardrails card missing cut figure %q inside #whatif-guardrail-anchors\ncontainer:\n%s", wantCut, container)
	}
	if !strings.Contains(container, wantRaise) {
		t.Errorf("guardrails card missing raise figure %q inside #whatif-guardrail-anchors\ncontainer:\n%s", wantRaise, container)
	}

	// Guardrails disabled: the card renders no anchor container at all, so
	// neither figure nor the cut/raise phrasing should appear anywhere.
	disabled := gv4AnchorFixtureSettings()
	disabled.Guardrails.Enabled = false
	w2 := httptest.NewRecorder()
	if err := renderer.RenderPartial(w2, "whatif-guardrails", map[string]any{"Settings": disabled, "GuardrailAnchors": nil}); err != nil {
		t.Fatal(err)
	}
	disabledBody := w2.Body.String()
	if strings.Contains(disabledBody, wantCut) || strings.Contains(disabledBody, wantRaise) {
		t.Fatalf("disabled guardrails card unexpectedly contains anchor figures\nbody:\n%s", disabledBody)
	}
	if strings.Contains(disabledBody, "cut if the portfolio") {
		t.Fatalf("disabled guardrails card unexpectedly contains guardrail-anchor phrasing\nbody:\n%s", disabledBody)
	}
}

// oobGuardrailAnchorsSnippet isolates the OOB `#whatif-guardrail-anchors`
// element's own markup (as opposed to the identically-worded, non-OOB copy
// rendered inside the visible guardrails card) so assertions can't pass by
// matching the wrong copy.
func oobGuardrailAnchorsSnippet(t *testing.T, body string) string {
	t.Helper()
	const marker = `id="whatif-guardrail-anchors" hx-swap-oob="true">`
	idx := strings.Index(body, marker)
	if idx < 0 {
		t.Fatalf("missing OOB #whatif-guardrail-anchors element\nbody:\n%s", body)
	}
	rest := body[idx+len(marker):]
	end := strings.Index(rest, "</div>")
	if end < 0 {
		t.Fatal("could not find closing </div> for the OOB guardrail anchors element")
	}
	return rest[:end]
}

// TestRenderWhatIfResultsWithOOB_GuardrailAnchors is criterion 1/2's
// end-to-end render test: buildResultsPartialData carries GuardrailAnchors,
// and whatif-results-with-oob's OOB #whatif-guardrail-anchors element
// renders the SAME rendered strings the year-0 engine fields produce.
func TestRenderWhatIfResultsWithOOB_GuardrailAnchors(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s := gv4AnchorFixtureSettings()
	// analysisFastOrCached (not a bare engine.Run), matching the production
	// handleWhatIf/saveAndRecalc path; pendingHash != "" marks the async
	// (FailurePoints/Sensitivity/etc.) fields not yet computed, exactly as
	// renderResultsTemplate does, so the nested pending-card branches (not
	// the fields themselves) render.
	analysis, pendingHash, err := analysisFastOrCached(s)
	if err != nil {
		t.Fatal(err)
	}
	year0 := analysis.Projection.YearlySummaries[0]
	if year0.GuardrailCutTrigger <= 0 || year0.GuardrailRaiseTrigger <= 0 {
		t.Fatal("fixture defect: year-0 triggers not positive")
	}

	data := buildResultsPartialData(s, analysis, nil)
	data["AnalysisPending"] = pendingHash != ""
	data["AsyncHash"] = pendingHash

	w := httptest.NewRecorder()
	if err := renderer.RenderPartial(w, "whatif-results-with-oob", data); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()

	snippet := oobGuardrailAnchorsSnippet(t, body)
	wantCut := "Next yearly check: cut if the portfolio falls below " + templates.FormatMoney(year0.GuardrailCutTrigger)
	wantRaise := "Next yearly check: raise if it rises above " + templates.FormatMoney(year0.GuardrailRaiseTrigger)
	if !strings.Contains(snippet, wantCut) {
		t.Errorf("OOB anchors missing cut line %q\nsnippet:\n%s", wantCut, snippet)
	}
	if !strings.Contains(snippet, wantRaise) {
		t.Errorf("OOB anchors missing raise line %q\nsnippet:\n%s", wantRaise, snippet)
	}
}

// TestHandleWhatIfGuardrails_AnchorsRefreshOnSave is criterion 2's handler
// test: POST /whatif/guardrails changing the drop % must make the response
// body's OOB #whatif-guardrail-anchors show the NEW figure -- proving the
// anchors refresh on save and come from the recomputed projection's year-0
// summary, not a stale render.
func TestHandleWhatIfGuardrails_AnchorsRefreshOnSave(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s, err := rm.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.PortfolioValue = 1_000_000
	s.ProjectionYears = 5
	s.Guardrails = &models.GuardrailConfig{Enabled: true, FloorDropPct: 10, FloorCutPct: 10, CeilingRisePct: 20, CeilingRaisePct: 10, MinSpendingPct: 50, MaxSpendingPct: 150}
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	oldCut := s.PortfolioValue * (1 - 10.0/100) // 900,000.00

	form := url.Values{
		"enabled":           {"on"},
		"floor_drop_pct":    {"25"}, // changed from 10 -> 25
		"floor_cut_pct":     {"10"},
		"ceiling_rise_pct":  {"20"},
		"ceiling_raise_pct": {"10"},
		"min_spending_pct":  {"50"},
		"max_spending_pct":  {"150"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/guardrails", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGuardrails(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	snippet := oobGuardrailAnchorsSnippet(t, body)
	newCut := s.PortfolioValue * (1 - 25.0/100) // 750,000.00
	wantNew := templates.FormatMoney(newCut)
	wantOld := templates.FormatMoney(oldCut)
	if !strings.Contains(snippet, wantNew) {
		t.Fatalf("OOB anchors missing NEW cut figure %q\nsnippet:\n%s", wantNew, snippet)
	}
	if strings.Contains(snippet, wantOld) {
		t.Fatalf("OOB anchors still shows OLD cut figure %q\nsnippet:\n%s", wantOld, snippet)
	}
}
