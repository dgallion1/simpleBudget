package dashboard

// KD1 oracle — staged into internal/handlers/dashboard/ by
// .swarm/tier3/KD1/accept.sh for one run, then removed. Encodes the
// KD-2026-08-30d reconciliation contract with MULTI-month fixtures that
// discriminate signed-per-month arithmetic from per-month-Abs arithmetic
// (every earlier fixture was single-month, where the two are identical).

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"budget2/internal/models"
	"budget2/internal/templates"
	"budget2/internal/testutil"
)

// oracleKD1Env builds a router over custom CSV rows plus the flagged
// "Lucid Loan" major expense, mirroring setupLivingHealthcareEnv.
func oracleKD1Env(t *testing.T, rows [][]string, withRenderer bool) chi.Router {
	t.Helper()
	_, dl, store, cleanup := writeTempCSV(t, rows)
	t.Cleanup(cleanup)
	if err := dl.SaveMajorExpenses([]models.MajorExpense{
		{ID: "lucid", Name: "Lucid Loan", Keywords: []string{"Lucid"}, ExcludeFromPlanSync: true},
	}); err != nil {
		t.Fatalf("SaveMajorExpenses: %v", err)
	}
	var rend *templates.Renderer
	if withRenderer {
		templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
		var err error
		rend, err = templates.New(templateDir, false)
		if err != nil {
			t.Fatalf("templates.New: %v", err)
		}
	}
	Initialize(dl, rend, nil, store)
	r := chi.NewRouter()
	RegisterRoutes(r)
	return r
}

// TestOracleKD1_LivingSignedRowsReconcile: Jan is refund-dominant (+500
// net refund), Feb is ordinary spend (1000). Contract: rows are negated
// signed sums (-500, 1000) and Total is their sum (500), which equals
// Metrics.LivingExpensesTotal = Abs(500-1000). Per-month-Abs arithmetic
// would render Jan=500, Total=1500 and must fail here.
func TestOracleKD1_LivingSignedRowsReconcile(t *testing.T) {
	rows := [][]string{
		{"2025-01-25", "Autopay Reversal", "500", "Shopping"},
		{"2025-02-05", "Rent", "-1000", "Housing"},
		{"2025-02-14", "Lucid Loan Payment", "-600", "Loan"},
	}
	router := oracleKD1Env(t, rows, false)
	result := decodeMonthDetail(t, router, "/dashboard/kpi/living?start=2025-01-01&end=2025-02-28")

	monthly, _ := result["Monthly"].([]interface{})
	if len(monthly) != 2 {
		t.Fatalf("Monthly = %v, want 2 months", monthly)
	}
	want := map[string]float64{"2025-01": -500, "2025-02": 1000}
	var rowSum float64
	for _, m := range monthly {
		row, ok := m.(map[string]interface{})
		if !ok {
			t.Fatalf("month row is %T, want object", m)
		}
		month, _ := row["Month"].(string)
		value, _ := row["Value"].(float64)
		if wv, ok := want[month]; !ok || value != wv {
			t.Errorf("month %s Value = %v, want %v (negated signed sum, no per-month Abs)", month, value, want[month])
		}
		rowSum += value
	}
	total, _ := result["Total"].(float64)
	if total != rowSum {
		t.Errorf("Total = %v but rows sum to %v — rows must reconcile with the tile exactly", total, rowSum)
	}
	if total != 500 {
		t.Errorf("Total = %v, want 500 = Abs(range signed sum) = Metrics.LivingExpensesTotal", total)
	}
}

// TestOracleKD1_HealthcareSignedRowsReconcile: same discrimination for the
// healthcare kind. Coverage starts at the Jan-10 premium; Feb is
// refund-dominant. Contract rows: Jan=300, Feb=-100, Total=200.
// Per-month-Abs would give Total=400.
func TestOracleKD1_HealthcareSignedRowsReconcile(t *testing.T) {
	rows := [][]string{
		{"2025-01-10", "Health Insurance Premium", "-300", "Health Insurance"},
		{"2025-02-05", "Health Insurance Autopay Reversal", "100", "Health Insurance"},
		{"2025-01-05", "Rent", "-1500", "Housing"},
	}
	router := oracleKD1Env(t, rows, false)
	result := decodeMonthDetail(t, router, "/dashboard/kpi/healthcare?start=2025-01-01&end=2025-02-28")

	monthly, _ := result["Monthly"].([]interface{})
	if len(monthly) != 2 {
		t.Fatalf("Monthly = %v, want 2 months", monthly)
	}
	want := map[string]float64{"2025-01": 300, "2025-02": -100}
	var rowSum float64
	for _, m := range monthly {
		row := m.(map[string]interface{})
		month, _ := row["Month"].(string)
		value, _ := row["Value"].(float64)
		if wv, ok := want[month]; !ok || value != wv {
			t.Errorf("month %s Value = %v, want %v", month, value, want[month])
		}
		rowSum += value
	}
	total, _ := result["Total"].(float64)
	if total != rowSum || total != 200 {
		t.Errorf("Total = %v (rows sum %v), want 200 — signed reconciliation", total, rowSum)
	}
}

// TestOracleKD1_ExportMatchesSignedRows: the CSV export must carry the
// same signed month values as the modal (shared month function). The Jan
// living line must show the net refund as negative.
func TestOracleKD1_ExportMatchesSignedRows(t *testing.T) {
	rows := [][]string{
		{"2025-01-25", "Autopay Reversal", "500", "Shopping"},
		{"2025-02-05", "Rent", "-1000", "Housing"},
	}
	router := oracleKD1Env(t, rows, false)
	rec := doGet(t, router, "/dashboard/kpi/living/export?start=2025-01-01&end=2025-02-28")
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if len(strings.TrimSpace(body)) == 0 {
		t.Fatal("export body is empty")
	}
	var janLine string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "2025-01") {
			janLine = line
		}
	}
	if janLine == "" {
		t.Fatalf("no 2025-01 line in export:\n%s", body)
	}
	if !strings.Contains(janLine, "-500") {
		t.Errorf("export Jan line %q must carry the signed net refund (-500)", janLine)
	}
}

// TestOracleKD1_MonthDrillMatchesSignedRow: the month drill-down's total
// must equal the parent modal row exactly — including a refund-dominant
// month, where both are the NEGATIVE net (-500), not an Abs.
func TestOracleKD1_MonthDrillMatchesSignedRow(t *testing.T) {
	rows := [][]string{
		{"2025-01-25", "Autopay Reversal", "500", "Shopping"},
		{"2025-02-05", "Rent", "-1000", "Housing"},
	}
	router := oracleKD1Env(t, rows, false)
	result := decodeMonthDetail(t, router, "/dashboard/kpi/living/month/2025-01?start=2025-01-01&end=2025-02-28")
	total, _ := result["Total"].(float64)
	if total != -500 {
		t.Errorf("month drill Total = %v, want -500 (parent row's signed value, not Abs)", total)
	}
}

// TestOracleKD1_NoCoverageDashRegression: the KD-2026-08-30c state must
// keep rendering an em-dash tile with real text, never $0.00.
func TestOracleKD1_NoCoverageDashRegression(t *testing.T) {
	rows := [][]string{
		{"2025-01-15", "Health Insurance Autopay Reversal", "150", "Health Insurance"},
		{"2025-01-05", "Rent", "-500", "Housing"},
	}
	router := oracleKD1Env(t, rows, true)
	rec := doGet(t, router, "/dashboard/kpi/healthcare?start=2025-01-01&end=2025-01-31")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "no coverage in this range") {
		t.Error("missing 'no coverage in this range' text")
	}
	if strings.Contains(body, "$0.00/mo") {
		t.Error("no-coverage state must not render a $0.00 per-month figure")
	}
}
