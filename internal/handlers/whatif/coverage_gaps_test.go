package whatif

// Tests targeting per-function coverage gaps: handleWhatIfIncomeChart,
// handleWhatIfProjectionChartNoGuardrails, handleWhatIfTaxOptimize,
// parseProjectionStartYear, getSettingsHash, buildEngineInput,
// buildIncomeChartData, handleWhatIfSync/syncSettingsFromDashboard,
// the purge handlers' analysis-error branches, and
// handleSwitchScenario's generic-error branch.

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"budget2/internal/models"
	"budget2/internal/services/retirement"
)

// ── parseProjectionStartYear ────────────────────────────────────────────────

func TestParseProjectionStartYear(t *testing.T) {
	currentYear := time.Now().Year()

	tests := []struct {
		name      string
		startDate string
		want      int
	}{
		{"empty falls back to current year", "", currentYear},
		{"garbage falls back to current year", "not-a-date", currentYear},
		{"partial date falls back to current year", "2030", currentYear},
		{"valid YYYY-MM returns its year", "2030-06", 2030},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseProjectionStartYear(tt.startDate); got != tt.want {
				t.Errorf("parseProjectionStartYear(%q) = %d, want %d", tt.startDate, got, tt.want)
			}
		})
	}
}

// ── getSettingsHash ─────────────────────────────────────────────────────────

// A NaN float cannot be marshaled to JSON, so getSettingsHash must return
// the empty string rather than a hash of partial data.
func TestGetSettingsHash_MarshalError(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.MonthlyLivingExpenses = math.NaN()

	if got := getSettingsHash(s); got != "" {
		t.Errorf("getSettingsHash with NaN field = %q, want empty string", got)
	}

	// Sanity: a normal settings object hashes to 16 hex chars (8 bytes).
	valid := models.DefaultWhatIfSettings()
	h := getSettingsHash(valid)
	if len(h) != 16 {
		t.Errorf("getSettingsHash length = %d (%q), want 16", len(h), h)
	}
}

// ── buildEngineInput ────────────────────────────────────────────────────────

// Settings with no Persons fail prepare.From validation; buildEngineInput
// must surface that as a "prepare primary settings" error.
func TestBuildEngineInput_PreparePrimaryError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	s.Persons = nil

	_, _, err := buildEngineInput(s)
	if err == nil {
		t.Fatal("expected buildEngineInput to fail for settings without persons")
	}
	if !strings.Contains(err.Error(), "prepare primary settings") {
		t.Errorf("error = %q, want it to mention 'prepare primary settings'", err.Error())
	}
}

// ── buildIncomeChartData ────────────────────────────────────────────────────

func TestBuildIncomeChartData_NilProjection(t *testing.T) {
	settings := models.DefaultWhatIfSettings()

	chartData := buildIncomeChartData(settings, nil, "nominal")

	traces, ok := chartData["data"].([]map[string]interface{})
	if !ok || len(traces) != 3 {
		t.Fatalf("expected 3 traces for nil projection, got %#v", chartData["data"])
	}
	for _, tr := range traces {
		x := tr["x"].([]int)
		y := tr["y"].([]float64)
		if len(x) != 0 || len(y) != 0 {
			t.Errorf("trace %v: expected empty series for nil projection, got x=%v y=%v", tr["name"], x, y)
		}
	}
	if _, ok := chartData["layout"].(map[string]interface{}); !ok {
		t.Fatalf("expected layout map, got %#v", chartData["layout"])
	}
}

// When TotalIncome < SocialSecurityIncome (e.g. rounding in the engine),
// the "Other Income" bucket must clamp to zero, not go negative.
func TestBuildIncomeChartData_ClampsNegativeOther(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.ProjectionYears = 1

	months := []models.ProjectionMonth{
		{
			Month:                0,
			Year:                 0,
			SocialSecurityIncome: 2000,
			TotalIncome:          1000, // less than SS
		},
	}
	projection := &models.ProjectionResult{Months: months}

	chartData := buildIncomeChartData(settings, projection, "nominal")
	traces := chartData["data"].([]map[string]interface{})

	otherY := traces[1]["y"].([]float64)
	if len(otherY) != 1 {
		t.Fatalf("expected 1 yearly bucket, got %d", len(otherY))
	}
	if otherY[0] != 0 {
		t.Errorf("Other income = %v, want 0 (clamped)", otherY[0])
	}
	ssY := traces[0]["y"].([]float64)
	if ssY[0] != 2000 {
		t.Errorf("SS income = %v, want 2000", ssY[0])
	}
}

func TestBuildIncomeChartData_DtickByHorizon(t *testing.T) {
	tests := []struct {
		years int
		want  int
	}{
		{10, 1},
		{20, 2}, // 13..24-year horizon: the previously uncovered else-if
		{30, 5},
	}
	for _, tt := range tests {
		settings := models.DefaultWhatIfSettings()
		settings.ProjectionYears = tt.years

		chartData := buildIncomeChartData(settings, &models.ProjectionResult{}, "nominal")
		layout := chartData["layout"].(map[string]interface{})
		xaxis := layout["xaxis"].(map[string]interface{})
		if got := xaxis["dtick"]; got != tt.want {
			t.Errorf("ProjectionYears=%d: dtick = %v, want %d", tt.years, got, tt.want)
		}
	}
}

// ── handleWhatIfIncomeChart ─────────────────────────────────────────────────

func TestHandleWhatIfIncomeChart(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/chart/income", nil)
	handleWhatIfIncomeChart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var chartData map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &chartData); err != nil {
		t.Fatalf("unmarshal chart JSON: %v", err)
	}
	data, ok := chartData["data"].([]interface{})
	if !ok || len(data) != 3 {
		t.Fatalf("expected 3 traces, got %#v", chartData["data"])
	}
	names := map[string]bool{}
	for _, tr := range data {
		m := tr.(map[string]interface{})
		names[m["name"].(string)] = true
	}
	for _, want := range []string{"Social Security", "Other Income", "Withdrawals"} {
		if !names[want] {
			t.Errorf("missing trace %q in %v", want, names)
		}
	}
	layout, ok := chartData["layout"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected layout map, got %#v", chartData["layout"])
	}
	if got := layout["title"]; got != "Yearly Income by Source" {
		t.Errorf("title = %v, want 'Yearly Income by Source'", got)
	}
}

func TestHandleWhatIfIncomeChart_RealDollars(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/chart/income?display_dollars=real", nil)
	handleWhatIfIncomeChart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	var chartData map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &chartData); err != nil {
		t.Fatalf("unmarshal chart JSON: %v", err)
	}
	layout := chartData["layout"].(map[string]interface{})
	if got := layout["title"]; got != "Yearly Income by Source — Today's Dollars" {
		t.Errorf("real-dollars title = %v", got)
	}
}

func TestHandleWhatIfIncomeChart_LoadError(t *testing.T) {
	setupBrokenEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/chart/income", nil)
	handleWhatIfIncomeChart(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	var errBody map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("expected JSON error body, got %q: %v", w.Body.String(), err)
	}
	if errBody["error"] == "" {
		t.Errorf("expected non-empty error message, got %#v", errBody)
	}
}

func TestHandleWhatIfIncomeChart_AnalysisError(t *testing.T) {
	setupBrokenChainEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/chart/income", nil)
	handleWhatIfIncomeChart(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	var errBody map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("expected JSON error body, got %q: %v", w.Body.String(), err)
	}
	if errBody["error"] == "" {
		t.Errorf("expected non-empty error message, got %#v", errBody)
	}
}

// ── handleWhatIfProjectionChartNoGuardrails error paths ─────────────────────

func TestHandleWhatIfProjectionChartNoGuardrails_LoadError(t *testing.T) {
	setupBrokenEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/chart/projection/no-guardrails", nil)
	handleWhatIfProjectionChartNoGuardrails(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	var errBody map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("expected JSON error body, got %q: %v", w.Body.String(), err)
	}
	if errBody["error"] == "" {
		t.Errorf("expected non-empty error message, got %#v", errBody)
	}
}

func TestHandleWhatIfProjectionChartNoGuardrails_BuildEngineInputError(t *testing.T) {
	setupBrokenChainEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/chart/projection/no-guardrails", nil)
	handleWhatIfProjectionChartNoGuardrails(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	var errBody map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("expected JSON error body, got %q: %v", w.Body.String(), err)
	}
	if !strings.Contains(errBody["error"], "whatif_corrupt.json") {
		t.Errorf("expected error to reference the corrupt chained scenario, got %q", errBody["error"])
	}
}

// ── handleWhatIfSync / syncSettingsFromDashboard LoadData error ─────────────

// setupSyncEnvWithBadCSVDir wires a dataloader whose CSV directory contains
// an unclosed '[' so filepath.Glob fails with ErrBadPattern — the only way
// LoadData itself errors (unreadable files are skipped with a warning).
func setupSyncEnvWithBadCSVDir(t *testing.T) *retirement.SettingsManager {
	t.Helper()

	badCSVDir := filepath.Join(t.TempDir(), "bad[dir")
	if err := os.MkdirAll(badCSVDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	return wireWhatIfEnv(t, t.TempDir(), badCSVDir)
}

func TestSyncSettingsFromDashboard_LoadDataError(t *testing.T) {
	setupSyncEnvWithBadCSVDir(t)

	s := models.DefaultWhatIfSettings()
	if err := syncSettingsFromDashboard(s); err == nil {
		t.Fatal("expected syncSettingsFromDashboard to fail when LoadData errors")
	}
}

func TestHandleWhatIfSync_LoadDataError(t *testing.T) {
	setupSyncEnvWithBadCSVDir(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/sync", nil)
	handleWhatIfSync(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Failed to sync from dashboard") {
		t.Errorf("body missing sync failure message: %s", w.Body.String())
	}
}

// ── syncSettingsFromDashboard income-pattern branches ───────────────────────

// setupSyncEnvWithCSV wires a fresh env whose CSV dir contains exactly the
// given rows (header added automatically).
func setupSyncEnvWithCSV(t *testing.T, rows []string) {
	t.Helper()

	csvDir := t.TempDir()

	lines := append([]string{"Date,Description,Amount,Type,Category"}, rows...)
	if err := os.WriteFile(filepath.Join(csvDir, "test.csv"), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	wireWhatIfEnv(t, t.TempDir(), csvDir)
}

// Weekly deposits must be converted to a monthly amount (×52/12).
func TestSyncSettingsFromDashboard_WeeklyIncomePattern(t *testing.T) {
	var rows []string
	now := time.Now()
	for i := 0; i < 10; i++ {
		d := now.AddDate(0, 0, -7*i)
		rows = append(rows, fmt.Sprintf("%s,Wages,500,Income,Employment", d.Format("2006-01-02")))
	}
	setupSyncEnvWithCSV(t, rows)

	s := models.DefaultWhatIfSettings()
	if err := syncSettingsFromDashboard(s); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	var wages *models.IncomeSource
	for i := range s.IncomeSources {
		if s.IncomeSources[i].ID == "insights-wages" {
			wages = &s.IncomeSources[i]
		}
	}
	if wages == nil {
		t.Fatalf("expected insights-wages source after weekly sync, got %+v", s.IncomeSources)
	}
	want := 500.0 * 52 / 12
	if diff := wages.Amount - want; diff > 0.01 || diff < -0.01 {
		t.Errorf("weekly income monthly amount = %.2f, want %.2f", wages.Amount, want)
	}
	if wages.Name != "Wages" {
		t.Errorf("Name = %q, want Wages", wages.Name)
	}
}

// Irregular income patterns (wildly varying intervals) are not regular and
// must be skipped, not turned into income sources.
func TestSyncSettingsFromDashboard_SkipsIrregularIncome(t *testing.T) {
	now := time.Now()
	rows := []string{
		fmt.Sprintf("%s,Bonus,3000,Income,Employment", now.Format("2006-01-02")),
		fmt.Sprintf("%s,Bonus,1500,Income,Employment", now.AddDate(0, 0, -3).Format("2006-01-02")),
		fmt.Sprintf("%s,Bonus,4000,Income,Employment", now.AddDate(0, 0, -50).Format("2006-01-02")),
		fmt.Sprintf("%s,Bonus,2500,Income,Employment", now.AddDate(0, 0, -120).Format("2006-01-02")),
	}
	setupSyncEnvWithCSV(t, rows)

	s := models.DefaultWhatIfSettings()
	if err := syncSettingsFromDashboard(s); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	for _, src := range s.IncomeSources {
		if src.ID == "insights-bonus" {
			t.Fatalf("irregular income pattern should be skipped, but got source %+v", src)
		}
	}
}

// A user-modified Type on an auto-detected insights source must survive
// a re-sync (along with StartMonth/EndMonth/COLARate).
func TestSyncSettingsFromDashboard_PreservesModifiedType(t *testing.T) {
	var rows []string
	now := time.Now()
	for i := 0; i < 10; i++ {
		d := now.AddDate(0, 0, -7*i)
		rows = append(rows, fmt.Sprintf("%s,Wages,500,Income,Employment", d.Format("2006-01-02")))
	}
	setupSyncEnvWithCSV(t, rows)

	end := 120
	s := models.DefaultWhatIfSettings()
	s.IncomeSources = []models.IncomeSource{
		{
			ID:         "insights-wages",
			Name:       "Wages",
			Amount:     999, // stale; sync recomputes
			Type:       models.IncomeVariable,
			StartMonth: 6,
			EndMonth:   &end,
			COLARate:   0.02,
		},
	}

	if err := syncSettingsFromDashboard(s); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	var wages *models.IncomeSource
	for i := range s.IncomeSources {
		if s.IncomeSources[i].ID == "insights-wages" {
			wages = &s.IncomeSources[i]
		}
	}
	if wages == nil {
		t.Fatalf("expected insights-wages source, got %+v", s.IncomeSources)
	}
	if wages.Type != models.IncomeVariable {
		t.Errorf("Type = %q, want %q (user modification must be preserved)", wages.Type, models.IncomeVariable)
	}
	if wages.StartMonth != 6 {
		t.Errorf("StartMonth = %d, want 6", wages.StartMonth)
	}
	if wages.EndMonth == nil || *wages.EndMonth != 120 {
		t.Errorf("EndMonth = %v, want 120", wages.EndMonth)
	}
	if wages.COLARate != 0.02 {
		t.Errorf("COLARate = %v, want 0.02", wages.COLARate)
	}
}

// ── Purge handlers: analysis-error branches ─────────────────────────────────

func TestHandleWhatIfPurgeIncome_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		src := models.IncomeSource{ID: "danger-purge-inc", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
		if _, err := rm.AddIncomeSource(src); err != nil {
			t.Fatalf("AddIncomeSource: %v", err)
		}
		if _, err := rm.RemoveIncomeSource("danger-purge-inc"); err != nil {
			t.Fatalf("RemoveIncomeSource: %v", err)
		}
	})

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/income/danger-purge-inc/purge", nil, map[string]string{"id": "danger-purge-inc"})
	handleWhatIfPurgeIncome(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Analysis failed") {
		t.Errorf("body missing 'Analysis failed': %s", w.Body.String())
	}
}

func TestHandleWhatIfPurgeExpense_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		src := models.ExpenseSource{ID: "danger-purge-exp", Name: "Test", Amount: 100}
		if _, err := rm.AddExpenseSource(src); err != nil {
			t.Fatalf("AddExpenseSource: %v", err)
		}
		if _, err := rm.RemoveExpenseSource("danger-purge-exp"); err != nil {
			t.Fatalf("RemoveExpenseSource: %v", err)
		}
	})

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/expense/danger-purge-exp/purge", nil, map[string]string{"id": "danger-purge-exp"})
	handleWhatIfPurgeExpense(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Analysis failed") {
		t.Errorf("body missing 'Analysis failed': %s", w.Body.String())
	}
}

func TestHandleWhatIfPurgeBigTicket_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		item := models.BigTicketItem{ID: "danger-purge-bt", Name: "Test", Amount: 1000, Year: 5, Type: models.BigTicketExpense}
		if _, err := rm.AddBigTicketItem(item); err != nil {
			t.Fatalf("AddBigTicketItem: %v", err)
		}
		if _, err := rm.RemoveBigTicketItem("danger-purge-bt"); err != nil {
			t.Fatalf("RemoveBigTicketItem: %v", err)
		}
	})

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/bigticket/danger-purge-bt/purge", nil, map[string]string{"id": "danger-purge-bt"})
	handleWhatIfPurgeBigTicket(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Analysis failed") {
		t.Errorf("body missing 'Analysis failed': %s", w.Body.String())
	}
}

// ── handleWhatIfTaxOptimize ─────────────────────────────────────────────────

func TestHandleWhatIfTaxOptimize_LoadError(t *testing.T) {
	setupBrokenEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/tax-optimize", nil)
	handleWhatIfTaxOptimize(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Failed to load settings") {
		t.Errorf("body missing load failure message: %s", w.Body.String())
	}
}

func TestHandleWhatIfTaxOptimize_BuildEngineInputError(t *testing.T) {
	setupBrokenChainEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/tax-optimize", nil)
	handleWhatIfTaxOptimize(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Failed to build engine input") {
		t.Errorf("body missing engine-input failure message: %s", w.Body.String())
	}
}

// With a nil renderer the handler must fall back to encoding the partial
// data as JSON. Uses a short projection horizon to keep the optimizer fast.
func TestHandleWhatIfTaxOptimize_JSONFallback(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.ProjectionYears = 10
	if err := rm.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/tax-optimize", nil)
	handleWhatIfTaxOptimize(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("expected JSON fallback body: %v\n%s", err, w.Body.String())
	}
	analysis, ok := body["Analysis"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected Analysis object in JSON fallback, got %#v", body["Analysis"])
	}
	if analysis["tax_optimizer"] == nil {
		t.Errorf("expected tax_optimizer result in Analysis, got keys %v", mapKeys(analysis))
	}
}

func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ── handleWhatIfSettings validation branches ────────────────────────────────

func TestHandleWhatIfSettings_InvalidRMDTiming(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"rmd_timing": {"bogus"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Invalid RMD timing") {
		t.Errorf("body missing 'Invalid RMD timing': %s", w.Body.String())
	}
}

// Optional-float fields (state_income_tax_rate) that are present and
// non-empty but unparseable must produce the ParseLabel error message.
func TestHandleWhatIfSettings_InvalidStateIncomeTaxRate(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"state_income_tax_rate": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Invalid state income tax rate") {
		t.Errorf("body missing state tax parse error: %s", w.Body.String())
	}
}

// ── Healthcare person-link branches ─────────────────────────────────────────

func TestHandleWhatIfAddHealthcare_UnknownPersonID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"person_id": {"no-such-person"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Selected person was not found") {
		t.Errorf("body missing unknown-person message: %s", w.Body.String())
	}
	assertRetargetHeader(t, w, "#whatif-add-healthcare-error")
}

// Linking a healthcare entry to a valid person must copy that person's
// name and derived age into the entry.
func TestHandleWhatIfUpdateHealthcare_LinkToPerson(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Capture the persisted primary person AFTER the first save: an unsaved
	// default Load() would regenerate the person UUID on the next write.
	hp := models.HealthcarePerson{ID: "hc-link", Name: "Unlinked", CurrentAge: 50, CurrentCoverage: models.CoverageACA, MedicareEligibleAge: 65}
	settings, err := rm.AddHealthcarePerson(hp)
	if err != nil {
		t.Fatalf("AddHealthcarePerson: %v", err)
	}
	primaryID := settings.Persons[0].ID
	primaryName := settings.Persons[0].Name

	form := url.Values{"person_id": {primaryID}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/hc-link", formBody(form), map[string]string{"id": "hc-link"})
	handleWhatIfUpdateHealthcare(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	after, err := rm.Load()
	if err != nil {
		t.Fatalf("Load after update: %v", err)
	}
	var linked *models.HealthcarePerson
	for i := range after.HealthcarePersons {
		if after.HealthcarePersons[i].ID == "hc-link" {
			linked = &after.HealthcarePersons[i]
		}
	}
	if linked == nil {
		t.Fatalf("healthcare person hc-link not found after update: %+v", after.HealthcarePersons)
	}
	if linked.PersonID != primaryID {
		t.Errorf("PersonID = %q, want %q", linked.PersonID, primaryID)
	}
	if linked.Name != primaryName {
		t.Errorf("Name = %q, want %q (copied from linked person)", linked.Name, primaryName)
	}
	if linked.CurrentAge == 50 {
		t.Errorf("CurrentAge still 50; expected age derived from linked person's birth month")
	}
}

// Unlinking (person_id present but empty) with an explicit name and age must
// store the provided age.
func TestHandleWhatIfUpdateHealthcare_UnlinkWithNameAndAge(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	hp := models.HealthcarePerson{ID: "hc-unlink", Name: "Old Name", CurrentAge: 60, CurrentCoverage: models.CoverageACA, MedicareEligibleAge: 65}
	if _, err := rm.AddHealthcarePerson(hp); err != nil {
		t.Fatalf("AddHealthcarePerson: %v", err)
	}

	form := url.Values{
		"person_id":   {""},
		"name":        {"Solo"},
		"current_age": {"55"},
	}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/hc-unlink", formBody(form), map[string]string{"id": "hc-unlink"})
	handleWhatIfUpdateHealthcare(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	after, err := rm.Load()
	if err != nil {
		t.Fatalf("Load after update: %v", err)
	}
	var updated *models.HealthcarePerson
	for i := range after.HealthcarePersons {
		if after.HealthcarePersons[i].ID == "hc-unlink" {
			updated = &after.HealthcarePersons[i]
		}
	}
	if updated == nil {
		t.Fatalf("healthcare person hc-unlink not found after update: %+v", after.HealthcarePersons)
	}
	if updated.PersonID != "" {
		t.Errorf("PersonID = %q, want empty (unlinked)", updated.PersonID)
	}
	if updated.Name != "Solo" {
		t.Errorf("Name = %q, want Solo", updated.Name)
	}
	if updated.CurrentAge != 55 {
		t.Errorf("CurrentAge = %d, want 55", updated.CurrentAge)
	}
}

// ── handleSwitchScenario generic (non-typed) error branch ───────────────────

// A Stat failure that is NOT os.IsNotExist must fall through to the generic
// 500 renderError branch.
//
// SwitchScenario never calls saveInternal (it only Stats the target file and
// flips sm.filename in memory), so it needs its own injection independent of
// makeSaveFail's save-failure mechanism -- this used to reuse makeSaveFail's
// settingsDir-destroyed-to-a-plain-FILE trick (before that, chmod 0o000,
// which root's CAP_DAC_OVERRIDE reads straight through), piggybacking on the
// fact that destroying the directory made every Stat underneath it fail
// ENOTDIR. makeSaveFail's root-proof replacement deliberately keeps
// settingsDir itself real and readable (so loads keep succeeding and only
// writes fail), which means a lookup of a genuinely absent scenario file now
// correctly returns ENOENT -- the typed ScenarioNotFoundError, not this
// test's generic branch. Root-proof replacement here: an overlong scenario
// basename. scenarioPath's own validation only checks string shape (starts
// with "whatif", ends in ".json", no slashes or ".."), so a basename that
// satisfies all of that but exceeds NAME_MAX (255 bytes, the F3/
// TestRenameScenario_WriteError precedent) passes validation and reaches
// sm.store.Stat(path), which then fails ENAMETOOLONG at the kernel level --
// not os.IsNotExist -- at any uid.
func TestHandleSwitchScenario_GenericError(t *testing.T) {
	_, _, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	longName := "whatif" + strings.Repeat("x", 250) + ".json" // 261 bytes > NAME_MAX (255)

	form := url.Values{"filename": {longName}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/scenarios/switch", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleSwitchScenario(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Failed to switch scenario") {
		t.Errorf("body missing generic switch failure message: %s", w.Body.String())
	}
	if got := w.Header().Get("HX-Retarget"); got != "" {
		t.Errorf("generic errors must not be retargeted, got HX-Retarget=%q", got)
	}
}
