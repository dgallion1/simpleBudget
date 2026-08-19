package accounts

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
	accountssvc "budget2/internal/services/accounts"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
	"budget2/web"
)

// setupTestEnv mirrors majorexpenses.setupTestEnv: a temp data directory
// with a couple of CSV fixtures, a storage service rooted there, a
// dataloader, and the accounts handler package initialized in JSON-mode
// (renderer == nil) so tests can assert on the round-tripped pageData.
//
// The CSVs here are the pattern-editor fixture: the tests assert which
// account wins each file, so the filenames are chosen to exercise the
// glob + substring fallback and first-match-wins ordering.
func setupTestEnv(t *testing.T) (*dataloader.DataLoader, *storage.Storage, func()) {
	t.Helper()
	csvDir := t.TempDir()

	// Three CSVs exercising the matching rules:
	//  - usaa-checking.csv        → substring "usaa" + glob "usaa-checking*.csv"
	//  - usaa-credit-2026-08.csv  → glob "usaa-credit*.csv" (also matches "usaa")
	//  - vanguard-2026.csv        → matches no account (unassigned)
	for _, name := range []string{"usaa-checking.csv", "usaa-credit-2026-08.csv", "vanguard-2026.csv"} {
		if err := os.WriteFile(filepath.Join(csvDir, name), []byte("Date,Description,Amount\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	store, err := storage.New(csvDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	dl := dataloader.New(csvDir, store)

	Initialize(dl, store, nil) // renderer = nil → JSON responses for tests
	return dl, store, func() {}
}

// setupTestEnvWithRenderer wires the package with a real templates.Renderer
// pulling from the embedded FS, so tests can assert on rendered HTML rather
// than the JSON fallback. Mirrors the majorexpenses helper of the same name.
func setupTestEnvWithRenderer(t *testing.T) (*dataloader.DataLoader, *storage.Storage, func()) {
	t.Helper()
	dl, store, prevCleanup := setupTestEnv(t)

	templatesFS, err := fs.Sub(web.EmbeddedFS, "templates")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	rend, err := templates.NewFromFS(templatesFS, false)
	if err != nil {
		t.Fatalf("NewFromFS: %v", err)
	}
	Initialize(dl, store, rend)
	return dl, store, func() {
		prevCleanup()
		Initialize(dl, store, nil) // restore JSON-mode
	}
}

func newRouter() http.Handler {
	r := chi.NewRouter()
	RegisterRoutes(r)
	return r
}

// readJSON decodes the recorder's body into a map. Used to assert on the
// JSON-mode (renderer == nil) response shape.
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

// loadAccounts reads the current sidecar through the accounts service,
// the same path the handler uses, so a test's assertions about persisted
// state go through the production code path rather than a second reader.
func loadAccounts(t *testing.T, store *storage.Storage) []models.Account {
	t.Helper()
	accts, err := accountssvc.Load(store)
	if err != nil {
		t.Fatalf("load accounts: %v", err)
	}
	return accts
}

func saveAccounts(t *testing.T, store *storage.Storage, accts []models.Account) {
	t.Helper()
	if err := accountssvc.Save(store, accts); err != nil {
		t.Fatalf("save accounts: %v", err)
	}
}

// formPost builds a url-encoded POST request, the shape every mutation
// handler reads.
func formPost(method, target string, values url.Values) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func parseDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// TestHandleCreate_PersistsAndRoundsTrips: POST /accounts with a full
// form, then assert the store holds the account and the response carries
// it in the rendered list.
func TestHandleCreate_PersistsAndRoundsTrips(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"id":                    {"usaa-checking"},
		"name":                  {"USAA Checking"},
		"institution":           {"USAA"},
		"kind":                  {"checking"},
		"file_patterns":         {"usaa-checking*.csv\nusaa"},
		"low_balance_threshold": {"750"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	accts := loadAccounts(t, store)
	if len(accts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accts))
	}
	if accts[0].ID != "usaa-checking" || accts[0].Name != "USAA Checking" {
		t.Errorf("persisted account = %+v", accts[0])
	}
	if accts[0].Kind != models.AccountKindChecking {
		t.Errorf("kind = %q, want checking", accts[0].Kind)
	}
	if accts[0].Institution != "USAA" {
		t.Errorf("institution = %q, want USAA", accts[0].Institution)
	}
	if len(accts[0].FilePatterns) != 2 {
		t.Errorf("patterns = %v, want 2", accts[0].FilePatterns)
	}
	if accts[0].LowBalanceThreshold != 750 {
		t.Errorf("threshold = %v, want 750", accts[0].LowBalanceThreshold)
	}

	// The response (JSON-mode) carries the account in the Accounts list.
	body := readJSON(t, w.Result())
	accountsList, ok := body["Accounts"].([]interface{})
	if !ok || len(accountsList) != 1 {
		t.Fatalf("expected 1 account in response, got %+v", body["Accounts"])
	}
}

// TestHandleCreate_DuplicateIDSurfaced: creating a second account with
// an existing ID must surface the validation error (from accounts.Save)
// rather than swallowing it. The error must appear in the rendered list.
func TestHandleCreate_DuplicateIDSurfaced(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "dup", Name: "First", Kind: models.AccountKindOther},
	})

	form := url.Values{
		"id":   {"dup"},
		"name": {"Second"},
		"kind": {"other"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts", form))

	// The save fails; the handler re-renders the list with the error. The
	// store must NOT contain the second account.
	accts := loadAccounts(t, store)
	if len(accts) != 1 || accts[0].Name != "First" {
		t.Fatalf("store should still hold only the first account, got %+v", accts)
	}
	body := readJSON(t, w.Result())
	errMsg, _ := body["Error"].(string)
	if !strings.Contains(errMsg, "duplicate") {
		t.Errorf("Error = %q, want a duplicate-ID message", errMsg)
	}
	if field, _ := body["ErrorField"].(string); field != "id" {
		t.Errorf("ErrorField = %q, want \"id\" (for focus)", field)
	}
}

// TestHandleCreate_EmptyNameSurfaced: an empty name is a validation
// error the handler catches before save, but it must still be surfaced
// in the rendered list with the right field flagged for focus.
func TestHandleCreate_EmptyNameSurfaced(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	form := url.Values{
		"id":   {"no-name"},
		"name": {"   "},
		"kind": {"other"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts", form))

	accts := loadAccounts(t, store)
	if len(accts) != 0 {
		t.Fatalf("store should be empty, got %+v", accts)
	}
	body := readJSON(t, w.Result())
	errMsg, _ := body["Error"].(string)
	if !strings.Contains(strings.ToLower(errMsg), "name") {
		t.Errorf("Error = %q, want a name-related message", errMsg)
	}
	if field, _ := body["ErrorField"].(string); field != "name" {
		t.Errorf("ErrorField = %q, want \"name\"", field)
	}
}

// TestHandleUpdate_PersistsChanges: POST /accounts/{id} updates the
// existing record in place; the ID in the URL is authoritative, so a
// hidden id field cannot rename the account.
func TestHandleUpdate_PersistsChanges(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "edit-me", Name: "Old", Kind: models.AccountKindOther, FilePatterns: []string{"old*.csv"}},
	})

	form := url.Values{
		"name":          {"New Name"},
		"institution":   {"New Inst"},
		"kind":          {"savings"},
		"file_patterns": {"new*.csv"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/edit-me", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	accts := loadAccounts(t, store)
	if len(accts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accts))
	}
	if accts[0].ID != "edit-me" || accts[0].Name != "New Name" {
		t.Errorf("account = %+v", accts[0])
	}
	if accts[0].Institution != "New Inst" || accts[0].Kind != models.AccountKindSavings {
		t.Errorf("fields not updated: %+v", accts[0])
	}
	if len(accts[0].FilePatterns) != 1 || accts[0].FilePatterns[0] != "new*.csv" {
		t.Errorf("patterns = %v, want [new*.csv]", accts[0].FilePatterns)
	}
}

// TestHandleDelete_RequiresConfirmStep: a delete request without
// confirm=yes must NOT delete. It re-renders with the confirm panel open
// for that account, so a single stray click cannot destroy the account.
func TestHandleDelete_RequiresConfirmStep(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "doomed", Name: "Doomed", Kind: models.AccountKindOther},
	})

	// First click: no confirm token.
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/doomed/delete", url.Values{}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	accts := loadAccounts(t, store)
	if len(accts) != 1 {
		t.Fatalf("account was deleted without confirm, store = %+v", accts)
	}
	body := readJSON(t, w.Result())
	if id, _ := body["ConfirmDeleteID"].(string); id != "doomed" {
		t.Errorf("ConfirmDeleteID = %q, want \"doomed\" (panel should be open)", id)
	}
}

// TestHandleDelete_ConfirmDeletes: with confirm=yes, the account is
// removed and the response reflects the new (empty) list.
func TestHandleDelete_ConfirmDeletes(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "doomed", Name: "Doomed", Kind: models.AccountKindOther},
		{ID: "keep", Name: "Keep", Kind: models.AccountKindOther},
	})

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/doomed/delete", url.Values{"confirm": {"yes"}}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	accts := loadAccounts(t, store)
	if len(accts) != 1 || accts[0].ID != "keep" {
		t.Fatalf("expected only \"keep\" to remain, got %+v", accts)
	}
	body := readJSON(t, w.Result())
	if id, _ := body["ConfirmDeleteID"].(string); id != "" {
		t.Errorf("ConfirmDeleteID = %q, want empty after confirmed delete", id)
	}
}

// TestBuildPageData_PatternEditorListsMatchedFiles: the pattern editor
// must show, per account, which CSVs its patterns currently win. First
// match wins by ascending ID; the fixture has usaa-checking.csv and
// usaa-credit-2026-08.csv both matching the "usaa" substring, and
// vanguard-2026.csv matching nothing.
//
// This pins the documented MatchFile semantics: first match wins by
// ascending ID, NOT most-specific-glob-wins. A future "helpful" change
// to most-specific-wins is caught here.
func TestBuildPageData_PatternEditorListsMatchedFiles(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "usaa-checking", Name: "USAA Checking", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-checking*.csv"}},
		{ID: "usaa-credit", Name: "USAA Credit", Kind: models.AccountKindCredit, FilePatterns: []string{"usaa-credit*.csv"}},
		// A broad catch-all whose ID sorts BEFORE the specific accounts, so
		// first-match-wins gives the catch-all every "usaa" file despite the
		// more specific globs on the later accounts.
		{ID: "usaa-all", Name: "USAA All", Kind: models.AccountKindOther, FilePatterns: []string{"usaa"}},
	})

	data, err := buildPageData(httptest.NewRequest("GET", "/accounts", nil))
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}

	// Account order is by ascending ID: usaa-all, usaa-checking, usaa-credit.
	// "usaa-all" matches every usaa*.csv via the substring "usaa" and wins
	// them all because its ID sorts first.
	got := make(map[string][]string)
	for _, v := range data.Accounts {
		got[v.Account.ID] = v.MatchedFiles
	}
	if want := []string{"usaa-checking.csv", "usaa-credit-2026-08.csv"}; !equalStrings(got["usaa-all"], want) {
		t.Errorf("usaa-all matched = %v, want %v (first match wins by ID order, even over a more specific glob)", got["usaa-all"], want)
	}
	if len(got["usaa-checking"]) != 0 {
		t.Errorf("usaa-checking matched = %v, want empty (usaa-all wins by ID order)", got["usaa-checking"])
	}
	if len(got["usaa-credit"]) != 0 {
		t.Errorf("usaa-credit matched = %v, want empty (usaa-all wins by ID order)", got["usaa-credit"])
	}
	if want := []string{"vanguard-2026.csv"}; !equalStrings(data.UnassignedFiles, want) {
		t.Errorf("unassigned = %v, want %v", data.UnassignedFiles, want)
	}
}

// TestHandleAddAnchor_Persists: POST /accounts/{id}/anchor appends a
// BalanceAnchor (sorted by date) and the response reflects it.
func TestHandleAddAnchor_Persists(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "anch", Name: "Anchors", Kind: models.AccountKindChecking},
	})

	form := url.Values{
		"anchor_date":   {"2026-08-01"},
		"anchor_amount": {"4210.55"},
		"anchor_note":   {"statement"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/anch/anchor", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	accts := loadAccounts(t, store)
	if len(accts) != 1 || len(accts[0].Anchors) != 1 {
		t.Fatalf("expected 1 anchor, got %+v", accts)
	}
	a := accts[0].Anchors[0]
	if a.Date.Format("2006-01-02") != "2026-08-01" {
		t.Errorf("anchor date = %v, want 2026-08-01", a.Date)
	}
	if a.Amount != 4210.55 {
		t.Errorf("anchor amount = %v, want 4210.55", a.Amount)
	}
	if a.Note != "statement" {
		t.Errorf("anchor note = %q, want \"statement\"", a.Note)
	}
}

// TestHandleAddAnchor_KeepsAnchorsSortedByDate: adding an out-of-order
// anchor leaves the slice sorted ascending by date, matching the
// BalanceAt contract (latest anchor at or before a date wins).
func TestHandleAddAnchor_KeepsAnchorsSortedByDate(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "anch", Name: "Anchors", Kind: models.AccountKindChecking, Anchors: []models.BalanceAnchor{
			{Date: parseDate("2026-08-15"), Amount: 1000, Note: "later"},
		}},
	})

	// Add an EARLIER anchor; it must come first in the sorted slice.
	form := url.Values{
		"anchor_date":   {"2026-08-01"},
		"anchor_amount": {"500"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/anch/anchor", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	accts := loadAccounts(t, store)
	if len(accts[0].Anchors) != 2 {
		t.Fatalf("expected 2 anchors, got %d", len(accts[0].Anchors))
	}
	if accts[0].Anchors[0].Date.Format("2006-01-02") != "2026-08-01" {
		t.Errorf("anchors not sorted: first = %v, want 2026-08-01", accts[0].Anchors[0].Date)
	}
}

// TestHandleAddAnchor_ReplacesSameDayAnchor: adding an anchor for a date
// that already has one must leave exactly ONE anchor for that date, and
// the amount every balance/projection uses (via BalanceAt) must be the
// NEW one, not the stale first-seen one latestAnchorAtOrBefore would
// otherwise silently keep (its tie-break favors the first anchor it
// scans, and a naive append would have left two anchors on 2026-08-15).
func TestHandleAddAnchor_ReplacesSameDayAnchor(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "anch", Name: "Anchors", Kind: models.AccountKindChecking, Anchors: []models.BalanceAnchor{
			{Date: parseDate("2026-08-15"), Amount: 1000, Note: "stale statement"},
		}},
	})

	form := url.Values{
		"anchor_date":   {"2026-08-15"},
		"anchor_amount": {"4210.55"},
		"anchor_note":   {"corrected statement"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/anch/anchor", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	accts := loadAccounts(t, store)
	if len(accts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accts))
	}
	if len(accts[0].Anchors) != 1 {
		t.Fatalf("expected exactly 1 anchor on 2026-08-15, got %d: %+v", len(accts[0].Anchors), accts[0].Anchors)
	}
	got := accts[0].Anchors[0]
	if got.Date.Format("2006-01-02") != "2026-08-15" {
		t.Errorf("anchor date = %v, want 2026-08-15", got.Date)
	}
	if got.Amount != 4210.55 {
		t.Errorf("anchor amount = %v, want 4210.55 (the new value, not the stale 1000)", got.Amount)
	}
	if got.Note != "corrected statement" {
		t.Errorf("anchor note = %q, want %q", got.Note, "corrected statement")
	}

	// The balance the rest of the app derives from this account must also
	// reflect the new anchor, not the stale one.
	bal, err := accountssvc.BalanceAt(accts[0], nil, parseDate("2026-08-20"))
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	if !bal.Available {
		t.Fatal("BalanceAt: Available = false, want true")
	}
	if bal.Amount != 4210.55 {
		t.Errorf("BalanceAt = %v, want 4210.55 (from the corrected anchor)", bal.Amount)
	}
}

// TestHandleAddAnchor_SameDayReplacementIsDeterministic pins that adding
// two anchors for the same day, in sequence, always leaves the SECOND
// one's amount authoritative. Pre-fix, this depended on sort.Slice's
// unstable ordering of equal-date elements plus latestAnchorAtOrBefore's
// first-seen tie-break, so which amount won could vary between runs; this
// test repeats the sequence with a fresh store each time to catch that
// kind of nondeterminism, which a single run cannot reveal.
func TestHandleAddAnchor_SameDayReplacementIsDeterministic(t *testing.T) {
	const iterations = 50
	for i := 0; i < iterations; i++ {
		_, store, cleanup := setupTestEnv(t)

		saveAccounts(t, store, []models.Account{
			{ID: "anch", Name: "Anchors", Kind: models.AccountKindChecking},
		})

		first := url.Values{
			"anchor_date":   {"2026-08-15"},
			"anchor_amount": {"1000"},
		}
		w1 := httptest.NewRecorder()
		newRouter().ServeHTTP(w1, formPost("POST", "/accounts/anch/anchor", first))
		if w1.Code != http.StatusOK {
			t.Fatalf("iteration %d: first add status = %d, body=%s", i, w1.Code, w1.Body.String())
		}

		second := url.Values{
			"anchor_date":   {"2026-08-15"},
			"anchor_amount": {"4210.55"},
		}
		w2 := httptest.NewRecorder()
		newRouter().ServeHTTP(w2, formPost("POST", "/accounts/anch/anchor", second))
		if w2.Code != http.StatusOK {
			t.Fatalf("iteration %d: second add status = %d, body=%s", i, w2.Code, w2.Body.String())
		}

		accts := loadAccounts(t, store)
		if len(accts[0].Anchors) != 1 {
			t.Fatalf("iteration %d: expected 1 anchor, got %d: %+v", i, len(accts[0].Anchors), accts[0].Anchors)
		}
		if accts[0].Anchors[0].Amount != 4210.55 {
			t.Fatalf("iteration %d: authoritative amount = %v, want 4210.55 on every run", i, accts[0].Anchors[0].Amount)
		}

		bal, err := accountssvc.BalanceAt(accts[0], nil, parseDate("2026-08-20"))
		if err != nil {
			t.Fatalf("iteration %d: BalanceAt: %v", i, err)
		}
		if bal.Amount != 4210.55 {
			t.Fatalf("iteration %d: BalanceAt = %v, want 4210.55 on every run", i, bal.Amount)
		}

		cleanup()
	}
}

// TestHandleDeleteAnchor_RemovesByDate: the anchor is keyed by its date
// in the URL; deleting removes that one anchor and leaves the rest.
func TestHandleDeleteAnchor_RemovesByDate(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "anch", Name: "Anchors", Kind: models.AccountKindChecking, Anchors: []models.BalanceAnchor{
			{Date: parseDate("2026-08-01"), Amount: 500},
			{Date: parseDate("2026-08-15"), Amount: 1000},
		}},
	})

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/anch/anchor/2026-08-01/delete", url.Values{}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	accts := loadAccounts(t, store)
	if len(accts[0].Anchors) != 1 {
		t.Fatalf("expected 1 anchor after delete, got %d", len(accts[0].Anchors))
	}
	if accts[0].Anchors[0].Date.Format("2006-01-02") != "2026-08-15" {
		t.Errorf("wrong anchor deleted: remaining = %v", accts[0].Anchors[0].Date)
	}
}

// TestHandlePage_RendersFullPageInRendererMode: with a renderer wired,
// GET /accounts returns the base layout (the "accounts-content"
// template) — NOT a bare partial. Guards against the ruling 2026-08-16a
// failure mode where the handler renders a different template than the
// page expects.
func TestHandlePage_RendersFullPageInRendererMode(t *testing.T) {
	_, _, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/accounts", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := strings.ToLower(w.Body.String())
	if !strings.Contains(body, "<!doctype") && !strings.Contains(body, "<html") {
		t.Errorf("non-HTMX GET must include the base layout; got:\n%s", w.Body.String())
	}
	if !strings.Contains(body, "<h1") {
		t.Errorf("page must have an <h1>; got:\n%s", w.Body.String())
	}
	// The accounts-content template must be what rendered, not the
	// "Page Not Found" fallback.
	if !strings.Contains(body, "accounts") {
		t.Errorf("page body does not mention accounts")
	}
}

// TestHandleCreate_RendererMode_SwapsAccountsListPartial: per ruling
// 2026-08-16a, a mutation served as an HTMX partial must render the
// SWAP PARTIAL (accounts-list-partial), not the base layout. This is
// the test that catches the failure mode where tests assert against a
// different template than the handler returns.
func TestHandleCreate_RendererMode_SwapsAccountsListPartial(t *testing.T) {
	_, _, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{
		"id":   {"renderer-acct"},
		"name": {"Renderer"},
		"kind": {"other"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := strings.ToLower(w.Body.String())
	if strings.Contains(body, "<!doctype") || strings.Contains(body, "<html") {
		t.Errorf("mutation response must be the accounts-list partial, not the base layout; got:\n%s", body)
	}
	// The partial is the inner content of #accounts-list (an innerHTML
	// swap, so the wrapping div stays in the DOM). It must render the
	// newly-created account so the user sees their change reflected.
	if !strings.Contains(w.Body.String(), "Renderer") {
		t.Errorf("partial does not show the new account; got:\n%s", w.Body.String())
	}
	// And it must carry the section the page's list lives in, so the
	// swapped-in markup is the list (not the create form alone).
	if !strings.Contains(body, `id="accounts-list-heading"`) {
		t.Errorf("partial must include the accounts list section; got:\n%s", w.Body.String())
	}
}

// TestHandleCreate_RendererMode_ValidationErrorSurfacedInPartial: a
// validation error in renderer mode must surface in the swapped partial
// (with role="alert" / aria-live), not just in the store-state path.
func TestHandleCreate_RendererMode_ValidationErrorSurfacedInPartial(t *testing.T) {
	_, store, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "dup", Name: "First", Kind: models.AccountKindOther},
	})

	form := url.Values{"id": {"dup"}, "name": {"Second"}, "kind": {"other"}}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "duplicate") {
		t.Errorf("partial must surface the duplicate-ID error; got:\n%s", body)
	}
	if !strings.Contains(lower, `role="alert"`) && !strings.Contains(lower, `aria-live="polite"`) {
		t.Errorf("error must be announced via role=alert or aria-live; got:\n%s", body)
	}
	// The offending field must carry aria-invalid so AT announces it.
	if !strings.Contains(lower, `aria-invalid="true"`) {
		t.Errorf("error field must carry aria-invalid=\"true\"; got:\n%s", body)
	}
}

// TestHandlePage_RendererMode_LabelsAssociatedWithInputs: every input
// has a programmatically associated <label for>. A spot-check of the
// create form's required fields, not an exhaustive sweep.
func TestHandlePage_RendererMode_LabelsAssociatedWithInputs(t *testing.T) {
	_, _, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/accounts", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, id := range []string{"acct-new-id", "acct-new-name", "acct-new-kind", "acct-new-patterns", "acct-new-threshold"} {
		if !strings.Contains(body, `for="`+id+`"`) {
			t.Errorf("missing <label for=%q> in page", id)
		}
		if !strings.Contains(body, `id="`+id+`"`) {
			t.Errorf("missing <input id=%q> in page", id)
		}
	}
}

// TestHandlePage_RendererMode_RequiredFieldsAndFormatsStatedInText:
// required fields and the date/currency formats must be stated in text,
// not via placeholder alone (ACCESSIBILITY.md point 6). The anchor
// format/convention text lives in the per-account section, so the test
// seeds one account to render that section.
func TestHandlePage_RendererMode_RequiredFieldsAndFormatsStatedInText(t *testing.T) {
	_, store, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "seed", Name: "Seed", Kind: models.AccountKindChecking},
	})

	req := httptest.NewRequest("GET", "/accounts", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	body := strings.ToLower(w.Body.String())

	if !strings.Contains(body, "required fields are marked") {
		t.Error("required-fields text is missing")
	}
	if !strings.Contains(body, "yyyy-mm-dd") {
		t.Error("anchor date format (YYYY-MM-DD) not stated in text")
	}
	if !strings.Contains(body, "bank convention") {
		t.Error("anchor amount convention not stated in text")
	}
	if !strings.Contains(body, "use the default") {
		t.Error("threshold default text missing")
	}
	if !strings.Contains(body, "anchor day") {
		t.Error("anchor end-of-day meaning not stated")
	}
}

// TestHandlePage_RendererMode_UnassignedFilesShownByText: status is
// never by color alone (point 8). The unassigned-files section carries
// a text label, not just a colored badge.
func TestHandlePage_RendererMode_UnassignedFilesShownByText(t *testing.T) {
	_, _, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/accounts", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(strings.ToLower(body), "unassigned") {
		t.Errorf("unassigned-files text label missing; got:\n%s", body)
	}
	if !strings.Contains(body, "vanguard-2026.csv") {
		t.Errorf("unassigned fixture file not listed; got:\n%s", body)
	}
}

// TestHandleDelete_RendererMode_ConfirmPanelRendered: the delete confirm
// step is served as an HTMX partial. Per ruling 2026-08-16a, the partial
// must render the confirm panel markup (a second submit with
// name="confirm" value="yes"), not a bare acknowledgement. A single click
// must NOT delete — the confirm panel is what gates the destructive
// action, and it must be visible in the swapped markup.
func TestHandleDelete_RendererMode_ConfirmPanelRendered(t *testing.T) {
	_, store, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "doomed", Name: "Doomed", Kind: models.AccountKindOther},
	})

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/doomed/delete", url.Values{}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	// The confirm panel's confirming submit carries the confirm token. If
	// the handler deleted on the first click, this markup would be absent
	// and the account gone.
	if !strings.Contains(body, `name="confirm"`) || !strings.Contains(body, `value="yes"`) {
		t.Errorf("confirm panel must render a confirm=yes submit; got:\n%s", body)
	}
	if !strings.Contains(body, "Yes, delete") {
		t.Errorf("confirm panel must label the destructive submit; got:\n%s", body)
	}
	// And the account must still be present.
	if !strings.Contains(body, "Doomed") {
		t.Errorf("account should still be listed (not deleted); got:\n%s", body)
	}
}

// TestHandleDelete_RendererMode_ConfirmedDeleteRemovesAccount: with
// confirm=yes in renderer mode, the account is gone from the swapped
// partial. Guards against the confirm step being purely cosmetic.
func TestHandleDelete_RendererMode_ConfirmedDeleteRemovesAccount(t *testing.T) {
	_, store, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "doomed", Name: "Doomed", Kind: models.AccountKindOther},
	})

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/doomed/delete", url.Values{"confirm": {"yes"}}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "Doomed") {
		t.Errorf("account should be gone from the partial after confirmed delete; got:\n%s", body)
	}
	// The partial must still be the list section (not a bare ack), so the
	// page's swap target stays coherent.
	if !strings.Contains(body, `id="accounts-list-heading"`) {
		t.Errorf("partial must still render the accounts list section; got:\n%s", body)
	}
}

// TestHandleAddAnchor_RendererMode_AnchorShownInPartial: an anchor added
// via the HTMX partial must appear in the swapped markup, so the user
// sees their change without a full page reload.
func TestHandleAddAnchor_RendererMode_AnchorShownInPartial(t *testing.T) {
	_, store, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "anch", Name: "Anchors", Kind: models.AccountKindChecking},
	})

	form := url.Values{
		"anchor_date":   {"2026-08-01"},
		"anchor_amount": {"4210.55"},
		"anchor_note":   {"statement"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/anch/anchor", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "2026-08-01") {
		t.Errorf("anchor date must appear in the swapped partial; got:\n%s", body)
	}
	if !strings.Contains(body, "$4,210.55") {
		t.Errorf("anchor amount must be formatted and visible; got:\n%s", body)
	}
	if !strings.Contains(body, "statement") {
		t.Errorf("anchor note must appear in the swapped partial; got:\n%s", body)
	}
}

// equalStrings is a small helper for order-equal slice comparison.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
