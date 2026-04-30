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

func TestHandleAdd_ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
	}{
		{"missing name", url.Values{"keywords": {"x"}}},
		{"empty keywords without amount range", url.Values{"name": {"X"}, "keywords": {"  ,  "}}},
		{"empty keywords with only min", url.Values{"name": {"X"}, "keywords": {"  ,  "}, "expected_min": {"100"}}},
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

func makeExpense(id, name string, keywords []string, min, max float64) models.MajorExpense {
	return models.MajorExpense{
		ID:          id,
		Name:        name,
		Keywords:    keywords,
		ExpectedMin: min,
		ExpectedMax: max,
	}
}
