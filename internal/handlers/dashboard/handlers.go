// Package dashboard serves the main landing page and its HTMX-driven
// partials: KPI tiles, budget tracking, monthly-variance and healthcare
// charts, and the income-vs-expense rollups. Reads transactions from the
// dataloader and renders via the templates package; holds no state of
// its own beyond the per-request request scope.
package dashboard

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"net/http"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/majorexpenses"
	"budget2/internal/services/metrics"
	"budget2/internal/services/retirement"
	"budget2/internal/services/storage"
	"budget2/internal/templates"
)

var (
	loader        *dataloader.DataLoader
	renderer      *templates.Renderer
	retirementMgr *retirement.SettingsManager
	store         *storage.Storage
)

// Initialize sets up the dashboard package with required dependencies. The
// storage service is needed to read the accounts sidecar for the accounts
// card and the projection summary line (A8); a nil store is tolerated --
// buildAccountsCard returns an empty card -- so existing tests that wire
// only a loader still pass.
func Initialize(l *dataloader.DataLoader, r *templates.Renderer, rm *retirement.SettingsManager, s *storage.Storage) {
	loader = l
	renderer = r
	retirementMgr = rm
	store = s
}

// currentBudgetSettings returns the active what-if settings used to
// derive the dashboard budget target, or nil if the settings manager
// is unset or the load fails. The dashboard treats a missing settings
// (or zero MonthlyLivingExpenses) as "no budget configured" and renders
// the fallback card; loading errors are non-fatal.
func currentBudgetSettings() *models.WhatIfSettings {
	if retirementMgr == nil {
		return nil
	}
	settings, err := retirementMgr.Load()
	if err != nil {
		return nil
	}
	return settings
}

// RegisterRoutes registers all dashboard routes
func RegisterRoutes(r chi.Router) {
	r.Get("/dashboard", handleDashboard)
	r.Get("/dashboard/kpis", handleKPIsPartial)
	r.Get("/dashboard/charts/data/{chartType}", handleChartData)
	r.Get("/dashboard/major-expense", handleMajorExpenseDrilldown)
	r.Get("/dashboard/kpi/{kpiType}", handleKPIDetail)
	r.Get("/dashboard/kpi/{kpiType}/export", handleKPIExport)
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadDataContext(r.Context())
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

	startDate, endDate := resolveDateRange(startStr, endStr, minDate, maxDate)

	filtered := data.Active().FilterByDateRange(startDate, endDate)
	settings := currentBudgetSettings()
	target, healthTarget := metrics.BudgetTargets(settings, startDate, endDate)
	dashMetrics := metrics.Calculate(filtered, startDate, endDate, target, healthTarget)

	// Calculate period comparison if requested
	var periodComparison *models.PeriodComparison
	if comparison != "" {
		periodComparison = metrics.Comparison(data.Active(), startDate, endDate, comparison, settings)
	}

	// Accounts card (A8): per-account balance, freshness, low/stale/no-anchor
	// flags, and a checking/savings projection summary line. Uses the loader's
	// maxDate as the "as of" date so "data through Aug 12" and the balance are
	// consistent with what the rest of the dashboard shows. Reuses
	// accounts.BalanceAt / Freshness / Project; adds no balance or projection
	// logic of its own.
	asOf := maxDate
	if asOf.IsZero() {
		asOf = time.Now()
	}
	accountsCard := buildAccountsCard(store, data.Active().Transactions, asOf, detectRecurringForDashboard(data, asOf))

	// Unassigned-files banner (A8): surfaces how many transactions came from
	// CSVs matching no account, linking to /accounts. Files are never
	// silently dropped; this banner is what makes that visible. Read from
	// the loader's most recent load count (the same figure the accounts page
	// derives its unassigned file list from).
	unassignedCount := 0
	if loader != nil {
		unassignedCount = loader.UnassignedCount()
	}

	pageData := map[string]interface{}{
		"Title":            "Dashboard",
		"ActiveTab":        "dashboard",
		"Metrics":          dashMetrics,
		"BudgetVerdict":    BuildBudgetVerdict(dashMetrics),
		"PeriodComparison": periodComparison,
		"StartDate":        startDate.Format("2006-01-02"),
		"EndDate":          endDate.Format("2006-01-02"),
		"MinDate":          minDate.Format("2006-01-02"),
		"MaxDate":          maxDate.Format("2006-01-02"),
		"Comparison":       comparison,
		"AccountsCard":     accountsCard,
		"UnassignedCount":  unassignedCount,
	}

	templates.AttachDuplicateCount(pageData, loader)
	if renderer != nil {
		_ = renderer.Render(w, "base", pageData)
	} else {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><h1>Dashboard</h1><p>Templates not loaded. Check configuration.</p></body></html>"))
	}
}

func handleKPIsPartial(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadDataContext(r.Context())
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

	filtered := data.Active().FilterByDateRange(startDate, endDate)
	settings := currentBudgetSettings()
	target, healthTarget := metrics.BudgetTargets(settings, startDate, endDate)
	dashMetrics := metrics.Calculate(filtered, startDate, endDate, target, healthTarget)

	var periodComparison *models.PeriodComparison
	if comparison != "" {
		periodComparison = metrics.Comparison(data.Active(), startDate, endDate, comparison, settings)
	}

	partialData := map[string]interface{}{
		"Metrics":          dashMetrics,
		"BudgetVerdict":    BuildBudgetVerdict(dashMetrics),
		"PeriodComparison": periodComparison,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "kpis", partialData)
	} else if err := json.NewEncoder(w).Encode(partialData); err != nil {
		log.Printf("dashboard: encoding kpis JSON: %v", err)
	}
}

func handleChartData(w http.ResponseWriter, r *http.Request) {
	chartType := chi.URLParam(r, "chartType")

	data, err := loader.LoadDataContext(r.Context())
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

	filtered := data.Active().FilterByDateRange(startDate, endDate)

	var chartData interface{}

	switch chartType {
	case "major-expense":
		chartData = buildMajorExpenseChartData(filtered)
	case "spending-trend":
		chartData = buildSpendingTrendChartData(filtered)
	case "merchants":
		chartData = buildMerchantsChartData(filtered)
	case "cumulative":
		chartData = buildCumulativeChartData(filtered)
	case "budget-vs-actual":
		settings := currentBudgetSettings()
		livingTarget, healthTarget := metrics.BudgetTargets(settings, startDate, endDate)
		chartData = buildBudgetVsActualChartData(filtered, startDate, endDate, livingTarget, healthTarget)
	default:
		http.Error(w, "Unknown chart type", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(chartData); err != nil {
		log.Printf("dashboard: encoding chart JSON: %v", err)
	}
}

// handleMajorExpenseDrilldown returns the transactions covered by a
// single wedge in the Spending by Major Expense donut. The wedge name
// is taken from the "name" query param (URL-safe; major expense names
// can include arbitrary user text). Two synthetic wedges are honored:
// "Unmatched" returns outflows that didn't match any major expense, and
// "Other" returns the rolled-up tail beyond the donut display limit.
func handleMajorExpenseDrilldown(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")

	data, err := loader.LoadDataContext(r.Context())
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

	filtered := data.Active().FilterByDateRange(startDate, endDate)
	buckets, unmatched := bucketMajorExpenses(filtered)

	var txns []models.Transaction
	switch name {
	case "Unmatched":
		txns = unmatched
	case "Other":
		if len(buckets) > majorExpenseDonutLimit {
			for _, b := range buckets[majorExpenseDonutLimit:] {
				txns = append(txns, b.txns...)
			}
		}
	default:
		for _, b := range buckets {
			if b.name == name {
				txns = b.txns
				break
			}
		}
	}

	sort.Slice(txns, func(i, j int) bool { return txns[i].Date.After(txns[j].Date) })

	var total float64
	for _, t := range txns {
		total += math.Abs(t.Amount)
	}
	count := len(txns)
	var avgAmount float64
	if count > 0 {
		avgAmount = total / float64(count)
	}

	partialData := map[string]interface{}{
		"Name":         name,
		"Transactions": txns,
		"Total":        total,
		"Count":        count,
		"AvgAmount":    avgAmount,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "major-expense-drilldown", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

func handleKPIDetail(w http.ResponseWriter, r *http.Request) {
	kpiType := chi.URLParam(r, "kpiType")

	data, err := loader.LoadDataContext(r.Context())
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

	filtered := data.Active().FilterByDateRange(startDate, endDate)
	income := filtered.FilterByType(models.Income)
	outflows := filtered.FilterByType(models.Outflow)

	// Group by month
	monthlyIncome := income.GroupByMonth()
	monthlyOutflows := outflows.GroupByMonth()

	// Collect all months
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

	// Calculate monthly summaries
	type MonthlyStat struct {
		Month    string
		Value    float64
		Income   float64
		Expenses float64
		Savings  float64
		Rate     float64
	}
	var monthlySummaries []MonthlyStat
	var values []float64

	for _, m := range months {
		incAmt := 0.0
		if inc, ok := monthlyIncome[m]; ok {
			incAmt = inc.SumAmount()
		}

		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = math.Abs(exp.SumAmount())
		}

		savings := incAmt - expAmt
		rate := 0.0
		if incAmt > 0 {
			rate = (savings / incAmt) * 100
		}

		var value float64
		switch kpiType {
		case "income":
			value = incAmt
		case "expenses":
			value = expAmt
		case "savings":
			value = savings
		case "savings-rate":
			value = rate
		}

		monthlySummaries = append(monthlySummaries, MonthlyStat{
			Month:    m,
			Value:    value,
			Income:   incAmt,
			Expenses: expAmt,
			Savings:  savings,
			Rate:     rate,
		})
		values = append(values, value)
	}

	// Calculate stats
	var sum, min, max, avg float64
	var minMonth, maxMonth string

	if len(values) > 0 {
		min = values[0]
		max = values[0]
		minMonth = monthlySummaries[0].Month
		maxMonth = monthlySummaries[0].Month

		for i, v := range values {
			sum += v
			if v < min {
				min = v
				minMonth = monthlySummaries[i].Month
			}
			if v > max {
				max = v
				maxMonth = monthlySummaries[i].Month
			}
		}
		avg = sum / float64(len(values))
	}

	// Calculate number of months for period breakdown
	numMonths := len(months)
	if numMonths == 0 {
		numMonths = 1
	}

	// Title based on type
	titles := map[string]string{
		"income":       "Total Income",
		"expenses":     "Total Expenses",
		"savings":      "Net Savings",
		"savings-rate": "Savings Rate",
	}

	partialData := map[string]interface{}{
		"Type":      kpiType,
		"Title":     titles[kpiType],
		"Monthly":   monthlySummaries,
		"Total":     sum,
		"Average":   avg,
		"Min":       min,
		"Max":       max,
		"MinMonth":  minMonth,
		"MaxMonth":  maxMonth,
		"NumMonths": numMonths,
		"IsRate":    kpiType == "savings-rate",
		"IsSavings": kpiType == "savings",
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "kpi-detail", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

func handleKPIExport(w http.ResponseWriter, r *http.Request) {
	kpiType := chi.URLParam(r, "kpiType")

	data, err := loader.LoadDataContext(r.Context())
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

	filtered := data.Active().FilterByDateRange(startDate, endDate)
	income := filtered.FilterByType(models.Income)
	outflows := filtered.FilterByType(models.Outflow)

	monthlyIncome := income.GroupByMonth()
	monthlyOutflows := outflows.GroupByMonth()

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

	// Build CSV
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Write header based on type
	switch kpiType {
	case "income":
		_ = writer.Write([]string{"Month", "Income"})
	case "expenses":
		_ = writer.Write([]string{"Month", "Expenses"})
	case "savings":
		_ = writer.Write([]string{"Month", "Income", "Expenses", "Savings"})
	case "savings-rate":
		_ = writer.Write([]string{"Month", "Income", "Expenses", "Savings", "Savings Rate %"})
	}

	for _, m := range months {
		incAmt := 0.0
		if inc, ok := monthlyIncome[m]; ok {
			incAmt = inc.SumAmount()
		}

		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = math.Abs(exp.SumAmount())
		}

		savings := incAmt - expAmt
		rate := 0.0
		if incAmt > 0 {
			rate = (savings / incAmt) * 100
		}

		switch kpiType {
		case "income":
			_ = writer.Write([]string{m, fmt.Sprintf("%.2f", incAmt)})
		case "expenses":
			_ = writer.Write([]string{m, fmt.Sprintf("%.2f", expAmt)})
		case "savings":
			_ = writer.Write([]string{m, fmt.Sprintf("%.2f", incAmt), fmt.Sprintf("%.2f", expAmt), fmt.Sprintf("%.2f", savings)})
		case "savings-rate":
			_ = writer.Write([]string{m, fmt.Sprintf("%.2f", incAmt), fmt.Sprintf("%.2f", expAmt), fmt.Sprintf("%.2f", savings), fmt.Sprintf("%.1f", rate)})
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		log.Printf("dashboard: building CSV export: %v", err)
		http.Error(w, "Failed to build export", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("%s_%s_to_%s.csv", kpiType, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	_, _ = w.Write(buf.Bytes())
}

// Utility Functions

// resolveDateRange converts start/end query strings into time.Time values,
// defaulting to YTD start (clamped to data bounds) and maxDate end.
func resolveDateRange(startStr, endStr string, minDate, maxDate time.Time) (time.Time, time.Time) {
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
	return startDate, endDate
}

// buildBudgetVsActualChartData renders a two-panel Plotly chart showing
// monthly Living + Healthcare actuals stacked against a combined budget
// target line (top panel) and the per-month running cumulative balance
// (bottom panel). Balance uses the savings convention: positive = ahead
// of budget (saved), negative = behind (overspent), opposite of the
// CombinedCumulativeDelta KPI field. Returns a payload with empty data
// when the combined target is 0 so the front end can branch on
// len(data)==0 to show its "Set a budget in What-If →" empty state.
func buildBudgetVsActualChartData(ts *models.TransactionSet, rangeStart, rangeEnd time.Time, livingTarget, healthcareTarget float64) map[string]interface{} {
	combinedTarget := livingTarget + healthcareTarget
	if combinedTarget <= 0 {
		return map[string]interface{}{
			"data":   []map[string]interface{}{},
			"layout": map[string]interface{}{},
		}
	}

	outflows := ts.FilterByType(models.Outflow)
	healthcareOutflows := outflows.FilterByCategory(metrics.HealthInsuranceCategory)
	monthlyOutflows := outflows.GroupByMonth()
	monthlyHealthcare := healthcareOutflows.GroupByMonth()

	monthSet := make(map[string]bool)
	for m := range monthlyOutflows {
		monthSet[m] = true
	}
	var months []string
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)

	livingValues := make([]float64, 0, len(months))
	healthcareValues := make([]float64, 0, len(months))
	cumulativeValues := make([]float64, 0, len(months))

	var running float64
	for _, m := range months {
		expAmt := 0.0
		if exp, ok := monthlyOutflows[m]; ok {
			expAmt = math.Abs(exp.SumAmount())
		}
		hcAmt := 0.0
		if hc, ok := monthlyHealthcare[m]; ok {
			hcAmt = math.Abs(hc.SumAmount())
		}
		livingMonth := expAmt - hcAmt

		livingValues = append(livingValues, livingMonth)
		healthcareValues = append(healthcareValues, hcAmt)

		running += combinedTarget - (livingMonth + hcAmt)
		cumulativeValues = append(cumulativeValues, running)
	}

	livingTrace := map[string]interface{}{
		"type": "bar",
		"name": "Living",
		"x":    months,
		"y":    livingValues,
		"marker": map[string]interface{}{
			"color": "#9ca3af", // gray — matches Living card icon
		},
		"xaxis": "x",
		"yaxis": "y",
	}

	healthcareTrace := map[string]interface{}{
		"type": "bar",
		"name": "Healthcare",
		"x":    months,
		"y":    healthcareValues,
		"marker": map[string]interface{}{
			"color": "#e11d48", // rose — matches Healthcare card icon
		},
		"xaxis": "x",
		"yaxis": "y",
	}

	cumulativeTrace := map[string]interface{}{
		"type": "scatter",
		"mode": "lines+markers",
		"name": "Cumulative balance",
		"x":    months,
		"y":    cumulativeValues,
		"line": map[string]interface{}{
			"color": "#6366f1",
			"width": 2,
		},
		"fill":      "tozeroy",
		"fillcolor": "rgba(99, 102, 241, 0.2)",
		"xaxis":     "x2",
		"yaxis":     "y2",
	}

	layout := map[string]interface{}{
		"barmode":    "stack",
		"showlegend": true,
		"legend": map[string]interface{}{
			"orientation": "h",
			"y":           1.12,
		},
		"grid": map[string]interface{}{
			"rows":    2,
			"columns": 1,
			"pattern": "independent",
		},
		"xaxis": map[string]interface{}{
			"showticklabels": false,
		},
		"yaxis": map[string]interface{}{
			"title":  map[string]interface{}{"text": "Monthly $"},
			"domain": []float64{0.55, 1.0},
		},
		"xaxis2": map[string]interface{}{
			"anchor": "y2",
		},
		"yaxis2": map[string]interface{}{
			"title":         map[string]interface{}{"text": "Cumulative balance $"},
			"domain":        []float64{0.0, 0.42},
			"zeroline":      true,
			"zerolinecolor": "#6b7280",
			"zerolinewidth": 2,
		},
		"shapes": []map[string]interface{}{
			{
				// Combined target line on top subplot
				"type": "line",
				"xref": "paper",
				"x0":   0,
				"x1":   1,
				"yref": "y",
				"y0":   combinedTarget,
				"y1":   combinedTarget,
				"line": map[string]interface{}{
					"color": "#6b7280",
					"width": 2,
					"dash":  "dash",
				},
			},
		},
		"annotations": []map[string]interface{}{
			{
				"xref":      "paper",
				"yref":      "y",
				"x":         1,
				"xanchor":   "right",
				"y":         combinedTarget,
				"yanchor":   "bottom",
				"text":      fmt.Sprintf("Target $%.0f", combinedTarget),
				"showarrow": false,
				"font": map[string]interface{}{
					"color": "#6b7280",
					"size":  11,
				},
			},
		},
	}

	return map[string]interface{}{
		"data":   []map[string]interface{}{livingTrace, healthcareTrace, cumulativeTrace},
		"layout": layout,
	}
}

// majorExpenseDonutLimit caps the number of individual wedges shown in
// the Spending by Major Expense donut. Anything past this is rolled
// into a single "Other" wedge with a text breakdown beneath the chart.
// The drilldown handler reads the same constant so clicking "Other"
// returns exactly the rolled-up tail.
const majorExpenseDonutLimit = 8

// majorExpenseBucket is one wedge candidate: a named major expense plus
// every outflow that landed in it (after pin overrides + keyword/amount
// matching) and the pre-summed absolute total used for sorting.
type majorExpenseBucket struct {
	name  string
	txns  []models.Transaction
	total float64
}

// bucketMajorExpenses groups outflows by user-declared major expense
// and returns the sorted (descending by total) bucket list plus the
// unmatched outflows. Both the chart builder and the drilldown handler
// call this so they always agree on which transactions belong to which
// wedge — including pin overrides.
func bucketMajorExpenses(ts *models.TransactionSet) (buckets []majorExpenseBucket, unmatched []models.Transaction) {
	if ts == nil {
		return nil, nil
	}
	outflows := ts.FilterByType(models.Outflow)

	// Best-effort load — empty config (or no loader during unit tests)
	// just means everything goes in "Unmatched", which is a perfectly
	// fine empty state.
	var expenses []models.MajorExpense
	var pins map[string]string
	if loader != nil {
		expenses, _ = loader.LoadMajorExpenses()
		pins, _ = loader.LoadTransactionPins()
	}

	match := majorexpenses.Match(outflows, expenses, majorexpenses.MatchOptions{Pins: pins})

	expenseByID := make(map[string]models.MajorExpense, len(expenses))
	for _, e := range expenses {
		expenseByID[e.ID] = e
	}

	for id, txns := range match.Groups {
		name := expenseByID[id].Name
		if name == "" {
			continue // expense was deleted between Match and lookup; skip
		}
		var total float64
		for _, t := range txns {
			// Net spend: refunds (positive Outflow amounts per classifier
			// convention) reduce the bucket instead of inflating it via
			// AbsAmount. Mirrors the Major Expenses page.
			total += -t.Amount
		}
		if total > 0 {
			buckets = append(buckets, majorExpenseBucket{name: name, txns: txns, total: total})
		}
	}

	sort.Slice(buckets, func(i, j int) bool { return buckets[i].total > buckets[j].total })
	return buckets, match.Unmatched
}

// buildMajorExpenseChartData renders a pie chart of outflow spending
// grouped by user-declared major expense for the date-filtered window.
// Transactions that don't match any major expense are bucketed under
// "Unmatched" so the totals add up to the period's total outflows.
//
// To keep the donut readable when many small buckets exist, only the
// top majorExpenseDonutLimit matched buckets are kept as individual
// wedges; the rest are rolled into a single "Other" wedge. The list
// of rolled-up items is returned alongside the chart in the "smaller"
// field so the client can render a text breakdown.
func buildMajorExpenseChartData(ts *models.TransactionSet) map[string]interface{} {
	buckets, unmatchedTxns := bucketMajorExpenses(ts)

	var unmatchedTotal float64
	for _, t := range unmatchedTxns {
		// Net spend, consistent with bucketMajorExpenses.
		unmatchedTotal += -t.Amount
	}

	type wedge struct {
		name string
		val  float64
	}
	display := make([]wedge, 0, len(buckets)+2)
	var rolledUp []wedge
	if len(buckets) > majorExpenseDonutLimit {
		for _, b := range buckets[:majorExpenseDonutLimit] {
			display = append(display, wedge{b.name, b.total})
		}
		var otherTotal float64
		for _, b := range buckets[majorExpenseDonutLimit:] {
			rolledUp = append(rolledUp, wedge{b.name, b.total})
			otherTotal += b.total
		}
		display = append(display, wedge{"Other", otherTotal})
	} else {
		for _, b := range buckets {
			display = append(display, wedge{b.name, b.total})
		}
	}

	if unmatchedTotal > 0 {
		display = append(display, wedge{"Unmatched", unmatchedTotal})
	}

	labels := make([]string, len(display))
	values := make([]float64, len(display))
	var grandTotal float64
	for i, w := range display {
		labels[i] = w.name
		values[i] = w.val
		grandTotal += w.val
	}

	out := map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type":   "pie",
				"labels": labels,
				"values": values,
				"hole":   0.4,
			},
		},
	}

	if len(rolledUp) > 0 {
		smaller := make([]map[string]interface{}, 0, len(rolledUp))
		for _, w := range rolledUp {
			pct := 0.0
			if grandTotal > 0 {
				pct = w.val / grandTotal * 100
			}
			// One decimal for ≥ 1%, two decimals for < 1% so that a
			// 0.45% slice does not display as "0%".
			if pct < 1 {
				pct = math.Round(pct*100) / 100
			} else {
				pct = math.Round(pct*10) / 10
			}
			smaller = append(smaller, map[string]interface{}{
				"name":    w.name,
				"amount":  w.val,
				"percent": pct,
			})
		}
		out["smaller"] = smaller
	}

	return out
}

func buildSpendingTrendChartData(ts *models.TransactionSet) map[string]interface{} {
	outflows := ts.FilterByType(models.Outflow)
	monthlyOutflowSets := outflows.GroupByMonth()

	// Build sorted month list and per-month absolute totals
	var months []string
	for m := range monthlyOutflowSets {
		months = append(months, m)
	}
	sort.Strings(months)

	monthlyTotals := make(map[string]float64, len(months))
	for _, m := range months {
		monthlyTotals[m] = math.Abs(monthlyOutflowSets[m].SumAmount())
	}

	// Need at least 2 months to show change
	if len(months) < 2 {
		return map[string]interface{}{
			"data":   []map[string]interface{}{},
			"layout": map[string]interface{}{},
		}
	}

	// Calculate month-over-month percentage change
	var changeMonths []string
	var changeValues []float64
	var colors []string
	var currAmounts []float64
	var prevAmounts []float64

	for i := 1; i < len(months); i++ {
		prev := monthlyTotals[months[i-1]]
		curr := monthlyTotals[months[i]]

		var pctChange float64
		if prev > 0 {
			pctChange = ((curr - prev) / prev) * 100
		}

		changeMonths = append(changeMonths, months[i])
		changeValues = append(changeValues, pctChange)
		currAmounts = append(currAmounts, curr)
		prevAmounts = append(prevAmounts, prev)
		if pctChange <= 0 {
			colors = append(colors, "#22c55e") // green = spending decreased
		} else {
			colors = append(colors, "#ef4444") // red = spending increased
		}
	}

	return map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"type": "bar",
				"name": "Spending Change",
				"x":    changeMonths,
				"y":    changeValues,
				"customdata": func() [][]float64 {
					var cd [][]float64
					for i := range currAmounts {
						cd = append(cd, []float64{currAmounts[i], prevAmounts[i]})
					}
					return cd
				}(),
				"marker": map[string]interface{}{
					"color": colors,
				},
				"hovertemplate": "<b>%{x}</b><br>Spent: $%{customdata[0]:,.0f}<br>Prior: $%{customdata[1]:,.0f}<br>Change: %{y:+.1f}%<extra></extra>",
			},
		},
		"layout": map[string]interface{}{
			"yaxis": map[string]interface{}{
				"title":      "Change (%)",
				"ticksuffix": "%",
			},
		},
	}
}

func buildMerchantsChartData(ts *models.TransactionSet) map[string]interface{} {
	outflows := ts.FilterByType(models.Outflow)

	// Group by description (merchant)
	merchantTotals := make(map[string]float64)
	for _, t := range outflows.Transactions {
		merchantTotals[t.Label()] += math.Abs(t.Amount)
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
		var dayTotal float64
		for _, t := range daily[d].Transactions {
			// A transfer is neither income nor expense (GLOSSARY:
			// "Transfer"). This loop's else branch is the only
			// income/expense consumer in the app that is not a
			// FilterByType call, so without this skip every transfer
			// leg would be subtracted from cumulative cash flow --
			// and a paired transfer would be subtracted twice.
			if t.TransactionType == models.Transfer {
				continue
			}
			if t.TransactionType == models.Income {
				dayTotal += math.Abs(t.Amount)
			} else {
				dayTotal -= math.Abs(t.Amount)
			}
		}
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
