package templates

import (
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
)

// IP1 test 1: the contributor bar's inline width is a literal percentage
// (no ZgotmplZ), the largest |Change.Amount| renders 100%, and a smaller
// row renders the rounded proportional value.
func TestIP1ContributorBarWidths(t *testing.T) {
	period := rf3Period(true)
	inv := rf3BaseInvestigation(0, 0.0, 0.0)
	inv["Current"] = 500.0
	inv["Previous"] = 400.0
	inv["Change"] = models.ChangeCell{Kind: "dollar", Amount: 100.0, Direction: "up"}
	inv["Contributors"] = []models.CategoryTrend{
		{Category: "Largest", Change: models.ChangeCell{Amount: 200.0, Direction: "up"}},
		{Category: "Half", Change: models.ChangeCell{Amount: 100.0, Direction: "down"}},
		{Category: "Quarter", Change: models.ChangeCell{Amount: 50.0, Direction: "stable"}},
	}

	out := rf3Render(t, period, inv)

	if strings.Contains(out, "ZgotmplZ") {
		t.Fatal("output contains ZgotmplZ -- computed style width was rejected by html/template's CSS filter")
	}
	for _, want := range []string{"width: 100%", "width: 50%", "width: 25%"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing bar width %q in:\n%s", want, out)
		}
	}
}

// IP1 test 2: anomaly findings carry a severity badge with the right text
// ("High severity" / "Medium severity"); price-creep findings carry NO
// severity badge. The finding-type chip always carries .Label.
func TestIP1FindingSeverityBadges(t *testing.T) {
	period := rf3Period(true)
	tx := func(hash, desc, cat string, amount float64, d string) models.Transaction {
		date, _ := time.Parse("2006-01-02", d)
		return models.Transaction{Hash: hash, Description: desc, Category: cat, Amount: amount, Date: date}
	}
	findingHigh := map[string]any{
		"Type": "anomaly", "Label": "Unusual amount", "Evidence": "high severity", "Severity": "high",
		"URL": "/explorer?search=repair", "Creep": nil,
		"Transaction": tx("h-high", "One-off Repair", "Auto", -400, "2026-08-12"),
	}
	findingMedium := map[string]any{
		"Type": "anomaly", "Label": "Unusual amount", "Evidence": "medium severity", "Severity": "medium",
		"URL": "/explorer?search=gift", "Creep": nil,
		"Transaction": tx("h-medium", "Gift Shop", "Shopping", -80, "2026-08-13"),
	}
	findingCreep := map[string]any{
		"Type": "price-creep", "Label": "Price creep", "Evidence": "Median of first three vs last three charges",
		"Severity": "", "URL": "/explorer?search=coffee",
		"Transaction": tx("h-creep", "Coffee Shop", "Dining", -12.5, "2026-08-10"),
		"Creep": map[string]any{
			"FirstAmount": 10.0, "CurrentAmount": 12.5, "PctChange": 25.0,
			"FirstDate": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "LastDate": time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		},
	}

	inv := rf3BaseInvestigation(3, 0.0, 0.0)
	all := []any{findingHigh, findingMedium, findingCreep}
	inv["Findings"] = all
	inv["Preview"] = all
	inv["Remaining"] = []any{}

	out := rf3Render(t, period, inv)

	if n := strings.Count(out, "High severity"); n != 1 {
		t.Errorf("High severity badge count = %d, want 1", n)
	}
	if n := strings.Count(out, "Medium severity"); n != 1 {
		t.Errorf("Medium severity badge count = %d, want 1", n)
	}
	if n := strings.Count(out, ">Unusual amount<"); n != 2 {
		t.Errorf("Unusual amount chip count = %d, want 2", n)
	}
	if !strings.Contains(out, ">Price creep<") {
		t.Error("missing Price creep chip label")
	}
	// The price-creep row's evidence sentence must still render.
	if !strings.Contains(out, "Median of first three vs last three charges: $10.00") {
		t.Error("missing price-creep evidence sentence")
	}
	// Anomaly rows no longer render Evidence ("<sev> severity") as prose.
	if strings.Contains(out, "high severity") || strings.Contains(out, "medium severity") {
		t.Error("anomaly rows must not render the old lowercase '<sev> severity' prose")
	}
}

// IP1 test 3: shared/period-context Compact rendering. The stale callout
// with "Note:" renders only when Compact AND Stale; the non-Compact call
// shape (today's contract, used by Dashboard/RF3/LT1/DI2) never renders
// Compact-only classes, regardless of Stale.
func TestIP1PeriodContextCompact(t *testing.T) {
	renderer := lt5Renderer(t)
	day := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	base := models.PeriodContext{
		SelectedStart: day("2026-08-01"), SelectedEnd: day("2026-08-31"), SelectedDays: 31,
		PreviousStart: day("2026-07-01"), PreviousEnd: day("2026-07-31"), PreviousDays: 31,
		HasData: true, LatestTransaction: day("2026-08-20"),
		HistoryAvailable: true,
	}

	stale := base
	stale.Stale = true
	stale.DataAgeDays = 11

	fresh := base
	fresh.Stale = false

	const calloutOpen = `<p class="rounded-lg border border-warning bg-warning-soft text-warning px-3 py-2 text-sm"><span class="font-semibold">Note:</span>`

	// Compact + stale: callout renders.
	out, err := renderer.RenderToString("shared/period-context", map[string]any{"Period": stale, "Compact": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, calloutOpen) {
		t.Errorf("compact+stale must render the Note callout: %s", out)
	}
	if !strings.Contains(out, `<section class="space-y-2">`) {
		t.Error("compact must render the chrome-less section wrapper")
	}
	if strings.Contains(out, "bg-white dark:bg-gray-800 rounded-lg shadow p-4") {
		t.Error("compact must not render card chrome")
	}

	// Compact + fresh: no callout.
	out, err = renderer.RenderToString("shared/period-context", map[string]any{"Period": fresh, "Compact": true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Note:") {
		t.Error("compact+fresh must not render the Note callout")
	}

	// Non-Compact (today's call shape, map without "Compact"): stale fixture
	// must render the plain-paragraph stale sentence, never the callout.
	out, err = renderer.RenderToString("shared/period-context", map[string]any{"Period": stale})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, calloutOpen) {
		t.Error("non-compact must never render the compact callout markup")
	}
	if strings.Contains(out, `<section class="space-y-2">`) {
		t.Error("non-compact must never render the compact section wrapper")
	}
	if !strings.Contains(out, `<p class="text-sm text-gray-700 dark:text-gray-300">Data is more than seven calendar days old (11 days). Current-month forecast is unavailable.</p>`) {
		t.Errorf("non-compact stale sentence must render unchanged: %s", out)
	}
	if !strings.Contains(out, `<section class="bg-white dark:bg-gray-800 rounded-lg shadow p-4">`) {
		t.Error("non-compact must render the original card chrome")
	}
}

// IP1 test 4: the in-page nav has all four hrefs.
func TestIP1OnThisPageNav(t *testing.T) {
	period := rf3Period(true)
	inv := rf3BaseInvestigation(0, 0.0, 0.0)

	out := rf3Render(t, period, inv)

	navAt := strings.Index(out, `aria-label="On this page"`)
	if navAt < 0 {
		t.Fatal("missing on-this-page nav")
	}
	for _, href := range []string{`href="#insights-change"`, `href="#insights-findings"`, `href="#insights-recurring"`, `href="#insights-supporting"`} {
		if !strings.Contains(out, href) {
			t.Errorf("nav missing %s", href)
		}
	}
}

// IP1 test 5: the recurring summary strip renders one link per group, with
// href="#recurring-<id>" and the group's formatMoney .Monthly.
func TestIP1RecurringStrip(t *testing.T) {
	period := rf3Period(true)
	inv := rf3BaseInvestigation(0, 0.0, 0.0)
	inv["Groups"] = []any{
		map[string]any{"ID": "subscriptions", "Label": "Detected subscriptions", "Monthly": 12.34, "Annual": 148.08, "Rows": []any{}},
		map[string]any{"ID": "bills", "Label": "Recurring bills", "Monthly": 56.0, "Annual": 672.0, "Rows": []any{}},
	}

	out := rf3Render(t, period, inv)

	for _, want := range []string{
		`href="#recurring-subscriptions"`, "$12.34",
		`href="#recurring-bills"`, "$56.00",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recurring strip missing %q in:\n%s", want, out)
		}
	}
}

// IP1 attempt 2 (ruling IP-2026-09-13a): the Compact period context must be
// wired on the RENDERED PAGE, not only in the partial. A stale period renders
// the "Note:" callout inside #period-context with no card chrome; a fresh
// period renders neither the callout nor the chrome.
func TestIP1PageWiresCompactPeriodContext(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stale bool
	}{{"stale", true}, {"fresh", false}} {
		t.Run(tc.name, func(t *testing.T) {
			period := rf3Period(true)
			period.Stale = tc.stale
			period.DataAgeDays = 17
			out := rf3Render(t, period, rf3BaseInvestigation(0, 0.0, 0.0))
			start := strings.Index(out, `<div id="period-context">`)
			end := strings.Index(out, `id="insights-lead"`)
			if start < 0 || end < 0 || end < start {
				t.Fatalf("period-context/insights-lead not found in order: %d %d", start, end)
			}
			ctx := out[start:end]
			if strings.Contains(ctx, "bg-white dark:bg-gray-800 rounded-lg shadow p-4") {
				t.Error("page must render the Compact period context (no card chrome)")
			}
			if !strings.Contains(ctx, `<section class="space-y-2">`) {
				t.Error("page must render the Compact section wrapper")
			}
			hasNote := strings.Contains(ctx, `<span class="font-semibold">Note:</span> Data is more than seven calendar days old (17 days).`)
			if hasNote != tc.stale {
				t.Errorf("Note callout present=%v, want %v:\n%s", hasNote, tc.stale, ctx)
			}
		})
	}
}

// IP1 attempt 2 (checker-a11y FAIL, ACCESSIBILITY.md §7): the five neutral
// tiles must not use the sub-3:1 border-gray-200/700 pair; the pinned pair
// measures ≥ 4.4:1 (light, stone-500 on white/stone-100) and ≥ 6.0:1 (dark,
// stone-400 on stone-800/900).
func TestIP1NeutralTileBorderContrast(t *testing.T) {
	period := rf3Period(true)
	inv := rf3BaseInvestigation(0, 0.0, 0.0)
	inv["Current"] = 500.0
	inv["Previous"] = 500.0
	inv["Change"] = models.ChangeCell{Kind: "none", Amount: 0.0, Direction: "stable"}
	inv["Groups"] = []any{
		map[string]any{"ID": "subscriptions", "Label": "Detected subscriptions", "Rows": []any{}, "Monthly": 1.0, "Annual": 12.0},
		map[string]any{"ID": "bills", "Label": "Recurring bills", "Rows": []any{}, "Monthly": 2.0, "Annual": 24.0},
		map[string]any{"ID": "other", "Label": "Other recurring spending", "Rows": []any{}, "Monthly": 3.0, "Annual": 36.0},
	}
	out := rf3Render(t, period, inv)
	if strings.Contains(out, `border-gray-200 dark:border-gray-700 p-4`) {
		t.Error("a tile still uses the sub-3:1 border pair")
	}
	const pair = `border-gray-500 dark:border-gray-400`
	// Selected + Prior + Change(neutral fallback) + three recurring-strip links.
	if n := strings.Count(out, `rounded-xl border `+pair+` p-4`) + strings.Count(out, `rounded-xl border p-4 `+pair); n != 6 {
		t.Errorf("expected 6 neutral tiles with the ≥3:1 border pair, got %d", n)
	}
}
