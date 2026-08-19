// Package explorer serves the file-manager and transaction-explorer pages:
// browsing/enabling CSV files in the data directory, listing transactions
// with filtering and sorting, and editing per-transaction aliases.
package explorer

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"budget2/internal/config"
	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/majorexpenses"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
)

var (
	loader   *dataloader.DataLoader
	renderer *templates.Renderer
	cfg      *config.Config
	store    *storage.Storage
)

// Initialize sets up the explorer package with required dependencies
func Initialize(l *dataloader.DataLoader, r *templates.Renderer, c *config.Config, s *storage.Storage) {
	loader = l
	renderer = r
	cfg = c
	store = s
}

// annotateAndFilterByMajorExpense loads the user's major-expense
// definitions plus pin overrides, runs the matching engine against the
// filtered transaction set, and returns:
//   - the full list of declared major expenses (for the dropdown),
//   - a hash → expense-name lookup so the template can render the
//     "Major Expense" column for each row,
//   - a (possibly narrowed) transaction set: if selectedID is non-empty
//     and points to an existing expense, the set is replaced with just
//     that expense's matched transactions.
//
// Errors loading expenses or pins are logged and treated as empty.
func annotateAndFilterByMajorExpense(filtered *models.TransactionSet, selectedID string) ([]models.MajorExpense, map[string]string, *models.TransactionSet) {
	expenses, err := loader.LoadMajorExpenses()
	if err != nil {
		log.Printf("Warning: could not load major expenses: %v", err)
		expenses = nil
	}
	pins, err := loader.LoadTransactionPins()
	if err != nil {
		log.Printf("Warning: could not load transaction pins: %v", err)
		pins = nil
	}

	match := majorexpenses.Match(filtered, expenses, majorexpenses.MatchOptions{Pins: pins})

	expenseByID := make(map[string]models.MajorExpense, len(expenses))
	for _, e := range expenses {
		expenseByID[e.ID] = e
	}
	hashToExpense := make(map[string]string)
	for id, txns := range match.Groups {
		name := expenseByID[id].Name
		for _, t := range txns {
			if t.Hash != "" {
				hashToExpense[t.Hash] = name
			}
		}
	}

	if selectedID != "" {
		// Validate the ID against the declared expense list, NOT against
		// match.Groups: an expense with zero matching transactions is
		// missing from Groups, so a Groups-only check would silently leave
		// `filtered` unchanged and the UI would claim "filtered to X" while
		// actually showing every transaction.
		if _, ok := expenseByID[selectedID]; ok {
			filtered = models.NewTransactionSet(match.Groups[selectedID])
		}
	}
	return expenses, hashToExpense, filtered
}

// RegisterRoutes registers all explorer routes
func RegisterRoutes(r chi.Router) {
	r.Get("/explorer", handleExplorer)
	r.Get("/explorer/transactions", handleTransactionsPartial)
	r.Get("/explorer/files", handleFileManager)
	r.Get("/explorer/import/scan", handleImportScan)
	r.Post("/explorer/import", handleImport)
	r.Post("/explorer/files/toggle", handleFileToggle)
	r.Post("/explorer/upload", handleFileUpload)
	r.Post("/explorer/alias", handleAlias)
	r.Delete("/explorer/files/{filename}", handleFileDelete)
}

func handleExplorer(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, "Error loading data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get filter parameters
	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	txnType := r.URL.Query().Get("type")
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	sortField := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")
	pageStr := r.URL.Query().Get("page")
	perPageStr := r.URL.Query().Get("perPage")

	// Defaults
	if sortField == "" {
		sortField = "date"
	}
	if order == "" {
		order = "desc"
	}

	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(perPageStr)
	if perPage < 1 {
		perPage = 25
	}

	minDate := data.MinDate()
	maxDate := data.MaxDate()

	var startDate, endDate time.Time
	if startStr != "" {
		startDate, _ = time.Parse("2006-01-02", startStr)
	} else {
		startDate = minDate
	}
	if endStr != "" {
		endDate, _ = time.Parse("2006-01-02", endStr)
	} else {
		endDate = maxDate
	}

	// Apply filters
	filtered := data.FilterByDateRange(startDate, endDate)

	if category != "" {
		filtered = filtered.FilterByCategory(category)
	}
	if search != "" {
		filtered = filtered.FilterBySearch(search)
	}
	if txnType != "" {
		if txnType == "Income" {
			filtered = filtered.FilterByType(models.Income)
		} else if txnType == "Outflow" {
			filtered = filtered.FilterByType(models.Outflow)
		} else if txnType == "Transfer" {
			filtered = filtered.FilterByType(models.Transfer)
		}
	}

	// Major expense annotation + filter
	majorExpenseID := r.URL.Query().Get("majorExpense")
	expenses, hashToExpense, filtered := annotateAndFilterByMajorExpense(filtered, majorExpenseID)

	// Calculate totals before pagination
	totalCount := filtered.Len()
	totalIncome := filtered.FilterByType(models.Income).SumAmount()
	// Use signed sum so refunds (opposite-signed Outflow rows) reduce the total.
	// math.Abs makes it positive regardless of CSV convention (purchases stored
	// as positive vs. negative).
	totalExpenses := math.Abs(filtered.FilterByType(models.Outflow).SumAmount())
	netAmount := totalIncome - totalExpenses

	// Apply sorting
	filtered = sortTransactions(filtered, sortField, order)

	// Apply pagination
	totalPages := filtered.TotalPages(perPage)
	if page > totalPages && totalPages > 0 {
		page = totalPages
	}
	paginated := filtered.Paginate(page, perPage)

	// Calculate page range for pagination UI
	pageRange := calculatePageRange(page, totalPages)

	// Calculate page start/end for display
	pageStart := (page-1)*perPage + 1
	pageEnd := pageStart + paginated.Len() - 1
	if totalCount == 0 {
		pageStart = 0
		pageEnd = 0
	}

	pageData := map[string]interface{}{
		"Title":              "Data Explorer",
		"ActiveTab":          "explorer",
		"Transactions":       paginated.Transactions,
		"Categories":         data.Categories(),
		"Search":             search,
		"Category":           category,
		"Type":               txnType,
		"StartDate":          startDate.Format("2006-01-02"),
		"EndDate":            endDate.Format("2006-01-02"),
		"MinDate":            minDate.Format("2006-01-02"),
		"MaxDate":            maxDate.Format("2006-01-02"),
		"Sort":               sortField,
		"Order":              order,
		"Page":               page,
		"PerPage":            perPage,
		"TotalPages":         totalPages,
		"TotalCount":         totalCount,
		"TotalIncome":        totalIncome,
		"TotalExpenses":      totalExpenses,
		"NetAmount":          netAmount,
		"PageRange":          pageRange,
		"PageStart":          pageStart,
		"PageEnd":            pageEnd,
		"MajorExpenses":      expenses,
		"HashToExpense":      hashToExpense,
		"MajorExpenseFilter": majorExpenseID,
	}

	templates.AttachDuplicateCount(pageData, loader)
	if renderer != nil {
		_ = renderer.Render(w, "base", pageData)
	} else {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><h1>Data Explorer</h1><p>Templates not loaded.</p></body></html>"))
	}
}

func handleTransactionsPartial(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get filter parameters
	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")
	txnType := r.URL.Query().Get("type")
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	sortField := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")
	pageStr := r.URL.Query().Get("page")
	perPageStr := r.URL.Query().Get("perPage")

	// Defaults
	if sortField == "" {
		sortField = "date"
	}
	if order == "" {
		order = "desc"
	}

	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(perPageStr)
	if perPage < 1 {
		perPage = 25
	}

	minDate := data.MinDate()
	maxDate := data.MaxDate()

	var startDate, endDate time.Time
	if startStr != "" {
		startDate, _ = time.Parse("2006-01-02", startStr)
	} else {
		startDate = minDate
	}
	if endStr != "" {
		endDate, _ = time.Parse("2006-01-02", endStr)
	} else {
		endDate = maxDate
	}

	// Apply filters
	filtered := data.FilterByDateRange(startDate, endDate)

	if category != "" {
		filtered = filtered.FilterByCategory(category)
	}
	if search != "" {
		filtered = filtered.FilterBySearch(search)
	}
	if txnType != "" {
		if txnType == "Income" {
			filtered = filtered.FilterByType(models.Income)
		} else if txnType == "Outflow" {
			filtered = filtered.FilterByType(models.Outflow)
		} else if txnType == "Transfer" {
			filtered = filtered.FilterByType(models.Transfer)
		}
	}

	// Major expense annotation + filter
	majorExpenseID := r.URL.Query().Get("majorExpense")
	_, hashToExpense, filtered := annotateAndFilterByMajorExpense(filtered, majorExpenseID)

	// Calculate totals before pagination
	totalCount := filtered.Len()
	totalIncome := filtered.FilterByType(models.Income).SumAmount()
	// Use signed sum so refunds (opposite-signed Outflow rows) reduce the total.
	// math.Abs makes it positive regardless of CSV convention (purchases stored
	// as positive vs. negative).
	totalExpenses := math.Abs(filtered.FilterByType(models.Outflow).SumAmount())
	netAmount := totalIncome - totalExpenses

	// Apply sorting
	filtered = sortTransactions(filtered, sortField, order)

	// Apply pagination
	totalPages := filtered.TotalPages(perPage)
	if page > totalPages && totalPages > 0 {
		page = totalPages
	}
	paginated := filtered.Paginate(page, perPage)

	// Calculate page range for pagination UI
	pageRange := calculatePageRange(page, totalPages)

	// Calculate page start/end for display
	pageStart := (page-1)*perPage + 1
	pageEnd := pageStart + paginated.Len() - 1
	if totalCount == 0 {
		pageStart = 0
		pageEnd = 0
	}

	appendRows := r.URL.Query().Get("append") == "true"

	partialData := map[string]interface{}{
		"Transactions":       paginated.Transactions,
		"Search":             search,
		"Category":           category,
		"Type":               txnType,
		"Sort":               sortField,
		"Order":              order,
		"Page":               page,
		"PerPage":            perPage,
		"TotalPages":         totalPages,
		"TotalCount":         totalCount,
		"TotalIncome":        totalIncome,
		"TotalExpenses":      totalExpenses,
		"NetAmount":          netAmount,
		"PageRange":          pageRange,
		"PageStart":          pageStart,
		"PageEnd":            pageEnd,
		"HashToExpense":      hashToExpense,
		"MajorExpenseFilter": majorExpenseID,
	}

	if renderer != nil {
		if appendRows {
			_ = renderer.RenderPartial(w, "transaction-rows", partialData)
		} else {
			_ = renderer.RenderPartial(w, "transactions-table", partialData)
		}
		// Always render summary stats for OOB update when filters change
		_ = renderer.RenderPartial(w, "summary-stats", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

func handleFileManager(w http.ResponseWriter, r *http.Request) {
	files, err := loader.GetFileInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	partialData := map[string]interface{}{
		"Files": files,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "file-manager", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

// importScanEntry is one CSV discovered in ImportDirectory. It carries the
// parsed transaction date range (derived through the same loader path as the
// data-directory file listing — no second CSV parser) and an `exists` flag
// set when a file of that name is already present in DataDirectory, so the UI
// can pre-disable it.
type importScanEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	MinDate string `json:"min_date"`
	MaxDate string `json:"max_date"`
	Exists  bool   `json:"exists"`
}

// handleImportScan lists *.csv files found directly inside ImportDirectory.
// It is read-only: it never creates, modifies, or deletes a file. The actual
// import (copy + optional source delete) is handleImport, POST
// /explorer/import.
//
// Exclusions, per the §3 spec:
//   - No recursion: only files directly in ImportDirectory are listed.
//   - Symlinks are neither followed nor listed. The date-range parse reuses
//     loader.GetFileInfo (via a throwaway DataLoader pointed at the import
//     dir), which reads through os.Stat — so symlinks would otherwise be
//     followed. We Lstat each candidate and drop any symlink before returning
//     it, regardless of what it points at.
//   - Non-CSV files are not listed (GetFileInfo already globs *.csv).
//
// A missing or unreadable ImportDirectory is not an error: the user may simply
// have no Downloads folder. The handler returns 200 with an empty list and a
// message the UI can show.
func handleImportScan(w http.ResponseWriter, r *http.Request) {
	entries, message := scanImportDirectory(cfg.ImportDirectory)

	partialData := map[string]interface{}{
		"ImportEntries": entries,
		"ImportMessage": message,
		"ImportPath":    cfg.ImportDirectory,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "import-scan", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

// scanImportDirectory enumerates importable CSVs in importDir. It is split
// from the handler so tests can drive it directly without an HTTP recorder.
//
// It builds a short-lived DataLoader whose CSVDirectory is the import dir and
// reuses GetFileInfo's existing scanCSVMetadata parse for the date range,
// rather than re-implementing CSV parsing here. Symlinks are filtered out
// after the fact with os.Lstat (GetFileInfo stats via os.Stat, which follows
// links); everything else in GetFileInfo's inclusion rule — glob *.csv, drop
// stat failures — is preserved.
//
// The exists flag is derived from the package-global loader, which is bound
// to cfg.DataDirectory — the same source the file-manager page uses.
func scanImportDirectory(importDir string) ([]importScanEntry, string) {
	if importDir == "" {
		return nil, "No import folder is configured."
	}
	if _, err := os.Stat(importDir); err != nil {
		// Missing or unreadable — not an error. The user may simply have no
		// Downloads folder.
		return nil, "Import folder not found: " + importDir
	}

	importLoader := dataloader.New(importDir, store)
	infos, err := importLoader.GetFileInfo()
	if err != nil {
		// A Glob error reads as "no files to import" for the caller's
		// purposes; GetFileInfo already drops per-file stat failures.
		return nil, "Could not read import folder: " + importDir
	}

	// Names already present in the data dir mark an import entry as existing.
	dataNames := make(map[string]bool)
	if files, err := loader.GetFileInfo(); err == nil {
		for _, f := range files {
			dataNames[f.Name] = true
		}
	}

	entries := make([]importScanEntry, 0, len(infos))
	for _, fi := range infos {
		// GetFileInfo follows symlinks via os.Stat. Re-stat without following
		// and drop any symlink, no matter what it targets.
		if li, err := os.Lstat(fi.Path); err == nil && li.Mode()&os.ModeSymlink != 0 {
			continue
		}
		entries = append(entries, importScanEntry{
			Name:    fi.Name,
			Size:    fi.Size,
			MinDate: fi.MinDate,
			MaxDate: fi.MaxDate,
			Exists:  dataNames[fi.Name],
		})
	}

	if len(entries) == 0 {
		return entries, "No CSV files found in import folder."
	}
	return entries, ""
}

// importOutcome is the per-file result of a folder import. It mirrors
// uploadOutcome's shape (P12's batch upload) so the two panels can speak the
// same vocabulary: "imported", "skipped", or "rejected", with a human reason.
// SourceDeleted records whether the original in ImportDirectory was removed,
// so the result panel can state that in text rather than leaving the user to
// infer it from the presence of a delete tick.
type importOutcome struct {
	Name          string `json:"name"`
	Status        string `json:"status"`
	Reason        string `json:"reason,omitempty"`
	SourceDeleted bool   `json:"source_deleted"`
}

// importDeps are the four filesystem/storage operations a single file import
// performs. Production code always uses defaultImportDeps; tests substitute
// one operation at a time to stage failures that real files cannot produce
// reliably — above all a readback that returns short bytes, which is the only
// thing standing between a truncated or encryption-failed write and an
// irreversible source delete.
type importDeps struct {
	readSource func(path string) ([]byte, error)
	write      func(path string, data []byte, perm os.FileMode) error
	readBack   func(path string) ([]byte, error)
	removeSrc  func(path string) error
}

// defaultImportDeps binds the real operations. The source lives outside the
// data directory, so it is read with os.ReadFile and removed with os.Remove;
// only the destination goes through store, so that encryption applies to what
// lands in the data directory and the readback decrypts symmetrically.
func defaultImportDeps() importDeps {
	return importDeps{
		readSource: os.ReadFile,
		write:      func(path string, data []byte, perm os.FileMode) error { return store.WriteFile(path, data, perm) },
		readBack:   func(path string) ([]byte, error) { return store.ReadFile(path) },
		removeSrc:  os.Remove,
	}
}

// handleImport copies selected CSVs out of ImportDirectory into the data
// directory and, when asked, deletes the originals.
//
// Wire format (pinned by the §3 design so independent implementations agree):
// form-encoded, with `name` repeated once per selected file — bare filenames
// as returned by the scan — and an optional `delete_source` whose value must
// be exactly the string "true" to enable deletion. Any other value, and the
// field's absence, mean keep the sources.
//
// A processed batch is a 200 even when individual files were skipped or
// rejected; the per-file outcomes travel in the body, matching P12's batch
// upload. Only a malformed request — no `name` fields at all — is a 400, and
// that path returns before touching the filesystem so it is provably inert.
func handleImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	// Read names from PostForm, not Form: the selection belongs in the request
	// body, and Form would also fold in URL query parameters.
	names := make([]string, 0, len(r.PostForm["name"]))
	for _, n := range r.PostForm["name"] {
		if strings.TrimSpace(n) != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		http.Error(w, "No files selected for import", http.StatusBadRequest)
		return
	}

	deleteSource := r.PostFormValue("delete_source") == "true"

	deps := defaultImportDeps()
	outcomes := make([]importOutcome, 0, len(names))
	for _, name := range names {
		outcomes = append(outcomes, importOneFile(name, deleteSource, deps))
	}

	partialData := map[string]interface{}{
		"Results":      outcomes,
		"DeleteSource": deleteSource,
		"ImportPath":   cfg.ImportDirectory,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "import-result", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

// importOneFile runs the six-step import for one selected name. It never
// returns an error: a failure is the caller's outcome for that file, and the
// rest of the batch continues.
//
// The source delete at the end is gated three ways, all of which must hold:
// the write succeeded, the readback matched the expected byte length, and the
// path resolved to a direct child of ImportDirectory. A file that was skipped
// or rejected returns before reaching it, so it is never deleted.
func importOneFile(name string, deleteSource bool, deps importDeps) importOutcome {
	// Step 1 — validate the name with no filesystem call at all. filepath.Base
	// strips every directory component, so a name that differs from its own
	// Base carried a separator and cannot be a name the scan returned. "." and
	// ".." are their own Base, so they are excluded by hand.
	if name == "" || name == "." || name == ".." ||
		name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return importOutcome{Name: name, Status: "rejected", Reason: "invalid filename"}
	}

	// The scan only ever offers *.csv; an explicitly posted non-CSV name is
	// rejected here, still before any filesystem access.
	if !strings.HasSuffix(strings.ToLower(name), ".csv") {
		return importOutcome{Name: name, Status: "rejected", Reason: "only CSV files can be imported"}
	}

	if cfg.ImportDirectory == "" {
		return importOutcome{Name: name, Status: "rejected", Reason: "no import folder is configured"}
	}

	// Step 2 — re-stat inside ImportDirectory and confirm a direct child.
	// Lstat, not Stat: a symlink is never followed, so a link planted in the
	// import folder cannot make an outside file look like a member of it — and
	// cannot get that outside file deleted.
	srcPath := filepath.Join(cfg.ImportDirectory, name)
	info, err := os.Lstat(srcPath)
	if err != nil {
		return importOutcome{Name: name, Status: "rejected", Reason: "not found in the import folder"}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return importOutcome{Name: name, Status: "rejected", Reason: "symlinks are not imported"}
	}
	if !info.Mode().IsRegular() {
		return importOutcome{Name: name, Status: "rejected", Reason: "not a regular file"}
	}
	if !isDirectChild(cfg.ImportDirectory, srcPath) {
		return importOutcome{Name: name, Status: "rejected", Reason: "not directly inside the import folder"}
	}

	// Step 3 — a name already in the data directory skips. No overwrite, no
	// auto-rename. A Stat that failed for a reason other than "absent" is not
	// read as absent: overwriting on a permission or I/O error would lose the
	// existing file, and a source delete on top of that would make it
	// unrecoverable.
	destPath := filepath.Join(cfg.DataDirectory, name)
	if _, err := store.Stat(destPath); err == nil {
		return importOutcome{Name: name, Status: "skipped", Reason: "already exists in the data folder"}
	} else if !errors.Is(err, os.ErrNotExist) {
		return importOutcome{Name: name, Status: "rejected", Reason: "could not check the destination"}
	}

	// Step 4 — read the source, then write through store so encryption applies.
	data, err := deps.readSource(srcPath)
	if err != nil {
		return importOutcome{Name: name, Status: "rejected", Reason: "could not read the source file"}
	}
	if err := deps.write(destPath, data, 0644); err != nil {
		return importOutcome{Name: name, Status: "rejected", Reason: "could not save the file"}
	}

	// Step 5 — read the destination back and confirm the expected byte length.
	// This is what makes "fully saved" mean something: without it a truncated
	// or encryption-failed write would still clear the way for step 6.
	back, err := deps.readBack(destPath)
	if err != nil {
		return importOutcome{Name: name, Status: "rejected", Reason: "saved file could not be read back"}
	}
	if len(back) != len(data) {
		return importOutcome{Name: name, Status: "rejected", Reason: "saved file failed verification"}
	}

	outcome := importOutcome{Name: name, Status: "imported"}
	if !deleteSource {
		log.Printf("Imported file: %s", name)
		return outcome
	}

	// Step 6 — all three guards have held: the write succeeded, the readback
	// matched, and srcPath was confirmed a direct, non-symlink child of
	// ImportDirectory. Only now is the original removed.
	if err := deps.removeSrc(srcPath); err != nil {
		outcome.Reason = "imported, but the source file could not be deleted"
		log.Printf("Imported file: %s (source delete failed: %v)", name, err)
		return outcome
	}
	outcome.SourceDeleted = true
	outcome.Reason = "source file deleted"
	log.Printf("Imported file: %s (source deleted)", name)
	return outcome
}

// isDirectChild reports whether path names an entry sitting directly inside
// dir. Both sides go through EvalSymlinks so that a symlinked import folder
// (a symlinked ~/Downloads, /tmp on systems where it is a link) still matches,
// while anything reached by leaving the folder does not. Callers Lstat path
// first and reject symlinks, so EvalSymlinks here only resolves path's parent
// components.
func isDirectChild(dir, path string) bool {
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	return filepath.Dir(resolvedPath) == resolvedDir
}

func HandleFileManagerPage(w http.ResponseWriter, r *http.Request) {
	isLocked := store.IsEncrypted() && !store.IsUnlocked()

	data := map[string]interface{}{
		"Title":           "File Manager",
		"ActiveTab":       "filemanager",
		"IsEncrypted":     store.IsEncrypted(),
		"IsLocked":        isLocked,
		"ImportDirectory": cfg.ImportDirectory,
	}

	// Add auth method info if encrypted
	if store.IsEncrypted() {
		if config := store.GetConfig(); config != nil {
			data["AuthMethod"] = string(config.Method)
		}
	}

	// Only get file info if unlocked
	if !isLocked {
		files, err := loader.GetFileInfo()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data["Files"] = files
	}

	templates.AttachDuplicateCount(data, loader)
	_ = renderer.Render(w, "base", data)
}

func handleFileToggle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filename := r.FormValue("file")
	enabled := r.FormValue("enabled") == "true"

	// Get current file info
	files, err := loader.GetFileInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Build enabled files list
	var enabledFiles []string
	for _, f := range files {
		if f.Name == filename {
			if enabled {
				enabledFiles = append(enabledFiles, f.Name)
			}
		} else if f.Enabled {
			enabledFiles = append(enabledFiles, f.Name)
		}
	}

	// Update loader
	loader.SetEnabledFiles(enabledFiles)

	// Return updated file list.
	files, _ = loader.GetFileInfo()
	partialData := map[string]interface{}{
		"Files": files,
	}

	// Render the same `filemanager-file-list`
	// partial the File Manager page uses on initial render, so the swapped-in
	// table carries the sortable headers (data-sort-btn, scope="col") and the
	// per-row data-* attributes the client-side sort JS re-wires on
	// htmx:afterSettle. The `file-list` template (explorer.html) is the old
	// non-sortable markup; rendering it here was the P13 sort-survival defect.
	if renderer != nil {
		_ = renderer.RenderPartial(w, "filemanager-file-list", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

// maxUploadBatchBytes caps the total size of a multipart upload request body.
// Bank-export CSVs are small (typically well under a megabyte each); 50MB
// comfortably covers a large batch (dozens of files) while still bounding
// the resources a single request can consume. Before this cap was added,
// ParseMultipartForm's maxMemory argument only limited how much of the body
// was buffered in memory before spilling to temp files on disk — it did not
// limit the total request size at all.
const maxUploadBatchBytes = 50 << 20

// uploadOutcome is the per-file result of a batch upload, surfaced to the
// caller alongside the refreshed file list.
type uploadOutcome struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "saved", "skipped", or "rejected"
	Reason string `json:"reason,omitempty"`
}

func handleFileUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBatchBytes)

	// Parse multipart form. The 10MB argument only sets the in-memory spill
	// threshold (larger parts are buffered to temp files); the real size
	// limit is the MaxBytesReader wrap above.
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	var headers []*multipart.FileHeader
	if r.MultipartForm != nil {
		headers = r.MultipartForm.File["file"]
	}
	if len(headers) == 0 {
		http.Error(w, "Error reading file", http.StatusBadRequest)
		return
	}

	outcomes := make([]uploadOutcome, 0, len(headers))
	for _, header := range headers {
		outcomes = append(outcomes, uploadOneFile(header))
	}

	// Return updated file list plus the per-file outcomes of this batch.
	// `filemanager-file-list` ignores .Results and only renders .Files, which
	// is exactly what the #file-list swap target needs; the outcomes are kept
	// for the JSON fallback. See handleFileToggle for why this renders
	// filemanager-file-list rather than the legacy file-list partial.
	files, _ := loader.GetFileInfo()
	partialData := map[string]interface{}{
		"Files":   files,
		"Results": outcomes,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "filemanager-file-list", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

// uploadOneFile validates and saves a single file from a batch. It never
// aborts the caller's loop: any failure is reported as part of the returned
// outcome rather than an HTTP error.
func uploadOneFile(header *multipart.FileHeader) uploadOutcome {
	rawName := header.Filename

	filename, err := sanitizeUploadFilename(rawName)
	if err != nil {
		return uploadOutcome{Name: rawName, Status: "rejected", Reason: "invalid filename"}
	}

	if !strings.HasSuffix(strings.ToLower(filename), ".csv") {
		return uploadOutcome{Name: filename, Status: "rejected", Reason: "only CSV files are allowed"}
	}

	destPath := filepath.Join(cfg.DataDirectory, filename)

	file, err := header.Open()
	if err != nil {
		return uploadOutcome{Name: filename, Status: "rejected", Reason: "error reading file"}
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		return uploadOutcome{Name: filename, Status: "rejected", Reason: "error reading file"}
	}

	// Collisions skip: never overwrite, never auto-rename. The existence test
	// is the create itself. A separate Stat first would let two uploads of the
	// same name both pass it and then overwrite each other, and would read a
	// permission or I/O error from Stat as "absent" and overwrite anyway.
	// Writes via storage, so encryption still applies when enabled.
	if err := store.CreateExclusive(destPath, data, 0644); err != nil {
		if errors.Is(err, os.ErrExist) {
			return uploadOutcome{Name: filename, Status: "skipped", Reason: "already exists"}
		}
		return uploadOutcome{Name: filename, Status: "rejected", Reason: "error saving file"}
	}

	log.Printf("Uploaded file: %s", filename)
	return uploadOutcome{Name: filename, Status: "saved"}
}

func sanitizeUploadFilename(filename string) (string, error) {
	filename = filepath.Base(strings.ReplaceAll(filename, `\`, "/"))
	if filename == "." || filename == "" || strings.Contains(filename, "..") {
		return "", os.ErrInvalid
	}
	return filename, nil
}

func handleFileDelete(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")

	// URL-decode the filename (handles %20 for spaces, etc.)
	decodedFilename, err := url.PathUnescape(filename)
	if err != nil {
		http.Error(w, "Invalid filename encoding", http.StatusBadRequest)
		return
	}
	filename = decodedFilename

	// Validate filename (reuse sanitizer to prevent path traversal)
	filename, err = sanitizeUploadFilename(filename)
	if err != nil {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(cfg.DataDirectory, filename)

	// Check if file exists
	if _, err := store.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Delete file
	if err := store.Remove(filePath); err != nil {
		http.Error(w, "Error deleting file", http.StatusInternalServerError)
		return
	}

	log.Printf("Deleted file: %s", filename)

	// Return updated file list. See handleFileToggle for why this renders
	// filemanager-file-list rather than the legacy file-list partial.
	files, _ := loader.GetFileInfo()
	partialData := map[string]interface{}{
		"Files": files,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "filemanager-file-list", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

func handleAlias(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	hash := r.FormValue("hash")
	displayName := strings.TrimSpace(r.FormValue("display_name"))

	if hash == "" || len(hash) > 128 {
		http.Error(w, "invalid hash", http.StatusBadRequest)
		return
	}

	if len(displayName) > 200 {
		http.Error(w, "display name too long", http.StatusBadRequest)
		return
	}

	if err := loader.SaveAlias(hash, displayName); err != nil {
		log.Printf("Error saving alias: %v", err)
		http.Error(w, "Error saving alias", http.StatusInternalServerError)
		return
	}

	// Return just the updated description cell content
	w.WriteHeader(http.StatusNoContent)
}

// sortTransactions sorts the transaction set by the specified field
func sortTransactions(ts *models.TransactionSet, field, order string) *models.TransactionSet {
	sorted := ts.Copy()

	switch field {
	case "date":
		sort.Slice(sorted.Transactions, func(i, j int) bool {
			if order == "asc" {
				return sorted.Transactions[i].Date.Before(sorted.Transactions[j].Date)
			}
			return sorted.Transactions[i].Date.After(sorted.Transactions[j].Date)
		})
	case "description":
		sort.Slice(sorted.Transactions, func(i, j int) bool {
			if order == "asc" {
				return strings.ToLower(sorted.Transactions[i].Label()) < strings.ToLower(sorted.Transactions[j].Label())
			}
			return strings.ToLower(sorted.Transactions[i].Label()) > strings.ToLower(sorted.Transactions[j].Label())
		})
	case "category":
		sort.Slice(sorted.Transactions, func(i, j int) bool {
			catI := sorted.Transactions[i].Category
			catJ := sorted.Transactions[j].Category
			if catI == "" {
				catI = "Uncategorized"
			}
			if catJ == "" {
				catJ = "Uncategorized"
			}
			if order == "asc" {
				return strings.ToLower(catI) < strings.ToLower(catJ)
			}
			return strings.ToLower(catI) > strings.ToLower(catJ)
		})
	case "amount":
		sort.Slice(sorted.Transactions, func(i, j int) bool {
			if order == "asc" {
				return sorted.Transactions[i].Amount < sorted.Transactions[j].Amount
			}
			return sorted.Transactions[i].Amount > sorted.Transactions[j].Amount
		})
	case "type":
		sort.Slice(sorted.Transactions, func(i, j int) bool {
			if order == "asc" {
				return sorted.Transactions[i].TransactionType < sorted.Transactions[j].TransactionType
			}
			return sorted.Transactions[i].TransactionType > sorted.Transactions[j].TransactionType
		})
	case "source":
		sort.Slice(sorted.Transactions, func(i, j int) bool {
			if order == "asc" {
				return strings.ToLower(sorted.Transactions[i].SourceFile) < strings.ToLower(sorted.Transactions[j].SourceFile)
			}
			return strings.ToLower(sorted.Transactions[i].SourceFile) > strings.ToLower(sorted.Transactions[j].SourceFile)
		})
	case "majorExpense":
		sort.Slice(sorted.Transactions, func(i, j int) bool {
			// Empty MajorExpenseName means the txn matched no rule — push those
			// to the bottom in both directions so rule-grouped rows stay together.
			a := strings.ToLower(sorted.Transactions[i].MajorExpenseName)
			b := strings.ToLower(sorted.Transactions[j].MajorExpenseName)
			if a == "" && b != "" {
				return false
			}
			if a != "" && b == "" {
				return true
			}
			if order == "asc" {
				return a < b
			}
			return a > b
		})
	default:
		// Default to date descending
		sort.Slice(sorted.Transactions, func(i, j int) bool {
			return sorted.Transactions[i].Date.After(sorted.Transactions[j].Date)
		})
	}

	return sorted
}

// calculatePageRange returns a slice of page numbers to display in pagination
func calculatePageRange(currentPage, totalPages int) []int {
	if totalPages <= 7 {
		result := make([]int, totalPages)
		for i := range result {
			result[i] = i + 1
		}
		return result
	}

	// Show pages around current page
	var pages []int
	start := currentPage - 2
	end := currentPage + 2

	if start < 1 {
		start = 1
		end = 5
	}
	if end > totalPages {
		end = totalPages
		start = totalPages - 4
		if start < 1 {
			start = 1
		}
	}

	for i := start; i <= end; i++ {
		pages = append(pages, i)
	}

	return pages
}
