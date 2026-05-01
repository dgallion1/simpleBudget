package majorexpenses

import (
	"encoding/json"
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
	"budget2/internal/services/storage"
)

func setupTestEnv(t *testing.T) (*dataloader.DataLoader, func()) {
	t.Helper()
	csvDir := t.TempDir()

	// A small CSV so loader.LoadData returns real data.
	csvPath := filepath.Join(csvDir, "test.csv")
	csvContent := "Date,Description,Amount,Type,Category\n" +
		time.Now().AddDate(0, -2, 0).Format("2006-01-02") + ",My Landlord LLC,-1700,Outflow,Housing\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",My Landlord LLC,-3500,Outflow,Housing\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Random Big Purchase,-450,Outflow,Misc\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Tiny Coffee,-5,Outflow,Food\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	store, err := storage.New(csvDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	dl := dataloader.New(csvDir, store)

	Initialize(dl, nil) // renderer = nil → JSON responses for tests

	return dl, func() {}
}

func newRouter() http.Handler {
	r := chi.NewRouter()
	RegisterRoutes(r)
	return r
}

func readJSON(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, string(body))
	}
	return m
}

func TestHandleAdd_Success(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{}
	form.Set("name", "Rent")
	form.Set("keywords", "landlord, my landlord")
	form.Set("expected_min", "1500")
	form.Set("expected_max", "2000")

	req := httptest.NewRequest("POST", "/major-expenses", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	expenses, ok := body["Expenses"].([]interface{})
	if !ok || len(expenses) != 1 {
		t.Fatalf("expected 1 expense, body=%+v", body)
	}
	first := expenses[0].(map[string]interface{})
	if first["name"] != "Rent" {
		t.Errorf("expected name=Rent, got %v", first["name"])
	}
	keywords := first["keywords"].([]interface{})
	if len(keywords) != 2 {
		t.Errorf("expected 2 keywords, got %v", keywords)
	}
}

func TestBuildPageData_IncomeNotIncludedInGroups(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// Seed a CSV that has BOTH an outflow and an income with descriptions
	// matching the same keyword. Without the outflow filter, the keyword
	// match would put both into the same group and inflate count/total.
	csvDir := t.TempDir()
	csvPath := filepath.Join(csvDir, "test.csv")
	csvContent := "Date,Description,Amount,Type,Category\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Anthropic Subscription,-108,Outflow,Software\n" +
		time.Now().AddDate(0, -1, 0).Format("2006-01-02") + ",Anthropic Refund,200,Income,Software\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	store, _ := storage.New(csvDir)
	dl2 := dataloader.New(csvDir, store)
	Initialize(dl2, nil)
	defer Initialize(dl, nil) // restore for any other test that follows

	if _, err := dl2.AddMajorExpense(makeExpense("anthropic", "Anthropic", []string{"anthropic"}, 0, 0)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	data, err := buildPageData(httptest.NewRequest("GET", "/major-expenses", nil))
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}
	// Summaries is a slice of an unexported struct, so use JSON
	// round-trip for assertion.
	body, _ := json.Marshal(data["Summaries"])
	var raw []map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode summaries: %v\n%s", err, body)
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 summary, got %d: %s", len(raw), body)
	}
	count := int(raw[0]["Count"].(float64))
	total := raw[0]["Total"].(float64)
	if count != 1 {
		t.Errorf("expected Count=1 (outflow only), got %d — income is being grouped", count)
	}
	if total != 108 {
		t.Errorf("expected Total=108 (outflow only), got %v — income inflates total", total)
	}
}

func TestHandleAdd_AmountOnlyAccepted(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{}
	form.Set("name", "Car Payment")
	// no keywords — amount-only matching
	form.Set("expected_min", "620")
	form.Set("expected_max", "630")

	req := httptest.NewRequest("POST", "/major-expenses", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for amount-only entry, got %d body=%s", w.Code, w.Body.String())
	}
	out, err := dl.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != 1 || out[0].Name != "Car Payment" || len(out[0].Keywords) != 0 {
		t.Errorf("expected one amount-only entry with no keywords, got %+v", out)
	}
}

func TestHandleAdd_PinOnlyTargetAccepted(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// No keywords, no amount range — this is a "pin-only" target: the
	// user will manually assign transactions to it via the Pin to…
	// dropdown. This is the original Amazon-Books / Amazon-Household
	// use case from the per-transaction-pinning feature.
	form := url.Values{
		"name":         {"Amazon - Books"},
		"keywords":     {""},
		"expected_min": {"0"},
		"expected_max": {"0"},
	}
	req := httptest.NewRequest("POST", "/major-expenses", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("pin-only entry should be accepted, got %d body=%s", w.Code, w.Body.String())
	}
	out, err := dl.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 expense, got %d", len(out))
	}
	if out[0].Name != "Amazon - Books" {
		t.Errorf("name = %q, want %q", out[0].Name, "Amazon - Books")
	}
	if len(out[0].Keywords) != 0 || out[0].ExpectedMin != 0 || out[0].ExpectedMax != 0 {
		t.Errorf("expected empty keywords + zero range, got %+v", out[0])
	}
}

func TestHandleAdd_ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
	}{
		{"missing name", url.Values{"keywords": {"x"}}},
		{"empty keywords with only min", url.Values{"name": {"X"}, "keywords": {"  ,  "}, "expected_min": {"100"}}},
		{"empty keywords with only max", url.Values{"name": {"X"}, "keywords": {"  ,  "}, "expected_max": {"100"}}},
		{"min > max", url.Values{"name": {"X"}, "keywords": {"x"}, "expected_min": {"500"}, "expected_max": {"100"}}},
		{"negative min", url.Values{"name": {"X"}, "keywords": {"x"}, "expected_min": {"-1"}}},
		{"non-numeric min", url.Values{"name": {"X"}, "keywords": {"x"}, "expected_min": {"abc"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cleanup := setupTestEnv(t)
			defer cleanup()

			req := httptest.NewRequest("POST", "/major-expenses", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			newRouter().ServeHTTP(w, req)
			if w.Code < 400 {
				t.Errorf("expected 4xx, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleUpdate_Success(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// seed an entry directly via the loader
	list, err := dl.AddMajorExpense(makeExpense("seed", "Old Name", []string{"old"}, 0, 0))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id := list[0].ID

	form := url.Values{}
	form.Set("name", "New Name")
	form.Set("keywords", "new")
	form.Set("expected_min", "100")
	form.Set("expected_max", "200")
	form.Set("notes", "updated")

	req := httptest.NewRequest("PUT", "/major-expenses/"+id, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	out, err := dl.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out) != 1 || out[0].Name != "New Name" || out[0].ExpectedMax != 200 || out[0].Notes != "updated" {
		t.Errorf("update did not apply: %+v", out)
	}
}

func TestHandleUpdate_MissingID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"name": {"X"}, "keywords": {"x"}}
	req := httptest.NewRequest("PUT", "/major-expenses/missing", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code < 400 {
		t.Errorf("expected error for unknown id, got %d", w.Code)
	}
}

func TestHandleDelete_Success(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	list, err := dl.AddMajorExpense(makeExpense("d", "Doomed", []string{"doomed"}, 0, 0))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id := list[0].ID

	req := httptest.NewRequest("DELETE", "/major-expenses/"+id, nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	out, _ := dl.LoadMajorExpenses()
	if len(out) != 0 {
		t.Errorf("expected list to be empty, got %+v", out)
	}
}

func TestHandleExceptions_ReturnsAllThreeBuckets(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// Seed a Rent definition that the seeded landlord transactions match.
	if _, err := dl.AddMajorExpense(makeExpense("rent", "Rent", []string{"landlord"}, 1500, 2000)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := httptest.NewRequest("GET", "/major-expenses/exceptions", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	match, ok := body["Match"].(map[string]interface{})
	if !ok {
		t.Fatalf("Match missing in response: %+v", body)
	}
	exc, ok := match["Exceptions"].(map[string]interface{})
	if !ok {
		t.Fatalf("Exceptions missing: %+v", match)
	}
	if _, ok := exc["unknown_large"]; !ok {
		t.Error("unknown_large bucket missing")
	}
	if _, ok := exc["anomalous"]; !ok {
		t.Error("anomalous bucket missing")
	}
	if _, ok := exc["new_merchants"]; !ok {
		t.Error("new_merchants bucket missing")
	}
}

func TestHandleMajorExpensesPage_RendersWithEmptyList(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/major-expenses", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	body := readJSON(t, w.Result())
	if got := body["ActiveTab"]; got != "major-expenses" {
		t.Errorf("ActiveTab = %v, want major-expenses", got)
	}
	if expenses, ok := body["Expenses"].([]interface{}); !ok || len(expenses) != 0 {
		t.Errorf("expected empty Expenses list, got %+v", body["Expenses"])
	}
}

func TestParseExpenseForm_TrimsAndDropsEmpty(t *testing.T) {
	form := url.Values{}
	form.Set("name", "  Rent  ")
	form.Set("keywords", " landlord ,  ,my landlord , ")
	form.Set("expected_min", "1500")
	form.Set("expected_max", "")
	form.Set("notes", "  primary residence  ")

	req := httptest.NewRequest("POST", "/major-expenses", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("parse: %v", err)
	}
	me, err := parseExpenseForm(req)
	if err != nil {
		t.Fatalf("parseExpenseForm: %v", err)
	}
	if me.Name != "Rent" {
		t.Errorf("name not trimmed: %q", me.Name)
	}
	if len(me.Keywords) != 2 {
		t.Errorf("expected 2 keywords, got %v", me.Keywords)
	}
	if me.ExpectedMax != 0 {
		t.Errorf("empty max should be 0, got %v", me.ExpectedMax)
	}
	if me.Notes != "primary residence" {
		t.Errorf("notes not trimmed: %q", me.Notes)
	}
}

func TestHandlePin_Success(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// seed an expense
	list, _ := dl.AddMajorExpense(makeExpense("amazon", "Amazon - Books", nil, 0, 0))
	id := list[0].ID

	form := url.Values{}
	form.Set("hash", "txn-hash-1")
	form.Set("expense_id", id)

	req := httptest.NewRequest("POST", "/major-expenses/pins", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	pins, _ := dl.LoadTransactionPins()
	if pins["txn-hash-1"] != id {
		t.Errorf("expected pin saved, got %+v", pins)
	}
}

func TestHandlePin_RejectsUnknownExpense(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"hash": {"x"}, "expense_id": {"does-not-exist"}}
	req := httptest.NewRequest("POST", "/major-expenses/pins", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleBulkPin_Success(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	list, _ := dl.AddMajorExpense(makeExpense("amazon", "Amazon", nil, 0, 0))
	id := list[0].ID

	form := url.Values{
		"expense_id": {id},
		"hashes":     {"h1", "h2", "h3"},
	}
	req := httptest.NewRequest("POST", "/major-expenses/pins/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	pins, _ := dl.LoadTransactionPins()
	if pins["h1"] != id || pins["h2"] != id || pins["h3"] != id {
		t.Errorf("expected all 3 hashes pinned to %s, got %+v", id, pins)
	}
}

func TestHandleBulkPin_RejectsEmptyHashList(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	list, _ := dl.AddMajorExpense(makeExpense("a", "A", nil, 0, 0))

	form := url.Values{"expense_id": {list[0].ID}}
	req := httptest.NewRequest("POST", "/major-expenses/pins/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleBulkPin_RejectsUnknownExpense(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"expense_id": {"missing"}, "hashes": {"h1"}}
	req := httptest.NewRequest("POST", "/major-expenses/pins/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandlePin_RejectsEmptyHash(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	list, _ := dl.AddMajorExpense(makeExpense("a", "A", nil, 0, 0))

	req := httptest.NewRequest("POST", "/major-expenses/pins", strings.NewReader("hash=&expense_id="+list[0].ID))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleUnpin_Success(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	dl.SetTransactionPin("hashA", "expense-1")

	req := httptest.NewRequest("DELETE", "/major-expenses/pins/hashA", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	pins, _ := dl.LoadTransactionPins()
	if _, exists := pins["hashA"]; exists {
		t.Errorf("expected pin removed, still present: %+v", pins)
	}
}

func TestHandleDelete_PrunesOrphanPins(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	list, _ := dl.AddMajorExpense(makeExpense("doomed", "Doomed", nil, 0, 0))
	id := list[0].ID
	dl.SetTransactionPin("orphan-hash", id)

	// Confirm pin exists before delete
	pins, _ := dl.LoadTransactionPins()
	if pins["orphan-hash"] != id {
		t.Fatalf("setup: expected pin, got %+v", pins)
	}

	req := httptest.NewRequest("DELETE", "/major-expenses/"+id, nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}

	pins, _ = dl.LoadTransactionPins()
	if _, exists := pins["orphan-hash"]; exists {
		t.Errorf("expected orphan pin to be pruned, still present: %+v", pins)
	}
}

func TestBuildExpenseOptions_SortsAndDisambiguates(t *testing.T) {
	in := []models.MajorExpense{
		makeExpense("1", "Home Improvement", []string{"Lowe's"}, 0, 0),
		makeExpense("2", "Cellphone", []string{"AT&T"}, 0, 0),
		makeExpense("3", "Home Improvement", []string{"The Home Depot"}, 0, 0),
		makeExpense("4", "Booze", []string{"Chateau"}, 0, 0),
		makeExpense("5", "home improvement", []string{"Weiders"}, 0, 0), // case-insensitive collision
	}
	got := buildExpenseOptions(in)
	wantLabels := []string{
		"Booze",
		"Cellphone",
		"Home Improvement — Lowe's",
		"Home Improvement — The Home Depot",
		"home improvement — Weiders",
	}
	if len(got) != len(wantLabels) {
		t.Fatalf("len = %d, want %d", len(got), len(wantLabels))
	}
	for i, w := range wantLabels {
		if got[i].Label != w {
			t.Errorf("[%d] Label = %q, want %q", i, got[i].Label, w)
		}
	}
}

func makeExpense(id, name string, keywords []string, min, max float64) models.MajorExpense {
	return models.MajorExpense{
		ID:          id,
		Name:        name,
		Keywords:    keywords,
		ExpectedMin: min,
		ExpectedMax: max,
	}
}

func TestBuildPageData_DateRangeFiltersTransactions(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// Replace the default fixture with one that spans 3 calendar years
	// so we can prove a windowed call only sees the in-window subset.
	csvDir := t.TempDir()
	csvPath := filepath.Join(csvDir, "test.csv")
	csvContent := "Date,Description,Amount,Type,Category\n" +
		"2023-06-15,Landlord LLC,-1500,Outflow,Housing\n" +
		"2024-06-15,Landlord LLC,-1700,Outflow,Housing\n" +
		"2025-06-15,Landlord LLC,-1900,Outflow,Housing\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	store, _ := storage.New(csvDir)
	dl2 := dataloader.New(csvDir, store)
	Initialize(dl2, nil)
	defer Initialize(dl, nil)

	if _, err := dl2.AddMajorExpense(makeExpense("rent", "Rent", []string{"landlord"}, 0, 0)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 2024-only window — only the middle row should match.
	req := httptest.NewRequest("GET", "/major-expenses?start=2024-01-01&end=2024-12-31", nil)
	data, err := buildPageData(req)
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}

	// Map keys are echoed for the template inputs.
	if got := data["StartDate"]; got != "2024-01-01" {
		t.Errorf("StartDate = %v, want 2024-01-01", got)
	}
	if got := data["EndDate"]; got != "2024-12-31" {
		t.Errorf("EndDate = %v, want 2024-12-31", got)
	}
	if got := data["MinDate"]; got != "2023-06-15" {
		t.Errorf("MinDate = %v, want 2023-06-15 (full-data min)", got)
	}
	if got := data["MaxDate"]; got != "2025-06-15" {
		t.Errorf("MaxDate = %v, want 2025-06-15 (full-data max)", got)
	}

	// Per-expense rollup reflects only the 2024 transaction.
	body, _ := json.Marshal(data["Summaries"])
	var raw []map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode summaries: %v\n%s", err, body)
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(raw))
	}
	if got := int(raw[0]["Count"].(float64)); got != 1 {
		t.Errorf("Count = %d, want 1 (only the 2024 txn is in window)", got)
	}
	if got := raw[0]["Total"].(float64); got != 1700 {
		t.Errorf("Total = %v, want 1700 (only the 2024 txn is in window)", got)
	}
}

func TestParseRangeFromRequest(t *testing.T) {
	// Build a small TransactionSet so MinDate/MaxDate are deterministic.
	txns := &models.TransactionSet{Transactions: []models.Transaction{
		{Date: time.Date(2023, 1, 15, 0, 0, 0, 0, time.UTC), Amount: -10, TransactionType: models.Outflow},
		{Date: time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC), Amount: -20, TransactionType: models.Outflow},
	}}
	min := txns.MinDate()
	max := txns.MaxDate()
	mustParse := func(s string) time.Time {
		t.Helper()
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return v
	}

	cases := []struct {
		name             string
		req              *http.Request
		wantStart, wantEnd time.Time
	}{
		{
			name:      "url query parses both",
			req:       httptest.NewRequest("GET", "/major-expenses?start=2024-01-01&end=2024-12-31", nil),
			wantStart: mustParse("2024-01-01"),
			wantEnd:   mustParse("2024-12-31"),
		},
		{
			name:      "missing both falls back to MinDate/MaxDate",
			req:       httptest.NewRequest("GET", "/major-expenses", nil),
			wantStart: min,
			wantEnd:   max,
		},
		{
			name:      "unparseable both falls back to MinDate/MaxDate",
			req:       httptest.NewRequest("GET", "/major-expenses?start=garbage&end=also-garbage", nil),
			wantStart: min,
			wantEnd:   max,
		},
		{
			name: "form values used when query missing",
			req: func() *http.Request {
				form := url.Values{"start": {"2024-03-01"}, "end": {"2024-03-31"}}
				r := httptest.NewRequest("POST", "/major-expenses", strings.NewReader(form.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return r
			}(),
			wantStart: mustParse("2024-03-01"),
			wantEnd:   mustParse("2024-03-31"),
		},
		{
			name:      "only start parses; end falls back",
			req:       httptest.NewRequest("GET", "/major-expenses?start=2024-05-01", nil),
			wantStart: mustParse("2024-05-01"),
			wantEnd:   max,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStart, gotEnd := parseRangeFromRequest(tc.req, txns)
			if !gotStart.Equal(tc.wantStart) {
				t.Errorf("start = %v, want %v", gotStart, tc.wantStart)
			}
			if !gotEnd.Equal(tc.wantEnd) {
				t.Errorf("end = %v, want %v", gotEnd, tc.wantEnd)
			}
		})
	}
}
