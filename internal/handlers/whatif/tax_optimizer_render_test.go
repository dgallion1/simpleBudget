package whatif

import (
	"maps"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"budget2/internal/models"
)

// taxOptimizerFixture builds a TaxOptimizerAnalysis with distinctive
// PeakMarginalBracket / TotalRothConverted values on Best and Top[0], plus a
// second candidate with both fields zero to cover "—" zero rendering.
// monteCarloRuns controls whether the MC-only columns (and the wider
// details-row colspan) are present.
func taxOptimizerFixture(monteCarloRuns int) *models.WhatIfAnalysis {
	// Every renderable column gets a value distinct from every other column
	// in the same row, so a template mutation that swaps two same-format
	// cells (e.g. two formatMoney columns) cannot render identical output
	// and slip past the header-indexed cell assertions.
	best := models.TaxOptimizerCandidate{
		PrimaryClaimAge:     67,
		RothStrategy:        models.RothOptimizerStrategy{Label: "Fill 24% bracket, 67→72"},
		EndingPortfolioReal: 900000,
		MCMedianEndingReal:  875000,
		MCSurvivalRate:      91,
		LifetimeTaxReal:     150000,
		PeakMarginalBracket: 40.5,
		TotalRothConverted:  123456,
		PerYearConversions: []models.YearlyConversion{
			{Age: 67, Amount: 20000},
		},
	}
	zeroCandidate := models.TaxOptimizerCandidate{
		PrimaryClaimAge:     67,
		RothStrategy:        models.RothOptimizerStrategy{Label: "No conversions"},
		EndingPortfolioReal: 850000,
		MCMedianEndingReal:  810000,
		MCSurvivalRate:      84,
		LifetimeTaxReal:     180000,
		PeakMarginalBracket: 0,
		TotalRothConverted:  0,
	}
	return &models.WhatIfAnalysis{
		TaxOptimizer: &models.TaxOptimizerAnalysis{
			Eligible:         true,
			Baseline:         zeroCandidate,
			Best:             best,
			Top:              []models.TaxOptimizerCandidate{best, zeroCandidate},
			MonteCarloRuns:   monteCarloRuns,
			CandidatesScored: 12,
		},
	}
}

// taxOptimizerZeroBestFixture is a dedicated fixture where Best itself has
// zero PeakMarginalBracket/TotalRothConverted, to cover "—" rendering in the
// headline (the headline reads from Best directly, not from Top).
func taxOptimizerZeroBestFixture(monteCarloRuns int) *models.WhatIfAnalysis {
	zeroBest := models.TaxOptimizerCandidate{
		PrimaryClaimAge:     67,
		RothStrategy:        models.RothOptimizerStrategy{Label: "No conversions"},
		EndingPortfolioReal: 850000,
		LifetimeTaxReal:     180000,
		PeakMarginalBracket: 0,
		TotalRothConverted:  0,
	}
	return &models.WhatIfAnalysis{
		TaxOptimizer: &models.TaxOptimizerAnalysis{
			Eligible:         true,
			Baseline:         zeroBest,
			Best:             zeroBest,
			Top:              []models.TaxOptimizerCandidate{zeroBest},
			MonteCarloRuns:   monteCarloRuns,
			CandidatesScored: 12,
		},
	}
}

func renderTaxOptimizerAnalysis(t *testing.T, analysis *models.WhatIfAnalysis) string {
	t.Helper()
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	out, err := renderer.RenderToString("whatif-tax-optimizer-results", map[string]any{
		"Settings": models.DefaultWhatIfSettings(), "Analysis": analysis,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	return out
}

func renderTaxOptimizer(t *testing.T, monteCarloRuns int) string {
	t.Helper()
	return renderTaxOptimizerAnalysis(t, taxOptimizerFixture(monteCarloRuns))
}

// theadOf extracts the outer table's <thead>...</thead> substring. The
// outer thead closes well before any nested per-candidate details table
// opens, so first-open/first-close is unambiguous.
func theadOf(t *testing.T, out string) string {
	t.Helper()
	start := strings.Index(out, "<thead>")
	if start == -1 {
		t.Fatalf("expected <thead> in rendered output")
	}
	start += len("<thead>")
	rest := out[start:]
	end := strings.Index(rest, "</thead>")
	if end == -1 {
		t.Fatalf("expected </thead> in rendered output")
	}
	return rest[:end]
}

// tbodyOf extracts the OUTER table's <tbody>...</tbody> substring. A
// candidate row with PerYearConversions renders a nested details table that
// has its own <thead>/<tbody>, nested inside the outer <tbody> — so the
// outer close must be found via the LAST "</tbody>", not the first, or the
// extraction would truncate before later candidate rows.
func tbodyOf(t *testing.T, out string) string {
	t.Helper()
	start := strings.Index(out, "<tbody>")
	if start == -1 {
		t.Fatalf("expected <tbody> in rendered output")
	}
	start += len("<tbody>")
	end := strings.LastIndex(out, "</tbody>")
	if end == -1 || end < start {
		t.Fatalf("expected </tbody> after <tbody> in rendered output")
	}
	return out[start:end]
}

// TestWhatIfTaxOptimizer_RendersPeakRateAndRothConverted covers the two
// previously-unrendered TaxOptimizerCandidate fields: PeakMarginalBracket and
// TotalRothConverted must appear in both the headline and the table body,
// with the new headers and glossary entries present too.
func TestWhatIfTaxOptimizer_RendersPeakRateAndRothConverted(t *testing.T) {
	out := renderTaxOptimizer(t, 0)
	flat := strings.Join(strings.Fields(out), " ")

	// Headline (Best candidate): 40.5 rounds to "40" per %.0f (round-half-to-even).
	if !strings.Contains(flat, "peak marginal: 40%") {
		t.Errorf("expected headline peak marginal rate; got: %s", truncate(out, 1200))
	}
	if !strings.Contains(flat, "Roth converted: $123,456.00 nominal") {
		t.Errorf("expected headline Roth converted amount; got: %s", truncate(out, 1200))
	}

	// New header labels — scoped to <thead> so deleting the <th> elements
	// can't be masked by the identical label text in the glossary <dt>s.
	thead := theadOf(t, out)
	for _, want := range []string{"Peak Rate", "Roth Conv"} {
		if !strings.Contains(thead, want) {
			t.Errorf("expected <thead> to contain header label %q; got: %s", want, truncate(thead, 1500))
		}
	}
	if n := strings.Count(thead, "<th"); n != 7 {
		t.Errorf("expected 7 <th> in thead with no MC columns, got %d: %s", n, truncate(thead, 1500))
	}

	// Table body — scoped to <tbody> so deleting the new <td> cells can't be
	// masked by the identical strings that also appear in the headline.
	tbody := tbodyOf(t, out)
	if !strings.Contains(tbody, "40%") {
		t.Errorf("expected table body to render 40%% peak rate; got: %s", truncate(tbody, 1500))
	}
	if !strings.Contains(tbody, "$123,456.00") {
		t.Errorf("expected table body to render $123,456.00 Roth converted; got: %s", truncate(tbody, 1500))
	}

	// Glossary entries for the new columns.
	for _, want := range []string{
		"<dt class=\"font-medium text-gray-700 dark:text-gray-200\">Peak Rate</dt>",
		"<dt class=\"font-medium text-gray-700 dark:text-gray-200\">Roth Conv</dt>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected glossary entry %q; got: %s", want, truncate(out, 2000))
		}
	}
}

// TestWhatIfTaxOptimizer_ZeroRendersAsDash covers the contract documented in
// tax_optimizer.go: PeakMarginalBracket and TotalRothConverted stay zero for
// a projection that never converts / never records a rate, and the UI must
// render "—" for zero — not "0%" or "$0.00".
func TestWhatIfTaxOptimizer_ZeroRendersAsDash(t *testing.T) {
	t.Run("table body", func(t *testing.T) {
		out := renderTaxOptimizer(t, 0)
		tbody := tbodyOf(t, out)

		// Isolate the zero-valued candidate's own row. It is also Baseline
		// in this fixture, so its Δ-vs-Baseline cell legitimately renders
		// "$0.00" (delta == 0) — scoping to this row (not asserting on
		// "$0.00" at all) keeps that legitimate zero from masking a
		// regression in the two fields under test.
		rowStart := strings.Index(tbody, "No conversions")
		if rowStart == -1 {
			t.Fatalf("expected zero-valued candidate row in tbody; got: %s", truncate(tbody, 1500))
		}
		rowRest := tbody[rowStart:]
		rowEnd := strings.Index(rowRest, "</tr>")
		if rowEnd == -1 {
			t.Fatalf("expected closing </tr> for zero-valued candidate row")
		}
		zeroRow := rowRest[:rowEnd]

		if n := strings.Count(zeroRow, "&mdash;"); n != 2 {
			t.Errorf("expected 2 &mdash; in zero-valued candidate row (peak rate + Roth converted), got %d: %s", n, zeroRow)
		}
		if strings.Contains(zeroRow, "0%") {
			t.Errorf("zero PeakMarginalBracket must not render 0%%; zero-valued candidate row: %s", zeroRow)
		}
	})

	t.Run("headline", func(t *testing.T) {
		out := renderTaxOptimizerAnalysis(t, taxOptimizerZeroBestFixture(0))
		flat := strings.Join(strings.Fields(out), " ")

		if !strings.Contains(flat, "peak marginal: &mdash;") {
			t.Errorf("expected headline to render &mdash; for zero peak marginal; got: %s", truncate(out, 1500))
		}
		if !strings.Contains(flat, "Roth converted: &mdash;") {
			t.Errorf("expected headline to render &mdash; for zero Roth converted; got: %s", truncate(out, 1500))
		}
		if strings.Contains(flat, "peak marginal: 0%") {
			t.Errorf("zero peak marginal must not render 0%%; got: %s", truncate(out, 1500))
		}
		if strings.Contains(flat, "Roth converted: $0.00") {
			t.Errorf("zero Roth converted must not render $0.00; got: %s", truncate(out, 1500))
		}
	})
}

// TestWhatIfTaxOptimizer_ColspanMatchesColumnCount covers the details-row
// colspan by checking it against the ACTUAL rendered <th> count in <thead>,
// per mode (MC and non-MC), rather than only against hard-coded literals —
// a mismatch between colspan and the real header count is the real bug this
// guards against.
func TestWhatIfTaxOptimizer_ColspanMatchesColumnCount(t *testing.T) {
	colspanRE := regexp.MustCompile(`colspan="(\d+)"`)

	cases := []struct {
		name           string
		monteCarloRuns int
		wantThCount    int
	}{
		{"without Monte Carlo columns", 0, 7},
		{"with Monte Carlo columns", 500, 9},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderTaxOptimizer(t, tc.monteCarloRuns)

			thead := theadOf(t, out)
			thCount := strings.Count(thead, "<th")
			if thCount != tc.wantThCount {
				t.Errorf("expected %d <th> in thead, got %d: %s", tc.wantThCount, thCount, truncate(thead, 1500))
			}

			m := colspanRE.FindStringSubmatch(out)
			if m == nil {
				t.Fatalf("expected a colspan=\"N\" attribute in rendered output")
			}
			gotColspan, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatalf("colspan value %q is not an int: %v", m[1], err)
			}

			if gotColspan != thCount {
				t.Errorf("colspan=%d does not match the actual rendered <th> count in thead (%d)", gotColspan, thCount)
			}
		})
	}
}

var (
	taxoptTrRE = regexp.MustCompile(`(?s)<tr\b[^>]*>(.*?)</tr>`)
	taxoptThRE = regexp.MustCompile(`(?s)<th\b[^>]*>(.*?)</th>`)
	taxoptTdRE = regexp.MustCompile(`(?s)<td\b[^>]*>(.*?)</td>`)
	// The header tooltip span repeats the Tip text as element content; it
	// must be removed before tag-stripping or the tip text would pollute
	// the extracted header label.
	taxoptTooltipRE = regexp.MustCompile(`(?s)<span role="tooltip".*?</span>`)
	taxoptTagRE     = regexp.MustCompile(`(?s)<[^>]*>`)
)

// taxoptCellText reduces a <th>/<td> inner HTML fragment to its visible text:
// tooltip spans removed, remaining tags stripped, whitespace collapsed.
// Entities are left as written in the template (&mdash;, &#9733;) so
// assertions can match them literally.
func taxoptCellText(s string) string {
	s = taxoptTooltipRE.ReplaceAllString(s, "")
	s = taxoptTagRE.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}

// taxoptHeaderIndex parses the OUTER table's <thead> into a label→column-index
// map, so cell assertions can address a column by its rendered header label
// instead of by substring anywhere in the tbody. Scoped via theadOf, which
// stops at the first </thead> — before the nested per-year-conversions table's
// own thead.
func taxoptHeaderIndex(t *testing.T, out string) map[string]int {
	t.Helper()
	ths := taxoptThRE.FindAllStringSubmatch(theadOf(t, out), -1)
	if len(ths) == 0 {
		t.Fatalf("expected <th> cells in outer thead")
	}
	idx := make(map[string]int, len(ths))
	for i, m := range ths {
		label := taxoptCellText(m[1])
		if label == "" {
			t.Fatalf("outer thead column %d has an empty label", i)
		}
		if prev, dup := idx[label]; dup {
			t.Fatalf("duplicate header label %q at columns %d and %d", label, prev, i)
		}
		idx[label] = i
	}
	return idx
}

// taxoptCandidateRows returns the visible cell text of each candidate row in
// the OUTER table's tbody, in column order. Rows whose <td> count differs from
// wantCols are skipped: that filters out the details row (its colspan cell has
// no matching </td> inside the non-greedy <tr> match) and the nested
// per-year-conversions table's 2-cell rows. Callers must therefore assert the
// returned row count — a candidate row with a missing or extra cell lands in
// the skipped bucket and shows up as a row-count mismatch, not silence.
func taxoptCandidateRows(t *testing.T, out string, wantCols int) [][]string {
	t.Helper()
	var rows [][]string
	for _, tr := range taxoptTrRE.FindAllStringSubmatch(tbodyOf(t, out), -1) {
		tds := taxoptTdRE.FindAllStringSubmatch(tr[1], -1)
		if len(tds) != wantCols {
			continue
		}
		cells := make([]string, 0, wantCols)
		for _, td := range tds {
			cells = append(cells, taxoptCellText(td[1]))
		}
		rows = append(rows, cells)
	}
	return rows
}

// TestWhatIfTaxOptimizer_CellsMatchHeaderColumns pins EVERY main-table cell to
// the column its header labels, per mode. The earlier substring tests could
// not see a mutation that swapped two same-format cells (e.g. Peak Rate ↔
// Roth Conv, or End Portfolio ↔ Lifetime Tax) because the swapped values were
// still present somewhere in the tbody; asserting exact text at the
// header-matched index makes any such swap render the wrong value under at
// least one header for the best-candidate row, whose columns are all distinct
// by fixture construction.
func TestWhatIfTaxOptimizer_CellsMatchHeaderColumns(t *testing.T) {
	// Expected visible cell text per header label, per candidate row
	// (row order matches taxOptimizerFixture: best first, then the
	// zero-valued candidate). Δ for the zero candidate is $0.00 because it
	// doubles as Baseline in the fixture.
	baseWant := []map[string]string{
		{
			"Strategy":      "Fill 24% bracket, 67→72 &#9733;",
			"SS (P/S)":      "67",
			"End Portfolio": "$900,000.00",
			"Lifetime Tax":  "$150,000.00",
			"Peak Rate":     "40%", // 40.5 → "40" per %.0f round-half-to-even
			"Roth Conv":     "$123,456.00",
			"Δ vs Baseline": "+$50,000.00",
		},
		{
			"Strategy":      "No conversions",
			"SS (P/S)":      "67",
			"End Portfolio": "$850,000.00",
			"Lifetime Tax":  "$180,000.00",
			"Peak Rate":     "&mdash;",
			"Roth Conv":     "&mdash;",
			"Δ vs Baseline": "$0.00",
		},
	}
	mcExtra := []map[string]string{
		{"MC Median ▼": "$875,000.00", "MC Surv%": "91%"},
		{"MC Median ▼": "$810,000.00", "MC Surv%": "84%"},
	}

	cases := []struct {
		name           string
		monteCarloRuns int
		withMC         bool
	}{
		{"without Monte Carlo columns", 0, false},
		{"with Monte Carlo columns", 500, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantRows := make([]map[string]string, len(baseWant))
			for r, base := range baseWant {
				want := make(map[string]string, len(base)+2)
				maps.Copy(want, base)
				if tc.withMC {
					maps.Copy(want, mcExtra[r])
				}
				wantRows[r] = want
			}

			out := renderTaxOptimizer(t, tc.monteCarloRuns)
			idx := taxoptHeaderIndex(t, out)

			// Header set must match the expectation set exactly, so every
			// rendered column is covered — a new column cannot be added to
			// the template without an expectation here.
			for label := range idx {
				if _, ok := wantRows[0][label]; !ok {
					t.Errorf("rendered header %q has no cell expectation — add it to this test", label)
				}
			}
			for label := range wantRows[0] {
				if _, ok := idx[label]; !ok {
					t.Errorf("expected header %q not rendered in thead", label)
				}
			}
			if t.Failed() {
				// Header/expectation sets disagree — positional cell
				// checks below would be misleading noise.
				t.FailNow()
			}

			rows := taxoptCandidateRows(t, out, len(idx))
			if len(rows) != len(wantRows) {
				t.Fatalf("expected %d candidate rows with %d cells each, got %d (a row with a missing/extra <td> is skipped): %s",
					len(wantRows), len(idx), len(rows), truncate(tbodyOf(t, out), 2000))
			}

			for r, want := range wantRows {
				for label, wantText := range want {
					col, ok := idx[label]
					if !ok {
						continue // already reported above
					}
					if got := rows[r][col]; got != wantText {
						t.Errorf("row %d, column %q (index %d): got %q, want %q", r, label, col, got, wantText)
					}
				}
			}
		})
	}
}
