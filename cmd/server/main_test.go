package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"budget2/internal/config"
	"budget2/internal/services/storage"
	"budget2/internal/testutil"
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

// TestDashboardAlertsPartial tests the alerts partial endpoint
func TestDashboardAlertsPartial(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp := ts.GET("/dashboard/alerts")
	testutil.AssertResponse(t, resp).
		StatusOK().
		ContentTypeHTML()
}

// TestDashboardChartData tests chart data endpoints
func TestDashboardChartData(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	chartTypes := []string{
		"monthly",
		"category",
		"cashflow",
		"merchants",
		"weekly",
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
