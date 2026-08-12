package templates

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/web"
)

// phasesEnabledSettings returns what-if settings with phase-based spending on,
// which is the branch that suppresses the decline-rate slider and the spending
// preview panel in whatif-rate-assumptions.
func phasesEnabledSettings() *models.WhatIfSettings {
	s := models.DefaultWhatIfSettings()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  models.DefaultSpendingPhases(),
	}
	return s
}

// TestRenderRateAssumptions_PhasesEnabledOmitsPreviewPanel pins the render-side
// precondition of the null-deref bug: with phases enabled the preview panel and
// the decline slider are not emitted, yet the inflation slider still calls
// updateSpendingPreview() on every input event.
func TestRenderRateAssumptions_PhasesEnabledOmitsPreviewPanel(t *testing.T) {
	r := newWhatIfRenderer(t)

	html, err := r.RenderToString("whatif-rate-assumptions", map[string]any{
		"Settings": phasesEnabledSettings(),
	})
	if err != nil {
		t.Fatalf("RenderToString() error: %v", err)
	}
	out := collapse(html)

	for _, id := range []string{"spending-preview-panel", "spending-decline-slider"} {
		if strings.Contains(out, `id="`+id+`"`) {
			t.Errorf("expected #%s to be absent when spending phases are enabled", id)
		}
	}
	// The caller survives the branch that removes the panel: that asymmetry is
	// exactly what the guard in updateSpendingPreview has to tolerate.
	if !strings.Contains(out, "updateSpendingPreview()") {
		t.Fatal("expected the inflation slider to still call updateSpendingPreview()")
	}
}

// scriptSourceRE pulls the static JS out of the whatif-spending-preview-scripts
// template. The block contains no template actions, so it runs as-is.
var scriptSourceRE = regexp.MustCompile(`(?s)\{\{define "whatif-spending-preview-scripts"\}\}\s*<script>(.*?)</script>\s*\{\{end\}\}`)

// TestUpdateSpendingPreview_NoPanelDoesNotThrow executes the real
// updateSpendingPreview against a DOM that omits the preview panel — the
// phases-enabled render. Before the null guard this threw
// "Cannot read properties of null (reading 'classList')" on every input event
// from the inflation and monthly-living-expenses sliders.
func TestUpdateSpendingPreview_NoPanelDoesNotThrow(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; skipping browser-script execution test")
	}

	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}
	raw, err := fs.ReadFile(templatesFS, "components/whatif/rate-assumptions.html")
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	m := scriptSourceRE.FindSubmatch(raw)
	if m == nil {
		t.Fatal("could not extract whatif-spending-preview-scripts source")
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "script.js")
	if err := os.WriteFile(scriptPath, m[1], 0o600); err != nil {
		t.Fatalf("WriteFile(script) error: %v", err)
	}
	harnessPath := filepath.Join(dir, "harness.js")
	if err := os.WriteFile(harnessPath, []byte(previewHarness), 0o600); err != nil {
		t.Fatalf("WriteFile(harness) error: %v", err)
	}

	tests := []struct {
		name string
		mode string
		want string
	}{
		// The bug: every input event on the inflation or living-expenses slider
		// threw once phase-based spending removed the panel from the render.
		{name: "panel absent", mode: "absent", want: "OK skipped"},
		// The guard must not disable the feature when the panel is there.
		{name: "panel visible", mode: "visible", want: "OK rows=6"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := exec.Command(node, harnessPath, scriptPath, tc.mode).CombinedOutput()
			if err != nil {
				t.Fatalf("updateSpendingPreview() failed with panel %s:\n%s", tc.mode, out)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("want %q in harness output, got:\n%s", tc.want, out)
			}
		})
	}
}

// previewHarness stubs just enough DOM for updateSpendingPreview. Mode
// "absent" models the phases-enabled page (inflation and living-expenses inputs
// present, panel and decline slider gone); mode "visible" models the
// phases-disabled page with the panel open.
const previewHarness = `
const fs = require('fs');
const vm = require('vm');

const mode = process.argv[3];

function makeEl(value, hidden) {
  const el = {
    value,
    textContent: '',
    innerHTML: '',
    children: [],
    classList: {
      contains: (c) => c === 'hidden' && hidden === true,
      add() { hidden = true; },
      remove() { hidden = false; },
    },
  };
  el.appendChild = (child) => el.children.push(child);
  return el;
}

const present = {
  'inflation-rate-slider': makeEl('2.5'),
  'monthly_living_expenses_input': makeEl('5000'),
};
if (mode === 'visible') {
  present['spending-preview-panel'] = makeEl('', false);
  present['spending-decline-slider'] = makeEl('1.0');
  present['net-rate-display'] = makeEl('');
  present['spending-preview-body'] = makeEl('');
}

const document = {
  getElementById: (id) => (id in present ? present[id] : null),
  querySelectorAll: () => [],
  createElement: () => makeEl(''),
  addEventListener: () => {},
};

const sandbox = { document, window: {}, console };
sandbox.window.document = document;
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync(process.argv[2], 'utf8'), sandbox);

sandbox.updateSpendingPreview();

if (mode === 'visible') {
  const body = present['spending-preview-body'];
  console.log('OK rows=' + body.children.length,
              'netRate=' + present['net-rate-display'].textContent);
} else {
  console.log('OK skipped');
}
`
