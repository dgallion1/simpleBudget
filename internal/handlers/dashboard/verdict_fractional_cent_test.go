package dashboard

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestDashboardVerdictBar_LivingHealthcareBreakdown_RenderedSumHoldsOnFractionalCentBase
// guards ruling 2026-08-29b ("the displayed figures must sum" is a claim
// about the RENDERED strings, not the underlying floats) for HC1's clipped
// healthcare accrual: Living.Delta and Healthcare.Delta are exact-float
// components of the combined Delta, but formatMoney's independent "%.2f" on
// each of the three can disagree with the others at a floating-point tie.
//
// 1804.415 is stored as 1804.41499999999996362021, which "%.2f" rounds DOWN
// to 1804.41; summed twice the exact float is 3608.82999999999992724042,
// which "%.2f" rounds UP to 3608.83. Naive independent rounding would
// render "Living: $1,804.41" + "Healthcare: $1,804.41" next to a combined
// "$3,608.83 over budget" -- a one-cent visible mismatch. BuildBudgetVerdict
// derives Healthcare's rendered cents as the combined total's cents minus
// Living's cents (both via the same "%.2f"-then-parse path formatMoney
// uses) so the three rendered dollar figures agree exactly, mirroring
// analysis.BudgetFit's base+adjustment=total fix for the same defect class.
func TestDashboardVerdictBar_LivingHealthcareBreakdown_RenderedSumHoldsOnFractionalCentBase(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	m := bothBucketsMetrics(1804.415, 1804.415)
	v := BuildBudgetVerdict(m)

	out, err := renderer.RenderToString("dashboard-verdict-bar", map[string]any{"BudgetVerdict": v})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}

	total := extractDollarBefore(t, out, "over budget")
	living := extractDollarAfter(t, out, "Living:")
	healthcare := extractDollarAfter(t, out, "Healthcare:")

	if got, want := living+healthcare, total; math.Abs(got-want) > 0.001 {
		t.Errorf("rendered Living (%.2f) + rendered Healthcare (%.2f) = %.2f, want rendered total %.2f (must sum to the cent as DISPLAYED): %s",
			living, healthcare, got, want, trunc(out, 1400))
	}
}

var verdictDollarRe = regexp.MustCompile(`\$([\d,]+\.\d{2})`)

func parseVerdictDollar(t *testing.T, match []string) float64 {
	t.Helper()
	numStr := strings.ReplaceAll(match[1], ",", "")
	v, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		t.Fatalf("ParseFloat(%q): %v", numStr, err)
	}
	return v
}

// extractDollarAfter finds label in html, then parses the next
// "$X,XXX.XX" dollar figure that follows it (label precedes the figure,
// e.g. "Living: $1,804.41").
func extractDollarAfter(t *testing.T, html, label string) float64 {
	t.Helper()
	idx := strings.Index(html, label)
	if idx < 0 {
		t.Fatalf("label %q not found in rendered html: %s", label, html)
	}
	match := verdictDollarRe.FindStringSubmatch(html[idx:])
	if match == nil {
		t.Fatalf("no dollar figure found after label %q in: %s", label, html[idx:idx+200])
	}
	return parseVerdictDollar(t, match)
}

// extractDollarBefore finds label in html, then parses the LAST
// "$X,XXX.XX" dollar figure that precedes it (the figure precedes the
// label, e.g. "$3,608.83</span> over budget").
func extractDollarBefore(t *testing.T, html, label string) float64 {
	t.Helper()
	idx := strings.Index(html, label)
	if idx < 0 {
		t.Fatalf("label %q not found in rendered html: %s", label, html)
	}
	head := html[:idx]
	matches := verdictDollarRe.FindAllStringSubmatch(head, -1)
	if len(matches) == 0 {
		t.Fatalf("no dollar figure found before label %q in: %s", label, head)
	}
	return parseVerdictDollar(t, matches[len(matches)-1])
}
