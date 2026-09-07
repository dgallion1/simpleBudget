package dashboard

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestDI5DashboardConsumers(t *testing.T) {
	router, cleanup := setupTestEnv(t, [][]string{
		{"2025-12-31", "Old purchase", "-999", "Home"},
		{"2026-01-01", "Home purchase", "-100", "Home"},
		{"2026-01-31", "Boutique Store Credit", "150", "Furniture"},
	})
	defer cleanup()
	old := dashboardNow
	dashboardNow = func() time.Time { return time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC) }
	defer func() { dashboardNow = old }()
	for _, path := range []string{"/dashboard", "/dashboard/kpis",
		"/dashboard/charts/data/major-expense", "/dashboard/charts/data/spending-trend", "/dashboard/charts/data/merchants", "/dashboard/charts/data/cumulative", "/dashboard/charts/data/budget-vs-actual",
		"/dashboard/major-expense?name=Unmatched", "/dashboard/kpi/expenses", "/dashboard/kpi/expenses/month/2026-01", "/dashboard/kpi/expenses/export"} {
		sep := "?"
		if path == "/dashboard/major-expense?name=Unmatched" {
			sep = "&"
		}
		a := doGet(t, router, path)
		b := doGet(t, router, path+sep+"start=2026-01-01&end=2026-01-31")
		if a.Code != 200 || b.Code != 200 {
			t.Fatalf("%s status=%d,%d", path, a.Code, b.Code)
		}
		if !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) {
			t.Errorf("%s default/explicit byte mismatch", path)
		}
		t.Logf("%s default vs explicit January: byte-identical=%v", path, bytes.Equal(a.Body.Bytes(), b.Body.Bytes()))
	}
	rec := doGet(t, router, "/dashboard/kpis")
	var got struct {
		Metrics struct {
			TotalExpenses float64 `json:"total_expenses"`
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Metrics.TotalExpenses != -50 {
		t.Fatalf("selected refund net=%v want -50; %s", got.Metrics.TotalExpenses, rec.Body.String())
	}
	csv := doGet(t, router, "/dashboard/kpi/expenses/export")
	if !bytes.Contains(csv.Body.Bytes(), []byte("2026-01,-50.00")) || bytes.Contains(csv.Body.Bytes(), []byte("999")) {
		t.Fatalf("CSV: %s", csv.Body.String())
	}
	t.Log("KPI signed expenses=-50; rendered CSV=2026-01,-50.00; prior-year sentinel excluded")
}
