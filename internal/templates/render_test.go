package templates

import (
	"fmt"
	"io/fs"
	"math"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/insights"
	"budget2/web"
)

func TestRenderInsightsContentAllowsBlankSubscriptionDescriptions(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}

	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}

	html, err := renderer.RenderToString("insights-content", map[string]any{
		// The verdict band requires a typed Health; this test doesn't
		// exercise it, so use a neutral no-data verdict.
		"PaceVerdict": map[string]any{"Health": models.HealthNeutral, "HasData": false},
		"MinDate":     "2024-01-01",
		"MaxDate":     "2024-12-31",
		"StartDate":   "2024-01-01",
		"EndDate":     "2024-12-31",
		"Insights": models.InsightsData{
			Subscriptions: []models.RecurringPayment{
				{
					Description: "",
					Frequency:   "monthly",
					Amount:      12.34,
					AnnualCost:  148.08,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderToString() error: %v", err)
	}

	if !strings.Contains(html, "Unlabeled subscription") {
		t.Fatalf("expected fallback subscription label, got %q", html)
	}
	if !strings.Contains(html, ">?<") {
		t.Fatalf("expected fallback subscription initial, got %q", html)
	}
}

func TestRenderProjectionBreakdownCard(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}

	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}

	html, err := renderer.RenderToString("whatif-projection-breakdown", map[string]any{
		"Settings": models.DefaultWhatIfSettings(),
		"Analysis": &models.WhatIfAnalysis{
			ProjectionExplainability: &models.ProjectionExplainability{
				YearlySummaries: []models.ProjectionYearSummary{
					{
						Year:              0,
						StartingBalance:   1_000_000,
						Growth:            40_000,
						GrossIncome:       60_000,
						Taxes:             9_000,
						Expenses:          55_000,
						Withdrawals:       4_000,
						EndingBalance:     1_041_000,
						EndingBalanceReal: 1_010_000,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderToString() error: %v", err)
	}

	if !strings.Contains(html, "Year-by-Year Projection") {
		t.Fatalf("expected projection breakdown title, got %q", html)
	}
	if !strings.Contains(html, "Gross cash in includes income, dividends, and portfolio distributions before taxes.") {
		t.Fatalf("expected projection breakdown cash-flow note, got %q", html)
	}
	if !strings.Contains(html, "Portfolio Out") {
		t.Fatalf("expected projection breakdown withdrawal label, got %q", html)
	}
	if !strings.Contains(html, "$1,041,000") {
		t.Fatalf("expected ending balance in rendered html, got %q", html)
	}
}

func TestRenderProjectionChartCardIncludesModelAssumptions(t *testing.T) {
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}

	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}

	settings := models.DefaultWhatIfSettings()
	settings.ProjectionTiming = models.ProjectionTimingMidMonth

	html, err := renderer.RenderToString("whatif-projection-chart", map[string]any{
		"Settings": settings,
		"Analysis": &models.WhatIfAnalysis{
			Projection: &models.ProjectionResult{
				FinalBalance: 1_250_000,
				Survives:     true,
			},
			ProjectionExplainability: &models.ProjectionExplainability{
				FinalBalanceReal:        900_000,
				InflationLossPercent:    28.0,
				TaxShareOfGrossCashFlow: 12.5,
				CumulativeInflation:     1.72,
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderToString() error: %v", err)
	}

	if !strings.Contains(html, "Model assumptions") {
		t.Fatalf("expected model assumptions summary, got %q", html)
	}
	if !strings.Contains(html, "Average-cost basis for taxable sales") {
		t.Fatalf("expected taxable basis assumption, got %q", html)
	}
	if !strings.Contains(html, "mid-month") {
		t.Fatalf("expected timing assumption in rendered html, got %q", html)
	}
}

// --- U5 contract v3 (SPEC §2d) rendered-string tests --------------------
//
// Two same-class FAILs preceded this version (U-2026-09-03d: dollar delta
// derived from unrounded floats; U-2026-09-03f: MajorExpenseTrends'
// float-noise sum classified on raw floats while the display rounded --
// a split classification). Contract v3's fix is "round at the source,
// one classifier owns Kind/Amount/Percent/Direction"; these tests assert
// self-consistency of the RENDERED strings at BOTH template sites, never
// recomputing an expectation from a raw float the producer already saw.
//
// Helper names in this file are deliberately distinct from the Tier-3
// oracle's planted helpers (oRow, oRender, oText, oRows, oMoney, oCents,
// oCheckRow, oCheckProducer, oTxn, ...) so accept.sh can plant its test
// files into these same packages without a symbol collision.

func trendsOutflow(desc, cat string, amount float64, date time.Time) models.Transaction {
	return models.Transaction{Description: desc, Category: cat, Amount: -amount, Date: date, TransactionType: models.Outflow}
}

func renderTrendsSite(t *testing.T, site string, trends []models.CategoryTrend) string {
	t.Helper()
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub() error: %v", err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS() error: %v", err)
	}
	var html string
	if site == "insights-content" {
		html, err = renderer.RenderToString("insights-content", map[string]any{
			"PaceVerdict": map[string]any{"Health": models.HealthNeutral, "HasData": false},
			"MinDate":     "2024-01-01", "MaxDate": "2025-02-28",
			"StartDate": "2025-02-01", "EndDate": "2025-02-28",
			"Insights": models.InsightsData{CategoryTrends: trends},
		})
	} else {
		html, err = renderer.RenderToString(site, map[string]any{"CategoryTrends": trends})
	}
	if err != nil {
		t.Fatalf("RenderToString(%s) error: %v", site, err)
	}
	return html
}

// extractRow isolates the single trends-table row for category from html
// (both template sites render one <tr>...</tr> per category with a
// data-category attribute) and returns its rendered Current, Previous, and
// Change cell text (trimmed), the Change cell's raw (untrimmed, tags
// included) HTML for accessible-text checks, and the arrow/color
// direction ("up"/"down"/"stable") read from the Change cell's classes.
func extractRow(t *testing.T, html, category string) (current, previous, change, rawChange, direction string) {
	t.Helper()
	rowRe := regexp.MustCompile(`(?s)data-category="` + regexp.QuoteMeta(category) + `".*?</tr>`)
	row := rowRe.FindString(html)
	if row == "" {
		t.Fatalf("row for category %q not found in rendered html", category)
	}

	curRe := regexp.MustCompile(`text-right text-gray-800 dark:text-gray-200">([^<]+)<`)
	prevRe := regexp.MustCompile(`text-right text-gray-600 dark:text-gray-400">([^<]+)<`)
	curM := curRe.FindStringSubmatch(row)
	prevM := prevRe.FindStringSubmatch(row)
	if curM == nil || prevM == nil {
		t.Fatalf("could not extract Current/Previous cells for category %q from row: %s", category, row)
	}

	// The Change cell: site 1 wraps it in <span class="num">; site 2 does
	// not. Either way it is the LAST <td>...</td> in the row.
	tdRe := regexp.MustCompile(`(?s)<td([^>]*)>(.*?)</td>`)
	tds := tdRe.FindAllStringSubmatch(row, -1)
	if len(tds) == 0 {
		t.Fatalf("no <td> cells found in row for category %q: %s", category, row)
	}
	lastAttrs, lastContent := tds[len(tds)-1][1], tds[len(tds)-1][2]
	rawChange = lastAttrs + lastContent

	direction = "stable"
	// U6: insights.html renders the arrow/color from the semantic tokens
	// (text-negative for "up" spend, text-positive for "down"), not the
	// pre-U6 text-rose/text-emerald hue literals.
	if strings.Contains(rawChange, "text-negative") {
		direction = "up"
	} else if strings.Contains(rawChange, "text-positive") {
		direction = "down"
	}

	// Strip ALL tags (SVG icons, sr-only spans) then collapse whitespace,
	// mirroring how a screen reader/plain-text view would present it --
	// this is deliberately the SAME technique the Tier-3 oracle uses.
	tagRe := regexp.MustCompile(`<[^>]+>`)
	wsRe := regexp.MustCompile(`\s+`)
	change = strings.TrimSpace(wsRe.ReplaceAllString(tagRe.ReplaceAllString(lastContent, " "), " "))

	return strings.TrimSpace(curM[1]), strings.TrimSpace(prevM[1]), change, rawChange, direction
}

// parseMoney parses a formatMoney-rendered string ("$1,234.56",
// "-$1,234.56", or "+$1,234.56") back to a float64.
func parseMoney(t *testing.T, s string) float64 {
	t.Helper()
	s = strings.TrimSpace(s)
	neg := false
	switch {
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	case strings.HasPrefix(s, "-"):
		neg = true
		s = s[1:]
	}
	s = strings.TrimPrefix(s, "$")
	s = strings.ReplaceAll(s, ",", "")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("parseMoney(%q): %v", s, err)
	}
	if neg {
		v = -v
	}
	return v
}

// roundToCentsForTest mirrors models.RoundToCents exactly (same
// fmt.Sprintf("%.2f") primitive) so test-side expectations use the SAME
// rounding rule as the code under test, never a hand-rolled alternative
// (ruling 2026-08-29b / the "two formatters" defect class).
func roundToCentsForTest(v float64) float64 {
	r, _ := strconv.ParseFloat(fmt.Sprintf("%.2f", v), 64)
	return r
}

// oCheckRowLocal is the persistent (non-ephemeral) equivalent of the
// Tier-3 oracle's oCheckRow: given a row's rendered Current/Previous/
// Change/rawChange/direction, it re-derives contract v3's expectations
// from the DISPLAYED strings only and asserts the cell and arrow agree.
func oCheckRowLocal(t *testing.T, site, category, curStr, prevStr, change, rawChange, direction string) {
	t.Helper()
	prev := parseMoney(t, prevStr)
	cur := parseMoney(t, curStr)
	if prev != roundToCentsForTest(prev) || cur != roundToCentsForTest(cur) {
		t.Errorf("[%s %s] displayed money not at cent precision: prev=%q cur=%q", site, category, prevStr, curStr)
	}
	diff := roundToCentsForTest(cur - prev)

	var pct float64
	switch {
	case prev == 0 && cur > 0:
		pct = 100
	case prev == 0 && cur < 0:
		pct = -100
	case prev == 0:
		pct = 0
	default:
		pct = diff / math.Abs(prev) * 100
	}
	wantDir := "stable"
	if pct > 5 {
		wantDir = "up"
	} else if pct < -5 {
		wantDir = "down"
	}
	var wantKind string
	switch {
	case prev == 0 && cur > 0:
		wantKind = "new"
	case prev == 0 && cur == 0:
		wantKind = "none"
	case (prev != 0 && math.Abs(prev) < 100) || (prev == 0 && cur < 0):
		wantKind = "dollar"
	default:
		wantKind = "percent"
	}

	switch wantKind {
	case "new":
		if change != "new" {
			t.Errorf("[%s %s] prev=%s cur=%s: want cell 'new', got %q", site, category, prevStr, curStr, change)
		}
	case "none":
		if !strings.Contains(change, "—") || !strings.Contains(strings.ToLower(rawChange), "no change") {
			t.Errorf("[%s %s] prev=%s cur=%s: want '—' with accessible 'no change', got text %q raw %q", site, category, prevStr, curStr, change, rawChange)
		}
		if strings.Contains(change, "%") || strings.Contains(change, "$") {
			t.Errorf("[%s %s] $0.00->$0.00 row shows a figure: %q", site, category, change)
		}
	case "dollar":
		if strings.Contains(change, "%") || change == "new" {
			t.Errorf("[%s %s] prev=%s cur=%s: want dollar delta, got %q", site, category, prevStr, curStr, change)
			break
		}
		got := parseMoney(t, change)
		if got != diff {
			t.Errorf("[%s %s] SUM INVARIANT: %s - %s = %.2f but cell shows %q", site, category, curStr, prevStr, diff, change)
		}
		if diff > 0 && !strings.HasPrefix(change, "+") {
			t.Errorf("[%s %s] positive dollar delta lacks '+': %q", site, category, change)
		}
		if diff < 0 && !(strings.HasPrefix(change, "-") || strings.HasPrefix(change, "−")) {
			t.Errorf("[%s %s] negative dollar delta lacks '-': %q", site, category, change)
		}
	case "percent":
		if !strings.HasSuffix(change, "%") {
			t.Errorf("[%s %s] prev=%s cur=%s: want percent, got %q", site, category, prevStr, curStr, change)
			break
		}
		ps := strings.TrimSuffix(strings.TrimLeft(change, "+"), "%")
		got, err := strconv.ParseFloat(strings.ReplaceAll(ps, "−", "-"), 64)
		if err != nil {
			t.Fatalf("[%s %s] parse percent %q: %v", site, category, change, err)
		}
		if math.Abs(got-pct) > 0.051 {
			t.Errorf("[%s %s] PERCENT vs DOLLARS: (%s - %s)/|%s| = %.1f%% but cell shows %q", site, category, curStr, prevStr, prevStr, pct, change)
		}
		if curStr == prevStr && got != 0 {
			t.Errorf("[%s %s] identical displayed amounts but percent %q", site, category, change)
		}
	}
	if direction != wantDir {
		t.Errorf("[%s %s] prev=%s cur=%s cell=%q: arrow/color says %s, displayed figures imply %s", site, category, prevStr, curStr, change, direction, wantDir)
	}
}

// TestRenderCategoryTrendsChangeCell_NamedFixtures drives real transactions
// through insights.CategoryTrends and renders the result at BOTH template
// sites, asserting the exact rendered Change text (and arrow) for one
// fixture per Change.Kind boundary plus the two prior FAILs' exact repros.
func TestRenderCategoryTrendsChangeCell_NamedFixtures(t *testing.T) {
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	prevDate := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	curDate := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)

	type fixture struct {
		name       string
		prev, cur  float64
		hasPrev    bool // false = no transaction at all in the previous window
		wantChange string
		wantDir    string
	}
	fixtures := []fixture{
		{"Dollar", 30.005, 6931.004, true, "+$6,901.00", "up"},
		{"New", 0, 12.345, false, "new", "up"},
		{"Percent", 250.555, 300.001, true, "+19.7%", "up"},
		// The checker's exact U-2026-09-03d repro: previous displays as
		// $100.00 (at the floor, NOT under it) -> percent, not dollar.
		{"Boundary", 99.995, 150.005, true, "+50.0%", "up"},
		// Named oracle fixture "dollar-rounding-divergence": kept for
		// boundary correctness even though (per checker-tests, U5 attempt
		// 2 addendum) it does NOT discriminate a round-after-subtract
		// implementation at real float64 runtime precision -- both give
		// $100.12 here. TestChangeDisplay_AmountUsesRoundedOperands_
		// DiscriminatesFromSumFirst (trends_test.go) is the fixture that
		// actually discriminates (4.246/686.823).
		{"DollarRoundingBoundary", 40.00, 140.115, true, "+$100.12", "up"},
		{"RoundThenSubtract", 4.246, 686.823, true, "+$682.57", "up"},
		{"PercentStable", 1000, 1020, true, "+2.0%", "stable"},
		{"DollarDown", 80, 20, true, "-$60.00", "down"},
	}

	var txns []models.Transaction
	for _, f := range fixtures {
		if f.hasPrev {
			txns = append(txns, trendsOutflow("x", f.name, f.prev, prevDate))
		}
		txns = append(txns, trendsOutflow("x", f.name, f.cur, curDate))
	}
	trends := insights.CategoryTrends(models.NewTransactionSet(txns), currentStart, currentEnd)

	for _, site := range []string{"insights-content", "category-trends"} {
		html := renderTrendsSite(t, site, trends)
		for _, f := range fixtures {
			curStr, prevStr, change, rawChange, direction := extractRow(t, html, f.name)
			if change != f.wantChange {
				t.Errorf("[%s %s] Change = %q, want %q (prev=%s cur=%s)", site, f.name, change, f.wantChange, prevStr, curStr)
			}
			if direction != f.wantDir {
				t.Errorf("[%s %s] direction = %s, want %s", site, f.name, direction, f.wantDir)
			}
			oCheckRowLocal(t, site, f.name, curStr, prevStr, change, rawChange, direction)
		}
	}
}

// TestRenderCategoryTrendsChangeCell_MajorExpenseSignedAndFloatNoise is the
// persistent version of the exact U-2026-09-03f repro through the REAL
// producer path (MajorExpenseTrends' signed totals), plus refund-only and
// refund-dominant-previous cases that only that producer can exercise.
func TestRenderCategoryTrendsChangeCell_MajorExpenseSignedAndFloatNoise(t *testing.T) {
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	prevDate := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	curDate := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)

	defs := []models.MajorExpense{
		{ID: "noise", Name: "Noise", Keywords: []string{"noise vendor"}},
		{ID: "refprev", Name: "RefundPrev", Keywords: []string{"refprev vendor"}},
		{ID: "refonly", Name: "RefundOnly", Keywords: []string{"refonly vendor"}},
		{ID: "steady", Name: "Steady", Keywords: []string{"steady vendor"}},
	}
	txns := []models.Transaction{
		// 0.10 + 0.20 charges and a 0.30 refund, nothing prior -> true
		// $0.00 -> $0.00 -- the exact U-2026-09-03f repro.
		trendsOutflow("Noise Vendor", "c", 0.10, curDate),
		trendsOutflow("Noise Vendor", "c", 0.20, curDate),
		trendsOutflow("Noise Vendor", "c", -0.30, curDate),
		// previous window refund-dominant (-50), current 40 -> dollar up.
		trendsOutflow("Refprev Vendor", "c", -50, prevDate),
		trendsOutflow("Refprev Vendor", "c", 40, curDate),
		// no previous, current is a lone refund (-12.34) -> dollar down.
		trendsOutflow("Refonly Vendor", "c", -12.34, curDate),
		// identical totals both windows via noisy multi-transaction sums.
		trendsOutflow("Steady Vendor", "c", 100.10, prevDate),
		trendsOutflow("Steady Vendor", "c", 200.20, prevDate),
		trendsOutflow("Steady Vendor", "c", 300.30, curDate),
	}
	trends := insights.MajorExpenseTrends(models.NewTransactionSet(txns), defs, nil, currentStart, currentEnd)

	want := map[string][2]string{
		"Noise":      {"—", "stable"},
		"RefundPrev": {"+$90.00", "up"},
		"RefundOnly": {"-$12.34", "down"},
		"Steady":     {"0.0%", "stable"},
	}
	for _, site := range []string{"insights-content", "category-trends"} {
		html := renderTrendsSite(t, site, trends)
		for name, w := range want {
			curStr, prevStr, change, rawChange, direction := extractRow(t, html, name)
			if name == "Noise" {
				if !strings.Contains(change, "—") {
					t.Errorf("[%s Noise] Change = %q, want '—' (prev=%s cur=%s)", site, change, prevStr, curStr)
				}
			} else if change != w[0] {
				t.Errorf("[%s %s] Change = %q, want %q (prev=%s cur=%s)", site, name, change, w[0], prevStr, curStr)
			}
			if direction != w[1] {
				t.Errorf("[%s %s] direction = %s, want %s", site, name, direction, w[1])
			}
			oCheckRowLocal(t, site, name, curStr, prevStr, change, rawChange, direction)
		}
	}
}

// TestRenderCategoryTrendsChangeCell_PropertySelfConsistency drives a
// smaller (persistent-suite-sized, not the oracle's 2000+ sweep) batch of
// random previous/current pairs -- including fractional cents, near-floor
// values, and multi-transaction float-noise sums -- through the real
// producer and BOTH template sites, and checks self-consistency of the
// rendered output via oCheckRowLocal (never re-deriving an expectation
// from the raw transaction amounts).
func TestRenderCategoryTrendsChangeCell_PropertySelfConsistency(t *testing.T) {
	currentStart := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	currentEnd := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	prevDate := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	curDate := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)

	rng := rand.New(rand.NewSource(20260903))
	amount := func() float64 {
		switch rng.Intn(6) {
		case 0:
			return 0
		case 1:
			return 99.99 + rng.Float64()*0.02 // straddle the $100 floor
		case 2:
			return math.Round(rng.Float64()*30000) / 100 // exact cent-valued
		default:
			return rng.Float64() * 300 // arbitrary fractional
		}
	}
	split := func(total float64) []float64 { // 1-4 transactions summing (with float noise) to total
		k := 1 + rng.Intn(4)
		if total == 0 || k == 1 {
			return []float64{total}
		}
		var parts []float64
		rem := total
		for i := 0; i < k-1; i++ {
			p := math.Round(rem*rng.Float64()*100) / 100
			parts = append(parts, p)
			rem -= p
		}
		return append(parts, rem)
	}

	checked := 0
	for batch := 0; batch < 40; batch++ {
		var txns []models.Transaction
		for i := 0; i < 10; i++ {
			name := fmt.Sprintf("cat-%03d-%d", batch, i)
			for _, a := range split(amount()) {
				txns = append(txns, trendsOutflow("x", name, a, prevDate))
			}
			for _, a := range split(amount()) {
				txns = append(txns, trendsOutflow("x", name, a, curDate))
			}
		}
		trends := insights.CategoryTrends(models.NewTransactionSet(txns), currentStart, currentEnd)
		site := "category-trends"
		if batch%10 == 0 {
			site = "insights-content"
		}
		html := renderTrendsSite(t, site, trends)
		for _, tr := range trends {
			curStr, prevStr, change, rawChange, direction := extractRow(t, html, tr.Category)
			oCheckRowLocal(t, site, tr.Category, curStr, prevStr, change, rawChange, direction)
			checked++
		}
	}
	if checked < 300 {
		t.Fatalf("property sweep checked only %d rows", checked)
	}
	t.Logf("property sweep: %d rendered rows checked", checked)
}
