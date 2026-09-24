package whatif

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/internal/testutil"
)

// setupBrokenChainEnvWithRenderer mirrors setupItemsThenBreakChain (a
// pre-existing helper) but wires a REAL template renderer, the way
// setupTestEnvWithRenderer does for its own tests: WS3 (D4) needs the
// actual whatif page templates to exercise, not the "Templates not loaded"
// fallback. The primary settings' chain has one link, index 0, pointing at
// a scenario file that exists on disk (passes the save-time Stat check) but
// is not valid JSON, so buildEngineInput's LoadScenarioSettings fails at
// analysis time — the exact D4 failure mode.
func setupBrokenChainEnvWithRenderer(t *testing.T) (*retirement.SettingsManager, func()) {
	t.Helper()

	settingsDir := t.TempDir()
	csvDir := t.TempDir()

	csvPath := filepath.Join(csvDir, "test.csv")
	csvContent := "Date,Description,Amount,Type,Category\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Salary,5000,Income,Employment\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("WriteFile csv: %v", err)
	}

	store, err := storage.New(settingsDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	rm := retirement.NewSettingsManager(settingsDir, store)
	dl := dataloader.New(csvDir, store)

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	Initialize(dl, rend, rm)

	cache.mu.Lock()
	cache.hash = ""
	cache.analysis = nil
	cache.cachedAt = time.Time{}
	cache.mu.Unlock()

	corruptFile := "whatif_corrupt.json"
	if err := os.WriteFile(filepath.Join(settingsDir, corruptFile), []byte("{not json"), 0644); err != nil {
		t.Fatalf("WriteFile corrupt: %v", err)
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: corruptFile, TransitionAge: 70},
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save settings with broken chain: %v", err)
	}

	cache.mu.Lock()
	cache.hash = ""
	cache.analysis = nil
	cache.cachedAt = time.Time{}
	cache.mu.Unlock()

	return rm, func() {}
}

// TestHandleWhatIf_BrokenChainRendersFullPageWithAlert is D4 / acceptance
// criterion 3: GET /whatif on a scenario whose chain link fails to load
// still renders the FULL page — the Scenario Chain card (with its Remove
// control, so the user can fix it) and the rest of the inputs column — with
// the failure surfaced as a role="alert" in the results area and NO
// projection figures. It must never fall back to the bare error fragment
// renderError produces on its own.
func TestHandleWhatIf_BrokenChainRendersFullPageWithAlert(t *testing.T) {
	_, cleanup := setupBrokenChainEnvWithRenderer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)

	body := w.Body.String()

	// The page still renders in full (not a bare fragment): it has the
	// document chrome the "base" layout always emits.
	if !strings.Contains(body, "<html") || !strings.Contains(body, "</html>") {
		t.Fatalf("expected a full HTML document, got a fragment (len=%d): %.300s", len(body), body)
	}

	// The inputs column, including the Scenario Chain card and its Remove
	// control for link 0, must still be present so the user can fix it.
	if !strings.Contains(body, "Scenario Chain") {
		t.Error("expected the Scenario Chain card in the rendered page")
	}
	if !strings.Contains(body, `hx-delete="/whatif/chain/0"`) {
		t.Error("expected a Remove control for chain link 0")
	}

	// The failure is surfaced as an alert in the results area, not a bare
	// error page.
	if !strings.Contains(body, `role="alert"`) {
		t.Error("expected role=\"alert\" somewhere in the rendered page")
	}
	if !strings.Contains(body, "Analysis failed") {
		t.Errorf("expected the analysis failure message in the page, got: %.2000s", body)
	}

	// No projection figures: the projection-chart card (which unconditionally
	// dereferences .Analysis.Projection) must not have rendered at all.
	if strings.Contains(body, "data-whatif-projection-card") {
		t.Error("expected NO projection chart card when analysis failed")
	}
}

// TestHandleWhatIfDeleteChainLink_FromBrokenPageThenAnalysesNormally is the
// rest of acceptance criterion 3: removing the broken step from the failed
// page succeeds (200), and the next GET /whatif analyses normally — no
// AnalysisError, real projection figures back.
func TestHandleWhatIfDeleteChainLink_FromBrokenPageThenAnalysesNormally(t *testing.T) {
	_, cleanup := setupBrokenChainEnvWithRenderer(t)
	defer cleanup()

	// Confirm the page is broken first (fixture sanity).
	w1 := httptest.NewRecorder()
	handleWhatIf(w1, httptest.NewRequest("GET", "/whatif", nil))
	if !strings.Contains(w1.Body.String(), "Analysis failed") {
		t.Fatalf("fixture defect: expected the page to start broken: %.500s", w1.Body.String())
	}

	// Remove the broken link (index 0) — the "Remove" button's own request.
	wDel := httptest.NewRecorder()
	reqDel := chiRequest("DELETE", "/whatif/chain/0", nil, map[string]string{"index": "0"})
	handleWhatIfDeleteChainLink(wDel, reqDel)
	if wDel.Code != 200 {
		t.Fatalf("delete status = %d, want 200. body: %s", wDel.Code, wDel.Body.String())
	}

	// The next GET /whatif must analyse normally: no AnalysisError, real
	// figures back.
	w2 := httptest.NewRecorder()
	handleWhatIf(w2, httptest.NewRequest("GET", "/whatif", nil))
	if w2.Code != 200 {
		t.Fatalf("status after fix = %d, want 200. body: %.500s", w2.Code, w2.Body.String())
	}
	body2 := w2.Body.String()
	if strings.Contains(body2, "Analysis failed") {
		t.Errorf("expected no analysis-failed alert after removing the broken link: %.500s", body2)
	}
	if !strings.Contains(body2, "data-whatif-projection-card") {
		t.Error("expected the projection chart card back once analysis succeeds")
	}
}

// TestHandleWhatIf_ChainGuardrailOnToOffDoesNotPanic is the full-stack
// counterpart to the engine-level TestChainGuardrailsOnToOff (D3): a real
// primary scenario with guardrails enabled, chained (via the production
// age-based ResolveChainTransition hook, not a test stub) into a scenario
// with guardrails left off. Before the WS3 fix this panicked inside
// GuardrailState.Evaluate (nil cfg) and — even after being recovered by
// runFastRecovered — used to render only the bare error fragment (D4).
// After the fix it must render normally: 200, no AnalysisError, a real
// projection.
func TestHandleWhatIf_ChainGuardrailOnToOffDoesNotPanic(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	// Persist the default whatif.json first: Load() on a not-yet-existing
	// file returns in-memory defaults without writing them, so SwitchScenario
	// back to "whatif.json" below would otherwise fail with file-not-found
	// (see TestRevision_BumpsOnSwitchAndCreateScenario for the same fixture
	// requirement).
	defaults, err := rm.Load()
	if err != nil {
		t.Fatalf("Load defaults: %v", err)
	}
	if err := rm.Save(defaults); err != nil {
		t.Fatalf("Save defaults: %v", err)
	}

	if _, err := rm.CreateScenario("No Guardrails Step"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	scenarios, err := rm.ListScenarios()
	if err != nil {
		t.Fatalf("ListScenarios: %v", err)
	}
	var targetFile string
	for _, s := range scenarios {
		if s.Name == "No Guardrails Step" {
			targetFile = s.Filename
		}
	}
	if targetFile == "" {
		t.Fatalf("fixture defect: target=%q scenarios=%#v", targetFile, scenarios)
	}

	// CreateScenario switches the active scenario to the new one; switch
	// back to the primary (whatif.json, active by default before any
	// CreateScenario call) so we mutate and chain FROM it.
	if err := rm.SwitchScenario("whatif.json"); err != nil {
		t.Fatalf("SwitchScenario primary: %v", err)
	}
	primary, err := rm.Load()
	if err != nil {
		t.Fatalf("Load primary: %v", err)
	}
	primary.PortfolioValue = 500_000
	primary.MonthlyLivingExpenses = 4_000
	primary.InvestmentReturn = -10
	primary.ProjectionYears = 6
	primary.Guardrails = &models.GuardrailConfig{
		Enabled: true, FloorDropPct: 1, FloorCutPct: 10,
		CeilingRisePct: 1000, CeilingRaisePct: 0,
		MinSpendingPct: 40, MaxSpendingPct: 200,
	}
	// CurrentAge 65 + 2 years = transition at age 67, well inside the
	// 6-year projection.
	primary.ScenarioChain = []models.ScenarioChainLink{{ScenarioFilename: targetFile, TransitionAge: primary.CurrentAge + 2}}
	if err := rm.Save(primary); err != nil {
		t.Fatalf("Save primary: %v", err)
	}
	// The linked scenario keeps DefaultWhatIfSettings' Guardrails (nil) —
	// guardrails off for that step, which used to be the nil-cfg panic.

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (no panic/analysis error). body: %.2000s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "Analysis failed") {
		t.Errorf("expected no analysis failure with the WS3 fix: %.2000s", body)
	}
	if !strings.Contains(body, "data-whatif-projection-card") {
		t.Error("expected the projection chart card to render")
	}
	// Acceptance criterion 1 names this table explicitly: "Year-by-Year
	// renders".
	if !strings.Contains(body, "Year-by-Year Projection") {
		t.Error("expected the Year-by-Year Projection table to render")
	}
}

// ws3GuardrailSummaryRe extracts the "whatif-guardrail-events" chart
// caption paragraph (projection-chart.html: `<p
// id="projection-guardrail-summary">Guardrails: {{EventsLine}}
// {{NextLine}} See Guardrail Events on the Risk tab.</p>`) verbatim — no
// nested tags sit between the anchors, so the raw HTML capture IS the
// caption text.
var ws3GuardrailSummaryRe = regexp.MustCompile(`(?s)<p id="projection-guardrail-summary"[^>]*>(.*?)</p>`)

// ws3GuardrailEventRe extracts one "whatif-guardrail-events" row
// (guardrails.html) per match: the year, cut/raise, its percentage change,
// its percentage of plan, and the before/after monthly-spending dollar
// figures — everything that distinguishes one GuardrailEvent from another
// on screen. "Year N:" appears nowhere else on the page (the Year-by-Year
// table's year cells are bare numbers), so this is unambiguous without
// needing to bound the surrounding block.
var ws3GuardrailEventRe = regexp.MustCompile(`(?s)Year (\d+):.*?(Cut|Raise)(?: by (\d+)%)?.*?\((\d+)% of plan\).*?\$([\d,]+\.\d{2})/mo\s*→\s*\$([\d,]+\.\d{2})/mo`)

func ws3ExtractGuardrailSummary(t *testing.T, body string) string {
	t.Helper()
	m := ws3GuardrailSummaryRe.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func ws3ExtractGuardrailEventRows(t *testing.T, body string) [][]string {
	t.Helper()
	return ws3GuardrailEventRe.FindAllStringSubmatch(body, -1)
}

// TestHandleWhatIf_GuardrailEventsAndChartCaptionMatchUnchainedWhenPrimaryFloorBinds
// is the handler-level half of the WS3 attempt-3 checker-tests gap
// (checker-tests, 2026-09-24): primary A's real spending floor (3,000)
// binds starting exactly at the transition year; A chains into step Z,
// whose OWN guardrail config is very different and whose OWN floor (1,000)
// is lower. Per D3', Z's config is never consulted, so the RENDERED
// Guardrail Events list and the GuardrailChartSummary caption — both of
// which read the identical projection.GuardrailEvents / YearlySummaries
// fields the engine-level TestChainGuardrailEventsMatchUnchainedWhenPrimaryFloorBinds
// pins directly — must be byte-identical between the chained page and the
// SAME primary run unchained.
func TestHandleWhatIf_GuardrailEventsAndChartCaptionMatchUnchainedWhenPrimaryFloorBinds(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	// The primary: guardrails enabled, a real floor (3,000) that the
	// guaranteed yearly cuts push spending below starting exactly at the
	// transition year (3 years in, on a 6-year projection). Saved BEFORE
	// CreateScenario so the cloned step inherits these SAME financial
	// parameters (portfolio, living expenses, return, horizon) — isolating
	// the comparison to the guardrail config alone, the way the engine-level
	// TestChainGuardrailEventsMatchUnchainedWhenPrimaryFloorBinds fixture
	// does (ws3ChainFixture reused verbatim for both steps).
	primary, err := rm.Load()
	if err != nil {
		t.Fatalf("Load primary: %v", err)
	}
	primary.PortfolioValue = 500_000
	primary.MonthlyLivingExpenses = 4_000
	primary.InvestmentReturn = -10
	primary.ProjectionYears = 6
	primary.Guardrails = &models.GuardrailConfig{
		Enabled: true, FloorDropPct: 1, FloorCutPct: 10,
		CeilingRisePct: 1000, CeilingRaisePct: 0,
		MinSpendingPct: 40, MaxSpendingPct: 200,
		MinMonthlySpendingReal: 3000,
	}
	if err := rm.Save(primary); err != nil {
		t.Fatalf("Save primary (unchained): %v", err)
	}

	if _, err := rm.CreateScenario("Different Floor Step"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	scenarios, err := rm.ListScenarios()
	if err != nil {
		t.Fatalf("ListScenarios: %v", err)
	}
	var targetFile string
	for _, s := range scenarios {
		if s.Name == "Different Floor Step" {
			targetFile = s.Filename
		}
	}
	if targetFile == "" {
		t.Fatalf("fixture defect: target=%q scenarios=%#v", targetFile, scenarios)
	}

	// The chained-into step: same financial parameters as the primary
	// (inherited from CreateScenario above), but guardrails enabled with a
	// VERY different config and a LOWER floor (1,000) than the primary's —
	// must never be consulted (D3').
	target, err := rm.Load() // CreateScenario already switched the active scenario to targetFile.
	if err != nil {
		t.Fatalf("Load target: %v", err)
	}
	target.Guardrails = &models.GuardrailConfig{
		Enabled: true, FloorDropPct: 2, FloorCutPct: 30,
		CeilingRisePct: 50, CeilingRaisePct: 1,
		MinSpendingPct: 40, MaxSpendingPct: 200,
		MinMonthlySpendingReal: 1000,
	}
	if err := rm.Save(target); err != nil {
		t.Fatalf("Save target: %v", err)
	}

	if err := rm.SwitchScenario("whatif.json"); err != nil {
		t.Fatalf("SwitchScenario primary: %v", err)
	}

	// Render UNCHAINED first — this is the control.
	wUnchained := httptest.NewRecorder()
	handleWhatIf(wUnchained, httptest.NewRequest("GET", "/whatif", nil))
	if wUnchained.Code != 200 {
		t.Fatalf("unchained status = %d, want 200. body: %.500s", wUnchained.Code, wUnchained.Body.String())
	}
	unchainedBody := wUnchained.Body.String()
	unchainedEvents := ws3ExtractGuardrailEventRows(t, unchainedBody)
	unchainedSummary := ws3ExtractGuardrailSummary(t, unchainedBody)
	if len(unchainedEvents) == 0 {
		t.Fatalf("fixture defect: expected at least one rendered guardrail event, got none: %.2000s", unchainedBody)
	}
	if unchainedSummary == "" {
		t.Fatalf("fixture defect: expected a rendered GuardrailChartSummary caption, got none: %.2000s", unchainedBody)
	}

	// Now chain the primary into the different-floor step and render again.
	primary.ScenarioChain = []models.ScenarioChainLink{{ScenarioFilename: targetFile, TransitionAge: primary.CurrentAge + 3}}
	if err := rm.Save(primary); err != nil {
		t.Fatalf("Save primary (chained): %v", err)
	}

	wChained := httptest.NewRecorder()
	handleWhatIf(wChained, httptest.NewRequest("GET", "/whatif", nil))
	if wChained.Code != 200 {
		t.Fatalf("chained status = %d, want 200. body: %.500s", wChained.Code, wChained.Body.String())
	}
	chainedBody := wChained.Body.String()
	chainedEvents := ws3ExtractGuardrailEventRows(t, chainedBody)
	chainedSummary := ws3ExtractGuardrailSummary(t, chainedBody)

	if len(chainedEvents) != len(unchainedEvents) {
		t.Fatalf("chained rendered %d guardrail events, want %d (the unchained-primary control)\nchained=%#v\nunchained=%#v", len(chainedEvents), len(unchainedEvents), chainedEvents, unchainedEvents)
	}
	for i := range unchainedEvents {
		if strings.Join(chainedEvents[i], "|") != strings.Join(unchainedEvents[i], "|") {
			t.Errorf("event row %d = %#v, want %#v (chained step's own guardrail config/floor must be ignored, D3')", i, chainedEvents[i], unchainedEvents[i])
		}
	}
	if chainedSummary != unchainedSummary {
		t.Errorf("GuardrailChartSummary caption =\n%q\nwant (unchained-primary control):\n%q", chainedSummary, unchainedSummary)
	}
}
