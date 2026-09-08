package templates_test

// RF2: dashboard lead sentence (components/kpis.html, "kpis" define). These
// tests render "kpis" through the SAME BudgetVerdictView/CashFlowDisplay
// producers the dashboard handler uses (dashboard.BuildBudgetVerdict, which
// itself calls metrics.ReportingCashFlow), never hand-rolled floats -- the
// lead's money spans must come from $v.Delta and $flow.Balance exactly as
// the tiles render them (RF2 letter A). This file lives in package
// templates_test (external test package), not the directory's usual
// "package templates", because dashboard.BuildBudgetVerdict/BudgetVerdictView
// live in budget2/internal/handlers/dashboard, which imports
// budget2/internal/templates -- an internal (same-package) test file
// importing dashboard back would be a real import cycle (verified: go vet
// rejects it), but an external test package compiles independently of the
// package under test and may depend on anything that itself depends on the
// production (non-test) package.
//
// Fixture-math note (RF2 attempt 1): the RF-RUN-SPEC.md acceptance text for
// case (iii) specifies income 10.006 / spending 10.004 and asserts the
// result is "matched". Verified against the actual (unowned, unchanged)
// metrics.ReportingCashFlow: it rounds Income and Spending to cents
// INDEPENDENTLY first ("aggregate anchors"), THEN derives Balance from the
// two ALREADY-ROUNDED values -- so 10.006->10.01 and 10.004->10.00 yields
// Balance 0.01 ("Recorded income above spending"), not 0. The spec's
// deduction ("Balance is ReportingMoney-rounded, so a $0.002 raw difference
// must round to 0") assumes a single round-the-difference model that the
// real code does not use. Substituted income 10.001 / spending 9.999 below
// -- verified via the same producer to yield Balance exactly 0 ("Recorded
// income matches spending") -- which still exercises the intended point
// (fractional-cent, non-equal raw inputs that resolve to a "matched"
// display because $flow.Balance, not a raw float, drives the branch) while
// being arithmetically correct. Flagged for the lead as a fixture-math
// finding (not a template defect); accepted by ruling RF-2026-09-07b
// (item 3): the spec now specifies this substitution.
import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"budget2/internal/handlers/dashboard"
	"budget2/internal/models"
	"budget2/internal/templates"
	"budget2/web"
)

// newKPIsRenderer builds a *templates.Renderer over the real embedded
// template tree, exactly as production wiring does (fs.Sub + NewFromFS),
// mirroring internal/handlers/dashboard.TestDI2SecondYearHistoryRendered's
// harness shape.
func newKPIsRenderer(t *testing.T) *templates.Renderer {
	t.Helper()
	fsys, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	renderer, err := templates.NewFromFS(fsys, false)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}
	return renderer
}

// renderKPIs renders "kpis" for m via dashboard.BuildBudgetVerdict, the SAME
// producer handleKPIsPartial/handleDashboard use, so CashFlow/Living/
// Healthcare classification is never reimplemented by the test.
func renderKPIs(t *testing.T, renderer *templates.Renderer, m *models.DashboardMetrics) string {
	t.Helper()
	v := dashboard.BuildBudgetVerdict(m)
	out, err := renderer.RenderToString("kpis", map[string]any{
		"Metrics":       m,
		"BudgetVerdict": v,
	})
	if err != nil {
		t.Fatalf("RenderToString(kpis): %v", err)
	}
	return out
}

// leadHTML extracts the <p id="dashboard-lead" ...>...</p> element's inner
// content (everything after the opening tag's ">", up to the matching
// "</p>"), or "" plus ok=false if the id is absent.
func leadHTML(out string) (string, bool) {
	idx := strings.Index(out, `id="dashboard-lead"`)
	if idx == -1 {
		return "", false
	}
	tagEnd := strings.Index(out[idx:], ">")
	if tagEnd == -1 {
		return "", false
	}
	start := idx + tagEnd + 1
	end := strings.Index(out[start:], "</p>")
	if end == -1 {
		return "", false
	}
	return out[start : start+end], true
}

// moneySpans returns every `<span class="num">...</span>` payload appearing
// in s, in document order.
var moneySpanRe = regexp.MustCompile(`<span class="num">([^<]*)</span>`)

func moneySpans(s string) []string {
	matches := moneySpanRe.FindAllStringSubmatch(s, -1)
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m[1]
	}
	return out
}

// stripSign removes a leading "-" so a signed tile figure (e.g. the
// Cash-flow balance tile, which renders $flow.Balance raw) can be compared
// against the lead's abs()-based money span for the SAME underlying figure.
func stripSign(s string) string {
	return strings.TrimPrefix(s, "-")
}

func TestDashboardLead(t *testing.T) {
	renderer := newKPIsRenderer(t)

	t.Run("over plan, income did not cover spending", func(t *testing.T) {
		m := &models.DashboardMetrics{
			TransactionCount:        1,
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: 1234.565,
			TotalIncome:             44123.21,
			TotalExpenses:           70166.53,
		}
		out := renderKPIs(t, renderer, m)

		lead, ok := leadHTML(out)
		if !ok {
			t.Fatalf("expected #dashboard-lead in output; got: %s", trunc(out, 2000))
		}
		wantLead := `Spending is <span class="num">$1,234.57</span> over plan for this period. Recorded income did not cover spending by <span class="num">$26,043.32</span>.`
		if lead != wantLead {
			t.Fatalf("lead = %q, want %q", lead, wantLead)
		}

		spans := moneySpans(lead)
		if len(spans) != 2 {
			t.Fatalf("expected 2 money spans in lead, got %d: %v", len(spans), spans)
		}
		deltaMoney, balanceMoney := spans[0], spans[1]

		// Sentence-1 money must equal the "Spending versus plan" tile's
		// money span (both render formatMoney(abs $v.Delta) from the same
		// $v.Delta -- byte-identical, no sign to normalize).
		if !strings.Contains(out, `<p class="text-2xl font-semibold text-negative"><span class="num">`+deltaMoney+`</span> over</p>`) {
			t.Errorf("expected the Budget tile to render the SAME over-amount %q as the lead; got: %s", deltaMoney, trunc(out, 3000))
		}

		// Sentence-2 money must equal the Cash-flow balance tile's figure --
		// the tile renders $flow.Balance signed (raw), the lead renders
		// abs($flow.Balance); strip the sign before comparing so the
		// assertion is about the SAME underlying figure/producer, not
		// literal byte identity of the signed vs. unsigned string.
		if !strings.Contains(out, `<h2 class="text-base font-medium text-gray-700 dark:text-gray-300">Cash-flow balance</h2>`) {
			t.Fatalf("fixture sanity: Cash-flow balance tile heading not found")
		}
		// RF6 (ruling RF-2026-09-07e): the folded methodology note must not
		// say "below" now that it sits after the budget-details grid.
		if !strings.Contains(out, "Monthly equivalents use the selected range") || strings.Contains(out, "equivalents below") {
			t.Error("RF6 guard: methodology sentence wording")
		}
		balTileRe := regexp.MustCompile(`Cash-flow balance</h2>\s*<p class="num text-4xl font-semibold text-negative">(-?[^<]*)</p>`)
		mtch := balTileRe.FindStringSubmatch(out)
		if mtch == nil {
			t.Fatalf("could not locate Cash-flow balance tile figure; got: %s", trunc(out, 3000))
		}
		if stripSign(mtch[1]) != balanceMoney {
			t.Errorf("lead balance money %q does not equal Cash-flow balance tile figure %q (sign-normalized)", balanceMoney, mtch[1])
		}
	})

	t.Run("under plan, income exceeded spending", func(t *testing.T) {
		m := &models.DashboardMetrics{
			TransactionCount:        1,
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: -500.0,
			TotalIncome:             5000.50,
			TotalExpenses:           4000.25,
		}
		out := renderKPIs(t, renderer, m)

		lead, ok := leadHTML(out)
		if !ok {
			t.Fatalf("expected #dashboard-lead in output; got: %s", trunc(out, 2000))
		}
		wantLead := `Spending is <span class="num">$500.00</span> under plan for this period. Recorded income exceeded spending by <span class="num">$1,000.25</span>.`
		if lead != wantLead {
			t.Fatalf("lead = %q, want %q", lead, wantLead)
		}
		if !strings.Contains(out, `<p class="text-2xl font-semibold text-positive"><span class="num">$500.00</span> under</p>`) {
			t.Errorf("expected Budget tile to agree with the lead's $500.00 under-amount; got: %s", trunc(out, 3000))
		}
		// Balance is positive here, so the tile renders the SAME unsigned
		// string as the lead (no sign to strip) -- byte-identical check.
		if !strings.Contains(out, `<p class="num text-4xl font-semibold text-gray-900 dark:text-gray-100">$1,000.25</p>`) {
			t.Errorf("expected Cash-flow balance tile to render the SAME $1,000.25 as the lead; got: %s", trunc(out, 3000))
		}
	})

	t.Run("no target, fractional-cent inputs resolve to matched", func(t *testing.T) {
		// See file-level comment: 10.001/9.999 substituted for the spec's
		// 10.006/10.004 pairing, verified against the real
		// metrics.ReportingCashFlow to produce Balance == 0 ("matched"),
		// which the original pairing does not.
		m := &models.DashboardMetrics{
			TransactionCount:  1,
			HasCombinedTarget: false,
			TotalIncome:       10.001,
			TotalExpenses:     9.999,
		}
		out := renderKPIs(t, renderer, m)

		lead, ok := leadHTML(out)
		if !ok {
			t.Fatalf("expected #dashboard-lead in output; got: %s", trunc(out, 2000))
		}
		wantLead := `No budget is set for this period. Recorded income matched spending.`
		if lead != wantLead {
			t.Fatalf("lead = %q, want %q", lead, wantLead)
		}
		if !strings.Contains(out, `<p class="num text-4xl font-semibold text-gray-900 dark:text-gray-100">$0.00</p>`) {
			t.Errorf("expected the Cash-flow balance tile to show $0.00; got: %s", trunc(out, 3000))
		}
		if !strings.Contains(out, "Recorded income matches spending") {
			t.Errorf("expected the tile's Explanation string \"Recorded income matches spending\" (present tense, from CashFlowDisplay); got: %s", trunc(out, 3000))
		}
	})

	t.Run("no transactions: no lead at all", func(t *testing.T) {
		m := &models.DashboardMetrics{
			TransactionCount:        0,
			HasCombinedTarget:       true,
			CombinedCumulativeDelta: 1234.565,
			TotalIncome:             44123.21,
			TotalExpenses:           70166.53,
		}
		out := renderKPIs(t, renderer, m)
		if strings.Contains(out, `id="dashboard-lead"`) {
			t.Errorf("did not expect #dashboard-lead when TransactionCount is 0; got: %s", trunc(out, 2000))
		}
		if !strings.Contains(out, "No transactions in this selected period.") {
			t.Errorf("expected the existing zero-transaction message to survive; got: %s", trunc(out, 2000))
		}
	})
}

// TestDashboardLeadIsFirstChildOfOverviewSection guards the placement rule
// (RF2 letter A): #dashboard-lead must be the FIRST element inside the
// aria-label="Selected-period overview" section.
func TestDashboardLeadIsFirstChildOfOverviewSection(t *testing.T) {
	renderer := newKPIsRenderer(t)
	m := &models.DashboardMetrics{
		TransactionCount:        1,
		HasCombinedTarget:       true,
		CombinedCumulativeDelta: 1234.565,
		TotalIncome:             44123.21,
		TotalExpenses:           70166.53,
	}
	out := renderKPIs(t, renderer, m)

	secIdx := strings.Index(out, `aria-label="Selected-period overview"`)
	if secIdx == -1 {
		t.Fatalf("overview section not found; got: %s", trunc(out, 1000))
	}
	afterSectionTag := strings.Index(out[secIdx:], ">")
	if afterSectionTag == -1 {
		t.Fatalf("could not find end of the overview <section> tag")
	}
	rest := out[secIdx+afterSectionTag+1:]
	rest = strings.TrimLeft(rest, " \t\r\n")
	if !strings.HasPrefix(rest, `<p id="dashboard-lead"`) {
		t.Errorf("expected #dashboard-lead to be the first child of the overview section; got: %s", trunc(rest, 300))
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("...(%d more bytes)", len(s)-n)
}
