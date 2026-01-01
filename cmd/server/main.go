package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"budget2/internal/config"
	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/retirement"
	"budget2/internal/templates"
)

var (
	cfg             *config.Config
	loader          *dataloader.DataLoader
	renderer        *templates.Renderer
	retirementMgr   *retirement.SettingsManager
)

func main() {
	// Load configuration
	cfg = config.Load()
	log.Printf("Starting Budget Dashboard on %s", cfg.ListenAddr)
	log.Printf("Data directory: %s", cfg.DataDirectory)

	// Initialize data loader
	loader = dataloader.New(cfg.DataDirectory)

	// Initialize template renderer
	var err error
	renderer, err = templates.New(cfg.TemplatesDirectory, true) // force debug for template hot reload
	if err != nil {
		log.Fatalf("FATAL: Template validation failed: %v", err)
	}

	// Initialize retirement settings manager
	settingsDir := filepath.Join(cfg.DataDirectory, "settings")
	retirementMgr = retirement.NewSettingsManager(settingsDir)

	// Setup router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	// Static files
	fileServer := http.FileServer(http.Dir(cfg.StaticDirectory))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Routes
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusTemporaryRedirect)
	})

	// Dashboard routes
	r.Get("/dashboard", handleDashboard)
	r.Get("/dashboard/kpis", handleKPIsPartial)
	r.Get("/dashboard/charts/data/{chartType}", handleChartData)
	r.Get("/dashboard/alerts", handleAlertsPartial)
	r.Get("/dashboard/category/{category}", handleCategoryDrilldown)

	// Explorer routes
	r.Get("/explorer", handleExplorer)
	r.Get("/explorer/transactions", handleTransactionsPartial)
	r.Get("/explorer/files", handleFileManager)
	r.Post("/explorer/files/toggle", handleFileToggle)
	r.Post("/explorer/upload", handleFileUpload)
	r.Delete("/explorer/files/{filename}", handleFileDelete)

	// What-if routes
	r.Get("/whatif", handleWhatIf)
	r.Post("/whatif/calculate", handleWhatIfCalculate)
	r.Post("/whatif/settings", handleWhatIfSettings)
	r.Post("/whatif/income", handleWhatIfAddIncome)
	r.Delete("/whatif/income/{id}", handleWhatIfDeleteIncome)
	r.Post("/whatif/expense", handleWhatIfAddExpense)
	r.Delete("/whatif/expense/{id}", handleWhatIfDeleteExpense)
	r.Get("/whatif/chart/projection", handleWhatIfProjectionChart)
	r.Post("/whatif/sync", handleWhatIfSync)

	// Insights routes
	r.Get("/insights", handleInsights)
	r.Get("/insights/recurring", handleRecurringPartial)
	r.Get("/insights/trends", handleTrendsPartial)
	r.Get("/insights/trends/chart", handleTrendsChartData)
	r.Get("/insights/velocity", handleVelocityPartial)
	r.Get("/insights/income", handleIncomePartial)

	// API routes
	r.Get("/api/health", handleHealth)
	r.Get("/killme", handleKillServer)

	// Start server
	log.Printf("Server starting on %s", cfg.ListenAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, r))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleKillServer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("Server shutting down...\n"))
	log.Println("Received /killme request, shutting down")
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.Exit(0)
	}()
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		log.Printf("Error loading data: %v", err)
		http.Error(w, "Error loading data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse date range from query params
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	comparison := r.URL.Query().Get("comparison")

	minDate := data.MinDate()
	maxDate := data.MaxDate()

	var startDate, endDate time.Time
	if startStr != "" {
		startDate, _ = time.Parse("2006-01-02", startStr)
	} else {
		// Default to YTD
		startDate = time.Date(time.Now().Year(), 1, 1, 0, 0, 0, 0, time.Local)
		// If YTD range starts after our data ends, default to all-time
		if !maxDate.IsZero() && startDate.After(maxDate) {
			startDate = minDate
		} else if startDate.Before(minDate) {
			startDate = minDate
		}
	}
	if endStr != "" {
		endDate, _ = time.Parse("2006-01-02", endStr)
	} else {
		endDate = maxDate
	}

	filtered := data.FilterByDateRange(startDate, endDate)
	metrics := calculateMetrics(filtered)

	// Calculate period comparison if requested
	var periodComparison *models.PeriodComparison
	if comparison != "" {
		periodComparison = calculateComparison(data, startDate, endDate, comparison)
	}

	pageData := map[string]interface{}{
		"Title":            "Dashboard",
		"ActiveTab":        "dashboard",
		"Metrics":          metrics,
		"PeriodComparison": periodComparison,
		"StartDate":        startDate.Format("2006-01-02"),
		"EndDate":          endDate.Format("2006-01-02"),
		"MinDate":          minDate.Format("2006-01-02"),
		"MaxDate":          maxDate.Format("2006-01-02"),
		"Comparison":       comparison,
	}

	if renderer != nil {
		renderer.Render(w, "base", pageData)
	} else {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Dashboard</h1><p>Templates not loaded. Check configuration.</p></body></html>"))
	}
}

func handleKPIsPartial(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	comparison := r.URL.Query().Get("comparison")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MinDate()
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	filtered := data.FilterByDateRange(startDate, endDate)
	metrics := calculateMetrics(filtered)

	var periodComparison *models.PeriodComparison
	if comparison != "" {
		periodComparison = calculateComparison(data, startDate, endDate, comparison)
	}

	partialData := map[string]interface{}{
		"Metrics":          metrics,
		"PeriodComparison": periodComparison,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "kpis", partialData)
	} else {
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleChartData(w http.ResponseWriter, r *http.Request) {
	chartType := chi.URLParam(r, "chartType")

	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MinDate()
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	filtered := data.FilterByDateRange(startDate, endDate)

	var chartData interface{}

	switch chartType {
	case "monthly":
		chartData = buildMonthlyChartData(filtered)
	case "category":
		chartData = buildCategoryChartData(filtered)
	case "cashflow":
		chartData = buildCashflowChartData(filtered)
	case "merchants":
		chartData = buildMerchantsChartData(filtered)
	case "weekly":
		chartData = buildWeeklyPatternChartData(filtered)
	case "cumulative":
		chartData = buildCumulativeChartData(filtered)
	default:
		http.Error(w, "Unknown chart type", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chartData)
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
	pageStart := (page-1)*perPage + 1
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
	pageStart := (page-1)*perPage + 1
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
 			// Also render summary stats for OOB update
 			renderer.RenderPartial(w, "summary-stats", partialData)
 		} else {
 			renderer.RenderPartial(w, "transactions-table", partialData)
 		}
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

	// Create destination path
	destPath := filepath.Join(cfg.DataDirectory, header.Filename)

	// Create destination file
	dst, err := os.Create(destPath)
	if err != nil {
		http.Error(w, "Error saving file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// Copy file
	if _, err := io.Copy(dst, file); err != nil {
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

	// Validate filename (prevent path traversal)
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || strings.Contains(filename, "..") {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(cfg.DataDirectory, filename)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Delete file
	if err := os.Remove(filePath); err != nil {
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

func handleWhatIf(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		log.Printf("Error loading what-if settings: %v", err)
		settings = models.DefaultWhatIfSettings()
	}

	// Run full analysis
	calc := retirement.NewCalculator(settings)
	analysis := calc.RunFullAnalysis()

	pageData := map[string]interface{}{
		"Title":     "What-If Analysis",
		"ActiveTab": "whatif",
		"Settings":  settings,
		"Analysis":  analysis,
	}

	if renderer != nil {
		renderer.Render(w, "base", pageData)
	} else {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>What-If Analysis</h1><p>Templates not loaded.</p></body></html>"))
	}
}

func handleWhatIfCalculate(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	calc := retirement.NewCalculator(settings)
	analysis := calc.RunFullAnalysis()

	partialData := map[string]interface{}{
		"Settings": settings,
		"Analysis": analysis,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleWhatIfSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Parse form values
	updates := make(map[string]interface{})

	if v := r.FormValue("portfolio_value"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["portfolio_value"] = f
		}
	}
	if v := r.FormValue("monthly_living_expenses"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["monthly_living_expenses"] = f
		}
	}
	if v := r.FormValue("monthly_healthcare"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["monthly_healthcare"] = f
		}
	}
	if v := r.FormValue("healthcare_start_years"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			updates["healthcare_start_years"] = i
		}
	}
	if v := r.FormValue("max_withdrawal_rate"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["max_withdrawal_rate"] = f
		}
	}
	if v := r.FormValue("inflation_rate"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["inflation_rate"] = f
		}
	}
	if v := r.FormValue("healthcare_inflation"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["healthcare_inflation"] = f
		}
	}
	if v := r.FormValue("spending_decline_rate"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["spending_decline_rate"] = f
		}
	}
	if v := r.FormValue("investment_return"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["investment_return"] = f
		}
	}
	if v := r.FormValue("discount_rate"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["discount_rate"] = f
		}
	}
	if v := r.FormValue("projection_years"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			updates["projection_years"] = i
		}
	}

	settings, err := retirementMgr.UpdateSettings(updates)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	calc := retirement.NewCalculator(settings)
	analysis := calc.RunFullAnalysis()

	partialData := map[string]interface{}{
		"Settings": settings,
		"Analysis": analysis,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleWhatIfAddIncome(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	amount, _ := strconv.ParseFloat(r.FormValue("amount"), 64)
	startYear, _ := strconv.Atoi(r.FormValue("start_year"))
	endYear, _ := strconv.Atoi(r.FormValue("end_year"))
	cola := r.FormValue("cola") == "on" || r.FormValue("cola") == "true"

	source := models.IncomeSource{
		ID:         uuid.New().String(),
		Name:       name,
		Amount:     amount,
		Type:       models.IncomeFixed,
		StartMonth: startYear * 12,
		COLARate:   0,
	}

	if cola {
		source.COLARate = 0.02 // 2% COLA
	}

	if endYear > 0 {
		endMonth := endYear * 12
		source.EndMonth = &endMonth
	}

	settings, err := retirementMgr.AddIncomeSource(source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	calc := retirement.NewCalculator(settings)
	analysis := calc.RunFullAnalysis()

	partialData := map[string]interface{}{
		"Settings": settings,
		"Analysis": analysis,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleWhatIfDeleteIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RemoveIncomeSource(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	calc := retirement.NewCalculator(settings)
	analysis := calc.RunFullAnalysis()

	partialData := map[string]interface{}{
		"Settings": settings,
		"Analysis": analysis,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleWhatIfAddExpense(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	amount, _ := strconv.ParseFloat(r.FormValue("amount"), 64)
	startYear, _ := strconv.Atoi(r.FormValue("start_year"))
	endYear, _ := strconv.Atoi(r.FormValue("end_year"))
	inflation := r.FormValue("inflation") == "on" || r.FormValue("inflation") == "true"

	source := models.ExpenseSource{
		ID:        uuid.New().String(),
		Name:      name,
		Amount:    amount,
		StartYear: startYear,
		EndYear:   endYear,
		Inflation: inflation,
	}

	settings, err := retirementMgr.AddExpenseSource(source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	calc := retirement.NewCalculator(settings)
	analysis := calc.RunFullAnalysis()

	partialData := map[string]interface{}{
		"Settings": settings,
		"Analysis": analysis,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleWhatIfDeleteExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RemoveExpenseSource(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	calc := retirement.NewCalculator(settings)
	analysis := calc.RunFullAnalysis()

	partialData := map[string]interface{}{
		"Settings": settings,
		"Analysis": analysis,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleWhatIfProjectionChart(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	calc := retirement.NewCalculator(settings)
	projection := calc.RunProjection()

	// Build chart data
	var years []float64
	var balances []float64

	for _, m := range projection.Months {
		years = append(years, m.Year)
		balances = append(balances, m.PortfolioBalance)
	}

	// Determine color based on survival
	fillColor := "rgba(34, 197, 94, 0.3)" // Green
	lineColor := "#22c55e"
	if !projection.Survives {
		fillColor = "rgba(239, 68, 68, 0.3)" // Red
		lineColor = "#ef4444"
	}

	chartData := map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type":      "scatter",
				"mode":      "lines",
				"name":      "Portfolio Balance",
				"x":         years,
				"y":         balances,
				"fill":      "tozeroy",
				"fillcolor": fillColor,
				"line": map[string]interface{}{
					"color": lineColor,
					"width": 2,
				},
			},
		},
		"layout": map[string]interface{}{
			"title": "Portfolio Projection",
			"xaxis": map[string]interface{}{
				"title": "Years",
			},
			"yaxis": map[string]interface{}{
				"title": "Balance ($)",
				"tickformat": "$,.0f",
			},
			"showlegend": false,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chartData)
}

func handleWhatIfSync(w http.ResponseWriter, r *http.Request) {
	// Sync settings from dashboard data
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Calculate average monthly expenses from last 12 months
	now := time.Now()
	yearAgo := now.AddDate(-1, 0, 0)
	filtered := data.FilterByDateRange(yearAgo, now)
	outflows := filtered.FilterByType(models.Outflow)

	totalExpenses := outflows.SumAbsAmount()
	months := 12.0
	if filtered.MinDate().After(yearAgo) {
		months = now.Sub(filtered.MinDate()).Hours() / 24 / 30
		if months < 1 {
			months = 1
		}
	}
	avgMonthlyExpenses := totalExpenses / months

	// Update settings
	updates := map[string]interface{}{
		"monthly_living_expenses": avgMonthlyExpenses,
	}

	settings, err := retirementMgr.UpdateSettings(updates)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	calc := retirement.NewCalculator(settings)
	analysis := calc.RunFullAnalysis()

	partialData := map[string]interface{}{
		"Settings":     settings,
		"Analysis":     analysis,
		"SyncedAmount": avgMonthlyExpenses,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleInsights(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		log.Printf("Error loading data: %v", err)
		http.Error(w, "Error loading data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse date range from query params
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	minDate := data.MinDate()
	maxDate := data.MaxDate()

	var startDate, endDate time.Time
	if startStr != "" {
		startDate, _ = time.Parse("2006-01-02", startStr)
	} else {
		// Default to last 12 months
		startDate = maxDate.AddDate(0, -12, 0)
		if startDate.Before(minDate) {
			startDate = minDate
		}
	}
	if endStr != "" {
		endDate, _ = time.Parse("2006-01-02", endStr)
	} else {
		endDate = maxDate
	}

	filtered := data.FilterByDateRange(startDate, endDate)

	// Calculate insights
	insights := calculateInsights(data, filtered, startDate, endDate)

	pageData := map[string]interface{}{
		"Title":     "Insights",
		"ActiveTab": "insights",
		"Insights":  insights,
		"StartDate": startDate.Format("2006-01-02"),
		"EndDate":   endDate.Format("2006-01-02"),
		"MinDate":   minDate.Format("2006-01-02"),
		"MaxDate":   maxDate.Format("2006-01-02"),
	}

	if renderer != nil {
		renderer.Render(w, "base", pageData)
	} else {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Insights</h1><p>Coming soon...</p></body></html>"))
	}
}

func handleAlertsPartial(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MinDate()
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	filtered := data.FilterByDateRange(startDate, endDate)
	alerts := detectAlerts(filtered)

	partialData := map[string]interface{}{
		"Alerts": alerts,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "alerts", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleCategoryDrilldown(w http.ResponseWriter, r *http.Request) {
	category := chi.URLParam(r, "category")

	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MinDate()
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	filtered := data.FilterByDateRange(startDate, endDate)
	outflows := filtered.FilterByType(models.Outflow)
	categoryTxns := outflows.FilterByCategory(category).SortByDateDesc()

	// Calculate category stats
	total := categoryTxns.SumAbsAmount()
	count := categoryTxns.Len()
	var avgAmount float64
	if count > 0 {
		avgAmount = total / float64(count)
	}

	partialData := map[string]interface{}{
		"Category":     category,
		"Transactions": categoryTxns.Transactions,
		"Total":        total,
		"Count":        count,
		"AvgAmount":    avgAmount,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "category-drilldown", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

// detectAlerts finds unusual spending patterns
func detectAlerts(ts *models.TransactionSet) []models.SpendingAlert {
	var alerts []models.SpendingAlert

	outflows := ts.FilterByType(models.Outflow)
	if outflows.Len() == 0 {
		return alerts
	}

	// Group by date to find unusual days
	daily := outflows.GroupByDate()

	// Calculate mean and std dev of daily spending
	var dailyTotals []float64
	var sum, sumSq float64

	for _, dayTxns := range daily {
		total := dayTxns.SumAbsAmount()
		dailyTotals = append(dailyTotals, total)
		sum += total
		sumSq += total * total
	}

	n := float64(len(dailyTotals))
	if n < 7 { // Need at least a week of data
		return alerts
	}

	mean := sum / n
	variance := (sumSq / n) - (mean * mean)
	stdDev := math.Sqrt(variance)
	threshold := mean + 2*stdDev

	// Find unusual days (more than 2 standard deviations above mean)
	for dateStr, dayTxns := range daily {
		total := dayTxns.SumAbsAmount()
		if total > threshold && total > mean*1.5 { // Must be 50% above mean too
			date, _ := time.Parse("2006-01-02", dateStr)
			alerts = append(alerts, models.SpendingAlert{
				Type:     "unusual_day",
				Severity: "warning",
				Title:    "High Spending Day",
				Message:  fmt.Sprintf("$%.0f spent on %s (%.0f%% above average)", total, date.Format("Jan 2"), ((total-mean)/mean)*100),
				Date:     &date,
				Amount:   total,
			})
		}
	}

	// Find large individual transactions (top 5% by amount)
	sortedTxns := make([]models.Transaction, len(outflows.Transactions))
	copy(sortedTxns, outflows.Transactions)
	sort.Slice(sortedTxns, func(i, j int) bool {
		return math.Abs(sortedTxns[i].Amount) > math.Abs(sortedTxns[j].Amount)
	})

	// Take top 3 largest transactions if they're significant
	for i := 0; i < 3 && i < len(sortedTxns); i++ {
		t := sortedTxns[i]
		amt := math.Abs(t.Amount)
		if amt > mean*3 { // Must be 3x average daily spending
			date := t.Date
			alerts = append(alerts, models.SpendingAlert{
				Type:     "large_transaction",
				Severity: "info",
				Title:    "Large Transaction",
				Message:  fmt.Sprintf("$%.0f at %s", amt, t.Description),
				Date:     &date,
				Amount:   amt,
			})
		}
	}

	// Sort alerts by date (most recent first)
	sort.Slice(alerts, func(i, j int) bool {
		if alerts[i].Date == nil || alerts[j].Date == nil {
			return false
		}
		return alerts[i].Date.After(*alerts[j].Date)
	})

	// Limit to 5 alerts
	if len(alerts) > 5 {
		alerts = alerts[:5]
	}

	return alerts
}

// Helper functions

func calculateMetrics(ts *models.TransactionSet) *models.DashboardMetrics {
	income := ts.FilterByType(models.Income)
	outflows := ts.FilterByType(models.Outflow)

	totalIncome := income.SumAmount()
	totalExpenses := outflows.SumAbsAmount()
	netSavings := totalIncome - totalExpenses

	var savingsRate float64
	if totalIncome > 0 {
		savingsRate = (netSavings / totalIncome) * 100
	}

	// Calculate monthly trends
	var incomeTrend, expensesTrend, savingsTrend []float64
	var trendLabels []string

	monthlyIncome := income.GroupByMonth()
	monthlyOutflows := outflows.GroupByMonth()

	// Get sorted months
	monthSet := make(map[string]bool)
	for m := range monthlyIncome {
		monthSet[m] = true
	}
	for m := range monthlyOutflows {
		monthSet[m] = true
	}

	var months []string
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)

	// Take last 6 months
	if len(months) > 6 {
		months = months[len(months)-6:]
	}

	for _, m := range months {
		incAmt := 0.0
		if inc, ok := monthlyIncome[m]; ok {
			incAmt = inc.SumAmount()
		}

		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = exp.SumAbsAmount()
		}

		incomeTrend = append(incomeTrend, incAmt)
		expensesTrend = append(expensesTrend, expAmt)
		savingsTrend = append(savingsTrend, incAmt-expAmt)
		trendLabels = append(trendLabels, m)
	}

	return &models.DashboardMetrics{
		TotalIncome:      totalIncome,
		TotalExpenses:    totalExpenses,
		NetSavings:       netSavings,
		SavingsRate:      savingsRate,
		TransactionCount: ts.Len(),
		StartDate:        ts.MinDate(),
		EndDate:          ts.MaxDate(),
		IncomeTrend:      incomeTrend,
		ExpensesTrend:    expensesTrend,
		SavingsTrend:     savingsTrend,
		TrendLabels:      trendLabels,
	}
}

func calculateComparison(data *models.TransactionSet, start, end time.Time, compType string) *models.PeriodComparison {
	duration := end.Sub(start)

	var compStart, compEnd time.Time

	switch compType {
	case "previous":
		compEnd = start.Add(-24 * time.Hour) // Day before start
		compStart = compEnd.Add(-duration)
	case "year":
		compStart = start.AddDate(-1, 0, 0)
		compEnd = end.AddDate(-1, 0, 0)
	default:
		return nil
	}

	currentFiltered := data.FilterByDateRange(start, end)
	compFiltered := data.FilterByDateRange(compStart, compEnd)

	if compFiltered.Len() == 0 {
		return &models.PeriodComparison{HasData: false}
	}

	currentMetrics := calculateMetrics(currentFiltered)
	compMetrics := calculateMetrics(compFiltered)

	incomeChange := percentChange(currentMetrics.TotalIncome, compMetrics.TotalIncome)
	expensesChange := percentChange(currentMetrics.TotalExpenses, compMetrics.TotalExpenses)
	savingsChange := percentChange(currentMetrics.NetSavings, compMetrics.NetSavings)
	savingsRateChange := currentMetrics.SavingsRate - compMetrics.SavingsRate

	return &models.PeriodComparison{
		Current:           currentMetrics,
		Previous:          compMetrics,
		HasData:           true,
		IncomeChange:      incomeChange,
		ExpensesChange:    expensesChange,
		SavingsChange:     savingsChange,
		SavingsRateChange: savingsRateChange,
	}
}

func percentChange(current, previous float64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return ((current - previous) / math.Abs(previous)) * 100
}

// Chart data builders

func buildMonthlyChartData(ts *models.TransactionSet) map[string]interface{} {
	income := ts.FilterByType(models.Income)
	outflows := ts.FilterByType(models.Outflow)

	monthlyIncome := income.MonthlyTotals()
	monthlyOutflows := outflows.MonthlyTotals()

	// Combine and sort months
	monthSet := make(map[string]bool)
	for m := range monthlyIncome {
		monthSet[m] = true
	}
	for m := range monthlyOutflows {
		monthSet[m] = true
	}

	var months []string
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)

	var incomeValues, expenseValues []float64
	for _, m := range months {
		incomeValues = append(incomeValues, monthlyIncome[m])
		expenseValues = append(expenseValues, math.Abs(monthlyOutflows[m]))
	}

	return map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type": "bar",
				"name": "Income",
				"x":    months,
				"y":    incomeValues,
				"marker": map[string]string{
					"color": "#22c55e",
				},
			},
			{
				"type": "bar",
				"name": "Expenses",
				"x":    months,
				"y":    expenseValues,
				"marker": map[string]string{
					"color": "#ef4444",
				},
			},
		},
		"layout": map[string]interface{}{
			"barmode": "group",
		},
	}
}

func buildCategoryChartData(ts *models.TransactionSet) map[string]interface{} {
	outflows := ts.FilterByType(models.Outflow)
	categoryTotals := outflows.CategoryTotals()

	// Sort by value
	type catVal struct {
		cat string
		val float64
	}
	var sorted []catVal
	for cat, val := range categoryTotals {
		sorted = append(sorted, catVal{cat, val})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].val > sorted[j].val
	})

	// Take top 10
	if len(sorted) > 10 {
		other := 0.0
		for _, cv := range sorted[10:] {
			other += cv.val
		}
		sorted = sorted[:10]
		if other > 0 {
			sorted = append(sorted, catVal{"Other", other})
		}
	}

	var labels []string
	var values []float64
	for _, cv := range sorted {
		labels = append(labels, cv.cat)
		values = append(values, cv.val)
	}

	return map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type":   "pie",
				"labels": labels,
				"values": values,
				"hole":   0.4,
			},
		},
	}
}

func buildCashflowChartData(ts *models.TransactionSet) map[string]interface{} {
	sorted := ts.SortByDate()
	daily := sorted.GroupByDate()

	// Sort dates
	var dates []string
	for d := range daily {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	var dateLabels []string
	var amounts []float64

	for _, d := range dates {
		dayTotal := daily[d].SumAmount()
		dateLabels = append(dateLabels, d)
		amounts = append(amounts, dayTotal)
	}

	return map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type": "scatter",
				"mode": "lines",
				"name": "Cash Flow",
				"x":    dateLabels,
				"y":    amounts,
				"line": map[string]interface{}{
					"color": "#6366f1",
					"width": 2,
				},
				"fill":      "tozeroy",
				"fillcolor": "rgba(99, 102, 241, 0.1)",
			},
		},
	}
}

func buildMerchantsChartData(ts *models.TransactionSet) map[string]interface{} {
	outflows := ts.FilterByType(models.Outflow)

	// Group by description (merchant)
	merchantTotals := make(map[string]float64)
	for _, t := range outflows.Transactions {
		merchantTotals[t.Description] += math.Abs(t.Amount)
	}

	// Sort by value
	type merchVal struct {
		name string
		val  float64
	}
	var sorted []merchVal
	for name, val := range merchantTotals {
		sorted = append(sorted, merchVal{name, val})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].val > sorted[j].val
	})

	// Take top 10
	if len(sorted) > 10 {
		sorted = sorted[:10]
	}

	// Reverse for horizontal bar chart
	var labels []string
	var values []float64
	for i := len(sorted) - 1; i >= 0; i-- {
		labels = append(labels, sorted[i].name)
		values = append(values, sorted[i].val)
	}

	return map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type":        "bar",
				"orientation": "h",
				"x":           values,
				"y":           labels,
				"marker": map[string]string{
					"color": "#8b5cf6",
				},
			},
		},
	}
}

func buildWeeklyPatternChartData(ts *models.TransactionSet) map[string]interface{} {
	outflows := ts.FilterByType(models.Outflow)

	// Group by day of week
	dayTotals := make(map[int]float64)
	dayCounts := make(map[int]int)
	dayNames := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

	for _, t := range outflows.Transactions {
		dow := int(t.Date.Weekday())
		dayTotals[dow] += math.Abs(t.Amount)
		dayCounts[dow]++
	}

	// Calculate averages per day
	var values []float64
	for i := 0; i < 7; i++ {
		if dayCounts[i] > 0 {
			// Get number of weeks in the data
			minDate := ts.MinDate()
			maxDate := ts.MaxDate()
			weeks := maxDate.Sub(minDate).Hours() / 24 / 7
			if weeks < 1 {
				weeks = 1
			}
			values = append(values, dayTotals[i]/weeks)
		} else {
			values = append(values, 0)
		}
	}

	return map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type": "bar",
				"x":    dayNames,
				"y":    values,
				"marker": map[string]interface{}{
					"color": []string{
						"#94a3b8", "#3b82f6", "#3b82f6", "#3b82f6",
						"#3b82f6", "#3b82f6", "#94a3b8",
					},
				},
			},
		},
		"layout": map[string]interface{}{
			"yaxis": map[string]interface{}{
				"title": "Avg Spending ($)",
			},
		},
	}
}

func buildCumulativeChartData(ts *models.TransactionSet) map[string]interface{} {
	sorted := ts.SortByDate()
	daily := sorted.GroupByDate()

	// Sort dates
	var dates []string
	for d := range daily {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	var dateLabels []string
	var cumulative []float64
	var runningTotal float64

	for _, d := range dates {
		dayTotal := daily[d].SumAmount()
		runningTotal += dayTotal
		dateLabels = append(dateLabels, d)
		cumulative = append(cumulative, runningTotal)
	}

	// Determine line color based on final value
	lineColor := "#22c55e"
	fillColor := "rgba(34, 197, 94, 0.1)"
	if runningTotal < 0 {
		lineColor = "#ef4444"
		fillColor = "rgba(239, 68, 68, 0.1)"
	}

	return map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type": "scatter",
				"mode": "lines",
				"name": "Cumulative Balance",
				"x":    dateLabels,
				"y":    cumulative,
				"line": map[string]interface{}{
					"color": lineColor,
					"width": 2,
				},
				"fill":      "tozeroy",
				"fillcolor": fillColor,
			},
		},
		"layout": map[string]interface{}{
			"yaxis": map[string]interface{}{
				"title": "Cumulative ($)",
			},
		},
	}
}

// Insights analysis functions

func calculateInsights(allData, filtered *models.TransactionSet, startDate, endDate time.Time) *models.InsightsData {
	recurring := detectRecurringPayments(allData)
	trends := analyzeCategoryTrends(allData, startDate, endDate)
	income := analyzeIncomePatterns(allData)
	velocity := calculateSpendingVelocity(filtered, allData)

	// Calculate totals
	var totalRecurring, monthlyRecurring, regularIncome float64
	for _, r := range recurring {
		totalRecurring += r.AnnualCost
	}
	monthlyRecurring = totalRecurring / 12

	for _, ip := range income {
		if ip.IsRegular {
			regularIncome += ip.TotalAmount
		}
	}

	return &models.InsightsData{
		RecurringPayments:  recurring,
		CategoryTrends:     trends,
		IncomePatterns:     income,
		Velocity:           velocity,
		TotalRecurring:     totalRecurring,
		MonthlyRecurring:   monthlyRecurring,
		RegularIncomeTotal: regularIncome,
	}
}

func detectRecurringPayments(ts *models.TransactionSet) []models.RecurringPayment {
	var recurring []models.RecurringPayment

	outflows := ts.FilterByType(models.Outflow)
	if outflows.Len() < 2 {
		return recurring
	}

	// Group by normalized description
	groups := make(map[string][]models.Transaction)
	for _, t := range outflows.Transactions {
		key := strings.ToLower(strings.TrimSpace(t.Description))
		groups[key] = append(groups[key], t)
	}

	for desc, txns := range groups {
		if len(txns) < 3 {
			// Require at least 3 occurrences to establish a recurring pattern
			continue
		}

		// Sort by date
		sort.Slice(txns, func(i, j int) bool {
			return txns[i].Date.Before(txns[j].Date)
		})

		// Calculate intervals between transactions
		var intervals []float64
		for i := 1; i < len(txns); i++ {
			days := txns[i].Date.Sub(txns[i-1].Date).Hours() / 24
			intervals = append(intervals, days)
		}

		if len(intervals) == 0 {
			continue
		}

		// Calculate median interval
		sortedIntervals := make([]float64, len(intervals))
		copy(sortedIntervals, intervals)
		sort.Float64s(sortedIntervals)
		medianInterval := sortedIntervals[len(sortedIntervals)/2]

		// Check interval consistency (std dev < 5 days)
		var sumSq float64
		for _, interval := range intervals {
			diff := interval - medianInterval
			sumSq += diff * diff
		}
		stdDev := math.Sqrt(sumSq / float64(len(intervals)))

		if stdDev > 7 { // Allow some variance
			continue
		}

		// Calculate amount consistency
		var amounts []float64
		for _, t := range txns {
			amounts = append(amounts, math.Abs(t.Amount))
		}
		avgAmount := 0.0
		for _, a := range amounts {
			avgAmount += a
		}
		avgAmount /= float64(len(amounts))

		// Check if amounts are within 10% tolerance
		amountConsistent := true
		for _, a := range amounts {
			if math.Abs(a-avgAmount)/avgAmount > 0.10 {
				amountConsistent = false
				break
			}
		}

		if !amountConsistent {
			continue
		}

		// Classify frequency
		var frequency string
		var annualMultiplier float64

		switch {
		case medianInterval >= 5 && medianInterval <= 9:
			// Weekly requires at least 4 occurrences (4 weeks of data)
			if len(txns) < 4 {
				continue
			}
			frequency = "weekly"
			annualMultiplier = 52
		case medianInterval >= 12 && medianInterval <= 16:
			// Biweekly requires at least 4 occurrences (2 months of data)
			if len(txns) < 4 {
				continue
			}
			frequency = "biweekly"
			annualMultiplier = 26
		case medianInterval >= 25 && medianInterval <= 35:
			frequency = "monthly"
			annualMultiplier = 12
		case medianInterval >= 85 && medianInterval <= 95:
			frequency = "quarterly"
			annualMultiplier = 4
		case medianInterval >= 350 && medianInterval <= 380:
			frequency = "yearly"
			annualMultiplier = 1
		default:
			continue
		}

		// Calculate confidence based on consistency
		confidence := 1.0 - (stdDev / medianInterval)
		if confidence < 0.5 {
			continue
		}

		lastDate := txns[len(txns)-1].Date
		nextExpected := lastDate.AddDate(0, 0, int(medianInterval))

		recurring = append(recurring, models.RecurringPayment{
			Description:  desc,
			Amount:       avgAmount,
			Frequency:    frequency,
			LastDate:     lastDate,
			NextExpected: nextExpected,
			AnnualCost:   avgAmount * annualMultiplier,
			Occurrences:  len(txns),
			Confidence:   confidence,
			Transactions: txns,
		})
	}

	// Sort by annual cost (highest first)
	sort.Slice(recurring, func(i, j int) bool {
		return recurring[i].AnnualCost > recurring[j].AnnualCost
	})

	// Limit to top 20
	if len(recurring) > 20 {
		recurring = recurring[:20]
	}

	return recurring
}

func analyzeCategoryTrends(ts *models.TransactionSet, currentStart, currentEnd time.Time) []models.CategoryTrend {
	var trends []models.CategoryTrend

	// Calculate previous period
	duration := currentEnd.Sub(currentStart)
	prevStart := currentStart.Add(-duration - 24*time.Hour)
	prevEnd := currentStart.Add(-24 * time.Hour)

	currentFiltered := ts.FilterByDateRange(currentStart, currentEnd)
	prevFiltered := ts.FilterByDateRange(prevStart, prevEnd)

	currentOutflows := currentFiltered.FilterByType(models.Outflow)
	prevOutflows := prevFiltered.FilterByType(models.Outflow)

	currentTotals := currentOutflows.CategoryTotals()
	prevTotals := prevOutflows.CategoryTotals()

	// Get all categories
	catSet := make(map[string]bool)
	for cat := range currentTotals {
		catSet[cat] = true
	}
	for cat := range prevTotals {
		catSet[cat] = true
	}

	for cat := range catSet {
		current := currentTotals[cat]
		previous := prevTotals[cat]

		var changePercent float64
		var direction string

		if previous == 0 {
			if current == 0 {
				changePercent = 0
				direction = "stable"
			} else {
				changePercent = 100
				direction = "up"
			}
		} else {
			changePercent = ((current - previous) / previous) * 100
			if changePercent > 5 {
				direction = "up"
			} else if changePercent < -5 {
				direction = "down"
			} else {
				direction = "stable"
			}
		}

		trends = append(trends, models.CategoryTrend{
			Category:       cat,
			CurrentAmount:  current,
			PreviousAmount: previous,
			ChangePercent:  changePercent,
			ChangeAmount:   current - previous,
			Direction:      direction,
		})
	}

	// Sort by absolute change amount (biggest changes first)
	sort.Slice(trends, func(i, j int) bool {
		return math.Abs(trends[i].ChangeAmount) > math.Abs(trends[j].ChangeAmount)
	})

	// Limit to top 10
	if len(trends) > 10 {
		trends = trends[:10]
	}

	return trends
}

func analyzeIncomePatterns(ts *models.TransactionSet) []models.IncomePattern {
	var patterns []models.IncomePattern

	income := ts.FilterByType(models.Income)
	if income.Len() < 2 {
		return patterns
	}

	// Group by normalized description
	groups := make(map[string][]models.Transaction)
	for _, t := range income.Transactions {
		key := strings.ToLower(strings.TrimSpace(t.Description))
		groups[key] = append(groups[key], t)
	}

	for desc, txns := range groups {
		if len(txns) < 2 {
			continue
		}

		// Sort by date
		sort.Slice(txns, func(i, j int) bool {
			return txns[i].Date.Before(txns[j].Date)
		})

		// Calculate total and average
		var total float64
		for _, t := range txns {
			total += t.Amount
		}
		avg := total / float64(len(txns))

		// Calculate intervals
		var intervals []float64
		for i := 1; i < len(txns); i++ {
			days := txns[i].Date.Sub(txns[i-1].Date).Hours() / 24
			intervals = append(intervals, days)
		}

		// Determine frequency
		var frequency string
		isRegular := false

		if len(intervals) > 0 {
			// Calculate median interval
			sortedIntervals := make([]float64, len(intervals))
			copy(sortedIntervals, intervals)
			sort.Float64s(sortedIntervals)
			medianInterval := sortedIntervals[len(sortedIntervals)/2]

			// Calculate std dev
			var sumSq float64
			for _, interval := range intervals {
				diff := interval - medianInterval
				sumSq += diff * diff
			}
			stdDev := math.Sqrt(sumSq / float64(len(intervals)))

			// Classify frequency
			switch {
			case medianInterval >= 5 && medianInterval <= 9 && stdDev < 3:
				frequency = "weekly"
				isRegular = true
			case medianInterval >= 12 && medianInterval <= 16 && stdDev < 4:
				frequency = "biweekly"
				isRegular = true
			case medianInterval >= 25 && medianInterval <= 35 && stdDev < 7:
				frequency = "monthly"
				isRegular = true
			default:
				frequency = "irregular"
				isRegular = false
			}
		} else {
			frequency = "one-time"
		}

		patterns = append(patterns, models.IncomePattern{
			Description: desc,
			AvgAmount:   avg,
			Frequency:   frequency,
			IsRegular:   isRegular,
			Occurrences: len(txns),
			TotalAmount: total,
		})
	}

	// Sort by total amount (highest first)
	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].TotalAmount > patterns[j].TotalAmount
	})

	// Limit to top 10
	if len(patterns) > 10 {
		patterns = patterns[:10]
	}

	return patterns
}

func calculateSpendingVelocity(currentPeriod, allData *models.TransactionSet) *models.SpendingVelocity {
	currentOutflows := currentPeriod.FilterByType(models.Outflow)
	allOutflows := allData.FilterByType(models.Outflow)

	if currentOutflows.Len() == 0 {
		return &models.SpendingVelocity{}
	}

	// Calculate current daily average
	currentMin := currentPeriod.MinDate()
	currentMax := currentPeriod.MaxDate()
	currentDays := currentMax.Sub(currentMin).Hours()/24 + 1
	if currentDays < 1 {
		currentDays = 1
	}
	dailyAvg := currentOutflows.SumAbsAmount() / currentDays

	// Calculate historical daily average
	allMin := allData.MinDate()
	allMax := allData.MaxDate()
	allDays := allMax.Sub(allMin).Hours()/24 + 1
	if allDays < 1 {
		allDays = 1
	}
	historicalDaily := allOutflows.SumAbsAmount() / allDays

	// Calculate month projection
	now := time.Now()
	daysInMonth := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.Local).Day()
	dayOfMonth := now.Day()
	daysRemaining := daysInMonth - dayOfMonth

	// Project spending for rest of month
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	currentMonthData := currentPeriod.FilterByDateRange(currentMonthStart, now)
	currentMonthOutflows := currentMonthData.FilterByType(models.Outflow)
	spentSoFar := currentMonthOutflows.SumAbsAmount()

	monthProjection := spentSoFar + (dailyAvg * float64(daysRemaining))

	// Calculate burn rate change
	var burnRateChange float64
	if historicalDaily > 0 {
		burnRateChange = ((dailyAvg - historicalDaily) / historicalDaily) * 100
	}

	return &models.SpendingVelocity{
		DailyAverage:    dailyAvg,
		HistoricalDaily: historicalDaily,
		MonthProjection: monthProjection,
		DaysRemaining:   daysRemaining,
		BurnRateChange:  burnRateChange,
	}
}

// Insight partial handlers

func handleRecurringPartial(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	recurring := detectRecurringPayments(data)

	var totalRecurring float64
	for _, r := range recurring {
		totalRecurring += r.AnnualCost
	}

	partialData := map[string]interface{}{
		"RecurringPayments": recurring,
		"TotalRecurring":    totalRecurring,
		"MonthlyRecurring":  totalRecurring / 12,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "recurring-payments", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleTrendsPartial(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MaxDate().AddDate(0, -1, 0)
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	trends := analyzeCategoryTrends(data, startDate, endDate)

	partialData := map[string]interface{}{
		"CategoryTrends": trends,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "category-trends", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleTrendsChartData(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MaxDate().AddDate(0, -1, 0)
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	trends := analyzeCategoryTrends(data, startDate, endDate)

	// Build chart data
	var categories []string
	var currentValues []float64
	var previousValues []float64
	var colors []string

	for _, t := range trends {
		categories = append(categories, t.Category)
		currentValues = append(currentValues, t.CurrentAmount)
		previousValues = append(previousValues, t.PreviousAmount)
		if t.Direction == "up" {
			colors = append(colors, "#ef4444") // Red for increased spending
		} else if t.Direction == "down" {
			colors = append(colors, "#22c55e") // Green for decreased spending
		} else {
			colors = append(colors, "#6b7280") // Gray for stable
		}
	}

	chartData := map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type":   "bar",
				"name":   "Current Period",
				"x":      categories,
				"y":      currentValues,
				"marker": map[string]interface{}{"color": colors},
			},
			{
				"type":   "bar",
				"name":   "Previous Period",
				"x":      categories,
				"y":      previousValues,
				"marker": map[string]string{"color": "#94a3b8"},
			},
		},
		"layout": map[string]interface{}{
			"barmode": "group",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chartData)
}

func handleVelocityPartial(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)

	if startDate.IsZero() {
		startDate = data.MinDate()
	}
	if endDate.IsZero() {
		endDate = data.MaxDate()
	}

	filtered := data.FilterByDateRange(startDate, endDate)
	velocity := calculateSpendingVelocity(filtered, data)

	partialData := map[string]interface{}{
		"Velocity": velocity,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "spending-velocity", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleIncomePartial(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	income := analyzeIncomePatterns(data)

	var regularTotal float64
	for _, ip := range income {
		if ip.IsRegular {
			regularTotal += ip.TotalAmount
		}
	}

	partialData := map[string]interface{}{
		"IncomePatterns":     income,
		"RegularIncomeTotal": regularTotal,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "income-patterns", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}
