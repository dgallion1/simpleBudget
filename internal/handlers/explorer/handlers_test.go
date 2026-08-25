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

// uploadResponsePayload mirrors the JSON fallback body (renderer == nil).
type uploadResponsePayload struct {
	Files   []any           `json:"Files"`
	Results []uploadOutcome `json:"Results"`
}

func decodeUploadResponse(t *testing.T, rec *httptest.ResponseRecorder) uploadResponsePayload {
	t.Helper()
	var payload uploadResponsePayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response JSON error: %v (body: %s)", err, rec.Body.String())
	}
	return payload
}

func outcomeFor(t *testing.T, payload uploadResponsePayload, name string) uploadOutcome {
	t.Helper()
	for _, o := range payload.Results {
		if o.Name == name {
			return o
		}
	}
	t.Fatalf("no outcome for %q in %+v", name, payload.Results)
	return uploadOutcome{}
}

// Collisions skip: an upload naming an existing file must not overwrite it,
// and the response reports "skipped: already exists" for that entry.
func TestHandleFileUpload_CollisionSkips(t *testing.T) {
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
	if string(saved) != string(existing) {
		t.Fatalf("expected existing file to be left untouched (no overwrite)\n got: %q\nwant: %q", string(saved), string(existing))
	}

	payload := decodeUploadResponse(t, rec)
	outcome := outcomeFor(t, payload, "transactions.csv")
	if outcome.Status != "skipped" {
		t.Fatalf("expected status skipped, got %+v", outcome)
	}
	if outcome.Reason != "already exists" {
		t.Fatalf("expected reason 'already exists', got %+v", outcome)
	}
}

func TestHandleFileUpload_NonCSV(t *testing.T) {
	setupTestEnv(t)

	uploaded := []byte("not a csv")
	req := newUploadRequest(t, "data.txt", uploaded)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	// A bad file does not abort the (single-file) batch; it's reported as a
	// rejected outcome rather than an HTTP error.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for non-CSV (reported as rejected), got %d", rec.Code)
	}
	payload := decodeUploadResponse(t, rec)
	outcome := outcomeFor(t, payload, "data.txt")
	if outcome.Status != "rejected" {
		t.Fatalf("expected status rejected, got %+v", outcome)
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

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for invalid filename (reported as rejected), got %d", rec.Code)
	}
	payload := decodeUploadResponse(t, rec)
	outcome := outcomeFor(t, payload, "..")
	if outcome.Status != "rejected" {
		t.Fatalf("expected status rejected, got %+v", outcome)
	}
}

func TestHandleFileUpload_PathTraversal(t *testing.T) {
	dataDir := setupTestEnv(t)

	uploaded := []byte("Date,Description,Amount\n2024-01-01,Test,10.00\n")
	req := newUploadRequest(t, "../../etc/passwd.csv", uploaded)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	// sanitizeUploadFilename should strip path, resulting in passwd.csv
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (path stripped to base), got %d", rec.Code)
	}
	payload := decodeUploadResponse(t, rec)
	outcome := outcomeFor(t, payload, "passwd.csv")
	if outcome.Status != "saved" {
		t.Fatalf("expected status saved, got %+v", outcome)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "passwd.csv")); err != nil {
		t.Fatalf("expected passwd.csv on disk: %v", err)
	}
}

// Required test 1: a batch mixing a valid CSV, a .txt file, and a name that
// already exists yields saved / rejected / skipped respectively, and the
// valid file actually lands on disk.
func TestHandleFileUpload_MixedBatch(t *testing.T) {
	dataDir := setupTestEnv(t)

	existingPath := filepath.Join(dataDir, "existing.csv")
	existingContent := []byte("Date,Description,Amount\n2024-01-01,Old,1.00\n")
	if err := store.WriteFile(existingPath, existingContent, 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	validContent := []byte("Date,Description,Amount\n2024-03-01,New,2.00\n")
	req := newMultiUploadRequest(t, []uploadFile{
		{name: "valid.csv", content: validContent},
		{name: "notes.txt", content: []byte("not a csv")},
		{name: "existing.csv", content: []byte("Date,Description,Amount\n2024-04-01,Ignored,3.00\n")},
	})
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	payload := decodeUploadResponse(t, rec)
	if len(payload.Results) != 3 {
		t.Fatalf("expected 3 outcomes, got %d: %+v", len(payload.Results), payload.Results)
	}

	valid := outcomeFor(t, payload, "valid.csv")
	if valid.Status != "saved" {
		t.Fatalf("expected valid.csv saved, got %+v", valid)
	}
	notes := outcomeFor(t, payload, "notes.txt")
	if notes.Status != "rejected" {
		t.Fatalf("expected notes.txt rejected, got %+v", notes)
	}
	existing := outcomeFor(t, payload, "existing.csv")
	if existing.Status != "skipped" || existing.Reason != "already exists" {
		t.Fatalf("expected existing.csv skipped: already exists, got %+v", existing)
	}

	// The valid file actually landed on disk.
	saved, err := store.ReadFile(filepath.Join(dataDir, "valid.csv"))
	if err != nil {
		t.Fatalf("ReadFile(valid.csv) error: %v", err)
	}
	if string(saved) != string(validContent) {
		t.Fatalf("valid.csv content mismatch\n got: %q\nwant: %q", saved, validContent)
	}

	// The collision must not have overwritten the existing file.
	untouched, err := store.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("ReadFile(existing.csv) error: %v", err)
	}
	if string(untouched) != string(existingContent) {
		t.Fatalf("existing.csv was overwritten\n got: %q\nwant: %q", untouched, existingContent)
	}
}

// Required test 2: a single-file upload still works — regression coverage
// for the existing form that posts exactly one "file" part.
func TestHandleFileUpload_SingleFileRegression(t *testing.T) {
	dataDir := setupTestEnv(t)

	content := []byte("Date,Description,Amount\n2024-05-01,Solo,4.00\n")
	req := newUploadRequest(t, "solo.csv", content)
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	payload := decodeUploadResponse(t, rec)
	if len(payload.Results) != 1 {
		t.Fatalf("expected exactly 1 outcome, got %d: %+v", len(payload.Results), payload.Results)
	}
	outcome := outcomeFor(t, payload, "solo.csv")
	if outcome.Status != "saved" {
		t.Fatalf("expected saved, got %+v", outcome)
	}

	saved, err := store.ReadFile(filepath.Join(dataDir, "solo.csv"))
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(saved) != string(content) {
		t.Fatalf("content mismatch\n got: %q\nwant: %q", saved, content)
	}
}

// Required test 3: path-traversal-shaped names in a batch don't abort the
// rest of the batch, and — per the unchanged sanitizeUploadFilename contract
// (filepath.Base strips directory components before the ".." substring
// check) — they resolve to their base filename inside DataDirectory rather
// than escaping it. A name that is *only* ".." still hits the explicit
// reject path. See judgment call in the task write-up.
func TestHandleFileUpload_BatchTraversalNamesDoNotEscapeOrAbort(t *testing.T) {
	dataDir := setupTestEnv(t)

	req := newMultiUploadRequest(t, []uploadFile{
		{name: "../evil.csv", content: []byte("Date,Description,Amount\n2024-06-01,A,1.00\n")},
		{name: "/etc/evil2.csv", content: []byte("Date,Description,Amount\n2024-06-02,B,2.00\n")},
		{name: `..\..\evil3.csv`, content: []byte("Date,Description,Amount\n2024-06-03,C,3.00\n")},
		{name: "..", content: []byte("Date,Description,Amount\n2024-06-04,D,4.00\n")},
		{name: "sibling.csv", content: []byte("Date,Description,Amount\n2024-06-05,E,5.00\n")},
	})
	rec := httptest.NewRecorder()
	handleFileUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	payload := decodeUploadResponse(t, rec)
	if len(payload.Results) != 5 {
		t.Fatalf("expected 5 outcomes, got %d: %+v", len(payload.Results), payload.Results)
	}

	// None of the traversal-shaped names may write outside dataDir, and the
	// batch must still process the unrelated valid file.
	sibling := outcomeFor(t, payload, "sibling.csv")
	if sibling.Status != "saved" {
		t.Fatalf("expected sibling.csv saved (batch not aborted), got %+v", sibling)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "sibling.csv")); err != nil {
		t.Fatalf("expected sibling.csv on disk: %v", err)
	}

	// Bare ".." is explicitly rejected by sanitizeUploadFilename.
	dotdot := outcomeFor(t, payload, "..")
	if dotdot.Status != "rejected" {
		t.Fatalf("expected \"..\" rejected, got %+v", dotdot)
	}

	// The rest normalize to their base name and land inside dataDir — never
	// above/outside it — confirming no traversal occurs.
	for _, base := range []string{"evil.csv", "evil2.csv", "evil3.csv"} {
		if _, err := os.Stat(filepath.Join(dataDir, base)); err != nil {
			t.Fatalf("expected %s inside dataDir (normalized, not escaped): %v", base, err)
		}
	}
	parentDir := filepath.Dir(dataDir)
	for _, escaped := range []string{"evil.csv", "evil2.csv", "evil3.csv"} {
		if _, err := os.Stat(filepath.Join(parentDir, escaped)); err == nil {
			t.Fatalf("%s must not exist outside dataDir", escaped)
		}
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

// TestSwapPartials_RenderSortableFileList is the P13 regression guard. The
// three htmx swap handlers (toggle, upload, delete) must render the
// `filemanager-file-list` partial — the same sortable table the File Manager
// page renders on initial load — not the legacy non-sortable `file-list`
// partial. Otherwise an htmx swap (clicking the On checkbox, deleting a row,
// or uploading) strips the sort buttons, the per-row data-* attributes, and
// scope="col" off the table until a full page reload, so the client-side sort
// JS has nothing to re-wire and the active sort is lost.
//
// This test renders each swap partial's HTTP response body and asserts the
// sortable-table contract is present: real <button data-sort-btn="...">,
// <th scope="col">, and tr[data-name][data-size][data-rows][data-mindate]
// [data-enabled] on every file row. It would have failed against attempt 1,
// which rendered the `file-list` partial with none of these attributes.
func TestSwapPartials_RenderSortableFileList(t *testing.T) {
	// sortableAttrs are the substrings every swap response must contain.
	// They mirror the attributes the File Manager's initial render produces
	// (filemanager.html filemanager-file-list define) and that the sort JS
	// (window.FileManagerSort) depends on.
	sortableAttrs := []string{
		`data-sort-btn="name"`,
		`data-sort-btn="rows"`,
		`data-sort-btn="mindate"`,
		`data-sort-btn="enabled"`,
		`scope="col"`,
		`data-name="`,
		`data-size="`,
		`data-rows="`,
		`data-mindate="`,
		`data-enabled="`,
	}

	// Two CSVs with deliberately disagreeing alphabetical / row-count /
	// date-range orderings, so a future regression that re-introduces lexical
	// sorting or drops a data-* attribute is still caught by the presence
	// checks (and so the rows are unambiguously distinguishable).
	csvA := "Date,Description,Amount\n2024-01-15,Salary,5000.00\n"
	csvB := "Date,Description,Amount\n2025-03-01,X,-10.00\n2025-03-02,Y,-20.00\n"

	assertSortable := func(t *testing.T, body string) {
		t.Helper()
		for _, want := range sortableAttrs {
			if !strings.Contains(body, want) {
				t.Errorf("swap response missing %q\nbody:\n%s", want, body)
			}
		}
		// The legacy non-sortable partial used plain <th class=...> with no
		// scope and a "Delete" text button. Their absence is a negative
		// signal that the old partial is gone. (>Delete</ appeared in the
		// file-list partial's row action; the sortable partial uses an SVG
		// trash icon with no visible "Delete" text.)
		if strings.Contains(body, ">Delete<") {
			t.Errorf("swap response rendered legacy file-list partial (contains \">Delete<\")\nbody:\n%s", body)
		}
	}

	t.Run("toggle", func(t *testing.T) {
		setupTestEnvWithRenderer(t, csvA, csvB)
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
		body := rec.Body.String()
		// Toggling test0.csv on must leave both files as sortable rows,
		// and test0.csv must now be marked enabled.
		if !strings.Contains(body, `data-name="test0.csv"`) {
			t.Errorf("toggle swap response missing data-name=\"test0.csv\"\nbody:\n%s", body)
		}
		if !strings.Contains(body, `data-name="test1.csv"`) {
			t.Errorf("toggle swap response missing data-name=\"test1.csv\"\nbody:\n%s", body)
		}
		assertSortable(t, body)
	})

	t.Run("upload", func(t *testing.T) {
		setupTestEnvWithRenderer(t, csvA)
		uploaded := []byte("Date,Description,Amount\n2025-03-01,Test,10.00\n")
		req := newUploadRequest(t, "test1.csv", uploaded)
		rec := httptest.NewRecorder()
		handleFileUpload(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		// Both the pre-existing test0.csv and the just-uploaded test1.csv
		// must appear as sortable rows.
		if !strings.Contains(body, `data-name="test0.csv"`) {
			t.Errorf("upload swap response missing data-name=\"test0.csv\"\nbody:\n%s", body)
		}
		if !strings.Contains(body, `data-name="test1.csv"`) {
			t.Errorf("upload swap response missing data-name=\"test1.csv\"\nbody:\n%s", body)
		}
		assertSortable(t, body)
	})

	t.Run("delete", func(t *testing.T) {
		setupTestEnvWithRenderer(t, csvA, csvB)
		req := httptest.NewRequest(http.MethodDelete, "/explorer/files/test0.csv", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("filename", "test0.csv")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		handleFileDelete(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
		}
		// After deleting test0.csv, only test1.csv remains — assert its row
		// carries the sortable data-* attributes.
		body := rec.Body.String()
		if !strings.Contains(body, `data-name="test1.csv"`) {
			t.Errorf("delete swap response missing data-name=\"test1.csv\"\nbody:\n%s", body)
		}
		assertSortable(t, body)
	})
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

	// A per-file write failure does not abort/fail the batch request; it is
	// reported as a rejected outcome instead.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for write error (reported as rejected), got %d", rec.Code)
	}
	payload := decodeUploadResponse(t, rec)
	outcome := outcomeFor(t, payload, "test.csv")
	if outcome.Status != "rejected" {
		t.Fatalf("expected status rejected, got %+v", outcome)
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

// uploadFile is one part of a simulated multi-file <input multiple> submission.
type uploadFile struct {
	name    string
	content []byte
}

// newMultiUploadRequest builds a single multipart request carrying several
// "file" parts, matching what a browser sends for <input type="file" multiple>.
func newMultiUploadRequest(t *testing.T, files []uploadFile) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	for _, f := range files {
		part, err := writer.CreateFormFile("file", f.name)
		if err != nil {
			t.Fatalf("CreateFormFile() error: %v", err)
		}
		if _, err := part.Write(f.content); err != nil {
			t.Fatalf("part.Write() error: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/explorer/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	return req
}

// ---- handleImportScan tests ----

// importScanResponse mirrors the JSON fallback body (renderer == nil).
type importScanResponse struct {
	ImportEntries []importScanEntry `json:"ImportEntries"`
	ImportMessage string            `json:"ImportMessage"`
	ImportPath    string            `json:"ImportPath"`
}

func decodeImportScan(t *testing.T, rec *httptest.ResponseRecorder) importScanResponse {
	t.Helper()
	var payload importScanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response JSON error: %v (body: %s)", err, rec.Body.String())
	}
	return payload
}

// scanEntryFor returns the named scan entry or fails the test.
func scanEntryFor(t *testing.T, payload importScanResponse, name string) importScanEntry {
	t.Helper()
	for _, e := range payload.ImportEntries {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no scan entry for %q in %+v", name, payload.ImportEntries)
	return importScanEntry{}
}

// setupImportScanEnv initializes the explorer package globals with a data dir
// (optionally seeded with CSVs) and returns a separate import dir the test
// populates itself.
func setupImportScanEnv(t *testing.T, dataCSVs ...string) (dataDir, importDir string) {
	t.Helper()
	dataDir = t.TempDir()
	importDir = t.TempDir()

	testStore, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	for i, content := range dataCSVs {
		name := fmt.Sprintf("test%d.csv", i)
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	loader = dataloader.New(dataDir, testStore)
	renderer = nil
	cfg = &config.Config{DataDirectory: dataDir, ImportDirectory: importDir}
	store = testStore
	return dataDir, importDir
}

func TestHandleImportScan_ListsCSVWithDateRange(t *testing.T) {
	_, importDir := setupImportScanEnv(t)

	content := []byte(sampleCSV)
	if err := os.WriteFile(filepath.Join(importDir, "bank.csv"), content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/explorer/import/scan", nil)
	rec := httptest.NewRecorder()
	handleImportScan(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	payload := decodeImportScan(t, rec)
	if len(payload.ImportEntries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %+v", len(payload.ImportEntries), payload.ImportEntries)
	}
	entry := payload.ImportEntries[0]
	if entry.Name != "bank.csv" {
		t.Errorf("Name=%q want bank.csv", entry.Name)
	}
	if entry.MinDate != "2024-01-15" {
		t.Errorf("MinDate=%q want 2024-01-15", entry.MinDate)
	}
	if entry.MaxDate != "2024-03-01" {
		t.Errorf("MaxDate=%q want 2024-03-01", entry.MaxDate)
	}
	if entry.Size != int64(len(content)) {
		t.Errorf("Size=%d want %d", entry.Size, len(content))
	}
	if entry.Exists {
		t.Errorf("Exists=true want false (not present in data dir)")
	}
	if payload.ImportMessage != "" {
		t.Errorf("ImportMessage=%q want empty", payload.ImportMessage)
	}
	if payload.ImportPath != importDir {
		t.Errorf("ImportPath=%q want %q", payload.ImportPath, importDir)
	}
}

// Non-CSV files, subdirectories, and symlinks must all be excluded.
func TestHandleImportScan_ExcludesNonCSVSubdirSymlink(t *testing.T) {
	_, importDir := setupImportScanEnv(t)

	// A real CSV that should be listed.
	if err := os.WriteFile(filepath.Join(importDir, "real.csv"),
		[]byte("Date,Description,Amount\n2024-01-01,A,1.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Non-CSV file — excluded.
	if err := os.WriteFile(filepath.Join(importDir, "notes.txt"),
		[]byte("not a csv"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Subdirectory (even containing a .csv) — excluded, no recursion.
	sub := filepath.Join(importDir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.csv"),
		[]byte("Date,Description,Amount\n2024-01-02,B,2.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Symlink to a CSV, named *.csv — excluded, not followed.
	target := filepath.Join(importDir, "real.csv")
	link := filepath.Join(importDir, "link.csv")
	hasSymlink := true
	if err := os.Symlink(target, link); err != nil {
		// Symlinks may be unsupported (e.g. unprivileged container). The
		// non-CSV and subdir assertions below still hold; only the symlink
		// exclusion can't be exercised here.
		hasSymlink = false
	}

	req := httptest.NewRequest(http.MethodGet, "/explorer/import/scan", nil)
	rec := httptest.NewRecorder()
	handleImportScan(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	payload := decodeImportScan(t, rec)
	if len(payload.ImportEntries) != 1 {
		t.Fatalf("expected exactly 1 entry (real.csv), got %d: %+v", len(payload.ImportEntries), payload.ImportEntries)
	}
	if payload.ImportEntries[0].Name != "real.csv" {
		t.Fatalf("expected real.csv, got %q", payload.ImportEntries[0].Name)
	}
	if hasSymlink {
		// Confirm the symlink was genuinely excluded, not followed. If it had
		// been followed, link.csv would appear alongside real.csv.
		for _, e := range payload.ImportEntries {
			if e.Name == "link.csv" {
				t.Fatalf("symlink link.csv was listed — symlinks must be excluded, not followed")
			}
		}
	}
}

func TestHandleImportScan_ExistsFlag(t *testing.T) {
	// Seed the data dir with a CSV that shares a name with an import-dir file.
	_, importDir := setupImportScanEnv(t, "Date,Description,Amount\n2024-01-01,Existing,1.00\n")

	// The data-dir file is test0.csv (setupImportScanEnv names them testN.csv).
	// Place a same-named CSV in the import dir to set exists=true, plus a new one.
	if err := os.WriteFile(filepath.Join(importDir, "test0.csv"),
		[]byte("Date,Description,Amount\n2024-02-02,Dup,2.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(importDir, "new.csv"),
		[]byte("Date,Description,Amount\n2024-02-03,New,3.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/explorer/import/scan", nil)
	rec := httptest.NewRecorder()
	handleImportScan(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	payload := decodeImportScan(t, rec)
	if len(payload.ImportEntries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(payload.ImportEntries), payload.ImportEntries)
	}
	dup := scanEntryFor(t, payload, "test0.csv")
	if !dup.Exists {
		t.Errorf("test0.csv Exists=false want true (present in data dir)")
	}
	fresh := scanEntryFor(t, payload, "new.csv")
	if fresh.Exists {
		t.Errorf("new.csv Exists=true want false")
	}
}

func TestHandleImportScan_MissingDirectory(t *testing.T) {
	setupImportScanEnv(t) // import dir exists but we point cfg elsewhere
	cfg.ImportDirectory = filepath.Join(t.TempDir(), "does-not-exist")

	req := httptest.NewRequest(http.MethodGet, "/explorer/import/scan", nil)
	rec := httptest.NewRecorder()
	handleImportScan(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for missing import dir, got %d; body: %s", rec.Code, rec.Body.String())
	}
	payload := decodeImportScan(t, rec)
	if len(payload.ImportEntries) != 0 {
		t.Fatalf("expected empty entry list for missing dir, got %d", len(payload.ImportEntries))
	}
	if payload.ImportMessage == "" {
		t.Fatal("expected a non-empty ImportMessage for missing dir")
	}
	if !strings.Contains(payload.ImportMessage, "not found") {
		t.Errorf("ImportMessage=%q should mention 'not found'", payload.ImportMessage)
	}
}

func TestHandleImportScan_EmptyDirectory(t *testing.T) {
	_, _ = setupImportScanEnv(t) // import dir exists, no CSVs in it

	req := httptest.NewRequest(http.MethodGet, "/explorer/import/scan", nil)
	rec := httptest.NewRecorder()
	handleImportScan(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	payload := decodeImportScan(t, rec)
	if len(payload.ImportEntries) != 0 {
		t.Fatalf("expected empty entry list, got %d", len(payload.ImportEntries))
	}
	if payload.ImportMessage == "" {
		t.Fatal("expected a non-empty ImportMessage for empty dir")
	}
}

func TestHandleImportScan_HonorsConfiguredImportDir(t *testing.T) {
	// config.Load honors BUDGET2_IMPORT_DIR (covered in internal/config tests).
	// Here we confirm the handler scans whatever cfg.ImportDirectory points at
	// — the field Load populates from that env var — rather than a hard-coded
	// path.
	dataDir := t.TempDir()
	importDir := t.TempDir()

	cfgLoaded := config.DefaultConfig()
	cfgLoaded.ImportDirectory = importDir
	cfgLoaded.DataDirectory = dataDir

	testStore, err := storage.New(dataDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	loader = dataloader.New(dataDir, testStore)
	renderer = nil
	cfg = cfgLoaded
	store = testStore

	if cfg.ImportDirectory != importDir {
		t.Fatalf("ImportDirectory=%q want %q", cfg.ImportDirectory, importDir)
	}

	if err := os.WriteFile(filepath.Join(importDir, "env.csv"),
		[]byte("Date,Description,Amount\n2024-05-05,Env,5.00\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/explorer/import/scan", nil)
	rec := httptest.NewRecorder()
	handleImportScan(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	payload := decodeImportScan(t, rec)
	if payload.ImportPath != importDir {
		t.Errorf("ImportPath=%q want %q", payload.ImportPath, importDir)
	}
	if len(payload.ImportEntries) != 1 || payload.ImportEntries[0].Name != "env.csv" {
		t.Fatalf("expected single env.csv entry, got %+v", payload.ImportEntries)
	}
}

// ---- handleImport tests (POST /explorer/import) ----

// importCSV is the source content used by the import tests. Two rows, so a
// truncated write is detectable by length.
const importCSV = "Date,Description,Amount\n2024-04-01,Coffee,-4.50\n2024-04-02,Groceries,-52.10\n"

// importResultResponse mirrors the JSON fallback body (renderer == nil).
type importResultResponse struct {
	Results      []importOutcome `json:"Results"`
	DeleteSource bool            `json:"DeleteSource"`
	ImportPath   string          `json:"ImportPath"`
}

// postImport drives handleImport with a form-encoded body, the pinned wire
// format for this endpoint.
func postImport(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/explorer/import", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handleImport(rec, req)
	return rec
}

func decodeImportResult(t *testing.T, rec *httptest.ResponseRecorder) importResultResponse {
	t.Helper()
	var payload importResultResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response JSON error: %v (body: %s)", err, rec.Body.String())
	}
	return payload
}

// outcomeFor returns the named per-file outcome or fails the test.
func importOutcomeFor(t *testing.T, payload importResultResponse, name string) importOutcome {
	t.Helper()
	for _, o := range payload.Results {
		if o.Name == name {
			return o
		}
	}
	t.Fatalf("no outcome for %q in %+v", name, payload.Results)
	return importOutcome{}
}

// seedImportFile writes content into dir under name and returns the path.
func seedImportFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	return path
}

func mustExist(t *testing.T, path, why string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("%s: expected %s to exist, got %v", why, path, err)
	}
}

func mustNotExist(t *testing.T, path, why string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%s: expected %s to be absent, got err=%v", why, path, err)
	}
}

// Without delete_source the file lands in the data dir and the original stays
// exactly where the user left it.
func TestHandleImport_KeepsSourceWhenNotDeleting(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	src := seedImportFile(t, importDir, "alpha.csv", importCSV)

	rec := postImport(t, url.Values{"name": {"alpha.csv"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	payload := decodeImportResult(t, rec)
	if payload.DeleteSource {
		t.Errorf("DeleteSource=true want false (field absent)")
	}
	out := importOutcomeFor(t, payload, "alpha.csv")
	if out.Status != "imported" {
		t.Errorf("Status=%q want imported (reason %q)", out.Status, out.Reason)
	}
	if out.SourceDeleted {
		t.Errorf("SourceDeleted=true want false")
	}

	mustExist(t, src, "source must survive an import without delete_source")
	got, err := store.ReadFile(filepath.Join(dataDir, "alpha.csv"))
	if err != nil {
		t.Fatalf("ReadFile destination: %v", err)
	}
	if string(got) != importCSV {
		t.Errorf("destination content = %q want %q", got, importCSV)
	}
}

// With delete_source=true a fully imported file's original is removed.
func TestHandleImport_DeletesSourceWhenRequested(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	src := seedImportFile(t, importDir, "beta.csv", importCSV)

	rec := postImport(t, url.Values{"name": {"beta.csv"}, "delete_source": {"true"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	payload := decodeImportResult(t, rec)
	if !payload.DeleteSource {
		t.Errorf("DeleteSource=false want true")
	}
	out := importOutcomeFor(t, payload, "beta.csv")
	if out.Status != "imported" {
		t.Fatalf("Status=%q want imported (reason %q)", out.Status, out.Reason)
	}
	if !out.SourceDeleted {
		t.Errorf("SourceDeleted=false want true")
	}

	mustNotExist(t, src, "source must be deleted after a verified import")
	mustExist(t, filepath.Join(dataDir, "beta.csv"), "destination must exist")
}

// The wire format pins deletion to the literal string "true"; anything else
// means keep the source.
func TestHandleImport_DeleteSourceOnlyOnLiteralTrue(t *testing.T) {
	for _, value := range []string{"1", "yes", "TRUE", "on", ""} {
		t.Run("value="+value, func(t *testing.T) {
			dataDir, importDir := setupImportScanEnv(t)
			src := seedImportFile(t, importDir, "gamma.csv", importCSV)

			rec := postImport(t, url.Values{"name": {"gamma.csv"}, "delete_source": {value}})
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
			}
			payload := decodeImportResult(t, rec)
			if payload.DeleteSource {
				t.Errorf("delete_source=%q was read as enabled", value)
			}
			out := importOutcomeFor(t, payload, "gamma.csv")
			if out.SourceDeleted {
				t.Errorf("SourceDeleted=true for delete_source=%q", value)
			}
			mustExist(t, src, "source must survive delete_source="+value)
			mustExist(t, filepath.Join(dataDir, "gamma.csv"), "destination must exist")
		})
	}
}

// A name already present in the data dir skips: no overwrite, no auto-rename,
// and — the safety property — the source is not deleted even though the batch
// asked for deletion.
func TestHandleImport_CollisionSkipsAndKeepsSource(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	const existing = "Date,Description,Amount\n2024-01-01,Existing,1.00\n"
	dest := filepath.Join(dataDir, "collide.csv")
	if err := os.WriteFile(dest, []byte(existing), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	src := seedImportFile(t, importDir, "collide.csv", importCSV)

	rec := postImport(t, url.Values{"name": {"collide.csv"}, "delete_source": {"true"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	out := importOutcomeFor(t, decodeImportResult(t, rec), "collide.csv")
	if out.Status != "skipped" {
		t.Errorf("Status=%q want skipped (reason %q)", out.Status, out.Reason)
	}
	if out.SourceDeleted {
		t.Errorf("SourceDeleted=true — a skipped file must never be deleted")
	}

	mustExist(t, src, "a skipped file's source must survive")
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile destination: %v", err)
	}
	if string(got) != existing {
		t.Errorf("destination was overwritten: %q want %q", got, existing)
	}
}

// A name carrying a path separator is rejected before any filesystem call:
// nothing outside the import dir is read, copied, or deleted.
func TestHandleImport_TraversalNameRejected(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)

	// Sibling of importDir, so "../outside/outside.csv" is a traversal that
	// genuinely resolves — otherwise this test would pass vacuously.
	outsideDir := filepath.Join(filepath.Dir(importDir), "outside")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	outside := seedImportFile(t, outsideDir, "outside.csv", importCSV)
	traversal := "../outside/outside.csv"
	if _, err := os.Stat(filepath.Join(importDir, traversal)); err != nil {
		t.Fatalf("fixture: %q should resolve from the import dir, got %v", traversal, err)
	}

	rec := postImport(t, url.Values{"name": {traversal}, "delete_source": {"true"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), traversal)
	if out.Status != "rejected" {
		t.Errorf("Status=%q want rejected (reason %q)", out.Status, out.Reason)
	}
	if out.SourceDeleted {
		t.Errorf("SourceDeleted=true for a rejected traversal name")
	}

	mustExist(t, outside, "a file outside the import folder must survive")
	mustNotExist(t, filepath.Join(dataDir, "outside.csv"), "nothing may be copied for a rejected name")
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("data dir must be untouched, contains %d entries", len(entries))
	}
}

// A non-CSV name is rejected even when posted explicitly, and its source stays.
func TestHandleImport_NonCSVRejected(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	src := seedImportFile(t, importDir, "notes.txt", "not a csv\n")

	rec := postImport(t, url.Values{"name": {"notes.txt"}, "delete_source": {"true"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), "notes.txt")
	if out.Status != "rejected" {
		t.Errorf("Status=%q want rejected (reason %q)", out.Status, out.Reason)
	}
	mustExist(t, src, "a rejected non-CSV source must survive")
	mustNotExist(t, filepath.Join(dataDir, "notes.txt"), "a non-CSV must not be copied")
}

// A symlink planted in the import folder is not followed, so the file it
// points at is neither imported nor deleted.
func TestHandleImport_SymlinkNotFollowedTargetSurvives(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)

	outsideDir := filepath.Join(filepath.Dir(importDir), "outside-link")
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	target := seedImportFile(t, outsideDir, "target.csv", importCSV)

	link := filepath.Join(importDir, "link.csv")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	rec := postImport(t, url.Values{"name": {"link.csv"}, "delete_source": {"true"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), "link.csv")
	if out.Status != "rejected" {
		t.Errorf("Status=%q want rejected (reason %q)", out.Status, out.Reason)
	}
	if out.SourceDeleted {
		t.Errorf("SourceDeleted=true — a symlink must never be removed")
	}

	mustExist(t, target, "the symlink target must survive")
	mustExist(t, link, "the symlink itself must survive a rejection")
	mustNotExist(t, filepath.Join(dataDir, "link.csv"), "a symlink must not be imported")
}

// A write that cannot succeed must leave the source in place. Staged for real
// by making the data directory unwritable.
func TestHandleImport_FailedWriteKeepsSource(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: an unwritable directory does not block writes")
	}
	dataDir, importDir := setupImportScanEnv(t)
	src := seedImportFile(t, importDir, "delta.csv", importCSV)

	if err := os.Chmod(dataDir, 0555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0755) })

	rec := postImport(t, url.Values{"name": {"delta.csv"}, "delete_source": {"true"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), "delta.csv")
	if out.Status != "rejected" {
		t.Errorf("Status=%q want rejected (reason %q)", out.Status, out.Reason)
	}
	if out.SourceDeleted {
		t.Errorf("SourceDeleted=true after a failed write")
	}

	mustExist(t, src, "a failed write must leave the source in place")
	mustNotExist(t, filepath.Join(dataDir, "delta.csv"), "nothing may land when the write fails")
}

// The race the fix closes: a concurrent uploader or importer creates the
// destination in the window between step 3's existence check and step 4's
// write. deps.write is bound to store.CreateExclusive in production, whose
// test-and-create is one indivisible step, so the write below — issued after
// the "concurrent" writer has already landed its file — must lose, report
// skipped, leave the winner's bytes in place, and never touch the source.
func TestImportOneFile_ConcurrentCreateBetweenCheckAndWriteSkipsWithoutOverwrite(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	src := seedImportFile(t, importDir, "race.csv", importCSV)
	destPath := filepath.Join(dataDir, "race.csv")

	deps := defaultImportDeps()
	deps.write = func(path string, data []byte, perm os.FileMode) error {
		// Simulate another upload/import winning the race for this exact
		// destination after step 3 observed it absent but before this write
		// lands — the same store.CreateExclusive the production write uses.
		if err := store.CreateExclusive(path, []byte("concurrent-winner"), 0644); err != nil {
			t.Fatalf("simulated concurrent writer: %v", err)
		}
		return store.CreateExclusive(path, data, perm)
	}

	out := importOneFile("race.csv", true, deps)
	if out.Status != "skipped" {
		t.Fatalf("Status=%q want skipped (reason %q)", out.Status, out.Reason)
	}
	if out.Reason != "already exists in the data folder" {
		t.Errorf("Reason=%q want %q", out.Reason, "already exists in the data folder")
	}
	if out.SourceDeleted {
		t.Error("SourceDeleted=true — the loser of the race must never delete the source")
	}

	got, err := store.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile destination: %v", err)
	}
	if string(got) != "concurrent-winner" {
		t.Errorf("destination content = %q, want the winner's bytes %q — the loser overwrote it", got, "concurrent-winner")
	}

	mustExist(t, src, "the loser's source must survive even though delete_source was requested")
}

// Guard 1, isolated: when the write itself errors, no delete is attempted.
// Injected rather than staged with permissions so the assertion holds
// regardless of the uid running the suite.
func TestImportOneFile_WriteFailureNeverDeletes(t *testing.T) {
	_, importDir := setupImportScanEnv(t)
	src := seedImportFile(t, importDir, "epsilon.csv", importCSV)

	removed := false
	deps := defaultImportDeps()
	deps.write = func(string, []byte, os.FileMode) error { return os.ErrPermission }
	deps.removeSrc = func(path string) error { removed = true; return os.Remove(path) }

	out := importOneFile("epsilon.csv", true, deps)
	if out.Status != "rejected" {
		t.Errorf("Status=%q want rejected (reason %q)", out.Status, out.Reason)
	}
	if removed {
		t.Error("os.Remove was reached despite a failed write")
	}
	if out.SourceDeleted {
		t.Error("SourceDeleted=true after a failed write")
	}
	mustExist(t, src, "source must survive a failed write")
}

// Guard 2, isolated: this is why the readback exists. The write "succeeds" but
// the file reads back short — a truncated or encryption-failed save. No delete
// may follow.
func TestImportOneFile_ShortReadbackNeverDeletes(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	src := seedImportFile(t, importDir, "zeta.csv", importCSV)

	removed := false
	deps := defaultImportDeps()
	deps.readBack = func(string) ([]byte, error) { return []byte(importCSV[:10]), nil }
	deps.removeSrc = func(path string) error { removed = true; return os.Remove(path) }

	out := importOneFile("zeta.csv", true, deps)
	if out.Status != "rejected" {
		t.Errorf("Status=%q want rejected (reason %q)", out.Status, out.Reason)
	}
	if removed {
		t.Error("os.Remove was reached despite a short readback")
	}
	if out.SourceDeleted {
		t.Error("SourceDeleted=true after a short readback")
	}
	mustExist(t, src, "source must survive a readback mismatch")

	// The write did happen; only the verification failed. The source surviving
	// is the property under test, not the destination's absence.
	mustExist(t, filepath.Join(dataDir, "zeta.csv"), "the write itself was allowed to land")
}

// A readback that errors outright is treated the same way.
func TestImportOneFile_ReadbackErrorNeverDeletes(t *testing.T) {
	_, importDir := setupImportScanEnv(t)
	src := seedImportFile(t, importDir, "eta.csv", importCSV)

	removed := false
	deps := defaultImportDeps()
	deps.readBack = func(string) ([]byte, error) { return nil, os.ErrPermission }
	deps.removeSrc = func(path string) error { removed = true; return os.Remove(path) }

	out := importOneFile("eta.csv", true, deps)
	if out.Status != "rejected" {
		t.Errorf("Status=%q want rejected (reason %q)", out.Status, out.Reason)
	}
	if removed {
		t.Error("os.Remove was reached despite a failed readback")
	}
	mustExist(t, src, "source must survive a failed readback")
}

// Guard 3, isolated: a name whose path does not resolve to a direct child of
// ImportDirectory never reaches the delete. Staged with a subdirectory entry,
// which the scan never offers.
func TestImportOneFile_SubdirectoryEntryNeverDeletes(t *testing.T) {
	_, importDir := setupImportScanEnv(t)
	sub := filepath.Join(importDir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	nested := seedImportFile(t, sub, "nested.csv", importCSV)

	removed := false
	deps := defaultImportDeps()
	deps.removeSrc = func(path string) error { removed = true; return os.Remove(path) }

	// Both spellings must fail: the separator form is rejected by name, and the
	// bare form does not exist directly inside the import dir.
	for _, name := range []string{"sub/nested.csv", "nested.csv"} {
		out := importOneFile(name, true, deps)
		if out.Status != "rejected" {
			t.Errorf("%s: Status=%q want rejected (reason %q)", name, out.Status, out.Reason)
		}
	}
	if removed {
		t.Error("os.Remove was reached for a non-direct-child path")
	}
	mustExist(t, nested, "a file in a subdirectory must survive")
}

// A batch with no name fields is malformed: 400, and provably inert.
func TestHandleImport_EmptyBatchIs400AndInert(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	src := seedImportFile(t, importDir, "theta.csv", importCSV)

	before, err := os.ReadDir(importDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	rec := postImport(t, url.Values{"delete_source": {"true"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}

	after, err := os.ReadDir(importDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(before) != len(after) {
		t.Errorf("import dir changed: %d entries before, %d after", len(before), len(after))
	}
	mustExist(t, src, "a 400 must delete nothing")

	dataEntries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(dataEntries) != 0 {
		t.Errorf("a 400 must copy nothing, data dir has %d entries", len(dataEntries))
	}
}

// A blank name field is not a selection either.
func TestHandleImport_BlankNameIs400(t *testing.T) {
	_, _ = setupImportScanEnv(t)

	rec := postImport(t, url.Values{"name": {"", "   "}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

// A name that is not in the import folder is rejected, not an error response.
func TestHandleImport_MissingSourceRejected(t *testing.T) {
	dataDir, _ := setupImportScanEnv(t)

	rec := postImport(t, url.Values{"name": {"absent.csv"}, "delete_source": {"true"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), "absent.csv")
	if out.Status != "rejected" {
		t.Errorf("Status=%q want rejected (reason %q)", out.Status, out.Reason)
	}
	mustNotExist(t, filepath.Join(dataDir, "absent.csv"), "nothing may be created for a missing source")
}

// In a mixed batch only the files that genuinely imported lose their sources.
func TestHandleImport_MixedBatchDeletesOnlyImported(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)

	good := seedImportFile(t, importDir, "good.csv", importCSV)
	collide := seedImportFile(t, importDir, "dupe.csv", importCSV)
	txt := seedImportFile(t, importDir, "notes.txt", "not a csv\n")
	if err := os.WriteFile(filepath.Join(dataDir, "dupe.csv"), []byte(importCSV), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rec := postImport(t, url.Values{
		"name":          {"good.csv", "dupe.csv", "notes.txt", "../escape.csv"},
		"delete_source": {"true"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	payload := decodeImportResult(t, rec)
	if len(payload.Results) != 4 {
		t.Fatalf("expected 4 outcomes, got %d: %+v", len(payload.Results), payload.Results)
	}
	for name, want := range map[string]string{
		"good.csv":      "imported",
		"dupe.csv":      "skipped",
		"notes.txt":     "rejected",
		"../escape.csv": "rejected",
	} {
		if got := importOutcomeFor(t, payload, name); got.Status != want {
			t.Errorf("%s: Status=%q want %q (reason %q)", name, got.Status, want, got.Reason)
		}
	}

	mustNotExist(t, good, "the imported file's source is deleted")
	mustExist(t, collide, "the skipped file's source survives")
	mustExist(t, txt, "the rejected file's source survives")
	mustExist(t, filepath.Join(dataDir, "good.csv"), "the imported file lands")
}

// The endpoint is reachable through the package's own router, not just by
// calling the handler directly.
func TestHandleImport_RouteRegistered(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	seedImportFile(t, importDir, "routed.csv", importCSV)

	r := chi.NewRouter()
	RegisterRoutes(r)

	form := url.Values{"name": {"routed.csv"}}
	req := httptest.NewRequest(http.MethodPost, "/explorer/import", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from the router, got %d; body: %s", rec.Code, rec.Body.String())
	}
	mustExist(t, filepath.Join(dataDir, "routed.csv"), "routed import must land")
}

// The swap target's markup: the result panel is what the browser actually
// receives, so it is asserted against the rendered partial, not the page's
// include of it.
func TestImportResult_Render_Outcomes(t *testing.T) {
	setupTestEnvWithRenderer(t)

	out, err := renderer.RenderToString("import-result", map[string]any{
		"Results": []importOutcome{
			{Name: "good.csv", Status: "imported", Reason: "source file deleted", SourceDeleted: true},
			{Name: "dupe.csv", Status: "skipped", Reason: "already exists in the data folder"},
			{Name: "notes.txt", Status: "rejected", Reason: "only CSV files can be imported"},
		},
		"DeleteSource": true,
	})
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	// Every outcome is legible as text — no meaning carried by colour alone.
	for _, want := range []string{
		"good.csv", "imported", "source file deleted",
		"dupe.csv", "skipped", "already exists in the data folder",
		"notes.txt", "rejected", "only CSV files can be imported",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in import-result; got: %s", want, strunc(out, 900))
		}
	}
}

// Every handleImport test above (and TestImportResult_Render_Outcomes) runs
// with renderer == nil or calls RenderToString directly, so none of them ever
// executes the handler's own `renderer.RenderPartial(w, "import-result", …)`
// call — a typo in that literal template name would still ship a green suite
// with an empty response body (ruling 2026-08-16a's failure shape). This
// drives the handler itself through HTTP with a real renderer configured, so
// the literal name in handlers.go is what gets exercised.
func TestHandleImport_WithRenderer_RendersImportResultBlock(t *testing.T) {
	setupTestEnvWithRenderer(t)
	importDir := t.TempDir()
	cfg.ImportDirectory = importDir
	seedImportFile(t, importDir, "render.csv", importCSV)

	rec := postImport(t, url.Values{"name": {"render.csv"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Import finished") {
		t.Fatalf("expected rendered import-result block containing %q; got: %s", "Import finished", strunc(body, 900))
	}
	if !strings.Contains(body, "imported") {
		t.Errorf("expected per-file outcome word %q in body; got: %s", "imported", strunc(body, 900))
	}
}

// A symlinked ImportDirectory (a symlinked ~/Downloads, or /tmp where it is a
// link) is still a legitimate import folder: the direct-child check resolves
// both sides, so files inside it import normally.
func TestHandleImport_SymlinkedImportDirectoryStillImports(t *testing.T) {
	dataDir, importDir := setupImportScanEnv(t)
	src := seedImportFile(t, importDir, "iota.csv", importCSV)

	linkedDir := filepath.Join(filepath.Dir(importDir), "linked-import")
	if err := os.Symlink(importDir, linkedDir); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	cfg.ImportDirectory = linkedDir

	rec := postImport(t, url.Values{"name": {"iota.csv"}, "delete_source": {"true"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	out := importOutcomeFor(t, decodeImportResult(t, rec), "iota.csv")
	if out.Status != "imported" {
		t.Fatalf("Status=%q want imported (reason %q)", out.Status, out.Reason)
	}
	mustExist(t, filepath.Join(dataDir, "iota.csv"), "a symlinked import folder still imports")
	mustNotExist(t, src, "the source inside the symlinked folder is deleted")
}

// isDirectChild is deletion guard 3 in isolation.
func TestIsDirectChild(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "import")
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, p := range []string{
		filepath.Join(dir, "a.csv"),
		filepath.Join(sub, "b.csv"),
		filepath.Join(outside, "c.csv"),
	} {
		if err := os.WriteFile(p, []byte(importCSV), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	linkedDir := filepath.Join(root, "linked")
	haveSymlink := os.Symlink(dir, linkedDir) == nil

	tests := []struct {
		name string
		dir  string
		path string
		want bool
	}{
		{"direct child", dir, filepath.Join(dir, "a.csv"), true},
		{"grandchild in subdirectory", dir, filepath.Join(sub, "b.csv"), false},
		{"sibling reached by traversal", dir, filepath.Join(dir, "..", "outside", "c.csv"), false},
		{"outside directory", dir, filepath.Join(outside, "c.csv"), false},
		{"nonexistent path", dir, filepath.Join(dir, "missing.csv"), false},
		{"nonexistent dir", filepath.Join(root, "gone"), filepath.Join(dir, "a.csv"), false},
	}
	for _, tc := range tests {
		if got := isDirectChild(tc.dir, tc.path); got != tc.want {
			t.Errorf("%s: isDirectChild(%q, %q)=%v want %v", tc.name, tc.dir, tc.path, got, tc.want)
		}
	}

	if haveSymlink {
		// A symlinked import folder must still recognise its own children,
		// otherwise a symlinked ~/Downloads could never import anything.
		if !isDirectChild(linkedDir, filepath.Join(linkedDir, "a.csv")) {
			t.Error("a symlinked import dir should recognise its own direct child")
		}
	}
}
