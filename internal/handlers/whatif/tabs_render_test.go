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
	if strings.Contains(out, `data-scenario="<no value>"`) {
		t.Errorf("partial render emitted missing ActiveFilename sentinel in data-scenario")
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

func TestWhatIfSettings_Groups(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		t.Fatalf("runAnalysisWithCache: %v", err)
	}
	out, err := renderer.RenderToString("whatif-content", map[string]any{
		"Settings": settings, "Analysis": analysis,
		"Verdict": BuildVerdict(analysis, settings),
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
}

func TestWhatIfOverviewKPIs_Render(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		t.Fatalf("runAnalysisWithCache: %v", err)
	}
	out, err := renderer.RenderToString("whatif-overview-kpis", map[string]any{
		"Settings": settings, "Analysis": analysis,
		"Verdict": BuildVerdict(analysis, settings),
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	// Gap tile label is "Monthly Gap" today, or "Gap @ Yr N" when a steady-state
	// year is in view (default settings have one) — accept either.
	if !strings.Contains(out, "Monthly Gap") && !strings.Contains(out, "Gap @ Yr") {
		t.Errorf("expected a gap tile (\"Monthly Gap\" or \"Gap @ Yr N\"); got: %s", truncate(out, 800))
	}
	for _, want := range []string{"Success", "End Balance", `class="num`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in KPI tiles; got: %s", want, truncate(out, 800))
		}
	}
}
