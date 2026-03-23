package whatif

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budget2/internal/handlers/insights"
	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/retirement"
	"budget2/internal/templates"
)

// analysisCache caches expensive analysis results keyed by settings hash
type analysisCache struct {
	mu       sync.RWMutex
	hash     string
	analysis *models.WhatIfAnalysis
	cachedAt time.Time
}

var cache = &analysisCache{}

// getSettingsHash generates a hash of the settings for cache key
func getSettingsHash(settings *models.WhatIfSettings) string {
	data, err := json.Marshal(settings)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:8]) // Use first 8 bytes for shorter key
}

// getCachedAnalysis returns cached analysis if settings match and cache is fresh
func getCachedAnalysis(settings *models.WhatIfSettings) *models.WhatIfAnalysis {
	hash := getSettingsHash(settings)
	if hash == "" {
		return nil
	}

	cache.mu.RLock()
	defer cache.mu.RUnlock()

	// Return cached result if hash matches and cache is less than 5 minutes old
	if cache.hash == hash && time.Since(cache.cachedAt) < 5*time.Minute {
		return cache.analysis
	}
	return nil
}

// setCachedAnalysis stores analysis result in cache
func setCachedAnalysis(settings *models.WhatIfSettings, analysis *models.WhatIfAnalysis) {
	hash := getSettingsHash(settings)
	if hash == "" {
		return
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.hash = hash
	cache.analysis = analysis
	cache.cachedAt = time.Now()
}

// runAnalysisWithCache runs full analysis, using cache when available
func runAnalysisWithCache(settings *models.WhatIfSettings) *models.WhatIfAnalysis {
	// Check cache first
	if cached := getCachedAnalysis(settings); cached != nil {
		return cached
	}

	// Run full analysis
	calc := retirement.NewCalculator(settings)
	analysis := calc.RunFullAnalysis()

	// Cache the result
	setCachedAnalysis(settings, analysis)

	return analysis
}

// renderError renders an HTML error fragment for HTMX requests
func renderError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	html := fmt.Sprintf(`<div class="p-4 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg">
		<div class="flex items-center">
			<svg class="w-5 h-5 text-red-500 dark:text-red-400 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>
			</svg>
			<span class="text-red-700 dark:text-red-300 font-medium">Error</span>
		</div>
		<p class="mt-2 text-sm text-red-600 dark:text-red-400">%s</p>
	</div>`, message)
	w.Write([]byte(html))
}

// parseFormFloat parses a float64 from form data, returning an error if invalid
func parseFormFloat(r *http.Request, key string) (float64, error) {
	v := r.FormValue(key)
	if v == "" {
		return 0, nil
	}
	return strconv.ParseFloat(v, 64)
}

// parseFormInt parses an int from form data, returning an error if invalid
func parseFormInt(r *http.Request, key string) (int, error) {
	v := r.FormValue(key)
	if v == "" {
		return 0, nil
	}
	return strconv.Atoi(v)
}

// parseRequiredFormFloat parses a required float64 from form data
func parseRequiredFormFloat(r *http.Request, key string) (float64, error) {
	v := r.FormValue(key)
	if v == "" {
		return 0, fmt.Errorf("missing required field: %s", key)
	}
	val, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: must be a number", key)
	}
	return val, nil
}

// parseRequiredFormInt parses a required int from form data
func parseRequiredFormInt(r *http.Request, key string) (int, error) {
	v := r.FormValue(key)
	if v == "" {
		return 0, fmt.Errorf("missing required field: %s", key)
	}
	val, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: must be an integer", key)
	}
	return val, nil
}

var (
	loader        *dataloader.DataLoader
	renderer      *templates.Renderer
	retirementMgr *retirement.SettingsManager
)

// Initialize sets up the whatif package with required dependencies
func Initialize(l *dataloader.DataLoader, r *templates.Renderer, rm *retirement.SettingsManager) {
	loader = l
	renderer = r
	retirementMgr = rm
}

// RegisterRoutes registers all whatif routes
func RegisterRoutes(r chi.Router) {
	r.Get("/whatif", handleWhatIf)
	r.Post("/whatif/calculate", handleWhatIfCalculate)
	r.Post("/whatif/settings", handleWhatIfSettings)
	r.Post("/whatif/income", handleWhatIfAddIncome)
	r.Put("/whatif/income/{id}", handleWhatIfUpdateIncome)
	r.Delete("/whatif/income/{id}", handleWhatIfDeleteIncome)
	r.Post("/whatif/income/{id}/restore", handleWhatIfRestoreIncome)
	r.Post("/whatif/expense", handleWhatIfAddExpense)
	r.Put("/whatif/expense/{id}", handleWhatIfUpdateExpense)
	r.Delete("/whatif/expense/{id}", handleWhatIfDeleteExpense)
	r.Post("/whatif/expense/{id}/restore", handleWhatIfRestoreExpense)
	r.Post("/whatif/healthcare", handleWhatIfAddHealthcare)
	r.Put("/whatif/healthcare/{id}", handleWhatIfUpdateHealthcare)
	r.Delete("/whatif/healthcare/{id}", handleWhatIfDeleteHealthcare)
	r.Post("/whatif/spending-phases", handleWhatIfSpendingPhases)
	r.Post("/whatif/spending-phases/add", handleWhatIfAddPhase)
	r.Delete("/whatif/spending-phases/{index}", handleWhatIfDeletePhase)
	r.Post("/whatif/spending-phases/reset", handleWhatIfResetPhases)
	r.Get("/whatif/chart/projection", handleWhatIfProjectionChart)
	r.Post("/whatif/sync", handleWhatIfSync)
	r.Post("/whatif/montecarlo", handleWhatIfMonteCarlo)
	r.Post("/whatif/roth-conversion", handleWhatIfRothConversion)
	r.Post("/whatif/bigticket", handleWhatIfAddBigTicket)
	r.Delete("/whatif/bigticket/{id}", handleWhatIfDeleteBigTicket)
	r.Post("/whatif/bigticket/{id}/restore", handleWhatIfRestoreBigTicket)
	r.Get("/whatif/scenarios", handleListScenarios)
	r.Post("/whatif/scenarios", handleCreateScenario)
	r.Post("/whatif/scenarios/switch", handleSwitchScenario)
	r.Delete("/whatif/scenarios/{filename}", handleDeleteScenario)
	r.Put("/whatif/scenarios/{filename}", handleRenameScenario)
}

func handleWhatIf(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		log.Printf("Error loading what-if settings: %v", err)
		settings = models.DefaultWhatIfSettings()
	}

	// If no income sources saved yet, auto-sync from dashboard on first load
	if len(settings.IncomeSources) == 0 {
		syncSettingsFromDashboard(settings)
		retirementMgr.Save(settings)
	}

	// Run full analysis (with caching)
	analysis := runAnalysisWithCache(settings)

	scenarios, _ := retirementMgr.ListScenarios()
	activeScenario := retirementMgr.ActiveScenario()
	activeFilename := retirementMgr.ActiveFilename()

	pageData := map[string]interface{}{
		"Title":          "What-If Analysis",
		"ActiveTab":      "whatif",
		"Settings":       settings,
		"Analysis":       analysis,
		"Scenarios":      scenarios,
		"ActiveScenario": activeScenario,
		"ActiveFilename": activeFilename,
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
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse form values with error handling
	updates := make(map[string]interface{})

	if v, err := parseFormFloat(r, "portfolio_value"); err != nil {
		renderError(w, "Invalid portfolio value: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("portfolio_value") != "" {
		updates["portfolio_value"] = v
	}

	if v, err := parseFormFloat(r, "monthly_living_expenses"); err != nil {
		renderError(w, "Invalid monthly expenses: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("monthly_living_expenses") != "" {
		updates["monthly_living_expenses"] = v
	}

	if v, err := parseFormFloat(r, "monthly_healthcare"); err != nil {
		renderError(w, "Invalid healthcare cost: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("monthly_healthcare") != "" {
		updates["monthly_healthcare"] = v
	}

	if v, err := parseFormInt(r, "healthcare_start_years"); err != nil {
		renderError(w, "Invalid healthcare start years: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("healthcare_start_years") != "" {
		updates["healthcare_start_years"] = v
	}

	if v, err := parseFormInt(r, "current_age"); err != nil {
		renderError(w, "Invalid age: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("current_age") != "" {
		if v < 18 || v > 120 {
			renderError(w, "Age must be between 18 and 120", http.StatusBadRequest)
			return
		}
		updates["current_age"] = v
	}

	// Spouse age (0 = no spouse)
	if v, err := parseFormInt(r, "spouse_age"); err != nil {
		renderError(w, "Invalid spouse age: "+err.Error(), http.StatusBadRequest)
		return
	} else if r.FormValue("spouse_age") != "" {
		if v < 0 || v > 120 {
			renderError(w, "Spouse age must be between 0 and 120", http.StatusBadRequest)
			return
		}
		updates["spouse_age"] = v
	}

	// Phase age reference (which person's age triggers phase transitions)
	if v := r.FormValue("phase_age_reference"); v != "" {
		if v != "younger" && v != "older" && v != "primary" && v != "spouse" {
			renderError(w, "Invalid phase age reference", http.StatusBadRequest)
			return
		}
		updates["phase_age_reference"] = v
	}

	if v, err := parseFormFloat(r, "tax_deferred_percent"); err != nil {
		renderError(w, "Invalid tax-deferred percent: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("tax_deferred_percent") != "" {
		if v < 0 || v > 100 {
			renderError(w, "Tax-deferred percent must be between 0 and 100", http.StatusBadRequest)
			return
		}
		updates["tax_deferred_percent"] = v
	}

	if v, err := parseFormFloat(r, "roth_percent"); err != nil {
		renderError(w, "Invalid Roth percent: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("roth_percent") != "" {
		if v < 0 || v > 100 {
			renderError(w, "Roth percent must be between 0 and 100", http.StatusBadRequest)
			return
		}
		// Validate that tax_deferred + roth <= 100
		taxDeferred := 0.0
		if td, ok := updates["tax_deferred_percent"]; ok {
			taxDeferred = td.(float64)
		} else if tdStr := r.FormValue("tax_deferred_percent"); tdStr != "" {
			taxDeferred, _ = strconv.ParseFloat(tdStr, 64)
		}
		if taxDeferred+v > 100 {
			renderError(w, "Tax-deferred + Roth cannot exceed 100%", http.StatusBadRequest)
			return
		}
		updates["roth_percent"] = v
	}

	if v, err := parseFormFloat(r, "stock_percent"); err != nil {
		renderError(w, "Invalid stock percent: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("stock_percent") != "" {
		if v < 0 || v > 100 {
			renderError(w, "Stock percent must be between 0 and 100", http.StatusBadRequest)
			return
		}
		updates["stock_percent"] = v
	}

	if v, err := parseFormFloat(r, "cash_percent"); err != nil {
		renderError(w, "Invalid cash percent: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("cash_percent") != "" {
		if v < 0 || v > 100 {
			renderError(w, "Cash percent must be between 0 and 100", http.StatusBadRequest)
			return
		}
		// Validate that stock + cash <= 100
		stockPct := 60.0 // Default
		if sp, ok := updates["stock_percent"]; ok {
			stockPct = sp.(float64)
		} else if spStr := r.FormValue("stock_percent"); spStr != "" {
			stockPct, _ = strconv.ParseFloat(spStr, 64)
		}
		if stockPct+v > 100 {
			renderError(w, "Stocks + Cash cannot exceed 100%", http.StatusBadRequest)
			return
		}
		updates["cash_percent"] = v
	}

	// Per-account asset allocation fields
	// Tax-Deferred allocation
	if v, err := parseFormFloat(r, "tax_deferred_stock_percent"); err != nil {
		renderError(w, "Invalid tax-deferred stock percent: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("tax_deferred_stock_percent") != "" {
		if v < 0 || v > 100 {
			renderError(w, "Tax-deferred stock percent must be between 0 and 100", http.StatusBadRequest)
			return
		}
		updates["tax_deferred_stock_percent"] = v
	}

	if v, err := parseFormFloat(r, "tax_deferred_cash_percent"); err != nil {
		renderError(w, "Invalid tax-deferred cash percent: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("tax_deferred_cash_percent") != "" {
		if v < 0 || v > 100 {
			renderError(w, "Tax-deferred cash percent must be between 0 and 100", http.StatusBadRequest)
			return
		}
		updates["tax_deferred_cash_percent"] = v
	}

	// Roth allocation
	if v, err := parseFormFloat(r, "roth_stock_percent"); err != nil {
		renderError(w, "Invalid Roth stock percent: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("roth_stock_percent") != "" {
		if v < 0 || v > 100 {
			renderError(w, "Roth stock percent must be between 0 and 100", http.StatusBadRequest)
			return
		}
		updates["roth_stock_percent"] = v
	}

	if v, err := parseFormFloat(r, "roth_cash_percent"); err != nil {
		renderError(w, "Invalid Roth cash percent: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("roth_cash_percent") != "" {
		if v < 0 || v > 100 {
			renderError(w, "Roth cash percent must be between 0 and 100", http.StatusBadRequest)
			return
		}
		updates["roth_cash_percent"] = v
	}

	// Taxable allocation
	if v, err := parseFormFloat(r, "taxable_stock_percent"); err != nil {
		renderError(w, "Invalid taxable stock percent: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("taxable_stock_percent") != "" {
		if v < 0 || v > 100 {
			renderError(w, "Taxable stock percent must be between 0 and 100", http.StatusBadRequest)
			return
		}
		updates["taxable_stock_percent"] = v
	}

	if v, err := parseFormFloat(r, "taxable_cash_percent"); err != nil {
		renderError(w, "Invalid taxable cash percent: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("taxable_cash_percent") != "" {
		if v < 0 || v > 100 {
			renderError(w, "Taxable cash percent must be between 0 and 100", http.StatusBadRequest)
			return
		}
		updates["taxable_cash_percent"] = v
	}

	// Validate per-account allocations: stocks + cash must not exceed 100%
	validateAlloc := func(stockKey, cashKey, label string) bool {
		stockVal, hasStock := updates[stockKey].(float64)
		cashVal, hasCash := updates[cashKey].(float64)
		if hasStock && hasCash && stockVal+cashVal > 100 {
			renderError(w, label+" stocks + cash cannot exceed 100%", http.StatusBadRequest)
			return false
		}
		return true
	}
	if !validateAlloc("tax_deferred_stock_percent", "tax_deferred_cash_percent", "Tax-deferred") {
		return
	}
	if !validateAlloc("roth_stock_percent", "roth_cash_percent", "Roth") {
		return
	}
	if !validateAlloc("taxable_stock_percent", "taxable_cash_percent", "Taxable") {
		return
	}

	if v, err := parseFormFloat(r, "inflation_rate"); err != nil {
		renderError(w, "Invalid inflation rate: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("inflation_rate") != "" {
		updates["inflation_rate"] = v
	}

	if v, err := parseFormFloat(r, "healthcare_inflation"); err != nil {
		renderError(w, "Invalid healthcare inflation: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("healthcare_inflation") != "" {
		updates["healthcare_inflation"] = v
	}

	if v, err := parseFormFloat(r, "spending_decline_rate"); err != nil {
		renderError(w, "Invalid spending decline rate: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("spending_decline_rate") != "" {
		updates["spending_decline_rate"] = v
	}

	if v, err := parseFormFloat(r, "investment_return"); err != nil {
		renderError(w, "Invalid investment return: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("investment_return") != "" {
		updates["investment_return"] = v
	}

	if v, err := parseFormFloat(r, "discount_rate"); err != nil {
		renderError(w, "Invalid discount rate: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("discount_rate") != "" {
		updates["discount_rate"] = v
	}

	if v, err := parseFormInt(r, "projection_years"); err != nil {
		renderError(w, "Invalid projection years: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("projection_years") != "" {
		if v < 1 || v > 100 {
			renderError(w, "Projection years must be between 1 and 100", http.StatusBadRequest)
			return
		}
		updates["projection_years"] = v
	}

	if v, err := parseFormInt(r, "tax_deferred_delay_years"); err != nil {
		renderError(w, "Invalid tax-deferred delay: "+err.Error(), http.StatusBadRequest)
		return
	} else if r.FormValue("tax_deferred_delay_years") != "" {
		if v < 0 || v > 30 {
			renderError(w, "Tax-deferred delay must be between 0 and 30 years", http.StatusBadRequest)
			return
		}
		updates["tax_deferred_delay_years"] = v
	}

	if v, err := parseFormFloat(r, "steady_state_override_year"); err != nil {
		renderError(w, "Invalid steady state year: "+err.Error(), http.StatusBadRequest)
		return
	} else if v != 0 || r.FormValue("steady_state_override_year") != "" {
		updates["steady_state_override_year"] = v
	}

	settings, err := retirementMgr.UpdateSettings(updates)
	if err != nil {
		renderError(w, "Failed to save settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		renderError(w, "Income source name is required", http.StatusBadRequest)
		return
	}

	amount, err := parseRequiredFormFloat(r, "amount")
	if err != nil {
		renderError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if amount < 0 {
		renderError(w, "Amount cannot be negative", http.StatusBadRequest)
		return
	}

	startYear, err := parseFormInt(r, "start_year")
	if err != nil {
		renderError(w, "Invalid start year: "+err.Error(), http.StatusBadRequest)
		return
	}
	if startYear < 0 {
		renderError(w, "Start year cannot be negative", http.StatusBadRequest)
		return
	}

	var endYearPtr *int
	if r.FormValue("end_year") != "" {
		ey, err := parseFormInt(r, "end_year")
		if err != nil {
			renderError(w, "Invalid end year: "+err.Error(), http.StatusBadRequest)
			return
		}
		if ey < 0 {
			renderError(w, "End year cannot be negative", http.StatusBadRequest)
			return
		}
		if ey < startYear {
			renderError(w, "End year cannot be before start year", http.StatusBadRequest)
			return
		}
		endYearPtr = &ey
	}

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

	if endYearPtr != nil {
		endMonth := *endYearPtr * 12
		source.EndMonth = &endMonth
	}

	settings, err := retirementMgr.AddIncomeSource(source)
	if err != nil {
		renderError(w, "Failed to add income source: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

func handleWhatIfUpdateIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	startYear, err := parseFormInt(r, "start_year")
	if err != nil {
		renderError(w, "Invalid start year: "+err.Error(), http.StatusBadRequest)
		return
	}
	if startYear < 0 {
		renderError(w, "Start year cannot be negative", http.StatusBadRequest)
		return
	}

	var endYearPtr *int
	if r.FormValue("end_year") != "" {
		ey, err := parseFormInt(r, "end_year")
		if err != nil {
			renderError(w, "Invalid end year: "+err.Error(), http.StatusBadRequest)
			return
		}
		if ey < 0 {
			renderError(w, "End year cannot be negative", http.StatusBadRequest)
			return
		}
		if ey < startYear {
			renderError(w, "End year cannot be before start year", http.StatusBadRequest)
			return
		}
		endYearPtr = &ey
	}

	cola := r.FormValue("cola") == "on" || r.FormValue("cola") == "true"

	colaRate := 0.0
	if cola {
		colaRate = 0.02 // 2% COLA
	}

	settings, err := retirementMgr.UpdateIncomeSource(id, startYear, endYearPtr, colaRate)
	if err != nil {
		renderError(w, "Failed to update income source: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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
		renderError(w, "Failed to remove income source: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

func handleWhatIfRestoreIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RestoreIncomeSource(id)
	if err != nil {
		renderError(w, "Failed to restore income source: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		renderError(w, "Expense name is required", http.StatusBadRequest)
		return
	}

	amount, err := parseRequiredFormFloat(r, "amount")
	if err != nil {
		renderError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if amount < 0 {
		renderError(w, "Amount cannot be negative", http.StatusBadRequest)
		return
	}

	startYear, err := parseFormInt(r, "start_year")
	if err != nil {
		renderError(w, "Invalid start year: "+err.Error(), http.StatusBadRequest)
		return
	}
	if startYear < 0 {
		renderError(w, "Start year cannot be negative", http.StatusBadRequest)
		return
	}

	var endYearPtr *int
	if r.FormValue("end_year") != "" {
		ey, err := parseFormInt(r, "end_year")
		if err != nil {
			renderError(w, "Invalid end year: "+err.Error(), http.StatusBadRequest)
			return
		}
		if ey < 0 {
			renderError(w, "End year cannot be negative", http.StatusBadRequest)
			return
		}
		if ey < startYear {
			renderError(w, "End year cannot be before start year", http.StatusBadRequest)
			return
		}
		endYearPtr = &ey
	}

	inflation := r.FormValue("inflation") == "on" || r.FormValue("inflation") == "true"
	discretionary := r.FormValue("discretionary") == "on" || r.FormValue("discretionary") == "true"

	source := models.ExpenseSource{
		ID:            uuid.New().String(),
		Name:          name,
		Amount:        amount,
		StartYear:     startYear,
		EndYear:       0, // Default to perpetual
		Inflation:     inflation,
		Discretionary: discretionary,
	}
	if endYearPtr != nil {
		source.EndYear = *endYearPtr
	}

	settings, err := retirementMgr.AddExpenseSource(source)
	if err != nil {
		renderError(w, "Failed to add expense: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

func handleWhatIfUpdateExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	startYear, err := parseFormInt(r, "start_year")
	if err != nil {
		renderError(w, "Invalid start year: "+err.Error(), http.StatusBadRequest)
		return
	}
	if startYear < 0 {
		renderError(w, "Start year cannot be negative", http.StatusBadRequest)
		return
	}

	var endYearPtr *int
	if r.FormValue("end_year") != "" {
		ey, err := parseFormInt(r, "end_year")
		if err != nil {
			renderError(w, "Invalid end year: "+err.Error(), http.StatusBadRequest)
			return
		}
		if ey < 0 {
			renderError(w, "End year cannot be negative", http.StatusBadRequest)
			return
		}
		if ey < startYear {
			renderError(w, "End year cannot be before start year", http.StatusBadRequest)
			return
		}
		endYearPtr = &ey
	}

	inflation := r.FormValue("inflation") == "on" || r.FormValue("inflation") == "true"
	discretionary := r.FormValue("discretionary") == "on" || r.FormValue("discretionary") == "true"

	settings, err := retirementMgr.UpdateExpenseSource(id, startYear, endYearPtr, inflation, discretionary)
	if err != nil {
		renderError(w, "Failed to update expense: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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
		renderError(w, "Failed to remove expense: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

func handleWhatIfRestoreExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RestoreExpenseSource(id)
	if err != nil {
		renderError(w, "Failed to restore expense: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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
				"title":      "Balance ($)",
				"tickformat": "$,.0f",
			},
			"showlegend": false,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chartData)
}

func handleWhatIfSync(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync expenses and income from dashboard
	if err := syncSettingsFromDashboard(settings); err != nil {
		renderError(w, "Failed to sync from dashboard: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Save the synced settings
	if err := retirementMgr.Save(settings); err != nil {
		renderError(w, "Failed to save settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

func handleWhatIfMonteCarlo(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-run the full analysis which includes a fresh Monte Carlo simulation
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

func handleWhatIfAddHealthcare(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	age, err := parseFormInt(r, "current_age")
	if err != nil {
		renderError(w, "Invalid age: "+err.Error(), http.StatusBadRequest)
		return
	}
	coverageType := r.FormValue("current_coverage")
	monthlyCost, err := parseFormFloat(r, "current_monthly_cost")
	if err != nil {
		renderError(w, "Invalid monthly cost: "+err.Error(), http.StatusBadRequest)
		return
	}
	if monthlyCost < 0 {
		renderError(w, "Monthly cost cannot be negative", http.StatusBadRequest)
		return
	}
	preMedicareInflation, err := parseFormFloat(r, "pre_medicare_inflation")
	if err != nil {
		renderError(w, "Invalid pre-Medicare inflation: "+err.Error(), http.StatusBadRequest)
		return
	}
	medicareCost, err := parseFormFloat(r, "medicare_monthly_cost")
	if err != nil {
		renderError(w, "Invalid Medicare cost: "+err.Error(), http.StatusBadRequest)
		return
	}
	postMedicareInflation, err := parseFormFloat(r, "post_medicare_inflation")
	if err != nil {
		renderError(w, "Invalid post-Medicare inflation: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Set defaults if not provided
	if name == "" {
		name = "Person"
	}
	if age == 0 {
		age = 65
	}
	if age < 0 || age > 120 {
		renderError(w, "Age must be between 0 and 120", http.StatusBadRequest)
		return
	}
	if coverageType == "" {
		if age >= 65 {
			coverageType = string(models.CoverageMedicare)
		} else {
			coverageType = string(models.CoverageACA)
		}
	}
	if monthlyCost == 0 {
		if coverageType == string(models.CoverageMedicare) {
			monthlyCost = 459
		} else {
			monthlyCost = 1100
		}
	}
	if preMedicareInflation == 0 {
		preMedicareInflation = 7.0
	}
	if medicareCost == 0 {
		medicareCost = 600
	}
	if postMedicareInflation == 0 {
		postMedicareInflation = 4.0
	}

	person := models.HealthcarePerson{
		ID:                    uuid.New().String(),
		Name:                  name,
		CurrentAge:            age,
		CurrentCoverage:       models.CoverageType(coverageType),
		CurrentMonthlyCost:    monthlyCost,
		PreMedicareInflation:  preMedicareInflation,
		MedicareMonthlyCost:   medicareCost,
		PostMedicareInflation: postMedicareInflation,
		MedicareEligibleAge:   65,
	}

	settings, err := retirementMgr.AddHealthcarePerson(person)
	if err != nil {
		renderError(w, "Failed to add healthcare person: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

func handleWhatIfUpdateHealthcare(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	updates := make(map[string]interface{})

	if v := r.FormValue("name"); v != "" {
		updates["name"] = v
	}
	if v := r.FormValue("current_age"); v != "" {
		i, err := strconv.Atoi(v)
		if err != nil {
			renderError(w, "Invalid age: must be an integer", http.StatusBadRequest)
			return
		}
		if i < 0 || i > 120 {
			renderError(w, "Age must be between 0 and 120", http.StatusBadRequest)
			return
		}
		updates["current_age"] = i
	}
	if v := r.FormValue("current_coverage"); v != "" {
		updates["current_coverage"] = v
	}
	if v := r.FormValue("current_monthly_cost"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			renderError(w, "Invalid monthly cost: must be a number", http.StatusBadRequest)
			return
		}
		if f < 0 {
			renderError(w, "Monthly cost cannot be negative", http.StatusBadRequest)
			return
		}
		updates["current_monthly_cost"] = f
	}
	if v := r.FormValue("pre_medicare_inflation"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			renderError(w, "Invalid pre-Medicare inflation: must be a number", http.StatusBadRequest)
			return
		}
		updates["pre_medicare_inflation"] = f
	}
	if v := r.FormValue("medicare_monthly_cost"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			renderError(w, "Invalid Medicare cost: must be a number", http.StatusBadRequest)
			return
		}
		if f < 0 {
			renderError(w, "Medicare cost cannot be negative", http.StatusBadRequest)
			return
		}
		updates["medicare_monthly_cost"] = f
	}
	if v := r.FormValue("post_medicare_inflation"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			renderError(w, "Invalid post-Medicare inflation: must be a number", http.StatusBadRequest)
			return
		}
		updates["post_medicare_inflation"] = f
	}
	if v := r.FormValue("employer_coverage_years"); v != "" {
		i, err := strconv.Atoi(v)
		if err != nil {
			renderError(w, "Invalid employer coverage years: must be an integer", http.StatusBadRequest)
			return
		}
		if i < 0 {
			renderError(w, "Employer coverage years cannot be negative", http.StatusBadRequest)
			return
		}
		updates["employer_coverage_years"] = i
	}
	if v := r.FormValue("aca_cost_after_employer"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			renderError(w, "Invalid ACA cost: must be a number", http.StatusBadRequest)
			return
		}
		if f < 0 {
			renderError(w, "ACA cost cannot be negative", http.StatusBadRequest)
			return
		}
		updates["aca_cost_after_employer"] = f
	}

	settings, err := retirementMgr.UpdateHealthcarePerson(id, updates)
	if err != nil {
		renderError(w, "Failed to update healthcare person: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

func handleWhatIfDeleteHealthcare(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RemoveHealthcarePerson(id)
	if err != nil {
		renderError(w, "Failed to remove healthcare person: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

// handleWhatIfSpendingPhases handles updates to spending phase configuration
func handleWhatIfSpendingPhases(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Parse enabled toggle
	enabled := r.FormValue("enabled") == "on" || r.FormValue("enabled") == "true"

	// Load current settings to get existing phases as base
	currentSettings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Use current phases or defaults
	basePhases := models.DefaultSpendingPhases()
	if currentSettings.SpendingPhaseConfig != nil && len(currentSettings.SpendingPhaseConfig.Phases) > 0 {
		basePhases = currentSettings.SpendingPhaseConfig.Phases
	}

	// Build phases from form data - support any number of phases
	phases := []models.SpendingPhase{}
	for i := 0; i < 20; i++ { // Support up to 20 phases (more than enough)
		// Check if this phase exists in form data
		nameKey := fmt.Sprintf("phase_%d_name", i)
		multKey := fmt.Sprintf("phase_%d_multiplier", i)

		// If no multiplier field, phase doesn't exist
		if r.FormValue(multKey) == "" {
			// Use base phase if available
			if i < len(basePhases) {
				phase := basePhases[i]
				// Parse start age if provided
				if startAge, err := parseFormInt(r, fmt.Sprintf("phase_%d_start_age", i)); err == nil && startAge > 0 {
					phase.StartAge = startAge
				}
				phases = append(phases, phase)
			}
			continue
		}

		// Create phase from form data
		phase := models.SpendingPhase{}
		if i < len(basePhases) {
			phase = basePhases[i] // Start with base values
		}

		// Parse name if provided
		if name := r.FormValue(nameKey); name != "" {
			phase.Name = name
		}

		// Parse start age if provided (skip for phase 0 which always starts at 0)
		if i > 0 {
			if startAge, err := parseFormInt(r, fmt.Sprintf("phase_%d_start_age", i)); err == nil {
				phase.StartAge = startAge
			}
		}

		// Parse multiplier
		if mult, err := parseFormFloat(r, multKey); err == nil {
			phase.Multiplier = mult
		}

		// Parse description if provided
		if desc := r.FormValue(fmt.Sprintf("phase_%d_description", i)); desc != "" {
			phase.Description = desc
		}

		phases = append(phases, phase)
	}

	settings, err := retirementMgr.UpdateSpendingPhases(enabled, phases)
	if err != nil {
		renderError(w, "Failed to save spending phases: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

// handleWhatIfAddPhase adds a new spending phase
func handleWhatIfAddPhase(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Initialize phases if needed
	if settings.SpendingPhaseConfig == nil {
		settings.SpendingPhaseConfig = &models.SpendingPhaseConfig{
			Enabled: true,
			Phases:  models.DefaultSpendingPhases(),
		}
	}

	// Find the last phase to determine new phase values
	phases := settings.SpendingPhaseConfig.Phases
	lastPhase := phases[len(phases)-1]

	// Create new phase 5 years after the last one, with 5% lower multiplier
	newMultiplier := lastPhase.Multiplier - 0.05
	if newMultiplier < 0.30 {
		newMultiplier = 0.30 // Floor at 30%
	}

	newPhase := models.SpendingPhase{
		Name:        fmt.Sprintf("Phase %d", len(phases)+1),
		StartAge:    lastPhase.StartAge + 5,
		Multiplier:  newMultiplier,
		Description: "Custom spending phase",
	}

	settings.SpendingPhaseConfig.Phases = append(settings.SpendingPhaseConfig.Phases, newPhase)

	if err := retirementMgr.Save(settings); err != nil {
		renderError(w, "Failed to save settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

// handleWhatIfDeletePhase removes a spending phase by index
func handleWhatIfDeletePhase(w http.ResponseWriter, r *http.Request) {
	indexStr := chi.URLParam(r, "index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		renderError(w, "Invalid phase index", http.StatusBadRequest)
		return
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if settings.SpendingPhaseConfig == nil || len(settings.SpendingPhaseConfig.Phases) == 0 {
		renderError(w, "No phases to delete", http.StatusBadRequest)
		return
	}

	// Don't allow deleting the first phase or below minimum
	if index == 0 {
		renderError(w, "Cannot delete the first phase", http.StatusBadRequest)
		return
	}

	phases := settings.SpendingPhaseConfig.Phases
	if index < 0 || index >= len(phases) {
		renderError(w, "Phase index out of range", http.StatusBadRequest)
		return
	}

	// Minimum 2 phases
	if len(phases) <= 2 {
		renderError(w, "Must have at least 2 phases", http.StatusBadRequest)
		return
	}

	// Remove the phase at index
	settings.SpendingPhaseConfig.Phases = append(phases[:index], phases[index+1:]...)

	if err := retirementMgr.Save(settings); err != nil {
		renderError(w, "Failed to save settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

// handleWhatIfResetPhases resets phases to defaults
func handleWhatIfResetPhases(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Reset to default phases
	settings.SpendingPhaseConfig = &models.SpendingPhaseConfig{
		Enabled: settings.SpendingPhaseConfig != nil && settings.SpendingPhaseConfig.Enabled,
		Phases:  models.DefaultSpendingPhases(),
	}

	if err := retirementMgr.Save(settings); err != nil {
		renderError(w, "Failed to save settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

// syncSettingsFromDashboard updates settings with values from dashboard data
func syncSettingsFromDashboard(settings *models.WhatIfSettings) error {
	data, err := loader.LoadData()
	if err != nil {
		return err
	}

	// Calculate average monthly values from last 12 months
	now := time.Now()
	yearAgo := now.AddDate(-1, 0, 0)
	filtered := data.FilterByDateRange(yearAgo, now)
	outflows := filtered.FilterByType(models.Outflow)

	months := 12.0
	if filtered.MinDate().After(yearAgo) {
		months = now.Sub(filtered.MinDate()).Hours() / 24 / 30
		if months < 1 {
			months = 1
		}
	}

	// Calculate and set average monthly expenses
	totalExpenses := outflows.SumAbsAmount()
	settings.MonthlyLivingExpenses = totalExpenses / months

	// Use insights income pattern detection for individual income sources
	incomePatterns := insights.AnalyzeIncomePatterns(filtered)

	// Remove old auto-detected sources (prefixed with "insights-" or old "dashboard-income")
	// Keep user-added sources (no special prefix)
	// BUT preserve user modifications (EndMonth, StartMonth, COLARate, Type) from existing insights sources
	userSources := make([]models.IncomeSource, 0)
	existingMods := make(map[string]models.IncomeSource)

	for _, src := range settings.IncomeSources {
		if strings.HasPrefix(src.ID, "insights-") || src.ID == "dashboard-income" {
			// Save user modifications for this auto-detected source
			existingMods[src.ID] = src
		} else {
			userSources = append(userSources, src)
		}
	}

	// Convert detected income patterns to income sources
	for _, pattern := range incomePatterns {
		// Only include regular income patterns (skip one-time or irregular)
		if !pattern.IsRegular {
			continue
		}

		// Convert to monthly amount based on frequency
		monthlyAmount := pattern.AvgAmount
		switch pattern.Frequency {
		case "weekly":
			monthlyAmount = pattern.AvgAmount * 52 / 12
		case "biweekly":
			monthlyAmount = pattern.AvgAmount * 26 / 12
			// monthly is already correct
		}

		// Create a stable ID from the description
		id := "insights-" + strings.ToLower(strings.ReplaceAll(pattern.Description, " ", "-"))

		newSource := models.IncomeSource{
			ID:     id,
			Name:   strings.Title(pattern.Description),
			Amount: monthlyAmount,
			Type:   models.IncomeFixed,
		}

		// Preserve user modifications from existing source with same ID
		if existing, ok := existingMods[id]; ok {
			newSource.EndMonth = existing.EndMonth
			newSource.StartMonth = existing.StartMonth
			newSource.COLARate = existing.COLARate
			newSource.InflationAdjusted = existing.InflationAdjusted
			// Preserve Type only if user changed it from default
			if existing.Type != "" && existing.Type != models.IncomeFixed {
				newSource.Type = existing.Type
			}
		}

		userSources = append(userSources, newSource)
	}

	settings.IncomeSources = userSources

	return nil
}

// handleWhatIfRothConversion handles Roth conversion configuration updates
func handleWhatIfRothConversion(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	settings, err := retirementMgr.Load()
	if err != nil {
		renderError(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Initialize RothConversion if nil
	if settings.RothConversion == nil {
		settings.RothConversion = &models.RothConversionConfig{}
	}

	// Parse enabled checkbox (unchecked means not present in form)
	settings.RothConversion.Enabled = r.FormValue("enabled") == "on"

	// Parse numeric fields
	if amount, err := parseFormFloat(r, "annual_amount"); err == nil {
		settings.RothConversion.AnnualAmount = amount
	}

	if startYear, err := parseFormInt(r, "start_year"); err == nil {
		settings.RothConversion.StartYear = startYear
	}

	if endYear, err := parseFormInt(r, "end_year"); err == nil {
		settings.RothConversion.EndYear = endYear
	}

	// Save settings
	if err := retirementMgr.Save(settings); err != nil {
		renderError(w, "Failed to save settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Run analysis and return results (cache auto-invalidates on settings hash change)
	calc := retirement.NewCalculator(settings)
	analysis := calc.RunFullAnalysis()

	partialData := &models.WhatIfPageData{
		Title:    "What-If Analysis",
		Settings: settings,
		Analysis: analysis,
	}

	if renderer != nil {
		renderer.RenderPartial(w, "whatif-results", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleWhatIfAddBigTicket(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "Invalid form data: "+err.Error(), http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		renderError(w, "Big ticket item name is required", http.StatusBadRequest)
		return
	}

	amount, err := parseRequiredFormFloat(r, "amount")
	if err != nil {
		renderError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if amount < 0 {
		renderError(w, "Amount cannot be negative", http.StatusBadRequest)
		return
	}

	year, err := parseFormInt(r, "year")
	if err != nil {
		year = 0
	}
	if year < 0 {
		year = 0
	}

	itemType := models.BigTicketType(r.FormValue("type"))
	if itemType != models.BigTicketIncome && itemType != models.BigTicketExpense {
		itemType = models.BigTicketExpense
	}

	taxTreatment := models.TaxTreatment(r.FormValue("tax_treatment"))
	if taxTreatment != models.TaxNone && taxTreatment != models.TaxOrdinary && taxTreatment != models.TaxCapGains {
		taxTreatment = models.TaxNone
	}

	notes := r.FormValue("notes")

	item := models.BigTicketItem{
		ID:           uuid.New().String(),
		Name:         name,
		Amount:       amount,
		Year:         year,
		Type:         itemType,
		TaxTreatment: taxTreatment,
		Notes:        notes,
	}

	settings, err := retirementMgr.AddBigTicketItem(item)
	if err != nil {
		renderError(w, "Failed to add big ticket item: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

func handleWhatIfDeleteBigTicket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RemoveBigTicketItem(id)
	if err != nil {
		renderError(w, "Failed to remove big ticket item: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

func handleWhatIfRestoreBigTicket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RestoreBigTicketItem(id)
	if err != nil {
		renderError(w, "Failed to restore big ticket item: "+err.Error(), http.StatusInternalServerError)
		return
	}

	analysis := runAnalysisWithCache(settings)

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

func handleListScenarios(w http.ResponseWriter, r *http.Request) {
	scenarios, err := retirementMgr.ListScenarios()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scenarios)
}

func handleCreateScenario(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if name == "" {
		renderError(w, "Scenario name is required", http.StatusBadRequest)
		return
	}

	if _, err := retirementMgr.CreateScenario(name); err != nil {
		renderError(w, "Failed to create scenario: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// HTMX redirect for full page reload
	w.Header().Set("HX-Redirect", "/whatif")
	w.WriteHeader(http.StatusOK)
}

func handleSwitchScenario(w http.ResponseWriter, r *http.Request) {
	filename := r.FormValue("filename")
	if filename == "" {
		renderError(w, "Scenario filename is required", http.StatusBadRequest)
		return
	}

	if err := retirementMgr.SwitchScenario(filename); err != nil {
		renderError(w, "Failed to switch scenario: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// HTMX redirect for full page reload
	w.Header().Set("HX-Redirect", "/whatif")
	w.WriteHeader(http.StatusOK)
}

func handleDeleteScenario(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if filename == "" {
		renderError(w, "Scenario filename is required", http.StatusBadRequest)
		return
	}

	if err := retirementMgr.DeleteScenario(filename); err != nil {
		renderError(w, "Failed to delete scenario: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// HTMX redirect for full page reload
	w.Header().Set("HX-Redirect", "/whatif")
	w.WriteHeader(http.StatusOK)
}

func handleRenameScenario(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if filename == "" {
		renderError(w, "Scenario filename is required", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		renderError(w, "New scenario name is required", http.StatusBadRequest)
		return
	}

	if err := retirementMgr.RenameScenario(filename, name); err != nil {
		renderError(w, "Failed to rename scenario: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// HTMX redirect for full page reload
	w.Header().Set("HX-Redirect", "/whatif")
	w.WriteHeader(http.StatusOK)
}
