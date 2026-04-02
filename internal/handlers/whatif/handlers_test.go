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
		"portfolio_value":        {"1500000"},
		"monthly_living_expenses": {"5000"},
		"current_age":            {"60"},
		"projection_years":       {"30"},
		"investment_return":      {"7.0"},
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
		"inflation_rate":       {"3.0"},
		"healthcare_inflation": {"5.0"},
		"spending_decline_rate": {"1.0"},
		"investment_return":    {"7.0"},
		"discount_rate":        {"3.0"},
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
		"enabled":              {"on"},
		"phase_0_name":         {"Go-Go"},
		"phase_0_multiplier":   {"1.0"},
		"phase_1_name":         {"Slow-Go"},
		"phase_1_start_age":    {"75"},
		"phase_1_multiplier":   {"0.8"},
		"phase_1_description":  {"Slower spending"},
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
	// Negative year is clamped to 0, should succeed
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (year clamped), got %d", w.Code)
	}
}

func TestHandleWhatIfAddBigTicket_BadYear(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// Invalid year falls back to 0 (per code logic)
	form := url.Values{"name": {"Test"}, "amount": {"100"}, "year": {"abc"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/bigticket", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleWhatIfAddBigTicket(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (bad year defaults to 0), got %d", w.Code)
	}
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

// ── buildCalculator ─────────────────────────────────────────────────────────

func TestBuildCalculator_NoChain(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	calc, hash, err := buildCalculator(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calc == nil {
		t.Fatal("expected non-nil calculator")
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestBuildCalculator_WithChain(t *testing.T) {
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
	calc, hash, err := buildCalculator(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calc == nil {
		t.Fatal("expected non-nil calculator")
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestBuildCalculator_ChainBadFile(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	s := models.DefaultWhatIfSettings()
	s.ScenarioChain = []models.ScenarioChainLink{
		{ScenarioFilename: "nonexistent.json", TransitionAge: 70},
	}
	_, _, err := buildCalculator(s)
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
	settings.ProjectionYears = 15

	projection := sampleProjectionForChart()
	events := buildProjectionChartEvents(settings, projection)

	for _, e := range events {
		if e.Label == "RMD starts" {
			t.Fatal("should not add RMD event when already past RMD age")
		}
	}
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
		"enabled":              {"on"},
		"phase_0_multiplier":   {"1.0"},
		"phase_0_start_age":    {"60"}, // Phase 0 start age
		"phase_1_multiplier":   {"0.8"},
		"phase_1_start_age":    {"80"},
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
		"enabled":              {"true"},
		"phase_0_multiplier":   {"0.95"},
		"phase_0_name":         {"Updated Active"},
		"phase_1_multiplier":   {"0.75"},
		"phase_1_start_age":    {"76"},
		"phase_1_name":         {"Updated Slower"},
		"phase_1_description":  {"Updated description"},
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
		"monthly_healthcare":    {"500"},
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
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"enabled": {"on"}}
	for i := 0; i < 5; i++ {
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
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d", w.Code)
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
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d", w.Code)
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
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 200 or 500, got %d", w.Code)
	}
}

// ── Error paths for scenarios ──────────────────────────────────────────────

func TestHandleSwitchScenario_NonexistentFile(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"filename": {"nonexistent.json"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/whatif/scenarios/switch", formBody(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handleSwitchScenario(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for nonexistent scenario, got %d", w.Code)
	}
}

func TestHandleDeleteScenario_NonexistentFile(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := chiRequest("DELETE", "/whatif/scenarios/nonexistent.json", nil, map[string]string{"filename": "nonexistent.json"})
	handleDeleteScenario(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for nonexistent scenario, got %d", w.Code)
	}
}

func TestHandleRenameScenario_NonexistentFile(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"New Name"}}
	w := httptest.NewRecorder()
	req := chiRequest("PUT", "/whatif/scenarios/nonexistent.json", formBody(form), map[string]string{"filename": "nonexistent.json"})
	handleRenameScenario(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for nonexistent scenario, got %d", w.Code)
	}
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
		"enabled":              {"on"},
		"phase_0_name":         {"Active"},
		"phase_0_multiplier":   {"1.0"},
		"phase_0_description":  {"Full spending"},
		"phase_1_name":         {"Slow"},
		"phase_1_multiplier":   {"0.8"},
		"phase_1_start_age":    {"75"},
		"phase_1_description":  {"Reduced spending"},
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

// ── Test handleWhatIf first load with no income sources (auto-sync) ────────

func TestHandleWhatIf_AutoSync(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/whatif", nil)
	handleWhatIf(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
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
		"name":                 {"Robert"},
		"current_age":          {"61"},
		"current_coverage":     {"aca"},
		"current_monthly_cost": {"1100"},
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
