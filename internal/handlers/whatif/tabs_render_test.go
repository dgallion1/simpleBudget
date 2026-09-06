package whatif

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestWhatIfResults_TabStructure(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	analysis, err := runAnalysisWithCache(context.Background(), settings)
	if err != nil {
		t.Fatalf("runAnalysisWithCache: %v", err)
	}
	partialData := buildResultsPartialData(settings, analysis, nil)
	out, err := renderer.RenderToString("whatif-results", partialData)
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	if !strings.Contains(out, `id="whatif-verdict-bar"`) {
		t.Errorf("expected verdict bar in results")
	}
	if !strings.Contains(out, `id="whatif-tabs"`) {
		t.Errorf("expected tab container")
	}
	if !strings.Contains(out, `data-scenario="whatif.json"`) {
		t.Errorf("expected partial render to preserve active scenario in data-scenario; got: %s", truncate(out, 800))
	}
	if strings.Contains(out, `data-scenario=""`) {
		t.Errorf("partial render emitted empty data-scenario (missing ActiveFilename); per-scenario tab persistence would fall back to the 'default' key")
	}
	for _, tab := range []string{`data-wf-tab="overview"`, `data-wf-tab="cashflow"`, `data-wf-tab="risk"`, `data-wf-tab="taxes"`, `data-wf-tab="strategies"`} {
		if !strings.Contains(out, tab) {
			t.Errorf("expected tab button %q", tab)
		}
	}
	for _, panel := range []string{`data-wf-panel="overview"`, `data-wf-panel="cashflow"`, `data-wf-panel="risk"`, `data-wf-panel="taxes"`, `data-wf-panel="strategies"`} {
		if !strings.Contains(out, panel) {
			t.Errorf("expected panel %q", panel)
		}
	}
	if !strings.Contains(out, "Monte Carlo Simulation") {
		t.Errorf("expected Monte Carlo section to render inside Risk panel")
	}
}

func TestWhatIfResults_TabAccessibilityRelationships(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	analysis, err := runAnalysisWithCache(context.Background(), settings)
	if err != nil {
		t.Fatalf("runAnalysisWithCache: %v", err)
	}
	out, err := renderer.RenderToString("whatif-results", buildResultsPartialData(settings, analysis, nil))
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	for i, name := range []string{"overview", "cashflow", "risk", "taxes", "strategies"} {
		tabID := "wf-tab-" + name
		panelID := "wf-panel-" + name
		selected := "false"
		tabindex := "-1"
		if i == 0 {
			selected = "true"
			tabindex = "0"
		}
		tab := `id="` + tabID + `" role="tab" aria-controls="` + panelID + `" aria-selected="` + selected + `" tabindex="` + tabindex + `" data-wf-tab="` + name + `"`
		if !strings.Contains(out, tab) {
			t.Errorf("tab %q missing accessible identity/state: %s", name, tab)
		}
		panel := `id="` + panelID + `" role="tabpanel" aria-labelledby="` + tabID + `" data-wf-panel="` + name + `"`
		if !strings.Contains(out, panel) {
			t.Errorf("panel %q missing relationship to its tab: %s", name, panel)
		}
		if count := strings.Count(out, `id="`+tabID+`"`); count != 1 {
			t.Errorf("tab id %q appears %d times, want 1", tabID, count)
		}
		if count := strings.Count(out, `id="`+panelID+`"`); count != 1 {
			t.Errorf("panel id %q appears %d times, want 1", panelID, count)
		}
	}
}

// TestWhatIfCalculateAndSync_PreserveActiveScenario is a regression test for
// the calculate/sync handlers omitting ActiveFilename from the partial data:
// the whatif-results template then rendered data-scenario="" and
// whatif-tabs.js fell back to the shared 'default' localStorage key, breaking
// per-scenario tab persistence after every Calculate or Sync.
func TestWhatIfCalculateAndSync_PreserveActiveScenario(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	cases := []struct {
		name    string
		handler http.HandlerFunc
		req     func(t *testing.T) *http.Request
	}{
		{"calculate", handleWhatIfCalculate, func(t *testing.T) *http.Request {
			return httptest.NewRequest("POST", "/whatif/calculate", nil)
		}},
		{"sync-apply", handleWhatIfSyncApply, func(t *testing.T) *http.Request {
			// The apply guard requires expected_scenario/plan_hash from a
			// real preview — a plain nil-body POST now gets 400, not 200.
			previewW := httptest.NewRecorder()
			handleWhatIfSync(previewW, httptest.NewRequest("POST", "/whatif/sync", nil))
			if previewW.Code != http.StatusOK {
				t.Fatalf("preview status = %d, want 200", previewW.Code)
			}
			return syncApplyRequest(extractSyncGuardFields(t, previewW.Body.String()))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.handler(w, tc.req(t))

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", w.Code, truncate(w.Body.String(), 300))
			}
			out := w.Body.String()
			if !strings.Contains(out, `data-scenario="whatif.json"`) {
				t.Errorf("expected %s response to carry data-scenario=\"whatif.json\"; got: %s", tc.name, truncate(out, 800))
			}
			if strings.Contains(out, `data-scenario=""`) {
				t.Errorf("%s response emitted empty data-scenario (ActiveFilename missing from partial data)", tc.name)
			}
		})
	}
}

func TestWhatIfSettings_Groups(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	analysis, err := runAnalysisWithCache(context.Background(), settings)
	if err != nil {
		t.Fatalf("runAnalysisWithCache: %v", err)
	}
	out, err := renderer.RenderToString("whatif-content", map[string]any{
		"Settings": settings, "Analysis": analysis,
		"Verdict":   BuildVerdict(analysis, settings),
		"Scenarios": nil, "ActiveFilename": "whatif.json", "Findings": nil,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	for _, label := range []string{"Money In / Out", "Assumptions", "Strategies"} {
		if !strings.Contains(out, label) {
			t.Errorf("expected settings group label %q", label)
		}
	}
	for _, attr := range []string{`data-wf-collapse="money"`, `data-wf-collapse="assumptions"`, `data-wf-collapse="strategies"`} {
		if !strings.Contains(out, attr) {
			t.Errorf("expected collapsible group %q", attr)
		}
	}
	if !strings.Contains(out, `id="whatif-settings-col" class="order-2 lg:order-none lg:col-span-2 space-y-4"`) {
		t.Errorf("settings column must order after results on small screens")
	}
	if !strings.Contains(out, `class="order-1 lg:order-none lg:col-span-4 space-y-4" id="whatif-results"`) {
		t.Errorf("results column must order before settings on small screens")
	}
}

func TestWhatIfSettings_CollapseAccessibilityState(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	analysis, err := runAnalysisWithCache(context.Background(), settings)
	if err != nil {
		t.Fatalf("runAnalysisWithCache: %v", err)
	}
	out, err := renderer.RenderToString("whatif-content", map[string]any{
		"Settings": settings, "Analysis": analysis,
		"Verdict": BuildVerdict(analysis, settings), "ActiveFilename": "whatif.json",
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	for _, name := range []string{"money", "assumptions", "strategies"} {
		toggleID := "wf-collapse-" + name + "-toggle"
		bodyID := "wf-collapse-" + name + "-body"
		toggle := `id="` + toggleID + `" aria-expanded="true" aria-controls="` + bodyID + `" data-wf-collapse-toggle`
		if !strings.Contains(out, toggle) {
			t.Errorf("collapse %q missing expanded state and controlled body: %s", name, toggle)
		}
		body := `id="` + bodyID + `" role="region" aria-labelledby="` + toggleID + `" data-wf-collapse-body`
		if !strings.Contains(out, body) {
			t.Errorf("collapse body %q missing stable accessible relationship: %s", name, body)
		}
	}
}

func TestWhatIfProjectionChart_ResponsiveControlLayout(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	analysis, err := runAnalysisWithCache(context.Background(), settings)
	if err != nil {
		t.Fatalf("runAnalysisWithCache: %v", err)
	}
	out, err := renderer.RenderToString("whatif-results", buildResultsPartialData(settings, analysis, nil))
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	for _, className := range []string{
		`class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between mb-4"`,
		`class="flex flex-wrap items-center gap-2"`,
		`class="inline-flex flex-wrap shrink-0 rounded-md shadow-sm border border-gray-200 dark:border-gray-600"`,
	} {
		if !strings.Contains(out, className) {
			t.Errorf("projection chart missing responsive control class %s", className)
		}
	}
}
