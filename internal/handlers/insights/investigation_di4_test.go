package insights

import (
	"budget2/internal/models"
	"budget2/internal/services/anomalies"
	insightssvc "budget2/internal/services/insights"
	"budget2/internal/services/pricecreep"
	"fmt"
	"math"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Removing the investigation or dropping the selected bounds must fail at
// the rendered handler boundary, including requests used for HTMX refresh.
func TestDI4InvestigationRendered(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, "Date,Description,Amount,Category\n2026-07-01,Home purchase,-100,Home\n2026-08-28,Home purchase,-200,Home\n")
	defer cleanup()
	for _, hx := range []bool{false, true} {
		r := httptest.NewRequest("GET", "/insights?start=2026-08-01&end=2026-08-31", nil)
		if hx {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		handleInsights(w, r)
		out := w.Body.String()
		last := -1
		for _, marker := range []string{`id="period-context"`, `id="insights-change"`, `id="insights-findings"`, `id="insights-recurring"`, `id="insights-supporting"`} {
			at := strings.Index(out, marker)
			if at < 0 || at <= last {
				t.Errorf("HX=%v missing/out-of-order %s", hx, marker)
			}
			last = at
		}
		for _, want := range []string{"2026-08-01", "2026-08-31", "2026-07-01", "2026-07-31", "$100.00", "$200.00", "What changed", "No detected findings in this period", "Detected subscriptions", "Recurring bills", "Other recurring spending"} {
			if !strings.Contains(out, want) {
				t.Errorf("HX=%v missing %q", hx, want)
			}
		}
	}
}

func TestDI4PartialsRespectSelection(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, "Date,Description,Amount,Category\n2026-07-01,July employer,1111,Income\n2026-07-15,July employer,1111,Income\n2026-08-01,August employer,2222,Income\n2026-08-15,August employer,2222,Income\n")
	defer cleanup()
	for _, path := range []string{"/insights/recurring", "/insights/income"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path+"?start=2026-08-01&end=2026-08-31", nil)
		if path == "/insights/recurring" {
			handleRecurringPartial(w, r)
		} else {
			handleIncomePartial(w, r)
		}
		out := w.Body.String()
		for _, want := range []string{"2026-08-01", "2026-08-31"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s lost %s", path, want)
			}
		}
		if path == "/insights/income" && (strings.Contains(out, "july employer") || !strings.Contains(out, "august employer")) {
			t.Errorf("income bypasses selection: %s", out)
		}
		if path == "/insights/recurring" {
			for _, want := range []string{"Detected subscriptions", "Recurring bills", "Other recurring spending", "rounded separately"} {
				if !strings.Contains(out, want) {
					t.Errorf("recurring missing %s", want)
				}
			}
		}
	}
}

func di4Date(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }

// Signed outflows include refunds but never transfers; ranking must use dollars,
// with category-name ties, rather than percentages or map iteration.
func TestDI4MoneyAndContributors(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, "Date,Description,Amount,Category\n")
	defer cleanup()
	old := loader
	loader = nil
	defer func() { loader = old }()
	ts := models.NewTransactionSet([]models.Transaction{
		{Date: di4Date("2026-07-01"), TransactionType: models.Outflow, Amount: -100, Category: "Zulu"},
		{Date: di4Date("2026-07-02"), TransactionType: models.Outflow, Amount: -100, Category: "Alpha"},
		{Date: di4Date("2026-07-03"), TransactionType: models.Outflow, Amount: -50, Category: "Beta"},
		{Date: di4Date("2026-07-04"), TransactionType: models.Outflow, Amount: -1, Category: "Tiny"},
		{Date: di4Date("2026-08-01"), TransactionType: models.Outflow, Amount: -200, Category: "Zulu"},
		{Date: di4Date("2026-08-02"), TransactionType: models.Outflow, Amount: 0, Category: "Alpha"},
		{Date: di4Date("2026-08-03"), TransactionType: models.Outflow, Amount: -60, Category: "Beta"},
		{Date: di4Date("2026-08-04"), TransactionType: models.Outflow, Amount: -3, Category: "Tiny"},
		{Date: di4Date("2026-08-05"), TransactionType: models.Outflow, Amount: 20, Category: "Refund"},
		{Date: di4Date("2026-08-06"), TransactionType: models.Transfer, Amount: -999, Category: "Transfer"},
	})
	p := insightssvc.BuildPeriodContext(ts, di4Date("2026-08-01"), di4Date("2026-08-31"), di4Date("2026-09-06"))
	data := &models.InsightsData{Period: &p, CategoryTrends: insightssvc.CategoryTrendsForPeriod(ts, p)}
	v := buildInvestigation(ts, data)
	if v.Current != 243 || v.Previous != 251 || v.Change.Amount != -8 {
		t.Fatalf("signed all-outflow comparison: %+v", v)
	}
	if len(v.Contributors) != 3 || v.Contributors[0].Category != "Alpha" || v.Contributors[1].Category != "Zulu" || v.Contributors[2].Category != "Refund" {
		t.Fatalf("dollar ranking: %+v", v.Contributors)
	}
	html, err := renderer.RenderToString("insights-content", map[string]any{"Insights": data, "Period": p, "Investigation": v, "StartDate": "2026-08-01", "EndDate": "2026-08-31"})
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for _, name := range []string{"Alpha", "Zulu", "Refund"} {
		at := strings.Index(html, `data-contributor="`+name+`"`)
		if at <= last {
			t.Fatal("rendered contributor order", name)
		}
		last = at
	}
	if strings.Contains(html, `data-contributor="Beta"`) || strings.Count(html, "data-contributor=") != 3 {
		t.Fatal("rendered contributor cap")
	}
	for _, money := range []string{"$243.00", "$251.00", "-$8.00"} {
		if !strings.Contains(html, money) {
			t.Fatal("rendered all-outflow comparison", money)
		}
	}
	p.HistoryAvailable = false
	v = buildInvestigation(ts, data)
	if len(v.Contributors) != 0 || v.Change.Kind != "" {
		t.Fatal("invented no-history comparison")
	}
	groups := recurringGroups(&models.InsightsData{Subscriptions: []models.RecurringPayment{{AnnualCost: 0.06, Amount: 0.005}, {AnnualCost: 0.06, Amount: 0.005}}, OtherRecurring: []models.RecurringPayment{{AnnualCost: -0.001}}})
	if groups[0].Monthly != 0.02 || groups[0].Annual != 0.12 || groups[0].Rows[0].PaymentEstimate != 0.01 {
		t.Fatalf("row precision/totals: %+v", groups)
	}
	if math.Signbit(groups[2].Rows[0].AnnualEstimate) {
		t.Fatal("negative zero estimate")
	}
}

// Real detector output, eleven groups, subset merchant variants, selected end-day
// timestamps, and duplicate evidence expose cap-before-count and label joins.
func TestDI4FindingsCompleteStableAndAnchored(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, "Date,Description,Amount,Category\n")
	defer cleanup()
	var tx []models.Transaction
	for i := 0; i < 11; i++ {
		for m := 1; m <= 6; m++ {
			name := fmt.Sprintf("Merchant%c Coffee", 'A'+i)
			if m == 6 {
				name += " Store"
			}
			amount := -10.0
			if m >= 4 {
				amount = -20
			}
			row := models.Transaction{Date: time.Date(2026, time.Month(m), 28, 12, 0, 0, 0, time.UTC), Description: name, Category: "Dining", Amount: amount, TransactionType: models.Outflow, Hash: fmt.Sprintf("g%d-m%d", i, m)}
			tx = append(tx, row)
		}
	}
	ts := models.NewTransactionSet(tx)
	p := insightssvc.BuildPeriodContext(ts, di4Date("2026-06-01"), di4Date("2026-06-28"), di4Date("2026-09-06"))
	creeps := pricecreep.Detect(*ts)
	if len(creeps) != 11 {
		t.Fatalf("fixture: %d creep groups", len(creeps))
	}
	historical := p
	historical.SelectedEnd = di4Date("2026-05-31")
	historical.SelectedStart = di4Date("2026-05-01")
	if got := buildFindings(ts, historical, nil, creeps); len(got) != 0 {
		t.Errorf("later full-history finding pulled into historical preview: %d", len(got))
	}
	flags := []anomalies.Anomaly{{Hash: "g0-m6", Method: "mad_category"}, {Hash: "g0-m6", Method: "mad_category"}, {Hash: "invented", Method: "mad_category"}, {Hash: "g0-m1", Method: "mad_category"}}
	findings := buildFindings(ts, p, flags, append(creeps, creeps[0]))
	if len(findings) != 12 {
		t.Fatalf("want 11 creeps + 1 anomaly, got %d", len(findings))
	}
	seen := map[string]bool{}
	for _, f := range findings {
		key := f.Transaction.Hash + "/" + f.Type
		if seen[key] {
			t.Fatal("duplicate", key)
		}
		seen[key] = true
		if f.Transaction.Date.Month() != 6 || f.Transaction.Hash == "" {
			t.Fatal("not selected transaction", f)
		}
		u, err := url.Parse(f.URL)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if q.Get("start") != "2026-06-01" || q.Get("end") != "2026-06-28" || q.Get("category") != "Dining" || q.Get("search") != f.Transaction.Description {
			t.Fatal("lost actual row/filter", f.URL)
		}
	}
	if !seen["g0-m6/anomaly"] || !seen["g0-m6/price-creep"] {
		t.Fatal("dedup collapsed different finding types")
	}
	for i, j := 0, len(tx)-1; i < j; i, j = i+1, j-1 {
		tx[i], tx[j] = tx[j], tx[i]
	}
	reversed := buildFindings(models.NewTransactionSet(tx), p, flags, creeps)
	for i, f := range findings {
		if f.Transaction.Hash != reversed[i].Transaction.Hash || f.Type != reversed[i].Type {
			t.Fatal("unstable finding order")
		}
	}
	old := loader
	loader = nil
	defer func() { loader = old }()
	v := buildInvestigation(ts, &models.InsightsData{Period: &p})
	if len(v.Preview) != 5 || len(v.Findings) < 11 || len(v.Preview)+len(v.Remaining) != len(v.Findings) {
		t.Fatal("capped full findings", v)
	}
	html, err := renderer.RenderToString("insights-content", map[string]any{"Insights": models.InsightsData{Period: &p}, "Period": p, "Investigation": v, "StartDate": "2026-06-01", "EndDate": "2026-06-28"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(html, "data-finding-hash=") != len(v.Findings) || !strings.Contains(html, `<details id="all-findings"`) {
		t.Fatal("all-results destination does not contain all real results")
	}
	for _, f := range v.Findings {
		if !strings.Contains(html, `id="finding-`+f.Type+"-"+f.Transaction.Hash+`"`) {
			t.Fatal("missing transaction anchor")
		}
	}
}

func TestDI4EmptyHistoryRendered(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, "Date,Description,Amount,Category\n")
	defer cleanup()
	w := httptest.NewRecorder()
	handleInsights(w, httptest.NewRequest("GET", "/insights?start=2026-08-01&end=2026-08-31", nil))
	out := w.Body.String()
	for _, want := range []string{"Not enough history to compare", "No detected findings in this period", "Import transactions", "No detected payments in this group"} {
		if !strings.Contains(out, want) {
			t.Error("missing empty guidance", want)
		}
	}
	if strings.Contains(out, `id="chart-trends"`) || strings.Contains(out, "Dollar difference:") {
		t.Fatal("empty history invented comparison")
	}
}
