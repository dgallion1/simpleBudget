package whatif

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"budget2/internal/handlers/insights"
	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/retirement"
	"budget2/internal/templates"
)

var (
	loader       *dataloader.DataLoader
	renderer     *templates.Renderer
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
	r.Get("/whatif/chart/projection", handleWhatIfProjectionChart)
	r.Post("/whatif/sync", handleWhatIfSync)
	r.Post("/whatif/montecarlo", handleWhatIfMonteCarlo)
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
	if v := r.FormValue("current_age"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			updates["current_age"] = i
		}
	}
	if v := r.FormValue("tax_deferred_percent"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["tax_deferred_percent"] = f
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
	if v := r.FormValue("steady_state_override_year"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["steady_state_override_year"] = f
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

func handleWhatIfUpdateIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	startYear, _ := strconv.Atoi(r.FormValue("start_year"))
	endYear, _ := strconv.Atoi(r.FormValue("end_year"))
	cola := r.FormValue("cola") == "on" || r.FormValue("cola") == "true"

	colaRate := 0.0
	if cola {
		colaRate = 0.02 // 2% COLA
	}

	settings, err := retirementMgr.UpdateIncomeSource(id, startYear, endYear, colaRate)
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

func handleWhatIfRestoreIncome(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RestoreIncomeSource(id)
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

func handleWhatIfUpdateExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	startYear, _ := strconv.Atoi(r.FormValue("start_year"))
	endYear, _ := strconv.Atoi(r.FormValue("end_year"))
	inflation := r.FormValue("inflation") == "on" || r.FormValue("inflation") == "true"

	settings, err := retirementMgr.UpdateExpenseSource(id, startYear, endYear, inflation)
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

func handleWhatIfRestoreExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RestoreExpenseSource(id)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync expenses and income from dashboard
	if err := syncSettingsFromDashboard(settings); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Save the synced settings
	if err := retirementMgr.Save(settings); err != nil {
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

func handleWhatIfMonteCarlo(w http.ResponseWriter, r *http.Request) {
	settings, err := retirementMgr.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	age, _ := strconv.Atoi(r.FormValue("current_age"))
	coverageType := r.FormValue("current_coverage")
	monthlyCost, _ := strconv.ParseFloat(r.FormValue("current_monthly_cost"), 64)
	preMedicareInflation, _ := strconv.ParseFloat(r.FormValue("pre_medicare_inflation"), 64)
	medicareCost, _ := strconv.ParseFloat(r.FormValue("medicare_monthly_cost"), 64)
	postMedicareInflation, _ := strconv.ParseFloat(r.FormValue("post_medicare_inflation"), 64)

	// Set defaults if not provided
	if name == "" {
		name = "Person"
	}
	if age == 0 {
		age = 65
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

func handleWhatIfUpdateHealthcare(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	updates := make(map[string]interface{})

	if v := r.FormValue("name"); v != "" {
		updates["name"] = v
	}
	if v := r.FormValue("current_age"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			updates["current_age"] = i
		}
	}
	if v := r.FormValue("current_coverage"); v != "" {
		updates["current_coverage"] = v
	}
	if v := r.FormValue("current_monthly_cost"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["current_monthly_cost"] = f
		}
	}
	if v := r.FormValue("pre_medicare_inflation"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["pre_medicare_inflation"] = f
		}
	}
	if v := r.FormValue("medicare_monthly_cost"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["medicare_monthly_cost"] = f
		}
	}
	if v := r.FormValue("post_medicare_inflation"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["post_medicare_inflation"] = f
		}
	}
	if v := r.FormValue("employer_coverage_years"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			updates["employer_coverage_years"] = i
		}
	}
	if v := r.FormValue("aca_cost_after_employer"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			updates["aca_cost_after_employer"] = f
		}
	}

	settings, err := retirementMgr.UpdateHealthcarePerson(id, updates)
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

func handleWhatIfDeleteHealthcare(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	settings, err := retirementMgr.RemoveHealthcarePerson(id)
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
