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
	"regexp"
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

// TestBuildPageData_OverlapWarningsNameContestedBasename: two accounts
// whose FilePatterns both match usaa-checking.csv (the broad "usaa"
// substring on one, the specific "usaa-checking*.csv" glob on the other)
// must produce a non-empty Warnings naming that basename. This is the
// GET/full-page path — S4 wires buildPageData's Warnings field to
// accounts.OverlapWarnings, which previously went nowhere.
func TestBuildPageData_OverlapWarningsNameContestedBasename(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "a-wide", Name: "Wide", Kind: models.AccountKindOther, FilePatterns: []string{"usaa"}},
		{ID: "b-narrow", Name: "Narrow", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-checking*.csv"}},
	})

	data, err := buildPageData(httptest.NewRequest("GET", "/accounts", nil))
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}
	if len(data.Warnings) == 0 {
		t.Fatalf("Warnings is empty, want a warning naming usaa-checking.csv")
	}
	found := false
	for _, w := range data.Warnings {
		if strings.Contains(w, "usaa-checking.csv") {
			found = true
		}
	}
	if !found {
		t.Errorf("Warnings = %v, want one naming usaa-checking.csv", data.Warnings)
	}
}

// TestHandleCreate_OverlapWarningsSurfacedInPartial: creating an account
// whose patterns overlap an existing account's must surface the warning
// on the very response that renders the mutation (the accounts-list
// partial, JSON-mode here since no renderer is wired), not only on a
// subsequent full page load. The handler package falls back to JSON when
// no renderer is wired, which makes Warnings directly assertable.
func TestHandleCreate_OverlapWarningsSurfacedInPartial(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "a-wide", Name: "Wide", Kind: models.AccountKindOther, FilePatterns: []string{"usaa"}},
	})

	form := url.Values{
		"id":            {"b-narrow"},
		"name":          {"Narrow"},
		"kind":          {"checking"},
		"file_patterns": {"usaa-checking*.csv"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	body := readJSON(t, w.Result())
	warnings, ok := body["Warnings"].([]interface{})
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected non-empty Warnings in the mutation response, got %+v", body["Warnings"])
	}
	found := false
	for _, w := range warnings {
		if s, _ := w.(string); strings.Contains(s, "usaa-checking.csv") {
			found = true
		}
	}
	if !found {
		t.Errorf("Warnings = %v, want one naming usaa-checking.csv", warnings)
	}
}

// TestBuildPageData_NoOverlapNoWarnings: accounts whose patterns match
// disjoint files produce no warnings, and the amber block must not
// render for them (the renderer-mode assertion lives alongside the other
// RendererMode tests below; this pins the underlying data).
func TestBuildPageData_NoOverlapNoWarnings(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "usaa-checking", Name: "USAA Checking", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-checking*.csv"}},
		{ID: "usaa-credit", Name: "USAA Credit", Kind: models.AccountKindCredit, FilePatterns: []string{"usaa-credit*.csv"}},
	})

	data, err := buildPageData(httptest.NewRequest("GET", "/accounts", nil))
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}
	if len(data.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none for non-overlapping patterns", data.Warnings)
	}
}

// scriptBlockRE matches every <script>...</script> element in a rendered
// page, used to scope body-text assertions to the RENDERED content and
// exclude script source. base.html and accounts.html both emit <script>
// blocks on every response regardless of warning state (HTMX config, theme
// init, syncWarnings itself), and syncWarnings' own dismiss-confirmation
// string legitimately contains the phrase "Pattern overlap warning" as
// literal JS source. A whole-body substring scan for that phrase would
// therefore fail the moment the confirmation wording used it too -- which
// is exactly why an earlier attempt had to use inconsistent wording
// ("Account overlap warning dismissed.") instead of matching the banner's
// own text, purely to dodge this test. Stripping script blocks before
// scanning removes that coupling.
var scriptBlockRE = regexp.MustCompile(`(?s)<script[^>]*>.*?</script>`)

// renderedBodyWithoutScripts returns body with every <script>...</script>
// element removed, so assertions about what is actually RENDERED (visible
// markup, sr-only live regions, hidden data carriers) are not tripped by
// unrelated JS source text that happens to share a substring.
func renderedBodyWithoutScripts(body string) string {
	return scriptBlockRE.ReplaceAllString(body, "")
}

// TestHandlePage_RendererMode_NoOverlapRendersNoWarningBlock: with a
// renderer wired, a non-overlapping configuration must not render the
// amber "Pattern overlap warning" block at all.
func TestHandlePage_RendererMode_NoOverlapRendersNoWarningBlock(t *testing.T) {
	_, store, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "usaa-checking", Name: "USAA Checking", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-checking*.csv"}},
		{ID: "usaa-credit", Name: "USAA Credit", Kind: models.AccountKindCredit, FilePatterns: []string{"usaa-credit*.csv"}},
	})

	req := httptest.NewRequest("GET", "/accounts", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	// Scoped to the rendered warning block (banner, live region, hidden
	// data carrier), not the whole body: the whole body also contains the
	// always-served <script> block, whose dismiss-confirmation string
	// legitimately shares this same phrase as literal JS source.
	rendered := renderedBodyWithoutScripts(w.Body.String())
	if strings.Contains(rendered, "Pattern overlap warning") {
		t.Errorf("non-overlapping config rendered the overlap warning block:\n%s", rendered)
	}
}

// TestHandlePage_RendererMode_OverlapRendersWarningBlockOnce: with a
// renderer wired, an overlapping configuration must render the amber
// warning block, and exactly once — the full page previously duplicated
// it (once outside #accounts-list, once inside accounts-list-partial),
// which S4 removed since Warnings is now always populated when there is
// an overlap.
func TestHandlePage_RendererMode_OverlapRendersWarningBlockOnce(t *testing.T) {
	_, store, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "a-wide", Name: "Wide", Kind: models.AccountKindOther, FilePatterns: []string{"usaa"}},
		{ID: "b-narrow", Name: "Narrow", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-checking*.csv"}},
	})

	req := httptest.NewRequest("GET", "/accounts", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Pattern overlap warning") {
		t.Fatalf("overlapping config did not render the overlap warning block:\n%s", body)
	}
	// Count the VISIBLE warning heading (">Pattern overlap warning<"), not
	// bare substring occurrences: the stable live region's announcement
	// text and the hidden #accounts-warnings-data payload both legitimately
	// carry the same phrase (as "Pattern overlap warning: ..."), which is
	// not the double-rendering bug this test guards against.
	if got := strings.Count(body, ">Pattern overlap warning<"); got != 1 {
		t.Errorf("visible overlap warning heading rendered %d times, want exactly 1 (was duplicated: once outside #accounts-list, once inside the partial)", got)
	}
	// The ANNOUNCEMENT lives on the stable #accounts-warnings-region
	// (outside #accounts-list, so it survives every HTMX swap), not on the
	// visible amber block (which is destroyed and recreated by
	// hx-swap="innerHTML" on every mutation and would not reliably
	// announce — ACCESSIBILITY.md point 10).
	if !strings.Contains(body, `id="accounts-warnings-region"`) {
		t.Fatalf("missing the stable #accounts-warnings-region live region:\n%s", body)
	}
	regionStart := strings.Index(body, `id="accounts-warnings-region"`)
	regionTagStart := strings.LastIndex(body[:regionStart], "<div")
	regionTagEnd := strings.Index(body[regionStart:], ">") + regionStart
	regionTag := body[regionTagStart:regionTagEnd]
	if !strings.Contains(regionTag, `role="status"`) || !strings.Contains(regionTag, `aria-live="polite"`) {
		t.Errorf("#accounts-warnings-region must carry role=\"status\" aria-live=\"polite\"; got:\n%s", regionTag)
	}
	if !strings.Contains(body, "usaa-checking.csv") {
		t.Fatalf("expected warning text naming usaa-checking.csv; got:\n%s", body)
	}
	regionText := body[regionTagEnd : strings.Index(body[regionTagEnd:], "</div>")+regionTagEnd]
	if !strings.Contains(regionText, "usaa-checking.csv") {
		t.Errorf("#accounts-warnings-region text does not carry the warning content; got:\n%s", regionText)
	}
	// The visible amber block must NOT also carry a live-region role: two
	// live regions announcing the same warning would read it out twice.
	bannerStart := strings.Index(body, `id="accounts-warnings-banner"`)
	if bannerStart == -1 {
		t.Fatalf("missing the visible #accounts-warnings-banner block:\n%s", body)
	}
	bannerTagStart := strings.LastIndex(body[:bannerStart], "<div")
	bannerTagEnd := strings.Index(body[bannerStart:], ">") + bannerStart
	bannerTag := body[bannerTagStart:bannerTagEnd]
	if strings.Contains(bannerTag, `role="status"`) || strings.Contains(bannerTag, "aria-live") {
		t.Errorf("#accounts-warnings-banner must not also be a live region (would double-announce); got:\n%s", bannerTag)
	}
	// The banner must carry a real, keyboard-operable dismiss button with
	// an accessible name that says what it dismisses (ACCESSIBILITY.md
	// point 14) — not a bare "x".
	if !strings.Contains(body, `aria-label="Dismiss pattern overlap warning"`) {
		t.Errorf("overlap warning banner is missing a dismiss button with an accessible name; got:\n%s", body)
	}
	if !strings.Contains(body, "data-dismiss-warnings") {
		t.Errorf("dismiss button markup (data-dismiss-warnings) missing; got:\n%s", body)
	}
}

// TestHandleCreate_RendererMode_OverlapWarningRendersOnceInPartialSwap:
// the accounts-list-partial response served directly to a mutation (the
// actual HTMX swap payload, not the full page) must render the warning
// markup exactly once. A future edit that moves the warning block out of
// the partial (e.g. back to living only in the full-page template) must
// turn this test red — the full-page assertion above renders through
// buildPageData + the base layout and would not by itself catch that.
func TestHandleCreate_RendererMode_OverlapWarningRendersOnceInPartialSwap(t *testing.T) {
	_, store, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "a-wide", Name: "Wide", Kind: models.AccountKindOther, FilePatterns: []string{"usaa"}},
	})

	form := url.Values{
		"id":            {"b-narrow"},
		"name":          {"Narrow"},
		"kind":          {"checking"},
		"file_patterns": {"usaa-checking*.csv"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	// This response IS the partial (asserted elsewhere: no <html>/<!doctype>
	// wrapper). The warning must appear exactly once in it.
	if strings.Contains(strings.ToLower(body), "<!doctype") || strings.Contains(strings.ToLower(body), "<html") {
		t.Fatalf("mutation response must be the bare partial, not the full page; got:\n%s", body)
	}
	// Count the VISIBLE warning heading (">Pattern overlap warning<"), not
	// bare substring occurrences: the hidden #accounts-warnings-data node
	// also carries the phrase (inside data-warnings-text="Pattern overlap
	// warning: ..."), which is a second, legitimate, non-duplicate copy
	// used only to sync the stable live region — not a rendering bug.
	if got := strings.Count(body, ">Pattern overlap warning<"); got != 1 {
		t.Errorf("mutation (partial-swap) response rendered the visible warning heading %d times, want exactly 1; got:\n%s", got, body)
	}
	if !strings.Contains(body, `id="accounts-warnings-data"`) {
		t.Errorf("partial-swap response missing #accounts-warnings-data (needed to sync the stable live region after a swap); got:\n%s", body)
	}
}

// TestHandleCreate_RendererMode_DismissButtonAbsentWithoutWarnings: the
// dismiss control is part of the warning banner. With no overlap, there is
// nothing to dismiss, so the control must not be present.
func TestHandleCreate_RendererMode_DismissButtonAbsentWithoutWarnings(t *testing.T) {
	_, _, cleanup := setupTestEnvWithRenderer(t)
	defer cleanup()

	form := url.Values{"id": {"solo"}, "name": {"Solo"}, "kind": {"other"}}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "Pattern overlap warning") {
		t.Fatalf("no accounts overlap; warning block should not render:\n%s", body)
	}
	if strings.Contains(body, `aria-label="Dismiss pattern overlap warning"`) {
		t.Errorf("dismiss button should not render when there are no warnings; got:\n%s", body)
	}
	// The data carrier must still be present (empty), so the client script
	// can clear a stale live region / dismissal key from a previous state.
	if !strings.Contains(body, `id="accounts-warnings-data"`) {
		t.Errorf("partial-swap response missing #accounts-warnings-data even with no warnings; got:\n%s", body)
	}
	if !strings.Contains(body, `data-warnings-key=""`) {
		t.Errorf("data-warnings-key should be empty with no warnings; got:\n%s", body)
	}
}

// TestBuildPageData_WarningsKey_ChangesWithContent: WarningsKey is the
// client-side dismissal fingerprint (ACCESSIBILITY.md point 14). It must be
// empty with no warnings, non-empty with warnings, and DIFFERENT when the
// warning content differs — otherwise a dismissal of one overlap would
// silently swallow a different, later overlap that happens to arrive while
// the same sessionStorage key is still set.
func TestBuildPageData_WarningsKey_ChangesWithContent(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	// No accounts yet: no warnings, no key.
	data, err := buildPageData(httptest.NewRequest("GET", "/accounts", nil))
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}
	if data.WarningsKey != "" {
		t.Errorf("WarningsKey = %q, want empty with no warnings", data.WarningsKey)
	}

	saveAccounts(t, store, []models.Account{
		{ID: "a-wide", Name: "Wide", Kind: models.AccountKindOther, FilePatterns: []string{"usaa"}},
		{ID: "b-narrow", Name: "Narrow", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-checking*.csv"}},
	})
	dataWithOverlap, err := buildPageData(httptest.NewRequest("GET", "/accounts", nil))
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}
	if dataWithOverlap.WarningsKey == "" {
		t.Fatal("WarningsKey is empty despite non-empty Warnings")
	}

	// Change the overlap: a-wide no longer overlaps b-narrow (patterns no
	// longer share a file), but usaa-credit-2026-08.csv now overlaps with a
	// third, broader account instead — a DIFFERENT warning set.
	saveAccounts(t, store, []models.Account{
		{ID: "a-wide", Name: "Wide", Kind: models.AccountKindOther, FilePatterns: []string{"usaa-credit"}},
		{ID: "b-narrow", Name: "Narrow", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-checking*.csv"}},
		{ID: "c-catchall", Name: "Catchall", Kind: models.AccountKindOther, FilePatterns: []string{"usaa"}},
	})
	dataChanged, err := buildPageData(httptest.NewRequest("GET", "/accounts", nil))
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}
	if dataChanged.WarningsKey == "" {
		t.Fatal("WarningsKey is empty despite non-empty Warnings")
	}
	if dataChanged.WarningsKey == dataWithOverlap.WarningsKey {
		t.Errorf("WarningsKey did not change even though the warning content changed: both = %q", dataChanged.WarningsKey)
	}
}

// TestHandleCreate_OverlapDoesNotBlockSave: pattern-overlap warnings are
// advisory. Creating an account whose patterns overlap an existing one
// must still persist the new account — the save is not turned into a
// validation error.
func TestHandleCreate_OverlapDoesNotBlockSave(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "a-wide", Name: "Wide", Kind: models.AccountKindOther, FilePatterns: []string{"usaa"}},
	})

	form := url.Values{
		"id":            {"b-narrow"},
		"name":          {"Narrow"},
		"kind":          {"checking"},
		"file_patterns": {"usaa-checking*.csv"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	body := readJSON(t, w.Result())
	if errMsg, _ := body["Error"].(string); errMsg != "" {
		t.Errorf("Error = %q, want empty — an overlap is advisory, not a validation error", errMsg)
	}

	accts := loadAccounts(t, store)
	if len(accts) != 2 {
		t.Fatalf("expected both accounts persisted despite the overlap, got %+v", accts)
	}
}

// TestWarningsKey_OrderInvariant: warningsKey must be a function of the
// warning SET, not the slice order buildPageData happened to produce it
// in. csvBasenames() sorts and accounts are walked by ID, so today's order
// is incidentally stable, but nothing pins either of those; an
// order-sensitive key would make a future reordering look like a content
// change and un-dismiss (and re-announce) a warning set the user already
// saw, which is exactly what ACCESSIBILITY.md point 14 forbids.
func TestWarningsKey_OrderInvariant(t *testing.T) {
	a := []string{"a", "b"}
	b := []string{"b", "a"}
	ka := warningsKey(a)
	kb := warningsKey(b)
	if ka == "" || kb == "" {
		t.Fatalf("warningsKey returned empty for non-empty input: key(%v)=%q key(%v)=%q", a, ka, b, kb)
	}
	if ka != kb {
		t.Errorf("warningsKey is order-sensitive: key(%v) = %q, key(%v) = %q, want equal (same content, different order)", a, ka, b, kb)
	}
	// warningsKey must sort a COPY for hashing, not the caller's slice:
	// Warnings is rendered in display order, which is someone else's
	// decision to make, not warningsKey's. Assert this on b, NOT a: a is
	// already in sorted order ("a" < "b"), so an implementation that sorted
	// the caller's slice in place (e.g. `sorted := warnings` followed by
	// sort.Strings(sorted), which aliases rather than copies) would leave a
	// untouched and this guard would never catch it. b starts unsorted
	// ("b", "a"); if warningsKey sorts in place it becomes ("a", "b"),
	// which this assertion catches.
	if b[0] != "b" || b[1] != "a" {
		t.Errorf("warningsKey mutated its input slice in place: got %v, want [b a] unchanged", b)
	}
}

// TestHandleUpdate_ResolvingOverlapClearsWarnings: a mutation path that can
// resolve a pattern overlap (narrowing a pattern so it no longer contests a
// file) must clear both Warnings and WarningsKey on the very response that
// applies it — not just leave the stale warning in place until the next
// unrelated read. This is the regression the checker's own test proved was
// unguarded in attempt 2: all nine shipped tests only ever created
// overlaps, never resolved one, so a handler that never re-clears Warnings
// (e.g. a stale per-request cache) would still pass every shipped test.
func TestHandleUpdate_ResolvingOverlapClearsWarnings(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "a-wide", Name: "Wide", Kind: models.AccountKindOther, FilePatterns: []string{"usaa"}},
		{ID: "b-narrow", Name: "Narrow", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-checking*.csv"}},
	})

	// Confirm the overlap actually exists before resolving it, so this
	// test exercises a real transition rather than starting from "already
	// empty" (which would pass vacuously).
	before, err := buildPageData(httptest.NewRequest("GET", "/accounts", nil))
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}
	if len(before.Warnings) == 0 || before.WarningsKey == "" {
		t.Fatalf("fixture does not overlap before the update: Warnings=%v WarningsKey=%q", before.Warnings, before.WarningsKey)
	}

	// Narrow b-narrow's pattern so it no longer matches usaa-checking.csv.
	// a-wide's substring match ("usaa") is then the only remaining match
	// for that file, so the contest is gone.
	form := url.Values{
		"name":          {"Narrow"},
		"kind":          {"checking"},
		"file_patterns": {"no-such-file*.csv"},
	}
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/b-narrow", form))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	body := readJSON(t, w.Result())
	warnings, _ := body["Warnings"].([]interface{})
	if len(warnings) != 0 {
		t.Errorf("Warnings = %v, want empty on the response that resolves the overlap", warnings)
	}
	if key, _ := body["WarningsKey"].(string); key != "" {
		t.Errorf("WarningsKey = %q, want empty on the response that resolves the overlap", key)
	}
}

// TestHandleDelete_ResolvingOverlapClearsWarnings: deleting the account
// that was contesting a file must clear both Warnings and WarningsKey on
// the response — the same stale-warning regression as the update case
// above, but via removing an account rather than editing its patterns.
func TestHandleDelete_ResolvingOverlapClearsWarnings(t *testing.T) {
	_, store, cleanup := setupTestEnv(t)
	defer cleanup()

	saveAccounts(t, store, []models.Account{
		{ID: "a-wide", Name: "Wide", Kind: models.AccountKindOther, FilePatterns: []string{"usaa"}},
		{ID: "b-narrow", Name: "Narrow", Kind: models.AccountKindChecking, FilePatterns: []string{"usaa-checking*.csv"}},
	})

	before, err := buildPageData(httptest.NewRequest("GET", "/accounts", nil))
	if err != nil {
		t.Fatalf("buildPageData: %v", err)
	}
	if len(before.Warnings) == 0 || before.WarningsKey == "" {
		t.Fatalf("fixture does not overlap before the delete: Warnings=%v WarningsKey=%q", before.Warnings, before.WarningsKey)
	}

	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, formPost("POST", "/accounts/b-narrow/delete", url.Values{"confirm": {"yes"}}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	body := readJSON(t, w.Result())
	warnings, _ := body["Warnings"].([]interface{})
	if len(warnings) != 0 {
		t.Errorf("Warnings = %v, want empty on the response that removes the contesting account", warnings)
	}
	if key, _ := body["WarningsKey"].(string); key != "" {
		t.Errorf("WarningsKey = %q, want empty on the response that removes the contesting account", key)
	}

	accts := loadAccounts(t, store)
	if len(accts) != 1 || accts[0].ID != "a-wide" {
		t.Fatalf("expected only a-wide to remain, got %+v", accts)
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
