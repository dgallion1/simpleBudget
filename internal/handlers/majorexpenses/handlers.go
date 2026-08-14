// Package majorexpenses serves the user-managed list of declared major
// expenses and the exception buckets that fall out of matching them
// against imported transactions.
package majorexpenses

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	majorexpenseengine "budget2/internal/services/majorexpenses"
	"budget2/internal/templates"
)

const (
	defaultUnknownThreshold = 100.0
	defaultNewWindowDays    = 30
)

var (
	loader   *dataloader.DataLoader
	renderer *templates.Renderer
)

// Initialize sets up the package with required dependencies.
func Initialize(l *dataloader.DataLoader, r *templates.Renderer) {
	loader = l
	renderer = r
}

// RegisterRoutes registers all major-expenses routes.
func RegisterRoutes(r chi.Router) {
	r.Get("/major-expenses", handleMajorExpensesPage)
	r.Post("/major-expenses", handleAdd)
	r.Put("/major-expenses/{id}", handleUpdate)
	r.Delete("/major-expenses/{id}", handleDelete)
	r.Post("/major-expenses/{id}/restore", handleRestore)
	r.Delete("/major-expenses/deleted/{id}", handleDiscard)
	r.Get("/major-expenses/exceptions", handleExceptions)
	r.Post("/major-expenses/pins", handlePin)
	r.Post("/major-expenses/pins/bulk", handleBulkPin)
	r.Delete("/major-expenses/pins/{hash}", handleUnpin)
}

func handleMajorExpensesPage(w http.ResponseWriter, r *http.Request) {
	data, err := buildPageData(r)
	if err != nil {
		renderError(w, "Failed to build page: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if renderer != nil {
		// HTMX requests targeting the results wrapper get only the
		// wrapper partial. Returning the full base layout into the
		// wrapper would nest a complete page inside the results area.
		if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Target") == "major-expenses-results-wrapper" {
			_ = renderer.RenderPartial(w, "major-expenses-results-wrapper", data)
			return
		}
		templates.AttachDuplicateCount(data, loader)
		_ = renderer.Render(w, "base", data)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func handleAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}
	me, err := parseExpenseForm(r)
	if err != nil {
		renderError(w, err.Error(), http.StatusBadRequest)
		return
	}
	me.ID = uuid.New().String()
	if _, err := loader.AddMajorExpense(me); err != nil {
		renderError(w, "Failed to save: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Optional: pin a specific transaction to the new expense in the
	// same operation. Used by the "+ Create new from this" affordance on
	// exception rows so the originating transaction is guaranteed to be
	// matched, even if its description wouldn't have been picked up by
	// the keywords alone. Pin failure does not roll back the create.
	if pinHash := strings.TrimSpace(r.FormValue("pin_hash")); pinHash != "" {
		if err := loader.SetTransactionPin(pinHash, me.ID); err != nil {
			log.Printf("major-expenses: create-and-pin failed for hash=%q expense=%q: %v", pinHash, me.ID, err)
			_, _ = fmt.Fprintf(w, "<!-- pin_hash %q ignored: %v -->", pinHash, err)
		}
	}
	renderResults(w, r)
}

func handleUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		renderError(w, "Missing id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}
	me, err := parseExpenseForm(r)
	if err != nil {
		renderError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := loader.UpdateMajorExpense(id, me); err != nil {
		renderError(w, "Failed to save: "+err.Error(), http.StatusInternalServerError)
		return
	}
	renderResults(w, r)
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		renderError(w, "Missing id", http.StatusBadRequest)
		return
	}
	// Soft-delete: archive the definition and capture pinned hashes so
	// the user can Restore later. ArchiveMajorExpense removes the entry
	// from the active list and detaches its pins.
	if err := loader.ArchiveMajorExpense(id); err != nil {
		renderError(w, "Failed to delete: "+err.Error(), http.StatusInternalServerError)
		return
	}
	renderResults(w, r)
}

// handleRestore moves a soft-deleted expense back into the active list,
// re-applying captured pins for transactions that aren't currently
// pinned to a different expense.
func handleRestore(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		renderError(w, "Missing id", http.StatusBadRequest)
		return
	}
	if err := loader.RestoreMajorExpense(id); err != nil {
		renderError(w, "Failed to restore: "+err.Error(), http.StatusInternalServerError)
		return
	}
	renderResults(w, r)
}

// handleDiscard permanently removes an archived expense from the
// soft-delete archive. There is no undo.
func handleDiscard(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		renderError(w, "Missing id", http.StatusBadRequest)
		return
	}
	if err := loader.DiscardDeletedMajorExpense(id); err != nil {
		renderError(w, "Failed to discard: "+err.Error(), http.StatusInternalServerError)
		return
	}
	renderResults(w, r)
}

// handlePin assigns a transaction (by hash) to a major expense. The
// pin overrides keyword/amount matching for that one transaction.
// Form body: hash, expense_id.
func handlePin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}
	hash := strings.TrimSpace(r.FormValue("hash"))
	expenseID := strings.TrimSpace(r.FormValue("expense_id"))
	if hash == "" {
		renderError(w, "Missing transaction hash", http.StatusBadRequest)
		return
	}
	if expenseID == "" {
		renderError(w, "Missing expense id", http.StatusBadRequest)
		return
	}
	// Validate the target expense exists.
	expenses, err := loader.LoadMajorExpenses()
	if err != nil {
		renderError(w, "Failed to load expenses: "+err.Error(), http.StatusInternalServerError)
		return
	}
	found := false
	for _, e := range expenses {
		if e.ID == expenseID {
			found = true
			break
		}
	}
	if !found {
		renderError(w, "Major expense not found", http.StatusNotFound)
		return
	}
	if err := loader.SetTransactionPin(hash, expenseID); err != nil {
		renderError(w, "Failed to save pin: "+err.Error(), http.StatusInternalServerError)
		return
	}
	renderResults(w, r)
}

// handleBulkPin assigns many transactions (by hash) to a single major
// expense in one disk write. Form body: expense_id, hashes (repeated
// form field — one entry per transaction). The UI uses this to pin
// every currently-visible exception row when the user has narrowed the
// list with the search filter, so they don't have to dropdown-and-pick
// 30 times.
func handleBulkPin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}
	expenseID := strings.TrimSpace(r.FormValue("expense_id"))
	if expenseID == "" {
		renderError(w, "Missing expense id", http.StatusBadRequest)
		return
	}
	rawHashes := r.Form["hashes"]
	updates := make(map[string]string, len(rawHashes))
	for _, h := range rawHashes {
		if h = strings.TrimSpace(h); h != "" {
			updates[h] = expenseID
		}
	}
	if len(updates) == 0 {
		renderError(w, "No transaction hashes supplied", http.StatusBadRequest)
		return
	}

	expenses, err := loader.LoadMajorExpenses()
	if err != nil {
		renderError(w, "Failed to load expenses: "+err.Error(), http.StatusInternalServerError)
		return
	}
	found := false
	for _, e := range expenses {
		if e.ID == expenseID {
			found = true
			break
		}
	}
	if !found {
		renderError(w, "Major expense not found", http.StatusNotFound)
		return
	}
	if _, err := loader.SetTransactionPins(updates); err != nil {
		renderError(w, "Failed to save pins: "+err.Error(), http.StatusInternalServerError)
		return
	}
	renderResults(w, r)
}

// handleUnpin removes a transaction's pin so it falls back to
// keyword/amount matching.
func handleUnpin(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	if hash == "" {
		renderError(w, "Missing hash", http.StatusBadRequest)
		return
	}
	if err := loader.ClearTransactionPin(hash); err != nil {
		renderError(w, "Failed to unpin: "+err.Error(), http.StatusInternalServerError)
		return
	}
	renderResults(w, r)
}

func handleExceptions(w http.ResponseWriter, r *http.Request) {
	data, err := buildPageData(r)
	if err != nil {
		renderError(w, "Failed to compute exceptions: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if renderer != nil {
		_ = renderer.RenderPartial(w, "major-expenses-exceptions", data)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

// renderResults sends the combined dual-column partial used by every
// mutation handler. Threads the active window through so post-mutation
// re-renders preserve the user's filter.
func renderResults(w http.ResponseWriter, r *http.Request) {
	data, err := buildPageData(r)
	if err != nil {
		renderError(w, "Failed to refresh page: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if renderer != nil {
		_ = renderer.RenderPartial(w, "major-expenses-results", data)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

// buildPageData loads expenses + transactions, applies the active date
// window from the request, runs Match, and produces the dual-card page
// data. It is the single source of truth for both full-page and partial
// rendering.
func buildPageData(r *http.Request) (map[string]interface{}, error) {
	expenses, err := loader.LoadMajorExpenses()
	if err != nil {
		return nil, fmt.Errorf("load major expenses: %w", err)
	}
	if expenses == nil {
		expenses = []models.MajorExpense{}
	}

	txns, err := loader.LoadData()
	if err != nil {
		return nil, fmt.Errorf("load transactions: %w", err)
	}

	pins, err := loader.LoadTransactionPins()
	if err != nil {
		return nil, fmt.Errorf("load transaction pins: %w", err)
	}

	// Resolve the active window from the request, then narrow the
	// transaction set BEFORE the outflow filter / matcher pipeline so
	// per-expense rollups AND exception buckets reflect only in-window
	// transactions.
	minDate := txns.MinDate()
	maxDate := txns.MaxDate()
	startDate, endDate := parseRangeFromRequest(r, txns)
	windowed := txns.Active().FilterByDateRange(startDate, endDate)

	// Major Expenses is an expense-tracking page — filter to outflows
	// BEFORE matching so income (paychecks, refunds, transfers) can't
	// inflate "matched" counts/totals when its description happens to
	// contain a keyword.
	outflows := windowed.FilterByType(models.Outflow)

	match := majorexpenseengine.Match(outflows, expenses, majorexpenseengine.MatchOptions{
		UnknownLargeThreshold: defaultUnknownThreshold,
		NewMerchantWindow:     time.Duration(defaultNewWindowDays) * 24 * time.Hour,
		Pins:                  pins,
	})

	type ExpenseSummary struct {
		Expense      models.MajorExpense
		Count        int
		PinnedCount  int
		Total        float64
		Transactions []models.Transaction
		PinnedHashes map[string]bool
	}
	summaries := make([]ExpenseSummary, 0, len(expenses))
	for _, e := range expenses {
		var total float64
		txns := append([]models.Transaction(nil), match.Groups[e.ID]...)
		for _, t := range txns {
			// Outflows: purchases are negative, refunds/credits stay
			// positive (classifier convention). Net spend = -sum(amount)
			// so refunds reduce the group total instead of inflating it
			// via AbsAmount.
			total += -t.Amount
		}
		sort.Slice(txns, func(i, j int) bool { return txns[i].Date.After(txns[j].Date) })

		pinnedForExpense := make(map[string]bool)
		for _, t := range txns {
			if match.PinnedHashes[t.Hash] {
				pinnedForExpense[t.Hash] = true
			}
		}

		summaries = append(summaries, ExpenseSummary{
			Expense:      e,
			Count:        len(txns),
			PinnedCount:  len(pinnedForExpense),
			Total:        total,
			Transactions: txns,
			PinnedHashes: pinnedForExpense,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return strings.ToLower(strings.TrimSpace(summaries[i].Expense.Name)) <
			strings.ToLower(strings.TrimSpace(summaries[j].Expense.Name))
	})

	var totalDeclared float64
	for _, s := range summaries {
		totalDeclared += s.Total
	}

	// Surface the silent unmatched gap. match.Unmatched contains every
	// in-window outflow that didn't match a keyword/amount/pin — its sum
	// is what makes Dashboard's total exceed Major Expenses' "declared"
	// total when the over-$100 exception bucket is empty.
	var unmatchedTotal float64
	for _, t := range match.Unmatched {
		// Net spend, consistent with per-group totals: refunds (positive
		// Outflow amounts in the classifier convention) reduce the gap
		// instead of inflating it via AbsAmount.
		unmatchedTotal += -t.Amount
	}

	// Pre-sort the full unmatched list by abs amount desc so the bucket
	// renders biggest-first. Over-threshold items naturally float to the
	// top (they used to be the only visible rows); sub-threshold items
	// follow so the user can see — and pin — the previously-hidden long
	// tail of small transactions.
	allUnmatched := append([]models.Transaction(nil), match.Unmatched...)
	sort.Slice(allUnmatched, func(i, j int) bool {
		return allUnmatched[i].AbsAmount() > allUnmatched[j].AbsAmount()
	})

	// Inverted maps for O(1) per-row lookups in the template:
	//   matchedHashToExpenseID: which expense a transaction is matched to
	//   expenseByID:            full definition for label rendering
	matchedHashToExpenseID := make(map[string]string)
	for expenseID, group := range match.Groups {
		for _, t := range group {
			if t.Hash != "" {
				matchedHashToExpenseID[t.Hash] = expenseID
			}
		}
	}
	expenseByID := make(map[string]models.MajorExpense, len(expenses))
	for _, e := range expenses {
		expenseByID[e.ID] = e
	}

	deleted, err := loader.LoadDeletedMajorExpenses()
	if err != nil {
		return nil, fmt.Errorf("load deleted major expenses: %w", err)
	}
	if deleted == nil {
		deleted = []models.DeletedMajorExpense{}
	}
	// Most-recently-deleted first so the panel reads top-down by recency.
	sort.Slice(deleted, func(i, j int) bool {
		return deleted[i].DeletedAt.After(deleted[j].DeletedAt)
	})

	return map[string]interface{}{
		"Title":                  "Major Expenses",
		"ActiveTab":              "major-expenses",
		"Expenses":               expenses,
		"ExpenseByID":            expenseByID,
		"ExpenseOptions":         buildExpenseOptions(expenses),
		"Summaries":              summaries,
		"TotalDeclared":          totalDeclared,
		"UnmatchedTotal":         unmatchedTotal,
		"UnmatchedCount":         len(match.Unmatched),
		"TrackingVerdict":        BuildTrackingVerdict(totalDeclared, unmatchedTotal, len(match.Unmatched)),
		"AllUnmatched":           allUnmatched,
		"Match":                  match,
		"MatchedHashToExpenseID": matchedHashToExpenseID,
		"PinnedHashes":           match.PinnedHashes,
		"PinMap":                 pins,
		"Deleted":                deleted,
		"Threshold":              defaultUnknownThreshold,
		"WindowDays":             defaultNewWindowDays,
		"StartDate":              formatDateInputValue(startDate),
		"EndDate":                formatDateInputValue(endDate),
		"MinDate":                formatDateInputValue(minDate),
		"MaxDate":                formatDateInputValue(maxDate),
	}, nil
}

// ExpenseOption is the label model used by the "Pin to…" picker. Labels
// are disambiguated by appending the first keyword only when the name
// collides with another entry, so unique names stay short.
type ExpenseOption struct {
	ID    string
	Label string
}

func buildExpenseOptions(expenses []models.MajorExpense) []ExpenseOption {
	nameCount := make(map[string]int, len(expenses))
	for _, e := range expenses {
		nameCount[strings.ToLower(strings.TrimSpace(e.Name))]++
	}
	out := make([]ExpenseOption, 0, len(expenses))
	for _, e := range expenses {
		label := e.Name
		key := strings.ToLower(strings.TrimSpace(e.Name))
		if nameCount[key] > 1 && len(e.Keywords) > 0 {
			if kw := strings.TrimSpace(e.Keywords[0]); kw != "" {
				label = e.Name + " — " + kw
			}
		}
		out = append(out, ExpenseOption{ID: e.ID, Label: label})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label)
	})
	return out
}

// parseExpenseForm extracts a MajorExpense from form values without
// stamping ID/timestamps — those are set by the storage layer or
// preserved on update. The rules for what makes a definition valid live
// in majorexpenseengine.Validate, shared with the MCP curation tools so
// the page and the tools cannot disagree about what is acceptable.
func parseExpenseForm(r *http.Request) (models.MajorExpense, error) {
	expectedMin, err := parseFormFloat(r, "expected_min")
	if err != nil {
		return models.MajorExpense{}, fmt.Errorf("invalid expected_min: %w", err)
	}
	expectedMax, err := parseFormFloat(r, "expected_max")
	if err != nil {
		return models.MajorExpense{}, fmt.Errorf("invalid expected_max: %w", err)
	}

	me := models.MajorExpense{
		Name:               strings.TrimSpace(r.FormValue("name")),
		Keywords:           splitAndTrim(r.FormValue("keywords"), ","),
		ExpectedMin:        expectedMin,
		ExpectedMax:        expectedMax,
		Notes:              strings.TrimSpace(r.FormValue("notes")),
		IsInternalTransfer: parseFormBool(r, "is_internal_transfer"),
	}
	if err := majorexpenseengine.Validate(me); err != nil {
		return models.MajorExpense{}, err
	}
	return me, nil
}

// parseFormBool returns true when the form value is "on", "true", or "1"
// (case-insensitive). Browsers send "on" for unset-value checkboxes and
// omit the field entirely when unchecked, so a missing key yields false.
func parseFormBool(r *http.Request, key string) bool {
	v := strings.ToLower(strings.TrimSpace(r.FormValue(key)))
	return v == "on" || v == "true" || v == "1"
}

func parseFormFloat(r *http.Request, key string) (float64, error) {
	v := strings.TrimSpace(r.FormValue(key))
	if v == "" {
		return 0, nil
	}
	return strconv.ParseFloat(v, 64)
}

func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// formatDateInputValue formats a date for use as an <input type="date">
// value attribute. Returns "" for the zero time so empty data sets render
// blank inputs rather than "0001-01-01".
func formatDateInputValue(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// parseRangeFromRequest resolves the active date window for a request.
// Order of resolution per side: URL query → form value → fallback to the
// loaded data's MinDate / MaxDate. Unparseable values silently fall back —
// query params are a UX convenience, not a strict API contract.
func parseRangeFromRequest(r *http.Request, txns *models.TransactionSet) (start, end time.Time) {
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	if startStr == "" || endStr == "" {
		// Form values cover POST/PUT/DELETE bodies (mutation handlers).
		// ParseForm is idempotent and cheap — safe to call even on GET.
		_ = r.ParseForm()
		if startStr == "" {
			startStr = r.PostForm.Get("start")
		}
		if endStr == "" {
			endStr = r.PostForm.Get("end")
		}
	}
	start = txns.MinDate()
	end = txns.MaxDate()
	if startStr != "" {
		if t, err := time.Parse("2006-01-02", startStr); err == nil {
			start = t
		}
	}
	if endStr != "" {
		if t, err := time.Parse("2006-01-02", endStr); err == nil {
			end = t
		}
	}
	return start, end
}

func renderError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	body := fmt.Sprintf(`<div class="p-4 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg">
		<div class="flex items-center">
			<svg class="w-5 h-5 text-red-500 dark:text-red-400 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>
			</svg>
			<span class="text-red-700 dark:text-red-300 font-medium">Error</span>
		</div>
		<p class="mt-2 text-sm text-red-600 dark:text-red-400">%s</p>
	</div>`, html.EscapeString(message))
	_, _ = w.Write([]byte(body))
}
