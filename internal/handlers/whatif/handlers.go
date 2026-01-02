package whatif

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
	r.Get("/whatif/chart/projection", handleWhatIfProjectionChart)
	r.Post("/whatif/sync", handleWhatIfSync)
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
	income := filtered.FilterByType(models.Income)

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

	// Calculate and set average monthly income - merge instead of replace
	totalIncome := income.SumAmount()
	avgMonthlyIncome := totalIncome / months
	if avgMonthlyIncome > 0 {
		// Find and update existing dashboard-income, or add it
		found := false
		for i := range settings.IncomeSources {
			if settings.IncomeSources[i].ID == "dashboard-income" {
				settings.IncomeSources[i].Amount = avgMonthlyIncome
				found = true
				break
			}
		}
		if !found {
			settings.IncomeSources = append(settings.IncomeSources, models.IncomeSource{
				ID:     "dashboard-income",
				Name:   "Current Income",
				Amount: avgMonthlyIncome,
				Type:   models.IncomeFixed,
			})
		}
	}

	return nil
}
