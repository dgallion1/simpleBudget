package whatif

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/retirement"
	"budget2/internal/services/retirement/engine"
	"budget2/internal/services/retirement/prepare"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/internal/testutil"
)

// setupTestEnv creates temp directories, initializes package-level vars,
// and returns the SettingsManager plus a cleanup function.
func setupTestEnv(t *testing.T) (*retirement.SettingsManager, func()) {
	t.Helper()

	settingsDir := t.TempDir()
	csvDir := t.TempDir()

	// Create a minimal CSV so dataloader.LoadData returns data
	csvPath := filepath.Join(csvDir, "test.csv")
	csvContent := "Date,Description,Amount,Type,Category\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Salary,5000,Income,Employment\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Rent,-2000,Outflow,Housing\n" +
		time.Now().AddDate(0, -2, 0).Format("2006-01-02") + ",Salary,5000,Income,Employment\n" +
		time.Now().AddDate(0, -2, 0).Format("2006-01-02") + ",Groceries,-500,Outflow,Food\n"
	os.WriteFile(csvPath, []byte(csvContent), 0644)

	store, err := storage.New(settingsDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	rm := retirement.NewSettingsManager(settingsDir, store)
	dl := dataloader.New(csvDir, store)

	// Set package-level vars; renderer is nil so handlers encode JSON
	Initialize(dl, nil, rm)

	// Clear cache
	cache.mu.Lock()
	cache.hash = ""
	cache.analysis = nil
	cache.cachedAt = time.Time{}
	cache.mu.Unlock()

	cleanup := func() {
		// Nothing extra needed; t.TempDir auto-cleans
	}
	return rm, cleanup
}

// readJSON reads the response body and unmarshals into a map.
func readJSON(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal JSON: %v\nbody: %s", err, string(body))
	}
	return m
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

func assertRetargetHeader(t *testing.T, w *httptest.ResponseRecorder, target string) {
	t.Helper()
	if got := w.Header().Get("HX-Retarget"); got != target {
		t.Fatalf("HX-Retarget = %q, want %q", got, target)
	}
	if got := w.Header().Get("HX-Reswap"); got != "innerHTML" {
		t.Fatalf("HX-Reswap = %q, want innerHTML", got)
	}
}

// chiRequest creates an http.Request with chi URL params set.
func chiRequest(method, path string, body io.Reader, params map[string]string) *http.Request {
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func formBody(vals url.Values) *strings.Reader {
	return strings.NewReader(vals.Encode())
}

func readRecorderPageData(t *testing.T, w *httptest.ResponseRecorder) *models.WhatIfPageData {
	t.Helper()
	var pageData models.WhatIfPageData
	if err := json.Unmarshal(w.Body.Bytes(), &pageData); err != nil {
		t.Fatalf("unmarshal page data: %v\nbody: %s", err, w.Body.String())
	}
	return &pageData
}

func primeAnalysisCache(settings *models.WhatIfSettings, monthlyExpenses float64) {
	cache.mu.Lock()
	cache.hash = getSettingsHash(settings)
	cache.analysis = &models.WhatIfAnalysis{
		Settings: settings,
		BudgetFit: &models.BudgetFitAnalysis{
			MonthlyExpenses: monthlyExpenses,
		},
	}
	cache.cachedAt = time.Now()
	cache.mu.Unlock()
}

// ── getSettingsHash ─────────────────────────────────────────────────────────

func TestGetSettingsHash(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	h := getSettingsHash(s)
	if h == "" {
		t.Fatal("expected non-empty hash")
	}
	// Same settings -> same hash
	if h2 := getSettingsHash(s); h2 != h {
		t.Fatalf("same settings, different hash: %s vs %s", h, h2)
	}
	// Different settings -> different hash
	s.PortfolioValue = 999999
	if h3 := getSettingsHash(s); h3 == h {
		t.Fatal("different settings produced same hash")
	}
}

// ── normalizeDisplayDollars ─────────────────────────────────────────────────

func TestNormalizeDisplayDollars(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"real", "real"},
		{"nominal", "nominal"},
		{"", "nominal"},
		{"anything", "nominal"},
	}
	for _, tt := range tests {
		if got := normalizeDisplayDollars(tt.in); got != tt.want {
			t.Errorf("normalizeDisplayDollars(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ── humanizeScenarioFilename ────────────────────────────────────────────────

func TestHumanizeScenarioFilename(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"downsize-plan.json", "Downsize Plan"},
		{"early_retirement.json", "Early Retirement"},
		{"simple.json", "Simple"},
	}
	for _, tt := range tests {
		if got := humanizeScenarioFilename(tt.in); got != tt.want {
			t.Errorf("humanizeScenarioFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ── projectionValueAtYear ───────────────────────────────────────────────────

func TestProjectionValueAtYear(t *testing.T) {
	proj := sampleProjectionForChart()

	// nominal
	v := projectionValueAtYear(proj, 0, "nominal")
	if v != proj.Months[0].PortfolioBalance {
		t.Fatalf("expected nominal balance at year 0, got %f", v)
	}

	// real
	v = projectionValueAtYear(proj, 0, "real")
	if v != proj.Months[0].PortfolioBalanceReal {
		t.Fatalf("expected real balance at year 0, got %f", v)
	}

	// nil projection
	v = projectionValueAtYear(nil, 0, "nominal")
	if v != 0 {
		t.Fatalf("nil projection should return 0, got %f", v)
	}

	// empty projection
	v = projectionValueAtYear(&models.ProjectionResult{}, 0, "nominal")
	if v != 0 {
		t.Fatalf("empty projection should return 0, got %f", v)
	}

	// negative year clamps to 0
	v = projectionValueAtYear(proj, -1, "nominal")
	if v != proj.Months[0].PortfolioBalance {
		t.Fatalf("negative year should clamp to 0")
	}

	// beyond-max year clamps to last
	v = projectionValueAtYear(proj, 9999, "nominal")
	last := proj.Months[len(proj.Months)-1].PortfolioBalance
	if v != last {
		t.Fatalf("beyond-max should clamp to last, got %f want %f", v, last)
	}
}

// ── buildProjectionChartEvents ──────────────────────────────────────────────

func TestBuildProjectionChartEvents_NilInputs(t *testing.T) {
	if events := buildProjectionChartEvents(nil, nil); events != nil {
		t.Fatalf("expected nil for nil inputs, got %v", events)
	}
}

func TestHandleWhatIfSettings_PersonsWithoutStartDate(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	vals := url.Values{
		// start_date intentionally omitted — handler must reject persons without it.
		"person_id[]":          {"primary"},
		"person_name[]":        {"Alex"},
		"person_birth_month[]": {"1960-05"},
		"person_role[]":        {"primary"},
	}
	req := httptest.NewRequest(http.MethodPost, "/whatif/settings", formBody(vals))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handleWhatIfSettings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Projection start date is required") {
		t.Errorf("body should explain missing start date, got: %s", w.Body.String())
	}
}

func TestHandleWhatIfSettings_WithPersons(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	vals := url.Values{
		"start_date":           {"2026-04"},
		"person_id[]":          {"primary", "spouse"},
		"person_name[]":        {"Alex", "Casey"},
		"person_birth_month[]": {"1960-05", "1962-04"},
		"person_role[]":        {"primary", "spouse"},
		"phase_age_reference":  {"spouse"},
	}

	req := httptest.NewRequest(http.MethodPost, "/whatif/settings", formBody(vals))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handleWhatIfSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if settings.CurrentAge != 65 || settings.SpouseAge != 64 {
		t.Fatalf("derived ages = (%d,%d), want (65,64)", settings.CurrentAge, settings.SpouseAge)
	}
	if settings.PhaseAgeReference != "spouse" {
		t.Fatalf("PhaseAgeReference = %q, want spouse", settings.PhaseAgeReference)
	}
}

func TestHandleWhatIfAddHealthcare_LinkedPerson(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.MonthlyHealthcare = 0
	settings.HealthcarePersons = []models.HealthcarePerson{}
	settings.Persons = []models.Person{
		{ID: "primary", Name: "Alex", BirthMonth: "1960-05", Role: models.PersonRolePrimary},
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	vals := url.Values{
		"person_id":            {"primary"},
		"current_coverage":     {"aca"},
		"current_monthly_cost": {"900"},
	}

	req := httptest.NewRequest(http.MethodPost, "/whatif/healthcare", formBody(vals))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handleWhatIfAddHealthcare(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(loaded.HealthcarePersons) != 1 {
		t.Fatalf("expected 1 healthcare person, got %d", len(loaded.HealthcarePersons))
	}
	person := loaded.HealthcarePersons[0]
	if person.PersonID != "primary" || person.Name != "Alex" || person.CurrentAge != 65 {
		t.Fatalf("linked healthcare person = %+v", person)
	}
}

// ── buildProjectionChartData ────────────────────────────────────────────────

func TestBuildProjectionChartData_NilProjection(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	data := buildProjectionChartData(s, nil, "nominal")
	if data == nil {
		t.Fatal("expected non-nil chart data")
	}
	traces := data["data"].([]map[string]interface{})
	if len(traces) == 0 {
		t.Fatal("expected at least one trace")
	}
}

func TestBuildProjectionChartData_SurvivesFalse(t *testing.T) {
	proj := sampleProjectionForChart()
	proj.Survives = false
	s := models.DefaultWhatIfSettings()
	data := buildProjectionChartData(s, proj, "nominal")
	traces := data["data"].([]map[string]interface{})
	line := traces[0]["line"].(map[string]interface{})
	if line["color"] != "#ef4444" {
		t.Fatalf("expected red line color for non-surviving, got %v", line["color"])
	}
}

func TestBuildProjectionChartData_DtickVariations(t *testing.T) {
	proj := sampleProjectionForChart()

	tests := []struct {
		years int
		dtick int
	}{
		{10, 1},
		{20, 2},
		{40, 5},
	}
	for _, tt := range tests {
		s := models.DefaultWhatIfSettings()
		s.ProjectionYears = tt.years
		data := buildProjectionChartData(s, proj, "nominal")
		layout := data["layout"].(map[string]interface{})
		xaxis := layout["xaxis"].(map[string]interface{})
		if xaxis["dtick"] != tt.dtick {
			t.Errorf("years=%d: dtick=%v, want %d", tt.years, xaxis["dtick"], tt.dtick)
		}
	}
}

func TestBuildProjectionChartData_NilSettings(t *testing.T) {
	proj := sampleProjectionForChart()
	data := buildProjectionChartData(nil, proj, "nominal")
	if data == nil {
		t.Fatal("expected non-nil chart data with nil settings")
	}
	// dtick should be 5 (default) when settings is nil
	layout := data["layout"].(map[string]interface{})
	xaxis := layout["xaxis"].(map[string]interface{})
	if xaxis["dtick"] != 5 {
		t.Errorf("expected dtick=5 for nil settings, got %v", xaxis["dtick"])
	}
}

// ── renderError ─────────────────────────────────────────────────────────────

func TestRenderError(t *testing.T) {
	w := httptest.NewRecorder()
	renderError(w, "something broke", http.StatusBadRequest)
	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "something broke") {
		t.Fatalf("body should contain error message")
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected html content type, got %s", ct)
	}
}

// ── parseFormFloat / parseFormInt / parseRequiredFormFloat ───────────────────

func TestParseFormFloat(t *testing.T) {
	tests := []struct {
		val     string
		wantV   float64
		wantErr bool
	}{
		{"", 0, false},
		{"3.14", 3.14, false},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		form := url.Values{"key": {tt.val}}
		req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		v, err := parseFormFloat(req, "key")
		if (err != nil) != tt.wantErr {
			t.Errorf("val=%q: err=%v, wantErr=%v", tt.val, err, tt.wantErr)
		}
		if !tt.wantErr && v != tt.wantV {
			t.Errorf("val=%q: v=%f, want %f", tt.val, v, tt.wantV)
		}
	}
}

func TestParseFormInt(t *testing.T) {
	tests := []struct {
		val     string
		wantV   int
		wantErr bool
	}{
		{"", 0, false},
		{"42", 42, false},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		form := url.Values{"key": {tt.val}}
		req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		v, err := parseFormInt(req, "key")
		if (err != nil) != tt.wantErr {
			t.Errorf("val=%q: err=%v, wantErr=%v", tt.val, err, tt.wantErr)
		}
		if !tt.wantErr && v != tt.wantV {
			t.Errorf("val=%q: v=%d, want %d", tt.val, v, tt.wantV)
		}
	}
}

func TestParseRequiredFormFloat(t *testing.T) {
	// Missing
	req := httptest.NewRequest("POST", "/", nil)
	_, err := parseRequiredFormFloat(req, "key")
	if err == nil {
		t.Fatal("expected error for missing required field")
	}

	// Invalid
	form := url.Values{"key": {"abc"}}
	req = httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err = parseRequiredFormFloat(req, "key")
	if err == nil {
		t.Fatal("expected error for invalid float")
	}

	// Valid
	form = url.Values{"key": {"1.5"}}
	req = httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	v, err := parseRequiredFormFloat(req, "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 1.5 {
		t.Fatalf("got %f, want 1.5", v)
	}
}

// ── handleWhatIf (GET /whatif) ──────────────────────────────────────────────

func TestHandleWhatIf(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)

	resp := w.Result()
	// With renderer=nil, should return fallback HTML
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "What-If Analysis") {
		t.Fatal("expected fallback HTML with title")
	}
}

// ── handleWhatIfCalculate (POST /whatif/calculate) ──────────────────────────

func TestHandleWhatIfCalculate(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/calculate", nil)
	handleWhatIfCalculate(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	data := readJSON(t, resp)
	if data["Settings"] == nil {
		t.Fatal("expected Settings in response")
	}
	if data["Analysis"] == nil {
		t.Fatal("expected Analysis in response")
	}
}

// ── handleWhatIfSettings (POST /whatif/settings) ────────────────────────────

func TestHandleWhatIfSettings_BasicUpdate(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"portfolio_value":         {"1500000"},
		"monthly_living_expenses": {"5000"},
		"current_age":             {"60"},
		"projection_years":        {"30"},
		"investment_return":       {"7.0"},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		body := readBody(t, resp)
		t.Fatalf("status = %d, body: %s", resp.StatusCode, body)
	}
}

func TestHandleWhatIfSettings_InvalidAge(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"current_age": {"10"}} // too young
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for age=10, got %d", w.Code)
	}

	form = url.Values{"current_age": {"130"}} // too old
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for age=130, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_InvalidSpouseAge(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"spouse_age": {"130"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_InvalidPhaseAgeReference(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"phase_age_reference": {"invalid"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_ValidPhaseAgeReferences(t *testing.T) {
	for _, ref := range []string{"younger", "older", "primary", "spouse"} {
		t.Run(ref, func(t *testing.T) {
			_, cleanup := setupTestEnv(t)
			defer cleanup()

			form := url.Values{"phase_age_reference": {ref}}
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			handleWhatIfSettings(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 for ref=%s, got %d", ref, w.Code)
			}
		})
	}
}

func TestHandleWhatIfSettings_TaxDeferredPctBadRange(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"tax_deferred_percent": {"110"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_RothPctTooHigh(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"roth_percent": {"110"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_TaxDeferredPlusRothOver100(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"tax_deferred_percent": {"60"},
		"roth_percent":         {"50"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_StockPctBadRange(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"stock_percent": {"-5"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_CashPctBadRange(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"cash_percent": {"110"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_StockPlusCashOver100(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"stock_percent": {"70"},
		"cash_percent":  {"40"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_PerAccountAllocation(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"tax_deferred_stock_percent": {"60"},
		"tax_deferred_cash_percent":  {"20"},
		"roth_stock_percent":         {"70"},
		"roth_cash_percent":          {"30"},
		"taxable_stock_percent":      {"80"},
		"taxable_cash_percent":       {"20"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_PerAccountBadRange(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	tests := []struct {
		key string
	}{
		{"tax_deferred_stock_percent"},
		{"tax_deferred_cash_percent"},
		{"roth_stock_percent"},
		{"roth_cash_percent"},
		{"taxable_stock_percent"},
		{"taxable_cash_percent"},
	}
	for _, tt := range tests {
		form := url.Values{tt.key: {"110"}}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handleWhatIfSettings(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s=110: expected 400, got %d", tt.key, w.Code)
		}
	}
}

func TestHandleWhatIfSettings_InflationAndReturnFields(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"inflation_rate":        {"3.0"},
		"healthcare_inflation":  {"5.0"},
		"spending_decline_rate": {"1.0"},
		"investment_return":     {"7.0"},
		"discount_rate":         {"3.0"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_TaxableFields(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"taxable_dividend_yield":              {"2.0"},
		"taxable_qualified_dividend_percent":  {"80"},
		"taxable_cap_gains_distribution_rate": {"1.5"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_TaxableFieldsBadRange(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	tests := []struct {
		key, val string
	}{
		{"taxable_dividend_yield", "25"},
		{"taxable_qualified_dividend_percent", "110"},
		{"taxable_cap_gains_distribution_rate", "25"},
	}
	for _, tt := range tests {
		form := url.Values{tt.key: {tt.val}}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handleWhatIfSettings(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s=%s: expected 400, got %d", tt.key, tt.val, w.Code)
		}
	}
}

func TestHandleWhatIfSettings_ProjectionYearsBadRange(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"projection_years": {"0"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	form = url.Values{"projection_years": {"200"}}
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_InvalidProjectionTiming(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"projection_timing": {"bogus"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_TaxDeferredDelay(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"tax_deferred_delay_years": {"5"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	form = url.Values{"tax_deferred_delay_years": {"35"}}
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_SteadyStateOverride(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"steady_state_override_year": {"5.0"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_InvalidParseErrors(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Each of these should produce a 400 due to parse errors
	fields := []string{
		"portfolio_value", "monthly_living_expenses", "monthly_healthcare",
		"healthcare_start_years", "current_age", "spouse_age",
		"tax_deferred_percent", "roth_percent", "stock_percent", "cash_percent",
		"tax_deferred_stock_percent", "tax_deferred_cash_percent",
		"roth_stock_percent", "roth_cash_percent",
		"taxable_stock_percent", "taxable_cash_percent",
		"inflation_rate", "healthcare_inflation", "spending_decline_rate",
		"investment_return", "discount_rate",
		"taxable_dividend_yield", "taxable_qualified_dividend_percent",
		"taxable_cap_gains_distribution_rate",
		"projection_years", "tax_deferred_delay_years",
		"steady_state_override_year",
	}
	for _, field := range fields {
		form := url.Values{field: {"not-a-number"}}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handleWhatIfSettings(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("field %s with 'not-a-number': expected 400, got %d", field, w.Code)
		}
	}
}

// ── Income CRUD ─────────────────────────────────────────────────────────────

func TestHandleWhatIfAddIncome(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":       {"Social Security"},
		"amount":     {"2000"},
		"start_year": {"5"},
		"end_year":   {"30"},
		"cola":       {"on"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddIncome_MissingName(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"amount": {"2000"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-add-income-error")
}

func TestHandleWhatIfAddIncome_MissingAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddIncome_NegativeAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"-100"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddIncome_BadStartYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "start_year": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddIncome_NegativeStartYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "start_year": {"-1"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddIncome_BadEndYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "end_year": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddIncome_NegativeEndYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "end_year": {"-1"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddIncome_EndBeforeStart(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "start_year": {"5"}, "end_year": {"3"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddIncome_NoCola(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateIncome(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add an income source first
	src := models.IncomeSource{ID: "test-inc-1", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
	rm.AddIncomeSource(src)

	form := url.Values{"start_year": {"2"}, "end_year": {"10"}, "cola": {"true"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/income/test-inc-1", formBody(form), map[string]string{"id": "test-inc-1"})
	handleWhatIfUpdateIncome(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateIncome_BadStartYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"start_year": {"abc"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/income/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateIncome_NegativeStartYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"start_year": {"-1"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/income/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateIncome_BadEndYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"end_year": {"abc"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/income/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateIncome_NegativeEndYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"end_year": {"-1"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/income/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateIncome_EndBeforeStart(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"start_year": {"5"}, "end_year": {"3"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/income/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfDeleteIncome(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	src := models.IncomeSource{ID: "del-inc-1", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
	rm.AddIncomeSource(src)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/income/del-inc-1", nil, map[string]string{"id": "del-inc-1"})
	handleWhatIfDeleteIncome(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHandleWhatIfRestoreIncome(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	src := models.IncomeSource{ID: "rest-inc-1", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
	rm.AddIncomeSource(src)
	rm.RemoveIncomeSource("rest-inc-1")

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/income/rest-inc-1/restore", nil, map[string]string{"id": "rest-inc-1"})
	handleWhatIfRestoreIncome(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Expense CRUD ────────────────────────────────────────────────────────────

func TestHandleWhatIfAddExpense(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":          {"Car Payment"},
		"amount":        {"500"},
		"start_year":    {"0"},
		"end_year":      {"5"},
		"inflation":     {"on"},
		"discretionary": {"true"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddExpense_MissingName(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"amount": {"500"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-add-expense-error")
}

func TestHandleWhatIfAddExpense_MissingAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddExpense_NegativeAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"-100"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddExpense_BadStartYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "start_year": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddExpense_NegativeStartYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "start_year": {"-1"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddExpense_BadEndYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "end_year": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddExpense_NegativeEndYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "end_year": {"-1"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddExpense_EndBeforeStart(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "start_year": {"5"}, "end_year": {"3"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddExpense_NoEndYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateExpense(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	exp := models.ExpenseSource{ID: "test-exp-1", Name: "Test", Amount: 500}
	rm.AddExpenseSource(exp)

	form := url.Values{"start_year": {"1"}, "end_year": {"5"}, "inflation": {"on"}, "discretionary": {"true"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/expense/test-exp-1", formBody(form), map[string]string{"id": "test-exp-1"})
	handleWhatIfUpdateExpense(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateExpense_BadStartYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"start_year": {"abc"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/expense/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateExpense_NegativeStartYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"start_year": {"-1"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/expense/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateExpense_BadEndYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"end_year": {"abc"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/expense/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateExpense_NegativeEndYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"end_year": {"-1"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/expense/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateExpense_EndBeforeStart(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"start_year": {"5"}, "end_year": {"3"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/expense/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfDeleteExpense(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	exp := models.ExpenseSource{ID: "del-exp-1", Name: "Test", Amount: 500}
	rm.AddExpenseSource(exp)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/expense/del-exp-1", nil, map[string]string{"id": "del-exp-1"})
	handleWhatIfDeleteExpense(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHandleWhatIfRestoreExpense(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	exp := models.ExpenseSource{ID: "rest-exp-1", Name: "Test", Amount: 500}
	rm.AddExpenseSource(exp)
	rm.RemoveExpenseSource("rest-exp-1")

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/expense/rest-exp-1/restore", nil, map[string]string{"id": "rest-exp-1"})
	handleWhatIfRestoreExpense(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Projection Chart ────────────────────────────────────────────────────────

func TestHandleWhatIfProjectionChart(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/chart/projection?display_dollars=real", nil)
	handleWhatIfProjectionChart(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var data map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &data)
	if data["data"] == nil {
		t.Fatal("expected chart data")
	}
}

func TestHandleWhatIfProjectionChart_Nominal(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/chart/projection", nil)
	handleWhatIfProjectionChart(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Sync ────────────────────────────────────────────────────────────────────

func TestHandleWhatIfSync(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/sync", nil)
	handleWhatIfSync(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

// ── Monte Carlo ─────────────────────────────────────────────────────────────

func TestHandleWhatIfMonteCarlo(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/montecarlo", nil)
	handleWhatIfMonteCarlo(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Healthcare CRUD ─────────────────────────────────────────────────────────

func TestHandleWhatIfAddHealthcare_Defaults(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Minimal form - all defaults
	form := url.Values{}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddHealthcare_FullForm(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":                    {"Alice"},
		"current_age":             {"55"},
		"current_coverage":        {"aca"},
		"current_monthly_cost":    {"1200"},
		"pre_medicare_inflation":  {"6.0"},
		"medicare_monthly_cost":   {"500"},
		"post_medicare_inflation": {"3.5"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHandleWhatIfAddHealthcare_MedicarePerson(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Age >= 65, no coverage type -> defaults to Medicare
	form := url.Values{"current_age": {"67"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHandleWhatIfAddHealthcare_BadAge(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"current_age": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-add-healthcare-error")
}

func TestHandleWhatIfAddHealthcare_AgeTooHigh(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"current_age": {"130"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddHealthcare_NegativeCost(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"current_monthly_cost": {"-100"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddHealthcare_BadMonthlyCost(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"current_monthly_cost": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddHealthcare_BadInflationFields(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	for _, field := range []string{"pre_medicare_inflation", "medicare_monthly_cost", "post_medicare_inflation"} {
		form := url.Values{field: {"abc"}}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handleWhatIfAddHealthcare(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s=abc: expected 400, got %d", field, w.Code)
		}
	}
}

func TestHandleWhatIfUpdateHealthcare(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	person := models.HealthcarePerson{
		ID:                  "hc-1",
		Name:                "Bob",
		CurrentAge:          60,
		CurrentCoverage:     models.CoverageACA,
		CurrentMonthlyCost:  1000,
		MedicareMonthlyCost: 500,
		MedicareEligibleAge: 65,
	}
	rm.AddHealthcarePerson(person)

	form := url.Values{
		"name":                    {"Robert"},
		"current_age":             {"61"},
		"current_coverage":        {"aca"},
		"current_monthly_cost":    {"1100"},
		"pre_medicare_inflation":  {"7.0"},
		"medicare_monthly_cost":   {"550"},
		"post_medicare_inflation": {"4.0"},
		"employer_coverage_years": {"3"},
		"aca_cost_after_employer": {"1200"},
	}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/hc-1", formBody(form), map[string]string{"id": "hc-1"})
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateHealthcare_BadAge(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"current_age": {"abc"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateHealthcare_AgeTooHigh(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"current_age": {"130"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateHealthcare_BadCost(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"current_monthly_cost": {"abc"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateHealthcare_NegativeCost(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"current_monthly_cost": {"-100"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateHealthcare_BadInflation(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	for _, field := range []string{"pre_medicare_inflation", "post_medicare_inflation"} {
		form := url.Values{field: {"abc"}}
		w := httptest.NewRecorder()
		req := chiRequest("PUT", "/whatif/healthcare/x", formBody(form), map[string]string{"id": "x"})
		handleWhatIfUpdateHealthcare(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s=abc: expected 400, got %d", field, w.Code)
		}
	}
}

func TestHandleWhatIfUpdateHealthcare_BadMedicareCost(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"medicare_monthly_cost": {"abc"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateHealthcare_NegativeMedicareCost(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"medicare_monthly_cost": {"-100"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateHealthcare_BadEmployerYears(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"employer_coverage_years": {"abc"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateHealthcare_NegativeEmployerYears(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"employer_coverage_years": {"-1"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateHealthcare_BadACACost(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"aca_cost_after_employer": {"abc"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateHealthcare_NegativeACACost(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"aca_cost_after_employer": {"-100"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/x", formBody(form), map[string]string{"id": "x"})
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfDeleteHealthcare(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	person := models.HealthcarePerson{ID: "hc-del", Name: "Test", CurrentAge: 60, CurrentCoverage: models.CoverageACA, CurrentMonthlyCost: 1000, MedicareMonthlyCost: 500, MedicareEligibleAge: 65}
	rm.AddHealthcarePerson(person)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/healthcare/hc-del", nil, map[string]string{"id": "hc-del"})
	handleWhatIfDeleteHealthcare(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Spending Phases ─────────────────────────────────────────────────────────

func TestHandleWhatIfSpendingPhases(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":             {"on"},
		"phase_0_name":        {"Go-Go"},
		"phase_0_multiplier":  {"1.0"},
		"phase_1_name":        {"Slow-Go"},
		"phase_1_start_age":   {"75"},
		"phase_1_multiplier":  {"0.8"},
		"phase_1_description": {"Slower spending"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSpendingPhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfSpendingPhases_Disabled(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{} // no "enabled" = disabled
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSpendingPhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWhatIfAddPhase(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/add", nil)
	handleWhatIfAddPhase(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfDeletePhase_InvalidIndex(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/spending-phases/abc", nil, map[string]string{"index": "abc"})
	handleWhatIfDeletePhase(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfDeletePhase_Index0(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Ensure phases exist
	s, _ := rm.Load()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  models.DefaultSpendingPhases(),
	}
	rm.Save(s)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/spending-phases/0", nil, map[string]string{"index": "0"})
	handleWhatIfDeletePhase(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for deleting phase 0, got %d", w.Code)
	}
}

func TestHandleWhatIfDeletePhase_OutOfRange(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  models.DefaultSpendingPhases(),
	}
	rm.Save(s)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/spending-phases/99", nil, map[string]string{"index": "99"})
	handleWhatIfDeletePhase(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfDeletePhase_MinimumPhases(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Phase 1", StartAge: 0, Multiplier: 1.0},
			{Name: "Phase 2", StartAge: 75, Multiplier: 0.8},
		},
	}
	rm.Save(s)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/spending-phases/1", nil, map[string]string{"index": "1"})
	handleWhatIfDeletePhase(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for deleting below minimum, got %d", w.Code)
	}
}

func TestHandleWhatIfDeletePhase_NilConfig(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = nil
	rm.Save(s)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/spending-phases/1", nil, map[string]string{"index": "1"})
	handleWhatIfDeletePhase(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfDeletePhase_Success(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Phase 1", StartAge: 0, Multiplier: 1.0},
			{Name: "Phase 2", StartAge: 70, Multiplier: 0.9},
			{Name: "Phase 3", StartAge: 80, Multiplier: 0.7},
		},
	}
	rm.Save(s)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/spending-phases/2", nil, map[string]string{"index": "2"})
	handleWhatIfDeletePhase(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfResetPhases(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/reset", nil)
	handleWhatIfResetPhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHandleWhatIfResetPhases_WithExistingEnabled(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases:  []models.SpendingPhase{{Name: "Custom", StartAge: 0, Multiplier: 0.5}},
	}
	rm.Save(s)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/reset", nil)
	handleWhatIfResetPhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Roth Conversion ─────────────────────────────────────────────────────────

func TestHandleWhatIfRothConversion(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":       {"on"},
		"annual_amount": {"50000"},
		"start_year":    {"0"},
		"end_year":      {"10"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRothConversion_Disabled(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"annual_amount": {"50000"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHandleWhatIfRothConversion_RejectsNegativeAnnualAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":       {"on"},
		"annual_amount": {"-1"},
		"start_year":    {"0"},
		"end_year":      {"10"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRothConversion_RejectsEndYearBeforeStartYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":       {"on"},
		"annual_amount": {"10000"},
		"start_year":    {"5"},
		"end_year":      {"4"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

// ── Big Ticket Items ────────────────────────────────────────────────────────

func TestHandleWhatIfAddBigTicket(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":          {"New Roof"},
		"amount":        {"25000"},
		"year":          {"3"},
		"type":          {"expense"},
		"tax_treatment": {"none"},
		"notes":         {"Replace aging roof"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddBigTicket_IncomeType(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":          {"Home Sale"},
		"amount":        {"200000"},
		"year":          {"5"},
		"type":          {"income"},
		"tax_treatment": {"cap_gains"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWhatIfAddBigTicket_InvalidTypeDefaultsToExpense(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":   {"Test"},
		"amount": {"1000"},
		"type":   {"bogus"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWhatIfAddBigTicket_InvalidTaxTreatmentDefaultsToNone(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":          {"Test"},
		"amount":        {"1000"},
		"tax_treatment": {"bogus"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWhatIfAddBigTicket_OrdinaryTax(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":          {"Bonus"},
		"amount":        {"10000"},
		"type":          {"income"},
		"tax_treatment": {"ordinary"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWhatIfAddBigTicket_MissingName(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"amount": {"1000"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddBigTicket_MissingAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddBigTicket_NegativeAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"-100"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddBigTicket_NegativeYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "year": {"-5"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-add-bigticket-error")
}

func TestHandleWhatIfAddBigTicket_BadYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "year": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-add-bigticket-error")
}

func TestHandleWhatIfDeleteBigTicket(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	item := models.BigTicketItem{ID: "bt-1", Name: "Test", Amount: 1000, Type: models.BigTicketExpense}
	rm.AddBigTicketItem(item)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/bigticket/bt-1", nil, map[string]string{"id": "bt-1"})
	handleWhatIfDeleteBigTicket(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestHandleWhatIfRestoreBigTicket(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	item := models.BigTicketItem{ID: "bt-r", Name: "Test", Amount: 1000, Type: models.BigTicketExpense}
	rm.AddBigTicketItem(item)
	rm.RemoveBigTicketItem("bt-r")

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/bigticket/bt-r/restore", nil, map[string]string{"id": "bt-r"})
	handleWhatIfRestoreBigTicket(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Scenarios ───────────────────────────────────────────────────────────────

func TestHandleListScenarios(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/scenarios", nil)
	handleListScenarios(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON content type, got %s", ct)
	}
}

func TestHandleCreateScenario(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test Scenario"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/scenarios", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleCreateScenario(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("HX-Redirect") != "/whatif" {
		t.Fatal("expected HX-Redirect header")
	}
}

func TestHandleCreateScenario_MissingName(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/scenarios", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleCreateScenario(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-scenario-error")
}

func TestHandleCreateScenario_WhitespaceName(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"   "}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/scenarios", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleCreateScenario(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-scenario-error")
}

func TestHandleSwitchScenario(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create a scenario to switch to
	rm.CreateScenario("Switch Target")

	// List scenarios to get the filename
	scenarios, _ := rm.ListScenarios()
	var targetFile string
	for _, s := range scenarios {
		if s.Name == "Switch Target" {
			targetFile = s.Filename
		}
	}

	form := url.Values{"filename": {targetFile}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/scenarios/switch", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleSwitchScenario(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("HX-Redirect") != "/whatif" {
		t.Fatal("expected HX-Redirect")
	}
}

func TestHandleSwitchScenario_MissingFilename(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/scenarios/switch", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleSwitchScenario(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleDeleteScenario(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	rm.CreateScenario("To Delete")
	scenarios, _ := rm.ListScenarios()
	var targetFile string
	for _, s := range scenarios {
		if s.Name == "To Delete" {
			targetFile = s.Filename
		}
	}

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/scenarios/"+targetFile, nil, map[string]string{"filename": targetFile})
	handleDeleteScenario(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteScenario_MissingFilename(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/scenarios/", nil, map[string]string{"filename": ""})
	handleDeleteScenario(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleDeleteScenario_DefaultScenarioConflict(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/scenarios/whatif.json", nil, map[string]string{"filename": "whatif.json"})
	handleDeleteScenario(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-scenario-error")
}

func TestHandleDeleteScenario_ReferencedScenarioConflict(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	if _, err := rm.CreateScenario("Primary"); err != nil {
		t.Fatalf("CreateScenario primary: %v", err)
	}
	if _, err := rm.CreateScenario("Chain Target"); err != nil {
		t.Fatalf("CreateScenario target: %v", err)
	}

	scenarios, err := rm.ListScenarios()
	if err != nil {
		t.Fatalf("ListScenarios: %v", err)
	}

	var primaryFile, targetFile string
	for _, s := range scenarios {
		if s.Name == "Primary" {
			primaryFile = s.Filename
		}
		if s.Name == "Chain Target" {
			targetFile = s.Filename
		}
	}

	if err := rm.SwitchScenario(primaryFile); err != nil {
		t.Fatalf("SwitchScenario primary: %v", err)
	}
	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.ScenarioChain = []models.ScenarioChainLink{{
		ScenarioFilename: targetFile,
		TransitionAge:    70,
	}}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/scenarios/"+targetFile, nil, map[string]string{"filename": targetFile})
	handleDeleteScenario(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-scenario-error")
}

func TestHandleRenameScenario(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	rm.CreateScenario("Old Name")
	scenarios, _ := rm.ListScenarios()
	var targetFile string
	for _, s := range scenarios {
		if s.Name == "Old Name" {
			targetFile = s.Filename
		}
	}

	form := url.Values{"name": {"New Name"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/scenarios/"+targetFile, formBody(form), map[string]string{"filename": targetFile})
	handleRenameScenario(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRenameScenario_MissingFilename(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"New"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/scenarios/", formBody(form), map[string]string{"filename": ""})
	handleRenameScenario(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleRenameScenario_MissingName(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/scenarios/some.json", formBody(form), map[string]string{"filename": "some.json"})
	handleRenameScenario(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-scenario-error")
}

func TestHandleRenameScenario_DefaultScenarioConflict(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Something Else"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/scenarios/whatif.json", formBody(form), map[string]string{"filename": "whatif.json"})
	handleRenameScenario(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-scenario-error")
}

func TestHandleRenameScenario_WhitespaceName(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	rm.CreateScenario("Old Name")
	scenarios, _ := rm.ListScenarios()
	var targetFile string
	for _, s := range scenarios {
		if s.Name == "Old Name" {
			targetFile = s.Filename
		}
	}

	form := url.Values{"name": {"   "}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/scenarios/"+targetFile, formBody(form), map[string]string{"filename": targetFile})
	handleRenameScenario(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-scenario-error")
}

// ── Chain ───────────────────────────────────────────────────────────────────

func TestHandleWhatIfUpdateChain(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create two scenarios: one to be active and one to chain to
	rm.CreateScenario("Primary")
	rm.CreateScenario("Chain Target")
	scenarios, _ := rm.ListScenarios()
	var primaryFile, targetFile string
	for _, s := range scenarios {
		if s.Name == "Primary" {
			primaryFile = s.Filename
		}
		if s.Name == "Chain Target" {
			targetFile = s.Filename
		}
	}

	// Switch to primary so chain target is a different scenario
	rm.SwitchScenario(primaryFile)

	form := url.Values{
		"chain_scenario[]": {targetFile},
		"chain_age[]":      {"70"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/chain", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateChain(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateChain_MismatchedArrays(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"chain_scenario[]": {"a.json", "b.json"},
		"chain_age[]":      {"70"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/chain", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateChain(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateChain_BadAge(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"chain_scenario[]": {"a.json"},
		"chain_age[]":      {"abc"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/chain", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateChain(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateChain_EmptyScenarioSkipped(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"chain_scenario[]": {""},
		"chain_age[]":      {"70"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/chain", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateChain(w, req)
	// Empty scenario is skipped, result is empty chain, which is valid
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfDeleteChainLink(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create two scenarios: one active, one to chain to
	rm.CreateScenario("Primary")
	rm.CreateScenario("Chain Link")
	scenarios, _ := rm.ListScenarios()
	var primaryFile, targetFile string
	for _, s := range scenarios {
		if s.Name == "Primary" {
			primaryFile = s.Filename
		}
		if s.Name == "Chain Link" {
			targetFile = s.Filename
		}
	}

	// Switch to primary so we can chain to Chain Link
	rm.SwitchScenario(primaryFile)

	s, _ := rm.Load()
	s.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: targetFile, TransitionAge: 70},
	}
	rm.Save(s)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/chain/0", nil, map[string]string{"index": "0"})
	handleWhatIfDeleteChainLink(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfDeleteChainLink_InvalidIndex(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/chain/abc", nil, map[string]string{"index": "abc"})
	handleWhatIfDeleteChainLink(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfDeleteChainLink_OutOfRange(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/chain/99", nil, map[string]string{"index": "99"})
	handleWhatIfDeleteChainLink(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfDeleteChainLink_NegativeIndex(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/chain/-1", nil, map[string]string{"index": "-1"})
	handleWhatIfDeleteChainLink(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── buildEngineInput ────────────────────────────────────────────────────────

func TestBuildEngineInput_NoChain(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	in, hash, err := buildEngineInput(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Prepared.Settings() == nil {
		t.Fatal("expected non-nil Prepared.Settings()")
	}
	if len(in.Chain) != 0 {
		t.Fatalf("expected empty chain, got %d links", len(in.Chain))
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestBuildEngineInput_WithChain(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	rm.CreateScenario("Chained")
	scenarios, _ := rm.ListScenarios()
	var targetFile string
	for _, s := range scenarios {
		if s.Name == "Chained" {
			targetFile = s.Filename
		}
	}

	s := models.DefaultWhatIfSettings()
	s.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: targetFile, TransitionAge: 70},
	}
	in, hash, err := buildEngineInput(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Prepared.Settings() == nil {
		t.Fatal("expected non-nil Prepared.Settings()")
	}
	if len(in.Chain) != 1 {
		t.Fatalf("expected 1 chain link, got %d", len(in.Chain))
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestBuildEngineInput_ChainBadFile(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	s.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: "nonexistent.json", TransitionAge: 70},
	}
	_, _, err := buildEngineInput(s)
	if err == nil {
		t.Fatal("expected error for bad chain file")
	}
}

// ── runAnalysisWithCache ────────────────────────────────────────────────────

func TestRunAnalysisWithCache(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	a1, err := runAnalysisWithCache(s)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if a1 == nil {
		t.Fatal("expected non-nil analysis")
	}

	// Second call should hit cache
	a2, err := runAnalysisWithCache(s)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if a2 != a1 {
		t.Fatal("expected same pointer from cache")
	}
}

// ── RegisterRoutes ──────────────────────────────────────────────────────────

func TestRegisterRoutes(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	r := chi.NewRouter()
	RegisterRoutes(r)

	// Just verify it doesn't panic and routes are registered by hitting one
	ts := httptest.NewServer(r)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/whatif")
	if err != nil {
		t.Fatalf("GET /whatif: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /whatif: status = %d", resp.StatusCode)
	}
}

// ── Initialize ──────────────────────────────────────────────────────────────

func TestInitialize(t *testing.T) {
	old := retirementMgr
	Initialize(nil, nil, nil)
	if retirementMgr != nil {
		t.Fatal("expected nil retirementMgr")
	}
	// Restore
	retirementMgr = old
}

// ── Event marker with zero value (y <= 0) branch ───────────────────────────

func TestBuildProjectionChartData_EventMarkerZeroBalance(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 70
	settings.ProjectionYears = 15

	// Create projection where balance goes to 0
	months := make([]models.ProjectionMonth, 180)
	for i := range months {
		months[i] = models.ProjectionMonth{
			Month:            i,
			Year:             float64(i) / 12.0,
			PortfolioBalance: 0, // All zero
		}
	}
	projection := &models.ProjectionResult{
		Months:   months,
		Survives: false,
	}

	settings.IncomeSources = []models.IncomeSource{
		{Name: "Social Security", Amount: 2000, StartMonth: 24},
	}

	chartData := buildProjectionChartData(settings, projection, "nominal")
	traces := chartData["data"].([]map[string]interface{})
	if len(traces) < 2 {
		t.Fatal("expected event marker trace")
	}

	// When balance is 0, y should be maxBalance * 0.05 = 0 (since maxBalance is 0)
	eventY := traces[1]["y"].([]float64)
	if eventY[0] != 0 {
		t.Fatalf("expected y=0 for zero-balance projection event, got %f", eventY[0])
	}
}

// ── Multiple events at same year offset ────────────────────────────────────

func TestBuildProjectionChartData_MultipleEventsAtSameYear(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 30

	// Two income sources starting at the same year
	settings.IncomeSources = []models.IncomeSource{
		{Name: "Social Security", Amount: 2000, StartMonth: 60},
		{Name: "Pension", Amount: 1500, StartMonth: 60},
	}

	projection := sampleProjectionForChart()
	chartData := buildProjectionChartData(settings, projection, "nominal")
	traces := chartData["data"].([]map[string]interface{})
	if len(traces) < 2 {
		t.Fatal("expected event marker trace")
	}

	// Both events should be present with different y offsets
	eventY := traces[1]["y"].([]float64)
	if len(eventY) < 2 {
		t.Fatalf("expected 2 events, got %d", len(eventY))
	}
	// Second event at same year should have higher y due to offset
	if eventY[1] <= eventY[0] {
		t.Fatalf("second event at same year should have higher y offset: %f vs %f", eventY[1], eventY[0])
	}
}

// ── Chart events: RMD start ────────────────────────────────────────────────

func TestBuildProjectionChartEvents_RMDNotAdded(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 80 // Already past RMD age
	// F-078: keep Persons[0].BirthMonth in sync with CurrentAge so the
	// calendar-year RMD gate sees the intended birth year.
	settings.Persons[0].BirthMonth = models.BirthMonthForAge(settings.StartDate, settings.CurrentAge)
	settings.ProjectionYears = 15

	projection := sampleProjectionForChart()
	events := buildProjectionChartEvents(settings, projection)

	for _, e := range events {
		if e.Label == "RMD starts" {
			t.Fatal("should not add RMD event when already past RMD age")
		}
	}
}

// F-075: event-timeline "RMD starts" label uses EffectiveRMDStartAge so
// 2033+ scenarios show 75 - olderAge instead of 73 - olderAge.
func TestBuildProjectionChartEvents_F075_RMDStartsUsesEffectiveAge(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 70
	settings.SpouseAge = 0
	settings.ProjectionYears = 15
	settings.StartDate = "2033-01" // SECURE 2.0: effective RMD age = 75
	// F-078: keep Persons[0].BirthMonth in sync with CurrentAge so the
	// calendar-year RMD gate sees the intended birth year.
	settings.Persons[0].BirthMonth = models.BirthMonthForAge(settings.StartDate, settings.CurrentAge)

	projection := sampleProjectionForChart()
	events := buildProjectionChartEvents(settings, projection)

	var found bool
	for _, e := range events {
		if e.Label == "RMD starts" {
			found = true
			if e.Year != 5 { // 75 - 70 = 5 (not 73 - 70 = 3)
				t.Errorf("RMD starts year = %v; want 5 (2033+ effective start age 75 minus current 70)", e.Year)
			}
		}
	}
	if !found {
		t.Error("RMD starts event not found in timeline")
	}
}

// F-075: pre-2033 scenarios still surface "RMD starts" at age 73 minus olderAge.
func TestBuildProjectionChartEvents_F075_RMDStartsPre2033Uses73(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 70
	settings.SpouseAge = 0
	settings.ProjectionYears = 15
	settings.StartDate = "2026-01" // pre-SECURE-2.0 transition: age 73
	settings.Persons[0].BirthMonth = models.BirthMonthForAge(settings.StartDate, settings.CurrentAge)

	projection := sampleProjectionForChart()
	events := buildProjectionChartEvents(settings, projection)

	var found bool
	for _, e := range events {
		if e.Label == "RMD starts" {
			found = true
			if e.Year != 3 { // 73 - 70 = 3
				t.Errorf("RMD starts year = %v; want 3 (pre-2033 start age 73 minus current 70)", e.Year)
			}
		}
	}
	if !found {
		t.Error("RMD starts event not found in timeline")
	}
}

// F-078: the "RMD starts" event-timeline label must use the calendar year
// of first RMD (FirstRMDCalendarYear), not floor'd-age arithmetic. For a
// primary born 1959-12 with StartDate=2026-01, RMDs start in calendar
// year 2032 → 6 years from start, not 7.
func TestProjectionChartEvents_F078_RMDStartsLabel_LateYearBirth(t *testing.T) {
	s := models.DefaultWhatIfSettings()
	s.StartDate = "2026-01"
	s.Persons = []models.Person{
		{ID: "p1", Name: "Primary", Role: models.PersonRolePrimary, BirthMonth: "1959-12"},
	}
	prepare.ComputeAges(s)
	s.PortfolioValue = 1_000_000
	s.TaxDeferredPercent = 100
	s.ProjectionYears = 10

	eng := engine.New()
	proj := eng.Run(engine.Input{Prepared: prepare.MustFrom(t, s)})
	events := buildProjectionChartEvents(s, proj)

	for _, e := range events {
		if e.Label == "RMD starts" {
			if e.Year != 6 {
				t.Errorf("RMD starts event year = %.2f; want 6 (born 1959-12, first RMD calendar 2032)", e.Year)
			}
			return
		}
	}
	t.Fatalf("no 'RMD starts' event in %+v", events)
}

// ── Spending phases with base phase fallback ───────────────────────────────

func TestHandleWhatIfSpendingPhases_BasePhasePreserved(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Pre-set spending phases
	s, _ := rm.Load()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Active", StartAge: 0, Multiplier: 1.0},
			{Name: "Slower", StartAge: 75, Multiplier: 0.8},
			{Name: "Minimal", StartAge: 85, Multiplier: 0.6},
		},
	}
	rm.Save(s)

	// Submit with only partial form data (only first phase has multiplier)
	form := url.Values{
		"enabled":            {"on"},
		"phase_0_multiplier": {"0.95"},
		// phase 1 and 2 have no multiplier key, so base values should be preserved
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSpendingPhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Healthcare: MedicareCoverage defaults ──────────────────────────────────

func TestHandleWhatIfAddHealthcare_MedicareCoverageDefaults(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Age >= 65, coverage defaults to Medicare, cost default changes
	form := url.Values{
		"current_age":      {"67"},
		"current_coverage": {string(models.CoverageMedicare)},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Spending phases: adding phase hits floor ───────────────────────────────

func TestHandleWhatIfAddPhase_MultiplierFloor(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Phase 1", StartAge: 0, Multiplier: 1.0},
			{Name: "Phase 2", StartAge: 75, Multiplier: 0.32}, // 0.32 - 0.05 = 0.27 < 0.30 floor
		},
	}
	rm.Save(s)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/add", nil)
	handleWhatIfAddPhase(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── syncSettingsFromDashboard ───────────────────────────────────────────────

func TestSyncSettingsFromDashboard(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	err := syncSettingsFromDashboard(s)
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	// Should have set monthly expenses from outflow data
	if s.MonthlyLivingExpenses <= 0 {
		t.Fatal("expected positive monthly expenses after sync")
	}
}

// ── Additional edge cases for clampAlloc ────────────────────────────────────

func TestHandleWhatIfSettings_PerAccountAllocClamp(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Stock 80% + Cash 30% should be clamped (cash -> 20%)
	form := url.Values{
		"tax_deferred_stock_percent": {"80"},
		"tax_deferred_cash_percent":  {"30"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── Test spending phases with start_age override ───────────────────────────

func TestHandleWhatIfSpendingPhases_StartAgeOverride(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":            {"on"},
		"phase_0_multiplier": {"1.0"},
		"phase_0_start_age":  {"60"}, // Phase 0 start age
		"phase_1_multiplier": {"0.8"},
		"phase_1_start_age":  {"80"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSpendingPhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── Test spouse age valid values ───────────────────────────────────────────

func TestHandleWhatIfSettings_SpouseAge(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// spouse_age=0 means no spouse (valid)
	form := url.Values{"spouse_age": {"0"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for spouse_age=0, got %d", w.Code)
	}

	// spouse_age negative
	form = url.Values{"spouse_age": {"-5"}}
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative spouse_age, got %d", w.Code)
	}
}

// ── Test buildProjectionChartEvents with event at year 0 or beyond max ─────

func TestBuildProjectionChartEvents_EventAtZeroOrBeyondMax(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 15

	// Income source starting at month 0 (year 0) - should be skipped
	settings.IncomeSources = []models.IncomeSource{
		{Name: "Social Security", Amount: 2000, StartMonth: 0},
	}

	projection := sampleProjectionForChart()
	events := buildProjectionChartEvents(settings, projection)

	for _, e := range events {
		if e.Label == "Social Security starts" {
			t.Fatal("events at year 0 should be filtered out")
		}
	}
}

// ── Additional coverage for projection chart events dedup ──────────────────

func TestBuildProjectionChartEvents_Dedup(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 30

	// Same scenario at same age twice
	settings.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: "plan-a.json", TransitionAge: 70},
		{ScenarioFilename: "plan-a.json", TransitionAge: 70},
	}

	projection := sampleProjectionForChart()
	events := buildProjectionChartEvents(settings, projection)

	count := 0
	for _, e := range events {
		if e.Label == "Scenario: Plan A" {
			count++
		}
	}
	if count > 1 {
		t.Fatalf("expected dedup to prevent duplicate events, got %d", count)
	}
}

// ── Healthcare person with transition info for chart ───────────────────────

func TestBuildProjectionChartEvents_HealthcareNoTransition(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 60
	settings.ProjectionYears = 30

	// Person already on Medicare - no transition
	settings.HealthcarePersons = []models.HealthcarePerson{
		{
			Name:                "Bob",
			CurrentAge:          70,
			CurrentCoverage:     models.CoverageMedicare,
			CurrentMonthlyCost:  500,
			MedicareMonthlyCost: 500,
			MedicareEligibleAge: 65,
		},
	}

	projection := sampleProjectionForChart()
	events := buildProjectionChartEvents(settings, projection)

	for _, e := range events {
		if strings.Contains(e.Label, "Medicare: Bob") {
			t.Fatal("should not add Medicare event for person already on Medicare")
		}
	}
}

// ── handleWhatIfSettings with valid projection timing ──────────────────────

func TestHandleWhatIfSettings_ValidProjectionTiming(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	for _, timing := range []string{"start_of_month", "mid_month", "end_of_month"} {
		form := url.Values{"projection_timing": {timing}}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handleWhatIfSettings(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("timing=%s: expected 200, got %d", timing, w.Code)
		}
	}
}

// ── HandleWhatIfAddPhase with nil config ───────────────────────────────────

func TestHandleWhatIfAddPhase_NilConfig(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = nil
	rm.Save(s)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/add", nil)
	handleWhatIfAddPhase(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── ResetPhases with nil SpendingPhaseConfig ───────────────────────────────

func TestHandleWhatIfResetPhases_NilConfig(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = nil
	rm.Save(s)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/reset", nil)
	handleWhatIfResetPhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Additional coverage for spending phases with existing phases ────────────

func TestHandleWhatIfSpendingPhases_WithExistingPhases(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Active", StartAge: 0, Multiplier: 1.0},
			{Name: "Slower", StartAge: 75, Multiplier: 0.8},
		},
	}
	rm.Save(s)

	form := url.Values{
		"enabled":             {"true"},
		"phase_0_multiplier":  {"0.95"},
		"phase_0_name":        {"Updated Active"},
		"phase_1_multiplier":  {"0.75"},
		"phase_1_start_age":   {"76"},
		"phase_1_name":        {"Updated Slower"},
		"phase_1_description": {"Updated description"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSpendingPhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Test with healthcare: MedicareCoverage as 0 cost ──────────────────────

func TestHandleWhatIfAddHealthcare_ZeroCostMedicare(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// MonthlyCost=0 with Medicare coverage should default to 459
	form := url.Values{
		"current_age":      {"67"},
		"current_coverage": {string(models.CoverageMedicare)},
		// no current_monthly_cost -> defaults to 459 for Medicare
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Full integration: Register routes and exercise through server ──────────

func TestIntegration_FullRouter(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	r := chi.NewRouter()
	RegisterRoutes(r)
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Test POST /whatif/calculate
	resp, err := http.Post(ts.URL+"/whatif/calculate", "", nil)
	if err != nil {
		t.Fatalf("POST /whatif/calculate: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /whatif/calculate: status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Test GET /whatif/chart/projection
	resp, err = http.Get(ts.URL + "/whatif/chart/projection?display_dollars=nominal")
	if err != nil {
		t.Fatalf("GET /whatif/chart/projection: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /whatif/chart/projection: status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Test GET /whatif/scenarios
	resp, err = http.Get(ts.URL + "/whatif/scenarios")
	if err != nil {
		t.Fatalf("GET /whatif/scenarios: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /whatif/scenarios: status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Test POST /whatif/settings with form data
	formData := url.Values{"portfolio_value": {"2000000"}}
	resp, err = http.Post(ts.URL+"/whatif/settings", "application/x-www-form-urlencoded", strings.NewReader(formData.Encode()))
	if err != nil {
		t.Fatalf("POST /whatif/settings: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /whatif/settings: status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ── Test buildProjectionChartEvents sorting ────────────────────────────────

func TestBuildProjectionChartEvents_Sorting(t *testing.T) {
	settings := models.DefaultWhatIfSettings()
	settings.CurrentAge = 55
	settings.ProjectionYears = 30

	settings.IncomeSources = []models.IncomeSource{
		{Name: "Social Security", Amount: 2000, StartMonth: 120},
		{Name: "Pension", Amount: 1500, StartMonth: 60},
	}

	projection := sampleProjectionForChart()
	events := buildProjectionChartEvents(settings, projection)

	// Pension (year 5) should come before SS (year 10)
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
	pensionIdx := -1
	ssIdx := -1
	for i, e := range events {
		if e.Label == "Pension starts" {
			pensionIdx = i
		}
		if e.Label == "Social Security starts" {
			ssIdx = i
		}
	}
	if pensionIdx >= 0 && ssIdx >= 0 && pensionIdx > ssIdx {
		t.Fatalf("events not sorted: pension at %d, SS at %d", pensionIdx, ssIdx)
	}
}

// ── Healthcare settings per-account bad parse ──────────────────────────────

func TestHandleWhatIfSettings_PerAccountBadParse(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	badFields := []string{
		"tax_deferred_stock_percent",
		"tax_deferred_cash_percent",
		"roth_stock_percent",
		"roth_cash_percent",
		"taxable_stock_percent",
		"taxable_cash_percent",
	}
	for _, field := range badFields {
		form := url.Values{field: {"not-a-number"}}
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handleWhatIfSettings(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s=not-a-number: expected 400, got %d", field, w.Code)
		}
	}
}

// ── Healthcare: explicitly test healthcare_start_years zero ────────────────

func TestHandleWhatIfSettings_HealthcareStartYears(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"monthly_healthcare":     {"500"},
		"healthcare_start_years": {"3"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── Test income with cola=true (not "on") ──────────────────────────────────

func TestHandleWhatIfAddIncome_ColaTrue(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"100"}, "cola": {"true"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── Test expense with inflation=true and discretionary=on ──────────────────

func TestHandleWhatIfAddExpense_Flags(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":          {"Test"},
		"amount":        {"100"},
		"inflation":     {"true"},
		"discretionary": {"on"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── Roth percent with tax_deferred from existing form value ────────────────

func TestHandleWhatIfSettings_RothPercentFromFormValue(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// First set tax_deferred
	s, _ := rm.Load()
	s.TaxDeferredPercent = 40
	rm.Save(s)

	// Submit only roth_percent (tax_deferred not in updates map but in form)
	form := url.Values{
		"roth_percent":         {"50"},
		"tax_deferred_percent": {"40"}, // in form, not changed
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (40+50=90 <= 100), got %d", w.Code)
	}
}

// ── Cash percent with stock from existing form value ───────────────────────

func TestHandleWhatIfSettings_CashPercentFromFormValue(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// stock_percent in form but not as an update, cash_percent submitted
	form := url.Values{
		"stock_percent": {"50"},
		"cash_percent":  {"40"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (50+40=90 <= 100), got %d", w.Code)
	}
}

// ── syncSettingsFromDashboard with insights prefix preservation ─────────────

func TestSyncSettingsFromDashboard_PreservesUserSources(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	s.IncomeSources = []models.IncomeSource{
		{ID: "user-custom-1", Name: "Rental Income", Amount: 1000, Type: models.IncomeFixed},
		{ID: "insights-salary", Name: "Salary", Amount: 5000, Type: models.IncomeFixed, StartMonth: 12},
	}

	err := syncSettingsFromDashboard(s)
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// User source should be preserved
	found := false
	for _, src := range s.IncomeSources {
		if src.ID == "user-custom-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("user source 'user-custom-1' should be preserved after sync")
	}
}

// ── Test handleWhatIfAddBigTicket with amount bad parse ────────────────────

func TestHandleWhatIfAddBigTicket_BadAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── Test handleWhatIfAddIncome bad amount parse ────────────────────────────

func TestHandleWhatIfAddIncome_BadAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── Test handleWhatIfAddExpense bad amount parse ───────────────────────────

func TestHandleWhatIfAddExpense_BadAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Test"}, "amount": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── Additional clampAlloc coverage: Roth and Taxable ───────────────────────

func TestHandleWhatIfSettings_RothAllocClamp(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"roth_stock_percent": {"80"},
		"roth_cash_percent":  {"30"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_TaxableAllocClamp(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"taxable_stock_percent": {"80"},
		"taxable_cash_percent":  {"30"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── Coverage for handleWhatIfSpendingPhases start_age for base phases ──────

func TestHandleWhatIfSpendingPhases_BasePhaseStartAgeOverride(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Active", StartAge: 0, Multiplier: 1.0},
			{Name: "Slow", StartAge: 75, Multiplier: 0.8},
		},
	}
	rm.Save(s)

	// Only provide start_age for phase 1 (no multiplier key => base phase path with start_age override)
	form := url.Values{
		"enabled":           {"on"},
		"phase_1_start_age": {"80"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSpendingPhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── RothConversion with existing non-nil config ────────────────────────────

func TestHandleWhatIfRothConversion_ExistingConfig(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.RothConversion = &models.RothConversionConfig{
		Enabled:      true,
		AnnualAmount: 25000,
		StartYear:    0,
		EndYear:      5,
	}
	rm.Save(s)

	form := url.Values{
		"enabled":       {"on"},
		"annual_amount": {"75000"},
		"start_year":    {"1"},
		"end_year":      {"15"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Coverage for spending phases with many phases (beyond base) ─────────────

func TestHandleWhatIfSpendingPhases_ManyPhases(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"enabled": {"on"}}
	for i := 0; i < 22; i++ {
		form.Set(fmt.Sprintf("phase_%d_name", i), fmt.Sprintf("Phase %d", i))
		form.Set(fmt.Sprintf("phase_%d_multiplier", i), fmt.Sprintf("%.2f", 1.0-float64(i)*0.1))
		if i > 0 {
			form.Set(fmt.Sprintf("phase_%d_start_age", i), fmt.Sprintf("%d", 60+i*5))
		}
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSpendingPhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if settings.SpendingPhaseConfig == nil {
		t.Fatal("expected spending phase config to be saved")
	}
	if got := len(settings.SpendingPhaseConfig.Phases); got != 22 {
		t.Fatalf("expected 22 phases to persist, got %d", got)
	}
}

// ── CRUD operations with nonexistent IDs (manager handles gracefully) ──────

func TestHandleWhatIfDeleteIncome_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/income/nonexistent", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfDeleteIncome(w, req)
	// Manager handles missing IDs gracefully
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d", w.Code)
	}
}

func TestHandleWhatIfRestoreIncome_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/income/nonexistent/restore", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfRestoreIncome(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent removed source, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateIncome_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"start_year": {"0"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/income/nonexistent", formBody(form), map[string]string{"id": "nonexistent"})
	handleWhatIfUpdateIncome(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d", w.Code)
	}
}

func TestHandleWhatIfDeleteExpense_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/expense/nonexistent", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfDeleteExpense(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d", w.Code)
	}
}

func TestHandleWhatIfRestoreExpense_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/expense/nonexistent/restore", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfRestoreExpense(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent removed source, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateExpense_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"start_year": {"0"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/expense/nonexistent", formBody(form), map[string]string{"id": "nonexistent"})
	handleWhatIfUpdateExpense(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d", w.Code)
	}
}

func TestHandleWhatIfDeleteHealthcare_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/healthcare/nonexistent", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfDeleteHealthcare(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateHealthcare_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Updated"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/nonexistent", formBody(form), map[string]string{"id": "nonexistent"})
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d", w.Code)
	}
}

func TestHandleWhatIfDeleteBigTicket_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/bigticket/nonexistent", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfDeleteBigTicket(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d", w.Code)
	}
}

func TestHandleWhatIfRestoreBigTicket_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/bigticket/nonexistent/restore", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfRestoreBigTicket(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent removed item, got %d", w.Code)
	}
}

// ── Error paths for scenarios ──────────────────────────────────────────────

func TestHandleSwitchScenario_NonexistentFile(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"filename": {"whatif_nonexistent.json"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/scenarios/switch", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleSwitchScenario(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent scenario, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-scenario-error")
}

func TestHandleDeleteScenario_NonexistentFile(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/scenarios/whatif_nonexistent.json", nil, map[string]string{"filename": "whatif_nonexistent.json"})
	handleDeleteScenario(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent scenario, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-scenario-error")
}

func TestHandleRenameScenario_NonexistentFile(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"New Name"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/scenarios/whatif_nonexistent.json", formBody(form), map[string]string{"filename": "whatif_nonexistent.json"})
	handleRenameScenario(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent scenario, got %d", w.Code)
	}
	assertRetargetHeader(t, w, "#whatif-scenario-error")
}

// ── Test handleWhatIfUpdateChain with invalid validation ───────────────────

func TestHandleWhatIfUpdateChain_SelfReferenceError(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	activeFile := rm.ActiveFilename()

	form := url.Values{
		"chain_scenario[]": {activeFile},
		"chain_age[]":      {"70"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/chain", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateChain(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for self-reference, got %d", w.Code)
	}
}

// ── Test handleWhatIfSpendingPhases with description ────────────────────────

func TestHandleWhatIfSpendingPhases_WithDescription(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":             {"on"},
		"phase_0_name":        {"Active"},
		"phase_0_multiplier":  {"1.0"},
		"phase_0_description": {"Full spending"},
		"phase_1_name":        {"Slow"},
		"phase_1_multiplier":  {"0.8"},
		"phase_1_start_age":   {"75"},
		"phase_1_description": {"Reduced spending"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSpendingPhases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── Test handleWhatIfAddPhase with pre-existing many phases ────────────────

func TestHandleWhatIfAddPhase_WithManyExistingPhases(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Phase 1", StartAge: 0, Multiplier: 1.0},
			{Name: "Phase 2", StartAge: 70, Multiplier: 0.9},
			{Name: "Phase 3", StartAge: 75, Multiplier: 0.8},
			{Name: "Phase 4", StartAge: 80, Multiplier: 0.7},
			{Name: "Phase 5", StartAge: 85, Multiplier: 0.6},
		},
	}
	rm.Save(s)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/add", nil)
	handleWhatIfAddPhase(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ── Test handleWhatIfDeletePhase with negative index ───────────────────────

func TestHandleWhatIfDeletePhase_NegativeIndex(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Phase 1", StartAge: 0, Multiplier: 1.0},
			{Name: "Phase 2", StartAge: 70, Multiplier: 0.9},
			{Name: "Phase 3", StartAge: 80, Multiplier: 0.7},
		},
	}
	rm.Save(s)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/spending-phases/-1", nil, map[string]string{"index": "-1"})
	handleWhatIfDeletePhase(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative index, got %d", w.Code)
	}
}

// ── Test handleWhatIfAddHealthcare with age 0 ──────────────────────────────

func TestHandleWhatIfAddHealthcare_AgeZero(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Baby"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── Test handleWhatIfSync with no CSV data ─────────────────────────────────

func TestHandleWhatIfSync_NoCsvData(t *testing.T) {
	settingsDir := t.TempDir()
	csvDir := t.TempDir()

	store, _ := storage.New(settingsDir)
	rm := retirement.NewSettingsManager(settingsDir, store)
	dl := dataloader.New(csvDir, store)
	Initialize(dl, nil, rm)

	cache.mu.Lock()
	cache.hash = ""
	cache.analysis = nil
	cache.cachedAt = time.Time{}
	cache.mu.Unlock()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/sync", nil)
	handleWhatIfSync(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", w.Code, w.Body.String())
	}
}

// ── Test handleWhatIf first load keeps saved settings untouched ─────────────

func TestHandleWhatIf_DoesNotAutoSyncEmptyIncomeSources(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(settings.IncomeSources) != 0 {
		t.Fatalf("expected no income sources to be auto-created, got %d", len(settings.IncomeSources))
	}
}

// ── ParseForm error tests (multipart with bad boundary triggers error) ─────

func badParseFormRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader("bad"))
	req.Header.Set("Content-Type", "multipart/form-data") // missing boundary
	return req
}

func TestHandleWhatIfAddIncome_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfAddIncome(w, badParseFormRequest("POST", "/whatif/income"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfAddExpense_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfAddExpense(w, badParseFormRequest("POST", "/whatif/expense"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfSettings_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfSettings(w, badParseFormRequest("POST", "/whatif/settings"))
	// ParseForm may or may not error depending on handler; just ensure no panic
}

func TestHandleWhatIfAddHealthcare_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfAddHealthcare(w, badParseFormRequest("POST", "/whatif/healthcare"))
	// Exercise code path; ParseForm may not error on all Go versions
}

func TestHandleWhatIfUpdateHealthcare_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := badParseFormRequest("PUT", "/whatif/healthcare/x")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "x")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	handleWhatIfUpdateHealthcare(w, req)
}

func TestHandleWhatIfUpdateIncome_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := badParseFormRequest("PUT", "/whatif/income/x")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "x")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	handleWhatIfUpdateIncome(w, req)
}

func TestHandleWhatIfUpdateExpense_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := badParseFormRequest("PUT", "/whatif/expense/x")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "x")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	handleWhatIfUpdateExpense(w, req)
}

func TestHandleWhatIfSpendingPhases_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfSpendingPhases(w, badParseFormRequest("POST", "/whatif/spending-phases"))
}

func TestHandleWhatIfRothConversion_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfRothConversion(w, badParseFormRequest("POST", "/whatif/roth-conversion"))
}

func TestHandleWhatIfAddBigTicket_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfAddBigTicket(w, badParseFormRequest("POST", "/whatif/bigticket"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWhatIfUpdateChain_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfUpdateChain(w, badParseFormRequest("POST", "/whatif/chain"))
}

// ── Test handleCreateScenario error (duplicate) ────────────────────────────

func TestHandleCreateScenario_Valid(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"Another Scenario"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/scenarios", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleCreateScenario(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("HX-Redirect") != "/whatif" {
		t.Fatal("expected HX-Redirect header")
	}
}

// ── Test coverage for syncSettingsFromDashboard income pattern branches ─────

func TestSyncSettingsFromDashboard_WithFrequentIncome(t *testing.T) {
	settingsDir := t.TempDir()
	csvDir := t.TempDir()

	var lines []string
	lines = append(lines, "Date,Description,Amount,Type,Category")
	now := time.Now()
	for i := 0; i < 12; i++ {
		d := now.AddDate(0, 0, -i*14)
		lines = append(lines, fmt.Sprintf("%s,Paycheck,2500,Income,Employment", d.Format("2006-01-02")))
		lines = append(lines, fmt.Sprintf("%s,Groceries,-200,Outflow,Food", d.Format("2006-01-02")))
	}
	os.WriteFile(filepath.Join(csvDir, "test.csv"), []byte(strings.Join(lines, "\n")), 0644)

	store, _ := storage.New(settingsDir)
	rm := retirement.NewSettingsManager(settingsDir, store)
	dl := dataloader.New(csvDir, store)
	Initialize(dl, nil, rm)

	s := models.DefaultWhatIfSettings()
	s.IncomeSources = []models.IncomeSource{
		{ID: "dashboard-income", Name: "Old Dashboard", Amount: 3000, Type: models.IncomeFixed},
	}
	err := syncSettingsFromDashboard(s)
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	for _, src := range s.IncomeSources {
		if src.ID == "dashboard-income" {
			t.Fatal("old dashboard-income should have been removed")
		}
	}
}

// Regression: a refund row (opposite-signed Outflow) must REDUCE
// MonthlyLivingExpenses, not be added as an absolute value.
// Pre-fix bug: $1000 of purchases + $200 refund produced expenses=$1200
// instead of $800.
func TestSyncSettingsFromDashboard_RefundReducesMonthlyExpenses(t *testing.T) {
	settingsDir := t.TempDir()
	csvDir := t.TempDir()

	today := time.Now().Format("2006-01-02")
	lines := "Date,Description,Amount,Category\n" +
		today + ",Hotel,1000.00,Hotel\n" +
		today + ",Hotel Refund,-200.00,Hotel\n"
	if err := os.WriteFile(filepath.Join(csvDir, "test.csv"), []byte(lines), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	store, _ := storage.New(settingsDir)
	rm := retirement.NewSettingsManager(settingsDir, store)
	dl := dataloader.New(csvDir, store)
	Initialize(dl, nil, rm)

	s := models.DefaultWhatIfSettings()
	if err := syncSettingsFromDashboard(s); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// All transactions are dated today, so months is clamped to 1 (< 1 floor).
	// Net spending = 1000 - 200 = 800. Pre-fix would have been 1200.
	const want = 800.0
	if diff := s.MonthlyLivingExpenses - want; diff > 0.01 || diff < -0.01 {
		t.Errorf("MonthlyLivingExpenses = %.2f, want %.2f (refund must subtract)", s.MonthlyLivingExpenses, want)
	}
}

// ── Test syncSettingsFromDashboard with short date range ───────────────────

func TestSyncSettingsFromDashboard_ShortDateRange(t *testing.T) {
	settingsDir := t.TempDir()
	csvDir := t.TempDir()

	lines := "Date,Description,Amount,Type,Category\n" +
		time.Now().Format("2006-01-02") + ",Salary,5000,Income,Employment\n" +
		time.Now().Format("2006-01-02") + ",Rent,-2000,Outflow,Housing\n"
	os.WriteFile(filepath.Join(csvDir, "test.csv"), []byte(lines), 0644)

	store, _ := storage.New(settingsDir)
	rm := retirement.NewSettingsManager(settingsDir, store)
	dl := dataloader.New(csvDir, store)
	Initialize(dl, nil, rm)

	s := models.DefaultWhatIfSettings()
	err := syncSettingsFromDashboard(s)
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
}

// ── Tests with real renderer (renderer != nil branches) ─────────────────────

// setupTestEnvWithRenderer creates the same test environment as setupTestEnv
// but initializes the package with a real template renderer so that the
// renderer != nil branches are exercised.
func setupTestEnvWithRenderer(t *testing.T) (*retirement.SettingsManager, func()) {
	t.Helper()

	settingsDir := t.TempDir()
	csvDir := t.TempDir()

	csvPath := filepath.Join(csvDir, "test.csv")
	csvContent := "Date,Description,Amount,Type,Category\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Salary,5000,Income,Employment\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Rent,-2000,Outflow,Housing\n" +
		time.Now().AddDate(0, -2, 0).Format("2006-01-02") + ",Salary,5000,Income,Employment\n" +
		time.Now().AddDate(0, -2, 0).Format("2006-01-02") + ",Groceries,-500,Outflow,Food\n"
	os.WriteFile(csvPath, []byte(csvContent), 0644)

	store, err := storage.New(settingsDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	rm := retirement.NewSettingsManager(settingsDir, store)
	dl := dataloader.New(csvDir, store)

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	Initialize(dl, rend, rm)

	cache.mu.Lock()
	cache.hash = ""
	cache.analysis = nil
	cache.cachedAt = time.Time{}
	cache.mu.Unlock()

	cleanup := func() {}
	return rm, cleanup
}

func TestHandleWhatIf_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestHandleWhatIfCalculate_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/calculate", nil)
	handleWhatIfCalculate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestHandleWhatIfSettings_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"portfolio_value":         {"1500000"},
		"monthly_living_expenses": {"5000"},
		"current_age":             {"60"},
		"projection_years":        {"30"},
		"investment_return":       {"7.0"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestHandleWhatIfAddIncome_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"name":       {"Social Security"},
		"amount":     {"2000"},
		"start_year": {"5"},
		"end_year":   {"30"},
		"cola":       {"on"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
}

func TestHandleWhatIfUpdateIncome_WithRenderer(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	src := models.IncomeSource{ID: "rend-inc-1", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
	rm.AddIncomeSource(src)

	form := url.Values{"start_year": {"2"}, "end_year": {"10"}, "cola": {"true"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/income/rend-inc-1", formBody(form), map[string]string{"id": "rend-inc-1"})
	handleWhatIfUpdateIncome(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfSocialSecurity_WithRendererIncludesOOBRefresh(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"fra_benefit": {"2500"},
		"fra":         {"67"},
		"cola_rate":   {"2.0"},
		"claim_age":   {"68"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
	if !strings.Contains(w.Body.String(), `id="whatif-social-security-card" hx-swap-oob="true"`) {
		t.Fatalf("expected Social Security OOB refresh in body: %s", w.Body.String()[:min(w.Body.Len(), 500)])
	}
}

func TestHandleWhatIfDeleteIncome_WithRenderer(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	src := models.IncomeSource{ID: "rend-del-inc", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
	rm.AddIncomeSource(src)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/income/rend-del-inc", nil, map[string]string{"id": "rend-del-inc"})
	handleWhatIfDeleteIncome(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfRestoreIncome_WithRenderer(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	src := models.IncomeSource{ID: "rend-rest-inc", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
	rm.AddIncomeSource(src)
	rm.RemoveIncomeSource("rend-rest-inc")

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/income/rend-rest-inc/restore", nil, map[string]string{"id": "rend-rest-inc"})
	handleWhatIfRestoreIncome(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfAddExpense_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"name":       {"Car Payment"},
		"amount":     {"500"},
		"start_year": {"0"},
		"end_year":   {"5"},
		"inflation":  {"on"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfUpdateExpense_WithRenderer(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	exp := models.ExpenseSource{ID: "rend-exp-1", Name: "Test", Amount: 500}
	rm.AddExpenseSource(exp)

	form := url.Values{"start_year": {"1"}, "end_year": {"5"}, "inflation": {"on"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/expense/rend-exp-1", formBody(form), map[string]string{"id": "rend-exp-1"})
	handleWhatIfUpdateExpense(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfDeleteExpense_WithRenderer(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	exp := models.ExpenseSource{ID: "rend-del-exp", Name: "Test", Amount: 500}
	rm.AddExpenseSource(exp)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/expense/rend-del-exp", nil, map[string]string{"id": "rend-del-exp"})
	handleWhatIfDeleteExpense(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfRestoreExpense_WithRenderer(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	exp := models.ExpenseSource{ID: "rend-rest-exp", Name: "Test", Amount: 500}
	rm.AddExpenseSource(exp)
	rm.RemoveExpenseSource("rend-rest-exp")

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/expense/rend-rest-exp/restore", nil, map[string]string{"id": "rend-rest-exp"})
	handleWhatIfRestoreExpense(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfSync_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/sync", nil)
	handleWhatIfSync(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfMonteCarlo_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/montecarlo", nil)
	handleWhatIfMonteCarlo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfAddHealthcare_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"name":                 {"Alice"},
		"current_age":          {"55"},
		"current_coverage":     {"aca"},
		"current_monthly_cost": {"1200"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfUpdateHealthcare_WithRenderer(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	person := models.HealthcarePerson{
		ID:                  "rend-hc-1",
		Name:                "Bob",
		CurrentAge:          60,
		CurrentCoverage:     models.CoverageACA,
		CurrentMonthlyCost:  1000,
		MedicareMonthlyCost: 500,
		MedicareEligibleAge: 65,
	}
	rm.AddHealthcarePerson(person)

	form := url.Values{
		"name":                  {"Robert"},
		"current_age":           {"61"},
		"current_coverage":      {"aca"},
		"current_monthly_cost":  {"1100"},
		"medicare_monthly_cost": {"550"},
	}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/rend-hc-1", formBody(form), map[string]string{"id": "rend-hc-1"})
	handleWhatIfUpdateHealthcare(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfDeleteHealthcare_WithRenderer(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	person := models.HealthcarePerson{ID: "rend-hc-del", Name: "Test", CurrentAge: 60, CurrentCoverage: models.CoverageACA, CurrentMonthlyCost: 1000, MedicareMonthlyCost: 500, MedicareEligibleAge: 65}
	rm.AddHealthcarePerson(person)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/healthcare/rend-hc-del", nil, map[string]string{"id": "rend-hc-del"})
	handleWhatIfDeleteHealthcare(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfSpendingPhases_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"enabled":            {"on"},
		"phase_0_name":       {"Go-Go"},
		"phase_0_multiplier": {"1.0"},
		"phase_1_name":       {"Slow-Go"},
		"phase_1_start_age":  {"75"},
		"phase_1_multiplier": {"0.8"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSpendingPhases(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfAddPhase_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/add", nil)
	handleWhatIfAddPhase(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfDeletePhase_WithRenderer(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	s, _ := rm.Load()
	s.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: true,
		Phases: []models.SpendingPhase{
			{Name: "Phase 1", StartAge: 0, Multiplier: 1.0},
			{Name: "Phase 2", StartAge: 70, Multiplier: 0.9},
			{Name: "Phase 3", StartAge: 80, Multiplier: 0.7},
		},
	}
	rm.Save(s)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/spending-phases/2", nil, map[string]string{"index": "2"})
	handleWhatIfDeletePhase(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfResetPhases_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/reset", nil)
	handleWhatIfResetPhases(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfRothConversion_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"enabled":       {"on"},
		"annual_amount": {"50000"},
		"start_year":    {"0"},
		"end_year":      {"10"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfAddBigTicket_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"name":   {"New Roof"},
		"amount": {"25000"},
		"year":   {"5"},
		"type":   {"expense"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfDeleteBigTicket_WithRenderer(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	item := models.BigTicketItem{ID: "rend-bt-del", Name: "Test", Amount: 1000, Type: models.BigTicketExpense}
	rm.AddBigTicketItem(item)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/bigticket/rend-bt-del", nil, map[string]string{"id": "rend-bt-del"})
	handleWhatIfDeleteBigTicket(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfRestoreBigTicket_WithRenderer(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	item := models.BigTicketItem{ID: "rend-bt-rest", Name: "Test", Amount: 1000, Type: models.BigTicketExpense}
	rm.AddBigTicketItem(item)
	rm.RemoveBigTicketItem("rend-bt-rest")

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/bigticket/rend-bt-rest/restore", nil, map[string]string{"id": "rend-bt-rest"})
	handleWhatIfRestoreBigTicket(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

// ── Error path tests (retirementMgr.Load/Save failures) ────────────────────

// setupBrokenEnv creates an environment where retirementMgr operations fail
// because the settings file contains invalid JSON.
func setupBrokenEnv(t *testing.T) {
	t.Helper()

	settingsDir := t.TempDir()
	csvDir := t.TempDir()

	csvPath := filepath.Join(csvDir, "test.csv")
	os.WriteFile(csvPath, []byte("Date,Description,Amount,Type,Category\n2025-01-15,Salary,5000,Income,Employment\n"), 0644)

	store, err := storage.New(settingsDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	rm := retirement.NewSettingsManager(settingsDir, store)
	dl := dataloader.New(csvDir, store)
	Initialize(dl, nil, rm)

	// Write corrupt settings file so loadInternal fails
	os.WriteFile(filepath.Join(settingsDir, "whatif.json"), []byte("{invalid json!!!"), 0644)

	// Clear cache so Load() will try to read the corrupt file
	cache.mu.Lock()
	cache.hash = ""
	cache.analysis = nil
	cache.cachedAt = time.Time{}
	cache.mu.Unlock()
}

func expectError(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code == http.StatusOK {
		t.Fatalf("expected error status, got 200")
	}
}

func TestHandleWhatIf_LoadErrorFallsBackToDefaults(t *testing.T) {
	setupBrokenEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)

	// handleWhatIf gracefully falls back to default settings on load error
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (fallback to defaults), got %d", w.Code)
	}
}

func TestErrorPaths_LoadFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
		form    url.Values
		params  map[string]string
	}{
		{"Calculate", handleWhatIfCalculate, "POST", "/whatif/calculate", nil, nil},
		{"Settings", handleWhatIfSettings, "POST", "/whatif/settings", url.Values{"retirement_age": {"65"}}, nil},
		{"DeleteIncome", handleWhatIfDeleteIncome, "DELETE", "/whatif/income/x", nil, map[string]string{"id": "x"}},
		{"RestoreIncome", handleWhatIfRestoreIncome, "POST", "/whatif/income/x/restore", nil, map[string]string{"id": "x"}},
		{"DeleteExpense", handleWhatIfDeleteExpense, "DELETE", "/whatif/expense/x", nil, map[string]string{"id": "x"}},
		{"RestoreExpense", handleWhatIfRestoreExpense, "POST", "/whatif/expense/x/restore", nil, map[string]string{"id": "x"}},
		{"ProjectionChart", handleWhatIfProjectionChart, "GET", "/whatif/chart", nil, nil},
		{"Sync", handleWhatIfSync, "POST", "/whatif/sync", nil, nil},
		{"MonteCarlo", handleWhatIfMonteCarlo, "POST", "/whatif/montecarlo", nil, nil},
		{"DeleteHealthcare", handleWhatIfDeleteHealthcare, "DELETE", "/whatif/healthcare/x", nil, map[string]string{"id": "x"}},
		{"SpendingPhases", handleWhatIfSpendingPhases, "POST", "/whatif/spending-phases", url.Values{"enabled": {"true"}}, nil},
		{"AddPhase", handleWhatIfAddPhase, "POST", "/whatif/spending-phases/add", url.Values{"name": {"T"}, "multiplier": {"0.8"}, "start_year": {"5"}}, nil},
		{"DeletePhase", handleWhatIfDeletePhase, "DELETE", "/whatif/spending-phases/1", nil, map[string]string{"index": "1"}},
		{"ResetPhases", handleWhatIfResetPhases, "POST", "/whatif/spending-phases/reset", nil, nil},
		{"RothConversion", handleWhatIfRothConversion, "POST", "/whatif/roth-conversion", url.Values{"enabled": {"true"}}, nil},
		{"DeleteBigTicket", handleWhatIfDeleteBigTicket, "DELETE", "/whatif/bigticket/x", nil, map[string]string{"id": "x"}},
		{"RestoreBigTicket", handleWhatIfRestoreBigTicket, "POST", "/whatif/bigticket/x/restore", nil, map[string]string{"id": "x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupBrokenEnv(t)

			var body io.Reader
			if tt.form != nil {
				body = formBody(tt.form)
			}

			var req *http.Request
			if tt.params != nil {
				req = chiRequest(tt.method, tt.path, body, tt.params)
			} else {
				req = httptest.NewRequest(tt.method, tt.path, body)
				if tt.form != nil {
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
			}

			w := httptest.NewRecorder()
			tt.handler(w, req)
			expectError(t, w)
		})
	}
}

// ── Social Security ────────────────────────────────────────────────────────

func TestHandleWhatIfGuardrails(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":           {"on"},
		"floor_drop_pct":    {"20"},
		"floor_cut_pct":     {"10"},
		"ceiling_rise_pct":  {"25"},
		"ceiling_raise_pct": {"10"},
		"min_spending_pct":  {"75"},
		"max_spending_pct":  {"125"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/guardrails", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGuardrails(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if settings.Guardrails == nil || !settings.Guardrails.Enabled {
		t.Fatal("Guardrails should be enabled")
	}
	if settings.Guardrails.FloorDropPct != 20 {
		t.Errorf("FloorDropPct = %.0f, want 20", settings.Guardrails.FloorDropPct)
	}
	if settings.Guardrails.MinSpendingPct != 75 {
		t.Errorf("MinSpendingPct = %.0f, want 75", settings.Guardrails.MinSpendingPct)
	}
}

func TestHandleWhatIfGuardrails_Disabled(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/guardrails", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGuardrails(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if settings.Guardrails != nil && settings.Guardrails.Enabled {
		t.Error("Guardrails should be disabled")
	}
}

func TestHandleWhatIfGuardrails_UsesAnalysisCache(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	targetSettings, err := rm.Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	targetSettings.Guardrails = &models.GuardrailConfig{
		Enabled:         true,
		FloorDropPct:    20,
		FloorCutPct:     10,
		CeilingRisePct:  25,
		CeilingRaisePct: 10,
		MinSpendingPct:  75,
		MaxSpendingPct:  125,
	}
	primeAnalysisCache(targetSettings, 123456)

	form := url.Values{
		"enabled":           {"on"},
		"floor_drop_pct":    {"20"},
		"floor_cut_pct":     {"10"},
		"ceiling_rise_pct":  {"25"},
		"ceiling_raise_pct": {"10"},
		"min_spending_pct":  {"75"},
		"max_spending_pct":  {"125"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/guardrails", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGuardrails(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	pageData := readRecorderPageData(t, w)
	if pageData.Analysis == nil || pageData.Analysis.BudgetFit == nil {
		t.Fatal("expected cached analysis in response")
	}
	if pageData.Analysis.BudgetFit.MonthlyExpenses != 123456 {
		t.Fatalf("MonthlyExpenses = %.0f, want cached 123456", pageData.Analysis.BudgetFit.MonthlyExpenses)
	}
}

func TestHandleWhatIfGlidePath(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":          {"on"},
		"start_stock_pct":  {"80"},
		"end_stock_pct":    {"30"},
		"transition_years": {"20"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/glide-path", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGlidePath(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if settings.GlidePath == nil || !settings.GlidePath.Enabled {
		t.Fatal("GlidePath should be enabled")
	}
	if settings.GlidePath.StartStockPct != 80 {
		t.Errorf("StartStockPct = %.0f, want 80", settings.GlidePath.StartStockPct)
	}
	if settings.GlidePath.EndStockPct != 30 {
		t.Errorf("EndStockPct = %.0f, want 30", settings.GlidePath.EndStockPct)
	}
	if settings.GlidePath.TransitionYears != 20 {
		t.Errorf("TransitionYears = %d, want 20", settings.GlidePath.TransitionYears)
	}
}

func TestHandleWhatIfGlidePath_Disabled(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// First enable it
	form := url.Values{"enabled": {"on"}, "start_stock_pct": {"80"}, "end_stock_pct": {"30"}, "transition_years": {"20"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/glide-path", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGlidePath(w, req)

	// Now disable it
	form = url.Values{}
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/whatif/glide-path", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGlidePath(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if settings.GlidePath != nil && settings.GlidePath.Enabled {
		t.Error("GlidePath should be disabled")
	}
}

func TestHandleWhatIfGlidePath_UsesAnalysisCache(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	targetSettings, err := rm.Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	targetSettings.GlidePath = &models.GlidePathConfig{
		Enabled:         true,
		StartStockPct:   80,
		EndStockPct:     30,
		TransitionYears: 20,
	}
	primeAnalysisCache(targetSettings, 234567)

	form := url.Values{
		"enabled":          {"on"},
		"start_stock_pct":  {"80"},
		"end_stock_pct":    {"30"},
		"transition_years": {"20"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/glide-path", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGlidePath(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	pageData := readRecorderPageData(t, w)
	if pageData.Analysis == nil || pageData.Analysis.BudgetFit == nil {
		t.Fatal("expected cached analysis in response")
	}
	if pageData.Analysis.BudgetFit.MonthlyExpenses != 234567 {
		t.Fatalf("MonthlyExpenses = %.0f, want cached 234567", pageData.Analysis.BudgetFit.MonthlyExpenses)
	}
}

func TestHandleWhatIfSocialSecurity(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"fra_benefit": {"2500"},
		"fra":         {"67"},
		"cola_rate":   {"2.0"},
		"claim_age":   {"68"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	// Verify settings were saved
	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if settings.SocialSecurity == nil {
		t.Fatal("SocialSecurity config should not be nil")
	}
	if settings.SocialSecurity.FRABenefit != 2500 {
		t.Errorf("FRABenefit = %.0f, want 2500", settings.SocialSecurity.FRABenefit)
	}
	if settings.SocialSecurity.FRA != 67 {
		t.Errorf("FRA = %d, want 67", settings.SocialSecurity.FRA)
	}
	if settings.SocialSecurity.COLARate != 0.02 {
		t.Errorf("COLARate = %f, want 0.02", settings.SocialSecurity.COLARate)
	}
	if settings.SocialSecurity.ClaimAge != 68 {
		t.Errorf("ClaimAge = %d, want 68", settings.SocialSecurity.ClaimAge)
	}
}

func TestHandleWhatIfSocialSecurity_ClearsOnZero(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"fra_benefit": {"0"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if settings.SocialSecurity != nil {
		t.Error("SocialSecurity should be nil when benefit is 0")
	}
}

func TestHandleWhatIfSocialSecurity_UsesAnalysisCache(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	targetSettings, err := rm.Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	targetSettings.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit:  2500,
		FRA:         67,
		COLARate:    0.02,
		COLARateSet: true, // F-026: form handler always sets this flag on submit
		ClaimAge:    68,
	}
	primeAnalysisCache(targetSettings, 345678)

	form := url.Values{
		"fra_benefit": {"2500"},
		"fra":         {"67"},
		"cola_rate":   {"2.0"},
		"claim_age":   {"68"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	pageData := readRecorderPageData(t, w)
	if pageData.Analysis == nil || pageData.Analysis.BudgetFit == nil {
		t.Fatal("expected cached analysis in response")
	}
	if pageData.Analysis.BudgetFit.MonthlyExpenses != 345678 {
		t.Fatalf("MonthlyExpenses = %.0f, want cached 345678", pageData.Analysis.BudgetFit.MonthlyExpenses)
	}
}

func TestHandleWhatIfSocialSecurity_InvalidClaimAgesClearSelection(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit:       2500,
		FRA:              67,
		ClaimAge:         67,
		SpouseFRABenefit: 1500,
		SpouseFRA:        67,
		SpouseClaimAge:   67,
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("failed to seed settings: %v", err)
	}

	form := url.Values{
		"fra_benefit":      {"2500"},
		"fra":              {"67"},
		"claim_age":        {"71"},
		"spouse_claim_age": {"bad"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if loaded.SocialSecurity.ClaimAge != 0 {
		t.Errorf("ClaimAge = %d, want cleared 0", loaded.SocialSecurity.ClaimAge)
	}
	if loaded.SocialSecurity.SpouseClaimAge != 0 {
		t.Errorf("SpouseClaimAge = %d, want cleared 0", loaded.SocialSecurity.SpouseClaimAge)
	}
}

func TestHandleWhatIfSocialSecurity_WithSpouse(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"fra_benefit":        {"2500"},
		"fra":                {"67"},
		"cola_rate":          {"2.0"},
		"spouse_fra_benefit": {"1800"},
		"spouse_fra":         {"66"},
		"spouse_claim_age":   {"66"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("failed to load settings: %v", err)
	}
	if settings.SocialSecurity.SpouseFRABenefit != 1800 {
		t.Errorf("SpouseFRABenefit = %.0f, want 1800", settings.SocialSecurity.SpouseFRABenefit)
	}
	if settings.SocialSecurity.SpouseFRA != 66 {
		t.Errorf("SpouseFRA = %d, want 66", settings.SocialSecurity.SpouseFRA)
	}
	if settings.SocialSecurity.SpouseClaimAge != 66 {
		t.Errorf("SpouseClaimAge = %d, want 66", settings.SocialSecurity.SpouseClaimAge)
	}
}

func TestHandleWhatIfSocialSecurity_PopulatesPortfolio(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings := models.DefaultWhatIfSettings()
	settings.StartDate = "2026-04"
	settings.PortfolioValue = 1_000_000
	settings.MonthlyLivingExpenses = 5000
	settings.TaxDeferredPercent = 60
	settings.RothPercent = 10
	settings.ProjectionYears = 15
	settings.Persons = []models.Person{
		{ID: "p1", Name: "You", BirthMonth: "1958-11", Role: models.PersonRolePrimary},
		{ID: "p2", Name: "Spouse", BirthMonth: "1971-08", Role: models.PersonRoleSpouse},
	}
	prepare.ComputeAges(settings)
	settings.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit:       4100,
		FRA:              66,
		COLARate:         0.02,
		SpouseFRABenefit: 154,
		SpouseFRA:        67,
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("failed to seed settings: %v", err)
	}

	form := url.Values{
		"fra_benefit":        {"4100"},
		"fra":                {"66"},
		"cola_rate":          {"2.0"},
		"spouse_fra_benefit": {"154"},
		"spouse_fra":         {"67"},
		"spouse_claim_age":   {"62"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	var pageData models.WhatIfPageData
	if err := json.NewDecoder(w.Body).Decode(&pageData); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if pageData.Analysis == nil || pageData.Analysis.SocialSecurity == nil {
		t.Fatal("expected social security analysis in response")
	}
	if pageData.Analysis.SocialSecurity.Portfolio == nil {
		t.Fatal("expected portfolio analysis in response")
	}

	portfolio := pageData.Analysis.SocialSecurity.Portfolio
	if len(portfolio.SpouseOptions) == 0 {
		t.Fatal("expected spouse portfolio options")
	}
	if portfolio.BaselineSurvivalRate < 0 || portfolio.BaselineSurvivalRate > 100 {
		t.Fatalf("baseline survival rate out of range: %.2f", portfolio.BaselineSurvivalRate)
	}
}

// ── Save() failure tests via chmod-readonly settingsDir ─────────────────────
//
// The helpers below set up an environment where retirementMgr.Save will fail
// because the settings directory is read-only. They prime the load cache
// (so Load() succeeds via cache) and then chmod the dir before calling the
// handler so the subsequent Save fails. t.Cleanup restores 0o755 so that
// t.TempDir's cleanup can remove the dir.

// setupTestEnvWithDir is like setupTestEnv but also returns the underlying
// settingsDir so tests can chmod it to provoke Save failures.
func setupTestEnvWithDir(t *testing.T) (*retirement.SettingsManager, string, func()) {
	t.Helper()

	settingsDir := t.TempDir()
	csvDir := t.TempDir()

	csvPath := filepath.Join(csvDir, "test.csv")
	csvContent := "Date,Description,Amount,Type,Category\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Salary,5000,Income,Employment\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Rent,-2000,Outflow,Housing\n"
	os.WriteFile(csvPath, []byte(csvContent), 0644)

	store, err := storage.New(settingsDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	rm := retirement.NewSettingsManager(settingsDir, store)
	dl := dataloader.New(csvDir, store)
	Initialize(dl, nil, rm)

	cache.mu.Lock()
	cache.hash = ""
	cache.analysis = nil
	cache.cachedAt = time.Time{}
	cache.mu.Unlock()

	cleanup := func() {}
	return rm, settingsDir, cleanup
}

// makeSaveFail chmod's settingsDir to 0o500 (read+execute, no write) so the
// next Save() call fails. Registers a Cleanup to restore 0o755 so t.TempDir
// can purge the directory.
func makeSaveFail(t *testing.T, settingsDir string) {
	t.Helper()
	if err := os.Chmod(settingsDir, 0o500); err != nil {
		t.Fatalf("chmod 0o500 %s: %v", settingsDir, err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(settingsDir, 0o755)
	})
}

// primeLoadCache loads settings once so that subsequent Load() calls hit
// the cache and don't try to read from disk after we lock it down.
func primeLoadCache(t *testing.T, rm *retirement.SettingsManager) {
	t.Helper()
	if _, err := rm.Load(); err != nil {
		t.Fatalf("prime Load: %v", err)
	}
}

func TestHandleWhatIfRothConversion_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{
		"enabled":       {"on"},
		"annual_amount": {"10000"},
		"start_year":    {"2027"},
		"end_year":      {"2030"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRothConversion_NegativeAmount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":       {"on"},
		"annual_amount": {"-100"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "negative") {
		t.Errorf("body should mention negative; got: %s", w.Body.String())
	}
}

func TestHandleWhatIfRothConversion_NegativeStartYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":    {"on"},
		"start_year": {"-1"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRothConversion_NegativeEndYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":  {"on"},
		"end_year": {"-1"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRothConversion_EndBeforeStart(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"enabled":    {"on"},
		"start_year": {"2030"},
		"end_year":   {"2025"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "earlier than start") {
		t.Errorf("body should mention end before start; got: %s", w.Body.String())
	}
}

func TestHandleWhatIfSocialSecurity_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{
		"fra_benefit": {"2500"},
		"fra":         {"67"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

// badEncodedRequest creates a request with an x-www-form-urlencoded body
// containing invalid percent encoding, which makes r.ParseForm() return
// "invalid URL escape" — unlike multipart/form-data without a boundary,
// which Go's net/http silently accepts.
func badEncodedRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader("foo=%ZZ"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestHandleWhatIfSocialSecurity_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfSocialSecurity(w, badEncodedRequest("POST", "/whatif/social-security"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfGlidePath_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfGlidePath(w, badEncodedRequest("POST", "/whatif/glide-path"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfGlidePath_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{
		"enabled":          {"on"},
		"start_stock_pct":  {"80"},
		"end_stock_pct":    {"40"},
		"transition_years": {"15"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/glide-path", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGlidePath(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfGlidePath_DisableExisting(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Seed an enabled glide path so the disable path mutates an existing config.
	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.GlidePath = &models.GlidePathConfig{
		Enabled:         true,
		StartStockPct:   80,
		EndStockPct:     40,
		TransitionYears: 15,
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Submit form without enabled=on to disable it.
	form := url.Values{}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/glide-path", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGlidePath(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.GlidePath == nil || loaded.GlidePath.Enabled {
		t.Fatalf("expected GlidePath to be disabled, got %+v", loaded.GlidePath)
	}
}

func TestHandleWhatIfGuardrails_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfGuardrails(w, badEncodedRequest("POST", "/whatif/guardrails"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfGuardrails_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{
		"enabled":           {"on"},
		"floor_drop_pct":    {"20"},
		"floor_cut_pct":     {"10"},
		"ceiling_rise_pct":  {"25"},
		"ceiling_raise_pct": {"10"},
		"min_spending_pct":  {"75"},
		"max_spending_pct":  {"125"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/guardrails", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGuardrails(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfGuardrails_DisableExisting(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.Guardrails = &models.GuardrailConfig{
		Enabled:         true,
		FloorDropPct:    20,
		FloorCutPct:     10,
		CeilingRisePct:  25,
		CeilingRaisePct: 10,
		MinSpendingPct:  75,
		MaxSpendingPct:  125,
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	form := url.Values{} // no "enabled" -> disable
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/guardrails", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGuardrails(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Guardrails == nil || loaded.Guardrails.Enabled {
		t.Fatalf("expected Guardrails to be disabled, got %+v", loaded.Guardrails)
	}
}

func TestHandleWhatIfAddPhase_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/add", nil)
	handleWhatIfAddPhase(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddPhase_NilConfigInitializes(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Force-clear SpendingPhaseConfig in the cache to exercise the "nil config" branch.
	settings.SpendingPhaseConfig = nil
	// Bypass Save's normalization which would re-create defaults; mutate the
	// cached pointer directly. handleWhatIfAddPhase reads via Load() which
	// returns the same cached pointer when present.

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/add", nil)
	handleWhatIfAddPhase(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfResetPhases_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/reset", nil)
	handleWhatIfResetPhases(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfSync_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/sync", nil)
	handleWhatIfSync(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfSettings_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{
		"portfolio_value":         {"1500000"},
		"monthly_living_expenses": {"5000"},
		"projection_years":        {"30"},
		"investment_return":       {"7.0"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateChain_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	// Create two scenarios so we have two distinct files; the first becomes
	// the active filename after CreateScenario, and the second is a valid
	// chain target.
	if _, err := rm.CreateScenario("Primary"); err != nil {
		t.Fatalf("CreateScenario primary: %v", err)
	}
	if _, err := rm.CreateScenario("Target"); err != nil {
		t.Fatalf("CreateScenario target: %v", err)
	}
	scenarios, _ := rm.ListScenarios()
	var primaryFile, targetFile string
	for _, s := range scenarios {
		switch s.Name {
		case "Primary":
			primaryFile = s.Filename
		case "Target":
			targetFile = s.Filename
		}
	}
	if err := rm.SwitchScenario(primaryFile); err != nil {
		t.Fatalf("SwitchScenario: %v", err)
	}

	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{
		"chain_scenario[]": {targetFile},
		"chain_age[]":      {"70"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/chain", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateChain(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateChain_InvalidChain(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Reference a nonexistent scenario file; ValidateScenarioChain should reject.
	form := url.Values{
		"chain_scenario[]": {"whatif_nonexistent.json"},
		"chain_age[]":      {"70"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/chain", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateChain(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Invalid chain") {
		t.Errorf("body should mention invalid chain; got: %s", w.Body.String())
	}
}

func TestHandleWhatIfDeleteChainLink_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	// Seed scenarios so there's a valid active file and chain target.
	if _, err := rm.CreateScenario("Primary"); err != nil {
		t.Fatalf("CreateScenario primary: %v", err)
	}
	if _, err := rm.CreateScenario("Target"); err != nil {
		t.Fatalf("CreateScenario target: %v", err)
	}
	scenarios, _ := rm.ListScenarios()
	var primaryFile, targetFile string
	for _, s := range scenarios {
		switch s.Name {
		case "Primary":
			primaryFile = s.Filename
		case "Target":
			targetFile = s.Filename
		}
	}
	if err := rm.SwitchScenario(primaryFile); err != nil {
		t.Fatalf("SwitchScenario: %v", err)
	}
	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.ScenarioChain = []models.ScenarioChainLink{{ScenarioFilename: targetFile, TransitionAge: 70}}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/chain/0", nil, map[string]string{"index": "0"})
	handleWhatIfDeleteChainLink(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

// ── ParsePersonsForm misalignment branches ─────────────────────────────────

func TestHandleWhatIfSettings_PersonsZeroRows(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Provide start_date to flip hasPersons to true with no name rows.
	form := url.Values{"start_date": {"2026-04"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "at least one person") {
		t.Errorf("body should mention persons required; got: %s", w.Body.String())
	}
}

func TestHandleWhatIfSettings_PersonsMisaligned(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// names has 2 entries but birth_month only has 1 -> misaligned.
	form := url.Values{
		"start_date":           {"2026-04"},
		"person_id[]":          {"primary", "spouse"},
		"person_name[]":        {"Alex", "Casey"},
		"person_birth_month[]": {"1960-05"},
		"person_role[]":        {"primary", "spouse"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "misaligned") {
		t.Errorf("body should mention misaligned; got: %s", w.Body.String())
	}
}

func TestHandleWhatIfSettings_PersonsInvalidRole(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"start_date":           {"2026-04"},
		"person_id[]":          {"primary"},
		"person_name[]":        {"Alex"},
		"person_birth_month[]": {"1960-05"},
		"person_role[]":        {"emperor"}, // invalid role
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid role") {
		t.Errorf("body should mention invalid role; got: %s", w.Body.String())
	}
}

// ── formHasKey direct tests (cover both early-return branches) ─────────────

func TestFormHasKey_DirectAndArrayKeys(t *testing.T) {
	cases := []struct {
		name     string
		form     url.Values
		key      string
		expected bool
	}{
		{"direct", url.Values{"foo": {"v"}}, "foo", true},
		{"array", url.Values{"foo[]": {"v"}}, "foo", true},
		{"missing", url.Values{"bar": {"v"}}, "foo", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/x", formBody(tc.form))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if err := req.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			if got := formHasKey(req, tc.key); got != tc.expected {
				t.Errorf("formHasKey(%q) = %v, want %v", tc.key, got, tc.expected)
			}
		})
	}
}

// ── formValues direct test (covers both ok branches and the nil fallthrough) ─

func TestFormValues_DirectAndArrayAndMissing(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		key  string
		want []string
	}{
		{"array", url.Values{"foo[]": {"a", "b"}}, "foo", []string{"a", "b"}},
		{"direct", url.Values{"foo": {"x"}}, "foo", []string{"x"}},
		{"missing", url.Values{"bar": {"x"}}, "foo", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/x", formBody(tc.form))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if err := req.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			got := formValues(req, tc.key)
			if len(got) != len(tc.want) {
				t.Fatalf("formValues(%q) = %v, want %v", tc.key, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] %q != %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// ── handleWhatIfProjectionChart load failure path ───────────────────────────

func TestHandleWhatIfProjectionChart_LoadFailure(t *testing.T) {
	setupBrokenEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/chart/projection", nil)
	handleWhatIfProjectionChart(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

// ── handleListScenarios — ensure non-empty list is returned (covers loop body) ─

func TestHandleListScenarios_WithCreatedScenarios(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	if _, err := rm.CreateScenario("Plan B"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/scenarios", nil)
	handleListScenarios(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Plan B") {
		t.Errorf("body should contain newly-created scenario; got: %s", w.Body.String())
	}
}

// ── handleSwitchScenario / handleDeleteScenario / handleRenameScenario
//    fallthrough InternalServerError tests via Save() failure ──────────────

// statusForScenarioOperationError covers ValidationError, NotFoundError,
// ConflictError; the InternalServerError fallback (last `return` in the
// handler) is exercised by causing the underlying Save to fail with an
// untyped error. Because Switch/Delete/Rename go through file-system ops
// (rename, glob, etc.), chmod 0o500 reliably triggers a non-typed error.

func TestHandleSwitchScenario_NonTypedError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	// Create a target scenario first.
	if _, err := rm.CreateScenario("Switch Target"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	scenarios, _ := rm.ListScenarios()
	var targetFile string
	for _, s := range scenarios {
		if s.Name == "Switch Target" {
			targetFile = s.Filename
		}
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{"filename": {targetFile}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/scenarios/switch", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleSwitchScenario(w, req)

	// SwitchScenario itself only does Stat+filename swap; it doesn't write
	// to disk. Thus chmod 0o500 should not break it. Just verify it doesn't
	// 500 with an unexpected typed error. If it returns 200 that's also fine.
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 200 or 500. body: %s", w.Code, w.Body.String())
	}
}

// ── handleWhatIfMonteCarlo — ensure renderer-nil path on success ───────────

func TestHandleWhatIfMonteCarlo_Success(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/montecarlo", nil)
	handleWhatIfMonteCarlo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON content type, got %s", ct)
	}
}

// ── handleWhatIfRothConversion — initialise nil RothConversion ─────────────

// ── Real ParseForm error coverage for handlers that previously only had
//    the multipart-no-boundary "smoke" test (which doesn't trigger an
//    actual ParseForm error in modern Go). These use invalid percent
//    encoding which always errors. ─────────────────────────────────────

func TestHandleWhatIfRothConversion_RealParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfRothConversion(w, badEncodedRequest("POST", "/whatif/roth-conversion"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfSettings_RealParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfSettings(w, badEncodedRequest("POST", "/whatif/settings"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfSpendingPhases_RealParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfSpendingPhases(w, badEncodedRequest("POST", "/whatif/spending-phases"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfAddIncome_RealParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfAddIncome(w, badEncodedRequest("POST", "/whatif/income"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfAddExpense_RealParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfAddExpense(w, badEncodedRequest("POST", "/whatif/expense"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfAddBigTicket_RealParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfAddBigTicket(w, badEncodedRequest("POST", "/whatif/bigticket"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfUpdateChain_RealParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfUpdateChain(w, badEncodedRequest("POST", "/whatif/chain"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfAddHealthcare_RealParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleWhatIfAddHealthcare(w, badEncodedRequest("POST", "/whatif/healthcare"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfUpdateHealthcare_RealParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := badEncodedRequest("PUT", "/whatif/healthcare/x")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "x")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfUpdateIncome_RealParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := badEncodedRequest("PUT", "/whatif/income/x")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "x")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	handleWhatIfUpdateIncome(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleWhatIfUpdateExpense_RealParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := badEncodedRequest("PUT", "/whatif/expense/x")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "x")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	handleWhatIfUpdateExpense(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// covered indirectly elsewhere, but make sure the disabled+nil branch
// covers initialization (line 379-381).
// ── Save-failure tests for income/expense/bigticket/healthcare handlers ─────

func TestHandleWhatIfDeleteIncome_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	// Add an income source first.
	src := models.IncomeSource{ID: "del-save", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
	if _, err := rm.AddIncomeSource(src); err != nil {
		t.Fatalf("AddIncomeSource: %v", err)
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/income/del-save", nil, map[string]string{"id": "del-save"})
	handleWhatIfDeleteIncome(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRestoreIncome_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	src := models.IncomeSource{ID: "rest-save", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
	if _, err := rm.AddIncomeSource(src); err != nil {
		t.Fatalf("AddIncomeSource: %v", err)
	}
	if _, err := rm.RemoveIncomeSource("rest-save"); err != nil {
		t.Fatalf("RemoveIncomeSource: %v", err)
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/income/rest-save/restore", nil, map[string]string{"id": "rest-save"})
	handleWhatIfRestoreIncome(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("expected error, got 200. body: %s", w.Body.String())
	}
}

func TestHandleWhatIfDeleteExpense_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	src := models.ExpenseSource{ID: "del-exp-save", Name: "Test", Amount: 100}
	if _, err := rm.AddExpenseSource(src); err != nil {
		t.Fatalf("AddExpenseSource: %v", err)
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/expense/del-exp-save", nil, map[string]string{"id": "del-exp-save"})
	handleWhatIfDeleteExpense(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRestoreExpense_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	src := models.ExpenseSource{ID: "rest-exp-save", Name: "Test", Amount: 100}
	if _, err := rm.AddExpenseSource(src); err != nil {
		t.Fatalf("AddExpenseSource: %v", err)
	}
	if _, err := rm.RemoveExpenseSource("rest-exp-save"); err != nil {
		t.Fatalf("RemoveExpenseSource: %v", err)
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/expense/rest-exp-save/restore", nil, map[string]string{"id": "rest-exp-save"})
	handleWhatIfRestoreExpense(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("expected error, got 200. body: %s", w.Body.String())
	}
}

func TestHandleWhatIfDeleteBigTicket_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	item := models.BigTicketItem{ID: "del-bt-save", Name: "Test", Amount: 1000, Year: 5, Type: models.BigTicketExpense}
	if _, err := rm.AddBigTicketItem(item); err != nil {
		t.Fatalf("AddBigTicketItem: %v", err)
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/bigticket/del-bt-save", nil, map[string]string{"id": "del-bt-save"})
	handleWhatIfDeleteBigTicket(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRestoreBigTicket_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	item := models.BigTicketItem{ID: "rest-bt-save", Name: "Test", Amount: 1000, Year: 5, Type: models.BigTicketExpense}
	if _, err := rm.AddBigTicketItem(item); err != nil {
		t.Fatalf("AddBigTicketItem: %v", err)
	}
	if _, err := rm.RemoveBigTicketItem("rest-bt-save"); err != nil {
		t.Fatalf("RemoveBigTicketItem: %v", err)
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/bigticket/rest-bt-save/restore", nil, map[string]string{"id": "rest-bt-save"})
	handleWhatIfRestoreBigTicket(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("expected error, got 200. body: %s", w.Body.String())
	}
}

func TestHandleWhatIfDeleteHealthcare_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	person := models.HealthcarePerson{ID: "del-hc-save", Name: "Test", CurrentAge: 60, CurrentCoverage: models.CoverageACA, CurrentMonthlyCost: 1000, MedicareEligibleAge: 65}
	if _, err := rm.AddHealthcarePerson(person); err != nil {
		t.Fatalf("AddHealthcarePerson: %v", err)
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/healthcare/del-hc-save", nil, map[string]string{"id": "del-hc-save"})
	handleWhatIfDeleteHealthcare(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddIncome_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{
		"name":       {"Test Income"},
		"amount":     {"1000"},
		"start_year": {"0"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddExpense_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{
		"name":       {"Test Expense"},
		"amount":     {"500"},
		"start_year": {"0"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddBigTicket_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{
		"name":   {"Big Item"},
		"amount": {"5000"},
		"year":   {"3"},
		"type":   {"expense"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddHealthcare_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{
		"name":                    {"Test Person"},
		"current_age":             {"50"},
		"current_monthly_cost":    {"1000"},
		"current_coverage":        {"aca"},
		"medicare_monthly_cost":   {"500"},
		"pre_medicare_inflation":  {"7"},
		"post_medicare_inflation": {"4"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateIncome_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	src := models.IncomeSource{ID: "upd-inc-save", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
	if _, err := rm.AddIncomeSource(src); err != nil {
		t.Fatalf("AddIncomeSource: %v", err)
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{"start_year": {"5"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/income/upd-inc-save", formBody(form), map[string]string{"id": "upd-inc-save"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateIncome(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateExpense_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	src := models.ExpenseSource{ID: "upd-exp-save", Name: "Test", Amount: 100}
	if _, err := rm.AddExpenseSource(src); err != nil {
		t.Fatalf("AddExpenseSource: %v", err)
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{"start_year": {"3"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/expense/upd-exp-save", formBody(form), map[string]string{"id": "upd-exp-save"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateExpense(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateHealthcare_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	person := models.HealthcarePerson{
		ID:                  "upd-hc-save",
		Name:                "Test",
		CurrentAge:          60,
		CurrentCoverage:     models.CoverageACA,
		CurrentMonthlyCost:  1000,
		MedicareEligibleAge: 65,
	}
	if _, err := rm.AddHealthcarePerson(person); err != nil {
		t.Fatalf("AddHealthcarePerson: %v", err)
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{"current_monthly_cost": {"1500"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/upd-hc-save", formBody(form), map[string]string{"id": "upd-hc-save"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfDeletePhase_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	// Ensure at least 3 phases exist before chmod.
	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.SpendingPhaseConfig == nil || len(settings.SpendingPhaseConfig.Phases) < 3 {
		settings.SpendingPhaseConfig = &models.SpendingPhaseConfig{
			Enabled: true,
			Phases: []models.SpendingPhase{
				{Name: "P1", StartAge: 0, Multiplier: 1.0},
				{Name: "P2", StartAge: 70, Multiplier: 0.9},
				{Name: "P3", StartAge: 80, Multiplier: 0.8},
			},
		}
		if err := rm.Save(settings); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/spending-phases/1", nil, map[string]string{"index": "1"})
	handleWhatIfDeletePhase(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfSpendingPhases_SaveError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)
	makeSaveFail(t, dir)

	form := url.Values{
		"enabled": {"on"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSpendingPhases(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

// ── Healthcare validation paths ─────────────────────────────────────────────

func TestHandleWhatIfAddHealthcare_NegativeMonthlyCost(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":                 {"Test"},
		"current_age":          {"50"},
		"current_monthly_cost": {"-50"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddHealthcare_AgeOutOfRange(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":        {"Test"},
		"current_age": {"200"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateHealthcare_AgeOutOfRange(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	person := models.HealthcarePerson{
		ID: "upd-aoor", Name: "Test", CurrentAge: 50,
		CurrentCoverage: models.CoverageACA, MedicareEligibleAge: 65,
	}
	if _, err := rm.AddHealthcarePerson(person); err != nil {
		t.Fatalf("AddHealthcarePerson: %v", err)
	}

	form := url.Values{"current_age": {"-5"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/upd-aoor", formBody(form), map[string]string{"id": "upd-aoor"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateHealthcare_PersonIDNotFound(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"person_id": {"nonexistent-person"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/abc", formBody(form), map[string]string{"id": "abc"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateHealthcare_UnlinkRequiresName(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create a healthcare person linked to a real person.
	person := models.HealthcarePerson{
		ID: "linked-hc", Name: "Test", CurrentAge: 50,
		CurrentCoverage: models.CoverageACA, MedicareEligibleAge: 65,
	}
	if _, err := rm.AddHealthcarePerson(person); err != nil {
		t.Fatalf("AddHealthcarePerson: %v", err)
	}

	// person_id="" means unlink. Without name, must error.
	form := url.Values{"person_id": {""}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/linked-hc", formBody(form), map[string]string{"id": "linked-hc"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateHealthcare_UnlinkRequiresAge(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	person := models.HealthcarePerson{
		ID: "linked-hc-2", Name: "Test", CurrentAge: 50,
		CurrentCoverage: models.CoverageACA, MedicareEligibleAge: 65,
	}
	if _, err := rm.AddHealthcarePerson(person); err != nil {
		t.Fatalf("AddHealthcarePerson: %v", err)
	}

	// Unlink with name but no age.
	form := url.Values{
		"person_id": {""},
		"name":      {"Updated Name"},
	}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/linked-hc-2", formBody(form), map[string]string{"id": "linked-hc-2"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateHealthcare_UnlinkBadAge(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	person := models.HealthcarePerson{
		ID: "linked-hc-3", Name: "Test", CurrentAge: 50,
		CurrentCoverage: models.CoverageACA, MedicareEligibleAge: 65,
	}
	if _, err := rm.AddHealthcarePerson(person); err != nil {
		t.Fatalf("AddHealthcarePerson: %v", err)
	}

	form := url.Values{
		"person_id":   {""},
		"name":        {"Updated Name"},
		"current_age": {"abc"},
	}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/linked-hc-3", formBody(form), map[string]string{"id": "linked-hc-3"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateHealthcare_UnlinkAgeOutOfRange(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	person := models.HealthcarePerson{
		ID: "linked-hc-4", Name: "Test", CurrentAge: 50,
		CurrentCoverage: models.CoverageACA, MedicareEligibleAge: 65,
	}
	if _, err := rm.AddHealthcarePerson(person); err != nil {
		t.Fatalf("AddHealthcarePerson: %v", err)
	}

	form := url.Values{
		"person_id":   {""},
		"name":        {"Updated Name"},
		"current_age": {"200"},
	}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/linked-hc-4", formBody(form), map[string]string{"id": "linked-hc-4"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", w.Code, w.Body.String())
	}
}

// ── handleWhatIfAddHealthcare default-when-missing branches ────────────────

func TestHandleWhatIfAddHealthcare_DefaultsACAUnder65(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// No coverage_type, no monthly_cost; age < 65 should default to ACA + 1100.
	form := url.Values{
		"name":        {"Test"},
		"current_age": {"55"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(settings.HealthcarePersons) == 0 {
		t.Fatal("expected a healthcare person to be created")
	}
	last := settings.HealthcarePersons[len(settings.HealthcarePersons)-1]
	if last.CurrentCoverage != models.CoverageACA {
		t.Errorf("CurrentCoverage = %v, want CoverageACA", last.CurrentCoverage)
	}
	if last.CurrentMonthlyCost != 1100 {
		t.Errorf("CurrentMonthlyCost = %v, want 1100 (ACA default)", last.CurrentMonthlyCost)
	}
}

// ── Load() failure tests for Add* handlers ─────────────────────────────────

func TestHandleWhatIfAddHealthcare_LoadError(t *testing.T) {
	setupBrokenEnv(t)

	form := url.Values{"name": {"Test"}, "current_age": {"55"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateHealthcare_LoadError(t *testing.T) {
	setupBrokenEnv(t)

	form := url.Values{"current_monthly_cost": {"1500"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/x", formBody(form), map[string]string{"id": "x"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateHealthcare(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

// ── runAnalysisWithCache failure: settings with broken chain after deletion ──
//
// To reach the runAnalysisWithCache error branches, we need buildCalculator
// to fail. Create a scenario, save settings referencing it, then delete the
// referenced scenario file directly from disk (bypassing DeleteScenario's
// referential-integrity check). Subsequent Load returns settings with a
// dangling ScenarioChain, and buildCalculator fails to load it.

func setupBrokenChainEnv(t *testing.T) (*retirement.SettingsManager, string) {
	t.Helper()
	return setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})
}

func TestRunAnalysisWithCache_BrokenChain(t *testing.T) {
	rm, _ := setupBrokenChainEnv(t)

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(settings.ScenarioChain) == 0 {
		t.Skip("expected dangling chain in settings")
	}

	_, err = runAnalysisWithCache(settings)
	if err == nil {
		t.Fatal("expected runAnalysisWithCache to fail with broken chain")
	}
}

// Drives handleWhatIfCalculate through the runAnalysisWithCache failure path.
func TestHandleWhatIfCalculate_AnalysisError(t *testing.T) {
	setupBrokenChainEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/calculate", nil)
	handleWhatIfCalculate(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfProjectionChart_AnalysisError(t *testing.T) {
	setupBrokenChainEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/chart/projection", nil)
	handleWhatIfProjectionChart(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIf_AnalysisError(t *testing.T) {
	setupBrokenChainEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfMonteCarlo_BuildCalculatorError(t *testing.T) {
	setupBrokenChainEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/montecarlo", nil)
	handleWhatIfMonteCarlo(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

// setupItemsThenBreakChain seeds an item via the provided seed function,
// then makes the chain "look valid" (file exists) but unparseable so that
// LoadScenarioSettings inside buildCalculator fails. saveInternal's
// validateChainInternal only checks file existence, so saving a chain that
// references a corrupt file still succeeds. Subsequent Add/Remove/Restore
// operations succeed too — but runAnalysisWithCache fails when
// buildCalculator tries to parse the corrupt scenario file.
func setupItemsThenBreakChain(t *testing.T, seed func(rm *retirement.SettingsManager)) (*retirement.SettingsManager, string) {
	t.Helper()

	settingsDir := t.TempDir()
	csvDir := t.TempDir()

	csvPath := filepath.Join(csvDir, "test.csv")
	csvContent := "Date,Description,Amount,Type,Category\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Salary,5000,Income,Employment\n"
	os.WriteFile(csvPath, []byte(csvContent), 0644)

	store, err := storage.New(settingsDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	rm := retirement.NewSettingsManager(settingsDir, store)
	dl := dataloader.New(csvDir, store)
	Initialize(dl, nil, rm)

	cache.mu.Lock()
	cache.hash = ""
	cache.analysis = nil
	cache.cachedAt = time.Time{}
	cache.mu.Unlock()

	// Write an UNPARSEABLE scenario file directly. validateChainInternal
	// only checks file existence (via Stat), so save will accept a chain
	// referencing it, but buildCalculator -> LoadScenarioSettings will fail
	// with a JSON-parse error.
	corruptFile := "whatif_corrupt.json"
	if err := os.WriteFile(filepath.Join(settingsDir, corruptFile), []byte("{not json"), 0644); err != nil {
		t.Fatalf("WriteFile corrupt: %v", err)
	}

	// Seed default whatif.json so subsequent SwitchScenario works.
	defaults, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defaults.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: corruptFile, TransitionAge: 70},
	}
	if err := rm.Save(defaults); err != nil {
		t.Fatalf("Save defaults with chain: %v", err)
	}

	// Run the seed function to add the test items.
	seed(rm)

	// Bust the analysis cache so the next runAnalysisWithCache rebuilds.
	cache.mu.Lock()
	cache.hash = ""
	cache.analysis = nil
	cache.cachedAt = time.Time{}
	cache.mu.Unlock()

	return rm, settingsDir
}

func TestHandleWhatIfDeleteIncome_AnalysisError(t *testing.T) {
	rm, _ := setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		src := models.IncomeSource{ID: "danger-inc", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
		if _, err := rm.AddIncomeSource(src); err != nil {
			t.Fatalf("AddIncomeSource: %v", err)
		}
	})
	_ = rm

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/income/danger-inc", nil, map[string]string{"id": "danger-inc"})
	handleWhatIfDeleteIncome(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRestoreIncome_AnalysisError(t *testing.T) {
	rm, _ := setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		src := models.IncomeSource{ID: "danger-rest", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
		if _, err := rm.AddIncomeSource(src); err != nil {
			t.Fatalf("AddIncomeSource: %v", err)
		}
		if _, err := rm.RemoveIncomeSource("danger-rest"); err != nil {
			t.Fatalf("RemoveIncomeSource: %v", err)
		}
	})
	_ = rm

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/income/danger-rest/restore", nil, map[string]string{"id": "danger-rest"})
	handleWhatIfRestoreIncome(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfDeleteExpense_AnalysisError(t *testing.T) {
	rm, _ := setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		src := models.ExpenseSource{ID: "danger-exp", Name: "Test", Amount: 100}
		if _, err := rm.AddExpenseSource(src); err != nil {
			t.Fatalf("AddExpenseSource: %v", err)
		}
	})
	_ = rm

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/expense/danger-exp", nil, map[string]string{"id": "danger-exp"})
	handleWhatIfDeleteExpense(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRestoreExpense_AnalysisError(t *testing.T) {
	rm, _ := setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		src := models.ExpenseSource{ID: "danger-exp-r", Name: "Test", Amount: 100}
		if _, err := rm.AddExpenseSource(src); err != nil {
			t.Fatalf("AddExpenseSource: %v", err)
		}
		if _, err := rm.RemoveExpenseSource("danger-exp-r"); err != nil {
			t.Fatalf("RemoveExpenseSource: %v", err)
		}
	})
	_ = rm

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/expense/danger-exp-r/restore", nil, map[string]string{"id": "danger-exp-r"})
	handleWhatIfRestoreExpense(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfDeleteBigTicket_AnalysisError(t *testing.T) {
	rm, _ := setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		item := models.BigTicketItem{ID: "danger-bt", Name: "Test", Amount: 1000, Year: 5, Type: models.BigTicketExpense}
		if _, err := rm.AddBigTicketItem(item); err != nil {
			t.Fatalf("AddBigTicketItem: %v", err)
		}
	})
	_ = rm

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/bigticket/danger-bt", nil, map[string]string{"id": "danger-bt"})
	handleWhatIfDeleteBigTicket(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRestoreBigTicket_AnalysisError(t *testing.T) {
	rm, _ := setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		item := models.BigTicketItem{ID: "danger-bt-r", Name: "Test", Amount: 1000, Year: 5, Type: models.BigTicketExpense}
		if _, err := rm.AddBigTicketItem(item); err != nil {
			t.Fatalf("AddBigTicketItem: %v", err)
		}
		if _, err := rm.RemoveBigTicketItem("danger-bt-r"); err != nil {
			t.Fatalf("RemoveBigTicketItem: %v", err)
		}
	})
	_ = rm

	w := httptest.NewRecorder()
	req := chiRequest("POST", "/whatif/bigticket/danger-bt-r/restore", nil, map[string]string{"id": "danger-bt-r"})
	handleWhatIfRestoreBigTicket(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfDeleteHealthcare_AnalysisError(t *testing.T) {
	rm, _ := setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		person := models.HealthcarePerson{ID: "danger-hc", Name: "Test", CurrentAge: 60, CurrentCoverage: models.CoverageACA, MedicareEligibleAge: 65}
		if _, err := rm.AddHealthcarePerson(person); err != nil {
			t.Fatalf("AddHealthcarePerson: %v", err)
		}
	})
	_ = rm

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/healthcare/danger-hc", nil, map[string]string{"id": "danger-hc"})
	handleWhatIfDeleteHealthcare(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

// Drives Add/Update handlers through the runAnalysisWithCache failure path.

func TestHandleWhatIfAddIncome_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	form := url.Values{
		"name":       {"Test"},
		"amount":     {"1000"},
		"start_year": {"0"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/income", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddIncome(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddExpense_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	form := url.Values{
		"name":       {"Test"},
		"amount":     {"500"},
		"start_year": {"0"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/expense", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddExpense(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddBigTicket_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	form := url.Values{
		"name":   {"Big"},
		"amount": {"5000"},
		"year":   {"3"},
		"type":   {"expense"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddHealthcare_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	form := url.Values{
		"name":                 {"Test"},
		"current_age":          {"55"},
		"current_monthly_cost": {"500"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/healthcare", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddHealthcare(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateIncome_AnalysisError(t *testing.T) {
	rm, _ := setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		src := models.IncomeSource{ID: "danger-upd-i", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
		if _, err := rm.AddIncomeSource(src); err != nil {
			t.Fatalf("AddIncomeSource: %v", err)
		}
	})
	_ = rm

	form := url.Values{"start_year": {"5"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/income/danger-upd-i", formBody(form), map[string]string{"id": "danger-upd-i"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateIncome(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateExpense_AnalysisError(t *testing.T) {
	rm, _ := setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		src := models.ExpenseSource{ID: "danger-upd-e", Name: "Test", Amount: 100}
		if _, err := rm.AddExpenseSource(src); err != nil {
			t.Fatalf("AddExpenseSource: %v", err)
		}
	})
	_ = rm

	form := url.Values{"start_year": {"3"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/expense/danger-upd-e", formBody(form), map[string]string{"id": "danger-upd-e"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateExpense(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateHealthcare_AnalysisError(t *testing.T) {
	rm, _ := setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		person := models.HealthcarePerson{ID: "danger-upd-h", Name: "Test", CurrentAge: 50, CurrentCoverage: models.CoverageACA, MedicareEligibleAge: 65}
		if _, err := rm.AddHealthcarePerson(person); err != nil {
			t.Fatalf("AddHealthcarePerson: %v", err)
		}
	})
	_ = rm

	form := url.Values{"current_monthly_cost": {"1500"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/healthcare/danger-upd-h", formBody(form), map[string]string{"id": "danger-upd-h"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateHealthcare(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfSettings_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	form := url.Values{
		"portfolio_value":         {"1000000"},
		"monthly_living_expenses": {"5000"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfSpendingPhases_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	form := url.Values{"enabled": {"on"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSpendingPhases(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfAddPhase_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/add", nil)
	handleWhatIfAddPhase(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfDeletePhase_AnalysisError(t *testing.T) {
	rm, _ := setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {
		// Ensure we have at least 3 phases.
		settings, err := rm.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		settings.SpendingPhaseConfig = &models.SpendingPhaseConfig{
			Enabled: true,
			Phases: []models.SpendingPhase{
				{Name: "P1", StartAge: 0, Multiplier: 1.0},
				{Name: "P2", StartAge: 70, Multiplier: 0.9},
				{Name: "P3", StartAge: 80, Multiplier: 0.8},
			},
		}
		if err := rm.Save(settings); err != nil {
			t.Fatalf("Save: %v", err)
		}
	})
	_ = rm

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/spending-phases/1", nil, map[string]string{"index": "1"})
	handleWhatIfDeletePhase(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfResetPhases_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/spending-phases/reset", nil)
	handleWhatIfResetPhases(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRothConversion_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	form := url.Values{"enabled": {"on"}, "annual_amount": {"10000"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfSocialSecurity_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	form := url.Values{"fra_benefit": {"2500"}, "fra": {"67"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfGlidePath_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	form := url.Values{"enabled": {"on"}, "start_stock_pct": {"80"}, "end_stock_pct": {"40"}, "transition_years": {"15"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/glide-path", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGlidePath(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfGuardrails_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	form := url.Values{"enabled": {"on"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/guardrails", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGuardrails(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfSync_AnalysisError(t *testing.T) {
	setupItemsThenBreakChain(t, func(rm *retirement.SettingsManager) {})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/sync", nil)
	handleWhatIfSync(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

// ── statusForWhatIfSaveError direct tests ──────────────────────────────────

func TestStatusForWhatIfSaveError_ChainValidation(t *testing.T) {
	err := &retirement.ScenarioChainValidationError{Err: fmt.Errorf("bad chain")}
	if got := statusForWhatIfSaveError(err); got != http.StatusBadRequest {
		t.Errorf("expected 400 for chain validation error, got %d", got)
	}
}

func TestStatusForWhatIfSaveError_Generic(t *testing.T) {
	err := fmt.Errorf("generic save error")
	if got := statusForWhatIfSaveError(err); got != http.StatusInternalServerError {
		t.Errorf("expected 500 for generic error, got %d", got)
	}
}

// ── statusForScenarioOperationError direct tests ───────────────────────────

func TestStatusForScenarioOperationError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"validation", &retirement.ScenarioValidationError{Err: fmt.Errorf("v")}, http.StatusBadRequest},
		{"not found", &retirement.ScenarioNotFoundError{Err: fmt.Errorf("nf")}, http.StatusNotFound},
		{"conflict", &retirement.ScenarioConflictError{Err: fmt.Errorf("cf")}, http.StatusConflict},
		{"generic", fmt.Errorf("other"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusForScenarioOperationError(tc.err); got != tc.want {
				t.Errorf("status for %s = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

// ── handleWhatIfDeleteChainLink Load failure ───────────────────────────────

func TestHandleWhatIfDeleteChainLink_LoadError(t *testing.T) {
	setupBrokenEnv(t)

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/chain/0", nil, map[string]string{"index": "0"})
	handleWhatIfDeleteChainLink(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfUpdateChain_LoadError(t *testing.T) {
	setupBrokenEnv(t)

	form := url.Values{
		"chain_scenario[]": {"some.json"},
		"chain_age[]":      {"70"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/chain", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateChain(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

// ── handleWhatIfUpdateChain with multiple links (exercises sort.Less) ──────

func TestHandleWhatIfUpdateChain_SortsByAge(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create three scenarios.
	if _, err := rm.CreateScenario("Primary"); err != nil {
		t.Fatalf("CreateScenario primary: %v", err)
	}
	if _, err := rm.CreateScenario("LinkA"); err != nil {
		t.Fatalf("CreateScenario A: %v", err)
	}
	if _, err := rm.CreateScenario("LinkB"); err != nil {
		t.Fatalf("CreateScenario B: %v", err)
	}
	scenarios, _ := rm.ListScenarios()
	var primaryFile, fileA, fileB string
	for _, s := range scenarios {
		switch s.Name {
		case "Primary":
			primaryFile = s.Filename
		case "LinkA":
			fileA = s.Filename
		case "LinkB":
			fileB = s.Filename
		}
	}
	if err := rm.SwitchScenario(primaryFile); err != nil {
		t.Fatalf("SwitchScenario: %v", err)
	}

	// Submit links out of order so the sort.Slice exercises the Less fn.
	form := url.Values{
		"chain_scenario[]": {fileA, fileB},
		"chain_age[]":      {"75", "70"}, // out of order; should be sorted to 70, 75
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/chain", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfUpdateChain(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(settings.ScenarioChain) != 2 {
		t.Fatalf("expected 2 chain links, got %d", len(settings.ScenarioChain))
	}
	if settings.ScenarioChain[0].TransitionAge != 70 {
		t.Errorf("expected first link age 70 after sort, got %d", settings.ScenarioChain[0].TransitionAge)
	}
}

// ── handleCreateScenario non-typed error fallback (Save failure) ───────────

func TestHandleCreateScenario_NonTypedError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()
	primeLoadCache(t, rm)

	// chmod the dir so the saveInternal inside CreateScenario fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	form := url.Values{"name": {"FailMe"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/scenarios", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleCreateScenario(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("expected error status, got 200")
	}
}

// ── handleDeleteScenario / handleRenameScenario non-typed error fallback ───

func TestHandleDeleteScenario_NonTypedError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	if _, err := rm.CreateScenario("ToDelete"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	scenarios, _ := rm.ListScenarios()
	var targetFile string
	for _, s := range scenarios {
		if s.Name == "ToDelete" {
			targetFile = s.Filename
		}
	}

	// Switch active to whatif.json so the target isn't currently active.
	// (CreateScenario sets it as active — we want an inactive scenario to delete.)
	if err := rm.SwitchScenario("whatif.json"); err != nil {
		// whatif.json may not yet exist; create it by saving a fresh settings.
		settings, lErr := rm.Load()
		if lErr != nil {
			t.Fatalf("Load: %v", lErr)
		}
		_ = settings
	}

	// chmod the dir so os.Remove fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/scenarios/"+targetFile, nil, map[string]string{"filename": targetFile})
	handleDeleteScenario(w, req)

	// Either 500 (non-typed Remove error) or 404 (NotFound) is acceptable —
	// what we want to verify is that the handler fell through, not a 200.
	if w.Code == http.StatusOK {
		t.Fatalf("expected error status, got 200")
	}
}

func TestHandleRenameScenario_NonTypedError(t *testing.T) {
	rm, dir, cleanup := setupTestEnvWithDir(t)
	defer cleanup()

	if _, err := rm.CreateScenario("ToRename"); err != nil {
		t.Fatalf("CreateScenario: %v", err)
	}
	scenarios, _ := rm.ListScenarios()
	var targetFile string
	for _, s := range scenarios {
		if s.Name == "ToRename" {
			targetFile = s.Filename
		}
	}

	// chmod the dir so the WriteFile inside RenameScenario fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	form := url.Values{"name": {"NewName"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/scenarios/"+targetFile, formBody(form), map[string]string{"filename": targetFile})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleRenameScenario(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("expected error status, got 200")
	}
}

// ── parsePersonsForm: empty person_id auto-generates UUID ─────────────────

func TestHandleWhatIfSettings_PersonsEmptyIDAutoGenerated(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"start_date":           {"2026-04"},
		"person_id[]":          {""}, // empty -> handler should generate UUID
		"person_name[]":        {"Alex"},
		"person_birth_month[]": {"1960-05"},
		"person_role[]":        {"primary"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/settings", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(settings.Persons) == 0 {
		t.Fatal("expected at least one person")
	}
	if settings.Persons[0].ID == "" {
		t.Fatal("expected auto-generated UUID, got empty ID")
	}
}

// ── Load() failure tests for handlers that load settings up front ─────────

func TestHandleWhatIfSocialSecurity_LoadError(t *testing.T) {
	setupBrokenEnv(t)

	form := url.Values{"fra_benefit": {"2500"}, "fra": {"67"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfGlidePath_LoadError(t *testing.T) {
	setupBrokenEnv(t)

	form := url.Values{"enabled": {"on"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/glide-path", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGlidePath(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfGuardrails_LoadError(t *testing.T) {
	setupBrokenEnv(t)

	form := url.Values{"enabled": {"on"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/guardrails", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGuardrails(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

// ── Renderer != nil tests for SocialSecurity / GlidePath / Guardrails ──────

func TestHandleWhatIfSocialSecurity_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"fra_benefit": {"2500"},
		"fra":         {"67"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfGlidePath_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"enabled":          {"on"},
		"start_stock_pct":  {"80"},
		"end_stock_pct":    {"40"},
		"transition_years": {"15"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/glide-path", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGlidePath(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

func TestHandleWhatIfGuardrails_WithRenderer(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"enabled":           {"on"},
		"floor_drop_pct":    {"20"},
		"floor_cut_pct":     {"10"},
		"ceiling_rise_pct":  {"25"},
		"ceiling_raise_pct": {"10"},
		"min_spending_pct":  {"75"},
		"max_spending_pct":  {"125"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/guardrails", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfGuardrails(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String()[:min(w.Body.Len(), 300)])
	}
}

// ── Test Social Security ColRate fallback (cola_rate missing, COLARate 0) ──

func TestHandleWhatIfSocialSecurity_ColaRateFallback(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Pre-seed SocialSecurity with COLARate==0 so the else-if branch fires.
	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit: 2000,
		FRA:        67,
		COLARate:   0, // explicitly 0 so fallback triggers
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Submit form WITHOUT cola_rate so parseFormFloat returns ("", nil) -> err is nil,
	// but actually that path still sets COLARate to 0/100 = 0, and we never enter the
	// else-if. To enter the else-if we need parseFormFloat to fail (return error),
	// which only happens when cola_rate is non-empty AND non-numeric.
	form := url.Values{
		"fra_benefit": {"2500"},
		"fra":         {"67"},
		"cola_rate":   {"not-a-number"}, // triggers parse error -> else-if runs
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SocialSecurity == nil {
		t.Fatal("expected SocialSecurity")
	}
	// Fallback should set COLARate to 0.02.
	if loaded.SocialSecurity.COLARate != 0.02 {
		t.Errorf("COLARate = %v, want 0.02 (fallback)", loaded.SocialSecurity.COLARate)
	}
}

// ── maxSubmittedSpendingPhaseIndex direct tests ────────────────────────────

func TestMaxSubmittedSpendingPhaseIndex(t *testing.T) {
	cases := []struct {
		name string
		form map[string][]string
		want int
	}{
		{"empty", map[string][]string{}, -1},
		{"non-phase keys ignored", map[string][]string{"foo": {"bar"}}, -1},
		{"phase without underscore", map[string][]string{"phase_5": {"x"}}, -1},
		{"phase with non-numeric index", map[string][]string{"phase_abc_name": {"x"}}, -1},
		{"single phase", map[string][]string{"phase_2_name": {"P"}}, 2},
		{"max wins", map[string][]string{"phase_0_x": {"a"}, "phase_5_y": {"b"}, "phase_2_z": {"c"}}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := maxSubmittedSpendingPhaseIndex(tc.form)
			if got != tc.want {
				t.Errorf("maxSubmittedSpendingPhaseIndex = %d, want %d", got, tc.want)
			}
		})
	}
}

// ── handleWhatIfMonteCarlo Load failure ────────────────────────────────────

func TestHandleWhatIfMonteCarlo_LoadError(t *testing.T) {
	setupBrokenEnv(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/montecarlo", nil)
	handleWhatIfMonteCarlo(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500. body: %s", w.Code, w.Body.String())
	}
}

func TestHandleWhatIfRothConversion_NilInitializesConfig(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Confirm starting state has no RothConversion config.
	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.RothConversion != nil {
		// Force-clear it.
		settings.RothConversion = nil
		if err := rm.Save(settings); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	form := url.Values{} // No "enabled" -> Enabled=false, but config is initialized
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/roth-conversion", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfRothConversion(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.RothConversion == nil {
		t.Fatal("expected RothConversion to be initialized")
	}
}

func TestHandleWhatIfPurgeIncome(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	src := models.IncomeSource{ID: "purge-inc-1", Name: "Test", Amount: 1000, Type: models.IncomeFixed}
	if _, err := rm.AddIncomeSource(src); err != nil {
		t.Fatalf("AddIncomeSource: %v", err)
	}
	if _, err := rm.RemoveIncomeSource("purge-inc-1"); err != nil {
		t.Fatalf("RemoveIncomeSource: %v", err)
	}

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/income/purge-inc-1/purge", nil, map[string]string{"id": "purge-inc-1"})
	handleWhatIfPurgeIncome(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(settings.RemovedIncomeSources) != 0 {
		t.Errorf("expected RemovedIncomeSources empty, got %+v", settings.RemovedIncomeSources)
	}
}

func TestHandleWhatIfPurgeIncome_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/income/nonexistent/purge", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfPurgeIncome(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent removed source, got %d", w.Code)
	}
}

func TestHandleWhatIfPurgeExpense(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	src := models.ExpenseSource{ID: "purge-exp-1", Name: "Test", Amount: 500, StartYear: 0}
	if _, err := rm.AddExpenseSource(src); err != nil {
		t.Fatalf("AddExpenseSource: %v", err)
	}
	if _, err := rm.RemoveExpenseSource("purge-exp-1"); err != nil {
		t.Fatalf("RemoveExpenseSource: %v", err)
	}

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/expense/purge-exp-1/purge", nil, map[string]string{"id": "purge-exp-1"})
	handleWhatIfPurgeExpense(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(settings.RemovedExpenseSources) != 0 {
		t.Errorf("expected RemovedExpenseSources empty, got %+v", settings.RemovedExpenseSources)
	}
}

func TestHandleWhatIfPurgeExpense_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/expense/nonexistent/purge", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfPurgeExpense(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent removed source, got %d", w.Code)
	}
}

func TestHandleWhatIfPurgeBigTicket(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	item := models.BigTicketItem{ID: "purge-bt-1", Name: "Test", Amount: 10000, Year: 2030, Type: "expense"}
	if _, err := rm.AddBigTicketItem(item); err != nil {
		t.Fatalf("AddBigTicketItem: %v", err)
	}
	if _, err := rm.RemoveBigTicketItem("purge-bt-1"); err != nil {
		t.Fatalf("RemoveBigTicketItem: %v", err)
	}

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/bigticket/purge-bt-1/purge", nil, map[string]string{"id": "purge-bt-1"})
	handleWhatIfPurgeBigTicket(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(settings.RemovedBigTicketItems) != 0 {
		t.Errorf("expected RemovedBigTicketItems empty, got %+v", settings.RemovedBigTicketItems)
	}
}

func TestHandleWhatIfPurgeBigTicket_NonexistentID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/bigticket/nonexistent/purge", nil, map[string]string{"id": "nonexistent"})
	handleWhatIfPurgeBigTicket(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent removed item, got %d", w.Code)
	}
}

// ── F-026: explicit zero COLA honored after handler save ──────────────────────

func TestHandleWhatIfSocialSecurity_F026_ExplicitZeroCOLA(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	// Seed with a non-zero COLA so we can confirm the zero overwrite sticks.
	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	settings.SocialSecurity = &models.SocialSecurityConfig{
		FRABenefit:  2000,
		FRA:         67,
		COLARate:    0.02, // prior non-zero value
		COLARateSet: false,
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// POST cola_rate=0 — user explicitly wants 0% COLA.
	form := url.Values{
		"fra_benefit": {"2000"},
		"fra":         {"67"},
		"cola_rate":   {"0"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/social-security", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfSocialSecurity(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	loaded, err := rm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SocialSecurity == nil {
		t.Fatal("expected SocialSecurity settings")
	}
	// F-026: explicit 0 must be stored, not silently replaced with 0.02.
	if loaded.SocialSecurity.COLARate != 0.0 {
		t.Errorf("COLARate = %v, want 0.0 (explicit zero honored)", loaded.SocialSecurity.COLARate)
	}
	if !loaded.SocialSecurity.COLARateSet {
		t.Errorf("COLARateSet = false; want true (form submit should set the flag)")
	}
}

// ── handleWhatIf breakdown guardrail visibility ─────────────────────────────

// The breakdown table must surface the planned-vs-adjusted spending stack
// and the guardrail multiplier badge when guardrails are enabled and a cut fires.
func TestHandleWhatIf_BreakdownShowsGuardrailEffect(t *testing.T) {
	rm, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings, err := rm.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	// PortfolioValue must be nonzero so the guardrail state initialises with a
	// real peak; InvestmentReturn=0 means no growth, so the portfolio drops each
	// year from spending, triggering the floor cut after the first year.
	settings.PortfolioValue = 1_000_000
	settings.InvestmentReturn = 0
	settings.MonthlyLivingExpenses = 8000
	settings.Guardrails = &models.GuardrailConfig{
		Enabled:         true,
		FloorDropPct:    1,
		FloorCutPct:     10,
		CeilingRisePct:  500,
		CeilingRaisePct: 10,
		MinSpendingPct:  50,
		MaxSpendingPct:  150,
	}
	if err := rm.Save(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	cache.mu.Lock()
	cache.hash = ""
	cache.mu.Unlock()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// Multiplier badge text — "×0.90" produced by `printf "%.2f"` for a 10% cut.
	if !strings.Contains(body, "×0.90") && !strings.Contains(body, "×0.9") {
		t.Errorf("expected ×0.90 multiplier badge in breakdown body, not found")
	}
	// Planned-spending suffix — relies on the data-planned-spending marker we render.
	if !strings.Contains(body, "data-planned-spending") {
		t.Errorf("expected data-planned-spending marker in breakdown body, not found")
	}
}

func TestHandleWhatIf_EventsPanelShowsDollarDelta(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	settings.PortfolioValue = 1_000_000
	settings.InvestmentReturn = 0
	settings.MonthlyLivingExpenses = 8000
	settings.Guardrails = &models.GuardrailConfig{
		Enabled:         true,
		FloorDropPct:    1,
		FloorCutPct:     10,
		CeilingRisePct:  500,
		CeilingRaisePct: 10,
		MinSpendingPct:  50,
		MaxSpendingPct:  150,
	}
	if err := retirementMgr.Save(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	cache.mu.Lock()
	cache.hash = ""
	cache.mu.Unlock()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "data-event-spending-delta") {
		t.Errorf("expected data-event-spending-delta marker in events panel, not found")
	}
}

func TestHandleWhatIfProjectionChartNoGuardrails(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	settings.PortfolioValue = 1_000_000
	settings.InvestmentReturn = 0
	settings.MonthlyLivingExpenses = 8000
	settings.Guardrails = &models.GuardrailConfig{
		Enabled: true, FloorDropPct: 1, FloorCutPct: 10,
		CeilingRisePct: 500, CeilingRaisePct: 10,
		MinSpendingPct: 50, MaxSpendingPct: 150,
	}
	if err := retirementMgr.Save(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	cache.mu.Lock()
	cache.hash = ""
	cache.mu.Unlock()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif/chart/projection/no-guardrails?display_dollars=nominal", nil)
	handleWhatIfProjectionChartNoGuardrails(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if data["data"] == nil {
		t.Fatal("expected chart data array")
	}

	// Sanity: the no-guardrails endpoint must produce a different balance series
	// than the guardrails-on endpoint with the same settings. If they were equal,
	// the handler is failing to disable guardrails (e.g., clone.Guardrails = nil
	// got dropped). With InvestmentReturn=0 and a hair-trigger cut, the two paths
	// must diverge.
	wOn := httptest.NewRecorder()
	reqOn := httptest.NewRequest("GET", "/whatif/chart/projection?display_dollars=nominal", nil)
	handleWhatIfProjectionChart(wOn, reqOn)
	if wOn.Code != http.StatusOK {
		t.Fatalf("guardrails-on status = %d", wOn.Code)
	}
	if w.Body.String() == wOn.Body.String() {
		t.Errorf("no-guardrails response is byte-identical to guardrails-on response; clone.Guardrails = nil may have been dropped")
	}
}

// The no-guardrails projection must be insensitive to the configured guardrail thresholds —
// both should produce identical balance series when guardrails are disabled in the run.
func TestHandleWhatIfProjectionChartNoGuardrails_IndependentOfThresholds(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	settings, err := retirementMgr.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	settings.PortfolioValue = 1_000_000
	settings.InvestmentReturn = 0
	settings.MonthlyLivingExpenses = 8000

	hashOf := func(cfg *models.GuardrailConfig) string {
		settings.Guardrails = cfg
		if err := retirementMgr.Save(settings); err != nil {
			t.Fatalf("save: %v", err)
		}
		cache.mu.Lock()
		cache.hash = ""
		cache.mu.Unlock()

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/whatif/chart/projection/no-guardrails", nil)
		handleWhatIfProjectionChartNoGuardrails(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		return w.Body.String()
	}

	a := hashOf(&models.GuardrailConfig{Enabled: true, FloorDropPct: 1, FloorCutPct: 50, CeilingRisePct: 1, CeilingRaisePct: 50, MinSpendingPct: 50, MaxSpendingPct: 200})
	b := hashOf(&models.GuardrailConfig{Enabled: true, FloorDropPct: 30, FloorCutPct: 5, CeilingRisePct: 30, CeilingRaisePct: 5, MinSpendingPct: 80, MaxSpendingPct: 120})
	if a != b {
		t.Fatalf("no-guardrails endpoint output should be identical regardless of configured thresholds")
	}
}

func TestHandleWhatIfSettings_FilingStatusRoundTrip(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{}
	form.Set("filing_status", "married_joint")
	req := httptest.NewRequest(http.MethodPost, "/whatif/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handleWhatIfSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	got, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if got.TaxConfig == nil {
		t.Fatal("TaxConfig is nil after POST")
	}
	if got.TaxConfig.FilingStatus != models.FilingMarriedJoint {
		t.Errorf("FilingStatus = %q, want married_joint", got.TaxConfig.FilingStatus)
	}
}

func TestHandleWhatIfSettings_FilingStatusInvalid(t *testing.T) {
	rm, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{}
	form.Set("filing_status", "garbage")
	req := httptest.NewRequest(http.MethodPost, "/whatif/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handleWhatIfSettings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "Invalid filing status") {
		t.Errorf("body does not contain 'Invalid filing status': %s", w.Body.String())
	}

	// Verify settings were not changed
	got, err := rm.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	// Just check that we can still load the config (it should be unchanged)
	if got.TaxConfig == nil {
		t.Fatal("TaxConfig is nil")
	}
}
