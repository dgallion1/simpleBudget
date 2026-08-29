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
		path    string
		handler http.HandlerFunc
	}{
		{"calculate", "/whatif/calculate", handleWhatIfCalculate},
		{"sync-apply", "/whatif/sync/apply", handleWhatIfSyncApply},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", tc.path, nil)
			tc.handler(w, req)

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
