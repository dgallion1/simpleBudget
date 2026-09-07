package dashboard

import (
	"encoding/csv"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestDI5MoneySurfaces(t *testing.T) {
	for _, tc := range []struct {
		name, income, charge, refund string
		shown, csv                   []string
	}{
		{"fractional", "10.006", "-0.004", "0", []string{"$10.01", "$0.00", "$10.01"}, []string{"2026-01", "10.01", "0.00", "10.01"}},
		{"boundary", "2.675", "-0.004", "0", []string{"$2.67", "$0.00", "$2.67"}, []string{"2026-01", "2.67", "0.00", "2.67"}},
		{"refund", "10", "-1", "3", []string{"$10.00", "-$2.00", "$12.00"}, []string{"2026-01", "10.00", "-2.00", "12.00"}},
		{"zero", "10.001", "-10.004", "0", []string{"$10.00", "$10.00", "$0.00"}, []string{"2026-01", "10.00", "10.00", "0.00"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, done := setupTestEnvWithRenderer(t, [][]string{{"2026-01-12", "PAYCHECK", tc.income, "Income"}, {"2026-01-14", "Home purchase", tc.charge, "Home"}, {"2026-01-15", "Boutique Store Credit", tc.refund, "Home"}, {"2026-02-02", "PAYCHECK", "999", "Income"}})
			defer done()
			const q = "?start=2026-01-01&end=2026-01-31"
			for _, path := range []string{"/dashboard", "/dashboard/kpis"} {
				body := doGet(t, r, path+q).Body.String()
				for i, label := range []string{"Recorded income", "Net spending", "Cash-flow balance"} {
					if got := extractAfterLabel(t, body, label); got != tc.shown[i] {
						t.Fatalf("%s %s got %s want %s", path, label, got, tc.shown[i])
					}
				}
				if !strings.Contains(body, "/whatif") || !strings.Contains(body, "Not set") {
					t.Fatal("missing no-plan actuals guidance")
				}
			}
			for i, kind := range []string{"income", "expenses", "savings"} {
				body := doGet(t, r, "/dashboard/kpi/"+kind+q).Body.String()
				if got := extractAfterLabel(t, body, "Total"); got != tc.shown[i] {
					t.Fatalf("%s detail got %s want %s", kind, got, tc.shown[i])
				}
			}
			body := doGet(t, r, "/dashboard/kpi/savings/month/2026-01"+q).Body.String()
			for i, label := range []string{"Income", "Expenses", "Net"} {
				if got := extractAfterLabel(t, body, label); got != tc.shown[i] {
					t.Fatalf("month %s got %s want %s", label, got, tc.shown[i])
				}
			}
			rows, err := csv.NewReader(strings.NewReader(doGet(t, r, "/dashboard/kpi/savings/export"+q).Body.String())).ReadAll()
			if err != nil || len(rows) != 2 || !reflect.DeepEqual(rows[1], tc.csv) {
				t.Fatalf("CSV %v err %v", rows, err)
			}
			t.Logf("full/HTMX/detail/month/CSV exact values: %v", tc.shown)
		})
	}
}
func TestDI5MonthlyRenderedResidual(t *testing.T) {
	r, done := setupTestEnvWithRenderer(t, [][]string{{"2026-01-12", "Home purchase", "-0.004", "Home"}, {"2026-02-12", "Home purchase", "-0.004", "Home"}})
	defer done()
	const q = "?start=2026-01-01&end=2026-02-28"
	for _, tc := range []struct{ kind, total, adjust string }{{"expenses", "$0.01", "$0.01"}, {"savings", "-$0.01", "-$0.01"}} {
		b := doGet(t, r, "/dashboard/kpi/"+tc.kind+q).Body.String()
		if got := extractAfterLabel(t, b, "Total"); got != tc.total {
			t.Fatalf("total %s want %s", got, tc.total)
		}
		if strings.Count(b, "Rounding adjustment: "+tc.adjust+".") != 1 {
			t.Fatal("exact adjustment missing or repeated")
		}
		for _, month := range []string{"2026-01", "2026-02"} {
			re := regexp.MustCompile("(?s)<tr[^>]*data-kpi-month-detail=\"" + tc.kind + "\\|" + month + "\"[^>]*>(.*?)</tr>")
			m := re.FindStringSubmatch(b)
			if m == nil {
				t.Fatal("missing row " + month)
			}
			cells := regexp.MustCompile(`<td[^>]*>(-?\$[0-9,.]+)</td>`).FindAllStringSubmatch(m[1], -1)
			if len(cells) == 0 || cells[0][1] != "$0.00" {
				t.Fatalf("%s row: %v", month, cells)
			}
		}
		t.Logf("%s total=%s rows=$0.00/$0.00 explicit adjustment=%s", tc.kind, tc.total, tc.adjust)
	}
}

func di5RenderedCents(t *testing.T, s string) int64 {
	t.Helper()
	s = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "$", ""), ",", "")
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) != 2 || len(parts[1]) != 2 {
		t.Fatalf("not rendered money %q", s)
	}
	n, e := strconv.ParseInt(parts[0]+parts[1], 10, 64)
	if e != nil {
		t.Fatal(e)
	}
	if negative {
		return -n
	}
	return n
}

func TestDI5RenderedResiduals(t *testing.T) {
	for _, value := range []string{"0.004", "0.006", "2.675", "10.006"} {
		t.Run(value, func(t *testing.T) {
			rows := [][]string{{"2026-01-12", "PAYCHECK", value, "Income"}, {"2026-02-12", "PAYCHECK", value, "Income"}, {"2026-01-14", "Home purchase", "-0.004", "Home"}, {"2026-02-14", "Home purchase", "-0.004", "Home"}}
			router, cleanup := setupTestEnvWithRenderer(t, rows)
			defer cleanup()
			const window = "start=2026-01-01&end=2026-02-28"
			var headline map[string]int64
			for _, path := range []string{"/dashboard", "/dashboard/kpis"} {
				body := doGet(t, router, path+"?"+window).Body.String()
				headline = map[string]int64{}
				for _, label := range []string{"Recorded income", "Net spending", "Cash-flow balance"} {
					headline[label] = di5RenderedCents(t, extractAfterLabel(t, body, label))
				}
				if headline["Recorded income"]-headline["Net spending"] != headline["Cash-flow balance"] {
					t.Fatalf("rendered equation fails: %v", headline)
				}
				if headline["Cash-flow balance"] == 0 && (!strings.Contains(body, "Recorded income matches spending") || strings.Contains(body, "Recorded income above spending") || strings.Contains(body, "Spending not covered by recorded income")) {
					t.Fatal("displayed zero falsely classified")
				}
			}
			for _, kind := range []string{"income", "expenses", "savings", "living"} {
				body := doGet(t, router, "/dashboard/kpi/"+kind+"?"+window).Body.String()
				total := di5RenderedCents(t, extractAfterLabel(t, body, "Total"))
				var sum int64
				rowMatches := regexp.MustCompile(`(?s)<tr[^>]*data-kpi-month-detail="[^"]+"[^>]*>(.*?)</tr>`).FindAllStringSubmatch(body, -1)
				if len(rowMatches) != 2 {
					t.Fatalf("%s monthly rows=%d", kind, len(rowMatches))
				}
				for _, row := range rowMatches {
					money := regexp.MustCompile(`>\s*(-?\$[0-9,.]+)\s*<`).FindAllStringSubmatch(row[1], -1)
					if len(money) == 0 {
						t.Fatal("no rendered monthly value")
					}
					sum += di5RenderedCents(t, money[0][1])
					if kind == "savings" && (len(money) != 3 || di5RenderedCents(t, money[1][1])-di5RenderedCents(t, money[2][1]) != di5RenderedCents(t, money[0][1])) {
						t.Fatalf("monthly displayed arithmetic: %+v", money)
					}
				}
				adjustment := int64(0)
				matches := regexp.MustCompile(`Rounding adjustment: (-?\$[0-9,]+\.[0-9]{2})`).FindAllStringSubmatch(body, -1)
				if len(matches) > 1 {
					t.Fatal("duplicated adjustment")
				}
				if len(matches) == 1 {
					adjustment = di5RenderedCents(t, matches[0][1])
				}
				if sum+adjustment != total {
					t.Errorf("%s rendered rows %d + adjustment %d != total %d", kind, sum, adjustment, total)
				}
				label := map[string]string{"income": "Recorded income", "expenses": "Net spending", "savings": "Cash-flow balance"}[kind]
				if label != "" && total != headline[label] {
					t.Errorf("%s modal %d != headline %d", kind, total, headline[label])
				}
				t.Logf("fixture=%s kind=%s rowCents=%d adjustmentCents=%d totalCents=%d", value, kind, sum, adjustment, total)
			}
		})
	}
}
