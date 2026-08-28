package templates

import (
	"io/fs"
	"strings"
	"testing"

	"budget2/web"
)

// newConversionSweepRenderer builds a real renderer against the embedded
// template FS, matching the render_*_test.go precedent elsewhere in this
// package (e.g. TestRenderProjectionBreakdownCard).
func newConversionSweepRenderer(t *testing.T) *Renderer {
	t.Helper()
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}
	r, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}
	return r
}

// TestRenderConversionSweepResults_SurvivesAndDepletesSplit covers the
// acceptance-criteria rendering split: a surviving candidate renders
// "survives" plus its ending real balance and NEVER a depletion month/year
// figure; a depleting candidate renders "N mo (~Y yrs)" and never a
// fabricated survives/ending-balance figure.
func TestRenderConversionSweepResults_SurvivesAndDepletesSplit(t *testing.T) {
	r := newConversionSweepRenderer(t)

	rows := []map[string]interface{}{
		{
			"Amount":            0.0,
			"Current":           false,
			"Survives":          true,
			"EndingBalanceReal": 3_150_000.0,
			"DepletionMonth":    0,
			"DepletionYears":    0,
			"LifetimeTax":       500_000.0,
			"LifetimeIRMAA":     12_000.0,
		},
		{
			"Amount":            200_000.0,
			"Current":           false,
			"Survives":          false,
			"EndingBalanceReal": 0.0,
			"DepletionMonth":    455,
			"DepletionYears":    38,
			"LifetimeTax":       900_000.0,
			"LifetimeIRMAA":     45_000.0,
		},
	}

	out, err := r.RenderToString("whatif-conversion-sweep-results", map[string]interface{}{"Rows": rows})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	// Isolate each row's own markup so a substring belonging to one row
	// can't mask (or fake a pass for) an assertion about the other row.
	survivingRowStart := strings.LastIndex(out[:strings.Index(out, "$3,150,000.00")], "<tr")
	survivingRowEnd := survivingRowStart + strings.Index(out[survivingRowStart:], "</tr>")
	survivingRow := out[survivingRowStart:survivingRowEnd]

	depletingRowStart := strings.LastIndex(out[:strings.Index(out, "455 mo")], "<tr")
	depletingRowEnd := depletingRowStart + strings.Index(out[depletingRowStart:], "</tr>")
	depletingRow := out[depletingRowStart:depletingRowEnd]

	if !strings.Contains(collapse(survivingRow), "survives") {
		t.Errorf("expected the surviving row to render \"survives\"; row: %s", survivingRow)
	}
	if !strings.Contains(survivingRow, "$3,150,000.00") {
		t.Errorf("expected the surviving row's ending real balance to render; row: %s", survivingRow)
	}
	if strings.Contains(survivingRow, "mo (~") {
		t.Errorf("surviving row must not render a fabricated depletion month; row: %s", survivingRow)
	}

	if !strings.Contains(collapse(depletingRow), "455 mo (~38 yrs)") {
		t.Errorf("expected the depleting row to render \"455 mo (~38 yrs)\"; row: %s", depletingRow)
	}
	if strings.Contains(depletingRow, "survives") {
		t.Errorf("depleting row must not also render \"survives\"; row: %s", depletingRow)
	}
}

// TestRenderConversionSweepResults_CurrentRowMarked covers the "current"
// visual marker on the row matching the saved plan's current conversion
// amount, and that non-current rows do not carry it.
func TestRenderConversionSweepResults_CurrentRowMarked(t *testing.T) {
	r := newConversionSweepRenderer(t)

	rows := []map[string]interface{}{
		{"Amount": 0.0, "Current": false, "Survives": true, "EndingBalanceReal": 1_000_000.0, "LifetimeTax": 100_000.0, "LifetimeIRMAA": 1_000.0},
		{"Amount": 50_000.0, "Current": true, "Survives": true, "EndingBalanceReal": 900_000.0, "LifetimeTax": 200_000.0, "LifetimeIRMAA": 2_000.0},
	}

	out, err := r.RenderToString("whatif-conversion-sweep-results", map[string]interface{}{"Rows": rows})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	if n := strings.Count(out, ">current<"); n != 1 {
		t.Errorf("expected exactly one \"current\" marker, got %d; body: %s", n, out)
	}

	currentIdx := strings.Index(out, ">current<")
	if currentIdx == -1 {
		t.Fatalf("expected a \"current\" marker in the rendered output")
	}
	rowStart := strings.LastIndex(out[:currentIdx], "<tr")
	if rowStart == -1 {
		t.Fatalf("could not find the row containing the \"current\" marker")
	}
	rowEnd := strings.Index(out[currentIdx:], "</tr>")
	row := out[rowStart : currentIdx+rowEnd]
	if !strings.Contains(row, "$50,000.00") {
		t.Errorf("expected the \"current\" marker on the $50,000 row, got row: %s", row)
	}
}

// TestRenderConversionSweepResults_NoRowsFallback covers the empty-state
// message when Rows is nil/empty.
func TestRenderConversionSweepResults_NoRowsFallback(t *testing.T) {
	r := newConversionSweepRenderer(t)

	out, err := r.RenderToString("whatif-conversion-sweep-results", map[string]interface{}{"Rows": nil})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "No results.") {
		t.Errorf("expected empty-state message; got: %s", out)
	}
}

// ── T16: best-row markers ─────────────────────────────────────────────────

// TestRenderConversionSweepResults_LeastTaxAndLongestLastingSeparateMarkers
// covers the non-color-only markers (ACCESSIBILITY.md #8 — pairs the
// highlight with an icon and text, not color alone) when two different rows
// each win one marker.
func TestRenderConversionSweepResults_LeastTaxAndLongestLastingSeparateMarkers(t *testing.T) {
	r := newConversionSweepRenderer(t)

	rows := []map[string]interface{}{
		{"Amount": 0.0, "Current": false, "Survives": true, "EndingBalanceReal": 1_000_000.0, "LifetimeTax": 500_000.0, "LifetimeIRMAA": 0.0, "LeastLifetimeTax": true},
		{"Amount": 50_000.0, "Current": true, "Survives": true, "EndingBalanceReal": 2_000_000.0, "LifetimeTax": 600_000.0, "LifetimeIRMAA": 0.0, "LongestLasting": true},
	}

	out, err := r.RenderToString("whatif-conversion-sweep-results", map[string]interface{}{"Rows": rows})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	if !strings.Contains(out, "&#9733; least lifetime tax") {
		t.Errorf("expected the least-lifetime-tax marker (icon + text); body: %s", out)
	}
	if !strings.Contains(out, "&#9733; longest-lasting") {
		t.Errorf("expected the longest-lasting marker (icon + text); body: %s", out)
	}
	if strings.Contains(out, "least tax &amp; longest-lasting") {
		t.Errorf("did not expect the combined marker when two different rows win; body: %s", out)
	}
}

// TestRenderConversionSweepResults_CombinedMarkerWhenSameRowWinsBoth covers
// the "one combined marker" rule from CONVERSION_SWEEP_SPEC.md T16: a row
// with both LeastLifetimeTax and LongestLasting true renders a single
// combined badge, not two stacked badges.
func TestRenderConversionSweepResults_CombinedMarkerWhenSameRowWinsBoth(t *testing.T) {
	r := newConversionSweepRenderer(t)

	rows := []map[string]interface{}{
		{"Amount": 25_000.0, "Current": false, "Survives": true, "EndingBalanceReal": 2_000_000.0, "LifetimeTax": 100_000.0, "LifetimeIRMAA": 0.0, "LeastLifetimeTax": true, "LongestLasting": true},
	}

	out, err := r.RenderToString("whatif-conversion-sweep-results", map[string]interface{}{"Rows": rows})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	if !strings.Contains(out, "&#9733; least tax &amp; longest-lasting") {
		t.Errorf("expected one combined marker; body: %s", out)
	}
	if strings.Contains(out, "&#9733; least lifetime tax<") || strings.Contains(out, "&#9733; longest-lasting<") {
		t.Errorf("did not expect the separate single-purpose markers alongside the combined one; body: %s", out)
	}
}

// TestRenderConversionSweepResults_NoMarkerWhenNeitherFlagSet covers the
// default: a row with neither flag renders no marker badge at all.
func TestRenderConversionSweepResults_NoMarkerWhenNeitherFlagSet(t *testing.T) {
	r := newConversionSweepRenderer(t)

	rows := []map[string]interface{}{
		{"Amount": 75_000.0, "Current": false, "Survives": true, "EndingBalanceReal": 500_000.0, "LifetimeTax": 800_000.0, "LifetimeIRMAA": 0.0},
	}

	out, err := r.RenderToString("whatif-conversion-sweep-results", map[string]interface{}{"Rows": rows})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	// Scope the check to the table body — the caption below the table
	// statically mentions the marker glyph to explain its meaning, so
	// searching the whole document would false-positive on that legend text.
	tbodyStart := strings.Index(out, "<tbody>")
	tbodyEnd := strings.Index(out, "</tbody>")
	if tbodyStart == -1 || tbodyEnd == -1 {
		t.Fatalf("could not locate <tbody>...</tbody> in output: %s", out)
	}
	if strings.Contains(out[tbodyStart:tbodyEnd], "&#9733;") {
		t.Errorf("did not expect any best-row marker on the row; body: %s", out)
	}
}

// ── T16: Apply buttons ───────────────────────────────────────────────────

// TestRenderConversionSweepResults_ApplyButtonOnNonCurrentRowsOnly covers:
// non-current rows get an Apply button whose accessible (visible) text
// includes the dollar amount, submitting to the same route/semantics as the
// standalone Roth Conversion form; the current row gets no Apply button.
func TestRenderConversionSweepResults_ApplyButtonOnNonCurrentRowsOnly(t *testing.T) {
	r := newConversionSweepRenderer(t)

	rows := []map[string]interface{}{
		{"Amount": 0.0, "Current": false, "Survives": true, "EndingBalanceReal": 1_000_000.0, "LifetimeTax": 500_000.0, "LifetimeIRMAA": 0.0, "RothStartYear": 2, "RothEndYear": 15},
		{"Amount": 50_000.0, "Current": true, "Survives": true, "EndingBalanceReal": 900_000.0, "LifetimeTax": 600_000.0, "LifetimeIRMAA": 0.0, "RothStartYear": 2, "RothEndYear": 15},
	}

	out, err := r.RenderToString("whatif-conversion-sweep-results", map[string]interface{}{"Rows": rows})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	if n := strings.Count(out, "hx-post=\"/whatif/roth-conversion\""); n != 1 {
		t.Errorf("expected exactly one Apply button (only on the non-current row), got %d; body: %s", n, out)
	}
	if !strings.Contains(out, "Apply $0.00") {
		t.Errorf("expected the $0 row's Apply button to state its own amount; body: %s", out)
	}
	if !strings.Contains(out, `name="apply_source" value="conversion-sweep"`) {
		t.Errorf("expected the apply_source hidden field identifying the sweep Apply flow; body: %s", out)
	}
	if !strings.Contains(out, `name="annual_amount" value="0"`) {
		t.Errorf("expected annual_amount=0 for the $0 row's Apply form; body: %s", out)
	}
	if !strings.Contains(out, `name="start_year" value="2"`) || !strings.Contains(out, `name="end_year" value="15"`) {
		t.Errorf("expected the saved start/end years preserved in the Apply form; body: %s", out)
	}
	// The $0 row's Apply form must submit enabled="" (falsy), not "on" —
	// enabled = amount > 0.
	if !strings.Contains(out, `name="enabled" value="">`) {
		t.Errorf("expected the $0 row's enabled field to be empty (unchecked); body: %s", out)
	}
}

// TestRenderConversionSweepResults_ApplyButtonEnabledForPositiveAmount
// covers the enabled = amount > 0 rule for a nonzero, non-current row.
func TestRenderConversionSweepResults_ApplyButtonEnabledForPositiveAmount(t *testing.T) {
	r := newConversionSweepRenderer(t)

	rows := []map[string]interface{}{
		{"Amount": 100_000.0, "Current": false, "Survives": false, "DepletionMonth": 300, "DepletionYears": 25, "LifetimeTax": 700_000.0, "LifetimeIRMAA": 5_000.0, "RothStartYear": 0, "RothEndYear": 0},
	}

	out, err := r.RenderToString("whatif-conversion-sweep-results", map[string]interface{}{"Rows": rows})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	if !strings.Contains(out, "Apply $100,000.00") {
		t.Errorf("expected the Apply button to state the amount; body: %s", out)
	}
	if !strings.Contains(out, `name="enabled" value="on">`) {
		t.Errorf("expected enabled=\"on\" for a positive-amount Apply form; body: %s", out)
	}
}

// ── T16: applied confirmation (aria-live) ────────────────────────────────

// TestRenderConversionSweepResults_AppliedConfirmationAriaLive covers the
// ACCESSIBILITY.md #10 requirement that a state-changing action announces
// its result via an aria-live=polite region.
func TestRenderConversionSweepResults_AppliedConfirmationAriaLive(t *testing.T) {
	r := newConversionSweepRenderer(t)

	rows := []map[string]interface{}{
		{"Amount": 100_000.0, "Current": true, "Survives": true, "EndingBalanceReal": 1_200_000.0, "LifetimeTax": 700_000.0, "LifetimeIRMAA": 5_000.0},
	}

	out, err := r.RenderToString("whatif-conversion-sweep-results", map[string]interface{}{
		"Rows": rows, "Applied": true, "AppliedAmount": 100_000.0,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	if !strings.Contains(out, `aria-live="polite"`) {
		t.Errorf("expected an aria-live region announcing the apply result; body: %s", out)
	}
	if !strings.Contains(out, "$100,000.00") {
		t.Errorf("expected the confirmation to state the applied amount; body: %s", out)
	}
}

// TestRenderConversionSweepResults_NoAppliedConfirmationOnPlainRun covers
// the un-applied case (the button-triggered sweep run): no confirmation
// banner when Applied is unset/false.
func TestRenderConversionSweepResults_NoAppliedConfirmationOnPlainRun(t *testing.T) {
	r := newConversionSweepRenderer(t)

	rows := []map[string]interface{}{
		{"Amount": 0.0, "Current": true, "Survives": true, "EndingBalanceReal": 1_000_000.0, "LifetimeTax": 500_000.0, "LifetimeIRMAA": 0.0},
	}

	out, err := r.RenderToString("whatif-conversion-sweep-results", map[string]interface{}{"Rows": rows})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if strings.Contains(out, `aria-live="polite"`) {
		t.Errorf("did not expect an applied confirmation on a plain (non-apply) sweep run; body: %s", out)
	}
}

// TestRenderConversionSweep_ButtonState covers the initial page-embed
// template: button-triggered only (a bare hx-post button, no results table,
// no auto-run on load).
func TestRenderConversionSweep_ButtonState(t *testing.T) {
	r := newConversionSweepRenderer(t)

	out, err := r.RenderToString("whatif-conversion-sweep", map[string]interface{}{})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, `hx-post="/whatif/conversion-sweep"`) {
		t.Errorf("expected the run button to hx-post to /whatif/conversion-sweep; got: %s", out)
	}
	if strings.Contains(out, "<table") {
		t.Errorf("initial embed must not render a results table (button-triggered only); got: %s", out)
	}
	if strings.Contains(out, `hx-trigger="load"`) || strings.Contains(out, `hx-trigger="revealed"`) {
		t.Errorf("panel must not auto-run on page load; got: %s", out)
	}
}
