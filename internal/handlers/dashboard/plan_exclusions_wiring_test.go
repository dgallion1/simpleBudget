package dashboard

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/metrics"
	"budget2/internal/services/retirement"
	"budget2/internal/templates"
	"budget2/internal/testutil"

	"github.com/go-chi/chi/v5"
)

// SY4 attempt 2 / ruling SY-2026-08-30c (checker-tests F1): no attempt-1
// test actually executed handleDashboard/handleKPIsPartial/handleChartData's
// own map-BUILDING code -- every dashboard test called metrics.Calculate or
// buildBudgetVsActualChartData directly with an explicit map, so breaking
// planSyncExclusions' wiring at any of the three call sites (e.g. passing
// nil unconditionally) left the whole suite green. These three tests drive
// the REAL HTTP handlers end to end against a fixture data directory
// containing a flagged major expense, so a broken wiring site fails here.
//
// planExclusionWiringRows is a one-month fixture: $5000 salary, $1500 rent,
// and a $600 "Lucid Loan Payment" that a flagged major expense (matched by
// keyword) must exclude from every living-expense surface.
func planExclusionWiringRows() [][]string {
	return [][]string{
		{"2025-01-15", "Salary", "5000", "Payroll"},
		{"2025-01-05", "Rent", "-1500", "Housing"},
		{"2025-01-20", "Lucid Loan Payment", "-600", "Loan"},
	}
}

// setupPlanExclusionEnv writes planExclusionWiringRows() plus a flagged
// "Lucid Loan" major expense (keyword-matched) to a temp data directory,
// initializes the dashboard package against it, and returns the router.
// withRenderer selects setupTestEnvWithRenderer's real-template path (needed
// to inspect rendered HTML) vs. a nil renderer (handleKPIsPartial and
// handleChartData both fall back to raw JSON with no renderer wired, which
// is simpler to assert against directly).
func setupPlanExclusionEnv(t *testing.T, withRenderer bool) (chi.Router, func()) {
	t.Helper()

	tmpDir, dl, store, cleanup := writeTempCSV(t, planExclusionWiringRows())
	if err := dl.SaveMajorExpenses([]models.MajorExpense{
		{ID: "lucid", Name: "Lucid Loan", Keywords: []string{"Lucid"}, ExcludeFromPlanSync: true},
	}); err != nil {
		cleanup()
		t.Fatalf("SaveMajorExpenses: %v", err)
	}

	// A living target is wired (mirrors TestDashboardKPIs_LivingSparkline_
	// HasTargetAttribute's pattern) so the budget-vs-actual chart's
	// rawCombinedTarget gate doesn't short-circuit to an empty payload --
	// the chart-wiring test needs a non-empty Living trace to assert on.
	rm := retirement.NewSettingsManager(tmpDir, store)
	settingsPath := filepath.Join(tmpDir, "whatif.json")
	if err := os.WriteFile(settingsPath, []byte(`{"monthly_living_expenses": 1200}`), 0o600); err != nil {
		cleanup()
		t.Fatalf("write settings: %v", err)
	}

	var rend *templates.Renderer
	if withRenderer {
		templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
		var err error
		rend, err = templates.New(templateDir, false)
		if err != nil {
			cleanup()
			t.Fatalf("templates.New: %v", err)
		}
	}
	Initialize(dl, rend, rm, store)

	r := chi.NewRouter()
	RegisterRoutes(r)
	return r, cleanup
}

func doGetJSON(t *testing.T, router chi.Router, path string, out any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, body = %s", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("GET %s: decode JSON: %v; body = %s", path, err, rec.Body.String())
	}
}

// TestHandleDashboard_PlanExclusionWiring drives GET /dashboard end to end
// (real renderer) and asserts the rendered Monthly Living Expenses figure
// excludes the flagged $600 Lucid payment -- i.e. that handleDashboard's own
// planSyncExclusions(data.Active()) call is actually wired into the
// metrics.Calculate it feeds, not just that metrics.Calculate itself works
// when called directly with an explicit map.
func TestHandleDashboard_PlanExclusionWiring(t *testing.T) {
	router, cleanup := setupPlanExclusionEnv(t, true)
	defer cleanup()

	rec := doGet(t, router, "/dashboard?start=2025-01-01&end=2025-01-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// The card renders ActualMonthly (LivingExpensesTotal / MonthsInRange),
	// not the raw total -- so the expected figure is the rent-only total
	// divided by the same MonthsBetween the handler uses, not 1500 itself.
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)
	want := 1500.0 / metrics.MonthsBetween(start, end)

	got := extractDollarAfter(t, body, "Monthly Living Expenses</p>")
	if math.Abs(got-want) > 0.01 {
		t.Errorf("rendered Monthly Living Expenses = %.2f, want %.2f (rent only, converted to a monthly rate; handleDashboard must wire planSyncExclusions into metrics.Calculate): %s",
			got, want, trunc(body, 2000))
	}
}

// TestHandleKPIsPartial_PlanExclusionWiring drives GET /dashboard/kpis with
// no renderer wired (JSON fallback), decoding the raw DashboardMetrics JSON
// handleKPIsPartial serves, and asserts living_expenses_total excludes the
// flagged Lucid payment.
func TestHandleKPIsPartial_PlanExclusionWiring(t *testing.T) {
	router, cleanup := setupPlanExclusionEnv(t, false)
	defer cleanup()

	var out struct {
		Metrics struct {
			LivingExpensesTotal float64 `json:"living_expenses_total"`
		} `json:"Metrics"`
	}
	doGetJSON(t, router, "/dashboard/kpis?start=2025-01-01&end=2025-01-31", &out)

	if out.Metrics.LivingExpensesTotal != 1500 {
		t.Errorf("Metrics.living_expenses_total = %v, want 1500 (handleKPIsPartial must wire planSyncExclusions into metrics.Calculate)", out.Metrics.LivingExpensesTotal)
	}
}

// TestHandleChartData_BudgetVsActual_PlanExclusionWiring drives GET
// /dashboard/charts/data/budget-vs-actual (always JSON) and asserts the
// Living trace's January bar excludes the flagged Lucid payment.
func TestHandleChartData_BudgetVsActual_PlanExclusionWiring(t *testing.T) {
	router, cleanup := setupPlanExclusionEnv(t, false)
	defer cleanup()

	var out struct {
		Data []struct {
			Name string    `json:"name"`
			Y    []float64 `json:"y"`
		} `json:"data"`
	}
	doGetJSON(t, router, "/dashboard/charts/data/budget-vs-actual?start=2025-01-01&end=2025-01-31", &out)

	var livingY []float64
	for _, trace := range out.Data {
		if trace.Name == "Living" {
			livingY = trace.Y
			break
		}
	}
	if len(livingY) == 0 {
		t.Fatalf("no Living trace found in response: %+v", out.Data)
	}
	if livingY[0] != 1500 {
		t.Errorf("Living trace y[0] = %v, want 1500 (handleChartData's budget-vs-actual branch must wire planSyncExclusions into buildBudgetVsActualChartData)", livingY[0])
	}
}
