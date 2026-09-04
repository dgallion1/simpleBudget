package explorer

import (
	"strings"
	"testing"
)

func TestSummaryStats_Render_Tokens(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	out, err := renderer.RenderToString("summary-stats", map[string]any{
		"TotalCount":    42,
		"TotalIncome":   5000.0,
		"TotalExpenses": 3200.0,
		"NetAmount":     1800.0,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	for _, want := range []string{
		`id="summary-stats"`, // OOB target id preserved
		`hx-swap-oob="true"`, // OOB behavior preserved
		`class="num`,         // monospace numerals applied
		"Income",
		"Expenses",
		"Net",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in summary-stats; got: %s", want, strunc(out, 700))
		}
	}
	// Income tile uses the positive token, expenses uses negative (U6;
	// these were the emerald/rose hue literals before the token sweep).
	if !strings.Contains(out, "text-positive") {
		t.Errorf("expected positive token on income tile; got: %s", strunc(out, 700))
	}
	if !strings.Contains(out, "text-negative") {
		t.Errorf("expected negative token on expenses tile; got: %s", strunc(out, 700))
	}
}

func strunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
