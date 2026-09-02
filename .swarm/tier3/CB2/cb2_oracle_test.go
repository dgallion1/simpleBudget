package dashboard

// CB2 oracle probe. Copied by accept.sh into internal/handlers/dashboard/
// as zz_cb2_oracle_test.go for the oracle run, then removed. Exercises all
// three surface groups of the seven abs sites with refund-dominant months.

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/metrics"
)

func cb2Txn(desc string, amount float64, date time.Time, tt models.TransactionType, category string) models.Transaction {
	return models.Transaction{Description: desc, Amount: amount, Date: date, TransactionType: tt, Category: category}
}

// cb2FixtureSet: Jan normal (outflows net -1800), Feb refund-dominant
// (outflows net +500: groceries -300, cruise refund +800). Income 5000/mo.
func cb2FixtureSet() *models.TransactionSet {
	return models.NewTransactionSet([]models.Transaction{
		cb2Txn("Salary", 5000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		cb2Txn("Rent", -1500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
		cb2Txn("Groceries", -300, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		cb2Txn("Salary", 5000, time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		cb2Txn("Groceries", -300, time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		cb2Txn("Carnival Cruise Lines", 800, time.Date(2025, 2, 12, 0, 0, 0, 0, time.UTC), models.Outflow, "Travel"),
	})
}

func cb2Feq(a, b float64) bool { return math.Abs(a-b) < 0.01 }

// Surface 1: metrics trend series (sites metrics.go expAmt/hcAmt/livingMonth)
// plus the derived savingsTrend.
func TestCB2Oracle_TrendSeriesSigned(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	m := metrics.Calculate(cb2FixtureSet(), start, end, 0, 0, time.Time{}, false, nil)

	if len(m.ExpensesTrend) < 2 {
		t.Fatalf("harness error: ExpensesTrend len %d, want >=2", len(m.ExpensesTrend))
	}
	feb := len(m.ExpensesTrend) - 1
	jan := feb - 1
	// Harness validity: Jan identical under bug and fix.
	if !cb2Feq(m.ExpensesTrend[jan], 1800) {
		t.Errorf("harness error, not the defect: Jan expenses trend = %.2f, want 1800", m.ExpensesTrend[jan])
	}
	// Discriminators: signed contract for the refund-dominant month.
	if !cb2Feq(m.ExpensesTrend[feb], -500) {
		t.Errorf("Feb expenses trend = %.2f, want -500 (signed net credit); per-month abs gives 500", m.ExpensesTrend[feb])
	}
	if !cb2Feq(m.SavingsTrend[feb], 5500) {
		t.Errorf("Feb savings trend = %.2f, want 5500 (income 5000 PLUS 500 net refund); abs gives 4500", m.SavingsTrend[feb])
	}
	if !cb2Feq(m.LivingExpensesTrend[feb], -500) {
		t.Errorf("Feb living trend = %.2f, want -500 (signed)", m.LivingExpensesTrend[feb])
	}
}

// Surface 2: budget-vs-actual bar traces (sites handlers.go hcAmt/livingMonth).
func TestCB2Oracle_BarTracesSigned(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
	chart := buildBudgetVsActualChartData(cb2FixtureSet(), start, end, 1500, 0, time.Time{}, false, nil)

	// The chart is Plotly-shaped: find the bar trace named "Living" and
	// read its y series, via a JSON round-trip so the probe is agnostic to
	// the concrete Go types inside the map.
	b, err := json.Marshal(chart)
	if err != nil {
		t.Fatalf("harness error: marshal chart: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("harness error: unmarshal chart: %v", err)
	}
	var living []float64
	var findTraces func(v interface{})
	findTraces = func(v interface{}) {
		switch node := v.(type) {
		case map[string]interface{}:
			if node["name"] == "Living" {
				if ys, ok := node["y"].([]interface{}); ok {
					for _, y := range ys {
						if f, ok := y.(float64); ok {
							living = append(living, f)
						}
					}
					return
				}
			}
			for _, child := range node {
				findTraces(child)
			}
		case []interface{}:
			for _, child := range node {
				findTraces(child)
			}
		}
	}
	findTraces(decoded)
	if len(living) < 2 {
		t.Fatalf("harness error: Living trace not found or too short (%d); chart JSON: %.400s", len(living), string(b))
	}
	if !cb2Feq(living[len(living)-1], -500) {
		t.Errorf("Feb living bar = %.2f, want -500 (signed); per-month abs gives 500", living[len(living)-1])
	}
}

// Surface 3: handleKPIDetail monthly stats + handleKPIExport CSV (sites
// handlers.go:~524 and ~893), driven through the real HTTP surface with
// loader-inferred types ("Carnival Cruise Lines" +800 avoids every income
// keyword, so it classifies Outflow — the real-data refund shape).
func TestCB2Oracle_KPIStatsAndCSVSigned(t *testing.T) {
	rows := [][]string{
		{"2025-01-15", "Salary", "5000", "Payroll"},
		{"2025-01-05", "Rent", "-1500", "Housing"},
		{"2025-01-10", "Groceries", "-300", "Food"},
		{"2025-02-15", "Salary", "5000", "Payroll"},
		{"2025-02-10", "Groceries", "-300", "Food"},
		{"2025-02-12", "Carnival Cruise Lines", "800", "Travel"},
	}
	router, cleanup := setupTestEnv(t, rows)
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/expenses?start=2025-01-01&end=2025-02-28")
	if rec.Code != http.StatusOK {
		t.Fatalf("kpi/expenses status = %d, want 200", rec.Code)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sums, ok := result["Monthly"].([]interface{})
	if !ok || len(sums) == 0 {
		t.Fatalf("harness error: Monthly missing in response keys %v", keysOf(result))
	}
	var febStat map[string]interface{}
	for _, s := range sums {
		ms := s.(map[string]interface{})
		if strings.HasPrefix(ms["Month"].(string), "2025-02") || ms["Month"] == "Feb 2025" || strings.Contains(ms["Month"].(string), "Feb") {
			febStat = ms
		}
	}
	if febStat == nil {
		t.Fatalf("harness error: no Feb entry in MonthlySummaries: %v", sums)
	}
	if exp := febStat["Expenses"].(float64); !cb2Feq(exp, -500) {
		t.Errorf("KPI Feb Expenses = %.2f, want -500 (signed); abs gives 500", exp)
	}
	if sav := febStat["Savings"].(float64); !cb2Feq(sav, 5500) {
		t.Errorf("KPI Feb Savings = %.2f, want 5500 (income + refund); abs gives 4500", sav)
	}

	recCSV := doGet(t, router, "/dashboard/kpi/expenses/export?start=2025-01-01&end=2025-02-28")
	if recCSV.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200", recCSV.Code)
	}
	body := recCSV.Body.String()
	if !strings.Contains(body, "-500") {
		t.Errorf("CSV export lacks the signed Feb value -500; body:\n%s", body)
	}
}

// Surface 4 (amendment CB2-c): handleKPIMonthDetail's "Total Spent" tile
// (site ~738) and buildSpendingTrendChartData (site ~1409) — the last two
// month-bucket abs sites.
func TestCB2Oracle_MonthDetailAndSpendingTrendSigned(t *testing.T) {
	rows := [][]string{
		{"2025-01-15", "Salary", "5000", "Payroll"},
		{"2025-01-05", "Rent", "-1500", "Housing"},
		{"2025-01-10", "Groceries", "-300", "Food"},
		{"2025-02-15", "Salary", "5000", "Payroll"},
		{"2025-02-10", "Groceries", "-300", "Food"},
		{"2025-02-12", "Carnival Cruise Lines", "800", "Travel"},
	}
	router, cleanup := setupTestEnv(t, rows)
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/expenses/month/2025-02?start=2025-01-01&end=2025-02-28")
	if rec.Code != http.StatusOK {
		t.Fatalf("kpi month detail status = %d, want 200", rec.Code)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode month detail: %v", err)
	}
	tot, ok := result["Total"].(float64)
	if !ok {
		t.Fatalf("harness error: Total missing in month-detail keys %v", keysOf(result))
	}
	if !cb2Feq(tot, -500) {
		t.Errorf("month-detail Total Spent = %.2f, want -500 (signed, matching the KD-signed living/healthcare kinds in the same handler); abs gives 500", tot)
	}

	chart := buildSpendingTrendChartData(cb2FixtureSet())
	b, err := json.Marshal(chart)
	if err != nil {
		t.Fatalf("harness error: marshal spending trend: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("harness error: unmarshal spending trend: %v", err)
	}
	var series [][]float64
	var collectY func(v interface{})
	collectY = func(v interface{}) {
		switch node := v.(type) {
		case map[string]interface{}:
			if ys, ok := node["y"].([]interface{}); ok {
				var fs []float64
				for _, y := range ys {
					if f, ok := y.(float64); ok {
						fs = append(fs, f)
					}
				}
				if len(fs) > 0 {
					series = append(series, fs)
				}
			}
			for _, child := range node {
				collectY(child)
			}
		case []interface{}:
			for _, child := range node {
				collectY(child)
			}
		}
	}
	collectY(decoded)
	// The chart renders MoM %-change bars: one value for Feb. With signed
	// monthly totals, Feb = (-500-1800)/1800*100 = -127.7778 ("spending
	// fell past zero into net refund"); per-month abs gives -72.2222.
	// The existing prev>0 guard (refund month as BASE renders 0) stays.
	found := false
	for _, s := range series {
		if len(s) == 1 && (cb2Feq(s[0], -127.7778) || cb2Feq(s[0], -72.2222)) {
			found = true
			if !cb2Feq(s[0], -127.7778) {
				t.Errorf("spending trend Feb %%change = %.4f, want -127.7778 (signed curr); abs gives -72.2222", s[0])
			}
		}
	}
	if !found {
		t.Fatalf("harness error: no single-value %%change series found in spending trend; series: %v", series)
	}
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
