package explorer

import (
	"encoding/json"
	"io"
	"log"
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

// RegisterRoutes registers all explorer routes
func RegisterRoutes(r chi.Router) {
	r.Get("/explorer", handleExplorer)
	r.Get("/explorer/transactions", handleTransactionsPartial)
	r.Get("/explorer/files", handleFileManager)
	r.Post("/explorer/files/toggle", handleFileToggle)
	r.Post("/explorer/upload", handleFileUpload)
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
		}
	}

	// Calculate totals before pagination
	totalCount := filtered.Len()
	totalIncome := filtered.FilterByType(models.Income).SumAmount()
	totalExpenses := filtered.FilterByType(models.Outflow).SumAbsAmount()
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
	pageStart := (page - 1) * perPage + 1
	pageEnd := pageStart + paginated.Len() - 1
	if totalCount == 0 {
		pageStart = 0
		pageEnd = 0
	}

	pageData := map[string]interface{}{
		"Title":         "Data Explorer",
		"ActiveTab":     "explorer",
		"Transactions":  paginated.Transactions,
		"Categories":    data.Categories(),
		"Search":        search,
		"Category":      category,
		"Type":          txnType,
		"StartDate":     startDate.Format("2006-01-02"),
		"EndDate":       endDate.Format("2006-01-02"),
		"MinDate":       minDate.Format("2006-01-02"),
		"MaxDate":       maxDate.Format("2006-01-02"),
		"Sort":          sortField,
		"Order":         order,
		"Page":          page,
		"PerPage":       perPage,
		"TotalPages":    totalPages,
		"TotalCount":    totalCount,
		"TotalIncome":   totalIncome,
		"TotalExpenses": totalExpenses,
		"NetAmount":     netAmount,
		"PageRange":     pageRange,
		"PageStart":     pageStart,
		"PageEnd":       pageEnd,
	}

	if renderer != nil {
		renderer.Render(w, "base", pageData)
	} else {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Data Explorer</h1><p>Templates not loaded.</p></body></html>"))
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
		}
	}

	// Calculate totals before pagination
	totalCount := filtered.Len()
	totalIncome := filtered.FilterByType(models.Income).SumAmount()
	totalExpenses := filtered.FilterByType(models.Outflow).SumAbsAmount()
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
	pageStart := (page - 1) * perPage + 1
	pageEnd := pageStart + paginated.Len() - 1
	if totalCount == 0 {
		pageStart = 0
		pageEnd = 0
	}

	appendRows := r.URL.Query().Get("append") == "true"

	partialData := map[string]interface{}{
		"Transactions":  paginated.Transactions,
		"Search":        search,
		"Category":      category,
		"Type":          txnType,
		"Sort":          sortField,
		"Order":         order,
		"Page":          page,
		"PerPage":       perPage,
		"TotalPages":    totalPages,
		"TotalCount":    totalCount,
		"TotalIncome":   totalIncome,
		"TotalExpenses": totalExpenses,
		"NetAmount":     netAmount,
		"PageRange":     pageRange,
		"PageStart":     pageStart,
		"PageEnd":       pageEnd,
	}

	if renderer != nil {
		if appendRows {
			renderer.RenderPartial(w, "transaction-rows", partialData)
		} else {
			renderer.RenderPartial(w, "transactions-table", partialData)
		}
		// Always render summary stats for OOB update when filters change
		renderer.RenderPartial(w, "summary-stats", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
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
		renderer.RenderPartial(w, "file-manager", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func HandleFileManagerPage(w http.ResponseWriter, r *http.Request) {
	files, err := loader.GetFileInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Title":     "File Manager",
		"ActiveTab": "filemanager",
		"Files":     files,
	}

	renderer.Render(w, "base", data)
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

	// Return updated file list
	files, _ = loader.GetFileInfo()
	partialData := map[string]interface{}{
		"Files": files,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "file-list", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleFileUpload(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (max 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error reading file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".csv") {
		http.Error(w, "Only CSV files are allowed", http.StatusBadRequest)
		return
	}

	// Read file content
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}

	// Write via storage (handles encryption if enabled)
	destPath := filepath.Join(cfg.DataDirectory, header.Filename)
	if err := store.WriteFile(destPath, data, 0644); err != nil {
		http.Error(w, "Error saving file", http.StatusInternalServerError)
		return
	}

	log.Printf("Uploaded file: %s", header.Filename)

	// Return updated file list
	files, _ := loader.GetFileInfo()
	partialData := map[string]interface{}{
		"Files": files,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "file-list", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
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

	// Validate filename (prevent path traversal)
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || strings.Contains(filename, "..") {
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

	// Return updated file list
	files, _ := loader.GetFileInfo()
	partialData := map[string]interface{}{
		"Files": files,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "file-list", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
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
				return strings.ToLower(sorted.Transactions[i].Description) < strings.ToLower(sorted.Transactions[j].Description)
			}
			return strings.ToLower(sorted.Transactions[i].Description) > strings.ToLower(sorted.Transactions[j].Description)
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
