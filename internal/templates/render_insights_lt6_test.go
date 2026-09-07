package templates

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/web"
)

// LT6: the supporting section (#insights-supporting) is now behind five
// tabs. Server markup must carry no `hidden` attribute and no
// `aria-selected="true"` — with JS off all five panels show (point 16) —
// and the tab/panel ARIA wiring must be internally consistent.
func TestRenderInsightsSupportingTabs(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderer.RenderToString("insights-content", map[string]any{
		"PaceVerdict": map[string]any{"Health": models.HealthNeutral, "HasData": false},
		"MinDate":     "2024-01-01", "MaxDate": "2024-12-31",
		"StartDate": "2024-01-01", "EndDate": "2024-12-31",
		"Insights": models.InsightsData{
			CategoryTrends: []models.CategoryTrend{{
				Category: "Groceries", CurrentAmount: 120.50, PreviousAmount: 100.00,
				ChangePercent: 20.5, ChangeAmount: 20.50, Direction: "up",
				Change: models.ChangeCell{Kind: "dollar", Amount: 20.50, Percent: 20.5, Direction: "up"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	tabCount := strings.Count(out, `role="tab"`)
	if tabCount != 5 {
		t.Errorf("role=\"tab\" count = %d, want 5", tabCount)
	}
	panelCount := strings.Count(out, `role="tabpanel"`)
	if panelCount != 5 {
		t.Errorf("role=\"tabpanel\" count = %d, want 5", panelCount)
	}

	tabRe := regexp.MustCompile(`<button[^>]*\brole="tab"[^>]*>`)
	panelRe := regexp.MustCompile(`<div[^>]*\brole="tabpanel"[^>]*>`)
	idRe := regexp.MustCompile(`\bid="([^"]+)"`)
	controlsRe := regexp.MustCompile(`aria-controls="([^"]+)"`)
	labelledbyRe := regexp.MustCompile(`aria-labelledby="([^"]+)"`)

	panelIDs := map[string]bool{}
	for _, m := range panelRe.FindAllString(out, -1) {
		id := idRe.FindStringSubmatch(m)
		if len(id) != 2 {
			t.Fatalf("tabpanel missing id: %s", m)
		}
		panelIDs[id[1]] = true
	}
	if len(panelIDs) != 5 {
		t.Errorf("distinct tabpanel ids = %d, want 5 (%v)", len(panelIDs), panelIDs)
	}

	tabIDs := map[string]bool{}
	for _, m := range tabRe.FindAllString(out, -1) {
		id := idRe.FindStringSubmatch(m)
		if len(id) != 2 {
			t.Fatalf("tab missing id: %s", m)
		}
		tabIDs[id[1]] = true

		controls := controlsRe.FindStringSubmatch(m)
		if len(controls) != 2 {
			t.Fatalf("tab %s missing aria-controls", id[1])
		}
		if !panelIDs[controls[1]] {
			t.Errorf("tab %s aria-controls=%q does not match any panel id", id[1], controls[1])
		}
	}
	if len(tabIDs) != 5 {
		t.Errorf("distinct tab ids = %d, want 5 (%v)", len(tabIDs), tabIDs)
	}

	for _, m := range panelRe.FindAllString(out, -1) {
		id := idRe.FindStringSubmatch(m)[1]
		labelledby := labelledbyRe.FindStringSubmatch(m)
		if len(labelledby) != 2 {
			t.Fatalf("panel %s missing aria-labelledby", id)
		}
		if !tabIDs[labelledby[1]] {
			t.Errorf("panel %s aria-labelledby=%q does not match any tab id", id, labelledby[1])
		}
	}

	// "hidden" as a boolean attribute must not appear; pre-existing
	// aria-hidden="true" (decorative SVGs) and the date-filter's
	// type="hidden" input are legitimate and excluded from the check.
	sanitized := strings.ReplaceAll(out, `aria-hidden="true"`, "")
	sanitized = strings.ReplaceAll(sanitized, `type="hidden"`, "")
	if strings.Contains(sanitized, "hidden") {
		t.Error(`server output must carry no "hidden" attribute (point 16: all panels visible with JS off)`)
	}
	if strings.Contains(out, `aria-selected="true"`) {
		t.Error(`server output must carry no aria-selected="true" (point 16: JS, not the server, picks the active tab)`)
	}

	if !strings.Contains(out, `id="chart-trends"`) {
		t.Fatal(`missing "#chart-trends"`)
	}
	if !strings.Contains(out, `hx-trigger="load"`) {
		t.Error(`#chart-trends must still carry hx-trigger="load"`)
	}
}
