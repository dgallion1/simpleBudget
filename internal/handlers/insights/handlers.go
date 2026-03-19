package insights

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"budget2/internal/models"
	"budget2/internal/services/dataloader"
	"budget2/internal/templates"
)

var (
	loader   *dataloader.DataLoader
	renderer *templates.Renderer
)

// Initialize sets up the insights package with required dependencies
func Initialize(l *dataloader.DataLoader, r *templates.Renderer) {
	loader = l
	renderer = r
}

// RegisterRoutes registers all insights routes
func RegisterRoutes(r chi.Router) {
	r.Get("/insights", handleInsights)
	r.Get("/insights/recurring", handleRecurringPartial)
	r.Get("/insights/trends", handleTrendsPartial)
	r.Get("/insights/trends/chart", handleTrendsChartData)
	r.Get("/insights/velocity", handleVelocityPartial)
	r.Get("/insights/income", handleIncomePartial)
}

// Utility Functions

// retailKeywords identifies transactions that are store/retail purchases, not subscriptions.
// These have recurring patterns due to frequent shopping but are not subscription services.
var retailKeywords = []string{
	"walmart", "target", "costco", "kroger", "publix", "aldi",
	"wegmans", "trader joe", "whole foods", "safeway", "albertsons",
	"home depot", "lowes", "lowe's", "menards",
	"walgreens", "cvs", "rite aid",
	"amazon", "ebay", "etsy",
	"bj's", "sam's club",
	"pet supplies", "petsmart", "petco",
	"dollar general", "dollar tree", "five below",
	"restaurant", "grubhub", "doordash", "uber eats",
	"gas station", "shell", "exxon", "bp ", "chevron", "speedway",
	"starbucks", "dunkin", "mcdonald", "wendy", "chipotle",
	"grocery", "groceries", "market",
	"wine", "spirits", "liquor", "cafe", "diner", "grill",
	"food co op", "abundance",
}

// billKeywords identifies transactions that are utility bills, not subscription services.
var billKeywords = []string{
	"electric", "g&e", "power", "energy",
	"water", "sewer",
	"rent ", "mortgage", "hoa",
	"insurance", "casualty", "geico", "allstate", "state farm",
	"credit card payment", "loan payment", "payment - thank",
	"transfer", "funds transfer",
	"tax", "dmv", "government",
}

// isSubscription classifies a recurring payment as a subscription service.
// Subscriptions are regular payments that are not retail stores or utility bills.
// This includes both fixed-amount (Netflix) and variable-amount (API billing) services.
func isSubscription(rp models.RecurringPayment) bool {
	desc := strings.ToLower(rp.Description)

	// Check against retail keywords - stores you shop at are not subscriptions
	for _, kw := range retailKeywords {
		if strings.Contains(desc, kw) {
			return false
		}
	}

	// Check against bill keywords - utilities/bills are not subscriptions
	for _, kw := range billKeywords {
		if strings.Contains(desc, kw) {
			return false
		}
	}

	// Fixed-interval payments (monthly/yearly/quarterly) are subscriptions
	if rp.Frequency == "monthly" || rp.Frequency == "yearly" || rp.Frequency == "quarterly" {
		return true
	}

	// "ongoing" payments that aren't retail or bills are likely subscription services
	// with variable billing (e.g., API usage, metered services)
	if rp.Frequency == "ongoing" {
		return true
	}

	return false
}

func calculateInsights(allData, filtered *models.TransactionSet, startDate, endDate time.Time) *models.InsightsData {
	recurring := detectRecurringPayments(filtered)
	trends := analyzeCategoryTrends(allData, startDate, endDate)
	income := AnalyzeIncomePatterns(filtered)
	velocity := calculateSpendingVelocity(filtered, allData)

	// Split recurring payments into subscriptions and bills
	var subscriptions, bills []models.RecurringPayment
	for _, r := range recurring {
		if isSubscription(r) {
			subscriptions = append(subscriptions, r)
		} else {
			bills = append(bills, r)
		}
	}

	var totalRecurring, monthlySubscriptions, regularIncome float64
	for _, r := range recurring {
		totalRecurring += r.AnnualCost
	}
	for _, s := range subscriptions {
		monthlySubscriptions += s.AnnualCost / 12
	}

	for _, ip := range income {
		if ip.IsRegular {
			regularIncome += ip.TotalAmount
		}
	}

	return &models.InsightsData{
		RecurringPayments:    bills,
		Subscriptions:        subscriptions,
		CategoryTrends:       trends,
		IncomePatterns:       income,
		Velocity:             velocity,
		TotalRecurring:       totalRecurring,
		MonthlyRecurring:     totalRecurring / 12,
		MonthlySubscriptions: monthlySubscriptions,
		RegularIncomeTotal:   regularIncome,
	}
}

// mergeSimlarGroups consolidates transaction groups where descriptions refer to
// the same vendor. It strips common suffixes like ".com", ".net", etc. and then
// checks if the shorter stripped name is a prefix of the longer one. When a match
// is found, transactions are merged under the shorter (more canonical) description.
func mergeSimlarGroups(groups map[string][]models.Transaction) map[string][]models.Transaction {
	// Strip common domain/URL suffixes for comparison
	strip := func(s string) string {
		s = strings.TrimSuffix(s, ".com")
		s = strings.TrimSuffix(s, ".net")
		s = strings.TrimSuffix(s, ".org")
		s = strings.TrimSuffix(s, ".io")
		s = strings.TrimSuffix(s, ".co")
		return s
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	// Sort by length so shorter (canonical) names come first
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i]) < len(keys[j])
	})

	merged := make(map[string][]models.Transaction)
	// Maps a stripped key to the canonical group key
	canonical := make(map[string]string)

	for _, key := range keys {
		stripped := strip(key)
		// Check if this stripped name matches or is a prefix of an existing canonical,
		// or if an existing canonical is a prefix of this one
		found := false
		for canon, canonKey := range canonical {
			if strings.HasPrefix(stripped, canon) || strings.HasPrefix(canon, stripped) {
				// Merge into the existing canonical group
				merged[canonKey] = append(merged[canonKey], groups[key]...)
				// If the new stripped name is shorter, update the canonical mapping
				if len(stripped) < len(canon) {
					// Re-map: the shorter name becomes canonical
					txns := merged[canonKey]
					delete(merged, canonKey)
					merged[key] = txns
					delete(canonical, canon)
					canonical[stripped] = key
				}
				found = true
				break
			}
		}
		if !found {
			merged[key] = groups[key]
			canonical[stripped] = key
		}
	}

	return merged
}

func detectRecurringPayments(ts *models.TransactionSet) []models.RecurringPayment {
	var recurring []models.RecurringPayment

	outflows := ts.FilterByType(models.Outflow)
	if outflows.Len() < 2 {
		return recurring
	}

	groups := make(map[string][]models.Transaction)
	for _, t := range outflows.Transactions {
		key := strings.ToLower(strings.TrimSpace(t.Description))
		groups[key] = append(groups[key], t)
	}

	// Merge groups with fuzzy matching: if one description contains another,
	// combine them under the shorter (canonical) name. This handles cases like
	// "lucid" and "lucidmotors.com" referring to the same vendor.
	groups = mergeSimlarGroups(groups)

	// Track which descriptions matched strict criteria
	strictMatches := make(map[string]bool)

	// First pass: strict recurring detection (consistent amounts and intervals)
	for desc, txns := range groups {
		if len(txns) < 3 {
			continue
		}

		sort.Slice(txns, func(i, j int) bool {
			return txns[i].Date.Before(txns[j].Date)
		})

		var intervals []float64
		for i := 1; i < len(txns); i++ {
			days := txns[i].Date.Sub(txns[i-1].Date).Hours() / 24
			intervals = append(intervals, days)
		}

		if len(intervals) == 0 {
			continue
		}

		sortedIntervals := make([]float64, len(intervals))
		copy(sortedIntervals, intervals)
		sort.Float64s(sortedIntervals)
		medianInterval := sortedIntervals[len(sortedIntervals)/2]

		var sumSq float64
		for _, interval := range intervals {
			diff := interval - medianInterval
			sumSq += diff * diff
		}
		stdDev := math.Sqrt(sumSq / float64(len(intervals)))

		if stdDev > 7 {
			continue
		}

		var amounts []float64
		for _, t := range txns {
			amounts = append(amounts, math.Abs(t.Amount))
		}
		avgAmount := 0.0
		for _, a := range amounts {
			avgAmount += a
		}
		avgAmount /= float64(len(amounts))

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

		var frequency string
		var annualMultiplier float64

		switch {
		case medianInterval >= 5 && medianInterval <= 9:
			if len(txns) < 4 {
				continue
			}
			frequency = "weekly"
			annualMultiplier = 52
		case medianInterval >= 12 && medianInterval <= 16:
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
		strictMatches[desc] = true
	}

	// Second pass: ongoing payment detection (variable amounts but consistent relationship)
	now := time.Now()
	for desc, txns := range groups {
		// Skip if already matched by strict criteria
		if strictMatches[desc] {
			continue
		}

		if len(txns) < 3 {
			continue
		}

		sort.Slice(txns, func(i, j int) bool {
			return txns[i].Date.Before(txns[j].Date)
		})

		firstDate := txns[0].Date
		lastDate := txns[len(txns)-1].Date

		// Must span at least 60 days (2+ months of relationship)
		spanDays := lastDate.Sub(firstDate).Hours() / 24
		if spanDays < 60 {
			continue
		}

		// Must have activity within last 90 days (still active)
		daysSinceLastPayment := now.Sub(lastDate).Hours() / 24
		if daysSinceLastPayment > 90 {
			continue
		}

		// Calculate total and average amount
		var totalAmount float64
		for _, t := range txns {
			totalAmount += math.Abs(t.Amount)
		}
		avgAmount := totalAmount / float64(len(txns))

		// Calculate annual cost based on actual spending rate
		months := spanDays / 30.0
		if months < 1 {
			months = 1
		}
		monthlyRate := totalAmount / months
		annualCost := monthlyRate * 12

		// Confidence based on recency and number of transactions
		confidence := 0.7
		if daysSinceLastPayment < 30 {
			confidence = 0.9
		} else if daysSinceLastPayment < 60 {
			confidence = 0.8
		}

		// Estimate next expected based on average interval
		avgInterval := spanDays / float64(len(txns)-1)
		nextExpected := lastDate.AddDate(0, 0, int(avgInterval))

		recurring = append(recurring, models.RecurringPayment{
			Description:  desc,
			Amount:       avgAmount,
			Frequency:    "ongoing",
			LastDate:     lastDate,
			NextExpected: nextExpected,
			AnnualCost:   annualCost,
			Occurrences:  len(txns),
			Confidence:   confidence,
			Transactions: txns,
		})
		strictMatches[desc] = true
	}

	// Third pass: amount-based grouping for payments with different descriptions
	// but identical amounts at regular intervals (e.g., check payments and bill pay
	// to the same vendor like "Check #996578" and "Lucid" both at $1,580.43).
	recurring = append(recurring, detectByAmount(strictMatches, groups, now)...)

	sort.Slice(recurring, func(i, j int) bool {
		return recurring[i].AnnualCost > recurring[j].AnnualCost
	})

	if len(recurring) > 20 {
		recurring = recurring[:20]
	}

	return recurring
}

// detectByAmount finds recurring payments across different descriptions that share
// the same exact amount and regular intervals. This catches cases like a car payment
// that switches from checks to direct bill pay mid-stream.
func detectByAmount(alreadyMatched map[string]bool, groups map[string][]models.Transaction, now time.Time) []models.RecurringPayment {
	var results []models.RecurringPayment

	// Collect all unmatched transactions
	var unmatched []models.Transaction
	for desc, txns := range groups {
		if alreadyMatched[desc] {
			continue
		}
		unmatched = append(unmatched, txns...)
	}

	// Group unmatched transactions by exact amount (rounded to cents)
	amountGroups := make(map[int64][]models.Transaction)
	for _, t := range unmatched {
		// Key on cents to avoid floating point comparison issues
		cents := int64(math.Round(math.Abs(t.Amount) * 100))
		// Skip tiny amounts — not meaningful recurring payments
		if cents < 500 { // $5.00 minimum
			continue
		}
		amountGroups[cents] = append(amountGroups[cents], t)
	}

	for _, txns := range amountGroups {
		if len(txns) < 3 {
			continue
		}

		sort.Slice(txns, func(i, j int) bool {
			return txns[i].Date.Before(txns[j].Date)
		})

		var intervals []float64
		for i := 1; i < len(txns); i++ {
			days := txns[i].Date.Sub(txns[i-1].Date).Hours() / 24
			intervals = append(intervals, days)
		}

		if len(intervals) == 0 {
			continue
		}

		sortedIntervals := make([]float64, len(intervals))
		copy(sortedIntervals, intervals)
		sort.Float64s(sortedIntervals)
		medianInterval := sortedIntervals[len(sortedIntervals)/2]

		var sumSq float64
		for _, interval := range intervals {
			diff := interval - medianInterval
			sumSq += diff * diff
		}
		stdDev := math.Sqrt(sumSq / float64(len(intervals)))

		// Allow slightly more variance than pass 1 since descriptions differ
		if stdDev > 10 {
			continue
		}

		var frequency string
		var annualMultiplier float64

		switch {
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

		confidence := 1.0 - (stdDev / medianInterval)
		if confidence < 0.4 {
			continue
		}
		// Reduce confidence slightly since we're matching on amount alone
		confidence *= 0.9

		// Must have activity within last 90 days
		lastDate := txns[len(txns)-1].Date
		if now.Sub(lastDate).Hours()/24 > 90 {
			continue
		}

		avgAmount := math.Abs(txns[0].Amount)
		nextExpected := lastDate.AddDate(0, 0, int(medianInterval))

		// Use the most recent transaction's description as the label
		desc := strings.ToLower(strings.TrimSpace(txns[len(txns)-1].Description))

		results = append(results, models.RecurringPayment{
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

	return results
}

func analyzeCategoryTrends(ts *models.TransactionSet, currentStart, currentEnd time.Time) []models.CategoryTrend {
	var trends []models.CategoryTrend

	duration := currentEnd.Sub(currentStart)
	prevStart := currentStart.Add(-duration - 24*time.Hour)
	prevEnd := currentStart.Add(-24 * time.Hour)

	currentFiltered := ts.FilterByDateRange(currentStart, currentEnd)
	prevFiltered := ts.FilterByDateRange(prevStart, prevEnd)

	currentOutflows := currentFiltered.FilterByType(models.Outflow)
	prevOutflows := prevFiltered.FilterByType(models.Outflow)

	currentTotals := currentOutflows.CategoryTotals()
	prevTotals := prevOutflows.CategoryTotals()

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

	sort.Slice(trends, func(i, j int) bool {
		return math.Abs(trends[i].ChangeAmount) > math.Abs(trends[j].ChangeAmount)
	})

	if len(trends) > 10 {
		trends = trends[:10]
	}

	return trends
}

// AnalyzeIncomePatterns detects recurring income sources from transaction data.
// Exported for use by other packages (e.g., whatif).
func AnalyzeIncomePatterns(ts *models.TransactionSet) []models.IncomePattern {
	var patterns []models.IncomePattern

	income := ts.FilterByType(models.Income)
	if income.Len() < 2 {
		return patterns
	}

	groups := make(map[string][]models.Transaction)
	for _, t := range income.Transactions {
		key := strings.ToLower(strings.TrimSpace(t.Description))
		groups[key] = append(groups[key], t)
	}

	for desc, txns := range groups {
		if len(txns) < 2 {
			continue
		}

		sort.Slice(txns, func(i, j int) bool {
			return txns[i].Date.Before(txns[j].Date)
		})

		var total float64
		for _, t := range txns {
			total += t.Amount
		}
		avg := total / float64(len(txns))

		var intervals []float64
		for i := 1; i < len(txns); i++ {
			days := txns[i].Date.Sub(txns[i-1].Date).Hours() / 24
			intervals = append(intervals, days)
		}

		var frequency string
		isRegular := false

		if len(intervals) > 0 {
			sortedIntervals := make([]float64, len(intervals))
			copy(sortedIntervals, intervals)
			sort.Float64s(sortedIntervals)
			medianInterval := sortedIntervals[len(sortedIntervals)/2]

			var sumSq float64
			for _, interval := range intervals {
				diff := interval - medianInterval
				sumSq += diff * diff
			}
			stdDev := math.Sqrt(sumSq / float64(len(intervals)))

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

	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].TotalAmount > patterns[j].TotalAmount
	})

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

	currentMin := currentPeriod.MinDate()
	currentMax := currentPeriod.MaxDate()
	currentDays := currentMax.Sub(currentMin).Hours()/24 + 1
	if currentDays < 1 {
		currentDays = 1
	}
	dailyAvg := currentOutflows.SumAbsAmount() / currentDays

	allMin := allData.MinDate()
	allMax := allData.MaxDate()
	allDays := allMax.Sub(allMin).Hours()/24 + 1
	if allDays < 1 {
		allDays = 1
	}
	historicalDaily := allOutflows.SumAbsAmount() / allDays

	now := time.Now()
	daysInMonth := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.Local).Day()
	dayOfMonth := now.Day()
	daysRemaining := daysInMonth - dayOfMonth

	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	currentMonthData := currentPeriod.FilterByDateRange(currentMonthStart, now)
	currentMonthOutflows := currentMonthData.FilterByType(models.Outflow)
	spentSoFar := currentMonthOutflows.SumAbsAmount()

	monthProjection := spentSoFar + (dailyAvg * float64(daysRemaining))

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

// HTTP Handlers

func handleInsights(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, "Error loading data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	preset := r.URL.Query().Get("preset")

	minDate := data.MinDate()
	maxDate := data.MaxDate()

	var startDate, endDate time.Time
	if startStr != "" {
		startDate, _ = time.Parse("2006-01-02", startStr)
	} else {
		startDate = maxDate.AddDate(0, -12, 0)
		if startDate.Before(minDate) {
			startDate = minDate
		}
		preset = "12m"
	}
	if endStr != "" {
		endDate, _ = time.Parse("2006-01-02", endStr)
	} else {
		endDate = maxDate
	}

	filtered := data.FilterByDateRange(startDate, endDate)

	insights := calculateInsights(data, filtered, startDate, endDate)

	pageData := map[string]interface{}{
		"Title":     "Insights",
		"ActiveTab": "insights",
		"Insights":  insights,
		"StartDate": startDate.Format("2006-01-02"),
		"EndDate":   endDate.Format("2006-01-02"),
		"MinDate":   minDate.Format("2006-01-02"),
		"MaxDate":   maxDate.Format("2006-01-02"),
		"Preset":    preset,
	}

	if renderer != nil {
		renderer.Render(w, "base", pageData)
	} else {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Insights</h1><p>Coming soon...</p></body></html>"))
	}
}

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

	var categories []string
	var currentValues []float64
	var previousValues []float64
	var colors []string

	for _, t := range trends {
		categories = append(categories, t.Category)
		currentValues = append(currentValues, t.CurrentAmount)
		previousValues = append(previousValues, t.PreviousAmount)
		if t.Direction == "up" {
			colors = append(colors, "#ef4444")
		} else if t.Direction == "down" {
			colors = append(colors, "#22c55e")
		} else {
			colors = append(colors, "#6b7280")
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

	income := AnalyzeIncomePatterns(data)

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
