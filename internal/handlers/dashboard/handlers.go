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
	"budget2/internal/templates"
)

var (
	loader   *dataloader.DataLoader
	renderer *templates.Renderer
)

// Initialize sets up the dashboard package with required dependencies
func Initialize(l *dataloader.DataLoader, r *templates.Renderer) {
	loader = l
	renderer = r
}

// RegisterRoutes registers all dashboard routes
func RegisterRoutes(r chi.Router) {
	r.Get("/dashboard", handleDashboard)
	r.Get("/dashboard/kpis", handleKPIsPartial)
	r.Get("/dashboard/charts/data/{chartType}", handleChartData)
	r.Get("/dashboard/alerts", handleAlertsPartial)
	r.Get("/dashboard/category/{category}", handleCategoryDrilldown)
	r.Get("/dashboard/kpi/{kpiType}", handleKPIDetail)
	r.Get("/dashboard/kpi/{kpiType}/export", handleKPIExport)
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

func handleKPIDetail(w http.ResponseWriter, r *http.Request) {
	kpiType := chi.URLParam(r, "kpiType")

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
			expAmt = exp.SumAbsAmount()
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
		renderer.RenderPartial(w, "kpi-detail", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(partialData)
	}
}

func handleKPIExport(w http.ResponseWriter, r *http.Request) {
	kpiType := chi.URLParam(r, "kpiType")

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
		writer.Write([]string{"Month", "Income"})
	case "expenses":
		writer.Write([]string{"Month", "Expenses"})
	case "savings":
		writer.Write([]string{"Month", "Income", "Expenses", "Savings"})
	case "savings-rate":
		writer.Write([]string{"Month", "Income", "Expenses", "Savings", "Savings Rate %"})
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

		savings := incAmt - expAmt
		rate := 0.0
		if incAmt > 0 {
			rate = (savings / incAmt) * 100
		}

		switch kpiType {
		case "income":
			writer.Write([]string{m, fmt.Sprintf("%.2f", incAmt)})
		case "expenses":
			writer.Write([]string{m, fmt.Sprintf("%.2f", expAmt)})
		case "savings":
			writer.Write([]string{m, fmt.Sprintf("%.2f", incAmt), fmt.Sprintf("%.2f", expAmt), fmt.Sprintf("%.2f", savings)})
		case "savings-rate":
			writer.Write([]string{m, fmt.Sprintf("%.2f", incAmt), fmt.Sprintf("%.2f", expAmt), fmt.Sprintf("%.2f", savings), fmt.Sprintf("%.1f", rate)})
		}
	}

	writer.Flush()

	filename := fmt.Sprintf("%s_%s_to_%s.csv", kpiType, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Write(buf.Bytes())
}

// Utility Functions

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
			// Sort transactions by amount (largest first) for display
			txnsCopy := make([]models.Transaction, len(dayTxns.Transactions))
			copy(txnsCopy, dayTxns.Transactions)
			sort.Slice(txnsCopy, func(i, j int) bool {
				return math.Abs(txnsCopy[i].Amount) > math.Abs(txnsCopy[j].Amount)
			})
			alerts = append(alerts, models.SpendingAlert{
				Type:         "unusual_day",
				Severity:     "warning",
				Title:        "High Spending Day",
				Message:      fmt.Sprintf("$%.0f spent on %s (%.0f%% above average)", total, date.Format("Jan 2"), ((total-mean)/mean)*100),
				Date:         &date,
				Amount:       total,
				Transactions: txnsCopy,
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
				Detail:   t.Description,
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
