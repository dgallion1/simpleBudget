package whatif

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

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

func statusForWhatIfSaveError(err error) int {
	var chainErr *retirement.ScenarioChainValidationError
	if errors.As(err, &chainErr) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func statusForScenarioOperationError(err error) int {
	var validationErr *retirement.ScenarioValidationError
	if errors.As(err, &validationErr) {
		return http.StatusBadRequest
	}

	var notFoundErr *retirement.ScenarioNotFoundError
	if errors.As(err, &notFoundErr) {
		return http.StatusNotFound
	}

	var conflictErr *retirement.ScenarioConflictError
	if errors.As(err, &conflictErr) {
		return http.StatusConflict
	}

	return http.StatusInternalServerError
}

// getSettingsHash generates a hash of the settings for cache key
func getSettingsHash(settings *models.WhatIfSettings) string {
	data, err := json.Marshal(settings)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:8]) // Use first 8 bytes for shorter key
}

// buildCalculator creates a chain-aware calculator from settings.
func buildCalculator(settings *models.WhatIfSettings) (*retirement.Calculator, string, error) {
	hashData := getSettingsHash(settings)

	if len(settings.ScenarioChain) == 0 {
		return retirement.NewCalculator(settings), hashData, nil
	}

	chain := make([]retirement.ResolvedScenarioChainLink, 0, len(settings.ScenarioChain))
	for _, link := range settings.ScenarioChain {
		linked, err := retirementMgr.LoadScenarioSettings(link.ScenarioFilename)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load chained scenario %s: %w", link.ScenarioFilename, err)
		}

		linkedHash := getSettingsHash(linked)
		hashData += linkedHash

		chain = append(chain, retirement.ResolvedScenarioChainLink{
			ScenarioFilename: link.ScenarioFilename,
			TransitionAge:    link.TransitionAge,
			Settings:         linked,
		})
	}

	combined := sha256.Sum256([]byte(hashData))
	combinedHash := fmt.Sprintf("%x", combined[:8])

	return retirement.NewCalculatorWithChain(settings, chain), combinedHash, nil
}

// runAnalysisWithCache runs full analysis, using cache when available
func runAnalysisWithCache(settings *models.WhatIfSettings) (*models.WhatIfAnalysis, error) {
	calc, depHash, err := buildCalculator(settings)
	if err != nil {
		return nil, err
	}

	cache.mu.RLock()
	if cache.hash == depHash && time.Since(cache.cachedAt) < 5*time.Minute {
		cached := cache.analysis
		cache.mu.RUnlock()
		return cached, nil
	}
	cache.mu.RUnlock()

	analysis := calc.RunFullAnalysis()

	cache.mu.Lock()
	cache.hash = depHash
	cache.analysis = analysis
	cache.cachedAt = time.Now()
	cache.mu.Unlock()

	return analysis, nil
}

func normalizeDisplayDollars(raw string) string {
	if raw == "real" {
		return "real"
	}
	return "nominal"
}

type projectionChartEvent struct {
	Year  float64
	Label string
}

func projectionValueAtYear(projection *models.ProjectionResult, year float64, displayDollars string) float64 {
	if projection == nil || len(projection.Months) == 0 {
		return 0
	}

	index := int(math.Round(year * 12))
	if index < 0 {
		index = 0
	}
	if index >= len(projection.Months) {
		index = len(projection.Months) - 1
	}

	month := projection.Months[index]
	if displayDollars == "real" {
		return month.PortfolioBalanceReal
	}
	return month.PortfolioBalance
}

func humanizeScenarioFilename(filename string) string {
	name := strings.TrimSuffix(filename, ".json")
	name = strings.NewReplacer("-", " ", "_", " ").Replace(name)
	return cases.Title(language.English).String(name)
}

func buildProjectionChartEvents(settings *models.WhatIfSettings, projection *models.ProjectionResult) []projectionChartEvent {
	if settings == nil || projection == nil {
		return nil
	}

	maxYear := 0.0
	if len(projection.Months) > 0 {
		maxYear = projection.Months[len(projection.Months)-1].Year
	}

	events := make([]projectionChartEvent, 0, 8)
	seen := map[string]struct{}{}
	appendEvent := func(year float64, label string) {
		if year <= 0 || year > maxYear {
			return
		}
		key := fmt.Sprintf("%.2f:%s", year, label)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		events = append(events, projectionChartEvent{Year: year, Label: label})
	}

	for _, link := range settings.ScenarioChain {
		appendEvent(float64(link.TransitionAge-settings.CurrentAge), "Scenario: "+humanizeScenarioFilename(link.ScenarioFilename))
	}

	for _, source := range settings.IncomeSources {
		year := float64(source.StartMonth) / 12.0
		lowerName := strings.ToLower(source.Name)
		switch {
		case strings.Contains(lowerName, "social security"):
			appendEvent(year, "Social Security starts")
		case strings.Contains(lowerName, "pension"):
			appendEvent(year, "Pension starts")
		}
	}

	for _, person := range settings.HealthcarePersons {
		if hasTransition, yearsUntil, _, _ := person.GetTransitionInfo(); hasTransition {
			appendEvent(float64(yearsUntil), fmt.Sprintf("Medicare: %s", person.Name))
		}
	}

	olderAge := settings.GetOlderAge()
	if olderAge < retirement.RMDStartAge {
		appendEvent(float64(retirement.RMDStartAge-olderAge), "RMD starts")
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].Year == events[j].Year {
			return events[i].Label < events[j].Label
		}
		return events[i].Year < events[j].Year
	})

	return events
}

func buildProjectionChartData(settings *models.WhatIfSettings, projection *models.ProjectionResult, displayDollars string) map[string]interface{} {
	displayDollars = normalizeDisplayDollars(displayDollars)
	if projection == nil {
		projection = &models.ProjectionResult{}
	}

	years := make([]float64, 0)
	balances := make([]float64, 0)
	maxBalance := 0.0
	for _, m := range projection.Months {
		years = append(years, m.Year)
		balance := m.PortfolioBalance
		if displayDollars == "real" {
			balance = m.PortfolioBalanceReal
		}
		balances = append(balances, balance)
		if balance > maxBalance {
			maxBalance = balance
		}
	}

	fillColor := "rgba(34, 197, 94, 0.3)"
	lineColor := "#22c55e"
	if !projection.Survives {
		fillColor = "rgba(239, 68, 68, 0.3)"
		lineColor = "#ef4444"
	}

	traces := []map[string]interface{}{
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
	}

	events := buildProjectionChartEvents(settings, projection)
	if len(events) > 0 {
		eventX := make([]float64, 0, len(events))
		eventY := make([]float64, 0, len(events))
		eventText := make([]string, 0, len(events))
		yearOffsets := map[float64]int{}
		for _, event := range events {
			offsetCount := yearOffsets[event.Year]
			yearOffsets[event.Year] = offsetCount + 1
			y := projectionValueAtYear(projection, event.Year, displayDollars)
			if y <= 0 {
				y = maxBalance * 0.05
			} else {
				y *= 1.02 + 0.05*float64(offsetCount)
			}
			eventX = append(eventX, event.Year)
			eventY = append(eventY, y)
			eventText = append(eventText, event.Label)
		}

		traces = append(traces, map[string]interface{}{
			"type":         "scatter",
			"mode":         "markers+text",
			"name":         "Key events",
			"x":            eventX,
			"y":            eventY,
			"text":         eventText,
			"textposition": "top center",
			"marker": map[string]interface{}{
				"color":  "#f59e0b",
				"size":   9,
				"symbol": "diamond",
			},
			"hoverinfo": "skip",
		})
	}

	dtick := 5
	if settings != nil && settings.ProjectionYears <= 12 {
		dtick = 1
	} else if settings != nil && settings.ProjectionYears <= 24 {
		dtick = 2
	}

	yAxisTitle := "Balance ($)"
	title := "Portfolio Projection"
	if displayDollars == "real" {
		yAxisTitle = "Balance (Today's Dollars)"
		title = "Portfolio Projection In Today's Dollars"
	}

	return map[string]interface{}{
		"data": traces,
		"layout": map[string]interface{}{
			"title": title,
			"xaxis": map[string]interface{}{
				"title":    "Years",
				"tickmode": "linear",
				"tick0":    0,
				"dtick":    dtick,
			},
			"yaxis": map[string]interface{}{
				"title":      yAxisTitle,
				"tickformat": "$,.0f",
			},
			"legend": map[string]interface{}{
				"orientation": "h",
			},
		},
	}
}

// renderError renders an HTML error fragment for HTMX requests
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

func renderRetargetedError(w http.ResponseWriter, message string, statusCode int, target string) {
	w.Header().Set("HX-Retarget", target)
	w.Header().Set("HX-Reswap", "innerHTML")
	renderError(w, message, statusCode)
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

func parseSSClaimAge(r *http.Request, key string) int {
	age, err := parseFormInt(r, key)
	if err != nil || age < 62 || age > 70 {
		return 0
	}
	return age
}

func formValues(r *http.Request, key string) []string {
	if values, ok := r.Form[key+"[]"]; ok {
		return values
	}
	if values, ok := r.Form[key]; ok {
		return values
	}
	return nil
}

func formHasKey(r *http.Request, key string) bool {
	if _, ok := r.Form[key]; ok {
		return true
	}
	if _, ok := r.Form[key+"[]"]; ok {
		return true
	}
	return false
}

func parsePersonsForm(r *http.Request) (string, []models.Person, bool, error) {
	startDate := strings.TrimSpace(r.FormValue("start_date"))
	personIDs := formValues(r, "person_id")
	names := formValues(r, "person_name")
	birthMonths := formValues(r, "person_birth_month")
	roles := formValues(r, "person_role")

	hasPersons := startDate != "" || len(personIDs) > 0 || len(names) > 0 || len(birthMonths) > 0 || len(roles) > 0
	if !hasPersons {
		return "", nil, false, nil
	}

	rowCount := len(names)
	switch {
	case rowCount == 0:
		return "", nil, true, fmt.Errorf("at least one person is required")
	case len(personIDs) != rowCount, len(birthMonths) != rowCount, len(roles) != rowCount:
		return "", nil, true, fmt.Errorf("person rows are misaligned")
	}

	persons := make([]models.Person, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		id := strings.TrimSpace(personIDs[i])
		if id == "" {
			id = uuid.New().String()
		}
		role := models.PersonRole(strings.TrimSpace(roles[i]))
		switch role {
		case models.PersonRolePrimary, models.PersonRoleSpouse, models.PersonRoleOther:
		default:
			return "", nil, true, fmt.Errorf("invalid role for person row %d", i+1)
		}

		persons = append(persons, models.Person{
			ID:         id,
			Name:       strings.TrimSpace(names[i]),
			BirthMonth: strings.TrimSpace(birthMonths[i]),
			Role:       role,
		})
	}

	return startDate, persons, true, nil
}

func findHealthcarePerson(settings *models.WhatIfSettings, id string) *models.HealthcarePerson {
	for i := range settings.HealthcarePersons {
		if settings.HealthcarePersons[i].ID == id {
			return &settings.HealthcarePersons[i]
		}
	}
	return nil
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
	r.Delete("/whatif/income/{id}/purge", handleWhatIfPurgeIncome)
	r.Post("/whatif/expense", handleWhatIfAddExpense)
	r.Put("/whatif/expense/{id}", handleWhatIfUpdateExpense)
	r.Delete("/whatif/expense/{id}", handleWhatIfDeleteExpense)
	r.Post("/whatif/expense/{id}/restore", handleWhatIfRestoreExpense)
	r.Delete("/whatif/expense/{id}/purge", handleWhatIfPurgeExpense)
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
	r.Delete("/whatif/bigticket/{id}/purge", handleWhatIfPurgeBigTicket)
	r.Get("/whatif/scenarios", handleListScenarios)
	r.Post("/whatif/scenarios", handleCreateScenario)
	r.Post("/whatif/scenarios/switch", handleSwitchScenario)
	r.Delete("/whatif/scenarios/{filename}", handleDeleteScenario)
	r.Put("/whatif/scenarios/{filename}", handleRenameScenario)
	r.Post("/whatif/chain", handleWhatIfUpdateChain)
	r.Delete("/whatif/chain/{index}", handleWhatIfDeleteChainLink)
	r.Post("/whatif/social-security", handleWhatIfSocialSecurity)
	r.Post("/whatif/glide-path", handleWhatIfGlidePath)
	r.Post("/whatif/guardrails", handleWhatIfGuardrails)
}

func handleWhatIf(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		log.Printf("Error loading what-if settings: %v", err)
		settings = models.DefaultWhatIfSettings()
	}

	// Run full analysis (with caching)
	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

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

	templates.AttachDuplicateCount(pageData, loader)
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

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

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

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	displayDollars := normalizeDisplayDollars(r.URL.Query().Get("display_dollars"))
	chartData := buildProjectionChartData(settings, analysis.Projection, displayDollars)

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

	analysis, err := runAnalysisWithCache(settings)
	if err != nil {
		renderError(w, "Analysis failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

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
	filtered := data.Active().FilterByDateRange(yearAgo, now)
	outflows := filtered.FilterByType(models.Outflow)

	months := 12.0
	if filtered.MinDate().After(yearAgo) {
		months = now.Sub(filtered.MinDate()).Hours() / 24 / 30
		if months < 1 {
			months = 1
		}
	}

	// Calculate and set average monthly expenses.
	// Signed sum + math.Abs so refunds reduce the total instead of inflating it.
	totalExpenses := math.Abs(outflows.SumAmount())
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
			Name:   cases.Title(language.English).String(pattern.Description),
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
