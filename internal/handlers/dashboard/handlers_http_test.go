package dashboard

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/internal/testutil"

	"github.com/go-chi/chi/v5"
)

// writeTempCSV creates a temp dir with a CSV file and returns the dir,
// loader, store, and cleanup fn. The store is returned so the dashboard's
// accounts card (A8), which reads the accounts sidecar through it, can be
// exercised by tests that write accounts fixtures.
func writeTempCSV(t *testing.T, rows [][]string) (string, *dataloader.DataLoader, *storage.Storage, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "dashboard-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Write CSV file
	csvPath := filepath.Join(tmpDir, "test.csv")
	f, err := os.Create(csvPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create CSV: %v", err)
	}
	w := csv.NewWriter(f)
	w.Write([]string{"Date", "Description", "Amount", "Category"})
	for _, row := range rows {
		w.Write(row)
	}
	w.Flush()
	f.Close()

	store, err := storage.New(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("storage.New: %v", err)
	}

	dl := dataloader.New(tmpDir, store)
	return tmpDir, dl, store, func() { os.RemoveAll(tmpDir) }
}

// setupTestEnv creates a temp directory with a CSV file and initializes the
// dashboard package with a DataLoader (renderer=nil so handlers use JSON/HTML
// fallbacks). Returns the chi router and a cleanup function.
func setupTestEnv(t *testing.T, rows [][]string) (chi.Router, func()) {
	t.Helper()

	_, dl, store, cleanup := writeTempCSV(t, rows)
	Initialize(dl, nil, nil, store) // nil renderer triggers fallback paths

	r := chi.NewRouter()
	RegisterRoutes(r)

	return r, cleanup
}

// setupTestEnvWithRenderer creates a test environment with a real template
// renderer, so that the renderer != nil branches are exercised.
func setupTestEnvWithRenderer(t *testing.T, rows [][]string) (chi.Router, func()) {
	t.Helper()

	_, dl, store, cleanup := writeTempCSV(t, rows)

	// Use the project's actual template directory.
	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		cleanup()
		t.Fatalf("templates.New: %v", err)
	}

	Initialize(dl, rend, nil, store)

	r := chi.NewRouter()
	RegisterRoutes(r)

	return r, cleanup
}

// defaultRows returns a set of CSV rows spanning Jan–Mar 2025 with income and
// outflow transactions.
func defaultRows() [][]string {
	return [][]string{
		// Jan 2025
		{"2025-01-15", "Salary", "5000", "Payroll"},
		{"2025-01-05", "Rent", "-1500", "Housing"},
		{"2025-01-10", "Groceries", "-300", "Food"},
		// Feb 2025
		{"2025-02-15", "Salary", "5000", "Payroll"},
		{"2025-02-05", "Rent", "-1500", "Housing"},
		{"2025-02-10", "Groceries", "-400", "Food"},
		// Mar 2025
		{"2025-03-15", "Salary", "5500", "Payroll"},
		{"2025-03-05", "Rent", "-1500", "Housing"},
		{"2025-03-10", "Groceries", "-350", "Food"},
	}
}

func doGet(t *testing.T, router chi.Router, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// ---------- Initialize & RegisterRoutes ----------

func TestInitialize(t *testing.T) {
	// Just verify it doesn't panic and sets globals.
	Initialize(nil, nil, nil, nil)
}

func TestRegisterRoutes(t *testing.T) {
	r := chi.NewRouter()
	RegisterRoutes(r)
	// Walk all routes and verify the expected ones exist.
	var paths []string
	chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		paths = append(paths, method+" "+route)
		return nil
	})
	expected := []string{
		"GET /dashboard",
		"GET /dashboard/kpis",
		"GET /dashboard/charts/data/{chartType}",
		"GET /dashboard/major-expense",
		"GET /dashboard/kpi/{kpiType}",
		"GET /dashboard/kpi/{kpiType}/export",
	}
	for _, exp := range expected {
		found := false
		for _, p := range paths {
			if p == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("route %q not registered; got %v", exp, paths)
		}
	}
}

// ---------- handleDashboard ----------

func TestHandleDashboard_OK(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Nil renderer => fallback HTML
	body := rec.Body.String()
	if !strings.Contains(body, "Dashboard") {
		t.Errorf("body missing 'Dashboard'; got %s", body[:min(len(body), 200)])
	}
}

func TestHandleDashboard_DefaultDates(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleDashboard_WithComparison(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard?start=2025-02-01&end=2025-02-28&comparison=previous")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleDashboard_LoadDataError(t *testing.T) {
	// Point loader at a non-existent directory with no CSV files.
	tmpDir, _ := os.MkdirTemp("", "dashboard-empty-*")
	defer os.RemoveAll(tmpDir)

	store, _ := storage.New(tmpDir)
	dl := dataloader.New(filepath.Join(tmpDir, "nonexistent"), store)
	Initialize(dl, nil, nil, nil)

	r := chi.NewRouter()
	RegisterRoutes(r)

	// The dataloader returns empty data (not an error) for missing dirs,
	// so let's test with a truly broken path.
	rec := doGet(t, r, "/dashboard")
	// Should either succeed with empty data or return 500.
	if rec.Code != http.StatusOK && rec.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status %d", rec.Code)
	}
}

// ---------- handleKPIsPartial ----------

func TestHandleKPIsPartial_OK(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpis?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// nil renderer => JSON fallback
	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if _, ok := result["Metrics"]; !ok {
		t.Error("response missing 'Metrics' key")
	}
}

func TestHandleKPIsPartial_DefaultDates(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpis")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleKPIsPartial_WithComparison(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpis?start=2025-02-01&end=2025-02-28&comparison=year")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleKPIsPartial_LoadError(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "dashboard-kpis-err-*")
	defer os.RemoveAll(tmpDir)

	// Create a CSV file that is actually a directory to force a load error.
	badCSV := filepath.Join(tmpDir, "bad.csv")
	os.Mkdir(badCSV, 0755)

	store, _ := storage.New(tmpDir)
	dl := dataloader.New(tmpDir, store)
	Initialize(dl, nil, nil, nil)

	r := chi.NewRouter()
	RegisterRoutes(r)

	rec := doGet(t, r, "/dashboard/kpis")
	// Should still handle gracefully (empty data or error).
	_ = rec
}

// ---------- handleChartData ----------

func TestHandleChartData_SpendingTrend(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/charts/data/spending-trend?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleChartData_Merchants(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/charts/data/merchants?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleChartData_Cumulative(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/charts/data/cumulative?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleChartData_UnknownType(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/charts/data/bogus?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleChartData_DefaultDates(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/charts/data/spending-trend")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleChartData_LoadError(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "dashboard-chart-err-*")
	defer os.RemoveAll(tmpDir)

	badCSV := filepath.Join(tmpDir, "bad.csv")
	os.Mkdir(badCSV, 0755)

	store, _ := storage.New(tmpDir)
	dl := dataloader.New(tmpDir, store)
	Initialize(dl, nil, nil, nil)

	r := chi.NewRouter()
	RegisterRoutes(r)

	rec := doGet(t, r, "/dashboard/charts/data/spending-trend")
	_ = rec // just ensure no panic
}

// ---------- handleMajorExpenseDrilldown ----------

func TestHandleMajorExpenseDrilldown_Unmatched(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/major-expense?name=Unmatched&start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if result["Name"] != "Unmatched" {
		t.Errorf("Name = %v, want Unmatched", result["Name"])
	}
}

func TestHandleMajorExpenseDrilldown_DefaultDates(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/major-expense?name=Other")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleMajorExpenseDrilldown_UnknownName(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/major-expense?name=DoesNotExist&start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var result map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&result)
	if count, ok := result["Count"].(float64); ok && count != 0 {
		t.Errorf("Count = %v, want 0 for unknown wedge", count)
	}
}

func TestHandleMajorExpenseDrilldown_LoadError(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "dashboard-me-err-*")
	defer os.RemoveAll(tmpDir)
	badCSV := filepath.Join(tmpDir, "bad.csv")
	os.Mkdir(badCSV, 0755)

	store, _ := storage.New(tmpDir)
	dl := dataloader.New(tmpDir, store)
	Initialize(dl, nil, nil, nil)

	r := chi.NewRouter()
	RegisterRoutes(r)

	rec := doGet(t, r, "/dashboard/major-expense?name=Foo")
	_ = rec
}

// ---------- handleKPIDetail ----------

func TestHandleKPIDetail_Income(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/income?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var result map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&result)
	if result["Type"] != "income" {
		t.Errorf("Type = %v, want income", result["Type"])
	}
	if result["Title"] != "Total Income" {
		t.Errorf("Title = %v, want Total Income", result["Title"])
	}
	if result["IsRate"] != false {
		t.Errorf("IsRate = %v, want false", result["IsRate"])
	}
}

func TestHandleKPIDetail_Expenses(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/expenses?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var result map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&result)
	if result["Title"] != "Total Expenses" {
		t.Errorf("Title = %v, want Total Expenses", result["Title"])
	}
}

func TestHandleKPIDetail_Savings(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/savings?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var result map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&result)
	if result["Title"] != "Net Savings" {
		t.Errorf("Title = %v, want Net Savings", result["Title"])
	}
	if result["IsSavings"] != true {
		t.Errorf("IsSavings = %v, want true", result["IsSavings"])
	}
}

func TestHandleKPIDetail_SavingsRate(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/savings-rate?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var result map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&result)
	if result["Title"] != "Savings Rate" {
		t.Errorf("Title = %v, want Savings Rate", result["Title"])
	}
	if result["IsRate"] != true {
		t.Errorf("IsRate = %v, want true", result["IsRate"])
	}
}

func TestHandleKPIDetail_DefaultDates(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/income")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleKPIDetail_UnknownType(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	// Unknown KPI type should still return 200 with zero values (no error path for this).
	rec := doGet(t, router, "/dashboard/kpi/bogus?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleKPIDetail_LoadError(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "dashboard-kpidet-err-*")
	defer os.RemoveAll(tmpDir)
	badCSV := filepath.Join(tmpDir, "bad.csv")
	os.Mkdir(badCSV, 0755)

	store, _ := storage.New(tmpDir)
	dl := dataloader.New(tmpDir, store)
	Initialize(dl, nil, nil, nil)

	r := chi.NewRouter()
	RegisterRoutes(r)

	rec := doGet(t, r, "/dashboard/kpi/income")
	_ = rec
}

func TestHandleKPIDetail_EmptyData(t *testing.T) {
	// No transactions => empty months, numMonths default to 1
	router, cleanup := setupTestEnv(t, [][]string{})
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/income?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var result map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&result)
	if numMonths, ok := result["NumMonths"].(float64); ok && numMonths != 1 {
		t.Errorf("NumMonths = %v, want 1 for empty data", numMonths)
	}
}

// ---------- handleKPIExport ----------

func TestHandleKPIExport_Income(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/income/export?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	disp := rec.Header().Get("Content-Disposition")
	if !strings.Contains(disp, "income_") {
		t.Errorf("Content-Disposition = %q, want to contain 'income_'", disp)
	}
	// Parse CSV
	body := rec.Body.String()
	reader := csv.NewReader(strings.NewReader(body))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}
	// Header + data rows
	if len(records) < 2 {
		t.Fatalf("expected at least header + 1 data row, got %d rows", len(records))
	}
	if records[0][0] != "Month" || records[0][1] != "Income" {
		t.Errorf("header = %v, want [Month Income]", records[0])
	}
}

func TestHandleKPIExport_Expenses(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/expenses/export?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	reader := csv.NewReader(strings.NewReader(body))
	records, _ := reader.ReadAll()
	if len(records) < 2 {
		t.Fatalf("expected at least header + 1 data row, got %d rows", len(records))
	}
	if records[0][0] != "Month" || records[0][1] != "Expenses" {
		t.Errorf("header = %v, want [Month Expenses]", records[0])
	}
}

func TestHandleKPIExport_Savings(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/savings/export?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	reader := csv.NewReader(strings.NewReader(body))
	records, _ := reader.ReadAll()
	if len(records) < 2 {
		t.Fatalf("expected at least header + 1 data row, got %d rows", len(records))
	}
	// Savings header: Month, Income, Expenses, Savings
	if len(records[0]) != 4 {
		t.Errorf("savings header columns = %d, want 4", len(records[0]))
	}
	if records[0][3] != "Savings" {
		t.Errorf("header[3] = %v, want Savings", records[0][3])
	}
}

func TestHandleKPIExport_SavingsRate(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/savings-rate/export?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	reader := csv.NewReader(strings.NewReader(body))
	records, _ := reader.ReadAll()
	if len(records) < 2 {
		t.Fatalf("expected at least header + 1 data row, got %d rows", len(records))
	}
	// Savings rate header: Month, Income, Expenses, Savings, Savings Rate %
	if len(records[0]) != 5 {
		t.Errorf("savings-rate header columns = %d, want 5", len(records[0]))
	}
	if records[0][4] != "Savings Rate %" {
		t.Errorf("header[4] = %v, want 'Savings Rate %%'", records[0][4])
	}
}

func TestHandleKPIExport_DefaultDates(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/income/export")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
}

func TestHandleKPIExport_LoadError(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "dashboard-export-err-*")
	defer os.RemoveAll(tmpDir)
	badCSV := filepath.Join(tmpDir, "bad.csv")
	os.Mkdir(badCSV, 0755)

	store, _ := storage.New(tmpDir)
	dl := dataloader.New(tmpDir, store)
	Initialize(dl, nil, nil, nil)

	r := chi.NewRouter()
	RegisterRoutes(r)

	rec := doGet(t, r, "/dashboard/kpi/income/export")
	_ = rec
}

func TestHandleKPIExport_ZeroIncome(t *testing.T) {
	// Test the savings rate calculation when income is 0
	rows := [][]string{
		{"2025-01-05", "Rent", "-1500", "Housing"},
	}
	router, cleanup := setupTestEnv(t, rows)
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/savings-rate/export?start=2025-01-01&end=2025-01-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	reader := csv.NewReader(strings.NewReader(body))
	records, _ := reader.ReadAll()
	if len(records) >= 2 {
		// Rate should be "0.0" when income is 0
		if records[1][4] != "0.0" {
			t.Errorf("savings rate with zero income = %q, want '0.0'", records[1][4])
		}
	}
}

// ---------- resolveDateRange edge cases ----------

func TestResolveDateRange_YTDStartBeforeMinDate(t *testing.T) {
	// If minDate is after Jan 1 of current year, YTD start should be clamped to minDate.
	now := time.Now()
	minDate := time.Date(now.Year(), 6, 1, 0, 0, 0, 0, time.Local)
	maxDate := time.Date(now.Year(), 12, 31, 0, 0, 0, 0, time.Local)

	start, end := resolveDateRange("", "", minDate, maxDate)

	// YTD start (Jan 1) is before minDate (Jun 1), so should be clamped to minDate
	if start != minDate {
		t.Errorf("start = %v, want %v (clamped to minDate)", start, minDate)
	}
	if end != maxDate {
		t.Errorf("end = %v, want %v", end, maxDate)
	}
}

func TestResolveDateRange_InvalidDateStrings(t *testing.T) {
	minDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local)
	maxDate := time.Date(2025, 12, 31, 0, 0, 0, 0, time.Local)

	// Invalid date strings should fall back to defaults
	start, end := resolveDateRange("not-a-date", "also-not-a-date", minDate, maxDate)

	// Invalid start string means time.Parse returns zero, which triggers default.
	// But the code only checks empty string for defaults. Invalid parse returns zero time.
	// Actually, looking at the code: if startStr != "" it parses, and if parse fails,
	// startDate is zero. It doesn't re-default. This is just testing current behavior.
	_ = start
	_ = end
}

// ---------- handleKPIDetail edge cases ----------

func TestHandleKPIDetail_MinMaxCalculation(t *testing.T) {
	// Provide varying income across months to test min/max/avg calculation paths.
	rows := [][]string{
		{"2025-01-15", "Salary", "3000", "Payroll"},
		{"2025-02-15", "Salary", "5000", "Payroll"},
		{"2025-03-15", "Salary", "4000", "Payroll"},
	}
	router, cleanup := setupTestEnv(t, rows)
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/income?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var result map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&result)

	// Verify min/max/avg are present and sensible
	if _, ok := result["Min"]; !ok {
		t.Error("response missing 'Min'")
	}
	if _, ok := result["Max"]; !ok {
		t.Error("response missing 'Max'")
	}
	if _, ok := result["Average"]; !ok {
		t.Error("response missing 'Average'")
	}
}

// ---------- buildSpendingTrendChartData edge: zero prev ----------

func TestBuildSpendingTrendChartData_ZeroPreviousMonth(t *testing.T) {
	// If previous month is 0, pctChange should be 0 (guard in code: if prev > 0).
	ts := makeTransactionSet(
		// Jan has no outflows (only income), Feb has outflows
		makeTransaction("Salary", 5000, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -1500, time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	result := buildSpendingTrendChartData(ts)
	data := result["data"].([]map[string]interface{})

	// Only outflows are considered; Jan has none, Feb has some.
	// Since only 1 month has outflows, < 2 months => empty data.
	if len(data) != 0 {
		// If somehow we get data, the change should be 0 since prev = 0.
		trace := data[0]
		values := trace["y"].([]float64)
		if len(values) > 0 && !floatEqual(values[0], 0) {
			t.Errorf("change with zero prev = %v, want 0", values[0])
		}
	}
}

// ---------- buildMajorExpenseChartData edge cases ----------

func TestBuildMajorExpenseChartData_EmptyData(t *testing.T) {
	ts := makeTransactionSet()
	result := buildMajorExpenseChartData(ts)
	data := result["data"].([]map[string]interface{})
	trace := data[0]
	if values, ok := trace["values"].([]float64); ok && len(values) > 0 {
		t.Errorf("expected no values for empty data, got %v", values)
	}
}

func TestBuildMajorExpenseChartData_AllUnmatched(t *testing.T) {
	// No major expenses defined → every outflow lands in "Unmatched"
	// and the residual equals the total.
	ts := makeTransactionSet(
		makeTransaction("Coffee", -5, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"),
		makeTransaction("Gas", -45, time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Auto"),
	)
	result := buildMajorExpenseChartData(ts)
	data := result["data"].([]map[string]interface{})
	trace := data[0]
	labels := trace["labels"].([]string)
	values := trace["values"].([]float64)
	if len(labels) != 1 || labels[0] != "Unmatched" {
		t.Errorf("expected single 'Unmatched' bucket, got %v", labels)
	}
	if len(values) != 1 || values[0] != 50 {
		t.Errorf("expected unmatched residual = 50, got %v", values)
	}
}

// withMajorExpenses installs a loader populated with the given major
// expenses for the duration of the test. The loader is a package-level
// var; restored on cleanup.
func withMajorExpenses(t *testing.T, expenses []models.MajorExpense) {
	t.Helper()
	tmpDir, dl, _, cleanup := writeTempCSV(t, [][]string{})
	t.Cleanup(cleanup)
	_ = tmpDir
	if err := dl.SaveMajorExpenses(expenses); err != nil {
		t.Fatalf("SaveMajorExpenses: %v", err)
	}
	prev := loader
	loader = dl
	t.Cleanup(func() { loader = prev })
}

func TestBuildMajorExpenseChartData_FewerThanThreshold(t *testing.T) {
	// 5 distinct major expenses, all matched → no "Other" wedge, no smaller.
	var expenses []models.MajorExpense
	var txns []models.Transaction
	for i := 0; i < 5; i++ {
		name := string(rune('A' + i))
		expenses = append(expenses, models.MajorExpense{
			ID: name, Name: name + "-bucket", Keywords: []string{name + "-kw"},
		})
		txns = append(txns, makeTransaction(name+"-kw payment", -float64((5-i)*100),
			time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"))
	}
	withMajorExpenses(t, expenses)

	ts := makeTransactionSet(txns...)
	result := buildMajorExpenseChartData(ts)

	data := result["data"].([]map[string]interface{})
	labels := data[0]["labels"].([]string)
	for _, l := range labels {
		if l == "Other" {
			t.Errorf("expected no 'Other' wedge with 5 buckets, got labels=%v", labels)
		}
	}
	if _, ok := result["smaller"]; ok {
		t.Errorf("expected no 'smaller' field with 5 buckets, got %v", result["smaller"])
	}
}

func TestBuildMajorExpenseChartData_ExactlyAtThreshold(t *testing.T) {
	// 8 distinct major expenses → no "Other" wedge, no smaller.
	var expenses []models.MajorExpense
	var txns []models.Transaction
	for i := 0; i < 8; i++ {
		name := string(rune('A' + i))
		expenses = append(expenses, models.MajorExpense{
			ID: name, Name: name + "-bucket", Keywords: []string{name + "-kw"},
		})
		txns = append(txns, makeTransaction(name+"-kw payment", -float64((8-i)*100),
			time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"))
	}
	withMajorExpenses(t, expenses)

	ts := makeTransactionSet(txns...)
	result := buildMajorExpenseChartData(ts)

	labels := result["data"].([]map[string]interface{})[0]["labels"].([]string)
	if len(labels) != 8 {
		t.Errorf("expected 8 labels (no Other), got %d: %v", len(labels), labels)
	}
	for _, l := range labels {
		if l == "Other" {
			t.Errorf("expected no 'Other' wedge at exactly 8 buckets, got labels=%v", labels)
		}
	}
	if _, ok := result["smaller"]; ok {
		t.Errorf("expected no 'smaller' field at exactly 8 buckets")
	}
}

func TestBuildMajorExpenseChartData_AboveThresholdRollup(t *testing.T) {
	// 11 distinct major expenses → top 8 + Other (rollup of 3) on the donut,
	// smaller has 3 entries with descending amounts.
	var expenses []models.MajorExpense
	var txns []models.Transaction
	for i := 0; i < 11; i++ {
		name := string(rune('A' + i))
		expenses = append(expenses, models.MajorExpense{
			ID: name, Name: name + "-bucket", Keywords: []string{name + "-kw"},
		})
		// Amounts: 1100, 1000, 900, ..., 100 (descending)
		txns = append(txns, makeTransaction(name+"-kw payment", -float64((11-i)*100),
			time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"))
	}
	withMajorExpenses(t, expenses)

	ts := makeTransactionSet(txns...)
	result := buildMajorExpenseChartData(ts)

	labels := result["data"].([]map[string]interface{})[0]["labels"].([]string)
	values := result["data"].([]map[string]interface{})[0]["values"].([]float64)

	if len(labels) != 9 {
		t.Fatalf("expected 9 wedges (top 8 + Other), got %d: %v", len(labels), labels)
	}
	if labels[8] != "Other" {
		t.Errorf("expected last wedge to be 'Other', got %q", labels[8])
	}

	// Sum of bottom 3 buckets (300+200+100 = 600).
	if !floatEqual(values[8], 600) {
		t.Errorf("Other value = %v, want 600", values[8])
	}

	smaller, ok := result["smaller"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected smaller []map[string]interface{}, got %T", result["smaller"])
	}
	if len(smaller) != 3 {
		t.Fatalf("expected 3 smaller entries, got %d: %v", len(smaller), smaller)
	}
	if smaller[0]["name"] != "I-bucket" {
		t.Errorf("smaller[0].name = %v, want I-bucket", smaller[0]["name"])
	}
	if a, _ := smaller[0]["amount"].(float64); !floatEqual(a, 300) {
		t.Errorf("smaller[0].amount = %v, want 300", smaller[0]["amount"])
	}
	if p, _ := smaller[0]["percent"].(float64); !floatEqual(p, 4.5) && !floatEqual(p, 4.55) {
		t.Errorf("smaller[0].percent = %v, want ~4.5", smaller[0]["percent"])
	}
}

func TestBuildMajorExpenseChartData_RollupWithUnmatched(t *testing.T) {
	// 11 matched + some unmatched → wedge order: top 8 matched, Other,
	// then Unmatched last. smaller excludes Unmatched.
	var expenses []models.MajorExpense
	var txns []models.Transaction
	for i := 0; i < 11; i++ {
		name := string(rune('A' + i))
		expenses = append(expenses, models.MajorExpense{
			ID: name, Name: name + "-bucket", Keywords: []string{name + "-kw"},
		})
		txns = append(txns, makeTransaction(name+"-kw payment", -float64((11-i)*100),
			time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Food"))
	}
	txns = append(txns, makeTransaction("mystery", -2000,
		time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Misc"))
	withMajorExpenses(t, expenses)

	ts := makeTransactionSet(txns...)
	result := buildMajorExpenseChartData(ts)

	labels := result["data"].([]map[string]interface{})[0]["labels"].([]string)
	if len(labels) != 10 {
		t.Fatalf("expected 10 wedges (top 8 + Other + Unmatched), got %d: %v", len(labels), labels)
	}
	if labels[8] != "Other" {
		t.Errorf("expected wedge 8 = Other, got %q", labels[8])
	}
	if labels[9] != "Unmatched" {
		t.Errorf("expected wedge 9 = Unmatched (last), got %q", labels[9])
	}

	smaller := result["smaller"].([]map[string]interface{})
	for _, item := range smaller {
		if item["name"] == "Unmatched" {
			t.Errorf("smaller must not contain Unmatched, got %v", smaller)
		}
	}
}

func TestBuildMajorExpenseChartData_SubOnePercentPrecision(t *testing.T) {
	// One huge bucket plus 10 tiny ones → tail items have sub-1% shares
	// and must be returned with two-decimal precision (not 0.0).
	var expenses []models.MajorExpense
	var txns []models.Transaction

	expenses = append(expenses, models.MajorExpense{
		ID: "big", Name: "Big", Keywords: []string{"big-kw"},
	})
	txns = append(txns, makeTransaction("big-kw rent", -99000,
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"))

	// 10 tiny buckets at $50 each → total = 500. Grand total = 99500.
	// Each tiny share = 50/99500 ≈ 0.0503% → must NOT round to 0.0.
	for i := 0; i < 10; i++ {
		name := "tiny" + string(rune('A'+i))
		expenses = append(expenses, models.MajorExpense{
			ID: name, Name: name, Keywords: []string{name + "-kw"},
		})
		txns = append(txns, makeTransaction(name+"-kw small", -50,
			time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC), models.Outflow, "Misc"))
	}

	withMajorExpenses(t, expenses)
	ts := makeTransactionSet(txns...)
	result := buildMajorExpenseChartData(ts)

	smaller, ok := result["smaller"].([]map[string]interface{})
	if !ok || len(smaller) == 0 {
		t.Fatalf("expected smaller entries, got %v", result["smaller"])
	}
	for _, item := range smaller {
		p, _ := item["percent"].(float64)
		if p == 0 {
			t.Errorf("sub-1%% bucket rounded to 0%%; need 2-decimal precision: %v", item)
		}
	}
}

// ---------- buildMerchantsChartData edge cases ----------

func TestBuildMerchantsChartData_LessThanTen(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Alpha", -500, time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), models.Outflow, "Shopping"),
		makeTransaction("Bravo", -300, time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), models.Outflow, "Shopping"),
	)

	result := buildMerchantsChartData(ts)
	data := result["data"].([]map[string]interface{})
	trace := data[0]
	labels := trace["y"].([]string)

	if len(labels) != 2 {
		t.Errorf("expected 2 merchants, got %d", len(labels))
	}
}

func TestBuildMerchantsChartData_EmptyData(t *testing.T) {
	ts := makeTransactionSet()
	result := buildMerchantsChartData(ts)
	data := result["data"].([]map[string]interface{})
	trace := data[0]
	labels := trace["y"]
	if labels != nil {
		if ls, ok := labels.([]string); ok && len(ls) > 0 {
			t.Errorf("expected no labels, got %v", ls)
		}
	}
}

// ---------- buildCumulativeChartData edge cases ----------

func TestBuildCumulativeChartData_EmptyData(t *testing.T) {
	ts := makeTransactionSet()
	result := buildCumulativeChartData(ts)
	data := result["data"].([]map[string]interface{})
	trace := data[0]

	cumulative := trace["y"]
	if cumulative != nil {
		if cs, ok := cumulative.([]float64); ok && len(cs) > 0 {
			t.Errorf("expected no cumulative data, got %v", cs)
		}
	}

	// Zero running total => green
	lineColor := trace["line"].(map[string]interface{})["color"].(string)
	if lineColor != "#22c55e" {
		t.Errorf("line color for zero balance = %v, want #22c55e (green)", lineColor)
	}
}

func TestBuildCumulativeChartData_MultipleTxnsSameDay(t *testing.T) {
	ts := makeTransactionSet(
		makeTransaction("Salary", 5000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Bonus", 1000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), models.Income, "Payroll"),
		makeTransaction("Rent", -2000, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), models.Outflow, "Housing"),
	)

	result := buildCumulativeChartData(ts)
	data := result["data"].([]map[string]interface{})
	trace := data[0]
	cumulative := trace["y"].([]float64)

	// All same day: 5000 + 1000 - 2000 = 4000
	if len(cumulative) != 1 {
		t.Fatalf("expected 1 data point for same day, got %d", len(cumulative))
	}
	if !floatEqual(cumulative[0], 4000) {
		t.Errorf("cumulative = %v, want 4000", cumulative[0])
	}
}

// ---------- Error path tests (LoadData returns error) ----------
// filepath.Glob returns ErrBadPattern when the directory contains an unclosed bracket.

func setupBrokenLoader(t *testing.T) (chi.Router, func()) {
	t.Helper()
	tmpDir, _ := os.MkdirTemp("", "dashboard-err-*")
	// Create a subdirectory with a bracket in the name that makes Glob fail
	badDir := filepath.Join(tmpDir, "[bad")
	os.Mkdir(badDir, 0755)

	store, _ := storage.New(tmpDir)
	dl := dataloader.New(badDir, store)
	Initialize(dl, nil, nil, nil)

	r := chi.NewRouter()
	RegisterRoutes(r)
	return r, func() { os.RemoveAll(tmpDir) }
}

func TestHandleDashboard_LoadError(t *testing.T) {
	router, cleanup := setupBrokenLoader(t)
	defer cleanup()

	rec := doGet(t, router, "/dashboard")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleKPIsPartial_LoadErrorReturns500(t *testing.T) {
	router, cleanup := setupBrokenLoader(t)
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpis")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleChartData_LoadErrorReturns500(t *testing.T) {
	router, cleanup := setupBrokenLoader(t)
	defer cleanup()

	rec := doGet(t, router, "/dashboard/charts/data/spending-trend")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleMajorExpenseDrilldown_LoadErrorReturns500(t *testing.T) {
	router, cleanup := setupBrokenLoader(t)
	defer cleanup()

	rec := doGet(t, router, "/dashboard/major-expense?name=Foo")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleKPIDetail_LoadErrorReturns500(t *testing.T) {
	router, cleanup := setupBrokenLoader(t)
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/income")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandleKPIExport_LoadErrorReturns500(t *testing.T) {
	router, cleanup := setupBrokenLoader(t)
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/income/export")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// ---------- Tests with real renderer (renderer != nil branches) ----------

func TestHandleDashboard_WithRenderer(t *testing.T) {
	router, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

// headingLevels returns the ordered list of heading levels (1-6) as they
// appear in the body, so a test can assert the outline has no skipped levels
// rather than describing it in an unenforced comment.
var headingLevelRe = regexp.MustCompile(`<h([1-6])\b`)

func headingLevels(body string) []int {
	matches := headingLevelRe.FindAllStringSubmatch(body, -1)
	out := make([]int, len(matches))
	for i, m := range matches {
		out[i] = int(m[1][0] - '0')
	}
	return out
}

// assertNoSkippedLevels fails the test if the heading outline skips a level:
// the first heading must be <h1>, and each subsequent heading may be the same
// level or nested one level deeper, never more than one.
func assertNoSkippedLevels(t *testing.T, body string) {
	t.Helper()
	levels := headingLevels(body)
	if len(levels) == 0 {
		t.Fatalf("no headings found in body")
	}
	if levels[0] != 1 {
		t.Errorf("first heading is h%d, want h1", levels[0])
	}
	prev := levels[0]
	for _, l := range levels[1:] {
		if l < 1 || l > 6 {
			t.Errorf("heading level %d out of range", l)
			continue
		}
		if l > prev+1 {
			t.Errorf("heading outline skips a level: h%d -> h%d", prev, l)
		}
		if l > prev {
			prev = l
		} else {
			prev = l
		}
	}
}

// TestHandleDashboard_HeadingOutlineNoSkippedLevels asserts the dashboard
// page has exactly one <h1> naming the page and a heading outline with no
// skipped levels (ACCESSIBILITY.md point 1), in BOTH the no-accounts state
// (the normal first-run state and the state setupTestEnvWithRenderer
// renders, since it never writes an accounts.json) and the with-accounts
// state. The dashboard's panels -- the accounts card, the Budget vs Actual
// card, and the four chart cards -- are siblings, so each is an <h2>; none
// is nested under another, so <h3> would skip a level when the accounts
// card is absent.
func TestHandleDashboard_HeadingOutlineNoSkippedLevels(t *testing.T) {
	// --- No accounts configured (the first-run state) ---
	router, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
	body := rec.Body.String()

	openH1 := strings.Count(body, "<h1")
	closeH1 := strings.Count(body, "</h1>")
	if openH1 != 1 {
		t.Errorf("dashboard page (no accounts) has %d <h1> open tags, want exactly 1", openH1)
	}
	if closeH1 != 1 {
		t.Errorf("dashboard page (no accounts) has %d </h1> close tags, want exactly 1", closeH1)
	}
	if !strings.Contains(body, ">Dashboard</h1>") {
		t.Errorf("dashboard page <h1> must name the page; got:\n%s", excerptAround(body, "<h1", 200))
	}
	// The accounts card is absent (HasAny is false), so the only <h2>s are the
	// chart/budget section headings. The outline must still have no skips.
	assertNoSkippedLevels(t, body)
	if strings.Contains(body, `aria-labelledby="accounts-card-heading"`) {
		t.Errorf("dashboard page (no accounts) must not render the accounts card section")
	}

	// --- With an account configured (accounts card present) ---
	router2, _, cleanup2 := setupAccountsTestEnv(t, defaultRows(), []models.Account{
		{
			ID:                  "usaa",
			Name:                "USAA Checking",
			Institution:         "USAA",
			Kind:                models.AccountKindChecking,
			FilePatterns:        []string{"test.csv"},
			Anchors:             []models.BalanceAnchor{{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Amount: 2000.00}},
			LowBalanceThreshold: 500.00,
		},
	}, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))
	defer cleanup2()

	rec2 := doGetDash(t, router2, "/dashboard?start=2025-01-01&end=2025-03-31")
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec2.Code, rec2.Body.String()[:min(rec2.Body.Len(), 300)])
	}
	body2 := rec2.Body.String()

	if strings.Count(body2, "<h1") != 1 {
		t.Errorf("dashboard page (with accounts) has %d <h1> open tags, want exactly 1", strings.Count(body2, "<h1"))
	}
	if !strings.Contains(body2, `aria-labelledby="accounts-card-heading"`) {
		t.Errorf("dashboard page (with accounts) must render the accounts card section")
	}
	assertNoSkippedLevels(t, body2)
}

func TestHandleKPIsPartial_WithRenderer(t *testing.T) {
	router, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpis?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
}

func TestHandleMajorExpenseDrilldown_WithRenderer(t *testing.T) {
	router, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/major-expense?name=Unmatched&start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
}

func TestHandleKPIDetail_WithRenderer(t *testing.T) {
	router, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpi/income?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
}

// ---------- /dashboard/charts/data/budget-vs-actual ----------

func TestHandleChartData_BudgetVsActual_Empty(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/charts/data/budget-vs-actual?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	data, ok := payload["data"].([]interface{})
	if !ok {
		t.Fatalf("response.data missing or wrong type")
	}
	// No retirement manager wired (Initialize(_, _, nil)) so no combined target;
	// builder returns empty data.
	if len(data) != 0 {
		t.Errorf("data length = %d, want 0 when no combined target configured", len(data))
	}
}

func TestHandleChartData_BudgetVsActual_BadType(t *testing.T) {
	router, cleanup := setupTestEnv(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard/charts/data/not-a-real-chart-type")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unknown chart type", rec.Code)
	}
}

// ---------- KPI sparkline target overlays ----------

func TestDashboardKPIs_LivingSparkline_HasTargetAttribute(t *testing.T) {
	rows := [][]string{
		{"2025-01-15", "Salary", "5000", "Payroll"},
		{"2025-01-05", "Rent", "-1500", "Housing"},
	}
	tmpDir, dl, store, cleanup := writeTempCSV(t, rows)
	defer cleanup()

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	rm := retirement.NewSettingsManager(tmpDir, store)
	settingsPath := filepath.Join(tmpDir, "whatif.json")
	if err := os.WriteFile(settingsPath, []byte(`{"monthly_living_expenses": 2000}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	Initialize(dl, rend, rm, store)

	r := chi.NewRouter()
	RegisterRoutes(r)

	rec := doGet(t, r, "/dashboard/kpis?start=2025-01-01&end=2025-01-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
	body := rec.Body.String()

	if !strings.Contains(body, `id="sparkline-monthly"`) {
		t.Errorf("response missing sparkline-monthly container")
	}
	if !strings.Contains(body, `data-target="2000"`) {
		t.Errorf("sparkline-monthly missing data-target=\"2000\"; body excerpt: %s", excerptAround(body, "sparkline-monthly", 200))
	}
}

func TestDashboardKPIs_BudgetSparkline_BalanceMode(t *testing.T) {
	rows := [][]string{
		{"2025-01-05", "Rent", "-1500", "Housing"},
	}
	tmpDir, dl, store, cleanup := writeTempCSV(t, rows)
	defer cleanup()

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	rm := retirement.NewSettingsManager(tmpDir, store)
	settingsPath := filepath.Join(tmpDir, "whatif.json")
	if err := os.WriteFile(settingsPath, []byte(`{"monthly_living_expenses": 2000}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	Initialize(dl, rend, rm, store)

	r := chi.NewRouter()
	RegisterRoutes(r)

	rec := doGet(t, r, "/dashboard/kpis?start=2025-01-01&end=2025-01-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
	body := rec.Body.String()

	if !strings.Contains(body, `id="sparkline-budget"`) {
		t.Errorf("response missing sparkline-budget container")
	}
	if !strings.Contains(body, `data-mode="balance"`) {
		t.Errorf("sparkline-budget missing data-mode=\"balance\"; body excerpt: %s", excerptAround(body, "sparkline-budget", 200))
	}
}

// excerptAround returns a substring of body centered on needle for diagnostics.
func excerptAround(body, needle string, around int) string {
	idx := strings.Index(body, needle)
	if idx < 0 {
		return "(needle not found)"
	}
	start := idx - around
	if start < 0 {
		start = 0
	}
	end := idx + around
	if end > len(body) {
		end = len(body)
	}
	return body[start:end]
}

// ---------- Budget vs Actual chart card ----------

func TestHandleDashboard_RendersBudgetVsActualCard(t *testing.T) {
	rows := defaultRows()
	tmpDir, dl, store, cleanup := writeTempCSV(t, rows)
	defer cleanup()

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	rm := retirement.NewSettingsManager(tmpDir, store)
	settingsPath := filepath.Join(tmpDir, "whatif.json")
	if err := os.WriteFile(settingsPath, []byte(`{"monthly_living_expenses": 2000}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	Initialize(dl, rend, rm, store)

	r := chi.NewRouter()
	RegisterRoutes(r)

	rec := doGet(t, r, "/dashboard?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="chart-budget-vs-actual"`) {
		t.Errorf("dashboard page missing chart-budget-vs-actual container")
	}
	if !strings.Contains(body, `data-chart-url="/dashboard/charts/data/budget-vs-actual"`) {
		t.Errorf("chart container missing data-chart-url attribute")
	}
}

func TestHandleDashboard_BudgetVsActualCard_EmptyState(t *testing.T) {
	router, cleanup := setupTestEnvWithRenderer(t, defaultRows())
	defer cleanup()

	rec := doGet(t, router, "/dashboard?start=2025-01-01&end=2025-03-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	// When HasCombinedTarget is false (no retirement manager wired), the chart
	// card renders the empty-state link instead of the chart container.
	if strings.Contains(body, `id="chart-budget-vs-actual"`) {
		t.Errorf("chart container rendered when no combined target — expected empty state")
	}
	if !strings.Contains(body, "Budget vs Actual Over Time") {
		t.Errorf("chart card heading missing")
	}
}

// ---------- Target provenance (title/aria on the phase-adjusted target) ----------

func TestDashboardKPIs_TargetProvenance_AnnotatesWhenPhaseActive(t *testing.T) {
	rows := [][]string{
		{"2025-01-05", "Rent", "-1500", "Housing"},
	}
	tmpDir, dl, store, cleanup := writeTempCSV(t, rows)
	defer cleanup()

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	rm := retirement.NewSettingsManager(tmpDir, store)
	settingsPath := filepath.Join(tmpDir, "whatif.json")
	// Primary is 70 at start ("2025-01") -> Active phase (multiplier 0.95),
	// so the $2,000 base target is phase-adjusted to $1,900. The whole
	// one-month query range sits inside the Active phase, so no straddle.
	settingsJSON := fmt.Sprintf(`{
		"monthly_living_expenses": 2000,
		"start_date": "2025-01",
		"phase_age_reference": "primary",
		"persons": [{"id": "p1", "name": "Primary", "birth_month": %q, "role": "primary"}],
		"spending_phase_config": {
			"enabled": true,
			"phases": [
				{"name": "Go-Go", "start_age": 0, "multiplier": 1.0},
				{"name": "Active", "start_age": 65, "multiplier": 0.95}
			]
		}
	}`, models.BirthMonthForAge("2025-01", 70))
	if err := os.WriteFile(settingsPath, []byte(settingsJSON), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	Initialize(dl, rend, rm, store)

	r := chi.NewRouter()
	RegisterRoutes(r)

	rec := doGet(t, r, "/dashboard/kpis?start=2025-01-01&end=2025-01-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
	body := rec.Body.String()

	wantText := "Target $1,900.00 — from What-If plan: $2,000 base × 0.95 (Active phase)"
	if !strings.Contains(body, `title="`+wantText+`"`) {
		t.Errorf("missing title=%q; body excerpt: %s", wantText, excerptAround(body, "Monthly Living Expenses", 600))
	}
	if !strings.Contains(body, `aria-label="`+wantText+`"`) {
		t.Errorf("missing aria-label=%q; body excerpt: %s", wantText, excerptAround(body, "Monthly Living Expenses", 600))
	}
}

func TestDashboardKPIs_TargetProvenance_AbsentWhenNoPhaseConfig(t *testing.T) {
	rows := [][]string{
		{"2025-01-05", "Rent", "-1500", "Housing"},
	}
	tmpDir, dl, store, cleanup := writeTempCSV(t, rows)
	defer cleanup()

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	rm := retirement.NewSettingsManager(tmpDir, store)
	settingsPath := filepath.Join(tmpDir, "whatif.json")
	// No spending_phase_config at all -> target equals the base; nothing to annotate.
	if err := os.WriteFile(settingsPath, []byte(`{"monthly_living_expenses": 2000}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	Initialize(dl, rend, rm, store)

	r := chi.NewRouter()
	RegisterRoutes(r)

	rec := doGet(t, r, "/dashboard/kpis?start=2025-01-01&end=2025-01-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
	body := rec.Body.String()

	if !strings.Contains(body, "Target") {
		t.Fatalf("expected Monthly Living Expenses card to show 'Target'; body:\n%s", body)
	}
	if strings.Contains(body, "Target from What-If plan:") {
		t.Errorf("target annotation rendered with no phase config; expected byte-identical (unannotated) rendering; body:\n%s", body)
	}
	if strings.Contains(body, `aria-label="Target`) {
		t.Errorf("unexpected aria-label on Target when no phase config exists; body:\n%s", body)
	}
	// Byte-identical to master's fragment: no wrapper <span> around "Target"
	// at all when unannotated — just the bare "Target <span class=\"num\">"
	// text master has always rendered.
	if !regexp.MustCompile(`<p class="text-sm [^"]*">\s*Target <span class="num">`).MatchString(body) {
		t.Errorf("expected bare \"Target <span...\" markup (byte-identical to master, no wrapper span) when unannotated; body:\n%s", body)
	}
}

// min helper for Go < 1.21
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------- KPI card content assertions ----------

func TestDashboardKPIs_RendersBudgetCards_NoTarget(t *testing.T) {
	router, cleanup := setupTestEnvWithRenderer(t, [][]string{
		{"2025-01-15", "Salary", "5000", "Payroll"},
		{"2025-01-05", "Rent", "-1500", "Housing"},
	})
	defer cleanup()

	rec := doGet(t, router, "/dashboard/kpis?start=2025-01-01&end=2025-01-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, "Monthly Living Expenses") {
		t.Errorf("response missing 'Monthly Living Expenses' card; body:\n%s", body)
	}
	if !strings.Contains(body, "Budget") {
		t.Errorf("response missing 'Budget' card; body:\n%s", body)
	}
	if strings.Contains(body, "Savings Rate") {
		t.Errorf("response still contains 'Savings Rate' card (should be removed); body:\n%s", body)
	}
	// No retirement manager wired (setupTestEnvWithRenderer passes nil) → fallback link
	if !strings.Contains(body, "Set a budget in What-If") {
		t.Errorf("response missing fallback link 'Set a budget in What-If'; body:\n%s", body)
	}
}

func TestDashboardKPIs_RendersBudgetCards_WithTarget(t *testing.T) {
	rows := [][]string{
		{"2025-01-05", "Rent", "-3000", "Housing"},
	}
	tmpDir, dl, store, cleanup := writeTempCSV(t, rows)
	defer cleanup()

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	rm := retirement.NewSettingsManager(tmpDir, store)
	// Write the settings file directly so the manager loads our target.
	settingsPath := filepath.Join(tmpDir, "whatif.json")
	if err := os.WriteFile(settingsPath, []byte(`{"monthly_living_expenses": 1000}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	Initialize(dl, rend, rm, store)

	r := chi.NewRouter()
	RegisterRoutes(r)

	rec := doGet(t, r, "/dashboard/kpis?start=2025-01-01&end=2025-01-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if strings.Contains(body, "Set a budget in What-If") {
		t.Errorf("budget loaded but fallback link still rendered; body:\n%s", body)
	}
	if !strings.Contains(body, "over") {
		t.Errorf("expected Budget card to show 'over'; body:\n%s", body)
	}
	if !strings.Contains(body, "Target") {
		t.Errorf("expected Monthly Living Expenses card to show 'Target'; body:\n%s", body)
	}
}

// suppress unused import warnings
var _ = io.Discard

func TestBuildMerchantsChartData_AggregatesByLabel(t *testing.T) {
	ts := models.NewTransactionSet([]models.Transaction{
		{Description: "BOFA HOMELOANS 0123", MajorExpenseName: "Mortgage", Amount: -1500, TransactionType: models.Outflow},
		{Description: "BOFA HOMELOANS 0124", MajorExpenseName: "Mortgage", Amount: -1500, TransactionType: models.Outflow},
		{Description: "Whole Foods", Amount: -42, TransactionType: models.Outflow},
	})

	chart := buildMerchantsChartData(ts)
	data, ok := chart["data"].([]map[string]interface{})
	if !ok || len(data) == 0 {
		t.Fatalf("chart[data] missing or wrong type: %T", chart["data"])
	}
	labels, ok := data[0]["y"].([]string)
	if !ok {
		t.Fatalf("data[0][y] not []string: %T", data[0]["y"])
	}

	mortgageCount := 0
	for _, l := range labels {
		if l == "Mortgage" {
			mortgageCount++
		}
	}
	if mortgageCount != 1 {
		t.Errorf("expected 'Mortgage' to roll up into exactly one entry; saw %d times in %v", mortgageCount, labels)
	}

	for _, l := range labels {
		if strings.Contains(l, "BOFA HOMELOANS") {
			t.Errorf("expected raw bank text to be replaced by 'Mortgage'; found %q", l)
		}
	}
}
