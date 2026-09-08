package templates

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
)

// RF3 acceptance 2: the #insights-lead sentence is built from .Current,
// .Change (via the shared "insights-dollar-change" define), len(.Findings),
// and .Monthly -- the SAME field the "Retained estimates" line renders. All
// four fixtures render the whole "insights-content" template (as DI4/DI5/LT5
// do) so a regression in section ordering or a nil-field panic surfaces here
// too, not just in a narrower unit render.

func rf3Period(historyAvailable bool) models.PeriodContext {
	return models.PeriodContext{
		SelectedStart:    time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		SelectedEnd:      time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
		PreviousStart:    time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PreviousEnd:      time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
		HistoryAvailable: historyAvailable,
	}
}

func rf3Render(t *testing.T, period models.PeriodContext, investigation map[string]any) string {
	t.Helper()
	renderer := lt5Renderer(t)
	out, err := renderer.RenderToString("insights-content", map[string]any{
		"Insights":      &models.InsightsData{Period: &period},
		"Period":        period,
		"Investigation": investigation,
		"StartDate":     period.SelectedStart.Format("2006-01-02"),
		"EndDate":       period.SelectedEnd.Format("2006-01-02"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func rf3BaseInvestigation(findings int, monthly, annual float64) map[string]any {
	// Each Findings element needs a "Creep" field: #insights-findings ranges
	// over .Findings (not just .Preview) to compute $hasCreep for the moved
	// price-creep footnote.
	fs := make([]any, findings)
	for i := range fs {
		fs[i] = map[string]any{"Creep": nil}
	}
	return map[string]any{
		"Current":      0.0,
		"Previous":     0.0,
		"Change":       models.ChangeCell{},
		"Contributors": []models.CategoryTrend{},
		"Grouping":     "Categories",
		"GroupIDs":     map[string]string{},
		"Findings":     fs,
		"Preview":      []any{},
		"Remaining":    []any{},
		"Groups":       []any{},
		"Monthly":      monthly,
		"Annual":       annual,
	}
}

// (i) HasTarget-equivalent: history available, 52 findings.
func TestRF3InsightsLeadHistoryAvailable(t *testing.T) {
	period := rf3Period(true)
	inv := rf3BaseInvestigation(52, 7803.64, 93643.68)
	inv["Current"] = 102113.36
	inv["Previous"] = 78070.03
	inv["Change"] = models.ChangeCell{Kind: "dollar", Amount: 24043.33, Direction: "up"}

	out := rf3Render(t, period, inv)

	want := `Net spending this period: <span class="num">$102,113.36</span> (<span class="num">+$24,043.33 increase</span> versus the prior period). 52 transactions to review; detected recurring spending is about <span class="num">$7,803.64</span> a month.`
	if !strings.Contains(out, want) {
		t.Fatalf("lead sentence mismatch; want substring:\n%s\ngot:\n%s", want, out)
	}
	// RF5 (ruling RF-2026-09-07d): the folded caveat must not say "below"
	// now that it sits after the findings list, and the retained-estimates
	// line carries the RF3-B size bump.
	for _, must := range []string{"Each finding is tied to an actual transaction", `<p class="text-base text-gray-700 dark:text-gray-300">Retained estimates:`} {
		if !strings.Contains(out, must) {
			t.Errorf("RF5 guard: missing %q", must)
		}
	}
	if strings.Contains(out, "finding below") {
		t.Error("RF5 guard: stale wording 'finding below' present")
	}
	leadAt := strings.Index(out, `id="insights-lead"`)
	ctxAt := strings.Index(out, `id="period-context"`)
	if leadAt < 0 || ctxAt < 0 || leadAt < ctxAt {
		t.Fatalf("insights-lead must follow period-context: leadAt=%d ctxAt=%d", leadAt, ctxAt)
	}
	// The section line ("Retained estimates: ...") must render the SAME
	// money string as the lead -- one formatter, one source field.
	if strings.Count(out, `<span class="num">$7,803.64</span>`) < 2 {
		t.Fatalf("lead and section line must both show $7,803.64 (same .Monthly field): %s", out)
	}
}

// (ii) history unavailable, singular "transaction".
func TestRF3InsightsLeadHistoryUnavailableSingular(t *testing.T) {
	period := rf3Period(false)
	inv := rf3BaseInvestigation(1, 100.00, 1200.00)
	inv["Current"] = 500.00

	out := rf3Render(t, period, inv)

	want := `Net spending this period: <span class="num">$500.00</span>. 1 transaction to review; detected recurring spending is about <span class="num">$100.00</span> a month.`
	if !strings.Contains(out, want) {
		t.Fatalf("lead sentence mismatch; want substring:\n%s\ngot:\n%s", want, out)
	}
	if strings.Contains(out, "1 transactions") {
		t.Fatal("singular finding must not pluralize")
	}
	if strings.Contains(out, "versus the prior period") {
		t.Fatal("no-history lead must not invent a comparison")
	}
}

// (iii) zero findings.
func TestRF3InsightsLeadZeroFindings(t *testing.T) {
	period := rf3Period(false)
	inv := rf3BaseInvestigation(0, 0.0, 0.0)
	inv["Current"] = 0.0

	out := rf3Render(t, period, inv)

	want := `Net spending this period: <span class="num">$0.00</span>. No transactions to review; detected recurring spending is about <span class="num">$0.00</span> a month.`
	if !strings.Contains(out, want) {
		t.Fatalf("lead sentence mismatch; want substring:\n%s\ngot:\n%s", want, out)
	}
}

// (iv) trends-table cap: 15 categories cap at 12, a 15th-count toggle, no
// server-side `hidden`; 12 categories carry neither overflow markers nor a
// toggle.
func TestRF3TrendsTableCap(t *testing.T) {
	renderer := lt5Renderer(t)
	period := rf3Period(true)

	mkTrends := func(n int) []models.CategoryTrend {
		out := make([]models.CategoryTrend, n)
		for i := 0; i < n; i++ {
			out[i] = models.CategoryTrend{
				Category:      fmt.Sprintf("Category%02d", i),
				CurrentAmount: float64(100 - i),
				Change:        models.ChangeCell{Kind: "dollar", Amount: float64(i)},
			}
		}
		return out
	}

	t.Run("fifteen categories cap at twelve", func(t *testing.T) {
		out, err := renderer.RenderToString("insights-trends-table", map[string]any{
			"CategoryTrends": mkTrends(15),
			"Period":         period,
			"GroupIDs":       map[string]string{},
		})
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(out, `data-trend-overflow="1"`); n != 3 {
			t.Fatalf("data-trend-overflow count = %d, want 3", n)
		}
		if !strings.Contains(out, `data-trends-toggle`) {
			t.Fatal("missing toggle button")
		}
		if !strings.Contains(out, `Show all 15 categories`) {
			t.Fatal("toggle text must name the real total")
		}
		if !strings.Contains(out, `aria-expanded="false"`) {
			t.Fatal("toggle must start collapsed")
		}
		if strings.Contains(out, "hidden") {
			t.Fatal(`server markup must carry no "hidden" (point 16: JS off shows every row)`)
		}
		// Overflow rows are the ORIGINAL 13th-15th rows (server-side, by
		// original order), not a JS-only concept.
		for i := 12; i < 15; i++ {
			cat := fmt.Sprintf("Category%02d", i)
			at := strings.Index(out, `data-category="`+cat+`"`)
			rowEnd := strings.Index(out[at:], "</tr>") + at
			if at < 0 || !strings.Contains(out[at:rowEnd], `data-trend-overflow="1"`) {
				t.Errorf("row %s should carry data-trend-overflow", cat)
			}
		}
		for i := 0; i < 12; i++ {
			cat := fmt.Sprintf("Category%02d", i)
			at := strings.Index(out, `data-category="`+cat+`"`)
			rowEnd := strings.Index(out[at:], "</tr>") + at
			if at < 0 || strings.Contains(out[at:rowEnd], `data-trend-overflow`) {
				t.Errorf("row %s should NOT carry data-trend-overflow", cat)
			}
		}
	})

	t.Run("twelve categories: no toggle, no overflow", func(t *testing.T) {
		out, err := renderer.RenderToString("insights-trends-table", map[string]any{
			"CategoryTrends": mkTrends(12),
			"Period":         period,
			"GroupIDs":       map[string]string{},
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "data-trends-toggle") {
			t.Fatal("12 rows must not render a toggle")
		}
		if strings.Contains(out, "data-trend-overflow") {
			t.Fatal("12 rows must not carry any overflow marker")
		}
	})
}
