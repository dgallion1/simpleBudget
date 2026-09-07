package insights

import (
	"budget2/internal/config"
	"budget2/internal/handlers/explorer"
	"budget2/internal/models"
	svc "budget2/internal/services/insights"
	"fmt"
	"github.com/go-chi/chi/v5"
	"html"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDI5HTTPFindingsLinks(t *testing.T) {
	var csv strings.Builder
	csv.WriteString("Date,Description,Amount,Category\n")
	for i := 0; i < 11; i++ {
		for m := 1; m <= 6; m++ {
			amount := -10
			if m >= 4 {
				amount = -20
			}
			fmt.Fprintf(&csv, "2026-%02d-28,Merchant%c Coffee,%d,Dining\n", m, 'A'+i, amount)
		}
	}
	cleanup := setupTestLoaderWithRenderer(t, csv.String())
	defer cleanup()
	explorer.Initialize(loader, renderer, &config.Config{}, nil)
	router := chi.NewRouter()
	explorer.RegisterRoutes(router)
	for _, hx := range []bool{false, true} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/insights?start=2026-06-01&end=2026-06-28", nil)
		if hx {
			r.Header.Set("HX-Request", "true")
		}
		handleInsights(w, r)
		body := w.Body.String()
		if !hx && os.Getenv("DI5_ARTIFACT_DIR") != "" {
			dir := filepath.Clean(os.Getenv("DI5_ARTIFACT_DIR"))
			if !strings.HasPrefix(dir, os.TempDir()+string(os.PathSeparator)) {
				t.Fatal("DI5 artifacts require a temporary output directory")
			}
			if err := os.WriteFile(filepath.Join(dir, "findings.html"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
		rows := regexp.MustCompile(`(?s)data-finding-hash="([^"]+)".*?<a[^>]+href="([^"]+)"`).FindAllStringSubmatch(body, -1)
		if len(rows) < 11 {
			t.Fatalf("HX=%v only%d findings", hx, len(rows))
		}
		start := strings.Index(body, `id="findings-preview"`)
		end := strings.Index(body[start:], `</ul>`) + start
		if strings.Count(body[start:end], "data-finding-hash=") != 5 {
			t.Fatal("preview not five")
		}
		if !strings.Contains(body, `<details id="all-findings"`) {
			t.Fatal("no disclosure")
		}
		for _, row := range rows {
			url := html.UnescapeString(row[2])
			out := httptest.NewRecorder()
			router.ServeHTTP(out, httptest.NewRequest("GET", url, nil))
			if out.Code != 200 || !strings.Contains(out.Body.String(), `data-hash="`+row[1]+`"`) {
				t.Fatalf("link lost actual hash %s: %s status%d", row[1], url, out.Code)
			}
		}
		t.Logf("HX=%v findings=%d preview=5 remaining=%d; every rendered URL returns its actual hash", hx, len(rows), len(rows)-5)
	}
}
func TestDI5RenderedFractional(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, "Date,Description,Amount,Category\n")
	defer cleanup()
	old := loader
	loader = nil
	defer func() { loader = old }()
	ts := models.NewTransactionSet([]models.Transaction{{Date: di4Date("2026-07-01"), TransactionType: models.Outflow, Amount: -0.004}, {Date: di4Date("2026-08-01"), TransactionType: models.Outflow, Amount: -12.681}, {Date: di4Date("2026-08-02"), TransactionType: models.Outflow, Amount: 2.675}, {Date: di4Date("2026-08-03"), TransactionType: models.Transfer, Amount: -999}})
	p := svc.BuildPeriodContext(ts, di4Date("2026-08-01"), di4Date("2026-08-31"), di4Date("2026-09-06"))
	data := &models.InsightsData{Period: &p, Subscriptions: []models.RecurringPayment{{Description: "Netflix", AnnualCost: 0.06, Amount: 2.675}, {Description: "Spotify", AnnualCost: 0.06, Amount: 0.005}}}
	v := buildInvestigation(ts, data)
	b, err := renderer.RenderToString("insights-content", map[string]any{"Insights": data, "Period": p, "Investigation": v, "StartDate": "2026-08-01", "EndDate": "2026-08-31"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`Selected period: <span class="num font-semibold">$10.01</span>`, `Prior period: <span class="num">$0.00</span>`, `+$10.01`, `<span class="num">$0.02</span> monthly`, `<span class="num">$0.12</span> annual`, `$2.67`, `$0.01`, `Monthly and annual figures are rounded separately.`} {
		if !strings.Contains(b, s) {
			t.Errorf("missing rendered %s", s)
		}
	}
	if strings.Contains(b, "-$0.00") {
		t.Fatal("negative zero")
	}
	t.Log("rendered signed aggregates $10.01/$0.00 delta+$10.01; displayed estimates monthly$0.02 annual$0.12; 2.675->$2.67; no negative zero")
}
func TestDI5GroupingLinksAndPartials(t *testing.T) {
	cleanup := setupTestLoaderWithRenderer(t, "Date,Description,Amount,Category\n2026-06-01,Electric Company,-10,Utilities\n2026-07-01,Electric Company,-10,Utilities\n2026-08-01,Electric Company,-20,Utilities\n2026-08-02,Unmatched Shop,-7,Shopping\n")
	defer cleanup()
	if err := loader.SaveMajorExpenses([]models.MajorExpense{{ID: "actual-power-id", Name: "Power & Light", Keywords: []string{"electric company"}}}); err != nil {
		t.Fatal(err)
	}
	explorer.Initialize(loader, renderer, &config.Config{}, nil)
	router := chi.NewRouter()
	explorer.RegisterRoutes(router)
	ts, err := loader.LoadData()
	if err != nil {
		t.Fatal(err)
	}
	wantHash := ""
	wrongHash := ""
	for _, tx := range ts.Transactions {
		if tx.Date.Equal(di4Date("2026-08-01")) {
			wantHash = tx.Hash
		}
		if tx.Date.Equal(di4Date("2026-08-02")) {
			wrongHash = tx.Hash
		}
	}
	if wantHash == "" || wrongHash == "" {
		t.Fatal("fixture hashes")
	}
	for _, kind := range []string{"full", "HX", "trends", "recurring", "income"} {
		r := httptest.NewRequest("GET", "/insights?start=2026-08-01&end=2026-08-31", nil)
		w := httptest.NewRecorder()
		switch kind {
		case "HX":
			r.Header.Set("HX-Request", "true")
			handleInsights(w, r)
		case "full":
			handleInsights(w, r)
		case "trends":
			handleTrendsPartial(w, r)
		case "recurring":
			handleRecurringPartial(w, r)
		case "income":
			handleIncomePartial(w, r)
		}
		b := w.Body.String()
		if w.Code != 200 || !strings.Contains(b, "2026-08-01") || !strings.Contains(b, "2026-08-31") {
			t.Fatalf("%s lost period or failed", kind)
		}
		links := regexp.MustCompile(`href="(/explorer\?[^"]+)"`).FindAllStringSubmatch(b, -1)
		checked := 0
		for _, link := range links {
			u := html.UnescapeString(link[1])
			if !strings.Contains(u, "majorExpense=") && !(kind == "recurring" && strings.Contains(u, "search=")) {
				continue
			}
			if strings.Contains(u, "majorExpense=") && !strings.Contains(u, "majorExpense=actual-power-id") {
				t.Fatalf("wrong ID %s", u)
			}
			if !strings.Contains(u, "start=2026-08-01") || !strings.Contains(u, "end=2026-08-31") {
				t.Fatalf("lost dates %s", u)
			}
			out := httptest.NewRecorder()
			router.ServeHTTP(out, httptest.NewRequest("GET", u, nil))
			if out.Code != 200 || !strings.Contains(out.Body.String(), `data-hash="`+wantHash+`"`) || strings.Contains(out.Body.String(), `data-hash="`+wrongHash+`"`) {
				t.Fatalf("wrong real Explorer rows for %s", u)
			}
			checked++
		}
		if kind != "income" && checked == 0 {
			t.Fatalf("%s no actual drilldown checked", kind)
		}
		if (kind == "full" || kind == "HX") && !strings.Contains(b, "Major Expenses (unmatched outflows excluded)") {
			t.Fatal("missing grouping scope")
		}
		if kind == "recurring" && !strings.Contains(b, "Expected payment estimate as of 2026-08-31") {
			t.Fatal("missing historical reference")
		}
		t.Logf("%s selected bounds present, %d actual ID/merchant Explorer links matched selected transaction and excluded unmatched row", kind, checked)
	}
}
