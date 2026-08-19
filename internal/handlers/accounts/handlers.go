// Package accounts serves the /accounts settings page: CRUD for Account
// records, a file-pattern editor showing which CSVs each pattern currently
// matches, balance-anchor entry, and the per-account low-balance threshold.
//
// Storage goes through internal/services/accounts (the sidecar owner) so
// the page and the loader can never disagree about what an account is. The
// CSV listing for the pattern editor comes from the data directory directly
// (the same glob the accounts service uses for overlap warnings), and
// matching uses accounts.MatchFile so what the page shows is what the
// loader will do (first match wins, by ascending account ID).
package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"budget2/internal/models"
	"budget2/internal/services/accounts"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
)

var (
	loader   *dataloader.DataLoader
	store    *storage.Storage
	renderer *templates.Renderer
)

// errAccountNotFound and errAnchorNotFound are sentinels a Mutate callback
// returns to abort the mutation without saving, letting the handler tell a
// missing-account 404 apart from an unexpected load/save failure without
// depending on Mutate's error carrying anything beyond fmt.Errorf strings.
var (
	errAccountNotFound = errors.New("account not found")
	errAnchorNotFound  = errors.New("anchor not found")
)

// Initialize wires the package with its dependencies. The storage service
// is required: the accounts sidecar is read and written through it. The
// dataloader is used only for its CSVDirectory (the same directory the
// accounts service scans for overlap warnings) so the pattern editor and
// the loader agree on which files exist.
func Initialize(l *dataloader.DataLoader, s *storage.Storage, r *templates.Renderer) {
	loader = l
	store = s
	renderer = r
}

// RegisterRoutes registers the /accounts routes. It is additive on the
// shared chi router; no existing route is modified.
func RegisterRoutes(r chi.Router) {
	r.Get("/accounts", handlePage)
	r.Post("/accounts", handleCreate)
	r.Post("/accounts/{id}", handleUpdate)
	r.Post("/accounts/{id}/delete", handleDelete)
	r.Post("/accounts/{id}/anchor", handleAddAnchor)
	r.Post("/accounts/{id}/anchor/{aid}/delete", handleDeleteAnchor)
}

// accountKinds is the closed list of AccountKind values, in display order.
// The template renders these as <option>s; keeping the order here keeps the
// page and the enum consistent.
var accountKinds = []models.AccountKind{
	models.AccountKindChecking,
	models.AccountKindSavings,
	models.AccountKindBrokerage,
	models.AccountKindCredit,
	models.AccountKindOther,
}

// DefaultLowBalanceThreshold is the UI-stated default an account uses when
// its LowBalanceThreshold is zero. The value matches the design spec
// (§4: "default $500"). It is advisory text only; the accounts service does
// not read it — zero in the record means "use the default" everywhere.
const DefaultLowBalanceThreshold = 500.0

// accountView is the per-account model the template renders. It carries
// the stored account plus the derived data the page needs (matched files,
// whether this account wins each file, and anchor display helpers).
type accountView struct {
	Account      models.Account
	MatchedFiles []string // CSV basenames this account's patterns win
	IsFirst      bool     // first in ID-sorted order (for "first match wins" labels)
}

// pageData is the full-page model. It is also the partial model: the
// accounts-list partial reads the same keys.
type pageData struct {
	Title            string
	ActiveTab        string
	Accounts         []accountView
	UnassignedFiles  []string // CSVs matching no account
	AccountKinds     []models.AccountKind
	DefaultThreshold float64
	Warnings         []string // pattern-overlap warnings from Save
	Error            string   // validation / save error to surface
	ErrorField       string   // the field the error pertains to (for focus)
	ConfirmDeleteID  string   // account whose delete-confirm panel is open
	// UnresolvedDuplicateCount is read by the base layout's nav badge. The
	// accounts page does not own that count; it stays zero here so the
	// badge stays hidden, matching the other settings-style pages.
	UnresolvedDuplicateCount int
}

// handlePage renders the full /accounts page.
func handlePage(w http.ResponseWriter, r *http.Request) {
	data, err := buildPageData(r)
	if err != nil {
		renderError(w, "Failed to load accounts: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if renderer != nil {
		_ = renderer.Render(w, "base", data)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// handleCreate handles POST /accounts (new account).
func handleCreate(w http.ResponseWriter, r *http.Request) {
	accts, formData, saveErr := applyForm(r, "")
	if saveErr != nil {
		// Surface the validation error on the re-rendered list.
		data, _ := buildPageData(r)
		data.Error = saveErr.Error()
		data.ErrorField = formData.errorField
		renderList(w, r, data)
		return
	}
	_ = accts
	data, _ := buildPageData(r)
	renderList(w, r, data)
}

// handleUpdate handles POST /accounts/{id} (edit existing).
func handleUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		renderError(w, "Missing account id", http.StatusBadRequest)
		return
	}
	if _, _, saveErr := applyForm(r, id); saveErr != nil {
		data, _ := buildPageData(r)
		data.Error = saveErr.Error()
		renderList(w, r, data)
		return
	}
	data, _ := buildPageData(r)
	renderList(w, r, data)
}

// handleDelete handles POST /accounts/{id}/delete. Deletion requires an
// explicit confirm step: the form must carry confirm=yes (the confirm
// panel's submit button is a separate action from the initial "Delete"
// button, which only reveals the confirm panel). A request without the
// confirm token re-renders with the confirm panel open rather than
// deleting — so a stray single click cannot destroy the account.
func handleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		renderError(w, "Missing account id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Without the confirm token, open the confirm panel for this account.
	if strings.TrimSpace(r.FormValue("confirm")) != "yes" {
		data, _ := buildPageData(r)
		data.ConfirmDeleteID = id
		renderList(w, r, data)
		return
	}

	loaded := false
	err := accounts.Mutate(store, func(accts []models.Account) ([]models.Account, error) {
		loaded = true
		filtered := make([]models.Account, 0, len(accts))
		for _, a := range accts {
			if a.ID != id {
				filtered = append(filtered, a)
			}
		}
		return filtered, nil
	})
	if err != nil {
		if !loaded {
			renderError(w, "Failed to load accounts: "+err.Error(), http.StatusInternalServerError)
			return
		}
		data, _ := buildPageData(r)
		data.Error = err.Error()
		renderList(w, r, data)
		return
	}
	data, _ := buildPageData(r)
	renderList(w, r, data)
}

// handleAddAnchor handles POST /accounts/{id}/anchor. Records a
// BalanceAnchor on the account and re-sorts anchors by date. Any existing
// anchor on the same calendar day is replaced rather than left alongside
// the new one -- the end-of-day balance is a single fact, so two anchors
// on one day would make it ambiguous which amount is authoritative, and
// latestAnchorAtOrBefore's tie-break on encounter order would make that
// ambiguity depend on slice order rather than intent. This mirrors
// applyAnchor in internal/services/mcpsvc/ledger/anchor.go so the UI and
// MCP paths agree. The anchor records the balance at the END of the
// anchor day; the template states that in the label so the user does not
// misread it as a start-of-day figure.
func handleAddAnchor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		renderError(w, "Missing account id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	dateStr := strings.TrimSpace(r.FormValue("anchor_date"))
	amountStr := strings.TrimSpace(r.FormValue("anchor_amount"))
	note := strings.TrimSpace(r.FormValue("anchor_note"))

	if dateStr == "" || amountStr == "" {
		data, _ := buildPageData(r)
		data.Error = "Anchor date and amount are required."
		data.ErrorField = "anchor_date"
		renderList(w, r, data)
		return
	}
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		data, _ := buildPageData(r)
		data.Error = "Anchor date must be in YYYY-MM-DD format."
		data.ErrorField = "anchor_date"
		renderList(w, r, data)
		return
	}
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		data, _ := buildPageData(r)
		data.Error = "Anchor amount must be a number, e.g. 4210.55."
		data.ErrorField = "anchor_amount"
		renderList(w, r, data)
		return
	}

	loaded := false
	err = accounts.Mutate(store, func(accts []models.Account) ([]models.Account, error) {
		loaded = true
		found := false
		for i := range accts {
			if accts[i].ID != id {
				continue
			}
			// Drop any anchor already on this calendar day before
			// appending the new one, then re-sort. Filtering first keeps
			// the outcome deterministic: exactly one anchor per day
			// regardless of what order the old slice held them in.
			filtered := make([]models.BalanceAnchor, 0, len(accts[i].Anchors)+1)
			for _, a := range accts[i].Anchors {
				if !sameCalendarDay(a.Date, date) {
					filtered = append(filtered, a)
				}
			}
			filtered = append(filtered, models.BalanceAnchor{
				Date:   date,
				Amount: amount,
				Note:   note,
			})
			sort.Slice(filtered, func(j, k int) bool { return filtered[j].Date.Before(filtered[k].Date) })
			accts[i].Anchors = filtered
			accts[i].UpdatedAt = time.Now()
			found = true
			break
		}
		if !found {
			return nil, errAccountNotFound
		}
		return accts, nil
	})
	if err != nil {
		if !loaded {
			renderError(w, "Failed to load accounts: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if errors.Is(err, errAccountNotFound) {
			renderError(w, "Account not found", http.StatusNotFound)
			return
		}
		data, _ := buildPageData(r)
		data.Error = err.Error()
		renderList(w, r, data)
		return
	}
	data, _ := buildPageData(r)
	renderList(w, r, data)
}

// sameCalendarDay reports whether a and b fall on the same calendar day in
// a's location, ignoring time-of-day. A BalanceAnchor states the balance
// as of the END of a day, so two anchors landing on the same day are
// necessarily competing statements of the same fact.
func sameCalendarDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// handleDeleteAnchor handles POST /accounts/{id}/anchor/{aid}/delete. The
// anchor is identified by its date (YYYY-MM-DD) — anchors are unique by
// date in the UI (the add handler keeps them sorted; duplicates are a data
// error the user can resolve by deleting one).
func handleDeleteAnchor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	aid := chi.URLParam(r, "aid")
	if id == "" || aid == "" {
		renderError(w, "Missing account or anchor id", http.StatusBadRequest)
		return
	}
	date, err := time.Parse("2006-01-02", aid)
	if err != nil {
		renderError(w, "Invalid anchor date", http.StatusBadRequest)
		return
	}
	loaded := false
	err = accounts.Mutate(store, func(accts []models.Account) ([]models.Account, error) {
		loaded = true
		found := false
		for i := range accts {
			if accts[i].ID != id {
				continue
			}
			filtered := make([]models.BalanceAnchor, 0, len(accts[i].Anchors))
			for _, a := range accts[i].Anchors {
				if a.Date.Equal(date) {
					found = true
					continue
				}
				filtered = append(filtered, a)
			}
			accts[i].Anchors = filtered
			accts[i].UpdatedAt = time.Now()
			break
		}
		if !found {
			return nil, errAnchorNotFound
		}
		return accts, nil
	})
	if err != nil {
		if !loaded {
			renderError(w, "Failed to load accounts: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if errors.Is(err, errAnchorNotFound) {
			renderError(w, "Anchor not found", http.StatusNotFound)
			return
		}
		data, _ := buildPageData(r)
		data.Error = err.Error()
		renderList(w, r, data)
		return
	}
	data, _ := buildPageData(r)
	renderList(w, r, data)
}

// formData carries the parsed form fields plus the field name to focus on
// error. It is populated by parseAccountForm and consumed by applyForm.
type formData struct {
	id           string
	name         string
	institution  string
	kind         string
	filePatterns []string
	lowThreshold float64
	errorField   string
}

// parseAccountForm reads the account fields from a submitted form. It
// performs input normalization the accounts service does not (pattern
// splitting, kind validation), so the service's Validate sees clean data.
func parseAccountForm(r *http.Request) (formData, error) {
	if err := r.ParseForm(); err != nil {
		return formData{}, fmt.Errorf("invalid form data: %w", err)
	}
	fd := formData{
		id:          strings.TrimSpace(r.FormValue("id")),
		name:        strings.TrimSpace(r.FormValue("name")),
		institution: strings.TrimSpace(r.FormValue("institution")),
		kind:        strings.TrimSpace(r.FormValue("kind")),
	}
	// File patterns: accept one-per-line (textarea) and/or repeated
	// "file_pattern" inputs. The textarea is the primary UI; the repeated
	// input is a convenience for test fixtures. Blank lines and
	// whitespace-only entries are dropped.
	patterns := []string{}
	if raw := r.FormValue("file_patterns"); raw != "" {
		for _, line := range strings.Split(raw, "\n") {
			if p := strings.TrimSpace(line); p != "" {
				patterns = append(patterns, p)
			}
		}
	}
	for _, p := range r.Form["file_pattern"] {
		if v := strings.TrimSpace(p); v != "" {
			patterns = append(patterns, v)
		}
	}
	fd.filePatterns = patterns

	// Low-balance threshold: blank or zero = use default. A non-numeric
	// value is a user error worth surfacing.
	thrStr := strings.TrimSpace(r.FormValue("low_balance_threshold"))
	if thrStr != "" {
		v, err := strconv.ParseFloat(thrStr, 64)
		if err != nil {
			fd.errorField = "low_balance_threshold"
			return fd, fmt.Errorf("low-balance threshold must be a number, e.g. 500")
		}
		fd.lowThreshold = v
	}
	// Kind: default to "other" when omitted or unknown so the record is
	// always valid; the <select> restricts the UI to the five values.
	switch models.AccountKind(fd.kind) {
	case models.AccountKindChecking, models.AccountKindSavings,
		models.AccountKindBrokerage, models.AccountKindCredit, models.AccountKindOther:
	default:
		fd.kind = string(models.AccountKindOther)
	}
	return fd, nil
}

// applyForm parses the form, upserts the account into the loaded set, and
// saves through accounts.Mutate so the load, the upsert and the save are one
// held section. On update (existingID != "") the ID field in the form is
// ignored — the URL param is authoritative, so a user cannot rename an
// account's ID by editing the hidden field. Returns the saved account list
// so callers can skip a second Load.
func applyForm(r *http.Request, existingID string) ([]models.Account, formData, error) {
	fd, err := parseAccountForm(r)
	if err != nil {
		return nil, fd, err
	}

	id := existingID
	if id == "" {
		id = fd.id
	}
	if id == "" {
		fd.errorField = "id"
		return nil, fd, fmt.Errorf("account ID is required")
	}
	if strings.TrimSpace(fd.name) == "" {
		fd.errorField = "name"
		return nil, fd, fmt.Errorf("account name is required")
	}

	now := time.Now()
	var result []models.Account
	err = accounts.Mutate(store, func(accts []models.Account) ([]models.Account, error) {
		// Update or append. On update (existingID != "") the URL-param ID is
		// authoritative, so we merge into the matching record. On create
		// (existingID == "") we ALWAYS append — even if an account with this
		// ID already exists — so accounts.Mutate's save-side Validate sees
		// the duplicate and surfaces the error rather than silently
		// overwriting the existing record.
		if existingID != "" {
			for i := range accts {
				if accts[i].ID == id {
					accts[i].Name = fd.name
					accts[i].Institution = fd.institution
					accts[i].Kind = models.AccountKind(fd.kind)
					accts[i].FilePatterns = fd.filePatterns
					accts[i].LowBalanceThreshold = fd.lowThreshold
					accts[i].UpdatedAt = now
					break
				}
			}
		} else {
			accts = append(accts, models.Account{
				ID:                  id,
				Name:                fd.name,
				Institution:         fd.institution,
				Kind:                models.AccountKind(fd.kind),
				FilePatterns:        fd.filePatterns,
				LowBalanceThreshold: fd.lowThreshold,
				CreatedAt:           now,
				UpdatedAt:           now,
			})
		}
		result = accts
		return accts, nil
	})
	// Mutate's save surfaces duplicate-ID and empty-name errors from
	// Validate. We pre-validated name above; the duplicate-ID case is the
	// one this catches on create.
	if err != nil {
		if existingID == "" && HasDuplicateID(result, id) {
			fd.errorField = "id"
		}
		return nil, fd, err
	}
	return result, fd, nil
}

// buildPageData loads accounts and CSVs, computes the pattern matches the
// template shows, and returns the full page model. It never returns a
// nil data map — on load failure the caller renders an error page.
func buildPageData(r *http.Request) (pageData, error) {
	accts, err := accounts.Load(store)
	if err != nil {
		return pageData{}, err
	}
	if accts == nil {
		accts = []models.Account{}
	}

	// List the CSV files in the data directory. The accounts service's
	// overlap check uses the same glob; using it here keeps the pattern
	// editor consistent with what the loader will match.
	basenames := csvBasenames()

	// First-match-wins map: for each file, the winning account ID.
	matchByID := make(map[string]string, len(basenames))
	for _, name := range basenames {
		matchByID[name] = accounts.MatchFile(accts, name)
	}

	// Build per-account views in ID order (the order MatchFile resolves).
	sorted := sortedAccountsByID(accts)
	views := make([]accountView, 0, len(sorted))
	for i, a := range sorted {
		var matched []string
		for _, name := range basenames {
			if matchByID[name] == a.ID {
				matched = append(matched, name)
			}
		}
		views = append(views, accountView{
			Account:      a,
			MatchedFiles: matched,
			IsFirst:      i == 0,
		})
	}

	var unassigned []string
	for _, name := range basenames {
		if matchByID[name] == "" {
			unassigned = append(unassigned, name)
		}
	}

	return pageData{
		Title:            "Accounts",
		ActiveTab:        "accounts",
		Accounts:         views,
		UnassignedFiles:  unassigned,
		AccountKinds:     accountKinds,
		DefaultThreshold: DefaultLowBalanceThreshold,
	}, nil
}

// renderList renders the accounts-list partial. It is the HTMX swap
// target for every mutation handler, so a single write is the page's
// whole response to a mutation. When no renderer is wired (tests), it
// falls back to JSON so the mutation is still observable.
func renderList(w http.ResponseWriter, r *http.Request, data pageData) {
	if renderer != nil {
		_ = renderer.RenderPartial(w, "accounts-list-partial", data)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// writeJSON is the test-fallback path (renderer == nil). It lets the
// handler-package tests assert on round-trips without rendering HTML.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// renderError mirrors the majorexpenses error card so the styling is
// consistent across pages.
func renderError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	body := fmt.Sprintf(`<div class="p-4 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg">
		<div class="flex items-center">
			<svg class="w-5 h-5 text-red-500 dark:text-red-400 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>
			</svg>
			<span class="text-red-700 dark:text-red-300 font-medium">Error</span>
		</div>
		<p class="mt-2 text-sm text-red-600 dark:text-red-400">%s</p>
	</div>`, html.EscapeString(message))
	_, _ = w.Write([]byte(body))
}

// csvBasenames lists *.csv in the data directory, sorted. Falls back to
// the dataloader's CSVDirectory when the store is unavailable; the store
// is the authoritative path in production.
func csvBasenames() []string {
	dir := ""
	if store != nil {
		dir = store.BaseDir()
	} else if loader != nil {
		dir = loader.CSVDirectory
	}
	if dir == "" {
		return nil
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.csv"))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, filepath.Base(f))
	}
	sort.Strings(out)
	return out
}

// sortedAccountsByID returns a copy in ascending ID order, matching the
// order accounts.MatchFile resolves in.
func sortedAccountsByID(accts []models.Account) []models.Account {
	out := make([]models.Account, len(accts))
	copy(out, accts)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// HasDuplicateID reports whether id appears more than once in accts. Used
// to focus the ID field on a duplicate-ID save error rather than the name
// field. Kept here (not in the accounts service) because it is a UI
// concern: the service returns a single error string; the handler needs
// to know which field to focus.
func HasDuplicateID(accts []models.Account, id string) bool {
	n := 0
	for _, a := range accts {
		if a.ID == id {
			n++
		}
	}
	return n > 1
}

// init guarantees the package-level logger is never a nil-deref in tests
// that never call Initialize: accounts.Save logs warnings via the stdlib
// log package, which is always safe.
var _ = log.Print
