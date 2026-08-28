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
