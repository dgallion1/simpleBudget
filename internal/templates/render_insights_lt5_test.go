package templates

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	insightssvc "budget2/internal/services/insights"
	"budget2/web"
)

func lt5Renderer(t *testing.T) *Renderer {
	t.Helper()
	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatal(err)
	}
	return renderer
}

// lt5Row mirrors the fields internal/handlers/insights.RecurringRowView
// exposes to the "insights-recurring-group"/"insights-recurring-table"
// templates (a local shape, since that package imports this one).
type lt5Row struct {
	Description, MajorExpenseName, ClassificationReason, Frequency string
	Occurrences                                                    int
	LastDate, NextExpected                                         time.Time
	Confidence                                                     float64
	PaymentEstimate, MonthlyEstimate, AnnualEstimate               float64
}

type lt5Group struct {
	ID, Label       string
	Rows            []lt5Row
	Monthly, Annual float64
}

func lt5TableRows(t *testing.T, html string) []string {
	t.Helper()
	rows := regexp.MustCompile(`(?s)<tr>(.*?)</tr>`).FindAllStringSubmatch(html, -1)
	if len(rows) == 0 {
		t.Fatal("no <tr> rows found")
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r[1])
	}
	return out
}

// LT5 acceptance 2(a): recurring rows compact with an Evidence disclosure,
// exact rendered money strings for a fractional-cents fixture.
func TestLT5RecurringRowEvidenceDisclosure(t *testing.T) {
	renderer := lt5Renderer(t)
	period := models.PeriodContext{SelectedEnd: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)}
	group := lt5Group{
		ID: "subscriptions", Label: "Detected subscriptions",
		Monthly: 158.24, Annual: 1899.00,
		Rows: []lt5Row{
			{
				Description: "Aster Gym", ClassificationReason: "Fixed amount, monthly cadence",
				Frequency: "monthly", Occurrences: 6,
				LastDate: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), NextExpected: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
				Confidence: 0.92, PaymentEstimate: 1655.30, MonthlyEstimate: 137.94, AnnualEstimate: 1655.30,
			},
			{
				Description: "Birch Storage", MajorExpenseName: "Household", ClassificationReason: "Regular weekly charge",
				Frequency: "weekly", Occurrences: 12,
				LastDate: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC), NextExpected: time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC),
				Confidence: 0.75, PaymentEstimate: 27.35, MonthlyEstimate: 20.30, AnnualEstimate: 243.70,
			},
		},
	}
	out, err := renderer.RenderToString("insights-recurring-group", map[string]any{
		"Group": group, "Period": period, "StartDate": "2026-06-01", "EndDate": "2026-08-31",
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := lt5TableRows(t, out)
	if len(rows) != 2 {
		t.Fatalf("got %d <tr> rows, want 2", len(rows))
	}
	wantMoney := [][3]string{
		{">$1,655.30<", ">$137.94<", ">$1,655.30<"},
		{">$27.35<", ">$20.30<", ">$243.70<"},
	}
	for i, row := range rows {
		if n := strings.Count(row, "<details"); n != 1 {
			t.Errorf("row %d: %d <details, want 1", i, n)
		}
		summaryMatch := regexp.MustCompile(`<summary[^>]*>([^<]*)</summary>`).FindStringSubmatch(row)
		if len(summaryMatch) != 2 || summaryMatch[1] != "Evidence" {
			t.Errorf("row %d: summary text = %q, want \"Evidence\"", i, summaryMatch)
		}
		detailsAt := strings.Index(row, "<details")
		for _, line := range []string{group.Rows[i].ClassificationReason, "occurrences", "Expected payment estimate"} {
			at := strings.Index(row, line)
			if at < 0 || at < detailsAt {
				t.Errorf("row %d: evidence line %q not found after <details (at=%d, detailsAt=%d)", i, line, at, detailsAt)
			}
		}
		for j, want := range wantMoney[i] {
			if !strings.Contains(row, want) {
				t.Errorf("row %d: missing money cell %q in %s", i, want, row)
			}
			_ = j
		}
	}
	if group.Rows[1].MajorExpenseName != "" {
		if !strings.Contains(rows[1], `<span class="text-gray-600 dark:text-gray-400">Major Expense: Household</span>`) {
			t.Error("missing inline Major Expense span on line 1")
		}
		// Major Expense annotation must be on the same line as the link, before the Evidence disclosure.
		if strings.Index(rows[1], "Major Expense: Household") > strings.Index(rows[1], "<details") {
			t.Error("Major Expense annotation rendered after the Evidence disclosure, not on line 1")
		}
	}
}

// LT5 acceptance 2(b): "Other recurring spending" is collapsed behind a
// closed <details>; subscriptions/bills render open with no such wrapper.
func TestLT5OtherRecurringCollapsedSubscriptionsOpen(t *testing.T) {
	renderer := lt5Renderer(t)
	period := models.PeriodContext{SelectedEnd: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)}
	mkRow := func(name string) lt5Row {
		return lt5Row{
			Description: name, ClassificationReason: "reason", Frequency: "monthly", Occurrences: 3,
			LastDate: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), NextExpected: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			Confidence: 0.5, PaymentEstimate: 10, MonthlyEstimate: 10, AnnualEstimate: 120,
		}
	}
	other := lt5Group{ID: "other", Label: "Other recurring spending", Rows: []lt5Row{mkRow("Alder"), mkRow("Birch")}}
	subs := lt5Group{ID: "subscriptions", Label: "Detected subscriptions", Rows: []lt5Row{mkRow("Netflix")}}

	outOther, err := renderer.RenderToString("insights-recurring-group", map[string]any{"Group": other, "Period": period, "StartDate": "2026-06-01", "EndDate": "2026-08-31"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(outOther, "<details open") {
		t.Error("other-recurring group's wrapping <details> must not be open")
	}
	detailsAt := strings.Index(outOther, "<details")
	tableAt := strings.Index(outOther, "<table")
	if detailsAt < 0 || tableAt < 0 || detailsAt > tableAt {
		t.Fatalf("expected a <details> wrapping <table>: detailsAt=%d tableAt=%d", detailsAt, tableAt)
	}
	summaryMatch := regexp.MustCompile(`<summary[^>]*>([^<]*)</summary>`).FindStringSubmatch(outOther[detailsAt:tableAt])
	if len(summaryMatch) != 2 || strings.TrimSpace(summaryMatch[1]) != "Show all 2 series" {
		t.Errorf("wrapping summary = %q, want \"Show all 2 series\"", summaryMatch)
	}

	outSubs, err := renderer.RenderToString("insights-recurring-group", map[string]any{"Group": subs, "Period": period, "StartDate": "2026-06-01", "EndDate": "2026-08-31"})
	if err != nil {
		t.Fatal(err)
	}
	subsTableAt := strings.Index(outSubs, "<table")
	if subsTableAt < 0 {
		t.Fatal("subscriptions group missing <table")
	}
	if firstDetails := strings.Index(outSubs, "<details"); firstDetails >= 0 && firstDetails < subsTableAt {
		t.Errorf("subscriptions group has a <details> (at %d) before its <table> (at %d)", firstDetails, subsTableAt)
	}
}

// LT5 acceptance 2(c): the price-creep caveat renders exactly once, after
// the findings-preview list, only when a finding carries a Creep.
func TestLT5PriceCreepCaveatRendersOnce(t *testing.T) {
	renderer := lt5Renderer(t)
	const caveat = "actual amount may differ from the median"
	ts := models.NewTransactionSet(nil)
	p := insightssvc.BuildPeriodContext(ts, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC))

	creepFinding := map[string]any{
		"Type": "price-creep",
		"Transaction": models.Transaction{
			Hash: "hash-creep", Description: "Coffee Shop", Category: "Dining",
			Amount: -12.5, Date: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		},
		"Label": "Price creep", "Evidence": "Median of first three vs last three charges",
		"URL": "/explorer?search=coffee",
		"Creep": map[string]any{
			"FirstAmount": 10.0, "CurrentAmount": 12.5, "PctChange": 25.0,
			"FirstDate": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "LastDate": time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		},
	}
	outlierFinding := map[string]any{
		"Type": "anomaly",
		"Transaction": models.Transaction{
			Hash: "hash-outlier", Description: "One-off Repair", Category: "Auto",
			Amount: -400, Date: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC),
		},
		"Label": "Unusual amount", "Evidence": "high severity", "URL": "/explorer?search=repair",
		"Creep": nil,
	}

	build := func(findings []map[string]any) string {
		invFindings := make([]any, len(findings))
		for i, f := range findings {
			invFindings[i] = f
		}
		investigation := map[string]any{
			"Current": 0.0, "Findings": invFindings, "Preview": invFindings, "Remaining": []any{},
			"Groups": []any{}, "Monthly": 0.0, "Annual": 0.0,
		}
		out, err := renderer.RenderToString("insights-content", map[string]any{
			"Insights": &models.InsightsData{}, "Period": p, "Investigation": investigation,
			"StartDate": "2026-08-01", "EndDate": "2026-08-31",
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	withCreep := build([]map[string]any{creepFinding, outlierFinding})
	if n := strings.Count(withCreep, caveat); n != 1 {
		t.Fatalf("caveat occurred %d times with a creep finding present, want 1", n)
	}
	ulEnd := strings.Index(withCreep, `id="findings-preview"`)
	if ulEnd < 0 {
		t.Fatal("missing findings-preview")
	}
	ulEnd = strings.Index(withCreep[ulEnd:], "</ul>") + ulEnd
	if ulEnd < 0 {
		t.Fatal("missing findings-preview closing </ul>")
	}
	caveatAt := strings.Index(withCreep, caveat)
	if caveatAt < ulEnd {
		t.Fatalf("caveat at %d occurs before findings-preview's closing </ul> at %d", caveatAt, ulEnd)
	}

	withoutCreep := build([]map[string]any{outlierFinding})
	if n := strings.Count(withoutCreep, caveat); n != 0 {
		t.Fatalf("caveat occurred %d times with zero creep findings, want 0", n)
	}
}
