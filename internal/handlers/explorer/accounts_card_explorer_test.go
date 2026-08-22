package explorer

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
)

// TestExplorerUnassignedBanner_Present asserts the explorer page shows the
// unassigned banner when the count is non-zero (A8). The explorer reads the
// same loader.UnassignedCount the dashboard does; with no accounts sidecar,
// every CSV row is unassigned and the banner must appear on both pages.
func TestExplorerUnassignedBanner_Present(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/explorer", nil)
	rec := httptest.NewRecorder()
	handleExplorer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "unassigned-banner") {
		t.Errorf("explorer must show the unassigned banner when count > 0; got:\n%s", body)
	}
	if !strings.Contains(body, "matching no account") {
		t.Errorf("explorer banner must state 'matching no account'; got:\n%s", body)
	}
	if !strings.Contains(body, `href="/accounts"`) {
		t.Errorf("explorer banner must link to /accounts; got:\n%s", body)
	}
}

// TestExplorerUnassignedBanner_Absent asserts the explorer page does NOT
// show the banner when the count is zero. With an accounts sidecar that
// matches the CSV, every row is assigned and the count is zero.
func TestExplorerUnassignedBanner_Absent(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	// Save an accounts sidecar matching test0.csv through the storage
	// service the explorer test env already wired (store). When the
	// account's FilePatterns match the CSV basename, the loader stamps
	// AccountID on every row and the unassigned count is zero.
	if err := accounts.Save(store, []models.Account{{
		ID:           "bank",
		Name:         "Bank Checking",
		Kind:         models.AccountKindChecking,
		FilePatterns: []string{"test0.csv"},
	}}); err != nil {
		t.Fatalf("accounts.Save: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/explorer", nil)
	rec := httptest.NewRecorder()
	handleExplorer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "unassigned-banner") {
		t.Errorf("explorer must NOT show the unassigned banner when count is 0; got:\n%s", body)
	}
}

// TestExplorer_RendersExactlyOneH1 asserts the explorer page has exactly one
// <h1> naming the page (ACCESSIBILITY.md point 1). The explorer's heading
// outline is a single <h1>Data Explorer</h1> with no descendant headings
// (the transactions table uses <th>/<td>, not heading elements), so no
// levels are skipped.
func TestExplorer_RendersExactlyOneH1(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/explorer", nil)
	rec := httptest.NewRecorder()
	handleExplorer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	openH1 := strings.Count(body, "<h1")
	closeH1 := strings.Count(body, "</h1>")
	if openH1 != 1 {
		t.Errorf("explorer page has %d <h1> open tags, want exactly 1", openH1)
	}
	if closeH1 != 1 {
		t.Errorf("explorer page has %d </h1> close tags, want exactly 1", closeH1)
	}
	if !strings.Contains(body, ">Data Explorer</h1>") {
		t.Errorf("explorer page <h1> must name the page; got:\n%s", body[:min(len(body), 400)])
	}
}
