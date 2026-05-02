package majorexpenses

import (
	"encoding/json"
	"io"
	"io/fs"
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
	"budget2/internal/templates"
	"budget2/web"
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

func TestHandleDelete_ArchivesExpenseAndPins(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	list, _ := dl.AddMajorExpense(makeExpense("doomed", "Doomed", nil, 0, 0))
	id := list[0].ID
	if err := dl.SetTransactionPin("orphan-hash", id); err != nil {
		t.Fatalf("seed pin: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/major-expenses/"+id, nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	// Active pin map no longer contains the hash.
	pins, _ := dl.LoadTransactionPins()
	if _, exists := pins["orphan-hash"]; exists {
		t.Errorf("expected pin to be detached from active map, still present: %+v", pins)
	}

	// Archive contains the definition and the pin hash.
	deleted, err := dl.LoadDeletedMajorExpenses()
	if err != nil {
		t.Fatalf("load deleted: %v", err)
	}
	if len(deleted) != 1 || deleted[0].Expense.ID != id {
		t.Fatalf("expected 1 archived entry for %s, got %+v", id, deleted)
	}
	if len(deleted[0].PinnedHashes) != 1 || deleted[0].PinnedHashes[0] != "orphan-hash" {
		t.Errorf("expected orphan-hash in archive, got %+v", deleted[0].PinnedHashes)
	}
}

func TestHandleRestore_ReturnsExpenseAndPinsToActive(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	list, _ := dl.AddMajorExpense(makeExpense("back", "Back", []string{"x"}, 10, 20))
	id := list[0].ID
	dl.SetTransactionPin("h1", id)
	dl.SetTransactionPin("h2", id)

	if err := dl.ArchiveMajorExpense(id); err != nil {
		t.Fatalf("archive: %v", err)
	}

	req := httptest.NewRequest("POST", "/major-expenses/"+id+"/restore", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	active, _ := dl.LoadMajorExpenses()
	if len(active) != 1 || active[0].ID != id {
		t.Errorf("expected restored, got %+v", active)
	}
	pins, _ := dl.LoadTransactionPins()
	if pins["h1"] != id || pins["h2"] != id {
		t.Errorf("pins not restored: %+v", pins)
	}
	deleted, _ := dl.LoadDeletedMajorExpenses()
	if len(deleted) != 0 {
		t.Errorf("archive should be empty, got %+v", deleted)
	}
}

func TestHandleDiscard_RemovesFromArchive(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	list, _ := dl.AddMajorExpense(makeExpense("trash", "T", nil, 0, 0))
	id := list[0].ID
	if err := dl.ArchiveMajorExpense(id); err != nil {
		t.Fatalf("archive: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/major-expenses/deleted/"+id, nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	deleted, _ := dl.LoadDeletedMajorExpenses()
	if len(deleted) != 0 {
		t.Errorf("archive should be empty after discard, got %+v", deleted)
	}
}

func TestHandleAdd_WithPinHash_PinsImmediately(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{}
	form.Set("name", "AdHoc")
	form.Set("keywords", "this-keyword-wont-match-any-csv-row")
	form.Set("pin_hash", "specific-hash-9876")

	req := httptest.NewRequest("POST", "/major-expenses", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	expenses, _ := dl.LoadMajorExpenses()
	if len(expenses) != 1 {
		t.Fatalf("expected 1 expense, got %+v", expenses)
	}
	pins, _ := dl.LoadTransactionPins()
	if pins["specific-hash-9876"] != expenses[0].ID {
		t.Errorf("expected hash pinned to new expense, got %+v", pins)
	}
}

func TestBuildPageData_UnmatchedTotalAndCount(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	// setupTestEnv seeds: 2x landlord (-1700, -3500), 1x random big (-450),
	// 1x tiny coffee (-5). With NO declared expenses, every outflow goes
	// to Unmatched. Total should sum the abs amounts.
	req := httptest.NewRequest("GET", "/major-expenses", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	body := readJSON(t, w.Result())
	count, ok := body["UnmatchedCount"].(float64)
	if !ok {
		t.Fatalf("UnmatchedCount missing/not a number: %+v", body["UnmatchedCount"])
	}
	if int(count) != 4 {
		t.Errorf("UnmatchedCount = %v, want 4", count)
	}
	total, ok := body["UnmatchedTotal"].(float64)
	if !ok {
		t.Fatalf("UnmatchedTotal missing/not a number")
	}
	want := 1700.0 + 3500.0 + 450.0 + 5.0
	if total != want {
		t.Errorf("UnmatchedTotal = %v, want %v", total, want)
	}
}

func TestBuildPageData_MatchedHashToExpenseID_Inverted(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// Declare Rent matching the seeded landlord rows.
	if _, err := dl.AddMajorExpense(makeExpense("rent", "Rent", []string{"landlord"}, 1000, 5000)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := httptest.NewRequest("GET", "/major-expenses", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	body := readJSON(t, w.Result())
	matched, ok := body["MatchedHashToExpenseID"].(map[string]interface{})
	if !ok {
		t.Fatalf("MatchedHashToExpenseID missing or wrong type: %+v", body["MatchedHashToExpenseID"])
	}
	if len(matched) == 0 {
		t.Errorf("expected at least one matched hash, got empty map")
	}
	for _, v := range matched {
		if id, _ := v.(string); id != "rent" {
			t.Errorf("expected all matched hashes to point to 'rent', got %v", v)
		}
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

// setupTestEnvWithRenderer wires the package up with a real templates.Renderer
// pulling from the embedded FS, so tests can assert on rendered HTML rather
// than the JSON fallback. Mirrors setupTestEnv otherwise.
func setupTestEnvWithRenderer(t *testing.T) (*dataloader.DataLoader, func()) {
	t.Helper()
	dl, cleanup := setupTestEnv(t)

	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	rend, err := templates.NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}
	Initialize(dl, rend)
	prevCleanup := cleanup
	return dl, func() {
		prevCleanup()
		Initialize(dl, nil) // restore JSON-mode for any tests that follow
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

func TestBuildPageData_TotalDeclared(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// Seed two outflows that match two different expenses. Total must
	// equal the sum of their absolute amounts.
	csvDir := t.TempDir()
	csvPath := filepath.Join(csvDir, "test.csv")
	today := time.Now().Format("2006-01-02")
	csvContent := "Date,Description,Amount,Type,Category\n" +
		today + ",Anthropic Subscription,-108,Outflow,Software\n" +
		today + ",Verizon Wireless,-92,Outflow,Utilities\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	store, _ := storage.New(csvDir)
	dl2 := dataloader.New(csvDir, store)
	Initialize(dl2, nil)
	defer Initialize(dl, nil)

	if _, err := dl2.AddMajorExpense(makeExpense("anthropic", "Anthropic", []string{"anthropic"}, 0, 0)); err != nil {
		t.Fatalf("seed anthropic: %v", err)
	}
	if _, err := dl2.AddMajorExpense(makeExpense("verizon", "Verizon", []string{"verizon"}, 0, 0)); err != nil {
		t.Fatalf("seed verizon: %v", err)
	}

	data, err := buildPageData(httptest.NewRequest("GET", "/major-expenses", nil))
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}

	total, ok := data["TotalDeclared"].(float64)
	if !ok {
		t.Fatalf("expected TotalDeclared float64 in context, got %T (%v)", data["TotalDeclared"], data["TotalDeclared"])
	}
	if total != 200 {
		t.Errorf("TotalDeclared = %v, want 200 (108 + 92)", total)
	}

	// Cross-check: TotalDeclared must equal the sum of Summaries[].Total.
	body, _ := json.Marshal(data["Summaries"])
	var raw []map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode summaries: %v", err)
	}
	var sum float64
	for _, s := range raw {
		sum += s["Total"].(float64)
	}
	if sum != total {
		t.Errorf("TotalDeclared (%v) != sum of Summaries[].Total (%v)", total, sum)
	}
}

func TestHandleMajorExpensesPage_NoQueryParamsDefaultsToAllTime(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/major-expenses", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	// MinDate/MaxDate are populated by the fixture (now-2mo .. now-1mo).
	// Default window equals the full range when no params are provided.
	if body["StartDate"] != body["MinDate"] {
		t.Errorf("StartDate = %v, want = MinDate %v", body["StartDate"], body["MinDate"])
	}
	if body["EndDate"] != body["MaxDate"] {
		t.Errorf("EndDate = %v, want = MaxDate %v", body["EndDate"], body["MaxDate"])
	}
}

func TestHandleMajorExpensesPage_StartEndQueryParamsEchoed(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/major-expenses?start=2024-01-01&end=2024-12-31", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	if body["StartDate"] != "2024-01-01" {
		t.Errorf("StartDate = %v, want 2024-01-01", body["StartDate"])
	}
	if body["EndDate"] != "2024-12-31" {
		t.Errorf("EndDate = %v, want 2024-12-31", body["EndDate"])
	}
}

func TestHandleMajorExpensesPage_UnparseableDatesFallBackToAllTime(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/major-expenses?start=garbage&end=also-garbage", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	if body["StartDate"] != body["MinDate"] {
		t.Errorf("unparseable start should fall back to MinDate; StartDate=%v MinDate=%v", body["StartDate"], body["MinDate"])
	}
	if body["EndDate"] != body["MaxDate"] {
		t.Errorf("unparseable end should fall back to MaxDate; EndDate=%v MaxDate=%v", body["EndDate"], body["MaxDate"])
	}
}

func TestHandleMajorExpensesPage_StartAfterEndReturnsEmptyWindow(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// Seed a defined expense so we can confirm it still appears with
	// zero counts/totals when the window collapses.
	if _, err := dl.AddMajorExpense(makeExpense("rent", "Rent", []string{"landlord"}, 0, 0)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := httptest.NewRequest("GET", "/major-expenses?start=2099-01-01&end=2024-01-01", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())

	// The defined expense is still listed (left card always shows every
	// definition) — but with zero count and zero total because the
	// window is empty.
	rawSummaries, _ := json.Marshal(body["Summaries"])
	var summaries []map[string]interface{}
	if err := json.Unmarshal(rawSummaries, &summaries); err != nil {
		t.Fatalf("decode summaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary (defined expense), got %d", len(summaries))
	}
	if got := int(summaries[0]["Count"].(float64)); got != 0 {
		t.Errorf("Count = %d, want 0 (empty window)", got)
	}
	if got := summaries[0]["Total"].(float64); got != 0 {
		t.Errorf("Total = %v, want 0 (empty window)", got)
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

func TestHandleMajorExpensesPage_HTMXFilterReturnsWrapperOnly(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/major-expenses?start=2024-01-01&end=2024-12-31", nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "major-expenses-results-wrapper")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	// Wrapper marker is present.
	if !strings.Contains(body, `id="major-expenses-results-wrapper"`) {
		t.Errorf("expected wrapper id in HTMX response; got:\n%s", body)
	}
	// Base-layout markers are absent — otherwise the swap nests a full
	// page inside the wrapper.
	if strings.Contains(strings.ToLower(body), "<!doctype") {
		t.Errorf("HTMX response must NOT include the base layout; found <!doctype")
	}
	if strings.Contains(body, "<html") {
		t.Errorf("HTMX response must NOT include the base layout; found <html>")
	}
}

func TestHandleMajorExpensesPage_NonHTMXReturnsBaseLayout(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/major-expenses", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := strings.ToLower(w.Body.String())
	if !strings.Contains(body, "<!doctype") && !strings.Contains(body, "<html") {
		t.Errorf("non-HTMX response must include the base layout; got:\n%s", body)
	}
}

func TestHandleAdd_PreservesDateRange(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{}
	form.Set("name", "Rent")
	form.Set("keywords", "landlord")
	form.Set("expected_min", "1500")
	form.Set("expected_max", "2000")
	form.Set("start", "2024-01-01")
	form.Set("end", "2024-12-31")

	req := httptest.NewRequest("POST", "/major-expenses", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	if body["StartDate"] != "2024-01-01" {
		t.Errorf("StartDate = %v, want 2024-01-01 (mutation must preserve window)", body["StartDate"])
	}
	if body["EndDate"] != "2024-12-31" {
		t.Errorf("EndDate = %v, want 2024-12-31 (mutation must preserve window)", body["EndDate"])
	}
}

func TestHandleBulkPin_PreservesDateRange(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	exp, err := dl.AddMajorExpense(makeExpense("rent", "Rent", []string{"landlord"}, 0, 0))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	form := url.Values{}
	form.Set("expense_id", exp[0].ID)
	form.Add("hashes", "deadbeef")
	form.Set("start", "2024-01-01")
	form.Set("end", "2024-12-31")

	req := httptest.NewRequest("POST", "/major-expenses/pins/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())
	if body["StartDate"] != "2024-01-01" {
		t.Errorf("StartDate = %v, want 2024-01-01 (bulk-pin must preserve window)", body["StartDate"])
	}
	if body["EndDate"] != "2024-12-31" {
		t.Errorf("EndDate = %v, want 2024-12-31 (bulk-pin must preserve window)", body["EndDate"])
	}
}

func TestHandleExceptions_StartEndQueryParams(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	// Seed an expense definition + a multi-year fixture.
	csvDir := t.TempDir()
	csvPath := filepath.Join(csvDir, "test.csv")
	csvContent := "Date,Description,Amount,Type,Category\n" +
		"2023-06-15,Random Big Purchase A,-450,Outflow,Misc\n" +
		"2024-06-15,Random Big Purchase B,-450,Outflow,Misc\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	store, _ := storage.New(csvDir)
	dl2 := dataloader.New(csvDir, store)
	Initialize(dl2, nil)
	defer Initialize(dl, nil)

	req := httptest.NewRequest("GET", "/major-expenses/exceptions?start=2024-01-01&end=2024-12-31", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := readJSON(t, w.Result())

	// Defaults are not "all-time" here — the query params should win.
	if body["StartDate"] != "2024-01-01" {
		t.Errorf("StartDate = %v, want 2024-01-01", body["StartDate"])
	}
	if body["EndDate"] != "2024-12-31" {
		t.Errorf("EndDate = %v, want 2024-12-31", body["EndDate"])
	}
}
