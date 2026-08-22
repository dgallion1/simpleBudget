package majorexpenses

// Tests that exist solely to cover the remaining error/branch paths in
// handlers.go. They use four reusable patterns:
//
//  1. `name=%ZZ` body forces r.ParseForm() to return a hex-decode error,
//     which is the only modern way to break ParseForm on a small form
//     body (the old "multipart without boundary" trick is silently
//     accepted by net/http).
//  2. Direct handler invocation with a chi.RouteContext that has an
//     empty URLParam — the registered routes never match an empty id,
//     so the missing-id guards inside handleUpdate/Delete/Restore/
//     Discard/Unpin can only be exercised this way.
//  3. Pre-writing malformed JSON to the data file (major_expenses.json,
//     transaction_pins.json, deleted_major_expenses.json) makes the
//     corresponding Load* call return an "invalid …file" error without
//     touching the storage layer.
//  4. chmod(0o500) on the data dir blocks every subsequent write, so
//     any handler whose mutation path ends in a Save fails. A t.Cleanup
//     restores 0o755 so t.TempDir can do its own cleanup.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
)

// chiCtx returns an http.Request whose chi RouteContext has the given
// URL params. Used to exercise empty-id and empty-hash branches that
// the registered routes can never produce.
func chiCtx(req *http.Request, kv ...string) *http.Request {
	rctx := chi.NewRouteContext()
	for i := 0; i+1 < len(kv); i += 2 {
		rctx.URLParams.Add(kv[i], kv[i+1])
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// makeChmodReadOnly chmods dir to 0o500 (no write) and registers a
// cleanup that restores 0o755 so t.TempDir can rm-rf at end.
func makeChmodReadOnly(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod 0500 %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// formEncoded wraps a body string in the standard form-encoded request
// shape used across this test file.
func formEncoded(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// =========================================================================
// ParseForm error branches — handleAdd, handleUpdate, handlePin, handleBulkPin
// =========================================================================

func TestHandleAdd_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses", "name=%ZZ"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestHandleUpdate_ParseFormError(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	list, _ := dl.AddMajorExpense(makeExpense("seed", "X", []string{"x"}, 0, 0))

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("PUT", "/major-expenses/"+list[0].ID, "name=%ZZ"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandlePin_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses/pins", "hash=%ZZ"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleBulkPin_ParseFormError(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses/pins/bulk", "hashes=%ZZ"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// =========================================================================
// Empty path-param branches — only reachable via direct handler invocation
// =========================================================================

func TestHandleUpdate_EmptyIDDirectCall(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	req := chiCtx(httptest.NewRequest("PUT", "/major-expenses/", nil), "id", "")
	w := httptest.NewRecorder()
	handleUpdate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for empty id", w.Code)
	}
}

func TestHandleDelete_EmptyIDDirectCall(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	req := chiCtx(httptest.NewRequest("DELETE", "/major-expenses/", nil), "id", "")
	w := httptest.NewRecorder()
	handleDelete(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for empty id", w.Code)
	}
}

func TestHandleRestore_EmptyIDDirectCall(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	req := chiCtx(httptest.NewRequest("POST", "/major-expenses//restore", nil), "id", "")
	w := httptest.NewRecorder()
	handleRestore(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for empty id", w.Code)
	}
}

func TestHandleDiscard_EmptyIDDirectCall(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	req := chiCtx(httptest.NewRequest("DELETE", "/major-expenses/deleted/", nil), "id", "")
	w := httptest.NewRecorder()
	handleDiscard(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for empty id", w.Code)
	}
}

func TestHandleUnpin_EmptyHashDirectCall(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	req := chiCtx(httptest.NewRequest("DELETE", "/major-expenses/pins/", nil), "hash", "")
	w := httptest.NewRecorder()
	handleUnpin(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for empty hash", w.Code)
	}
}

// =========================================================================
// Missing required form fields in pin handlers
// =========================================================================

func TestHandlePin_RejectsMissingExpenseID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"hash": {"h1"}} // expense_id omitted
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses/pins", form.Encode()))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleBulkPin_RejectsMissingExpenseID(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{"hashes": {"h1"}} // expense_id omitted
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses/pins/bulk", form.Encode()))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// =========================================================================
// parseExpenseForm validation branches
// =========================================================================

func TestHandleAdd_NameTooLong(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{}
	form.Set("name", strings.Repeat("a", 201))
	form.Set("keywords", "x")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses", form.Encode()))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleAdd_InvalidExpectedMax(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":         {"X"},
		"keywords":     {"x"},
		"expected_max": {"abc"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses", form.Encode()))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleAdd_NegativeExpectedMax(t *testing.T) {
	_, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"name":         {"X"},
		"keywords":     {"x"},
		"expected_max": {"-1"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses", form.Encode()))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestHandleUpdate_InvalidFormAfterValidID confirms parseExpenseForm
// errors are surfaced from the update handler too. The previous
// TestHandleUpdate_MissingID test uses a non-existent id so it never
// reaches parseExpenseForm.
func TestHandleUpdate_InvalidFormAfterValidID(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	list, _ := dl.AddMajorExpense(makeExpense("u", "U", []string{"u"}, 0, 0))

	form := url.Values{} // missing name → parseExpenseForm rejects
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("PUT", "/major-expenses/"+list[0].ID, form.Encode()))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// =========================================================================
// Renderer-mode partial branches
// =========================================================================

func TestHandleExceptions_WithRenderer_ReturnsPartial(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/major-expenses/exceptions", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := strings.ToLower(w.Body.String())
	// Partial response — must NOT contain the base layout markers.
	if strings.Contains(body, "<!doctype") || strings.Contains(body, "<html") {
		t.Errorf("renderer-mode exceptions response should be a partial, got base layout:\n%s", body)
	}
}

// TestHandleAdd_WithRenderer_TriggersResultsPartial covers the partial
// branch of renderResults — every mutation handler routes through it,
// so we only need one renderer-mode test for the whole family.
func TestHandleAdd_WithRenderer_TriggersResultsPartial(t *testing.T) {
	_, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"name":     {"Renderer Add"},
		"keywords": {"renderer"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses", form.Encode()))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := strings.ToLower(w.Body.String())
	if strings.Contains(body, "<!doctype") || strings.Contains(body, "<html") {
		t.Errorf("renderer-mode mutation response should be a results partial, got base layout")
	}
}

// =========================================================================
// Load failure paths — corrupt JSON in data files
// =========================================================================

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestBuildPageData_LoadMajorExpensesError(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	writeFile(t, filepath.Join(dl.CSVDirectory, "major_expenses.json"), "{not json")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("GET", "/major-expenses", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestBuildPageData_LoadTransactionPinsError(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	writeFile(t, filepath.Join(dl.CSVDirectory, "transaction_pins.json"), "{not json")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("GET", "/major-expenses", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestBuildPageData_LoadDeletedMajorExpensesError(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	writeFile(t, filepath.Join(dl.CSVDirectory, "deleted_major_expenses.json"), "{not json")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("GET", "/major-expenses", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleExceptions_LoadError(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	writeFile(t, filepath.Join(dl.CSVDirectory, "major_expenses.json"), "{bad")

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("GET", "/major-expenses/exceptions", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandlePin_LoadMajorExpensesError(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	writeFile(t, filepath.Join(dl.CSVDirectory, "major_expenses.json"), "{bad")

	form := url.Values{"hash": {"h1"}, "expense_id": {"any"}}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses/pins", form.Encode()))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleBulkPin_LoadMajorExpensesError(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	writeFile(t, filepath.Join(dl.CSVDirectory, "major_expenses.json"), "{bad")

	form := url.Values{"expense_id": {"any"}, "hashes": {"h1"}}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses/pins/bulk", form.Encode()))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

// TestHandleAdd_PinHashFailureLogged covers the log-and-comment branch
// (handlers.go: 98–101) when SetTransactionPin returns an error after
// AddMajorExpense has already succeeded. Pre-writing a corrupt
// transaction_pins.json makes SetTransactionPin's Load step fail, while
// AddMajorExpense's writes (to major_expenses.json) succeed.
//
// The downstream renderResults call also fails (buildPageData reads the
// same corrupt file), so the response body contains BOTH the
// "<!-- pin_hash ... -->" comment and a renderError block. Asserting on
// the comment marker is enough to prove we hit the target branch.
func TestHandleAdd_PinHashFailureLogged(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	writeFile(t, filepath.Join(dl.CSVDirectory, "transaction_pins.json"), "{not json")
	form := url.Values{
		"name":     {"PinFail"},
		"keywords": {"pinfail"},
		"pin_hash": {"some-hash"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses", form.Encode()))

	if !strings.Contains(w.Body.String(), "pin_hash") {
		t.Errorf("expected pin_hash failure marker in response body, got:\n%s", w.Body.String())
	}
	// The expense should still be persisted — pin failure does not roll back.
	got, err := dl.LoadMajorExpenses()
	if err != nil {
		t.Fatalf("LoadMajorExpenses: %v", err)
	}
	if len(got) != 1 || got[0].Name != "PinFail" {
		t.Errorf("expected expense to be persisted despite pin failure, got %+v", got)
	}
}

// =========================================================================
// Save failure paths — chmod 0o500 on data dir
// =========================================================================

func TestHandleAdd_SaveFails(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	makeChmodReadOnly(t, dl.CSVDirectory)

	form := url.Values{"name": {"X"}, "keywords": {"x"}}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses", form.Encode()))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleUpdate_SaveFails(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	list, _ := dl.AddMajorExpense(makeExpense("s", "S", []string{"s"}, 0, 0))
	makeChmodReadOnly(t, dl.CSVDirectory)

	form := url.Values{"name": {"S2"}, "keywords": {"s"}}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("PUT", "/major-expenses/"+list[0].ID, form.Encode()))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleDelete_SaveFails(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	list, _ := dl.AddMajorExpense(makeExpense("d", "D", nil, 0, 0))
	makeChmodReadOnly(t, dl.CSVDirectory)

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("DELETE", "/major-expenses/"+list[0].ID, nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleRestore_SaveFails(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	list, _ := dl.AddMajorExpense(makeExpense("r", "R", nil, 0, 0))
	id := list[0].ID
	if err := dl.ArchiveMajorExpense(id); err != nil {
		t.Fatalf("archive: %v", err)
	}
	makeChmodReadOnly(t, dl.CSVDirectory)

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("POST", "/major-expenses/"+id+"/restore", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleDiscard_SaveFails(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	list, _ := dl.AddMajorExpense(makeExpense("g", "G", nil, 0, 0))
	id := list[0].ID
	if err := dl.ArchiveMajorExpense(id); err != nil {
		t.Fatalf("archive: %v", err)
	}
	makeChmodReadOnly(t, dl.CSVDirectory)

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("DELETE", "/major-expenses/deleted/"+id, nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandlePin_SaveFails(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	list, _ := dl.AddMajorExpense(makeExpense("p", "P", nil, 0, 0))
	makeChmodReadOnly(t, dl.CSVDirectory)

	form := url.Values{"hash": {"h1"}, "expense_id": {list[0].ID}}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses/pins", form.Encode()))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleBulkPin_SaveFails(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	list, _ := dl.AddMajorExpense(makeExpense("bp", "BP", nil, 0, 0))
	makeChmodReadOnly(t, dl.CSVDirectory)

	form := url.Values{"expense_id": {list[0].ID}, "hashes": {"h1", "h2"}}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formEncoded("POST", "/major-expenses/pins/bulk", form.Encode()))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandleUnpin_SaveFails(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()
	list, _ := dl.AddMajorExpense(makeExpense("up", "UP", nil, 0, 0))
	if err := dl.SetTransactionPin("h1", list[0].ID); err != nil {
		t.Fatalf("seed pin: %v", err)
	}
	makeChmodReadOnly(t, dl.CSVDirectory)

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("DELETE", "/major-expenses/pins/h1", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

// =========================================================================
// Misc edge cases
// =========================================================================

// TestFormatDateInputValue_ZeroTimeReturnsEmpty exercises the IsZero
// branch via a buildPageData call against a data directory with no CSV
// files — MinDate/MaxDate fall back to the zero Time and the date
// inputs render as "".
func TestFormatDateInputValue_ZeroTimeReturnsEmpty(t *testing.T) {
	emptyDir := t.TempDir()
	store, err := storage.New(emptyDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	dl := dataloader.New(emptyDir, store)
	prevLoader := loader
	Initialize(dl, nil)
	defer Initialize(prevLoader, nil)

	data, err := buildPageData(httptest.NewRequest("GET", "/major-expenses", nil))
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}
	if got := data["MinDate"]; got != "" {
		t.Errorf("MinDate = %q, want \"\" for zero time", got)
	}
	if got := data["MaxDate"]; got != "" {
		t.Errorf("MaxDate = %q, want \"\" for zero time", got)
	}
}

// TestBuildPageData_PinnedHashesReachSummary covers the
// `if match.PinnedHashes[t.Hash]` body in the per-expense summary loop.
// It works by:
//  1. Loading transactions to discover their real Hash values
//  2. Pinning one transaction's hash to a freshly-created expense
//  3. Calling buildPageData and verifying the summary's PinnedHashes
//     map contains that hash
func TestBuildPageData_PinnedHashesReachSummary(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	ts, err := dl.LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if len(ts.Transactions) == 0 {
		t.Skip("fixture produced no transactions")
	}
	hash := ts.Transactions[0].Hash
	if hash == "" {
		t.Skip("fixture produced no transaction hashes")
	}

	added, err := dl.AddMajorExpense(makeExpense("pinned-target", "PinTarget", nil, 0, 0))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := dl.SetTransactionPin(hash, added[len(added)-1].ID); err != nil {
		t.Fatalf("pin: %v", err)
	}

	if _, err := buildPageData(httptest.NewRequest("GET", "/major-expenses", nil)); err != nil {
		t.Fatalf("buildPageData: %v", err)
	}
	// Sanity check: the pin we set drives the inner-loop branch we
	// wanted to cover. The exact summary struct is unexported, so we
	// only verify the pin was stored — the coverage profile confirms
	// the branch executed.
	//
	// Resolved through PinFor rather than indexed by hash: pins are
	// stored under the transaction's StableID now, and the raw hash is
	// only the legacy fallback key.
	if id, ok := dl.PinFor(ts.Transactions[0]); !ok || id == "" {
		pins, _ := dl.LoadTransactionPins()
		t.Errorf("pin not stored: pins=%+v", pins)
	}
}

// TestBuildPageData_DeletedSortClosureExecutes seeds two archived
// entries with distinct DeletedAt timestamps so the sort.Slice closure
// in buildPageData (handlers.go:444) is invoked at least once.
func TestBuildPageData_DeletedSortClosureExecutes(t *testing.T) {
	dl, cleanup := setupTestEnv(t)
	defer cleanup()

	for i, name := range []string{"A", "B"} {
		list, err := dl.AddMajorExpense(makeExpense("del-"+string(rune('a'+i)), name, nil, 0, 0))
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := dl.ArchiveMajorExpense(list[len(list)-1].ID); err != nil {
			t.Fatalf("archive: %v", err)
		}
	}

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, httptest.NewRequest("GET", "/major-expenses", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	deleted, err := dl.LoadDeletedMajorExpenses()
	if err != nil {
		t.Fatalf("load deleted: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("expected 2 archived entries (sort closure runs at len≥2), got %d", len(deleted))
	}
}
