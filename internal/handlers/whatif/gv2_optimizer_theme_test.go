package whatif

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/web"
)

// GV2 attempt 3 (ruling GV-2026-09-09m): checker-second's FAIL was that the
// optimizer's "Base case" preview (guardrail_optimizer.html's graphTheme /
// graphTable) recolours buildProjectionChartData's traces by INDEX instead
// of by meta.tone/meta.tones, overwrites the markers trace's per-point
// marker.color ARRAY with a scalar, hardcodes height 340, and mislabels the
// budget-panel table columns "Portfolio balance". This file is the
// permanent regression: it extracts the REAL graphTheme/graphTable source
// out of the template (they live inside the page's IIFE and are not
// reachable any other way) plus the REAL charts.js (loaded whole, like
// internal/templates/render_spending_preview_test.go's established
// node-harness precedent), and runs them under Node against a payload the
// REAL handleGuardrailOptimizerGraph handler produced.

// gv2ExtractFunction returns the verbatim source of `function <name>(...)
// {...}` from src, using brace-depth counting from the first `{` after the
// signature (none of the functions this test extracts contain a literal
// brace inside a string or comment, so naive counting is exact).
func gv2ExtractFunction(t *testing.T, src, name string) string {
	t.Helper()
	marker := "function " + name + "("
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatalf("function %s not found in source", name)
	}
	relBrace := strings.IndexByte(src[start:], '{')
	if relBrace < 0 {
		t.Fatalf("no opening brace for function %s", name)
	}
	braceStart := start + relBrace
	depth := 0
	for i := braceStart; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		}
	}
	t.Fatalf("unbalanced braces extracting function %s", name)
	return ""
}

// gv2OptimizerThemeFixtureSettings mirrors gv2ChartSettings (gv2_chart_test.go):
// guaranteed non-empty GuardrailEvents (both a cut and the fixture the
// existing permanent tests already validate against), plus an income source
// named "Pension" so buildProjectionChartData also emits a "Key events"
// trace — needed to exercise the untouched index-based (non-meta) coloring
// path alongside the tone-bearing traces.
func gv2OptimizerThemeFixtureSettings(s *models.WhatIfSettings) {
	s.PortfolioValue = 500000
	s.MonthlyLivingExpenses = 12000
	s.MonthlyHealthcare = 0
	s.MonthlyPropertyTax = 0
	s.InflationRate = 2.5
	s.SpendingDeclineRate = 0
	s.InvestmentReturn = 1.0
	s.ProjectionYears = 8
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{Enabled: false}
	s.IncomeSources = []models.IncomeSource{{ID: "gv2-pension", Name: "Pension", Amount: 30000, Type: models.IncomeDelayed, StartMonth: 36}}
}

// gv2OptimizerThemeGraphPayload drives the REAL handleGuardrailOptimizerGraph
// handler (the same one the "View graph" button calls) for a candidate
// carrying the given guardrail config, and returns the decoded JSON payload
// exactly as the browser receives it -- the same shape graphTheme/graphTable
// consume (payload.chart, payload.display_dollars).
func gv2OptimizerThemeGraphPayload(t *testing.T, label string, guardrails *models.GuardrailConfig) map[string]interface{} {
	t.Helper()
	rm, cleanup := setupTestEnv(t)
	defer cleanup()
	s, _ := rm.Load()
	gv2OptimizerThemeFixtureSettings(s)
	if err := rm.Save(s); err != nil {
		t.Fatal(err)
	}
	s, revision, _ := rm.LoadContextWithRevision(context.Background())
	raw, _ := json.Marshal(s)
	candidate := models.GuardrailOptimizerCandidate{ID: label, Guardrails: guardrails, Qualifies: false}
	id := "gv2-optimizer-theme-" + label
	guardrailPreviews.Lock()
	guardrailPreviews.entries[id] = &guardrailPreview{manager: rm, scenario: rm.ActiveFilename(), revision: revision, fingerprint: sha256.Sum256(raw), expires: time.Now().Add(time.Minute), graphs: map[string]models.GuardrailOptimizerCandidate{"graph-only": candidate}}
	guardrailPreviews.Unlock()
	defer func() {
		guardrailPreviews.Lock()
		delete(guardrailPreviews.entries, id)
		guardrailPreviews.Unlock()
	}()

	form := url.Values{"request_id": {id}, "candidate": {"graph-only"}, "display_dollars": {"real"}}
	req := httptest.NewRequest("POST", "/whatif/guardrails/optimize/graph", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleGuardrailOptimizerGraph(w, req)
	if w.Code != 200 {
		t.Fatalf("handleGuardrailOptimizerGraph status=%d body=%s", w.Code, w.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode graph payload: %v", err)
	}
	chart, _ := payload["chart"].(map[string]interface{})
	data, _ := chart["data"].([]interface{})
	if len(data) == 0 {
		t.Fatalf("%s: fixture defect: empty chart.data", label)
	}
	return payload
}

// TestGuardrailOptimizerGraphThemeAndTableUseSharedPaletteAndAxisLabels is
// the GV2 attempt-3 permanent test for criteria 1-3: it runs the REAL
// graphTheme/graphTable against real handleGuardrailOptimizerGraph output.
func TestGuardrailOptimizerGraphThemeAndTableUseSharedPaletteAndAxisLabels(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; skipping graphTheme/graphTable regression test")
	}

	guardrails := &models.GuardrailConfig{Enabled: true, FloorDropPct: 20, FloorCutPct: 10, CeilingRisePct: 20, CeilingRaisePct: 10, MinSpendingPct: 50, MaxSpendingPct: 150}
	enabledPayload := gv2OptimizerThemeGraphPayload(t, "enabled", guardrails)
	disabledPayload := gv2OptimizerThemeGraphPayload(t, "disabled", nil)

	// Fixture sanity: both trace sets the assertions below depend on.
	if !gv2GraphHasTrace(enabledPayload["chart"].(map[string]interface{}), "Guardrail cuts / raises") {
		t.Fatal("fixture defect: enabled candidate has no guardrail markers trace")
	}
	if gv2GraphHasTrace(disabledPayload["chart"].(map[string]interface{}), "Cut trigger") {
		t.Fatal("fixture defect: disabled candidate unexpectedly has guardrail traces")
	}
	if !gv2GraphHasTrace(disabledPayload["chart"].(map[string]interface{}), "Key events") {
		t.Fatal("fixture defect: disabled candidate has no Key events trace (needed to test index-based coloring)")
	}

	chartsJS, err := fs.ReadFile(web.EmbeddedFS, "static/js/charts.js")
	if err != nil {
		t.Fatalf("ReadFile(static/js/charts.js): %v", err)
	}
	templateHTML, err := fs.ReadFile(web.EmbeddedFS, "templates/components/whatif/guardrail_optimizer.html")
	if err != nil {
		t.Fatalf("ReadFile(templates/components/whatif/guardrail_optimizer.html): %v", err)
	}
	graphTheme := gv2ExtractFunction(t, string(templateHTML), "graphTheme")
	graphTable := gv2ExtractFunction(t, string(templateHTML), "graphTable")

	dir := t.TempDir()
	writeJSON := func(name string, v interface{}) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		p := filepath.Join(dir, name+".json")
		if err := os.WriteFile(p, raw, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}
	enabledPath := writeJSON("enabled", enabledPayload)
	disabledPath := writeJSON("disabled", disabledPayload)

	chartsJSPath := filepath.Join(dir, "charts.js")
	if err := os.WriteFile(chartsJSPath, chartsJS, 0o600); err != nil {
		t.Fatalf("write charts.js copy: %v", err)
	}
	extractedPath := filepath.Join(dir, "extracted.js")
	if err := os.WriteFile(extractedPath, []byte(graphTheme+"\n"+graphTable+"\n"), 0o600); err != nil {
		t.Fatalf("write extracted.js: %v", err)
	}
	harnessPath := filepath.Join(dir, "harness.js")
	if err := os.WriteFile(harnessPath, []byte(gv2OptimizerThemeHarness), 0o600); err != nil {
		t.Fatalf("write harness.js: %v", err)
	}

	out, err := exec.Command(node, harnessPath, chartsJSPath, extractedPath, enabledPath, disabledPath).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "GV2_OPTIMIZER_THEME_NODE_TEST_PASS") {
		t.Fatalf("graphTheme/graphTable node harness failed: %v\n%s", err, out)
	}
}

// gv2OptimizerThemeHarness loads the REAL, unmodified charts.js in a vm
// context (mirroring internal/templates/render_spending_preview_test.go's
// established pattern: stub just enough document/window no-ops so the
// file's top-level addEventListener registrations don't throw -- they are
// never invoked), then runs the extracted graphTheme/graphTable source in
// the SAME context so they resolve the shared getTonePalette/applyTonePalette
// globals exactly as the real page does, plus a minimal DOM stub covering
// only the table/fragment APIs graphTable calls.
const gv2OptimizerThemeHarness = `
const fs = require('fs');
const vm = require('vm');
const assert = require('assert/strict');

class FakeEl {
  constructor(tag) { this.tagName = tag; this.children = []; this.className = ''; this._text = ''; }
  set textContent(v) { this._text = String(v); }
  get textContent() { return this._text; }
  append(...items) { this.children.push(...items); }
  appendChild(item) { this.children.push(item); return item; }
}
class FakeRow extends FakeEl {
  insertCell() { const td = new FakeEl('td'); this.append(td); return td; }
}
class FakeSection extends FakeEl {
  insertRow() { const tr = new FakeRow('tr'); this.append(tr); return tr; }
}
class FakeTable extends FakeEl {
  createCaption() { const c = new FakeEl('caption'); this.append(c); return c; }
  createTHead() { const t = new FakeSection('thead'); this.append(t); return t; }
  createTBody() { const t = new FakeSection('tbody'); this.append(t); return t; }
}

let dark = false;
const document = {
  documentElement: { classList: { contains: (cls) => cls === 'dark' && dark } },
  createDocumentFragment() { return new FakeEl('fragment'); },
  createElement(tag) { return tag === 'table' ? new FakeTable('table') : new FakeEl(tag); },
  addEventListener() {},
  body: { addEventListener() {} },
  querySelectorAll() { return []; },
};
const window = { addEventListener() {}, document };

const sandbox = { document, window, console, structuredClone, Intl };
vm.createContext(sandbox);
vm.runInContext(fs.readFileSync(process.argv[2], 'utf8'), sandbox, { filename: 'charts.js' });
vm.runInContext(fs.readFileSync(process.argv[3], 'utf8'), sandbox, { filename: 'extracted.js' });

const enabledPayload = JSON.parse(fs.readFileSync(process.argv[4], 'utf8'));
const disabledPayload = JSON.parse(fs.readFileSync(process.argv[5], 'utf8'));

function findTrace(chart, name) { return (chart.data || []).find(t => t.name === name); }

function themed(payload, isDark) {
  dark = isDark;
  return sandbox.graphTheme(payload);
}

const LIGHT = { negative: '#dc2626', positive: '#15803d', planned: '#57534e', after: '#1d4ed8' };
const DARK = { negative: '#f87171', positive: '#4ade80', planned: '#d6d3d1', after: '#93c5fd' };
const TONE_TRACES = { 'Cut trigger': 'negative', 'Raise trigger': 'positive', 'Planned': 'planned', 'After guardrails': 'after' };

function assertToneTheming(payload, isDark, stage) {
  const chart = themed(payload, isDark);
  const palette = isDark ? DARK : LIGHT;
  assert.equal(chart.layout.height, 520, stage + ': layout.height');
  assert.ok(chart.layout.yaxis2, stage + ': yaxis2 present');
  assert.equal(chart.layout.yaxis2.gridcolor, chart.layout.yaxis.gridcolor, stage + ': yaxis2 gridcolor matches yaxis');
  assert.equal(chart.layout.yaxis2.automargin, true, stage + ': yaxis2 automargin');
  const domain = payload.chart.layout.yaxis2.domain;
  assert.deepEqual(chart.layout.yaxis2.domain, domain, stage + ': yaxis2 domain unchanged from server');

  const seen = new Set();
  for (const [name, tone] of Object.entries(TONE_TRACES)) {
    const trace = findTrace(chart, name);
    assert.ok(trace, stage + ': missing trace ' + name);
    assert.equal(trace.line.color, palette[tone], stage + ': ' + name + ' color');
    seen.add(trace.line.color);
  }
  assert.equal(seen.size, 4, stage + ': tone colors must be distinct');

  const markers = findTrace(chart, 'Guardrail cuts / raises');
  assert.ok(markers, stage + ': missing markers trace');
  const origTones = findTrace(payload.chart, 'Guardrail cuts / raises').meta.tones;
  assert.ok(Array.isArray(markers.marker.color), stage + ': markers marker.color must stay an array, not a scalar');
  assert.equal(markers.marker.color.length, origTones.length, stage + ': markers marker.color length');
  markers.marker.color.forEach((c, i) => {
    assert.equal(c, palette[origTones[i]], stage + ': marker ' + i + ' color');
  });

  const balanceColor = isDark ? '#86efac' : '#166534';
  const balance = findTrace(chart, 'Portfolio Balance');
  assert.equal(balance.line.color, balanceColor, stage + ': Portfolio Balance keeps index-based color');
}

assertToneTheming(enabledPayload, false, 'enabled/light');
assertToneTheming(enabledPayload, true, 'enabled/dark');

// No-guardrails candidate: 340px single panel, no yaxis2, pure index coloring
// (Portfolio Balance green, Key events amber) -- unchanged from pre-GV2.
{
  const chart = themed(disabledPayload, false);
  assert.equal(chart.layout.height, 340, 'disabled/light: layout.height');
  assert.ok(!chart.layout.yaxis2, 'disabled/light: no yaxis2');
  assert.ok(!findTrace(chart, 'Cut trigger'), 'disabled/light: no guardrail traces');
  const balance = findTrace(chart, 'Portfolio Balance');
  assert.equal(balance.line.color, '#166534', 'disabled/light: Portfolio Balance index-green');
  const keyEvents = findTrace(chart, 'Key events');
  assert.ok(keyEvents, 'disabled/light: fixture must include Key events trace');
  assert.equal(keyEvents.marker.color, '#92400e', 'disabled/light: Key events index-amber');
}

// graphTable: y2 (budget-panel) traces get "Monthly living budget"; others
// keep "Portfolio balance". The markers trace sits on the budget panel when
// guardrails are enabled, so it gets the budget label too, and keeps its
// Event column.
{
  function rows(fragment) {
    return fragment.children.map(table => {
      const caption = table.children.find(c => c.tagName === 'caption');
      const thead = table.children.find(c => c.tagName === 'thead');
      const headerRow = thead.children[0];
      return { caption: caption.textContent, headers: headerRow.children.map(th => th.textContent) };
    });
  }
  const tables = rows(sandbox.graphTable(enabledPayload));
  const byCaptionPrefix = (prefix) => tables.find(t => t.caption.startsWith(prefix));

  const planned = byCaptionPrefix('Planned');
  assert.ok(planned, 'graphTable: missing Planned table');
  assert.equal(planned.headers[1], 'Monthly living budget', 'graphTable: Planned column label');

  const after = byCaptionPrefix('After guardrails');
  assert.ok(after, 'graphTable: missing After guardrails table');
  assert.equal(after.headers[1], 'Monthly living budget', 'graphTable: After guardrails column label');

  const balanceTable = byCaptionPrefix('Portfolio Balance');
  assert.ok(balanceTable, 'graphTable: missing Portfolio Balance table');
  assert.equal(balanceTable.headers[1], 'Portfolio balance', 'graphTable: Portfolio Balance column label');

  const cutTable = byCaptionPrefix('Cut trigger');
  assert.ok(cutTable, 'graphTable: missing Cut trigger table');
  assert.equal(cutTable.headers[1], 'Portfolio balance', 'graphTable: Cut trigger column label (balance-panel axis)');

  const markersTable = byCaptionPrefix('Guardrail cuts / raises');
  assert.ok(markersTable, 'graphTable: missing markers table');
  assert.equal(markersTable.headers[1], 'Monthly living budget', 'graphTable: markers column label (on budget panel)');
  assert.equal(markersTable.headers[2], 'Event', 'graphTable: markers table keeps its Event column');
}

console.log('GV2_OPTIMIZER_THEME_NODE_TEST_PASS');
`
