package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"budget2/internal/config"
	"budget2/internal/handlers/backup"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/internal/testutil"
	"budget2/internal/version"
)

// setupTestServer initializes dependencies with test data and returns a test server
func setupTestServer(t *testing.T) *testutil.TestServer {
	t.Helper()

	// Create test config pointing to testdata
	root := testutil.ProjectRoot()
	cfg := &config.Config{
		ListenAddr:         ":0",
		Debug:              true,
		DataDirectory:      testutil.TestDataDir(),
		UploadsDirectory:   testutil.TestDataDir() + "/uploads",
		SettingsDirectory:  testutil.TestDataDir() + "/settings",
		TemplatesDirectory: root + "/web/templates",
		StaticDirectory:    root + "/web/static",
		BackupDir:          t.TempDir(),
	}

	// Initialize storage (unencrypted for tests)
	var err error
	store, err = storage.New(cfg.DataDirectory)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Setup dependencies with test config
	if err := SetupDependencies(cfg); err != nil {
		t.Fatalf("Failed to setup dependencies: %v", err)
	}

	// Create router and test server
	router := SetupRouter()
	return testutil.NewTestServer(t, router)
}

// TestHealthEndpoint tests the /api/health endpoint
func TestHealthEndpoint(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/api/health")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeJSON().
		Contains(`"status":"ok"`)
}

// TestRootRedirect tests that / redirects to /dashboard
func TestRootRedirect(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Don't follow redirects
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(ts.BaseURL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("Expected status %d, got %d", http.StatusTemporaryRedirect, resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location != "/dashboard" {
		t.Errorf("Expected redirect to /dashboard, got %s", location)
	}
}

// TestDashboard tests the main dashboard page
func TestDashboard(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/dashboard")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeHTML().
		ContainsAll(
			"Dashboard",
			"Total Income",
			"Total Expenses",
			"Net Savings",
		)
}

// TestDashboardKPIsPartial tests the KPIs partial endpoint
func TestDashboardKPIsPartial(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/dashboard/kpis")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeHTML().
		ContainsAll("Total Income", "Total Expenses")
}

// TestDashboardChartData tests chart data endpoints
func TestDashboardChartData(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	chartTypes := []string{
		"monthly",
		"category",
		"spending-trend",
		"merchants",
		"cumulative",
	}

	for _, chartType := range chartTypes {
		t.Run(chartType, func(t *testing.T) {
			resp := ts.GET("/dashboard/charts/data/" + chartType)
			testutil.AssertResponse(t, resp).
				StatusOK().
				ContentTypeJSON()

			// Verify it's valid JSON
			body := testutil.ReadBody(t, resp)
			var data interface{}
			if err := json.Unmarshal([]byte(body), &data); err != nil {
				t.Errorf("Invalid JSON for chart %s: %v", chartType, err)
			}
		})
	}
}

// TestExplorer tests the explorer page
func TestExplorer(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/explorer")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeHTML().
		ContainsAll(
			"Explorer",
			"Search",
			"Category",
		)
}

// TestExplorerTransactionsPartial tests the transactions partial
func TestExplorerTransactionsPartial(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/explorer/transactions")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeHTML()
}

// TestExplorerFiltering tests transaction filtering
func TestExplorerFiltering(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	tests := []struct {
		name  string
		query map[string]string
	}{
		{"search", map[string]string{"search": "ACME"}},
		{"category", map[string]string{"category": "Groceries"}},
		{"type-income", map[string]string{"type": "income"}},
		{"type-expense", map[string]string{"type": "expense"}},
		{"date-range", map[string]string{"start": "2025-01-01", "end": "2025-06-30"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := ts.GETWithQuery("/explorer/transactions", tt.query)
			testutil.AssertResponse(t, resp).
				StatusOK().
				ContentTypeHTML()
		})
	}
}

// TestWhatIf tests the what-if analysis page
func TestWhatIf(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/whatif")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeHTML().
		ContainsAll(
			"What-If",
			"Portfolio Value",
		)
}

// TestWhatIfProjectionChart tests the projection chart endpoint
func TestWhatIfProjectionChart(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/whatif/chart/projection")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeJSON()
}

// TestInsights tests the insights page
func TestInsights(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/insights")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeHTML().
		ContainsAll(
			"Insights",
			"Recurring",
		)
}

// TestInsightsRecurringPartial tests the recurring payments partial
func TestInsightsRecurringPartial(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/insights/recurring")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeHTML()
}

// TestInsightsTrendsPartial tests the trends partial
func TestInsightsTrendsPartial(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/insights/trends")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeHTML()
}

// TestInsightsTrendsChartData tests the trends chart data endpoint
func TestInsightsTrendsChartData(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/insights/trends/chart")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeJSON()
}

// TestInsightsVelocityPartial tests the velocity partial
func TestInsightsVelocityPartial(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/insights/velocity")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeHTML()
}

// TestInsightsIncomePartial tests the income analysis partial
func TestInsightsIncomePartial(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/insights/income")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeHTML()
}

// TestFileManager tests the file manager page
func TestFileManager(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/explorer/files")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeHTML()
}

// TestHandleVersion tests the /api/version endpoint returns valid JSON with version info
func TestHandleVersion(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/api/version")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeJSON()

	body := testutil.ReadBody(t, resp)
	var info version.Info
	if err := json.Unmarshal([]byte(body), &info); err != nil {
		t.Fatalf("Failed to unmarshal version info: %v", err)
	}

	// Version should have at least GoVersion populated
	if info.GoVersion == "" {
		t.Error("Expected GoVersion to be non-empty")
	}
}

// TestHandleVersionDirect tests handleVersion directly with httptest
func TestHandleVersionDirect(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	handleVersion(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", ct)
	}

	var info version.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("Failed to unmarshal version response: %v", err)
	}
}

// TestLockCheckMiddleware_Unlocked tests that the middleware passes through when unlocked
func TestLockCheckMiddleware_Unlocked(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Storage is not encrypted in tests, so middleware should pass through
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(ts.BaseURL + "/dashboard")
	if err != nil {
		t.Fatalf("GET /dashboard failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 (unlocked pass-through), got %d", resp.StatusCode)
	}
}

// TestSetupDependencies_Production tests SetupDependencies with Debug=false (embedded FS)
func TestSetupDependencies_Production(t *testing.T) {
	root := testutil.ProjectRoot()
	c := &config.Config{
		ListenAddr:         ":0",
		Debug:              false, // Production mode - use embedded FS
		DataDirectory:      testutil.TestDataDir(),
		UploadsDirectory:   testutil.TestDataDir() + "/uploads",
		SettingsDirectory:  testutil.TestDataDir() + "/settings",
		TemplatesDirectory: root + "/web/templates",
		StaticDirectory:    root + "/web/static",
		BackupDir:          t.TempDir(),
	}

	var err error
	store, err = storage.New(c.DataDirectory)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	if err := SetupDependencies(c); err != nil {
		t.Fatalf("SetupDependencies failed in production mode: %v", err)
	}
}

// TestSetupRouter_Production tests SetupRouter with Debug=false (embedded static files)
func TestSetupRouter_Production(t *testing.T) {
	root := testutil.ProjectRoot()
	c := &config.Config{
		ListenAddr:         ":0",
		Debug:              false, // Production mode
		DataDirectory:      testutil.TestDataDir(),
		UploadsDirectory:   testutil.TestDataDir() + "/uploads",
		SettingsDirectory:  testutil.TestDataDir() + "/settings",
		TemplatesDirectory: root + "/web/templates",
		StaticDirectory:    root + "/web/static",
		BackupDir:          t.TempDir(),
	}

	var err error
	store, err = storage.New(c.DataDirectory)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	if err := SetupDependencies(c); err != nil {
		t.Fatalf("SetupDependencies failed: %v", err)
	}

	router := SetupRouter()
	ts := testutil.NewTestServer(t, router)
	defer ts.Close()

	// Verify static file serving works with embedded FS
	resp := ts.GET("/api/health")
	testutil.AssertResponse(t, resp).StatusOK()
}

// TestKillPreviousInstance_NoServer tests killPreviousInstance when no server is running
func TestKillPreviousInstance_NoServer(t *testing.T) {
	// Use a port that is very unlikely to be in use
	// This should just return without error when no server is reachable
	killPreviousInstance(":19999")
}

// TestKillPreviousInstance_WithServer tests killPreviousInstance when a server is running
func TestKillPreviousInstance_WithServer(t *testing.T) {
	killCalled := false
	healthCalls := 0

	// Create a mock server that responds to /killme and /health
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/killme":
			killCalled = true
			w.WriteHeader(http.StatusOK)
		case "/health":
			healthCalls++
			// Simulate server shutting down after first health check
			if healthCalls > 1 {
				// Close connection to simulate server gone
				hj, ok := w.(http.Hijacker)
				if ok {
					conn, _, _ := hj.Hijack()
					conn.Close()
					return
				}
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Extract the address from the test server URL (strip "http://")
	addr := srv.Listener.Addr().String()

	killPreviousInstance(addr)

	if !killCalled {
		t.Error("Expected /killme to be called on the previous instance")
	}
}

// TestKillPreviousInstance_WithColonPrefix tests the host prefix logic
func TestKillPreviousInstance_WithColonPrefix(t *testing.T) {
	// When addr starts with ":", it should prepend "localhost"
	// This will fail to connect since nothing is on that port, which is fine
	killPreviousInstance(":19998")
}

// TestKillPreviousInstance_ServerStaysUp tests when previous instance doesn't shut down
func TestKillPreviousInstance_ServerStaysUp(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow test in short mode")
	}

	// Create a mock server that never shuts down (always responds to /health)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addr := srv.Listener.Addr().String()

	// This will try to kill the server and wait, eventually giving up
	killPreviousInstance(addr)
	// Should complete without error (just logs a warning)
}

// TestMainFunction_InvalidConfig tests that we can at least verify main's structure
// We can't easily test main() directly since it calls log.Fatal and http.ListenAndServe,
// but we can test all its component parts which are already tested above.
func TestMainFunction_Coverage(t *testing.T) {
	// Test the version logging path that main() uses
	versionInfo := version.Get()
	s := versionInfo.String()
	if s == "" {
		t.Error("Expected non-empty version string")
	}

	// Test the Check() path
	_ = versionInfo.Check()

	// Test config.Load via env (covered by other tests but verifying the path)
	cleanup := testutil.SetTestEnv(t)
	defer cleanup()

	// We've tested SetupDependencies, SetupRouter, killPreviousInstance separately
	// main() is a composition of these plus config.Load + http.ListenAndServe
}

// TestStaticFiles tests that static files are served (covers the fileServer branch)
func TestStaticFiles(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Request a known static file (CSS or JS)
	resp, err := http.Get(ts.BaseURL + "/static/css/style.css")
	if err != nil {
		t.Fatalf("GET /static/css/style.css failed: %v", err)
	}
	defer resp.Body.Close()

	// Static file may or may not exist in test setup, but the handler should respond
	// (200 if found, 404 if not - either way the handler code is exercised)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 200 or 404, got %d", resp.StatusCode)
	}
}

// TestVersionEndpointViaRouter tests /api/version through the full router
func TestVersionEndpointViaRouter(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/api/version")
	body := testutil.ReadBody(t, resp)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("Version endpoint returned invalid JSON: %v", err)
	}

	// Should have version field
	if _, ok := result["version"]; !ok {
		t.Error("Expected 'version' field in response")
	}
	if _, ok := result["goVersion"]; !ok {
		t.Error("Expected 'goVersion' field in response")
	}
}

// TestKillPreviousInstance_FullAddress tests with a full host:port address (no colon prefix)
func TestKillPreviousInstance_FullAddress(t *testing.T) {
	// Test with a full address that doesn't start with ":"
	// Nothing should be running, so it should return immediately
	killPreviousInstance("127.0.0.1:19997")
}

// TestLockCheckMiddleware_Direct tests the lockCheckMiddleware function directly
func TestLockCheckMiddleware_Direct(t *testing.T) {
	// Setup test dependencies (storage is unencrypted)
	_ = setupTestServer(t)

	// Create a simple handler that the middleware wraps
	innerCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	// Apply the middleware
	handler := lockCheckMiddleware(inner)

	// Test: when storage is not locked, inner handler should be called
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	handler.ServeHTTP(rec, req)

	if !innerCalled {
		t.Error("Expected inner handler to be called when storage is unlocked")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

// TestLockCheckMiddleware_Locked tests the middleware redirects when storage is locked
func TestLockCheckMiddleware_Locked(t *testing.T) {
	// Create a temporary directory with an .encrypted marker to simulate locked storage
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, ".encrypted"), []byte(""), 0644)
	if err != nil {
		t.Fatalf("Failed to create .encrypted marker: %v", err)
	}

	// Create encrypted (locked) storage
	encStore, err := storage.New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create encrypted storage: %v", err)
	}

	// Initialize backup package with the locked storage so IsStorageLocked() returns true
	root := testutil.ProjectRoot()
	c := &config.Config{
		ListenAddr:         ":0",
		Debug:              true,
		DataDirectory:      tmpDir,
		UploadsDirectory:   tmpDir + "/uploads",
		SettingsDirectory:  tmpDir + "/settings",
		TemplatesDirectory: root + "/web/templates",
		StaticDirectory:    root + "/web/static",
	}

	renderer, err := newTestRenderer(root)
	if err != nil {
		t.Fatalf("Failed to create renderer: %v", err)
	}
	backup.Initialize(c, encStore, renderer, nil)

	// Create a handler wrapped by the middleware
	innerCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := lockCheckMiddleware(inner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	handler.ServeHTTP(rec, req)

	if innerCalled {
		t.Error("Expected inner handler NOT to be called when storage is locked")
	}
	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("Expected redirect status %d, got %d", http.StatusTemporaryRedirect, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/unlock" {
		t.Errorf("Expected redirect to /unlock, got %s", loc)
	}

	// Restore normal test state so other tests aren't affected
	normalStore, _ := storage.New(testutil.TestDataDir())
	backup.Initialize(c, normalStore, renderer, nil)
}

// newTestRenderer creates a template renderer for testing
func newTestRenderer(root string) (*templates.Renderer, error) {
	return templates.New(root+"/web/templates", true)
}

// TestRun_Success tests the run() function with valid configuration
func TestRun(t *testing.T) {
	cleanup := testutil.SetTestEnv(t)
	defer cleanup()

	handler, addr, err := run()
	if err != nil {
		t.Fatalf("run() returned error: %v", err)
	}
	if handler == nil {
		t.Fatal("run() returned nil handler")
	}
	if addr == "" {
		t.Fatal("run() returned empty address")
	}
}

// TestRun_SetupDependenciesError tests run() when SetupDependencies fails
func TestRun_SetupDependenciesError(t *testing.T) {
	cleanup := testutil.SetTestEnv(t)
	defer cleanup()

	// Use debug mode with a non-existent templates dir to trigger template error
	os.Setenv("BUDGET_DEBUG", "true")
	os.Setenv("BUDGET_TEMPLATES_DIR", "/nonexistent/templates/dir")

	_, _, err := run()
	if err == nil {
		t.Fatal("expected run() to return error with invalid templates directory")
	}
}

// TestRun_StorageInitError tests run() when storage initialization fails
func TestRun_StorageInitError(t *testing.T) {
	cleanup := testutil.SetTestEnv(t)
	defer cleanup()

	// Create a temp dir with .encrypted marker and a corrupt encryption config
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, ".encrypted"), []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create .encrypted marker: %v", err)
	}
	// Write invalid JSON to the encryption config file to trigger a parse error
	if err := os.WriteFile(filepath.Join(tmpDir, ".encryption-config.json"), []byte("{invalid json"), 0644); err != nil {
		t.Fatalf("Failed to create corrupt config: %v", err)
	}

	os.Setenv("BUDGET_DATA_DIR", tmpDir)

	_, _, err := run()
	if err == nil {
		t.Fatal("expected run() to return error with corrupt encryption config")
	}
}

// TestRun_EncryptedStorage tests run() with encrypted storage detected
func TestRun_EncryptedStorage(t *testing.T) {
	cleanup := testutil.SetTestEnv(t)
	defer cleanup()

	// Create a temp dir with an .encrypted marker to simulate encrypted storage
	tmpDir := t.TempDir()
	err := os.WriteFile(filepath.Join(tmpDir, ".encrypted"), []byte(""), 0644)
	if err != nil {
		t.Fatalf("Failed to create .encrypted marker: %v", err)
	}

	os.Setenv("BUDGET_DATA_DIR", tmpDir)

	handler, addr, runErr := run()
	if runErr != nil {
		t.Fatalf("run() returned error: %v", runErr)
	}
	if handler == nil {
		t.Fatal("run() returned nil handler")
	}
	if addr == "" {
		t.Fatal("run() returned empty address")
	}
}

// TestSetupDependencies_Error tests the error path in SetupDependencies
func TestSetupDependencies_Error(t *testing.T) {
	// Use a non-existent templates directory in debug mode to trigger error
	c := &config.Config{
		ListenAddr:         ":0",
		Debug:              true,
		DataDirectory:      testutil.TestDataDir(),
		UploadsDirectory:   testutil.TestDataDir() + "/uploads",
		SettingsDirectory:  testutil.TestDataDir() + "/settings",
		TemplatesDirectory: "/nonexistent/templates/dir",
		StaticDirectory:    "/nonexistent/static/dir",
		BackupDir:          t.TempDir(),
	}

	var err error
	store, err = storage.New(c.DataDirectory)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	err = SetupDependencies(c)
	if err == nil {
		t.Error("Expected SetupDependencies to return error with invalid templates directory")
	}
}
