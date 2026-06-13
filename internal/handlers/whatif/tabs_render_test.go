package whatif

import (
	"strings"
	"testing"

	"budget2/internal/models"
)

func TestWhatIfResults_TabStructure(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		t.Fatalf("runAnalysisWithCache: %v", err)
	}
	out, err := renderer.RenderToString("whatif-results", map[string]any{
		"Settings": settings,
		"Analysis": analysis,
		"Verdict":  BuildVerdict(analysis, settings),
		"Findings": nil,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	if !strings.Contains(out, `id="whatif-verdict-bar"`) {
		t.Errorf("expected verdict bar in results")
	}
	if !strings.Contains(out, `id="whatif-tabs"`) {
		t.Errorf("expected tab container")
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
