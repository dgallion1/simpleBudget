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
	r.Get("/major-expenses/exceptions", handleExceptions)
	r.Post("/major-expenses/pins", handlePin)
	r.Post("/major-expenses/pins/bulk", handleBulkPin)
	r.Delete("/major-expenses/pins/{hash}", handleUnpin)
}

func handleMajorExpensesPage(w http.ResponseWriter, r *http.Request) {
	data, err := buildPageData()
	if err != nil {
		renderError(w, "Failed to build page: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if renderer != nil {
		renderer.Render(w, "base", data)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
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
	renderResults(w)
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
	renderResults(w)
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		renderError(w, "Missing id", http.StatusBadRequest)
		return
	}
	remaining, err := loader.DeleteMajorExpense(id)
	if err != nil {
		renderError(w, "Failed to delete: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Drop any pins that pointed at the deleted expense so transactions
	// fall back to keyword/amount matching instead of disappearing.
	validIDs := make(map[string]bool, len(remaining))
	for _, e := range remaining {
		validIDs[e.ID] = true
	}
	if err := loader.PrunePinsForMissingExpenses(validIDs); err != nil {
		log.Printf("warning: prune pins after delete: %v", err)
	}
	renderResults(w)
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
	renderResults(w)
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
	renderResults(w)
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
	renderResults(w)
}

func handleExceptions(w http.ResponseWriter, r *http.Request) {
	data, err := buildPageData()
	if err != nil {
		renderError(w, "Failed to compute exceptions: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if renderer != nil {
		renderer.RenderPartial(w, "major-expenses-exceptions", data)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// renderResults sends the combined dual-column partial used by every
// mutation handler.
func renderResults(w http.ResponseWriter) {
	data, err := buildPageData()
	if err != nil {
		renderError(w, "Failed to refresh page: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if renderer != nil {
		renderer.RenderPartial(w, "major-expenses-results", data)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// buildPageData loads expenses + transactions and runs Match. It is the
// single source of truth for both full-page and partial rendering.
func buildPageData() (map[string]interface{}, error) {
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

	// Major Expenses is an expense-tracking page — filter to outflows
	// BEFORE matching so income (paychecks, refunds, transfers) can't
	// inflate "matched" counts/totals when its description happens to
	// contain a keyword (e.g. "ANTHROPIC" appearing in both a $108
	// subscription charge AND a $2000 paycheck). The exception
	// detectors filter to outflows internally, so this only affects
	// grouping and the per-expense Summary roll-ups.
	outflows := txns.FilterByType(models.Outflow)

	match := majorexpenseengine.Match(outflows, expenses, majorexpenseengine.MatchOptions{
		UnknownLargeThreshold: defaultUnknownThreshold,
		NewMerchantWindow:     time.Duration(defaultNewWindowDays) * 24 * time.Hour,
		Pins:                  pins,
	})

	// Build per-expense summaries so the list partial can render counts,
	// totals, and the matched transactions without recomputing. The
	// PinnedHashes map is scoped to THIS expense so the sub-template
	// (where $ is the Summary, not the page data) can render the
	// 📌-prefix and "unpin" affordance per row without traversing back
	// up the data tree.
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
			total += t.AbsAmount()
		}
		// Most-recent first so the user sees the latest match at the top.
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

	return map[string]interface{}{
		"Title":          "Major Expenses",
		"ActiveTab":      "major-expenses",
		"Expenses":       expenses,
		"ExpenseOptions": buildExpenseOptions(expenses),
		"Summaries":      summaries,
		"Match":          match,
		"PinnedHashes":   match.PinnedHashes,
		"Threshold":      defaultUnknownThreshold,
		"WindowDays":     defaultNewWindowDays,
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
// preserved on update.
func parseExpenseForm(r *http.Request) (models.MajorExpense, error) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		return models.MajorExpense{}, fmt.Errorf("name is required")
	}
	if len(name) > 200 {
		return models.MajorExpense{}, fmt.Errorf("name is too long (max 200 chars)")
	}

	keywordsRaw := r.FormValue("keywords")
	keywords := splitAndTrim(keywordsRaw, ",")

	min, err := parseFormFloat(r, "expected_min")
	if err != nil {
		return models.MajorExpense{}, fmt.Errorf("invalid expected_min: %w", err)
	}
	if min < 0 {
		return models.MajorExpense{}, fmt.Errorf("expected_min cannot be negative")
	}
	max, err := parseFormFloat(r, "expected_max")
	if err != nil {
		return models.MajorExpense{}, fmt.Errorf("invalid expected_max: %w", err)
	}
	if max < 0 {
		return models.MajorExpense{}, fmt.Errorf("expected_max cannot be negative")
	}
	if min > 0 && max > 0 && min > max {
		return models.MajorExpense{}, fmt.Errorf("expected_min cannot exceed expected_max")
	}

	// An expense is valid in three configurations:
	//  1. At least one keyword (range optional, anomaly-only when set).
	//  2. No keywords + both Min and Max > 0 (amount-only match — useful
	//     for fixed-dollar checks where the description varies).
	//  3. No keywords + Min == Max == 0 (pin-only target — the user
	//     plans to manually pin transactions to it; e.g. an "Amazon —
	//     Books" sub-bucket separate from "Amazon — Household").
	// Anything else is a partial/inconsistent config: only Min or only
	// Max set without a keyword usually means the user forgot the other
	// half of the range.
	if len(keywords) == 0 && (min > 0) != (max > 0) {
		return models.MajorExpense{}, fmt.Errorf("set BOTH Min and Max to match by amount, or leave both blank to create a pin-only target")
	}

	return models.MajorExpense{
		Name:        name,
		Keywords:    keywords,
		ExpectedMin: min,
		ExpectedMax: max,
		Notes:       strings.TrimSpace(r.FormValue("notes")),
	}, nil
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
	w.Write([]byte(body))
}
