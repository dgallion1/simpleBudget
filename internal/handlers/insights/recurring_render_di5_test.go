package insights

import (
	"budget2/internal/models"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// M03 retains the original DI1 merchant fixture in the final DI4 renderer.
func TestDI5RecurringRenderedRetention(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, "Date,Description,Amount,Category\n")
	defer cleanup()
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	var tx []models.Transaction
	for _, name := range []string{"Alder", "Birch", "Cedar", "Dogwood", "Elm", "Fir", "Ginkgo", "Hazel", "Ivy", "Juniper", "Kapok", "Larch", "Maple", "Nutmeg", "Oak", "Pine", "Quince", "Rowan", "Spruce", "Teak", "Electric Company", "Netflix"} {
		amount := -100.0
		if name == "Netflix" {
			amount = -10
		}
		for i := 0; i < 3; i++ {
			tx = append(tx, models.Transaction{Description: name, Category: "Household", Amount: amount, Date: now.AddDate(0, -i, 0), TransactionType: models.Outflow})
		}
	}
	ts := models.NewTransactionSet(tx)
	data := calculateInsightsAt(ts, ts, ts.MinDate(), now, now)
	for _, rows := range [][]models.RecurringPayment{data.Subscriptions, data.RecurringPayments, data.OtherRecurring} {
		for i := range rows {
			rows[i].MajorExpenseName = "Shared household label"
		}
	}
	v := buildInvestigation(ts, data)
	if v.Monthly != 2110 || v.Annual != 25320 || len(v.Groups[0].Rows) != 1 || v.Groups[0].Monthly != 10 || len(v.Groups[2].Rows) != 20 {
		t.Fatalf("fixture groups: %+v", v)
	}
	ctx := map[string]any{"Insights": data, "Period": *data.Period, "Investigation": v, "Groups": v.Groups, "StartDate": "2026-06-15", "EndDate": "2026-08-15"}
	for _, template := range []string{"insights-content", "insights-recurring-partial"} {
		body, err := renderer.RenderToString(template, ctx)
		if err != nil {
			t.Fatal(err)
		}
		if template == "insights-content" {
			if !strings.Contains(body, "$2,110.00") || !strings.Contains(body, "$25,320.00") {
				t.Fatal("missing grand totals")
			}
			start := strings.Index(body, `id="insights-recurring"`)
			end := strings.Index(body, `id="insights-supporting"`)
			if start < 0 || end <= start {
				t.Fatal("missing recurring section")
			}
			body = body[start:end]
		}
		if strings.Count(body, "Major Expense: Shared household label") != 22 {
			t.Fatal("category annotations merged merchants")
		}
		for _, group := range v.Groups {
			section := regexp.MustCompile(`(?s)<section[^>]*aria-labelledby="recurring-` + group.ID + `".*?</section>`).FindString(body)
			if strings.Count(section, "Expected payment estimate as of 2026-08-15:") != len(group.Rows) {
				t.Fatalf("%s %s retained rows", template, group.ID)
			}
			for _, row := range group.Rows {
				if !strings.Contains(section, "<span>"+row.Description+"</span>") || !strings.Contains(section, html.EscapeString(row.ClassificationReason)) {
					t.Fatalf("missing merchant/reason %s", row.Description)
				}
			}
		}
		for _, want := range []string{"1 retained series · Estimated monthly $10.00 · Estimated annual $120.00", "20 retained series · Estimated monthly $2,000.00 · Estimated annual $24,000.00"} {
			if !strings.Contains(body, want) {
				t.Fatalf("missing rendered %s", want)
			}
		}
		links := regexp.MustCompile(`href="(/explorer\?[^"]+)"`).FindAllStringSubmatch(body, -1)
		seen := map[string]bool{}
		for _, link := range links {
			u, err := url.Parse(html.UnescapeString(link[1]))
			if err != nil {
				t.Fatal(err)
			}
			q := u.Query()
			if q.Get("search") == "" {
				continue
			}
			if q.Get("start") != "2026-06-15" || q.Get("end") != "2026-08-15" || q.Get("type") != "Outflow" {
				t.Fatal("merchant link lost range/type")
			}
			seen[q.Get("search")] = true
		}
		if len(seen) != 22 || !seen["netflix"] || !seen["electric company"] {
			t.Fatalf("merchant navigation: %v", seen)
		}
	}
}
