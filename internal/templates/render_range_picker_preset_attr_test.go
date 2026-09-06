package templates

import (
	"io/fs"
	"strings"
	"testing"

	"budget2/web"
)

// The range-picker preset buttons must carry a REAL data-* attribute the page
// JS can read (btn.dataset.preset / btn.dataset.months). Emitting the attribute
// NAME via a template action ({{$.PresetAttr}}="...") makes html/template write
// the literal "ZgotmplZ" in its place, which silently broke every preset button
// sitewide. Fixed by emitting a static-literal attribute name per known
// PresetAttr value. This pins that the real attribute renders and ZgotmplZ does
// not, for both attribute conventions.
func TestRangePickerPresetButtonsCarryRealDataAttr(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	r, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}

	cases := []struct {
		layout, presetAttr, wantAttr string
	}{
		{"inline", "data-months", `data-months="6"`},
		{"stacked", "data-months", `data-months="6"`},
		{"inline", "data-preset", `data-preset="6"`},
		{"stacked", "data-preset", `data-preset="6"`},
	}
	for _, c := range cases {
		out, err := r.RenderToString("shared/range-picker", map[string]any{
			"Layout": c.layout, "StartID": "s", "StartName": "start", "StartValue": "2025-01-01",
			"EndID": "e", "EndName": "end", "EndValue": "2025-03-01",
			"MinDate": "2024-01-01", "MaxDate": "2025-12-31",
			"PresetAttr": c.presetAttr, "PresetClass": "date-range-btn",
			"PresetActiveClass": "a", "PresetInactiveClass": "b",
			"Presets": []map[string]any{{"Value": "6", "Label": "6M", "Active": true}},
		})
		if err != nil {
			t.Fatalf("render(%s/%s): %v", c.layout, c.presetAttr, err)
		}
		if strings.Contains(out, "ZgotmplZ") {
			t.Errorf("%s/%s: preset button rendered ZgotmplZ (dead button); the attribute name must be a static literal, not an interpolated action:\n%s", c.layout, c.presetAttr, out)
		}
		if !strings.Contains(out, c.wantAttr) {
			t.Errorf("%s/%s: preset button missing %q (JS reads btn.dataset to get the preset):\n%s", c.layout, c.presetAttr, c.wantAttr, out)
		}
	}
}
