package insights

import (
	"budget2/internal/models"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func di5EstimateCents(t *testing.T, s string) int {
	t.Helper()
	s = strings.ReplaceAll(strings.ReplaceAll(s, "$", ""), ",", "")
	s = strings.ReplaceAll(s, ".", "")
	v, e := strconv.Atoi(s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestDI5RenderedEstimateSums(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, "Date,Description,Amount,Category\n")
	defer cleanup()
	rows := []models.RecurringPayment{{Description: "Aster", Amount: 2.675, AnnualCost: .06}, {Description: "Birch", Amount: .005, AnnualCost: .06}}
	p := models.PeriodContext{SelectedStart: di4Date("2026-08-01"), SelectedEnd: di4Date("2026-08-31")}
	groups := recurringGroups(&models.InsightsData{Subscriptions: rows, RecurringPayments: rows, OtherRecurring: rows})
	for _, g := range groups {
		html, e := renderer.RenderToString("insights-recurring-group", map[string]any{"Group": g, "Period": p, "StartDate": "2026-08-01", "EndDate": "2026-08-31"})
		if e != nil {
			t.Fatal(e)
		}
		money := regexp.MustCompile(`class="p-3 text-right num[^>]*>(-?\$[0-9,]+\.[0-9]{2})<`).FindAllStringSubmatch(html, -1)
		if len(money) != 6 {
			t.Fatalf("rendered numeric cells=%d want 6", len(money))
		}
		if money[0][1] != "$2.67" || money[3][1] != "$0.01" {
			t.Fatalf("payment precision %+v", money)
		}
		monthly := di5EstimateCents(t, money[1][1]) + di5EstimateCents(t, money[4][1])
		annual := di5EstimateCents(t, money[2][1]) + di5EstimateCents(t, money[5][1])
		totals := regexp.MustCompile(`Estimated monthly (\$[0-9,]+\.[0-9]{2}) · Estimated annual (\$[0-9,]+\.[0-9]{2})`).FindStringSubmatch(html)
		if len(totals) != 3 || di5EstimateCents(t, totals[1]) != monthly || di5EstimateCents(t, totals[2]) != annual {
			t.Fatalf("rendered group sums monthly=%d annual=%d totals=%v", monthly, annual, totals)
		}
		if monthly != 2 || annual != 12 {
			t.Fatal("monthly/annual estimates not independently rounded")
		}
	}
}
