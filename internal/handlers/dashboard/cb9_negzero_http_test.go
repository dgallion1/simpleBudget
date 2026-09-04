package dashboard

import (
	"encoding/csv"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"budget2/internal/services/retirement"

	"github.com/go-chi/chi/v5"
)

// setupCB9EnvWithTargets mirrors setupPlanExclusionEnv's target wiring
// (living + legacy healthcare monthly targets in whatif.json) so the
// budget-vs-actual chart's combined-target gate does not short-circuit to
// an empty payload; nil renderer keeps handleChartData on its JSON path.
func setupCB9EnvWithTargets(t *testing.T) (chi.Router, func()) {
	t.Helper()
	tmpDir, dl, store, cleanup := writeTempCSV(t, cb9CancellingRows())
	rm := retirement.NewSettingsManager(tmpDir, store)
	if err := os.WriteFile(filepath.Join(tmpDir, "whatif.json"), []byte(`{"monthly_living_expenses": 1200, "monthly_healthcare": 300}`), 0o600); err != nil {
		cleanup()
		t.Fatalf("write settings: %v", err)
	}
	Initialize(dl, nil, rm, store)
	r := chi.NewRouter()
	RegisterRoutes(r)
	return r, cleanup
}

// cb9CancellingRows builds a range whose February outflow buckets net to
// EXACTLY zero at every granularity the KPI modal/export slice them:
//   - healthcare: -300 premium + 300 refund (both Health Insurance)
//   - living:     -100 groceries + 100 store credit
//   - expenses:   the union, also 0
//
// January is ordinary so the range as a whole nets spend.
func cb9CancellingRows() [][]string {
	return [][]string{
		{"2025-01-15", "Salary", "4200", "Payroll"},
		{"2025-01-15", "Health Insurance Premium", "-300", "Health Insurance"},
		{"2025-01-10", "Groceries", "-220", "Food"},
		{"2025-02-15", "Salary", "4200", "Payroll"},
		{"2025-02-15", "Health Insurance Premium", "-300", "Health Insurance"},
		{"2025-02-20", "Health Insurance Premium Credit", "300", "Health Insurance"},
		{"2025-02-10", "Groceries", "-100", "Food"},
		{"2025-02-12", "Store Credit", "100", "Food"},
	}
}

// negZeroJSON matches a JSON number token of -0 (optionally -0.0...) that
// is NOT the prefix of a larger negative number.
var negZeroJSON = regexp.MustCompile(`-0(\.0+)?[,}\]\s]`)

// TestHandleKPIExport_CB9_CancellingMonthWritesPositiveZero: the CSV export
// is raw fmt %.2f (no formatMoney belt), so a -0 from `-set.SumAmount()` on
// an exactly-cancelling month printed "-0.00" (checker-second, CB7 attempt
// 3 observation). Every export kind must write "0.00" for February.
func TestHandleKPIExport_CB9_CancellingMonthWritesPositiveZero(t *testing.T) {
	router, cleanup := setupTestEnv(t, cb9CancellingRows())
	defer cleanup()

	for _, kind := range []string{"expenses", "living", "healthcare", "savings"} {
		rec := doGet(t, router, "/dashboard/kpi/"+kind+"/export?start=2025-01-01&end=2025-02-28")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", kind, rec.Code)
		}
		body := rec.Body.String()
		records, err := csv.NewReader(strings.NewReader(body)).ReadAll()
		if err != nil {
			t.Fatalf("%s: harness error parsing CSV: %v", kind, err)
		}
		var feb []string
		for _, row := range records {
			if len(row) > 0 && strings.HasPrefix(row[0], "2025-02") {
				feb = row
			}
		}
		if feb == nil {
			t.Fatalf("%s: harness error: no Feb row in export: %v", kind, records)
		}
		for i, cell := range feb[1:] {
			if strings.HasPrefix(cell, "-0") {
				t.Errorf("%s: Feb column %d = %q, want a positive-zero rendering (an inline -SumAmount() yields IEEE -0 and %%.2f prints \"-0.00\"); body:\n%s", kind, i+1, cell, body)
			}
		}
		// The spend column of every kind here is exactly cancelling.
		if kind != "savings" && feb[1] != "0.00" {
			t.Errorf("%s: Feb value = %q, want \"0.00\"", kind, feb[1])
		}
	}
}

// TestHandleKPIMonthDetail_CB9_CancellingMonthTotalIsPositiveZero: the
// month-detail JSON must not carry a -0 token for Total (a JSON consumer
// or a raw formatter would surface the sign), and the decoded float must
// have a clear sign bit for every spend-side kind.
func TestHandleKPIMonthDetail_CB9_CancellingMonthTotalIsPositiveZero(t *testing.T) {
	router, cleanup := setupTestEnv(t, cb9CancellingRows())
	defer cleanup()

	for _, kind := range []string{"expenses", "living", "healthcare"} {
		rec := doGet(t, router, "/dashboard/kpi/"+kind+"/month/2025-02?start=2025-01-01&end=2025-02-28")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", kind, rec.Code)
		}
		raw := rec.Body.String()
		if loc := negZeroJSON.FindStringIndex(raw); loc != nil {
			t.Errorf("%s: month-detail JSON carries a -0 token at %q", kind, raw[max(0, loc[0]-30):min(len(raw), loc[1]+10)])
		}
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			t.Fatalf("%s: decode: %v", kind, err)
		}
		tot, ok := result["Total"].(float64)
		if !ok {
			t.Fatalf("%s: harness error: Total missing in keys %v", kind, result)
		}
		if tot != 0 || math.Signbit(tot) {
			t.Errorf("%s: Total = %v (signbit=%v), want +0 for an exactly-cancelling month", kind, tot, math.Signbit(tot))
		}
	}
}

// TestHandleKPIDetail_CB9_CancellingMonthRowsHaveNoNegativeZero: the KPI
// detail modal's per-month rows (classifiedMonthlyTotals / expAmt) feed
// both the modal and the export; the rendered response must not contain
// "-0" anywhere for the cancelling month.
func TestHandleKPIDetail_CB9_CancellingMonthRowsHaveNoNegativeZero(t *testing.T) {
	router, cleanup := setupTestEnv(t, cb9CancellingRows())
	defer cleanup()

	for _, kind := range []string{"expenses", "living", "healthcare"} {
		rec := doGet(t, router, "/dashboard/kpi/"+kind+"?start=2025-01-01&end=2025-02-28")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", kind, rec.Code)
		}
		raw := rec.Body.String()
		if strings.Contains(raw, "$-0") || strings.Contains(raw, "-$0.00") || negZeroJSON.MatchString(raw) {
			t.Errorf("%s: KPI detail response renders a negative zero; body:\n%s", kind, raw)
		}
	}
}

// TestHandleChartData_CB9_CancellingMonthHasNoNegativeZero covers the two
// chart JSON endpoints that attempt 1 left untested (both lanes' FAIL
// ground): buildSpendingTrendChartData's monthlyTotals and
// buildBudgetVsActualChartData's hcAmt/livingMonth/spend walk. A -0 in
// any trace's y/customdata is a defect Plotly renders as "-0".
func TestHandleChartData_CB9_CancellingMonthHasNoNegativeZero(t *testing.T) {
	router, cleanup := setupCB9EnvWithTargets(t)
	defer cleanup()

	for _, chart := range []string{"spending-trend", "budget-vs-actual"} {
		rec := doGet(t, router, "/dashboard/charts/data/"+chart+"?start=2025-01-01&end=2025-02-28")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200; body: %s", chart, rec.Code, rec.Body.String())
		}
		raw := rec.Body.String()
		if loc := negZeroJSON.FindStringIndex(raw); loc != nil {
			t.Errorf("%s: chart JSON carries a -0 token near %q", chart, raw[max(0, loc[0]-40):min(len(raw), loc[1]+10)])
		}
		var out struct {
			Data []struct {
				Name string    `json:"name"`
				Y    []float64 `json:"y"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("%s: decode: %v", chart, err)
		}
		if len(out.Data) == 0 {
			t.Fatalf("%s: harness error: no traces", chart)
		}
		for _, tr := range out.Data {
			for i, y := range tr.Y {
				if math.Signbit(y) && y == 0 {
					t.Errorf("%s: trace %q y[%d] is IEEE -0", chart, tr.Name, i)
				}
			}
		}
		if chart == "budget-vs-actual" {
			seen := map[string]bool{}
			for _, tr := range out.Data {
				seen[tr.Name] = true
			}
			if !seen["Living"] || !seen["Healthcare"] {
				t.Fatalf("harness error: expected Living and Healthcare traces, got %v", seen)
			}
		}
	}
}
