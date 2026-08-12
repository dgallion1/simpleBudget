// Package insights serves the Insights page and exposes pattern-detection
// helpers (income cadence, recurring expenses, anomaly detection) used by
// both the dashboard and the what-if defaults pipeline.
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
	"budget2/internal/services/anomalies"
	"budget2/internal/services/dataloader"
	"budget2/internal/services/majorexpenses"
	"budget2/internal/services/merchants"
	"budget2/internal/services/pricecreep"
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

func recurringFreshnessWindow(intervalDays float64) float64 {
	switch {
	case intervalDays <= 0:
		return 90
	case intervalDays <= 9:
		return 21
	case intervalDays <= 16:
		return 45
	case intervalDays <= 35:
		return 90
	case intervalDays <= 95:
		return 180
	default:
		return 455
	}
}

func recurringPaymentIsActive(lastDate time.Time, intervalDays float64, now time.Time) bool {
	daysSinceLastPayment := now.Sub(lastDate).Hours() / 24
	return daysSinceLastPayment <= recurringFreshnessWindow(intervalDays)
}

func recurringReferenceDate(ts *models.TransactionSet, referenceDate time.Time) time.Time {
	if !referenceDate.IsZero() {
		return referenceDate
	}
	if ts != nil {
		if maxDate := ts.MaxDate(); !maxDate.IsZero() {
			return maxDate
		}
	}
	return time.Now()
}

func recurringTransactionSet(ts *models.TransactionSet, referenceDate time.Time) *models.TransactionSet {
	if ts == nil || referenceDate.IsZero() {
		return ts
	}
	return ts.FilterByDateRange(ts.MinDate(), referenceDate)
}

// annotateRecurringWithMajorExpense fills MajorExpenseName on each
// detected recurring payment, honoring user pins. Errors and a nil
// loader are swallowed (insights still useful without the badge — and
// some unit tests construct insights without initializing the loader).
func annotateRecurringWithMajorExpense(payments []models.RecurringPayment) []models.RecurringPayment {
	if loader == nil {
		return payments
	}
	expenses, err := loader.LoadMajorExpenses()
	if err != nil {
		return payments
	}
	pins, _ := loader.LoadTransactionPins()
	return majorexpenses.AnnotateRecurringPayments(payments, expenses, pins)
}

// AnomalyView adds a plain-language method label to an anomalies.Anomaly
// for display on the Insights page. Embedding preserves direct template
// access to all of anomalies.Anomaly's fields (Date, Description,
// Category, Amount, Severity, ...).
type AnomalyView struct {
	anomalies.Anomaly
	MethodLabel string
}

// anomalyMethodLabels maps anomalies.Anomaly.Method values to the
// plain-language labels shown in the Insights "Method" column.
var anomalyMethodLabels = map[string]string{
	"mad_category": "Category outlier",
	"mad_merchant": "Merchant outlier",
	"new_merchant": "New merchant",
}

// anomalyMethodLabel returns the plain-language label for an
// anomalies.Anomaly.Method value, falling back to the raw method string
// for any value not in anomalyMethodLabels (defensive; every method the
// anomalies package currently produces is mapped).
func anomalyMethodLabel(method string) string {
	if label, ok := anomalyMethodLabels[method]; ok {
		return label
	}
	return method
}

// anomaliesForPeriod runs anomalies.Detect over the FULL transaction
// history (allData — not the period-filtered set) and returns only the
// flags whose Date falls within [startDate, endDate] for display.
//
// Detecting first against the full history and window-filtering only for
// display is a ruled design requirement (ANALYTICS_PORT_SPEC.md Rulings,
// P2 finding carried forward to P4): peer baselines (mad_category /
// mad_merchant) and each merchant group's first-ever occurrence
// (new_merchant) must be computed against the complete transaction
// history, or a narrow display window would chronically re-flag a large,
// long-standing recurring bill as "new" merely because its true first
// occurrence predates the window. The selected period only scopes which
// already-detected flags are shown; it never changes what counts as a
// merchant's first occurrence or a peer group's typical amount.
//
// Returns the display-filtered views (never nil) and the total number of
// anomalies detected across the full history, regardless of window.
func anomaliesForPeriod(allData *models.TransactionSet, startDate, endDate time.Time) ([]AnomalyView, int) {
	if allData == nil {
		return []AnomalyView{}, 0
	}
	detected := anomalies.Detect(*allData)

	views := make([]AnomalyView, 0, len(detected))
	for _, a := range detected {
		if a.Date.Before(startDate) || a.Date.After(endDate) {
			continue
		}
		views = append(views, AnomalyView{Anomaly: a, MethodLabel: anomalyMethodLabel(a.Method)})
	}
	return views, len(detected)
}

// maxPriceCreepRows caps the number of price-creep rows shown on the
// Insights page; priceCreepForDisplay reports the full detected count
// separately so the template can show a "showing top N of M" line.
const maxPriceCreepRows = 10

// priceCreepForDisplay runs pricecreep.Detect over the FULL transaction
// history (allData). Price creep needs the long series — it compares the
// median of a merchant's first 3 occurrences to its last 3 — so unlike
// anomalies it is never scoped by the page's date filter. Returns the
// (possibly capped) rows to display and the total number detected.
func priceCreepForDisplay(allData *models.TransactionSet) ([]pricecreep.Creep, int) {
	if allData == nil {
		return nil, 0
	}
	creeps := pricecreep.Detect(*allData)
	total := len(creeps)
	if len(creeps) > maxPriceCreepRows {
		creeps = creeps[:maxPriceCreepRows]
	}
	return creeps, total
}

func calculateInsights(allData, filtered *models.TransactionSet, startDate, endDate time.Time) *models.InsightsData {
	// Detect recurring patterns against all data so short date ranges still find them
	recurring := annotateRecurringWithMajorExpense(detectRecurringPaymentsAt(allData, endDate))
	trends := loadAndAnalyzeTrends(allData, startDate, endDate)
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

// mergeSimilarGroups consolidates transaction groups where descriptions refer to
// the same vendor. It strips common suffixes like ".com", ".net", etc. and then
// checks if the shorter stripped name is a prefix of the longer one. When a match
// is found, transactions are merged under the shorter (more canonical) description.
func mergeSimilarGroups(groups map[string][]models.Transaction) map[string][]models.Transaction {
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
	return detectRecurringPaymentsAt(ts, time.Time{})
}

func detectRecurringPaymentsAt(ts *models.TransactionSet, referenceDate time.Time) []models.RecurringPayment {
	var recurring []models.RecurringPayment

	relevantTransactions := recurringTransactionSet(ts, referenceDate)
	outflows := relevantTransactions.FilterByType(models.Outflow)
	if outflows.Len() < 2 {
		return recurring
	}

	// Group transactions into merchant clusters via the shared merchants
	// package (token-subset merge rule with the degenerate-key guard,
	// ported from the ruled b2 algorithm standard) instead of the old
	// hand-rolled per-transaction lowercase-description key. Each
	// cluster is then re-keyed under a human-readable display label
	// (merchants.DisplayLabel) — the merchants canonical key itself is
	// UPPERCASE-normalized purely for matching and must never leak into
	// RecurringPayment.Description or any other user-visible string.
	canonGroups := merchants.GroupTransactions(outflows.Transactions)
	groups := make(map[string][]models.Transaction, len(canonGroups))
	for _, txns := range canonGroups {
		label := merchants.DisplayLabel(txns)
		groups[label] = append(groups[label], txns...)
	}

	// The ruled token-subset rule intentionally does NOT merge two
	// single-token keys unless they are exactly equal (the degenerate-key
	// guard that stops bare processor prefixes like "SQ" from bridging
	// unrelated merchants) — so it does not, on its own, merge e.g.
	// "lucid" and "lucidmotors.com" (both single-token: "LUCID" and
	// "LUCIDMOTORS.COM"). mergeSimilarGroups is layered on top of the
	// canonical clusters to preserve that pre-existing domain-suffix
	// consolidation, which is orthogonal to (and not prohibited by) the
	// ruled standard. See the P3 report for this flagged discrepancy.
	groups = mergeSimilarGroups(groups)

	// Track which descriptions matched strict criteria
	strictMatches := make(map[string]bool)
	now := recurringReferenceDate(ts, referenceDate)

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
		if !recurringPaymentIsActive(lastDate, medianInterval, now) {
			continue
		}
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
		if !recurringPaymentIsActive(lastDate, 0, now) {
			continue
		}
		daysSinceLastPayment := now.Sub(lastDate).Hours() / 24

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
		if !recurringPaymentIsActive(lastDate, medianInterval, now) {
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

// loadAndAnalyzeTrends returns the trends shown in the "Category Spending
// Trends" section. When the user has declared major expenses, transactions
// are grouped by major-expense name (pins + keyword/amount matching) and
// unmatched outflows are dropped — the user told us which spend matters.
// With no defs configured (or no loader in tests), it falls back to plain
// category grouping so new users still see something.
func loadAndAnalyzeTrends(ts *models.TransactionSet, currentStart, currentEnd time.Time) []models.CategoryTrend {
	if loader == nil {
		return analyzeCategoryTrends(ts, currentStart, currentEnd)
	}
	defs, err := loader.LoadMajorExpenses()
	if err != nil || len(defs) == 0 {
		return analyzeCategoryTrends(ts, currentStart, currentEnd)
	}
	pins, _ := loader.LoadTransactionPins()
	return analyzeMajorExpenseTrends(ts, defs, pins, currentStart, currentEnd)
}

// analyzeMajorExpenseTrends groups outflows by matched MajorExpense.Name
// for the current and previous periods and returns the same CategoryTrend
// shape as analyzeCategoryTrends so existing UI can render it. Unmatched
// transactions are intentionally excluded — the trend list is meant to
// surface movement on what the user has declared important. Pins win
// over keyword/amount matching, mirroring the rest of the engine.
func analyzeMajorExpenseTrends(ts *models.TransactionSet, defs []models.MajorExpense, pins map[string]string, currentStart, currentEnd time.Time) []models.CategoryTrend {
	if ts == nil || len(defs) == 0 {
		return nil
	}

	defByID := make(map[string]models.MajorExpense, len(defs))
	for _, d := range defs {
		defByID[d.ID] = d
	}

	duration := currentEnd.Sub(currentStart)
	prevStart := currentStart.Add(-duration - 24*time.Hour)
	prevEnd := currentStart.Add(-24 * time.Hour)

	sumByExpense := func(window *models.TransactionSet) map[string]float64 {
		totals := make(map[string]float64)
		for _, t := range window.FilterByType(models.Outflow).Transactions {
			// Pin wins.
			if pins != nil && t.Hash != "" {
				if id, ok := pins[t.Hash]; ok {
					if def, exists := defByID[id]; exists {
						totals[def.Name] += math.Abs(t.Amount)
						continue
					}
				}
			}
			if id, ok := majorexpenses.MatchTransaction(t, defs); ok {
				totals[defByID[id].Name] += math.Abs(t.Amount)
			}
		}
		return totals
	}

	currentTotals := sumByExpense(ts.FilterByDateRange(currentStart, currentEnd))
	prevTotals := sumByExpense(ts.FilterByDateRange(prevStart, prevEnd))

	nameSet := make(map[string]bool)
	for n := range currentTotals {
		nameSet[n] = true
	}
	for n := range prevTotals {
		nameSet[n] = true
	}

	var trends []models.CategoryTrend
	for name := range nameSet {
		current := currentTotals[name]
		previous := prevTotals[name]

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
			switch {
			case changePercent > 5:
				direction = "up"
			case changePercent < -5:
				direction = "down"
			default:
				direction = "stable"
			}
		}

		trends = append(trends, models.CategoryTrend{
			Category:       name,
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
	dailyAvg := math.Abs(currentOutflows.SumAmount()) / currentDays

	allMin := allData.MinDate()
	allMax := allData.MaxDate()
	allDays := allMax.Sub(allMin).Hours()/24 + 1
	if allDays < 1 {
		allDays = 1
	}
	historicalDaily := math.Abs(allOutflows.SumAmount()) / allDays

	now := time.Now()
	daysInMonth := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.Local).Day()
	dayOfMonth := now.Day()
	daysRemaining := daysInMonth - dayOfMonth

	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	currentMonthData := currentPeriod.FilterByDateRange(currentMonthStart, now)
	currentMonthOutflows := currentMonthData.FilterByType(models.Outflow)
	spentSoFar := math.Abs(currentMonthOutflows.SumAmount())

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

	active := data.Active()
	filtered := active.FilterByDateRange(startDate, endDate)

	insights := calculateInsights(active, filtered, startDate, endDate)

	// Anomalies and price creep both run against the full active history
	// (not `filtered`) — see anomaliesForPeriod / priceCreepForDisplay doc
	// comments for why. anomaliesForPeriod then window-filters its result
	// for display only; priceCreepForDisplay is never window-filtered.
	anomalyViews, anomalyTotal := anomaliesForPeriod(active, startDate, endDate)
	creepRows, creepTotal := priceCreepForDisplay(active)

	pageData := map[string]interface{}{
		"Title":           "Insights",
		"ActiveTab":       "insights",
		"Insights":        insights,
		"PaceVerdict":     BuildPaceVerdict(insights.Velocity),
		"StartDate":       startDate.Format("2006-01-02"),
		"EndDate":         endDate.Format("2006-01-02"),
		"MinDate":         minDate.Format("2006-01-02"),
		"MaxDate":         maxDate.Format("2006-01-02"),
		"Preset":          preset,
		"Anomalies":       anomalyViews,
		"AnomalyTotal":    anomalyTotal,
		"PriceCreep":      creepRows,
		"PriceCreepTotal": creepTotal,
	}

	templates.AttachDuplicateCount(pageData, loader)
	if renderer != nil {
		_ = renderer.Render(w, "base", pageData)
	} else {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><h1>Insights</h1><p>Coming soon...</p></body></html>"))
	}
}

func handleRecurringPartial(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	recurring := annotateRecurringWithMajorExpense(detectRecurringPaymentsAt(data.Active(), data.MaxDate()))

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
		_ = renderer.RenderPartial(w, "recurring-payments", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
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

	trends := loadAndAnalyzeTrends(data.Active(), startDate, endDate)

	partialData := map[string]interface{}{
		"CategoryTrends": trends,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "category-trends", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
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

	trends := loadAndAnalyzeTrends(data.Active(), startDate, endDate)

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
	_ = json.NewEncoder(w).Encode(chartData)
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

	active := data.Active()
	filtered := active.FilterByDateRange(startDate, endDate)
	velocity := calculateSpendingVelocity(filtered, active)

	partialData := map[string]interface{}{
		"Velocity": velocity,
	}

	if renderer != nil {
		_ = renderer.RenderPartial(w, "spending-velocity", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}

func handleIncomePartial(w http.ResponseWriter, r *http.Request) {
	data, err := loader.LoadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	income := AnalyzeIncomePatterns(data.Active())

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
		_ = renderer.RenderPartial(w, "income-patterns", partialData)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(partialData)
	}
}
