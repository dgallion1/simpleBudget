package whatif

import (
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
	best := models.TaxOptimizerCandidate{
		PrimaryClaimAge:     67,
		RothStrategy:        models.RothOptimizerStrategy{Label: "Fill 24% bracket, 67→72"},
		EndingPortfolioReal: 900000,
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
