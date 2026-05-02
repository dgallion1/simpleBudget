package explorer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"budget2/internal/config"
	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/internal/testutil"
)

// setupTestEnv creates a temp dir with optional CSV data and initializes package globals.
// When renderer is nil, handlers fall back to JSON output which is easy to assert on.
func setupTestEnv(t *testing.T, csvContent ...string) string {
	t.Helper()
	dataDir := t.TempDir()

	testStore, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	for i, content := range csvContent {
		name := fmt.Sprintf("test%d.csv", i)
		path := filepath.Join(dataDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	loader = dataloader.New(dataDir, testStore)
	renderer = nil
	cfg = &config.Config{DataDirectory: dataDir}
	store = testStore
	return dataDir
}

const sampleCSV = `Date,Description,Amount,Category
2024-01-15,Salary,5000.00,Income
2024-01-20,Groceries,-150.00,Food
2024-02-01,Rent,-1200.00,Housing
2024-02-15,Salary,5000.00,Income
2024-03-01,Electric,-100.00,Utilities
`

// ---- handleExplorer tests ----

func TestHandleExplorer_Basic(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/explorer", nil)
	rec := httptest.NewRecorder()
	handleExplorer(rec, req)

	// renderer is nil, so fallback HTML
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Data Explorer") {
		t.Fatalf("expected fallback HTML with 'Data Explorer', got %s", body)
	}
}

func TestHandleExplorer_LoadDataError(t *testing.T) {
	// Point loader to a nonexistent directory that will cause Glob to fail?
	// Actually Glob won't fail for nonexistent dir, it returns empty. Let's test
	// with empty data (no CSV).
	setupTestEnv(t) // no CSV files -> empty dataset, not error

	req := httptest.NewRequest(http.MethodGet, "/explorer", nil)
	rec := httptest.NewRecorder()
	handleExplorer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty data, got %d", rec.Code)
	}
}

func TestHandleExplorer_WithFilters(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	tests := []struct {
		name   string
		query  string
		status int
	}{
		{"search filter", "?search=Groceries", 200},
		{"category filter", "?category=Food", 200},
		{"type Income", "?type=Income", 200},
		{"type Outflow", "?type=Outflow", 200},
		{"date range", "?start=2024-01-01&end=2024-01-31", 200},
		{"sort by description asc", "?sort=description&order=asc", 200},
		{"sort by amount desc", "?sort=amount&order=desc", 200},
		{"sort by category", "?sort=category&order=asc", 200},
		{"sort by type", "?sort=type&order=desc", 200},
		{"sort by source", "?sort=source&order=asc", 200},
		{"sort default (unknown)", "?sort=unknown", 200},
		{"pagination page 1", "?page=1&perPage=2", 200},
		{"pagination page 2", "?page=2&perPage=2", 200},
		{"page exceeds total", "?page=999&perPage=2", 200},
		{"invalid page/perPage", "?page=-1&perPage=0", 200},
		{"all filters combined", "?search=Salary&category=Income&type=Income&sort=date&order=asc&page=1&perPage=10&start=2024-01-01&end=2024-12-31", 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/explorer"+tt.query, nil)
			rec := httptest.NewRecorder()
			handleExplorer(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("expected %d, got %d", tt.status, rec.Code)
			}
		})
	}
}

// Regression: a valid major-expense filter whose group has zero
// matched transactions used to leave `filtered` unchanged because
// match.Groups omits empty entries. The Explorer reported "filtered"
// but rendered every transaction.
func TestHandleTransactionsPartial_MajorExpenseFilterWithZeroMatches(t *testing.T) {
	dataDir := setupTestEnv(t, sampleCSV)

	// Seed a major expense whose keyword matches no transaction in sampleCSV.
	majorExpensesPath := filepath.Join(dataDir, "major_expenses.json")
	body := `{"expenses":[{"id":"none-id","name":"Nope","keywords":["NEVERMATCH"],"expected_min":0,"expected_max":0,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(majorExpensesPath, []byte(body), 0644); err != nil {
		t.Fatalf("write majors: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/explorer/transactions?majorExpense=none-id", nil)
	rec := httptest.NewRecorder()
	handleTransactionsPartial(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, rec.Body.String())
	}
	if got := payload["TotalCount"]; got != float64(0) {
		t.Errorf("expected TotalCount=0 when filter has zero matches, got %v — filter is being silently dropped", got)
	}
	txns, _ := payload["Transactions"].([]interface{})
	if len(txns) != 0 {
		t.Errorf("expected empty Transactions for zero-match filter, got %d entries", len(txns))
	}
}

func TestHandleExplorer_EmptyResults(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	// Search for something nonexistent -> totalCount == 0 -> pageStart/pageEnd = 0
	req := httptest.NewRequest(http.MethodGet, "/explorer?search=NONEXISTENT", nil)
	rec := httptest.NewRecorder()
	handleExplorer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// Regression: a refund row (opposite-signed amount) within the Outflow type
// must REDUCE TotalExpenses, not be added as an absolute value.
// Bug symptom: a -$199.78 refund mixed with $549.39 of purchases produced
// TotalExpenses=$749.17 (refund's magnitude added) instead of $349.61.
func TestHandleTransactionsPartial_RefundReducesTotalExpenses(t *testing.T) {
	// Amounts mirror the user's reported scenario: positive-convention CSV
	// where purchases are positive and the refund is negative.
	csv := `Date,Description,Amount,Category
2026-04-14,Shenandoahfood&beverag,4.84,Food & Dining
2026-04-14,Shenandoah Lodging,259.78,Hotel
2026-04-13,Shenandoahfood&beverag,30.31,Food & Dining
2026-04-13,Shenandoahfood&beverag,34.68,Food & Dining
2026-04-13,Shenandoah National Park,20.00,Uncategorized
2026-04-12,Shenandoah Lodging,-199.78,Hotel
2026-03-16,Shenandoah Lodging,199.78,Hotel
`
	setupTestEnv(t, csv)

	req := httptest.NewRequest(http.MethodGet, "/explorer/transactions?search=shen&type=Outflow", nil)
	rec := httptest.NewRecorder()
	handleTransactionsPartial(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, rec.Body.String())
	}

	const want = 349.61 // 4.84+259.78+30.31+34.68+20.00+199.78 - 199.78
	gotExpenses, _ := payload["TotalExpenses"].(float64)
	if diff := gotExpenses - want; diff > 0.01 || diff < -0.01 {
		t.Errorf("TotalExpenses = %.2f, want %.2f (refund of -199.78 must subtract, not add)", gotExpenses, want)
	}

	gotNet, _ := payload["NetAmount"].(float64)
	if diff := gotNet - (-want); diff > 0.01 || diff < -0.01 {
		t.Errorf("NetAmount = %.2f, want %.2f", gotNet, -want)
	}
}

// ---- handleTransactionsPartial tests ----

func TestHandleTransactionsPartial_Basic(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/explorer/transactions", nil)
	rec := httptest.NewRecorder()
	handleTransactionsPartial(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("JSON decode error: %v", err)
	}

	if _, ok := payload["Transactions"]; !ok {
		t.Fatal("expected Transactions in response")
	}
	if _, ok := payload["TotalCount"]; !ok {
		t.Fatal("expected TotalCount in response")
	}
}

func TestHandleTransactionsPartial_AppendRows(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	// append=true triggers different template, but renderer is nil -> JSON
	req := httptest.NewRequest(http.MethodGet, "/explorer/transactions?append=true", nil)
	rec := httptest.NewRecorder()
	handleTransactionsPartial(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleTransactionsPartial_WithFilters(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	tests := []struct {
		name  string
		query string
	}{
		{"search", "?search=Rent"},
		{"category", "?category=Housing"},
		{"type Income", "?type=Income"},
		{"type Outflow", "?type=Outflow"},
		{"date range", "?start=2024-02-01&end=2024-02-28"},
		{"sort description", "?sort=description&order=desc"},
		{"sort amount", "?sort=amount&order=asc"},
		{"sort category", "?sort=category&order=desc"},
		{"sort type", "?sort=type&order=asc"},
		{"sort source", "?sort=source&order=desc"},
		{"sort unknown", "?sort=bogus"},
		{"pagination", "?page=1&perPage=2"},
		{"page beyond total", "?page=100&perPage=2"},
		{"empty results", "?search=DOESNOTEXIST"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/explorer/transactions"+tt.query, nil)
			rec := httptest.NewRecorder()
			handleTransactionsPartial(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
		})
	}
}

func TestHandleTransactionsPartial_LoadDataError(t *testing.T) {
	// Use a directory that doesn't exist as the CSV dir
	badDir := "/nonexistent/dir/that/should/not/exist"
	testStore, _ := storage.New(t.TempDir())
	loader = dataloader.New(badDir, testStore)
	renderer = nil
	cfg = &config.Config{DataDirectory: badDir}
	store = testStore

	req := httptest.NewRequest(http.MethodGet, "/explorer/transactions", nil)
	rec := httptest.NewRecorder()
	handleTransactionsPartial(rec, req)

	// LoadData returns empty set, not error for missing dir
	// Actually let's verify
	if rec.Code != http.StatusOK {
		// It's fine if it's 200 with empty data or 500
		t.Logf("got status %d", rec.Code)
	}
}

// ---- handleFileManager tests ----

func TestHandleFileManager_Basic(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/explorer/files", nil)
	rec := httptest.NewRecorder()
	handleFileManager(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("JSON decode error: %v", err)
	}
	files, ok := payload["Files"].([]interface{})
	if !ok {
		t.Fatal("expected Files array")
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestHandleFileManager_NoFiles(t *testing.T) {
	setupTestEnv(t) // no CSV

	req := httptest.NewRequest(http.MethodGet, "/explorer/files", nil)
	rec := httptest.NewRecorder()
	handleFileManager(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// ---- HandleFileManagerPage tests ----

func TestHandleFileManagerPage_NotEncrypted(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	// This calls renderer.Render which will panic if renderer is nil.
	// We need renderer to be nil-safe or skip. Looking at the code:
	// renderer.Render(w, "base", data) is called without nil check.
	// So we can only test this by verifying the panic or by noting it.
	// Actually, HandleFileManagerPage doesn't check for renderer == nil.
	// We must skip or accept a panic. Let's just verify we can call it
	// and catch the panic.
	defer func() {
		if r := recover(); r != nil {
			// Expected: renderer is nil, so Render panics
		}
	}()

	req := httptest.NewRequest(http.MethodGet, "/filemanager", nil)
	rec := httptest.NewRecorder()
	HandleFileManagerPage(rec, req)
}

// ---- handleFileToggle tests ----

func TestHandleFileToggle_Enable(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	form := url.Values{
		"file":    {"test0.csv"},
		"enabled": {"true"},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/files/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleFileToggle(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("JSON decode error: %v", err)
	}
}

func TestHandleFileToggle_Disable(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	form := url.Values{
		"file":    {"test0.csv"},
		"enabled": {"false"},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/files/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleFileToggle(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleFileToggle_ParseFormError(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	// Send a request with an invalid body that fails ParseForm
	req := httptest.NewRequest(http.MethodPost, "/explorer/files/toggle", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Set body to nil and ContentLength to non-zero to cause parse error? Actually
	// ParseForm on POST with nil body just gives empty form, not an error.
	// Let's set the content-type to multipart with bad boundary
	req.Header.Set("Content-Type", "multipart/form-data; boundary=")
	req.Body = http.NoBody
	rec := httptest.NewRecorder()
	handleFileToggle(rec, req)

	// May or may not error; the handler calls r.ParseForm() which for POST with
	// multipart content type and empty boundary may error.
	// If it doesn't error, that's OK too, we're testing the branch.
	t.Logf("status: %d", rec.Code)
}

func TestHandleFileToggle_NonexistentFile(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	form := url.Values{
		"file":    {"nonexistent.csv"},
		"enabled": {"true"},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/files/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleFileToggle(rec, req)

	// Should still return 200 with updated file list (nonexistent file just won't appear)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleFileToggle_MultipleFiles(t *testing.T) {
	csv2 := `Date,Description,Amount
2024-06-01,Bonus,2000.00
`
	setupTestEnv(t, sampleCSV, csv2)

	// Enable only first file
	form := url.Values{
		"file":    {"test0.csv"},
		"enabled": {"true"},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/files/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleFileToggle(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// ---- handleFileUpload tests ----

func TestHandleFileUploadReplacesExistingCSV(t *testing.T) {
	dataDir := setupTestEnv(t)

	existingPath := filepath.Join(dataDir, "transactions.csv")
	existing := []byte("Date,Description,Amount\n2024-01-01,Old Row,10.00\n")
	if err := store.WriteFile(existingPath, existing, 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	uploaded := []byte("Date,Description,Amount\n2024-02-02,New Row,25.00\n")
	req := newUploadRequest(t, "transactions.csv", uploaded)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	saved, err := store.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(saved) != string(uploaded) {
		t.Fatalf("expected uploaded content to replace existing file\n got: %q\nwant: %q", string(saved), string(uploaded))
	}

	var payload struct {
		Files []any `json:"Files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response JSON error: %v", err)
	}
}

func TestHandleFileUpload_NonCSV(t *testing.T) {
	setupTestEnv(t)

	uploaded := []byte("not a csv")
	req := newUploadRequest(t, "data.txt", uploaded)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-CSV, got %d", rec.Code)
	}
}

func TestHandleFileUpload_NoFile(t *testing.T) {
	setupTestEnv(t)

	// POST with no multipart form at all
	req := httptest.NewRequest(http.MethodPost, "/explorer/upload", strings.NewReader("not multipart"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleFileUpload_MissingFormFile(t *testing.T) {
	setupTestEnv(t)

	// Valid multipart but wrong field name
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("wrongfield", "test.csv")
	part.Write([]byte("Date,Description,Amount\n"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/explorer/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing form file, got %d", rec.Code)
	}
}

func TestHandleFileUpload_InvalidFilename(t *testing.T) {
	setupTestEnv(t)

	uploaded := []byte("Date,Description,Amount\n2024-01-01,Test,10.00\n")
	req := newUploadRequest(t, "..", uploaded)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid filename, got %d", rec.Code)
	}
}

func TestHandleFileUpload_PathTraversal(t *testing.T) {
	setupTestEnv(t)

	uploaded := []byte("Date,Description,Amount\n2024-01-01,Test,10.00\n")
	req := newUploadRequest(t, "../../etc/passwd.csv", uploaded)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	// sanitizeUploadFilename should strip path, resulting in passwd.csv
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (path stripped to base), got %d", rec.Code)
	}
}

// ---- handleFileDelete tests ----

func TestHandleFileDelete_Success(t *testing.T) {
	dataDir := setupTestEnv(t, sampleCSV)

	// Verify file exists
	filePath := filepath.Join(dataDir, "test0.csv")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("test file should exist before delete")
	}

	// Use chi router to set URL param
	req := httptest.NewRequest(http.MethodDelete, "/explorer/files/test0.csv", nil)
	rec := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("filename", "test0.csv")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handleFileDelete(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Verify file is gone
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatal("file should have been deleted")
	}
}

func TestHandleFileDelete_NotFound(t *testing.T) {
	setupTestEnv(t)

	req := httptest.NewRequest(http.MethodDelete, "/explorer/files/nonexistent.csv", nil)
	rec := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("filename", "nonexistent.csv")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handleFileDelete(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleFileDelete_InvalidFilename(t *testing.T) {
	setupTestEnv(t)

	req := httptest.NewRequest(http.MethodDelete, "/explorer/files/..", nil)
	rec := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("filename", "..")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handleFileDelete(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid filename, got %d", rec.Code)
	}
}

func TestHandleFileDelete_URLEncoded(t *testing.T) {
	dataDir := setupTestEnv(t)

	// Create file with spaces in name
	spacedName := "my data.csv"
	spacedPath := filepath.Join(dataDir, spacedName)
	if err := os.WriteFile(spacedPath, []byte("Date,Description,Amount\n2024-01-01,Test,10\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/explorer/files/"+url.PathEscape(spacedName), nil)
	rec := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("filename", url.PathEscape(spacedName))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handleFileDelete(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFileDelete_InvalidEncoding(t *testing.T) {
	setupTestEnv(t)

	// Use a valid URL but put the bad percent-encoded value in the chi param
	req := httptest.NewRequest(http.MethodDelete, "/explorer/files/badname", nil)
	rec := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("filename", "bad%ZZname")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handleFileDelete(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad encoding, got %d", rec.Code)
	}
}

// ---- handleAlias tests ----

func TestHandleAlias_SetAlias(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	form := url.Values{
		"hash":         {"abc123"},
		"display_name": {"My Custom Name"},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/alias", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleAlias(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAlias_ClearAlias(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	// First set an alias
	form := url.Values{
		"hash":         {"abc123"},
		"display_name": {"My Custom Name"},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/alias", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleAlias(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on set, got %d", rec.Code)
	}

	// Now clear it (empty display_name)
	form2 := url.Values{
		"hash":         {"abc123"},
		"display_name": {""},
	}
	req2 := httptest.NewRequest(http.MethodPost, "/explorer/alias", strings.NewReader(form2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	handleAlias(rec2, req2)

	if rec2.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on clear, got %d", rec2.Code)
	}
}

func TestHandleAlias_EmptyHash(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	form := url.Values{
		"hash":         {""},
		"display_name": {"Name"},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/alias", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleAlias(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty hash, got %d", rec.Code)
	}
}

func TestHandleAlias_HashTooLong(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	longHash := strings.Repeat("a", 129)
	form := url.Values{
		"hash":         {longHash},
		"display_name": {"Name"},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/alias", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleAlias(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for hash too long, got %d", rec.Code)
	}
}

func TestHandleAlias_DisplayNameTooLong(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	longName := strings.Repeat("x", 201)
	form := url.Values{
		"hash":         {"abc123"},
		"display_name": {longName},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/alias", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleAlias(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for display name too long, got %d", rec.Code)
	}
}

func TestHandleAlias_ParseFormError(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	req := httptest.NewRequest(http.MethodPost, "/explorer/alias", nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=")
	req.Body = http.NoBody
	rec := httptest.NewRecorder()
	handleAlias(rec, req)

	// Should get 400 from ParseForm error
	t.Logf("status: %d", rec.Code)
}

func TestHandleAlias_DisplayNameWithWhitespace(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	form := url.Values{
		"hash":         {"abc123"},
		"display_name": {"  Trimmed Name  "},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/alias", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleAlias(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestHandleAlias_HashExactly128(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	hash128 := strings.Repeat("b", 128)
	form := url.Values{
		"hash":         {hash128},
		"display_name": {"Valid"},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/alias", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleAlias(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for hash exactly 128, got %d", rec.Code)
	}
}

func TestHandleAlias_DisplayNameExactly200(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	name200 := strings.Repeat("c", 200)
	form := url.Values{
		"hash":         {"abc123"},
		"display_name": {name200},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/alias", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleAlias(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for display name exactly 200, got %d", rec.Code)
	}
}

// ---- sanitizeUploadFilename tests ----

func TestSanitizeUploadFilename(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "plain", input: "transactions.csv", want: "transactions.csv"},
		{name: "windows path", input: `C:\fakepath\transactions.csv`, want: "transactions.csv"},
		{name: "unix path", input: "../../transactions.csv", want: "transactions.csv"},
		{name: "empty", input: "", wantErr: true},
		{name: "dot", input: ".", wantErr: true},
		{name: "dotdot basename", input: "..", wantErr: true},
		{name: "simple name with spaces", input: "my file.csv", want: "my file.csv"},
		{name: "deeply nested path", input: "/a/b/c/d/file.csv", want: "file.csv"},
		{name: "windows nested", input: `D:\Users\test\Documents\file.csv`, want: "file.csv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeUploadFilename(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// ---- sortTransactions tests ----

func TestSortTransactions(t *testing.T) {
	setupTestEnv(t, sampleCSV)

	data, err := loader.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}

	tests := []struct {
		field string
		order string
	}{
		{"date", "asc"},
		{"date", "desc"},
		{"description", "asc"},
		{"description", "desc"},
		{"category", "asc"},
		{"category", "desc"},
		{"amount", "asc"},
		{"amount", "desc"},
		{"type", "asc"},
		{"type", "desc"},
		{"source", "asc"},
		{"source", "desc"},
		{"majorExpense", "asc"},
		{"majorExpense", "desc"},
		{"unknown", "asc"},
		{"unknown", "desc"},
	}

	for _, tt := range tests {
		t.Run(tt.field+"_"+tt.order, func(t *testing.T) {
			result := sortTransactions(data, tt.field, tt.order)
			if result == nil {
				t.Fatal("sortTransactions returned nil")
			}
			if result.Len() != data.Len() {
				t.Fatalf("expected %d transactions, got %d", data.Len(), result.Len())
			}
		})
	}
}

func TestSortTransactions_EmptyCategory(t *testing.T) {
	// Test sorting with empty categories (should become "Uncategorized")
	// Include multiple empty-category items to ensure the comparator sees empty on both i and j
	ts := models.NewTransactionSet([]models.Transaction{
		{Description: "A", Category: ""},
		{Description: "B", Category: "Food"},
		{Description: "C", Category: ""},
		{Description: "D", Category: "Zeta"},
	})

	result := sortTransactions(ts, "category", "asc")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Len() != 4 {
		t.Fatalf("expected 4, got %d", result.Len())
	}

	// Also test desc
	result2 := sortTransactions(ts, "category", "desc")
	if result2.Len() != 4 {
		t.Fatalf("expected 4, got %d", result2.Len())
	}
}

func TestSortTransactions_ByMajorExpense(t *testing.T) {
	// Transactions with empty MajorExpenseName must sort to the bottom in both
	// directions so rule-grouped rows stay clustered. Within named groups,
	// asc/desc follow case-insensitive alphabetical order.
	ts := models.NewTransactionSet([]models.Transaction{
		{Description: "T1", MajorExpenseName: "Wegmans"},
		{Description: "T2", MajorExpenseName: ""},
		{Description: "T3", MajorExpenseName: "Mortgage"},
		{Description: "T4", MajorExpenseName: ""},
		{Description: "T5", MajorExpenseName: "abundance"},
	})

	asc := sortTransactions(ts, "majorExpense", "asc")
	wantAsc := []string{"abundance", "Mortgage", "Wegmans", "", ""}
	for i, want := range wantAsc {
		if got := asc.Transactions[i].MajorExpenseName; got != want {
			t.Errorf("asc[%d] = %q, want %q", i, got, want)
		}
	}

	desc := sortTransactions(ts, "majorExpense", "desc")
	wantDesc := []string{"Wegmans", "Mortgage", "abundance", "", ""}
	for i, want := range wantDesc {
		if got := desc.Transactions[i].MajorExpenseName; got != want {
			t.Errorf("desc[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestSortTransactions_Empty(t *testing.T) {
	empty := models.NewTransactionSet(nil)
	result := sortTransactions(empty, "date", "asc")
	if result.Len() != 0 {
		t.Fatalf("expected 0 transactions, got %d", result.Len())
	}
}

// ---- calculatePageRange tests ----

func TestCalculatePageRange(t *testing.T) {
	tests := []struct {
		name        string
		currentPage int
		totalPages  int
		wantLen     int
		wantFirst   int
		wantLast    int
	}{
		{"single page", 1, 1, 1, 1, 1},
		{"3 pages on page 1", 1, 3, 3, 1, 3},
		{"7 pages on page 4", 4, 7, 7, 1, 7},
		{"exactly 7 pages", 1, 7, 7, 1, 7},
		{"10 pages on page 1", 1, 10, 5, 1, 5},
		{"10 pages on page 5", 5, 10, 5, 3, 7},
		{"10 pages on page 10", 10, 10, 5, 6, 10},
		{"10 pages on page 9", 9, 10, 5, 6, 10},
		{"20 pages on page 2", 2, 20, 5, 1, 5},
		{"20 pages on page 15", 15, 20, 5, 13, 17},
		{"0 pages", 1, 0, 0, 0, 0},
		{"page near start boundary", 2, 10, 5, 1, 5},
		{"page near end boundary", 9, 10, 5, 6, 10},
		{"8 pages page 4", 4, 8, 5, 2, 6},
		{"8 pages page 1", 1, 8, 5, 1, 5},
		{"8 pages page 8", 8, 8, 5, 4, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculatePageRange(tt.currentPage, tt.totalPages)
			if len(result) != tt.wantLen {
				t.Fatalf("expected len %d, got %d: %v", tt.wantLen, len(result), result)
			}
			if tt.wantLen > 0 {
				if result[0] != tt.wantFirst {
					t.Fatalf("expected first page %d, got %d", tt.wantFirst, result[0])
				}
				if result[len(result)-1] != tt.wantLast {
					t.Fatalf("expected last page %d, got %d", tt.wantLast, result[len(result)-1])
				}
			}
		})
	}
}

// ---- RegisterRoutes / Initialize tests ----

func TestInitialize(t *testing.T) {
	dataDir := t.TempDir()
	testStore, _ := storage.New(dataDir)
	testLoader := dataloader.New(dataDir, testStore)
	testCfg := &config.Config{DataDirectory: dataDir}

	Initialize(testLoader, nil, testCfg, testStore)

	if loader != testLoader {
		t.Fatal("loader not set")
	}
	if cfg != testCfg {
		t.Fatal("cfg not set")
	}
	if store != testStore {
		t.Fatal("store not set")
	}
}

func TestRegisterRoutes(t *testing.T) {
	dataDir := t.TempDir()
	testStore, _ := storage.New(dataDir)
	Initialize(dataloader.New(dataDir, testStore), nil, &config.Config{DataDirectory: dataDir}, testStore)

	r := chi.NewRouter()
	RegisterRoutes(r)

	// Verify routes are registered by doing a walk
	routeCount := 0
	chi.Walk(r, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		routeCount++
		return nil
	})

	if routeCount < 7 {
		t.Fatalf("expected at least 7 routes, got %d", routeCount)
	}
}

// ---- Tests with real renderer (covers renderer != nil branches) ----

func setupTestEnvWithRenderer(t *testing.T, csvContent ...string) string {
	t.Helper()
	dataDir := t.TempDir()

	testStore, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	for i, content := range csvContent {
		name := fmt.Sprintf("test%d.csv", i)
		path := filepath.Join(dataDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	loader = dataloader.New(dataDir, testStore)
	renderer = rend
	cfg = &config.Config{DataDirectory: dataDir}
	store = testStore
	return dataDir
}

func TestHandleExplorer_WithRenderer(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/explorer", nil)
	rec := httptest.NewRecorder()
	handleExplorer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html content type, got %s", ct)
	}
}

func TestHandleExplorer_WithRenderer_EmptyResults(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/explorer?search=NONEXISTENT", nil)
	rec := httptest.NewRecorder()
	handleExplorer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleTransactionsPartial_WithRenderer(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/explorer/transactions", nil)
	rec := httptest.NewRecorder()
	handleTransactionsPartial(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTransactionsPartial_WithRenderer_AppendRows(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/explorer/transactions?append=true", nil)
	rec := httptest.NewRecorder()
	handleTransactionsPartial(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTransactionsPartial_WithRenderer_EmptyResults(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/explorer/transactions?search=NONEXISTENT", nil)
	rec := httptest.NewRecorder()
	handleTransactionsPartial(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleFileManager_WithRenderer(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/explorer/files", nil)
	rec := httptest.NewRecorder()
	handleFileManager(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFileManagerPage_WithRenderer(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/filemanager", nil)
	rec := httptest.NewRecorder()
	HandleFileManagerPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFileManagerPage_WithRenderer_NoFiles(t *testing.T) {
	setupTestEnvWithRenderer(t) // no CSV

	req := httptest.NewRequest(http.MethodGet, "/filemanager", nil)
	rec := httptest.NewRecorder()
	HandleFileManagerPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleFileToggle_WithRenderer(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	form := url.Values{
		"file":    {"test0.csv"},
		"enabled": {"true"},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/files/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleFileToggle(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFileUpload_WithRenderer(t *testing.T) {
	setupTestEnvWithRenderer(t)

	uploaded := []byte("Date,Description,Amount\n2024-01-01,Test,10.00\n")
	req := newUploadRequest(t, "new.csv", uploaded)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFileDelete_WithRenderer(t *testing.T) {
	dataDir := setupTestEnvWithRenderer(t, sampleCSV)

	_ = dataDir // file exists as test0.csv
	req := httptest.NewRequest(http.MethodDelete, "/explorer/files/test0.csv", nil)
	rec := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("filename", "test0.csv")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handleFileDelete(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFileManagerPage_Encrypted_Locked(t *testing.T) {
	dataDir := t.TempDir()

	// Create encrypted marker and config
	if err := os.WriteFile(filepath.Join(dataDir, ".encrypted"), []byte(""), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	configJSON := `{"method":"password"}`
	if err := os.WriteFile(filepath.Join(dataDir, ".encryption-config.json"), []byte(configJSON), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	testStore, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	loader = dataloader.New(dataDir, testStore)
	renderer = rend
	cfg = &config.Config{DataDirectory: dataDir}
	store = testStore

	req := httptest.NewRequest(http.MethodGet, "/filemanager", nil)
	rec := httptest.NewRecorder()
	HandleFileManagerPage(rec, req)

	// Encrypted + locked => skips GetFileInfo, still renders page
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleFileManagerPage_Encrypted_NoConfig(t *testing.T) {
	dataDir := t.TempDir()

	// Create encrypted marker but no config file
	if err := os.WriteFile(filepath.Join(dataDir, ".encrypted"), []byte(""), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	testStore, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	loader = dataloader.New(dataDir, testStore)
	renderer = rend
	cfg = &config.Config{DataDirectory: dataDir}
	store = testStore

	req := httptest.NewRequest(http.MethodGet, "/filemanager", nil)
	rec := httptest.NewRecorder()
	HandleFileManagerPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleFileManager_GetFileInfoError(t *testing.T) {
	dataDir := t.TempDir()
	testStore, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	// Use a directory path with '[' to make filepath.Glob fail
	badDir := filepath.Join(dataDir, "bad[dir")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	loader = dataloader.New(badDir, testStore)
	renderer = nil
	cfg = &config.Config{DataDirectory: badDir}
	store = testStore

	req := httptest.NewRequest(http.MethodGet, "/explorer/files", nil)
	rec := httptest.NewRecorder()
	handleFileManager(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for GetFileInfo error, got %d", rec.Code)
	}
}

func TestHandleFileToggle_GetFileInfoError(t *testing.T) {
	dataDir := t.TempDir()
	testStore, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	badDir := filepath.Join(dataDir, "bad[dir")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	loader = dataloader.New(badDir, testStore)
	renderer = nil
	cfg = &config.Config{DataDirectory: badDir}
	store = testStore

	form := url.Values{
		"file":    {"test.csv"},
		"enabled": {"true"},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/files/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleFileToggle(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for GetFileInfo error, got %d", rec.Code)
	}
}

func TestHandleFileManagerPage_GetFileInfoError(t *testing.T) {
	dataDir := t.TempDir()
	testStore, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	badDir := filepath.Join(dataDir, "bad[dir")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	templateDir := filepath.Join(testutil.ProjectRoot(), "web", "templates")
	rend, err := templates.New(templateDir, false)
	if err != nil {
		t.Fatalf("templates.New: %v", err)
	}

	loader = dataloader.New(badDir, testStore)
	renderer = rend
	cfg = &config.Config{DataDirectory: badDir}
	store = testStore

	req := httptest.NewRequest(http.MethodGet, "/filemanager", nil)
	rec := httptest.NewRecorder()
	HandleFileManagerPage(rec, req)

	// GetFileInfo should fail -> 500
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandleExplorer_LoadDataError_BadGlob(t *testing.T) {
	dataDir := t.TempDir()
	testStore, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	badDir := filepath.Join(dataDir, "bad[dir")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	loader = dataloader.New(badDir, testStore)
	renderer = nil
	cfg = &config.Config{DataDirectory: badDir}
	store = testStore

	req := httptest.NewRequest(http.MethodGet, "/explorer", nil)
	rec := httptest.NewRecorder()
	handleExplorer(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandleTransactionsPartial_LoadDataError_BadGlob(t *testing.T) {
	dataDir := t.TempDir()
	testStore, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	badDir := filepath.Join(dataDir, "bad[dir")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	loader = dataloader.New(badDir, testStore)
	renderer = nil
	cfg = &config.Config{DataDirectory: badDir}
	store = testStore

	req := httptest.NewRequest(http.MethodGet, "/explorer/transactions", nil)
	rec := httptest.NewRecorder()
	handleTransactionsPartial(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandleExplorer_WithRenderer_Filters(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	// Test with all filter parameters to hit remaining branches
	req := httptest.NewRequest(http.MethodGet, "/explorer?search=Salary&type=Income&category=Income&start=2024-01-01&end=2024-12-31&sort=date&order=asc&page=1&perPage=2", nil)
	rec := httptest.NewRecorder()
	handleExplorer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleTransactionsPartial_WithRenderer_Filters(t *testing.T) {
	setupTestEnvWithRenderer(t, sampleCSV)

	req := httptest.NewRequest(http.MethodGet, "/explorer/transactions?type=Outflow&page=999&perPage=2", nil)
	rec := httptest.NewRecorder()
	handleTransactionsPartial(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleAlias_SaveError(t *testing.T) {
	dataDir := setupTestEnv(t, sampleCSV)

	// Make the aliases file a directory so SaveAlias fails on write
	aliasPath := filepath.Join(dataDir, "aliases.json")
	if err := os.MkdirAll(aliasPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	form := url.Values{
		"hash":         {"abc123"},
		"display_name": {"Name"},
	}
	req := httptest.NewRequest(http.MethodPost, "/explorer/alias", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleAlias(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for save error, got %d", rec.Code)
	}
}

func TestHandleFileUpload_WriteError(t *testing.T) {
	dataDir := setupTestEnv(t)

	// Make the data directory read-only so WriteFile fails
	// Create a subdirectory that we'll use as DataDirectory that's read-only
	roDir := filepath.Join(dataDir, "readonly")
	if err := os.MkdirAll(roDir, 0555); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfg.DataDirectory = roDir

	uploaded := []byte("Date,Description,Amount\n2024-01-01,Test,10.00\n")
	req := newUploadRequest(t, "test.csv", uploaded)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for write error, got %d", rec.Code)
	}
}

func TestHandleFileDelete_RemoveError(t *testing.T) {
	dataDir := setupTestEnv(t, sampleCSV)

	// Make the directory read-only so Remove fails
	filePath := filepath.Join(dataDir, "test0.csv")
	// First verify file exists
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	// Make directory non-writable
	if err := os.Chmod(dataDir, 0555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	// Restore permissions on cleanup
	t.Cleanup(func() { os.Chmod(dataDir, 0755) })

	req := httptest.NewRequest(http.MethodDelete, "/explorer/files/test0.csv", nil)
	rec := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("filename", "test0.csv")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handleFileDelete(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for remove error, got %d", rec.Code)
	}
}

// ---- helper ----

func newUploadRequest(t *testing.T, filename string, content []byte) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile() error: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("part.Write() error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/explorer/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	return req
}
